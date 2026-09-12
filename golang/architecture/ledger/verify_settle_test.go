// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// A reader without the append lock can observe an Append between its durable entry
// write and its HEAD publication. That window is not HEAD damage: generic readers
// wait for the append owner and read the settled ledger rather than refusing it.
func TestGenericReadersSettleAnInFlightHeadPublication(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	appendPrepared(t, store)
	published := readHeadBytes(t, taskDir)

	release, err := acquireLock(context.Background(), store.lockDir())
	if err != nil {
		t.Fatal(err)
	}
	// The Append in flight: its entry is durable, its HEAD is not yet published.
	if err := os.Remove(headFile(taskDir)); err != nil {
		t.Fatal(err)
	}
	chainErr := make(chan error, 1)
	reportOut := make(chan VerificationReport, 1)
	go func() {
		_, err := store.VerifyChain()
		chainErr <- err
	}()
	go func() {
		report, err := store.Verify()
		if err != nil {
			t.Error(err)
		}
		reportOut <- report
	}()
	time.Sleep(50 * time.Millisecond)
	if err := writeFileAtomic(headFile(taskDir), published); err != nil {
		t.Fatal(err)
	}
	release()

	if err := <-chainErr; err != nil {
		t.Errorf("VerifyChain refused a ledger whose Append was still publishing HEAD: %v", err)
	}
	if report := <-reportOut; !report.Valid {
		t.Errorf("Verify refused a ledger whose Append was still publishing HEAD: %+v", report.Errors)
	}
}

// Settling is a wait, never a repair: an owner that releases the lock without
// publishing HEAD leaves the ledger refused, and the reader publishes nothing.
func TestSettledReadStillRefusesAHeadTheOwnerNeverPublished(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	appendPrepared(t, store)

	release, err := acquireLock(context.Background(), store.lockDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(headFile(taskDir)); err != nil {
		t.Fatal(err)
	}
	chainErr := make(chan error, 1)
	go func() {
		_, err := store.VerifyChain()
		chainErr <- err
	}()
	time.Sleep(50 * time.Millisecond)
	release()

	if err := <-chainErr; err == nil || !strings.Contains(err.Error(), "ledger.head_missing") {
		t.Errorf("VerifyChain err = %v, want a ledger.head_missing refusal", err)
	}
	if _, err := os.Stat(headFile(taskDir)); !os.IsNotExist(err) {
		t.Error("a settled read published HEAD")
	}
	if _, err := os.Stat(store.lockDir()); !os.IsNotExist(err) {
		t.Error("a settled read left the append lock held")
	}
}
