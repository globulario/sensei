// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import "testing"

func stringPtr(s string) *string { return &s }
func boolPtr(b bool) *bool       { return &b }

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

// finishReceipt fills in RunnerReceiptDigestSHA256 by recomputation.
func finishReceipt(t *testing.T, r RunnerReceipt) RunnerReceipt {
	t.Helper()
	digest, err := RunnerReceiptDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	r.RunnerReceiptDigestSHA256 = digest
	return r
}

// fixtureRunnerReceipt builds a syntactically valid RunnerReceipt for the
// given disposition, with every digest field's presence matching
// FieldPresenceFor(disposition) exactly, and CleanupSucceeded/
// CleanupFailureDetail/FailureDetail following their own presence rules.
// This is the one fixture builder every disposition-coverage test uses, so
// the presence pattern is defined in exactly one place.
func fixtureRunnerReceipt(t *testing.T, disposition Disposition, artifact CandidateArtifact) RunnerReceipt {
	t.Helper()
	presence, err := FieldPresenceFor(disposition)
	if err != nil {
		t.Fatal(err)
	}

	r := RunnerReceipt{
		SchemaVersion:       RunnerReceiptSchemaVersion,
		ReceiptID:           "runnerreceipt.fixture." + string(disposition),
		RequestDigestSHA256: zeroDigest,
		Disposition:         disposition,
		CompletedAt:         "2026-07-31T00:00:00Z",
	}
	if presence.Result {
		r.ResultDigestSHA256 = stringPtr(zeroDigest)
	}
	if presence.O2Receipt {
		r.O2ReceiptDigestSHA256 = stringPtr(zeroDigest)
	}
	if presence.InputCandidate {
		r.InputCandidateDigestSHA256 = stringPtr(artifact.InputCandidateDigestSHA256)
	}
	if presence.ProposedChange {
		r.ProposedChangeDigestSHA256 = stringPtr(artifact.ProposedChangeDigestSHA256)
	}
	if presence.FinalCandidateContent {
		r.FinalCandidateContentDigestSHA256 = stringPtr(artifact.FinalCandidateContentDigestSHA256)
	}
	if presence.CandidateArtifact {
		r.CandidateArtifactDigestSHA256 = stringPtr(artifact.CandidateArtifactDigestSHA256)
	}

	if disposition == DispositionVerified {
		r.FailureDetail = ""
	} else {
		r.FailureDetail = "fixture failure detail for " + string(disposition)
	}

	cleanupReq, err := CleanupRequirementFor(disposition)
	if err != nil {
		t.Fatal(err)
	}
	switch cleanupReq {
	case CleanupNotApplicable:
		r.CleanupSucceeded = nil
	default: // CleanupRequired or CleanupAmbiguous -- both accept a boolean.
		r.CleanupSucceeded = boolPtr(true)
	}
	r.CleanupFailureDetail = ""

	return finishReceipt(t, r)
}

// fixtureRunnerReceiptCleanupFailed mirrors fixtureRunnerReceipt for
// disposition, but with CleanupSucceeded == false and a non-empty
// CleanupFailureDetail -- proving cleanup outcome is independent of
// Disposition. Not valid for DispositionSnapshotFailure (cleanup is not
// applicable there).
func fixtureRunnerReceiptCleanupFailed(t *testing.T, disposition Disposition, artifact CandidateArtifact) RunnerReceipt {
	t.Helper()
	if disposition == DispositionSnapshotFailure {
		t.Fatal("cleanup is not applicable to DispositionSnapshotFailure")
	}
	r := fixtureRunnerReceipt(t, disposition, artifact)
	r.CleanupSucceeded = boolPtr(false)
	r.CleanupFailureDetail = "failed to remove the ephemeral candidate buffer directory"
	return finishReceipt(t, r)
}

// fixtureRunnerReceiptWorkspaceInitFailureCleanupUnknown mirrors
// fixtureRunnerReceipt(t, DispositionWorkspaceInitFailure, artifact) but with
// CleanupSucceeded == nil instead of a boolean -- proving BOTH valid shapes
// for this one ambiguous disposition are accepted, not just the boolean the
// shared builder happens to default to.
func fixtureRunnerReceiptWorkspaceInitFailureCleanupUnknown(t *testing.T, artifact CandidateArtifact) RunnerReceipt {
	t.Helper()
	r := fixtureRunnerReceipt(t, DispositionWorkspaceInitFailure, artifact)
	r.CleanupSucceeded = nil
	r.CleanupFailureDetail = ""
	return finishReceipt(t, r)
}
