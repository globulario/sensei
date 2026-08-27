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
// baseCommit is a commit already on the promotion base -- independent of any
// claimant by construction. Tests that expect EVIDENCE_VERIFIED cite this;
// HEAD of a feature worktree is the claimant's own line and is not.
func baseCommit(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "-C", "../..", "merge-base", "HEAD", "origin/main").Output()
	if err != nil {
		t.Skip("no origin/main to measure independence against")
	}
	return strings.TrimSpace(string(out))
}

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
	commit := baseCommit(t)
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
// conclusion is still wrong. Verified evidence must therefore never become an
// established semantic claim — and that is now guaranteed by there being no
// constructor for one outside a successful derivation, rather than by a
// function returning false.
//
// This test pins what remains true here: verification produces a verdict about
// CITATIONS, and this package has no vocabulary for establishing a claim.
func TestVerifiedEvidenceIsAVerdictAboutCitationsOnly(t *testing.T) {
	commit := baseCommit(t)
	verified := verifyEvidenceRefs(context.Background(), "../..", []evidenceRef{{
		Kind: "source_fact", Commit: commit, File: "cmd/awg/promote_evidence.go",
		Contains: "the only function returning one is Derive",
	}}, "")
	if verified.Verdict != evidenceVerified {
		t.Fatalf("%+v", verified)
	}
	// Every verdict this package can produce is about whether the citations are
	// real. None of them says the claim is true, and none of them is named as
	// though it did.
	for _, v := range []evidenceVerdict{
		evidenceVerified, evidenceUnverifiable, evidenceClaimantControlled, evidenceAbsent,
	} {
		if strings.Contains(string(v), "ESTABLISHED") {
			t.Fatalf("verdict %q reads as establishment; establishment lives in "+
				"golang/architecture/derive and requires a derivation receipt", v)
		}
	}
}

// The hole review found: an uncommitted candidate has no introducing commit,
// and an empty comparison made every real citation look independent. Now a
// citation is independent only if it is already on the promotion base; a
// commit on the claimant's own line, cited with no introducing commit known,
// is still the claimant's material.
func TestACitationOnTheClaimantsOwnLineIsNotIndependent(t *testing.T) {
	head := headCommit(t)
	if head == baseCommit(t) {
		t.Skip("HEAD is on the promotion base; nothing branch-only to cite")
	}
	got := verifyEvidenceRefs(context.Background(), "../..", []evidenceRef{{
		Kind: "source_fact", Commit: head, File: "go.mod", Contains: "module github.com/globulario/sensei"}}, "")
	if got.Verdict != evidenceClaimantControlled {
		t.Fatalf("a branch-only citation with no introducing commit was accepted as independent: %+v", got)
	}
}
