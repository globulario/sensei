// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import "testing"

func TestEvaluationPolicyDigestEqualsIndependentlyRecomputedDigest(t *testing.T) {
	p := fixtureEvaluationPolicy(t)
	got, err := EvaluationPolicyDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != p.PolicyDigestSHA256 {
		t.Errorf("declared %q, computed %q", p.PolicyDigestSHA256, got)
	}
}

func TestEvaluationPolicyDigestInvalidatedByMutatingContent(t *testing.T) {
	p := fixtureEvaluationPolicy(t)

	tampered := p
	tampered.Evaluators = append([]EvaluatorSpec{}, p.Evaluators...)
	tampered.Evaluators[0].Required = !tampered.Evaluators[0].Required

	got, err := EvaluationPolicyDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if got == p.PolicyDigestSHA256 {
		t.Error("mutating an evaluator spec's required flag did not change the computed EvaluationPolicy digest")
	}
}

func TestEvaluatorDescriptorDigestEqualsIndependentlyRecomputedDigest(t *testing.T) {
	d := fixtureEvaluatorDescriptor(t)
	got, err := EvaluatorDescriptorDigest(d)
	if err != nil {
		t.Fatal(err)
	}
	if got != d.DescriptorDigestSHA256 {
		t.Errorf("declared %q, computed %q", d.DescriptorDigestSHA256, got)
	}
}

func TestEvaluatorDescriptorDigestInvalidatedByMutatingVersion(t *testing.T) {
	d := fixtureEvaluatorDescriptor(t)

	tampered := d
	tampered.EvaluatorVersion = "2.0.0"

	got, err := EvaluatorDescriptorDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if got == d.DescriptorDigestSHA256 {
		t.Error("mutating evaluator_version did not change the computed EvaluatorDescriptor digest")
	}
}

func TestEvaluationInputDigestEqualsIndependentlyRecomputedDigest(t *testing.T) {
	i := fixtureEvaluationInput(t)
	got, err := EvaluationInputDigest(i)
	if err != nil {
		t.Fatal(err)
	}
	if got != i.EvaluationInputDigestSHA256 {
		t.Errorf("declared %q, computed %q", i.EvaluationInputDigestSHA256, got)
	}
}

func TestEvaluationInputDigestInvalidatedByMutatingAttemptNumber(t *testing.T) {
	i := fixtureEvaluationInput(t)

	tampered := i
	tampered.AttemptNumber++

	got, err := EvaluationInputDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if got == i.EvaluationInputDigestSHA256 {
		t.Error("mutating attempt_number did not change the computed EvaluationInput digest")
	}
}

func TestEvaluatorResultDigestEqualsIndependentlyRecomputedDigest(t *testing.T) {
	r := fixtureEvaluatorResult(t)
	got, err := EvaluatorResultDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	if got != r.ResultDigestSHA256 {
		t.Errorf("declared %q, computed %q", r.ResultDigestSHA256, got)
	}
}

func TestEvaluatorResultDigestInvalidatedByMutatingEvidenceReferenceDigest(t *testing.T) {
	r := fixtureEvaluatorResult(t)

	tampered := r
	refs := append([]EvidenceReference{}, r.EvidenceReferences...)
	refs[0].DigestSHA256 = zeroDigest[:63] + "1"
	tampered.EvidenceReferences = refs

	got, err := EvaluatorResultDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if got == r.ResultDigestSHA256 {
		t.Error("mutating an evidence reference's digest did not change the computed EvaluatorResult digest")
	}
}

func TestEvaluationReceiptDigestEqualsIndependentlyRecomputedDigest(t *testing.T) {
	r := fixtureEvaluationReceipt(t, DispositionEvaluated, true)
	got, err := EvaluationReceiptDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	if got != r.ReceiptDigestSHA256 {
		t.Errorf("declared %q, computed %q", r.ReceiptDigestSHA256, got)
	}
}

func TestEvaluationReceiptDigestInvalidatedByMutatingDisposition(t *testing.T) {
	r := fixtureEvaluationReceipt(t, DispositionEvaluated, true)

	tampered := r
	tampered.Disposition = DispositionCompositionFailure

	got, err := EvaluationReceiptDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if got == r.ReceiptDigestSHA256 {
		t.Error("mutating disposition did not change the computed EvaluationReceipt digest")
	}
}

func TestEvaluationReceiptDigestInvalidatedByMutatingReferencedDigest(t *testing.T) {
	r := fixtureEvaluationReceipt(t, DispositionEvaluated, true)

	tampered := r
	other := zeroDigest[:63] + "1"
	tampered.ResultDigestSHA256 = other

	got, err := EvaluationReceiptDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if got == r.ReceiptDigestSHA256 {
		t.Error("mutating result_digest_sha256 did not change the computed EvaluationReceipt digest")
	}
}

// TestEvaluationReceiptDigestExcludesCompletedAt proves receipt identity
// does not depend on wall-clock time, the same convention synthesis.Receipt
// and runnercomposition.RunnerReceipt already follow.
func TestEvaluationReceiptDigestExcludesCompletedAt(t *testing.T) {
	r := fixtureEvaluationReceipt(t, DispositionEvaluated, true)

	tampered := r
	tampered.CompletedAt = "2099-01-01T00:00:00Z"

	got, err := EvaluationReceiptDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if got != r.ReceiptDigestSHA256 {
		t.Errorf("completed_at leaked into the digest: declared %q, computed %q", r.ReceiptDigestSHA256, got)
	}
}
