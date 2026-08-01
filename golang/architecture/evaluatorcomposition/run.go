// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// Result is checkpoint 3's bounded return value from Run: either a
// terminal EvaluationReceipt -- DispositionInvalidOutputTerminated or
// DispositionCandidateLoadFailure, the only two dispositions reachable
// without evaluator execution -- or, once the handoff, policy, and sealed
// candidate all validate, the exact loaded, cross-bound CandidateArtifact
// ready for evaluator execution (checkpoint 4). Receipt and Candidate are
// never both non-nil.
type Result struct {
	SessionState synthesis.SessionState
	Events       []synthesis.Event
	Receipt      *EvaluationReceipt
	Candidate    *runnercomposition.CandidateArtifact
	Evaluation   *synthesis.Evaluation
}

// Run closes O4's exact generation-handoff seam
// (docs/design/governed-evaluator-composition-o4.md, "The
// generation-handoff seam" and "Candidate identity and evaluator
// surfaces"), through checkpoint 3's bounded scope: handoff and policy
// revalidation, the first O1 Transition (RecordAttemptCommand), the
// invalid_output short-circuit, and candidate loading plus cross-binding.
// It performs no evaluator execution, materialization, composition, or
// second O1 Evaluation transition -- those arrive at checkpoints 4 and 5.
//
// Every check up through the first Transition call is a precondition: on
// failure, Run returns a non-nil Go error and sessionState is left
// unchanged (the caller already holds it), matching the generation-handoff
// seam's own law that "a handoff failure is a contract/programming
// failure ... not an evaluator observation." Only once the first
// Transition call has actually run does a failure become an evidenced
// EvaluationReceipt disposition -- candidate-load-failure -- carried by a
// governed second Transition (EvaluatorUnavailableCommand) instead of a
// bare error, so SessionState is never left parked in PhaseEvaluating with
// no O1-recorded consequence (hard law 21).
func Run(
	ctx context.Context,
	sessionState synthesis.SessionState,
	handoff runnercomposition.VerifiedGenerationHandoff,
	policy EvaluationPolicy,
	store runnercomposition.CandidateArtifactStore,
	now func() time.Time,
) (Result, error) {
	// --- Generation-handoff seam laws 1-9: preconditions, before the first Transition call ---

	if err := runnercomposition.ValidateRunnerReceipt(handoff.RunnerReceipt); err != nil {
		return Result{}, fmt.Errorf("evaluatorcomposition.Run: invalid RunnerReceipt in handoff: %w", err)
	}
	if handoff.RunnerReceipt.Disposition != runnercomposition.DispositionVerified {
		return Result{}, fmt.Errorf("evaluatorcomposition.Run: RunnerReceipt.Disposition is %q, not %q -- only a verified O3 disposition may reach MapToCommand (hard law 2)", handoff.RunnerReceipt.Disposition, runnercomposition.DispositionVerified)
	}
	if handoff.RunnerReceipt.CandidateArtifactDigestSHA256 == nil {
		return Result{}, fmt.Errorf("evaluatorcomposition.Run: internal error: a verified RunnerReceipt has no candidate_artifact_digest_sha256")
	}

	requestDigest, resultDigest, o2ReceiptDigest, err := validateHandoffO2Documents(handoff)
	if err != nil {
		return Result{}, fmt.Errorf("evaluatorcomposition.Run: %w", err)
	}
	if handoff.RunnerReceipt.RequestDigestSHA256 != requestDigest {
		return Result{}, fmt.Errorf("evaluatorcomposition.Run: runner receipt references request digest %q, does not match handoff.Request's own recomputed digest %q", handoff.RunnerReceipt.RequestDigestSHA256, requestDigest)
	}
	if handoff.RunnerReceipt.ResultDigestSHA256 == nil || *handoff.RunnerReceipt.ResultDigestSHA256 != resultDigest {
		return Result{}, fmt.Errorf("evaluatorcomposition.Run: runner receipt's result_digest_sha256 reference does not match handoff.Result's own recomputed digest %q", resultDigest)
	}
	if handoff.RunnerReceipt.O2ReceiptDigestSHA256 == nil || *handoff.RunnerReceipt.O2ReceiptDigestSHA256 != o2ReceiptDigest {
		return Result{}, fmt.Errorf("evaluatorcomposition.Run: runner receipt's o2_receipt_digest_sha256 reference does not match handoff.O2Receipt's own recomputed digest %q", o2ReceiptDigest)
	}

	if handoff.Result.GenerationPayload == nil {
		return Result{}, fmt.Errorf("evaluatorcomposition.Run: handoff.Result carries no generation_payload despite a verified RunnerReceipt")
	}
	attempt := *handoff.Result.GenerationPayload
	attemptDigest, err := synthesis.AttemptDigest(attempt)
	if err != nil {
		return Result{}, fmt.Errorf("evaluatorcomposition.Run: compute attempt digest: %w", err)
	}

	if err := ValidateEvaluationPolicy(policy); err != nil {
		return Result{}, fmt.Errorf("evaluatorcomposition.Run: invalid evaluation policy: %w", err)
	}
	if policy.SessionDigestSHA256 != sessionState.Session.SessionDigestSHA256 {
		return Result{}, fmt.Errorf("evaluatorcomposition.Run: policy references session digest %q, current session is %q", policy.SessionDigestSHA256, sessionState.Session.SessionDigestSHA256)
	}
	if policy.AttemptDigestSHA256 != attemptDigest {
		return Result{}, fmt.Errorf("evaluatorcomposition.Run: policy references attempt digest %q, the attempt about to be recorded is %q", policy.AttemptDigestSHA256, attemptDigest)
	}
	if policy.CandidateArtifactDigestSHA256 != *handoff.RunnerReceipt.CandidateArtifactDigestSHA256 {
		return Result{}, fmt.Errorf("evaluatorcomposition.Run: policy references candidate artifact digest %q, the O3 receipt references %q", policy.CandidateArtifactDigestSHA256, *handoff.RunnerReceipt.CandidateArtifactDigestSHA256)
	}

	// --- First O1 transition: MapToCommand + Transition ---

	completedAt := now().UTC().Format(time.RFC3339)
	cmd, err := providerport.MapToCommand(sessionState, handoff.Request, handoff.Result, completedAt)
	if err != nil {
		return Result{}, fmt.Errorf("evaluatorcomposition.Run: map generation result to command: %w", err)
	}
	if _, ok := cmd.(synthesis.RecordAttemptCommand); !ok {
		return Result{}, fmt.Errorf("evaluatorcomposition.Run: internal error: MapToCommand returned %T for an OperationGeneration request, want synthesis.RecordAttemptCommand", cmd)
	}

	nextState, events, err := synthesis.Transition(sessionState, cmd)
	if err != nil {
		return Result{}, fmt.Errorf("evaluatorcomposition.Run: first transition (RecordAttemptCommand) rejected: %w", err)
	}

	// --- invalid_output short-circuit: O1 already terminated on this one call ---

	if nextState.Phase != synthesis.PhaseEvaluating {
		if nextState.Receipt == nil {
			return Result{}, fmt.Errorf("evaluatorcomposition.Run: internal error: first transition left phase %q with no terminal receipt", nextState.Phase)
		}
		receipt, err := newTerminalReceipt(handoff, policy, DispositionInvalidOutputTerminated,
			"the accepted attempt's own terminal_provider_status was invalid_output; O1 terminated on the first transition before evaluation could begin")
		if err != nil {
			return Result{}, fmt.Errorf("evaluatorcomposition.Run: build invalid-output-terminated receipt: %w", err)
		}
		o1ReceiptDigest := nextState.Receipt.ReceiptDigestSHA256
		receipt.O1TerminalReceiptDigestSHA256 = &o1ReceiptDigest
		receipt, err = finalizeReceipt(receipt, now)
		if err != nil {
			return Result{}, fmt.Errorf("evaluatorcomposition.Run: finalize invalid-output-terminated receipt: %w", err)
		}
		return Result{SessionState: nextState, Events: events, Receipt: &receipt}, nil
	}

	// --- Candidate load + cross-binding ---

	artifact, loadErr := store.Get(ctx, *handoff.RunnerReceipt.CandidateArtifactDigestSHA256)
	var bindDetail string
	if loadErr != nil {
		bindDetail = "candidate load: " + loadErr.Error()
	} else if bindErr := crossBindCandidate(artifact, nextState, attempt); bindErr != nil {
		bindDetail = "candidate cross-binding: " + bindErr.Error()
	}

	if bindDetail != "" {
		receipt, err := newTerminalReceipt(handoff, policy, DispositionCandidateLoadFailure, bindDetail)
		if err != nil {
			return Result{}, fmt.Errorf("evaluatorcomposition.Run: build candidate-load-failure receipt: %w", err)
		}

		unavailableAt := now().UTC().Format(time.RFC3339)
		unavailableDetail := evaluatorUnavailableDetail(receipt)
		finalState, finalEvents, err := synthesis.Transition(nextState, synthesis.EvaluatorUnavailableCommand{Detail: unavailableDetail, At: unavailableAt})
		if err != nil {
			return Result{}, fmt.Errorf("evaluatorcomposition.Run: second transition (EvaluatorUnavailableCommand) rejected: %w", err)
		}
		if finalState.Receipt == nil {
			return Result{}, fmt.Errorf("evaluatorcomposition.Run: internal error: EvaluatorUnavailableCommand did not produce a terminal receipt")
		}
		o1ReceiptDigest := finalState.Receipt.ReceiptDigestSHA256
		receipt.O1TerminalReceiptDigestSHA256 = &o1ReceiptDigest
		receipt, err = finalizeReceipt(receipt, now)
		if err != nil {
			return Result{}, fmt.Errorf("evaluatorcomposition.Run: finalize candidate-load-failure receipt: %w", err)
		}
		return Result{SessionState: finalState, Events: append(events, finalEvents...), Receipt: &receipt}, nil
	}

	return Result{SessionState: nextState, Events: events, Candidate: &artifact}, nil
}

// validateHandoffO2Documents re-validates handoff.Request/Result/O2Receipt
// (schema plus declared-versus-recomputed digest) independently of
// whatever O3 itself validated -- law 2: "O4 recomputes and verifies every
// document digest again because mutable Go values may have changed after
// O3 validated them." Returns the freshly recomputed digests so the
// caller can cross-check RunnerReceipt's own references against them.
//
// providerport.MapToCommand independently re-validates Request/Result
// again as part of its own contract (schema, digest, request-result
// binding, parent chain, session identity). O2Receipt is not one of
// MapToCommand's parameters, so this function additionally cross-binds the
// receipt's request/result/outcome/payload references to the exact Request
// and Result in the handoff before O1 records anything.
func validateHandoffO2Documents(handoff runnercomposition.VerifiedGenerationHandoff) (requestDigest, resultDigest, o2ReceiptDigest string, err error) {
	reqData, err := json.Marshal(handoff.Request)
	if err != nil {
		return "", "", "", fmt.Errorf("marshal request: %w", err)
	}
	if err := providerport.ValidateRequestSchema(reqData); err != nil {
		return "", "", "", fmt.Errorf("request schema: %w", err)
	}
	requestDigest, err = providerport.RequestDigest(handoff.Request)
	if err != nil {
		return "", "", "", fmt.Errorf("compute request digest: %w", err)
	}
	if handoff.Request.RequestDigestSHA256 != requestDigest {
		return "", "", "", fmt.Errorf("request declares digest %q but its actual computed digest is %q", handoff.Request.RequestDigestSHA256, requestDigest)
	}

	resData, err := json.Marshal(handoff.Result)
	if err != nil {
		return "", "", "", fmt.Errorf("marshal result: %w", err)
	}
	if err := providerport.ValidateResultSchema(resData); err != nil {
		return "", "", "", fmt.Errorf("result schema: %w", err)
	}
	resultDigest, err = providerport.ResultDigest(handoff.Result)
	if err != nil {
		return "", "", "", fmt.Errorf("compute result digest: %w", err)
	}
	if handoff.Result.ResultDigestSHA256 != resultDigest {
		return "", "", "", fmt.Errorf("result declares digest %q but its actual computed digest is %q", handoff.Result.ResultDigestSHA256, resultDigest)
	}

	o2ReceiptData, err := json.Marshal(handoff.O2Receipt)
	if err != nil {
		return "", "", "", fmt.Errorf("marshal o2 receipt: %w", err)
	}
	if err := providerport.ValidateReceiptSchema(o2ReceiptData); err != nil {
		return "", "", "", fmt.Errorf("o2 receipt schema: %w", err)
	}
	o2ReceiptDigest, err = providerport.ReceiptDigest(handoff.O2Receipt)
	if err != nil {
		return "", "", "", fmt.Errorf("compute o2 receipt digest: %w", err)
	}
	if handoff.O2Receipt.ReceiptDigestSHA256 != o2ReceiptDigest {
		return "", "", "", fmt.Errorf("o2 receipt declares digest %q but its actual computed digest is %q", handoff.O2Receipt.ReceiptDigestSHA256, o2ReceiptDigest)
	}
	if handoff.O2Receipt.RequestDigestSHA256 != requestDigest {
		return "", "", "", fmt.Errorf("o2 receipt references request digest %q, does not match handoff.Request's recomputed digest %q", handoff.O2Receipt.RequestDigestSHA256, requestDigest)
	}
	if handoff.O2Receipt.ResultDigestSHA256 != resultDigest {
		return "", "", "", fmt.Errorf("o2 receipt references result digest %q, does not match handoff.Result's recomputed digest %q", handoff.O2Receipt.ResultDigestSHA256, resultDigest)
	}
	if handoff.O2Receipt.TerminalOutcome != handoff.Result.TerminalOutcome {
		return "", "", "", fmt.Errorf("o2 receipt terminal_outcome %q does not match handoff.Result terminal_outcome %q", handoff.O2Receipt.TerminalOutcome, handoff.Result.TerminalOutcome)
	}
	if (handoff.O2Receipt.PayloadDigestSHA256 == nil) != (handoff.Result.PayloadDigestSHA256 == nil) {
		return "", "", "", fmt.Errorf("o2 receipt payload_digest_sha256 presence does not match handoff.Result")
	}
	if handoff.O2Receipt.PayloadDigestSHA256 != nil && *handoff.O2Receipt.PayloadDigestSHA256 != *handoff.Result.PayloadDigestSHA256 {
		return "", "", "", fmt.Errorf("o2 receipt payload_digest_sha256 %q does not match handoff.Result payload_digest_sha256 %q", *handoff.O2Receipt.PayloadDigestSHA256, *handoff.Result.PayloadDigestSHA256)
	}

	return requestDigest, resultDigest, o2ReceiptDigest, nil
}

// evaluatorUnavailableDetail binds the O1 terminal consequence back to the
// exact O4 evidence document under construction. ReceiptID is stable before
// the second Transition; the receipt digest cannot be included because it
// must itself bind the O1 terminal receipt digest produced by that call.
func evaluatorUnavailableDetail(receipt EvaluationReceipt) string {
	return fmt.Sprintf("o4_receipt_id=%q disposition=%q failure_detail=%q", receipt.ReceiptID, receipt.Disposition, receipt.FailureDetail)
}

// crossBindCandidate verifies every cross-binding the design doc's
// "Candidate identity and evaluator surfaces" section requires, before any
// evaluator runs. attempt is the exact accepted Attempt
// (handoff.Result.GenerationPayload). state is the SessionState AFTER the
// first Transition call -- its PlanDigestSHA256/PlanGeneration/AttemptNumber
// are already guaranteed to match attempt's own (transitionRecordAttempt
// verifies this before accepting), so checking against state alone is
// sufficient; a separate attempt-based check would be redundant, not
// additionally safe. artifact.CandidateArtifactDigestSHA256 is not
// separately checked here: store.Get is content-addressed and already
// guarantees the returned artifact's own digest equals the key it was
// fetched by.
func crossBindCandidate(artifact runnercomposition.CandidateArtifact, state synthesis.SessionState, attempt synthesis.Attempt) error {
	if artifact.RepositoryDomain != state.Session.RepositoryDomain {
		return fmt.Errorf("candidate repository_domain %q does not match session %q", artifact.RepositoryDomain, state.Session.RepositoryDomain)
	}
	if artifact.BaseRevision != state.Session.BaseRevision {
		return fmt.Errorf("candidate base_revision %q does not match session %q", artifact.BaseRevision, state.Session.BaseRevision)
	}
	if artifact.WorkspaceIdentityDigestSHA256 != state.Session.WorkspaceIdentityDigestSHA256 {
		return fmt.Errorf("candidate workspace_identity_digest_sha256 %q does not match session %q", artifact.WorkspaceIdentityDigestSHA256, state.Session.WorkspaceIdentityDigestSHA256)
	}
	if artifact.SessionDigestSHA256 != state.Session.SessionDigestSHA256 {
		return fmt.Errorf("candidate session_digest_sha256 %q does not match session %q", artifact.SessionDigestSHA256, state.Session.SessionDigestSHA256)
	}
	if artifact.PlanDigestSHA256 != state.LatestPlanDigestSHA256 {
		return fmt.Errorf("candidate plan_digest_sha256 %q does not match session's latest accepted plan %q", artifact.PlanDigestSHA256, state.LatestPlanDigestSHA256)
	}
	if artifact.PlanGeneration != state.PlanGeneration {
		return fmt.Errorf("candidate plan_generation %d does not match session's recorded generation %d", artifact.PlanGeneration, state.PlanGeneration)
	}
	if artifact.AttemptNumber != state.AttemptNumber {
		return fmt.Errorf("candidate attempt_number %d does not match session's recorded attempt %d", artifact.AttemptNumber, state.AttemptNumber)
	}
	if artifact.InputCandidateDigestSHA256 != attempt.InputCandidateDigestSHA256 {
		return fmt.Errorf("candidate input_candidate_digest_sha256 %q does not match the accepted attempt's %q", artifact.InputCandidateDigestSHA256, attempt.InputCandidateDigestSHA256)
	}
	if artifact.ProposedChangeDigestSHA256 != attempt.ProposedChangeDigestSHA256 {
		return fmt.Errorf("candidate proposed_change_digest_sha256 %q does not match the accepted attempt's %q", artifact.ProposedChangeDigestSHA256, attempt.ProposedChangeDigestSHA256)
	}
	return nil
}

// newTerminalReceipt builds an EvaluationReceipt scaffold for disposition,
// with every field's presence set per FieldPresenceFor(disposition) except
// O1TerminalReceiptDigestSHA256, ReceiptDigestSHA256, and CompletedAt --
// the caller sets O1TerminalReceiptDigestSHA256 once the relevant O1
// Transition call is known, then finishes via finalizeReceipt.
func newTerminalReceipt(handoff runnercomposition.VerifiedGenerationHandoff, policy EvaluationPolicy, disposition Disposition, failureDetail string) (EvaluationReceipt, error) {
	presence, err := FieldPresenceFor(disposition)
	if err != nil {
		return EvaluationReceipt{}, err
	}
	r := EvaluationReceipt{
		SchemaVersion:                 EvaluationReceiptSchemaVersion,
		ReceiptID:                     "evaluationreceipt." + handoff.RunnerReceipt.ReceiptID,
		SessionDigestSHA256:           policy.SessionDigestSHA256,
		AttemptDigestSHA256:           policy.AttemptDigestSHA256,
		RunnerReceiptDigestSHA256:     handoff.RunnerReceipt.RunnerReceiptDigestSHA256,
		RequestDigestSHA256:           handoff.RunnerReceipt.RequestDigestSHA256,
		ResultDigestSHA256:            *handoff.RunnerReceipt.ResultDigestSHA256,
		O2ReceiptDigestSHA256:         *handoff.RunnerReceipt.O2ReceiptDigestSHA256,
		PolicyDigestSHA256:            policy.PolicyDigestSHA256,
		CandidateArtifactDigestSHA256: *handoff.RunnerReceipt.CandidateArtifactDigestSHA256,
		CandidateArtifactVerified:     presence.CandidateArtifactVerified,
		Disposition:                   disposition,
		FailureDetail:                 failureDetail,
	}
	if presence.EvaluatorResultBindingsMustBeEmpty {
		r.EvaluatorResultBindings = []EvaluatorResultBinding{}
	}
	return r, nil
}

// finalizeReceipt sets CompletedAt and ReceiptDigestSHA256 and validates
// the fully-populated receipt -- the O4 mirror of O3's own finalize in
// golang/architecture/runnercomposition/run.go.
func finalizeReceipt(r EvaluationReceipt, now func() time.Time) (EvaluationReceipt, error) {
	r.CompletedAt = now().UTC().Format(time.RFC3339)
	digest, err := EvaluationReceiptDigest(r)
	if err != nil {
		return EvaluationReceipt{}, fmt.Errorf("compute evaluation receipt digest: %w", err)
	}
	r.ReceiptDigestSHA256 = digest
	if err := ValidateEvaluationReceipt(r); err != nil {
		return EvaluationReceipt{}, fmt.Errorf("internal error: constructed an invalid EvaluationReceipt: %w", err)
	}
	return r, nil
}
