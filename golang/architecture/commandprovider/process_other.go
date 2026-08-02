// SPDX-License-Identifier: AGPL-3.0-only

//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package commandprovider

import (
	"context"
	"os/exec"
)

func configureProcessGroup(command *exec.Cmd) {}

func waitProcess(ctx context.Context, command *exec.Cmd) error {
	done := make(chan error, 1)
	go func() {
		done <- command.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		<-done
		return ctx.Err()
	}
}
