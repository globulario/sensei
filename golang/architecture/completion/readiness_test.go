// SPDX-License-Identifier: AGPL-3.0-only

package completion

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

// Item 1: matching correctness + matching question-resolution evidence → all
// corresponding obligations satisfied and readiness ready.
func TestAssessReadyWhenBothCertificatesPresent(t *testing.T) {
	w := seedWorld(t)
	w.resolveAll(t)
	rb := currentResultBinding(t, w.TaskDir)
	w.seedCorrectness(t, rb, closureprotocol.Certified)
	w.runQRCert(t) // binds the post-certified head

	a := w.assess(t)
	if a.Readiness != ReadinessReady {
		t.Fatalf("readiness = %s; obligations: %+v", a.Readiness, a.Obligations)
	}
	for _, o := range a.Obligations {
		if o.State != EvidenceSatisfied {
			t.Fatalf("obligation %s = %s (%s), want satisfied", o.Obligation, o.State, o.Detail)
		}
	}
	if err := ValidateAssessment(a); err != nil {
		t.Fatalf("assessment invalid: %v", err)
	}
}

// Item 2: missing correctness evidence → typed missing (not generic failure).
func TestAssessMissingCorrectness(t *testing.T) {
	w := seedWorld(t)
	w.resolveAll(t)
	w.runQRCert(t)
	a := w.assess(t)
	if stateOf(a, ObligationCorrectnessCertificate) != EvidenceMissing {
		t.Fatalf("correctness = %s, want missing", stateOf(a, ObligationCorrectnessCertificate))
	}
	if a.Readiness != ReadinessNotReady {
		t.Fatal("must not be ready without correctness")
	}
}

// Item 3: missing question-resolution evidence → typed missing.
func TestAssessMissingQuestionResolution(t *testing.T) {
	w := seedWorld(t)
	w.resolveAll(t)
	rb := currentResultBinding(t, w.TaskDir)
	w.seedCorrectness(t, rb, closureprotocol.Certified)
	// no runQRCert
	a := w.assess(t)
	if stateOf(a, ObligationQuestionResolution) != EvidenceMissing {
		t.Fatalf("qr = %s, want missing", stateOf(a, ObligationQuestionResolution))
	}
	if a.Readiness != ReadinessNotReady {
		t.Fatal("must not be ready without question-resolution")
	}
}

// Item 4/5: stale question-resolution evidence → blocked/stale. (Correctness seeded
// AFTER the qr cert, so the qr cert binds an older head.)
func TestAssessStaleQuestionResolution(t *testing.T) {
	w := seedWorld(t)
	w.resolveAll(t)
	rb := currentResultBinding(t, w.TaskDir)
	w.runQRCert(t)                                      // binds head H1
	w.seedCorrectness(t, rb, closureprotocol.Certified) // head → H2, qr now stale
	a := w.assess(t)
	if stateOf(a, ObligationQuestionResolution) != EvidenceStale {
		t.Fatalf("qr = %s, want stale", stateOf(a, ObligationQuestionResolution))
	}
	if a.Readiness != ReadinessNotReady {
		t.Fatal("stale qr must not be ready")
	}
}

// Correction: a certificate bound only to a DIFFERENT result is historical, not a
// contradiction — the current result is simply uncertified (stale), never poisoned.
func TestAssessOtherResultCertificateIsStale(t *testing.T) {
	w := seedWorld(t)
	w.resolveAll(t)
	rb := currentResultBinding(t, w.TaskDir)
	other := rb
	other.ResultTreeDigestSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	w.seedCorrectness(t, other, closureprotocol.Certified) // bound to a different result
	w.runQRCert(t)
	a := w.assess(t)
	if stateOf(a, ObligationCorrectnessCertificate) != EvidenceStale {
		t.Fatalf("correctness = %s, want stale", stateOf(a, ObligationCorrectnessCertificate))
	}
}

// Correction proof: an old-result certificate plus one valid current-result
// certificate → correctness satisfied (the old one is historical, excluded).
func TestAssessOldResultPlusCurrentSatisfied(t *testing.T) {
	w := seedWorld(t)
	w.resolveAll(t)
	rb := currentResultBinding(t, w.TaskDir)
	old := rb
	old.ResultTreeDigestSHA256 = "1111111111111111111111111111111111111111111111111111111111111111"
	w.seedCorrectness(t, old, closureprotocol.Certified) // historical
	w.seedCorrectness(t, rb, closureprotocol.Certified)  // current
	w.runQRCert(t)
	a := w.assess(t)
	if stateOf(a, ObligationCorrectnessCertificate) != EvidenceSatisfied {
		t.Fatalf("correctness = %s, want satisfied", stateOf(a, ObligationCorrectnessCertificate))
	}
	if a.Readiness != ReadinessReady {
		t.Fatalf("readiness = %s, want ready", a.Readiness)
	}
}

// Correction proof: two distinct valid certificates for the CURRENT result →
// contradictory (fail closed, no map/order selection).
func TestAssessTwoCurrentCertsContradictory(t *testing.T) {
	w := seedWorld(t)
	w.resolveAll(t)
	rb := currentResultBinding(t, w.TaskDir)
	w.seedCorrectness(t, rb, closureprotocol.Certified)
	w.seedCorrectness(t, rb, closureprotocol.CertifiedWithConditions) // distinct receipt, same result
	w.runQRCert(t)
	a := w.assess(t)
	if stateOf(a, ObligationCorrectnessCertificate) != EvidenceContradictory {
		t.Fatalf("correctness = %s, want contradictory", stateOf(a, ObligationCorrectnessCertificate))
	}
}

// Correction proof: a broken certificate bound only to an older result must not
// poison a valid current result.
func TestAssessHistoricalTamperDoesNotPoison(t *testing.T) {
	w := seedWorld(t)
	w.resolveAll(t)
	rb := currentResultBinding(t, w.TaskDir)
	old := rb
	old.ResultTreeDigestSHA256 = "2222222222222222222222222222222222222222222222222222222222222222"
	oldDigest := w.seedCorrectness(t, old, closureprotocol.Certified)
	tamperCertReceipt(t, w.TaskDir, oldDigest) // corrupt the historical certificate
	w.seedCorrectness(t, rb, closureprotocol.Certified)
	w.runQRCert(t)
	a := w.assess(t)
	if stateOf(a, ObligationCorrectnessCertificate) != EvidenceSatisfied {
		t.Fatalf("correctness = %s, want satisfied (historical tamper must not poison)", stateOf(a, ObligationCorrectnessCertificate))
	}
}

// Correction proof (item 5): a certificate whose event routes it to the current
// result but whose verified receipt binds another result → fail closed.
func TestAssessMismatchedPayloadReceiptWrongResult(t *testing.T) {
	w := seedWorld(t)
	w.resolveAll(t)
	rb := currentResultBinding(t, w.TaskDir)
	other := rb
	other.ResultTreeDigestSHA256 = "3333333333333333333333333333333333333333333333333333333333333333"
	w.seedCorrectnessMismatched(t, rb, other, closureprotocol.Certified) // payload=current, receipt=other
	w.runQRCert(t)
	a := w.assess(t)
	if stateOf(a, ObligationCorrectnessCertificate) != EvidenceWrongBinding {
		t.Fatalf("correctness = %s, want wrong_task_or_result_binding", stateOf(a, ObligationCorrectnessCertificate))
	}
}

// Item 7: tampered certificate artifact → integrity failure.
func TestAssessTamperedCorrectnessIntegrity(t *testing.T) {
	w := seedWorld(t)
	w.resolveAll(t)
	rb := currentResultBinding(t, w.TaskDir)
	digest := w.seedCorrectness(t, rb, closureprotocol.Certified)
	w.runQRCert(t)
	// Tamper the stored certification receipt artifact bytes.
	tamperCertReceipt(t, w.TaskDir, digest)
	a := w.assess(t)
	if stateOf(a, ObligationCorrectnessCertificate) != EvidenceIntegrityFailure {
		t.Fatalf("correctness = %s, want integrity_failure", stateOf(a, ObligationCorrectnessCertificate))
	}
}

// Item 9: unrelated broken artifacts do not poison the task.
func TestAssessUnrelatedDebrisDoesNotPoison(t *testing.T) {
	w := seedWorld(t)
	w.resolveAll(t)
	rb := currentResultBinding(t, w.TaskDir)
	w.seedCorrectness(t, rb, closureprotocol.Certified)
	w.runQRCert(t)
	// A question-resolution certificate dir for a DIFFERENT task.
	writeForeignQRCert(t, w.Repo)
	a := w.assess(t)
	if a.Readiness != ReadinessReady {
		t.Fatalf("unrelated debris poisoned the task: %+v", a.Obligations)
	}
}

// Item 8: a conflicting terminal-completion fact → contradictory, not ready.
func TestAssessConflictingCompletion(t *testing.T) {
	w := seedWorld(t)
	w.resolveAll(t)
	rb := currentResultBinding(t, w.TaskDir)
	w.seedCorrectness(t, rb, closureprotocol.Certified)
	w.runQRCert(t)
	seedCompletedEvent(t, w.TaskDir)
	a := w.assess(t)
	if stateOf(a, ObligationNoConflictingCompletion) != EvidenceContradictory {
		t.Fatalf("no_conflicting_completion = %s, want contradictory", stateOf(a, ObligationNoConflictingCompletion))
	}
	if a.Readiness != ReadinessNotReady {
		t.Fatal("must not be ready with a conflicting completion fact")
	}
}

// Item 11 + 12: deterministic byte-identical replay and zero side effects.
func TestAssessDeterministicAndNoSideEffects(t *testing.T) {
	w := seedWorld(t)
	w.resolveAll(t)
	rb := currentResultBinding(t, w.TaskDir)
	w.seedCorrectness(t, rb, closureprotocol.Certified)
	w.runQRCert(t)

	before := treeDigest(t, w.Repo)
	entriesBefore := ledgerEntryCount(t, w.TaskDir)
	a1 := w.assess(t)
	a2 := w.assess(t)
	if a1.DigestSHA256 != a2.DigestSHA256 {
		t.Fatal("assessment is not deterministic on an unchanged world")
	}
	if treeDigest(t, w.Repo) != before {
		t.Fatal("assessment mutated the repository")
	}
	if ledgerEntryCount(t, w.TaskDir) != entriesBefore {
		t.Fatal("assessment appended a ledger event")
	}
}

// Item 13: the assessment does not assert terminal completion.
func TestAssessDoesNotAssertCompletion(t *testing.T) {
	w := seedWorld(t)
	w.resolveAll(t)
	rb := currentResultBinding(t, w.TaskDir)
	w.seedCorrectness(t, rb, closureprotocol.Certified)
	w.runQRCert(t)
	a := w.assess(t)
	if hasLedgerEvent(t, w.TaskDir, "completed") {
		t.Fatal("readiness evaluation must not append a completed event")
	}
	found := false
	for _, b := range a.Bound {
		if b == "a read-only terminal-completion READINESS assessment; it does not perform or authorize terminal completion" {
			found = true
		}
	}
	if !found {
		t.Fatal("assessment must disclaim that it is a completion")
	}
	// readiness=ready is not a terminal status.
	if string(a.Readiness) == string(closureprotocol.TerminalCompleted) {
		t.Fatal("readiness must not be the terminal completed status")
	}
}

// Item 10: no caller boolean or path exists to manufacture satisfaction — the
// request carries only repo + task dir, and an unresolved task is never ready.
func TestNoCallerCanManufactureSatisfaction(t *testing.T) {
	w := seedWorld(t)
	// Nothing resolved, no certificates.
	a, err := AssessReadiness(context.Background(), Request{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir})
	if err != nil {
		t.Fatalf("assess: %v", err)
	}
	if a.Readiness != ReadinessNotReady {
		t.Fatal("an unresolved task with no evidence must never be ready")
	}
}

// ── helpers specific to these tests ──

func tamperCertReceipt(t *testing.T, taskDir, receiptDigest string) {
	t.Helper()
	// Find the certification_receipt artifact and corrupt it.
	chain, err := ledger.NewStore(taskDir).VerifyChain()
	if err != nil {
		t.Fatal(err)
	}
	for i := len(chain.Entries) - 1; i >= 0; i-- {
		ve := chain.Entries[i]
		if ve.Entry.EventType != closureprotocol.LedgerEventCertified {
			continue
		}
		data, _ := ledger.ReadVerifiedPayload(ve)
		payload, _ := ledger.ParseTaskEventPayload(data)
		ref := payload.Artifacts["certification_receipt"]
		p := filepath.Join(taskDir, filepath.FromSlash(ref.Path))
		raw, _ := os.ReadFile(p)
		if err := os.WriteFile(p, append(raw, []byte(" ")...), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("no certified event to tamper")
}

func writeForeignQRCert(t *testing.T, repo string) {
	t.Helper()
	dir := filepath.Join(repo, ".sensei", "project", "question-resolution-certifications", "foreign-digest")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"task":{"id":"task.other","session_id":"session.other"},"task_ledger_head_digest_sha256":"deadbeef"}`
	if err := os.WriteFile(filepath.Join(dir, "certificate.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func seedCompletedEvent(t *testing.T, taskDir string) {
	t.Helper()
	store := ledger.NewStore(taskDir)
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	ra, err := admission.LoadRecordedAuthority(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	task := ra.Base.Task
	if _, err := store.Append(context.Background(), ledger.AppendRequest{
		TaskID:                   task.ID,
		SessionID:                task.SessionID,
		ExpectedHeadDigestSHA256: report.HeadDigestSHA256,
		EventType:                closureprotocol.LedgerEventCompleted,
		Payload: ledger.TaskEventPayload{
			SchemaVersion: ledger.EventPayloadSchemaVersion,
			EventType:     closureprotocol.LedgerEventCompleted,
			TaskID:        task.ID,
			SessionID:     task.SessionID,
			TaskPhase:     closureprotocol.PhaseCompleted,
		},
		PayloadMediaType: "application/yaml",
		ProducerID:       "test",
		ProducedAt:       certAt,
	}); err != nil {
		t.Fatalf("append completed: %v", err)
	}
}
