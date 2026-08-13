// SPDX-License-Identifier: AGPL-3.0-only

// mapping_test.go proves MapToCommand (mapping.go), the O2-to-O1 pure
// mapping layer checkpoint accepted after the alarm-bell test repair on
// PR #124 head 428a5a778bdd3436bebeea42c16006e1db9a4215: given a validated
// O2 Request and its completed Result, it produces the exact
// synthesis.Command golang/architecture/synthesis.Transition must be
// called with next, rejecting stale identity, wrong parent, wrong
// generation, and wrong attempt BEFORE Transition ever sees them. No
// mapping test here ever calls synthesis.Transition itself except to drive
// realistic SessionState fixtures forward -- MapToCommand's own
// non-mutation and determinism are proven directly.
package providerport

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// --- SessionState drivers: build realistic O1 state at each phase this
// package's four operations map into, using only synthesis's exported API
// (NewSessionState/Transition/Command types), mirroring
// golang/architecture/synthesis's own transition_test.go drive helpers. ---

func driveToCreated(t *testing.T) (synthesis.SessionState, synthesis.Session) {
	t.Helper()
	session := fixtureSynthesisSession(t)
	state, err := synthesis.NewSessionState(session)
	if err != nil {
		t.Fatalf("synthesis.NewSessionState: %v", err)
	}
	return state, session
}

func driveToPlanning(t *testing.T) (synthesis.SessionState, synthesis.Session, synthesis.Interpretation) {
	t.Helper()
	state, session := driveToCreated(t)
	interp := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	next, _, err := synthesis.Transition(state, testCertifiedInterpretationCommand(t, state, interp))
	if err != nil {
		t.Fatalf("synthesis.Transition(RecordInterpretation): %v", err)
	}
	return next, session, interp
}

func driveToAttempting(t *testing.T) (synthesis.SessionState, synthesis.Session, synthesis.Interpretation, synthesis.Plan) {
	t.Helper()
	state, session, interp := driveToPlanning(t)
	plan := fixtureSynthesisPlan(t, interp.InterpretationDigestSHA256)
	planned, _, err := synthesis.Transition(state, synthesis.RecordPlanCommand{Plan: plan})
	if err != nil {
		t.Fatalf("synthesis.Transition(RecordPlan): %v", err)
	}
	attempting, _, err := synthesis.Transition(planned, synthesis.StartAttemptCommand{})
	if err != nil {
		t.Fatalf("synthesis.Transition(StartAttempt): %v", err)
	}
	return attempting, session, interp, plan
}

func driveToEvaluating(t *testing.T) (synthesis.SessionState, synthesis.Session, synthesis.Interpretation, synthesis.Plan, synthesis.Attempt) {
	t.Helper()
	state, session, interp, plan := driveToAttempting(t)
	attempt := fixtureSynthesisAttempt(t, plan.PlanDigestSHA256, plan.PlanGeneration)
	evaluating, _, err := synthesis.Transition(state, synthesis.RecordAttemptCommand{Attempt: attempt})
	if err != nil {
		t.Fatalf("synthesis.Transition(RecordAttempt): %v", err)
	}
	return evaluating, session, interp, plan, attempt
}

// --- happy path: each operation maps to the exact expected command ---

func TestMapToCommandInterpretation(t *testing.T) {
	state, session := driveToCreated(t)
	request := fixtureInterpretationRequest(t, session)
	candidate := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	result := fixtureInterpretationResult(t, request.RequestDigestSHA256, candidate)

	cmd, err := MapToCommand(state, request, result, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := cmd.(synthesis.RecordInterpretationCommand)
	if !ok {
		t.Fatalf("command type = %T, want synthesis.RecordInterpretationCommand", cmd)
	}
	if got.Interpretation.InterpretationDigestSHA256 != candidate.InterpretationDigestSHA256 {
		t.Error("mapped command does not carry the exact candidate interpretation")
	}
}

func TestMapToCommandPlanning(t *testing.T) {
	state, session, interp := driveToPlanning(t)
	request := fixturePlanningRequest(t, session, interp)
	candidate := fixtureSynthesisPlan(t, interp.InterpretationDigestSHA256)
	result := fixturePlanningResult(t, request.RequestDigestSHA256, candidate)

	cmd, err := MapToCommand(state, request, result, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := cmd.(synthesis.RecordPlanCommand)
	if !ok {
		t.Fatalf("command type = %T, want synthesis.RecordPlanCommand", cmd)
	}
	if got.Plan.PlanDigestSHA256 != candidate.PlanDigestSHA256 {
		t.Error("mapped command does not carry the exact candidate plan")
	}
}

func TestMapToCommandGeneration(t *testing.T) {
	state, session, _, plan := driveToAttempting(t)
	request := fixtureGenerationRequest(t, session, plan)
	candidate := fixtureSynthesisAttempt(t, plan.PlanDigestSHA256, plan.PlanGeneration)
	result := fixtureGenerationResult(t, request.RequestDigestSHA256, candidate)

	cmd, err := MapToCommand(state, request, result, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := cmd.(synthesis.RecordAttemptCommand)
	if !ok {
		t.Fatalf("command type = %T, want synthesis.RecordAttemptCommand", cmd)
	}
	if got.Attempt.AttemptDigestSHA256 != candidate.AttemptDigestSHA256 {
		t.Error("mapped command does not carry the exact candidate attempt")
	}
}

func TestMapToCommandEvaluationObservation(t *testing.T) {
	state, session, _, _, attempt := driveToEvaluating(t)
	request := fixtureEvaluationObservationRequest(t, session, attempt)
	candidate := fixtureSynthesisEvaluation(t, attempt.AttemptDigestSHA256)
	result := fixtureEvaluationObservationResult(t, request.RequestDigestSHA256, candidate)

	const completedAt = "2026-01-01T00:20:00Z"
	cmd, err := MapToCommand(state, request, result, completedAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := cmd.(synthesis.RecordEvaluationCommand)
	if !ok {
		t.Fatalf("command type = %T, want synthesis.RecordEvaluationCommand", cmd)
	}
	if got.Evaluation.EvaluationDigestSHA256 != candidate.EvaluationDigestSHA256 {
		t.Error("mapped command does not carry the exact candidate evaluation")
	}
	if got.CompletedAt != completedAt {
		t.Errorf("CompletedAt = %q, want %q -- MapToCommand must never read a clock, only forward the caller-supplied value", got.CompletedAt, completedAt)
	}
}

// --- the mapped chain feeds real synthesis.Transition calls ---

func TestMapToCommandOutputIsAcceptedByTransition(t *testing.T) {
	state, session := driveToCreated(t)
	request := fixtureInterpretationRequest(t, session)
	candidate := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	result := fixtureInterpretationResult(t, request.RequestDigestSHA256, candidate)

	accepted, err := MapInterpretationCandidate(state, request, result)
	if err != nil {
		t.Fatalf("MapInterpretationCandidate: %v", err)
	}
	cmd := testCertifiedInterpretationCommand(t, state, accepted)
	next, events, err := synthesis.Transition(state, cmd)
	if err != nil {
		t.Fatalf("synthesis.Transition rejected the certified command: %v", err)
	}
	if next.Phase != synthesis.PhasePlanning {
		t.Errorf("Phase after the mapped command = %s, want %s", next.Phase, synthesis.PhasePlanning)
	}
	if len(events) == 0 {
		t.Error("expected at least one event from the accepted transition")
	}
}

// --- stale identity, wrong parent, wrong generation, wrong attempt: all
// rejected before Transition ever sees them ---

func TestMapToCommandRejectsStaleSessionIdentity(t *testing.T) {
	state, session := driveToCreated(t)
	request := fixtureInterpretationRequest(t, session)
	request.SessionDigestSHA256 = zeroDigest
	candidate := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	result := fixtureInterpretationResult(t, request.RequestDigestSHA256, candidate)

	if _, err := MapToCommand(state, request, result, ""); err == nil {
		t.Fatal("expected a stale session identity to be rejected")
	}
}

func TestMapToCommandRejectsStaleRepositoryIdentity(t *testing.T) {
	state, session := driveToCreated(t)
	request := fixtureInterpretationRequest(t, session)
	request.BaseRevision = "some-other-revision"
	candidate := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	result := fixtureInterpretationResult(t, request.RequestDigestSHA256, candidate)

	if _, err := MapToCommand(state, request, result, ""); err == nil {
		t.Fatal("expected a stale repository/base-revision identity to be rejected")
	}
}

func TestMapToCommandRejectsWrongParent(t *testing.T) {
	t.Run("interpretation", func(t *testing.T) {
		state, session := driveToCreated(t)
		request := fixtureInterpretationRequest(t, session)
		request.ParentArtifactDigestSHA256 = zeroDigest
		candidate := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
		result := fixtureInterpretationResult(t, request.RequestDigestSHA256, candidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected a wrong parent-artifact digest to be rejected")
		}
	})
	t.Run("planning", func(t *testing.T) {
		state, session, interp := driveToPlanning(t)
		request := fixturePlanningRequest(t, session, interp)
		request.ParentArtifactDigestSHA256 = zeroDigest
		candidate := fixtureSynthesisPlan(t, interp.InterpretationDigestSHA256)
		result := fixturePlanningResult(t, request.RequestDigestSHA256, candidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected a wrong parent-artifact digest to be rejected")
		}
	})
	t.Run("generation", func(t *testing.T) {
		state, session, _, plan := driveToAttempting(t)
		request := fixtureGenerationRequest(t, session, plan)
		request.ParentArtifactDigestSHA256 = zeroDigest
		candidate := fixtureSynthesisAttempt(t, plan.PlanDigestSHA256, plan.PlanGeneration)
		result := fixtureGenerationResult(t, request.RequestDigestSHA256, candidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected a wrong parent-artifact digest to be rejected")
		}
	})
	t.Run("evaluation-observation", func(t *testing.T) {
		state, session, _, _, attempt := driveToEvaluating(t)
		request := fixtureEvaluationObservationRequest(t, session, attempt)
		request.ParentArtifactDigestSHA256 = zeroDigest
		candidate := fixtureSynthesisEvaluation(t, attempt.AttemptDigestSHA256)
		result := fixtureEvaluationObservationResult(t, request.RequestDigestSHA256, candidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected a wrong parent-artifact digest to be rejected")
		}
	})
}

func TestMapToCommandRejectsWrongGeneration(t *testing.T) {
	t.Run("planning", func(t *testing.T) {
		state, session, interp := driveToPlanning(t)
		request := fixturePlanningRequest(t, session, interp)
		wrong := *request.ExpectedPlanGeneration + 1
		request.ExpectedPlanGeneration = &wrong
		candidate := fixtureSynthesisPlan(t, interp.InterpretationDigestSHA256)
		result := fixturePlanningResult(t, request.RequestDigestSHA256, candidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected a wrong expected plan generation to be rejected")
		}
	})
	t.Run("generation", func(t *testing.T) {
		state, session, _, plan := driveToAttempting(t)
		request := fixtureGenerationRequest(t, session, plan)
		wrong := *request.ExpectedPlanGeneration + 1
		request.ExpectedPlanGeneration = &wrong
		candidate := fixtureSynthesisAttempt(t, plan.PlanDigestSHA256, plan.PlanGeneration)
		result := fixtureGenerationResult(t, request.RequestDigestSHA256, candidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected a wrong plan generation reference to be rejected")
		}
	})
	t.Run("evaluation-observation", func(t *testing.T) {
		state, session, _, _, attempt := driveToEvaluating(t)
		request := fixtureEvaluationObservationRequest(t, session, attempt)
		wrong := *request.ExpectedPlanGeneration + 1
		request.ExpectedPlanGeneration = &wrong
		candidate := fixtureSynthesisEvaluation(t, attempt.AttemptDigestSHA256)
		result := fixtureEvaluationObservationResult(t, request.RequestDigestSHA256, candidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected a wrong plan generation reference to be rejected")
		}
	})
}

func TestMapToCommandRejectsWrongAttempt(t *testing.T) {
	t.Run("generation", func(t *testing.T) {
		state, session, _, plan := driveToAttempting(t)
		request := fixtureGenerationRequest(t, session, plan)
		wrong := *request.ExpectedAttemptNumber + 1
		request.ExpectedAttemptNumber = &wrong
		candidate := fixtureSynthesisAttempt(t, plan.PlanDigestSHA256, plan.PlanGeneration)
		result := fixtureGenerationResult(t, request.RequestDigestSHA256, candidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected a wrong expected attempt number to be rejected")
		}
	})
	t.Run("evaluation-observation", func(t *testing.T) {
		state, session, _, _, attempt := driveToEvaluating(t)
		request := fixtureEvaluationObservationRequest(t, session, attempt)
		wrong := *request.ExpectedAttemptNumber + 1
		request.ExpectedAttemptNumber = &wrong
		candidate := fixtureSynthesisEvaluation(t, attempt.AttemptDigestSHA256)
		result := fixtureEvaluationObservationResult(t, request.RequestDigestSHA256, candidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected a wrong attempt-number reference to be rejected")
		}
	})
}

// --- basic preconditions ---

func TestMapToCommandRejectsNonCompletedResult(t *testing.T) {
	state, session := driveToCreated(t)
	request := fixtureInterpretationRequest(t, session)
	result := fixtureUnavailableResult(t, request.RequestDigestSHA256, OperationInterpretation)

	if _, err := MapToCommand(state, request, result, ""); err == nil {
		t.Fatal("expected a non-completed result to be rejected")
	}
}

func TestMapToCommandRejectsMismatchedResultReference(t *testing.T) {
	state, session := driveToCreated(t)
	request := fixtureInterpretationRequest(t, session)
	candidate := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	result := fixtureInterpretationResult(t, zeroDigest, candidate) // does not reference request's real digest

	if _, err := MapToCommand(state, request, result, ""); err == nil {
		t.Fatal("expected a result that does not reference the given request to be rejected")
	}
}

// --- non-mutation and determinism ---

func TestMapToCommandDoesNotMutateState(t *testing.T) {
	state, session := driveToCreated(t)
	before := state
	request := fixtureInterpretationRequest(t, session)
	candidate := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	result := fixtureInterpretationResult(t, request.RequestDigestSHA256, candidate)

	if _, err := MapToCommand(state, request, result, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(state, before) {
		t.Error("MapToCommand mutated its SessionState argument")
	}
}

func TestMapToCommandIsDeterministic(t *testing.T) {
	state, session := driveToCreated(t)
	request := fixtureInterpretationRequest(t, session)
	candidate := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	result := fixtureInterpretationResult(t, request.RequestDigestSHA256, candidate)

	cmd1, err1 := MapToCommand(state, request, result, "2026-01-01T00:00:00Z")
	cmd2, err2 := MapToCommand(state, request, result, "2026-01-01T00:00:00Z")
	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected errors: %v, %v", err1, err2)
	}
	if !reflect.DeepEqual(cmd1, cmd2) {
		t.Error("MapToCommand returned different commands for identical inputs")
	}
}

// --- wrong phase: the operation must be legal for the CURRENT phase ---

// TestMapToCommandRejectsWrongPhase isolates the phase check specifically:
// each case starts from a state that is otherwise CORRECT for its
// operation (real parent digest, real generation/attempt expectations --
// exactly what driveToX already produces for that operation), then mutates
// ONLY state.Phase to something else. If phase were not checked, every one
// of these would otherwise map successfully.
func TestMapToCommandRejectsWrongPhase(t *testing.T) {
	t.Run("interpretation from a non-Created phase", func(t *testing.T) {
		state, session := driveToCreated(t)
		state.Phase = synthesis.PhasePlanning // otherwise-correct Created state, wrong phase only
		request := fixtureInterpretationRequest(t, session)
		candidate := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
		result := fixtureInterpretationResult(t, request.RequestDigestSHA256, candidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected interpretation to be rejected outside PhaseCreated")
		}
	})
	t.Run("planning from a non-Planning phase", func(t *testing.T) {
		state, session, interp := driveToPlanning(t)
		state.Phase = synthesis.PhaseCreated // otherwise-correct Planning state, wrong phase only
		request := fixturePlanningRequest(t, session, interp)
		candidate := fixtureSynthesisPlan(t, interp.InterpretationDigestSHA256)
		result := fixturePlanningResult(t, request.RequestDigestSHA256, candidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected planning to be rejected outside PhasePlanning")
		}
	})
	t.Run("generation from a non-Attempting phase", func(t *testing.T) {
		state, session, _, plan := driveToAttempting(t)
		state.Phase = synthesis.PhasePlanning // otherwise-correct Attempting state, wrong phase only
		request := fixtureGenerationRequest(t, session, plan)
		candidate := fixtureSynthesisAttempt(t, plan.PlanDigestSHA256, plan.PlanGeneration)
		result := fixtureGenerationResult(t, request.RequestDigestSHA256, candidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected generation to be rejected outside PhaseAttempting")
		}
	})
	t.Run("evaluation-observation from a non-Evaluating phase", func(t *testing.T) {
		state, session, _, _, attempt := driveToEvaluating(t)
		state.Phase = synthesis.PhaseAttempting // otherwise-correct Evaluating state, wrong phase only
		request := fixtureEvaluationObservationRequest(t, session, attempt)
		candidate := fixtureSynthesisEvaluation(t, attempt.AttemptDigestSHA256)
		result := fixtureEvaluationObservationResult(t, request.RequestDigestSHA256, candidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected evaluation-observation to be rejected outside PhaseEvaluating")
		}
	})
}

// --- contradictory embedded parent: ParentArtifactDigestSHA256 alone is
// not enough -- the embedded artifact itself must independently agree ---

func TestMapToCommandRejectsEmbeddedParentWithWrongDeclaredDigest(t *testing.T) {
	// The embedded artifact's own self-declared digest field does not match
	// its actual content -- declared != computed, isolated from whether
	// ParentArtifactDigestSHA256 itself looks right.
	t.Run("interpretation", func(t *testing.T) {
		state, session := driveToCreated(t)
		request := fixtureInterpretationRequest(t, session)
		tampered := session
		tampered.SessionDigestSHA256 = zeroDigest // self-declared digest now wrong
		request.InterpretationPayload = &tampered
		candidate := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
		result := fixtureInterpretationResult(t, request.RequestDigestSHA256, candidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected an embedded session with a self-declared digest that does not match its content to be rejected")
		}
	})
	t.Run("planning", func(t *testing.T) {
		state, session, interp := driveToPlanning(t)
		request := fixturePlanningRequest(t, session, interp)
		tampered := interp
		tampered.InterpretationDigestSHA256 = zeroDigest
		request.PlanningPayload = &tampered
		candidate := fixtureSynthesisPlan(t, interp.InterpretationDigestSHA256)
		result := fixturePlanningResult(t, request.RequestDigestSHA256, candidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected an embedded interpretation with a self-declared digest that does not match its content to be rejected")
		}
	})
	t.Run("generation", func(t *testing.T) {
		state, session, _, plan := driveToAttempting(t)
		request := fixtureGenerationRequest(t, session, plan)
		tampered := plan
		tampered.PlanDigestSHA256 = zeroDigest
		request.GenerationPayload = &tampered
		candidate := fixtureSynthesisAttempt(t, plan.PlanDigestSHA256, plan.PlanGeneration)
		result := fixtureGenerationResult(t, request.RequestDigestSHA256, candidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected an embedded plan with a self-declared digest that does not match its content to be rejected")
		}
	})
	t.Run("evaluation-observation", func(t *testing.T) {
		state, session, _, _, attempt := driveToEvaluating(t)
		request := fixtureEvaluationObservationRequest(t, session, attempt)
		tampered := attempt
		tampered.AttemptDigestSHA256 = zeroDigest
		request.EvaluationObservationPayload = &tampered
		candidate := fixtureSynthesisEvaluation(t, attempt.AttemptDigestSHA256)
		result := fixtureEvaluationObservationResult(t, request.RequestDigestSHA256, candidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected an embedded attempt with a self-declared digest that does not match its content to be rejected")
		}
	})
}

func TestMapToCommandRejectsEmbeddedParentThatDoesNotMatchRequestReference(t *testing.T) {
	// The exact scenario the review named: a freshly re-digested request
	// claims the current, correct parent digest while embedding a
	// DIFFERENT (internally self-consistent) artifact than the one that
	// digest actually belongs to.
	t.Run("interpretation", func(t *testing.T) {
		state, session := driveToCreated(t)
		request := fixtureInterpretationRequest(t, session)

		otherSession := session
		otherSession.Objective = "a completely different objective the provider never saw approved"
		otherDigest, err := synthesis.SessionDigest(otherSession)
		if err != nil {
			t.Fatal(err)
		}
		otherSession.SessionDigestSHA256 = otherDigest
		request.InterpretationPayload = &otherSession // self-consistent, but not what ParentArtifactDigestSHA256 (still session.SessionDigestSHA256) claims

		candidate := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
		result := fixtureInterpretationResult(t, request.RequestDigestSHA256, candidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected a request whose ParentArtifactDigestSHA256 does not match its own embedded (self-consistent) session to be rejected")
		}
	})
	t.Run("planning", func(t *testing.T) {
		state, session, interp := driveToPlanning(t)
		request := fixturePlanningRequest(t, session, interp)

		otherInterp := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
		otherInterp.InterpretationID = "interpretation.other.001"
		otherDigest, err := synthesis.InterpretationDigest(otherInterp)
		if err != nil {
			t.Fatal(err)
		}
		otherInterp.InterpretationDigestSHA256 = otherDigest
		request.PlanningPayload = &otherInterp

		candidate := fixtureSynthesisPlan(t, interp.InterpretationDigestSHA256)
		result := fixturePlanningResult(t, request.RequestDigestSHA256, candidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected a request whose ParentArtifactDigestSHA256 does not match its own embedded (self-consistent) interpretation to be rejected")
		}
	})
}

// --- stale candidate payload: the RESULT's candidate artifact must itself
// reference the current parent/generation/attempt, not just the request ---

func TestMapToCommandRejectsStaleCandidatePayload(t *testing.T) {
	t.Run("interpretation: candidate references the wrong session", func(t *testing.T) {
		state, session := driveToCreated(t)
		request := fixtureInterpretationRequest(t, session)
		staleCandidate := fixtureSynthesisInterpretation(t, zeroDigest) // self-consistent, wrong session reference
		result := fixtureInterpretationResult(t, request.RequestDigestSHA256, staleCandidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected a candidate interpretation referencing the wrong session to be rejected")
		}
	})
	t.Run("planning: candidate references the wrong interpretation", func(t *testing.T) {
		state, session, interp := driveToPlanning(t)
		request := fixturePlanningRequest(t, session, interp)
		staleCandidate := fixtureSynthesisPlan(t, zeroDigest) // self-consistent, wrong interpretation reference
		result := fixturePlanningResult(t, request.RequestDigestSHA256, staleCandidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected a candidate plan referencing the wrong interpretation to be rejected")
		}
	})
	t.Run("planning: candidate carries the wrong plan generation", func(t *testing.T) {
		state, session, interp := driveToPlanning(t)
		request := fixturePlanningRequest(t, session, interp)
		staleCandidate := fixtureSynthesisPlan(t, interp.InterpretationDigestSHA256)
		staleCandidate.PlanGeneration = state.ExpectedPlanGeneration + 1
		digest, err := synthesis.PlanDigest(staleCandidate)
		if err != nil {
			t.Fatal(err)
		}
		staleCandidate.PlanDigestSHA256 = digest
		result := fixturePlanningResult(t, request.RequestDigestSHA256, staleCandidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected a candidate plan carrying the wrong generation to be rejected")
		}
	})
	t.Run("generation: candidate references the wrong plan", func(t *testing.T) {
		state, session, _, plan := driveToAttempting(t)
		request := fixtureGenerationRequest(t, session, plan)
		staleCandidate := fixtureSynthesisAttempt(t, zeroDigest, plan.PlanGeneration) // self-consistent, wrong plan reference
		result := fixtureGenerationResult(t, request.RequestDigestSHA256, staleCandidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected a candidate attempt referencing the wrong plan to be rejected")
		}
	})
	t.Run("generation: candidate carries the wrong plan generation", func(t *testing.T) {
		state, session, _, plan := driveToAttempting(t)
		request := fixtureGenerationRequest(t, session, plan)
		staleCandidate := fixtureSynthesisAttempt(t, plan.PlanDigestSHA256, plan.PlanGeneration+1)
		result := fixtureGenerationResult(t, request.RequestDigestSHA256, staleCandidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected a candidate attempt carrying the wrong plan generation to be rejected")
		}
	})
	t.Run("generation: candidate carries the wrong attempt number", func(t *testing.T) {
		state, session, _, plan := driveToAttempting(t)
		request := fixtureGenerationRequest(t, session, plan)
		staleCandidate := fixtureSynthesisAttempt(t, plan.PlanDigestSHA256, plan.PlanGeneration)
		staleCandidate.AttemptNumber = state.ExpectedAttemptNumber + 1
		digest, err := synthesis.AttemptDigest(staleCandidate)
		if err != nil {
			t.Fatal(err)
		}
		staleCandidate.AttemptDigestSHA256 = digest
		result := fixtureGenerationResult(t, request.RequestDigestSHA256, staleCandidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected a candidate attempt carrying the wrong attempt number to be rejected")
		}
	})
	t.Run("evaluation-observation: candidate references the wrong attempt", func(t *testing.T) {
		state, session, _, _, attempt := driveToEvaluating(t)
		request := fixtureEvaluationObservationRequest(t, session, attempt)
		staleCandidate := fixtureSynthesisEvaluation(t, zeroDigest) // self-consistent, wrong attempt reference
		result := fixtureEvaluationObservationResult(t, request.RequestDigestSHA256, staleCandidate)

		if _, err := MapToCommand(state, request, result, ""); err == nil {
			t.Fatal("expected a candidate evaluation referencing the wrong attempt to be rejected")
		}
	})
}

// --- validation is not durable across the boundary: request/result are
// revalidated from scratch, and the returned command is detached ---

// TestMapToCommandRejectsRequestMutatedAfterValidation proves the request
// itself is re-validated, not merely trusted because it "was" valid when
// Run produced it: mutating any digest-covered field without recomputing
// RequestDigestSHA256 -- exactly what a caller holding request after Run
// returns could do -- is rejected.
func TestMapToCommandRejectsRequestMutatedAfterValidation(t *testing.T) {
	state, session, interp := driveToPlanning(t)
	request := fixturePlanningRequest(t, session, interp)
	candidate := fixtureSynthesisPlan(t, interp.InterpretationDigestSHA256)
	result := fixturePlanningResult(t, request.RequestDigestSHA256, candidate)

	// request is fully valid here. Mutate a digest-covered field without
	// recomputing RequestDigestSHA256.
	request.DeadlineAt = "2099-12-31T23:59:59Z"

	if _, err := MapToCommand(state, request, result, ""); err == nil {
		t.Fatal("expected a request mutated after its digest was computed to be rejected")
	}
}

// TestMapToCommandRejectsResultWhoseOuterDigestIsStaleAfterCandidateMutation
// proves that recomputing only the candidate's own digest field is not
// enough: mutating the embedded candidate and updating just its own digest
// and Result.PayloadDigestSHA256 to match, while leaving the OUTER
// Result.ResultDigestSHA256 stale (as a caller mutating a candidate in
// place after Run already digested the whole Result would produce), is
// rejected because the outer digest no longer matches the Result's actual
// content.
func TestMapToCommandRejectsResultWhoseOuterDigestIsStaleAfterCandidateMutation(t *testing.T) {
	state, session, interp := driveToPlanning(t)
	request := fixturePlanningRequest(t, session, interp)
	candidate := fixtureSynthesisPlan(t, interp.InterpretationDigestSHA256)
	result := fixturePlanningResult(t, request.RequestDigestSHA256, candidate)

	tampered := *result.PlanningPayload
	tampered.Risks = append(tampered.Risks, "a risk the provider never actually reported")
	newPlanDigest, err := synthesis.PlanDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	tampered.PlanDigestSHA256 = newPlanDigest
	result.PlanningPayload = &tampered
	result.PayloadDigestSHA256 = &newPlanDigest
	// result.ResultDigestSHA256 intentionally left stale -- still the
	// digest of the ORIGINAL (untampered) result content.

	if _, err := MapToCommand(state, request, result, ""); err == nil {
		t.Fatal("expected a result whose outer digest is stale relative to its mutated candidate to be rejected")
	}
}

// TestMapToCommandReturnedCommandIsDetachedFromOriginalResult proves the
// returned command's embedded artifact is an independent deep copy: after
// a successful mapping, mutating the ORIGINAL result's nested slice-backed
// field must not alter the already-returned command. A plain `*ptr`
// dereference would have only shallow-copied Plan, leaving Steps aliasing
// the same backing array.
func TestMapToCommandReturnedCommandIsDetachedFromOriginalResult(t *testing.T) {
	state, session, interp := driveToPlanning(t)
	request := fixturePlanningRequest(t, session, interp)
	candidate := fixtureSynthesisPlan(t, interp.InterpretationDigestSHA256)
	result := fixturePlanningResult(t, request.RequestDigestSHA256, candidate)

	cmd, err := MapToCommand(state, request, result, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := cmd.(synthesis.RecordPlanCommand)
	if !ok {
		t.Fatalf("command type = %T, want synthesis.RecordPlanCommand", cmd)
	}
	if len(got.Plan.Steps) == 0 {
		t.Fatal("setup: expected the candidate plan to carry at least one step")
	}
	original := got.Plan.Steps[0].Description

	// Mutate the ORIGINAL result's nested slice-backed field AFTER mapping
	// already happened.
	result.PlanningPayload.Steps[0].Description = "a description the provider never actually wrote"

	if got.Plan.Steps[0].Description != original {
		t.Errorf("mutating the original Result's nested slice altered the already-returned command: got %q, want the original %q -- MapToCommand did not deep-copy the candidate", got.Plan.Steps[0].Description, original)
	}
}

// TestMapToCommandRejectsEveryNonCompletedOutcome proves each of the five
// non-completed terminal outcomes is rejected at the mapping boundary, not
// merely the one representative case (unavailable) TestMapToCommandRejects
// NonCompletedResult already covers. MapToCommand's rejection is outcome-
// agnostic (any TerminalOutcome != OutcomeCompleted), so this is the same
// code path proven exhaustively rather than five different code paths.
func TestMapToCommandRejectsEveryNonCompletedOutcome(t *testing.T) {
	state, session, interp := driveToPlanning(t)
	request := fixturePlanningRequest(t, session, interp)

	outcomes := []TerminalOutcome{
		OutcomeUnavailable,
		OutcomeTimedOut,
		OutcomeCancelled,
		OutcomeInvalidOutput,
		OutcomeUnsupportedCapability,
	}
	for _, outcome := range outcomes {
		t.Run(string(outcome), func(t *testing.T) {
			result := Result{
				SchemaVersion:       ResultSchemaVersion,
				RequestDigestSHA256: request.RequestDigestSHA256,
				Operation:           request.Operation,
				TerminalOutcome:     outcome,
				Detail:              "synthesized for adversarial test",
			}
			digest, err := ResultDigest(result)
			if err != nil {
				t.Fatal(err)
			}
			result.ResultDigestSHA256 = digest

			cmd, err := MapToCommand(state, request, result, "")
			if err == nil {
				t.Fatalf("expected outcome %s to be rejected, got command %T", outcome, cmd)
			}
			if cmd != nil {
				t.Errorf("expected a nil command on rejection, got %T", cmd)
			}
		})
	}
}

// --- a capability claim grants no authority ---

// TestCapabilityClaimGrantsNoAuthority proves an honest capability claim,
// and even a fully successful Run() built on it, grant no lasting
// authority: a provider claims (truthfully) that it supports planning, and
// Run accepts that claim and lets Execute proceed, producing a Result that
// was valid relative to the state at the time. By the time MapToCommand is
// called, though, the session has moved on (state.InterpretationDigestSHA256
// now points at a DIFFERENT interpretation than the one the request/result
// were built against) -- neither the capability claim nor Run's own
// success carries any weight here. MapToCommand's own checks, run fresh
// against CURRENT state, are what decide, and they reject the now-stale
// result before any command is constructed. This also demonstrates
// structurally that a capability claim cannot "create an O1 command by
// itself": Run's return type has no Command in it at all -- only
// MapToCommand, a separate and deliberate call, can produce one, and
// nothing about Describe/Capabilities/Execute grants routing, mutation,
// admission, or transition authority anywhere in this package.
func TestCapabilityClaimGrantsNoAuthority(t *testing.T) {
	session := fixtureSynthesisSession(t)
	interp1 := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)

	capabilities := Capabilities{
		SchemaVersion: CapabilitiesSchemaVersion,
		ProviderObservation: synthesis.ProviderObservation{
			ProviderID:   "provider.honest",
			ProviderKind: "test-double",
			ObservedAt:   "2026-01-01T00:00:00Z",
		},
		SupportedOperations: []Operation{OperationPlanning},
	}
	capDigest, err := CapabilitiesDigest(capabilities)
	if err != nil {
		t.Fatal(err)
	}
	capabilities.CapabilitiesDigestSHA256 = capDigest

	request := withFreshDeadline(t, fixturePlanningRequest(t, session, interp1), 5*time.Second)
	plan := fixtureSynthesisPlan(t, interp1.InterpretationDigestSHA256)

	provider := &fakeProvider{
		capabilities: capabilities,
		executeFn: func(ctx context.Context, req Request, obs Observer) (Result, error) {
			return fixturePlanningResult(t, req.RequestDigestSHA256, plan), nil
		},
	}

	result, _, _, err := Run(context.Background(), provider, request, fixedClock(time.Now()))
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.TerminalOutcome != OutcomeCompleted {
		t.Fatalf("setup: expected the honest capability claim and valid result to complete, got %s", result.TerminalOutcome)
	}
	if provider.calls() != 1 {
		t.Fatalf("setup: expected Execute to be called exactly once, got %d", provider.calls())
	}

	// The session has since moved on: a genuinely different interpretation
	// (interp2) is now current, e.g. after a replan. request/result still
	// reference interp1 -- correctly rejected by MapToCommand's fresh
	// parent check, regardless of the capability claim or Run's success.
	freshCreated, err := synthesis.NewSessionState(session)
	if err != nil {
		t.Fatal(err)
	}
	interp2 := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	interp2.InterpretationID = "interpretation.advanced.001"
	interp2Digest, err := synthesis.InterpretationDigest(interp2)
	if err != nil {
		t.Fatal(err)
	}
	interp2.InterpretationDigestSHA256 = interp2Digest
	advancedState, _, err := synthesis.Transition(freshCreated, testCertifiedInterpretationCommand(t, freshCreated, interp2))
	if err != nil {
		t.Fatal(err)
	}
	if advancedState.InterpretationDigestSHA256 == interp1.InterpretationDigestSHA256 {
		t.Fatal("setup: expected the advanced state's interpretation to differ from interp1")
	}

	if cmd, err := MapToCommand(advancedState, request, result, ""); err == nil {
		t.Fatalf("expected a stale result to be rejected regardless of the provider's honest capability claim and successful execution, got command %T", cmd)
	}
}
