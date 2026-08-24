// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// Verified against this repository's own pinned history, not a fixture.
//
// The point of the whole mechanism is that the proposer does not supply the
// bytes, so a test that supplied its own bytes would be testing nothing.
func headCommit(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "-C", "../..", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Skip("not a git checkout")
	}
	return strings.TrimSpace(string(out))
}

// A real citation verifies. The proposer chose where to look; git supplied
// what is there.
func TestARealCitationIsVerifiedFromGitNotFromTheCandidate(t *testing.T) {
	commit := headCommit(t)
	got := verifyEvidenceRefs(context.Background(), "../..", []evidenceRef{{
		Kind: "source_fact", Commit: commit, File: "cmd/awg/promote_evidence.go",
		Contains: "Evidence may be SELECTED by the proposer",
	}}, "")
	if got.Verdict != evidenceVerified {
		t.Fatalf("%+v", got)
	}
}

// A fabricated citation fails. This is the property free-text evidence could
// not have: "trust me" was unfalsifiable, and this is not.
func TestAFabricatedCitationCannotBeVerified(t *testing.T) {
	commit := headCommit(t)
	for _, ref := range []evidenceRef{
		{Kind: "source_fact", Commit: commit, File: "cmd/awg/promote_evidence.go",
			Contains: "this sentence has never appeared in this repository"},
		{Kind: "source_fact", Commit: commit, File: "cmd/awg/no_such_file.go", Contains: "anything"},
		{Kind: "source_fact", Commit: "0000000000000000000000000000000000000000",
			File: "cmd/awg/promote_evidence.go", Contains: "package main"},
	} {
		if got := verifyEvidenceRefs(context.Background(), "../..", []evidenceRef{ref}, ""); got.Verdict != evidenceUnverifiable {
			t.Errorf("a fabricated citation was accepted: %+v", got)
		}
	}
}

// The C specimen, as a rule rather than an anecdote: every reference points at
// material the same change introduced, so the claim rests only on assertions
// the claimant controls.
func TestEvidenceTheClaimantIntroducedCannotEstablishItsOwnAuthority(t *testing.T) {
	commit := headCommit(t)
	refs := []evidenceRef{{
		Kind: "source_fact", Commit: commit, File: "cmd/awg/promote_evidence.go",
		Contains: "An authority-increasing claim may not be established solely",
	}}

	// Cited by an unrelated proposer: real, independent, verified.
	if got := verifyEvidenceRefs(context.Background(), "../..", refs, ""); got.Verdict != evidenceVerified {
		t.Fatalf("%+v", got)
	}
	// Cited by the very change that introduced the file: refused, although the
	// bytes are exactly as real.
	got := verifyEvidenceRefs(context.Background(), "../..", refs, commit)
	if got.Verdict != evidenceClaimantControlled {
		t.Fatalf("self-introduced evidence was accepted: %+v", got)
	}
	if !strings.Contains(got.Detail, "claimant controls") {
		t.Fatalf("the refusal does not say why: %q", got.Detail)
	}
}

// Free text is not evidence. This is the specimens' actual lesson.
func TestFreeTextIsNotEvidence(t *testing.T) {
	if got := verifyEvidenceRefs(context.Background(), "../..", nil, ""); got.Verdict != evidenceAbsent {
		t.Fatalf("%+v", got)
	}
}

// The B specimen, encoded as a boundary rather than a hope.
//
// B's citations are genuinely real, so they verify. Its architectural
// conclusion is still wrong. Verified evidence must therefore never be read as
// an established semantic claim, and nothing in this package provides a path
// from one to the other — this test fails the moment somebody adds one.
func TestVerifiedEvidenceIsNotAnEstablishedSemanticClaim(t *testing.T) {
	commit := headCommit(t)
	// The exact shape of B: true source facts about the bus mutex.
	verified := verifyEvidenceRefs(context.Background(), "../..", []evidenceRef{{
		Kind: "source_fact", Commit: commit, File: "cmd/awg/promote_evidence.go",
		Contains: "does not follow from the bytes being present",
	}}, "")
	if verified.Verdict != evidenceVerified {
		t.Fatalf("%+v", verified)
	}
	if establishesSemanticClaim(verified) {
		t.Fatal("verified citations were treated as establishing what the code is FOR; " +
			"B cites entirely real lines and draws the wrong conclusion from them, and any mechanism " +
			"claiming to establish this class must confront B before it is believed")
	}
}
