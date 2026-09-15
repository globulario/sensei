// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"os"
	"path/filepath"
	"testing"
)

// corruptEntryAndVerify builds a valid chain of n entries, overwrites entry
// position idx with unreadable bytes, and verifies. A panic fails the test.
func corruptEntryAndVerify(t *testing.T, n, idx int) (VerificationReport, string) {
	t.Helper()
	store, chain := buildScopeChain(t, n)
	path := filepath.FromSlash(chain.Entries[idx].EntryPath)
	if err := os.WriteFile(path, []byte("\x00\xff{{{ not a ledger entry"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatalf("verify returned an error instead of a typed report: %v", err)
	}
	return report, filepath.ToSlash(path)
}

func assertUnreadableEntryReported(t *testing.T, report VerificationReport, path string) {
	t.Helper()
	if report.Valid {
		t.Fatalf("expected invalid report after corrupting %s: %+v", path, report)
	}
	for _, e := range report.Errors {
		if e.Code == "ledger.entry_unreadable" && e.Path == path {
			return
		}
	}
	t.Fatalf("expected ledger.entry_unreadable naming %s, got %+v", path, report.Errors)
}

// Issue #356: a corrupt FIRST entry left nothing verified, and the next entry
// indexed the empty entries slice with a files-slice position and panicked.
func TestVerifyCorruptFirstEntryReturnsTypedRefusal(t *testing.T) {
	report, path := corruptEntryAndVerify(t, 5, 0)
	assertUnreadableEntryReported(t, report, path)
}

// Issue #356: a corrupt MIDDLE entry shifts the entries slice behind the files
// slice, so a later entry read past its end and panicked.
func TestVerifyCorruptMiddleEntryReturnsTypedRefusal(t *testing.T) {
	report, path := corruptEntryAndVerify(t, 5, 2)
	assertUnreadableEntryReported(t, report, path)
}
