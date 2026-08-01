// SPDX-License-Identifier: AGPL-3.0-only

//go:build !windows

package runtimedescriptor

import (
	"os"
	"syscall"
)

// isProcessAlive reports whether pid names a running process, via the
// standard "signal 0" liveness probe (sends no actual signal; only checks
// whether the kernel would permit sending one). Best-effort: after a PID
// wraps around post-reboot this can false-positive, an accepted residual
// risk for a local dev/CI tool — the existing graph-publication flock
// (cmd/awg/graph_publication_lock_unix.go) accepts the same posture.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
