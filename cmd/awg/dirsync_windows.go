// SPDX-License-Identifier: AGPL-3.0-only

//go:build windows

package main

// syncDirImpl is a deliberate no-op on Windows, not a best-effort attempt
// that would spuriously fail. os.Open on a directory yields a read-only
// handle; File.Sync on it calls FlushFileBuffers, which cannot flush a
// handle opened without write access -- every caller of syncDir (a rename
// or mkdir that just succeeded) would otherwise fail unconditionally on
// Windows. NTFS does not need the equivalent of the ext4 "fsync the parent
// directory after rename" step this package's Unix implementation performs:
// NTFS's own metadata journal (the $LogFile) makes a completed rename or
// directory-create operation durable through the filesystem's own
// transaction log, not through an explicit userspace directory flush.
func syncDirImpl(dir string) error {
	return nil
}
