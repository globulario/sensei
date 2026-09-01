// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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

	candidate := selectAdmittedBase(git)

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
	// THIS GUARD MUST FAIL, NOT SKIP.
	//
	// It used to call olderCommit(t), which SKIPS its caller when no commit
	// qualifies -- so the one test written to detect "the gate could not run
	// here" vanished under exactly the condition it existed to report.
	//
	// That was invisible from CI, and I twice offered the invisibility as
	// proof: `go test` WITHOUT -v prints nothing at all for a skipped test,
	// just `ok <pkg>`. Counting "--- SKIP" lines in a CI log therefore returns
	// zero whether the test ran or skipped. The count could not have come out
	// any other way, so it was never evidence.
	//
	// The repair is not to add -v and read logs harder. It is to make the
	// unrunnable case FAIL, so that no one has to notice a silence.
	git := func(args ...string) (string, bool) {
		out, err := exec.Command("git", append([]string{"-C", "../.."}, args...)...).Output()
		if err != nil {
			return "", false
		}
		return strings.TrimSpace(string(out)), true
	}

	// Skip ONLY for a named, detected reason -- never as a fallback for
	// "something didn't work".
	head, ok := git("rev-parse", "HEAD")
	if !ok {
		t.Skip("not a git checkout: no commit history to select a base from")
	}
	if shallow, ok := git("rev-parse", "--is-shallow-repository"); ok && shallow == "true" {
		t.Skip("shallow clone: history is truncated, so an older commit need not be present")
	}
	if _, hasParent := git("rev-parse", "--verify", "-q", "HEAD^"); !hasParent {
		if _, hasAdmitted := admittedRef(git); !hasAdmitted {
			t.Skip("root commit with no admitted ref: nothing older than HEAD exists")
		}
	}

	// Every remaining environment MUST yield a base. Silence here is a defect.
	got := selectAdmittedBase(git)
	if got == "" {
		refName, hasRef := admittedRef(git)
		t.Fatalf("no promotion base could be selected in a repository that has history "+
			"(admitted ref %q present=%v); the promotion gate would have skipped and CI "+
			"would be green having proven nothing", refName, hasRef)
	}
	if got == head {
		t.Fatal("the selected commit IS head; it is not independent of the claimant")
	}
	if err := exec.Command("git", "-C", "../..", "merge-base", "--is-ancestor", got, head).Run(); err != nil {
		t.Fatalf("the selected commit %s is not an ancestor of HEAD", got[:8])
	}
}

// admittedRef returns the admitted branch ref that is actually resolvable here,
// so callers use the ref that was found rather than one they assume exists.
// Its ABSENCE is the signature of a CI pull-request merge ref, where the
// workflow runs tests before fetching the base branch.
func admittedRef(git func(...string) (string, bool)) (string, bool) {
	for _, ref := range []string{"origin/main", "main"} {
		if _, ok := git("rev-parse", "--verify", "-q", ref+"^{commit}"); ok {
			return ref, true
		}
	}
	return "", false
}

// selectAdmittedBase picks the commit this environment can treat as admitted.
//
// It is separated from olderCommit and takes its git accessor as a parameter
// for one reason: the ordering below CANNOT BE FALSIFIED IN THIS CHECKOUT.
// Restoring the wrong order survives every test here, because HEAD is not a
// merge commit locally — so the environment that exposes the defect has to be
// BUILT rather than argued about. See
// TestTheAdmittedBaseIsNotTheClaimantsOwnPreMergeTip.
//
// THE ADMITTED REF WINS WHENEVER IT EXISTS. HEAD^1 is only correct when there
// is no admitted ref to consult.
//
// An earlier order tried HEAD^1 first whenever HEAD was a merge commit, on the
// assumption that a merge commit means a CI merge ref. It does not: a
// developer's feature branch that has merged the admitted branch also ends in a
// merge, and there HEAD^1 is the PRE-MERGE FEATURE TIP — a claimant-controlled
// commit, which would then have been handed to the verifier as the promotion
// base and made specimen A pass for the worst possible reason.
//
// Absence of the admitted ref is what identifies the CI environment, and that
// is exactly the condition under which HEAD^1 is the admitted side.
func selectAdmittedBase(git func(...string) (string, bool)) string {
	// THE REF THAT WAS FOUND IS THE REF THAT IS USED. Asking whether SOME
	// admitted ref exists and then hardcoding origin/main below is the same
	// class of split the whole gate is about: a checkout holding a local `main`
	// but no `origin/main` would take the admitted branch, fail the merge-base
	// against a ref it never found, return "", and SKIP -- silently undoing the
	// property that this test runs in CI at all.
	if ref, ok := admittedRef(git); ok {
		if _, onAdmitted := git("merge-base", "--is-ancestor", "HEAD", ref); onAdmitted {
			// On the admitted branch: no claimant is in flight.
			c, _ := git("rev-parse", "HEAD~1")
			return c
		}
		if base, ok := git("merge-base", "HEAD", ref); ok {
			return base
		}
		return ""
	}
	// No admitted ref at all: a CI pull-request merge ref, where the first
	// parent is the admitted side and HEAD^2 is the claimant's.
	if parents, ok := git("rev-list", "--parents", "-n", "1", "HEAD"); ok && len(strings.Fields(parents)) >= 3 {
		c, _ := git("rev-parse", "HEAD^1")
		return c
	}
	return ""
}

// A developer's feature branch that ends in an ordinary merge of the admitted
// branch. HEAD is a merge commit here, exactly as on a CI pull-request merge
// ref — but HEAD^1 is the PRE-MERGE FEATURE TIP, a commit the claimant wrote.
// Handing that to the verifier as the promotion base would accept specimen A as
// independently verified on the claimant's own material.
//
// The two environments are told apart by whether an admitted ref exists at all,
// so this fixture gives the working repository one.
func TestTheAdmittedBaseIsNotTheClaimantsOwnPreMergeTip(t *testing.T) {
	admitted, work := t.TempDir(), t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git fixture unavailable (%v): %s", err, out)
		}
	}
	write := func(dir, name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Skip("cannot write fixture")
		}
	}

	run(admitted, "init", "-q", "-b", "main")
	write(admitted, "base.txt", "admitted\n")
	run(admitted, "add", "-A")
	run(admitted, "commit", "-q", "-m", "admitted root")

	run(work, "clone", "-q", admitted, ".")
	run(work, "checkout", "-q", "-b", "feature")
	write(work, "claim.txt", "the claimant's own material\n")
	run(work, "add", "-A")
	run(work, "commit", "-q", "-m", "claimant work")
	preMergeTip, err := exec.Command("git", "-C", work, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Skip("no feature tip")
	}

	// The admitted branch moves, and the developer merges it in — an ordinary
	// merge, leaving HEAD a merge commit whose FIRST parent is the claimant's.
	write(admitted, "base.txt", "admitted, moved\n")
	run(admitted, "add", "-A")
	run(admitted, "commit", "-q", "-m", "admitted advances")
	run(work, "fetch", "-q", "origin")
	run(work, "merge", "-q", "--no-ff", "-m", "merge admitted into feature", "origin/main")

	parents, err := exec.Command("git", "-C", work, "rev-list", "--parents", "-n", "1", "HEAD").Output()
	if err != nil || len(strings.Fields(string(parents))) < 3 {
		t.Skip("fixture HEAD is not a merge commit")
	}

	git := func(args ...string) (string, bool) {
		out, err := exec.Command("git", append([]string{"-C", work}, args...)...).Output()
		if err != nil {
			return "", false
		}
		return strings.TrimSpace(string(out)), true
	}
	got := selectAdmittedBase(git)
	if got == strings.TrimSpace(string(preMergeTip)) {
		t.Fatal("the promotion base resolved to the claimant's own pre-merge feature tip; " +
			"specimen A would be accepted as independently verified on the claimant's material")
	}
	if got == "" {
		t.Fatal("no admitted base selected in a repository that has one")
	}
	if _, ok := git("merge-base", "--is-ancestor", got, "origin/main"); !ok {
		t.Errorf("selected base %q is not an ancestor of the admitted branch", got)
	}
}

// A checkout holding a local `main` but no `origin/main` — an unfetched clone,
// or a CI job that created the branch locally. Asking whether SOME admitted ref
// exists and then resolving against a hardcoded `origin/main` made this return
// no base at all, so the gate test SKIPPED: green CI proving nothing, which is
// the specific failure this whole helper was rewritten to end.
func TestAnAdmittedRefUnderADifferentNameStillYieldsABase(t *testing.T) {
	work := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", work}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git fixture unavailable (%v): %s", err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "base.txt"), []byte("admitted\n"), 0o644); err != nil {
		t.Skip("cannot write fixture")
	}
	run("add", "-A")
	run("commit", "-q", "-m", "admitted root")
	run("checkout", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(work, "claim.txt"), []byte("claim\n"), 0o644); err != nil {
		t.Skip("cannot write fixture")
	}
	run("add", "-A")
	run("commit", "-q", "-m", "claimant work")

	git := func(args ...string) (string, bool) {
		out, err := exec.Command("git", append([]string{"-C", work}, args...)...).Output()
		if err != nil {
			return "", false
		}
		return strings.TrimSpace(string(out)), true
	}
	if _, ok := git("rev-parse", "--verify", "-q", "origin/main^{commit}"); ok {
		t.Skip("fixture unexpectedly has origin/main")
	}
	got := selectAdmittedBase(git)
	if got == "" {
		t.Fatal("no base selected in a repository that has an admitted ref under the name `main`; " +
			"the gate test would skip and CI would be green having proven nothing")
	}
	if _, ok := git("merge-base", "--is-ancestor", got, "main"); !ok {
		t.Errorf("selected base %q is not an ancestor of the admitted branch", got)
	}
}

// The CI pull-request merge ref itself: a detached merge commit with NO
// admitted ref present, because the workflow runs tests before fetching the
// base branch.
//
// Proving that this test RUNS in CI is not the same as proving it picks the
// right parent there. Swapping HEAD^1 for HEAD^2 hands the CLAIMANT'S OWN TIP
// to the verifier as the promotion base — specimen A accepted on the
// claimant's material — and every other fixture here survives that mutation,
// because none of them reach this branch.
func TestOnACIMergeRefTheAdmittedSideIsTheFirstParent(t *testing.T) {
	work := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", work}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git fixture unavailable (%v): %s", err, out)
		}
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(work, name), []byte(body), 0o644); err != nil {
			t.Skip("cannot write fixture")
		}
	}
	rev := func(ref string) string {
		t.Helper()
		out, err := exec.Command("git", "-C", work, "rev-parse", ref).Output()
		if err != nil {
			t.Skipf("cannot resolve %s", ref)
		}
		return strings.TrimSpace(string(out))
	}

	run("init", "-q", "-b", "main")
	write("base.txt", "admitted\n")
	run("add", "-A")
	run("commit", "-q", "-m", "admitted root")
	run("checkout", "-q", "-b", "feature")
	write("claim.txt", "the claimant's own material\n")
	run("add", "-A")
	run("commit", "-q", "-m", "claimant work")
	claimantTip := rev("HEAD")

	run("checkout", "-q", "main")
	write("base.txt", "admitted, moved\n")
	run("add", "-A")
	run("commit", "-q", "-m", "admitted advances")
	admittedTip := rev("HEAD")

	// GitHub builds the merge ref with the base branch checked out, so the
	// first parent is the admitted side and the second is the claimant's.
	run("merge", "-q", "--no-ff", "-m", "Merge pull request", "feature")
	run("checkout", "-q", "--detach", "HEAD")
	// Tests run before the base-branch fetch: no admitted ref is present.
	run("branch", "-q", "-D", "main")
	run("branch", "-q", "-D", "feature")

	git := func(args ...string) (string, bool) {
		out, err := exec.Command("git", append([]string{"-C", work}, args...)...).Output()
		if err != nil {
			return "", false
		}
		return strings.TrimSpace(string(out)), true
	}
	if _, ok := admittedRef(git); ok {
		t.Skip("fixture unexpectedly retains an admitted ref")
	}
	got := selectAdmittedBase(git)
	if got == claimantTip {
		t.Fatal("on a CI merge ref the promotion base resolved to the CLAIMANT'S tip; " +
			"specimen A would be accepted as independently verified on the claimant's own material")
	}
	if got != admittedTip {
		t.Errorf("promotion base = %q, want the admitted side %q", got, admittedTip)
	}
}
