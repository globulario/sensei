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

// TestVerifyRejectsWhenEntryExistsButHeadIsStale replaces, by the owner's
// authorization, TestVerifyRecoversWhenEntryExistsButHeadIsStale. That test pinned
// the pre-contract behaviour -- Valid true, staleness a warning, HeadDigestSHA256
// silently replaced by the digest recomputed from the entries -- which made generic
// verification a recovery authority. A stale HEAD is a typed integrity error, the
// report says what HEAD published, and verification repairs nothing.
func TestVerifyRejectsWhenEntryExistsButHeadIsStale(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	first := appendPrepared(t, store)
	// A second entry lands without HEAD being republished: the residue a failed
	// HEAD publication leaves behind.
	second := buildManualEntry(t, taskDir, first.Entry, closureprotocol.LedgerEventClosureAssessed, testPayload{SchemaVersion: "1", Message: "closure"})
	headBefore := readHeadBytes(t, taskDir)

	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid {
		t.Error("report.Valid is true on an entry-present/HEAD-stale chain")
	}
	if !hasErrorCode(report, "ledger.head_stale") {
		t.Errorf("no ledger.head_stale error; errors=%+v", report.Errors)
	}
	for _, w := range report.Warnings {
		if w.Code == "ledger.head_stale" {
			t.Error("ledger.head_stale is reported as a warning, not an integrity error")
		}
	}
	if report.HeadDigestSHA256 == second.EntryDigestSHA256 {
		t.Error("HeadDigestSHA256 was replaced with the digest recomputed from the entries")
	}
	if report.HeadDigestSHA256 != first.Entry.EntryDigestSHA256 {
		t.Errorf("HeadDigestSHA256 = %q, want the published digest %q", report.HeadDigestSHA256, first.Entry.EntryDigestSHA256)
	}
	if !bytes.Equal(headBefore, readHeadBytes(t, taskDir)) {
		t.Error("Verify rewrote HEAD.yaml: verification observes, it does not repair")
	}
}

// An absent HEAD is not an empty ledger.
func TestVerifyRejectsWhenEntriesExistButHeadIsMissing(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	appendPrepared(t, store)
	if err := os.Remove(headFile(taskDir)); err != nil {
		t.Fatal(err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid || !hasErrorCode(report, "ledger.head_missing") {
		t.Errorf("missing HEAD with entries present verified: %+v", report)
	}
	if report.HeadDigestSHA256 != "" {
		t.Errorf("HeadDigestSHA256 = %q with no HEAD published; want empty", report.HeadDigestSHA256)
	}
	if _, err := os.Stat(headFile(taskDir)); !os.IsNotExist(err) {
		t.Error("Verify published a HEAD")
	}
}

// The reverse direction: with no entries there is nothing to publish.
func TestVerifyAcceptsAnEmptyLedger(t *testing.T) {
	report, err := NewStore(t.TempDir(), WithPayloadValidator(testPayloadValidator)).Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.EntryCount != 0 {
		t.Fatalf("an empty ledger did not verify: %+v", report)
	}
}

// Generic callers can neither read through an unpublished HEAD nor repair it.
func TestGenericCallersCannotReadThroughOrRepairAStaleHead(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	first := appendPrepared(t, store)
	buildManualEntry(t, taskDir, first.Entry, closureprotocol.LedgerEventClosureAssessed, testPayload{SchemaVersion: "1", Message: "closure"})
	headBefore := readHeadBytes(t, taskDir)

	if _, err := store.VerifyChain(); err == nil {
		t.Error("VerifyChain read through a stale HEAD")
	}
	if _, err := RebuildProjections(taskDir, testPayloadValidator); err == nil {
		t.Error("RebuildProjections read through a stale HEAD")
	}
	if _, err := store.ReconcileDerivedState(); err == nil {
		t.Error("ReconcileDerivedState accepted a stale HEAD")
	}
	if !bytes.Equal(headBefore, readHeadBytes(t, taskDir)) {
		t.Error("a generic caller rewrote HEAD.yaml")
	}
}

// TestAppendRecoversAnUnpublishedHeadUnderItsOwnLock is the positive half: the
// residue of an interrupted publication is republished by Store.Append, and a
// retry of the append that left it resolves as an exact replay.
func TestAppendRecoversAnUnpublishedHeadUnderItsOwnLock(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	first := appendPrepared(t, store)
	second := AppendRequest{
		TaskID: "task.example", SessionID: "session.example", ExpectedHeadDigestSHA256: first.Entry.EntryDigestSHA256,
		EventType:        closureprotocol.LedgerEventClosureAssessed,
		Payload:          testPayload{SchemaVersion: "1", Message: "closure"},
		PayloadMediaType: "application/yaml", ProducerID: "sensei.test", ProducedAt: time.Date(2026, 7, 15, 12, 5, 0, 0, time.UTC),
	}
	if _, err := store.Append(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	durable, err := store.VerifyChain()
	if err != nil {
		t.Fatal(err)
	}
	// Roll HEAD back to the first entry: exactly what a failed publication leaves.
	if err := writeHead(headFile(taskDir), Head{
		SchemaVersion: HeadSchemaVersion, TaskID: "task.example", Sequence: 1,
		EntryDigestSHA256: first.Entry.EntryDigestSHA256, EntryPath: first.Head.EntryPath,
	}); err != nil {
		t.Fatal(err)
	}
	if report, _ := store.Verify(); report.Valid {
		t.Fatal("precondition: the rolled-back HEAD must fail verification")
	}

	res, err := store.Append(context.Background(), second)
	if err != nil {
		t.Fatalf("retry of the durable append: %v", err)
	}
	if !res.Replay || res.Entry.EntryDigestSHA256 != durable.Head.EntryDigestSHA256 {
		t.Fatalf("retry = %+v, want an exact replay of the durable entry", res)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.EntryCount != 2 || report.HeadDigestSHA256 != durable.Head.EntryDigestSHA256 {
		t.Fatalf("HEAD not republished by Append: %+v", report)
	}
}

// A single-entry chain whose HEAD was never published is the other residue shape.
func TestAppendRecoversAFirstEntryWhoseHeadWasNeverPublished(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	first := appendPrepared(t, store)
	if err := os.Remove(headFile(taskDir)); err != nil {
		t.Fatal(err)
	}
	res, err := store.Append(context.Background(), preparedRequest())
	if err != nil {
		t.Fatalf("retry of the first append: %v", err)
	}
	if !res.Replay || res.Entry.EntryDigestSHA256 != first.Entry.EntryDigestSHA256 {
		t.Fatalf("retry = %+v, want an exact replay", res)
	}
	if report, _ := store.Verify(); !report.Valid {
		t.Fatalf("HEAD not republished: %+v", report)
	}
}

// Append recovers ONLY publication residue. A HEAD naming an entry the chain no
// longer contains is tail truncation (#352); a missing HEAD on a longer chain is
// not something an interrupted publication can produce. Both stay refused, and
// HEAD is left as it was.
func TestAppendRefusesHeadDamageThatIsNotPublicationResidue(t *testing.T) {
	for _, tc := range []struct {
		name   string
		damage func(t *testing.T, taskDir string, second AppendResult)
	}{
		{"tail truncated", func(t *testing.T, taskDir string, second AppendResult) {
			if err := os.Remove(filepath.Join(taskDir, filepath.FromSlash(second.Head.EntryPath))); err != nil {
				t.Fatal(err)
			}
		}},
		{"head missing on a longer chain", func(t *testing.T, taskDir string, _ AppendResult) {
			if err := os.Remove(headFile(taskDir)); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			taskDir := t.TempDir()
			store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
			first := appendPrepared(t, store)
			second, err := store.Append(context.Background(), AppendRequest{
				TaskID: "task.example", SessionID: "session.example", ExpectedHeadDigestSHA256: first.Entry.EntryDigestSHA256,
				EventType:        closureprotocol.LedgerEventClosureAssessed,
				Payload:          testPayload{SchemaVersion: "1", Message: "closure"},
				PayloadMediaType: "application/yaml", ProducerID: "sensei.test", ProducedAt: time.Date(2026, 7, 15, 12, 5, 0, 0, time.UTC),
			})
			if err != nil {
				t.Fatal(err)
			}
			tc.damage(t, taskDir, second)
			headBefore, _ := os.ReadFile(headFile(taskDir))

			if _, err := store.Append(context.Background(), preparedRequest()); err == nil {
				t.Fatal("Append accepted a ledger whose HEAD damage is not publication residue")
			}
			if report, _ := store.Verify(); report.Valid {
				t.Fatalf("damaged ledger verified after Append: %+v", report)
			}
			if headAfter, _ := os.ReadFile(headFile(taskDir)); !bytes.Equal(headBefore, headAfter) {
				t.Fatal("Append rewrote HEAD over damage it must refuse")
			}
		})
	}
}

func preparedRequest() AppendRequest {
	return AppendRequest{
		TaskID: "task.example", SessionID: "session.example", ExpectedHeadDigestSHA256: "",
		EventType:        closureprotocol.LedgerEventTaskPrepared,
		Payload:          testPayload{SchemaVersion: "1", Message: "prepared"},
		PayloadMediaType: "application/yaml", ProducerID: "sensei.test", ProducedAt: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	}
}

func appendPrepared(t *testing.T, store *Store) AppendResult {
	t.Helper()
	res, err := store.Append(context.Background(), preparedRequest())
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func headFile(taskDir string) string { return filepath.Join(taskDir, "ledger", "HEAD.yaml") }

func readHeadBytes(t *testing.T, taskDir string) []byte {
	t.Helper()
	data, err := os.ReadFile(headFile(taskDir))
	if err != nil {
		t.Fatal(err)
	}
	return data
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
