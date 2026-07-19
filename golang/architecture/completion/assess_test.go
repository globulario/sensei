// SPDX-License-Identifier: AGPL-3.0-only

package completion

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/questionresolution"
)

// satisfiedEvidence returns a synthetic, fully-satisfied evidence set. Pure-function
// tests mutate one field at a time to prove each obligation's classification without
// heavy durable seeding.
func satisfiedEvidence() *evidence {
	rb := closureprotocol.ResultBinding{
		BaseRevision:           "r0",
		PatchDigestSHA256:      "patch",
		ResultTreeDigestSHA256: "tree",
		GraphDigestSHA256:      "graph",
	}
	const manifest = "governed-manifest-digest"
	return &evidence{
		task:              closureprotocol.TaskBinding{ID: "task.x", SessionID: "session.x"},
		resultBinding:     rb,
		haveResultBinding: true,
		headDigest:        "head-1",
		governedManifest:  manifest,
		correctness: &closureprotocol.CertificationReceipt{
			ResultBinding:        rb,
			CertificationVerdict: closureprotocol.Certified,
			ProofLane:            closureprotocol.DimensionPass,
		},
		correctnessDigest:       "correctness-digest",
		correctnessCurrentValid: 1,
		qr: &questionresolution.QuestionResolutionCertificate{
			GovernedManifestDigestSHA256: manifest,
		},
		qrDigest:        "qr-digest",
		qrValid:         true,
		qrRelevantCount: 1,
		qrCurrentCount:  1,
	}
}

func TestAssessCorrectnessClassification(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*evidence)
		want EvidenceState
	}{
		{"satisfied", func(e *evidence) {}, EvidenceSatisfied},
		{"missing", func(e *evidence) { e.correctness = nil; e.correctnessCurrentValid = 0 }, EvidenceMissing},
		{"contradictory", func(e *evidence) { e.correctnessCurrentValid = 2 }, EvidenceContradictory},
		{"integrity", func(e *evidence) { e.correctnessTampered = true; e.correctnessTamperedErr = "tampered" }, EvidenceIntegrityFailure},
		{"unsupported_verdict", func(e *evidence) { e.correctness.CertificationVerdict = closureprotocol.CertificationReviewRequired }, EvidenceUnsupported},
		{"stale_historical_only", func(e *evidence) { e.correctness = nil; e.correctnessCurrentValid = 0; e.correctnessHistorical = 1 }, EvidenceStale},
		{"wrong_result", func(e *evidence) { e.correctnessWrongResult = true }, EvidenceWrongBinding},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := satisfiedEvidence()
			c.mut(e)
			got := stateOf(assess(e), ObligationCorrectnessCertificate)
			if got != c.want {
				t.Fatalf("correctness = %s, want %s", got, c.want)
			}
		})
	}
}

func TestAssessQuestionResolutionClassification(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*evidence)
		want EvidenceState
	}{
		{"satisfied", func(e *evidence) {}, EvidenceSatisfied},
		{"missing", func(e *evidence) { e.qr = nil; e.qrValid = false; e.qrRelevantCount = 0; e.qrCurrentCount = 0 }, EvidenceMissing},
		{"contradictory", func(e *evidence) { e.qrCurrentCount = 2 }, EvidenceContradictory},
		{"stale", func(e *evidence) { e.qrCurrentCount = 0 }, EvidenceStale},
		{"integrity", func(e *evidence) { e.qrValid = false; e.qrErr = "invalid" }, EvidenceIntegrityFailure},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := satisfiedEvidence()
			c.mut(e)
			got := stateOf(assess(e), ObligationQuestionResolution)
			if got != c.want {
				t.Fatalf("question_resolution = %s, want %s", got, c.want)
			}
		})
	}
}

func TestAssessGovernedFreshnessClassification(t *testing.T) {
	// changed governed source after the question-resolution certificate → stale.
	e := satisfiedEvidence()
	e.qr.GovernedManifestDigestSHA256 = "a-different-manifest"
	if got := stateOf(assess(e), ObligationGovernedFreshness); got != EvidenceStale {
		t.Fatalf("governed freshness = %s, want stale", got)
	}
	// no valid current question-resolution certificate to anchor freshness → unsupported.
	e2 := satisfiedEvidence()
	e2.qr = nil
	e2.qrRelevantCount = 0
	e2.qrCurrentCount = 0
	e2.qrValid = false
	if got := stateOf(assess(e2), ObligationGovernedFreshness); got != EvidenceUnsupported {
		t.Fatalf("governed freshness = %s, want unsupported", got)
	}
}

func TestAssessClosureAndProofSubsumed(t *testing.T) {
	// satisfied when correctness proof lane passes and the question loop is closed.
	if got := stateOf(assess(satisfiedEvidence()), ObligationClosureAndProof); got != EvidenceSatisfied {
		t.Fatalf("closure_and_proof = %s, want satisfied", got)
	}
	// a non-passing proof lane cannot be treated as satisfied.
	e := satisfiedEvidence()
	e.correctness.ProofLane = closureprotocol.DimensionBlocked
	if got := stateOf(assess(e), ObligationClosureAndProof); got == EvidenceSatisfied {
		t.Fatal("closure_and_proof must not be satisfied when the proof lane is blocked")
	}
}

func TestAssessReadyRequiresEveryObligation(t *testing.T) {
	if assess(satisfiedEvidence()).Readiness != ReadinessReady {
		t.Fatal("fully-satisfied evidence must be ready")
	}
	// Breaking any single obligation drops readiness.
	breakers := []func(*evidence){
		func(e *evidence) { e.haveResultBinding = false },
		func(e *evidence) { e.correctnessCurrentValid = 0; e.correctness = nil },
		func(e *evidence) { e.qrRelevantCount = 0; e.qr = nil; e.qrValid = false },
		func(e *evidence) { e.correctness.ProofLane = closureprotocol.DimensionBlocked },
		func(e *evidence) { e.qr.GovernedManifestDigestSHA256 = "changed" },
		func(e *evidence) { e.conflictingCompletion = true },
	}
	for i, b := range breakers {
		e := satisfiedEvidence()
		b(e)
		if assess(e).Readiness != ReadinessNotReady {
			t.Fatalf("breaker %d: expected not_ready", i)
		}
	}
}

func TestAssessPureDeterminismAndIdentity(t *testing.T) {
	d1, err := AssessmentDigest(assess(satisfiedEvidence()))
	if err != nil {
		t.Fatal(err)
	}
	d2, _ := AssessmentDigest(assess(satisfiedEvidence()))
	if d1 != d2 {
		t.Fatal("identical evidence must produce an identical assessment identity")
	}
	// Changed relevant evidence changes the identity.
	e := satisfiedEvidence()
	e.headDigest = "head-2"
	d3, _ := AssessmentDigest(assess(e))
	if d3 == d1 {
		t.Fatal("changed evidence must change the assessment identity")
	}
}
