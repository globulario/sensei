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
// golang/architecture/synthesis's fixtures_test.go conventions. The
// fixtureSynthesis* builders construct the O1 parent chain (Session ->
// Interpretation -> Plan -> Attempt -> Evaluation) this package's Request/
// Result payloads embed directly, reusing golang/architecture/synthesis's
// own exported types and digest functions rather than reinventing them.

func intPtr(v int) *int { return &v }

func fixtureSynthesisSession(t *testing.T) synthesis.Session {
	t.Helper()
	s := synthesis.Session{
		SchemaVersion:                 synthesis.SessionSchemaVersion,
		SessionID:                     "session.fixture.001",
		GeneratedBy:                   synthesis.GeneratedBy,
		RepositoryDomain:              "github.com/example/repo",
		BaseRevision:                  "abc123def456",
		WorkspaceIdentityDigestSHA256: zeroDigest,
		GraphAuthorityDigestSHA256:    zeroDigest,
		TaskSessionDigestSHA256:       zeroDigest,
		ClosureDigestSHA256:           zeroDigest,
		Objective:                     "fix the literal-colon route bug",
		RetryBudget:                   3,
		ReplanBudget:                  1,
		CreatedAt:                     "2026-01-01T00:00:00Z",
	}
	digest, err := synthesis.SessionDigest(s)
	if err != nil {
		t.Fatalf("synthesis.SessionDigest: %v", err)
	}
	s.SessionDigestSHA256 = digest
	return synthesis.NormalizeSession(s)
}

func fixtureSynthesisInterpretation(t *testing.T, sessionDigest string) synthesis.Interpretation {
	t.Helper()
	in := synthesis.Interpretation{
		SchemaVersion:       synthesis.InterpretationSchemaVersion,
		InterpretationID:    "interpretation.fixture.001",
		SessionDigestSHA256: sessionDigest,
		GeneratedBy:         synthesis.GeneratedBy,
		Objective:           "fix the literal-colon route bug",
		ApplicableIntent:    []string{"intent.router_must_never_panic"},
		SourceReferences:    []synthesis.SourceReference{{Reference: "gin.go:243", SourceDigestSHA256: zeroDigest}},
	}
	digest, err := synthesis.InterpretationDigest(in)
	if err != nil {
		t.Fatalf("synthesis.InterpretationDigest: %v", err)
	}
	in.InterpretationDigestSHA256 = digest
	return synthesis.NormalizeInterpretation(in)
}

func fixtureSynthesisPlan(t *testing.T, interpretationDigest string) synthesis.Plan {
	t.Helper()
	p := synthesis.Plan{
		SchemaVersion:              synthesis.PlanSchemaVersion,
		PlanID:                     "plan.fixture.001",
		InterpretationDigestSHA256: interpretationDigest,
		PlanGeneration:             1,
		Steps: []synthesis.PlanStep{
			{StepID: "step.1", Description: "hook the route-tree compile step", IntendedFiles: []string{"gin.go"}},
		},
		ProviderObservation: synthesis.ProviderObservation{ProviderID: "provider.fixture", ProviderKind: "test-double", ObservedAt: "2026-01-01T00:05:00Z"},
	}
	digest, err := synthesis.PlanDigest(p)
	if err != nil {
		t.Fatalf("synthesis.PlanDigest: %v", err)
	}
	p.PlanDigestSHA256 = digest
	return synthesis.NormalizePlan(p)
}

func fixtureSynthesisAttempt(t *testing.T, planDigest string, planGeneration int) synthesis.Attempt {
	t.Helper()
	a := synthesis.Attempt{
		SchemaVersion:              synthesis.AttemptSchemaVersion,
		AttemptID:                  "attempt.fixture.001",
		AttemptNumber:              1,
		PlanGeneration:             planGeneration,
		PlanDigestSHA256:           planDigest,
		InputCandidateDigestSHA256: zeroDigest,
		ProviderObservation:        synthesis.ProviderObservation{ProviderID: "provider.fixture", ProviderKind: "test-double", ObservedAt: "2026-01-01T00:10:00Z"},
		ProposedChangeDigestSHA256: zeroDigest,
		TerminalProviderStatus:     synthesis.ProviderStatusCompleted,
		ProducedAt:                 "2026-01-01T00:10:00Z",
	}
	digest, err := synthesis.AttemptDigest(a)
	if err != nil {
		t.Fatalf("synthesis.AttemptDigest: %v", err)
	}
	a.AttemptDigestSHA256 = digest
	return synthesis.NormalizeAttempt(a)
}

func fixtureSynthesisEvaluation(t *testing.T, attemptDigest string) synthesis.Evaluation {
	t.Helper()
	e := synthesis.Evaluation{
		SchemaVersion:       synthesis.EvaluationSchemaVersion,
		EvaluationID:        "evaluation.fixture.001",
		AttemptDigestSHA256: attemptDigest,
		EvaluatorKind:       "mechanical-test",
		EvaluatorVersion:    "v1",
		Checks:              []synthesis.CheckObservation{{CheckID: "check.go_test", Status: synthesis.CheckPassed}},
		Recommendation:      synthesis.RecommendAcceptCandidate,
	}
	digest, err := synthesis.EvaluationDigest(e)
	if err != nil {
		t.Fatalf("synthesis.EvaluationDigest: %v", err)
	}
	e.EvaluationDigestSHA256 = digest
	return synthesis.NormalizeEvaluation(e)
}

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

// --- per-operation Request builders: each embeds the parent O1 artifact the
// operation extends, exactly as the schema's per-operation conditional
// requires. ---

func fixtureInterpretationRequest(t *testing.T, session synthesis.Session) Request {
	t.Helper()
	r := Request{
		SchemaVersion:              RequestSchemaVersion,
		RequestID:                  "request.fixture.interpretation",
		Operation:                  OperationInterpretation,
		SessionDigestSHA256:        session.SessionDigestSHA256,
		RepositoryDomain:           session.RepositoryDomain,
		BaseRevision:               session.BaseRevision,
		ParentArtifactDigestSHA256: session.SessionDigestSHA256,
		DeadlineAt:                 "2026-01-01T00:05:00Z",
		MaxObservationCount:        100,
		MaxObservationBytes:        1 << 20,
		InterpretationPayload:      &session,
	}
	digest, err := RequestDigest(r)
	if err != nil {
		t.Fatalf("RequestDigest: %v", err)
	}
	r.RequestDigestSHA256 = digest
	return NormalizeRequest(r)
}

func fixturePlanningRequest(t *testing.T, session synthesis.Session, interpretation synthesis.Interpretation) Request {
	t.Helper()
	r := Request{
		SchemaVersion:              RequestSchemaVersion,
		RequestID:                  "request.fixture.planning",
		Operation:                  OperationPlanning,
		SessionDigestSHA256:        session.SessionDigestSHA256,
		RepositoryDomain:           session.RepositoryDomain,
		BaseRevision:               session.BaseRevision,
		ParentArtifactDigestSHA256: interpretation.InterpretationDigestSHA256,
		ExpectedPlanGeneration:     intPtr(1),
		DeadlineAt:                 "2026-01-01T00:06:00Z",
		MaxObservationCount:        100,
		MaxObservationBytes:        1 << 20,
		PlanningPayload:            &interpretation,
	}
	digest, err := RequestDigest(r)
	if err != nil {
		t.Fatalf("RequestDigest: %v", err)
	}
	r.RequestDigestSHA256 = digest
	return NormalizeRequest(r)
}

func fixtureGenerationRequest(t *testing.T, session synthesis.Session, plan synthesis.Plan) Request {
	t.Helper()
	r := Request{
		SchemaVersion:              RequestSchemaVersion,
		RequestID:                  "request.fixture.generation",
		Operation:                  OperationGeneration,
		SessionDigestSHA256:        session.SessionDigestSHA256,
		RepositoryDomain:           session.RepositoryDomain,
		BaseRevision:               session.BaseRevision,
		ParentArtifactDigestSHA256: plan.PlanDigestSHA256,
		ExpectedPlanGeneration:     intPtr(plan.PlanGeneration),
		ExpectedAttemptNumber:      intPtr(1),
		DeadlineAt:                 "2026-01-01T00:10:00Z",
		MaxObservationCount:        100,
		MaxObservationBytes:        1 << 20,
		GenerationPayload:          &plan,
	}
	digest, err := RequestDigest(r)
	if err != nil {
		t.Fatalf("RequestDigest: %v", err)
	}
	r.RequestDigestSHA256 = digest
	return NormalizeRequest(r)
}

func fixtureEvaluationObservationRequest(t *testing.T, session synthesis.Session, attempt synthesis.Attempt) Request {
	t.Helper()
	r := Request{
		SchemaVersion:                RequestSchemaVersion,
		RequestID:                    "request.fixture.evaluation_observation",
		Operation:                    OperationEvaluationObservation,
		SessionDigestSHA256:          session.SessionDigestSHA256,
		RepositoryDomain:             session.RepositoryDomain,
		BaseRevision:                 session.BaseRevision,
		ParentArtifactDigestSHA256:   attempt.AttemptDigestSHA256,
		ExpectedPlanGeneration:       intPtr(attempt.PlanGeneration),
		ExpectedAttemptNumber:        intPtr(attempt.AttemptNumber),
		DeadlineAt:                   "2026-01-01T00:15:00Z",
		MaxObservationCount:          100,
		MaxObservationBytes:          1 << 20,
		EvaluationObservationPayload: &attempt,
	}
	digest, err := RequestDigest(r)
	if err != nil {
		t.Fatalf("RequestDigest: %v", err)
	}
	r.RequestDigestSHA256 = digest
	return NormalizeRequest(r)
}

// --- per-operation Result builders: each embeds the candidate next O1
// artifact the operation proposes, only on a completed outcome. ---

func fixtureInterpretationResult(t *testing.T, requestDigest string, interpretation synthesis.Interpretation) Result {
	t.Helper()
	payloadDigest := interpretation.InterpretationDigestSHA256
	r := Result{
		SchemaVersion:         ResultSchemaVersion,
		RequestDigestSHA256:   requestDigest,
		Operation:             OperationInterpretation,
		TerminalOutcome:       OutcomeCompleted,
		Detail:                "interpretation complete",
		InterpretationPayload: &interpretation,
		PayloadDigestSHA256:   &payloadDigest,
	}
	digest, err := ResultDigest(r)
	if err != nil {
		t.Fatalf("ResultDigest: %v", err)
	}
	r.ResultDigestSHA256 = digest
	return NormalizeResult(r)
}

func fixturePlanningResult(t *testing.T, requestDigest string, plan synthesis.Plan) Result {
	t.Helper()
	payloadDigest := plan.PlanDigestSHA256
	r := Result{
		SchemaVersion:       ResultSchemaVersion,
		RequestDigestSHA256: requestDigest,
		Operation:           OperationPlanning,
		TerminalOutcome:     OutcomeCompleted,
		Detail:              "planning complete",
		PlanningPayload:     &plan,
		PayloadDigestSHA256: &payloadDigest,
	}
	digest, err := ResultDigest(r)
	if err != nil {
		t.Fatalf("ResultDigest: %v", err)
	}
	r.ResultDigestSHA256 = digest
	return NormalizeResult(r)
}

func fixtureGenerationResult(t *testing.T, requestDigest string, attempt synthesis.Attempt) Result {
	t.Helper()
	payloadDigest := attempt.AttemptDigestSHA256
	r := Result{
		SchemaVersion:       ResultSchemaVersion,
		RequestDigestSHA256: requestDigest,
		Operation:           OperationGeneration,
		TerminalOutcome:     OutcomeCompleted,
		Detail:              "generation complete",
		GenerationPayload:   &attempt,
		PayloadDigestSHA256: &payloadDigest,
	}
	digest, err := ResultDigest(r)
	if err != nil {
		t.Fatalf("ResultDigest: %v", err)
	}
	r.ResultDigestSHA256 = digest
	return NormalizeResult(r)
}

func fixtureEvaluationObservationResult(t *testing.T, requestDigest string, evaluation synthesis.Evaluation) Result {
	t.Helper()
	payloadDigest := evaluation.EvaluationDigestSHA256
	r := Result{
		SchemaVersion:                ResultSchemaVersion,
		RequestDigestSHA256:          requestDigest,
		Operation:                    OperationEvaluationObservation,
		TerminalOutcome:              OutcomeCompleted,
		Detail:                       "evaluation observation complete",
		EvaluationObservationPayload: &evaluation,
		PayloadDigestSHA256:          &payloadDigest,
	}
	digest, err := ResultDigest(r)
	if err != nil {
		t.Fatalf("ResultDigest: %v", err)
	}
	r.ResultDigestSHA256 = digest
	return NormalizeResult(r)
}

// fixtureUnavailableResult builds a typed-failure Result for op: no payload,
// no payload digest, but still a fully valid, digestible envelope -- proving
// a Result-envelope remains real evidence even on a non-completed outcome.
func fixtureUnavailableResult(t *testing.T, requestDigest string, op Operation) Result {
	t.Helper()
	r := Result{
		SchemaVersion:       ResultSchemaVersion,
		RequestDigestSHA256: requestDigest,
		Operation:           op,
		TerminalOutcome:     OutcomeUnavailable,
		Detail:              "provider unavailable",
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

func fixtureReceiptCompleted(t *testing.T, requestDigest, capabilitiesDigest, resultDigest, payloadDigest, batchDigest string) Receipt {
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
		ResultDigestSHA256:           resultDigest,
		PayloadDigestSHA256:          &payloadDigest,
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

// fixtureReceiptUnavailable proves typed failures do not lose their
// Result-envelope evidence: ResultDigestSHA256 is still populated (it
// always is now) even though PayloadDigestSHA256 stays nil.
func fixtureReceiptUnavailable(t *testing.T, requestDigest, capabilitiesDigest, resultDigest string) Receipt {
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
		ResultDigestSHA256:           resultDigest,
		PayloadDigestSHA256:          nil,
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

// --- happy-path chain: schema-valid end to end, across every operation ---

func TestHappyPathChainValidatesAgainstEverySchema(t *testing.T) {
	session := fixtureSynthesisSession(t)
	interpretation := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	plan := fixtureSynthesisPlan(t, interpretation.InterpretationDigestSHA256)
	attempt := fixtureSynthesisAttempt(t, plan.PlanDigestSHA256, plan.PlanGeneration)
	evaluation := fixtureSynthesisEvaluation(t, attempt.AttemptDigestSHA256)

	capabilities := fixtureCapabilities(t)

	// Drive all four operations' request/result pairs through real schema
	// validation, each carrying its operation-specific payload.
	requests := []struct {
		name string
		req  Request
	}{
		{"interpretation", fixtureInterpretationRequest(t, session)},
		{"planning", fixturePlanningRequest(t, session, interpretation)},
		{"generation", fixtureGenerationRequest(t, session, plan)},
		{"evaluation_observation", fixtureEvaluationObservationRequest(t, session, attempt)},
	}
	for _, c := range requests {
		t.Run("request/"+c.name, func(t *testing.T) {
			data, err := json.Marshal(c.req)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if err := ValidateRequestSchema(data); err != nil {
				t.Fatalf("schema validation failed: %v\ndocument: %s", err, data)
			}
		})
	}

	results := []struct {
		name string
		res  Result
	}{
		{"interpretation", fixtureInterpretationResult(t, requests[0].req.RequestDigestSHA256, interpretation)},
		{"planning", fixturePlanningResult(t, requests[1].req.RequestDigestSHA256, plan)},
		{"generation", fixtureGenerationResult(t, requests[2].req.RequestDigestSHA256, attempt)},
		{"evaluation_observation", fixtureEvaluationObservationResult(t, requests[3].req.RequestDigestSHA256, evaluation)},
	}
	for _, c := range results {
		t.Run("result/"+c.name, func(t *testing.T) {
			data, err := json.Marshal(c.res)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if err := ValidateResultSchema(data); err != nil {
				t.Fatalf("schema validation failed: %v\ndocument: %s", err, data)
			}
		})
	}

	// Representative full chain (planning) through observation batch and
	// receipt.
	request := requests[1].req
	result := results[1].res
	batch := fixtureObservationBatch(t, request.RequestDigestSHA256)
	receipt := fixtureReceiptCompleted(t, request.RequestDigestSHA256, capabilities.CapabilitiesDigestSHA256, result.ResultDigestSHA256, *result.PayloadDigestSHA256, batch.ObservationBatchDigestSHA256)

	cases := []struct {
		name     string
		doc      any
		validate func([]byte) error
	}{
		{"capabilities", capabilities, ValidateCapabilitiesSchema},
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
	if result.PlanningPayload == nil || result.PlanningPayload.PlanDigestSHA256 != plan.PlanDigestSHA256 {
		t.Error("planning result does not carry the real candidate plan")
	}
	if *result.PayloadDigestSHA256 != plan.PlanDigestSHA256 {
		t.Error("result's PayloadDigestSHA256 does not match the embedded plan's real digest")
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
	if receipt.ResultDigestSHA256 != result.ResultDigestSHA256 {
		t.Error("receipt does not reference the result's real digest")
	}
	if receipt.PayloadDigestSHA256 == nil || *receipt.PayloadDigestSHA256 != *result.PayloadDigestSHA256 {
		t.Error("receipt does not reference the result's real payload digest")
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
	r1 := fixtureReceiptUnavailable(t, zeroDigest, zeroDigest, zeroDigest)
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
	r1 := fixtureReceiptUnavailable(t, zeroDigest, zeroDigest, zeroDigest)
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

// TestNormalizeRequestNormalizesNestedPayload proves NormalizeRequest
// reaches into a populated payload and normalizes its own nested slices,
// reusing golang/architecture/synthesis's normalizer rather than
// duplicating it.
func TestNormalizeRequestNormalizesNestedPayload(t *testing.T) {
	session := synthesis.Session{ProofObligationDigests: nil}
	req := Request{PlanningPayload: nil, InterpretationPayload: &session}
	norm := NormalizeRequest(req)
	if norm.InterpretationPayload.ProofObligationDigests == nil {
		t.Error("NormalizeRequest did not normalize the embedded Session's nil slice field")
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
