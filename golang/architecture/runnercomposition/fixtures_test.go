// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import "testing"

func stringPtr(s string) *string { return &s }

// fixtureManifestEntries returns one regular file, one executable file, and
// one symlink, each with a correctly recomputed ContentDigestSHA256.
func fixtureManifestEntries(t *testing.T) []CandidateManifestEntry {
	t.Helper()
	entries := []CandidateManifestEntry{
		{Path: "main.go", Mode: ModeRegular, Content: []byte("package main\n")},
		{Path: "scripts/build.sh", Mode: ModeExecutable, Content: []byte("#!/bin/sh\necho build\n")},
		{Path: "link.txt", Mode: ModeSymlink, Content: []byte{}, SymlinkTarget: "main.go"},
	}
	for i, e := range entries {
		if e.Mode == ModeSymlink {
			entries[i].ContentDigestSHA256 = sha256Hex([]byte(e.SymlinkTarget))
		} else {
			entries[i].ContentDigestSHA256 = sha256Hex(e.Content)
		}
	}
	return entries
}

// fixtureCandidateArtifact builds a fully valid, digest-consistent
// CandidateArtifact fixture.
func fixtureCandidateArtifact(t *testing.T) CandidateArtifact {
	t.Helper()
	manifest := fixtureManifestEntries(t)
	finalDigest, err := ManifestDigest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	a := CandidateArtifact{
		SchemaVersion:                     CandidateArtifactSchemaVersion,
		RepositoryDomain:                  "github.com/globulario/sensei",
		BaseRevision:                      "c66a1d302272dc1ba96fcdc81ab4b127cd580992",
		WorkspaceIdentityDigestSHA256:     zeroDigest,
		SessionDigestSHA256:               zeroDigest,
		PlanDigestSHA256:                  zeroDigest,
		PlanGeneration:                    1,
		AttemptNumber:                     1,
		InputCandidateDigestSHA256:        zeroDigest,
		ProposedChangeDigestSHA256:        zeroDigest,
		FinalCandidateContentDigestSHA256: finalDigest,
		Manifest:                          manifest,
	}
	digest, err := CandidateArtifactDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	a.CandidateArtifactDigestSHA256 = digest
	return a
}

// fixtureRunnerReceiptVerified builds a RunnerReceipt with
// Disposition == DispositionVerified, every digest field non-nil,
// referencing artifact.
func fixtureRunnerReceiptVerified(t *testing.T, artifact CandidateArtifact) RunnerReceipt {
	t.Helper()
	r := RunnerReceipt{
		SchemaVersion:                     RunnerReceiptSchemaVersion,
		ReceiptID:                         "runnerreceipt.fixture.verified",
		RequestDigestSHA256:               zeroDigest,
		ResultDigestSHA256:                stringPtr(zeroDigest),
		O2ReceiptDigestSHA256:             stringPtr(zeroDigest),
		InputCandidateDigestSHA256:        stringPtr(artifact.InputCandidateDigestSHA256),
		ProposedChangeDigestSHA256:        stringPtr(artifact.ProposedChangeDigestSHA256),
		FinalCandidateContentDigestSHA256: stringPtr(artifact.FinalCandidateContentDigestSHA256),
		CandidateArtifactDigestSHA256:     stringPtr(artifact.CandidateArtifactDigestSHA256),
		Disposition:                       DispositionVerified,
		FailureDetail:                     "",
		CompletedAt:                       "2026-07-31T00:00:00Z",
	}
	digest, err := RunnerReceiptDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	r.RunnerReceiptDigestSHA256 = digest
	return r
}

// fixtureRunnerReceiptDigestMismatch mirrors fixtureRunnerReceiptVerified but
// with Disposition == DispositionDigestMismatch -- every digest field is
// still non-nil (O2's Run completed and O3 computed evidence), only the
// disposition and failure detail differ.
func fixtureRunnerReceiptDigestMismatch(t *testing.T, artifact CandidateArtifact) RunnerReceipt {
	t.Helper()
	r := fixtureRunnerReceiptVerified(t, artifact)
	r.ReceiptID = "runnerreceipt.fixture.digest-mismatch"
	r.Disposition = DispositionDigestMismatch
	r.FailureDetail = "provider-declared proposed_change_digest_sha256 does not match O3's independently computed evidence"
	digest, err := RunnerReceiptDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	r.RunnerReceiptDigestSHA256 = digest
	return r
}

// fixtureRunnerReceiptCleanupFailure mirrors fixtureRunnerReceiptVerified but
// with Disposition == DispositionCleanupFailure -- every digest field is
// still non-nil (sealing already succeeded), only cleanup of the ephemeral
// capture surface failed afterward.
func fixtureRunnerReceiptCleanupFailure(t *testing.T, artifact CandidateArtifact) RunnerReceipt {
	t.Helper()
	r := fixtureRunnerReceiptVerified(t, artifact)
	r.ReceiptID = "runnerreceipt.fixture.cleanup-failure"
	r.Disposition = DispositionCleanupFailure
	r.FailureDetail = "failed to remove the ephemeral candidate buffer directory after sealing"
	digest, err := RunnerReceiptDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	r.RunnerReceiptDigestSHA256 = digest
	return r
}

// fixtureRunnerReceiptSnapshotFailure builds a RunnerReceipt with
// Disposition == DispositionSnapshotFailure: only RequestDigestSHA256 is
// non-nil, every other digest field is nil.
func fixtureRunnerReceiptSnapshotFailure(t *testing.T) RunnerReceipt {
	t.Helper()
	r := RunnerReceipt{
		SchemaVersion:       RunnerReceiptSchemaVersion,
		ReceiptID:           "runnerreceipt.fixture.snapshot-failure",
		RequestDigestSHA256: zeroDigest,
		Disposition:         DispositionSnapshotFailure,
		FailureDetail:       "git show of the pinned base revision failed: unknown revision",
		CompletedAt:         "2026-07-31T00:00:00Z",
	}
	digest, err := RunnerReceiptDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	r.RunnerReceiptDigestSHA256 = digest
	return r
}

// fixtureRunnerReceiptSealFailure builds a RunnerReceipt with
// Disposition == DispositionSealFailure: every digest field is non-nil
// except CandidateArtifactDigestSHA256.
func fixtureRunnerReceiptSealFailure(t *testing.T, artifact CandidateArtifact) RunnerReceipt {
	t.Helper()
	r := RunnerReceipt{
		SchemaVersion:                     RunnerReceiptSchemaVersion,
		ReceiptID:                         "runnerreceipt.fixture.seal-failure",
		RequestDigestSHA256:               zeroDigest,
		ResultDigestSHA256:                stringPtr(zeroDigest),
		O2ReceiptDigestSHA256:             stringPtr(zeroDigest),
		InputCandidateDigestSHA256:        stringPtr(artifact.InputCandidateDigestSHA256),
		ProposedChangeDigestSHA256:        stringPtr(artifact.ProposedChangeDigestSHA256),
		FinalCandidateContentDigestSHA256: stringPtr(artifact.FinalCandidateContentDigestSHA256),
		CandidateArtifactDigestSHA256:     nil,
		Disposition:                       DispositionSealFailure,
		FailureDetail:                     "CandidateArtifactStore.Put failed: disk full",
		CompletedAt:                       "2026-07-31T00:00:00Z",
	}
	digest, err := RunnerReceiptDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	r.RunnerReceiptDigestSHA256 = digest
	return r
}
