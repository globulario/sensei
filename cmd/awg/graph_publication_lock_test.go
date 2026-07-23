// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"path/filepath"
	"testing"
)

func TestAcquireGraphPublicationLock_ExcludesConcurrentPublisher(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph-publication.lock")
	first, err := acquireGraphPublicationLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	second, err := acquireGraphPublicationLock(path)
	if err == nil {
		_ = second.Close()
		t.Fatal("second publisher acquired an already-held graph publication lock")
	}
}
