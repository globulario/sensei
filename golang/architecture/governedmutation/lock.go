// SPDX-License-Identifier: Apache-2.0

package governedmutation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	yaml "gopkg.in/yaml.v3"
)

// LockDirName is the repository-scoped governed-mutation lock directory (an
// os.Mkdir mutex), held by whichever owner is mutating governed sources.
const LockDirName = ".governed-mutation.lock"

// ErrLockHeld reports that the governed-mutation lock could not be acquired
// before the context deadline (another writer holds it).
type ErrLockHeld struct{ Path string }

func (e ErrLockHeld) Error() string { return "governed-mutation lock held: " + e.Path }

type lockRecord struct {
	PID       int    `yaml:"pid"`
	StartedAt string `yaml:"started_at"`
	Operation string `yaml:"operation"`
}

// AcquireLock takes the single repository-scoped governed-mutation lock and
// returns a release closure. Lock ownership is COMPOSABLE: the mutation owner
// never locks internally, so the promotion transaction can hold this one lock
// continuously across the source mutation and its later graph verification.
// The repository promotion lock is the outermost lock; source/journal/store
// internal locks are acquired beneath it.
//
// operation is a short label recorded for diagnostics (e.g. "propose",
// "promote"). now is the caller-supplied stamp so the record is deterministic.
func AcquireLock(ctx context.Context, root, operation string, now time.Time) (func(), error) {
	dir := filepath.Join(root, ".sensei", LockDirName)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return nil, err
	}
	for {
		if err := os.Mkdir(dir, 0o755); err != nil {
			if os.IsExist(err) {
				select {
				case <-ctx.Done():
					return nil, ErrLockHeld{Path: dir}
				case <-time.After(5 * time.Millisecond):
					continue
				}
			}
			return nil, err
		}
		break
	}
	record := lockRecord{PID: os.Getpid(), StartedAt: now.UTC().Format(time.RFC3339), Operation: operation}
	data, err := yaml.Marshal(record)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("marshal lock record: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lock.yaml"), data, 0o644); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	released := false
	return func() {
		if released {
			return
		}
		released = true
		_ = os.RemoveAll(dir)
	}, nil
}
