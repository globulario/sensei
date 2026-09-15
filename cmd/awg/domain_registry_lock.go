// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// domainRegistryLockPath is the lock guarding one registry file's read-modify-write.
//
// Beside the registry and named after it, so two processes given the same registry path
// agree on the lock without being told, and a second registry (a test's disposable one)
// is independently locked rather than contending with the operator's.
func domainRegistryLockPath(registryPath string) string {
	return filepath.Join(filepath.Dir(registryPath), "."+filepath.Base(registryPath)+".lock")
}

// lockDomainRegistry takes an exclusive, BLOCKING lock on the registry and returns the
// release. Blocking is the point: every caller must eventually get its turn, because each
// is recording a publication that already succeeded.
//
// A lock file that cannot be created is reported rather than skipped. Proceeding
// unserialized is how the pointer was lost in the first place, and a lost ACTIVE pointer
// is silent -- the graph keeps answering, from the wrong generation.
func lockDomainRegistry(registryPath string) (func(), error) {
	lockPath := domainRegistryLockPath(registryPath)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("domain registry lock %s: %w", lockPath, err)
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("domain registry lock %s: %w", lockPath, err)
	}
	if err := lockFileExclusive(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("domain registry lock %s: %w", lockPath, err)
	}
	return func() {
		_ = unlockFile(file)
		_ = file.Close()
	}, nil
}
