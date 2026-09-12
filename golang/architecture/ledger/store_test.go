// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"bytes"
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

// TestVerifyRejectsWhenEntryExistsButHeadIsStale pins the authority boundary.
//
// It replaces TestVerifyRecoversWhenEntryExistsButHeadIsStale, which asserted the
// pre-contract behaviour: report.Valid true, staleness a mere warning, and
// HeadDigestSHA256 silently replaced by the digest recomputed from the entries.
// That made generic verification a recovery authority -- it answered "what HEAD
// should say" while reporting it as "what HEAD says", so a caller could not tell a
// published pointer from a derived one. Recovery belongs to Store.Append, under
// the store that owns the ledger -- see
// TestStoreRecoversAFailedHeadPublicationUnderItsOwnLock, under the
// sensei_faultinject tag.
//
// A stale HEAD is now a typed integrity error, verification repairs nothing, and
// the report says what HEAD actually published.
func TestVerifyRejectsWhenEntryExistsButHeadIsStale(t *testing.T) {
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
	// A second entry lands on disk without HEAD being republished: exactly the
	// residue a HEAD-publication failure leaves behind.
	second := buildManualEntry(t, taskDir, first.Entry, closureprotocol.LedgerEventClosureAssessed, testPayload{SchemaVersion: "1", Message: "closure"})
	headBefore, err := os.ReadFile(filepath.Join(taskDir, "ledger", "HEAD.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}

	// 1. A stale pointer fails closed.
	if report.Valid {
		t.Error("report.Valid is true on an entry-present/HEAD-stale chain: a published " +
			"pointer that disagrees with the entries is an integrity failure, not a note")
	}
	// 2. Staleness is a typed integrity error, not a warning.
	if !hasErrorCode(report, "ledger.head_stale") {
		t.Errorf("no ledger.head_stale error; errors=%+v warnings=%+v", report.Errors, report.Warnings)
	}
	for _, w := range report.Warnings {
		if w.Code == "ledger.head_stale" {
			t.Error("ledger.head_stale is reported as a warning: a warning invites the caller " +
				"to proceed on a pointer the ledger cannot vouch for")
		}
	}
	// 3. The reported digest is NOT silently replaced with the recomputed one.
	if report.HeadDigestSHA256 == second.EntryDigestSHA256 {
		t.Error("HeadDigestSHA256 was replaced with the digest recomputed from the entries; " +
			"the report must say what HEAD published, not what it ought to publish")
	}
	if report.HeadDigestSHA256 != first.Entry.EntryDigestSHA256 {
		t.Errorf("HeadDigestSHA256 = %q, want the published (stale) digest %q",
			report.HeadDigestSHA256, first.Entry.EntryDigestSHA256)
	}
	// 4. Generic verification performs no repair or reconciliation.
	headAfter, err := os.ReadFile(filepath.Join(taskDir, "ledger", "HEAD.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(headBefore, headAfter) {
		t.Error("Verify rewrote HEAD.yaml: verification observes, it does not repair")
	}
}

// TestVerifyRejectsWhenEntriesExistButHeadIsMissing is the missing-pointer half of
// the same boundary. An absent HEAD is not an empty ledger.
func TestVerifyRejectsWhenEntriesExistButHeadIsMissing(t *testing.T) {
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
	if err := os.Remove(filepath.Join(taskDir, "ledger", "HEAD.yaml")); err != nil {
		t.Fatal(err)
	}

	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid {
		t.Error("report.Valid is true with entries present and no HEAD published")
	}
	if !hasErrorCode(report, "ledger.head_missing") {
		t.Errorf("no ledger.head_missing error; errors=%+v", report.Errors)
	}
	if report.HeadDigestSHA256 != "" {
		t.Errorf("HeadDigestSHA256 = %q with no HEAD published; want empty", report.HeadDigestSHA256)
	}
}

// TestVerifyAcceptsAnEmptyLedger guards the reverse direction: a task with no
// entries at all has nothing to publish, so an absent HEAD is correct there and
// the refusals above must not fire.
func TestVerifyAcceptsAnEmptyLedger(t *testing.T) {
	report, err := NewStore(t.TempDir(), WithPayloadValidator(testPayloadValidator)).Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.EntryCount != 0 {
		t.Fatalf("an empty ledger did not verify: %+v", report)
	}
}

func hasErrorCode(report VerificationReport, code string) bool {
	for _, e := range report.Errors {
		if e.Code == code {
			return true
		}
	}
	return false
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
