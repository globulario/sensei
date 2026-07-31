// SPDX-License-Identifier: AGPL-3.0-only

// smuggling_test.go makes the construction-only smuggling protections this
// package already relies on structurally explicit and named, per the O2
// closure audit on PR #124 head c89d0af6aaec913b97a79eefa9e44b617bcd0b35:
// nothing a provider controls can smuggle an O1 budget field through a
// closed schema, and nothing MapToCommand constructs can be any
// synthesis.Command other than the exact one each Operation permits.
package providerport

import (
	"encoding/json"
	"testing"

	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// injectExtraField marshals doc, adds key/value as an extra top-level JSON
// property, and returns the resulting bytes -- for proving a closed
// (additionalProperties:false) schema rejects fields it does not declare.
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

// TestClosedSchemasRejectUnknownBudgetFields proves no O2 document can
// carry an O1 budget field at all -- not merely that MapToCommand ignores
// one, but that the closed (additionalProperties:false) Request/Result
// schemas reject the field's mere presence outright.
func TestClosedSchemasRejectUnknownBudgetFields(t *testing.T) {
	budgetFields := []string{
		"retry_budget",
		"replan_budget",
		"remaining_retry_budget",
		"remaining_replan_budget",
	}

	session := fixtureSynthesisSession(t)
	interp := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	request := fixturePlanningRequest(t, session, interp)
	plan := fixtureSynthesisPlan(t, interp.InterpretationDigestSHA256)
	result := fixturePlanningResult(t, request.RequestDigestSHA256, plan)

	for _, field := range budgetFields {
		t.Run("request/"+field, func(t *testing.T) {
			data := injectExtraField(t, request, field, 999)
			if err := ValidateRequestSchema(data); err == nil {
				t.Fatalf("expected a request carrying an unknown %q field to fail schema validation", field)
			}
		})
		t.Run("result/"+field, func(t *testing.T) {
			data := injectExtraField(t, result, field, 999)
			if err := ValidateResultSchema(data); err == nil {
				t.Fatalf("expected a result carrying an unknown %q field to fail schema validation", field)
			}
		})
	}
}

// TestMapToCommandOnlyProducesThePermittedCommandPerOperation is the
// consolidated command-smuggling proof: provider-controlled Result content
// can never cause MapToCommand to construct anything other than the exact
// command each Operation permits -- never AbortCommand, ResumeCommand,
// StartPlanningCommand, StartAttemptCommand, EvaluatorUnavailableCommand,
// or any other member of synthesis's closed Command set.
// MapToCommand's own switch statement is the ONLY code path that
// constructs a synthesis.Command; it is hardcoded per Operation with no
// data-driven or reflective construction, so no provider-supplied field
// can select a different command type. The exhaustive type switch below
// (default case fails the test) proves this for every successful mapping,
// not merely that the "happy" type assertion succeeds.
func TestMapToCommandOnlyProducesThePermittedCommandPerOperation(t *testing.T) {
	assertOnlyPermittedCommand := func(t *testing.T, cmd synthesis.Command) {
		t.Helper()
		switch cmd.(type) {
		case synthesis.RecordInterpretationCommand,
			synthesis.RecordPlanCommand,
			synthesis.RecordAttemptCommand,
			synthesis.RecordEvaluationCommand:
			// exactly one of the four commands an operation may map to.
		default:
			t.Fatalf("MapToCommand produced %T, which is not one of the four commands any operation may map to", cmd)
		}
	}

	t.Run("interpretation maps only to RecordInterpretationCommand", func(t *testing.T) {
		state, session := driveToCreated(t)
		request := fixtureInterpretationRequest(t, session)
		candidate := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
		result := fixtureInterpretationResult(t, request.RequestDigestSHA256, candidate)

		cmd, err := MapToCommand(state, request, result, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertOnlyPermittedCommand(t, cmd)
		if _, ok := cmd.(synthesis.RecordInterpretationCommand); !ok {
			t.Fatalf("command type = %T, want synthesis.RecordInterpretationCommand", cmd)
		}
	})
	t.Run("planning maps only to RecordPlanCommand", func(t *testing.T) {
		state, session, interp := driveToPlanning(t)
		request := fixturePlanningRequest(t, session, interp)
		candidate := fixtureSynthesisPlan(t, interp.InterpretationDigestSHA256)
		result := fixturePlanningResult(t, request.RequestDigestSHA256, candidate)

		cmd, err := MapToCommand(state, request, result, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertOnlyPermittedCommand(t, cmd)
		if _, ok := cmd.(synthesis.RecordPlanCommand); !ok {
			t.Fatalf("command type = %T, want synthesis.RecordPlanCommand", cmd)
		}
	})
	t.Run("generation maps only to RecordAttemptCommand", func(t *testing.T) {
		state, session, _, plan := driveToAttempting(t)
		request := fixtureGenerationRequest(t, session, plan)
		candidate := fixtureSynthesisAttempt(t, plan.PlanDigestSHA256, plan.PlanGeneration)
		result := fixtureGenerationResult(t, request.RequestDigestSHA256, candidate)

		cmd, err := MapToCommand(state, request, result, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertOnlyPermittedCommand(t, cmd)
		if _, ok := cmd.(synthesis.RecordAttemptCommand); !ok {
			t.Fatalf("command type = %T, want synthesis.RecordAttemptCommand", cmd)
		}
	})
	t.Run("evaluation-observation maps only to RecordEvaluationCommand", func(t *testing.T) {
		state, session, _, _, attempt := driveToEvaluating(t)
		request := fixtureEvaluationObservationRequest(t, session, attempt)
		candidate := fixtureSynthesisEvaluation(t, attempt.AttemptDigestSHA256)
		result := fixtureEvaluationObservationResult(t, request.RequestDigestSHA256, candidate)

		cmd, err := MapToCommand(state, request, result, "2026-01-01T00:20:00Z")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertOnlyPermittedCommand(t, cmd)
		if _, ok := cmd.(synthesis.RecordEvaluationCommand); !ok {
			t.Fatalf("command type = %T, want synthesis.RecordEvaluationCommand", cmd)
		}
	})
}
