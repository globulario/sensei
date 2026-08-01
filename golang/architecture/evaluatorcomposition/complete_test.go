// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"context"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/synthesis"
)

func TestCompleteEvaluationAppliesExactlyOneSecondTransitionForEveryRecommendation(t *testing.T) {
	tests := []struct {
		name               string
		recommendation     synthesis.Recommendation
		remainingRetry     int
		remainingReplan    int
		wantPhase          synthesis.Phase
		wantRetry          int
		wantReplan         int
		wantTerminalDigest bool
		wantTerminalReason synthesis.TerminalReason
	}{
		{
			name: "accept", recommendation: synthesis.RecommendAcceptCandidate,
			remainingRetry: 3, remainingReplan: 1,
			wantPhase: synthesis.PhaseSucceeded, wantRetry: 3, wantReplan: 1,
			wantTerminalDigest: true, wantTerminalReason: synthesis.ReasonCandidateReadyForAdmission,
		},
		{
			name: "retry remains", recommendation: synthesis.RecommendRetryGeneration,
			remainingRetry: 2, remainingReplan: 1,
			wantPhase: synthesis.PhaseRetry, wantRetry: 1, wantReplan: 1,
		},
		{
			name: "retry exhausted", recommendation: synthesis.RecommendRetryGeneration,
			remainingRetry: 0, remainingReplan: 1,
			wantPhase: synthesis.PhaseFailed, wantRetry: 0, wantReplan: 1,
			wantTerminalDigest: true, wantTerminalReason: synthesis.ReasonRetryBudgetExhausted,
		},
		{
			name: "replan remains", recommendation: synthesis.RecommendReplan,
			remainingRetry: 3, remainingReplan: 1,
			wantPhase: synthesis.PhaseReplan, wantRetry: 3, wantReplan: 0,
		},
		{
			name: "replan exhausted", recommendation: synthesis.RecommendReplan,
			remainingRetry: 3, remainingReplan: 0,
			wantPhase: synthesis.PhaseFailed, wantRetry: 3, wantReplan: 0,
			wantTerminalDigest: true, wantTerminalReason: synthesis.ReasonReplanBudgetExhausted,
		},
		{
			name: "architect review", recommendation: synthesis.RecommendArchitectReview,
			remainingRetry: 3, remainingReplan: 1,
			wantPhase: synthesis.PhaseFailed, wantRetry: 3, wantReplan: 1,
			wantTerminalDigest: true, wantTerminalReason: synthesis.ReasonArchitectReviewRequired,
		},
		{
			name: "abort", recommendation: synthesis.RecommendAbort,
			remainingRetry: 3, remainingReplan: 1,
			wantPhase: synthesis.PhaseFailed, wantRetry: 3, wantReplan: 1,
			wantTerminalDigest: true, wantTerminalReason: synthesis.ReasonExplicitlyAborted,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handoff, checkpoint, policy := checkpoint5ReadyFixture(t, func(policy *EvaluationPolicy) {
				policy.Evaluators = []EvaluatorSpec{{EvaluatorID: "decision", Required: true}}
				policy.RequiredCheckIDs = []string{}
				if test.recommendation == synthesis.RecommendAcceptCandidate {
					policy.FailureClassRecommendations = []FailureClassRecommendation{}
				} else {
					policy.FailureClassRecommendations = []FailureClassRecommendation{
						{FailureClass: "decision.class", Recommendation: test.recommendation},
					}
				}
			})
			checkpoint.SessionState.RemainingRetryBudget = test.remainingRetry
			checkpoint.SessionState.RemainingReplanBudget = test.remainingReplan

			status := synthesis.CheckPassed
			classes := []string{}
			if test.recommendation != synthesis.RecommendAcceptCandidate {
				status = synthesis.CheckFailed
				classes = []string{"decision.class"}
			}
			execution := checkpoint5Execution(t, checkpoint, policy, "decision", EvaluatorOutcomeCompleted,
				[]synthesis.CheckObservation{checkpoint5Check("decision-check", status)}, classes, nil, true)

			result, err := CompleteEvaluation(context.Background(), checkpoint, handoff, policy,
				[]EvaluatorExecution{execution}, nil, runFixedNow)
			if err != nil {
				t.Fatal(err)
			}
			if result.SessionState.Phase != test.wantPhase {
				t.Fatalf("phase = %q, want %q", result.SessionState.Phase, test.wantPhase)
			}
			if result.SessionState.RemainingRetryBudget != test.wantRetry || result.SessionState.RemainingReplanBudget != test.wantReplan {
				t.Fatalf("remaining budgets = retry %d replan %d, want retry %d replan %d",
					result.SessionState.RemainingRetryBudget, result.SessionState.RemainingReplanBudget, test.wantRetry, test.wantReplan)
			}
			if result.Receipt == nil || result.Receipt.Disposition != DispositionEvaluated || result.Evaluation == nil {
				t.Fatalf("evaluated completion did not return evaluation receipt: %+v", result)
			}
			if result.Evaluation.Recommendation != test.recommendation {
				t.Fatalf("recorded recommendation = %q, want %q", result.Evaluation.Recommendation, test.recommendation)
			}
			if (result.Receipt.O1TerminalReceiptDigestSHA256 != nil) != test.wantTerminalDigest {
				t.Fatalf("O1 terminal receipt digest presence = %v, want %v", result.Receipt.O1TerminalReceiptDigestSHA256 != nil, test.wantTerminalDigest)
			}
			if test.wantTerminalDigest {
				if result.SessionState.Receipt == nil || result.SessionState.Receipt.TerminalReason != test.wantTerminalReason {
					t.Fatalf("terminal receipt = %+v, want reason %q", result.SessionState.Receipt, test.wantTerminalReason)
				}
				if *result.Receipt.O1TerminalReceiptDigestSHA256 != result.SessionState.Receipt.ReceiptDigestSHA256 {
					t.Fatal("O4 receipt does not bind the exact O1 terminal receipt")
				}
			} else if result.SessionState.Receipt != nil {
				t.Fatal("nonterminal retry/replan result unexpectedly carries an O1 receipt")
			}
			if len(result.Events) <= len(checkpoint.Events) {
				t.Fatal("second transition did not append O1 events")
			}
		})
	}
}

func TestCompleteEvaluationTerminatesRequiredEvaluatorAndCompositionFailures(t *testing.T) {
	t.Run("required evaluator unavailable", func(t *testing.T) {
		handoff, checkpoint, policy := checkpoint5ReadyFixture(t, func(policy *EvaluationPolicy) {
			policy.Evaluators = []EvaluatorSpec{{EvaluatorID: "required.missing", Required: true}}
			policy.RequiredCheckIDs = []string{}
		})
		result, err := CompleteEvaluation(context.Background(), checkpoint, handoff, policy, nil, nil, runFixedNow)
		if err != nil {
			t.Fatal(err)
		}
		assertEvaluatorUnavailableCompletion(t, result, DispositionRequiredEvaluatorUnavailable)
	})

	t.Run("composition failure", func(t *testing.T) {
		handoff, checkpoint, policy := checkpoint5ReadyFixture(t, func(policy *EvaluationPolicy) {
			policy.Evaluators = []EvaluatorSpec{{EvaluatorID: "unmapped", Required: true}}
			policy.RequiredCheckIDs = []string{}
			policy.FailureClassRecommendations = []FailureClassRecommendation{}
		})
		execution := checkpoint5Execution(t, checkpoint, policy, "unmapped", EvaluatorOutcomeCompleted,
			[]synthesis.CheckObservation{checkpoint5Check("failed", synthesis.CheckFailed)},
			[]string{"unmapped.failure"}, nil, true)
		result, err := CompleteEvaluation(context.Background(), checkpoint, handoff, policy,
			[]EvaluatorExecution{execution}, nil, runFixedNow)
		if err != nil {
			t.Fatal(err)
		}
		assertEvaluatorUnavailableCompletion(t, result, DispositionCompositionFailure)
		if len(result.Receipt.EvaluatorResultBindings) != 1 || result.Receipt.EvaluatorResultBindings[0].EvaluatorID != "unmapped" {
			t.Fatalf("composition-failure receipt lost evaluator evidence: %+v", result.Receipt.EvaluatorResultBindings)
		}
	})
}

func TestTerminateEvaluationUnavailableRecordsMaterializationFailure(t *testing.T) {
	handoff, checkpoint, policy := checkpoint5ReadyFixture(t, func(policy *EvaluationPolicy) {
		policy.Evaluators = []EvaluatorSpec{{EvaluatorID: "mechanical", Required: true}}
		policy.RequiredCheckIDs = []string{}
	})
	result, err := TerminateEvaluationUnavailable(checkpoint, handoff, policy,
		DispositionMaterializationFailure, "surface construction failed", nil, runFixedNow)
	if err != nil {
		t.Fatal(err)
	}
	assertEvaluatorUnavailableCompletion(t, result, DispositionMaterializationFailure)
	if result.Receipt.CleanupSucceeded == nil || !*result.Receipt.CleanupSucceeded {
		t.Fatal("materialization failure with no constructed surfaces did not record vacuous cleanup success")
	}
}

func TestCompleteEvaluationRejectsReplayAndMismatchedCheckpointBeforeTransition(t *testing.T) {
	handoff, checkpoint, policy := checkpoint5ReadyFixture(t, nil)
	checkpoint.Receipt = &EvaluationReceipt{}
	if _, err := CompleteEvaluation(context.Background(), checkpoint, handoff, policy, nil, nil, runFixedNow); err == nil || !strings.Contains(err.Error(), "already terminal") {
		t.Fatalf("replay refusal = %v", err)
	}

	_, cleanCheckpoint, cleanPolicy := checkpoint5ReadyFixture(t, nil)
	wrongHandoff, _, _ := verifiedHandoffFixtureForDomain(t, synthesis.ProviderStatusCompleted, "github.com/example/wrong-completion-handoff")
	if _, err := CompleteEvaluation(context.Background(), cleanCheckpoint, wrongHandoff, cleanPolicy, nil, nil, runFixedNow); err == nil || !strings.Contains(err.Error(), "candidate digest") {
		t.Fatalf("mismatched handoff refusal = %v", err)
	}
}

func assertEvaluatorUnavailableCompletion(t *testing.T, result Result, disposition Disposition) {
	t.Helper()
	if result.SessionState.Phase != synthesis.PhaseFailed || result.SessionState.Receipt == nil {
		t.Fatalf("unavailability did not terminate O1: %+v", result.SessionState)
	}
	if result.SessionState.Receipt.TerminalReason != synthesis.ReasonEvaluatorUnavailable {
		t.Fatalf("terminal reason = %q, want evaluator-unavailable", result.SessionState.Receipt.TerminalReason)
	}
	if result.Receipt == nil || result.Receipt.Disposition != disposition {
		t.Fatalf("O4 disposition = %+v, want %q", result.Receipt, disposition)
	}
	if result.Receipt.O1TerminalReceiptDigestSHA256 == nil || *result.Receipt.O1TerminalReceiptDigestSHA256 != result.SessionState.Receipt.ReceiptDigestSHA256 {
		t.Fatal("O4 unavailability receipt does not bind exact O1 terminal receipt")
	}
	if result.Evaluation != nil || result.Receipt.EvaluationDigestSHA256 != nil {
		t.Fatal("unavailability disposition fabricated an Evaluation")
	}
	if !strings.Contains(result.SessionState.Receipt.Summary, string(disposition)) {
		t.Fatalf("O1 terminal summary %q does not preserve O4 disposition %q", result.SessionState.Receipt.Summary, disposition)
	}
}
