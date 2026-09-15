// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"os"
	"path/filepath"
	"testing"
)

// corruptEntryAndVerify overwrites the entry at position idx of a valid five-entry chain
// with unreadable bytes and verifies the ledger. Verification must be total: one corrupt
// entry yields a typed fail-closed report, never a panic.
func corruptEntryAndVerify(t *testing.T, idx int) (VerificationReport, string) {
	t.Helper()
	store, chain := buildScopeChain(t, 5)
	entryPath := filepath.FromSlash(chain.Entries[idx].EntryPath)
	if err := os.WriteFile(entryPath, []byte("\x00\xff: [not yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatalf("verify must return a report, not an error: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected invalid report after corrupting entry %d: %+v", idx, report)
	}
	return report, filepath.ToSlash(entryPath)
}

func requireErrorAt(t *testing.T, report VerificationReport, code, path string) {
	t.Helper()
	for _, e := range report.Errors {
		if e.Code == code && e.Path == path {
			return
		}
	}
	t.Fatalf("expected %s for %s, got %+v", code, path, report.Errors)
}

func TestVerifyCorruptFirstEntryReturnsTypedRefusal(t *testing.T) {
	report, path := corruptEntryAndVerify(t, 0)
	requireErrorAt(t, report, "ledger.entry_unreadable", path)
}

func TestVerifyCorruptMiddleEntryReturnsTypedRefusal(t *testing.T) {
	report, path := corruptEntryAndVerify(t, 2)
	requireErrorAt(t, report, "ledger.entry_unreadable", path)
}
