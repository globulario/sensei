// SPDX-License-Identifier: Apache-2.0

package ledger

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"gopkg.in/yaml.v3"
)

type testPayload struct {
	SchemaVersion string   `json:"schema_version" yaml:"schema_version"`
	Message       string   `json:"message" yaml:"message"`
	Items         []string `json:"items,omitempty" yaml:"items,omitempty"`
}

func TestAppendCreatesLedgerChainAndContentAddressedPayload(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	res, err := store.Append(context.Background(), AppendRequest{
		TaskID: "task.example", SessionID: "session.example", ExpectedHeadDigestSHA256: "",
		EventType:        closureprotocol.LedgerEventTaskPrepared,
		Payload:          testPayload{SchemaVersion: "1", Message: "prepared", Items: []string{"b", "a"}},
		PayloadMediaType: "application/yaml", ProducerID: "sensei.test", ProducedAt: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Entry.Sequence != 1 || res.Entry.EntryDigestSHA256 == "" {
		t.Fatalf("unexpected append result: %+v", res.Entry)
	}
	if _, err := os.Stat(filepath.Join(taskDir, filepath.FromSlash(res.PayloadPath))); err != nil {
		t.Fatalf("missing payload artifact: %v", err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.EntryCount != 1 || report.HeadDigestSHA256 != res.Entry.EntryDigestSHA256 {
		t.Fatalf("unexpected verify report: %+v", report)
	}
}

func TestAppendRejectsStaleWriter(t *testing.T) {
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
	_, err = store.Append(context.Background(), AppendRequest{
		TaskID: "task.example", SessionID: "session.example", ExpectedHeadDigestSHA256: "",
		EventType:        closureprotocol.LedgerEventClosureAssessed,
		Payload:          testPayload{SchemaVersion: "1", Message: "closure"},
		PayloadMediaType: "application/yaml", ProducerID: "sensei.test", ProducedAt: time.Date(2026, 7, 15, 12, 5, 0, 0, time.UTC),
	})
	var stale ErrStaleHead
	if err == nil || !errorAs(err, &stale) || stale.Actual != first.Entry.EntryDigestSHA256 {
		t.Fatalf("expected stale head error, got %v", err)
	}
}

func TestVerifyRecoversWhenEntryExistsButHeadIsStale(t *testing.T) {
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
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.HeadDigestSHA256 != second.EntryDigestSHA256 {
		t.Fatalf("unexpected verify report: %+v", report)
	}
	if len(report.Warnings) == 0 {
		t.Fatal("expected stale head warning")
	}
}

func TestVerifyReportsOrphanArtifacts(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	if _, err := store.Append(context.Background(), AppendRequest{
		TaskID: "task.example", SessionID: "session.example", ExpectedHeadDigestSHA256: "",
		EventType:        closureprotocol.LedgerEventTaskPrepared,
		Payload:          testPayload{SchemaVersion: "1", Message: "prepared"},
		PayloadMediaType: "application/yaml", ProducerID: "sensei.test", ProducedAt: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(taskDir, "artifacts", "sha256", "orphan.yaml")
	if err := os.MkdirAll(filepath.Dir(orphan), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphan, []byte("orphan: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.OrphanArtifacts) != 1 {
		t.Fatalf("expected one orphan artifact, got %+v", report.OrphanArtifacts)
	}
}

func buildManualEntry(t *testing.T, taskDir string, prev Entry, eventType closureprotocol.LedgerEventType, payload testPayload) Entry {
	t.Helper()
	rendered, err := renderPayload(payload, "application/yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := storePayloadArtifacts(taskDir, rendered); err != nil {
		t.Fatal(err)
	}
	entry := Entry{
		Sequence:                  prev.Sequence + 1,
		PreviousEntryDigestSHA256: prev.EntryDigestSHA256,
		EventType:                 eventType,
		Task:                      prev.Task,
		Payload:                   closureprotocol.LedgerPayloadRef{Path: rendered.path, MediaType: rendered.mediaType, DigestSHA256: rendered.semanticDigest},
		Producer:                  "sensei.test",
		ProducedAt:                time.Date(2026, 7, 15, 12, 5, 0, 0, time.UTC).Format(time.RFC3339),
	}
	digest, err := closureprotocol.LedgerEntryDigest(entry)
	if err != nil {
		t.Fatal(err)
	}
	entry.EntryDigestSHA256 = digest
	if err := writeEntry(filepath.Join(taskDir, "ledger", ledgerEntryFilename(entry.Sequence, entry.EventType, digest)), entry); err != nil {
		t.Fatal(err)
	}
	return entry
}

func testPayloadValidator(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
	var payload testPayload
	if err := yaml.Unmarshal(data, &payload); err != nil {
		return err
	}
	if payload.SchemaVersion != "1" || payload.Message == "" {
		return fmt.Errorf("invalid test payload")
	}
	return nil
}

func errorAs(err error, target any) bool {
	return errors.As(err, target)
}
