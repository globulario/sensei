//go:build unix

// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"os"

	"golang.org/x/sys/unix"
)

// openSealedEntryNoFollow opens name for reading, relative to
// s.dirFile.Fd(), refusing to follow a symlink at the final path
// component and refusing to block opening a FIFO -- both in the same
// atomic open call. See readSealedEntry's doc comment in store.go for the
// full rationale.
//
// golang.org/x/sys/unix is used rather than the standard library's
// syscall package because syscall.Openat is Linux-only in Go's standard
// library; unix.Openat is portable across every unix GOOS this repository
// actually ships (linux, darwin), giving darwin the exact same
// dirfd-relative, rename-robust guarantee as linux, not a weaker
// fallback.
func openSealedEntryNoFollow(s *fsCandidateArtifactStore, name string) (*os.File, error) {
	fd, err := unix.Openat(int(s.dirFile.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, &os.PathError{Op: "openat", Path: name, Err: err}
	}
	return os.NewFile(uintptr(fd), name), nil
}
