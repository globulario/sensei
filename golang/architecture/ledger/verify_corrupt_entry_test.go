// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// TestVerifyCorruptEntryRefusesWithoutPanic covers issue #356: an entry that
// fails to parse is recorded and skipped without being appended, so any
// previous-entry lookup keyed on the files-slice index reads out of range.
// The verifier must still produce a typed refusal.
func TestVerifyCorruptEntryRefusesWithoutPanic(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	first, err := store.Append(context.Background(), AppendRequest{
		TaskID: "task.example", SessionID: "session.example", ExpectedHeadDigestSHA256: "",
		EventType:        closureprotocol.LedgerEventTaskPrepared,
		Payload:          testPayload{SchemaVersion: "1", Message: "prepared"},
		PayloadMediaType: "application/yaml", ProducerID: "sensei.test", ProducedAt: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(context.Background(), AppendRequest{
		TaskID: "task.example", SessionID: "session.example", ExpectedHeadDigestSHA256: first.Entry.EntryDigestSHA256,
		EventType:        closureprotocol.LedgerEventClosureAssessed,
		Payload:          testPayload{SchemaVersion: "1", Message: "closure"},
		PayloadMediaType: "application/yaml", ProducerID: "sensei.test", ProducedAt: time.Date(2026, 7, 15, 12, 5, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}

	firstPath := filepath.Join(taskDir, "ledger", ledgerEntryFilename(first.Entry.Sequence, first.Entry.EventType, first.Entry.EntryDigestSHA256))
	if err := os.WriteFile(firstPath, []byte("\tthis: is: not: valid: yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := store.Verify()
	if err != nil {
		t.Fatalf("verify returned a transport error instead of a refusal: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected invalid report after corrupting the first entry: %+v", report)
	}
	if len(report.Errors) == 0 {
		t.Fatal("expected at least one typed verification error")
	}
	var sawUnreadable bool
	for _, e := range report.Errors {
		if e.Code == "ledger.entry_unreadable" {
			sawUnreadable = true
		}
	}
	if !sawUnreadable {
		t.Fatalf("expected a ledger.entry_unreadable error: %+v", report.Errors)
	}
}
