//go:build windows

// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// openSealedEntryNoFollow opens name for reading, refusing to follow a
// symlink or other reparse point at the final path component. Windows has
// no dirfd-relative open primitive equivalent to POSIX openat, so unlike
// the unix helper (store_openat_unix.go) this resolves name against
// s.root.Name() as a plain path-string join, not a descriptor-relative
// open -- it therefore does not inherit dirFile's rename-robustness (see
// readSealedEntry's doc comment in store.go), an accepted, honest platform
// difference, not a silent regression. Detecting and refusing a reparse
// point at the final component mirrors
// synthesisdriver/fileinterpretation/open_windows.go's own accepted
// technique for the identical underlying platform gap: CreateFile with
// FILE_FLAG_OPEN_REPARSE_POINT opens the reparse point itself rather than
// transparently following it, letting this function detect it (via the
// returned handle's own FILE_ATTRIBUTE_REPARSE_POINT bit) and refuse --
// atomically, in the same call that creates the handle.
func openSealedEntryNoFollow(s *fsCandidateArtifactStore, name string) (*os.File, error) {
	path := filepath.Join(s.root.Name(), name)
	utf16Path, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("%q is not a valid path: %w", path, err)
	}
	handle, err := syscall.CreateFile(
		utf16Path,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_OPEN_REPARSE_POINT|syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}

	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(handle, &info); err != nil {
		_ = syscall.CloseHandle(handle)
		return nil, fmt.Errorf("inspect %q: %w", path, err)
	}
	if info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = syscall.CloseHandle(handle)
		return nil, fmt.Errorf("%q is a symlink or reparse point, not a regular file", name)
	}

	return os.NewFile(uintptr(handle), name), nil
}
