// SPDX-License-Identifier: AGPL-3.0-only

//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFileExclusive blocks until the lock is held: LOCKFILE_EXCLUSIVE_LOCK without
// LOCKFILE_FAIL_IMMEDIATELY, which is the Windows equivalent of a blocking flock.
func lockFileExclusive(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, new(windows.Overlapped))
}

func unlockFile(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped))
}
