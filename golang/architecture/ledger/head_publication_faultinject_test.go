// SPDX-License-Identifier: AGPL-3.0-only

//go:build sensei_faultinject

package ledger

import (
	"context"
	"errors"
	"testing"
)

// Run: go test -tags sensei_faultinject ./golang/architecture/ledger/ -run HeadPublication

// A HEAD publication that fails fewer times than the bound is recovered inside
// Store.Append, under its lock: the caller sees an ordinary success.
func TestAppendRetriesAFailedHeadPublicationWithinTheBound(t *testing.T) {
	store := NewStore(t.TempDir(), WithPayloadValidator(testPayloadValidator))
	InjectHeadWriteFaults(headPublicationAttempts - 1)
	defer InjectHeadWriteFaults(0)

	res, err := store.Append(context.Background(), preparedRequest())
	if err != nil {
		t.Fatalf("append with %d transient HEAD faults: %v", headPublicationAttempts-1, err)
	}
	if n := PendingHeadWriteFaults(); n != 0 {
		t.Fatalf("%d faults never fired; the retry path was not exercised", n)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.HeadDigestSHA256 != res.Entry.EntryDigestSHA256 {
		t.Fatalf("HEAD not published after the retry: %+v", report)
	}
}

// The retry is BOUNDED: one more fault than it absorbs exhausts it, Append reports
// ErrEntryDurable, and the ledger fails closed until a later Append republishes it.
func TestAppendReportsADurableEntryOnceHeadPublicationRetriesAreExhausted(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	InjectHeadWriteFaults(headPublicationAttempts + 1)
	defer InjectHeadWriteFaults(0)

	_, err := store.Append(context.Background(), preparedRequest())
	var durable ErrEntryDurable
	if !errors.As(err, &durable) {
		t.Fatalf("err = %v, want ErrEntryDurable", err)
	}
	if n := PendingHeadWriteFaults(); n != 1 {
		t.Fatalf("pending faults = %d, want 1: Append must make exactly %d attempts", n, headPublicationAttempts)
	}
	InjectHeadWriteFaults(0)

	if report, _ := store.Verify(); report.Valid || !hasErrorCode(report, "ledger.head_missing") {
		t.Fatalf("unpublished HEAD verified: %+v", report)
	}
	if _, err := store.ReconcileDerivedState(); err == nil {
		t.Fatal("ReconcileDerivedState repaired an unpublished HEAD")
	}

	res, err := store.Append(context.Background(), preparedRequest())
	if err != nil || !res.Replay || res.Entry.EntryDigestSHA256 != durable.Entry.EntryDigestSHA256 {
		t.Fatalf("retry = %+v err=%v, want an exact replay of the durable entry", res, err)
	}
	if report, _ := store.Verify(); !report.Valid {
		t.Fatalf("Append did not republish HEAD: %+v", report)
	}
}
