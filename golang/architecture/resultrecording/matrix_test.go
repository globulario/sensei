// SPDX-License-Identifier: AGPL-3.0-only

package resultrecording

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/resultpipeline"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
)

func headPath(taskDir string) string { return filepath.Join(taskDir, "ledger", "HEAD.yaml") }

// --- concurrency ---

func TestConcurrentIdenticalWritersOneEvent(t *testing.T) {
	taskDir, c := cleanCandidate(t, recAt)
	const n = 8
	var wg sync.WaitGroup
	results := make([]RecordResult, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c})
		}(i)
	}
	wg.Wait()
	recorded := 0
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("writer %d failed: %v", i, errs[i])
		}
		if results[i].Disposition == DispositionRecorded {
			recorded++
		}
	}
	if recorded != 1 {
		t.Fatalf("expected exactly one recorded disposition, got %d", recorded)
	}
	if countTransitionEvents(t, taskDir) != 1 {
		t.Fatal("concurrent identical writers produced more than one event")
	}
}

func TestConcurrentDifferentWritersOneWinner(t *testing.T) {
	repo, taskDir, resultRev := seedCleanTask(t, "package src\n\n// Publish is a no-op.\nfunc Publish() {}\n")
	head, _ := admission.TaskLedgerHead(taskDir)
	mk := func(at string) resultpipeline.TransitionCandidate {
		c, err := resultpipeline.PrepareTransition(context.Background(), resultpipeline.PrepareTransitionRequest{
			Build: resultpipeline.BuildRequest{RepositoryRoot: repo, TaskDirectory: taskDir,
				ResultMode: resulttransition.ResultModeRevision, ResultRevision: resultRev, RepositoryDomain: rDomain},
			ExpectedLedgerHeadDigestSHA256: head, RecordedAt: at,
		})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	c1, c2 := mk("2026-07-17T14:30:00Z"), mk("2026-07-18T09:00:00Z") // same transition id, different receipt
	var wg sync.WaitGroup
	var r1, r2 RecordResult
	var e1, e2 error
	wg.Add(2)
	go func() {
		defer wg.Done()
		r1, e1 = RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c1})
	}()
	go func() {
		defer wg.Done()
		r2, e2 = RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c2})
	}()
	wg.Wait()
	winners := 0
	if e1 == nil && r1.Disposition == DispositionRecorded {
		winners++
	}
	if e2 == nil && r2.Disposition == DispositionRecorded {
		winners++
	}
	if winners != 1 {
		t.Fatalf("expected exactly one winner, got %d (e1=%v e2=%v)", winners, e1, e2)
	}
	if countTransitionEvents(t, taskDir) != 1 {
		t.Fatal("two different writers produced more than one event")
	}
}

// --- derived-state recovery ---

// TestMissingHeadRefusesRatherThanReconciling and its stale twin below used to
// be TestMissingHeadRecovery/TestStaleHeadRecovery: a HEAD deleted or corrupted
// between two recordings was silently rebuilt from the surviving entries and the
// retry reported "reconciled".
//
// That recovery could not tell what it was recovering from. A HEAD that does not
// name the last entry is what a failed HEAD publication leaves behind AND what
// deleting the highest-sequence entry leaves behind, and the second one means a
// recorded transition has been erased. Rebuilding HEAD over it republished the
// truncated prefix as the whole history. An arbitrary later HEAD loss carries no
// evidence of which case it is, so recording refuses and the damage stays
// visible; the one condition that does carry evidence -- this process's own
// append, which minted the entry under the lock it still holds -- resolves
// inside that Append's own bounded HEAD retry and never becomes a later call's
// problem (see TestProjectionDriftRecovery for what still reconciles).
func TestMissingHeadRefusesRatherThanReconciling(t *testing.T) {
	taskDir, c := cleanCandidate(t, recAt)
	if _, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(headPath(taskDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c}); err == nil {
		t.Fatal("recording over a ledger whose HEAD is gone must refuse: the same state is produced by deleting the tail entry")
	}
	assertLedgerStillInvalid(t, taskDir, "ledger.head_missing")
	if countTransitionEntryFiles(t, taskDir) != 1 {
		t.Fatal("the refused retry appended an event")
	}
}

func TestStaleHeadRefusesRatherThanReconciling(t *testing.T) {
	taskDir, c := cleanCandidate(t, recAt)
	if _, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(headPath(taskDir), []byte("schema_version: \"1\"\ntask_id: task.rec\nsequence: 0\nentry_digest_sha256: deadbeef\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c}); err == nil {
		t.Fatal("recording over a ledger whose HEAD disagrees with its chain must refuse")
	}
	assertLedgerStillInvalid(t, taskDir, "ledger.head_stale")
	if countTransitionEntryFiles(t, taskDir) != 1 {
		t.Fatal("the refused retry appended an event")
	}
}

// countTransitionEntryFiles counts recorded-transition entries off the directory
// rather than off a verified chain, because these tests run against a ledger
// that deliberately does not verify -- and "no second event was appended" is a
// fact about the files, not about the verdict.
func countTransitionEntryFiles(t *testing.T, taskDir string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(taskDir, "ledger"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if strings.Contains(e.Name(), string(closureprotocol.LedgerEventResultTransitionRecorded)) {
			n++
		}
	}
	return n
}

// assertLedgerStillInvalid proves the refusal left the damage in place. A
// refusal that repaired on its way out would be the laundering this is about,
// one error return later.
func assertLedgerStillInvalid(t *testing.T, taskDir, wantCode string) {
	t.Helper()
	report, err := ledger.NewStore(taskDir, ledger.WithPayloadValidator(recordingPayloadValidator)).Verify()
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid {
		t.Fatal("the refusal repaired the ledger on its way out")
	}
	for _, e := range report.Errors {
		if e.Code == wantCode {
			return
		}
	}
	t.Fatalf("expected %s to still be reported, got %+v", wantCode, report.Errors)
}

func TestProjectionDriftRecovery(t *testing.T) {
	taskDir, c := cleanCandidate(t, recAt)
	if _, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c}); err != nil {
		t.Fatal(err)
	}
	// Corrupt a projection file, then retry.
	if err := os.WriteFile(filepath.Join(taskDir, "session.yaml"), []byte("drift"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c})
	if err != nil {
		t.Fatalf("retry after projection drift: %v", err)
	}
	if res.ProjectionState != "current" {
		t.Fatalf("projection not restored: %s", res.ProjectionState)
	}
	got, _ := os.ReadFile(filepath.Join(taskDir, "session.yaml"))
	if string(got) == "drift" {
		t.Fatal("drifted projection not rebuilt")
	}
}

// TestReconcileDerivedStateDirect calls the generic repair on its own, without
// a recording in front of it, because that is the shortest route to laundering a
// truncated ledger: one public call that rewrites HEAD from whatever entries are
// left. It must refuse, and it must refuse without touching the ledger.
func TestReconcileDerivedStateDirect(t *testing.T) {
	taskDir, c := cleanCandidate(t, recAt)
	if _, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c}); err != nil {
		t.Fatal(err)
	}
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(recordingPayloadValidator))

	// On a chain that verifies, reconciliation is projection repair and stays
	// available -- it just never rewrites HEAD.
	rec, err := store.ReconcileDerivedState()
	if err != nil {
		t.Fatalf("reconciling a valid chain must still work: %v", err)
	}
	if rec.HeadRewritten || rec.ProjectionState != "current" {
		t.Fatalf("reconcile on a valid chain rewrote HEAD or left projections behind: %+v", rec)
	}

	_ = os.Remove(headPath(taskDir))
	if _, err := store.ReconcileDerivedState(); err == nil {
		t.Fatal("reconciliation rebuilt a HEAD it cannot prove: deleting the tail entry produces the same state")
	}
	assertLedgerStillInvalid(t, taskDir, "ledger.head_missing")
}

// --- tamper matrix ---

func TestStorageTamperReceiptRejected(t *testing.T) {
	taskDir, c := cleanCandidate(t, recAt)
	res, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c})
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt the stored receipt CAS bytes.
	p := filepath.Join(taskDir, filepath.FromSlash(res.ReceiptRef.Path))
	if err := os.WriteFile(p, []byte(`{"tampered":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRecordedTransition(taskDir, c.Receipt.TransitionID); err == nil {
		t.Fatal("tampered receipt bytes must fail reload")
	}
}

func TestStorageTamperStageRejected(t *testing.T) {
	taskDir, c := cleanCandidate(t, recAt)
	res, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(taskDir, filepath.FromSlash(res.StageRefs[0].Ref.Path))
	if err := os.WriteFile(p, []byte(`{"tampered":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRecordedTransition(taskDir, c.Receipt.TransitionID); err == nil {
		t.Fatal("tampered stage bytes must fail reload")
	}
}

func TestLedgerTamperPayloadRejected(t *testing.T) {
	taskDir, c := cleanCandidate(t, recAt)
	if _, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c}); err != nil {
		t.Fatal(err)
	}
	// Corrupt the recorded event payload (find the result_transition_recorded payload).
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(recordingPayloadValidator))
	chain, _ := store.VerifyChain()
	last := chain.Entries[len(chain.Entries)-1]
	if err := os.WriteFile(last.PayloadPath, []byte("tampered: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyChain(); err == nil {
		t.Fatal("tampered ledger payload must fail chain verification")
	}
}
