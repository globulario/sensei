// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newTestWorkspace(t *testing.T) (CandidateWorkspace, string, string) {
	t.Helper()
	snapshotRoot := t.TempDir()
	bufferRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(snapshotRoot, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := newFSCandidateWorkspace(snapshotRoot, bufferRoot)
	if err != nil {
		t.Fatal(err)
	}
	return w, snapshotRoot, bufferRoot
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
// outside the buffer entirely -- creating the symlink itself is always
// permitted (hard law 9); it is TRAVERSING through it that containment
// blocks, proven separately below.
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

// --- symlink-escape containment: the exact class of attack flagged in
// review. Each test proves BOTH that the operation is rejected AND that no
// sentinel file/content outside the workspace's roots was ever touched --
// rejection alone would not prove the outside effect never happened.

func TestFSCandidateWorkspaceWriteCandidateCannotEscapeThroughSymlinkedParent(t *testing.T) {
	w, _, _ := newTestWorkspace(t)
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "pwn.txt")

	if err := w.Symlink("escape", outside); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteCandidate("escape/pwn.txt", []byte("owned")); err == nil {
		t.Error("expected WriteCandidate through a symlinked parent pointing outside bufferRoot to fail")
	}
	if _, statErr := os.Stat(sentinel); !os.IsNotExist(statErr) {
		t.Errorf("sentinel file was created outside bufferRoot at %s", sentinel)
	}
}

func TestFSCandidateWorkspaceSetModeCannotEscapeThroughSymlinkedFile(t *testing.T) {
	w, _, _ := newTestWorkspace(t)
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(outsideFile, []byte("victim"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(outsideFile)
	if err != nil {
		t.Fatal(err)
	}

	if err := w.Symlink("link", outsideFile); err != nil {
		t.Fatal(err)
	}
	if err := w.SetMode("link", ModeExecutable); err == nil {
		t.Error("expected SetMode through a symlink pointing outside bufferRoot to fail")
	}
	after, err := os.Stat(outsideFile)
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode() != after.Mode() {
		t.Errorf("outside file's mode changed: before %v, after %v", before.Mode(), after.Mode())
	}
}

func TestFSCandidateWorkspaceRenameDestinationCannotEscapeThroughSymlinkedParent(t *testing.T) {
	w, _, _ := newTestWorkspace(t)
	outside := t.TempDir()

	if err := w.WriteCandidate("source.txt", []byte("mine")); err != nil {
		t.Fatal(err)
	}
	if err := w.Symlink("escape", outside); err != nil {
		t.Fatal(err)
	}
	if err := w.Rename("source.txt", "escape/moved.txt"); err == nil {
		t.Error("expected Rename into a symlinked parent pointing outside bufferRoot to fail")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "moved.txt")); !os.IsNotExist(statErr) {
		t.Errorf("file was moved outside bufferRoot into %s", outside)
	}
}

// TestFSCandidateWorkspaceReadSnapshotCannotEscapeThroughPreexistingSymlink
// covers the case a symlink is not created via Symlink() at all, but was
// already present in the snapshot before the workspace was constructed --
// exactly what a real git tree can legitimately contain (a checked-in
// symlink pointing anywhere, including outside the repository).
func TestFSCandidateWorkspaceReadSnapshotCannotEscapeThroughPreexistingSymlink(t *testing.T) {
	snapshotRoot := t.TempDir()
	bufferRoot := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("do not read me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(snapshotRoot, "escape")); err != nil {
		t.Fatal(err)
	}

	w, err := newFSCandidateWorkspace(snapshotRoot, bufferRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.ReadSnapshot("escape/secret.txt"); err == nil {
		t.Error("expected ReadSnapshot through a pre-existing symlink pointing outside snapshotRoot to fail")
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

func TestFSCandidateWorkspaceCloseIsIdempotent(t *testing.T) {
	w, _, _ := newTestWorkspace(t)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("second Close call returned an error: %v", err)
	}
}

// TestFSCandidateWorkspaceCloseWaitsForInFlightOperation is the freeze-
// barrier proof: Close must not return while an operation that acquired its
// lock before Close was called is still running. This holds the read lock
// directly (the same lock every real method holds for its duration) to
// deterministically simulate a long-running in-flight operation, rather
// than relying on timing an actual filesystem call.
func TestFSCandidateWorkspaceCloseWaitsForInFlightOperation(t *testing.T) {
	w, _, _ := newTestWorkspace(t)
	raw := w.(*fsCandidateWorkspace)

	started := make(chan struct{})
	proceed := make(chan struct{})

	go func() {
		raw.mu.RLock()
		close(started)
		<-proceed
		raw.mu.RUnlock()
	}()
	<-started

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- w.Close()
	}()

	select {
	case <-closeDone:
		t.Fatal("Close returned before the in-flight operation released its lock")
	case <-time.After(50 * time.Millisecond):
		// Expected: Close is still blocked on the held read lock.
	}

	close(proceed)
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
}

// TestFSCandidateWorkspaceConcurrentOperationsRaceDetectorClean is the
// concurrent race-detector proof required alongside the sequential
// after-Close assertions above: many goroutines call WriteCandidate
// concurrently, then Close, with no synchronization beyond what
// fsCandidateWorkspace itself provides. Meaningful only under `go test
// -race`.
func TestFSCandidateWorkspaceConcurrentOperationsRaceDetectorClean(t *testing.T) {
	w, _, _ := newTestWorkspace(t)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = w.WriteCandidate(fmt.Sprintf("f%d.txt", i), []byte("x"))
		}()
	}
	wg.Wait()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestFSCandidateWorkspaceConcurrentOperationsRaceAgainstClose races
// WriteCandidate calls directly against Close, under the race detector,
// proving no data race on the shared closed flag or *os.Root handles --
// every WriteCandidate call must observe either success (completed before
// Close's write lock was granted) or ErrWorkspaceClosed, never a panic or a
// use of a closed handle.
func TestFSCandidateWorkspaceConcurrentOperationsRaceAgainstClose(t *testing.T) {
	w, _, _ := newTestWorkspace(t)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := w.WriteCandidate(fmt.Sprintf("g%d.txt", i), []byte("x"))
			if err != nil && !errors.Is(err, ErrWorkspaceClosed) {
				t.Errorf("WriteCandidate returned an unexpected error racing Close: %v", err)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()
	wg.Wait()
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

// --- newFSCandidateWorkspace construction validation ---

func TestNewFSCandidateWorkspaceAcceptsValidDistinctRoots(t *testing.T) {
	if _, err := newFSCandidateWorkspace(t.TempDir(), t.TempDir()); err != nil {
		t.Errorf("valid distinct roots rejected: %v", err)
	}
}

func TestNewFSCandidateWorkspaceRejectsSameDirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := newFSCandidateWorkspace(dir, dir); err == nil {
		t.Error("expected the same directory for both roots to be rejected")
	}
}

func TestNewFSCandidateWorkspaceRejectsNestedRoots(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := newFSCandidateWorkspace(parent, child); err == nil {
		t.Error("expected bufferRoot nested inside snapshotRoot to be rejected")
	}
	if _, err := newFSCandidateWorkspace(child, parent); err == nil {
		t.Error("expected snapshotRoot nested inside bufferRoot to be rejected")
	}
}

func TestNewFSCandidateWorkspaceRejectsSymlinkAlias(t *testing.T) {
	real := t.TempDir()
	aliasParent := t.TempDir()
	alias := filepath.Join(aliasParent, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := newFSCandidateWorkspace(real, alias); err == nil {
		t.Error("expected a symlink alias resolving to the same real directory to be rejected")
	}
}

func TestNewFSCandidateWorkspaceRejectsMissingPath(t *testing.T) {
	parent := t.TempDir()
	missing := filepath.Join(parent, "does-not-exist")
	other := t.TempDir()
	if _, err := newFSCandidateWorkspace(missing, other); err == nil {
		t.Error("expected a missing snapshotRoot path to be rejected")
	}
	if _, err := newFSCandidateWorkspace(other, missing); err == nil {
		t.Error("expected a missing bufferRoot path to be rejected")
	}
}

func TestNewFSCandidateWorkspaceRejectsRegularFileInsteadOfDirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	if _, err := newFSCandidateWorkspace(file, other); err == nil {
		t.Error("expected a regular file for snapshotRoot to be rejected")
	}
}

func TestNewFSCandidateWorkspaceRejectsEmptyPaths(t *testing.T) {
	dir := t.TempDir()
	if _, err := newFSCandidateWorkspace("", dir); err == nil {
		t.Error("expected an empty snapshotRoot to be rejected")
	}
	if _, err := newFSCandidateWorkspace(dir, ""); err == nil {
		t.Error("expected an empty bufferRoot to be rejected")
	}
}
