// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// appendFiveEntryChain builds a valid five-entry chain through the real Append
// path and returns the on-disk entry file of each sequence, in order.
func appendFiveEntryChain(t *testing.T, store *Store, taskDir string) []string {
	t.Helper()
	var (
		head  string
		paths []string
	)
	for i := 1; i <= 5; i++ {
		res, err := store.Append(context.Background(), AppendRequest{
			TaskID: "task.example", SessionID: "session.example", ExpectedHeadDigestSHA256: head,
			EventType:        closureprotocol.LedgerEventTaskPrepared,
			Payload:          testPayload{SchemaVersion: "1", Message: fmt.Sprintf("entry %d", i)},
			PayloadMediaType: "application/yaml", ProducerID: "sensei.test", ProducedAt: time.Date(2026, 7, 15, 12, i, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatal(err)
		}
		head = res.Entry.EntryDigestSHA256
		paths = append(paths, filepath.Join(taskDir, "ledger", ledgerEntryFilename(res.Entry.Sequence, res.Entry.EventType, res.Entry.EntryDigestSHA256)))
	}
	return paths
}

func makeEntryUnreadable(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("sequence: [unterminated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readEntry(path); err == nil {
		t.Fatalf("corruption of %s did not make the entry unreadable", path)
	}
}

// makeEntryInvalid keeps the entry parseable but empties its producer, so
// closureprotocol.ValidateLedgerEntry rejects it.
func makeEntryInvalid(t *testing.T, path string) {
	t.Helper()
	entry, err := readEntry(path)
	if err != nil {
		t.Fatal(err)
	}
	entry.Producer = ""
	if err := writeEntry(path, entry); err != nil {
		t.Fatal(err)
	}
	reread, err := readEntry(path)
	if err != nil {
		t.Fatalf("invalid entry must stay readable: %v", err)
	}
	if closureprotocol.ValidateLedgerEntry(reread) == nil {
		t.Fatalf("corruption of %s did not make the entry structurally invalid", path)
	}
}

func assertCorruptEntryReport(t *testing.T, store *Store, wantCode, wantPath string) {
	t.Helper()
	report, err := store.Verify()
	if err != nil {
		t.Fatalf("verify must return a typed report, got error: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected invalid report: %+v", report)
	}
	found := false
	for _, e := range report.Errors {
		if e.Code == wantCode && e.Path == filepath.ToSlash(wantPath) {
			found = true
		}
		if e.Code == "ledger.task_id_changed" {
			t.Fatalf("fabricated task_id_changed error at %s: %+v", e.Path, report.Errors)
		}
	}
	if !found {
		t.Fatalf("expected %s naming %s: %+v", wantCode, wantPath, report.Errors)
	}
}

func TestVerifyReportsUnreadableFirstEntryWithoutPanic(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	paths := appendFiveEntryChain(t, store, taskDir)
	makeEntryUnreadable(t, paths[0])
	assertCorruptEntryReport(t, store, "ledger.entry_unreadable", paths[0])
}

func TestVerifyReportsUnreadableMiddleEntryWithoutPanic(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	paths := appendFiveEntryChain(t, store, taskDir)
	makeEntryUnreadable(t, paths[2])
	assertCorruptEntryReport(t, store, "ledger.entry_unreadable", paths[2])
}

func TestVerifyReportsInvalidFirstEntryWithoutPanic(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	paths := appendFiveEntryChain(t, store, taskDir)
	makeEntryInvalid(t, paths[0])
	assertCorruptEntryReport(t, store, "ledger.entry_invalid", paths[0])
}
