// SPDX-License-Identifier: AGPL-3.0-only

package reachability

import (
	"context"
	"os/exec"
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
		t.Fatalf("a graph built from HEAD reported %q (%s); HEAD contains every authored corpus change",
			a.State, a.Detail)
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
