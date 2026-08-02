// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCandidateEvidencePreviewMatchesCanonicalOwnersAndCloses(t *testing.T) {
	snapshotDir := t.TempDir()
	bufferDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(snapshotDir, "a.txt"), []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bufferDir, "a.txt"), []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	workspace, err := newFSCandidateWorkspace(snapshotDir, bufferDir)
	if err != nil {
		t.Fatal(err)
	}
	previewer, ok := workspace.(CandidateEvidencePreviewer)
	if !ok {
		t.Fatal("O3 filesystem workspace does not expose its evidence preview capability")
	}
	if err := workspace.WriteCandidate("a.txt", []byte("after\n")); err != nil {
		t.Fatal(err)
	}

	got, err := previewer.PreviewCandidateEvidence(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	snapshotManifest, err := BuildManifest(snapshotDir)
	if err != nil {
		t.Fatal(err)
	}
	wantInput, err := ManifestDigest(snapshotManifest)
	if err != nil {
		t.Fatal(err)
	}
	finalManifest, err := BuildManifest(bufferDir)
	if err != nil {
		t.Fatal(err)
	}
	wantFinal, err := ManifestDigest(finalManifest)
	if err != nil {
		t.Fatal(err)
	}
	wantChange, err := GitChangeDigest(context.Background(), snapshotDir, bufferDir)
	if err != nil {
		t.Fatal(err)
	}
	if got.InputCandidateDigestSHA256 != wantInput ||
		got.ProposedChangeDigestSHA256 != wantChange ||
		got.FinalCandidateContentDigestSHA256 != wantFinal {
		t.Fatalf("preview = %#v, want input=%s change=%s final=%s", got, wantInput, wantChange, wantFinal)
	}

	if err := workspace.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := previewer.PreviewCandidateEvidence(context.Background()); !errors.Is(err, ErrWorkspaceClosed) {
		t.Fatalf("preview after Close error = %v, want ErrWorkspaceClosed", err)
	}
}
