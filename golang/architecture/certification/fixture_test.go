// SPDX-License-Identifier: AGPL-3.0-only

package certification

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/proofdischarge"
)

// The shared "all green" bundle: one admitted modify operation, a consumed
// single-use capability, a compliant scope verification, a valid per-operation
// authority resolution, one governed proof obligation discharged by a valid
// test receipt, and one required test evidence profile satisfied by the same
// receipt. Every digest reference is real (recomputed from the records).
const (
	greenTaskID      = "task.green"
	greenSessionID   = "session.green"
	greenEvaluatedAt = "2026-07-15T12:00:00Z"
	greenDomain      = "github.com/globulario/sensei"
)

func greenResultBinding() closureprotocol.ResultBinding {
	return closureprotocol.ResultBinding{
		BaseRevision:           "baserev123",
		PatchDigestSHA256:      "patch123",
		ResultTreeDigestSHA256: "tree123",
		ResultRevision:         "resultrev456",
		GraphDigestSHA256:      "resultgraph123",
	}
}

func greenActor() closureprotocol.ActorBinding {
	return closureprotocol.ActorBinding{
		PrincipalID: "actor.dave",
		ActorKind:   closureprotocol.ActorHuman,
		Roles:       []string{"owner"},
		Issuer:      "local-review",
	}
}

func greenPlan() closureprotocol.ChangePlan {
	return closureprotocol.ChangePlan{
		PlanID: "plan.green",
		Operations: []closureprotocol.ChangeOperation{{
			OperationID:        "op.modify.core",
			Kind:               closureprotocol.OperationModify,
			TargetKind:         "file",
			Target:             "golang/core/model.go",
			AuthorityDomainIDs: []string{"authority.core"},
			SelectedMechanism:  closureprotocol.MechanismRepositoryEdit,
			IntendedEffect:     "close architectural behavior gap",
		}},
	}
}

func greenBaseBinding(domain string) closureprotocol.BaseBinding {
	return closureprotocol.BaseBinding{
		Repository: closureprotocol.RepositorySnapshot{
			Domain:           domain,
			Revision:         "baserev123",
			RevisionStatus:   "resolved",
			TreeDigestSHA256: "basetree123",
		},
		Graph: closureprotocol.GraphSnapshot{
			DigestSHA256:  "basegraph123",
			DigestStatus:  "resolved",
			SchemaVersion: "awareness-ontology/0.2",
		},
		Task: closureprotocol.TaskBinding{ID: greenTaskID, SessionID: greenSessionID},
		Policies: closureprotocol.PolicyBinding{
			Admission:        "admission.strict.v2",
			Certification:    PolicyDefaultID,
			Completion:       "completion.architectural_closure.v1",
			Revocation:       "revocation.architectural_closure.v1",
			Ledger:           "ledger.task.v1",
			Canonicalization: "canonicalization.architectural_closure.v1",
		},
	}
}

func greenObligation() proofdischarge.ProofObligation {
	return proofdischarge.ProofObligation{
		ID:     "proof.core",
		Status: "approved",
		RequiredSlots: []proofdischarge.ProofSlotSpec{{
			ID:       "slot.tests",
			Kind:     proofdischarge.SlotKindTestOrRuntime,
			Required: true,
		}},
	}
}

func greenTestProfile() closureprotocol.EvidenceProfile {
	return closureprotocol.EvidenceProfile{
		ProfileID:            "profile.test.core",
		Owner:                "component.core",
		LegalObservationPath: "test_runner.go_test",
		EvidenceKind:         closureprotocol.EvidenceTest,
		Freshness:            "per-result",
		Trust:                "high",
		Status:               closureprotocol.ReceiptValid,
	}
}

func greenTestReceipt() closureprotocol.EvidenceReceipt {
	return closureprotocol.EvidenceReceipt{
		ReceiptID:           "receipt.test.core",
		EvidenceKind:        closureprotocol.EvidenceTest,
		ProfileID:           "profile.test.core",
		ResultBinding:       greenResultBinding(),
		Producer:            "ci.local",
		ObservationPath:     "go_test",
		ObservedAt:          "2026-07-15T11:45:00Z",
		ExpiresAt:           "2026-07-16T11:45:00Z",
		Status:              closureprotocol.ReceiptValid,
		Trust:               "high",
		PayloadDigestSHA256: "payload123",
	}
}

// greenBundle builds the fully consistent Request+Records pair. Mutate the
// returned Records, then call rebindGreen to recompute the digest references
// (so the mutation itself is honestly referenced, exercising the lane logic
// rather than the digest bijection).
func greenBundle(t *testing.T) (Request, Records) {
	t.Helper()
	rec := Records{
		AdmissionRequest: closureprotocol.AdmissionRequest{
			ActorBinding: greenActor(),
			BaseBinding:  greenBaseBinding(greenDomain),
			ChangePlan:   greenPlan(),
			PolicyID:     "admission.strict.v2",
		},
		Obligations:      []proofdischarge.ProofObligation{greenObligation()},
		EvidenceProfiles: []closureprotocol.EvidenceProfile{greenTestProfile()},
		EvidenceReceipts: []closureprotocol.EvidenceReceipt{greenTestReceipt()},
	}

	requestDigest := mustDigest(t, rec.AdmissionRequest)
	rec.AdmissionDecision = closureprotocol.AdmissionDecision{
		DecisionID:               "decision.green",
		RequestDigestSHA256:      requestDigest,
		PolicyID:                 "admission.strict.v2",
		OperationVerdicts:        []closureprotocol.OperationAdmissionVerdict{{OperationID: "op.modify.core", Verdict: "admitted"}},
		CapabilityID:             "cap.green",
		CapabilityExpiry:         "2026-07-15T13:00:00Z",
		RequiredProofSlots:       []string{"proof.core"},
		RequiredEvidenceProfiles: []string{"profile.test.core"},
		CompletionPolicyID:       "completion.architectural_closure.v1",
	}
	decisionDigest := mustDigest(t, rec.AdmissionDecision)
	rec.CapabilityConsumption = closureprotocol.CapabilityConsumption{
		CapabilityID:         "cap.green",
		Task:                 closureprotocol.TaskBinding{ID: greenTaskID, SessionID: greenSessionID},
		ConsumerActor:        greenActor(),
		ConsumedOperationIDs: []string{"op.modify.core"},
		ConsumedAt:           "2026-07-15T11:30:00Z",
		DecisionDigestSHA256: decisionDigest,
		OneUseStatus:         closureprotocol.ReceiptValid,
	}
	rec.ScopeVerification = ScopeVerification{
		DecisionDigestSHA256: decisionDigest,
		ResultBinding:        greenResultBinding(),
		ObservedPaths:        []string{"golang/core/model.go"},
		ObservedOperationIDs: []string{"op.modify.core"},
		Status:               ScopeCompliant,
		VerifiedAt:           "2026-07-15T11:50:00Z",
	}
	rec.AuthorityResolutions = []closureprotocol.AuthorityResolution{{
		OperationID:        "op.modify.core",
		Status:             closureprotocol.ReceiptValid,
		AuthorityDomainIDs: []string{"authority.core"},
		GrantIDs:           []string{"grant.core.owner"},
		LegalMechanisms:    []string{string(closureprotocol.MechanismRepositoryEdit)},
		SelectedMechanism:  closureprotocol.MechanismRepositoryEdit,
	}}
	rec.ProofDischarges = []closureprotocol.ProofDischarge{{
		ObligationID: "proof.core",
		Status:       closureprotocol.ReceiptValid,
		SlotResults: []closureprotocol.ProofSlotResult{{
			SlotID:     "slot.tests",
			Status:     closureprotocol.DimensionPass,
			ReceiptIDs: []string{"receipt.test.core"},
		}},
		MappedEvidence: []string{"receipt.test.core"},
	}}

	return rebindGreen(t, rec), rec
}

// rebindGreen recomputes every digest reference on the request from the
// current records so Request and Records stay a verifiable pair.
func rebindGreen(t *testing.T, rec Records) Request {
	t.Helper()
	req := Request{
		TaskID:        greenTaskID,
		PolicyID:      PolicyDefaultID,
		EvaluatedAt:   greenEvaluatedAt,
		ResultBinding: greenResultBinding(),
	}
	if rec.AdmissionRequest.PolicyID != "" || len(rec.AdmissionRequest.ChangePlan.Operations) > 0 {
		req.AdmissionRequestDigestSHA256 = mustDigest(t, rec.AdmissionRequest)
	}
	if rec.AdmissionDecision.CapabilityID != "" || len(rec.AdmissionDecision.OperationVerdicts) > 0 {
		req.AdmissionDecisionDigestSHA256 = mustDigest(t, rec.AdmissionDecision)
	}
	if rec.CapabilityConsumption.CapabilityID != "" {
		req.CapabilityConsumptionDigestSHA256 = mustDigest(t, rec.CapabilityConsumption)
	}
	if rec.ScopeVerification.Status != "" {
		req.ScopeVerificationDigestSHA256 = mustDigest(t, rec.ScopeVerification)
	}
	if rec.RuntimeTarget != nil {
		req.RuntimeTargetDigestSHA256 = mustDigest(t, *rec.RuntimeTarget)
	}
	for _, record := range rec.AuthorityResolutions {
		req.AuthorityResolutionDigests = append(req.AuthorityResolutionDigests, mustDigest(t, record))
	}
	for _, record := range rec.ProofDischarges {
		req.ProofDischargeDigests = append(req.ProofDischargeDigests, mustDigest(t, record))
	}
	for _, record := range rec.Obligations {
		req.ProofObligationDigests = append(req.ProofObligationDigests, mustDigest(t, record))
	}
	for _, record := range rec.EvidenceProfiles {
		req.EvidenceProfileDigests = append(req.EvidenceProfileDigests, mustDigest(t, record))
	}
	for _, record := range rec.EvidenceReceipts {
		req.EvidenceReceiptDigests = append(req.EvidenceReceiptDigests, mustDigest(t, record))
	}
	for _, record := range rec.ArtifactReceipts {
		req.ArtifactReceiptDigests = append(req.ArtifactReceiptDigests, mustDigest(t, record))
	}
	for _, record := range rec.Waivers {
		req.WaiverDigests = append(req.WaiverDigests, mustDigest(t, record))
	}
	for _, record := range rec.Revocations {
		req.RevocationDigests = append(req.RevocationDigests, mustDigest(t, record))
	}
	for _, record := range rec.ForbiddenMoveFindings {
		req.ForbiddenMoveFindingDigests = append(req.ForbiddenMoveFindingDigests, mustDigest(t, record))
	}
	return req
}

func mustDigest(t *testing.T, record any) string {
	t.Helper()
	digest, err := recordDigest(record)
	if err != nil {
		t.Fatalf("record digest: %v", err)
	}
	return digest
}

func mustEvaluate(t *testing.T, req Request, rec Records, policy CertificationPolicy) Result {
	t.Helper()
	result, err := Evaluate(req, rec, policy)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	return result
}

func laneByName(t *testing.T, result Result, lane Lane) LaneResult {
	t.Helper()
	for _, l := range result.Lanes {
		if l.Lane == lane {
			return l
		}
	}
	t.Fatalf("lane %s not found", lane)
	return LaneResult{}
}

func hasReasonPrefix(lane LaneResult, prefix string) bool {
	for _, reason := range lane.ReasonCodes {
		if reason == prefix || len(reason) > len(prefix) && reason[:len(prefix)+1] == prefix+":" {
			return true
		}
	}
	return false
}

func hasLimitationPrefix(lane LaneResult, prefix string) bool {
	for _, code := range lane.Limitations {
		if code == prefix || len(code) > len(prefix) && code[:len(prefix)+1] == prefix+":" {
			return true
		}
	}
	return false
}

// Runtime-mandate fixtures shared by policy_test.go and product_test.go.

func runtimeProfile() closureprotocol.EvidenceProfile {
	return closureprotocol.EvidenceProfile{
		ProfileID:            "profile.runtime.core",
		Owner:                "component.core",
		LegalObservationPath: "runtime_adapter.snapshot",
		EvidenceKind:         closureprotocol.EvidenceRuntime,
		Freshness:            "per-result",
		Trust:                "high",
		RuntimeTargetKind:    "service",
		Status:               closureprotocol.ReceiptValid,
	}
}

func runtimeTarget() *closureprotocol.RuntimeTarget {
	return &closureprotocol.RuntimeTarget{
		Platform:      "globular",
		EnvironmentID: "cluster.green",
		DeploymentID:  "deployment.core",
	}
}

func runtimeReceipt() closureprotocol.EvidenceReceipt {
	target := *runtimeTarget()
	return closureprotocol.EvidenceReceipt{
		ReceiptID:           "receipt.runtime.core",
		EvidenceKind:        closureprotocol.EvidenceRuntime,
		ProfileID:           "profile.runtime.core",
		ResultBinding:       greenResultBinding(),
		RuntimeTarget:       &target,
		Producer:            "runtime.owner",
		ObservationPath:     "snapshot",
		ObservedAt:          "2026-07-15T11:55:00Z",
		ExpiresAt:           "2026-07-16T11:55:00Z",
		Status:              closureprotocol.ReceiptValid,
		Trust:               "high",
		PayloadDigestSHA256: "runtimepayload123",
	}
}

// withRuntimeObligation rewires the green bundle so the governed obligation
// carries a runtime slot (mandated toggles RequiresRuntimeEvidence) and the
// admission decision requires the runtime evidence profile as well.
func withRuntimeObligation(t *testing.T, rec Records, mandated bool, slotStatus closureprotocol.DimensionStatus, slotReceipts []string) Records {
	t.Helper()
	obligation := greenObligation()
	obligation.RequiresRuntimeEvidence = mandated
	obligation.RequiredSlots = append(obligation.RequiredSlots, proofdischarge.ProofSlotSpec{
		ID:       "slot.runtime",
		Kind:     proofdischarge.SlotKindRuntime,
		Required: true,
	})
	rec.Obligations = []proofdischarge.ProofObligation{obligation}

	discharge := rec.ProofDischarges[0]
	discharge.SlotResults = append([]closureprotocol.ProofSlotResult{}, discharge.SlotResults...)
	discharge.SlotResults = append(discharge.SlotResults, closureprotocol.ProofSlotResult{
		SlotID:     "slot.runtime",
		Status:     slotStatus,
		ReceiptIDs: slotReceipts,
	})
	rec.ProofDischarges = []closureprotocol.ProofDischarge{discharge}

	rec.EvidenceProfiles = append(rec.EvidenceProfiles, runtimeProfile())
	rec.AdmissionDecision.RequiredEvidenceProfiles = append(
		append([]string{}, rec.AdmissionDecision.RequiredEvidenceProfiles...), "profile.runtime.core")
	rec.RuntimeTarget = runtimeTarget()

	// The decision changed, so the downstream chain references must follow.
	decisionDigest := mustDigest(t, rec.AdmissionDecision)
	rec.CapabilityConsumption.DecisionDigestSHA256 = decisionDigest
	rec.ScopeVerification.DecisionDigestSHA256 = decisionDigest
	return rec
}
