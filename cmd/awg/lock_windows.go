//go:build windows

package main

import (
	"fmt"
	"os"
	"syscall"
)

func acquireProjectLock(lockPath string) (*os.File, error) {
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	h := syscall.Handle(file.Fd())
	err = syscall.LockFile(h, 0, 0, 1, 0)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("concurrent reconstruction in progress (lock busy): %w", err)
	}
	return file, nil
}
