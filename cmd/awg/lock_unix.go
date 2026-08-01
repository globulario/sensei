//go:build !windows

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
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		file.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, fmt.Errorf("concurrent reconstruction in progress (lock busy)")
		}
		return nil, err
	}
	return file, nil
}
