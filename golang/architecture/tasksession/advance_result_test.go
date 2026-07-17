// SPDX-License-Identifier: Apache-2.0

package tasksession

import (
	"context"
	"testing"
	"time"
)

func advReq(repo, taskDir string, now time.Time) AdvanceResultRequest {
	return AdvanceResultRequest{RepositoryRoot: repo, TaskDirectory: taskDir, ResultRevision: "HEAD", Now: now}
}

// TestAdvanceRejectsMalformedRequest: a request with no task directory or no
// stable clock is a hard error, never a silent success.
func TestAdvanceRejectsMalformedRequest(t *testing.T) {
	if _, err := AdvanceResultTransition(context.Background(), AdvanceResultRequest{Now: time.Now()}); err == nil {
		t.Fatal("empty task directory must error")
	}
	if _, err := AdvanceResultTransition(context.Background(), AdvanceResultRequest{TaskDirectory: "/nonexistent"}); err == nil {
		t.Fatal("zero clock must error (the orchestrator has no internal clock)")
	}
}

// TestAdvanceWaitsForAdmissionDecision: authority is resolved but no typed
// decision is recorded, so the one legal action is admit-change — reported, not
// performed, and no transition is recorded.
func TestAdvanceWaitsForAdmissionDecision(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	res, err := AdvanceResultTransition(context.Background(), advReq(repo, taskDir, time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeWaiting || res.OperationalStatus != StatusReadyForAdmission {
		t.Fatalf("got %s/%s, want waiting/ready_for_admission", res.Outcome, res.OperationalStatus)
	}
	if res.TransitionRecorded {
		t.Fatal("no transition may be recorded before scope_verified")
	}
}

// TestAdvanceWaitsToConsumeThenVerify: after a decision the next legal action is
// consume-admission; after consumption it is verify-admission. The orchestrator
// reports the single load-bearing next action at each step.
func TestAdvanceWaitsToConsumeThenVerify(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	now := time.Now().UTC()
	dec := recordAdmissionDecision(t, taskDir, now)

	res, err := AdvanceResultTransition(context.Background(), advReq(repo, taskDir, now))
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeWaiting || res.NextAction.Action != NextConsumeCapability {
		t.Fatalf("after decision got %s/%q, want waiting/consume", res.Outcome, res.NextAction.Action)
	}

	recordCapabilityConsumption(t, taskDir, dec, now)
	res, err = AdvanceResultTransition(context.Background(), advReq(repo, taskDir, now))
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeWaiting || res.NextAction.Action != NextVerifyAdmission {
		t.Fatalf("after consume got %s/%q, want waiting/verify", res.Outcome, res.NextAction.Action)
	}
}

// TestAdvanceRefusesExpiredDecision: a decision whose single-use capability has
// expired grants nothing — the orchestrator refuses, never advances.
func TestAdvanceRefusesExpiredDecision(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	now := time.Now().UTC()
	recordAdmissionDecision(t, taskDir, now.Add(-25*time.Hour)) // capability expired (24h window)
	res, err := AdvanceResultTransition(context.Background(), advReq(repo, taskDir, now))
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeRefused {
		t.Fatalf("expired decision must refuse, got %s", res.Outcome)
	}
	if res.TransitionRecorded {
		t.Fatal("a refused task records no transition")
	}
}

// TestAdvanceWaitsMechanicalOnScopeViolation: a recorded scope verification that
// found an out-of-envelope change leaves the task waiting on mechanical repair,
// not advanced.
func TestAdvanceWaitsMechanicalOnScopeViolation(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	now := time.Now().UTC()
	dec := recordAdmissionDecision(t, taskDir, now)
	recordCapabilityConsumption(t, taskDir, dec, now)
	recordScopeVerification(t, taskDir, false) // verification failed

	res, err := AdvanceResultTransition(context.Background(), advReq(repo, taskDir, now))
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeWaiting || res.OperationalStatus != StatusWaitingMechanical {
		t.Fatalf("got %s/%s, want waiting/waiting_mechanical_repair", res.Outcome, res.OperationalStatus)
	}
}

// TestAdvanceFailsClosedAtScopeVerifiedWithoutValidResult: at the scope_verified
// terminal the orchestrator drives the result boundary, but a missing/invalid
// result prerequisite must produce a typed refusal — never a false success and
// never a recorded transition.
func TestAdvanceFailsClosedAtScopeVerifiedWithoutValidResult(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	now := time.Now().UTC()
	dec := recordAdmissionDecision(t, taskDir, now)
	recordCapabilityConsumption(t, taskDir, dec, now)
	recordScopeVerification(t, taskDir, true) // scope_verified terminal

	res, err := AdvanceResultTransition(context.Background(), AdvanceResultRequest{
		RepositoryRoot: repo, TaskDirectory: taskDir, ResultRevision: "does-not-exist", Now: now,
	})
	if err != nil {
		t.Fatalf("a bad result prerequisite must be a typed refusal result, not an error: %v", err)
	}
	if res.Outcome != OutcomeRefused {
		t.Fatalf("scope_verified with no valid result must refuse, never succeed; got %s (recorded=%v)", res.Outcome, res.TransitionRecorded)
	}
	if res.TransitionRecorded {
		t.Fatal("no transition may be recorded when preparation fails")
	}
	if res.RefusalCode == "" {
		t.Fatal("a refusal must carry the underlying typed code")
	}
}
