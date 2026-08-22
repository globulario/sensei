// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/prospective"
)

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixtureWorld builds a repository with a root commit, an add, a modify, and a
// merge — one of each shape the enumeration rule has to account for.
func fixtureWorld(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	dir := t.TempDir()
	mustGit(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "a.go", "package a\n")
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "root: add a")

	write(t, dir, "b.go", "package b\n")
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "add b")

	write(t, dir, "a.go", "package a // changed\n")
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "modify a")

	mustGit(t, dir, "checkout", "-q", "-b", "side", "HEAD~1")
	write(t, dir, "c.go", "package c\n")
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "add c")
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "merge", "-q", "--no-ff", "-m", "merge side", "side")
	return dir
}

// The population is enumerated from git, not from a supplied list. Whoever
// gets to decide what enters the denominator decides the score, so the
// enumeration has to be mechanical and complete over the pinned revision.
func TestEnumerationIsCompleteDeterministicAndCountsItsExclusions(t *testing.T) {
	dir := fixtureWorld(t)
	ctx := context.Background()
	head, err := ResolveRevision(ctx, dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	changes, exclusions, err := EnumerateChanges(ctx, dir, head)
	if err != nil {
		t.Fatal(err)
	}
	// Four non-merge commits are reachable; the merge and the root are
	// excluded, the root because it has no pre-authoring state at all.
	if len(changes) != 3 {
		t.Fatalf("expected 3 enumerated changes, got %d", len(changes))
	}
	if len(exclusions) != 2 {
		t.Fatalf("expected the merge and the root to be excluded, got %d exclusions", len(exclusions))
	}
	reasons := map[string]int{}
	for _, e := range exclusions {
		reasons[e.Reason]++
		if e.Detail == "" || e.ChangeID == "" {
			t.Fatalf("an exclusion carries no reason detail or identity: %#v", e)
		}
	}
	if reasons[prospective.ExcludedNoSingleBase] != 2 {
		t.Fatalf("exclusions did not name the rule that produced them: %#v", reasons)
	}

	again, _, err := EnumerateChanges(ctx, dir, head)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != len(changes) {
		t.Fatal("two enumerations of the same world disagree")
	}
	for i := range changes {
		if changes[i].ID != again[i].ID || changes[i].ContentDigestSHA256 != again[i].ContentDigestSHA256 {
			t.Fatal("enumeration is not deterministic, so no digest over it is reproducible")
		}
	}

	// A change must state what the world looked like before it, or prospective
	// recall is not a claim that can be made about it.
	for _, c := range changes {
		if c.BaseRevision == "" || c.BaseTreeDigestSHA256 == "" || c.ContentDigestSHA256 == "" || len(c.Paths) == 0 {
			t.Fatalf("change %s cannot state its pre-authoring state: %#v", c.ID, c)
		}
	}
}

// A new file is a new seam whatever git calls the operation. Existence is read
// from the status letter for the pair, never from the tree as it stands today.
func TestANewPathIsRecordedAsNewAtItsOwnMoment(t *testing.T) {
	dir := fixtureWorld(t)
	ctx := context.Background()
	head, _ := ResolveRevision(ctx, dir, "HEAD")
	changes, _, err := EnumerateChanges(ctx, dir, head)
	if err != nil {
		t.Fatal(err)
	}
	var sawNew, sawExisting bool
	for _, c := range changes {
		for _, p := range c.Paths {
			if p.Path == "b.go" && !p.ExistedBefore {
				sawNew = true
			}
			if p.Path == "a.go" && p.ExistedBefore {
				sawExisting = true
			}
		}
	}
	if !sawNew {
		t.Fatal("the commit that created b.go did not record it as new, so a new seam would be classified as an existing file")
	}
	if !sawExisting {
		t.Fatal("the commit that modified a.go did not record it as pre-existing")
	}
}

// The content lookup returns the exact diff the inventory froze, so the
// package a human reads is the change the sample names.
func TestContentLookupReturnsTheFrozenDiff(t *testing.T) {
	dir := fixtureWorld(t)
	ctx := context.Background()
	head, _ := ResolveRevision(ctx, dir, "HEAD")
	changes, _, err := EnumerateChanges(ctx, dir, head)
	if err != nil {
		t.Fatal(err)
	}
	lookup := ContentLookup(ctx, dir, changes)
	for _, c := range changes {
		body, err := lookup(c.ID)
		if err != nil {
			t.Fatalf("%s: %v", c.ID, err)
		}
		got, err := prospective.DigestOf(body)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.ContentDigestSHA256 {
			t.Fatalf("%s: the lookup returned contents the inventory did not freeze", c.ID)
		}
	}
	if _, err := lookup("change:nosuch"); err == nil {
		t.Fatal("the lookup answered for a change outside the frozen inventory")
	}
}
