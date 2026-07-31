// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"encoding/json"
	"fmt"
)

// ValidateCandidateArtifact is the one canonical acceptance path for a
// CandidateArtifact (hard law 14a). Neither a digest recomputation nor
// semantic checks alone certify a document -- a re-stamped document can have
// an internally-correct outer digest and a self-consistent manifest while
// still violating the closed schema (wrong schema_version, an empty
// required string, an out-of-range generation/attempt number, a malformed
// referenced digest, or nil Content/SymlinkTarget that marshals as JSON
// null where the schema requires a string). So this function runs, in
// order:
//
//  1. marshal a and validate it against the embedded closed schema
//     (ValidateCandidateArtifactSchema);
//  2. every manifest entry's path/mode/content-digest consistency (the same
//     checks CanonicalizeManifest already applies);
//  3. FinalCandidateContentDigestSHA256 == ManifestDigest(Manifest);
//  4. the declared CandidateArtifactDigestSHA256 equals a fresh
//     recomputation.
//
// CandidateArtifactStore.Put/Get must call this to truthfully promise
// "verified".
func ValidateCandidateArtifact(a CandidateArtifact) error {
	data, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := ValidateCandidateArtifactSchema(data); err != nil {
		return fmt.Errorf("schema: %w", err)
	}

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

// ValidateRunnerReceipt is the one canonical acceptance path for a
// RunnerReceipt (hard law 14a), running, in order:
//
//  1. marshal r and validate it against the embedded closed schema
//     (ValidateRunnerReceiptSchema);
//  2. Disposition is one of the ten closed values, and every digest field's
//     presence matches FieldPresenceFor(Disposition) exactly (the design
//     doc's disposition matrix);
//  3. FailureDetail's presence rule (non-empty unless verified);
//  4. CleanupSucceeded's presence rule per CleanupRequirementFor(Disposition)
//     -- nil for snapshot-failure, a non-nil boolean for every disposition
//     from provider-construction-failure onward, and EITHER for
//     workspace-init-failure specifically, since whether an ephemeral
//     surface existed before that failure is not generally knowable;
//     CleanupFailureDetail's presence rule (non-empty iff CleanupSucceeded
//     is false);
//  5. the declared RunnerReceiptDigestSHA256 equals a fresh recomputation.
func ValidateRunnerReceipt(r RunnerReceipt) error {
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := ValidateRunnerReceiptSchema(data); err != nil {
		return fmt.Errorf("schema: %w", err)
	}

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

	cleanupReq, err := CleanupRequirementFor(r.Disposition)
	if err != nil {
		return err
	}
	switch cleanupReq {
	case CleanupNotApplicable:
		if r.CleanupSucceeded != nil {
			return fmt.Errorf("cleanup_succeeded must be nil for disposition %q -- no ephemeral surface can exist yet", r.Disposition)
		}
	case CleanupRequired:
		if r.CleanupSucceeded == nil {
			return fmt.Errorf("cleanup_succeeded must be non-nil for disposition %q", r.Disposition)
		}
	case CleanupAmbiguous:
		// Either nil or a non-nil boolean is valid for
		// DispositionWorkspaceInitFailure -- no check.
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
