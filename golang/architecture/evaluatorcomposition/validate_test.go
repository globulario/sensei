// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/synthesis"
)

func TestValidateEvaluationPolicyAcceptsValidFixture(t *testing.T) {
	p := fixtureEvaluationPolicy(t)
	if err := ValidateEvaluationPolicy(p); err != nil {
		t.Fatalf("valid fixture rejected: %v", err)
	}
}

func TestValidateEvaluationPolicyRejectsDuplicateEvaluatorID(t *testing.T) {
	p := fixtureEvaluationPolicy(t)
	p.Evaluators = append(p.Evaluators, EvaluatorSpec{EvaluatorID: p.Evaluators[0].EvaluatorID, Required: !p.Evaluators[0].Required})
	digest, err := EvaluationPolicyDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	p.PolicyDigestSHA256 = digest

	if err := ValidateEvaluationPolicy(p); err == nil {
		t.Error("duplicate evaluator_id was wrongly accepted")
	} else if !strings.Contains(err.Error(), "appears more than once") {
		t.Errorf("wrong error for duplicate evaluator_id: %v", err)
	}
}

func TestValidateEvaluationPolicyRejectsDuplicateFailureClass(t *testing.T) {
	p := fixtureEvaluationPolicy(t)
	p.FailureClassRecommendations = append(p.FailureClassRecommendations, FailureClassRecommendation{
		FailureClass:   p.FailureClassRecommendations[0].FailureClass,
		Recommendation: "replan",
	})
	digest, err := EvaluationPolicyDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	p.PolicyDigestSHA256 = digest

	if err := ValidateEvaluationPolicy(p); err == nil {
		t.Error("duplicate failure_class was wrongly accepted")
	} else if !strings.Contains(err.Error(), "appears more than once") {
		t.Errorf("wrong error for duplicate failure_class: %v", err)
	}
}

func TestValidateEvaluationPolicyRejectsMutatedDigest(t *testing.T) {
	p := fixtureEvaluationPolicy(t)
	p.PolicyDigestSHA256 = zeroDigest[:63] + "1"
	if err := ValidateEvaluationPolicy(p); err == nil {
		t.Error("mutated policy_digest_sha256 was wrongly accepted")
	}
}

// finishPolicy recomputes and sets PolicyDigestSHA256.
func finishPolicy(t *testing.T, p EvaluationPolicy) EvaluationPolicy {
	t.Helper()
	digest, err := EvaluationPolicyDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	p.PolicyDigestSHA256 = digest
	return p
}

// policyWithFailureClassRule returns a fixture policy whose sole
// failure_class_recommendations entry maps class to recommendation.
func policyWithFailureClassRule(t *testing.T, class string, recommendation synthesis.Recommendation) EvaluationPolicy {
	t.Helper()
	p := fixtureEvaluationPolicy(t)
	p.FailureClassRecommendations = []FailureClassRecommendation{
		{FailureClass: class, Recommendation: recommendation},
	}
	return finishPolicy(t, p)
}

// TestValidateEvaluationPolicyAcceptsGovernedFailureClassAtItsMinimum proves
// a policy mapping a governed failure class to exactly its canonical
// minimum recommendation is accepted (equal severity).
func TestValidateEvaluationPolicyAcceptsGovernedFailureClassAtItsMinimum(t *testing.T) {
	p := policyWithFailureClassRule(t, string(FailureClassProofPlanStructural), synthesis.RecommendReplan)
	if err := ValidateEvaluationPolicy(p); err != nil {
		t.Errorf("equal-severity governed failure_class mapping wrongly rejected: %v", err)
	}
}

// TestValidateEvaluationPolicyAcceptsGovernedFailureClassEscalation proves a
// policy mapping a governed failure class to something MORE severe than its
// canonical minimum is accepted.
func TestValidateEvaluationPolicyAcceptsGovernedFailureClassEscalation(t *testing.T) {
	// proof-obligation-plan-structural's minimum is replan; abort and
	// architect-review are both more severe.
	for _, escalated := range []synthesis.Recommendation{synthesis.RecommendAbort, synthesis.RecommendArchitectReview} {
		escalated := escalated
		t.Run(string(escalated), func(t *testing.T) {
			p := policyWithFailureClassRule(t, string(FailureClassProofPlanStructural), escalated)
			if err := ValidateEvaluationPolicy(p); err != nil {
				t.Errorf("escalating governed failure_class mapping to %q wrongly rejected: %v", escalated, err)
			}
		})
	}
}

// TestValidateEvaluationPolicyRejectsGovernedFailureClassDowngrade proves a
// policy mapping a governed failure class to something LESS severe than its
// canonical minimum -- the exact "forbidden-fix mapped from abort to
// retry-generation" scenario the architect flagged -- is rejected before
// any O1 interaction (ValidateEvaluationPolicy is pure and has no side
// effects, so rejection here structurally precedes the first Transition
// call once checkpoint 3 wires this validator into that precondition).
func TestValidateEvaluationPolicyRejectsGovernedFailureClassDowngrade(t *testing.T) {
	p := policyWithFailureClassRule(t, string(FailureClassAuditForbiddenFix), synthesis.RecommendRetryGeneration)
	err := ValidateEvaluationPolicy(p)
	if err == nil {
		t.Fatal("downgrading audit-forbidden-fix from abort to retry-generation was wrongly accepted")
	}
	if !strings.Contains(err.Error(), "downgrades below its governed minimum") {
		t.Errorf("wrong error for governed failure_class downgrade: %v", err)
	}
}

// TestValidateEvaluationPolicyRejectsGovernedFailureClassPrecedenceReordering
// proves every level below a governed class's minimum is rejected, not just
// the adjacent one -- i.e. the check is a true severity-rank comparison,
// not a single-step-down special case.
func TestValidateEvaluationPolicyRejectsGovernedFailureClassPrecedenceReordering(t *testing.T) {
	// proof-obligation-plan-structural's minimum is replan (rank 2);
	// retry-generation (rank 3) is the only value strictly less severe.
	p := policyWithFailureClassRule(t, string(FailureClassProofPlanStructural), synthesis.RecommendRetryGeneration)
	if err := ValidateEvaluationPolicy(p); err == nil {
		t.Error("downgrading proof-obligation-plan-structural from replan to retry-generation was wrongly accepted")
	}
}

// TestValidateEvaluationPolicyIgnoresMinimumForUngovernedFailureClass proves
// a failure_class outside the governed registry is not bound to any floor
// -- evaluators and policies remain free to define their own ad hoc
// classes.
func TestValidateEvaluationPolicyIgnoresMinimumForUngovernedFailureClass(t *testing.T) {
	p := policyWithFailureClassRule(t, "some-evaluator-specific-classification", synthesis.RecommendRetryGeneration)
	if err := ValidateEvaluationPolicy(p); err != nil {
		t.Errorf("ungoverned failure_class wrongly rejected: %v", err)
	}
}

func TestValidateEvaluatorDescriptorAcceptsValidFixture(t *testing.T) {
	d := fixtureEvaluatorDescriptor(t)
	if err := ValidateEvaluatorDescriptor(d); err != nil {
		t.Fatalf("valid fixture rejected: %v", err)
	}
}

func TestValidateEvaluatorDescriptorRejectsMutatedDigest(t *testing.T) {
	d := fixtureEvaluatorDescriptor(t)
	d.DescriptorDigestSHA256 = zeroDigest[:63] + "1"
	if err := ValidateEvaluatorDescriptor(d); err == nil {
		t.Error("mutated descriptor_digest_sha256 was wrongly accepted")
	}
}

func TestValidateEvaluationInputAcceptsValidFixture(t *testing.T) {
	i := fixtureEvaluationInput(t)
	if err := ValidateEvaluationInput(i); err != nil {
		t.Fatalf("valid fixture rejected: %v", err)
	}
}

func TestValidateEvaluationInputRejectsMutatedDigest(t *testing.T) {
	i := fixtureEvaluationInput(t)
	i.EvaluationInputDigestSHA256 = zeroDigest[:63] + "1"
	if err := ValidateEvaluationInput(i); err == nil {
		t.Error("mutated evaluation_input_digest_sha256 was wrongly accepted")
	}
}

func TestValidateEvaluatorResultAcceptsValidFixture(t *testing.T) {
	r := fixtureEvaluatorResult(t)
	if err := ValidateEvaluatorResult(r); err != nil {
		t.Fatalf("valid fixture rejected: %v", err)
	}
}

func TestValidateEvaluatorResultRejectsMutatedDigest(t *testing.T) {
	r := fixtureEvaluatorResult(t)
	r.ResultDigestSHA256 = zeroDigest[:63] + "1"
	if err := ValidateEvaluatorResult(r); err == nil {
		t.Error("mutated result_digest_sha256 was wrongly accepted")
	}
}

// TestValidateEvaluationReceiptAcceptsValidFixtures proves every disposition
// (both evaluated variants, plus cleanup-failed variants where applicable)
// passes full validation, not just schema validation.
func TestValidateEvaluationReceiptAcceptsValidFixtures(t *testing.T) {
	for _, d := range AllDispositions() {
		for _, terminal := range []bool{true, false} {
			if d != DispositionEvaluated && !terminal {
				continue
			}
			r := fixtureEvaluationReceipt(t, d, terminal)
			if err := ValidateEvaluationReceipt(r); err != nil {
				t.Errorf("valid disposition %q (evaluatedTerminal=%v) fixture rejected: %v", d, terminal, err)
			}

			if d == DispositionInvalidOutputTerminated || d == DispositionCandidateLoadFailure {
				continue
			}
			cleanupFailed := fixtureEvaluationReceiptCleanupFailed(t, d, terminal)
			if err := ValidateEvaluationReceipt(cleanupFailed); err != nil {
				t.Errorf("valid cleanup-failed disposition %q (evaluatedTerminal=%v) fixture rejected: %v", d, terminal, err)
			}
		}
	}
}

func TestValidateEvaluationReceiptRejectsMutatedDigest(t *testing.T) {
	r := fixtureEvaluationReceipt(t, DispositionEvaluated, true)
	r.ReceiptDigestSHA256 = zeroDigest[:63] + "1"
	if err := ValidateEvaluationReceipt(r); err == nil {
		t.Error("mutated receipt_digest_sha256 was wrongly accepted")
	}
}

// TestValidateEvaluationReceiptRejectsWrongCandidateArtifactVerified proves
// the Go-level cross-check (redundant with the schema, per hard law
// defense-in-depth) fires for every disposition when
// CandidateArtifactVerified is flipped away from FieldPresenceFor(d).
func TestValidateEvaluationReceiptRejectsWrongCandidateArtifactVerified(t *testing.T) {
	for _, d := range AllDispositions() {
		d := d
		t.Run(string(d), func(t *testing.T) {
			r := fixtureEvaluationReceipt(t, d, true)
			r.CandidateArtifactVerified = !r.CandidateArtifactVerified
			digest, err := EvaluationReceiptDigest(r)
			if err != nil {
				t.Fatal(err)
			}
			r.ReceiptDigestSHA256 = digest
			if err := ValidateEvaluationReceipt(r); err == nil {
				t.Errorf("disposition %q: flipped candidate_artifact_verified was wrongly accepted", d)
			}
		})
	}
}

// TestValidateEvaluationReceiptRejectsMissingRequiredO1TerminalReceipt
// proves every disposition except DispositionEvaluated requires
// O1TerminalReceiptDigestSHA256 to be non-nil at the Go validation layer,
// not merely at the schema layer.
func TestValidateEvaluationReceiptRejectsMissingRequiredO1TerminalReceipt(t *testing.T) {
	for _, d := range AllDispositions() {
		if d == DispositionEvaluated {
			continue // ambiguous -- absence is legitimate
		}
		d := d
		t.Run(string(d), func(t *testing.T) {
			r := fixtureEvaluationReceipt(t, d, true)
			r.O1TerminalReceiptDigestSHA256 = nil
			digest, err := EvaluationReceiptDigest(r)
			if err != nil {
				t.Fatal(err)
			}
			r.ReceiptDigestSHA256 = digest
			if err := ValidateEvaluationReceipt(r); err == nil {
				t.Errorf("disposition %q: nil o1_terminal_receipt_digest_sha256 was wrongly accepted", d)
			}
		})
	}
}

func TestValidateEvaluationReceiptRejectsFailureDetailPresentWhenEvaluated(t *testing.T) {
	r := fixtureEvaluationReceipt(t, DispositionEvaluated, true)
	r.FailureDetail = "should be empty"
	digest, err := EvaluationReceiptDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	r.ReceiptDigestSHA256 = digest
	if err := ValidateEvaluationReceipt(r); err == nil {
		t.Error("non-empty failure_detail on an evaluated disposition was wrongly accepted")
	}
}

func TestValidateEvaluationReceiptRejectsFailureDetailAbsentWhenNotEvaluated(t *testing.T) {
	r := fixtureEvaluationReceipt(t, DispositionCandidateLoadFailure, true)
	r.FailureDetail = ""
	digest, err := EvaluationReceiptDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	r.ReceiptDigestSHA256 = digest
	if err := ValidateEvaluationReceipt(r); err == nil {
		t.Error("empty failure_detail on a non-evaluated disposition was wrongly accepted")
	}
}

func TestValidateEvaluationReceiptRejectsCleanupFailureDetailMismatch(t *testing.T) {
	r := fixtureEvaluationReceipt(t, DispositionEvaluated, true)
	// CleanupSucceeded is true here, so a non-empty CleanupFailureDetail
	// must be rejected.
	r.CleanupFailureDetail = "should be empty"
	digest, err := EvaluationReceiptDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	r.ReceiptDigestSHA256 = digest
	if err := ValidateEvaluationReceipt(r); err == nil {
		t.Error("non-empty cleanup_failure_detail alongside cleanup_succeeded=true was wrongly accepted")
	}
}

// TestValidateEvaluationReceiptAcceptsCanonicallyOrderedMultipleBindings
// proves a receipt with several distinct, strictly ascending EvaluatorID
// bindings is accepted -- not just the single-binding fixture shape.
func TestValidateEvaluationReceiptAcceptsCanonicallyOrderedMultipleBindings(t *testing.T) {
	r := fixtureEvaluationReceipt(t, DispositionEvaluated, true)
	r.EvaluatorResultBindings = []EvaluatorResultBinding{
		{EvaluatorID: "a.first", DescriptorDigestSHA256: zeroDigest, ResultDigestSHA256: zeroDigest},
		{EvaluatorID: "b.second", DescriptorDigestSHA256: zeroDigest, ResultDigestSHA256: zeroDigest},
		{EvaluatorID: "c.third", DescriptorDigestSHA256: zeroDigest, ResultDigestSHA256: zeroDigest},
	}
	r = finishEvaluationReceipt(t, r)
	if err := ValidateEvaluationReceipt(r); err != nil {
		t.Errorf("canonically ordered multi-binding receipt wrongly rejected: %v", err)
	}
}

// TestValidateEvaluationReceiptRejectsDuplicateEvaluatorIDBinding proves a
// receipt naming the same evaluator twice in evaluator_result_bindings is
// rejected -- the receipt cannot answer "which evaluator produced this
// result" if the same evaluator ID is ambiguous within it.
func TestValidateEvaluationReceiptRejectsDuplicateEvaluatorIDBinding(t *testing.T) {
	r := fixtureEvaluationReceipt(t, DispositionEvaluated, true)
	r.EvaluatorResultBindings = []EvaluatorResultBinding{
		{EvaluatorID: "a.first", DescriptorDigestSHA256: zeroDigest, ResultDigestSHA256: zeroDigest},
		{EvaluatorID: "a.first", DescriptorDigestSHA256: zeroDigest, ResultDigestSHA256: zeroDigest[:63] + "1"},
	}
	r = finishEvaluationReceipt(t, r)
	err := ValidateEvaluationReceipt(r)
	if err == nil {
		t.Fatal("duplicate evaluator_id in evaluator_result_bindings was wrongly accepted")
	}
	if !strings.Contains(err.Error(), "appears more than once") {
		t.Errorf("wrong error for duplicate evaluator_id binding: %v", err)
	}
}

// TestValidateEvaluationReceiptRejectsOutOfOrderEvaluatorIDBindings proves a
// receipt whose evaluator_result_bindings are NOT in strictly ascending
// EvaluatorID order is rejected, not silently reordered.
func TestValidateEvaluationReceiptRejectsOutOfOrderEvaluatorIDBindings(t *testing.T) {
	r := fixtureEvaluationReceipt(t, DispositionEvaluated, true)
	r.EvaluatorResultBindings = []EvaluatorResultBinding{
		{EvaluatorID: "b.second", DescriptorDigestSHA256: zeroDigest, ResultDigestSHA256: zeroDigest},
		{EvaluatorID: "a.first", DescriptorDigestSHA256: zeroDigest, ResultDigestSHA256: zeroDigest},
	}
	r = finishEvaluationReceipt(t, r)
	err := ValidateEvaluationReceipt(r)
	if err == nil {
		t.Fatal("out-of-order evaluator_result_bindings was wrongly accepted")
	}
	if !strings.Contains(err.Error(), "sorted in strictly ascending") {
		t.Errorf("wrong error for out-of-order bindings: %v", err)
	}
}

// finishEvaluatorResult recomputes and sets ResultDigestSHA256.
func finishEvaluatorResult(t *testing.T, r EvaluatorResult) EvaluatorResult {
	t.Helper()
	digest, err := EvaluatorResultDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	r.ResultDigestSHA256 = digest
	return r
}

// TestValidateEvaluatorResultAcceptsMultipleDistinctChecksAndEvidence proves
// a result with several checks and evidence references, all correctly
// cross-referenced, is accepted -- not just the single-check fixture shape.
func TestValidateEvaluatorResultAcceptsMultipleDistinctChecksAndEvidence(t *testing.T) {
	r := fixtureEvaluatorResult(t)
	r.Checks = []synthesis.CheckObservation{
		{CheckID: "go-test", Status: synthesis.CheckPassed, EvidenceReferences: []string{"evidence://go-test/stdout"}},
		{CheckID: "go-vet", Status: synthesis.CheckFailed, EvidenceReferences: []string{"evidence://go-vet/stdout"}},
	}
	r.EvidenceReferences = []EvidenceReference{
		{Reference: "evidence://go-test/stdout", DigestSHA256: zeroDigest},
		{Reference: "evidence://go-vet/stdout", DigestSHA256: zeroDigest[:63] + "1"},
	}
	r.ClassifiedFailureReasons = []string{"go-vet-failure"}
	r = finishEvaluatorResult(t, r)
	if err := ValidateEvaluatorResult(r); err != nil {
		t.Errorf("valid multi-check result wrongly rejected: %v", err)
	}
}

func TestValidateEvaluatorResultRejectsDuplicateCheckID(t *testing.T) {
	r := fixtureEvaluatorResult(t)
	r.Checks = append(r.Checks, synthesis.CheckObservation{
		CheckID:            r.Checks[0].CheckID,
		Status:             synthesis.CheckFailed,
		EvidenceReferences: []string{},
	})
	r = finishEvaluatorResult(t, r)
	err := ValidateEvaluatorResult(r)
	if err == nil {
		t.Fatal("duplicate check_id was wrongly accepted")
	}
	if !strings.Contains(err.Error(), "appears more than once in checks") {
		t.Errorf("wrong error for duplicate check_id: %v", err)
	}
}

func TestValidateEvaluatorResultRejectsDanglingCheckEvidenceReference(t *testing.T) {
	r := fixtureEvaluatorResult(t)
	r.Checks[0].EvidenceReferences = append(r.Checks[0].EvidenceReferences, "evidence://never-captured")
	r = finishEvaluatorResult(t, r)
	err := ValidateEvaluatorResult(r)
	if err == nil {
		t.Fatal("check citing an evidence reference absent from the top-level list was wrongly accepted")
	}
	if !strings.Contains(err.Error(), "does not resolve to any top-level evidence_references entry") {
		t.Errorf("wrong error for dangling check evidence reference: %v", err)
	}
}

func TestValidateEvaluatorResultRejectsDuplicateEvidenceReference(t *testing.T) {
	r := fixtureEvaluatorResult(t)
	r.EvidenceReferences = append(r.EvidenceReferences, EvidenceReference{
		Reference:    r.EvidenceReferences[0].Reference,
		DigestSHA256: r.EvidenceReferences[0].DigestSHA256,
	})
	r = finishEvaluatorResult(t, r)
	err := ValidateEvaluatorResult(r)
	if err == nil {
		t.Fatal("exact duplicate evidence reference was wrongly accepted")
	}
	if !strings.Contains(err.Error(), "appears more than once") {
		t.Errorf("wrong error for duplicate evidence reference: %v", err)
	}
}

func TestValidateEvaluatorResultRejectsConflictingEvidenceDigest(t *testing.T) {
	r := fixtureEvaluatorResult(t)
	r.EvidenceReferences = append(r.EvidenceReferences, EvidenceReference{
		Reference:    r.EvidenceReferences[0].Reference,
		DigestSHA256: zeroDigest[:63] + "1",
	})
	r = finishEvaluatorResult(t, r)
	err := ValidateEvaluatorResult(r)
	if err == nil {
		t.Fatal("same reference with conflicting digests was wrongly accepted")
	}
	if !strings.Contains(err.Error(), "conflicting digests") {
		t.Errorf("wrong error for conflicting evidence digest: %v", err)
	}
}

func TestValidateEvaluatorResultRejectsEmptyFailureClassification(t *testing.T) {
	r := fixtureEvaluatorResult(t)
	r.ClassifiedFailureReasons = []string{""}
	r = finishEvaluatorResult(t, r)
	err := ValidateEvaluatorResult(r)
	if err == nil {
		t.Fatal("empty classified_failure_reasons entry was wrongly accepted")
	}
	if !strings.Contains(err.Error(), "must not contain an empty entry") {
		t.Errorf("wrong error for empty failure classification: %v", err)
	}
}

func TestValidateEvaluatorResultRejectsDuplicateFailureClassification(t *testing.T) {
	r := fixtureEvaluatorResult(t)
	r.ClassifiedFailureReasons = []string{"go-vet-failure", "go-vet-failure"}
	r = finishEvaluatorResult(t, r)
	err := ValidateEvaluatorResult(r)
	if err == nil {
		t.Fatal("duplicate classified_failure_reasons entry was wrongly accepted")
	}
	if !strings.Contains(err.Error(), "appears more than once") {
		t.Errorf("wrong error for duplicate failure classification: %v", err)
	}
}
