// SPDX-License-Identifier: Apache-2.0

package certification

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/proofdischarge"
)

// Product proofs (Part Q): three end-to-end fixture tasks driven through the
// full ledger-integrated flow (seed ledger -> content-addressed records ->
// typed request -> CertifyTask).

// foreignize rebases the green bundle onto a foreign repository domain — the
// engine must behave identically for a repository Sensei merely imported.
func foreignize(t *testing.T, rec Records) Records {
	t.Helper()
	rec.AdmissionRequest.BaseBinding.Repository.Domain = "github.com/example/orleans-clone"
	requestDigest := mustDigest(t, rec.AdmissionRequest)
	rec.AdmissionDecision.RequestDigestSHA256 = requestDigest
	decisionDigest := mustDigest(t, rec.AdmissionDecision)
	rec.CapabilityConsumption.DecisionDigestSHA256 = decisionDigest
	rec.ScopeVerification.DecisionDigestSHA256 = decisionDigest
	return rec
}

// Proof A: a foreign repository under the default static_test policy with no
// governed runtime mandate and a valid static+test proof certifies, and the
// runtime evidence profile is documented not_applicable with a reason — never
// silently dropped, never uncertifiable.
func TestProductProof_ForeignRepoStaticTest(t *testing.T) {
	_, rec := greenBundle(t)
	rec = foreignize(t, rec)
	rec = withRuntimeObligation(t, rec, false, closureprotocol.DimensionNotApplicable, nil)
	req := rebindGreen(t, rec)

	taskDir, head := seedTaskDir(t, req, rec)
	res, err := CertifyTask(TaskCertifyOptions{
		TaskDir:                  taskDir,
		ExpectedHeadDigestSHA256: head,
		ProducedAt:               testProducedAt,
	})
	if err != nil {
		t.Fatalf("CertifyTask: %v", err)
	}
	receipt := res.Result.Receipt
	if receipt.CertificationVerdict != closureprotocol.Certified {
		t.Fatalf("verdict = %s, want certified; limitations %v", receipt.CertificationVerdict, receipt.Limitations)
	}
	if !res.Appended {
		t.Fatal("certified event not appended")
	}
	wantReason := LimitationEvidenceRuntimeNotApplicable + ":profile.runtime.core"
	found := false
	for _, limitation := range receipt.Limitations {
		if limitation == wantReason {
			found = true
		}
	}
	if !found {
		t.Fatalf("runtime not_applicable reason missing from receipt: %v", receipt.Limitations)
	}
}

// leaderElectionObligation is a claim static analysis and tests cannot
// honestly establish: the governed obligation mandates runtime evidence.
func leaderElectionObligation() proofdischarge.ProofObligation {
	return proofdischarge.ProofObligation{
		ID:                      "proof.leader_election",
		Status:                  "approved",
		RequiresRuntimeEvidence: true,
		RequiredSlots: []proofdischarge.ProofSlotSpec{
			{ID: "slot.tests", Kind: proofdischarge.SlotKindTestOrRuntime, Required: true},
			{ID: "slot.runtime", Kind: proofdischarge.SlotKindRuntime, Required: true},
		},
	}
}

func leaderElectionBundle(t *testing.T, runtimeSlot closureprotocol.ProofSlotResult, withReceipt bool) (Request, Records) {
	t.Helper()
	_, rec := greenBundle(t)
	rec.Obligations = []proofdischarge.ProofObligation{leaderElectionObligation()}
	rec.AdmissionDecision.RequiredProofSlots = []string{"proof.leader_election"}
	rec.AdmissionDecision.RequiredEvidenceProfiles = []string{"profile.test.core", "profile.runtime.core"}
	rec.EvidenceProfiles = append(rec.EvidenceProfiles, runtimeProfile())
	rec.RuntimeTarget = runtimeTarget()
	if withReceipt {
		rec.EvidenceReceipts = append(rec.EvidenceReceipts, runtimeReceipt())
	}
	rec.ProofDischarges = []closureprotocol.ProofDischarge{{
		ObligationID: "proof.leader_election",
		Status:       closureprotocol.ReceiptValid,
		SlotResults: []closureprotocol.ProofSlotResult{
			{SlotID: "slot.tests", Status: closureprotocol.DimensionPass, ReceiptIDs: []string{"receipt.test.core"}},
			runtimeSlot,
		},
		MappedEvidence: []string{"receipt.test.core"},
	}}
	decisionDigest := mustDigest(t, rec.AdmissionDecision)
	rec.CapabilityConsumption.DecisionDigestSHA256 = decisionDigest
	rec.ScopeVerification.DecisionDigestSHA256 = decisionDigest
	return rebindGreen(t, rec), rec
}

// Proof B: a runtime-mandated behavior (leader election) without a compatible
// runtime receipt must NOT certify under static_test; the runtime slot stays
// required and the next action names obtaining compatible runtime evidence.
func TestProductProof_RuntimeMandatedWithoutEvidence(t *testing.T) {
	req, rec := leaderElectionBundle(t, closureprotocol.ProofSlotResult{
		SlotID: "slot.runtime", Status: closureprotocol.DimensionBlocked,
	}, false)

	taskDir, head := seedTaskDir(t, req, rec)
	res, err := CertifyTask(TaskCertifyOptions{
		TaskDir:                  taskDir,
		ExpectedHeadDigestSHA256: head,
		ProducedAt:               testProducedAt,
	})
	if err != nil {
		t.Fatalf("CertifyTask: %v", err)
	}
	receipt := res.Result.Receipt
	if receipt.CertificationVerdict != closureprotocol.CertificationBlocked {
		t.Fatalf("verdict = %s, want blocked", receipt.CertificationVerdict)
	}
	if res.Appended {
		t.Fatal("blocked evaluation must not append a certified event")
	}
	proof := laneByName(t, res.Result, LaneProof)
	if proof.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(proof, ReasonProofMissingSlot) {
		t.Fatalf("runtime slot not enforced: %s %v", proof.Status, proof.ReasonCodes)
	}
	evidence := laneByName(t, res.Result, LaneEvidence)
	if evidence.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(evidence, ReasonEvidenceMissingRuntime) {
		t.Fatalf("runtime evidence not required: %s %v", evidence.Status, evidence.ReasonCodes)
	}
	if !strings.Contains(res.Result.NextAction, "runtime evidence") {
		t.Fatalf("next action = %q, want runtime-evidence guidance", res.Result.NextAction)
	}
}

// Proof C: the same mandated behavior with a valid, owner-path, correctly
// bound runtime receipt certifies, and the runtime slot is discharged.
func TestProductProof_RuntimeMandatedWithOwnerPathEvidence(t *testing.T) {
	req, rec := leaderElectionBundle(t, closureprotocol.ProofSlotResult{
		SlotID: "slot.runtime", Status: closureprotocol.DimensionPass,
		ReceiptIDs: []string{"receipt.runtime.core"},
	}, true)

	taskDir, head := seedTaskDir(t, req, rec)
	res, err := CertifyTask(TaskCertifyOptions{
		TaskDir:                  taskDir,
		ExpectedHeadDigestSHA256: head,
		ProducedAt:               testProducedAt,
	})
	if err != nil {
		t.Fatalf("CertifyTask: %v", err)
	}
	receipt := res.Result.Receipt
	if receipt.CertificationVerdict != closureprotocol.Certified {
		t.Fatalf("verdict = %s, want certified; lanes %+v", receipt.CertificationVerdict, res.Result.Lanes)
	}
	if !res.Appended {
		t.Fatal("certified event not appended")
	}
	for _, lane := range res.Result.Lanes {
		if lane.Status != closureprotocol.DimensionPass {
			t.Fatalf("lane %s = %s %v", lane.Lane, lane.Status, lane.ReasonCodes)
		}
	}
}
