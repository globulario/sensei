// SPDX-License-Identifier: Apache-2.0

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
// reconciliation's HEAD write both fail, so the entry is durable but derived-state
// repair cannot complete; a PostCommitError carries the committed identity, and a
// retry after the fault clears reconciles with no second event.
func TestDurableEntryReconcileFailsPostCommitError(t *testing.T) {
	taskDir, c := cleanCandidate(t, recAt)
	// Fail the append's HEAD write AND the reconciliation's HEAD write.
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
	if countTransitionEvents(t, taskDir) != 1 {
		t.Fatal("durable entry should exist exactly once")
	}
	// Clear the fault and retry: reconcile, no second event.
	ledger.InjectHeadWriteFaults(0)
	res, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c})
	if err != nil {
		t.Fatalf("retry after obstruction removed: %v", err)
	}
	if res.Disposition != DispositionReconciled {
		t.Fatalf("retry disposition = %s, want reconciled", res.Disposition)
	}
	if countTransitionEvents(t, taskDir) != 1 {
		t.Fatal("retry appended a second event")
	}
}
