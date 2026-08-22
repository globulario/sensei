// SPDX-License-Identifier: AGPL-3.0-only

//go:build !windows

package modelexec

import (
	"os/exec"
	"syscall"
)

// isolateProcessGroup puts the bridge in its own process group so cancellation
// can reach its DESCENDANTS.
//
// exec.CommandContext kills only the process it started. A bridge is usually a
// wrapper — the shipped example is a shell script that runs curl — and the
// child inherits the stdout pipe, so killing the wrapper alone leaves the real
// network request running past the caller's deadline while cmd.Run stays
// blocked on the pipe. A deadline that does not stop the work is not a
// deadline.
func isolateProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// terminateProcessGroup signals the whole group, not just the leader.
func terminateProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	// Negative pid addresses the process group.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
