//go:build unix

// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"os"

	"golang.org/x/sys/unix"
)

// openSealedEntryNoFollow opens name for reading, relative to s.dirFile,
// refusing to follow a symlink at the final path component and refusing to
// block opening a FIFO -- both in the same atomic open call. See
// readSealedEntry's doc comment in store.go for the full rationale.
//
// golang.org/x/sys/unix is used rather than the standard library's
// syscall package because syscall.Openat is Linux-only in Go's standard
// library; unix.Openat is portable across every unix GOOS this repository
// actually ships (linux, darwin), giving darwin the exact same
// dirfd-relative, rename-robust guarantee as linux, not a weaker
// fallback.
//
// The directory descriptor is obtained through s.dirFile's RawConn and
// used INSIDE Control's callback, never via s.dirFile.Fd(). Fd() hands
// back a bare int that outlives nothing: os.File closes its descriptor
// from a garbage-collection cleanup, and by the time openat(2) runs,
// s -- and therefore s.dirFile -- is no longer referenced by any live
// frame (the receiver's last use is the descriptor read itself), so the
// store is collectible mid-call. A collection landing in that window
// closes the descriptor while openat is in flight, and the number is then
// free to be reissued to any unrelated file or directory the process opens
// next. That is not merely a failed read: a reissued number makes openat
// resolve name against a DIFFERENT directory, so a sealed entry can be
// reported missing, reported corrupted, or -- with the wrong reissue --
// read from somewhere the store does not own. Control holds a reference
// for exactly the duration of the callback, so the descriptor cannot be
// closed or reissued while openat uses it, and returns os.ErrClosed
// rather than a stale number if the file has already been closed.
// See failure.runnercomposition.sealed_entry_opened_through_collectible_dirfd.
func openSealedEntryNoFollow(s *fsCandidateArtifactStore, name string) (*os.File, error) {
	rawConn, err := s.dirFile.SyscallConn()
	if err != nil {
		return nil, &os.PathError{Op: "openat", Path: name, Err: err}
	}
	var (
		fd      int
		openErr error
	)
	if controlErr := rawConn.Control(func(dirfd uintptr) {
		fd, openErr = unix.Openat(int(dirfd), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	}); controlErr != nil {
		// RawConn.Control fails only when the file has already been
		// closed. Report that as os.ErrClosed -- the error os.File's own
		// methods report for it -- rather than the internal poll
		// sentinel, which no caller can match on.
		return nil, &os.PathError{Op: "openat", Path: name, Err: os.ErrClosed}
	}
	if openErr != nil {
		return nil, &os.PathError{Op: "openat", Path: name, Err: openErr}
	}
	return os.NewFile(uintptr(fd), name), nil
}
