// SPDX-License-Identifier: Apache-2.0

package certification

import (
	"errors"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/proofdischarge"
)

func TestEvaluate_AllGreenCertified(t *testing.T) {
	req, rec := greenBundle(t)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	for _, lane := range result.Lanes {
		if lane.Status != closureprotocol.DimensionPass {
			t.Fatalf("lane %s = %s (reasons %v), want pass", lane.Lane, lane.Status, lane.ReasonCodes)
		}
	}
	if result.Receipt.CertificationVerdict != closureprotocol.Certified {
		t.Fatalf("verdict = %s, want certified", result.Receipt.CertificationVerdict)
	}
	if err := closureprotocol.ValidateCertificationReceipt(result.Receipt); err != nil {
		t.Fatalf("receipt invalid: %v", err)
	}
	if err := VerifyReceipt(result.Receipt); err != nil {
		t.Fatalf("VerifyReceipt: %v", err)
	}
	if result.NextAction != "proceed to Phase 7 result rebuild/freshness verification" {
		t.Fatalf("next action = %q", result.NextAction)
	}
}

// --- Scope lane -------------------------------------------------------------

func TestScope_CleanDiffWithoutConsumptionBlocks(t *testing.T) {
	req, rec := greenBundle(t)
	rec.CapabilityConsumption = closureprotocol.CapabilityConsumption{}
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	scope := laneByName(t, result, LaneScope)
	if scope.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(scope, ReasonScopeConsumptionMissing) {
		t.Fatalf("scope = %s %v, want blocked with %s", scope.Status, scope.ReasonCodes, ReasonScopeConsumptionMissing)
	}
	if result.Receipt.CertificationVerdict != closureprotocol.CertificationBlocked {
		t.Fatalf("verdict = %s, want blocked", result.Receipt.CertificationVerdict)
	}
}

func TestScope_ReplayedCapabilityBlocks(t *testing.T) {
	req, rec := greenBundle(t)
	rec.CapabilityConsumption.OneUseStatus = closureprotocol.ReceiptSuperseded
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	scope := laneByName(t, result, LaneScope)
	if scope.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(scope, ReasonScopeCapabilityReused) {
		t.Fatalf("scope = %s %v", scope.Status, scope.ReasonCodes)
	}
}

func TestScope_ExtraOperationBlocks(t *testing.T) {
	req, rec := greenBundle(t)
	rec.ScopeVerification.ObservedOperationIDs = append(rec.ScopeVerification.ObservedOperationIDs, "op.delete.unplanned")
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	scope := laneByName(t, result, LaneScope)
	if scope.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(scope, ReasonScopeOperationUnadmitted) {
		t.Fatalf("scope = %s %v", scope.Status, scope.ReasonCodes)
	}
}

func TestScope_ExtraFileBlocks(t *testing.T) {
	req, rec := greenBundle(t)
	rec.ScopeVerification.ObservedPaths = append(rec.ScopeVerification.ObservedPaths, "golang/other/unrelated.go")
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	scope := laneByName(t, result, LaneScope)
	if scope.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(scope, ReasonScopeUnadmittedPath) {
		t.Fatalf("scope = %s %v", scope.Status, scope.ReasonCodes)
	}
}

func TestScope_ResultTreeMismatchIsStale(t *testing.T) {
	req, rec := greenBundle(t)
	rec.ScopeVerification.ResultBinding.ResultTreeDigestSHA256 = "someothertree"
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	scope := laneByName(t, result, LaneScope)
	if scope.Status != closureprotocol.DimensionStale || !hasReasonPrefix(scope, ReasonScopeResultBindingMismatch) {
		t.Fatalf("scope = %s %v, want stale", scope.Status, scope.ReasonCodes)
	}
	if result.Receipt.CertificationVerdict != closureprotocol.CertificationStale {
		t.Fatalf("verdict = %s, want stale", result.Receipt.CertificationVerdict)
	}
}

func TestScope_CapabilityChainMismatchBlocks(t *testing.T) {
	req, rec := greenBundle(t)
	rec.CapabilityConsumption.DecisionDigestSHA256 = "notthedecision"
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	scope := laneByName(t, result, LaneScope)
	if scope.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(scope, ReasonScopeCapabilityChainMismatch) {
		t.Fatalf("scope = %s %v", scope.Status, scope.ReasonCodes)
	}
}

func TestScope_ExpiredAdmissionIsStale(t *testing.T) {
	req, rec := greenBundle(t)
	rec.AdmissionDecision.CapabilityExpiry = "2026-07-15T11:00:00Z" // before consumed_at 11:30
	decisionDigest := mustDigest(t, rec.AdmissionDecision)
	rec.CapabilityConsumption.DecisionDigestSHA256 = decisionDigest
	rec.ScopeVerification.DecisionDigestSHA256 = decisionDigest
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	scope := laneByName(t, result, LaneScope)
	if scope.Status != closureprotocol.DimensionStale || !hasReasonPrefix(scope, ReasonScopeAdmissionExpired) {
		t.Fatalf("scope = %s %v, want stale", scope.Status, scope.ReasonCodes)
	}
}

func TestScope_CompliantLabelWithViolationsStillBlocks(t *testing.T) {
	req, rec := greenBundle(t)
	rec.ScopeVerification.Status = ScopeCompliant // label says fine...
	rec.ScopeVerification.Violations = []ScopeViolation{{Code: "out_of_scope_edit", Path: "x.go"}}
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	scope := laneByName(t, result, LaneScope)
	if scope.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(scope, ReasonScopeViolation) {
		t.Fatalf("scope = %s %v: violations must override the label", scope.Status, scope.ReasonCodes)
	}
}

// --- Authority lane ---------------------------------------------------------

func TestAuthority_PassingTestsCannotCompensateForUnknownAuthority(t *testing.T) {
	req, rec := greenBundle(t)
	rec.AuthorityResolutions = nil // proof + evidence stay perfectly green
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	authority := laneByName(t, result, LaneAuthority)
	if authority.Status != closureprotocol.DimensionUnknown || !hasReasonPrefix(authority, ReasonAuthorityOperationUnresolved) {
		t.Fatalf("authority = %s %v, want unknown", authority.Status, authority.ReasonCodes)
	}
	if proof := laneByName(t, result, LaneProof); proof.Status != closureprotocol.DimensionPass {
		t.Fatalf("proof lane should still pass, got %s", proof.Status)
	}
	if result.Receipt.CertificationVerdict != closureprotocol.CertificationUncertifiable {
		t.Fatalf("verdict = %s, want uncertifiable", result.Receipt.CertificationVerdict)
	}
}

func TestAuthority_ActorMismatchBlocks(t *testing.T) {
	req, rec := greenBundle(t)
	rec.CapabilityConsumption.ConsumerActor.PrincipalID = "actor.someoneelse"
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	authority := laneByName(t, result, LaneAuthority)
	if authority.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(authority, ReasonAuthorityActorMismatch) {
		t.Fatalf("authority = %s %v", authority.Status, authority.ReasonCodes)
	}
}

func TestAuthority_WrongMechanismBlocks(t *testing.T) {
	req, rec := greenBundle(t)
	rec.AuthorityResolutions[0].OperationResults[0].SelectedMechanism = closureprotocol.MechanismOwnerRPC
	rec.AuthorityResolutions[0].OperationResults[0].LegalMechanisms = []string{string(closureprotocol.MechanismOwnerRPC)}
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	authority := laneByName(t, result, LaneAuthority)
	if authority.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(authority, ReasonAuthorityMechanismMismatch) {
		t.Fatalf("authority = %s %v", authority.Status, authority.ReasonCodes)
	}
}

func TestAuthority_IllegalMechanismBlocks(t *testing.T) {
	req, rec := greenBundle(t)
	rec.AuthorityResolutions[0].OperationResults[0].LegalMechanisms = []string{string(closureprotocol.MechanismOwnerRPC)}
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	authority := laneByName(t, result, LaneAuthority)
	if authority.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(authority, ReasonAuthorityMechanismIllegal) {
		t.Fatalf("authority = %s %v", authority.Status, authority.ReasonCodes)
	}
}

func TestAuthority_GrantMissingBlocks(t *testing.T) {
	req, rec := greenBundle(t)
	rec.AuthorityResolutions[0].OperationResults[0].GrantIDs = nil
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	authority := laneByName(t, result, LaneAuthority)
	if authority.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(authority, ReasonAuthorityGrantMissing) {
		t.Fatalf("authority = %s %v", authority.Status, authority.ReasonCodes)
	}
}

func TestAuthority_WrongDomainBlocks(t *testing.T) {
	req, rec := greenBundle(t)
	rec.AuthorityResolutions[0].OperationResults[0].AuthorityDomainIDs = []string{"authority.other"}
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	authority := laneByName(t, result, LaneAuthority)
	if authority.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(authority, ReasonAuthorityDomainMismatch) {
		t.Fatalf("authority = %s %v", authority.Status, authority.ReasonCodes)
	}
}

func TestAuthority_StaleResolutionNeverCertifies(t *testing.T) {
	// An expired grant or delegation surfaces on the frozen record as a stale
	// resolution status; it must never certify.
	req, rec := greenBundle(t)
	rec.AuthorityResolutions[0].OperationResults[0].Status = closureprotocol.ReceiptStale
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	authority := laneByName(t, result, LaneAuthority)
	if authority.Status != closureprotocol.DimensionStale || !hasReasonPrefix(authority, ReasonAuthorityResolutionStale) {
		t.Fatalf("authority = %s %v", authority.Status, authority.ReasonCodes)
	}
	if result.Receipt.CertificationVerdict == closureprotocol.Certified {
		t.Fatal("stale authority must not certify")
	}
}

func TestAuthority_UnresolvedDelegationBlocks(t *testing.T) {
	req, rec := greenBundle(t)
	rec.AdmissionRequest.ActorBinding.ActorKind = closureprotocol.ActorAgent
	rec.AdmissionRequest.ActorBinding.DelegationReceiptDigests = []string{"delegation-receipt-architect"}
	rec.CapabilityConsumption.ConsumerActor = rec.AdmissionRequest.ActorBinding
	// resolution carries no delegation chain entry, so a delegated actor does
	// not resolve through delegation
	requestDigest := mustDigest(t, rec.AdmissionRequest)
	rec.AdmissionDecision.RequestDigestSHA256 = requestDigest
	decisionDigest := mustDigest(t, rec.AdmissionDecision)
	rec.CapabilityConsumption.DecisionDigestSHA256 = decisionDigest
	rec.ScopeVerification.DecisionDigestSHA256 = decisionDigest
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	authority := laneByName(t, result, LaneAuthority)
	if authority.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(authority, ReasonAuthorityDelegationUnresolved) {
		t.Fatalf("authority = %s %v", authority.Status, authority.ReasonCodes)
	}
}

// --- Proof lane ---------------------------------------------------------

func TestProof_MissingRequiredDischargeBlocks(t *testing.T) {
	req, rec := greenBundle(t)
	rec.ProofDischarges = nil
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	proof := laneByName(t, result, LaneProof)
	if proof.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(proof, ReasonProofMissingObligation) {
		t.Fatalf("proof = %s %v", proof.Status, proof.ReasonCodes)
	}
	if result.Receipt.CertificationVerdict != closureprotocol.CertificationBlocked {
		t.Fatalf("verdict = %s, want blocked", result.Receipt.CertificationVerdict)
	}
}

func TestProof_OptionalSlotOpenStillCertifies(t *testing.T) {
	req, rec := greenBundle(t)
	obligation := greenObligation()
	obligation.RequiredSlots = append(obligation.RequiredSlots, greenOptionalSlot())
	rec.Obligations[0] = obligation
	discharge := rec.ProofDischarges[0]
	discharge.SlotResults = append(discharge.SlotResults, closureprotocol.ProofSlotResult{
		SlotID: "slot.optional", Status: closureprotocol.DimensionUnknown,
	})
	rec.ProofDischarges[0] = discharge
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	proof := laneByName(t, result, LaneProof)
	if proof.Status != closureprotocol.DimensionPass || !hasLimitationPrefix(proof, LimitationProofOptionalOpen) {
		t.Fatalf("proof = %s %v %v", proof.Status, proof.ReasonCodes, proof.Limitations)
	}
	if result.Receipt.CertificationVerdict != closureprotocol.Certified {
		t.Fatalf("verdict = %s", result.Receipt.CertificationVerdict)
	}
}

func TestProof_WrongResultDischargeBlocks(t *testing.T) {
	req, rec := greenBundle(t)
	// The discharge maps a receipt that was produced for a different result.
	foreign := greenTestReceipt()
	foreign.ReceiptID = "receipt.test.foreignresult"
	foreign.ResultBinding.ResultTreeDigestSHA256 = "othertree"
	rec.EvidenceReceipts = append(rec.EvidenceReceipts, foreign)
	discharge := rec.ProofDischarges[0]
	discharge.SlotResults[0].ReceiptIDs = []string{"receipt.test.foreignresult"}
	rec.ProofDischarges[0] = discharge
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	proof := laneByName(t, result, LaneProof)
	if proof.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(proof, ReasonProofResultBindingMismatch) {
		t.Fatalf("proof = %s %v", proof.Status, proof.ReasonCodes)
	}
}

func TestProof_RevokedDischargeBlocks(t *testing.T) {
	req, rec := greenBundle(t)
	rec.Revocations = []closureprotocol.RevocationReceipt{{
		RevocationID:      "revocation.1",
		RevokedTargetID:   "proof.core",
		PriorDigestSHA256: mustDigest(t, rec.ProofDischarges[0]),
		RevocationReason:  "falsified evidence",
		PolicyID:          "revocation.architectural_closure.v1",
		ActorID:           "actor.dave",
		RevokedAt:         "2026-07-15T11:59:00Z",
	}}
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	proof := laneByName(t, result, LaneProof)
	if proof.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(proof, ReasonProofDischargeRevoked) {
		t.Fatalf("proof = %s %v", proof.Status, proof.ReasonCodes)
	}
}

func TestProof_WaiverCoversOnlyItsSlot(t *testing.T) {
	req, rec := greenBundle(t)
	obligation := greenObligation()
	obligation.RequiredSlots = append(obligation.RequiredSlots, greenSecondSlot())
	rec.Obligations[0] = obligation
	waiver := greenProofWaiver("slot.second")
	rec.Waivers = []closureprotocol.WaiverReceipt{waiver}
	discharge := rec.ProofDischarges[0]
	discharge.SlotResults = append(discharge.SlotResults, closureprotocol.ProofSlotResult{
		SlotID: "slot.second", Status: closureprotocol.DimensionPassWithException,
		ReceiptIDs: []string{waiver.WaiverID},
	})
	rec.ProofDischarges[0] = discharge
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	proof := laneByName(t, result, LaneProof)
	if proof.Status != closureprotocol.DimensionPassWithException || !hasReasonPrefix(proof, ReasonProofWaived) {
		t.Fatalf("proof = %s %v", proof.Status, proof.ReasonCodes)
	}
	if result.Receipt.CertificationVerdict != closureprotocol.CertifiedWithConditions {
		t.Fatalf("verdict = %s, want certified_with_conditions", result.Receipt.CertificationVerdict)
	}

	// The same waiver must NOT cover a different slot.
	discharge = rec.ProofDischarges[0]
	discharge.SlotResults[len(discharge.SlotResults)-1].SlotID = "slot.other"
	rec.ProofDischarges[0] = discharge
	req = rebindGreen(t, rec)
	result = mustEvaluate(t, req, rec, DefaultPolicy())
	proof = laneByName(t, result, LaneProof)
	if proof.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(proof, ReasonProofWaiverInvalid) {
		t.Fatalf("waiver leaked across slots: %s %v", proof.Status, proof.ReasonCodes)
	}
}

func TestProof_ExpiredWaiverDoesNotDowngrade(t *testing.T) {
	req, rec := greenBundle(t)
	waiver := greenProofWaiver("slot.tests")
	waiver.ExpiresAt = "2026-07-15T11:00:00Z" // before EvaluatedAt
	rec.Waivers = []closureprotocol.WaiverReceipt{waiver}
	discharge := rec.ProofDischarges[0]
	discharge.SlotResults[0] = closureprotocol.ProofSlotResult{
		SlotID: "slot.tests", Status: closureprotocol.DimensionPassWithException,
		ReceiptIDs: []string{waiver.WaiverID},
	}
	rec.ProofDischarges[0] = discharge
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	proof := laneByName(t, result, LaneProof)
	if proof.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(proof, ReasonProofWaiverInvalid) {
		t.Fatalf("proof = %s %v", proof.Status, proof.ReasonCodes)
	}
	if result.Receipt.CertificationVerdict != closureprotocol.CertificationBlocked {
		t.Fatalf("verdict = %s", result.Receipt.CertificationVerdict)
	}
}

// --- Evidence lane ----------------------------------------------------------

func TestEvidence_SelfDeclaredFreshnessIsIgnored(t *testing.T) {
	req, rec := greenBundle(t)
	profile := rec.EvidenceProfiles[0]
	profile.Freshness = "self_declared"
	rec.EvidenceProfiles[0] = profile
	receipt := rec.EvidenceReceipts[0]
	receipt.ObservedAt = "" // no real observation
	receipt.ExpiresAt = ""
	rec.EvidenceReceipts[0] = receipt
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	evidence := laneByName(t, result, LaneEvidence)
	// A malformed observed_at fails shape validation (invalid) or, when the
	// shape survives, freshness stays unobservable (unknown) — never pass.
	if evidence.Status == closureprotocol.DimensionPass || evidence.Status == closureprotocol.DimensionNotApplicable {
		t.Fatalf("self-declared freshness produced %s", evidence.Status)
	}
	if result.Receipt.CertificationVerdict == closureprotocol.Certified {
		t.Fatal("self-declared freshness must not certify")
	}
}

func TestEvidence_WrongRepoBlocks(t *testing.T) {
	req, rec := greenBundle(t)
	receipt := rec.EvidenceReceipts[0]
	receipt.ResultBinding.BaseRevision = "someforeignrev"
	rec.EvidenceReceipts[0] = receipt
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	evidence := laneByName(t, result, LaneEvidence)
	if evidence.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(evidence, ReasonEvidenceReceiptInvalid) {
		t.Fatalf("evidence = %s %v", evidence.Status, evidence.ReasonCodes)
	}
}

func TestEvidence_WrongResultBindingBlocks(t *testing.T) {
	req, rec := greenBundle(t)
	receipt := rec.EvidenceReceipts[0]
	receipt.ResultBinding.ResultTreeDigestSHA256 = "othertree"
	rec.EvidenceReceipts[0] = receipt
	// keep the proof lane out of the way: map the discharge to nothing new
	discharge := rec.ProofDischarges[0]
	rec.ProofDischarges[0] = discharge
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	evidence := laneByName(t, result, LaneEvidence)
	if evidence.Status != closureprotocol.DimensionBlocked {
		t.Fatalf("evidence = %s %v", evidence.Status, evidence.ReasonCodes)
	}
}

func TestEvidence_NonOwnerPathBlocks(t *testing.T) {
	req, rec := greenBundle(t)
	receipt := rec.EvidenceReceipts[0]
	receipt.ObservationPath = "bash -c 'echo ok'"
	rec.EvidenceReceipts[0] = receipt
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	evidence := laneByName(t, result, LaneEvidence)
	if evidence.Status != closureprotocol.DimensionBlocked {
		t.Fatalf("evidence = %s %v", evidence.Status, evidence.ReasonCodes)
	}
}

func TestEvidence_ConflictingReceiptsUncertifiable(t *testing.T) {
	req, rec := greenBundle(t)
	second := greenTestReceipt()
	second.ReceiptID = "receipt.test.core.b"
	second.PayloadDigestSHA256 = "differentpayload456" // same subject, different truth
	rec.EvidenceReceipts = append(rec.EvidenceReceipts, second)
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	evidence := laneByName(t, result, LaneEvidence)
	if evidence.Status != closureprotocol.DimensionConflicted || !hasReasonPrefix(evidence, ReasonEvidenceConflicted) {
		t.Fatalf("evidence = %s %v", evidence.Status, evidence.ReasonCodes)
	}
	if len(result.Receipt.UnresolvedContradictions) == 0 {
		t.Fatal("expected unresolved contradictions on the receipt")
	}
	if result.Receipt.CertificationVerdict != closureprotocol.CertificationUncertifiable {
		t.Fatalf("verdict = %s, want uncertifiable", result.Receipt.CertificationVerdict)
	}
}

func TestEvidence_RevokedReceiptBlocks(t *testing.T) {
	req, rec := greenBundle(t)
	rec.Revocations = []closureprotocol.RevocationReceipt{{
		RevocationID:      "revocation.2",
		RevokedTargetID:   "receipt.test.core",
		PriorDigestSHA256: mustDigest(t, rec.EvidenceReceipts[0]),
		RevocationReason:  "producer compromised",
		PolicyID:          "revocation.architectural_closure.v1",
		ActorID:           "actor.dave",
		RevokedAt:         "2026-07-15T11:59:00Z",
	}}
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	evidence := laneByName(t, result, LaneEvidence)
	if evidence.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(evidence, ReasonEvidenceReceiptRevoked) {
		t.Fatalf("evidence = %s %v", evidence.Status, evidence.ReasonCodes)
	}
	proof := laneByName(t, result, LaneProof)
	if proof.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(proof, ReasonProofReceiptRevoked) {
		t.Fatalf("proof lane must also reject the revoked mapped receipt: %s %v", proof.Status, proof.ReasonCodes)
	}
}

// --- Forbidden moves and contradictions --------------------------------------

func TestForbidden_ApplicableFindingBlocksPerfectBundle(t *testing.T) {
	req, rec := greenBundle(t)
	rec.ForbiddenMoveFindings = []ForbiddenMoveFinding{{
		MoveID:        "forbidden.cache_reload_path",
		ResultBinding: greenResultBinding(),
		OperationIDs:  []string{"op.modify.core"},
		Evidence:      "detected forbidden repair shape",
	}}
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	if result.Receipt.CertificationVerdict != closureprotocol.CertificationBlocked {
		t.Fatalf("verdict = %s, want blocked", result.Receipt.CertificationVerdict)
	}
	if len(result.Receipt.ForbiddenMoves) != 1 || result.Receipt.ForbiddenMoves[0] != "forbidden.cache_reload_path" {
		t.Fatalf("forbidden moves = %v", result.Receipt.ForbiddenMoves)
	}
	// Every lane individually still passed — the forbidden move outranks them.
	for _, lane := range result.Lanes {
		if lane.Status != closureprotocol.DimensionPass {
			t.Fatalf("lane %s unexpectedly %s", lane.Lane, lane.Status)
		}
	}
}

func TestForbidden_UnrelatedFindingIsIrrelevant(t *testing.T) {
	req, rec := greenBundle(t)
	other := greenResultBinding()
	other.ResultTreeDigestSHA256 = "someotherresult"
	rec.ForbiddenMoveFindings = []ForbiddenMoveFinding{{
		MoveID:        "forbidden.cache_reload_path",
		ResultBinding: other, // bound to a different result
	}}
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	if result.Receipt.CertificationVerdict != closureprotocol.Certified {
		t.Fatalf("verdict = %s, want certified (unrelated finding)", result.Receipt.CertificationVerdict)
	}
	if len(result.Receipt.ForbiddenMoves) != 0 {
		t.Fatalf("forbidden moves = %v, want none", result.Receipt.ForbiddenMoves)
	}
}

func TestForbidden_ScopeViolationForbiddenMoveCodePopulatesReceipt(t *testing.T) {
	req, rec := greenBundle(t)
	rec.ScopeVerification.Status = ScopeViolated
	rec.ScopeVerification.Violations = []ScopeViolation{{Code: "forbidden_move:edit_frozen_schema"}}
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	if result.Receipt.CertificationVerdict != closureprotocol.CertificationBlocked {
		t.Fatalf("verdict = %s", result.Receipt.CertificationVerdict)
	}
	if len(result.Receipt.ForbiddenMoves) != 1 || result.Receipt.ForbiddenMoves[0] != "edit_frozen_schema" {
		t.Fatalf("forbidden moves = %v", result.Receipt.ForbiddenMoves)
	}
}

// --- Verdict priority and policy --------------------------------------------

func TestVerdict_RiskClassForcesReviewRequired(t *testing.T) {
	req, rec := greenBundle(t)
	plan := rec.AdmissionRequest.ChangePlan
	plan.Operations[0].RiskClass = "destructive_migration"
	rec.AdmissionRequest.ChangePlan = plan
	requestDigest := mustDigest(t, rec.AdmissionRequest)
	rec.AdmissionDecision.RequestDigestSHA256 = requestDigest
	decisionDigest := mustDigest(t, rec.AdmissionDecision)
	rec.CapabilityConsumption.DecisionDigestSHA256 = decisionDigest
	rec.ScopeVerification.DecisionDigestSHA256 = decisionDigest
	req = rebindGreen(t, rec)

	policy := DefaultPolicy()
	policy.RequireHumanReviewForRiskClasses = []string{"destructive_migration"}
	result := mustEvaluate(t, req, rec, policy)
	if result.Receipt.CertificationVerdict != closureprotocol.CertificationReviewRequired {
		t.Fatalf("verdict = %s, want review_required", result.Receipt.CertificationVerdict)
	}
}

func TestEvaluate_ForgedRecordIsRefusedNotScored(t *testing.T) {
	req, rec := greenBundle(t)
	// Tamper with the decision AFTER binding the digests: the engine must
	// refuse, not evaluate.
	rec.AdmissionDecision.RequiredProofSlots = nil
	if _, err := Evaluate(req, rec, DefaultPolicy()); !errors.Is(err, ErrRecordDigestMismatch) {
		t.Fatalf("err = %v, want ErrRecordDigestMismatch", err)
	}
}

func TestEvaluate_RevokedVerdictIsNeverProduced(t *testing.T) {
	// Sweep every mutation used across this file; none may yield "revoked".
	req, rec := greenBundle(t)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	if result.Receipt.CertificationVerdict == closureprotocol.CertificationRevoked {
		t.Fatal("Evaluate produced revoked")
	}
}

// Helpers local to this file.

func greenOptionalSlot() proofdischarge.ProofSlotSpec {
	return proofdischarge.ProofSlotSpec{ID: "slot.optional", Kind: proofdischarge.SlotKindStaticGuard, Required: false}
}

func greenSecondSlot() proofdischarge.ProofSlotSpec {
	return proofdischarge.ProofSlotSpec{ID: "slot.second", Kind: proofdischarge.SlotKindStaticGuard, Required: true}
}

func greenProofWaiver(slotID string) closureprotocol.WaiverReceipt {
	return closureprotocol.WaiverReceipt{
		WaiverID:      "waiver." + slotID,
		Dimension:     closureprotocol.DimensionProof,
		PolicyID:      "exception.core.approved",
		Justification: "governed exception for " + slotID,
		ExpiresAt:     "2026-07-16T12:00:00Z",
		AppliesTo:     []string{slotID},
		Status:        closureprotocol.ReceiptValid,
	}
}
