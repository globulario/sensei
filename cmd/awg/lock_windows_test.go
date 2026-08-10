//go:build windows

package main

import (
	"testing"

	"golang.org/x/sys/windows"
)

func acquireTestLock(t *testing.T, fd uintptr) {
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	err := windows.LockFileEx(windows.Handle(fd), flags, 0, 1, 0, new(windows.Overlapped))
	if err != nil {
		t.Fatalf("failed to acquire test lock: %v", err)
	}
}

func releaseTestLock(t *testing.T, fd uintptr) {
	windows.UnlockFileEx(windows.Handle(fd), 0, 1, 0, new(windows.Overlapped))
}
