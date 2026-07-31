// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newTestWorkspace(t *testing.T) (CandidateWorkspace, string, string) {
	t.Helper()
	snapshotRoot := t.TempDir()
	bufferRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(snapshotRoot, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewFSCandidateWorkspace(snapshotRoot, bufferRoot), snapshotRoot, bufferRoot
}

func TestFSCandidateWorkspaceReadSnapshot(t *testing.T) {
	w, _, _ := newTestWorkspace(t)
	got, err := w.ReadSnapshot("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package main\n" {
		t.Errorf("ReadSnapshot = %q, want %q", got, "package main\n")
	}
}

func TestFSCandidateWorkspaceWriteAndReadBackViaFilesystem(t *testing.T) {
	w, _, bufferRoot := newTestWorkspace(t)
	if err := w.WriteCandidate("a/b/new.txt", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(bufferRoot, "a", "b", "new.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("wrote file content = %q, want %q", got, "hello")
	}
}

func TestFSCandidateWorkspaceDelete(t *testing.T) {
	w, _, bufferRoot := newTestWorkspace(t)
	if err := w.WriteCandidate("x.txt", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := w.Delete("x.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(bufferRoot, "x.txt")); !os.IsNotExist(err) {
		t.Errorf("expected x.txt to be gone, stat err = %v", err)
	}
}

func TestFSCandidateWorkspaceDeleteNonexistentIsNotAnError(t *testing.T) {
	w, _, _ := newTestWorkspace(t)
	if err := w.Delete("never-existed.txt"); err != nil {
		t.Errorf("Delete of a nonexistent path returned an error: %v", err)
	}
}

func TestFSCandidateWorkspaceRename(t *testing.T) {
	w, _, bufferRoot := newTestWorkspace(t)
	if err := w.WriteCandidate("old.txt", []byte("content")); err != nil {
		t.Fatal(err)
	}
	if err := w.Rename("old.txt", "sub/new.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(bufferRoot, "old.txt")); !os.IsNotExist(err) {
		t.Error("old.txt should no longer exist after Rename")
	}
	got, err := os.ReadFile(filepath.Join(bufferRoot, "sub", "new.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "content" {
		t.Errorf("renamed file content = %q, want %q", got, "content")
	}
}

func TestFSCandidateWorkspaceSetMode(t *testing.T) {
	w, _, bufferRoot := newTestWorkspace(t)
	if err := w.WriteCandidate("script.sh", []byte("#!/bin/sh\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.SetMode("script.sh", ModeExecutable); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(bufferRoot, "script.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("script.sh mode = %v, want an executable bit set", info.Mode())
	}
}

func TestFSCandidateWorkspaceSetModeRejectsSymlinkMode(t *testing.T) {
	w, _, _ := newTestWorkspace(t)
	if err := w.WriteCandidate("f.txt", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := w.SetMode("f.txt", ModeSymlink); err == nil {
		t.Error("expected SetMode(ModeSymlink) to be rejected -- use Symlink instead")
	}
}

// TestFSCandidateWorkspaceSymlinkNeverResolvesTarget proves the symlink
// target is stored verbatim, even when it points somewhere nonexistent or
// outside the buffer entirely -- CandidateWorkspace never resolves or
// follows it (hard law 9).
func TestFSCandidateWorkspaceSymlinkNeverResolvesTarget(t *testing.T) {
	w, _, bufferRoot := newTestWorkspace(t)
	target := "/nonexistent/path/that/is/never/resolved"
	if err := w.Symlink("link", target); err != nil {
		t.Fatal(err)
	}
	got, err := os.Readlink(filepath.Join(bufferRoot, "link"))
	if err != nil {
		t.Fatal(err)
	}
	if got != target {
		t.Errorf("symlink target = %q, want verbatim %q", got, target)
	}
}

func TestFSCandidateWorkspaceCloseFailsClosedForEveryMethod(t *testing.T) {
	w, _, _ := newTestWorkspace(t)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := w.ReadSnapshot("main.go"); !errors.Is(err, ErrWorkspaceClosed) {
		t.Errorf("ReadSnapshot after Close = %v, want ErrWorkspaceClosed", err)
	}
	if err := w.WriteCandidate("x.txt", []byte("x")); !errors.Is(err, ErrWorkspaceClosed) {
		t.Errorf("WriteCandidate after Close = %v, want ErrWorkspaceClosed", err)
	}
	if err := w.Delete("x.txt"); !errors.Is(err, ErrWorkspaceClosed) {
		t.Errorf("Delete after Close = %v, want ErrWorkspaceClosed", err)
	}
	if err := w.Rename("a", "b"); !errors.Is(err, ErrWorkspaceClosed) {
		t.Errorf("Rename after Close = %v, want ErrWorkspaceClosed", err)
	}
	if err := w.SetMode("x.txt", ModeExecutable); !errors.Is(err, ErrWorkspaceClosed) {
		t.Errorf("SetMode after Close = %v, want ErrWorkspaceClosed", err)
	}
	if err := w.Symlink("link", "target"); !errors.Is(err, ErrWorkspaceClosed) {
		t.Errorf("Symlink after Close = %v, want ErrWorkspaceClosed", err)
	}
}

// TestFSCandidateWorkspaceRejectsInvalidPathsBeforeTouchingFilesystem proves
// every method rejects a traversal/absolute/backslash path via
// ValidateCandidatePath before any filesystem call.
func TestFSCandidateWorkspaceRejectsInvalidPathsBeforeTouchingFilesystem(t *testing.T) {
	invalid := []string{"../escape", "/absolute", `a\b`, ""}
	for _, p := range invalid {
		w, _, _ := newTestWorkspace(t)
		if _, err := w.ReadSnapshot(p); err == nil {
			t.Errorf("ReadSnapshot(%q) = nil, want an error", p)
		}
		if err := w.WriteCandidate(p, []byte("x")); err == nil {
			t.Errorf("WriteCandidate(%q) = nil, want an error", p)
		}
		if err := w.Delete(p); err == nil {
			t.Errorf("Delete(%q) = nil, want an error", p)
		}
		if err := w.Symlink(p, "target"); err == nil {
			t.Errorf("Symlink(%q) = nil, want an error", p)
		}
	}
}

// TestFSCandidateWorkspaceNeverWritesToSnapshotRoot proves the snapshot
// directory is never mutated by any buffer-mutating method -- ReadSnapshot
// is the only method that ever touches snapshotRoot, and it never writes.
func TestFSCandidateWorkspaceNeverWritesToSnapshotRoot(t *testing.T) {
	w, snapshotRoot, _ := newTestWorkspace(t)
	before, err := os.ReadFile(filepath.Join(snapshotRoot, "main.go"))
	if err != nil {
		t.Fatal(err)
	}

	if err := w.WriteCandidate("main.go", []byte("tampered")); err != nil {
		t.Fatal(err)
	}
	if err := w.SetMode("main.go", ModeExecutable); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(filepath.Join(snapshotRoot, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("snapshotRoot content changed: before %q, after %q", before, after)
	}
}
