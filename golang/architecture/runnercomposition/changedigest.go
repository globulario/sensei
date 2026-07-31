// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// GitChangeDigest returns the canonical content digest of the change from
// oldDir to newDir: sha256 over the raw bytes of
// `git diff --no-index --no-ext-diff --binary`, the same convention
// golang/architecture/admission.CaptureChanges and resulttransition's
// private patchDigest already use in this codebase, applied here to two
// independent ephemeral directories rather than two points in one
// repository's history. --no-index is exactly git diff's mode for comparing
// two arbitrary filesystem trees -- oldDir and newDir need not be, and for
// O3's use case never are, inside any git repository at all.
//
// oldDir and newDir must be absolute paths. GIT_CONFIG_NOSYSTEM=1 is set on
// the child process's environment, matching admission.CaptureChanges'
// existing hardening -- git never reads system-wide configuration to
// compute this digest.
func GitChangeDigest(ctx context.Context, oldDir, newDir string) (string, error) {
	if oldDir == "" || newDir == "" {
		return "", fmt.Errorf("GitChangeDigest: oldDir and newDir must both be non-empty")
	}
	// git diff --no-index exits 1 for BOTH "found differences" (success)
	// and "could not access a path" (a real error) -- the exit code alone
	// cannot distinguish them, so existence is verified here, in Go, before
	// git is ever invoked, rather than by parsing git's stderr text.
	if _, err := os.Stat(oldDir); err != nil {
		return "", fmt.Errorf("GitChangeDigest: oldDir: %w", err)
	}
	if _, err := os.Stat(newDir); err != nil {
		return "", fmt.Errorf("GitChangeDigest: newDir: %w", err)
	}

	cmd := exec.CommandContext(ctx, "git", "diff", "--no-index", "--no-ext-diff", "--binary", "--", oldDir, newDir)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// git diff --no-index exits 1 when it found differences --
			// that is success, not failure. Any other non-zero exit (2 for
			// a usage/IO error, etc.) is a real error.
			if exitErr.ExitCode() != 1 {
				return "", fmt.Errorf("git diff --no-index: %w: %s", err, stderr.String())
			}
		} else {
			return "", fmt.Errorf("git diff --no-index: %w", err)
		}
	}
	return sha256Hex(stdout.Bytes()), nil
}
