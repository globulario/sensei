// SPDX-License-Identifier: AGPL-3.0-only

//go:build windows

package main

import (
	"fmt"
	"os"
	"syscall"
)

func acquireGraphPublicationLock(lockPath string) (*os.File, error) {
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	handle := syscall.Handle(file.Fd())
	if err := syscall.LockFile(handle, 0, 0, 1, 0); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("concurrent graph publication in progress (lock busy): %w", err)
	}
	return file, nil
}
