// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"os"
	"path/filepath"
	"testing"
)

// verifyWithoutPanic runs Verify and turns a panic into a test failure, so a
// regression of issue #356 is reported as a witness failure, not a crashed binary.
func verifyWithoutPanic(t *testing.T, store *Store) (report VerificationReport) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Verify panicked on a corrupt entry instead of returning a typed refusal: %v", r)
		}
	}()
	report, err := store.Verify()
	if err != nil {
		t.Fatalf("Verify returned an error instead of a report: %v", err)
	}
	return report
}

func corruptEntryFixture(t *testing.T, position int) (*Store, string) {
	t.Helper()
	store, _ := buildScopeChain(t, 5)
	files, err := listLedgerEntryFiles(store.ledgerDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 5 {
		t.Fatalf("fixture expected 5 entries, got %d", len(files))
	}
	return store, files[position]
}

func assertCorruptEntryReport(t *testing.T, report VerificationReport, wantCode, wantPath string) {
	t.Helper()
	if report.Valid {
		t.Fatalf("expected invalid report, got %+v", report)
	}
	found := false
	for _, e := range report.Errors {
		if e.Code == wantCode && e.Path == filepath.ToSlash(wantPath) {
			found = true
		}
		if e.Code == "ledger.task_id_changed" {
			t.Errorf("fabricated task_id_changed error: %+v", e)
		}
	}
	if !found {
		t.Fatalf("expected %s naming %s, got %+v", wantCode, wantPath, report.Errors)
	}
}

func TestVerifyUnreadableFirstEntryIsTypedRefusal(t *testing.T) {
	store, path := corruptEntryFixture(t, 0)
	if err := os.WriteFile(path, []byte("sequence: [unterminated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	assertCorruptEntryReport(t, verifyWithoutPanic(t, store), "ledger.entry_unreadable", path)
}

func TestVerifyUnreadableMiddleEntryIsTypedRefusal(t *testing.T) {
	store, path := corruptEntryFixture(t, 2)
	if err := os.WriteFile(path, []byte("sequence: [unterminated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	assertCorruptEntryReport(t, verifyWithoutPanic(t, store), "ledger.entry_unreadable", path)
}

func TestVerifyInvalidFirstEntryIsTypedRefusal(t *testing.T) {
	store, path := corruptEntryFixture(t, 0)
	entry, err := readEntry(path)
	if err != nil {
		t.Fatal(err)
	}
	entry.Producer = ""
	if err := writeEntry(path, entry); err != nil {
		t.Fatal(err)
	}
	if _, err := readEntry(path); err != nil {
		t.Fatalf("fixture must stay readable so the entry_invalid site is exercised: %v", err)
	}
	assertCorruptEntryReport(t, verifyWithoutPanic(t, store), "ledger.entry_invalid", path)
}
