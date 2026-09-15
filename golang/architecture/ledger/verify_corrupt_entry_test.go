// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	unreadableEntryBytes = "\x00\xff{{{ not a ledger entry"
	// Parses as YAML but fails closureprotocol.ValidateLedgerEntry.
	structurallyInvalidEntryBytes = "sequence: 0\n"
)

// corruptEntryAndVerify builds a valid chain of n entries, overwrites entry
// position idx with content, and verifies. A panic fails the test.
func corruptEntryAndVerify(t *testing.T, n, idx int, content string) (VerificationReport, string) {
	t.Helper()
	store, chain := buildScopeChain(t, n)
	path := filepath.FromSlash(chain.Entries[idx].EntryPath)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatalf("verify returned an error instead of a typed report: %v", err)
	}
	return report, filepath.ToSlash(path)
}

func assertEntryRefused(t *testing.T, report VerificationReport, code, path string) {
	t.Helper()
	if report.Valid {
		t.Fatalf("expected invalid report after corrupting %s: %+v", path, report)
	}
	for _, e := range report.Errors {
		if e.Code == code && e.Path == path {
			return
		}
	}
	t.Fatalf("expected %s naming %s, got %+v", code, path, report.Errors)
}

// With the first entry skipped, the first verified entry establishes the task
// id; the surviving same-task entries must not be reported as a task change.
func assertTaskIDFromFirstVerifiedEntry(t *testing.T, report VerificationReport) {
	t.Helper()
	for _, e := range report.Errors {
		if e.Code == "ledger.task_id_changed" {
			t.Fatalf("invented task_id_changed for a same-task chain: %+v", report.Errors)
		}
	}
	if report.TaskID != "task.scope" {
		t.Fatalf("expected task id of the first verified entry, got %q", report.TaskID)
	}
}

// Issue #356: a corrupt FIRST entry left nothing verified, and the next entry
// indexed the empty entries slice with a files-slice position and panicked.
func TestVerifyCorruptFirstEntryReturnsTypedRefusal(t *testing.T) {
	report, path := corruptEntryAndVerify(t, 5, 0, unreadableEntryBytes)
	assertEntryRefused(t, report, "ledger.entry_unreadable", path)
	assertTaskIDFromFirstVerifiedEntry(t, report)
}

// Issue #356: a corrupt MIDDLE entry shifts the entries slice behind the files
// slice, so a later entry read past its end and panicked.
func TestVerifyCorruptMiddleEntryReturnsTypedRefusal(t *testing.T) {
	report, path := corruptEntryAndVerify(t, 5, 2, unreadableEntryBytes)
	assertEntryRefused(t, report, "ledger.entry_unreadable", path)
}

// Issue #356: the ledger.entry_invalid skip diverges the slices the same way.
func TestVerifyStructurallyInvalidFirstEntryReturnsTypedRefusal(t *testing.T) {
	report, path := corruptEntryAndVerify(t, 5, 0, structurallyInvalidEntryBytes)
	assertEntryRefused(t, report, "ledger.entry_invalid", path)
	assertTaskIDFromFirstVerifiedEntry(t, report)
}

func TestVerifyStructurallyInvalidMiddleEntryReturnsTypedRefusal(t *testing.T) {
	report, path := corruptEntryAndVerify(t, 5, 2, structurallyInvalidEntryBytes)
	assertEntryRefused(t, report, "ledger.entry_invalid", path)
}
