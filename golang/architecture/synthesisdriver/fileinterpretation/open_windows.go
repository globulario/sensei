//go:build windows

// SPDX-License-Identifier: AGPL-3.0-only

package fileinterpretation

import (
	"fmt"
	"os"
	"syscall"
)

// openNoFollow opens path for reading, refusing to follow a symlink or
// other reparse point (junction, mount point) at the final path component.
// Windows has no direct O_NOFOLLOW equivalent in the standard open call,
// so this uses CreateFile with FILE_FLAG_OPEN_REPARSE_POINT: if the final
// component is a reparse point, CreateFile opens the reparse point itself
// rather than transparently following it to its target, letting this
// function detect it (via the returned handle's own FILE_ATTRIBUTE_REPARSE_POINT
// bit) and refuse -- atomically, in the same call that creates the handle,
// not as a separate check-then-open step that leaves a race window.
func openNoFollow(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("fileinterpretation: %q is not a valid path: %w", path, err)
	}
	handle, err := syscall.CreateFile(
		name,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_OPEN_REPARSE_POINT|syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("fileinterpretation: open %q: %w", path, err)
	}

	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(handle, &info); err != nil {
		_ = syscall.CloseHandle(handle)
		return nil, fmt.Errorf("fileinterpretation: inspect %q: %w", path, err)
	}
	if info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = syscall.CloseHandle(handle)
		return nil, fmt.Errorf("fileinterpretation: %q is a symlink or reparse point, not a regular file -- pass the real path", path)
	}

	return os.NewFile(uintptr(handle), path), nil
}
