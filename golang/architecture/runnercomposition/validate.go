// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import "fmt"

// ValidateCandidateArtifact performs the semantic checks a bare digest
// recomputation does not (hard law 14a): every manifest entry's path, mode,
// and content digest are internally consistent (the same checks
// CanonicalizeManifest already applies), FinalCandidateContentDigestSHA256
// equals ManifestDigest(Manifest), and the declared
// CandidateArtifactDigestSHA256 equals a fresh recomputation.
// CandidateArtifactStore.Put/Get must call this to truthfully promise
// "verified" -- CandidateArtifactDigest alone only computes what the digest
// would be; it does not prove the document is internally consistent.
func ValidateCandidateArtifact(a CandidateArtifact) error {
	finalDigest, err := ManifestDigest(a.Manifest)
	if err != nil {
		return fmt.Errorf("manifest: %w", err)
	}
	if a.FinalCandidateContentDigestSHA256 != finalDigest {
		return fmt.Errorf("final_candidate_content_digest_sha256 %q does not match ManifestDigest(manifest) %q", a.FinalCandidateContentDigestSHA256, finalDigest)
	}
	wantDigest, err := CandidateArtifactDigest(a)
	if err != nil {
		return fmt.Errorf("candidate_artifact_digest_sha256: %w", err)
	}
	if a.CandidateArtifactDigestSHA256 != wantDigest {
		return fmt.Errorf("candidate_artifact_digest_sha256 %q does not match recomputed %q", a.CandidateArtifactDigestSHA256, wantDigest)
	}
	return nil
}

// ValidateRunnerReceipt performs the semantic checks a bare digest
// recomputation does not (hard law 14a): Disposition is one of the eight
// closed values, every digest field's presence matches
// FieldPresenceFor(Disposition) exactly (the design doc's disposition
// matrix), FailureDetail/CleanupSucceeded/CleanupFailureDetail follow their
// own presence rules, and the declared RunnerReceiptDigestSHA256 equals a
// fresh recomputation.
func ValidateRunnerReceipt(r RunnerReceipt) error {
	want, err := FieldPresenceFor(r.Disposition)
	if err != nil {
		return err
	}
	for _, c := range []struct {
		field       string
		ptr         *string
		wantPresent bool
	}{
		{"result_digest_sha256", r.ResultDigestSHA256, want.Result},
		{"o2_receipt_digest_sha256", r.O2ReceiptDigestSHA256, want.O2Receipt},
		{"input_candidate_digest_sha256", r.InputCandidateDigestSHA256, want.InputCandidate},
		{"proposed_change_digest_sha256", r.ProposedChangeDigestSHA256, want.ProposedChange},
		{"final_candidate_content_digest_sha256", r.FinalCandidateContentDigestSHA256, want.FinalCandidateContent},
		{"candidate_artifact_digest_sha256", r.CandidateArtifactDigestSHA256, want.CandidateArtifact},
	} {
		if c.wantPresent && c.ptr == nil {
			return fmt.Errorf("%s must be non-nil for disposition %q", c.field, r.Disposition)
		}
		if !c.wantPresent && c.ptr != nil {
			return fmt.Errorf("%s must be nil for disposition %q", c.field, r.Disposition)
		}
	}

	if r.Disposition == DispositionVerified && r.FailureDetail != "" {
		return fmt.Errorf("failure_detail must be empty when disposition is verified")
	}
	if r.Disposition != DispositionVerified && r.FailureDetail == "" {
		return fmt.Errorf("failure_detail must be non-empty when disposition is %q", r.Disposition)
	}

	if r.Disposition == DispositionSnapshotFailure {
		if r.CleanupSucceeded != nil {
			return fmt.Errorf("cleanup_succeeded must be nil for disposition snapshot-failure -- no ephemeral surface was ever created")
		}
	} else if r.CleanupSucceeded == nil {
		return fmt.Errorf("cleanup_succeeded must be non-nil for disposition %q", r.Disposition)
	}
	if r.CleanupSucceeded != nil && !*r.CleanupSucceeded && r.CleanupFailureDetail == "" {
		return fmt.Errorf("cleanup_failure_detail must be non-empty when cleanup_succeeded is false")
	}
	if (r.CleanupSucceeded == nil || *r.CleanupSucceeded) && r.CleanupFailureDetail != "" {
		return fmt.Errorf("cleanup_failure_detail must be empty when cleanup_succeeded is nil or true")
	}

	wantDigest, err := RunnerReceiptDigest(r)
	if err != nil {
		return fmt.Errorf("runner_receipt_digest_sha256: %w", err)
	}
	if r.RunnerReceiptDigestSHA256 != wantDigest {
		return fmt.Errorf("runner_receipt_digest_sha256 %q does not match recomputed %q", r.RunnerReceiptDigestSHA256, wantDigest)
	}
	return nil
}
