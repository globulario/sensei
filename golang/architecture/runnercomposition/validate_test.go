// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import "testing"

func TestValidateCandidateArtifactAcceptsValidFixture(t *testing.T) {
	a := fixtureCandidateArtifact(t)
	if err := ValidateCandidateArtifact(a); err != nil {
		t.Errorf("valid fixture rejected: %v", err)
	}
}

// TestValidateCandidateArtifactRejectsDigestCorrectButSemanticallyInvalid is
// the adversarial proof the architect asked for: a document whose OWN
// declared CandidateArtifactDigestSHA256 is internally correct (it really is
// CandidateArtifactDigest of the tampered value) must still be rejected,
// because the underlying manifest content it certifies is not
// self-consistent. A digest recomputation alone cannot catch this --
// CandidateArtifactDigest happily hashes whatever it is given.
func TestValidateCandidateArtifactRejectsDigestCorrectButSemanticallyInvalid(t *testing.T) {
	a := fixtureCandidateArtifact(t)

	// Corrupt the manifest (duplicate path) but re-stamp the artifact digest
	// so it IS internally correct for the corrupted value.
	tampered := a
	tampered.Manifest = append([]CandidateManifestEntry{}, a.Manifest...)
	tampered.Manifest = append(tampered.Manifest, tampered.Manifest[0])
	digest, err := CandidateArtifactDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	tampered.CandidateArtifactDigestSHA256 = digest

	// CandidateArtifactDigest itself agrees the digest is "correct" --
	// proving the bare digest check alone would pass this.
	recomputed, err := CandidateArtifactDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if recomputed != tampered.CandidateArtifactDigestSHA256 {
		t.Fatal("test setup invalid: expected the tampered artifact's digest to be internally self-consistent")
	}

	if err := ValidateCandidateArtifact(tampered); err == nil {
		t.Error("expected ValidateCandidateArtifact to reject a digest-correct artifact with a duplicate manifest path, but it passed")
	}
}

// TestValidateCandidateArtifactRejectsStaleFinalContentDigest proves
// FinalCandidateContentDigestSHA256 must equal ManifestDigest(Manifest) --
// re-stamping the outer artifact digest around a stale value is still
// rejected.
func TestValidateCandidateArtifactRejectsStaleFinalContentDigest(t *testing.T) {
	a := fixtureCandidateArtifact(t)

	tampered := a
	tampered.FinalCandidateContentDigestSHA256 = zeroDigest
	digest, err := CandidateArtifactDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	tampered.CandidateArtifactDigestSHA256 = digest

	if err := ValidateCandidateArtifact(tampered); err == nil {
		t.Error("expected ValidateCandidateArtifact to reject a stale final_candidate_content_digest_sha256, but it passed")
	}
}

func TestValidateCandidateArtifactRejectsMismatchedOuterDigest(t *testing.T) {
	a := fixtureCandidateArtifact(t)
	tampered := a
	tampered.CandidateArtifactDigestSHA256 = zeroDigest
	if err := ValidateCandidateArtifact(tampered); err == nil {
		t.Error("expected a mismatched candidate_artifact_digest_sha256 to be rejected")
	}
}

func TestValidateRunnerReceiptAcceptsEveryValidDisposition(t *testing.T) {
	a := fixtureCandidateArtifact(t)
	for _, d := range AllDispositions() {
		d := d
		t.Run(string(d), func(t *testing.T) {
			r := fixtureRunnerReceipt(t, d, a)
			if err := ValidateRunnerReceipt(r); err != nil {
				t.Errorf("valid disposition %q fixture rejected: %v", d, err)
			}
		})
	}
}

// TestValidateRunnerReceiptAcceptsBothCleanupShapesForWorkspaceInitFailure is
// the Go-level (not schema-level) analogue of
// TestRunnerReceiptSchemaAcceptsBothCleanupShapesForWorkspaceInitFailure:
// ValidateRunnerReceipt itself, not just the embedded schema validator, must
// accept EITHER a nil or a boolean CleanupSucceeded for
// DispositionWorkspaceInitFailure.
func TestValidateRunnerReceiptAcceptsBothCleanupShapesForWorkspaceInitFailure(t *testing.T) {
	a := fixtureCandidateArtifact(t)

	withBoolean := fixtureRunnerReceipt(t, DispositionWorkspaceInitFailure, a)
	if err := ValidateRunnerReceipt(withBoolean); err != nil {
		t.Errorf("workspace-init-failure with a boolean cleanup_succeeded rejected: %v", err)
	}

	withNil := fixtureRunnerReceiptWorkspaceInitFailureCleanupUnknown(t, a)
	if err := ValidateRunnerReceipt(withNil); err != nil {
		t.Errorf("workspace-init-failure with a nil cleanup_succeeded rejected: %v", err)
	}
}

// TestValidateRunnerReceiptRejectsDigestCorrectButWrongPresence is the
// RunnerReceipt analogue of the CandidateArtifact adversarial proof above:
// a receipt whose RunnerReceiptDigestSHA256 is internally correct for its
// (wrong) content must still be rejected, because its digest-field presence
// does not match its own declared disposition.
func TestValidateRunnerReceiptRejectsDigestCorrectButWrongPresence(t *testing.T) {
	a := fixtureCandidateArtifact(t)
	r := fixtureRunnerReceipt(t, DispositionSnapshotFailure, a)

	tampered := r
	tampered.ResultDigestSHA256 = stringPtr(zeroDigest) // verified's shape, not snapshot-failure's
	digest, err := RunnerReceiptDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	tampered.RunnerReceiptDigestSHA256 = digest

	if err := ValidateRunnerReceipt(tampered); err == nil {
		t.Error("expected ValidateRunnerReceipt to reject a digest-correct receipt whose field presence does not match its own disposition, but it passed")
	}
}

func TestValidateRunnerReceiptRejectsUnknownDisposition(t *testing.T) {
	a := fixtureCandidateArtifact(t)
	r := fixtureRunnerReceipt(t, DispositionVerified, a)
	r.Disposition = Disposition("not-a-real-disposition")
	if err := ValidateRunnerReceipt(r); err == nil {
		t.Error("expected an unknown disposition to be rejected")
	}
}

func TestValidateRunnerReceiptRejectsMismatchedOuterDigest(t *testing.T) {
	a := fixtureCandidateArtifact(t)
	r := fixtureRunnerReceipt(t, DispositionVerified, a)
	r.RunnerReceiptDigestSHA256 = zeroDigest
	if err := ValidateRunnerReceipt(r); err == nil {
		t.Error("expected a mismatched runner_receipt_digest_sha256 to be rejected")
	}
}

// --- schema-enforcement adversarial proofs: a document that is semantically
// self-consistent AND has a correct, re-stamped outer digest, but violates
// the closed schema, must still be rejected. Neither semantic checks nor
// digest recomputation alone can catch these -- only running the embedded
// schema validator does (hard law 14a). Each test confirms the digest is
// genuinely re-stamped correct, so a semantic-only ValidateCandidateArtifact/
// ValidateRunnerReceipt would have accepted it.

func TestValidateCandidateArtifactRejectsEmptyRepositoryDomainDespiteCorrectDigest(t *testing.T) {
	a := fixtureCandidateArtifact(t)
	tampered := a
	tampered.RepositoryDomain = "" // schema requires minLength 1
	digest, err := CandidateArtifactDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	tampered.CandidateArtifactDigestSHA256 = digest

	if err := ValidateCandidateArtifact(tampered); err == nil {
		t.Error("expected ValidateCandidateArtifact to reject an empty repository_domain even with a correct outer digest, but it passed")
	}
}

func TestValidateCandidateArtifactRejectsZeroPlanGenerationDespiteCorrectDigest(t *testing.T) {
	a := fixtureCandidateArtifact(t)
	tampered := a
	tampered.PlanGeneration = 0 // schema requires minimum 1
	digest, err := CandidateArtifactDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	tampered.CandidateArtifactDigestSHA256 = digest

	if err := ValidateCandidateArtifact(tampered); err == nil {
		t.Error("expected ValidateCandidateArtifact to reject plan_generation=0 even with a correct outer digest, but it passed")
	}
}

func TestValidateCandidateArtifactRejectsZeroAttemptNumberDespiteCorrectDigest(t *testing.T) {
	a := fixtureCandidateArtifact(t)
	tampered := a
	tampered.AttemptNumber = 0 // schema requires minimum 1
	digest, err := CandidateArtifactDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	tampered.CandidateArtifactDigestSHA256 = digest

	if err := ValidateCandidateArtifact(tampered); err == nil {
		t.Error("expected ValidateCandidateArtifact to reject attempt_number=0 even with a correct outer digest, but it passed")
	}
}

// TestValidateCandidateArtifactRejectsNilManifestContentDespiteCorrectDigests
// is the concrete "nil content that serializes as JSON null" case. sha256 of
// a nil []byte and sha256 of an empty, non-nil []byte are IDENTICAL (both
// the well-known empty-string digest), so an entry whose true content is
// empty and whose ContentDigestSHA256 is that empty digest passes
// CanonicalizeManifest/ManifestDigest/CandidateArtifactDigest cleanly
// whether Content is nil or []byte{} -- the digest layer cannot distinguish
// them. Only the schema catches that a nil []byte marshals to JSON null
// where the schema requires a string.
func TestValidateCandidateArtifactRejectsNilManifestContentDespiteCorrectDigests(t *testing.T) {
	a := fixtureCandidateArtifact(t)
	emptyDigest := sha256Hex(nil)
	tampered := a
	tampered.Manifest = append([]CandidateManifestEntry{}, a.Manifest...)
	tampered.Manifest = append(tampered.Manifest, CandidateManifestEntry{
		Path:                "empty.txt",
		Mode:                ModeRegular,
		Content:             nil, // marshals as JSON null
		ContentDigestSHA256: emptyDigest,
	})

	finalDigest, err := ManifestDigest(tampered.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	tampered.FinalCandidateContentDigestSHA256 = finalDigest
	digest, err := CandidateArtifactDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	tampered.CandidateArtifactDigestSHA256 = digest

	if err := ValidateCandidateArtifact(tampered); err == nil {
		t.Error("expected ValidateCandidateArtifact to reject a manifest entry with nil Content (serializes as JSON null) even with correct digests, but it passed")
	}
}

func TestValidateRunnerReceiptRejectsEmptyReceiptIDDespiteCorrectDigest(t *testing.T) {
	a := fixtureCandidateArtifact(t)
	r := fixtureRunnerReceipt(t, DispositionVerified, a)
	tampered := r
	tampered.ReceiptID = "" // schema requires minLength 1
	digest, err := RunnerReceiptDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	tampered.RunnerReceiptDigestSHA256 = digest

	if err := ValidateRunnerReceipt(tampered); err == nil {
		t.Error("expected ValidateRunnerReceipt to reject an empty receipt_id even with a correct outer digest, but it passed")
	}
}

func TestValidateRunnerReceiptRejectsMalformedReferencedDigestPatternDespiteCorrectDigest(t *testing.T) {
	a := fixtureCandidateArtifact(t)
	r := fixtureRunnerReceipt(t, DispositionVerified, a)
	tampered := r
	tampered.ResultDigestSHA256 = stringPtr("not-a-valid-hex-digest") // schema requires ^[0-9a-f]{64}$
	digest, err := RunnerReceiptDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	tampered.RunnerReceiptDigestSHA256 = digest

	if err := ValidateRunnerReceipt(tampered); err == nil {
		t.Error("expected ValidateRunnerReceipt to reject a malformed result_digest_sha256 pattern even with a correct outer digest, but it passed")
	}
}

func TestValidateRunnerReceiptRejectsWrongSchemaVersionDespiteCorrectDigest(t *testing.T) {
	a := fixtureCandidateArtifact(t)
	r := fixtureRunnerReceipt(t, DispositionVerified, a)
	tampered := r
	tampered.SchemaVersion = "sensei.runnercomposition.runnerreceipt.v0" // schema requires the const v1 value
	digest, err := RunnerReceiptDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	tampered.RunnerReceiptDigestSHA256 = digest

	if err := ValidateRunnerReceipt(tampered); err == nil {
		t.Error("expected ValidateRunnerReceipt to reject a wrong schema_version even with a correct outer digest, but it passed")
	}
}
