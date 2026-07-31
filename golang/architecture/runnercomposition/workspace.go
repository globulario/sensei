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
	// reaches into the candidate buffer. Errors if path, or any parent
	// component of path, is a symlink -- ReadSnapshot never dereferences
	// one (hard law 9).
	ReadSnapshot(path string) ([]byte, error)
	// WriteCandidate writes content to path in the ephemeral candidate
	// buffer, creating parent directories as needed. If path is currently a
	// symlink, that symlink is removed first and replaced with a regular
	// file -- WriteCandidate never writes through an existing symlink to
	// whatever it points at (hard law 9). Errors if any PARENT component of
	// path is a symlink.
	WriteCandidate(path string, content []byte) error
	// Delete removes path from the candidate buffer. Not an error if path
	// does not exist. Removing a symlink removes the link itself, never its
	// target. Errors if any parent component of path is a symlink.
	Delete(path string) error
	// Rename moves oldPath to newPath within the candidate buffer. If
	// newPath is currently a symlink, that symlink is removed first, the
	// same replace-not-write-through policy as WriteCandidate. Errors if
	// any parent component of oldPath or newPath is a symlink.
	Rename(oldPath, newPath string) error
	// SetMode sets path's mode to ModeRegular or ModeExecutable within the
	// candidate buffer. SetMode does not create a symlink -- use Symlink.
	// Errors if path, or any parent component of path, is a symlink --
	// SetMode never changes a dereferenced target's mode (hard law 9).
	SetMode(path string, mode CandidateFileMode) error
	// Symlink creates path in the candidate buffer as a symlink to target.
	// target is stored verbatim -- CandidateWorkspace never resolves or
	// follows it (hard law 9). Whatever currently exists at path (file,
	// directory, or symlink) is replaced. Errors if any PARENT component of
	// path is a symlink.
	Symlink(path, target string) error
	// Close freezes the workspace (hard law 6a). Every method above
	// returns ErrWorkspaceClosed after Close returns, regardless of
	// Close's own error.
	Close() error
}

// fsCandidateWorkspace is the concrete, filesystem-backed CandidateWorkspace.
// snapshotRoot is read-only -- no fsCandidateWorkspace method ever writes
// through it. bufferRoot is the ephemeral candidate buffer a provider
// mutates. Both are *os.Root handles (Go 1.24+): every Root method is
// CONTAINED to beneath its root even when a path component is a symlink --
// this is what makes escape (a symlink resolving to a location outside the
// root) impossible. Containment is not the same property as no-follow,
// though: os.Root's own documentation is explicit that "Methods on Root
// will follow symbolic links" as long as the target stays within the root,
// which would let one candidate-buffer entry silently alias another (e.g.
// WriteCandidate("alias.txt", ...) actually writing through to
// "target.txt" if "alias.txt" is a symlink to it) -- exactly the kind of
// aliasing the manifest/change-digest layer's one-path-one-entry semantics
// cannot tolerate. noFollowGuard is the layer ABOVE os.Root that closes
// that gap: every method walks path's parent components with Lstat one
// prefix at a time (never trusting a deeper Lstat call to have safely
// resolved a shallower symlink on its own) and rejects outright if any
// non-final component is a symlink; each method then applies its own
// explicit, documented policy for a symlink at the FINAL component (replace
// for the mutating methods, reject for ReadSnapshot/SetMode).
//
// mu is BOTH the freeze barrier for Close (hard law 6a) AND the mutual-
// exclusion barrier candidate-tree mutations need on top of noFollowGuard.
// Every buffer-mutating method (WriteCandidate, Delete, Rename, SetMode,
// Symlink) takes the EXCLUSIVE write lock for its full
// noFollowGuard-check-then-act sequence, not the shared read lock: a
// noFollowGuard check followed by a filesystem action is only truly safe
// against internal aliasing if no OTHER candidate-tree mutation can run
// between them, and a shared lock would only have prevented that pair from
// racing Close, not each other -- a different operation could still swap a
// checked-clean parent or final entry into a symlink in the gap. ReadSnapshot
// never touches bufferRoot (only the immutable snapshotRoot), so it keeps
// the shared read lock: concurrent reads are safe with each other, and the
// shared/exclusive relationship still correctly blocks it against Close and
// against any exclusive-locked buffer mutation. Close takes the same
// exclusive lock, which by definition cannot be acquired until every
// in-flight operation (shared or exclusive) has finished, so Close never
// returns while any operation is still in progress, and no new operation
// can begin once Close has acquired the lock.
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
//
// NOTE for the eventual merge: this package now relies on os.Root
// (introduced in Go 1.24) as a security boundary. Any Go 1.24/1.25 patch
// release later found to have a containment defect in os.Root should not
// remain a permitted toolchain version for this repository indefinitely --
// track this alongside normal Go version bumps, not as a one-time check.
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

// noFollowGuard walks name's path components one prefix at a time, calling
// root.Lstat on each -- never a deeper Lstat call alone, which os.Root's own
// documented "follows symbolic links" traversal behavior could satisfy by
// silently resolving a shallower symlink component first. Any NON-FINAL
// component that is a symlink is rejected outright: no method may traverse
// through an internal symlink, whether it stays within the root (os.Root
// alone would allow this) or not (os.Root alone already rejects that part).
//
// The return value is the final component's own os.FileInfo -- nil if it
// does not exist yet -- so the caller can apply its own policy for a
// symlink specifically at the final component (see each method's doc
// comment on CandidateWorkspace).
func noFollowGuard(root *os.Root, name string) (os.FileInfo, error) {
	if name == "." {
		return nil, nil
	}
	segments := strings.Split(filepath.ToSlash(name), "/")
	var finalInfo os.FileInfo
	for i := range segments {
		prefix := filepath.FromSlash(strings.Join(segments[:i+1], "/"))
		info, err := root.Lstat(prefix)
		if err != nil {
			if os.IsNotExist(err) {
				// Nothing exists from here down yet -- nothing further to
				// check, and the operation that follows will create it
				// fresh.
				return nil, nil
			}
			return nil, err
		}
		isFinal := i == len(segments)-1
		if !isFinal && info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("path component %q is a symlink -- traversal through an internal symlink is not permitted", prefix)
		}
		if isFinal {
			finalInfo = info
		}
	}
	return finalInfo, nil
}

func isSymlinkInfo(info os.FileInfo) bool {
	return info != nil && info.Mode()&os.ModeSymlink != 0
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
	name := filepath.FromSlash(path)
	info, err := noFollowGuard(w.snapshotRoot, name)
	if err != nil {
		return nil, err
	}
	if isSymlinkInfo(info) {
		return nil, fmt.Errorf("ReadSnapshot: %q is a symlink -- O3 never dereferences a symlink target", path)
	}
	return w.snapshotRoot.ReadFile(name)
}

func (w *fsCandidateWorkspace) WriteCandidate(path string, content []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrWorkspaceClosed
	}
	if err := ValidateCandidatePath(path); err != nil {
		return err
	}
	name := filepath.FromSlash(path)
	info, err := noFollowGuard(w.bufferRoot, name)
	if err != nil {
		return err
	}
	if isSymlinkInfo(info) {
		// Replace, never write through: remove the symlink itself first.
		if err := w.bufferRoot.Remove(name); err != nil {
			return fmt.Errorf("remove existing symlink before writing: %w", err)
		}
	}
	if err := w.bufferRoot.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	return w.bufferRoot.WriteFile(name, content, 0o644)
}

func (w *fsCandidateWorkspace) Delete(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrWorkspaceClosed
	}
	if err := ValidateCandidatePath(path); err != nil {
		return err
	}
	name := filepath.FromSlash(path)
	if _, err := noFollowGuard(w.bufferRoot, name); err != nil {
		return err
	}
	if err := w.bufferRoot.Remove(name); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (w *fsCandidateWorkspace) Rename(oldPath, newPath string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrWorkspaceClosed
	}
	if err := ValidateCandidatePath(oldPath); err != nil {
		return err
	}
	if err := ValidateCandidatePath(newPath); err != nil {
		return err
	}
	oldName := filepath.FromSlash(oldPath)
	newName := filepath.FromSlash(newPath)
	if _, err := noFollowGuard(w.bufferRoot, oldName); err != nil {
		return err
	}
	newInfo, err := noFollowGuard(w.bufferRoot, newName)
	if err != nil {
		return err
	}
	if isSymlinkInfo(newInfo) {
		// Replace, never write through: remove the destination symlink
		// itself first, the same policy as WriteCandidate.
		if err := w.bufferRoot.Remove(newName); err != nil {
			return fmt.Errorf("remove existing symlink at destination before renaming: %w", err)
		}
	}
	if err := w.bufferRoot.MkdirAll(filepath.Dir(newName), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	return w.bufferRoot.Rename(oldName, newName)
}

func (w *fsCandidateWorkspace) SetMode(path string, mode CandidateFileMode) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrWorkspaceClosed
	}
	if err := ValidateCandidatePath(path); err != nil {
		return err
	}
	name := filepath.FromSlash(path)
	info, err := noFollowGuard(w.bufferRoot, name)
	if err != nil {
		return err
	}
	if isSymlinkInfo(info) {
		return fmt.Errorf("SetMode: %q is a symlink -- O3 never dereferences a symlink target", path)
	}
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
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrWorkspaceClosed
	}
	if err := ValidateCandidatePath(path); err != nil {
		return err
	}
	name := filepath.FromSlash(path)
	if _, err := noFollowGuard(w.bufferRoot, name); err != nil {
		return err
	}
	if err := w.bufferRoot.RemoveAll(name); err != nil {
		return fmt.Errorf("remove existing entry before creating symlink: %w", err)
	}
	if err := w.bufferRoot.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	// target is stored verbatim -- os.Root.Symlink does not validate or
	// resolve it (hard law 9). Any LATER Root method that would traverse
	// THROUGH this symlink is still contained to beneath bufferRoot,
	// regardless of what target says, and noFollowGuard rejects traversal
	// through it as a non-final component entirely.
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
