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

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func corpusCommit(t *testing.T, dir, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "awareness", "law.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "docs/awareness/law.yaml")
	git(t, dir, "commit", "-m", "corpus: "+body)
	return git(t, dir, "rev-parse", "HEAD")
}

// A branch cut from an older main must NOT have its own unmerged corpus edit
// counted as admitted.
//
// HERMETIC, and it has to be. The repository-local version of this assertion
// passes on any branch that happens to be up to date with main -- which is most
// of them -- so it could not fail for the mutation that reintroduces the bug.
// It caught the real thing only by accident, on globulario/sensei PR #345,
// whose branch predated main.
//
// The shape that breaks it: local HEAD is trivially ordered against itself, so
// a selection that prefers "the first candidate ordered against the published
// revision" picks HEAD whenever the true admission base is unorderable. That is
// the false staleness the admitted-base preference exists to prevent, produced
// by the preference itself.
func TestAnUnmergedCorpusEditIsNotCountedAsAdmitted(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")

	base := corpusCommit(t, dir, "one")
	admitted := corpusCommit(t, dir, "two")
	// origin/main is the admitted line, and it moved on after the branch point.
	git(t, dir, "update-ref", "refs/remotes/origin/main", admitted)

	// The branch is cut from BEFORE the admitted change, as an older pull
	// request is, and carries a corpus edit of its own.
	git(t, dir, "checkout", "-q", "-b", "feature", base)
	unmerged := corpusCommit(t, dir, "three")

	a := ResolveFromGit(context.Background(), dir, unmerged)
	if a.CorpusCommit == unmerged {
		t.Fatalf("the branch's own unmerged corpus edit %s was counted as admitted; "+
			"the published graph would be reported stale during its own review", unmerged[:12])
	}
	if a.CorpusCommit != admitted {
		t.Fatalf("corpus resolved to %q, want the admitted base %q", a.CorpusCommit, admitted)
	}
}

// The solo checkout is the case the local-HEAD fallback legitimately serves: no
// remote, no admission branch, so HEAD is the only corpus there is. Removing
// the fallback entirely would break this, which is why it is kept -- but only
// when nothing else resolved, never as a fall-through from an unorderable base.
func TestASoloCheckoutStillResolvesItsOwnCorpus(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	only := corpusCommit(t, dir, "one")

	a := ResolveFromGit(context.Background(), dir, only)
	if a.CorpusCommit != only {
		t.Fatalf("a solo checkout resolved %q, want %q", a.CorpusCommit, only)
	}
}
