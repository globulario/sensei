// SPDX-License-Identifier: AGPL-3.0-only

//go:build windows

package commandprovider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func configureProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}

func waitProcess(ctx context.Context, command *exec.Cmd) error {
	done := make(chan error, 1)
	go func() {
		done <- command.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = terminateWindowsProcessTree(command)
		<-done
		return ctx.Err()
	}
}

// terminateWindowsProcessTree uses the operating system's task-tree owner to
// terminate the provider and all descendants. A direct Process.Kill remains the
// bounded fallback when the platform utility is unavailable or refuses the tree.
func terminateWindowsProcessTree(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}

	var treeError error
	windowsRoot := strings.TrimSpace(os.Getenv("SystemRoot"))
	if windowsRoot == "" {
		windowsRoot = strings.TrimSpace(os.Getenv("WINDIR"))
	}
	if windowsRoot != "" {
		taskkill := filepath.Join(windowsRoot, "System32", "taskkill.exe")
		killer := exec.Command(
			taskkill,
			"/PID", strconv.Itoa(command.Process.Pid),
			"/T",
			"/F",
		)
		killer.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if output, err := killer.CombinedOutput(); err == nil {
			return nil
		} else {
			treeError = fmt.Errorf(
				"taskkill provider tree: %w: %s",
				err,
				strings.TrimSpace(string(output)),
			)
		}
	}

	return errors.Join(treeError, command.Process.Kill())
}
