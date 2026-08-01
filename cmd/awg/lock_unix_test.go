//go:build !windows

package main

import (
	"syscall"
	"testing"
)

func acquireTestLock(t *testing.T, fd uintptr) {
	err := syscall.Flock(int(fd), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		t.Fatalf("failed to acquire test lock: %v", err)
	}
}

func releaseTestLock(t *testing.T, fd uintptr) {
	syscall.Flock(int(fd), syscall.LOCK_UN)
}
