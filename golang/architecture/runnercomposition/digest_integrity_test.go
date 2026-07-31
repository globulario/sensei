// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import "testing"

func TestCandidateArtifactDigestEqualsIndependentlyRecomputedDigest(t *testing.T) {
	a := fixtureCandidateArtifact(t)
	got, err := CandidateArtifactDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	if got != a.CandidateArtifactDigestSHA256 {
		t.Errorf("declared %q, computed %q", a.CandidateArtifactDigestSHA256, got)
	}
}

func TestCandidateArtifactDigestInvalidatedByMutatingManifestContent(t *testing.T) {
	a := fixtureCandidateArtifact(t)

	tampered := a
	tampered.Manifest = append([]CandidateManifestEntry{}, a.Manifest...)
	tampered.Manifest[0].Content = append([]byte{}, tampered.Manifest[0].Content...)
	tampered.Manifest[0].Content = append(tampered.Manifest[0].Content, '!')
	// Deliberately NOT recomputing ContentDigestSHA256 here -- the digest
	// function must still detect the change via the raw content, since
	// CandidateArtifactDigest hashes the whole normalized document,
	// including Content, not just the declared per-entry digest.

	got, err := CandidateArtifactDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if got == a.CandidateArtifactDigestSHA256 {
		t.Error("mutating a manifest entry's content did not change the computed CandidateArtifact digest")
	}
}

func TestCandidateArtifactDigestInvalidatedByMutatingIdentity(t *testing.T) {
	a := fixtureCandidateArtifact(t)

	tampered := a
	tampered.AttemptNumber = a.AttemptNumber + 1

	got, err := CandidateArtifactDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if got == a.CandidateArtifactDigestSHA256 {
		t.Error("mutating attempt_number did not change the computed CandidateArtifact digest")
	}
}

func TestRunnerReceiptDigestEqualsIndependentlyRecomputedDigest(t *testing.T) {
	a := fixtureCandidateArtifact(t)
	r := fixtureRunnerReceipt(t, DispositionVerified, a)
	got, err := RunnerReceiptDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	if got != r.RunnerReceiptDigestSHA256 {
		t.Errorf("declared %q, computed %q", r.RunnerReceiptDigestSHA256, got)
	}
}

func TestRunnerReceiptDigestInvalidatedByMutatingDisposition(t *testing.T) {
	a := fixtureCandidateArtifact(t)
	r := fixtureRunnerReceipt(t, DispositionVerified, a)

	tampered := r
	tampered.Disposition = DispositionDigestMismatch

	got, err := RunnerReceiptDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if got == r.RunnerReceiptDigestSHA256 {
		t.Error("mutating disposition did not change the computed RunnerReceipt digest")
	}
}

func TestRunnerReceiptDigestInvalidatedByMutatingReferencedDigest(t *testing.T) {
	a := fixtureCandidateArtifact(t)
	r := fixtureRunnerReceipt(t, DispositionVerified, a)

	tampered := r
	other := zeroDigest[:63] + "1"
	tampered.ResultDigestSHA256 = &other

	got, err := RunnerReceiptDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if got == r.RunnerReceiptDigestSHA256 {
		t.Error("mutating result_digest_sha256 did not change the computed RunnerReceipt digest")
	}
}

// TestRunnerReceiptDigestExcludesCompletedAt proves receipt identity does
// not depend on wall-clock time, the same convention synthesis.Receipt and
// providerport.Receipt already follow.
func TestRunnerReceiptDigestExcludesCompletedAt(t *testing.T) {
	a := fixtureCandidateArtifact(t)
	r := fixtureRunnerReceipt(t, DispositionVerified, a)

	tampered := r
	tampered.CompletedAt = "2099-01-01T00:00:00Z"

	got, err := RunnerReceiptDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if got != r.RunnerReceiptDigestSHA256 {
		t.Errorf("completed_at leaked into the digest: declared %q, computed %q", r.RunnerReceiptDigestSHA256, got)
	}
}
