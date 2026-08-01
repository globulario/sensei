// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"strings"
	"testing"
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
