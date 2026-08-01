//go:build windows

package main

import (
	"syscall"
	"testing"
)

func acquireTestLock(t *testing.T, fd uintptr) {
	err := syscall.LockFile(syscall.Handle(fd), 0, 0, 1, 0)
	if err != nil {
		t.Fatalf("failed to acquire test lock: %v", err)
	}
}

func releaseTestLock(t *testing.T, fd uintptr) {
	syscall.UnlockFile(syscall.Handle(fd), 0, 0, 1, 0)
}
