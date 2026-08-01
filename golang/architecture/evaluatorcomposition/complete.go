// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// CompleteEvaluation performs checkpoint 5's one and only second O1
// transition. checkpoint must be the successful checkpoint-3 Result in
// PhaseEvaluating, and handoff must be the exact process-local O3 carrier used
// to create it. Composition failures are converted into an evidenced terminal
// disposition rather than returned as a bare error that could strand O1.
func CompleteEvaluation(
	ctx context.Context,
	checkpoint Result,
	handoff runnercomposition.VerifiedGenerationHandoff,
	policy EvaluationPolicy,
	executions []EvaluatorExecution,
	resolver EvidenceResolver,
	now func() time.Time,
) (Result, error) {
	if err := validateCompletionCheckpoint(checkpoint, handoff, policy); err != nil {
		return Result{}, err
	}
	composition := ComposeEvaluation(ctx, checkpoint.SessionState, *checkpoint.Candidate, policy, executions, resolver)
	switch composition.Disposition {
	case DispositionEvaluated:
		if composition.Evaluation == nil {
			return TerminateEvaluationUnavailable(checkpoint, handoff, policy, DispositionCompositionFailure,
				"composer returned evaluated disposition with no Evaluation", executions, now)
		}
		return completeEvaluated(checkpoint, handoff, policy, composition, now)
	case DispositionRequiredEvaluatorUnavailable, DispositionCompositionFailure:
		return TerminateEvaluationUnavailable(checkpoint, handoff, policy, composition.Disposition,
			composition.FailureDetail, executions, now)
	default:
		return TerminateEvaluationUnavailable(checkpoint, handoff, policy, DispositionCompositionFailure,
			fmt.Sprintf("composer returned illegal checkpoint-5 disposition %q", composition.Disposition), executions, now)
	}
}

// TerminateEvaluationUnavailable gives checkpoint-4 orchestration a governed
// way to report materialization failure and is also the shared checkpoint-5
// path for required-evaluator-unavailable and composition-failure. It performs
// exactly one EvaluatorUnavailableCommand transition and produces the precise
// O4 receipt disposition.
func TerminateEvaluationUnavailable(
	checkpoint Result,
	handoff runnercomposition.VerifiedGenerationHandoff,
	policy EvaluationPolicy,
	disposition Disposition,
	failureDetail string,
	executions []EvaluatorExecution,
	now func() time.Time,
) (Result, error) {
	if disposition != DispositionMaterializationFailure && disposition != DispositionRequiredEvaluatorUnavailable && disposition != DispositionCompositionFailure {
		return Result{}, fmt.Errorf("TerminateEvaluationUnavailable: disposition %q is not a post-load unavailability disposition", disposition)
	}
	if err := validateCompletionCheckpoint(checkpoint, handoff, policy); err != nil {
		return Result{}, err
	}
	failureDetail = strings.TrimSpace(failureDetail)
	if failureDetail == "" {
		return Result{}, fmt.Errorf("TerminateEvaluationUnavailable: failure detail must not be empty")
	}

	receipt, err := newTerminalReceipt(handoff, policy, disposition, failureDetail)
	if err != nil {
		return Result{}, fmt.Errorf("TerminateEvaluationUnavailable: build receipt: %w", err)
	}
	receipt.EvaluatorResultBindings = bindingsFromExecutions(executions)
	cleanupSucceeded, cleanupDetail := cleanupSummary(executions)
	receipt.CleanupSucceeded = &cleanupSucceeded
	receipt.CleanupFailureDetail = cleanupDetail

	at := now().UTC().Format(time.RFC3339)
	finalState, finalEvents, err := synthesis.Transition(checkpoint.SessionState, synthesis.EvaluatorUnavailableCommand{
		Detail: evaluatorUnavailableDetail(receipt),
		At:     at,
	})
	if err != nil {
		return Result{}, fmt.Errorf("TerminateEvaluationUnavailable: second transition rejected: %w", err)
	}
	if finalState.Receipt == nil {
		return Result{}, fmt.Errorf("TerminateEvaluationUnavailable: evaluator-unavailable transition produced no O1 receipt")
	}
	o1Digest := finalState.Receipt.ReceiptDigestSHA256
	receipt.O1TerminalReceiptDigestSHA256 = &o1Digest
	receipt, err = finalizeReceipt(receipt, now)
	if err != nil {
		return Result{}, fmt.Errorf("TerminateEvaluationUnavailable: finalize receipt: %w", err)
	}
	return Result{
		SessionState: finalState,
		Events:       append(append([]synthesis.Event(nil), checkpoint.Events...), finalEvents...),
		Receipt:      &receipt,
		Candidate:    checkpoint.Candidate,
	}, nil
}

func completeEvaluated(checkpoint Result, handoff runnercomposition.VerifiedGenerationHandoff, policy EvaluationPolicy, composition Composition, now func() time.Time) (Result, error) {
	evaluation := *composition.Evaluation
	receipt, err := newTerminalReceipt(handoff, policy, DispositionEvaluated, "")
	if err != nil {
		return Result{}, fmt.Errorf("completeEvaluated: build receipt: %w", err)
	}
	receipt.EvaluatorResultBindings = append([]EvaluatorResultBinding(nil), composition.EvaluatorBindings...)
	evaluationDigest := evaluation.EvaluationDigestSHA256
	receipt.EvaluationDigestSHA256 = &evaluationDigest
	cleanupSucceeded := composition.CleanupSucceeded
	receipt.CleanupSucceeded = &cleanupSucceeded
	receipt.CleanupFailureDetail = composition.CleanupFailureDetail

	completedAt := now().UTC().Format(time.RFC3339)
	finalState, finalEvents, err := synthesis.Transition(checkpoint.SessionState, synthesis.RecordEvaluationCommand{
		Evaluation:  evaluation,
		CompletedAt: completedAt,
	})
	if err != nil {
		return Result{}, fmt.Errorf("completeEvaluated: second transition rejected: %w", err)
	}
	if finalState.Receipt != nil {
		o1Digest := finalState.Receipt.ReceiptDigestSHA256
		receipt.O1TerminalReceiptDigestSHA256 = &o1Digest
	}
	receipt, err = finalizeReceipt(receipt, now)
	if err != nil {
		return Result{}, fmt.Errorf("completeEvaluated: finalize receipt: %w", err)
	}
	return Result{
		SessionState: finalState,
		Events:       append(append([]synthesis.Event(nil), checkpoint.Events...), finalEvents...),
		Receipt:      &receipt,
		Candidate:    checkpoint.Candidate,
		Evaluation:   &evaluation,
	}, nil
}

func validateCompletionCheckpoint(checkpoint Result, handoff runnercomposition.VerifiedGenerationHandoff, policy EvaluationPolicy) error {
	if checkpoint.SessionState.Phase != synthesis.PhaseEvaluating {
		return fmt.Errorf("completion checkpoint phase %q is not %q", checkpoint.SessionState.Phase, synthesis.PhaseEvaluating)
	}
	if checkpoint.Candidate == nil {
		return fmt.Errorf("completion checkpoint has no verified candidate")
	}
	if checkpoint.Receipt != nil || checkpoint.Evaluation != nil {
		return fmt.Errorf("completion checkpoint is already terminal or evaluated")
	}
	if err := runnercomposition.ValidateRunnerReceipt(handoff.RunnerReceipt); err != nil {
		return fmt.Errorf("completion handoff runner receipt: %w", err)
	}
	if handoff.RunnerReceipt.Disposition != runnercomposition.DispositionVerified || handoff.RunnerReceipt.CandidateArtifactDigestSHA256 == nil {
		return fmt.Errorf("completion handoff is not the verified candidate carrier")
	}
	if *handoff.RunnerReceipt.CandidateArtifactDigestSHA256 != checkpoint.Candidate.CandidateArtifactDigestSHA256 {
		return fmt.Errorf("completion handoff candidate digest does not match checkpoint candidate")
	}
	requestDigest, resultDigest, o2ReceiptDigest, err := validateHandoffO2Documents(handoff)
	if err != nil {
		return fmt.Errorf("completion handoff O2 documents: %w", err)
	}
	if handoff.RunnerReceipt.RequestDigestSHA256 != requestDigest || handoff.RunnerReceipt.ResultDigestSHA256 == nil || *handoff.RunnerReceipt.ResultDigestSHA256 != resultDigest || handoff.RunnerReceipt.O2ReceiptDigestSHA256 == nil || *handoff.RunnerReceipt.O2ReceiptDigestSHA256 != o2ReceiptDigest {
		return fmt.Errorf("completion handoff O2 document digests do not match the accepted RunnerReceipt")
	}
	if handoff.Result.GenerationPayload == nil {
		return fmt.Errorf("completion handoff carries no generation payload")
	}
	attempt := *handoff.Result.GenerationPayload
	attemptDigest, err := synthesis.AttemptDigest(attempt)
	if err != nil {
		return fmt.Errorf("completion handoff attempt digest: %w", err)
	}
	if attemptDigest != checkpoint.SessionState.LatestAttemptDigestSHA256 {
		return fmt.Errorf("completion handoff attempt digest %q does not match checkpoint %q", attemptDigest, checkpoint.SessionState.LatestAttemptDigestSHA256)
	}
	if err := crossBindCandidate(*checkpoint.Candidate, checkpoint.SessionState, attempt); err != nil {
		return fmt.Errorf("completion checkpoint candidate binding: %w", err)
	}
	if err := ValidateEvaluationPolicy(policy); err != nil {
		return fmt.Errorf("completion policy: %w", err)
	}
	if policy.SessionDigestSHA256 != checkpoint.SessionState.Session.SessionDigestSHA256 || policy.AttemptDigestSHA256 != checkpoint.SessionState.LatestAttemptDigestSHA256 || policy.CandidateArtifactDigestSHA256 != checkpoint.Candidate.CandidateArtifactDigestSHA256 {
		return fmt.Errorf("completion policy does not bind the exact checkpoint session, attempt, and candidate")
	}
	return nil
}

func bindingsFromExecutions(executions []EvaluatorExecution) []EvaluatorResultBinding {
	bindings := make([]EvaluatorResultBinding, 0, len(executions))
	seen := make(map[string]bool)
	for _, execution := range executions {
		id := execution.Descriptor.EvaluatorID
		if id == "" || seen[id] || execution.Descriptor.DescriptorDigestSHA256 == "" || execution.Result.ResultDigestSHA256 == "" {
			continue
		}
		seen[id] = true
		bindings = append(bindings, EvaluatorResultBinding{
			EvaluatorID:            id,
			DescriptorDigestSHA256: execution.Descriptor.DescriptorDigestSHA256,
			ResultDigestSHA256:     execution.Result.ResultDigestSHA256,
		})
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].EvaluatorID < bindings[j].EvaluatorID })
	return bindings
}

func cleanupSummary(executions []EvaluatorExecution) (bool, string) {
	failures := make([]string, 0)
	for _, execution := range executions {
		if execution.Result.CleanupSucceeded != nil && !*execution.Result.CleanupSucceeded {
			failures = append(failures, execution.Descriptor.EvaluatorID)
		}
	}
	sort.Strings(failures)
	if len(failures) == 0 {
		return true, ""
	}
	return false, "cleanup failed for evaluators: " + strings.Join(failures, ", ")
}
