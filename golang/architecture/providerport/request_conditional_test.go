// SPDX-License-Identifier: AGPL-3.0-only

// request_conditional_test.go proves the Request schema's per-operation
// identity-binding conditional: a provider cannot choose or alter which
// plan generation/attempt number a request is bound to, so the schema
// enforces exactly which of ExpectedPlanGeneration/ExpectedAttemptNumber
// must be a positive integer versus null for each closed Operation
// (docs/design/provider-neutral-execution-port-o2.md hard law 4).
package providerport

import (
	"encoding/json"
	"testing"
)

func TestRequestConditionalIdentityBindingPerOperation(t *testing.T) {
	sessionDigest := zeroDigest
	parentDigest := zeroDigest

	valid := []struct {
		name           string
		op             Operation
		planGeneration *int
		attemptNumber  *int
	}{
		{"interpretation: both nil", OperationInterpretation, nil, nil},
		{"planning: generation only", OperationPlanning, intPtr(1), nil},
		{"generation: both set", OperationGeneration, intPtr(1), intPtr(1)},
		{"evaluation-observation: both set", OperationEvaluationObservation, intPtr(1), intPtr(1)},
	}
	for _, c := range valid {
		t.Run("valid/"+c.name, func(t *testing.T) {
			req := fixtureRequest(t, c.op, sessionDigest, parentDigest, c.planGeneration, c.attemptNumber)
			data, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateRequestSchema(data); err != nil {
				t.Fatalf("expected a valid per-operation identity binding to pass schema validation: %v\ndocument: %s", err, data)
			}
		})
	}

	invalid := []struct {
		name           string
		op             Operation
		planGeneration *int
		attemptNumber  *int
	}{
		{"interpretation with a plan generation", OperationInterpretation, intPtr(1), nil},
		{"interpretation with an attempt number", OperationInterpretation, nil, intPtr(1)},
		{"planning without a plan generation", OperationPlanning, nil, nil},
		{"planning with an attempt number", OperationPlanning, intPtr(1), intPtr(1)},
		{"generation without a plan generation", OperationGeneration, nil, intPtr(1)},
		{"generation without an attempt number", OperationGeneration, intPtr(1), nil},
		{"evaluation-observation without a plan generation", OperationEvaluationObservation, nil, intPtr(1)},
		{"evaluation-observation without an attempt number", OperationEvaluationObservation, intPtr(1), nil},
	}
	for _, c := range invalid {
		t.Run("invalid/"+c.name, func(t *testing.T) {
			req := fixtureRequest(t, c.op, sessionDigest, parentDigest, c.planGeneration, c.attemptNumber)
			data, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateRequestSchema(data); err == nil {
				t.Fatal("expected an invalid per-operation identity binding to fail schema validation")
			}
		})
	}
}

// TestResultTerminalOutcomeMatchesReceiptResponseDigestRule proves the
// Receipt schema's terminal_outcome/response_digest_sha256 conditional:
// response_digest_sha256 is required exactly when terminal_outcome is
// completed, and must be null otherwise.
func TestReceiptResponseDigestMatchesTerminalOutcome(t *testing.T) {
	requestDigest := zeroDigest
	capabilitiesDigest := zeroDigest
	responseDigest := zeroDigest

	t.Run("completed requires a non-null response digest", func(t *testing.T) {
		r := fixtureReceiptCompleted(t, requestDigest, capabilitiesDigest, responseDigest, zeroDigest)
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateReceiptSchema(data); err != nil {
			t.Fatalf("expected a completed receipt with a response digest to pass schema validation: %v", err)
		}
	})

	t.Run("completed with a null response digest is rejected", func(t *testing.T) {
		r := fixtureReceiptCompleted(t, requestDigest, capabilitiesDigest, responseDigest, zeroDigest)
		r.ResponseDigestSHA256 = nil
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateReceiptSchema(data); err == nil {
			t.Fatal("expected a completed receipt with a null response digest to fail schema validation")
		}
	})

	t.Run("non-completed requires a null response digest", func(t *testing.T) {
		r := fixtureReceiptUnavailable(t, requestDigest, capabilitiesDigest)
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateReceiptSchema(data); err != nil {
			t.Fatalf("expected an unavailable receipt with a null response digest to pass schema validation: %v", err)
		}
	})

	t.Run("non-completed with a non-null response digest is rejected", func(t *testing.T) {
		r := fixtureReceiptUnavailable(t, requestDigest, capabilitiesDigest)
		r.ResponseDigestSHA256 = &responseDigest
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateReceiptSchema(data); err == nil {
			t.Fatal("expected an unavailable receipt with a non-null response digest to fail schema validation")
		}
	})
}
