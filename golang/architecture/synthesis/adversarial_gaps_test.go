// SPDX-License-Identifier: AGPL-3.0-only

// adversarial_gaps_test.go closes every gap identified in the O1 Step 2
// coverage audit (PR #122): the design document's 11 state-machine proof
// bullets and 15 required fixtures, checked systematically against the
// tests transition_test.go already had. Each test here traces to exactly
// one row of that audit's coverage matrix.
package synthesis

import (
	"encoding/json"
	"reflect"
	"testing"
)

// --- real behavioral gap: invalid provider output was never wired in ---

func TestInvalidProviderOutputShortCircuitsToFailedWithoutEvaluation(t *testing.T) {
	state := driveToAttempting(t, freshCreatedState(t))
	attempt := buildAttempt(t, state.LatestPlanDigestSHA256, state.PlanGeneration, state.ExpectedAttemptNumber, ProviderStatusInvalidOutput)

	final, events, err := Transition(state, RecordAttemptCommand{Attempt: attempt})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if final.Phase != PhaseFailed {
		t.Fatalf("phase after invalid_output attempt = %s, want %s (must not proceed to Evaluating)", final.Phase, PhaseFailed)
	}
	if final.Receipt == nil || final.Receipt.TerminalReason != ReasonInvalidProviderOutput {
		t.Fatalf("expected TerminalReason %q, got %+v", ReasonInvalidProviderOutput, final.Receipt)
	}
	// Evidence preserved even though evaluation never happened.
	if final.Receipt.FinalAttemptDigestSHA256 == nil {
		t.Error("invalid-provider-output receipt must still preserve the attempt digest as evidence")
	}
	if final.Receipt.FinalEvaluationDigestSHA256 != nil {
		t.Error("invalid-provider-output receipt must not reference an evaluation — none was ever produced")
	}
	assertHasTerminatedEvent(t, events, ReasonInvalidProviderOutput)
	if err := ValidateReceiptSchema(mustMarshal(t, *final.Receipt)); err != nil {
		t.Errorf("receipt failed schema validation: %v", err)
	}
}

// --- proof #3: plan generations are monotonic (replan side, symmetric to
// the existing attempt-number test) ---

func TestPlanGenerationRejectsStaleGenerationAtReplan(t *testing.T) {
	state := driveToEvaluating(t, freshCreatedState(t))
	state, _ = driveEvaluation(t, state, RecommendReplan, "")
	planning, _ := mustTransition(t, state, StartPlanningCommand{})

	stale := buildPlan(t, planning.InterpretationDigestSHA256, 1) // generation 1 again, not 2
	if _, _, err := Transition(planning, RecordPlanCommand{Plan: stale}); err == nil {
		t.Fatal("expected a stale (non-monotonic) plan generation to be rejected at replan")
	}

	skipped := buildPlan(t, planning.InterpretationDigestSHA256, 3) // skips ahead
	if _, _, err := Transition(planning, RecordPlanCommand{Plan: skipped}); err == nil {
		t.Fatal("expected a skipped-ahead plan generation to be rejected at replan")
	}

	correct := buildPlan(t, planning.InterpretationDigestSHA256, 2)
	planned, _ := mustTransition(t, planning, RecordPlanCommand{Plan: correct})
	if planned.PlanGeneration != 2 {
		t.Errorf("PlanGeneration = %d, want 2", planned.PlanGeneration)
	}
}

// --- proofs #5/#6: retry/replan cannot occur without a classified
// evaluation — direct proof that the loop-back commands cannot be reached
// by skipping RecordEvaluation ---

func TestCannotSkipEvaluationToReachRetryOrReplan(t *testing.T) {
	evaluating := driveToEvaluating(t, freshCreatedState(t))

	if _, _, err := Transition(evaluating, StartAttemptCommand{}); err == nil {
		t.Error("StartAttemptCommand must not be legal directly from Evaluating (would skip evaluation to reach Attempting again)")
	}
	if _, _, err := Transition(evaluating, StartPlanningCommand{}); err == nil {
		t.Error("StartPlanningCommand must not be legal directly from Evaluating (would skip evaluation to reach Planning again)")
	}
}

// --- proof #7: candidate-ready requires a valid recorded attempt and
// evaluation — provable directly on the receipt's own evidence fields ---

func TestCandidateReadyReceiptReferencesRealAttemptAndEvaluation(t *testing.T) {
	state := driveToEvaluating(t, freshCreatedState(t))
	final, _ := driveEvaluation(t, state, RecommendAcceptCandidate, "2026-01-01T02:00:00Z")

	if final.Receipt.FinalAttemptDigestSHA256 == nil || *final.Receipt.FinalAttemptDigestSHA256 != state.LatestAttemptDigestSHA256 {
		t.Errorf("FinalAttemptDigestSHA256 = %v, want the real recorded attempt digest %q", final.Receipt.FinalAttemptDigestSHA256, state.LatestAttemptDigestSHA256)
	}
	if final.Receipt.FinalEvaluationDigestSHA256 == nil {
		t.Error("FinalEvaluationDigestSHA256 must be non-nil on a candidate-ready receipt")
	}
}

// --- proofs #10/required-fixture "evaluator claiming correctness or
// admission": Evaluation is a closed schema, same as every other artifact ---

func TestEvaluationRejectsUnknownExtraField(t *testing.T) {
	state := driveToEvaluating(t, freshCreatedState(t))
	eval := buildEvaluation(t, state.LatestAttemptDigestSHA256, RecommendAcceptCandidate)

	withClaim := injectExtraField(t, eval, "correctness_certified", true)
	if err := ValidateEvaluationSchema(withClaim); err == nil {
		t.Error("expected an evaluation claiming correctness_certified to fail schema validation (additionalProperties:false) — an evaluator may recommend, it may never certify correctness or admission itself")
	}

	withAdmission := injectExtraField(t, eval, "admission_status", "admitted")
	if err := ValidateEvaluationSchema(withAdmission); err == nil {
		t.Error("expected an evaluation claiming admission_status to fail schema validation")
	}
}

// --- required fixture "provider trying to enlarge a budget": no artifact
// schema carries any budget-shaped field, so the only representable attempt
// is an unknown extra field, which the closed schema rejects the same way ---

func TestProviderCannotEncodeABudgetViaAnyArtifactField(t *testing.T) {
	state := driveToEvaluating(t, freshCreatedState(t))
	eval := buildEvaluation(t, state.LatestAttemptDigestSHA256, RecommendRetryGeneration)

	withBudget := injectExtraField(t, eval, "retry_budget", 999)
	if err := ValidateEvaluationSchema(withBudget); err == nil {
		t.Error("expected an evaluation carrying a retry_budget field to fail schema validation — budgets are session-level and precommitted, never provider-supplied")
	}
}

// --- proof #4 / fixture "provider trying to enlarge a budget": budgets
// never increase across a real multi-loop sequence, not just at the two
// single-decrement call sites already tested ---

func TestBudgetsNeverIncreaseAcrossAFullSequence(t *testing.T) {
	session := fixtureSession(t)
	session.RetryBudget = 2
	session.ReplanBudget = 1
	digest, err := SessionDigest(session)
	if err != nil {
		t.Fatal(err)
	}
	session.SessionDigestSHA256 = digest

	state, err := NewSessionState(session)
	if err != nil {
		t.Fatal(err)
	}
	retryHistory := []int{state.RemainingRetryBudget}
	replanHistory := []int{state.RemainingReplanBudget}

	record := func(s SessionState) {
		retryHistory = append(retryHistory, s.RemainingRetryBudget)
		replanHistory = append(replanHistory, s.RemainingReplanBudget)
	}

	state = driveToEvaluating(t, state)
	record(state)
	state, _ = driveEvaluation(t, state, RecommendRetryGeneration, "") // consume 1 retry
	record(state)
	state = mustAdvanceToAttempting(t, state)
	record(state)
	attempt2 := buildAttempt(t, state.LatestPlanDigestSHA256, state.PlanGeneration, state.ExpectedAttemptNumber, ProviderStatusCompleted)
	state, _ = mustTransition(t, state, RecordAttemptCommand{Attempt: attempt2})
	record(state)
	state, _ = driveEvaluation(t, state, RecommendReplan, "") // consume the only replan
	record(state)
	planning, _ := mustTransition(t, state, StartPlanningCommand{})
	record(planning)
	plan2 := buildPlan(t, planning.InterpretationDigestSHA256, 2)
	planned, _ := mustTransition(t, planning, RecordPlanCommand{Plan: plan2})
	record(planned)
	state = driveToEvaluating(t, planned)
	record(state)
	final, _ := driveEvaluation(t, state, RecommendAcceptCandidate, "2026-01-01T09:00:00Z") // no further consumption
	record(final)

	assertNonIncreasing(t, "RemainingRetryBudget", retryHistory)
	assertNonIncreasing(t, "RemainingReplanBudget", replanHistory)

	if final.RemainingRetryBudget != 1 { // started 2, consumed exactly 1
		t.Errorf("RemainingRetryBudget at end = %d, want 1 (exactly one retry consumed)", final.RemainingRetryBudget)
	}
	if final.RemainingReplanBudget != 0 { // started 1, consumed exactly 1
		t.Errorf("RemainingReplanBudget at end = %d, want 0 (exactly one replan consumed)", final.RemainingReplanBudget)
	}
}

// TestReplanBudgetConsumedExactlyOnceAtAcceptedEvaluationOnly mirrors
// TestBudgetConsumedExactlyOnceAtAcceptedEvaluationOnly (transition_test.go,
// retry side) — that test never covered the replan side specifically.
func TestReplanBudgetConsumedExactlyOnceAtAcceptedEvaluationOnly(t *testing.T) {
	state := driveToEvaluating(t, freshCreatedState(t))
	initial := state.RemainingReplanBudget

	afterReplanDecision, _ := driveEvaluation(t, state, RecommendReplan, "")
	if afterReplanDecision.RemainingReplanBudget != initial-1 {
		t.Fatalf("after one accepted replan decision: remaining=%d, want %d", afterReplanDecision.RemainingReplanBudget, initial-1)
	}
	planning, _ := mustTransition(t, afterReplanDecision, StartPlanningCommand{})
	if planning.RemainingReplanBudget != initial-1 {
		t.Errorf("StartPlanning must not itself consume replan budget: remaining=%d, want %d", planning.RemainingReplanBudget, initial-1)
	}
}

func assertNonIncreasing(t *testing.T, name string, history []int) {
	t.Helper()
	for i := 1; i < len(history); i++ {
		if history[i] > history[i-1] {
			t.Errorf("%s increased at step %d: %d -> %d (full history: %v)", name, i, history[i-1], history[i], history)
		}
	}
}

// --- proof #8 / fixture "command submitted after terminal receipt": all 9
// command types, not just 4 ---

func TestTerminalSessionRejectsAllNineCommandTypes(t *testing.T) {
	state := driveToEvaluating(t, freshCreatedState(t))
	terminal, _ := driveEvaluation(t, state, RecommendAcceptCandidate, "2026-01-01T02:00:00Z")
	if !terminal.Phase.Terminal() {
		t.Fatalf("setup: expected a terminal phase, got %s", terminal.Phase)
	}

	commands := []Command{
		RecordInterpretationCommand{Interpretation: fixtureInterpretation(t, terminal.Session.SessionDigestSHA256)},
		StartPlanningCommand{},
		RecordPlanCommand{Plan: buildPlan(t, terminal.InterpretationDigestSHA256, 99)},
		StartAttemptCommand{},
		RecordAttemptCommand{Attempt: buildAttempt(t, terminal.LatestPlanDigestSHA256, terminal.PlanGeneration, 99, ProviderStatusCompleted)},
		RecordEvaluationCommand{Evaluation: buildEvaluation(t, terminal.LatestAttemptDigestSHA256, RecommendAcceptCandidate), CompletedAt: "2026-01-01T03:00:00Z"},
		EvaluatorUnavailableCommand{At: "2026-01-01T03:00:00Z"},
		AbortCommand{Reason: "r", At: "2026-01-01T03:00:00Z"},
		ResumeCommand{Observed: observedFromSession(terminal.Session), At: "2026-01-01T03:00:00Z"},
	}
	if len(commands) != 9 {
		t.Fatalf("test itself must cover exactly the 9 Command implementations, has %d", len(commands))
	}
	for _, cmd := range commands {
		t.Run(reflectTypeName(cmd), func(t *testing.T) {
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

func reflectTypeName(v any) string {
	return reflect.TypeOf(v).Name()
}

// --- proof #11: provider prose does not influence PHASE selection (it
// legitimately does influence an artifact's own digest — free text is
// real content — but Transition must decide only from typed enum fields) ---

func TestFreeTextDoesNotInfluencePhaseSelection(t *testing.T) {
	state := driveToEvaluating(t, freshCreatedState(t))

	terse := buildEvaluationWithDetail(t, state.LatestAttemptDigestSHA256, RecommendRetryGeneration, "")
	verbose := buildEvaluationWithDetail(t, state.LatestAttemptDigestSHA256, RecommendRetryGeneration,
		"extensive provider reasoning that happens to use words like 'admitted' and 'correct' in prose form")

	if terse.EvaluationDigestSHA256 == verbose.EvaluationDigestSHA256 {
		t.Fatal("setup invariant broken: different free text must produce different evaluation digests (it is real content)")
	}

	next1, _ := mustTransition(t, state, RecordEvaluationCommand{Evaluation: terse, CompletedAt: ""})
	next2, _ := mustTransition(t, state, RecordEvaluationCommand{Evaluation: verbose, CompletedAt: ""})

	if next1.Phase != next2.Phase {
		t.Errorf("free text changed the resulting phase: %s vs %s", next1.Phase, next2.Phase)
	}
	if next1.RemainingRetryBudget != next2.RemainingRetryBudget {
		t.Errorf("free text changed budget consumption: %d vs %d", next1.RemainingRetryBudget, next2.RemainingRetryBudget)
	}
}

func buildEvaluationWithDetail(t *testing.T, attemptDigest string, rec Recommendation, detail string) Evaluation {
	t.Helper()
	e := Evaluation{
		SchemaVersion:       EvaluationSchemaVersion,
		EvaluationID:        "evaluation.test",
		AttemptDigestSHA256: attemptDigest,
		EvaluatorKind:       "mechanical-test",
		EvaluatorVersion:    "v1",
		Checks:              []CheckObservation{{CheckID: "c1", Status: CheckPassed, Detail: detail, EvidenceReferences: nil}},
		Recommendation:      rec,
	}
	digest, err := EvaluationDigest(e)
	if err != nil {
		t.Fatal(err)
	}
	e.EvaluationDigestSHA256 = digest
	return NormalizeEvaluation(e)
}

// --- hard law 10: terminal failure preserves all prior evidence ---

func TestFailureReceiptsPreserveAttemptAndEvaluationEvidence(t *testing.T) {
	session := fixtureSession(t)
	session.RetryBudget = 0
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
	final, _ := driveEvaluation(t, state, RecommendRetryGeneration, "2026-01-01T05:00:00Z")

	if final.Phase != PhaseFailed || final.Receipt.TerminalReason != ReasonRetryBudgetExhausted {
		t.Fatalf("setup: expected Failed/retry-budget-exhausted, got %s/%v", final.Phase, final.Receipt)
	}
	if final.Receipt.FinalAttemptDigestSHA256 == nil || *final.Receipt.FinalAttemptDigestSHA256 != state.LatestAttemptDigestSHA256 {
		t.Error("retry-budget-exhausted receipt must preserve the final attempt as evidence")
	}
	if final.Receipt.FinalEvaluationDigestSHA256 == nil {
		t.Error("retry-budget-exhausted receipt must preserve the final (retry-recommending) evaluation as evidence")
	}
}

// --- helper: inject an unknown field into a marshaled document ---

func injectExtraField(t *testing.T, doc any, key string, value any) []byte {
	t.Helper()
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	raw[key] = value
	out, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
