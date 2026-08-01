// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fixtureCandidateArtifactWithContent builds a fixture artifact whose
// first manifest entry's content is distinct, so its digest is distinct
// from fixtureCandidateArtifact's -- used wherever a test needs two
// artifacts under two different digests in the same store.
func fixtureCandidateArtifactWithContent(t *testing.T, content string) CandidateArtifact {
	t.Helper()
	entries := fixtureManifestEntries(t)
	entries[0].Content = []byte(content)
	entries[0].ContentDigestSHA256 = sha256Hex(entries[0].Content)
	a := fixtureCandidateArtifact(t)
	a.Manifest = entries
	finalDigest, err := ManifestDigest(entries)
	if err != nil {
		t.Fatal(err)
	}
	a.FinalCandidateContentDigestSHA256 = finalDigest
	digest, err := CandidateArtifactDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	a.CandidateArtifactDigestSHA256 = digest
	return a
}

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

	second := fixtureCandidateArtifactWithContent(t, "different content so this is a distinct digest\n")

	if err := store.Put(context.Background(), second); err == nil {
		t.Fatal("expected Put to fail once root is read-only (link cannot create a new entry)")
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

// TestFSCandidateArtifactStoreLinkCommitIsAtomicNoClobberUnderConcurrency
// is the deterministic race proof review 4833186007 required: two
// goroutines each stage DIFFERENT payloads under their own names, then a
// commit barrier releases both to call Root.Link into the SAME final name
// as close to simultaneously as possible -- the exact primitive Put's
// commit step relies on. Exactly one Link call may succeed; the loser must
// fail with an EEXIST-class error, and the final content on disk must be
// exactly the winner's, never a mixture, never silently replaced.
func TestFSCandidateArtifactStoreLinkCommitIsAtomicNoClobberUnderConcurrency(t *testing.T) {
	root := t.TempDir()
	r, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if err := r.WriteFile("a", []byte("payload A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.WriteFile("b", []byte("payload B"), 0o644); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	race := func(src string) {
		<-start
		results <- r.Link(src, "final")
	}
	go race("a")
	go race("b")
	close(start)

	err1 := <-results
	err2 := <-results
	successes := 0
	if err1 == nil {
		successes++
	}
	if err2 == nil {
		successes++
	}
	if successes != 1 {
		t.Fatalf("expected exactly one of two concurrent Link calls into the same name to succeed, got %d (err1=%v, err2=%v)", successes, err1, err2)
	}
	loserErr := err1
	if err1 == nil {
		loserErr = err2
	}
	if !os.IsExist(loserErr) {
		t.Errorf("expected the losing Link call's error to satisfy os.IsExist, got %v", loserErr)
	}

	final, err := r.ReadFile("final")
	if err != nil {
		t.Fatal(err)
	}
	if string(final) != "payload A" && string(final) != "payload B" {
		t.Fatalf("final content %q is neither racer's payload -- data corruption", final)
	}
}

// TestFSCandidateArtifactStorePutConcurrentIdenticalPutsAreSafe proves the
// realistic "two store instances (or processes) addressing the same root"
// scenario the review named: two independently-constructed stores, both
// sealing the SAME artifact concurrently, must both succeed (idempotent),
// and the final content must be exactly the artifact's own canonical
// bytes.
func TestFSCandidateArtifactStorePutConcurrentIdenticalPutsAreSafe(t *testing.T) {
	root := t.TempDir()
	storeA, err := NewFSCandidateArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	storeB, err := NewFSCandidateArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	artifact := fixtureCandidateArtifact(t)

	start := make(chan struct{})
	results := make(chan error, 2)
	go func() { <-start; results <- storeA.Put(context.Background(), artifact) }()
	go func() { <-start; results <- storeB.Put(context.Background(), artifact) }()
	close(start)

	if err := <-results; err != nil {
		t.Errorf("concurrent Put A failed: %v", err)
	}
	if err := <-results; err != nil {
		t.Errorf("concurrent Put B failed: %v", err)
	}

	got, err := storeA.Get(context.Background(), artifact.CandidateArtifactDigestSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if got.CandidateArtifactDigestSHA256 != artifact.CandidateArtifactDigestSHA256 {
		t.Errorf("Get after concurrent identical Puts returned digest %q, want %q", got.CandidateArtifactDigestSHA256, artifact.CandidateArtifactDigestSHA256)
	}
}

// TestFSCandidateArtifactStorePutJoinsStagingWriteAndCleanupErrors proves
// review 4833186007's "propagate or join staging-cleanup errors rather
// than discarding Remove(tmpName) failures": makes .tmp itself read-only
// after it has already been created by a bootstrap Put, so a second Put's
// staged-content write AND its own cleanup Remove both fail, and confirms
// both failures are present in the single returned error -- not just the
// first one, with the second silently swallowed.
func TestFSCandidateArtifactStorePutJoinsStagingWriteAndCleanupErrors(t *testing.T) {
	root := t.TempDir()
	store, err := NewFSCandidateArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := fixtureCandidateArtifact(t)
	if err := store.Put(context.Background(), bootstrap); err != nil {
		t.Fatal(err) // bootstraps .tmp with normal permissions.
	}

	tmpDir := filepath.Join(root, ".tmp")
	if err := os.Chmod(tmpDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(tmpDir, 0o755) })

	second := fixtureCandidateArtifactWithContent(t, "distinct content for a distinct digest\n")
	err = store.Put(context.Background(), second)
	if err == nil {
		t.Fatal("expected Put to fail once .tmp is read-only")
	}
	if !strings.Contains(err.Error(), "write staged content") {
		t.Errorf("expected the primary staging-write failure in the error, got %v", err)
	}
	if !strings.Contains(err.Error(), "remove staged content") {
		t.Errorf("expected the cleanup failure to be joined (not discarded) in the error, got %v", err)
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

// TestFSCandidateArtifactStoreGetRejectsInjectedUnknownProperty is review
// 4833186007's concrete failure scenario, reproduced exactly: seal a valid
// artifact, add an unknown property to the raw stored JSON while leaving
// every governed field untouched, and confirm Get refuses it. Before the
// fix, json.Unmarshal silently dropped the unknown field before
// ValidateCandidateArtifact ever re-marshaled and validated the clean
// struct, so this would have passed.
func TestFSCandidateArtifactStoreGetRejectsInjectedUnknownProperty(t *testing.T) {
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
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	raw["tampered_extension"] = true
	tampered, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, tampered, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = store.Get(context.Background(), artifact.CandidateArtifactDigestSHA256)
	if err == nil {
		t.Fatal("expected Get to reject a sealed file carrying an unknown property")
	}
	if !errors.Is(err, ErrCandidateArtifactCorrupted) {
		t.Errorf("expected errors.Is(err, ErrCandidateArtifactCorrupted), got %v", err)
	}
}

// TestFSCandidateArtifactStoreGetRejectsSymlinkAlias is diagnostic for the
// no-follow fix specifically -- constructed so the symlink's target, if
// merely followed and read, would pass every content and digest check:
// the target holds fully valid content whose own recomputed
// CandidateArtifactDigestSHA256 equals the digest being requested. Only
// the "the digest entry itself must be a regular file" check catches this.
func TestFSCandidateArtifactStoreGetRejectsSymlinkAlias(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(root, "real.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.json", filepath.Join(root, artifact.CandidateArtifactDigestSHA256+".json")); err != nil {
		t.Fatal(err)
	}

	_, err = store.Get(context.Background(), artifact.CandidateArtifactDigestSHA256)
	if err == nil {
		t.Fatal("expected Get to reject a digest-named entry that is a symlink, even one aliasing otherwise fully valid content")
	}
	if !errors.Is(err, ErrCandidateArtifactCorrupted) {
		t.Errorf("expected errors.Is(err, ErrCandidateArtifactCorrupted), got %v", err)
	}
}

// TestFSCandidateArtifactStoreGetRejectsFIFOWithoutHanging proves a
// digest-named FIFO is refused rather than blocking Get indefinitely --
// os.Root.Open on a FIFO opened for reading blocks until a writer opens
// it, which the no-follow reader must never reach. Bounded by a timeout so
// a regression fails the test cleanly instead of hanging the suite,
// matching this package's established FIFO-hang-prevention pattern
// (TestBuildManifestRejectsFIFOWithoutHanging).
func TestFSCandidateArtifactStoreGetRejectsFIFOWithoutHanging(t *testing.T) {
	root := t.TempDir()
	store, err := NewFSCandidateArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	if err := syscall.Mkfifo(filepath.Join(root, digest+".json"), 0o644); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := store.Get(context.Background(), digest)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error for a digest-named FIFO")
		}
		if !errors.Is(err, ErrCandidateArtifactCorrupted) {
			t.Errorf("expected errors.Is(err, ErrCandidateArtifactCorrupted), got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Get did not return within 5s -- likely blocked opening a FIFO")
	}
}

// TestFSCandidateArtifactStorePutRejectsSymlinkConflictingEntry is Get's
// symlink-alias rejection's Put-side counterpart: a pre-existing symlink
// at the digest name must be treated as an unverifiable conflicting entry,
// not silently tolerated because Link's EEXIST fallback happens to read
// through it successfully.
func TestFSCandidateArtifactStorePutRejectsSymlinkConflictingEntry(t *testing.T) {
	root := t.TempDir()
	store, err := NewFSCandidateArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	artifact := fixtureCandidateArtifact(t)
	if err := os.WriteFile(filepath.Join(root, "real.json"), []byte("irrelevant"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.json", filepath.Join(root, artifact.CandidateArtifactDigestSHA256+".json")); err != nil {
		t.Fatal(err)
	}

	if err := store.Put(context.Background(), artifact); err == nil {
		t.Error("expected Put to refuse to seal over a digest-named entry that is a symlink")
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
