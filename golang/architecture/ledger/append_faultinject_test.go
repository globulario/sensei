// SPDX-License-Identifier: AGPL-3.0-only

//go:build sensei_faultinject

// This file is compiled ONLY under the sensei_faultinject build tag, where the
// HEAD-write seam can be made to fail. Nothing here is reachable from a default
// build, and TestNoHistoryRewriteOrFaultToggleAPI proves it.

package ledger

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// appendOnce appends one entry to a fresh store and returns the store, the task
// dir and the append outcome.
func appendOnce(t *testing.T) (*Store, string, AppendResult, error) {
	t.Helper()
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	res, err := store.Append(context.Background(), AppendRequest{
		TaskID: "task.example", SessionID: "session.example", ExpectedHeadDigestSHA256: "",
		EventType:        closureprotocol.LedgerEventTaskPrepared,
		Payload:          testPayload{SchemaVersion: "1", Message: "prepared"},
		PayloadMediaType: "application/yaml", ProducerID: "sensei.test", ProducedAt: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	})
	return store, taskDir, res, err
}

// TestStoreRecoversAFailedHeadPublicationUnderItsOwnLock is the POSITIVE half of
// the authority boundary that TestVerifyRejectsWhenEntryExistsButHeadIsStale pins.
//
// Generic verification refuses a stale or missing pointer and repairs nothing.
// That is only correct because recovery still exists somewhere -- and the somewhere
// is the store that owns the ledger: Append reports honestly what it committed,
// and ReconcileDerivedState rebuilds the derived pointer from the verified entries
// under the same append lock. Recovery did not disappear when the caller-supplied
// fault option did, and it never belonged to the verifier.
//
// The whole cycle is asserted here -- residue, refusal, repair -- because each
// step alone can look correct while the cycle is broken. A fail-closed verifier
// that also made the residue unrepairable would turn a recoverable interruption
// into a permanent one.
func TestStoreRecoversAFailedHeadPublicationUnderItsOwnLock(t *testing.T) {
	ClearHeadWriteFaults()
	t.Cleanup(ClearHeadWriteFaults)
	InjectHeadWriteFaults(1)

	store, taskDir, res, err := appendOnce(t)

	// 1. RESIDUE. The entry is committed and Append says so, naming what it
	//    committed so the owner can reconcile rather than guess.
	var durable ErrEntryDurable
	if !errors.As(err, &durable) {
		t.Fatalf("err = %v, want ErrEntryDurable: a failed HEAD publication is post-commit", err)
	}
	if durable.Entry.EntryDigestSHA256 != res.Entry.EntryDigestSHA256 {
		t.Error("ErrEntryDurable does not carry the committed entry identity the owner must reconcile")
	}
	if _, serr := os.Stat(filepath.Join(taskDir, "ledger", "HEAD.yaml")); !os.IsNotExist(serr) {
		t.Errorf("HEAD.yaml exists (%v) despite publication failing: the fault did not fire, "+
			"so nothing below is actually exercising the recovery path", serr)
	}

	// 2. REFUSAL. The verifier fails closed on that residue instead of inferring
	//    the pointer from the entries and reporting it as published.
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid {
		t.Error("a durable entry with no published HEAD verified as valid")
	}
	if !hasErrorCode(report, "ledger.head_missing") {
		t.Errorf("no ledger.head_missing error; errors=%+v", report.Errors)
	}
	if report.HeadDigestSHA256 != "" {
		t.Errorf("HeadDigestSHA256 = %q with no HEAD published; the report must not "+
			"substitute the digest recomputed from the entries", report.HeadDigestSHA256)
	}

	// 3. REPAIR. The store's own derived-state repair closes the condition.
	rec, rerr := store.ReconcileDerivedState()
	if rerr != nil {
		t.Fatalf("ReconcileDerivedState could not repair an unpublished HEAD: %v", rerr)
	}
	if !rec.HeadRewritten {
		t.Error("reconcile reported no HEAD repair")
	}
	head, herr := readHead(filepath.Join(taskDir, "ledger", "HEAD.yaml"))
	if herr != nil {
		t.Fatalf("HEAD was not published by the repair: %v", herr)
	}
	if head.EntryDigestSHA256 != res.Entry.EntryDigestSHA256 {
		t.Errorf("repaired HEAD publishes %q, want the committed entry %q",
			head.EntryDigestSHA256, res.Entry.EntryDigestSHA256)
	}
	if report, err := store.Verify(); err != nil || !report.Valid || report.HeadDigestSHA256 != res.Entry.EntryDigestSHA256 {
		t.Fatalf("ledger did not verify after repair: %+v err=%v", report, err)
	}
}

// TestAStaleHeadResidueIsAlsoRepairable covers the stale-pointer residue, which is
// what a HEAD-publication failure leaves on a ledger that already had a HEAD.
//
// The refusal in TestVerifyRejectsWhenEntryExistsButHeadIsStale is only safe if
// this repair works, and it is a DIFFERENT code path from the missing-HEAD case:
// the pointer is readable, so nothing short of comparing it to the entries reveals
// that it is wrong.
func TestAStaleHeadResidueIsAlsoRepairable(t *testing.T) {
	ClearHeadWriteFaults()
	t.Cleanup(ClearHeadWriteFaults)

	// First append succeeds, so a real HEAD is published.
	store, taskDir, first, err := appendOnce(t)
	if err != nil {
		t.Fatal(err)
	}
	headPath := filepath.Join(taskDir, "ledger", "HEAD.yaml")

	// The second append commits but cannot publish, leaving HEAD pointing at the
	// first entry: present, readable, and wrong.
	InjectHeadWriteFaults(1)
	_, err = store.Append(context.Background(), AppendRequest{
		TaskID: "task.example", SessionID: "session.example",
		ExpectedHeadDigestSHA256: first.Entry.EntryDigestSHA256,
		EventType:                closureprotocol.LedgerEventClosureAssessed,
		Payload:                  testPayload{SchemaVersion: "1", Message: "closure"},
		PayloadMediaType:         "application/yaml", ProducerID: "sensei.test",
		ProducedAt: time.Date(2026, 7, 15, 12, 5, 0, 0, time.UTC),
	})
	var durable ErrEntryDurable
	if !errors.As(err, &durable) {
		t.Fatalf("err = %v, want ErrEntryDurable", err)
	}

	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid || !hasErrorCode(report, "ledger.head_stale") {
		t.Fatalf("a stale published pointer did not fail closed: %+v", report)
	}
	if report.HeadDigestSHA256 != first.Entry.EntryDigestSHA256 {
		t.Errorf("HeadDigestSHA256 = %q, want the stale published digest %q: the report "+
			"must say what HEAD published", report.HeadDigestSHA256, first.Entry.EntryDigestSHA256)
	}

	if _, rerr := store.ReconcileDerivedState(); rerr != nil {
		t.Fatalf("ReconcileDerivedState could not repair a stale HEAD: %v", rerr)
	}
	head, herr := readHead(headPath)
	if herr != nil {
		t.Fatal(herr)
	}
	if head.EntryDigestSHA256 != durable.Entry.EntryDigestSHA256 {
		t.Errorf("repaired HEAD publishes %q, want the second entry %q",
			head.EntryDigestSHA256, durable.Entry.EntryDigestSHA256)
	}
	if report, err := store.Verify(); err != nil || !report.Valid {
		t.Fatalf("ledger did not verify after repair: %+v err=%v", report, err)
	}
}
