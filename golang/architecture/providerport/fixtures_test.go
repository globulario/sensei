// SPDX-License-Identifier: AGPL-3.0-only

package providerport

import (
	"encoding/json"
	"testing"

	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// --- fixture builders ---
//
// Each builder produces a document with its own digest field correctly
// computed, chained to its parent by the parent's real digest -- mirroring
// golang/architecture/synthesis's fixtures_test.go conventions.

func intPtr(v int) *int { return &v }

func fixtureCapabilities(t *testing.T) Capabilities {
	t.Helper()
	c := Capabilities{
		SchemaVersion: CapabilitiesSchemaVersion,
		ProviderObservation: synthesis.ProviderObservation{
			ProviderID:   "provider.fixture",
			ProviderKind: "test-double",
			ObservedAt:   "2026-01-01T00:00:00Z",
		},
		SupportedOperations: []Operation{
			OperationInterpretation,
			OperationPlanning,
			OperationGeneration,
			OperationEvaluationObservation,
		},
	}
	digest, err := CapabilitiesDigest(c)
	if err != nil {
		t.Fatalf("CapabilitiesDigest: %v", err)
	}
	c.CapabilitiesDigestSHA256 = digest
	return NormalizeCapabilities(c)
}

func fixtureRequest(t *testing.T, op Operation, sessionDigest, parentDigest string, planGeneration, attemptNumber *int) Request {
	t.Helper()
	r := Request{
		SchemaVersion:              RequestSchemaVersion,
		RequestID:                  "request.fixture." + string(op),
		Operation:                  op,
		SessionDigestSHA256:        sessionDigest,
		RepositoryDomain:           "github.com/example/repo",
		BaseRevision:               "abc123def456",
		ParentArtifactDigestSHA256: parentDigest,
		ExpectedPlanGeneration:     planGeneration,
		ExpectedAttemptNumber:      attemptNumber,
		DeadlineAt:                 "2026-01-01T00:05:00Z",
		MaxObservationCount:        100,
		MaxObservationBytes:        1 << 20,
	}
	digest, err := RequestDigest(r)
	if err != nil {
		t.Fatalf("RequestDigest: %v", err)
	}
	r.RequestDigestSHA256 = digest
	return NormalizeRequest(r)
}

// fixturePlanningRequest is the representative single-parent-digest fixture
// used throughout this package's non-conditional tests.
func fixturePlanningRequest(t *testing.T, sessionDigest, interpretationDigest string) Request {
	t.Helper()
	return fixtureRequest(t, OperationPlanning, sessionDigest, interpretationDigest, intPtr(1), nil)
}

func fixtureResult(t *testing.T, requestDigest string, op Operation, outcome TerminalOutcome, detail string) Result {
	t.Helper()
	r := Result{
		SchemaVersion:       ResultSchemaVersion,
		RequestDigestSHA256: requestDigest,
		Operation:           op,
		TerminalOutcome:     outcome,
		Detail:              detail,
	}
	digest, err := ResultDigest(r)
	if err != nil {
		t.Fatalf("ResultDigest: %v", err)
	}
	r.ResultDigestSHA256 = digest
	return NormalizeResult(r)
}

func fixtureObservationBatch(t *testing.T, requestDigest string) ObservationBatch {
	t.Helper()
	b := ObservationBatch{
		SchemaVersion:       ObservationBatchSchemaVersion,
		RequestDigestSHA256: requestDigest,
		Observations: []Observation{
			{SequenceNumber: 1, Detail: "began planning", ObservedAt: "2026-01-01T00:05:01Z"},
			{SequenceNumber: 2, Detail: "planning complete", ObservedAt: "2026-01-01T00:05:02Z"},
		},
	}
	digest, err := ObservationBatchDigest(b)
	if err != nil {
		t.Fatalf("ObservationBatchDigest: %v", err)
	}
	b.ObservationBatchDigestSHA256 = digest
	return NormalizeObservationBatch(b)
}

func fixtureReceiptCompleted(t *testing.T, requestDigest, capabilitiesDigest, responseDigest, batchDigest string) Receipt {
	t.Helper()
	r := Receipt{
		SchemaVersion:            ReceiptSchemaVersion,
		ReceiptID:                "receipt.fixture.completed",
		RequestDigestSHA256:      requestDigest,
		CapabilitiesDigestSHA256: capabilitiesDigest,
		ProviderObservation: synthesis.ProviderObservation{
			ProviderID:   "provider.fixture",
			ProviderKind: "test-double",
			ObservedAt:   "2026-01-01T00:05:03Z",
		},
		TerminalOutcome:              OutcomeCompleted,
		ResponseDigestSHA256:         &responseDigest,
		ObservationBatchDigestSHA256: &batchDigest,
		StartedAt:                    "2026-01-01T00:05:00Z",
		CompletedAt:                  "2026-01-01T00:05:03Z",
	}
	digest, err := ReceiptDigest(r)
	if err != nil {
		t.Fatalf("ReceiptDigest: %v", err)
	}
	r.ReceiptDigestSHA256 = digest
	return NormalizeReceipt(r)
}

func fixtureReceiptUnavailable(t *testing.T, requestDigest, capabilitiesDigest string) Receipt {
	t.Helper()
	r := Receipt{
		SchemaVersion:            ReceiptSchemaVersion,
		ReceiptID:                "receipt.fixture.unavailable",
		RequestDigestSHA256:      requestDigest,
		CapabilitiesDigestSHA256: capabilitiesDigest,
		ProviderObservation: synthesis.ProviderObservation{
			ProviderID:   "provider.fixture",
			ProviderKind: "test-double",
			ObservedAt:   "2026-01-01T00:05:01Z",
		},
		TerminalOutcome:              OutcomeUnavailable,
		ResponseDigestSHA256:         nil,
		ObservationBatchDigestSHA256: nil,
		StartedAt:                    "2026-01-01T00:05:00Z",
		CompletedAt:                  "2026-01-01T00:05:01Z",
	}
	digest, err := ReceiptDigest(r)
	if err != nil {
		t.Fatalf("ReceiptDigest: %v", err)
	}
	r.ReceiptDigestSHA256 = digest
	return NormalizeReceipt(r)
}

// --- happy-path chain: schema-valid end to end ---

func TestHappyPathChainValidatesAgainstEverySchema(t *testing.T) {
	sessionDigest := zeroDigest
	interpretationDigest := zeroDigest

	capabilities := fixtureCapabilities(t)
	request := fixturePlanningRequest(t, sessionDigest, interpretationDigest)
	result := fixtureResult(t, request.RequestDigestSHA256, OperationPlanning, OutcomeCompleted, "planning complete")
	batch := fixtureObservationBatch(t, request.RequestDigestSHA256)
	receipt := fixtureReceiptCompleted(t, request.RequestDigestSHA256, capabilities.CapabilitiesDigestSHA256, result.ResultDigestSHA256, batch.ObservationBatchDigestSHA256)

	cases := []struct {
		name     string
		doc      any
		validate func([]byte) error
	}{
		{"capabilities", capabilities, ValidateCapabilitiesSchema},
		{"request", request, ValidateRequestSchema},
		{"result", result, ValidateResultSchema},
		{"observation_batch", batch, ValidateObservationBatchSchema},
		{"receipt", receipt, ValidateReceiptSchema},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data, err := json.Marshal(c.doc)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if err := c.validate(data); err != nil {
				t.Fatalf("schema validation failed: %v\ndocument: %s", err, data)
			}
		})
	}

	// Chain integrity: each document references its exact parent's real
	// digest, not a placeholder.
	if result.RequestDigestSHA256 != request.RequestDigestSHA256 {
		t.Error("result does not reference the request's real digest")
	}
	if batch.RequestDigestSHA256 != request.RequestDigestSHA256 {
		t.Error("observation batch does not reference the request's real digest")
	}
	if receipt.RequestDigestSHA256 != request.RequestDigestSHA256 {
		t.Error("receipt does not reference the request's real digest")
	}
	if receipt.CapabilitiesDigestSHA256 != capabilities.CapabilitiesDigestSHA256 {
		t.Error("receipt does not reference the capabilities' real digest")
	}
	if receipt.ResponseDigestSHA256 == nil || *receipt.ResponseDigestSHA256 != result.ResultDigestSHA256 {
		t.Error("receipt does not reference the result's real digest")
	}
	if receipt.ObservationBatchDigestSHA256 == nil || *receipt.ObservationBatchDigestSHA256 != batch.ObservationBatchDigestSHA256 {
		t.Error("receipt does not reference the observation batch's real digest")
	}
}

// --- digest determinism and self-exclusion ---

func TestDigestsAreDeterministic(t *testing.T) {
	c1 := fixtureCapabilities(t)
	c2 := fixtureCapabilities(t)
	if c1.CapabilitiesDigestSHA256 != c2.CapabilitiesDigestSHA256 {
		t.Errorf("CapabilitiesDigest is not deterministic: %q vs %q", c1.CapabilitiesDigestSHA256, c2.CapabilitiesDigestSHA256)
	}
	if len(c1.CapabilitiesDigestSHA256) != 64 {
		t.Errorf("CapabilitiesDigestSHA256 is not a 64-char hex digest: %q", c1.CapabilitiesDigestSHA256)
	}
}

// TestDigestExcludesSelfField proves changing only the stored (pre-existing)
// digest-field value does not change the freshly-computed digest.
func TestDigestExcludesSelfField(t *testing.T) {
	base := fixtureCapabilities(t)

	withDifferentStaleDigest := base
	withDifferentStaleDigest.CapabilitiesDigestSHA256 = "stale-value-that-should-be-ignored"

	got, err := CapabilitiesDigest(withDifferentStaleDigest)
	if err != nil {
		t.Fatal(err)
	}
	want, err := CapabilitiesDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("CapabilitiesDigest depends on its own prior value: got %q, want %q", got, want)
	}
}

// TestReceiptDigestExcludesTimestamps proves receipt identity does not
// depend on wall-clock time: two receipts identical except for
// StartedAt/CompletedAt must have the same digest.
func TestReceiptDigestExcludesTimestamps(t *testing.T) {
	r1 := fixtureReceiptUnavailable(t, zeroDigest, zeroDigest)
	r2 := r1
	r2.StartedAt = "2099-12-31T23:59:58Z"
	r2.CompletedAt = "2099-12-31T23:59:59Z"

	d1, err := ReceiptDigest(r1)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := ReceiptDigest(r2)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Errorf("ReceiptDigest depends on StartedAt/CompletedAt: %q vs %q", d1, d2)
	}
}

// TestReceiptDigestStillDependsOnSubstance proves the timestamp exclusion is
// narrow: a substantive field change still changes the digest.
func TestReceiptDigestStillDependsOnSubstance(t *testing.T) {
	r1 := fixtureReceiptUnavailable(t, zeroDigest, zeroDigest)
	r2 := r1
	r2.TerminalOutcome = OutcomeCancelled

	d1, err := ReceiptDigest(r1)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := ReceiptDigest(r2)
	if err != nil {
		t.Fatal(err)
	}
	if d1 == d2 {
		t.Error("ReceiptDigest did not change when TerminalOutcome changed")
	}
}

// --- normalization ---

func TestNormalizeMakesNilSlicesNonNil(t *testing.T) {
	c := Capabilities{SupportedOperations: nil}
	if got := NormalizeCapabilities(c).SupportedOperations; got == nil {
		t.Error("NormalizeCapabilities left SupportedOperations nil")
	}

	b := ObservationBatch{Observations: nil}
	norm := NormalizeObservationBatch(b)
	data, err := json.Marshal(norm)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["observations"] == nil {
		t.Error("normalized observation batch still marshals \"observations\" as null, not []")
	}

	r := Receipt{Limitations: nil}
	if got := NormalizeReceipt(r).Limitations; got == nil {
		t.Error("NormalizeReceipt left Limitations nil")
	}
}

// TestUnnormalizedNilSlicesFailSchemaValidation proves the normalize step is
// load-bearing: a document built with a nil slice and NOT normalized fails
// schema validation, because JSON null is not a valid array instance.
func TestUnnormalizedNilSlicesFailSchemaValidation(t *testing.T) {
	b := ObservationBatch{
		SchemaVersion:                ObservationBatchSchemaVersion,
		RequestDigestSHA256:          zeroDigest,
		Observations:                 nil, // NOT normalized
		ObservationBatchDigestSHA256: zeroDigest,
	}
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateObservationBatchSchema(data); err == nil {
		t.Fatal("expected an un-normalized nil observations (marshals to null) to fail schema validation")
	}
}
