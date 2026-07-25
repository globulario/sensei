// SPDX-License-Identifier: AGPL-3.0-only

//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func acquireGraphPublicationLock(lockPath string) (*os.File, error) {
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	handle := windows.Handle(file.Fd())
	overlapped := new(windows.Overlapped)
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	if err := windows.LockFileEx(handle, flags, 0, 1, 0, overlapped); err != nil {
		_ = file.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, fmt.Errorf("concurrent graph publication in progress (lock busy)")
		}
		return nil, err
	}
	return file, nil
}
