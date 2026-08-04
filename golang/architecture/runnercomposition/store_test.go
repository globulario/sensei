// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// TestFSCandidateArtifactStorePutSucceedsDespiteOrphanedStagingEntries is
// review 4833229539's "unambiguous sealed-versus-failed outcome truth"
// blocker, tested at the level of the invariant Put's fix actually relies
// on: an entry left behind in .tmp under a NAME Put would never itself
// generate for a live seal (simulating what a hypothetical earlier failed
// staging-cleanup could leave) must never cause Put or Get to behave
// incorrectly for ANY digest -- proving a leftover ".tmp/*" name is truly
// inert, which is exactly why Put's Link-success branch is allowed to
// return nil unconditionally regardless of whether its own cleanup
// Remove(tmpName) succeeds.
func TestFSCandidateArtifactStorePutSucceedsDespiteOrphanedStagingEntries(t *testing.T) {
	root := t.TempDir()
	store, err := NewFSCandidateArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	artifact := fixtureCandidateArtifact(t)

	// Seal once normally, to create .tmp with its ordinary permissions.
	if err := store.Put(context.Background(), artifact); err != nil {
		t.Fatal(err)
	}

	// Simulate a leftover staging file an earlier, hypothetical failed
	// cleanup could have left behind: a hard link to the SAME sealed
	// content, sitting in .tmp under an arbitrary staging-shaped name.
	sealedPath := filepath.Join(root, artifact.CandidateArtifactDigestSHA256+".json")
	orphanPath := filepath.Join(root, ".tmp", artifact.CandidateArtifactDigestSHA256+".orphaned.tmp")
	if err := os.Link(sealedPath, orphanPath); err != nil {
		t.Fatal(err)
	}

	// A fresh Put for the SAME artifact (idempotent no-op path) must still
	// succeed cleanly with the orphan present.
	if err := store.Put(context.Background(), artifact); err != nil {
		t.Errorf("Put failed with an orphaned .tmp entry present: %v", err)
	}
	// A Put for a DIFFERENT artifact (fresh-seal path) must also succeed.
	other := fixtureCandidateArtifactWithContent(t, "unrelated content, unrelated digest\n")
	if err := store.Put(context.Background(), other); err != nil {
		t.Errorf("Put of an unrelated artifact failed with an orphaned .tmp entry present: %v", err)
	}
	// Get for both digests must still return correct content, unaffected
	// by the orphan's presence.
	got, err := store.Get(context.Background(), artifact.CandidateArtifactDigestSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if got.CandidateArtifactDigestSHA256 != artifact.CandidateArtifactDigestSHA256 {
		t.Errorf("Get returned wrong digest with an orphan present: %q", got.CandidateArtifactDigestSHA256)
	}
	if _, err := os.Stat(orphanPath); err != nil {
		t.Errorf("orphan entry unexpectedly disappeared: %v", err)
	}
}

// TestFSCandidateArtifactStoreReadSealedEntryRejectsSwapUnderConcurrentRace
// is review 4833229539's "deterministic swap-race" requirement for the
// no-follow reader: while one goroutine repeatedly seals and re-reads a
// digest via the real Put/Get API, another continuously swaps that exact
// path between a valid regular file and a symlink pointing at a DIFFERENT
// sealed artifact's file (chosen so the swap, if ever followed, would
// silently return that unrelated artifact's content instead of an error).
// The atomic O_NOFOLLOW|O_NONBLOCK open collapses "inspect" and "open"
// into one syscall, so there is no window left for a swap to land in
// between them -- every single Get across many iterations under continuous
// concurrent swapping must EITHER return the correct artifact for the
// requested digest, or fail with ErrCandidateArtifactCorrupted; it must
// never return the OTHER artifact's content, and it must never hang.
// Bounded by an overall timeout so a regression fails cleanly.
func TestFSCandidateArtifactStoreReadSealedEntryRejectsSwapUnderConcurrentRace(t *testing.T) {
	root := t.TempDir()
	store, err := NewFSCandidateArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	target := fixtureCandidateArtifact(t)
	decoy := fixtureCandidateArtifactWithContent(t, "decoy content from an unrelated sealed artifact\n")
	if err := store.Put(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), decoy); err != nil {
		t.Fatal(err)
	}

	targetPath := filepath.Join(root, target.CandidateArtifactDigestSHA256+".json")
	decoyPath := filepath.Join(root, decoy.CandidateArtifactDigestSHA256+".json")
	targetBackup := targetPath + ".backup"
	if err := os.Rename(targetPath, targetBackup); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(targetBackup, targetPath); err != nil {
		t.Fatal(err)
	}

	const iterations = 500
	done := make(chan struct{})
	swapErrs := make(chan error, 1)
	go func() {
		defer close(done)
		for i := 0; i < iterations; i++ {
			if err := os.Remove(targetPath); err != nil {
				swapErrs <- err
				return
			}
			if err := os.Symlink(decoyPath, targetPath); err != nil {
				swapErrs <- err
				return
			}
			if err := os.Remove(targetPath); err != nil {
				swapErrs <- err
				return
			}
			if err := os.Link(targetBackup, targetPath); err != nil {
				swapErrs <- err
				return
			}
		}
	}()

	readsDone := make(chan error, 1)
	go func() {
		for i := 0; i < iterations; i++ {
			got, err := store.Get(context.Background(), target.CandidateArtifactDigestSHA256)
			if err != nil {
				if errors.Is(err, ErrCandidateArtifactCorrupted) || errors.Is(err, ErrCandidateArtifactNotFound) {
					// The swap landed mid-transition: either the symlink
					// state (correctly refused) or the brief
					// removed-but-not-yet-relinked window (correctly
					// reported as absent). Neither is a leak or a hang.
					continue
				}
				readsDone <- fmt.Errorf("iteration %d: unexpected error: %w", i, err)
				return
			}
			if got.CandidateArtifactDigestSHA256 != target.CandidateArtifactDigestSHA256 {
				readsDone <- fmt.Errorf("iteration %d: Get(target) returned digest %q -- the decoy's content leaked through a followed symlink", i, got.CandidateArtifactDigestSHA256)
				return
			}
		}
		readsDone <- nil
	}()

	select {
	case err := <-readsDone:
		<-done
		select {
		case swapErr := <-swapErrs:
			t.Fatalf("swap goroutine failed: %v", swapErr)
		default:
		}
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("did not complete within 20s -- likely hung opening a swapped entry")
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

// TestFSCandidateArtifactStoreGetRejectsSymlinkToSameInodeAlias is the
// precise, non-racy reproduction of review 4833229539's second point: "a
// symlink targeting the same inode can pass the later SameFile check." A
// symlink whose target is ITSELF a hard link to the exact same inode the
// original regular file had would fool a post-open os.SameFile comparison
// -- same device, same inode -- even though what sits at the digest name
// is, structurally, a symlink, not a regular file. This needs no race or
// timing at all: the swapped state is simply set up before Get is called.
// O_NOFOLLOW rejects it unconditionally, independent of what its target
// ultimately resolves to.
func TestFSCandidateArtifactStoreGetRejectsSymlinkToSameInodeAlias(t *testing.T) {
	root := t.TempDir()
	store, err := NewFSCandidateArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	artifact := fixtureCandidateArtifact(t)
	if err := store.Put(context.Background(), artifact); err != nil {
		t.Fatal(err)
	}

	sealedPath := filepath.Join(root, artifact.CandidateArtifactDigestSHA256+".json")
	aliasPath := filepath.Join(root, "alias-same-inode.json")
	if err := os.Link(sealedPath, aliasPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(sealedPath); err != nil {
		t.Fatal(err)
	}
	// A relative target -- what os.Root actually permits (it rejects an
	// absolute symlink target outright as escaping the root, which would
	// make this reproduction pass for the wrong reason: os.Root's own
	// absolute-path containment, not the regular-file-only guard this test
	// exists to prove).
	if err := os.Symlink("alias-same-inode.json", sealedPath); err != nil {
		t.Fatal(err)
	}

	_, err = store.Get(context.Background(), artifact.CandidateArtifactDigestSHA256)
	if err == nil {
		t.Fatal("expected Get to reject a symlink whose target shares the sealed content's own inode")
	}
	if !errors.Is(err, ErrCandidateArtifactCorrupted) {
		t.Errorf("expected errors.Is(err, ErrCandidateArtifactCorrupted), got %v", err)
	}
}

// TestFSCandidateArtifactStorePutAndGetShareStableRootIdentityAcrossRename
// is review 4833317738's deterministic root-rename/replacement negative
// control: construct a store, seal an artifact, then rename the store's
// root directory away and create a REPLACEMENT directory at the original
// path with unrelated, invalid content under the same digest filename.
// Put and Get must both continue to operate on the SAME original
// directory os.Root itself tracks -- Get must never silently start
// reading whatever now sits at the store's original path, and a
// subsequent Put must land in the original directory too.
func TestFSCandidateArtifactStorePutAndGetShareStableRootIdentityAcrossRename(t *testing.T) {
	parent := t.TempDir()
	storePath := filepath.Join(parent, "store")
	if err := os.Mkdir(storePath, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := NewFSCandidateArtifactStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	artifact := fixtureCandidateArtifact(t)
	if err := store.Put(context.Background(), artifact); err != nil {
		t.Fatal(err)
	}

	// The attack: rename the store's root away, then create a replacement
	// directory at the original path with unrelated content under the
	// SAME digest filename.
	oldPath := filepath.Join(parent, "store-old")
	if err := os.Rename(storePath, oldPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(storePath, 0o755); err != nil {
		t.Fatal(err)
	}
	replacementDigestFile := filepath.Join(storePath, artifact.CandidateArtifactDigestSHA256+".json")
	if err := os.WriteFile(replacementDigestFile, []byte(`{"replacement":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Get must still see the ORIGINAL artifact -- the same directory Put
	// sealed into -- never the replacement's unrelated (and invalid)
	// content.
	got, err := store.Get(context.Background(), artifact.CandidateArtifactDigestSHA256)
	if err != nil {
		t.Fatalf("Get failed after root rename+replacement: %v", err)
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
		t.Errorf("Get returned content diverging from the originally sealed artifact after rename+replacement:\ngot:  %s\nwant: %s", gotJSON, wantJSON)
	}

	// A second Put for a NEW artifact must also land in the ORIGINAL
	// directory, not the replacement.
	other := fixtureCandidateArtifactWithContent(t, "sealed after the rename, still in the original directory\n")
	if err := store.Put(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(oldPath, other.CandidateArtifactDigestSHA256+".json")); err != nil {
		t.Errorf("expected the post-rename Put to land in the ORIGINAL directory (now at %q): %v", oldPath, err)
	}
	if _, err := os.Stat(filepath.Join(storePath, other.CandidateArtifactDigestSHA256+".json")); !os.IsNotExist(err) {
		t.Errorf("post-rename Put leaked into the REPLACEMENT directory: err=%v", err)
	}
}

// TestFSCandidateArtifactStorePutAuxiliaryFileWritesAndReplaces covers the
// ordinary case (no existing entry) and the legitimate replace case (an
// existing regular file, e.g. a second run reproducing the same candidate
// digest with fresh lineage receipt content) -- both must succeed, and no
// staging file must be left behind.
func TestFSCandidateArtifactStorePutAuxiliaryFileWritesAndReplaces(t *testing.T) {
	root := t.TempDir()
	store, err := NewFSCandidateArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.PutAuxiliaryFile(ctx, "x.lineage.json", []byte("first")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "x.lineage.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first" {
		t.Fatalf("content = %q, want %q", got, "first")
	}
	if err := store.PutAuxiliaryFile(ctx, "x.lineage.json", []byte("second")); err != nil {
		t.Fatalf("replace write: %v", err)
	}
	got, err = os.ReadFile(filepath.Join(root, "x.lineage.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Fatalf("content after replace = %q, want %q", got, "second")
	}
	// .tmp (the staging directory) is expected to remain, matching Put's
	// own established behavior -- only assert no stray "*.tmp" staging
	// FILE was left behind at the top level.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "x.lineage.json" && e.Name() != ".tmp" {
			t.Fatalf("root contains unexpected entry: %v", e.Name())
		}
	}
	tmpEntries, err := os.ReadDir(filepath.Join(root, ".tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tmpEntries) != 0 {
		t.Fatalf(".tmp contains leftover staging entries: %v", tmpEntries)
	}
}

// TestFSCandidateArtifactStorePutAuxiliaryFileRefusesExistingSymlink is the
// direct regression test for a live review finding: an existing symlink at
// the target name (planted by another process, or a pre-existing
// attacker-controlled entry) must be refused, never followed or
// transparently overwritten.
func TestFSCandidateArtifactStorePutAuxiliaryFileRefusesExistingSymlink(t *testing.T) {
	root := t.TempDir()
	store, err := NewFSCandidateArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	outsideTarget := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outsideTarget, []byte("do not touch"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(root, "x.lineage.json")
	if err := os.Symlink(outsideTarget, linkPath); err != nil {
		t.Skipf("symlinks unsupported in this environment: %v", err)
	}

	if err := store.PutAuxiliaryFile(context.Background(), "x.lineage.json", []byte("attempted overwrite")); err == nil {
		t.Fatal("expected an error writing through an existing symlink")
	}

	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink at the target name was replaced instead of refused")
	}
	outsideContent, err := os.ReadFile(outsideTarget)
	if err != nil {
		t.Fatal(err)
	}
	if string(outsideContent) != "do not touch" {
		t.Fatalf("symlink target was written through: %s", outsideContent)
	}
}

// TestFSCandidateArtifactStorePutAuxiliaryFileRejectsNestedPath covers the
// flat-filename requirement: name must never be usable to escape or nest
// beyond a single entry directly inside root.
func TestFSCandidateArtifactStorePutAuxiliaryFileRejectsNestedPath(t *testing.T) {
	root := t.TempDir()
	store, err := NewFSCandidateArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, name := range []string{"nested/x.json", "../x.json", "", ".", ".."} {
		if err := store.PutAuxiliaryFile(ctx, name, []byte("x")); err == nil {
			t.Fatalf("name %q: expected an error for a non-flat filename", name)
		}
	}
}

// TestFSCandidateArtifactStorePutAuxiliaryFileRejectsSealedCandidateNamespace
// is the direct regression test for a live review finding: PutAuxiliaryFile's
// replace-capable commit must not be usable to overwrite an already-sealed
// candidate artifact -- Put's create-only, no-clobber contract is the whole
// point of this store's immutability guarantee, and a name shaped exactly
// like Put's own "<64-lowercase-hex-digest>.json" filename must be refused,
// both when nothing is sealed under that name yet and when something
// already is.
func TestFSCandidateArtifactStorePutAuxiliaryFileRejectsSealedCandidateNamespace(t *testing.T) {
	root := t.TempDir()
	store, err := NewFSCandidateArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	artifact := fixtureCandidateArtifact(t)
	if err := store.Put(ctx, artifact); err != nil {
		t.Fatal(err)
	}
	sealedName := artifact.CandidateArtifactDigestSHA256 + ".json"

	if err := store.PutAuxiliaryFile(ctx, sealedName, []byte("attempted overwrite")); err == nil {
		t.Fatal("expected an error writing to a sealed-candidate-shaped name that is already sealed")
	}
	got, err := store.Get(ctx, artifact.CandidateArtifactDigestSHA256)
	if err != nil {
		t.Fatalf("Get after refused PutAuxiliaryFile: %v", err)
	}
	if got.CandidateArtifactDigestSHA256 != artifact.CandidateArtifactDigestSHA256 {
		t.Fatal("sealed candidate was corrupted despite PutAuxiliaryFile being refused")
	}

	// Also refused for a digest with nothing sealed under it yet -- the
	// namespace itself is reserved, not merely "don't overwrite existing
	// content."
	const unsealedDigest = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	if err := store.PutAuxiliaryFile(ctx, unsealedDigest+".json", []byte("x")); err == nil {
		t.Fatal("expected an error writing to a sealed-candidate-shaped name even with nothing sealed under it yet")
	}
}

// TestFSCandidateArtifactStorePutAuxiliaryFileSharesStableRootIdentityAcrossRename
// is the direct regression test for the live review finding this method
// exists to close: a candidate sealed via Put, followed by a rename of the
// store's root path and a replacement directory created at the original
// path, must still have its auxiliary file (e.g. an admission lineage
// bundle) land in the SAME physical directory the candidate was actually
// sealed into -- never split into the replacement directory that now
// happens to sit at the original path string.
func TestFSCandidateArtifactStorePutAuxiliaryFileSharesStableRootIdentityAcrossRename(t *testing.T) {
	parent := t.TempDir()
	storePath := filepath.Join(parent, "store")
	if err := os.Mkdir(storePath, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := NewFSCandidateArtifactStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	artifact := fixtureCandidateArtifact(t)
	if err := store.Put(ctx, artifact); err != nil {
		t.Fatal(err)
	}

	oldPath := filepath.Join(parent, "store-old")
	if err := os.Rename(storePath, oldPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(storePath, 0o755); err != nil {
		t.Fatal(err)
	}

	lineageName := artifact.CandidateArtifactDigestSHA256 + ".lineage.json"
	if err := store.PutAuxiliaryFile(ctx, lineageName, []byte(`{"lineage":true}`)); err != nil {
		t.Fatalf("PutAuxiliaryFile after root rename+replacement: %v", err)
	}

	if _, err := os.Stat(filepath.Join(oldPath, lineageName)); err != nil {
		t.Errorf("expected PutAuxiliaryFile to land in the ORIGINAL directory (now at %q), alongside the sealed candidate: %v", oldPath, err)
	}
	if _, err := os.Stat(filepath.Join(storePath, lineageName)); !os.IsNotExist(err) {
		t.Errorf("PutAuxiliaryFile leaked into the REPLACEMENT directory: err=%v", err)
	}
}

// TestFSCandidateArtifactStoreVerifyRootIdentity_PassesForUnchangedPath
// covers the ordinary case: nothing has happened to root since
// construction, so VerifyRootIdentity(root) must succeed.
func TestFSCandidateArtifactStoreVerifyRootIdentity_PassesForUnchangedPath(t *testing.T) {
	root := t.TempDir()
	store, err := NewFSCandidateArtifactStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.VerifyRootIdentity(root); err != nil {
		t.Fatalf("VerifyRootIdentity(unchanged root): %v", err)
	}
}

// TestFSCandidateArtifactStoreVerifyRootIdentity_DetectsRenameAndReplacement
// is the direct regression test for a live review finding: after the
// store's original root directory is renamed away and a DIFFERENT
// directory is created at the original path, VerifyRootIdentity(the
// original path) must fail -- that path string no longer identifies the
// directory this store actually writes through, exactly the situation
// that made a caller-reported candidate_path/lineage_path untrustworthy.
func TestFSCandidateArtifactStoreVerifyRootIdentity_DetectsRenameAndReplacement(t *testing.T) {
	parent := t.TempDir()
	storePath := filepath.Join(parent, "store")
	if err := os.Mkdir(storePath, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := NewFSCandidateArtifactStore(storePath)
	if err != nil {
		t.Fatal(err)
	}

	oldPath := filepath.Join(parent, "store-old")
	if err := os.Rename(storePath, oldPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(storePath, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := store.VerifyRootIdentity(storePath); err == nil {
		t.Fatal("expected an error: storePath now names a REPLACEMENT directory, not the one this store was constructed against")
	}
	// The original path, however, still correctly identifies the store's
	// real root.
	if err := store.VerifyRootIdentity(oldPath); err != nil {
		t.Fatalf("VerifyRootIdentity(original path after rename): %v", err)
	}
}

// TestFSCandidateArtifactStoreVerifyRootIdentity_FailsForMissingPath
// covers the companion case: the original directory renamed away with
// nothing put back at the original path at all.
func TestFSCandidateArtifactStoreVerifyRootIdentity_FailsForMissingPath(t *testing.T) {
	parent := t.TempDir()
	storePath := filepath.Join(parent, "store")
	if err := os.Mkdir(storePath, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := NewFSCandidateArtifactStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(storePath, filepath.Join(parent, "store-moved")); err != nil {
		t.Fatal(err)
	}
	if err := store.VerifyRootIdentity(storePath); err == nil {
		t.Fatal("expected an error: storePath no longer exists")
	}
}
