// SPDX-License-Identifier: AGPL-3.0-only

package reachability

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The resolver, against this repository, for the case that could never say yes.
//
// A build from a code-only or merge commit -- the common case -- has the corpus
// revision as an ANCESTOR, and the resolver tested only the reverse relation.
// So an ordinary up-to-date build reported Unknown, and a check that cannot say
// yes is not a check.
//
// An Assess-level fixture cannot catch this: it hits the measured-zero branch
// and passes either way. The seam is the ancestry test, so this exercises it.
func TestAGraphBuiltFromHeadIsCurrentAgainstThisRepository(t *testing.T) {
	head, err := exec.Command("git", "-C", "../..", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Skip("not a git checkout")
	}
	a := ResolveFromGit(context.Background(), "../..", strings.TrimSpace(string(head)))
	if a.State != StateCurrent {
		// Say what the environment actually was. This failed in CI and the
		// message could not distinguish an unfetched base from an unrelated
		// history, which cost a full diagnostic cycle.
		refs, _ := exec.Command("git", "-C", "../..", "for-each-ref", "--format=%(refname)",
			"refs/remotes/", "refs/heads/").Output()
		t.Fatalf("a graph built from HEAD reported %q (%s); HEAD contains every authored corpus change.\n"+
			"head=%s corpus=%s published=%s\nrefs present:\n%s",
			a.State, a.Detail, strings.TrimSpace(string(head)), a.CorpusCommit, a.PublishedCommit, refs)
	}
}

// The corpus revision must come from the ADMITTED base, not local HEAD.
//
// Resolving from HEAD counts a reviewer's own unmerged docs/awareness edit as
// "admitted", so the published graph is reported stale during the exact
// pre-merge review where Sensei is being used -- a false staleness warning
// manufactured by the branch under review.
//
// This branch carries unmerged corpus edits, which is what makes the assertion
// able to fail: resolving from HEAD would return a commit origin/main does not
// contain.
func TestTheCorpusRevisionComesFromTheAdmittedBase(t *testing.T) {
	if _, err := exec.Command("git", "-C", "../..", "rev-parse", "origin/main").Output(); err != nil {
		t.Skip("no origin/main to measure admission against")
	}
	head, err := exec.Command("git", "-C", "../..", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Skip("not a git checkout")
	}
	a := ResolveFromGit(context.Background(), "../..", strings.TrimSpace(string(head)))
	if a.CorpusCommit == "" {
		t.Skip("no corpus revision resolved")
	}
	// The resolved corpus revision must be contained in the admitted branch.
	if err := exec.Command("git", "-C", "../..", "merge-base", "--is-ancestor",
		a.CorpusCommit, "origin/main").Run(); err != nil {
		t.Fatalf("the corpus revision %s is not contained in origin/main: an unmerged edit was "+
			"counted as admitted, which reports the published graph stale during its own review",
			a.CorpusCommit[:12])
	}
}

// A root that does not hold the corpus cannot answer the question.
//
// GraphBuildCommit is the AWARENESS-GRAPH repository's SHA; measuring it
// against a governed checkout that merely happens to be a git repository would
// compare unrelated histories.
//
// HONEST NOTE ON WHAT PROVES THIS. The behaviour is enforced by every lookup
// being scoped to CorpusPaths, so a root without them resolves nothing. An
// explicit ownership guard was added and removed again: no mutation could
// distinguish it from the scoping that was already there, which made it
// duplication rather than defence. This test pins the BEHAVIOUR, and no
// mutation isolates it to one line -- that is stated rather than implied by a
// tidy mutation table.
func TestARootWithoutTheCorpusAnswersUnknown(t *testing.T) {
	// A REAL GIT REPOSITORY that simply holds no authored corpus. A bare
	// TempDir is not this test: git fails there anyway, so the assertion passes
	// without ever reaching the corpus-ownership check -- which is how the
	// first version of this test let its mutation survive.
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "seed"},
	} {
		if err := exec.Command("git", append([]string{"-C", dir}, args...)...).Run(); err != nil {
			t.Skipf("cannot build a git fixture: %v", err)
		}
	}
	head, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Skip("fixture has no HEAD")
	}
	// Cite the fixture's OWN commit, so ancestry would resolve happily and the
	// only thing that can produce Unknown is the missing corpus.
	a := ResolveFromGit(context.Background(), dir, strings.TrimSpace(string(head)))
	if a.State != StateUnknown {
		t.Fatalf("state=%q for a git repository that holds no authored corpus", a.State)
	}
	if a.Reachable() {
		t.Error("a root that cannot answer the question reported knowledge reachable")
	}
}

// A checkout whose canonical remote is not named `origin`.
//
// The lookup hardcoded refs/remotes/origin/HEAD and then origin/main, so a
// repository with an `upstream` remote and no `origin` failed both and resolved
// the corpus from LOCAL HEAD — counting the feature branch's unmerged awareness
// edits as admitted, which is the exact false staleness the admitted-base
// preference exists to prevent.
//
// Hardcoding a remote name is the same mistake as hardcoding a branch name, one
// level out. This builds the environment rather than arguing about it: the
// origin-only mutation cannot be falsified in a repository that has an origin.
func TestTheAdmittedBaseIsFoundOnARemoteNotNamedOrigin(t *testing.T) {
	if err := exec.Command("git", "--version").Run(); err != nil {
		t.Skipf("git is not installed: %v", err)
	}
	admitted := t.TempDir()
	work := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			// A fixture step that fails once `git --version` has succeeded is a
			// broken fixture, not a missing environment. Skipping here would
			// hide a test that never ran behind a passing package.
			t.Fatalf("fixture step `git %s` failed: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	// An admitted repository with one corpus commit.
	run(admitted, "init", "-q", "-b", "main")
	if err := os.MkdirAll(filepath.Join(admitted, "docs", "awareness"), 0o755); err != nil {
		t.Fatalf("cannot build fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(admitted, "docs/awareness/invariants.yaml"), []byte("invariants: []\n"), 0o644); err != nil {
		t.Fatalf("cannot build fixture: %v", err)
	}
	run(admitted, "add", "-A")
	run(admitted, "commit", "-q", "-m", "admitted corpus")

	// A working checkout whose ONLY remote is named upstream.
	run(work, "init", "-q", "-b", "feature")
	run(work, "remote", "add", "upstream", admitted)
	run(work, "fetch", "-q", "upstream")
	run(work, "reset", "-q", "--hard", "upstream/main")
	run(work, "remote", "set-head", "upstream", "main")

	// An UNMERGED corpus edit on the feature branch: resolving from local HEAD
	// would count it as admitted, which is the defect.
	if err := os.WriteFile(filepath.Join(work, "docs/awareness/invariants.yaml"), []byte("invariants: [ {id: local} ]\n"), 0o644); err != nil {
		t.Fatalf("cannot write fixture edit: %v", err)
	}
	run(work, "add", "-A")
	run(work, "commit", "-q", "-m", "unmerged local corpus edit")

	upstreamTip, err := exec.Command("git", "-C", work, "rev-parse", "upstream/main").Output()
	if err != nil {
		t.Fatalf("fixture has no upstream ref: %v", err)
	}
	head, err := exec.Command("git", "-C", work, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("fixture has no head: %v", err)
	}

	a := ResolveFromGit(context.Background(), work, strings.TrimSpace(string(head)))
	if a.CorpusCommit == strings.TrimSpace(string(head)) {
		t.Fatal("the corpus resolved from the feature branch's own HEAD; an unmerged awareness " +
			"edit was counted as admitted because the remote is not named origin")
	}
	if a.CorpusCommit != strings.TrimSpace(string(upstreamTip)) {
		t.Errorf("corpus resolved to %q, want the upstream default head %q",
			a.CorpusCommit, strings.TrimSpace(string(upstreamTip)))
	}
}

// A fork checkout: `origin` is the fork, `upstream` is the canonical
// repository, and `git remote` lists origin first.
//
// Taking the first remote default-head that returns any commit made the FORK's
// line the corpus, and the published revision need not be ordered against it —
// reported as "neither contains the other", an honest Unknown about a question
// asked of the wrong branch. The base must be chosen by whether it can be
// ordered against the published revision, not by the order git happens to list
// remotes in.
func TestTheCorpusBaseIsChosenByOrderabilityNotByRemoteListingOrder(t *testing.T) {
	if err := exec.Command("git", "--version").Run(); err != nil {
		t.Skipf("git is not installed: %v", err)
	}
	canonical, fork, work := t.TempDir(), t.TempDir(), t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture step `git %s` failed: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	corpus := func(dir, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, "docs", "awareness"), 0o755); err != nil {
			t.Fatalf("cannot build fixture: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "docs/awareness/invariants.yaml"), []byte(body), 0o644); err != nil {
			t.Fatalf("cannot write fixture: %v", err)
		}
	}

	// The canonical repository, with the admitted corpus.
	run(canonical, "init", "-q", "-b", "main")
	corpus(canonical, "invariants: [ {id: canonical} ]\n")
	run(canonical, "add", "-A")
	run(canonical, "commit", "-q", "-m", "canonical corpus")

	// A fork with an UNRELATED history that also carries a corpus commit.
	run(fork, "init", "-q", "-b", "main")
	corpus(fork, "invariants: [ {id: fork} ]\n")
	run(fork, "add", "-A")
	run(fork, "commit", "-q", "-m", "fork corpus")

	// The working checkout descends from CANONICAL, but its `origin` is the
	// fork — the ordinary shape of a contributor's clone.
	run(work, "init", "-q", "-b", "feature")
	run(work, "remote", "add", "origin", fork)
	run(work, "remote", "add", "upstream", canonical)
	run(work, "fetch", "-q", "origin")
	run(work, "fetch", "-q", "upstream")
	run(work, "reset", "-q", "--hard", "upstream/main")
	run(work, "remote", "set-head", "origin", "main")
	run(work, "remote", "set-head", "upstream", "main")

	head, err := exec.Command("git", "-C", work, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("fixture has no head: %v", err)
	}
	canonicalTip, err := exec.Command("git", "-C", work, "rev-parse", "upstream/main").Output()
	if err != nil {
		t.Fatalf("fixture has no upstream ref: %v", err)
	}
	// The fixture must actually be the case under test: origin listed first.
	remotes, err := exec.Command("git", "-C", work, "remote").Output()
	if err != nil || !strings.HasPrefix(strings.TrimSpace(string(remotes)), "origin") {
		t.Fatalf("fixture does not list origin first (%q); the case under test is not constructed", remotes)
	}

	a := ResolveFromGit(context.Background(), work, strings.TrimSpace(string(head)))
	if a.State != StateCurrent {
		t.Fatalf("a checkout whose canonical remote is `upstream` reported %q (%s); the fork's "+
			"unrelated line was taken as the corpus because git lists origin first",
			a.State, a.Detail)
	}
	if a.CorpusCommit != strings.TrimSpace(string(canonicalTip)) {
		t.Errorf("corpus resolved to %q, want the canonical tip %q", a.CorpusCommit, strings.TrimSpace(string(canonicalTip)))
	}
}
