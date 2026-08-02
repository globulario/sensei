// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
	"github.com/globulario/sensei/golang/architecture/workspacecontract"
)

// Run synchronously drives one fresh O1 session through the existing O2, O3,
// and O4 owners. It stops at candidate-ready-for-admission, an O1 terminal
// failure, a typed capability stop, or the separate O7 step limit.
func Run(ctx context.Context, initial synthesis.SessionState, config Config) (Result, error) {
	if err := validateConfig(initial, config); err != nil {
		return Result{}, err
	}
	startedAt := config.Now().UTC().Format(time.RFC3339)
	state := initial
	trace := Trace{
		ProviderExecutions: []ProviderExecution{},
		GenerationHandoffs: []runnercomposition.VerifiedGenerationHandoff{},
		EvaluationResults:  nil,
		Events:             []synthesis.Event{},
	}
	var interpretation *synthesis.Interpretation
	var plan *synthesis.Plan
	var candidate *runnercomposition.CandidateArtifact

	for step := 1; step <= config.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		switch state.Phase {
		case synthesis.PhaseCreated:
			request, err := buildInterpretationRequest(state, config.InterpretationPolicy)
			if err != nil {
				return Result{}, err
			}
			execution, err := executeProvider(ctx, config.InterpretationProvider, request, config.Now)
			if err != nil {
				return Result{}, fmt.Errorf("synthesisdriver: interpretation provider: %w", err)
			}
			trace.ProviderExecutions = append(trace.ProviderExecutions, execution)
			if execution.Result.TerminalOutcome != providerport.OutcomeCompleted {
				return finishResult(state, interpretation, plan, candidate, trace, step, DispositionProviderStopped,
					fmt.Sprintf("interpretation provider ended with %q: %s", execution.Result.TerminalOutcome, execution.Result.Detail), startedAt, config.Now)
			}
			if execution.Result.InterpretationPayload == nil {
				return Result{}, errors.New("synthesisdriver: completed interpretation result has no payload")
			}
			accepted, err := detached(*execution.Result.InterpretationPayload)
			if err != nil {
				return Result{}, err
			}
			command, err := providerport.MapToCommand(state, execution.Request, execution.Result, config.Now().UTC().Format(time.RFC3339))
			if err != nil {
				return Result{}, fmt.Errorf("synthesisdriver: map interpretation: %w", err)
			}
			next, events, err := synthesis.Transition(state, command)
			if err != nil {
				return Result{}, fmt.Errorf("synthesisdriver: record interpretation: %w", err)
			}
			state = next
			interpretation = &accepted
			trace.Events = append(trace.Events, events...)

		case synthesis.PhasePlanning:
			if interpretation == nil {
				return Result{}, errors.New("synthesisdriver: planning phase has no accepted interpretation")
			}
			request, err := buildPlanningRequest(state, *interpretation, config.PlanningPolicy)
			if err != nil {
				return Result{}, err
			}
			execution, err := executeProvider(ctx, config.PlanningProvider, request, config.Now)
			if err != nil {
				return Result{}, fmt.Errorf("synthesisdriver: planning provider: %w", err)
			}
			trace.ProviderExecutions = append(trace.ProviderExecutions, execution)
			if execution.Result.TerminalOutcome != providerport.OutcomeCompleted {
				return finishResult(state, interpretation, plan, candidate, trace, step, DispositionProviderStopped,
					fmt.Sprintf("planning provider ended with %q: %s", execution.Result.TerminalOutcome, execution.Result.Detail), startedAt, config.Now)
			}
			if execution.Result.PlanningPayload == nil {
				return Result{}, errors.New("synthesisdriver: completed planning result has no payload")
			}
			accepted, err := detached(*execution.Result.PlanningPayload)
			if err != nil {
				return Result{}, err
			}
			command, err := providerport.MapToCommand(state, execution.Request, execution.Result, config.Now().UTC().Format(time.RFC3339))
			if err != nil {
				return Result{}, fmt.Errorf("synthesisdriver: map planning result: %w", err)
			}
			next, events, err := synthesis.Transition(state, command)
			if err != nil {
				return Result{}, fmt.Errorf("synthesisdriver: record plan: %w", err)
			}
			state = next
			plan = &accepted
			trace.Events = append(trace.Events, events...)

		case synthesis.PhasePlanned, synthesis.PhaseRetry:
			next, events, err := synthesis.Transition(state, synthesis.StartAttemptCommand{})
			if err != nil {
				return Result{}, fmt.Errorf("synthesisdriver: start attempt: %w", err)
			}
			state = next
			trace.Events = append(trace.Events, events...)

		case synthesis.PhaseReplan:
			next, events, err := synthesis.Transition(state, synthesis.StartPlanningCommand{})
			if err != nil {
				return Result{}, fmt.Errorf("synthesisdriver: start replan: %w", err)
			}
			state = next
			trace.Events = append(trace.Events, events...)

		case synthesis.PhaseAttempting:
			if plan == nil {
				return Result{}, errors.New("synthesisdriver: attempting phase has no accepted plan")
			}
			handoff, err := runnercomposition.Run(
				ctx,
				state,
				config.WorkspaceIdentity,
				config.RepositoryRoot,
				*plan,
				config.GenerationFactory,
				config.CandidateStore,
				config.GenerationPolicy,
				config.Now,
			)
			if err != nil {
				return Result{}, fmt.Errorf("synthesisdriver: O3 generation: %w", err)
			}
			trace.GenerationHandoffs = append(trace.GenerationHandoffs, handoff)
			if handoff.RunnerReceipt.Disposition != runnercomposition.DispositionVerified {
				return finishResult(state, interpretation, plan, candidate, trace, step, DispositionRunnerStopped,
					fmt.Sprintf("O3 ended with %q: %s", handoff.RunnerReceipt.Disposition, handoff.RunnerReceipt.FailureDetail), startedAt, config.Now)
			}
			evaluated, err := config.EvaluationEngine.Evaluate(ctx, state, handoff)
			if err != nil {
				return Result{}, fmt.Errorf("synthesisdriver: O4 evaluation: %w", err)
			}
			trace.EvaluationResults = append(trace.EvaluationResults, evaluated)
			trace.Events = append(trace.Events, evaluated.Events...)
			state = evaluated.SessionState
			if evaluated.Candidate != nil {
				copyCandidate, err := detached(*evaluated.Candidate)
				if err != nil {
					return Result{}, err
				}
				candidate = &copyCandidate
			}

		case synthesis.PhaseEvaluating:
			return Result{}, errors.New("synthesisdriver: external evaluating state is not resumable in O7 v1; O4 must complete within the attempt step")

		case synthesis.PhaseSucceeded:
			return finishResult(state, interpretation, plan, candidate, trace, step, DispositionCandidateReady,
				"candidate is ready to be submitted to O5 admission", startedAt, config.Now)

		case synthesis.PhaseFailed:
			return finishResult(state, interpretation, plan, candidate, trace, step, DispositionTerminalFailure,
				"O1 reached a governed terminal failure", startedAt, config.Now)

		default:
			return Result{}, fmt.Errorf("synthesisdriver: unsupported O1 phase %q", state.Phase)
		}
	}
	return finishResult(state, interpretation, plan, candidate, trace, config.MaxSteps, DispositionStepLimitReached,
		fmt.Sprintf("O7 reached immutable max_steps=%d", config.MaxSteps), startedAt, config.Now)
}

func executeProvider(ctx context.Context, provider providerport.Provider, request providerport.Request, now func() time.Time) (ProviderExecution, error) {
	result, batch, receipt, err := providerport.Run(ctx, provider, request, now)
	if err != nil {
		return ProviderExecution{}, err
	}
	return ProviderExecution{Request: request, Result: result, ObservationBatch: batch, Receipt: receipt}, nil
}

func finishResult(
	state synthesis.SessionState,
	interpretation *synthesis.Interpretation,
	plan *synthesis.Plan,
	candidate *runnercomposition.CandidateArtifact,
	trace Trace,
	step int,
	disposition Disposition,
	detail string,
	startedAt string,
	now func() time.Time,
) (Result, error) {
	o2 := make([]string, 0, len(trace.ProviderExecutions))
	for _, execution := range trace.ProviderExecutions {
		o2 = append(o2, execution.Receipt.ReceiptDigestSHA256)
	}
	runners := make([]string, 0, len(trace.GenerationHandoffs))
	for _, handoff := range trace.GenerationHandoffs {
		runners = append(runners, handoff.RunnerReceipt.RunnerReceiptDigestSHA256)
	}
	evaluations := make([]string, 0, len(trace.EvaluationResults))
	for _, result := range trace.EvaluationResults {
		if result.Receipt != nil {
			evaluations = append(evaluations, result.Receipt.ReceiptDigestSHA256)
		}
	}
	var synthesisReceipt *string
	if state.Receipt != nil {
		value := state.Receipt.ReceiptDigestSHA256
		synthesisReceipt = &value
	}
	var candidateDigest *string
	if candidate != nil {
		value := candidate.CandidateArtifactDigestSHA256
		candidateDigest = &value
	}
	receipt := RunReceipt{
		SchemaVersion:                       RunReceiptSchemaVersion,
		ReceiptID:                           fmt.Sprintf("o7.%s.%d.%s", state.Session.SessionDigestSHA256[:16], step, disposition),
		GeneratedBy:                         GeneratedBy,
		SessionDigestSHA256:                 state.Session.SessionDigestSHA256,
		FinalPhase:                          string(state.Phase),
		Disposition:                         disposition,
		StepCount:                           step,
		O2ReceiptDigestsSHA256:              o2,
		RunnerReceiptDigestsSHA256:          runners,
		EvaluationReceiptDigestsSHA256:      evaluations,
		SynthesisReceiptDigestSHA256:        synthesisReceipt,
		CandidateArtifactDigestSHA256:       candidateDigest,
		Detail:                              strings.TrimSpace(detail),
		StartedAt:                           startedAt,
		CompletedAt:                         now().UTC().Format(time.RFC3339),
	}
	finalized, err := finalizeRunReceipt(receipt)
	if err != nil {
		return Result{}, err
	}
	return Result{
		SessionState:  state,
		Interpretation: interpretation,
		Plan:            plan,
		Candidate:       candidate,
		Trace:           trace,
		Receipt:         finalized,
	}, nil
}

func validateConfig(initial synthesis.SessionState, config Config) error {
	if initial.Phase != synthesis.PhaseCreated {
		return fmt.Errorf("synthesisdriver: O7 v1 requires a fresh created session, got %q", initial.Phase)
	}
	if initial.Session.SessionDigestSHA256 == "" {
		return errors.New("synthesisdriver: initial session has no digest")
	}
	if config.InterpretationProvider == nil || config.PlanningProvider == nil || config.GenerationFactory == nil || config.EvaluationEngine == nil || config.CandidateStore == nil {
		return errors.New("synthesisdriver: provider, generation, evaluation, and candidate-store capabilities are required")
	}
	if config.Now == nil {
		return errors.New("synthesisdriver: clock is required")
	}
	if config.MaxSteps <= 0 {
		return errors.New("synthesisdriver: max_steps must be positive")
	}
	if !filepath.IsAbs(config.RepositoryRoot) {
		return fmt.Errorf("synthesisdriver: repository root must be absolute: %q", config.RepositoryRoot)
	}
	identityDigest, err := workspacecontract.IdentityDigest(config.WorkspaceIdentity)
	if err != nil {
		return fmt.Errorf("synthesisdriver: workspace identity: %w", err)
	}
	if config.WorkspaceIdentity.Binding.RepositoryDomain != initial.Session.RepositoryDomain ||
		config.WorkspaceIdentity.Binding.Revision == nil || *config.WorkspaceIdentity.Binding.Revision != initial.Session.BaseRevision ||
		identityDigest != initial.Session.WorkspaceIdentityDigestSHA256 {
		return errors.New("synthesisdriver: workspace identity does not match the session repository/base binding")
	}
	if err := validateProviderPolicy(config.InterpretationPolicy); err != nil {
		return err
	}
	if err := validateProviderPolicy(config.PlanningPolicy); err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339, config.GenerationPolicy.DeadlineAt); err != nil {
		return fmt.Errorf("synthesisdriver: generation deadline_at must be RFC3339: %w", err)
	}
	return nil
}
