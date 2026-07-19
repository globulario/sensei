// SPDX-License-Identifier: Apache-2.0

package tasksession

import (
	"context"
	"testing"
	"time"
)

func advReq(repo, taskDir string) AdvanceResultRequest {
	return AdvanceResultRequest{RepositoryRoot: repo, TaskDirectory: taskDir, ResultRevision: "HEAD"}
}

// testAdvance runs the unexported orchestration helper with production dependencies,
// optionally mutated per-test (clock, snapshot hook, recorder). No process global is
// touched, so concurrent advances are unaffected.
func testAdvance(req AdvanceResultRequest, mut func(*advanceDeps)) (AdvanceResult, error) {
	d := productionAdvanceDeps()
	if mut != nil {
		mut(&d)
	}
	return advanceResultTransition(context.Background(), req, d)
}

// TestAdvanceRejectsMalformedRequest: a request with no task directory is a hard
// error, never a silent success.
func TestAdvanceRejectsMalformedRequest(t *testing.T) {
	if _, err := AdvanceResultTransition(context.Background(), AdvanceResultRequest{}); err == nil {
		t.Fatal("empty task directory must error")
	}
}

// TestAdvanceWaitsForAdmissionDecision: authority is resolved but no typed
// decision is recorded, so the one legal action is decide_admission — reported, not
// performed, and no transition is recorded.
func TestAdvanceWaitsForAdmissionDecision(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	res, err := AdvanceResultTransition(context.Background(), advReq(repo, taskDir))
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeWaiting || res.OperationalStatus != StatusReadyForAdmission {
		t.Fatalf("got %s/%s, want waiting/ready_for_admission", res.Outcome, res.OperationalStatus)
	}
	if res.NextAction.Action != AdvanceNextDecideAdmission {
		t.Fatalf("next = %q, want %q", res.NextAction.Action, AdvanceNextDecideAdmission)
	}
	if res.TransitionRecorded {
		t.Fatal("no transition may be recorded before scope_verified")
	}
	if !res.CurrentStateAvailable || res.CurrentLedgerHeadSHA256 == "" {
		t.Fatal("a waiting result must report the current verified head")
	}
}

// TestAdvanceWaitsToPerformMutationThenVerify: after a decision the next action is
// consume_capability; after consumption it is perform_mutation (a single step, not
// "apply then verify"). Each step is exactly one action.
func TestAdvanceWaitsToPerformMutationThenVerify(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	now := time.Now().UTC()
	dec := recordAdmissionDecision(t, taskDir, now)

	res, err := AdvanceResultTransition(context.Background(), advReq(repo, taskDir))
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeWaiting || res.NextAction.Action != AdvanceNextConsumeCapability {
		t.Fatalf("after decision got %s/%q, want waiting/consume_capability", res.Outcome, res.NextAction.Action)
	}

	recordCapabilityConsumption(t, taskDir, dec, now)
	res, err = AdvanceResultTransition(context.Background(), advReq(repo, taskDir))
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeWaiting || res.NextAction.Action != AdvanceNextPerformMutation {
		t.Fatalf("after consume got %s/%q, want waiting/perform_mutation", res.Outcome, res.NextAction.Action)
	}
}

// TestAdvanceRefusesExpiredDecision: a decision whose single-use capability has
// expired grants nothing — the orchestrator refuses, never advances. The expiry is
// evaluated against the trusted internal clock, not a caller-supplied time.
func TestAdvanceRefusesExpiredDecision(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	recordAdmissionDecision(t, taskDir, time.Now().UTC().Add(-25*time.Hour)) // 24h window elapsed
	res, err := AdvanceResultTransition(context.Background(), advReq(repo, taskDir))
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

// TestAdvanceCannotBackdateExpiredCapability proves an API caller cannot revive an
// expired capability by supplying a past time: the request has no clock field, so
// expiry is always evaluated against the trusted internal clock, which is an
// injected dependency (never a process global). When the clock is (test-only)
// injected just before expiry the capability binds; at/after expiry it refuses —
// the caller has no say.
func TestAdvanceCannotBackdateExpiredCapability(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	decidedAt := time.Now().UTC()
	recordAdmissionDecision(t, taskDir, decidedAt)

	// Internal clock before expiry → still admitted (ready_for_mutation). The clock
	// is an injected dependency, never a process global and never a request field, so
	// a caller cannot move it.
	res, err := testAdvance(advReq(repo, taskDir), func(d *advanceDeps) {
		d.now = func() time.Time { return decidedAt.Add(time.Hour) }
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.OperationalStatus != StatusReadyForMutation {
		t.Fatalf("within window got %s, want ready_for_mutation", res.OperationalStatus)
	}

	// Internal clock past expiry → refused.
	res, err = testAdvance(advReq(repo, taskDir), func(d *advanceDeps) {
		d.now = func() time.Time { return decidedAt.Add(25 * time.Hour) }
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeRefused {
		t.Fatalf("past window got %s, want refused", res.Outcome)
	}
}

// TestAdvanceWaitsMechanicalOnScopeViolation: a recorded scope verification that
// found an out-of-envelope change leaves the task waiting on mechanical repair.
func TestAdvanceWaitsMechanicalOnScopeViolation(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	now := time.Now().UTC()
	dec := recordAdmissionDecision(t, taskDir, now)
	recordCapabilityConsumption(t, taskDir, dec, now)
	recordScopeVerification(t, taskDir, false) // verification failed

	res, err := AdvanceResultTransition(context.Background(), advReq(repo, taskDir))
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeWaiting || res.OperationalStatus != StatusWaitingMechanical {
		t.Fatalf("got %s/%s, want waiting/waiting_mechanical_repair", res.Outcome, res.OperationalStatus)
	}
	if res.NextAction.Action != AdvanceNextMechanicalRepair {
		t.Fatalf("next = %q, want %q", res.NextAction.Action, AdvanceNextMechanicalRepair)
	}
}

// TestAdvanceFailsClosedAtScopeVerifiedWithoutValidResult: at the scope_verified
// terminal a missing/invalid result prerequisite is a typed refusal — never a false
// success and never a recorded transition — and still reports the current state.
func TestAdvanceFailsClosedAtScopeVerifiedWithoutValidResult(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	now := time.Now().UTC()
	dec := recordAdmissionDecision(t, taskDir, now)
	recordCapabilityConsumption(t, taskDir, dec, now)
	recordScopeVerification(t, taskDir, true) // scope_verified terminal

	res, err := AdvanceResultTransition(context.Background(), AdvanceResultRequest{
		RepositoryRoot: repo, TaskDirectory: taskDir, ResultRevision: "does-not-exist",
	})
	if err != nil {
		t.Fatalf("a bad result prerequisite must be a typed refusal result, not an error: %v", err)
	}
	if res.Outcome != OutcomeRefused {
		t.Fatalf("scope_verified with no valid result must refuse; got %s (recorded=%v)", res.Outcome, res.TransitionRecorded)
	}
	if res.TransitionRecorded {
		t.Fatal("no transition may be recorded when preparation fails")
	}
	if res.RefusalCode == "" {
		t.Fatal("a refusal must carry the underlying typed code")
	}
	if !res.CurrentStateAvailable || res.CurrentLedgerHeadSHA256 == "" {
		t.Fatal("a refusal must still report the current verified head")
	}
}

// TestNextActionTableIsSingleStep asserts that every machine action identity the
// orchestrator can emit has a summary, and that no summary smuggles a second action
// (no " then " / " and " connective that would recreate the flat-output failure).
func TestNextActionTableIsSingleStep(t *testing.T) {
	for id, summary := range advanceNextSummaries {
		if summary == "" {
			t.Errorf("action %q has no summary", id)
		}
		if containsSecondAction(summary) {
			t.Errorf("action %q summary describes more than one step: %q", id, summary)
		}
	}
}

func containsSecondAction(s string) bool {
	for _, conn := range []string{" then ", " and then ", ", then "} {
		if indexOf(s, conn) >= 0 {
			return true
		}
	}
	return false
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
