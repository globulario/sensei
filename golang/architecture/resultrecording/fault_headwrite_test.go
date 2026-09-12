// SPDX-License-Identifier: AGPL-3.0-only

//go:build sensei_faultinject

package resultrecording

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/globulario/sensei/golang/architecture/ledger"
)

// These durable-entry / HEAD-failure tests need a post-commit HEAD-write fault. The
// fault seam exists ONLY under the sensei_faultinject build tag (ledger's
// faultinject.go), so it ships in no normal build; these tests are compiled under
// the same tag. Run: go test -tags sensei_faultinject ./golang/architecture/... .

// TestDurableEntryHeadFailsReconcileSucceeds: the append's HEAD write fails, but
// the same RecordTransition reconciles HEAD and succeeds — one durable event.
func TestDurableEntryHeadFailsReconcileSucceeds(t *testing.T) {
	taskDir, c := cleanCandidate(t, recAt)
	ledger.InjectHeadWriteFaults(1)
	defer ledger.InjectHeadWriteFaults(0)
	res, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c})
	if err != nil {
		t.Fatalf("record with injected HEAD fault: %v", err)
	}
	if res.Disposition != DispositionRecorded {
		t.Fatalf("disposition = %s", res.Disposition)
	}
	if _, err := os.Stat(headPath(taskDir)); err != nil {
		t.Fatal("HEAD not reconciled after append fault")
	}
	if countTransitionEvents(t, taskDir) != 1 {
		t.Fatal("more than one event")
	}
}

// TestDurableEntryReconcileFailsPostCommitError: the append's HEAD write AND the
// recovery's HEAD write both fail, so the entry is durable but HEAD was never
// published; a PostCommitError carries the committed identity.
//
// The retry used to reconcile and report "reconciled". It no longer can, and the
// reason is the point of issue #352: across two calls nothing is left that
// distinguishes "this process committed an entry and could not publish HEAD"
// from "someone deleted the highest-sequence entry". Both leave entries that
// verify and a HEAD that does not name the last one. The proof that licenses
// republication -- the exact ErrEntryDurable identity -- lives inside the call
// that made the append, and it does not survive into a later one. So a fresh
// call refuses and leaves the damage visible rather than rebuilding HEAD from
// whatever entries are on disk, which is the operation that resurrects a spent
// mutation capability.
func TestDurableEntryReconcileFailsPostCommitError(t *testing.T) {
	taskDir, c := cleanCandidate(t, recAt)
	// Fail the append's HEAD write AND the recovery's HEAD write.
	ledger.InjectHeadWriteFaults(2)
	defer ledger.InjectHeadWriteFaults(0)
	_, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c})
	var pce *PostCommitError
	if !errors.As(err, &pce) {
		t.Fatalf("want PostCommitError, got %v", err)
	}
	if !isHex64(pce.EntryDigestSHA256) {
		t.Fatalf("post-commit error lacks committed entry identity: %+v", pce)
	}
	// Counted off the directory: the chain deliberately does not verify here.
	if countTransitionEntryFiles(t, taskDir) != 1 {
		t.Fatal("durable entry should exist exactly once")
	}

	ledger.InjectHeadWriteFaults(0)
	if _, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c}); err == nil {
		t.Fatal("a later call rebuilt an unpublished HEAD: it holds no evidence that the missing HEAD came from this append rather than from a deleted tail entry")
	}
	// The seeded task already had a chain, so HEAD survives naming the previous
	// entry: it lags the durable tip rather than being absent.
	assertLedgerStillInvalid(t, taskDir, "ledger.head_stale")
	if countTransitionEntryFiles(t, taskDir) != 1 {
		t.Fatal("the refused retry appended a second event")
	}
}
