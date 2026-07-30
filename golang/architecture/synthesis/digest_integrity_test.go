// SPDX-License-Identifier: AGPL-3.0-only

// digest_integrity_test.go proves the repair for the architect-review
// finding on PR #122 head 578ec86bde9cf40d3760c0c8583b35870ada3323: schema
// validation only proves a *_digest_sha256 field looks like a SHA-256 hex
// value — it cannot prove the value is the ACTUAL digest of the artifact's
// own content. Every accept path (NewSessionState for Session; Transition's
// RecordInterpretation/RecordPlan/RecordAttempt/RecordEvaluation for the
// other four documents) must independently recompute each artifact's
// content digest and reject any artifact whose self-declared digest field
// does not match, before that document's identity is trusted anywhere else
// (for example, as the parent reference the next artifact in the chain must
// carry).
package synthesis

import (
	"reflect"
	"testing"
)

func TestNewSessionStateRejectsDeclaredDigestMismatch(t *testing.T) {
	session := fixtureSession(t)
	session.SessionDigestSHA256 = zeroDigest // schema-valid shape, wrong content digest

	got, err := NewSessionState(session)
	if err == nil {
		t.Fatal("expected NewSessionState to reject a session whose declared digest does not match its actual computed digest")
	}
	if !reflect.DeepEqual(got, SessionState{}) {
		t.Errorf("expected the zero-value SessionState on rejection, got %+v", got)
	}
}

func TestRecordCommandsRejectDeclaredDigestMismatch(t *testing.T) {
	created := freshCreatedState(t)
	tamperedInterpretation := fixtureInterpretation(t, created.Session.SessionDigestSHA256)
	tamperedInterpretation.InterpretationDigestSHA256 = zeroDigest

	planningSeed := freshCreatedState(t)
	interp := fixtureInterpretation(t, planningSeed.Session.SessionDigestSHA256)
	planning, _ := mustTransition(t, planningSeed, RecordInterpretationCommand{Interpretation: interp})
	tamperedPlan := buildPlan(t, planning.InterpretationDigestSHA256, planning.ExpectedPlanGeneration)
	tamperedPlan.PlanDigestSHA256 = zeroDigest

	attempting := driveToAttempting(t, freshCreatedState(t))
	tamperedAttempt := buildAttempt(t, attempting.LatestPlanDigestSHA256, attempting.PlanGeneration, attempting.ExpectedAttemptNumber, ProviderStatusCompleted)
	tamperedAttempt.AttemptDigestSHA256 = zeroDigest

	evaluating := driveToEvaluating(t, freshCreatedState(t))
	// RecommendRetryGeneration (with the fixture's default budget) never
	// reaches terminate(), which keeps this case isolated to exactly the
	// digest-mismatch check: RecommendAcceptCandidate would also fail here
	// on the unrelated, unset RecordEvaluationCommand.CompletedAt field,
	// masking whether the digest check itself is what rejected it.
	tamperedEvaluation := buildEvaluation(t, evaluating.LatestAttemptDigestSHA256, RecommendRetryGeneration)
	tamperedEvaluation.EvaluationDigestSHA256 = zeroDigest

	cases := []struct {
		name  string
		state SessionState
		cmd   Command
	}{
		{"RecordInterpretation declares a digest that does not match its content", created, RecordInterpretationCommand{Interpretation: tamperedInterpretation}},
		{"RecordPlan declares a digest that does not match its content", planning, RecordPlanCommand{Plan: tamperedPlan}},
		{"RecordAttempt declares a digest that does not match its content", attempting, RecordAttemptCommand{Attempt: tamperedAttempt}},
		{"RecordEvaluation declares a digest that does not match its content", evaluating, RecordEvaluationCommand{Evaluation: tamperedEvaluation}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := c.state
			after, events, err := Transition(c.state, c.cmd)
			if err == nil {
				t.Fatalf("expected an error, got none (phase now %s)", after.Phase)
			}
			if events != nil {
				t.Errorf("expected no events on a digest-mismatch rejection, got %v", events)
			}
			if !reflect.DeepEqual(after, before) {
				t.Errorf("state changed on a digest-mismatch rejection:\nbefore: %+v\nafter:  %+v", before, after)
			}
		})
	}
}
