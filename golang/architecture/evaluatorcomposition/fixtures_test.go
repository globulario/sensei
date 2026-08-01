// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/synthesis"
)

const zeroDigest = "0000000000000000000000000000000000000000000000000000000000000000"

func stringPtr(s string) *string { return &s }
func boolPtr(b bool) *bool       { return &b }

// fixtureEvaluationPolicy builds a fully valid, digest-consistent
// EvaluationPolicy fixture.
func fixtureEvaluationPolicy(t *testing.T) EvaluationPolicy {
	t.Helper()
	p := EvaluationPolicy{
		SchemaVersion:                 EvaluationPolicySchemaVersion,
		PolicyID:                      "policy.session-1.attempt-1",
		SessionDigestSHA256:           zeroDigest,
		AttemptDigestSHA256:           zeroDigest,
		CandidateArtifactDigestSHA256: zeroDigest,
		Evaluators: []EvaluatorSpec{
			{EvaluatorID: "mechanical.go-test", Required: true},
			{EvaluatorID: "sensei.edit-diff-audit", Required: true},
			{EvaluatorID: "incident.scar-match", Required: false},
		},
		DeadlineAt:       "2026-08-01T00:00:00Z",
		MaxEvidenceCount: 100,
		MaxEvidenceBytes: 1 << 20,
		RequiredCheckIDs: []string{"go-test", "go-vet"},
		FailureClassRecommendations: []FailureClassRecommendation{
			{FailureClass: "mechanical-check-failure", Recommendation: synthesis.RecommendRetryGeneration},
			{FailureClass: "audit-forbidden-fix", Recommendation: synthesis.RecommendAbort},
		},
	}
	digest, err := EvaluationPolicyDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	p.PolicyDigestSHA256 = digest
	return p
}

// fixtureEvaluatorDescriptor builds a fully valid, digest-consistent
// EvaluatorDescriptor fixture.
func fixtureEvaluatorDescriptor(t *testing.T) EvaluatorDescriptor {
	t.Helper()
	d := EvaluatorDescriptor{
		SchemaVersion:        EvaluatorDescriptorSchemaVersion,
		EvaluatorID:          "mechanical.go-test",
		EvaluatorKind:        "mechanical",
		EvaluatorVersion:     "1.0.0",
		SupportedCheckIDs:    []string{"go-test", "go-vet"},
		Deterministic:        false,
		RequiredCapabilities: []string{"go-toolchain"},
		Limitations: []synthesis.Limitation{
			{Source: "mechanical.go-test", Scope: "flaky-tests", Reason: "known-flaky suite excluded", Blocking: false},
		},
	}
	digest, err := EvaluatorDescriptorDigest(d)
	if err != nil {
		t.Fatal(err)
	}
	d.DescriptorDigestSHA256 = digest
	return d
}

// fixtureEvaluationInput builds a fully valid, digest-consistent
// EvaluationInput fixture.
func fixtureEvaluationInput(t *testing.T) EvaluationInput {
	t.Helper()
	i := EvaluationInput{
		SchemaVersion:                  EvaluationInputSchemaVersion,
		SessionDigestSHA256:            zeroDigest,
		AttemptDigestSHA256:            zeroDigest,
		CandidateArtifactDigestSHA256:  zeroDigest,
		RepositoryDomain:               "github.com/globulario/sensei",
		BaseRevision:                   "c66a1d302272dc1ba96fcdc81ab4b127cd580992",
		PlanGeneration:                 1,
		AttemptNumber:                  1,
		EvaluatorSurfaceRef:            "surface://mechanical.go-test/1",
		DeadlineAt:                     "2026-08-01T00:00:00Z",
		MaxEvidenceCount:               100,
		MaxEvidenceBytes:               1 << 20,
		RequiredProofObligationDigests: []string{zeroDigest},
	}
	digest, err := EvaluationInputDigest(i)
	if err != nil {
		t.Fatal(err)
	}
	i.EvaluationInputDigestSHA256 = digest
	return i
}

// fixtureEvaluatorResult builds a fully valid, digest-consistent
// EvaluatorResult fixture for a completed evaluator invocation.
func fixtureEvaluatorResult(t *testing.T) EvaluatorResult {
	t.Helper()
	descriptor := fixtureEvaluatorDescriptor(t)
	input := fixtureEvaluationInput(t)
	r := EvaluatorResult{
		SchemaVersion:                   EvaluatorResultSchemaVersion,
		EvaluatorID:                     descriptor.EvaluatorID,
		EvaluatorDescriptorDigestSHA256: descriptor.DescriptorDigestSHA256,
		EvaluationInputDigestSHA256:     input.EvaluationInputDigestSHA256,
		TerminalOutcome:                 EvaluatorOutcomeCompleted,
		Checks: []synthesis.CheckObservation{
			{CheckID: "go-test", Status: synthesis.CheckPassed, EvidenceReferences: []string{"evidence://go-test/stdout"}},
		},
		EvidenceReferences: []EvidenceReference{
			{Reference: "evidence://go-test/stdout", DigestSHA256: zeroDigest},
		},
		ClassifiedFailureReasons: []string{},
		Limitations:              []synthesis.Limitation{},
		CleanupSucceeded:         boolPtr(true),
	}
	digest, err := EvaluatorResultDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	r.ResultDigestSHA256 = digest
	return r
}

// finishEvaluationReceipt fills in ReceiptDigestSHA256 by recomputation.
func finishEvaluationReceipt(t *testing.T, r EvaluationReceipt) EvaluationReceipt {
	t.Helper()
	digest, err := EvaluationReceiptDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	r.ReceiptDigestSHA256 = digest
	return r
}

// fixtureEvaluationReceipt builds a syntactically valid EvaluationReceipt
// for the given disposition, with every field's presence matching
// FieldPresenceFor(disposition) and O1TerminalReceiptRequirementFor(disposition)
// exactly. This is the one fixture builder every disposition-coverage test
// uses, so the presence pattern is defined in exactly one place.
//
// evaluatedTerminal only affects DispositionEvaluated (ignored otherwise):
// true builds the variant with a present O1TerminalReceiptDigestSHA256 (the
// resulting O1 phase was terminal), false builds the variant with it absent
// (the resulting O1 phase was PhaseRetry/PhaseReplan) -- both are valid per
// O1TerminalReceiptAmbiguous.
func fixtureEvaluationReceipt(t *testing.T, d Disposition, evaluatedTerminal bool) EvaluationReceipt {
	t.Helper()
	presence, err := FieldPresenceFor(d)
	if err != nil {
		t.Fatal(err)
	}
	o1Req, err := O1TerminalReceiptRequirementFor(d)
	if err != nil {
		t.Fatal(err)
	}

	r := EvaluationReceipt{
		SchemaVersion:                 EvaluationReceiptSchemaVersion,
		ReceiptID:                     "evaluationreceipt.session-1.attempt-1",
		SessionDigestSHA256:           zeroDigest,
		AttemptDigestSHA256:           zeroDigest,
		RunnerReceiptDigestSHA256:     zeroDigest,
		RequestDigestSHA256:           zeroDigest,
		ResultDigestSHA256:            zeroDigest,
		O2ReceiptDigestSHA256:         zeroDigest,
		PolicyDigestSHA256:            zeroDigest,
		CandidateArtifactDigestSHA256: zeroDigest,
		CandidateArtifactVerified:     presence.CandidateArtifactVerified,
		Disposition:                   d,
		CompletedAt:                   "2026-08-01T00:00:00Z",
	}

	if presence.EvaluatorResultDigestsMustBeEmpty {
		r.EvaluatorResultDigestsSHA256 = []string{}
	} else {
		r.EvaluatorResultDigestsSHA256 = []string{zeroDigest}
	}

	if presence.EvaluationDigest {
		r.EvaluationDigestSHA256 = stringPtr(zeroDigest)
	}

	switch o1Req {
	case O1TerminalReceiptRequired:
		r.O1TerminalReceiptDigestSHA256 = stringPtr(zeroDigest)
	case O1TerminalReceiptAmbiguous:
		if evaluatedTerminal {
			r.O1TerminalReceiptDigestSHA256 = stringPtr(zeroDigest)
		}
	}

	if d == DispositionEvaluated {
		r.FailureDetail = ""
	} else {
		r.FailureDetail = "evaluatorcomposition: " + string(d)
	}

	if presence.CleanupSucceeded {
		r.CleanupSucceeded = boolPtr(true)
	}

	return finishEvaluationReceipt(t, r)
}

// fixtureEvaluationReceiptCleanupFailed is fixtureEvaluationReceipt with
// CleanupSucceeded flipped to false and CleanupFailureDetail populated, for
// dispositions where cleanup applies at all.
func fixtureEvaluationReceiptCleanupFailed(t *testing.T, d Disposition, evaluatedTerminal bool) EvaluationReceipt {
	t.Helper()
	r := fixtureEvaluationReceipt(t, d, evaluatedTerminal)
	r.CleanupSucceeded = boolPtr(false)
	r.CleanupFailureDetail = "evaluatorcomposition: cleanup failed"
	return finishEvaluationReceipt(t, r)
}
