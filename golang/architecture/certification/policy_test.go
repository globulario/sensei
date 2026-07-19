// SPDX-License-Identifier: Apache-2.0

package certification

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

func TestPolicyByID(t *testing.T) {
	if p, ok := PolicyByID(PolicyDefaultID); !ok || p.CoverageProfile != CoverageStaticTest {
		t.Fatalf("default policy: %v %v", p, ok)
	}
	if p, ok := PolicyByID(PolicyRuntimeID); !ok || p.CoverageProfile != CoverageStaticTestRuntime {
		t.Fatalf("runtime policy: %v %v", p, ok)
	}
	if _, ok := PolicyByID("certification.someone_elses.policy"); ok {
		t.Fatal("unknown policy must not resolve")
	}
}

// Regression suite for the governed invariant
// closure.runtime_evidence_not_applicable_only_without_governed_runtime_mandate:
// the coverage profile sets the default only for runtime evidence no governed
// obligation mandates; a governed mandate always overrides the profile.

func TestRuntime_StaticTestNoMandate_NotApplicableAndCertified(t *testing.T) {
	req, rec := greenBundle(t)
	rec = withRuntimeObligation(t, rec, false, closureprotocol.DimensionNotApplicable, nil)
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())

	evidence := laneByName(t, result, LaneEvidence)
	if evidence.Status != closureprotocol.DimensionPass {
		t.Fatalf("evidence = %s %v, want pass", evidence.Status, evidence.ReasonCodes)
	}
	if !hasLimitationPrefix(evidence, LimitationEvidenceRuntimeNotApplicable) {
		t.Fatalf("missing not_applicable documentation: %v", evidence.Limitations)
	}
	if result.Receipt.CertificationVerdict != closureprotocol.Certified {
		t.Fatalf("verdict = %s, want certified", result.Receipt.CertificationVerdict)
	}
}

func TestRuntime_StaticTestWithMandateNoReceipt_Blocks(t *testing.T) {
	req, rec := greenBundle(t)
	rec = withRuntimeObligation(t, rec, true, closureprotocol.DimensionBlocked, nil)
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())

	evidence := laneByName(t, result, LaneEvidence)
	if evidence.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(evidence, ReasonEvidenceMissingRuntime) {
		t.Fatalf("evidence = %s %v", evidence.Status, evidence.ReasonCodes)
	}
	proof := laneByName(t, result, LaneProof)
	if proof.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(proof, ReasonProofMissingSlot) {
		t.Fatalf("proof = %s %v", proof.Status, proof.ReasonCodes)
	}
	if result.Receipt.CertificationVerdict != closureprotocol.CertificationBlocked {
		t.Fatalf("verdict = %s, want blocked", result.Receipt.CertificationVerdict)
	}
	if result.NextAction != "obtain compatible runtime evidence" {
		t.Fatalf("next action = %q", result.NextAction)
	}
}

func TestRuntime_ProfileCannotOverrideMandate(t *testing.T) {
	// A discharge that claims the mandated runtime slot is not_applicable
	// under static_test is exactly the forbidden relaxation: certification
	// must catch it even though the discharge's own status is valid.
	req, rec := greenBundle(t)
	rec = withRuntimeObligation(t, rec, true, closureprotocol.DimensionNotApplicable, nil)
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())

	proof := laneByName(t, result, LaneProof)
	if proof.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(proof, ReasonProofRuntimeMandateOverride) {
		t.Fatalf("proof = %s %v, want runtime_mandate_overridden", proof.Status, proof.ReasonCodes)
	}
	if result.Receipt.CertificationVerdict != closureprotocol.CertificationBlocked {
		t.Fatalf("verdict = %s", result.Receipt.CertificationVerdict)
	}
}

func TestRuntime_StaticTestWithMandateStaleReceipt_NeverCertifies(t *testing.T) {
	req, rec := greenBundle(t)
	rec = withRuntimeObligation(t, rec, true, closureprotocol.DimensionPass, []string{"receipt.runtime.core"})
	stale := runtimeReceipt()
	stale.ExpiresAt = "2026-07-15T11:59:00Z" // before EvaluatedAt
	rec.EvidenceReceipts = append(rec.EvidenceReceipts, stale)
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())

	evidence := laneByName(t, result, LaneEvidence)
	if evidence.Status != closureprotocol.DimensionStale || !hasReasonPrefix(evidence, ReasonEvidenceReceiptExpired) {
		t.Fatalf("evidence = %s %v, want stale", evidence.Status, evidence.ReasonCodes)
	}
	if result.Receipt.CertificationVerdict == closureprotocol.Certified {
		t.Fatal("stale mandated runtime evidence must not certify")
	}
	if result.Receipt.CertificationVerdict != closureprotocol.CertificationStale {
		t.Fatalf("verdict = %s, want stale", result.Receipt.CertificationVerdict)
	}
}

func TestRuntime_StaticTestWithMandateWrongTarget_Blocks(t *testing.T) {
	req, rec := greenBundle(t)
	rec = withRuntimeObligation(t, rec, true, closureprotocol.DimensionPass, []string{"receipt.runtime.core"})
	wrong := runtimeReceipt()
	wrong.RuntimeTarget.EnvironmentID = "cluster.other"
	rec.EvidenceReceipts = append(rec.EvidenceReceipts, wrong)
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())

	evidence := laneByName(t, result, LaneEvidence)
	if evidence.Status != closureprotocol.DimensionBlocked {
		t.Fatalf("evidence = %s %v, want blocked", evidence.Status, evidence.ReasonCodes)
	}
	if result.Receipt.CertificationVerdict != closureprotocol.CertificationBlocked {
		t.Fatalf("verdict = %s", result.Receipt.CertificationVerdict)
	}
}

func TestRuntime_StaticTestWithMandateConflictedReceipts_Uncertifiable(t *testing.T) {
	req, rec := greenBundle(t)
	rec = withRuntimeObligation(t, rec, true, closureprotocol.DimensionPass, []string{"receipt.runtime.core"})
	first := runtimeReceipt()
	second := runtimeReceipt()
	second.ReceiptID = "receipt.runtime.core.b"
	second.PayloadDigestSHA256 = "disagreeingpayload789"
	rec.EvidenceReceipts = append(rec.EvidenceReceipts, first, second)
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())

	evidence := laneByName(t, result, LaneEvidence)
	if evidence.Status != closureprotocol.DimensionConflicted {
		t.Fatalf("evidence = %s %v, want conflicted", evidence.Status, evidence.ReasonCodes)
	}
	if result.Receipt.CertificationVerdict != closureprotocol.CertificationUncertifiable {
		t.Fatalf("verdict = %s, want uncertifiable", result.Receipt.CertificationVerdict)
	}
}

func TestRuntime_StaticTestWithMandateValidReceipt_Certifies(t *testing.T) {
	req, rec := greenBundle(t)
	rec = withRuntimeObligation(t, rec, true, closureprotocol.DimensionPass, []string{"receipt.runtime.core"})
	rec.EvidenceReceipts = append(rec.EvidenceReceipts, runtimeReceipt())
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())

	for _, lane := range result.Lanes {
		if lane.Status != closureprotocol.DimensionPass {
			t.Fatalf("lane %s = %s %v", lane.Lane, lane.Status, lane.ReasonCodes)
		}
	}
	if result.Receipt.CertificationVerdict != closureprotocol.Certified {
		t.Fatalf("verdict = %s, want certified", result.Receipt.CertificationVerdict)
	}
}

func TestRuntime_StaticTestRuntimeProfileWithoutReceipt_Blocks(t *testing.T) {
	// Opt-in profile: no governed mandate needed; the runtime profile itself
	// becomes required, and its absence blocks.
	req, rec := greenBundle(t)
	rec = withRuntimeObligation(t, rec, false, closureprotocol.DimensionBlocked, nil)
	req = rebindGreen(t, rec)
	req.PolicyID = PolicyRuntimeID
	result := mustEvaluate(t, req, rec, RuntimePolicy())

	evidence := laneByName(t, result, LaneEvidence)
	if evidence.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(evidence, ReasonEvidenceMissingRuntime) {
		t.Fatalf("evidence = %s %v", evidence.Status, evidence.ReasonCodes)
	}
	if result.Receipt.CertificationVerdict != closureprotocol.CertificationBlocked {
		t.Fatalf("verdict = %s", result.Receipt.CertificationVerdict)
	}
}

func TestRuntime_UnresolvableMandateFailsClosed(t *testing.T) {
	// The admission decision requires an obligation the bundle cannot
	// resolve: relaxation needs positive knowledge that no mandate exists, so
	// the runtime profile stays required and its absence blocks.
	req, rec := greenBundle(t)
	rec = withRuntimeObligation(t, rec, false, closureprotocol.DimensionNotApplicable, nil)
	rec.Obligations = nil // mandate now unknowable
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())

	evidence := laneByName(t, result, LaneEvidence)
	if evidence.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(evidence, ReasonEvidenceMissingRuntime) {
		t.Fatalf("evidence = %s %v, want fail-closed blocked", evidence.Status, evidence.ReasonCodes)
	}
	if result.Receipt.CertificationVerdict == closureprotocol.Certified {
		t.Fatal("unknown mandate must not certify")
	}
}
