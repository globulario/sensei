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

// appendTruncationChain builds a valid two-entry chain and returns the store
// together with the tail entry, so a test can delete exactly the highest-sequence
// entry file and observe what verification then says.
func appendTruncationChain(t *testing.T, taskDir string) (*Store, Entry) {
	t.Helper()
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
	second, err := store.Append(context.Background(), AppendRequest{
		TaskID: "task.example", SessionID: "session.example", ExpectedHeadDigestSHA256: first.Entry.EntryDigestSHA256,
		EventType:        closureprotocol.LedgerEventClosureAssessed,
		Payload:          testPayload{SchemaVersion: "1", Message: "closure"},
		PayloadMediaType: "application/yaml", ProducerID: "sensei.test", ProducedAt: time.Date(2026, 7, 15, 12, 5, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.EntryCount != 2 {
		t.Fatalf("chain was not valid before truncation: %+v", report)
	}
	return store, second.Entry
}

// deleteTailEntry removes the highest-sequence entry file from the chain.
func deleteTailEntry(t *testing.T, taskDir string, tail Entry) {
	t.Helper()
	path := filepath.Join(taskDir, "ledger", ledgerEntryFilename(tail.Sequence, tail.EventType, tail.EntryDigestSHA256))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func reportHasError(report VerificationReport, code string) bool {
	for _, e := range report.Errors {
		if e.Code == code {
			return true
		}
	}
	return false
}

// TestVerifyRejectsTruncatedTailWithStaleHead is the truncation attack of issue
// #352: deleting the highest-sequence entry leaves HEAD pointing at an entry that
// is no longer on the chain. A ledger whose HEAD disagrees with its entries is
// damaged history, not an advisory, so it must not verify as valid.
func TestVerifyRejectsTruncatedTailWithStaleHead(t *testing.T) {
	taskDir := t.TempDir()
	store, tail := appendTruncationChain(t, taskDir)
	deleteTailEntry(t, taskDir, tail)

	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid {
		t.Fatalf("truncated chain verified as valid: %+v", report)
	}
	if !reportHasError(report, "ledger.head_stale") {
		t.Fatalf("expected ledger.head_stale error, got %+v", report.Errors)
	}
}

// TestVerifyRejectsTruncatedTailWithHeadRemoved is the same attack carried one
// deletion further. If only the stale case is promoted to an error, removing
// HEAD.yaml as well restores a clean verdict, because an absent HEAD matches
// neither branch of the HEAD check and reports nothing at all. A chain that has
// entries but no HEAD is damaged too.
func TestVerifyRejectsTruncatedTailWithHeadRemoved(t *testing.T) {
	taskDir := t.TempDir()
	store, tail := appendTruncationChain(t, taskDir)
	deleteTailEntry(t, taskDir, tail)
	if err := os.Remove(filepath.Join(taskDir, "ledger", "HEAD.yaml")); err != nil {
		t.Fatal(err)
	}

	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid {
		t.Fatalf("truncated chain with HEAD removed verified as valid: %+v", report)
	}
	if reportHasError(report, "ledger.head_stale") || reportHasError(report, "ledger.head_unreadable") {
		t.Fatalf("an absent HEAD is its own fact, not stale or unreadable: %+v", report.Errors)
	}
	if !reportHasError(report, "ledger.head_missing") {
		t.Fatalf("expected ledger.head_missing error, got %+v", report.Errors)
	}
}

// TestVerifyAcceptsEmptyLedgerWithNoHead pins the case the repair must not
// damage: a task with no entries and no HEAD has no chain yet. It is fresh, not
// truncated, and must keep verifying exactly as it did before.
func TestVerifyAcceptsEmptyLedgerWithNoHead(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))

	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid {
		t.Fatalf("empty ledger must verify as valid: %+v", report)
	}
	if report.EntryCount != 0 || len(report.Errors) != 0 {
		t.Fatalf("empty ledger must report no entries and no errors: %+v", report)
	}
}
