// SPDX-License-Identifier: AGPL-3.0-only

package commandprovider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RawCommand is the reusable process boundary underlying O6. It performs only
// direct argv execution with an explicit work directory, environment
// allowlist, and output limits. It assigns no provider or synthesis meaning to
// the bytes it carries.
type RawCommand struct {
	Command string
	Args    []string
	WorkDir string

	EnvironmentAllowlist []string
	MaxStdoutBytes       int64
	MaxStderrBytes       int64
}

// RawResult contains bounded process output. Exceeded is reported as an error;
// callers never receive a silently truncated semantic document.
type RawResult struct {
	Stdout []byte
	Stderr []byte
}

func RunRawCommand(ctx context.Context, spec RawCommand, stdin []byte) (RawResult, error) {
	spec.Command = strings.TrimSpace(spec.Command)
	spec.WorkDir = strings.TrimSpace(spec.WorkDir)
	spec.Args = append([]string{}, spec.Args...)
	spec.EnvironmentAllowlist = uniqueSortedStrings(spec.EnvironmentAllowlist)
	if err := validateRawCommand(spec); err != nil {
		return RawResult{}, err
	}

	command := exec.Command(spec.Command, spec.Args...)
	command.Dir = spec.WorkDir
	command.Env = allowedEnvironment(spec.EnvironmentAllowlist)
	configureProcessGroup(command)
	command.Stdin = bytes.NewReader(stdin)
	stdout := newLimitedBuffer(spec.MaxStdoutBytes)
	stderr := newLimitedBuffer(spec.MaxStderrBytes)
	command.Stdout = stdout
	command.Stderr = stderr

	if err := command.Start(); err != nil {
		return RawResult{}, fmt.Errorf("commandprovider: start raw command %q: %w", spec.Command, err)
	}
	waitErr := waitProcess(ctx, command)
	if err := ctx.Err(); err != nil {
		return RawResult{}, err
	}
	if stdout.exceeded() {
		return RawResult{}, fmt.Errorf("commandprovider: raw stdout exceeded configured limit of %d bytes", spec.MaxStdoutBytes)
	}
	if stderr.exceeded() {
		return RawResult{}, fmt.Errorf("commandprovider: raw stderr exceeded configured limit of %d bytes", spec.MaxStderrBytes)
	}
	result := RawResult{Stdout: stdout.bytes(), Stderr: stderr.bytes()}
	if waitErr != nil {
		return result, fmt.Errorf("commandprovider: raw command failed: %w", waitErr)
	}
	return result, nil
}

func validateRawCommand(spec RawCommand) error {
	if spec.Command == "" {
		return errors.New("commandprovider: raw command is required")
	}
	if !filepath.IsAbs(spec.Command) {
		return fmt.Errorf("commandprovider: raw command must be an absolute path: %q", spec.Command)
	}
	info, err := os.Stat(spec.Command)
	if err != nil {
		return fmt.Errorf("commandprovider: stat raw command: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("commandprovider: raw command is a directory: %q", spec.Command)
	}
	if spec.WorkDir != "" {
		if !filepath.IsAbs(spec.WorkDir) {
			return fmt.Errorf("commandprovider: raw work directory must be absolute: %q", spec.WorkDir)
		}
		info, err := os.Stat(spec.WorkDir)
		if err != nil {
			return fmt.Errorf("commandprovider: stat raw work directory: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("commandprovider: raw work directory is not a directory: %q", spec.WorkDir)
		}
	}
	if spec.MaxStdoutBytes <= 0 || spec.MaxStderrBytes <= 0 {
		return errors.New("commandprovider: raw stdout and stderr limits must be positive")
	}
	for _, name := range spec.EnvironmentAllowlist {
		if name == "" || strings.ContainsRune(name, '=') {
			return fmt.Errorf("commandprovider: invalid raw environment variable name %q", name)
		}
	}
	return nil
}
