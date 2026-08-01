// SPDX-License-Identifier: AGPL-3.0-only

package synthesis

import (
	"encoding/json"
	"testing"
)

// --- fixture builders ---
//
// Each builder produces a document with its own digest field correctly
// computed, chained to its parent by the parent's real digest — mirroring
// the happy path: interpretation -> plan -> attempt -> evaluation ->
// candidate-ready receipt.

func fixtureSession(t *testing.T) Session {
	t.Helper()
	s := Session{
		SchemaVersion:                 SessionSchemaVersion,
		SessionID:                     "session.fixture.001",
		GeneratedBy:                   GeneratedBy,
		RepositoryDomain:              "github.com/example/repo",
		BaseRevision:                  "abc123def456",
		WorkspaceIdentityDigestSHA256: zeroDigest,
		GraphAuthorityDigestSHA256:    zeroDigest,
		TaskSessionDigestSHA256:       zeroDigest,
		ClosureDigestSHA256:           zeroDigest,
		ProofObligationDigests:        nil, // intentionally nil: exercises normalization before validation
		Objective:                     "fix the literal-colon route bug",
		RetryBudget:                   3,
		ReplanBudget:                  1,
		CreatedAt:                     "2026-01-01T00:00:00Z",
	}
	digest, err := SessionDigest(s)
	if err != nil {
		t.Fatalf("SessionDigest: %v", err)
	}
	s.SessionDigestSHA256 = digest
	return NormalizeSession(s)
}

func fixtureInterpretation(t *testing.T, sessionDigest string) Interpretation {
	t.Helper()
	in := Interpretation{
		SchemaVersion:            InterpretationSchemaVersion,
		InterpretationID:         "interpretation.fixture.001",
		SessionDigestSHA256:      sessionDigest,
		GeneratedBy:              GeneratedBy,
		Objective:                "fix the literal-colon route bug",
		ApplicableIntent:         []string{"intent.router_must_never_panic"},
		BindingInvariants:        []string{"invariant.route_tree_consistency"},
		RelevantContracts:        nil,
		AuthorityBoundaries:      nil,
		KnownFailureModes:        []string{"failure.literal_colon_handler_bypass"},
		ForbiddenFixes:           nil,
		RequiredProofObligations: nil,
		Assumptions:              []string{"the route tree escape convention is unchanged"},
		UnresolvedQuestions:      nil,
		SourceReferences:         []SourceReference{{Reference: "gin.go:243", SourceDigestSHA256: zeroDigest}},
		Limitations:              nil,
	}
	digest, err := InterpretationDigest(in)
	if err != nil {
		t.Fatalf("InterpretationDigest: %v", err)
	}
	in.InterpretationDigestSHA256 = digest
	return NormalizeInterpretation(in)
}

func fixturePlan(t *testing.T, interpretationDigest string) Plan {
	t.Helper()
	p := Plan{
		SchemaVersion:              PlanSchemaVersion,
		PlanID:                     "plan.fixture.001",
		InterpretationDigestSHA256: interpretationDigest,
		PlanGeneration:             1,
		Steps: []PlanStep{
			{
				StepID:           "step.1",
				Description:      "hook the route-tree compile step into the shared serving path",
				IntendedFiles:    []string{"gin.go"},
				IntendedSymbols:  []string{"Engine.handleHTTPRequest"},
				ExpectedEvidence: []string{"test:TestEscapedColonHandler"},
			},
		},
		Assumptions:    nil,
		Risks:          []string{"may affect unrelated request paths if hooked too broadly"},
		StopConditions: []string{"any existing test regresses"},
		ProviderObservation: ProviderObservation{
			ProviderID:   "provider.fixture",
			ProviderKind: "test-double",
			ObservedAt:   "2026-01-01T00:05:00Z",
		},
	}
	digest, err := PlanDigest(p)
	if err != nil {
		t.Fatalf("PlanDigest: %v", err)
	}
	p.PlanDigestSHA256 = digest
	return NormalizePlan(p)
}

func fixtureAttempt(t *testing.T, planDigest string) Attempt {
	t.Helper()
	a := Attempt{
		SchemaVersion:              AttemptSchemaVersion,
		AttemptID:                  "attempt.fixture.001",
		AttemptNumber:              1,
		PlanGeneration:             1,
		PlanDigestSHA256:           planDigest,
		InputCandidateDigestSHA256: zeroDigest,
		ProviderObservation: ProviderObservation{
			ProviderID:   "provider.fixture",
			ProviderKind: "test-double",
			ObservedAt:   "2026-01-01T00:10:00Z",
		},
		ProposedChangeDigestSHA256: zeroDigest,
		EvidenceReferences:         nil,
		TerminalProviderStatus:     ProviderStatusCompleted,
		ProducedAt:                 "2026-01-01T00:10:00Z",
	}
	digest, err := AttemptDigest(a)
	if err != nil {
		t.Fatalf("AttemptDigest: %v", err)
	}
	a.AttemptDigestSHA256 = digest
	return NormalizeAttempt(a)
}

func fixtureEvaluation(t *testing.T, attemptDigest string) Evaluation {
	t.Helper()
	e := Evaluation{
		SchemaVersion:       EvaluationSchemaVersion,
		EvaluationID:        "evaluation.fixture.001",
		AttemptDigestSHA256: attemptDigest,
		EvaluatorKind:       "mechanical-test",
		EvaluatorVersion:    "v1",
		Checks: []CheckObservation{
			{CheckID: "check.go_test", Status: CheckPassed, EvidenceReferences: []string{"test:TestEscapedColonHandler"}},
		},
		ClassifiedFailureReasons: nil,
		Recommendation:           RecommendAcceptCandidate,
		Limitations:              nil,
	}
	digest, err := EvaluationDigest(e)
	if err != nil {
		t.Fatalf("EvaluationDigest: %v", err)
	}
	e.EvaluationDigestSHA256 = digest
	return NormalizeEvaluation(e)
}

func fixtureReceipt(t *testing.T, reason TerminalReason) Receipt {
	t.Helper()
	r := Receipt{
		SchemaVersion:                     ReceiptSchemaVersion,
		ReceiptID:                         "receipt.fixture.001",
		SessionDigestSHA256:               zeroDigest,
		TerminalReason:                    reason,
		FinalAttemptDigestSHA256:          nil,
		FinalEvaluationDigestSHA256:       nil,
		AdmissionDecisionDigestSHA256:     nil,
		AdmissionVerificationDigestSHA256: nil,
		RetryCount:                        3,
		ReplanCount:                       0,
		Summary:                           "retry budget exhausted after 3 attempts",
		Limitations:                       nil,
		CompletedAt:                       "2026-01-01T01:00:00Z",
	}
	digest, err := ReceiptDigest(r)
	if err != nil {
		t.Fatalf("ReceiptDigest: %v", err)
	}
	r.ReceiptDigestSHA256 = digest
	return NormalizeReceipt(r)
}

func fixtureReceiptCandidateReady(t *testing.T) Receipt {
	t.Helper()
	session := fixtureSession(t)
	interp := fixtureInterpretation(t, session.SessionDigestSHA256)
	plan := fixturePlan(t, interp.InterpretationDigestSHA256)
	attempt := fixtureAttempt(t, plan.PlanDigestSHA256)
	eval := fixtureEvaluation(t, attempt.AttemptDigestSHA256)

	admissionDigest := zeroDigest
	r := Receipt{
		SchemaVersion:                     ReceiptSchemaVersion,
		ReceiptID:                         "receipt.fixture.candidate_ready",
		SessionDigestSHA256:               session.SessionDigestSHA256,
		TerminalReason:                    ReasonCandidateReadyForAdmission,
		FinalAttemptDigestSHA256:          &attempt.AttemptDigestSHA256,
		FinalEvaluationDigestSHA256:       &eval.EvaluationDigestSHA256,
		AdmissionDecisionDigestSHA256:     &admissionDigest,
		AdmissionVerificationDigestSHA256: nil,
		RetryCount:                        0,
		ReplanCount:                       0,
		Summary:                           "candidate ready for submission to the existing admission owner",
		Limitations:                       nil,
		CompletedAt:                       "2026-01-01T01:00:00Z",
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
	session := fixtureSession(t)
	interp := fixtureInterpretation(t, session.SessionDigestSHA256)
	plan := fixturePlan(t, interp.InterpretationDigestSHA256)
	attempt := fixtureAttempt(t, plan.PlanDigestSHA256)
	eval := fixtureEvaluation(t, attempt.AttemptDigestSHA256)
	receipt := fixtureReceiptCandidateReady(t)

	cases := []struct {
		name     string
		doc      any
		validate func([]byte) error
	}{
		{"session", session, ValidateSessionSchema},
		{"interpretation", interp, ValidateInterpretationSchema},
		{"plan", plan, ValidatePlanSchema},
		{"attempt", attempt, ValidateAttemptSchema},
		{"evaluation", eval, ValidateEvaluationSchema},
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

	// Chain integrity: each child references its exact parent's real digest,
	// not a placeholder.
	if interp.SessionDigestSHA256 != session.SessionDigestSHA256 {
		t.Error("interpretation does not reference the session's real digest")
	}
	if plan.InterpretationDigestSHA256 != interp.InterpretationDigestSHA256 {
		t.Error("plan does not reference the interpretation's real digest")
	}
	if attempt.PlanDigestSHA256 != plan.PlanDigestSHA256 {
		t.Error("attempt does not reference the plan's real digest")
	}
	if eval.AttemptDigestSHA256 != attempt.AttemptDigestSHA256 {
		t.Error("evaluation does not reference the attempt's real digest")
	}
}

// --- digest determinism and self-exclusion ---

func TestDigestsAreDeterministic(t *testing.T) {
	s1 := fixtureSession(t)
	s2 := fixtureSession(t)
	if s1.SessionDigestSHA256 != s2.SessionDigestSHA256 {
		t.Errorf("SessionDigest is not deterministic: %q vs %q", s1.SessionDigestSHA256, s2.SessionDigestSHA256)
	}
	if len(s1.SessionDigestSHA256) != 64 {
		t.Errorf("SessionDigestSHA256 is not a 64-char hex digest: %q", s1.SessionDigestSHA256)
	}
}

// TestDigestExcludesSelfField proves changing only the stored (pre-existing)
// digest-field value does not change the freshly-computed digest — the
// field is zeroed before hashing, so a document does not need to already
// carry the correct digest to compute it.
func TestDigestExcludesSelfField(t *testing.T) {
	base := fixtureSession(t)

	withDifferentStaleDigest := base
	withDifferentStaleDigest.SessionDigestSHA256 = "stale-value-that-should-be-ignored"

	got, err := SessionDigest(withDifferentStaleDigest)
	if err != nil {
		t.Fatal(err)
	}
	want, err := SessionDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("SessionDigest depends on its own prior value: got %q, want %q", got, want)
	}
}

// TestAttemptDigestExcludesProducedAt proves attempt identity does not
// depend on wall-clock time: two attempts identical except for ProducedAt
// must have the same digest.
func TestAttemptDigestExcludesProducedAt(t *testing.T) {
	a1 := fixtureAttempt(t, zeroDigest)
	a2 := a1
	a2.ProducedAt = "2099-12-31T23:59:59Z"

	d1, err := AttemptDigest(a1)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := AttemptDigest(a2)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Errorf("AttemptDigest depends on ProducedAt: %q vs %q", d1, d2)
	}
}

// TestAttemptDigestStillDependsOnSubstance proves the ProducedAt exclusion
// is narrow: a substantive field change still changes the digest, so
// AttemptDigest is not accidentally constant.
func TestAttemptDigestStillDependsOnSubstance(t *testing.T) {
	a1 := fixtureAttempt(t, zeroDigest)
	a2 := a1
	a2.TerminalProviderStatus = ProviderStatusFailed

	d1, err := AttemptDigest(a1)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := AttemptDigest(a2)
	if err != nil {
		t.Fatal(err)
	}
	if d1 == d2 {
		t.Error("AttemptDigest did not change when TerminalProviderStatus changed")
	}
}

// --- normalization ---

func TestNormalizeMakesNilSlicesNonNil(t *testing.T) {
	s := Session{ProofObligationDigests: nil}
	if got := NormalizeSession(s).ProofObligationDigests; got == nil {
		t.Error("NormalizeSession left ProofObligationDigests nil")
	}

	in := Interpretation{
		ApplicableIntent:         nil,
		BindingInvariants:        nil,
		RelevantContracts:        nil,
		AuthorityBoundaries:      nil,
		KnownFailureModes:        nil,
		ForbiddenFixes:           nil,
		RequiredProofObligations: nil,
		Assumptions:              nil,
		UnresolvedQuestions:      nil,
		SourceReferences:         nil,
		Limitations:              nil,
	}
	norm := NormalizeInterpretation(in)
	data, err := json.Marshal(norm)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"applicable_intent", "binding_invariants", "source_references", "limitations"} {
		if raw[field] == nil {
			t.Errorf("normalized interpretation still marshals %q as null, not []", field)
		}
	}
}

// TestUnnormalizedNilSlicesFailSchemaValidation proves the normalize step is
// load-bearing: a document built with nil slices and NOT normalized fails
// schema validation, because these schemas require an array, and JSON null
// is not a valid array instance.
func TestUnnormalizedNilSlicesFailSchemaValidation(t *testing.T) {
	s := Session{
		SchemaVersion:                 SessionSchemaVersion,
		SessionID:                     "s",
		GeneratedBy:                   GeneratedBy,
		RepositoryDomain:              "github.com/example/repo",
		BaseRevision:                  "abc123",
		WorkspaceIdentityDigestSHA256: zeroDigest,
		GraphAuthorityDigestSHA256:    zeroDigest,
		TaskSessionDigestSHA256:       zeroDigest,
		ClosureDigestSHA256:           zeroDigest,
		ProofObligationDigests:        nil, // NOT normalized
		Objective:                     "o",
		RetryBudget:                   1,
		ReplanBudget:                  1,
		CreatedAt:                     "2026-01-01T00:00:00Z",
		SessionDigestSHA256:           zeroDigest,
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateSessionSchema(data); err == nil {
		t.Fatal("expected an un-normalized nil proof_obligation_digests (marshals to null) to fail schema validation")
	}
}
