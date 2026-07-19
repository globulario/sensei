// SPDX-License-Identifier: Apache-2.0

package completion

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Every identity-boundary bypass attempt is rejected as a TYPED identity error — so it
// can only ever block (or exit as a config error) under enforcement, never degrade.
func TestIdentityBoundary_AdversarialAreAllIdentityErrors(t *testing.T) {
	repo := t.TempDir()
	inside := filepath.Join(repo, ".sensei", "tasks", "task.x")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir() // a sibling repo/world

	cases := []struct {
		name       string
		root, task string
	}{
		{"absent_both", "", ""},
		{"absent_task", repo, ""},
		{"absent_root", "", inside},
		{"whitespace_task", repo, "   "},
		{"whitespace_root", "   ", inside},
		{"out_of_scope_sibling", repo, outside},
		{"relative_traversal_escape", repo, filepath.Join(repo, "..", filepath.Base(outside))},
		{"parent_of_repo", repo, filepath.Dir(repo)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateRepositoryTaskBinding(c.root, c.task)
			if err == nil {
				t.Fatalf("%s must be rejected", c.name)
			}
			if !IsProjectionIdentityError(err) {
				t.Fatalf("%s must be a TYPED identity error, got %T: %v", c.name, err, err)
			}
			// And it must never carry positive runtime evidence.
			if IsProjectionRuntimeError(err) {
				t.Fatalf("%s must never be runtime", c.name)
			}
		})
	}
}

// A symlinked task directory cannot smuggle in another repository's world: the
// symlink-resolved path is out of scope → a typed identity error.
func TestIdentityBoundary_SymlinkEscapeIsIdentityError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink escape requires POSIX symlink semantics")
	}
	repo := t.TempDir()
	tasks := filepath.Join(repo, ".sensei", "tasks")
	if err := os.MkdirAll(tasks, 0o755); err != nil {
		t.Fatal(err)
	}
	// A real world outside the repo, and a symlink inside the repo pointing at it.
	outside := t.TempDir()
	link := filepath.Join(tasks, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	err := validateRepositoryTaskBinding(repo, link)
	if err == nil || !IsProjectionIdentityError(err) {
		t.Fatalf("a symlink escaping the repo must be a typed identity error, got %v", err)
	}
}
