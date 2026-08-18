// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// latestPointerName is the small mutable reference naming the newest durable
// checkpoint. It holds a digest, never checkpoint content: the content lives
// in its own content-addressed file, so losing or lagging this pointer costs
// at most the newest boundary and never damages the history.
const latestPointerName = "latest"

// fsCheckpointStore is the filesystem adapter the CLI composes over a
// directory it owns. O7 never learns this layout.
//
// Ordering is the whole point of the adapter. A checkpoint is sealed under its
// own digest FIRST and the latest pointer is moved only afterwards, so a crash
// in between leaves the previous pointer intact and still resolvable — a
// recoverable older boundary rather than a pointer into nothing.
type fsCheckpointStore struct {
	root *os.Root
}

// NewFSCheckpointStore opens an append-only checkpoint store rooted at an
// existing absolute directory. Every read and write goes through the root, so
// a name can never escape it.
func NewFSCheckpointStore(root string) (CheckpointStore, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("NewFSCheckpointStore: root must be absolute: %q", root)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("NewFSCheckpointStore: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("NewFSCheckpointStore: root is not a directory: %q", root)
	}
	opened, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("NewFSCheckpointStore: open root: %w", err)
	}
	return &fsCheckpointStore{root: opened}, nil
}

func (s *fsCheckpointStore) Save(ctx context.Context, checkpoint Checkpoint) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	marshaled, digest, err := marshalCheckpointForStore(checkpoint)
	if err != nil {
		return fmt.Errorf("CheckpointStore.Save: %w", err)
	}
	finalName := digest + ".json"

	switch existing, err := s.readFile(finalName); {
	case err == nil:
		// Content-addressed and immutable: identical content is an idempotent
		// no-op, different content under the same digest means the entry is
		// not what its key names and is never silently replaced.
		if string(existing) != string(marshaled) {
			return fmt.Errorf("CheckpointStore.Save: %w: %s already holds different content", ErrCheckpointCorrupted, finalName)
		}
	case errors.Is(err, fs.ErrNotExist):
		if err := s.writeFileAtomic(finalName, marshaled); err != nil {
			return fmt.Errorf("CheckpointStore.Save: seal checkpoint: %w", err)
		}
	default:
		return fmt.Errorf("CheckpointStore.Save: read existing entry: %w", err)
	}

	// Only now, with the content durable, may the pointer advance — and only
	// for a genuinely newer checkpoint, so a stale writer cannot rewind a
	// resumed session to an earlier boundary.
	advance, err := s.shouldAdvanceLatest(ctx, checkpoint)
	if err != nil {
		return err
	}
	if !advance {
		return nil
	}
	if err := s.writeFileAtomic(latestPointerName, []byte(digest+"\n")); err != nil {
		return fmt.Errorf("CheckpointStore.Save: advance latest pointer: %w", err)
	}
	return nil
}

func (s *fsCheckpointStore) shouldAdvanceLatest(ctx context.Context, checkpoint Checkpoint) (bool, error) {
	current, err := s.Latest(ctx)
	switch {
	case errors.Is(err, ErrCheckpointNotFound):
		return true, nil
	case err != nil:
		return false, fmt.Errorf("CheckpointStore.Save: read current latest: %w", err)
	}
	return checkpoint.Sequence > current.Sequence, nil
}

func (s *fsCheckpointStore) Load(ctx context.Context, digestSHA256 string) (Checkpoint, error) {
	if err := ctx.Err(); err != nil {
		return Checkpoint{}, err
	}
	if !isSHA256(digestSHA256) {
		return Checkpoint{}, fmt.Errorf("CheckpointStore.Load: %q is not a well-formed sha256 digest", digestSHA256)
	}
	stored, err := s.readFile(digestSHA256 + ".json")
	if errors.Is(err, fs.ErrNotExist) {
		return Checkpoint{}, fmt.Errorf("CheckpointStore.Load: %w: %s", ErrCheckpointNotFound, digestSHA256)
	}
	if err != nil {
		return Checkpoint{}, fmt.Errorf("CheckpointStore.Load: %w", err)
	}
	return decodeCheckpointFromStore(stored, digestSHA256)
}

func (s *fsCheckpointStore) Latest(ctx context.Context) (Checkpoint, error) {
	if err := ctx.Err(); err != nil {
		return Checkpoint{}, err
	}
	pointer, err := s.readFile(latestPointerName)
	if errors.Is(err, fs.ErrNotExist) {
		return Checkpoint{}, fmt.Errorf("CheckpointStore.Latest: %w", ErrCheckpointNotFound)
	}
	if err != nil {
		return Checkpoint{}, fmt.Errorf("CheckpointStore.Latest: %w", err)
	}
	digest := strings.TrimSpace(string(pointer))
	if !isSHA256(digest) {
		return Checkpoint{}, fmt.Errorf("CheckpointStore.Latest: %w: pointer holds %q", ErrCheckpointCorrupted, digest)
	}
	return s.Load(ctx, digest)
}

// readFile reads a flat name directly inside the root, refusing anything that
// is not a regular file so a symlink or FIFO planted under a digest name
// cannot be read back as a checkpoint.
func (s *fsCheckpointStore) readFile(name string) ([]byte, error) {
	file, err := s.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s is not a regular file", ErrCheckpointCorrupted, name)
	}
	return io.ReadAll(file)
}

// writeFileAtomic stages content under a unique temporary name inside the same
// root and renames it into place, so a reader never observes a partially
// written checkpoint or pointer.
func (s *fsCheckpointStore) writeFileAtomic(name string, content []byte) error {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Errorf("generate staging name: %w", err)
	}
	staging := name + "." + hex.EncodeToString(suffix[:]) + ".tmp"
	if err := s.root.WriteFile(staging, content, 0o644); err != nil {
		return fmt.Errorf("write staged content: %w", err)
	}
	if err := s.root.Rename(staging, name); err != nil {
		return errors.Join(fmt.Errorf("commit %s: %w", name, err), s.root.Remove(staging))
	}
	return nil
}
