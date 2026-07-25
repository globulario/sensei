// SPDX-License-Identifier: AGPL-3.0-only

//go:build windows

package runtimedescriptor

import "syscall"

// stillActive is STILL_ACTIVE (259) per the Windows SDK: the exit code a
// process reports while it is still running.
const stillActive = 259

// processQueryLimitedInformation is PROCESS_QUERY_LIMITED_INFORMATION, the
// minimal access right needed to read a process's exit code.
const processQueryLimitedInformation = 0x1000

// isProcessAlive reports whether pid names a running process, by opening a
// query-limited handle and checking its exit code. Best-effort, same
// posture as the Unix liveness probe (see liveness_unix.go).
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(handle)
	var exitCode uint32
	if err := syscall.GetExitCodeProcess(handle, &exitCode); err != nil {
		return false
	}
	return exitCode == stillActive
}
