//go:build !windows

// SPDX-License-Identifier: AGPL-3.0-only

package fileinterpretation

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// openNoFollow opens path for reading, refusing to follow a symlink at the
// final path component. syscall.O_NOFOLLOW is passed directly into the
// same open(2) call that creates the file descriptor, so there is no
// separate check-then-open window: the kernel refuses the open atomically
// if the final component is a symlink, rather than this package checking
// the path first (via Lstat) and opening it separately afterward, which
// leaves a race where another process with write access to the same
// directory can swap the checked regular file for a symlink in between.
func openNoFollow(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("fileinterpretation: %q is a symlink, not a regular file -- pass the real path", path)
		}
		return nil, fmt.Errorf("fileinterpretation: open %q: %w", path, err)
	}
	return f, nil
}
