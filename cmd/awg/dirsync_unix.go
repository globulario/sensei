// SPDX-License-Identifier: AGPL-3.0-only

//go:build !windows

package main

import (
	"fmt"
	"os"
)

// syncDirImpl fsyncs a directory entry via a plain read-only directory
// handle, as ext4-family filesystems require to make a preceding rename or
// mkdir durable across a crash.
func syncDirImpl(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open %s to sync its directory entry: %w", dir, err)
	}
	defer d.Close()
	return d.Sync()
}
