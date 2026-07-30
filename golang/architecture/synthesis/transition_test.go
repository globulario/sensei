// SPDX-License-Identifier: AGPL-3.0-only

package synthesis

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

// --- local, parameterized artifact builders (transition tests need
// control over generation/attempt numbers the Step-1 fixtures don't
// expose) ---

func buildPlan(t *testing.T, interpretationDigest string, generation int) Plan {
	t.Helper()
	p := Plan{
		SchemaVersion:              PlanSchemaVersion,
		PlanID:                     fmt.Sprintf("plan.test.%d", generation),
		InterpretationDigestSHA256: interpretationDigest,
		PlanGeneration:             generation,
		Steps: []PlanStep{
			{StepID: "step.1", Description: "d", IntendedFiles: []string{"f.go"}, IntendedSymbols: nil, ExpectedEvidence: nil},
		},
		ProviderObservation: ProviderObservation{ProviderID: "p", ProviderKind: "k", ObservedAt: "2026-01-01T00:00:00Z"},
	}
	digest, err := PlanDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	p.PlanDigestSHA256 = digest
	return NormalizePlan(p)
}

func buildAttempt(t *testing.T, planDigest string, planGeneration, attemptNumber int, status TerminalProviderStatus) Attempt {
	t.Helper()
	a := Attempt{
		SchemaVersion:              AttemptSchemaVersion,
		AttemptID:                  fmt.Sprintf("attempt.test.%d", attemptNumber),
		AttemptNumber:              attemptNumber,
		PlanGeneration:             planGeneration,
		PlanDigestSHA256:           planDigest,
		InputCandidateDigestSHA256: zeroDigest,
		ProviderObservation:        ProviderObservation{ProviderID: "p", ProviderKind: "k", ObservedAt: "2026-01-01T00:10:00Z"},
		ProposedChangeDigestSHA256: zeroDigest,
		TerminalProviderStatus:     status,
		ProducedAt:                 "2026-01-01T00:10:00Z",
	}
	digest, err := AttemptDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	a.AttemptDigestSHA256 = digest
	return NormalizeAttempt(a)
}

func buildEvaluation(t *testing.T, attemptDigest string, rec Recommendation) Evaluation {
	t.Helper()
	e := Evaluation{
		SchemaVersion:       EvaluationSchemaVersion,
		EvaluationID:        "evaluation.test",
		AttemptDigestSHA256: attemptDigest,
		EvaluatorKind:       "mechanical-test",
		EvaluatorVersion:    "v1",
		Checks:              []CheckObservation{{CheckID: "c1", Status: CheckPassed, EvidenceReferences: nil}},
		Recommendation:      rec,
	}
	digest, err := EvaluationDigest(e)
	if err != nil {
		t.Fatal(err)
	}
	e.EvaluationDigestSHA256 = digest
	return NormalizeEvaluation(e)
}

func mustTransition(t *testing.T, state SessionState, cmd Command) (SessionState, []Event) {
	t.Helper()
	next, events, err := Transition(state, cmd)
	if err != nil {
		t.Fatalf("unexpected error transitioning from %s via %T: %v", state.Phase, cmd, err)
	}
	return next, events
}

func freshCreatedState(t *testing.T) SessionState {
	t.Helper()
	state, err := NewSessionState(fixtureSession(t))
	if err != nil {
		t.Fatalf("NewSessionState: %v", err)
	}
	return state
}

func driveToPlanned(t *testing.T, state SessionState) SessionState {
	t.Helper()
	interp := fixtureInterpretation(t, state.Session.SessionDigestSHA256)
	state, _ = mustTransition(t, state, RecordInterpretationCommand{Interpretation: interp})
	plan := buildPlan(t, state.InterpretationDigestSHA256, state.ExpectedPlanGeneration)
	state, _ = mustTransition(t, state, RecordPlanCommand{Plan: plan})
	return state
}

func driveToAttempting(t *testing.T, state SessionState) SessionState {
	t.Helper()
	if state.Phase == PhaseCreated {
		state = driveToPlanned(t, state)
	}
	state, _ = mustTransition(t, state, StartAttemptCommand{})
	return state
}

func driveToEvaluating(t *testing.T, state SessionState) SessionState {
	t.Helper()
	state = driveToAttempting(t, state)
	attempt := buildAttempt(t, state.LatestPlanDigestSHA256, state.PlanGeneration, state.ExpectedAttemptNumber, ProviderStatusCompleted)
	state, _ = mustTransition(t, state, RecordAttemptCommand{Attempt: attempt})
	return state
}

func driveEvaluation(t *testing.T, state SessionState, rec Recommendation, completedAt string) (SessionState, []Event) {
	t.Helper()
	eval := buildEvaluation(t, state.LatestAttemptDigestSHA256, rec)
	return mustTransition(t, state, RecordEvaluationCommand{Evaluation: eval, CompletedAt: completedAt})
}

// --- happy path ---

func TestHappyPath_CreatedToSucceeded(t *testing.T) {
	state := freshCreatedState(t)
	if state.Phase != PhaseCreated {
		t.Fatalf("initial phase = %s, want %s", state.Phase, PhaseCreated)
	}
	state = driveToEvaluating(t, state)
	if state.Phase != PhaseEvaluating {
		t.Fatalf("phase after attempt = %s, want %s", state.Phase, PhaseEvaluating)
	}
	final, events := driveEvaluation(t, state, RecommendAcceptCandidate, "2026-01-01T02:00:00Z")

	if final.Phase != PhaseSucceeded {
		t.Fatalf("final phase = %s, want %s", final.Phase, PhaseSucceeded)
	}
	if final.Receipt == nil {
		t.Fatal("expected a receipt on the terminal state")
	}
	if final.Receipt.TerminalReason != ReasonCandidateReadyForAdmission {
		t.Errorf("terminal reason = %q, want %q", final.Receipt.TerminalReason, ReasonCandidateReadyForAdmission)
	}
	if err := ValidateReceiptSchema(mustMarshal(t, *final.Receipt)); err != nil {
		t.Errorf("receipt failed schema validation: %v", err)
	}
	sawTerminated := false
	for _, e := range events {
		if term, ok := e.(SessionTerminatedEvent); ok {
			sawTerminated = true
			if term.TerminalReason != ReasonCandidateReadyForAdmission {
				t.Errorf("SessionTerminatedEvent reason = %q, want %q", term.TerminalReason, ReasonCandidateReadyForAdmission)
			}
		}
	}
	if !sawTerminated {
		t.Error("expected a SessionTerminatedEvent")
	}
}

// --- correction 2: Succeeded means candidate-ready-for-admission ONLY ---

func TestSucceededReceiptNeverClaimsAdmissionCompletionOrApproval(t *testing.T) {
	state := driveToEvaluating(t, freshCreatedState(t))
	final, _ := driveEvaluation(t, state, RecommendAcceptCandidate, "2026-01-01T02:00:00Z")

	r := final.Receipt
	if r == nil {
		t.Fatal("expected a receipt")
	}
	if r.TerminalReason != ReasonCandidateReadyForAdmission {
		t.Fatalf("terminal reason = %q, want exactly %q (never admitted/verified/completed/approved)", r.TerminalReason, ReasonCandidateReadyForAdmission)
	}
	// O1 has no admission producer (deferred to O5) — these must stay nil
	// even on a candidate-ready receipt.
	if r.AdmissionDecisionDigestSHA256 != nil {
		t.Errorf("AdmissionDecisionDigestSHA256 = %v, want nil (O1 has no admission producer)", *r.AdmissionDecisionDigestSHA256)
	}
	if r.AdmissionVerificationDigestSHA256 != nil {
		t.Errorf("AdmissionVerificationDigestSHA256 = %v, want nil (O1 has no admission producer)", *r.AdmissionVerificationDigestSHA256)
	}
}

// --- illegal transitions fail closed ---

func TestIllegalTransitions_FailClosedAndLeaveStateUnchanged(t *testing.T) {
	planned := driveToPlanned(t, freshCreatedState(t))
	evaluating := driveToEvaluating(t, freshCreatedState(t))

	cases := []struct {
		name  string
		state SessionState
		cmd   Command
	}{
		{"StartAttempt from Created", freshCreatedState(t), StartAttemptCommand{}},
		{"RecordPlan from Created", freshCreatedState(t), RecordPlanCommand{Plan: buildPlan(t, zeroDigest, 1)}},
		{"RecordAttempt from Planned", planned, RecordAttemptCommand{Attempt: buildAttempt(t, planned.LatestPlanDigestSHA256, 1, 1, ProviderStatusCompleted)}},
		{"RecordEvaluation from Planned", planned, RecordEvaluationCommand{Evaluation: buildEvaluation(t, zeroDigest, RecommendAcceptCandidate)}},
		{"StartPlanning from Created (must use RecordInterpretation)", freshCreatedState(t), StartPlanningCommand{}},
		{"RecordInterpretation a second time", planned, RecordInterpretationCommand{Interpretation: fixtureInterpretation(t, planned.Session.SessionDigestSHA256)}},
		{"EvaluatorUnavailable from Planned", planned, EvaluatorUnavailableCommand{At: "2026-01-01T00:00:00Z"}},
		{"RecordAttempt from Evaluating (already recorded)", evaluating, RecordAttemptCommand{Attempt: buildAttempt(t, evaluating.LatestPlanDigestSHA256, 1, 2, ProviderStatusCompleted)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := c.state
			after, events, err := Transition(c.state, c.cmd)
			if err == nil {
				t.Fatalf("expected an error, got none (phase now %s)", after.Phase)
			}
			if events != nil {
				t.Errorf("expected no events on an illegal transition, got %v", events)
			}
			if !reflect.DeepEqual(after, before) {
				t.Errorf("state changed on an illegal transition:\nbefore: %+v\nafter:  %+v", before, after)
			}
		})
	}
}

func TestTerminalSessionRejectsAnyCommand(t *testing.T) {
	state := driveToEvaluating(t, freshCreatedState(t))
	terminal, _ := driveEvaluation(t, state, RecommendAcceptCandidate, "2026-01-01T02:00:00Z")
	if !terminal.Phase.Terminal() {
		t.Fatalf("setup: expected a terminal phase, got %s", terminal.Phase)
	}

	commands := []Command{
		StartAttemptCommand{},
		StartPlanningCommand{},
		AbortCommand{Reason: "r", At: "2026-01-01T03:00:00Z"},
		ResumeCommand{Observed: observedFromSession(terminal.Session), At: "2026-01-01T03:00:00Z"},
	}
	for _, cmd := range commands {
		t.Run(fmt.Sprintf("%T", cmd), func(t *testing.T) {
			after, events, err := Transition(terminal, cmd)
			if err == nil {
				t.Fatalf("expected terminal session to reject %T, got phase %s", cmd, after.Phase)
			}
			if events != nil {
				t.Errorf("expected no events, got %v", events)
			}
			if !reflect.DeepEqual(after, terminal) {
				t.Error("terminal state was mutated by a rejected command")
			}
		})
	}
}

// --- correction 3: recorded counters advance only when the artifact is accepted ---

func TestAttemptNumberAdvancesOnlyOnRecordAttempt(t *testing.T) {
	state := driveToPlanned(t, freshCreatedState(t))
	if state.AttemptNumber != 0 {
		t.Fatalf("AttemptNumber before any attempt = %d, want 0", state.AttemptNumber)
	}

	started, _ := mustTransition(t, state, StartAttemptCommand{})
	if started.AttemptNumber != 0 {
		t.Errorf("StartAttempt must not advance the recorded AttemptNumber: got %d, want 0", started.AttemptNumber)
	}
	if started.ExpectedAttemptNumber != 1 {
		t.Errorf("ExpectedAttemptNumber = %d, want 1", started.ExpectedAttemptNumber)
	}

	// Interrupt: abort right after StartAttempt, before any Attempt is
	// recorded. No phantom attempt must exist.
	aborted, _ := mustTransition(t, started, AbortCommand{Reason: "interrupted", At: "2026-01-01T00:30:00Z"})
	if aborted.AttemptNumber != 0 {
		t.Errorf("an interrupted attempt phase left a phantom recorded AttemptNumber: got %d, want 0", aborted.AttemptNumber)
	}

	attempt := buildAttempt(t, state.LatestPlanDigestSHA256, state.PlanGeneration, 1, ProviderStatusCompleted)
	recorded, _ := mustTransition(t, started, RecordAttemptCommand{Attempt: attempt})
	if recorded.AttemptNumber != 1 {
		t.Errorf("AttemptNumber after RecordAttempt = %d, want 1", recorded.AttemptNumber)
	}
}

func TestPlanGenerationAdvancesOnlyOnRecordPlan(t *testing.T) {
	session := fixtureSession(t)
	created, err := NewSessionState(session)
	if err != nil {
		t.Fatal(err)
	}
	interp := fixtureInterpretation(t, session.SessionDigestSHA256)
	planning, _ := mustTransition(t, created, RecordInterpretationCommand{Interpretation: interp})
	if planning.PlanGeneration != 0 {
		t.Fatalf("PlanGeneration before any plan = %d, want 0", planning.PlanGeneration)
	}
	if planning.ExpectedPlanGeneration != 1 {
		t.Fatalf("ExpectedPlanGeneration = %d, want 1", planning.ExpectedPlanGeneration)
	}

	// Interrupt before RecordPlan: no phantom generation.
	aborted, _ := mustTransition(t, planning, AbortCommand{Reason: "interrupted", At: "2026-01-01T00:05:00Z"})
	if aborted.PlanGeneration != 0 {
		t.Errorf("an interrupted planning phase left a phantom recorded PlanGeneration: got %d, want 0", aborted.PlanGeneration)
	}
}

func TestAttemptNumbersMonotonicAcrossRetries(t *testing.T) {
	state := driveToEvaluating(t, freshCreatedState(t))
	state, _ = driveEvaluation(t, state, RecommendRetryGeneration, "")
	if state.Phase != PhaseRetry {
		t.Fatalf("phase after retry-generation = %s, want %s", state.Phase, PhaseRetry)
	}
	state, _ = mustTransition(t, state, StartAttemptCommand{})
	if state.ExpectedAttemptNumber != 2 {
		t.Fatalf("ExpectedAttemptNumber after second StartAttempt = %d, want 2", state.ExpectedAttemptNumber)
	}
	attempt2 := buildAttempt(t, state.LatestPlanDigestSHA256, state.PlanGeneration, 2, ProviderStatusCompleted)
	state, _ = mustTransition(t, state, RecordAttemptCommand{Attempt: attempt2})
	if state.AttemptNumber != 2 {
		t.Fatalf("AttemptNumber after second attempt = %d, want 2", state.AttemptNumber)
	}

	// Attempting to record attempt number 1 again (non-monotonic) must fail.
	// Reaching Attempting a third time requires another accepted retry
	// decision first — StartAttempt is only legal from Planned or Retry.
	retried, _ := driveEvaluation(t, state, RecommendRetryGeneration, "")
	stale := buildAttempt(t, state.LatestPlanDigestSHA256, state.PlanGeneration, 1, ProviderStatusCompleted)
	_, _, err := Transition(mustAdvanceToAttempting(t, retried), RecordAttemptCommand{Attempt: stale})
	if err == nil {
		t.Fatal("expected a stale (non-monotonic) attempt number to be rejected")
	}
}

func mustAdvanceToAttempting(t *testing.T, state SessionState) SessionState {
	t.Helper()
	next, _ := mustTransition(t, state, StartAttemptCommand{})
	return next
}

// --- retry then success / replan then success (required fixtures) ---

func TestRetryThenSuccess(t *testing.T) {
	state := driveToEvaluating(t, freshCreatedState(t))
	initialRemaining := state.RemainingRetryBudget

	state, events := driveEvaluation(t, state, RecommendRetryGeneration, "")
	if state.Phase != PhaseRetry {
		t.Fatalf("phase = %s, want %s", state.Phase, PhaseRetry)
	}
	if state.RemainingRetryBudget != initialRemaining-1 {
		t.Fatalf("RemainingRetryBudget = %d, want %d", state.RemainingRetryBudget, initialRemaining-1)
	}
	assertNoTerminatedEvent(t, events)

	state = mustAdvanceToAttempting(t, state)
	attempt2 := buildAttempt(t, state.LatestPlanDigestSHA256, state.PlanGeneration, state.ExpectedAttemptNumber, ProviderStatusCompleted)
	state, _ = mustTransition(t, state, RecordAttemptCommand{Attempt: attempt2})

	final, _ := driveEvaluation(t, state, RecommendAcceptCandidate, "2026-01-01T03:00:00Z")
	if final.Phase != PhaseSucceeded {
		t.Fatalf("final phase = %s, want %s", final.Phase, PhaseSucceeded)
	}
	if final.Receipt.RetryCount != 1 {
		t.Errorf("Receipt.RetryCount = %d, want 1", final.Receipt.RetryCount)
	}
}

func TestReplanThenSuccess(t *testing.T) {
	state := driveToEvaluating(t, freshCreatedState(t))
	initialRemaining := state.RemainingReplanBudget

	state, _ = driveEvaluation(t, state, RecommendReplan, "")
	if state.Phase != PhaseReplan {
		t.Fatalf("phase = %s, want %s", state.Phase, PhaseReplan)
	}
	if state.RemainingReplanBudget != initialRemaining-1 {
		t.Fatalf("RemainingReplanBudget = %d, want %d", state.RemainingReplanBudget, initialRemaining-1)
	}

	planning, _ := mustTransition(t, state, StartPlanningCommand{})
	if planning.ExpectedPlanGeneration != 2 {
		t.Fatalf("ExpectedPlanGeneration after replan = %d, want 2", planning.ExpectedPlanGeneration)
	}
	plan2 := buildPlan(t, planning.InterpretationDigestSHA256, 2)
	planned, _ := mustTransition(t, planning, RecordPlanCommand{Plan: plan2})
	if planned.PlanGeneration != 2 {
		t.Fatalf("PlanGeneration after replan = %d, want 2", planned.PlanGeneration)
	}

	evaluating := driveToEvaluating(t, planned)
	final, _ := driveEvaluation(t, evaluating, RecommendAcceptCandidate, "2026-01-01T04:00:00Z")
	if final.Phase != PhaseSucceeded {
		t.Fatalf("final phase = %s, want %s", final.Phase, PhaseSucceeded)
	}
	if final.Receipt.ReplanCount != 1 {
		t.Errorf("Receipt.ReplanCount = %d, want 1", final.Receipt.ReplanCount)
	}
}

// --- budget exhaustion (required fixtures) + never-increase/never-underflow ---

func TestRetryBudgetExhaustion(t *testing.T) {
	session := fixtureSession(t)
	session.RetryBudget = 1
	digest, err := SessionDigest(session)
	if err != nil {
		t.Fatal(err)
	}
	session.SessionDigestSHA256 = digest

	seed, err := NewSessionState(session)
	if err != nil {
		t.Fatal(err)
	}
	state := driveToEvaluating(t, seed)
	state, _ = driveEvaluation(t, state, RecommendRetryGeneration, "")
	if state.Phase != PhaseRetry || state.RemainingRetryBudget != 0 {
		t.Fatalf("after consuming the only retry unit: phase=%s remaining=%d, want %s/0", state.Phase, state.RemainingRetryBudget, PhaseRetry)
	}

	state = mustAdvanceToAttempting(t, state)
	attempt2 := buildAttempt(t, state.LatestPlanDigestSHA256, state.PlanGeneration, state.ExpectedAttemptNumber, ProviderStatusCompleted)
	state, _ = mustTransition(t, state, RecordAttemptCommand{Attempt: attempt2})

	final, events := driveEvaluation(t, state, RecommendRetryGeneration, "2026-01-01T05:00:00Z")
	if final.Phase != PhaseFailed {
		t.Fatalf("phase after exhausting retry budget = %s, want %s", final.Phase, PhaseFailed)
	}
	if final.Receipt.TerminalReason != ReasonRetryBudgetExhausted {
		t.Errorf("terminal reason = %q, want %q", final.Receipt.TerminalReason, ReasonRetryBudgetExhausted)
	}
	if final.RemainingRetryBudget < 0 {
		t.Errorf("RemainingRetryBudget underflowed: %d", final.RemainingRetryBudget)
	}
	assertHasTerminatedEvent(t, events, ReasonRetryBudgetExhausted)
}

func TestReplanBudgetExhaustion(t *testing.T) {
	session := fixtureSession(t)
	session.ReplanBudget = 1
	digest, err := SessionDigest(session)
	if err != nil {
		t.Fatal(err)
	}
	session.SessionDigestSHA256 = digest

	seed, err := NewSessionState(session)
	if err != nil {
		t.Fatal(err)
	}
	state := driveToEvaluating(t, seed)
	state, _ = driveEvaluation(t, state, RecommendReplan, "")
	planning, _ := mustTransition(t, state, StartPlanningCommand{})
	plan2 := buildPlan(t, planning.InterpretationDigestSHA256, 2)
	planned, _ := mustTransition(t, planning, RecordPlanCommand{Plan: plan2})
	evaluating := driveToEvaluating(t, planned)

	final, events := driveEvaluation(t, evaluating, RecommendReplan, "2026-01-01T06:00:00Z")
	if final.Phase != PhaseFailed {
		t.Fatalf("phase after exhausting replan budget = %s, want %s", final.Phase, PhaseFailed)
	}
	if final.Receipt.TerminalReason != ReasonReplanBudgetExhausted {
		t.Errorf("terminal reason = %q, want %q", final.Receipt.TerminalReason, ReasonReplanBudgetExhausted)
	}
	if final.RemainingReplanBudget < 0 {
		t.Errorf("RemainingReplanBudget underflowed: %d", final.RemainingReplanBudget)
	}
	assertHasTerminatedEvent(t, events, ReasonReplanBudgetExhausted)
}

// --- budget consumed exactly once per accepted evaluation, never at Start* ---

func TestBudgetConsumedExactlyOnceAtAcceptedEvaluationOnly(t *testing.T) {
	state := driveToEvaluating(t, freshCreatedState(t))
	initial := state.RemainingRetryBudget

	// StartAttempt alone (reached via Retry -> StartAttempt loop) must never
	// touch the budget; only the RecordEvaluation(retry-generation) that
	// enters Retry does.
	afterFirstRetryDecision, _ := driveEvaluation(t, state, RecommendRetryGeneration, "")
	if afterFirstRetryDecision.RemainingRetryBudget != initial-1 {
		t.Fatalf("after one accepted retry decision: remaining=%d, want %d", afterFirstRetryDecision.RemainingRetryBudget, initial-1)
	}
	started := mustAdvanceToAttempting(t, afterFirstRetryDecision)
	if started.RemainingRetryBudget != initial-1 {
		t.Errorf("StartAttempt must not itself consume budget: remaining=%d, want %d", started.RemainingRetryBudget, initial-1)
	}
}

// --- resume: matching identity is a true no-op ---

func observedFromSession(s Session) ObservedIdentity {
	return ObservedIdentity{
		RepositoryDomain:              s.RepositoryDomain,
		BaseRevision:                  s.BaseRevision,
		WorkspaceIdentityDigestSHA256: s.WorkspaceIdentityDigestSHA256,
		GraphAuthorityDigestSHA256:    s.GraphAuthorityDigestSHA256,
		TaskSessionDigestSHA256:       s.TaskSessionDigestSHA256,
		ClosureDigestSHA256:           s.ClosureDigestSHA256,
		ProofObligationDigests:        append([]string{}, s.ProofObligationDigests...),
	}
}

func TestResumeMatchingIdentityIsAByteForByteNoOp(t *testing.T) {
	state := driveToPlanned(t, freshCreatedState(t))
	observed := observedFromSession(state.Session)

	next, events, err := Transition(state, ResumeCommand{Observed: observed, At: "2026-01-01T00:20:00Z"})
	if err != nil {
		t.Fatalf("unexpected error on a matching resume: %v", err)
	}
	if !reflect.DeepEqual(next, state) {
		t.Errorf("matching resume changed state:\nbefore: %+v\nafter:  %+v", state, next)
	}
	if len(events) != 1 {
		t.Fatalf("expected exactly one event, got %d", len(events))
	}
	if _, ok := events[0].(ResumeValidatedEvent); !ok {
		t.Errorf("expected ResumeValidatedEvent, got %T", events[0])
	}
}

// --- resume: every bound identity field is independently checked ---

func TestResumeRejectsEachDriftedIdentityField(t *testing.T) {
	state := driveToPlanned(t, freshCreatedState(t))

	cases := []struct {
		name   string
		mutate func(o *ObservedIdentity)
	}{
		{"repository domain", func(o *ObservedIdentity) { o.RepositoryDomain = "github.com/other/repo" }},
		{"base revision", func(o *ObservedIdentity) { o.BaseRevision = "deadbeef" }},
		{"workspace identity digest", func(o *ObservedIdentity) { o.WorkspaceIdentityDigestSHA256 = otherDigest }},
		{"graph authority digest", func(o *ObservedIdentity) { o.GraphAuthorityDigestSHA256 = otherDigest }},
		{"task session digest", func(o *ObservedIdentity) { o.TaskSessionDigestSHA256 = otherDigest }},
		{"closure digest", func(o *ObservedIdentity) { o.ClosureDigestSHA256 = otherDigest }},
		{"proof obligation set", func(o *ObservedIdentity) { o.ProofObligationDigests = []string{otherDigest} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			observed := observedFromSession(state.Session)
			c.mutate(&observed)
			next, events, err := Transition(state, ResumeCommand{Observed: observed, At: "2026-01-01T00:20:00Z"})
			if err != nil {
				t.Fatalf("drift must be a governed refusal (err==nil, Failed phase), not a Go error: %v", err)
			}
			if next.Phase != PhaseFailed {
				t.Fatalf("phase after %s drift = %s, want %s", c.name, next.Phase, PhaseFailed)
			}
			if next.Receipt == nil || next.Receipt.TerminalReason != ReasonIdentityDriftRefused {
				t.Fatalf("expected TerminalReason %q for %s drift", ReasonIdentityDriftRefused, c.name)
			}
			assertHasTerminatedEvent(t, events, ReasonIdentityDriftRefused)
		})
	}
}

const otherDigest = "1111111111111111111111111111111111111111111111111111111111111111"

// --- abort from every non-terminal phase (adjustment: not only Evaluating) ---

func TestAbortFromEveryNonTerminalPhase(t *testing.T) {
	build := map[Phase]func(t *testing.T) SessionState{
		PhaseCreated: func(t *testing.T) SessionState { return freshCreatedState(t) },
		PhasePlanning: func(t *testing.T) SessionState {
			s := freshCreatedState(t)
			interp := fixtureInterpretation(t, s.Session.SessionDigestSHA256)
			s, _ = mustTransition(t, s, RecordInterpretationCommand{Interpretation: interp})
			return s
		},
		PhasePlanned:    func(t *testing.T) SessionState { return driveToPlanned(t, freshCreatedState(t)) },
		PhaseAttempting: func(t *testing.T) SessionState { return driveToAttempting(t, freshCreatedState(t)) },
		PhaseEvaluating: func(t *testing.T) SessionState { return driveToEvaluating(t, freshCreatedState(t)) },
		PhaseRetry: func(t *testing.T) SessionState {
			s := driveToEvaluating(t, freshCreatedState(t))
			s, _ = driveEvaluation(t, s, RecommendRetryGeneration, "")
			return s
		},
		PhaseReplan: func(t *testing.T) SessionState {
			s := driveToEvaluating(t, freshCreatedState(t))
			s, _ = driveEvaluation(t, s, RecommendReplan, "")
			return s
		},
	}
	for phase, setup := range build {
		t.Run(string(phase), func(t *testing.T) {
			state := setup(t)
			if state.Phase != phase {
				t.Fatalf("setup produced phase %s, want %s", state.Phase, phase)
			}
			final, events, err := Transition(state, AbortCommand{Reason: "operator stop", At: "2026-01-01T09:00:00Z"})
			if err != nil {
				t.Fatalf("abort from %s must succeed: %v", phase, err)
			}
			if final.Phase != PhaseFailed {
				t.Fatalf("phase after abort from %s = %s, want %s", phase, final.Phase, PhaseFailed)
			}
			if final.Receipt == nil || final.Receipt.TerminalReason != ReasonExplicitlyAborted {
				t.Fatalf("expected TerminalReason %q aborting from %s", ReasonExplicitlyAborted, phase)
			}
			assertHasTerminatedEvent(t, events, ReasonExplicitlyAborted)
		})
	}
}

// --- evaluator-unavailable / architect-review (other terminal reasons) ---

func TestEvaluatorUnavailable(t *testing.T) {
	state := driveToEvaluating(t, freshCreatedState(t))
	final, events, err := Transition(state, EvaluatorUnavailableCommand{Detail: "backend down", At: "2026-01-01T07:00:00Z"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if final.Phase != PhaseFailed || final.Receipt.TerminalReason != ReasonEvaluatorUnavailable {
		t.Fatalf("got phase=%s reason=%v, want %s/%s", final.Phase, final.Receipt, PhaseFailed, ReasonEvaluatorUnavailable)
	}
	assertHasTerminatedEvent(t, events, ReasonEvaluatorUnavailable)
}

func TestArchitectReviewRequired(t *testing.T) {
	state := driveToEvaluating(t, freshCreatedState(t))
	final, _ := driveEvaluation(t, state, RecommendArchitectReview, "2026-01-01T07:30:00Z")
	if final.Phase != PhaseFailed || final.Receipt.TerminalReason != ReasonArchitectReviewRequired {
		t.Fatalf("got phase=%s reason=%v, want %s/%s", final.Phase, final.Receipt, PhaseFailed, ReasonArchitectReviewRequired)
	}
}

// --- cross-reference integrity (required fixtures) ---

func TestAttemptReferencingWrongPlanGenerationRejected(t *testing.T) {
	state := driveToAttempting(t, freshCreatedState(t))
	wrong := buildAttempt(t, state.LatestPlanDigestSHA256, state.PlanGeneration+1, state.ExpectedAttemptNumber, ProviderStatusCompleted)
	_, _, err := Transition(state, RecordAttemptCommand{Attempt: wrong})
	if err == nil {
		t.Fatal("expected an attempt referencing the wrong plan generation to be rejected")
	}
}

func TestEvaluationReferencingWrongAttemptRejected(t *testing.T) {
	state := driveToEvaluating(t, freshCreatedState(t))
	wrong := buildEvaluation(t, otherDigest, RecommendAcceptCandidate)
	_, _, err := Transition(state, RecordEvaluationCommand{Evaluation: wrong, CompletedAt: "2026-01-01T02:00:00Z"})
	if err == nil {
		t.Fatal("expected an evaluation referencing the wrong attempt to be rejected")
	}
}

// --- malformed / unknown enum fields are rejected via real schema validation ---

func TestMalformedTerminalProviderStatusRejected(t *testing.T) {
	state := driveToAttempting(t, freshCreatedState(t))
	attempt := buildAttempt(t, state.LatestPlanDigestSHA256, state.PlanGeneration, state.ExpectedAttemptNumber, ProviderStatusCompleted)
	attempt.TerminalProviderStatus = "not-a-real-status"
	_, _, err := Transition(state, RecordAttemptCommand{Attempt: attempt})
	if err == nil {
		t.Fatal("expected an unknown terminal_provider_status enum value to fail schema validation")
	}
}

// --- correction 6: replan reuses the original interpretation ---

func TestReplanReusesOriginalInterpretation_MismatchedInterpretationRejected(t *testing.T) {
	state := driveToEvaluating(t, freshCreatedState(t))
	state, _ = driveEvaluation(t, state, RecommendReplan, "")
	planning, _ := mustTransition(t, state, StartPlanningCommand{})

	// A plan for generation 2 that claims a DIFFERENT interpretation must
	// be rejected — replanning never accepts a new interpretation.
	differentInterpretation := buildPlan(t, otherDigest, 2)
	_, _, err := Transition(planning, RecordPlanCommand{Plan: differentInterpretation})
	if err == nil {
		t.Fatal("expected a plan referencing a different interpretation on replan to be rejected")
	}

	correct := buildPlan(t, planning.InterpretationDigestSHA256, 2)
	planned, _ := mustTransition(t, planning, RecordPlanCommand{Plan: correct})
	if planned.InterpretationDigestSHA256 != state.InterpretationDigestSHA256 {
		t.Errorf("InterpretationDigestSHA256 changed across replan: %q -> %q", state.InterpretationDigestSHA256, planned.InterpretationDigestSHA256)
	}
}

// --- determinism: same state + same command -> byte-for-byte equal result ---

func TestSameStateSameCommandIsDeterministic(t *testing.T) {
	state := driveToEvaluating(t, freshCreatedState(t))
	eval := buildEvaluation(t, state.LatestAttemptDigestSHA256, RecommendAcceptCandidate)
	cmd := RecordEvaluationCommand{Evaluation: eval, CompletedAt: "2026-01-01T02:00:00Z"}

	next1, events1, err1 := Transition(state, cmd)
	next2, events2, err2 := Transition(state, cmd)
	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected errors: %v / %v", err1, err2)
	}
	if !reflect.DeepEqual(next1, next2) {
		t.Errorf("Transition is not deterministic across identical inputs:\n%+v\nvs\n%+v", next1, next2)
	}
	if !reflect.DeepEqual(events1, events2) {
		t.Errorf("events differ across identical inputs:\n%+v\nvs\n%+v", events1, events2)
	}
}

// --- provider prose / timestamps cannot influence transition identity ---

func TestAttemptProducedAtDoesNotInfluenceResultingState(t *testing.T) {
	state := driveToAttempting(t, freshCreatedState(t))
	a1 := buildAttempt(t, state.LatestPlanDigestSHA256, state.PlanGeneration, state.ExpectedAttemptNumber, ProviderStatusCompleted)
	a2 := a1
	a2.ProducedAt = "2099-12-31T23:59:59Z"
	a2ID, err := AttemptDigest(a2)
	if err != nil {
		t.Fatal(err)
	}
	if a2ID != a1.AttemptDigestSHA256 {
		t.Fatalf("setup invariant broken: AttemptDigest depends on ProducedAt")
	}

	next1, _ := mustTransition(t, state, RecordAttemptCommand{Attempt: a1})
	next2, _ := mustTransition(t, state, RecordAttemptCommand{Attempt: a2})
	if !reflect.DeepEqual(next1, next2) {
		t.Errorf("resulting SessionState differs only because ProducedAt differed:\n%+v\nvs\n%+v", next1, next2)
	}
}

// --- helpers ---

func assertHasTerminatedEvent(t *testing.T, events []Event, reason TerminalReason) {
	t.Helper()
	for _, e := range events {
		if term, ok := e.(SessionTerminatedEvent); ok {
			if term.TerminalReason != reason {
				t.Errorf("SessionTerminatedEvent reason = %q, want %q", term.TerminalReason, reason)
			}
			return
		}
	}
	t.Errorf("expected a SessionTerminatedEvent with reason %q, got %v", reason, events)
}

func assertNoTerminatedEvent(t *testing.T, events []Event) {
	t.Helper()
	for _, e := range events {
		if _, ok := e.(SessionTerminatedEvent); ok {
			t.Errorf("unexpected SessionTerminatedEvent: %v", e)
		}
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
