// SPDX-License-Identifier: AGPL-3.0-only

package main

// Phase 9.5 Checkpoint 5 — adversarial proofs for the guarded mutation family.
//
// These prove STATE IMMUTABILITY, not merely response codes: every refusal path
// snapshots the task ledger directory byte-for-byte before the operation and
// asserts it is byte-identical afterward. They drive the REAL owners through the
// real handlers over an ephemeral repo + active task + enrolled identity + the
// governed authority policy.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	awarenesspb "github.com/globulario/sensei/golang/pb"

	"github.com/globulario/sensei/golang/architecture/identity"
	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
	"github.com/globulario/sensei/golang/architecture/tasksession"
	"github.com/globulario/sensei/internal/resulttestkit"
)

var (
	mutSeedEpoch = time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	mutEnrollNow = time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
)

var mutPolicyFiles = []string{
	"actor_roles.yaml", "mutation_paths.yaml", "observation_paths.yaml",
	"delegation_policies.yaml", "authority_grants.yaml", "authority_domains.yaml",
}

type mutEnv struct {
	repo, taskDir, domain, questionID, scopeDomain string
	srv                                            *server
}

func mutModuleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func mutCopyPolicy(t *testing.T, repo string) {
	t.Helper()
	src := filepath.Join(mutModuleRoot(t), "docs", "awareness")
	dst := filepath.Join(repo, "docs", "awareness")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range mutPolicyFiles {
		data, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatalf("read policy %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dst, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// seedMutEnv builds a full disposable environment and a *server bound to it.
func seedMutEnv(t *testing.T) mutEnv {
	t.Helper()
	r, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{
		Direction:   "evolve",
		Epoch:       mutSeedEpoch,
		ResultFiles: map[string]string{"src/model.go": "package src\n\n// evolve\nfunc Publish() {}\n"},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	mutCopyPolicy(t, r.Repo)
	if _, err := identity.Enroll(identity.EnrollOptions{Root: identity.Root(r.Repo), Now: mutEnrollNow}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	adv, err := tasksession.AdvanceResultTransition(context.Background(), tasksession.AdvanceResultRequest{
		RepositoryRoot: r.Repo, TaskDirectory: r.TaskDir,
		RepositoryDomain: resulttestkit.Domain, ResultRevision: r.ResultRev,
	})
	if err != nil || adv.TransitionID == "" {
		t.Fatalf("advance: %v (outcome %v)", err, adv.Outcome)
	}
	questions, err := qd.OpenQuestionsForLatestTransition(r.TaskDir)
	if err != nil || len(questions) == 0 {
		t.Fatalf("no architect questions seeded: %v", err)
	}

	// Read the current head with a pure probe-prepare (writes nothing) so the
	// active pointer carries a real ledger head for the audit.
	head := ""
	if cand, perr := qd.Prepare(qd.PrepareRequest{
		TaskDirectory: r.TaskDir, RepositoryRoot: r.Repo, IdentityRoot: identity.Root(r.Repo),
		QuestionID: questions[0].QuestionID, Disposition: qd.DispositionDeferred, Reusability: qd.ReusabilityNone,
		Rationale: "probe", EffectiveScopeDomain: questions[0].ScopeDomain,
	}); perr == nil {
		head = cand.ExpectedLedgerHeadDigestSHA256
	}

	// The active task is ALWAYS server-resolved; override the resolver to point at
	// this fixture (never a client input).
	sessRel, _ := filepath.Rel(r.Repo, filepath.Join(r.TaskDir, "session.yaml"))
	ptr := tasksession.ActivePointer{
		TaskID: r.TaskID, RepositoryDomain: resulttestkit.Domain,
		SessionPath: filepath.ToSlash(sessRel), LedgerHeadDigestSHA256: head,
	}
	prev := loadActivePointer
	loadActivePointer = func(string) (tasksession.ActivePointer, error) { return ptr, nil }
	t.Cleanup(func() { loadActivePointer = prev })

	srv := &server{briefingRepo: &briefingRepositoryContext{Root: r.Repo, Domain: resulttestkit.Domain}}
	return mutEnv{repo: r.Repo, taskDir: r.TaskDir, domain: resulttestkit.Domain,
		questionID: questions[0].QuestionID, scopeDomain: questions[0].ScopeDomain, srv: srv}
}

// snapshotDir hashes every file under dir → map[relpath]sha256. Byte-for-byte
// immutability is proven by comparing two snapshots.
func snapshotDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		sum := sha256.Sum256(data)
		out[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", dir, err)
	}
	return out
}

func assertUnchanged(t *testing.T, before, after map[string]string) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("file set changed: %d before, %d after (a refusal must write nothing)", len(before), len(after))
	}
	for p, d := range before {
		if after[p] != d {
			t.Fatalf("file %s changed (a refusal must write nothing)", p)
		}
	}
}

func (e mutEnv) input(disposition awarenesspb.ArchitectureDisposition, reuse awarenesspb.ArchitectureReusability, answerID string, answer []byte) *awarenesspb.ArchitectureDispositionInput {
	return &awarenesspb.ArchitectureDispositionInput{
		RepositoryIdentity: e.domain, Domain: e.domain, QuestionId: e.questionID,
		Disposition: disposition, Reusability: reuse, Rationale: "the basis is X",
		AnswerId: answerID, AnswerBytes: answer, EffectiveScopeDomain: e.scopeDomain,
	}
}

func answeredInput(e mutEnv) *awarenesspb.ArchitectureDispositionInput {
	return e.input(awarenesspb.ArchitectureDisposition_ARCHITECTURE_DISPOSITION_ANSWERED,
		awarenesspb.ArchitectureReusability_ARCHITECTURE_REUSABILITY_REUSABLE_CANDIDATE, "answer.1", []byte("the intended basis is X"))
}

// ── prepare writes nothing ───────────────────────────────────────────────────
func TestMutation_PrepareWritesNothing(t *testing.T) {
	e := seedMutEnv(t)
	before := snapshotDir(t, e.taskDir)
	resp, err := e.srv.PrepareArchitectAnswerDisposition(context.Background(),
		&awarenesspb.PrepareArchitectAnswerDispositionRequest{Input: answeredInput(e)})
	if err != nil {
		t.Fatalf("prepare transport error: %v", err)
	}
	if resp.GetRefusal() != nil {
		t.Fatalf("expected a candidate, got refusal %s", resp.GetRefusal().GetReasonCode())
	}
	if resp.GetCandidate().GetExpectedLedgerHeadDigestSha256() == "" {
		t.Fatal("candidate must carry the expected ledger head")
	}
	assertUnchanged(t, before, snapshotDir(t, e.taskDir))
}

// ── refusals: each writes nothing, exposes mutation_applied=false + unchanged head
func TestMutation_RefusalsWriteNothing(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(e mutEnv, in *awarenesspb.ArchitectureDispositionInput) *awarenesspb.RecordArchitectAnswerDispositionRequest
		wantOwn string
	}{
		{"unauthorized actor", func(e mutEnv, in *awarenesspb.ArchitectureDispositionInput) *awarenesspb.RecordArchitectAnswerDispositionRequest {
			in.ActorIdentity = "actor.someone.else"
			return &awarenesspb.RecordArchitectAnswerDispositionRequest{Input: in}
		}, "identity"},
		{"task mismatch", func(e mutEnv, in *awarenesspb.ArchitectureDispositionInput) *awarenesspb.RecordArchitectAnswerDispositionRequest {
			in.TaskId = "task.not.active"
			return &awarenesspb.RecordArchitectAnswerDispositionRequest{Input: in}
		}, "tasksession"},
		{"domain mismatch", func(e mutEnv, in *awarenesspb.ArchitectureDispositionInput) *awarenesspb.RecordArchitectAnswerDispositionRequest {
			in.Domain = "github.com/other/repo"
			return &awarenesspb.RecordArchitectAnswerDispositionRequest{Input: in}
		}, "server.control"},
		{"stale expected head", func(e mutEnv, in *awarenesspb.ArchitectureDispositionInput) *awarenesspb.RecordArchitectAnswerDispositionRequest {
			return &awarenesspb.RecordArchitectAnswerDispositionRequest{Input: in, ExpectedLedgerHeadDigestSha256: "0000stalehead0000"}
		}, "questiondisposition"},
		{"unknown question", func(e mutEnv, in *awarenesspb.ArchitectureDispositionInput) *awarenesspb.RecordArchitectAnswerDispositionRequest {
			in.QuestionId = "openquestion.does.not.exist"
			return &awarenesspb.RecordArchitectAnswerDispositionRequest{Input: in}
		}, "questiondisposition"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := seedMutEnv(t)
			before := snapshotDir(t, e.taskDir)
			req := tc.mutate(e, answeredInput(e))
			resp, err := e.srv.RecordArchitectAnswerDisposition(context.Background(), req)
			if err != nil {
				t.Fatalf("a domain refusal must be RPC success, got transport error: %v", err)
			}
			ref := resp.GetRefusal()
			if ref == nil {
				t.Fatalf("expected a refusal, got a receipt (%s)", resp.GetReceipt().GetOutcome())
			}
			if ref.GetMutationApplied() {
				t.Fatal("a refusal must report mutation_applied=false")
			}
			if ref.GetAudit().GetMutationApplied() {
				t.Fatal("refusal audit must report mutation_applied=false")
			}
			if a := ref.GetAudit(); a.GetPreviousLedgerHeadSha256() != a.GetResultingLedgerHeadSha256() {
				t.Fatal("a refusal must leave the ledger identity UNCHANGED (previous == resulting)")
			}
			if ref.GetReasonCode() == "" || ref.GetOwner() == "" {
				t.Fatal("a refusal must carry a typed reason code + owner")
			}
			assertUnchanged(t, before, snapshotDir(t, e.taskDir))
		})
	}
}

// ── config absence is a stable typed refusal, not an internal failure ─────────
func TestMutation_ConfigAbsenceIsTypedRefusal(t *testing.T) {
	srv := &server{} // no briefingRepo
	resp, err := srv.RecordArchitectAnswerDisposition(context.Background(),
		&awarenesspb.RecordArchitectAnswerDispositionRequest{Input: &awarenesspb.ArchitectureDispositionInput{
			RepositoryIdentity: "github.com/x/y", QuestionId: "q.1",
			Disposition: awarenesspb.ArchitectureDisposition_ARCHITECTURE_DISPOSITION_DISMISSED,
			Reusability: awarenesspb.ArchitectureReusability_ARCHITECTURE_REUSABILITY_NONE,
		}})
	if err != nil {
		t.Fatalf("config absence must be a typed refusal, not a transport error: %v", err)
	}
	if resp.GetRefusal() == nil || resp.GetRefusal().GetReasonCode() != "repository_context_unavailable" {
		t.Fatalf("expected repository_context_unavailable refusal, got %+v", resp)
	}
	if resp.GetRefusal().GetMutationApplied() {
		t.Fatal("config-absence refusal must be mutation_applied=false")
	}
}

// ── record commits once; exact replay returns without a new write ────────────
func TestMutation_RecordThenExactReplay(t *testing.T) {
	e := seedMutEnv(t)
	resp1, err := e.srv.RecordArchitectAnswerDisposition(context.Background(),
		&awarenesspb.RecordArchitectAnswerDispositionRequest{Input: answeredInput(e)})
	if err != nil {
		t.Fatal(err)
	}
	rec := resp1.GetReceipt()
	if rec == nil || rec.GetOutcome() != awarenesspb.ArchitectureDispositionOutcome_ARCHITECTURE_DISPOSITION_OUTCOME_RECORDED {
		t.Fatalf("first record must be RECORDED, got %+v (refusal %v)", rec, resp1.GetRefusal())
	}
	if !rec.GetAudit().GetMutationApplied() {
		t.Fatal("a recorded disposition must report mutation_applied=true")
	}
	afterFirst := snapshotDir(t, e.taskDir)

	resp2, err := e.srv.RecordArchitectAnswerDisposition(context.Background(),
		&awarenesspb.RecordArchitectAnswerDispositionRequest{Input: answeredInput(e)})
	if err != nil {
		t.Fatal(err)
	}
	rep := resp2.GetReceipt()
	if rep == nil || rep.GetOutcome() != awarenesspb.ArchitectureDispositionOutcome_ARCHITECTURE_DISPOSITION_OUTCOME_REPLAYED {
		t.Fatalf("identical re-record must be REPLAYED, got %+v", rep)
	}
	if rep.GetReceiptDigestSha256() != rec.GetReceiptDigestSha256() {
		t.Fatal("exact replay must return the ORIGINAL receipt identity")
	}
	if rep.GetAudit().GetMutationApplied() {
		t.Fatal("exact replay must report mutation_applied=false (nothing new written)")
	}
	assertUnchanged(t, afterFirst, snapshotDir(t, e.taskDir))
}

// ── a successful record does NOT auto-promote (no hidden lifecycle chaining) ──
func TestMutation_RecordDoesNotAutoPromote(t *testing.T) {
	e := seedMutEnv(t)
	resp, err := e.srv.RecordArchitectAnswerDisposition(context.Background(),
		&awarenesspb.RecordArchitectAnswerDispositionRequest{Input: answeredInput(e)})
	if err != nil || resp.GetReceipt() == nil {
		t.Fatalf("record failed: %v %v", err, resp.GetRefusal())
	}
	// No promotion artifact may exist after a mere accept.
	promotions := filepath.Join(e.repo, ".sensei", "project", "promotions")
	if entries, err := os.ReadDir(promotions); err == nil && len(entries) > 0 {
		t.Fatal("recording an answer must NOT create a promotion (no auto-chaining)")
	}
}

// ── a conflicting second disposition CONTESTS; the prior record is immutable ──
func TestMutation_ContestedConflictPreservesPrior(t *testing.T) {
	e := seedMutEnv(t)
	first, err := e.srv.RecordArchitectAnswerDisposition(context.Background(),
		&awarenesspb.RecordArchitectAnswerDispositionRequest{Input: answeredInput(e)})
	if err != nil || first.GetReceipt() == nil {
		t.Fatalf("first record failed: %v %v", err, first.GetRefusal())
	}
	firstDigest := first.GetReceipt().GetReceiptDigestSha256()

	// A DIFFERENT disposition (deferred) for the same question.
	conflict := e.input(awarenesspb.ArchitectureDisposition_ARCHITECTURE_DISPOSITION_DEFERRED,
		awarenesspb.ArchitectureReusability_ARCHITECTURE_REUSABILITY_NONE, "", nil)
	resp, err := e.srv.RecordArchitectAnswerDisposition(context.Background(),
		&awarenesspb.RecordArchitectAnswerDispositionRequest{Input: conflict})
	if err != nil {
		t.Fatal(err)
	}
	rec := resp.GetReceipt()
	if rec == nil || rec.GetOutcome() != awarenesspb.ArchitectureDispositionOutcome_ARCHITECTURE_DISPOSITION_OUTCOME_CONTESTED {
		t.Fatalf("a conflicting disposition must be CONTESTED, got %+v (refusal %v)", rec, resp.GetRefusal())
	}
	// The prior record is PRESERVED and REFERENCED — a contest never overwrites it.
	if !contains(rec.GetContestedPriorDigests(), firstDigest) {
		t.Fatalf("the contested receipt must reference the immutable prior record %s, got %v", firstDigest, rec.GetContestedPriorDigests())
	}
	if rec.GetReceiptDigestSha256() == firstDigest {
		t.Fatal("the contested record must be a NEW immutable record, distinct from the prior")
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
