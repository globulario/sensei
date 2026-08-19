// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

func checkpoint5ReadyFixture(t *testing.T, configure func(*EvaluationPolicy)) (runnercomposition.VerifiedGenerationHandoff, Result, EvaluationPolicy) {
	t.Helper()
	handoff, sessionState, store := verifiedHandoffFixture(t, synthesis.ProviderStatusCompleted)
	policy := fixturePolicyForHandoff(t, sessionState, handoff)
	if configure != nil {
		configure(&policy)
	}
	policy = finishCheckpoint5Policy(t, policy)
	checkpoint, err := Run(context.Background(), sessionState, handoff, policy, store, runFixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.SessionState.Phase != synthesis.PhaseEvaluating || checkpoint.Candidate == nil || checkpoint.Receipt != nil {
		// Name the terminal receipt's own disposition and detail. %+v
		// prints Receipt as a bare pointer, so a failure here otherwise
		// reports the phase it ended in and nothing about why -- which is
		// exactly what it cost to diagnose the intermittent
		// candidate-load-failure in issue #214.
		t.Fatalf("fixture did not produce a successful checkpoint-3 result: phase=%q candidate=%v receipt=%s state=%+v",
			checkpoint.SessionState.Phase, checkpoint.Candidate != nil, describeCheckpointReceipt(checkpoint.Receipt), checkpoint.SessionState)
	}
	return handoff, checkpoint, policy
}

// describeCheckpointReceipt renders the fields that say why a terminal
// receipt exists at all, for fixture failures that would otherwise print a
// bare pointer.
func describeCheckpointReceipt(receipt *EvaluationReceipt) string {
	if receipt == nil {
		return "<nil>"
	}
	return fmt.Sprintf("{disposition=%q failure_detail=%q}", receipt.Disposition, receipt.FailureDetail)
}

func finishCheckpoint5Policy(t *testing.T, policy EvaluationPolicy) EvaluationPolicy {
	t.Helper()
	policy = NormalizeEvaluationPolicy(policy)
	digest, err := EvaluationPolicyDigest(policy)
	if err != nil {
		t.Fatal(err)
	}
	policy.PolicyDigestSHA256 = digest
	if err := ValidateEvaluationPolicy(policy); err != nil {
		t.Fatalf("checkpoint-5 test policy invalid: %v", err)
	}
	return policy
}

func checkpoint5Execution(
	t *testing.T,
	checkpoint Result,
	policy EvaluationPolicy,
	evaluatorID string,
	outcome EvaluatorTerminalOutcome,
	checks []synthesis.CheckObservation,
	classes []string,
	limitations []synthesis.Limitation,
	cleanupSucceeded bool,
) EvaluatorExecution {
	t.Helper()
	surface := &recordingEvaluatorSurface{
		ref:  fmt.Sprintf("surface://checkpoint5/%s/plain", evaluatorID),
		root: t.TempDir(),
		mode: SurfaceModePlain,
	}
	input, err := BuildEvaluationInput(checkpoint.SessionState, *checkpoint.Candidate, policy, surface)
	if err != nil {
		t.Fatal(err)
	}

	descriptor := evaluatorDescriptorForExecution(t, evaluatorID)
	supported := make([]string, 0, len(checks))
	for _, check := range checks {
		supported = append(supported, check.CheckID)
	}
	sort.Strings(supported)
	descriptor.SupportedCheckIDs = supported
	descriptor.Limitations = []synthesis.Limitation{}
	descriptor = NormalizeEvaluatorDescriptor(descriptor)
	descriptorDigest, err := EvaluatorDescriptorDigest(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	descriptor.DescriptorDigestSHA256 = descriptorDigest
	if err := ValidateEvaluatorDescriptor(descriptor); err != nil {
		t.Fatalf("checkpoint-5 descriptor invalid: %v", err)
	}

	for i := range checks {
		checks[i].EvidenceReferences = canonicalStrings(checks[i].EvidenceReferences)
	}
	result := EvaluatorResult{
		SchemaVersion:                   EvaluatorResultSchemaVersion,
		EvaluatorID:                     evaluatorID,
		EvaluatorDescriptorDigestSHA256: descriptor.DescriptorDigestSHA256,
		EvaluationInputDigestSHA256:     input.EvaluationInputDigestSHA256,
		TerminalOutcome:                 outcome,
		Checks:                          checks,
		EvidenceReferences:              []EvidenceReference{},
		ClassifiedFailureReasons:        classes,
		Limitations:                     limitations,
		CleanupSucceeded:                &cleanupSucceeded,
	}
	result = NormalizeEvaluatorResult(result)
	resultDigest, err := EvaluatorResultDigest(result)
	if err != nil {
		t.Fatal(err)
	}
	result.ResultDigestSHA256 = resultDigest
	if err := ValidateEvaluatorResult(result); err != nil {
		t.Fatalf("checkpoint-5 result invalid: %v", err)
	}
	return EvaluatorExecution{Descriptor: descriptor, Input: input, Result: result}
}

func checkpoint5Check(id string, status synthesis.CheckObservationStatus) synthesis.CheckObservation {
	return synthesis.CheckObservation{
		CheckID:            id,
		Status:             status,
		Detail:             "checkpoint-5 test observation " + id,
		EvidenceReferences: []string{},
	}
}

func TestComposeEvaluationIsOrderIndependentAndAcceptsOnlyCompletePassingEvidence(t *testing.T) {
	_, checkpoint, policy := checkpoint5ReadyFixture(t, func(policy *EvaluationPolicy) {
		policy.Evaluators = []EvaluatorSpec{
			{EvaluatorID: "z.second", Required: true},
			{EvaluatorID: "a.first", Required: true},
		}
		policy.RequiredCheckIDs = []string{"check.first", "check.second"}
		policy.FailureClassRecommendations = []FailureClassRecommendation{
			{FailureClass: string(FailureClassMechanicalCheckFailure), Recommendation: synthesis.RecommendRetryGeneration},
		}
	})
	first := checkpoint5Execution(t, checkpoint, policy, "a.first", EvaluatorOutcomeCompleted,
		[]synthesis.CheckObservation{checkpoint5Check("check.first", synthesis.CheckPassed)}, nil, nil, true)
	second := checkpoint5Execution(t, checkpoint, policy, "z.second", EvaluatorOutcomeCompleted,
		[]synthesis.CheckObservation{checkpoint5Check("check.second", synthesis.CheckPassed)}, nil, nil, true)

	forward := ComposeEvaluation(context.Background(), checkpoint.SessionState, *checkpoint.Candidate, policy,
		[]EvaluatorExecution{first, second}, nil)
	reverse := ComposeEvaluation(context.Background(), checkpoint.SessionState, *checkpoint.Candidate, policy,
		[]EvaluatorExecution{second, first}, nil)
	if forward.Disposition != DispositionEvaluated || reverse.Disposition != DispositionEvaluated {
		t.Fatalf("composition dispositions = %q/%q, details %q/%q", forward.Disposition, reverse.Disposition, forward.FailureDetail, reverse.FailureDetail)
	}
	if forward.Evaluation == nil || reverse.Evaluation == nil {
		t.Fatal("passing composition produced no Evaluation")
	}
	if forward.Evaluation.Recommendation != synthesis.RecommendAcceptCandidate {
		t.Fatalf("recommendation = %q, want accept-candidate", forward.Evaluation.Recommendation)
	}
	if forward.Evaluation.EvaluationID != reverse.Evaluation.EvaluationID || forward.Evaluation.EvaluationDigestSHA256 != reverse.Evaluation.EvaluationDigestSHA256 {
		t.Fatalf("execution order changed evaluation identity: %+v vs %+v", forward.Evaluation, reverse.Evaluation)
	}
	if !reflect.DeepEqual(forward.EvaluatorBindings, reverse.EvaluatorBindings) {
		t.Fatalf("execution order changed canonical bindings: %+v vs %+v", forward.EvaluatorBindings, reverse.EvaluatorBindings)
	}
	if got := []string{forward.EvaluatorBindings[0].EvaluatorID, forward.EvaluatorBindings[1].EvaluatorID}; !reflect.DeepEqual(got, []string{"a.first", "z.second"}) {
		t.Fatalf("binding order = %v", got)
	}
}

func TestComposeEvaluationAppliesFixedNonAcceptPrecedence(t *testing.T) {
	_, checkpoint, policy := checkpoint5ReadyFixture(t, func(policy *EvaluationPolicy) {
		policy.Evaluators = []EvaluatorSpec{{EvaluatorID: "evaluator.precedence", Required: true}}
		policy.RequiredCheckIDs = []string{}
		policy.FailureClassRecommendations = []FailureClassRecommendation{
			{FailureClass: string(FailureClassMechanicalCheckFailure), Recommendation: synthesis.RecommendRetryGeneration},
		}
	})
	execution := checkpoint5Execution(t, checkpoint, policy, "evaluator.precedence", EvaluatorOutcomeCompleted,
		[]synthesis.CheckObservation{checkpoint5Check("check.failed", synthesis.CheckFailed)},
		[]string{
			string(FailureClassMechanicalCheckFailure),
			string(FailureClassIncidentScarConcerning),
			string(FailureClassAuditForbiddenFix),
		}, nil, true)
	composition := ComposeEvaluation(context.Background(), checkpoint.SessionState, *checkpoint.Candidate, policy,
		[]EvaluatorExecution{execution}, nil)
	if composition.Disposition != DispositionEvaluated || composition.Evaluation == nil {
		t.Fatalf("composition failed: %+v", composition)
	}
	if composition.Evaluation.Recommendation != synthesis.RecommendAbort {
		t.Fatalf("recommendation = %q, want abort", composition.Evaluation.Recommendation)
	}
}

func TestComposeEvaluationDistinguishesRequiredAndOptionalEvaluatorUnavailability(t *testing.T) {
	t.Run("required missing", func(t *testing.T) {
		_, checkpoint, policy := checkpoint5ReadyFixture(t, func(policy *EvaluationPolicy) {
			policy.Evaluators = []EvaluatorSpec{
				{EvaluatorID: "required.present", Required: true},
				{EvaluatorID: "required.missing", Required: true},
			}
			policy.RequiredCheckIDs = []string{}
		})
		present := checkpoint5Execution(t, checkpoint, policy, "required.present", EvaluatorOutcomeCompleted,
			[]synthesis.CheckObservation{checkpoint5Check("present", synthesis.CheckPassed)}, nil, nil, true)
		composition := ComposeEvaluation(context.Background(), checkpoint.SessionState, *checkpoint.Candidate, policy,
			[]EvaluatorExecution{present}, nil)
		if composition.Disposition != DispositionRequiredEvaluatorUnavailable || composition.Evaluation != nil {
			t.Fatalf("required missing composition = %+v", composition)
		}
		if !strings.Contains(composition.FailureDetail, "required.missing") {
			t.Fatalf("required missing detail = %q", composition.FailureDetail)
		}
	})

	t.Run("optional missing is policy-classified evaluation", func(t *testing.T) {
		_, checkpoint, policy := checkpoint5ReadyFixture(t, func(policy *EvaluationPolicy) {
			policy.Evaluators = []EvaluatorSpec{
				{EvaluatorID: "required.present", Required: true},
				{EvaluatorID: "optional.missing", Required: false},
			}
			policy.RequiredCheckIDs = []string{"present"}
			policy.FailureClassRecommendations = []FailureClassRecommendation{
				{FailureClass: FailureClassOptionalEvaluatorUnavailable, Recommendation: synthesis.RecommendArchitectReview},
			}
		})
		present := checkpoint5Execution(t, checkpoint, policy, "required.present", EvaluatorOutcomeCompleted,
			[]synthesis.CheckObservation{checkpoint5Check("present", synthesis.CheckPassed)}, nil, nil, true)
		composition := ComposeEvaluation(context.Background(), checkpoint.SessionState, *checkpoint.Candidate, policy,
			[]EvaluatorExecution{present}, nil)
		if composition.Disposition != DispositionEvaluated || composition.Evaluation == nil {
			t.Fatalf("optional missing composition = %+v", composition)
		}
		if composition.Evaluation.Recommendation != synthesis.RecommendArchitectReview {
			t.Fatalf("optional missing recommendation = %q", composition.Evaluation.Recommendation)
		}
		if !containsString(composition.Evaluation.ClassifiedFailureReasons, FailureClassOptionalEvaluatorUnavailable) {
			t.Fatalf("optional missing failure classes = %v", composition.Evaluation.ClassifiedFailureReasons)
		}
		if len(composition.Evaluation.Limitations) == 0 {
			t.Fatal("optional missing evidence did not preserve a limitation")
		}
		if hasBlockingLimitation(composition.Evaluation.Limitations) {
			t.Fatal("optional missing evidence was incorrectly promoted to a blocking limitation")
		}
	})
}

func TestComposeEvaluationEnforcesRequiredChecksAndPolicyCoverage(t *testing.T) {
	t.Run("required check maps to retry", func(t *testing.T) {
		_, checkpoint, policy := checkpoint5ReadyFixture(t, func(policy *EvaluationPolicy) {
			policy.Evaluators = []EvaluatorSpec{{EvaluatorID: "checks", Required: true}}
			policy.RequiredCheckIDs = []string{"must-pass"}
			policy.FailureClassRecommendations = []FailureClassRecommendation{
				{FailureClass: FailureClassRequiredCheckUnsatisfied, Recommendation: synthesis.RecommendRetryGeneration},
			}
		})
		execution := checkpoint5Execution(t, checkpoint, policy, "checks", EvaluatorOutcomeCompleted,
			[]synthesis.CheckObservation{checkpoint5Check("other", synthesis.CheckPassed)}, nil, nil, true)
		composition := ComposeEvaluation(context.Background(), checkpoint.SessionState, *checkpoint.Candidate, policy,
			[]EvaluatorExecution{execution}, nil)
		if composition.Disposition != DispositionEvaluated || composition.Evaluation == nil || composition.Evaluation.Recommendation != synthesis.RecommendRetryGeneration {
			t.Fatalf("required check composition = %+v", composition)
		}
	})

	t.Run("unmapped evidence fails closed", func(t *testing.T) {
		_, checkpoint, policy := checkpoint5ReadyFixture(t, func(policy *EvaluationPolicy) {
			policy.Evaluators = []EvaluatorSpec{{EvaluatorID: "unmapped", Required: true}}
			policy.RequiredCheckIDs = []string{}
			policy.FailureClassRecommendations = []FailureClassRecommendation{}
		})
		execution := checkpoint5Execution(t, checkpoint, policy, "unmapped", EvaluatorOutcomeCompleted,
			[]synthesis.CheckObservation{checkpoint5Check("failed", synthesis.CheckFailed)},
			[]string{"unmapped.failure.class"}, nil, true)
		composition := ComposeEvaluation(context.Background(), checkpoint.SessionState, *checkpoint.Candidate, policy,
			[]EvaluatorExecution{execution}, nil)
		if composition.Disposition != DispositionCompositionFailure || composition.Evaluation != nil {
			t.Fatalf("unmapped composition = %+v", composition)
		}
		if !strings.Contains(composition.FailureDetail, "unmapped.failure.class") {
			t.Fatalf("unmapped failure detail = %q", composition.FailureDetail)
		}
	})
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func TestComposeEvaluationRejectsDuplicateCheckIDsAcrossEvaluators(t *testing.T) {
	_, checkpoint, policy := checkpoint5ReadyFixture(t, func(policy *EvaluationPolicy) {
		policy.Evaluators = []EvaluatorSpec{{EvaluatorID: "first", Required: true}, {EvaluatorID: "second", Required: true}}
		policy.RequiredCheckIDs = []string{}
	})
	first := checkpoint5Execution(t, checkpoint, policy, "first", EvaluatorOutcomeCompleted,
		[]synthesis.CheckObservation{checkpoint5Check("shared-check", synthesis.CheckPassed)}, nil, nil, true)
	second := checkpoint5Execution(t, checkpoint, policy, "second", EvaluatorOutcomeCompleted,
		[]synthesis.CheckObservation{checkpoint5Check("shared-check", synthesis.CheckPassed)}, nil, nil, true)
	composition := ComposeEvaluation(context.Background(), checkpoint.SessionState, *checkpoint.Candidate, policy,
		[]EvaluatorExecution{second, first}, nil)
	if composition.Disposition != DispositionCompositionFailure || !strings.Contains(composition.FailureDetail, "duplicate check_id") {
		t.Fatalf("duplicate cross-evaluator check composition = %+v", composition)
	}
}

func TestComposeEvaluationExcludesUnavailableRequiredEvaluatorFromAcceptedBindings(t *testing.T) {
	_, checkpoint, policy := checkpoint5ReadyFixture(t, func(policy *EvaluationPolicy) {
		policy.Evaluators = []EvaluatorSpec{{EvaluatorID: "complete", Required: true}, {EvaluatorID: "unavailable", Required: true}}
		policy.RequiredCheckIDs = []string{}
	})
	complete := checkpoint5Execution(t, checkpoint, policy, "complete", EvaluatorOutcomeCompleted,
		[]synthesis.CheckObservation{checkpoint5Check("complete-check", synthesis.CheckPassed)}, nil, nil, true)
	unavailable := checkpoint5Execution(t, checkpoint, policy, "unavailable", EvaluatorOutcomeUnavailable,
		[]synthesis.CheckObservation{}, nil, nil, true)
	composition := ComposeEvaluation(context.Background(), checkpoint.SessionState, *checkpoint.Candidate, policy,
		[]EvaluatorExecution{unavailable, complete}, nil)
	if composition.Disposition != DispositionRequiredEvaluatorUnavailable {
		t.Fatalf("required unavailable composition = %+v", composition)
	}
	if len(composition.EvaluatorBindings) != 1 || composition.EvaluatorBindings[0].EvaluatorID != "complete" {
		t.Fatalf("required unavailable bindings include unaccepted evaluator: %+v", composition.EvaluatorBindings)
	}
}
