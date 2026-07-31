// SPDX-License-Identifier: AGPL-3.0-only

// request_conditional_test.go proves the closed, operation-discriminated
// conditionals introduced by the architect review of PR #124 head
// 1d86a2f06c02da1782476180ac5ed7e259d10763:
//
//   - Request's per-operation identity binding: a provider cannot choose or
//     alter which plan generation/attempt number a request is bound to.
//   - Request's per-operation payload binding: exactly the payload matching
//     Operation is populated, embedding the parent O1 artifact directly.
//   - Result's per-operation, per-outcome payload binding: exactly the
//     payload matching Operation is populated, and only on a completed
//     outcome, with payload_digest_sha256 tracking presence.
//   - Receipt's result_digest_sha256 (always required) versus
//     payload_digest_sha256 (completed-only) binding.
//
// (docs/design/provider-neutral-execution-port-o2.md hard laws 4-5.)
package providerport

import (
	"encoding/json"
	"testing"

	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// --- Request: per-operation identity binding (plan generation / attempt
// number), payload always correct so this dimension is tested in isolation
// ---

func TestRequestConditionalIdentityBindingPerOperation(t *testing.T) {
	session := fixtureSynthesisSession(t)
	interpretation := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	plan := fixtureSynthesisPlan(t, interpretation.InterpretationDigestSHA256)
	attempt := fixtureSynthesisAttempt(t, plan.PlanDigestSHA256, plan.PlanGeneration)

	type namedRequest struct {
		name string
		req  Request
	}

	valid := []namedRequest{
		{"interpretation: both nil", fixtureInterpretationRequest(t, session)},
		{"planning: generation only", fixturePlanningRequest(t, session, interpretation)},
		{"generation: both set", fixtureGenerationRequest(t, session, plan)},
		{"evaluation-observation: both set", fixtureEvaluationObservationRequest(t, session, attempt)},
	}
	for _, c := range valid {
		t.Run("valid/"+c.name, func(t *testing.T) {
			data, err := json.Marshal(c.req)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateRequestSchema(data); err != nil {
				t.Fatalf("expected a valid per-operation identity binding to pass schema validation: %v\ndocument: %s", err, data)
			}
		})
	}

	mutateNumbers := func(base Request, planGeneration, attemptNumber *int) Request {
		r := base
		r.ExpectedPlanGeneration = planGeneration
		r.ExpectedAttemptNumber = attemptNumber
		return r
	}
	invalid := []namedRequest{
		{"interpretation with a plan generation", mutateNumbers(valid[0].req, intPtr(1), nil)},
		{"interpretation with an attempt number", mutateNumbers(valid[0].req, nil, intPtr(1))},
		{"planning without a plan generation", mutateNumbers(valid[1].req, nil, nil)},
		{"planning with an attempt number", mutateNumbers(valid[1].req, intPtr(1), intPtr(1))},
		{"generation without a plan generation", mutateNumbers(valid[2].req, nil, intPtr(1))},
		{"generation without an attempt number", mutateNumbers(valid[2].req, intPtr(1), nil)},
		{"evaluation-observation without a plan generation", mutateNumbers(valid[3].req, nil, intPtr(1))},
		{"evaluation-observation without an attempt number", mutateNumbers(valid[3].req, intPtr(1), nil)},
	}
	for _, c := range invalid {
		t.Run("invalid/"+c.name, func(t *testing.T) {
			data, err := json.Marshal(c.req)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateRequestSchema(data); err == nil {
				t.Fatal("expected an invalid per-operation identity binding to fail schema validation")
			}
		})
	}
}

// --- Request: per-operation payload binding (operation/payload mismatch)
// ---

func TestRequestConditionalPayloadPerOperation(t *testing.T) {
	session := fixtureSynthesisSession(t)
	interpretation := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	plan := fixtureSynthesisPlan(t, interpretation.InterpretationDigestSHA256)
	attempt := fixtureSynthesisAttempt(t, plan.PlanDigestSHA256, plan.PlanGeneration)

	base := []Request{
		fixtureInterpretationRequest(t, session),
		fixturePlanningRequest(t, session, interpretation),
		fixtureGenerationRequest(t, session, plan),
		fixtureEvaluationObservationRequest(t, session, attempt),
	}

	cases := []struct {
		name string
		req  Request
	}{
		{"interpretation with a planning payload instead", withRequestPayloads(base[0], nil, &interpretation, nil, nil)},
		{"interpretation with no payload at all", withRequestPayloads(base[0], nil, nil, nil, nil)},
		{"interpretation with an extra second payload also set", withRequestPayloads(base[0], base[0].InterpretationPayload, &interpretation, nil, nil)},
		{"planning with a generation payload instead", withRequestPayloads(base[1], nil, nil, &plan, nil)},
		{"planning with no payload at all", withRequestPayloads(base[1], nil, nil, nil, nil)},
		{"generation with an evaluation-observation payload instead", withRequestPayloads(base[2], nil, nil, nil, &attempt)},
		{"generation with no payload at all", withRequestPayloads(base[2], nil, nil, nil, nil)},
		{"evaluation-observation with an interpretation payload instead", withRequestPayloads(base[3], &session, nil, nil, nil)},
		{"evaluation-observation with no payload at all", withRequestPayloads(base[3], nil, nil, nil, nil)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data, err := json.Marshal(c.req)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateRequestSchema(data); err == nil {
				t.Fatal("expected an operation/payload mismatch to fail schema validation")
			}
		})
	}
}

func withRequestPayloads(base Request, interp *synthesis.Session, planning *synthesis.Interpretation, generation *synthesis.Plan, evalObs *synthesis.Attempt) Request {
	r := base
	r.InterpretationPayload = interp
	r.PlanningPayload = planning
	r.GenerationPayload = generation
	r.EvaluationObservationPayload = evalObs
	return r
}

// --- Result: per-operation, per-outcome payload binding (operation/payload
// mismatch) ---

func TestResultConditionalPayloadPerOperation(t *testing.T) {
	session := fixtureSynthesisSession(t)
	interpretation := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	plan := fixtureSynthesisPlan(t, interpretation.InterpretationDigestSHA256)
	attempt := fixtureSynthesisAttempt(t, plan.PlanDigestSHA256, plan.PlanGeneration)
	evaluation := fixtureSynthesisEvaluation(t, attempt.AttemptDigestSHA256)

	completed := []Result{
		fixtureInterpretationResult(t, zeroDigest, interpretation),
		fixturePlanningResult(t, zeroDigest, plan),
		fixtureGenerationResult(t, zeroDigest, attempt),
		fixtureEvaluationObservationResult(t, zeroDigest, evaluation),
	}
	planDigest := plan.PlanDigestSHA256
	attemptDigest := attempt.AttemptDigestSHA256

	cases := []struct {
		name string
		res  Result
	}{
		{"interpretation completed with a planning payload instead", func() Result {
			r := completed[0]
			r.InterpretationPayload = nil
			r.PlanningPayload = &plan
			return r
		}()},
		{"interpretation completed with no payload at all", func() Result {
			r := completed[0]
			r.InterpretationPayload = nil
			r.PayloadDigestSHA256 = nil
			return r
		}()},
		{"planning completed with a generation payload instead", func() Result {
			r := completed[1]
			r.PlanningPayload = nil
			r.GenerationPayload = &attempt
			return r
		}()},
		{"generation completed with an evaluation-observation payload instead", func() Result {
			r := completed[2]
			r.GenerationPayload = nil
			r.EvaluationObservationPayload = &evaluation
			return r
		}()},
		{"evaluation-observation completed with an interpretation payload instead", func() Result {
			r := completed[3]
			r.EvaluationObservationPayload = nil
			r.InterpretationPayload = &interpretation
			return r
		}()},
		{"completed but payload_digest_sha256 is null despite a real payload", func() Result {
			r := completed[1]
			r.PayloadDigestSHA256 = nil
			return r
		}()},
		{"unavailable outcome but still carries a payload", func() Result {
			r := fixtureUnavailableResult(t, zeroDigest, OperationPlanning)
			r.PlanningPayload = &plan
			r.PayloadDigestSHA256 = &planDigest
			return r
		}()},
		{"unavailable outcome but still carries a payload_digest_sha256", func() Result {
			r := fixtureUnavailableResult(t, zeroDigest, OperationGeneration)
			r.PayloadDigestSHA256 = &attemptDigest
			return r
		}()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data, err := json.Marshal(c.res)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateResultSchema(data); err == nil {
				t.Fatal("expected an operation/payload/outcome mismatch to fail schema validation")
			}
		})
	}
}

// --- Receipt: result_digest_sha256 (always required) vs.
// payload_digest_sha256 (completed-only) ---

func TestReceiptPayloadDigestMatchesTerminalOutcome(t *testing.T) {
	requestDigest := zeroDigest
	capabilitiesDigest := zeroDigest
	resultDigest := zeroDigest
	payloadDigest := zeroDigest

	t.Run("completed requires a non-null payload digest", func(t *testing.T) {
		r := fixtureReceiptCompleted(t, requestDigest, capabilitiesDigest, resultDigest, payloadDigest, zeroDigest)
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateReceiptSchema(data); err != nil {
			t.Fatalf("expected a completed receipt with a payload digest to pass schema validation: %v", err)
		}
	})

	t.Run("completed with a null payload digest is rejected", func(t *testing.T) {
		r := fixtureReceiptCompleted(t, requestDigest, capabilitiesDigest, resultDigest, payloadDigest, zeroDigest)
		r.PayloadDigestSHA256 = nil
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateReceiptSchema(data); err == nil {
			t.Fatal("expected a completed receipt with a null payload digest to fail schema validation")
		}
	})

	t.Run("non-completed requires a null payload digest but keeps a non-null result digest", func(t *testing.T) {
		r := fixtureReceiptUnavailable(t, requestDigest, capabilitiesDigest, resultDigest)
		if r.ResultDigestSHA256 == "" {
			t.Fatal("setup: expected fixtureReceiptUnavailable to carry a non-empty ResultDigestSHA256")
		}
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateReceiptSchema(data); err != nil {
			t.Fatalf("expected an unavailable receipt (null payload digest, non-null result digest) to pass schema validation: %v", err)
		}
	})

	t.Run("non-completed with a non-null payload digest is rejected", func(t *testing.T) {
		r := fixtureReceiptUnavailable(t, requestDigest, capabilitiesDigest, resultDigest)
		r.PayloadDigestSHA256 = &payloadDigest
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateReceiptSchema(data); err == nil {
			t.Fatal("expected an unavailable receipt with a non-null payload digest to fail schema validation")
		}
	})

	t.Run("result_digest_sha256 always marshals non-empty, even on a typed failure", func(t *testing.T) {
		r := fixtureReceiptUnavailable(t, requestDigest, capabilitiesDigest, resultDigest)
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		if s, ok := raw["result_digest_sha256"].(string); !ok || s == "" {
			t.Error("a non-completed receipt must still marshal a non-empty result_digest_sha256 -- typed failures must not lose their Result-envelope evidence")
		}
	})
}
