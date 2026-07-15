// SPDX-License-Identifier: Apache-2.0

package ledger

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type lockRecord struct {
	PID       int    `json:"pid" yaml:"pid"`
	StartedAt string `json:"started_at" yaml:"started_at"`
	Operation string `json:"operation" yaml:"operation"`
}

func acquireLock(ctx context.Context, dir string) (func(), error) {
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
	record := lockRecord{PID: os.Getpid(), StartedAt: time.Now().UTC().Format(time.RFC3339), Operation: "append"}
	data, err := yaml.Marshal(record)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
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
