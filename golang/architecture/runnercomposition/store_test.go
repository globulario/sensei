// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFSCandidateArtifactStorePutThenGetRoundTrips(t *testing.T) {
	root := t.TempDir()
	store, err := NewFSCandidateArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	artifact := fixtureCandidateArtifact(t)

	if err := store.Put(context.Background(), artifact); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(context.Background(), artifact.CandidateArtifactDigestSHA256)
	if err != nil {
		t.Fatal(err)
	}

	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("Get returned a document that does not byte-for-byte match what was Put:\ngot:  %s\nwant: %s", gotJSON, wantJSON)
	}
	for i, e := range got.Manifest {
		if string(e.Content) != string(artifact.Manifest[i].Content) {
			t.Errorf("manifest[%d].Content diverged after round-trip", i)
		}
	}
}

func TestFSCandidateArtifactStorePutIsIdempotentForIdenticalContent(t *testing.T) {
	root := t.TempDir()
	store, err := NewFSCandidateArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	artifact := fixtureCandidateArtifact(t)

	if err := store.Put(context.Background(), artifact); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), artifact); err != nil {
		t.Errorf("second identical Put should be an idempotent no-op, got error: %v", err)
	}
}

// TestFSCandidateArtifactStorePutRefusesToOverwriteDifferingContent proves
// immutability directly: since a valid CandidateArtifact's digest is a
// function of its own content, two DIFFERENT valid artifacts can never
// legitimately share a digest -- so this simulates the only way the
// scenario can arise in practice, tampering with the store's file directly
// between two Put calls, and confirms Put detects the mismatch and refuses
// to silently overwrite the sealed entry.
func TestFSCandidateArtifactStorePutRefusesToOverwriteDifferingContent(t *testing.T) {
	root := t.TempDir()
	store, err := NewFSCandidateArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	artifact := fixtureCandidateArtifact(t)
	if err := store.Put(context.Background(), artifact); err != nil {
		t.Fatal(err)
	}

	tamperedPath := filepath.Join(root, artifact.CandidateArtifactDigestSHA256+".json")
	if err := os.WriteFile(tamperedPath, []byte(`{"tampered":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := store.Put(context.Background(), artifact); err == nil {
		t.Error("expected Put to refuse to overwrite a sealed entry whose on-disk content no longer matches")
	}
}

// TestFSCandidateArtifactStorePutRejectsInvalidArtifactWithoutWritingAnything
// proves ValidateCandidateArtifact runs before any filesystem write: an
// artifact with a wrong declared digest is rejected, and no file appears
// anywhere under root.
func TestFSCandidateArtifactStorePutRejectsInvalidArtifactWithoutWritingAnything(t *testing.T) {
	root := t.TempDir()
	store, err := NewFSCandidateArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	artifact := fixtureCandidateArtifact(t)
	artifact.CandidateArtifactDigestSHA256 = zeroDigest // wrong on purpose

	if err := store.Put(context.Background(), artifact); err == nil {
		t.Error("expected Put to reject an artifact with an incorrect declared digest")
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("Put left %d entries behind after rejecting an invalid artifact, want 0: %v", len(entries), entries)
	}
}

// TestFSCandidateArtifactStorePutIsTransactional forces the commit
// (rename) step specifically to fail -- by pre-seeding the store's internal
// .tmp staging directory (writable throughout) via a first successful Put,
// then revoking write permission on root itself (blocking the creation of
// a NEW final entry, which rename requires, while leaving .tmp's own
// permissions, and therefore staged writes and their cleanup, untouched) --
// and proves nothing is visible under the failed Put's digest afterward: no
// partially-sealed entry, no leaked staging file.
func TestFSCandidateArtifactStorePutIsTransactional(t *testing.T) {
	root := t.TempDir()
	store, err := NewFSCandidateArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}

	bootstrap := fixtureCandidateArtifact(t)
	if err := store.Put(context.Background(), bootstrap); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0o755) })

	entries := fixtureManifestEntries(t)
	entries[0].Content = []byte("different content so this is a distinct digest\n")
	entries[0].ContentDigestSHA256 = sha256Hex(entries[0].Content)
	second := fixtureCandidateArtifact(t)
	second.Manifest = entries
	finalDigest, err := ManifestDigest(entries)
	if err != nil {
		t.Fatal(err)
	}
	second.FinalCandidateContentDigestSHA256 = finalDigest
	digest, err := CandidateArtifactDigest(second)
	if err != nil {
		t.Fatal(err)
	}
	second.CandidateArtifactDigestSHA256 = digest

	if err := store.Put(context.Background(), second); err == nil {
		t.Fatal("expected Put to fail once root is read-only (rename cannot create a new entry)")
	}

	os.Chmod(root, 0o755)
	if _, err := os.Stat(filepath.Join(root, second.CandidateArtifactDigestSHA256+".json")); !os.IsNotExist(err) {
		t.Errorf("a final entry exists for a Put that failed at the commit step: err=%v", err)
	}
	tmpEntries, err := os.ReadDir(filepath.Join(root, ".tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tmpEntries) != 0 {
		t.Errorf(".tmp still contains %d staged file(s) after a failed Put's cleanup: %v", len(tmpEntries), tmpEntries)
	}
}

func TestFSCandidateArtifactStoreGetReturnsNotFoundForUnknownDigest(t *testing.T) {
	root := t.TempDir()
	store, err := NewFSCandidateArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Get(context.Background(), zeroDigest)
	if err == nil {
		t.Fatal("expected an error for an unsealed digest")
	}
	if !errors.Is(err, ErrCandidateArtifactNotFound) {
		t.Errorf("expected errors.Is(err, ErrCandidateArtifactNotFound), got %v", err)
	}
}

func TestFSCandidateArtifactStoreGetRejectsCorruptedJSON(t *testing.T) {
	root := t.TempDir()
	store, err := NewFSCandidateArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, zeroDigest+".json"), []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = store.Get(context.Background(), zeroDigest)
	if err == nil {
		t.Fatal("expected an error for corrupted JSON")
	}
	if !errors.Is(err, ErrCandidateArtifactCorrupted) {
		t.Errorf("expected errors.Is(err, ErrCandidateArtifactCorrupted), got %v", err)
	}
}

// TestFSCandidateArtifactStoreGetRejectsDigestKeyMismatch proves the store
// refuses to hand back content whose own digest does not match the key it
// was retrieved by, even though the content is otherwise a fully valid
// CandidateArtifact on its own terms -- defending against an on-disk
// key/content association corrupted independently of the content itself
// (e.g. a filesystem-level rename/swap between two sealed entries).
func TestFSCandidateArtifactStoreGetRejectsDigestKeyMismatch(t *testing.T) {
	root := t.TempDir()
	store, err := NewFSCandidateArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	artifact := fixtureCandidateArtifact(t)
	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	// Store the fully valid artifact under a DIFFERENT digest than its own.
	if err := os.WriteFile(filepath.Join(root, zeroDigest+".json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = store.Get(context.Background(), zeroDigest)
	if err == nil {
		t.Fatal("expected an error when the stored content's digest does not match the requested key")
	}
	if !errors.Is(err, ErrCandidateArtifactCorrupted) {
		t.Errorf("expected errors.Is(err, ErrCandidateArtifactCorrupted), got %v", err)
	}
}

// TestFSCandidateArtifactStoreGetRejectsSemanticallyTamperedContent proves
// Get re-runs full verification (ValidateCandidateArtifact), not just a
// digest-filename match: flipping one field after sealing, while leaving
// everything else (including the stale outer digest) untouched, must be
// caught on read.
func TestFSCandidateArtifactStoreGetRejectsSemanticallyTamperedContent(t *testing.T) {
	root := t.TempDir()
	store, err := NewFSCandidateArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	artifact := fixtureCandidateArtifact(t)
	if err := store.Put(context.Background(), artifact); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, artifact.CandidateArtifactDigestSHA256+".json")
	var raw map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	raw["repository_domain"] = "tampered.example.com" // outer digest now stale
	tampered, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, tampered, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = store.Get(context.Background(), artifact.CandidateArtifactDigestSHA256)
	if err == nil {
		t.Fatal("expected an error for semantically tampered content")
	}
	if !errors.Is(err, ErrCandidateArtifactCorrupted) {
		t.Errorf("expected errors.Is(err, ErrCandidateArtifactCorrupted), got %v", err)
	}
}

func TestNewFSCandidateArtifactStoreRejectsSymlinkRoot(t *testing.T) {
	real := t.TempDir()
	linkParent := t.TempDir()
	link := filepath.Join(linkParent, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFSCandidateArtifactStore(link); err == nil {
		t.Error("expected a symlink root to be rejected")
	}
}

func TestNewFSCandidateArtifactStoreRejectsRelativeRoot(t *testing.T) {
	if _, err := NewFSCandidateArtifactStore("relative/path"); err == nil {
		t.Error("expected a relative root to be rejected")
	}
}

func TestNewFSCandidateArtifactStoreRejectsMissingRoot(t *testing.T) {
	if _, err := NewFSCandidateArtifactStore(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Error("expected a missing root to be rejected")
	}
}

func TestNewFSCandidateArtifactStoreRejectsFileRoot(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFSCandidateArtifactStore(file); err == nil {
		t.Error("expected a regular-file root to be rejected")
	}
}

// TestFSCandidateArtifactStoreSurvivesAfterBufferCleanup is the end-to-end
// proof of hard law 14: seal a CandidateArtifact built from a real
// ExtractSnapshot/InitializeCandidateBuffer buffer, destroy BOTH the
// snapshot and buffer directories entirely, and confirm Get still returns
// the full, byte-exact content -- proving the store made an independent,
// durable copy rather than referencing the ephemeral capture surface in
// any way.
func TestFSCandidateArtifactStoreSurvivesAfterBufferCleanup(t *testing.T) {
	repoRoot := initTestRepo(t, func(root string) {
		os.WriteFile(filepath.Join(root, "a.txt"), []byte("candidate content"), 0o644)
		runGit(t, root, "add", "-A")
		runGit(t, root, "commit", "-q", "-m", "init")
	})
	rev := runGit(t, repoRoot, "rev-parse", "HEAD")

	snapshotDir, snapshotManifest, snapshotDigest, snapshotCleanup, err := ExtractSnapshot(context.Background(), repoRoot, rev)
	if err != nil {
		t.Fatal(err)
	}
	bufferDir, bufferManifest, _, bufferCleanup, err := InitializeCandidateBuffer(snapshotDir, snapshotManifest)
	if err != nil {
		t.Fatal(err)
	}

	finalDigest, err := ManifestDigest(bufferManifest)
	if err != nil {
		t.Fatal(err)
	}
	artifact := CandidateArtifact{
		SchemaVersion:                     CandidateArtifactSchemaVersion,
		RepositoryDomain:                  "github.com/globulario/sensei",
		BaseRevision:                      rev,
		WorkspaceIdentityDigestSHA256:     zeroDigest,
		SessionDigestSHA256:               zeroDigest,
		PlanDigestSHA256:                  zeroDigest,
		PlanGeneration:                    1,
		AttemptNumber:                     1,
		InputCandidateDigestSHA256:        snapshotDigest,
		ProposedChangeDigestSHA256:        zeroDigest,
		FinalCandidateContentDigestSHA256: finalDigest,
		Manifest:                          bufferManifest,
	}
	digest, err := CandidateArtifactDigest(artifact)
	if err != nil {
		t.Fatal(err)
	}
	artifact.CandidateArtifactDigestSHA256 = digest

	storeRoot := t.TempDir()
	store, err := NewFSCandidateArtifactStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), artifact); err != nil {
		t.Fatal(err)
	}

	// Destroy the entire ephemeral capture surface.
	if err := bufferCleanup(); err != nil {
		t.Fatal(err)
	}
	if err := snapshotCleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(snapshotDir); !os.IsNotExist(err) {
		t.Fatalf("snapshotDir still exists after cleanup: err=%v", err)
	}
	if _, err := os.Stat(bufferDir); !os.IsNotExist(err) {
		t.Fatalf("bufferDir still exists after cleanup: err=%v", err)
	}

	got, err := store.Get(context.Background(), digest)
	if err != nil {
		t.Fatalf("Get failed after the ephemeral capture surface was destroyed: %v", err)
	}
	if len(got.Manifest) != 1 || string(got.Manifest[0].Content) != "candidate content" {
		t.Errorf("Get after cleanup returned unexpected content: %+v", got.Manifest)
	}
}
