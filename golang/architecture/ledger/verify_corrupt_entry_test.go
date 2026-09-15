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

// appendTestChain appends n valid entries and returns their entry paths in order.
func appendTestChain(t *testing.T, store *Store, taskDir string, n int) []string {
	t.Helper()
	var (
		head  string
		paths []string
	)
	for i := 0; i < n; i++ {
		res, err := store.Append(context.Background(), AppendRequest{
			TaskID: "task.example", SessionID: "session.example", ExpectedHeadDigestSHA256: head,
			EventType:        closureprotocol.LedgerEventTaskPrepared,
			Payload:          testPayload{SchemaVersion: "1", Message: fmt.Sprintf("entry %d", i+1)},
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

func assertCorruptEntryRefused(t *testing.T, store *Store, corruptPath string) {
	t.Helper()
	report, err := store.Verify()
	if err != nil {
		t.Fatalf("expected a typed report, got error %v", err)
	}
	if report.Valid {
		t.Fatalf("expected invalid report after entry corruption: %+v", report)
	}
	want := filepath.ToSlash(corruptPath)
	for _, e := range report.Errors {
		if e.Code == "ledger.entry_unreadable" && e.Path == want {
			return
		}
	}
	t.Fatalf("expected ledger.entry_unreadable naming %s, got %+v", want, report.Errors)
}

func TestVerifyRefusesUnreadableFirstEntryWithoutPanic(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	paths := appendTestChain(t, store, taskDir, 5)
	if err := os.WriteFile(paths[0], []byte("\x00\xffnot a ledger entry{{"), 0o644); err != nil {
		t.Fatal(err)
	}
	assertCorruptEntryRefused(t, store, paths[0])
}

func TestVerifyRefusesUnreadableMiddleEntryWithoutPanic(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	paths := appendTestChain(t, store, taskDir, 5)
	if err := os.WriteFile(paths[2], []byte("\x00\xffnot a ledger entry{{"), 0o644); err != nil {
		t.Fatal(err)
	}
	assertCorruptEntryRefused(t, store, paths[2])
}
