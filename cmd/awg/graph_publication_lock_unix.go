// SPDX-License-Identifier: AGPL-3.0-only

//go:build !windows

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
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, fmt.Errorf("concurrent graph publication in progress (lock busy)")
		}
		return nil, err
	}
	return file, nil
}
