// SPDX-License-Identifier: AGPL-3.0-only

//go:build !windows

package main

import (
	"os"
	"syscall"
)

// lockFileExclusive blocks until the lock is held. It is the same flock the graph
// publication lock uses, WITHOUT LOCK_NB: see lockDomainRegistry for why waiting rather
// than failing is correct for this file.
func lockFileExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
