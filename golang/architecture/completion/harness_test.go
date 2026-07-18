// SPDX-License-Identifier: Apache-2.0

package completion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/certification"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/identity"
	"github.com/globulario/sensei/golang/architecture/ledger"
	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
	"github.com/globulario/sensei/golang/architecture/questionpromotion"
	"github.com/globulario/sensei/golang/architecture/questionresolution"
	"github.com/globulario/sensei/golang/architecture/resultpipeline"
	"github.com/globulario/sensei/golang/architecture/resultrecording"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
	"github.com/globulario/sensei/golang/propose"
	"github.com/globulario/sensei/internal/resulttestkit"
)

const testDomain = "github.com/globulario/sensei"

var (
	seedEpoch = time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	enrollNow = time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	certAt    = time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
)

type world struct {
	Repo         string
	TaskDir      string
	IdentityRoot string
	Questions    []qd.OpenQuestionRef
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func copyGovernedPolicy(t *testing.T, repo string) {
	t.Helper()
	src := filepath.Join(moduleRoot(t), "docs", "awareness")
	dst := filepath.Join(repo, "docs", "awareness")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"actor_roles.yaml", "mutation_paths.yaml", "observation_paths.yaml",
		"delegation_policies.yaml", "authority_grants.yaml", "authority_domains.yaml",
	} {
		data, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dst, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func seedWorld(t *testing.T) world {
	t.Helper()
	r, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{
		Direction:   "evolve",
		Epoch:       seedEpoch,
		ResultFiles: map[string]string{"src/model.go": "package src\n\n// evolve\nfunc Publish() {}\n"},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	copyGovernedPolicy(t, r.Repo)
	if _, err := identity.Enroll(identity.EnrollOptions{Root: identity.Root(r.Repo), Now: enrollNow}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	head, herr := admission.TaskLedgerHead(r.TaskDir)
	if herr != nil {
		t.Fatalf("head: %v", herr)
	}
	c, perr := resultpipeline.PrepareTransition(context.Background(), resultpipeline.PrepareTransitionRequest{
		Build: resultpipeline.BuildRequest{RepositoryRoot: r.Repo, TaskDirectory: r.TaskDir,
			ResultMode: resulttransition.ResultModeRevision, ResultRevision: r.ResultRev, RepositoryDomain: resulttestkit.Domain},
		ExpectedLedgerHeadDigestSHA256: head, RecordedAt: "2026-07-16T00:00:00Z",
	})
	if perr != nil {
		t.Fatalf("prepare transition: %v", perr)
	}
	if _, rerr := resultrecording.RecordTransition(context.Background(), resultrecording.RecordRequest{TaskDirectory: r.TaskDir, Candidate: c}); rerr != nil {
		t.Fatalf("record transition: %v", rerr)
	}
	questions, err := qd.OpenQuestionsForLatestTransition(r.TaskDir)
	if err != nil {
		t.Fatalf("questions: %v", err)
	}
	return world{Repo: r.Repo, TaskDir: r.TaskDir, IdentityRoot: identity.Root(r.Repo), Questions: questions}
}

func (w world) dispose(t *testing.T, questionID string, disp qd.Disposition, reuse qd.Reusability, salt string) string {
	t.Helper()
	req := qd.PrepareRequest{
		TaskDirectory: w.TaskDir, RepositoryRoot: w.Repo, IdentityRoot: w.IdentityRoot,
		QuestionID: questionID, Disposition: disp, Reusability: reuse,
		Rationale: "the intended basis is " + salt,
	}
	if disp == qd.DispositionAnswered {
		req.AnswerID = "answer." + salt
		req.AnswerBytes = []byte("the intended basis is " + salt)
	}
	cand, err := qd.Prepare(req)
	if err != nil {
		t.Fatalf("dispose prepare: %v", err)
	}
	if _, err := qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: w.TaskDir, Candidate: cand}); err != nil {
		t.Fatalf("dispose record: %v", err)
	}
	return cand.Receipt.ReceiptDigestSHA256
}

func (w world) promote(t *testing.T, dispositionDigest string, p propose.Request) {
	t.Helper()
	res, err := questionpromotion.Promote(context.Background(), questionpromotion.PromoteRequest{
		RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir, RepositoryDomain: testDomain, IdentityRoot: w.IdentityRoot,
		QuestionDispositionReceiptDigestSHA256: dispositionDigest, Proposal: p,
	})
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if res.Outcome != questionpromotion.OutcomeCommitted {
		t.Fatalf("promote outcome = %s (%s)", res.Outcome, res.Detail)
	}
}

func proposedInvariant() propose.Request {
	return propose.Request{
		Kind: "invariant", ID: "invariant.promoted.reload_validates",
		Title: "Reload validates before serving", Description: "promoted from an accepted architect answer",
		SourceFiles: []string{"golang/server/reload.go"}, RelatedFailures: []string{"failure.x"},
		Domain: testDomain,
	}
}

// resolveAllQuestionsAndPromote disposes both binding questions terminally, promoting
// the reusable one, so the 8.1d gate can be satisfied.
func (w world) resolveAll(t *testing.T) {
	t.Helper()
	dA := w.dispose(t, w.Questions[0].QuestionID, qd.DispositionAnswered, qd.ReusabilityReusableCandidate, "A")
	w.promote(t, dA, proposedInvariant())
	w.dispose(t, w.Questions[1].QuestionID, qd.DispositionAnswered, qd.ReusabilityTaskLocal, "B")
}

// runQRCert produces the Phase-8.1d question-resolution certificate for the current
// head. Call AFTER seeding the correctness event so the cert binds the post-certified
// head.
func (w world) runQRCert(t *testing.T) {
	t.Helper()
	res, err := questionresolution.Certify(context.Background(), questionresolution.CertifyRequest{
		RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir, IdentityRoot: w.IdentityRoot,
	})
	if err != nil {
		t.Fatalf("qr certify: %v", err)
	}
	if res.Outcome != questionresolution.OutcomeSatisfied && res.Outcome != questionresolution.OutcomeReplay {
		t.Fatalf("qr certify outcome = %s (%s)", res.Outcome, res.Detail)
	}
}

func currentResultBinding(t *testing.T, taskDir string) closureprotocol.ResultBinding {
	t.Helper()
	chain, err := ledger.NewStore(taskDir).VerifyChain()
	if err != nil {
		t.Fatalf("verify chain: %v", err)
	}
	rb, ok := latestResultBinding(chain)
	if !ok {
		t.Fatal("no current result binding")
	}
	return rb
}

// seedCorrectness fabricates a Phase-6 certified ledger event carrying a valid,
// digest-sealed CertificationReceipt for the given result binding and verdict.
// It mirrors certification.CertifyTask's ledger write.
func (w world) seedCorrectness(t *testing.T, rb closureprotocol.ResultBinding, verdict closureprotocol.CertificationVerdict) string {
	t.Helper()
	receipt := closureprotocol.CertificationReceipt{
		ResultBinding:        rb,
		CertificationPolicy:  "certification.architectural_closure.v1",
		ScopeLane:            closureprotocol.DimensionPass,
		AuthorityLane:        closureprotocol.DimensionPass,
		ProofLane:            closureprotocol.DimensionPass,
		EvidenceLane:         closureprotocol.DimensionPass,
		CertificationVerdict: verdict,
	}
	digest, err := closureprotocol.CertificationReceiptDigest(receipt)
	if err != nil {
		t.Fatalf("cert digest: %v", err)
	}
	receipt.DigestSHA256 = digest
	if err := certification.VerifyReceipt(receipt); err != nil {
		t.Fatalf("fabricated receipt invalid: %v", err)
	}
	bytes, err := closureprotocol.CanonicalJSON(receipt)
	if err != nil {
		t.Fatalf("canonical json: %v", err)
	}
	store := ledger.NewStore(w.TaskDir)
	ref, err := store.StoreArtifactBytes(bytes, "application/json")
	if err != nil {
		t.Fatalf("store artifact: %v", err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	ra, err := admission.LoadRecordedAuthority(w.TaskDir)
	if err != nil {
		t.Fatalf("recorded authority: %v", err)
	}
	task := ra.Base.Task
	if _, err := store.Append(context.Background(), ledger.AppendRequest{
		TaskID:                   task.ID,
		SessionID:                task.SessionID,
		ExpectedHeadDigestSHA256: report.HeadDigestSHA256,
		EventType:                closureprotocol.LedgerEventCertified,
		Payload: ledger.TaskEventPayload{
			SchemaVersion: ledger.EventPayloadSchemaVersion,
			EventType:     closureprotocol.LedgerEventCertified,
			TaskID:        task.ID,
			SessionID:     task.SessionID,
			TaskPhase:     closureprotocol.PhaseCertified,
			Status:        string(verdict),
			ResultBinding: &rb,
			Artifacts:     map[string]closureprotocol.LedgerPayloadRef{"certification_receipt": ref},
		},
		PayloadMediaType: "application/yaml",
		ProducerID:       "sensei certify-change",
		ProducedAt:       certAt,
	}); err != nil {
		t.Fatalf("append certified: %v", err)
	}
	return digest
}

// seedCorrectnessMismatched fabricates a certified event whose PAYLOAD binds
// payloadRB (used for routing) while the receipt artifact binds receiptRB. It lets
// a test drive the item-5 case where routing claims the current result but the
// verified receipt binds another.
func (w world) seedCorrectnessMismatched(t *testing.T, payloadRB, receiptRB closureprotocol.ResultBinding, verdict closureprotocol.CertificationVerdict) string {
	t.Helper()
	receipt := closureprotocol.CertificationReceipt{
		ResultBinding:        receiptRB,
		CertificationPolicy:  "certification.architectural_closure.v1",
		ScopeLane:            closureprotocol.DimensionPass,
		AuthorityLane:        closureprotocol.DimensionPass,
		ProofLane:            closureprotocol.DimensionPass,
		EvidenceLane:         closureprotocol.DimensionPass,
		CertificationVerdict: verdict,
	}
	digest, err := closureprotocol.CertificationReceiptDigest(receipt)
	if err != nil {
		t.Fatalf("cert digest: %v", err)
	}
	receipt.DigestSHA256 = digest
	if err := certification.VerifyReceipt(receipt); err != nil {
		t.Fatalf("fabricated receipt invalid: %v", err)
	}
	bytes, err := closureprotocol.CanonicalJSON(receipt)
	if err != nil {
		t.Fatalf("canonical json: %v", err)
	}
	store := ledger.NewStore(w.TaskDir)
	ref, err := store.StoreArtifactBytes(bytes, "application/json")
	if err != nil {
		t.Fatalf("store artifact: %v", err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	ra, err := admission.LoadRecordedAuthority(w.TaskDir)
	if err != nil {
		t.Fatalf("recorded authority: %v", err)
	}
	task := ra.Base.Task
	prb := payloadRB
	if _, err := store.Append(context.Background(), ledger.AppendRequest{
		TaskID:                   task.ID,
		SessionID:                task.SessionID,
		ExpectedHeadDigestSHA256: report.HeadDigestSHA256,
		EventType:                closureprotocol.LedgerEventCertified,
		Payload: ledger.TaskEventPayload{
			SchemaVersion: ledger.EventPayloadSchemaVersion,
			EventType:     closureprotocol.LedgerEventCertified,
			TaskID:        task.ID,
			SessionID:     task.SessionID,
			TaskPhase:     closureprotocol.PhaseCertified,
			Status:        string(verdict),
			ResultBinding: &prb,
			Artifacts:     map[string]closureprotocol.LedgerPayloadRef{"certification_receipt": ref},
		},
		PayloadMediaType: "application/yaml",
		ProducerID:       "sensei certify-change",
		ProducedAt:       certAt,
	}); err != nil {
		t.Fatalf("append certified: %v", err)
	}
	return digest
}

func (w world) assess(t *testing.T) ReadinessAssessment {
	t.Helper()
	a, err := AssessReadiness(context.Background(), Request{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir})
	if err != nil {
		t.Fatalf("assess: %v", err)
	}
	return a
}

func stateOf(a ReadinessAssessment, id ObligationID) EvidenceState {
	for _, o := range a.Obligations {
		if o.Obligation == id {
			return o.State
		}
	}
	return ""
}

func treeDigest(t *testing.T, root string) string {
	t.Helper()
	h := sha256.New()
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.Contains(p, ".governed-mutation.lock") || strings.HasSuffix(p, ".tmp") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		data, _ := os.ReadFile(p)
		h.Write([]byte(rel))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))
}

func ledgerEntryCount(t *testing.T, taskDir string) int {
	t.Helper()
	rep, err := ledger.NewStore(taskDir).Verify()
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	return rep.EntryCount
}

func hasLedgerEvent(t *testing.T, taskDir, eventType string) bool {
	t.Helper()
	chain, err := ledger.NewStore(taskDir).VerifyChain()
	if err != nil {
		t.Fatalf("verify chain: %v", err)
	}
	for _, e := range chain.Entries {
		if string(e.Entry.EventType) == eventType {
			return true
		}
	}
	return false
}

func sha256hex(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
