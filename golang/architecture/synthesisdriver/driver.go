// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/interpretationclosure"
	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
	"github.com/globulario/sensei/golang/architecture/workspacecontract"
)

// Run synchronously drives one fresh O1 session through the existing O2, O3,
// and O4 owners. O2 interpretation output is candidate knowledge until the
// separately injected InterpretationAuthority closes it. The driver stops at
// candidate-ready-for-admission, an advisory interpretation, an O1 terminal
// failure, a typed capability stop, or the separate O7 step limit.
//
// When a checkpoint store is injected, Run records a durable boundary before
// each owner call, so the session it drives can be continued by Resume in a
// later process.
func Run(ctx context.Context, initial synthesis.SessionState, config Config) (Result, error) {
	if err := validateConfig(initial, config); err != nil {
		return Result{}, err
	}
	execution := &execution{
		config:    config,
		state:     initial,
		startedAt: config.Now().UTC().Format(time.RFC3339),
		trace: Trace{
			ProviderExecutions:            []ProviderExecution{},
			InterpretationClosureReceipts: []interpretationclosure.Receipt{},
			GenerationHandoffs:            []runnercomposition.VerifiedGenerationHandoff{},
			EvaluationResults:             nil,
			Events:                        []synthesis.Event{},
		},
	}
	return execution.drive(ctx)
}

// Resume continues an interrupted governed session from an exact durable
// boundary.
//
// It deliberately does NOT accept a synthesis.SessionState: a caller holding
// raw O1 state has no way to prove that state is the one a real session
// reached, nor what it had already consumed. Authority comes from the
// checkpoint — verified here — and permission comes from the assessment.
//
// Resume never reinterprets completed history. It calls no provider to redo an
// accepted Interpretation, asks no planner to replace an accepted Plan, and
// reruns no evaluator to refresh a finished O4 decision. It continues from the
// typed O1 phase captured at the boundary.
//
// The assessment is returned in every case, including refusal, because it is
// the evidence of the decision. On refusal the Result is zero and NO owner call
// has been made.
func Resume(ctx context.Context, checkpoint Checkpoint, binding ResumeBinding, config Config) (Result, ResumeAssessment, error) {
	if config.Now == nil {
		return Result{}, ResumeAssessment{}, errors.New("synthesisdriver: clock is required")
	}
	assessment, err := AssessResume(checkpoint, binding, config.Now)
	if err != nil {
		return Result{}, ResumeAssessment{}, err
	}
	if !assessment.Allowed() {
		return Result{}, assessment, nil
	}

	// The checkpoint was verified by the assessment; the capabilities needed
	// to make the NEXT owner call still have to be present in this process.
	state := checkpoint.SessionState.ToSessionState()
	if err := validateResumeConfig(state, checkpoint, config); err != nil {
		return Result{}, assessment, err
	}

	startedAt := checkpoint.RunStartedAt
	if strings.TrimSpace(startedAt) == "" {
		startedAt = config.Now().UTC().Format(time.RFC3339)
	}
	previous := checkpoint.CheckpointDigestSHA256
	execution := &execution{
		config:         config,
		state:          state,
		interpretation: checkpoint.Interpretation,
		plan:           checkpoint.Plan,
		carried:        NormalizeCheckpoint(checkpoint).Trace,
		stepsConsumed:  checkpoint.StepsConsumed,
		startedAt:      startedAt,
		sequence:       checkpoint.Sequence,
		// The resumed execution's first new boundary continues this
		// checkpoint's chain rather than starting a second history.
		previousCheckpointDigest: &previous,
		trace: Trace{
			ProviderExecutions: []ProviderExecution{},
			GenerationHandoffs: []runnercomposition.VerifiedGenerationHandoff{},
			EvaluationResults:  nil,
			Events:             []synthesis.Event{},
		},
	}
	result, err := execution.drive(ctx)
	return result, assessment, err
}

func executeProvider(ctx context.Context, provider providerport.Provider, request providerport.Request, now func() time.Time) (ProviderExecution, error) {
	result, batch, receipt, err := providerport.Run(ctx, provider, request, now)
	if err != nil {
		return ProviderExecution{}, err
	}
	return ProviderExecution{Request: request, Result: result, ObservationBatch: batch, Receipt: receipt}, nil
}

// sealedButUncarriedCandidateDigestSHA256 lets a caller that only knows a
// sealed candidate's digest -- not the full runnercomposition.CandidateArtifact
// struct -- still have Receipt.CandidateArtifactDigestSHA256 stamped
// correctly. This is deliberately separate from candidate/Result.Candidate:
// runnercomposition.Run's DispositionDigestMismatch path seals the
// artifact and stamps RunnerReceipt.CandidateArtifactDigestSHA256 before
// discovering the mismatch, but the driver's PhaseAttempting case only
// ever has the HANDOFF (receipts/digests) at that point, never the full
// CandidateArtifact object the store actually holds -- fabricating one
// with only the digest field populated to satisfy the `candidate`
// parameter would populate Result.Candidate with a struct that looks
// complete but is mostly zero-valued/fabricated, which is worse than
// leaving it correctly nil. Pass nil here whenever candidate itself
// already carries (or correctly lacks) the true digest.
//
// carried is the evidence recorded before a restart. It is merged AHEAD of
// this process's own evidence so a receipt stamped after a resume describes
// the whole session, not only the part this process witnessed.
func finishResult(
	state synthesis.SessionState,
	interpretation *synthesis.Interpretation,
	plan *synthesis.Plan,
	candidate *runnercomposition.CandidateArtifact,
	sealedButUncarriedCandidateDigestSHA256 *string,
	trace Trace,
	carried CheckpointTrace,
	step int,
	disposition Disposition,
	detail string,
	startedAt string,
	now func() time.Time,
) (Result, error) {
	o2 := append([]string{}, carried.O2ReceiptDigestsSHA256...)
	for _, execution := range trace.ProviderExecutions {
		o2 = append(o2, execution.Receipt.ReceiptDigestSHA256)
	}
	closures := append([]string{}, carried.InterpretationClosureReceiptDigestsSHA256...)
	for _, receipt := range trace.InterpretationClosureReceipts {
		closures = append(closures, receipt.ReceiptDigestSHA256)
	}
	runners := append([]string{}, carried.RunnerReceiptDigestsSHA256...)
	for _, handoff := range trace.GenerationHandoffs {
		runners = append(runners, handoff.RunnerReceipt.RunnerReceiptDigestSHA256)
	}
	evaluations := append([]string{}, carried.EvaluationReceiptDigestsSHA256...)
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
	} else if sealedButUncarriedCandidateDigestSHA256 != nil {
		value := *sealedButUncarriedCandidateDigestSHA256
		candidateDigest = &value
	}
	receipt := RunReceipt{
		SchemaVersion:          RunReceiptSchemaVersion,
		ReceiptID:              fmt.Sprintf("o7.%s.%d.%s", state.Session.SessionDigestSHA256[:16], step, disposition),
		GeneratedBy:            GeneratedBy,
		SessionDigestSHA256:    state.Session.SessionDigestSHA256,
		FinalPhase:             string(state.Phase),
		Disposition:            disposition,
		StepCount:              step,
		O2ReceiptDigestsSHA256: o2,
		InterpretationClosureReceiptDigestsSHA256: closures,
		RunnerReceiptDigestsSHA256:                runners,
		EvaluationReceiptDigestsSHA256:            evaluations,
		SynthesisReceiptDigestSHA256:              synthesisReceipt,
		CandidateArtifactDigestSHA256:             candidateDigest,
		Detail:                                    strings.TrimSpace(detail),
		StartedAt:                                 startedAt,
		CompletedAt:                               now().UTC().Format(time.RFC3339),
	}
	finalized, err := finalizeRunReceipt(receipt)
	if err != nil {
		return Result{}, err
	}
	return Result{
		SessionState:   state,
		Interpretation: interpretation,
		Plan:           plan,
		Candidate:      candidate,
		Trace:          trace,
		Receipt:        finalized,
	}, nil
}

func validateConfig(initial synthesis.SessionState, config Config) error {
	if initial.Phase != synthesis.PhaseCreated {
		return fmt.Errorf("synthesisdriver: O7 v1 requires a fresh created session, got %q", initial.Phase)
	}
	if initial.Session.SessionDigestSHA256 == "" {
		return errors.New("synthesisdriver: initial session has no digest")
	}
	if err := validateCapabilities(config); err != nil {
		return err
	}
	if config.WorkspaceIdentity.Binding.RepositoryDomain != initial.Session.RepositoryDomain ||
		config.WorkspaceIdentity.Binding.Revision == nil || *config.WorkspaceIdentity.Binding.Revision != initial.Session.BaseRevision ||
		workspaceIdentityDigestOrEmpty(config) != initial.Session.WorkspaceIdentityDigestSHA256 {
		return errors.New("synthesisdriver: workspace identity does not match the session repository/base binding")
	}
	return nil
}

// validateResumeConfig applies the same capability requirements to a resumed
// process. The checkpoint proves what the session IS; it cannot prove this
// process can still reach the owners needed to continue it, so the workspace
// binding is re-checked against the state the checkpoint carried.
func validateResumeConfig(state synthesis.SessionState, checkpoint Checkpoint, config Config) error {
	if err := validateCapabilities(config); err != nil {
		return err
	}
	if config.MaxSteps != checkpoint.MaxSteps {
		// max_steps is immutable for the life of the session. Accepting a new
		// value here would be exactly the budget refill section 7 forbids,
		// dressed up as configuration.
		return fmt.Errorf("synthesisdriver: max_steps is immutable across restart: checkpoint %d, config %d", checkpoint.MaxSteps, config.MaxSteps)
	}
	if config.WorkspaceIdentity.Binding.RepositoryDomain != state.Session.RepositoryDomain ||
		config.WorkspaceIdentity.Binding.Revision == nil || *config.WorkspaceIdentity.Binding.Revision != state.Session.BaseRevision ||
		workspaceIdentityDigestOrEmpty(config) != state.Session.WorkspaceIdentityDigestSHA256 {
		return errors.New("synthesisdriver: workspace identity does not match the checkpointed session repository/base binding")
	}
	return nil
}

func validateCapabilities(config Config) error {
	if config.InterpretationProvider == nil || config.InterpretationAuthority == nil || config.PlanningProvider == nil || config.GenerationFactory == nil || config.EvaluationEngine == nil || config.CandidateStore == nil {
		return errors.New("synthesisdriver: interpretation-provider, interpretation-authority, planning-provider, generation, evaluation, and candidate-store capabilities are required")
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
	if _, err := workspacecontract.IdentityDigest(config.WorkspaceIdentity); err != nil {
		return fmt.Errorf("synthesisdriver: workspace identity: %w", err)
	}
	// A store without the identity O7 must stamp into every checkpoint would
	// produce boundaries that cannot be resumed, discovered only at restart.
	if config.CheckpointStore != nil {
		if err := validateCheckpointBinding(config.CheckpointBinding); err != nil {
			return err
		}
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

func validateCheckpointBinding(binding CheckpointBinding) error {
	if strings.TrimSpace(binding.TaskID) == "" {
		return errors.New("synthesisdriver: a checkpoint store requires the task id to stamp into each boundary")
	}
	if !isSHA256(binding.TaskControlStateDigestSHA256) {
		return errors.New("synthesisdriver: a checkpoint store requires the current task control-state digest")
	}
	if binding.TaskControlGeneration < 0 {
		return errors.New("synthesisdriver: task control generation cannot be negative")
	}
	return nil
}

func workspaceIdentityDigestOrEmpty(config Config) string {
	digest, err := workspacecontract.IdentityDigest(config.WorkspaceIdentity)
	if err != nil {
		return ""
	}
	return digest
}
