// SPDX-License-Identifier: Apache-2.0

package ledger

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

func TestVerifyFailsWhenEntryIsTampered(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	res, err := store.Append(context.Background(), AppendRequest{
		TaskID: "task.example", SessionID: "session.example", ExpectedHeadDigestSHA256: "",
		EventType:        closureprotocol.LedgerEventTaskPrepared,
		Payload:          testPayload{SchemaVersion: "1", Message: "prepared"},
		PayloadMediaType: "application/yaml", ProducerID: "sensei.test", ProducedAt: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	entryPath := filepath.Join(taskDir, "ledger", ledgerEntryFilename(res.Entry.Sequence, res.Entry.EventType, res.Entry.EntryDigestSHA256))
	data, err := os.ReadFile(entryPath)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte("task_prepared"), []byte("closure_assessed"), 1)
	if err := os.WriteFile(entryPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid {
		t.Fatalf("expected invalid report after entry tamper: %+v", report)
	}
}

func TestVerifyFailsWhenPayloadIsTampered(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	res, err := store.Append(context.Background(), AppendRequest{
		TaskID: "task.example", SessionID: "session.example", ExpectedHeadDigestSHA256: "",
		EventType:        closureprotocol.LedgerEventTaskPrepared,
		Payload:          testPayload{SchemaVersion: "1", Message: "prepared"},
		PayloadMediaType: "application/yaml", ProducerID: "sensei.test", ProducedAt: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	payloadPath := filepath.Join(taskDir, filepath.FromSlash(res.PayloadPath))
	if err := os.WriteFile(payloadPath, []byte("schema_version: \"1\"\nmessage: tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid {
		t.Fatalf("expected invalid report after payload tamper: %+v", report)
	}
}

func TestVerifyFailsOnSequenceGap(t *testing.T) {
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
	second := buildManualEntry(t, taskDir, first.Entry, closureprotocol.LedgerEventClosureAssessed, testPayload{SchemaVersion: "1", Message: "closure"})
	oldPath := filepath.Join(taskDir, "ledger", ledgerEntryFilename(second.Sequence, second.EventType, second.EntryDigestSHA256))
	newPath := filepath.Join(taskDir, "ledger", ledgerEntryFilename(3, second.EventType, second.EntryDigestSHA256))
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid {
		t.Fatalf("expected invalid report after sequence gap: %+v", report)
	}
}

func TestAppendReplaysEquivalentCurrentHead(t *testing.T) {
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
	replay, err := store.Append(context.Background(), AppendRequest{
		TaskID: "task.example", SessionID: "session.example", ExpectedHeadDigestSHA256: "",
		EventType:        closureprotocol.LedgerEventTaskPrepared,
		Payload:          testPayload{SchemaVersion: "1", Message: "prepared"},
		PayloadMediaType: "application/yaml", ProducerID: "sensei.test", ProducedAt: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Replay || replay.Entry.EntryDigestSHA256 != first.Entry.EntryDigestSHA256 {
		t.Fatalf("expected replay of current head, got %+v", replay)
	}
}
