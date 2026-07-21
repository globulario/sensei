// SPDX-License-Identifier: AGPL-3.0-only

package resultpipeline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// initRepo builds a small committed repository with a plain file, an executable
// script, and a symlink, and returns the root and HEAD commit.
func initRepo(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-q")
	git(t, root, "config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(root, "readme.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("readme.txt", filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "init")
	head := git(t, root, "rev-parse", "HEAD")
	return root, head
}

func TestMaterializeTreeExactAndIsolated(t *testing.T) {
	root, head := initRepo(t)
	tree := git(t, root, "rev-parse", "HEAD^{tree}")

	// A dirty, untracked, live-worktree file that must NOT leak into the result.
	if err := os.WriteFile(filepath.Join(root, "untracked.tmp"), []byte("leak"), 0o644); err != nil {
		t.Fatal(err)
	}
	realStatusBefore := git(t, root, "status", "--porcelain")

	dest, cleanup, err := materializeTree(context.Background(), root, tree)
	if err != nil {
		t.Fatalf("materializeTree: %v", err)
	}
	defer cleanup()

	// Exact tracked content.
	if b, _ := os.ReadFile(filepath.Join(dest, "readme.txt")); string(b) != "hello\n" {
		t.Fatalf("readme.txt content = %q", b)
	}
	// Executable bit preserved.
	fi, err := os.Lstat(filepath.Join(dest, "bin", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o111 == 0 {
		t.Fatalf("run.sh lost its executable bit: %v", fi.Mode())
	}
	// Symlink preserved as a symlink, not followed.
	li, err := os.Lstat(filepath.Join(dest, "link.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if li.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link.txt is not a symlink: %v", li.Mode())
	}
	// No .git and no untracked leak.
	if _, err := os.Stat(filepath.Join(dest, ".git")); !os.IsNotExist(err) {
		t.Fatal("materialized root must not contain .git")
	}
	if _, err := os.Stat(filepath.Join(dest, "untracked.tmp")); !os.IsNotExist(err) {
		t.Fatal("untracked live-worktree file leaked into the result root")
	}

	// The real repository index/working tree/HEAD are untouched.
	if got := git(t, root, "status", "--porcelain"); got != realStatusBefore {
		t.Fatalf("real worktree status changed: %q -> %q", realStatusBefore, got)
	}
	if got := git(t, root, "rev-parse", "HEAD"); got != head {
		t.Fatalf("real HEAD moved: %s -> %s", head, got)
	}

	// Cleanup removes the root.
	cleanup()
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("cleanup did not remove the materialized root")
	}
}

func TestMaterializeTreeRelocationIndependent(t *testing.T) {
	root, _ := initRepo(t)
	tree := git(t, root, "rev-parse", "HEAD^{tree}")

	a, ca, err := materializeTree(context.Background(), root, tree)
	if err != nil {
		t.Fatal(err)
	}
	defer ca()
	b, cb, err := materializeTree(context.Background(), root, tree)
	if err != nil {
		t.Fatal(err)
	}
	defer cb()

	if a == b {
		t.Fatal("expected two distinct materialization roots")
	}
	for _, rel := range []string{"readme.txt", "bin/run.sh"} {
		ba, _ := os.ReadFile(filepath.Join(a, rel))
		bb, _ := os.ReadFile(filepath.Join(b, rel))
		if string(ba) != string(bb) {
			t.Fatalf("%s differs across relocated materializations", rel)
		}
	}
}
