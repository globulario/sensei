// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

var (
	// ErrCheckpointNotFound reports that nothing is stored under the
	// requested digest, or that the store holds no checkpoint at all.
	ErrCheckpointNotFound = errors.New("synthesisdriver: checkpoint not found")

	// ErrCheckpointCorrupted reports that something IS stored under the
	// requested key but fails verification — tampered, truncated, or no
	// longer the checkpoint that key names. It is deliberately distinct from
	// not-found: "never written" and "written and since damaged" are
	// different facts and a resume must not treat them the same way.
	ErrCheckpointCorrupted = errors.New("synthesisdriver: checkpoint corrupted")
)

// CheckpointStore is O7's injected durability boundary.
//
// The orchestration owner never learns where checkpoints live: the CLI
// composes a filesystem adapter over whatever task directory layout it owns,
// and deterministic tests compose the in-memory implementation. O7 itself only
// saves and loads.
//
// The history is append-only. Save never destroys an earlier checkpoint, so a
// crash mid-history leaves an older recoverable boundary rather than a
// corrupted one.
type CheckpointStore interface {
	// Save validates the checkpoint and seals it under its own digest.
	//
	// Sealing is content-addressed and no-clobber: saving the identical
	// checkpoint twice is an idempotent no-op, and different content under an
	// occupied digest is refused rather than silently overwritten. The latest
	// pointer advances only after the content itself is durable, and only for
	// a checkpoint that is actually newer.
	//
	// A nil error is an unconditional promise that the checkpoint is durable
	// and retrievable by Load.
	Save(ctx context.Context, checkpoint Checkpoint) error

	// Load retrieves the checkpoint sealed under digestSHA256, verifying it
	// before returning it. Returns an error satisfying
	// errors.Is(err, ErrCheckpointNotFound) when nothing is sealed there, or
	// ErrCheckpointCorrupted when something is but fails verification.
	Load(ctx context.Context, digestSHA256 string) (Checkpoint, error)

	// Latest returns the newest durable checkpoint, or an error satisfying
	// errors.Is(err, ErrCheckpointNotFound) when the store is empty.
	Latest(ctx context.Context) (Checkpoint, error)
}

// MemoryCheckpointStore is the deterministic in-memory implementation used by
// contract tests. It enforces exactly the same append-only, no-clobber rules
// as the filesystem adapter, so a test proving a rule here is proving the rule
// the production adapter also has to satisfy.
type MemoryCheckpointStore struct {
	mu      sync.Mutex
	entries map[string][]byte
	latest  string
}

func NewMemoryCheckpointStore() *MemoryCheckpointStore {
	return &MemoryCheckpointStore{entries: map[string][]byte{}}
}

func (s *MemoryCheckpointStore) Save(ctx context.Context, checkpoint Checkpoint) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	marshaled, digest, err := marshalCheckpointForStore(checkpoint)
	if err != nil {
		return fmt.Errorf("CheckpointStore.Save: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, occupied := s.entries[digest]; occupied {
		// Content-addressed: identical bytes are the same checkpoint, so
		// re-saving is a no-op. Different bytes under one digest means the
		// stored entry is not what the digest names.
		if string(existing) != string(marshaled) {
			return fmt.Errorf("CheckpointStore.Save: %w: digest %s already holds different content", ErrCheckpointCorrupted, digest)
		}
	} else {
		s.entries[digest] = marshaled
	}

	advance, err := s.shouldAdvanceLatestLocked(checkpoint)
	if err != nil {
		return err
	}
	if advance {
		s.latest = digest
	}
	return nil
}

// shouldAdvanceLatestLocked decides whether this checkpoint becomes the new
// latest. Saving an older checkpoint after a newer one leaves both readable
// but does not rewind the pointer — the history is append-only, and a stale
// writer must not drag a resumed session backwards.
func (s *MemoryCheckpointStore) shouldAdvanceLatestLocked(checkpoint Checkpoint) (bool, error) {
	if s.latest == "" {
		return true, nil
	}
	current, err := decodeCheckpointFromStore(s.entries[s.latest], s.latest)
	if err != nil {
		return false, fmt.Errorf("CheckpointStore.Save: read current latest: %w", err)
	}
	return checkpoint.Sequence > current.Sequence, nil
}

func (s *MemoryCheckpointStore) Load(ctx context.Context, digestSHA256 string) (Checkpoint, error) {
	if err := ctx.Err(); err != nil {
		return Checkpoint{}, err
	}
	s.mu.Lock()
	stored, ok := s.entries[digestSHA256]
	s.mu.Unlock()
	if !ok {
		return Checkpoint{}, fmt.Errorf("CheckpointStore.Load: %w: %s", ErrCheckpointNotFound, digestSHA256)
	}
	return decodeCheckpointFromStore(stored, digestSHA256)
}

func (s *MemoryCheckpointStore) Latest(ctx context.Context) (Checkpoint, error) {
	if err := ctx.Err(); err != nil {
		return Checkpoint{}, err
	}
	s.mu.Lock()
	latest := s.latest
	s.mu.Unlock()
	if latest == "" {
		return Checkpoint{}, fmt.Errorf("CheckpointStore.Latest: %w", ErrCheckpointNotFound)
	}
	return s.Load(ctx, latest)
}

// marshalCheckpointForStore validates before serializing. Nothing that fails
// ValidateCheckpoint is ever persisted, so a store entry that exists is an
// entry that was valid when written.
func marshalCheckpointForStore(checkpoint Checkpoint) ([]byte, string, error) {
	checkpoint = NormalizeCheckpoint(checkpoint)
	if err := ValidateCheckpoint(checkpoint); err != nil {
		return nil, "", err
	}
	marshaled, err := json.Marshal(checkpoint)
	if err != nil {
		return nil, "", fmt.Errorf("marshal: %w", err)
	}
	return marshaled, checkpoint.CheckpointDigestSHA256, nil
}

// decodeCheckpointFromStore verifies a stored entry in the order that closes
// the gaps: the RAW bytes go through the closed schema FIRST, because
// json.Unmarshal silently drops an unknown field before additionalProperties
// could ever object to it. Then it decodes strictly, revalidates semantically,
// and finally confirms the entry is the checkpoint the key names.
func decodeCheckpointFromStore(stored []byte, digestSHA256 string) (Checkpoint, error) {
	if err := ValidateCheckpointDocument(stored); err != nil {
		return Checkpoint{}, fmt.Errorf("%w: schema: %w", ErrCheckpointCorrupted, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(stored))
	decoder.DisallowUnknownFields()
	var checkpoint Checkpoint
	if err := decoder.Decode(&checkpoint); err != nil {
		return Checkpoint{}, fmt.Errorf("%w: decode: %w", ErrCheckpointCorrupted, err)
	}
	if decoder.More() {
		return Checkpoint{}, fmt.Errorf("%w: entry carries more than one JSON document", ErrCheckpointCorrupted)
	}
	if err := ValidateCheckpoint(checkpoint); err != nil {
		return Checkpoint{}, fmt.Errorf("%w: %w", ErrCheckpointCorrupted, err)
	}
	if checkpoint.CheckpointDigestSHA256 != digestSHA256 {
		return Checkpoint{}, fmt.Errorf("%w: entry stored under %s declares digest %s", ErrCheckpointCorrupted, digestSHA256, checkpoint.CheckpointDigestSHA256)
	}
	return checkpoint, nil
}
