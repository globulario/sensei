// SPDX-License-Identifier: AGPL-3.0-only

package synthesis

import (
	"encoding/json"
	"fmt"
)

// Transition is the O1 pure deterministic orchestration engine: the same
// SessionState plus the same Command always produces byte-for-byte
// equivalent normalized state and events. It invokes no model, runs no
// process, edits no worktree, calls no GitHub API, performs no admission,
// and reads no clock — every timestamp a resulting Receipt needs is
// caller-supplied on the command, never generated internally.
//
// Two, and only two, kinds of outcome exist:
//
//   - Illegal transition (a contract/programming violation — wrong phase
//     for this command, a cross-reference to the wrong artifact, malformed
//     schema content, a command sent to a terminal session): returns a
//     non-nil error, and state is returned UNCHANGED. Fails closed.
//   - Valid governed outcome (including refusals the O1 contract expects,
//     like budget exhaustion or identity drift): returns err == nil, a new
//     SessionState, and events explaining what happened — a terminal
//     outcome also carries a Receipt. These are not implementation errors;
//     they are the contract working as designed.
func Transition(state SessionState, command Command) (SessionState, []Event, error) {
	if state.Phase.Terminal() {
		return state, nil, fmt.Errorf("synthesis: session is terminal (%s); no further commands are accepted", state.Phase)
	}

	switch cmd := command.(type) {
	case RecordInterpretationCommand:
		return transitionRecordInterpretation(state, cmd)
	case StartPlanningCommand:
		return transitionStartPlanning(state, cmd)
	case RecordPlanCommand:
		return transitionRecordPlan(state, cmd)
	case StartAttemptCommand:
		return transitionStartAttempt(state, cmd)
	case RecordAttemptCommand:
		return transitionRecordAttempt(state, cmd)
	case RecordEvaluationCommand:
		return transitionRecordEvaluation(state, cmd)
	case EvaluatorUnavailableCommand:
		return transitionEvaluatorUnavailable(state, cmd)
	case AbortCommand:
		return transitionAbort(state, cmd)
	case ResumeCommand:
		return transitionResume(state, cmd)
	default:
		return state, nil, fmt.Errorf("synthesis: unknown command type %T", command)
	}
}

func illegal(state SessionState, format string, args ...any) (SessionState, []Event, error) {
	return state, nil, fmt.Errorf("synthesis: "+format, args...)
}

// --- Created -> Planning ---

func transitionRecordInterpretation(state SessionState, cmd RecordInterpretationCommand) (SessionState, []Event, error) {
	if state.Phase != PhaseCreated {
		return illegal(state, "RecordInterpretationCommand is only legal from %s, got %s", PhaseCreated, state.Phase)
	}
	interp := cmd.Interpretation
	if err := validateDocument(ValidateInterpretationSchema, interp); err != nil {
		return illegal(state, "interpretation failed schema validation: %v", err)
	}
	if interp.SessionDigestSHA256 != state.Session.SessionDigestSHA256 {
		return illegal(state, "interpretation references session digest %q, expected %q", interp.SessionDigestSHA256, state.Session.SessionDigestSHA256)
	}
	digest, err := InterpretationDigest(interp)
	if err != nil {
		return illegal(state, "compute interpretation digest: %v", err)
	}

	next := state
	next.InterpretationDigestSHA256 = digest
	next.Phase = PhasePlanning
	next.ExpectedPlanGeneration = 1
	return next, []Event{
		InterpretationRecordedEvent{InterpretationDigestSHA256: digest},
		PlanningStartedEvent{ExpectedPlanGeneration: 1},
	}, nil
}

// --- Replan -> Planning ---

func transitionStartPlanning(state SessionState, _ StartPlanningCommand) (SessionState, []Event, error) {
	if state.Phase != PhaseReplan {
		return illegal(state, "StartPlanningCommand is only legal from %s, got %s", PhaseReplan, state.Phase)
	}
	next := state
	next.Phase = PhasePlanning
	next.ExpectedPlanGeneration = state.PlanGeneration + 1
	return next, []Event{PlanningStartedEvent{ExpectedPlanGeneration: next.ExpectedPlanGeneration}}, nil
}

// --- Planning -> Planned ---

func transitionRecordPlan(state SessionState, cmd RecordPlanCommand) (SessionState, []Event, error) {
	if state.Phase != PhasePlanning {
		return illegal(state, "RecordPlanCommand is only legal from %s, got %s", PhasePlanning, state.Phase)
	}
	plan := cmd.Plan
	if err := validateDocument(ValidatePlanSchema, plan); err != nil {
		return illegal(state, "plan failed schema validation: %v", err)
	}
	if plan.InterpretationDigestSHA256 != state.InterpretationDigestSHA256 {
		return illegal(state, "plan references interpretation digest %q, expected the session's single interpretation %q (replanning reuses the original interpretation)", plan.InterpretationDigestSHA256, state.InterpretationDigestSHA256)
	}
	if plan.PlanGeneration != state.ExpectedPlanGeneration {
		return illegal(state, "plan generation %d does not match expected generation %d", plan.PlanGeneration, state.ExpectedPlanGeneration)
	}
	digest, err := PlanDigest(plan)
	if err != nil {
		return illegal(state, "compute plan digest: %v", err)
	}

	next := state
	next.Phase = PhasePlanned
	next.PlanGeneration = plan.PlanGeneration
	next.LatestPlanDigestSHA256 = digest
	next.ExpectedPlanGeneration = 0
	return next, []Event{PlanRecordedEvent{PlanGeneration: next.PlanGeneration, PlanDigestSHA256: digest}}, nil
}

// --- Planned|Retry -> Attempting ---

func transitionStartAttempt(state SessionState, _ StartAttemptCommand) (SessionState, []Event, error) {
	if state.Phase != PhasePlanned && state.Phase != PhaseRetry {
		return illegal(state, "StartAttemptCommand is only legal from %s or %s, got %s", PhasePlanned, PhaseRetry, state.Phase)
	}
	next := state
	next.Phase = PhaseAttempting
	next.ExpectedAttemptNumber = state.AttemptNumber + 1
	return next, []Event{AttemptStartedEvent{ExpectedAttemptNumber: next.ExpectedAttemptNumber}}, nil
}

// --- Attempting -> Evaluating ---

func transitionRecordAttempt(state SessionState, cmd RecordAttemptCommand) (SessionState, []Event, error) {
	if state.Phase != PhaseAttempting {
		return illegal(state, "RecordAttemptCommand is only legal from %s, got %s", PhaseAttempting, state.Phase)
	}
	attempt := cmd.Attempt
	if err := validateDocument(ValidateAttemptSchema, attempt); err != nil {
		return illegal(state, "attempt failed schema validation: %v", err)
	}
	if attempt.PlanDigestSHA256 != state.LatestPlanDigestSHA256 {
		return illegal(state, "attempt references plan digest %q, expected the current plan %q", attempt.PlanDigestSHA256, state.LatestPlanDigestSHA256)
	}
	if attempt.PlanGeneration != state.PlanGeneration {
		return illegal(state, "attempt references plan generation %d, expected the current generation %d", attempt.PlanGeneration, state.PlanGeneration)
	}
	if attempt.AttemptNumber != state.ExpectedAttemptNumber {
		return illegal(state, "attempt number %d does not match expected attempt number %d", attempt.AttemptNumber, state.ExpectedAttemptNumber)
	}
	digest, err := AttemptDigest(attempt)
	if err != nil {
		return illegal(state, "compute attempt digest: %v", err)
	}

	next := state
	next.Phase = PhaseEvaluating
	next.AttemptNumber = attempt.AttemptNumber
	next.LatestAttemptDigestSHA256 = digest
	next.ExpectedAttemptNumber = 0
	events := []Event{AttemptRecordedEvent{AttemptNumber: next.AttemptNumber, AttemptDigestSHA256: digest}}

	// invalid_output means the provider's output itself failed the O1 hard
	// law that provider output is untrusted input and must be closed-schema
	// validated before use — there is nothing meaningful left to evaluate.
	// This is categorically different from completed/failed/timed_out/
	// cancelled, all of which still produce a real (if unsuccessful)
	// attempt an evaluator can classify and recommend against (e.g. retry).
	// Short-circuit directly to Failed/invalid-provider-output, preserving
	// the attempt digest as evidence, rather than proceeding to Evaluating.
	if attempt.TerminalProviderStatus == ProviderStatusInvalidOutput {
		final, terminalEvents, err := terminate(next, ReasonInvalidProviderOutput,
			"provider output failed validation and cannot be evaluated", attempt.ProducedAt)
		if err != nil {
			return illegal(state, "build invalid-provider-output receipt: %v", err)
		}
		return final, append(events, terminalEvents...), nil
	}

	return next, events, nil
}

// --- Evaluating -> {Succeeded | Retry | Replan | Failed} ---

func transitionRecordEvaluation(state SessionState, cmd RecordEvaluationCommand) (SessionState, []Event, error) {
	if state.Phase != PhaseEvaluating {
		return illegal(state, "RecordEvaluationCommand is only legal from %s, got %s", PhaseEvaluating, state.Phase)
	}
	eval := cmd.Evaluation
	if err := validateDocument(ValidateEvaluationSchema, eval); err != nil {
		return illegal(state, "evaluation failed schema validation: %v", err)
	}
	if eval.AttemptDigestSHA256 != state.LatestAttemptDigestSHA256 {
		return illegal(state, "evaluation references attempt digest %q, expected the current attempt %q", eval.AttemptDigestSHA256, state.LatestAttemptDigestSHA256)
	}
	digest, err := EvaluationDigest(eval)
	if err != nil {
		return illegal(state, "compute evaluation digest: %v", err)
	}

	recorded := state
	recorded.LatestEvaluationDigestSHA256 = digest
	events := []Event{EvaluationRecordedEvent{EvaluationDigestSHA256: digest, Recommendation: eval.Recommendation}}

	switch eval.Recommendation {
	case RecommendAcceptCandidate:
		final, terminalEvents, err := terminate(recorded, ReasonCandidateReadyForAdmission,
			"candidate ready for submission to the existing admission owner", cmd.CompletedAt)
		if err != nil {
			return illegal(state, "build candidate-ready receipt: %v", err)
		}
		return final, append(events, terminalEvents...), nil

	case RecommendRetryGeneration:
		if recorded.RemainingRetryBudget <= 0 {
			final, terminalEvents, err := terminate(recorded, ReasonRetryBudgetExhausted,
				"retry recommended but the retry budget is exhausted", cmd.CompletedAt)
			if err != nil {
				return illegal(state, "build retry-budget-exhausted receipt: %v", err)
			}
			return final, append(events, terminalEvents...), nil
		}
		next := recorded
		next.Phase = PhaseRetry
		next.RemainingRetryBudget--
		return next, append(events, RetryScheduledEvent{RemainingRetryBudget: next.RemainingRetryBudget}), nil

	case RecommendReplan:
		if recorded.RemainingReplanBudget <= 0 {
			final, terminalEvents, err := terminate(recorded, ReasonReplanBudgetExhausted,
				"replan recommended but the replan budget is exhausted", cmd.CompletedAt)
			if err != nil {
				return illegal(state, "build replan-budget-exhausted receipt: %v", err)
			}
			return final, append(events, terminalEvents...), nil
		}
		next := recorded
		next.Phase = PhaseReplan
		next.RemainingReplanBudget--
		return next, append(events, ReplanScheduledEvent{RemainingReplanBudget: next.RemainingReplanBudget}), nil

	case RecommendArchitectReview:
		final, terminalEvents, err := terminate(recorded, ReasonArchitectReviewRequired,
			"evaluator recommended architect review", cmd.CompletedAt)
		if err != nil {
			return illegal(state, "build architect-review-required receipt: %v", err)
		}
		return final, append(events, terminalEvents...), nil

	case RecommendAbort:
		final, terminalEvents, err := terminate(recorded, ReasonExplicitlyAborted,
			"evaluator recommended abort", cmd.CompletedAt)
		if err != nil {
			return illegal(state, "build explicitly-aborted receipt: %v", err)
		}
		return final, append(events, terminalEvents...), nil

	default:
		return illegal(state, "evaluation carries unknown recommendation %q", eval.Recommendation)
	}
}

// --- Evaluating -> Failed (evaluator-unavailable) ---

func transitionEvaluatorUnavailable(state SessionState, cmd EvaluatorUnavailableCommand) (SessionState, []Event, error) {
	if state.Phase != PhaseEvaluating {
		return illegal(state, "EvaluatorUnavailableCommand is only legal from %s, got %s", PhaseEvaluating, state.Phase)
	}
	summary := "evaluator unavailable"
	if cmd.Detail != "" {
		summary = "evaluator unavailable: " + cmd.Detail
	}
	final, events, err := terminate(state, ReasonEvaluatorUnavailable, summary, cmd.At)
	if err != nil {
		return illegal(state, "build evaluator-unavailable receipt: %v", err)
	}
	return final, events, nil
}

// --- any non-terminal phase -> Failed (explicitly-aborted) ---

func transitionAbort(state SessionState, cmd AbortCommand) (SessionState, []Event, error) {
	summary := "explicitly aborted"
	if cmd.Reason != "" {
		summary = "explicitly aborted: " + cmd.Reason
	}
	final, events, err := terminate(state, ReasonExplicitlyAborted, summary, cmd.At)
	if err != nil {
		return illegal(state, "build explicitly-aborted receipt: %v", err)
	}
	return final, events, nil
}

// --- any non-terminal phase -> unchanged | Failed (identity-drift-refused) ---

func transitionResume(state SessionState, cmd ResumeCommand) (SessionState, []Event, error) {
	if cmd.Observed.matchesSession(state.Session) {
		// Successful resume is a no-op: state is returned byte-for-byte
		// identical, and the only event is a bare observation that carries
		// no field able to influence session identity.
		return state, []Event{ResumeValidatedEvent{}}, nil
	}
	final, events, err := terminate(state, ReasonIdentityDriftRefused,
		"observed identity does not match the session's bound identity", cmd.At)
	if err != nil {
		return illegal(state, "build identity-drift-refused receipt: %v", err)
	}
	return final, events, nil
}

// --- shared terminal-transition helper ---

// terminate builds the Receipt for state reaching TerminalReason reason at
// completedAt, sets Phase to Succeeded (only for
// ReasonCandidateReadyForAdmission) or Failed (every other reason), and
// returns the accompanying SessionTerminatedEvent.
func terminate(state SessionState, reason TerminalReason, summary, completedAt string) (SessionState, []Event, error) {
	var finalAttempt, finalEvaluation *string
	if state.LatestAttemptDigestSHA256 != "" {
		d := state.LatestAttemptDigestSHA256
		finalAttempt = &d
	}
	if state.LatestEvaluationDigestSHA256 != "" {
		d := state.LatestEvaluationDigestSHA256
		finalEvaluation = &d
	}

	receipt := Receipt{
		SchemaVersion:               ReceiptSchemaVersion,
		ReceiptID:                   "receipt." + state.Session.SessionID,
		SessionDigestSHA256:         state.Session.SessionDigestSHA256,
		TerminalReason:              reason,
		FinalAttemptDigestSHA256:    finalAttempt,
		FinalEvaluationDigestSHA256: finalEvaluation,
		// AdmissionDecisionDigestSHA256 / AdmissionVerificationDigestSHA256
		// are always nil here: O1 has no producer for them (see types.go's
		// package doc comment — deferred to O5, the admission bridge).
		AdmissionDecisionDigestSHA256:     nil,
		AdmissionVerificationDigestSHA256: nil,
		RetryCount:                        state.ConsumedRetryBudget(),
		ReplanCount:                       state.ConsumedReplanBudget(),
		Summary:                           summary,
		CompletedAt:                       completedAt,
	}
	receipt = NormalizeReceipt(receipt)
	digest, err := ReceiptDigest(receipt)
	if err != nil {
		return state, nil, err
	}
	receipt.ReceiptDigestSHA256 = digest

	if err := validateDocument(ValidateReceiptSchema, receipt); err != nil {
		return state, nil, fmt.Errorf("built receipt failed schema validation: %w", err)
	}

	next := state
	if reason == ReasonCandidateReadyForAdmission {
		next.Phase = PhaseSucceeded
	} else {
		next.Phase = PhaseFailed
	}
	next.Receipt = &receipt

	return next, []Event{SessionTerminatedEvent{TerminalReason: reason, ReceiptDigestSHA256: digest}}, nil
}

// validateDocument marshals doc to JSON and runs it through validate — the
// same real Draft 2020-12 validators Step 1 built, reused here rather than
// duplicated, so malformed or unknown enum/schema fields on any
// command-carried artifact are rejected the same way regardless of whether
// the caller reaches this package through Transition or validates
// documents directly.
func validateDocument(validate func([]byte) error, doc any) error {
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return validate(data)
}
