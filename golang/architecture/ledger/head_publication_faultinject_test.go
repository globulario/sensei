// SPDX-License-Identifier: AGPL-3.0-only

//go:build sensei_faultinject

package ledger

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

// Run: go test -tags sensei_faultinject ./golang/architecture/ledger/ -run HeadPublication

func publicationFaults(n int) []error {
	errs := make([]error, n)
	for i := range errs {
		errs[i] = fmt.Errorf("injected HEAD publication fault %d", i+1)
	}
	return errs
}

// A HEAD publication that fails fewer times than the bound is recovered inside
// Store.Append, under its lock: the caller sees an ordinary success.
func TestAppendRetriesAFailedHeadPublicationWithinTheBound(t *testing.T) {
	store := NewStore(t.TempDir(), WithPayloadValidator(testPayloadValidator),
		WithHeadPublicationFaults(publicationFaults(headPublicationAttempts-1)...))

	res, err := store.Append(context.Background(), preparedRequest())
	if err != nil {
		t.Fatalf("append with %d transient HEAD faults: %v", headPublicationAttempts-1, err)
	}
	if n := len(store.headFaults.errs); n != 0 {
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

// TestAppendRecoversAFailedHeadPublicationUnderItsTransactionalOwner is the
// positive half of the boundary TestVerifyRejectsWhenEntryExistsButHeadIsStale
// pins, driven by a REAL publication failure rather than a hand-built residue.
//
// The retry is BOUNDED: one more fault than it absorbs exhausts it, Append reports
// ErrEntryDurable, and the ledger fails closed -- to Verify, to generic chain reads
// and to ReconcileDerivedState alike. A later Append, under its own lock, is what
// republishes HEAD, and it resolves the retried request as an exact replay.
func TestAppendRecoversAFailedHeadPublicationUnderItsTransactionalOwner(t *testing.T) {
	taskDir := t.TempDir()
	faults := publicationFaults(headPublicationAttempts + 1)
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator), WithHeadPublicationFaults(faults...))

	_, err := store.Append(context.Background(), preparedRequest())
	var durable ErrEntryDurable
	if !errors.As(err, &durable) {
		t.Fatalf("err = %v, want ErrEntryDurable", err)
	}
	if n := len(store.headFaults.errs); n != 1 {
		t.Fatalf("pending faults = %d, want 1: Append must make exactly %d attempts", n, headPublicationAttempts)
	}
	if last := faults[headPublicationAttempts-1].Error(); !strings.Contains(durable.Detail, last) {
		t.Errorf("ErrEntryDurable detail %q does not carry the last injected fault %q", durable.Detail, last)
	}
	store.headFaults.errs = nil

	// The ledger fails closed on the residue, and no generic caller repairs it.
	if report, _ := store.Verify(); report.Valid || !hasErrorCode(report, "ledger.head_missing") {
		t.Fatalf("unpublished HEAD verified: %+v", report)
	}
	if _, err := store.VerifyChain(); err == nil {
		t.Fatal("VerifyChain read through an unpublished HEAD")
	}
	if _, err := store.ReconcileDerivedState(); err == nil {
		t.Fatal("ReconcileDerivedState repaired an unpublished HEAD")
	}
	if _, err := os.Stat(headFile(taskDir)); !os.IsNotExist(err) {
		t.Fatal("a generic caller published HEAD")
	}

	// The transactional owner recovers it.
	res, err := store.Append(context.Background(), preparedRequest())
	if err != nil || !res.Replay || res.Entry.EntryDigestSHA256 != durable.Entry.EntryDigestSHA256 {
		t.Fatalf("retry = %+v err=%v, want an exact replay of the durable entry", res, err)
	}
	if report, _ := store.Verify(); !report.Valid || report.HeadDigestSHA256 != durable.Entry.EntryDigestSHA256 {
		t.Fatalf("Append did not republish HEAD: %+v", report)
	}
}

// WithHeadPublicationFault arms exactly one fault, and only on the store it was
// given to.
func TestWithHeadPublicationFaultIsOneShotAndInstanceScoped(t *testing.T) {
	armed := NewStore(t.TempDir(), WithPayloadValidator(testPayloadValidator),
		WithHeadPublicationFault(errors.New("injected HEAD publication fault")))
	if n := len(armed.headFaults.errs); n != 1 {
		t.Fatalf("armed faults = %d, want 1", n)
	}
	other := NewStore(t.TempDir(), WithPayloadValidator(testPayloadValidator))
	if n := len(other.headFaults.errs); n != 0 {
		t.Fatalf("an unarmed store carries %d faults", n)
	}

	if _, err := armed.Append(context.Background(), preparedRequest()); err != nil {
		t.Fatalf("one fault must be absorbed by the bounded retry: %v", err)
	}
	if n := len(armed.headFaults.errs); n != 0 {
		t.Fatalf("%d faults never fired", n)
	}
}
