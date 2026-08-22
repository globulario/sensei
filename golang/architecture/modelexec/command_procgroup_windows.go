// SPDX-License-Identifier: AGPL-3.0-only

//go:build windows

package modelexec

import "os/exec"

// Windows has no POSIX process groups with the same semantics. The adapter
// still terminates the process it started; descendant termination is a known
// platform gap rather than a silent difference.
func isolateProcessGroup(cmd *exec.Cmd) {}

func terminateProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
