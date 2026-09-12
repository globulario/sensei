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

// TestDurableEntryHeadFailsReconcileSucceeds: the append's HEAD write fails once,
// Store.Append's bounded publication retry absorbs it under its lock, and
// RecordTransition succeeds — one durable event.
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

// TestDurableEntryReconcileFailsPostCommitError: every HEAD publication attempt
// Store.Append makes fails, so the entry is durable and HEAD is unpublished. A
// PostCommitError carries the committed identity, and neither this call nor a
// retry repairs HEAD or reads the chain through it: HEAD publication recovery
// belongs to Store.Append alone (#352).
func TestDurableEntryReconcileFailsPostCommitError(t *testing.T) {
	taskDir, c := cleanCandidate(t, recAt)
	ledger.InjectHeadWriteFaults(ledger.HeadPublicationAttempts())
	defer ledger.InjectHeadWriteFaults(0)
	_, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c})
	var pce *PostCommitError
	if !errors.As(err, &pce) {
		t.Fatalf("want PostCommitError, got %v", err)
	}
	if n := ledger.PendingHeadWriteFaults(); n != 0 {
		t.Fatalf("%d faults never fired; the retry bound was not exhausted", n)
	}
	if !isHex64(pce.EntryDigestSHA256) {
		t.Fatalf("post-commit error lacks committed entry identity: %+v", pce)
	}
	if durableTransitionEvents(t, taskDir) != 1 {
		t.Fatal("durable entry should exist exactly once")
	}
	ledger.InjectHeadWriteFaults(0)
	if _, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c}); err == nil {
		t.Fatal("a retry read through the unpublished HEAD")
	}
	if report, _ := ledger.NewStore(taskDir).Verify(); report.Valid {
		t.Fatal("HEAD was republished outside Store.Append")
	}
	if durableTransitionEvents(t, taskDir) != 1 {
		t.Fatal("retry appended a second event")
	}
}
