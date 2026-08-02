// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"context"
	"fmt"
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

// CandidateEvidencePreviewer is an optional capability implemented by O3's
// concrete filesystem workspace. A provider may use it only after applying
// its proposed operations through CandidateWorkspace. The preview grants no
// write access and never replaces Run's post-close recomputation and digest
// comparison.
type CandidateEvidencePreviewer interface {
	PreviewCandidateEvidence(ctx context.Context) (CandidateEvidence, error)
}

// PreviewCandidateEvidence computes evidence from the exact snapshot and
// candidate-buffer roots owned by this workspace. The workspace read lock
// excludes Close and every candidate mutation for the complete computation,
// so the three returned digests describe one stable candidate state.
func (w *fsCandidateWorkspace) PreviewCandidateEvidence(ctx context.Context) (CandidateEvidence, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return CandidateEvidence{}, ErrWorkspaceClosed
	}
	if err := ctx.Err(); err != nil {
		return CandidateEvidence{}, err
	}

	snapshotDir := w.snapshotRoot.Name()
	bufferDir := w.bufferRoot.Name()

	snapshotManifest, err := BuildManifest(snapshotDir)
	if err != nil {
		return CandidateEvidence{}, fmt.Errorf("runnercomposition: preview snapshot manifest: %w", err)
	}
	inputDigest, err := ManifestDigest(snapshotManifest)
	if err != nil {
		return CandidateEvidence{}, fmt.Errorf("runnercomposition: preview input digest: %w", err)
	}

	finalManifest, err := BuildManifest(bufferDir)
	if err != nil {
		return CandidateEvidence{}, fmt.Errorf("runnercomposition: preview final manifest: %w", err)
	}
	finalDigest, err := ManifestDigest(finalManifest)
	if err != nil {
		return CandidateEvidence{}, fmt.Errorf("runnercomposition: preview final digest: %w", err)
	}
	changeDigest, err := GitChangeDigest(ctx, snapshotDir, bufferDir)
	if err != nil {
		return CandidateEvidence{}, fmt.Errorf("runnercomposition: preview change digest: %w", err)
	}

	return CandidateEvidence{
		InputCandidateDigestSHA256:        inputDigest,
		ProposedChangeDigestSHA256:        changeDigest,
		FinalCandidateContentDigestSHA256: finalDigest,
	}, nil
}
