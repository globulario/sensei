// SPDX-License-Identifier: AGPL-3.0-only

package questiondisposition_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture/identity"
	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
)

func assertCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error %s, got nil", code)
	}
	var qe *qd.Error
	if !errors.As(err, &qe) {
		t.Fatalf("error %v is not *qd.Error", err)
	}
	if qe.Code != code {
		t.Fatalf("code = %s, want %s (%s)", qe.Code, code, qe.Detail)
	}
}

func record(t *testing.T, env disposableEnv, req qd.PrepareRequest) qd.RecordResult {
	t.Helper()
	cand, err := qd.Prepare(req)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	res, err := qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: env.TaskDir, Candidate: cand})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	return res
}

// --- disposition variants ---------------------------------------------------

func TestAnsweredTaskLocalReevaluatesAndContains(t *testing.T) {
	env := seedDisposable(t)
	req := answeredReusable(env)
	req.Reusability = qd.ReusabilityTaskLocal
	res := record(t, env, req)
	if res.Outcome != qd.OutcomeRecorded {
		t.Fatalf("outcome = %s", res.Outcome)
	}
	proj, _ := qd.ProjectQuestion(env.TaskDir, env.QuestionID)
	if proj.NextAction != qd.NextReevaluateTaskLocal {
		t.Fatalf("next = %s, want %s", proj.NextAction, qd.NextReevaluateTaskLocal)
	}
	// Containment: a task-local answer never enters governed sources.
	assertNoGovernedMutation(t, env)
}

func TestDismissedIsDurableAndDoesNotEraseQuestion(t *testing.T) {
	env := seedDisposable(t)
	req := answeredReusable(env)
	req.Disposition = qd.DispositionDismissed
	req.Reusability = qd.ReusabilityNone
	req.AnswerID = ""
	req.AnswerBytes = nil
	req.Rationale = "out of scope for this task"
	record(t, env, req)
	proj, _ := qd.ProjectQuestion(env.TaskDir, env.QuestionID)
	if proj.NextAction != qd.NextNone {
		t.Fatalf("next = %s, want none", proj.NextAction)
	}
	// The question is still present — a dismissal explains, it does not delete.
	qs, err := qd.OpenQuestionsForLatestTransition(env.TaskDir)
	if err != nil {
		t.Fatal(err)
	}
	if !containsQuestion(qs, env.QuestionID) {
		t.Fatal("dismissal erased the question from the transition")
	}
}

func TestDeferredAwaitsArchitect(t *testing.T) {
	env := seedDisposable(t)
	req := answeredReusable(env)
	req.Disposition = qd.DispositionDeferred
	req.Reusability = qd.ReusabilityNone
	req.AnswerID = ""
	req.AnswerBytes = nil
	req.Rationale = "needs runtime evidence first"
	record(t, env, req)
	proj, _ := qd.ProjectQuestion(env.TaskDir, env.QuestionID)
	if proj.NextAction != qd.NextAwaitArchitect {
		t.Fatalf("next = %s, want await_architect", proj.NextAction)
	}
}

// --- fail-closed prepare ----------------------------------------------------

func TestUnknownQuestionFailsClosed(t *testing.T) {
	env := seedDisposable(t)
	req := answeredReusable(env)
	req.QuestionID = "question.does-not-exist"
	_, err := qd.Prepare(req)
	assertCode(t, err, qd.CodeQuestionNotFound)
}

func TestUnenrolledActorFailsClosed(t *testing.T) {
	env := seedDisposable(t)
	req := answeredReusable(env)
	req.IdentityRoot = t.TempDir() // empty — no manifest
	_, err := qd.Prepare(req)
	assertCode(t, err, qd.CodeActorNotEnrolled)
}

func TestForeignIssuerActorFailsClosed(t *testing.T) {
	env := seedDisposable(t)
	foreign := t.TempDir()
	if _, err := identity.Enroll(identity.EnrollOptions{Root: foreign, Issuer: "attacker.local", Now: enrollNow}); err != nil {
		t.Fatalf("enroll foreign: %v", err)
	}
	req := answeredReusable(env)
	req.IdentityRoot = foreign
	_, err := qd.Prepare(req)
	assertCode(t, err, qd.CodeActorNotVerified)
}

func TestMissingDispositionGrantFailsClosed(t *testing.T) {
	env := seedDisposable(t, qd.GrantQuestionDisposition) // strip the grant
	_, err := qd.Prepare(answeredReusable(env))
	assertCode(t, err, qd.CodeAuthorityNotGranted)
}

func TestScopeBroadeningFailsClosed(t *testing.T) {
	env := seedDisposable(t)
	if env.ScopeDomain == "" {
		t.Skip("seeded question has no scope domain to broaden past")
	}
	req := answeredReusable(env)
	req.EffectiveScopeDomain = env.ScopeDomain + ".unrelated"
	_, err := qd.Prepare(req)
	assertCode(t, err, qd.CodeScopeBroadened)
}

func TestTamperedCandidateBytesFailClosed(t *testing.T) {
	env := seedDisposable(t)
	cand, err := qd.Prepare(answeredReusable(env))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	cand.ReceiptBytes = append([]byte(nil), cand.ReceiptBytes...)
	cand.ReceiptBytes[0] ^= 0xff // tamper after prepare
	_, err = qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: env.TaskDir, Candidate: cand})
	assertCode(t, err, qd.CodeArtifactStoreFailed)
}

// --- idempotency, conflict, concurrency -------------------------------------

func TestExactReplayCreatesNoSecondEvent(t *testing.T) {
	env := seedDisposable(t)
	cand, err := qd.Prepare(answeredReusable(env))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	first, err := qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: env.TaskDir, Candidate: cand})
	if err != nil {
		t.Fatalf("record 1: %v", err)
	}
	second, err := qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: env.TaskDir, Candidate: cand})
	if err != nil {
		t.Fatalf("record 2: %v", err)
	}
	if second.Outcome != qd.OutcomeReplayed {
		t.Fatalf("second outcome = %s, want replayed", second.Outcome)
	}
	if first.EntryDigestSHA256 != second.EntryDigestSHA256 {
		t.Fatal("replay reported a different entry")
	}
	if n := countDispositionEvents(t, env.TaskDir); n != 1 {
		t.Fatalf("disposition events = %d, want 1", n)
	}
}

func TestConflictingSecondIsContestedNeverOverwrites(t *testing.T) {
	env := seedDisposable(t)
	first := record(t, env, answeredReusable(env))
	if first.Outcome != qd.OutcomeRecorded {
		t.Fatalf("first outcome = %s", first.Outcome)
	}
	// A conflicting second disposition for the same question+result.
	conflict := answeredReusable(env)
	conflict.Disposition = qd.DispositionDeferred
	conflict.Reusability = qd.ReusabilityNone
	conflict.AnswerID = ""
	conflict.AnswerBytes = nil
	conflict.Rationale = "actually defer this"
	cand, err := qd.Prepare(conflict)
	if err != nil {
		t.Fatalf("prepare conflict: %v", err)
	}
	res, err := qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: env.TaskDir, Candidate: cand})
	if err != nil {
		t.Fatalf("record conflict: %v", err)
	}
	if res.Outcome != qd.OutcomeContested {
		t.Fatalf("outcome = %s, want contested", res.Outcome)
	}
	if len(res.ContestedPriorDigests) == 0 {
		t.Fatal("contested result names no prior")
	}
	// Both immutable records are preserved.
	if n := countDispositionEvents(t, env.TaskDir); n != 2 {
		t.Fatalf("disposition events = %d, want 2 (both preserved)", n)
	}
	if _, err := qd.LoadRecordedDisposition(env.TaskDir, first.ReceiptDigestSHA256); err != nil {
		t.Fatalf("first disposition was overwritten: %v", err)
	}
	proj, _ := qd.ProjectQuestion(env.TaskDir, env.QuestionID)
	if !proj.Contested || proj.NextAction != qd.NextAwaitAdjudication {
		t.Fatalf("projection contested=%v next=%s", proj.Contested, proj.NextAction)
	}
}

func TestStaleExpectedHeadFailsClosed(t *testing.T) {
	env := seedDisposable(t)
	cand, err := qd.Prepare(answeredReusable(env))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	// Record a different disposition first so the head moves past cand's expected head.
	other := answeredReusable(env)
	other.Disposition = qd.DispositionDeferred
	other.Reusability = qd.ReusabilityNone
	other.AnswerID = ""
	other.AnswerBytes = nil
	other.Rationale = "defer"
	record(t, env, other)
	// cand still carries the pre-move expected head → stale.
	_, err = qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: env.TaskDir, Candidate: cand})
	assertCode(t, err, qd.CodeStaleExpectedHead)
}

// --- boundary proof ---------------------------------------------------------

func TestBoundaryProofNoLaterPhaseOrGovernedMutation(t *testing.T) {
	env := seedDisposable(t)
	record(t, env, answeredReusable(env))
	// No certified/completed/revoked/migration/promotion event exists.
	for _, forbidden := range []string{
		"certified", "completed", "revoked", "migration_executed",
	} {
		if hasEventType(t, env.TaskDir, forbidden) {
			t.Fatalf("disposition produced a %q event", forbidden)
		}
	}
	// The disposition event carries no phase/status/result_binding.
	assertDispositionEventShape(t, env.TaskDir)
	assertNoGovernedMutation(t, env)
}

func containsQuestion(qs []qd.OpenQuestionRef, id string) bool {
	for _, q := range qs {
		if q.QuestionID == id {
			return true
		}
	}
	return false
}

// assertNoGovernedMutation proves the disposition wrote no candidate/governed
// promotion artifact into the repository's awareness tree.
func assertNoGovernedMutation(t *testing.T, env disposableEnv) {
	t.Helper()
	candidates := filepath.Join(env.Repo, "docs", "awareness", "candidates")
	if _, err := os.Stat(candidates); err == nil {
		t.Fatal("disposition created a governed-promotion candidate")
	}
}
