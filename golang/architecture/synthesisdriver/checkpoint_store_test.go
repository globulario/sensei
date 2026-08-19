// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// Every store rule is asserted against BOTH implementations. The in-memory
// store exists for deterministic driver tests, so it has to enforce the same
// contract as the adapter the CLI actually ships — otherwise a green driver
// test would prove nothing about production durability.
func eachCheckpointStore(t *testing.T, run func(t *testing.T, store CheckpointStore)) {
	t.Helper()
	t.Run("memory", func(t *testing.T) {
		run(t, NewMemoryCheckpointStore())
	})
	t.Run("filesystem", func(t *testing.T) {
		store, err := NewFSCheckpointStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		run(t, store)
	})
}

// nextCheckpoint links a successor onto a checkpoint, so tests build real
// chains rather than unrelated documents that happen to have rising sequences.
func nextCheckpoint(t *testing.T, previous Checkpoint, phase synthesis.Phase) Checkpoint {
	t.Helper()
	next := checkpointFixture(t, phase)
	next.Sequence = previous.Sequence + 1
	digest := previous.CheckpointDigestSHA256
	next.PreviousCheckpointDigestSHA256 = &digest
	next.StepsConsumed = previous.StepsConsumed + 1
	next.CheckpointID = "o7.checkpoint.test." + string(rune('0'+next.Sequence))

	finalized, err := FinalizeCheckpoint(next)
	if err != nil {
		t.Fatalf("successor checkpoint must finalize: %v", err)
	}
	return finalized
}

func TestCheckpointStoreRoundTrip(t *testing.T) {
	eachCheckpointStore(t, func(t *testing.T, store CheckpointStore) {
		ctx := context.Background()
		checkpoint := checkpointFixture(t, synthesis.PhasePlanning)

		if err := store.Save(ctx, checkpoint); err != nil {
			t.Fatalf("save: %v", err)
		}
		loaded, err := store.Load(ctx, checkpoint.CheckpointDigestSHA256)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if loaded.CheckpointDigestSHA256 != checkpoint.CheckpointDigestSHA256 {
			t.Fatalf("loaded a different checkpoint: %q", loaded.CheckpointDigestSHA256)
		}
		latest, err := store.Latest(ctx)
		if err != nil {
			t.Fatalf("latest: %v", err)
		}
		if latest.CheckpointDigestSHA256 != checkpoint.CheckpointDigestSHA256 {
			t.Fatal("latest does not point at the only checkpoint saved")
		}
	})
}

// The history is append-only: saving a newer boundary must never destroy the
// older one it continues from. This is what makes a crash mid-history
// recoverable instead of fatal.
func TestCheckpointStoreHistoryIsAppendOnly(t *testing.T) {
	eachCheckpointStore(t, func(t *testing.T, store CheckpointStore) {
		ctx := context.Background()
		first := checkpointFixture(t, synthesis.PhaseCreated)
		if err := store.Save(ctx, first); err != nil {
			t.Fatal(err)
		}
		second := nextCheckpoint(t, first, synthesis.PhasePlanning)
		if err := store.Save(ctx, second); err != nil {
			t.Fatal(err)
		}

		recovered, err := store.Load(ctx, first.CheckpointDigestSHA256)
		if err != nil {
			t.Fatalf("the older checkpoint must remain loadable after a newer one is saved: %v", err)
		}
		if recovered.Sequence != first.Sequence {
			t.Fatalf("older checkpoint came back as sequence %d", recovered.Sequence)
		}
		latest, err := store.Latest(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if latest.CheckpointDigestSHA256 != second.CheckpointDigestSHA256 {
			t.Fatal("latest did not advance to the newer checkpoint")
		}
		if got := *latest.PreviousCheckpointDigestSHA256; got != first.CheckpointDigestSHA256 {
			t.Fatalf("the chain link was not preserved: %q", got)
		}
	})
}

// A stale writer arriving late must not drag the pointer backwards: both
// checkpoints stay readable, but latest keeps naming the newest boundary.
func TestCheckpointStoreLatestNeverRewinds(t *testing.T) {
	eachCheckpointStore(t, func(t *testing.T, store CheckpointStore) {
		ctx := context.Background()
		first := checkpointFixture(t, synthesis.PhaseCreated)
		second := nextCheckpoint(t, first, synthesis.PhasePlanning)

		if err := store.Save(ctx, second); err != nil {
			t.Fatal(err)
		}
		if err := store.Save(ctx, first); err != nil {
			t.Fatalf("saving an older checkpoint must still succeed: %v", err)
		}

		latest, err := store.Latest(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if latest.CheckpointDigestSHA256 != second.CheckpointDigestSHA256 {
			t.Fatal("a late save of an older checkpoint rewound the latest pointer")
		}
		if _, err := store.Load(ctx, first.CheckpointDigestSHA256); err != nil {
			t.Fatalf("the older checkpoint must still be loadable: %v", err)
		}
	})
}

// Saving the same checkpoint twice is an idempotent no-op — a retried write
// after an ambiguous crash must not be an error or a duplicate.
func TestCheckpointStoreSaveIsIdempotent(t *testing.T) {
	eachCheckpointStore(t, func(t *testing.T, store CheckpointStore) {
		ctx := context.Background()
		checkpoint := checkpointFixture(t, synthesis.PhasePlanned)
		if err := store.Save(ctx, checkpoint); err != nil {
			t.Fatal(err)
		}
		if err := store.Save(ctx, checkpoint); err != nil {
			t.Fatalf("re-saving identical content must be a no-op: %v", err)
		}
		if _, err := store.Load(ctx, checkpoint.CheckpointDigestSHA256); err != nil {
			t.Fatal(err)
		}
	})
}

// Nothing invalid is ever persisted, so an entry that exists is an entry that
// was valid when written.
func TestCheckpointStoreRefusesInvalidCheckpoint(t *testing.T) {
	eachCheckpointStore(t, func(t *testing.T, store CheckpointStore) {
		ctx := context.Background()
		checkpoint := checkpointFixture(t, synthesis.PhaseCreated)
		checkpoint.StepsConsumed = checkpoint.MaxSteps + 1 // digest no longer matches either

		if err := store.Save(ctx, checkpoint); err == nil {
			t.Fatal("an invalid checkpoint was persisted")
		}
		if _, err := store.Latest(ctx); !errors.Is(err, ErrCheckpointNotFound) {
			t.Fatalf("a refused save must leave the store empty, got %v", err)
		}
	})
}

func TestCheckpointStoreMissingEntriesAreNotFound(t *testing.T) {
	eachCheckpointStore(t, func(t *testing.T, store CheckpointStore) {
		ctx := context.Background()
		if _, err := store.Latest(ctx); !errors.Is(err, ErrCheckpointNotFound) {
			t.Fatalf("an empty store must report not-found, got %v", err)
		}
		if _, err := store.Load(ctx, strings.Repeat("a", 64)); !errors.Is(err, ErrCheckpointNotFound) {
			t.Fatalf("an absent digest must report not-found, got %v", err)
		}
	})
}

// A crash between sealing the content and moving the pointer must leave a
// recoverable older boundary, not a pointer into nothing. The content of the
// newer checkpoint is on disk and loadable; latest still resolves to the
// previous one.
func TestFSCheckpointStoreCrashBeforePointerUpdateLeavesOlderPointer(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewFSCheckpointStore(root)
	if err != nil {
		t.Fatal(err)
	}
	first := checkpointFixture(t, synthesis.PhaseCreated)
	if err := store.Save(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := nextCheckpoint(t, first, synthesis.PhasePlanning)

	// Simulate the crash window exactly: the content-addressed write landed,
	// the pointer move did not.
	marshaled, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, second.CheckpointDigestSHA256+".json"), marshaled, 0o644); err != nil {
		t.Fatal(err)
	}

	latest, err := store.Latest(ctx)
	if err != nil {
		t.Fatalf("latest must still resolve after an interrupted save: %v", err)
	}
	if latest.CheckpointDigestSHA256 != first.CheckpointDigestSHA256 {
		t.Fatal("latest should still name the older, fully committed checkpoint")
	}
	if _, err := store.Load(ctx, second.CheckpointDigestSHA256); err != nil {
		t.Fatalf("the durably written newer checkpoint must still be loadable: %v", err)
	}
}

// An entry damaged after it was sealed must be refused, not handed back as if
// it were trustworthy. not-found and corrupted stay distinguishable.
func TestFSCheckpointStoreRefusesDamagedEntries(t *testing.T) {
	ctx := context.Background()

	t.Run("unknown field injected after sealing", func(t *testing.T) {
		root := t.TempDir()
		store, err := NewFSCheckpointStore(root)
		if err != nil {
			t.Fatal(err)
		}
		checkpoint := checkpointFixture(t, synthesis.PhaseCreated)
		if err := store.Save(ctx, checkpoint); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, checkpoint.CheckpointDigestSHA256+".json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var document map[string]any
		if err := json.Unmarshal(raw, &document); err != nil {
			t.Fatal(err)
		}
		document["skip_drift_check"] = true
		tampered, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, tampered, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(ctx, checkpoint.CheckpointDigestSHA256); !errors.Is(err, ErrCheckpointCorrupted) {
			t.Fatalf("a tampered entry must be refused as corrupted, got %v", err)
		}
	})

	t.Run("entry no longer matches the key it is stored under", func(t *testing.T) {
		root := t.TempDir()
		store, err := NewFSCheckpointStore(root)
		if err != nil {
			t.Fatal(err)
		}
		checkpoint := checkpointFixture(t, synthesis.PhaseCreated)
		marshaled, err := json.Marshal(checkpoint)
		if err != nil {
			t.Fatal(err)
		}
		wrongKey := strings.Repeat("b", 64)
		if err := os.WriteFile(filepath.Join(root, wrongKey+".json"), marshaled, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(ctx, wrongKey); !errors.Is(err, ErrCheckpointCorrupted) {
			t.Fatalf("an entry under the wrong key must be refused, got %v", err)
		}
	})

	t.Run("truncated entry", func(t *testing.T) {
		root := t.TempDir()
		store, err := NewFSCheckpointStore(root)
		if err != nil {
			t.Fatal(err)
		}
		checkpoint := checkpointFixture(t, synthesis.PhaseCreated)
		if err := store.Save(ctx, checkpoint); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, checkpoint.CheckpointDigestSHA256+".json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw[:len(raw)/2], 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(ctx, checkpoint.CheckpointDigestSHA256); !errors.Is(err, ErrCheckpointCorrupted) {
			t.Fatalf("a truncated entry must be refused, got %v", err)
		}
	})

	t.Run("pointer holding something that is not a digest", func(t *testing.T) {
		root := t.TempDir()
		store, err := NewFSCheckpointStore(root)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, latestPointerName), []byte("../../etc/passwd\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Latest(ctx); !errors.Is(err, ErrCheckpointCorrupted) {
			t.Fatalf("a pointer that is not a digest must be refused, got %v", err)
		}
	})
}

func TestNewFSCheckpointStoreRequiresAnExistingAbsoluteDirectory(t *testing.T) {
	if _, err := NewFSCheckpointStore("relative/path"); err == nil {
		t.Fatal("a relative root was accepted")
	}
	if _, err := NewFSCheckpointStore(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("a missing root was accepted")
	}
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFSCheckpointStore(file); err == nil {
		t.Fatal("a regular file was accepted as a store root")
	}
}
