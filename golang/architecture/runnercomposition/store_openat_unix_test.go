//go:build unix

// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"errors"
	"os"
	"testing"
)

// TestOpenSealedEntryRefusesADescriptorTheStoreNoLongerOwns pins the
// mechanism behind failure.runnercomposition.sealed_entry_opened_through_collectible_dirfd:
// the sealed-entry open must resolve names through a reference the store
// still owns, never through a bare descriptor number read out of
// os.File.Fd() ahead of the call.
//
// The defect this guards is a garbage-collection race, not a closed-file
// one: os.File closes its descriptor from a GC cleanup, and by the time
// openat(2) runs, nothing in any live frame references the store (the
// receiver's last use is the descriptor read itself), so a collection
// landing in that window closes the descriptor mid-call and frees its
// number for reissue to any unrelated file the process opens next. A
// stress reproduction against the pre-fix implementation -- 64,000 Get
// calls over freshly constructed store values while four goroutines
// hammered runtime.GC() at GOGC=1 -- failed 54 times, with errors
// including "openat <digest>.json: not a directory", i.e. the reissued
// number had landed on a regular file and the open resolved against
// something the store does not own. The same run against this
// implementation failed zero times. That race cannot be asserted
// deterministically, so this test asserts the property that closes it
// instead: the open goes through the file's RawConn, which holds a
// reference for exactly the duration of the call and refuses a descriptor
// the store no longer owns rather than using its stale number.
func TestOpenSealedEntryRefusesADescriptorTheStoreNoLongerOwns(t *testing.T) {
	store, err := NewFSCandidateArtifactStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fsStore, ok := store.(*fsCandidateArtifactStore)
	if !ok {
		t.Fatalf("NewFSCandidateArtifactStore returned %T", store)
	}
	if err := fsStore.dirFile.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = openSealedEntryNoFollow(fsStore, "0000000000000000000000000000000000000000000000000000000000000000.json")
	if err == nil {
		t.Fatal("opening a sealed entry through a descriptor the store no longer owns succeeded")
	}
	if !errors.Is(err, os.ErrClosed) {
		t.Fatalf("error = %v, want one satisfying errors.Is(err, os.ErrClosed) -- a raw EBADF means the bare descriptor number was used rather than an owned reference", err)
	}
}
