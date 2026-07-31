// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
// snapshotRoot is read-only -- no fsCandidateWorkspace method ever writes to
// it. bufferRoot is the ephemeral candidate buffer a provider mutates. Both
// roots must already exist and be populated by the caller (snapshot
// creation and buffer initialization are separate, later pieces of O3, not
// this type's concern); fsCandidateWorkspace does not create, populate, or
// destroy either root.
type fsCandidateWorkspace struct {
	snapshotRoot string
	bufferRoot   string
	closed       bool
}

// NewFSCandidateWorkspace constructs a CandidateWorkspace over an existing
// snapshotRoot (read-only) and bufferRoot (read-write) directory pair.
func NewFSCandidateWorkspace(snapshotRoot, bufferRoot string) CandidateWorkspace {
	return &fsCandidateWorkspace{snapshotRoot: snapshotRoot, bufferRoot: bufferRoot}
}

// resolve validates path and joins it under root, returning ErrWorkspaceClosed
// if the workspace has already been closed. ValidateCandidatePath already
// rejects "..", a leading "/", and backslash, so the result can never
// resolve outside root.
func (w *fsCandidateWorkspace) resolve(root, path string) (string, error) {
	if w.closed {
		return "", ErrWorkspaceClosed
	}
	if err := ValidateCandidatePath(path); err != nil {
		return "", err
	}
	return filepath.Join(root, filepath.FromSlash(path)), nil
}

func (w *fsCandidateWorkspace) ReadSnapshot(path string) ([]byte, error) {
	full, err := w.resolve(w.snapshotRoot, path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}

func (w *fsCandidateWorkspace) WriteCandidate(path string, content []byte) error {
	full, err := w.resolve(w.bufferRoot, path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	return os.WriteFile(full, content, 0o644)
}

func (w *fsCandidateWorkspace) Delete(path string) error {
	full, err := w.resolve(w.bufferRoot, path)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (w *fsCandidateWorkspace) Rename(oldPath, newPath string) error {
	oldFull, err := w.resolve(w.bufferRoot, oldPath)
	if err != nil {
		return err
	}
	newFull, err := w.resolve(w.bufferRoot, newPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(newFull), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	return os.Rename(oldFull, newFull)
}

func (w *fsCandidateWorkspace) SetMode(path string, mode CandidateFileMode) error {
	full, err := w.resolve(w.bufferRoot, path)
	if err != nil {
		return err
	}
	switch mode {
	case ModeRegular:
		return os.Chmod(full, 0o644)
	case ModeExecutable:
		return os.Chmod(full, 0o755)
	case ModeSymlink:
		return fmt.Errorf("SetMode: use Symlink to create a symlink, not SetMode(%q)", mode)
	default:
		return fmt.Errorf("SetMode: unknown mode %q", mode)
	}
}

func (w *fsCandidateWorkspace) Symlink(path, target string) error {
	full, err := w.resolve(w.bufferRoot, path)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(full); err != nil {
		return fmt.Errorf("remove existing entry before creating symlink: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	// target is stored verbatim by os.Symlink -- it is never resolved,
	// followed, or validated as a path itself (hard law 9).
	return os.Symlink(target, full)
}

func (w *fsCandidateWorkspace) Close() error {
	w.closed = true
	return nil
}

// GenerationProviderFactory produces a fresh, workspace-bound Provider for
// exactly one attempt (hard law 5). No Provider instance is ever reused
// across attempts or sessions. A concrete factory implementation wiring to
// a real model or SDK is O6, out of scope here.
type GenerationProviderFactory interface {
	NewProvider(workspace CandidateWorkspace) (providerport.Provider, error)
}
