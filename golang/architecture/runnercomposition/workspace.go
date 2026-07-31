// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/globulario/sensei/golang/architecture/providerport"
)

// ErrWorkspaceClosed is returned by every CandidateWorkspace method once
// Close has been called, per hard law 6 -- a provider that retains a handle
// past Close fails closed rather than silently succeeding or falling back
// to ambient access.
var ErrWorkspaceClosed = errors.New("runnercomposition: candidate workspace is closed")

// CandidateWorkspace is the typed, closable channel a concrete Provider is
// constructed with, so it never needs ambient filesystem access to
// participate (hard law 1). Every method validates path with
// ValidateCandidatePath before touching the filesystem, and every method
// returns ErrWorkspaceClosed once Close has been called (hard law 6).
type CandidateWorkspace interface {
	// ReadSnapshot reads path from the pinned, read-only snapshot. Never
	// reaches into the candidate buffer.
	ReadSnapshot(path string) ([]byte, error)
	// WriteCandidate writes content to path in the ephemeral candidate
	// buffer, creating parent directories as needed.
	WriteCandidate(path string, content []byte) error
	// Delete removes path from the candidate buffer. Not an error if path
	// does not exist.
	Delete(path string) error
	// Rename moves oldPath to newPath within the candidate buffer.
	Rename(oldPath, newPath string) error
	// SetMode sets path's mode to ModeRegular or ModeExecutable within the
	// candidate buffer. SetMode does not create a symlink -- use Symlink.
	SetMode(path string, mode CandidateFileMode) error
	// Symlink creates path in the candidate buffer as a symlink to target.
	// target is stored verbatim -- CandidateWorkspace never resolves or
	// follows it (hard law 9).
	Symlink(path, target string) error
	// Close freezes the workspace (hard law 6a). Every method above
	// returns ErrWorkspaceClosed after Close returns, regardless of
	// Close's own error.
	Close() error
}

// fsCandidateWorkspace is the concrete, filesystem-backed CandidateWorkspace.
// snapshotRoot is read-only -- no fsCandidateWorkspace method ever writes
// through it. bufferRoot is the ephemeral candidate buffer a provider
// mutates. Both are *os.Root handles (Go 1.24+), not bare path strings:
// every Root method is contained to beneath its root even when a path
// component is a symlink -- a symlink may exist and be created freely (hard
// law 9 never forbids that), but no Root method will ever traverse THROUGH
// one to a location outside its root. This is what makes WriteCandidate/
// Delete/Rename/SetMode/Symlink/ReadSnapshot safe against the exact class
// of symlink-escape bare os.* calls under a joined path string are
// vulnerable to.
//
// mu is the freeze barrier for Close (hard law 6a): every operation holds a
// read lock for its full duration; Close takes the write lock, which by
// definition cannot be acquired until every in-flight read-locked operation
// has finished, so Close never returns while a provider-initiated
// filesystem operation is still in progress, and no new operation can begin
// once Close has acquired the lock.
type fsCandidateWorkspace struct {
	mu           sync.RWMutex
	snapshotRoot *os.Root
	bufferRoot   *os.Root
	closed       bool
}

// isWithin reports whether child is parent itself or nested inside it, once
// both are already-cleaned, absolute, symlink-resolved paths.
func isWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// newFSCandidateWorkspace constructs a CandidateWorkspace over an existing
// snapshotRoot (read-only) and bufferRoot (read-write) directory pair.
// Unexported: the future snapshot/attempt orchestration that actually
// creates these directories is this function's only intended caller --
// exposing two arbitrary public path strings as a constructor invites
// exactly the aliasing/overlap bugs this function rejects, so this stays a
// package-internal seam rather than public API until a real external
// caller need is identified.
//
// Construction is fallible and verifies:
//   - both paths resolve (via filepath.EvalSymlinks) to existing, real
//     directories, not files and not missing paths;
//   - the two real directories are distinct -- not the same directory
//     reached by different paths or a symlink alias;
//   - neither real directory is nested inside the other.
//
// A caller who cannot satisfy these (e.g. because directory creation
// itself failed) maps that failure to DispositionWorkspaceInitFailure --
// this function returning an error is precisely that disposition's
// mechanical trigger.
func newFSCandidateWorkspace(snapshotRoot, bufferRoot string) (CandidateWorkspace, error) {
	if snapshotRoot == "" {
		return nil, fmt.Errorf("newFSCandidateWorkspace: snapshotRoot must not be empty")
	}
	if bufferRoot == "" {
		return nil, fmt.Errorf("newFSCandidateWorkspace: bufferRoot must not be empty")
	}

	snapReal, err := realDir(snapshotRoot)
	if err != nil {
		return nil, fmt.Errorf("snapshotRoot: %w", err)
	}
	bufReal, err := realDir(bufferRoot)
	if err != nil {
		return nil, fmt.Errorf("bufferRoot: %w", err)
	}

	if snapReal == bufReal {
		return nil, fmt.Errorf("newFSCandidateWorkspace: snapshotRoot and bufferRoot resolve to the same directory %q", snapReal)
	}
	if isWithin(snapReal, bufReal) {
		return nil, fmt.Errorf("newFSCandidateWorkspace: bufferRoot %q is nested inside snapshotRoot %q", bufReal, snapReal)
	}
	if isWithin(bufReal, snapReal) {
		return nil, fmt.Errorf("newFSCandidateWorkspace: snapshotRoot %q is nested inside bufferRoot %q", snapReal, bufReal)
	}

	snapHandle, err := os.OpenRoot(snapReal)
	if err != nil {
		return nil, fmt.Errorf("open snapshotRoot: %w", err)
	}
	bufHandle, err := os.OpenRoot(bufReal)
	if err != nil {
		snapHandle.Close()
		return nil, fmt.Errorf("open bufferRoot: %w", err)
	}

	return &fsCandidateWorkspace{snapshotRoot: snapHandle, bufferRoot: bufHandle}, nil
}

// realDir resolves path to an absolute, symlink-evaluated form and confirms
// it names an existing directory.
func realDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve: %w", err)
	}
	info, err := os.Lstat(real)
	if err != nil {
		return "", fmt.Errorf("stat: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", real)
	}
	return real, nil
}

func (w *fsCandidateWorkspace) ReadSnapshot(path string) ([]byte, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return nil, ErrWorkspaceClosed
	}
	if err := ValidateCandidatePath(path); err != nil {
		return nil, err
	}
	return w.snapshotRoot.ReadFile(filepath.FromSlash(path))
}

func (w *fsCandidateWorkspace) WriteCandidate(path string, content []byte) error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return ErrWorkspaceClosed
	}
	if err := ValidateCandidatePath(path); err != nil {
		return err
	}
	name := filepath.FromSlash(path)
	if err := w.bufferRoot.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	return w.bufferRoot.WriteFile(name, content, 0o644)
}

func (w *fsCandidateWorkspace) Delete(path string) error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return ErrWorkspaceClosed
	}
	if err := ValidateCandidatePath(path); err != nil {
		return err
	}
	if err := w.bufferRoot.Remove(filepath.FromSlash(path)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (w *fsCandidateWorkspace) Rename(oldPath, newPath string) error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return ErrWorkspaceClosed
	}
	if err := ValidateCandidatePath(oldPath); err != nil {
		return err
	}
	if err := ValidateCandidatePath(newPath); err != nil {
		return err
	}
	newName := filepath.FromSlash(newPath)
	if err := w.bufferRoot.MkdirAll(filepath.Dir(newName), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	return w.bufferRoot.Rename(filepath.FromSlash(oldPath), newName)
}

func (w *fsCandidateWorkspace) SetMode(path string, mode CandidateFileMode) error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return ErrWorkspaceClosed
	}
	if err := ValidateCandidatePath(path); err != nil {
		return err
	}
	name := filepath.FromSlash(path)
	switch mode {
	case ModeRegular:
		return w.bufferRoot.Chmod(name, 0o644)
	case ModeExecutable:
		return w.bufferRoot.Chmod(name, 0o755)
	case ModeSymlink:
		return fmt.Errorf("SetMode: use Symlink to create a symlink, not SetMode(%q)", mode)
	default:
		return fmt.Errorf("SetMode: unknown mode %q", mode)
	}
}

func (w *fsCandidateWorkspace) Symlink(path, target string) error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return ErrWorkspaceClosed
	}
	if err := ValidateCandidatePath(path); err != nil {
		return err
	}
	name := filepath.FromSlash(path)
	if err := w.bufferRoot.RemoveAll(name); err != nil {
		return fmt.Errorf("remove existing entry before creating symlink: %w", err)
	}
	if err := w.bufferRoot.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	// target is stored verbatim -- os.Root.Symlink does not validate or
	// resolve it (hard law 9). Any LATER Root method that would traverse
	// THROUGH this symlink is still contained to beneath bufferRoot,
	// regardless of what target says.
	return w.bufferRoot.Symlink(target, name)
}

// Close freezes the workspace (hard law 6a): it blocks until every
// in-flight operation above has finished (the write-lock cannot be
// acquired while any read-locked operation is still running), then marks
// the workspace closed before releasing the *os.Root handles, so no
// operation can observe a torn or half-closed state. Idempotent.
func (w *fsCandidateWorkspace) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	snapErr := w.snapshotRoot.Close()
	bufErr := w.bufferRoot.Close()
	return errors.Join(snapErr, bufErr)
}

// GenerationProviderFactory produces a fresh, workspace-bound Provider for
// exactly one attempt (hard law 5). No Provider instance is ever reused
// across attempts or sessions. A concrete factory implementation wiring to
// a real model or SDK is O6, out of scope here.
type GenerationProviderFactory interface {
	NewProvider(workspace CandidateWorkspace) (providerport.Provider, error)
}
