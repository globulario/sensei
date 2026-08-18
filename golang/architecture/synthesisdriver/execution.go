// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/globulario/sensei/golang/architecture/interpretationclosure"
	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// execution is the single internal dispatcher both fresh and resumed runs
// drive. Keeping one implementation is the point: two orchestration paths
// would be two places for the phase vocabulary, the step accounting, and the
// terminal-finalization rules to drift apart, and only one of them would be
// covered by any given test.
//
// A fresh Run and a Resume differ only in how this struct is SEEDED — from an
// initial session, or from a verified checkpoint plus an allowed assessment.
// After that the loop is identical.
type execution struct {
	config Config

	state          synthesis.SessionState
	interpretation *synthesis.Interpretation
	plan           *synthesis.Plan
	candidate      *runnercomposition.CandidateArtifact
	trace          Trace

	// carried is the evidence recorded BEFORE a restart, referenced by digest.
	// The final receipt merges it ahead of this process's own evidence so a
	// receipt stamped after a resume describes the whole session rather than
	// only the part this process happened to witness.
	carried CheckpointTrace

	// stepsConsumed is the immutable count this execution inherited. A fresh
	// run inherits zero; a resumed run inherits exactly what the checkpoint
	// recorded, so restart is never a budget refill.
	stepsConsumed int

	startedAt string

	// checkpoint chain position, advanced as boundaries are written
	sequence                 int
	previousCheckpointDigest *string
}

// drive runs the phase loop from the next unconsumed step to the immutable
// step limit.
func (e *execution) drive(ctx context.Context) (Result, error) {
	for step := e.stepsConsumed + 1; step <= e.config.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}

		// The boundary is recorded BEFORE the owner call this step will make,
		// so an interrupted call resumes from the state that preceded it. The
		// unfinished call is not completed history, and any artifact it left
		// behind is unreferenced and therefore non-authoritative.
		if err := e.saveCheckpoint(ctx, step); err != nil {
			return Result{}, err
		}

		finished, err := e.step(ctx, step)
		if err != nil {
			return Result{}, err
		}
		e.stepsConsumed = step
		if finished != nil {
			return *finished, nil
		}
	}
	return e.finish(e.config.MaxSteps, DispositionStepLimitReached,
		fmt.Sprintf("O7 reached immutable max_steps=%d", e.config.MaxSteps))
}

// step performs exactly one phase transition. A non-nil Result means the run
// reached a stop and the loop must end.
func (e *execution) step(ctx context.Context, step int) (*Result, error) {
	switch e.state.Phase {
	case synthesis.PhaseCreated:
		return e.stepInterpretation(ctx, step)
	case synthesis.PhasePlanning:
		return e.stepPlanning(ctx, step)
	case synthesis.PhasePlanned, synthesis.PhaseRetry:
		next, events, err := synthesis.Transition(e.state, synthesis.StartAttemptCommand{})
		if err != nil {
			return nil, fmt.Errorf("synthesisdriver: start attempt: %w", err)
		}
		e.state = next
		e.trace.Events = append(e.trace.Events, events...)
		return nil, nil
	case synthesis.PhaseReplan:
		next, events, err := synthesis.Transition(e.state, synthesis.StartPlanningCommand{})
		if err != nil {
			return nil, fmt.Errorf("synthesisdriver: start replan: %w", err)
		}
		e.state = next
		e.trace.Events = append(e.trace.Events, events...)
		return nil, nil
	case synthesis.PhaseAttempting:
		return e.stepAttempt(ctx, step)
	case synthesis.PhaseEvaluating:
		return nil, errors.New("synthesisdriver: external evaluating state is not resumable in O7 v1; O4 must complete within the attempt step")
	case synthesis.PhaseSucceeded:
		result, err := e.finish(step, DispositionCandidateReady, "candidate is ready to be submitted to O5 admission")
		return &result, err
	case synthesis.PhaseFailed:
		result, err := e.finish(step, DispositionTerminalFailure, "O1 reached a governed terminal failure")
		return &result, err
	default:
		return nil, fmt.Errorf("synthesisdriver: unsupported O1 phase %q", e.state.Phase)
	}
}

func (e *execution) stepInterpretation(ctx context.Context, step int) (*Result, error) {
	request, err := buildInterpretationRequest(e.state, e.config.InterpretationPolicy)
	if err != nil {
		return nil, err
	}
	execution, err := executeProvider(ctx, e.config.InterpretationProvider, request, e.config.Now)
	if err != nil {
		return nil, fmt.Errorf("synthesisdriver: interpretation provider: %w", err)
	}
	e.trace.ProviderExecutions = append(e.trace.ProviderExecutions, execution)
	if execution.Result.TerminalOutcome != providerport.OutcomeCompleted {
		result, err := e.finish(step, DispositionProviderStopped,
			fmt.Sprintf("interpretation provider ended with %q: %s", execution.Result.TerminalOutcome, execution.Result.Detail))
		return &result, err
	}

	// O2 has authority to produce a candidate interpretation, not to
	// promote it. This mapper re-validates and detaches the exact O2
	// payload while deliberately returning data rather than an O1
	// command.
	accepted, err := providerport.MapInterpretationCandidate(e.state, execution.Request, execution.Result)
	if err != nil {
		return nil, fmt.Errorf("synthesisdriver: map interpretation candidate: %w", err)
	}
	e.interpretation = &accepted

	closureReceipt, err := e.config.InterpretationAuthority.Assess(ctx, InterpretationAuthorityRequest{
		RepositoryRoot: e.config.RepositoryRoot,
		Session:        e.state.Session,
		Interpretation: accepted,
	})
	if err != nil {
		return nil, fmt.Errorf("synthesisdriver: interpretation authority: %w", err)
	}
	if err := interpretationclosure.Verify(
		closureReceipt,
		accepted.InterpretationDigestSHA256,
		e.state.Session.BaseRevision,
		e.state.Session.GraphAuthorityDigestSHA256,
		e.state.Session.ClosureDigestSHA256,
	); err != nil {
		return nil, fmt.Errorf("synthesisdriver: interpretation authority returned invalid receipt: %w", err)
	}
	e.trace.InterpretationClosureReceipts = append(e.trace.InterpretationClosureReceipts, closureReceipt)

	if closureReceipt.Authority != interpretationclosure.AuthorityGoverning {
		result, err := e.finish(step, DispositionInterpretationAdvisory,
			fmt.Sprintf("interpretation remains advisory: blockers=%v", closureReceipt.Blockers))
		return &result, err
	}

	// This is the only O7 promotion boundary. The constructor
	// independently recomputes the interpretation digest and requires
	// the closure receipt to be governing for the exact repository and
	// graph identity already bound into the session.
	command, err := synthesis.NewRecordInterpretationCommand(e.state, accepted, closureReceipt)
	if err != nil {
		return nil, fmt.Errorf("synthesisdriver: promote interpretation: %w", err)
	}
	next, events, err := synthesis.Transition(e.state, command)
	if err != nil {
		return nil, fmt.Errorf("synthesisdriver: record certified interpretation: %w", err)
	}
	e.state = next
	e.trace.Events = append(e.trace.Events, events...)
	return nil, nil
}

func (e *execution) stepPlanning(ctx context.Context, step int) (*Result, error) {
	if e.interpretation == nil {
		return nil, errors.New("synthesisdriver: planning phase has no accepted interpretation")
	}
	request, err := buildPlanningRequest(e.state, *e.interpretation, e.config.PlanningPolicy)
	if err != nil {
		return nil, err
	}
	execution, err := executeProvider(ctx, e.config.PlanningProvider, request, e.config.Now)
	if err != nil {
		return nil, fmt.Errorf("synthesisdriver: planning provider: %w", err)
	}
	e.trace.ProviderExecutions = append(e.trace.ProviderExecutions, execution)
	if execution.Result.TerminalOutcome != providerport.OutcomeCompleted {
		result, err := e.finish(step, DispositionProviderStopped,
			fmt.Sprintf("planning provider ended with %q: %s", execution.Result.TerminalOutcome, execution.Result.Detail))
		return &result, err
	}
	if execution.Result.PlanningPayload == nil {
		return nil, errors.New("synthesisdriver: completed planning result has no payload")
	}
	accepted, err := detached(*execution.Result.PlanningPayload)
	if err != nil {
		return nil, err
	}
	command, err := providerport.MapToCommand(e.state, execution.Request, execution.Result, e.config.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("synthesisdriver: map planning result: %w", err)
	}
	next, events, err := synthesis.Transition(e.state, command)
	if err != nil {
		return nil, fmt.Errorf("synthesisdriver: record plan: %w", err)
	}
	e.state = next
	e.plan = &accepted
	e.trace.Events = append(e.trace.Events, events...)
	return nil, nil
}

func (e *execution) stepAttempt(ctx context.Context, step int) (*Result, error) {
	if e.plan == nil {
		return nil, errors.New("synthesisdriver: attempting phase has no accepted plan")
	}
	handoff, err := runnercomposition.Run(
		ctx,
		e.state,
		e.config.WorkspaceIdentity,
		e.config.RepositoryRoot,
		*e.plan,
		e.config.GenerationFactory,
		e.config.CandidateStore,
		e.config.GenerationPolicy,
		e.config.Now,
	)
	if err != nil {
		return nil, fmt.Errorf("synthesisdriver: O3 generation: %w", err)
	}
	e.trace.GenerationHandoffs = append(e.trace.GenerationHandoffs, handoff)
	if handoff.RunnerReceipt.Disposition != runnercomposition.DispositionVerified {
		// A live review found this attempt's own handoff can
		// already carry a sealed candidate digest even though
		// it is NOT verified: runnercomposition.Run's
		// DispositionDigestMismatch path seals the artifact
		// (store.Put) and stamps
		// RunnerReceipt.CandidateArtifactDigestSHA256 BEFORE
		// discovering the mismatch (run.go's own hard law 11 --
		// a mismatched candidate is sealed as exactly what it
		// actually is, never repaired into agreement with what
		// the provider claimed). `candidate` (the execution
		// field) only ever gets set from a VERIFIED attempt's O4
		// evaluation, so for THIS attempt's own non-verified
		// handoff it stays whatever an EARLIER attempt left it
		// as (nil on a run's first attempt) -- passing
		// handoff.RunnerReceipt.CandidateArtifactDigestSHA256
		// here lets finishResult stamp the receipt correctly
		// regardless: it is non-nil only for
		// DispositionDigestMismatch (and DispositionVerified,
		// which never reaches this branch), nil for every other
		// non-verified disposition (snapshot/workspace-init/
		// provider-construction/o2-run-error/o2-non-completed/
		// workspace-freeze/evidence-computation/seal failures),
		// exactly matching which of them actually sealed
		// anything.
		result, err := e.finishWithSealedDigest(step, DispositionRunnerStopped,
			fmt.Sprintf("O3 ended with %q: %s", handoff.RunnerReceipt.Disposition, handoff.RunnerReceipt.FailureDetail),
			handoff.RunnerReceipt.CandidateArtifactDigestSHA256)
		return &result, err
	}
	evaluated, err := e.config.EvaluationEngine.Evaluate(ctx, e.state, handoff)
	if err != nil {
		return nil, fmt.Errorf("synthesisdriver: O4 evaluation: %w", err)
	}
	e.trace.EvaluationResults = append(e.trace.EvaluationResults, evaluated)
	e.trace.Events = append(e.trace.Events, evaluated.Events...)
	e.state = evaluated.SessionState
	if evaluated.Candidate != nil {
		copyCandidate, err := detached(*evaluated.Candidate)
		if err != nil {
			return nil, err
		}
		e.candidate = &copyCandidate
	}
	// evaluated.SessionState can already be PhaseSucceeded or
	// PhaseFailed here (config.EvaluationEngine.Evaluate fully
	// resolves PhaseEvaluating -> {Succeeded | Retry | Replan |
	// Failed} within this one call -- see synthesis.transitionRecordEvaluation).
	// A live review found that finalizing lazily -- falling
	// through to let the loop's next iteration hit the
	// PhaseSucceeded/PhaseFailed cases below -- has two real
	// costs: (1) if THIS was the last allowed step (step ==
	// config.MaxSteps), the loop exits without a next iteration
	// at all, and control falls to the step-limit finishResult
	// below with a terminal receipt already stamped on state --
	// which ValidateRunReceipt's own DispositionStepLimitReached
	// case explicitly rejects ("nonterminal stop cannot invent
	// an O1 terminal receipt"), turning a genuine success (or
	// governed failure) into a hard Go error and an internal-
	// defect exit for the CLI caller, silently orphaning a
	// sealed candidate with no lineage ever persisted; (2) even
	// on a non-boundary run, the lazily-finalized receipt's
	// StepCount is stamped one step later than the step that
	// actually produced the terminal transition. Finalizing
	// immediately, at the exact step the transition happened,
	// fixes both.
	if e.state.Phase.Terminal() {
		if e.state.Phase == synthesis.PhaseSucceeded {
			result, err := e.finish(step, DispositionCandidateReady, "candidate is ready to be submitted to O5 admission")
			return &result, err
		}
		result, err := e.finish(step, DispositionTerminalFailure, "O1 reached a governed terminal failure")
		return &result, err
	}
	return nil, nil
}

func (e *execution) finish(step int, disposition Disposition, detail string) (Result, error) {
	return e.finishWithSealedDigest(step, disposition, detail, nil)
}

func (e *execution) finishWithSealedDigest(step int, disposition Disposition, detail string, sealedDigest *string) (Result, error) {
	return finishResult(e.state, e.interpretation, e.plan, e.candidate, sealedDigest, e.trace, e.carried,
		step, disposition, detail, e.startedAt, e.config.Now)
}

// saveCheckpoint records the durable boundary preceding the step about to run.
// It is a no-op when no store is injected — checkpointing is a capability the
// caller grants, not something O7 assumes — and at any phase that is not a
// durable boundary.
func (e *execution) saveCheckpoint(ctx context.Context, step int) error {
	if e.config.CheckpointStore == nil || !ResumablePhase(e.state.Phase) {
		return nil
	}
	checkpoint := Checkpoint{
		SchemaVersion:                  CheckpointSchemaVersion,
		CheckpointID:                   fmt.Sprintf("o7.checkpoint.%s.%d", shortDigest(e.state.Session.SessionDigestSHA256), e.sequence+1),
		GeneratedBy:                    CheckpointGeneratedBy,
		Sequence:                       e.sequence + 1,
		PreviousCheckpointDigestSHA256: e.previousCheckpointDigest,
		SessionState:                   FromSessionState(e.state),
		Interpretation:                 e.interpretation,
		Plan:                           e.plan,
		Trace:                          e.mergedTrace(),
		StepsConsumed:                  step - 1,
		MaxSteps:                       e.config.MaxSteps,
		RepositoryDomain:               e.state.Session.RepositoryDomain,
		BaseRevision:                   e.state.Session.BaseRevision,
		WorkspaceIdentityDigestSHA256:  e.state.Session.WorkspaceIdentityDigestSHA256,
		GraphAuthorityDigestSHA256:     e.state.Session.GraphAuthorityDigestSHA256,
		TaskID:                         e.config.CheckpointBinding.TaskID,
		TaskSessionDigestSHA256:        e.state.Session.TaskSessionDigestSHA256,
		TaskControlStateDigestSHA256:   e.config.CheckpointBinding.TaskControlStateDigestSHA256,
		TaskControlGeneration:          e.config.CheckpointBinding.TaskControlGeneration,
		ClosureReportDigestSHA256:      e.state.Session.ClosureDigestSHA256,
		RunStartedAt:                   e.startedAt,
		ObservedAt:                     e.config.Now().UTC().Format(time.RFC3339),
	}
	if e.candidate != nil {
		digest := e.candidate.CandidateArtifactDigestSHA256
		checkpoint.CandidateArtifactDigestSHA256 = &digest
	}

	finalized, err := FinalizeCheckpoint(checkpoint)
	if err != nil {
		return fmt.Errorf("synthesisdriver: build checkpoint: %w", err)
	}
	if err := e.config.CheckpointStore.Save(ctx, finalized); err != nil {
		return fmt.Errorf("synthesisdriver: save checkpoint: %w", err)
	}
	e.sequence = finalized.Sequence
	digest := finalized.CheckpointDigestSHA256
	e.previousCheckpointDigest = &digest
	return nil
}

// mergedTrace is the evidence carried across the restart boundary followed by
// this process's own, in that order, so the sequence reads as the session
// actually ran.
func (e *execution) mergedTrace() CheckpointTrace {
	merged := CheckpointTrace{
		O2ReceiptDigestsSHA256:                    append([]string{}, e.carried.O2ReceiptDigestsSHA256...),
		InterpretationClosureReceiptDigestsSHA256: append([]string{}, e.carried.InterpretationClosureReceiptDigestsSHA256...),
		RunnerReceiptDigestsSHA256:                append([]string{}, e.carried.RunnerReceiptDigestsSHA256...),
		EvaluationReceiptDigestsSHA256:            append([]string{}, e.carried.EvaluationReceiptDigestsSHA256...),
	}
	for _, execution := range e.trace.ProviderExecutions {
		merged.O2ReceiptDigestsSHA256 = append(merged.O2ReceiptDigestsSHA256, execution.Receipt.ReceiptDigestSHA256)
	}
	for _, receipt := range e.trace.InterpretationClosureReceipts {
		merged.InterpretationClosureReceiptDigestsSHA256 = append(merged.InterpretationClosureReceiptDigestsSHA256, receipt.ReceiptDigestSHA256)
	}
	for _, handoff := range e.trace.GenerationHandoffs {
		merged.RunnerReceiptDigestsSHA256 = append(merged.RunnerReceiptDigestsSHA256, handoff.RunnerReceipt.RunnerReceiptDigestSHA256)
	}
	for _, result := range e.trace.EvaluationResults {
		if result.Receipt != nil {
			merged.EvaluationReceiptDigestsSHA256 = append(merged.EvaluationReceiptDigestsSHA256, result.Receipt.ReceiptDigestSHA256)
		}
	}
	return merged
}
