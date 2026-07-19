// SPDX-License-Identifier: AGPL-3.0-only

package certification

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

var testProducedAt = time.Date(2026, 7, 15, 12, 30, 0, 0, time.UTC)

// seedTaskDir creates a task directory with one valid task_prepared ledger
// entry, publishes every record content-addressed, and writes the typed
// certification request. Returns the task dir and the current ledger head.
func seedTaskDir(t *testing.T, req Request, rec Records) (string, string) {
	t.Helper()
	taskDir := t.TempDir()
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(taskPayloadValidator))
	seed, err := store.Append(context.Background(), ledger.AppendRequest{
		TaskID:                   req.TaskID,
		SessionID:                greenSessionID,
		ExpectedHeadDigestSHA256: "",
		EventType:                closureprotocol.LedgerEventTaskPrepared,
		Payload: ledger.TaskEventPayload{
			SchemaVersion: ledger.EventPayloadSchemaVersion,
			EventType:     closureprotocol.LedgerEventTaskPrepared,
			TaskID:        req.TaskID,
			SessionID:     greenSessionID,
			TaskPhase:     closureprotocol.PhasePrepared,
		},
		PayloadMediaType: "application/yaml",
		ProducerID:       "certification tests",
		ProducedAt:       testProducedAt.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("seed ledger: %v", err)
	}

	writeAll := func(records ...any) {
		for _, record := range records {
			if _, err := WriteRecordArtifact(taskDir, record); err != nil {
				t.Fatalf("write record: %v", err)
			}
		}
	}
	writeAll(rec.AdmissionRequest, rec.AdmissionDecision, rec.CapabilityConsumption, rec.ScopeVerification)
	if rec.RuntimeTarget != nil {
		writeAll(*rec.RuntimeTarget)
	}
	for _, record := range rec.AuthorityResolutions {
		writeAll(record)
	}
	for _, record := range rec.ProofDischarges {
		writeAll(record)
	}
	for _, record := range rec.Obligations {
		writeAll(record)
	}
	for _, record := range rec.EvidenceProfiles {
		writeAll(record)
	}
	for _, record := range rec.EvidenceReceipts {
		writeAll(record)
	}
	for _, record := range rec.Waivers {
		writeAll(record)
	}
	for _, record := range rec.Revocations {
		writeAll(record)
	}
	for _, record := range rec.ForbiddenMoveFindings {
		writeAll(record)
	}

	requestBytes, err := CanonicalRequestYAML(req)
	if err != nil {
		t.Fatalf("render request: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, RequestFileName), requestBytes, 0o644); err != nil {
		t.Fatalf("write request: %v", err)
	}
	return taskDir, seed.Head.EntryDigestSHA256
}

func certifyInFreshDir(t *testing.T, req Request, rec Records) string {
	t.Helper()
	taskDir, head := seedTaskDir(t, req, rec)
	res, err := CertifyTask(TaskCertifyOptions{
		TaskDir:                  taskDir,
		ExpectedHeadDigestSHA256: head,
		ProducedAt:               testProducedAt,
	})
	if err != nil {
		t.Fatalf("CertifyTask: %v", err)
	}
	return res.Result.Receipt.DigestSHA256
}

func chainEventTypes(t *testing.T, taskDir string) []closureprotocol.LedgerEventType {
	t.Helper()
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(taskPayloadValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		t.Fatalf("verify chain: %v", err)
	}
	var out []closureprotocol.LedgerEventType
	for _, entry := range chain.Entries {
		out = append(out, entry.Entry.EventType)
	}
	return out
}

func TestCertifyTask_AppendsCertifiedEventAndNeverCompletion(t *testing.T) {
	req, rec := greenBundle(t)
	taskDir, head := seedTaskDir(t, req, rec)
	res, err := CertifyTask(TaskCertifyOptions{
		TaskDir:                  taskDir,
		ExpectedHeadDigestSHA256: head,
		ProducedAt:               testProducedAt,
	})
	if err != nil {
		t.Fatalf("CertifyTask: %v", err)
	}
	if res.Result.Receipt.CertificationVerdict != closureprotocol.Certified {
		t.Fatalf("verdict = %s", res.Result.Receipt.CertificationVerdict)
	}
	if !res.Appended {
		t.Fatal("certified event not appended")
	}
	if !res.Verification.Valid {
		t.Fatalf("final ledger verification failed: %+v", res.Verification.Errors)
	}
	events := chainEventTypes(t, taskDir)
	sawCertified := false
	for _, event := range events {
		if event == closureprotocol.LedgerEventCertified {
			sawCertified = true
		}
		if event == closureprotocol.LedgerEventCompleted {
			t.Fatal("a completed event was appended — Phase 8 owns completion")
		}
	}
	if !sawCertified {
		t.Fatalf("no certified event in chain: %v", events)
	}

	// The persisted receipt must re-verify from its stored bytes.
	receiptPath := filepath.Join(taskDir, filepath.FromSlash(res.ReceiptRef.Path))
	data, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatalf("read receipt artifact: %v", err)
	}
	if !strings.Contains(string(data), string(closureprotocol.Certified)) {
		t.Fatal("stored receipt does not carry the verdict")
	}
}

func TestCertifyTask_StaleExpectedHeadFails(t *testing.T) {
	req, rec := greenBundle(t)
	taskDir, _ := seedTaskDir(t, req, rec)
	_, err := CertifyTask(TaskCertifyOptions{
		TaskDir:                  taskDir,
		ExpectedHeadDigestSHA256: "0000000000000000000000000000000000000000000000000000000000000000",
		ProducedAt:               testProducedAt,
	})
	if !errors.Is(err, ErrStaleExpectedHead) {
		t.Fatalf("err = %v, want ErrStaleExpectedHead", err)
	}
	for _, event := range chainEventTypes(t, taskDir) {
		if event == closureprotocol.LedgerEventCertified {
			t.Fatal("certified event appended despite stale head")
		}
	}
}

func TestCertifyTask_TamperedRecordFails(t *testing.T) {
	req, rec := greenBundle(t)
	taskDir, head := seedTaskDir(t, req, rec)
	// Tamper with the stored capability consumption artifact.
	digest := req.CapabilityConsumptionDigestSHA256
	path := filepath.Join(taskDir, "artifacts", "sha256", digest+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	tampered := strings.Replace(string(data), "cap.green", "cap.forged", 1)
	if tampered == string(data) {
		t.Fatal("test setup: nothing replaced")
	}
	if err := os.WriteFile(path, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = CertifyTask(TaskCertifyOptions{
		TaskDir:                  taskDir,
		ExpectedHeadDigestSHA256: head,
		ProducedAt:               testProducedAt,
	})
	if !errors.Is(err, ErrRecordDigestMismatch) {
		t.Fatalf("err = %v, want ErrRecordDigestMismatch", err)
	}
}

func TestCertifyTask_BlockedEvaluationLeavesLedgerUntouched(t *testing.T) {
	req, rec := greenBundle(t)
	rec.ProofDischarges = nil
	req = rebindGreen(t, rec)
	taskDir, head := seedTaskDir(t, req, rec)
	res, err := CertifyTask(TaskCertifyOptions{
		TaskDir:                  taskDir,
		ExpectedHeadDigestSHA256: head,
		ProducedAt:               testProducedAt,
	})
	if err != nil {
		t.Fatalf("CertifyTask: %v", err)
	}
	if res.Result.Receipt.CertificationVerdict != closureprotocol.CertificationBlocked {
		t.Fatalf("verdict = %s", res.Result.Receipt.CertificationVerdict)
	}
	if res.Appended {
		t.Fatal("blocked evaluation appended an event")
	}
	events := chainEventTypes(t, taskDir)
	if len(events) != 1 || events[0] != closureprotocol.LedgerEventTaskPrepared {
		t.Fatalf("ledger changed on blocked evaluation: %v", events)
	}
}

func TestCertifyTask_ProjectionsRebuildIdentically(t *testing.T) {
	req, rec := greenBundle(t)
	taskDir, head := seedTaskDir(t, req, rec)
	if _, err := CertifyTask(TaskCertifyOptions{
		TaskDir:                  taskDir,
		ExpectedHeadDigestSHA256: head,
		ProducedAt:               testProducedAt,
	}); err != nil {
		t.Fatalf("CertifyTask: %v", err)
	}
	first, err := ledger.RebuildProjections(taskDir, taskPayloadValidator)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ledger.RebuildProjections(taskDir, taskPayloadValidator)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Files) != len(second.Files) {
		t.Fatalf("projection file sets differ: %d vs %d", len(first.Files), len(second.Files))
	}
	for path, data := range first.Files {
		if string(second.Files[path]) != string(data) {
			t.Fatalf("projection %s differs between rebuilds", path)
		}
	}
}

func TestCertifyTask_TaskMismatchRefused(t *testing.T) {
	req, rec := greenBundle(t)
	taskDir, head := seedTaskDir(t, req, rec)
	// Rewrite the request with a different task id (records untouched).
	other := req
	other.TaskID = "task.other"
	requestBytes, err := CanonicalRequestYAML(other)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, RequestFileName), requestBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = CertifyTask(TaskCertifyOptions{
		TaskDir:                  taskDir,
		ExpectedHeadDigestSHA256: head,
		ProducedAt:               testProducedAt,
	})
	if !errors.Is(err, ErrTaskMismatch) {
		t.Fatalf("err = %v, want ErrTaskMismatch", err)
	}
}
