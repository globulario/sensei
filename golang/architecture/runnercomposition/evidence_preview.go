// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"context"
	"fmt"
	"sync"
)

// CandidateEvidence is O3's read-only preview of the exact evidence a
// workspace-bound generation provider must place on its proposed Attempt.
// It is an observation from the O3 evidence owner, not provider authority.
// Run independently recomputes the same values after the workspace closes.
type CandidateEvidence struct {
	InputCandidateDigestSHA256        string
	ProposedChangeDigestSHA256        string
	FinalCandidateContentDigestSHA256 string
}

// CandidateEvidencePreviewer is an optional capability implemented by the
// concrete workspace O3 passes to GenerationProviderFactory. A provider may
// use it only after applying its proposed operations through CandidateWorkspace.
// The preview grants no write access and does not replace Run's post-close
// recomputation and digest comparison.
type CandidateEvidencePreviewer interface {
	PreviewCandidateEvidence(ctx context.Context) (CandidateEvidence, error)
}

type evidencePreviewWorkspace struct {
	mu sync.RWMutex

	workspace    CandidateWorkspace
	snapshotDir  string
	bufferDir    string
	inputDigest  string
	closed       bool
}

func newEvidencePreviewWorkspace(workspace CandidateWorkspace, snapshotDir, bufferDir, inputDigest string) CandidateWorkspace {
	return &evidencePreviewWorkspace{
		workspace:   workspace,
		snapshotDir: snapshotDir,
		bufferDir:   bufferDir,
		inputDigest: inputDigest,
	}
}

func (w *evidencePreviewWorkspace) ReadSnapshot(path string) ([]byte, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return nil, ErrWorkspaceClosed
	}
	return w.workspace.ReadSnapshot(path)
}

func (w *evidencePreviewWorkspace) WriteCandidate(path string, content []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrWorkspaceClosed
	}
	return w.workspace.WriteCandidate(path, content)
}

func (w *evidencePreviewWorkspace) Delete(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrWorkspaceClosed
	}
	return w.workspace.Delete(path)
}

func (w *evidencePreviewWorkspace) Rename(oldPath, newPath string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrWorkspaceClosed
	}
	return w.workspace.Rename(oldPath, newPath)
}

func (w *evidencePreviewWorkspace) SetMode(path string, mode CandidateFileMode) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrWorkspaceClosed
	}
	return w.workspace.SetMode(path, mode)
}

func (w *evidencePreviewWorkspace) Symlink(path, target string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrWorkspaceClosed
	}
	return w.workspace.Symlink(path, target)
}

func (w *evidencePreviewWorkspace) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	return w.workspace.Close()
}

func (w *evidencePreviewWorkspace) PreviewCandidateEvidence(ctx context.Context) (CandidateEvidence, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return CandidateEvidence{}, ErrWorkspaceClosed
	}
	if err := ctx.Err(); err != nil {
		return CandidateEvidence{}, err
	}

	snapshotManifest, err := BuildManifest(w.snapshotDir)
	if err != nil {
		return CandidateEvidence{}, fmt.Errorf("runnercomposition: preview snapshot manifest: %w", err)
	}
	inputDigest, err := ManifestDigest(snapshotManifest)
	if err != nil {
		return CandidateEvidence{}, fmt.Errorf("runnercomposition: preview input digest: %w", err)
	}
	if inputDigest != w.inputDigest {
		return CandidateEvidence{}, fmt.Errorf("runnercomposition: preview input digest %q does not match O3 snapshot digest %q", inputDigest, w.inputDigest)
	}

	finalManifest, err := BuildManifest(w.bufferDir)
	if err != nil {
		return CandidateEvidence{}, fmt.Errorf("runnercomposition: preview final manifest: %w", err)
	}
	finalDigest, err := ManifestDigest(finalManifest)
	if err != nil {
		return CandidateEvidence{}, fmt.Errorf("runnercomposition: preview final digest: %w", err)
	}
	changeDigest, err := GitChangeDigest(ctx, w.snapshotDir, w.bufferDir)
	if err != nil {
		return CandidateEvidence{}, fmt.Errorf("runnercomposition: preview change digest: %w", err)
	}

	return CandidateEvidence{
		InputCandidateDigestSHA256:        inputDigest,
		ProposedChangeDigestSHA256:        changeDigest,
		FinalCandidateContentDigestSHA256: finalDigest,
	}, nil
}
