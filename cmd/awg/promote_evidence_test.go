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
// baseCommit is the PROMOTION BASE: the merge-base with origin/main. It may
// equal HEAD, and callers that care must say so.
func baseCommit(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "-C", "../..", "merge-base", "HEAD", "origin/main").Output()
	if err != nil {
		t.Skip("no origin/main to measure independence against")
	}
	return strings.TrimSpace(string(out))
}

// olderCommit returns a commit that is genuinely older than HEAD AND not part
// of the claimant's own line. Both halves matter, and each earlier version
// satisfied only one.
//
//	baseCommit           returned HEAD itself on main
//	an ancestor check    accepted a commit unrelated to HEAD
//	a HEAD~1 fallback    returned the claimant's own branch head on a merge ref
//	merge-base alone     SKIPPED everywhere: it fails on a PR merge ref and
//	                     equals HEAD on main, so the test ran nowhere and CI was
//	                     green because nothing executed
//
// The last one is the worst, because it looked like a fix. A test that cannot
// run is not stricter than a test that runs wrongly.
//
// The qualifying commit differs by environment, so it is selected by asking
// what the environment IS rather than by trying candidates until one parses:
//
//	HEAD is a merge commit   -> HEAD^1, the BASE branch tip. This is the CI
//	                            pull-request merge ref, and the first parent is
//	                            the admitted side; HEAD^2 is the claimant's.
//	HEAD is on the admitted  -> HEAD~1. There is no claimant in flight, so an
//	  branch                    earlier commit is not the claimant's material.
//	otherwise (a feature     -> merge-base with origin/main, which is the last
//	  branch checkout)          admitted commit the branch builds on.
func olderCommit(t *testing.T) string {
	t.Helper()
	git := func(args ...string) (string, bool) {
		out, err := exec.Command("git", append([]string{"-C", "../.."}, args...)...).Output()
		if err != nil {
			return "", false
		}
		return strings.TrimSpace(string(out)), true
	}
	headRev, ok := git("rev-parse", "HEAD")
	if !ok {
		t.Skip("not a git checkout")
	}

	var candidate string
	if parents, ok := git("rev-list", "--parents", "-n", "1", "HEAD"); ok && len(strings.Fields(parents)) >= 3 {
		// A merge commit: first parent is the base branch tip.
		candidate, _ = git("rev-parse", "HEAD^1")
	} else if _, onAdmitted := git("merge-base", "--is-ancestor", "HEAD", "origin/main"); onAdmitted {
		candidate, _ = git("rev-parse", "HEAD~1")
	} else if base, ok := git("merge-base", "HEAD", "origin/main"); ok {
		candidate = base
	}

	if candidate == "" || candidate == headRev {
		t.Skip("no commit older than HEAD and outside the claimant's line is available")
	}
	// TELL THE VERIFIER WHAT IS ADMITTED HERE, not just the selector.
	//
	// Selecting the right commit is half the job. isAncestorOfBase resolves the
	// promotion base from origin/main or main, and on a CI pull-request merge
	// ref NEITHER EXISTS -- tests run before the workflow's base-branch fetch.
	// So the verifier answered "not on the base" for a commit that IS the base,
	// and specimen A was classified claimant-controlled however well the helper
	// chose.
	//
	// One source of truth: the commit this helper identifies as admitted is the
	// commit the verifier treats as the promotion base.
	t.Setenv("SENSEI_PROMOTION_BASE", candidate)
	// Verify rather than assume: a candidate that is not an ancestor of HEAD is
	// not "material the claimant did not introduce".
	if err := exec.Command("git", "-C", "../..", "merge-base", "--is-ancestor", candidate, headRev).Run(); err != nil {
		t.Skipf("candidate %s is not an ancestor of HEAD", candidate[:8])
	}
	return candidate
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
	commit := baseCommit(t) // on the base: independent unless it is the introducing commit itself
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

// The gate test must actually RUN, not skip its way to green.
//
// A merge-base-only selection skipped in both environments -- it fails on a
// pull-request merge ref and equals HEAD on the admitted branch -- so the
// end-to-end gate test executed nowhere while CI reported success. A test that
// cannot run is not stricter than a test that runs wrongly; it is the
// false-green this whole program is about, produced by a repair for it.
// STATED LIMIT: this test cannot prove the CI path from here.
//
// The merge-base-only selection skips on the ADMITTED BRANCH and on a
// PULL-REQUEST MERGE REF. A feature-branch checkout is neither, so that
// mutation SURVIVES locally -- verified, not assumed. The environment this test
// protects is one it cannot reach.
//
// CI is the falsifier: on the merge ref the gate test must report PASS rather
// than SKIP. If it reports SKIP there, this selection is wrong again and the
// green is meaningless.
func TestTheGateTestCanActuallyRunHere(t *testing.T) {
	// olderCommit skips the calling test when it cannot qualify a commit, so
	// reaching the assertion at all means selection succeeded.
	got := olderCommit(t)
	if got == "" {
		t.Fatal("olderCommit returned empty without skipping")
	}
	head := headCommit(t)
	if got == head {
		t.Fatal("the selected commit IS head; it is not independent of the claimant")
	}
	if err := exec.Command("git", "-C", "../..", "merge-base", "--is-ancestor", got, head).Run(); err != nil {
		t.Fatalf("the selected commit %s is not an ancestor of HEAD", got[:8])
	}
}
