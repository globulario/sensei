// SPDX-License-Identifier: AGPL-3.0-only

package certification

import (
	"fmt"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/proofdischarge"
)

// ValidateRequest checks the request shape. It is deliberately shape-only —
// nothing here decides an outcome. The result binding must be a complete
// binding produced by earlier phases (base revision, patch digest, result tree
// digest, and result graph digest all present); the engine never fabricates or
// completes one.
func ValidateRequest(req Request) error {
	if strings.TrimSpace(req.TaskID) == "" {
		return fmt.Errorf("%w: task_id is required", ErrRequestInvalid)
	}
	if strings.TrimSpace(req.PolicyID) == "" {
		return fmt.Errorf("%w: policy_id is required", ErrRequestInvalid)
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(req.EvaluatedAt)); err != nil {
		return fmt.Errorf("%w: evaluated_at must be RFC3339", ErrRequestInvalid)
	}
	if err := binding.ValidateResult(req.ResultBinding); err != nil {
		return fmt.Errorf("%w: result binding: %v", ErrRequestInvalid, err)
	}
	return nil
}

// Record digests. Records that carry a self-digest field are digested with
// that field cleared (the frozen "a receipt digest field is omitted from the
// digest of that same receipt" law, via the frozen helpers); all others use
// the frozen semantic digest directly.

func admissionRequestDigest(in closureprotocol.AdmissionRequest) (string, error) {
	return closureprotocol.SemanticDigest(in)
}

func admissionDecisionDigest(in closureprotocol.AdmissionDecision) (string, error) {
	return closureprotocol.SemanticDigest(in)
}

func capabilityConsumptionDigest(in closureprotocol.CapabilityConsumption) (string, error) {
	return closureprotocol.SemanticDigest(in)
}

func scopeVerificationDigest(in ScopeVerification) (string, error) {
	return closureprotocol.SemanticDigest(in)
}

func waiverReceiptDigest(in closureprotocol.WaiverReceipt) (string, error) {
	copy := in
	copy.DigestSHA256 = ""
	return closureprotocol.SemanticDigest(copy)
}

// VerifyRecords proves that every resolved record is exactly the record the
// request references: each record must reproduce its claimed digest, every
// claimed digest must be backed by exactly one record, and no record may ride
// along unreferenced. A mismatch is a refusal (error), never a down-scored
// lane — the engine does not evaluate forged inputs.
func VerifyRecords(req Request, rec Records) error {
	// Singletons. A referenced digest with a zero-value record is missing; a
	// present record with no reference is unreferenced; both refuse. An
	// entirely absent pair (no digest, zero record) is legal here — the lanes
	// fail closed on missing required sources.
	if err := verifySingle("admission_request", req.AdmissionRequestDigestSHA256,
		rec.AdmissionRequest.PolicyID != "" || len(rec.AdmissionRequest.ChangePlan.Operations) > 0,
		func() (string, error) { return admissionRequestDigest(rec.AdmissionRequest) }); err != nil {
		return err
	}
	if err := verifySingle("admission_decision", req.AdmissionDecisionDigestSHA256,
		rec.AdmissionDecision.CapabilityID != "" || len(rec.AdmissionDecision.OperationVerdicts) > 0,
		func() (string, error) { return admissionDecisionDigest(rec.AdmissionDecision) }); err != nil {
		return err
	}
	if err := verifySingle("capability_consumption", req.CapabilityConsumptionDigestSHA256,
		rec.CapabilityConsumption.CapabilityID != "",
		func() (string, error) { return capabilityConsumptionDigest(rec.CapabilityConsumption) }); err != nil {
		return err
	}
	if err := verifySingle("scope_verification", req.ScopeVerificationDigestSHA256,
		rec.ScopeVerification.Status != "",
		func() (string, error) { return scopeVerificationDigest(rec.ScopeVerification) }); err != nil {
		return err
	}
	if rec.RuntimeTarget != nil || strings.TrimSpace(req.RuntimeTargetDigestSHA256) != "" {
		present := rec.RuntimeTarget != nil
		if err := verifySingle("runtime_target", req.RuntimeTargetDigestSHA256, present, func() (string, error) {
			return closureprotocol.SemanticDigest(*rec.RuntimeTarget)
		}); err != nil {
			return err
		}
	}

	// Collections: strict bijection between claimed digests and records.
	if err := verifySet("authority_resolution", req.AuthorityResolutionDigests, len(rec.AuthorityResolutions), func(i int) (string, error) {
		return closureprotocol.AuthorityResolutionDigest(rec.AuthorityResolutions[i])
	}); err != nil {
		return err
	}
	if err := verifySet("delegation_receipt", req.DelegationReceiptDigests, len(rec.DelegationReceipts), func(i int) (string, error) {
		return closureprotocol.DelegationReceiptDigest(rec.DelegationReceipts[i])
	}); err != nil {
		return err
	}
	if err := verifySet("proof_discharge", req.ProofDischargeDigests, len(rec.ProofDischarges), func(i int) (string, error) {
		return closureprotocol.ProofDischargeDigest(rec.ProofDischarges[i])
	}); err != nil {
		return err
	}
	if err := verifySet("proof_obligation", req.ProofObligationDigests, len(rec.Obligations), func(i int) (string, error) {
		return closureprotocol.SemanticDigest(rec.Obligations[i])
	}); err != nil {
		return err
	}
	if err := verifySet("evidence_profile", req.EvidenceProfileDigests, len(rec.EvidenceProfiles), func(i int) (string, error) {
		return closureprotocol.SemanticDigest(rec.EvidenceProfiles[i])
	}); err != nil {
		return err
	}
	if err := verifySet("evidence_receipt", req.EvidenceReceiptDigests, len(rec.EvidenceReceipts), func(i int) (string, error) {
		return closureprotocol.SemanticDigest(rec.EvidenceReceipts[i])
	}); err != nil {
		return err
	}
	if err := verifySet("artifact_receipt", req.ArtifactReceiptDigests, len(rec.ArtifactReceipts), func(i int) (string, error) {
		return closureprotocol.SemanticDigest(rec.ArtifactReceipts[i])
	}); err != nil {
		return err
	}
	if err := verifySet("waiver", req.WaiverDigests, len(rec.Waivers), func(i int) (string, error) {
		return waiverReceiptDigest(rec.Waivers[i])
	}); err != nil {
		return err
	}
	if err := verifySet("revocation", req.RevocationDigests, len(rec.Revocations), func(i int) (string, error) {
		return closureprotocol.SemanticDigest(rec.Revocations[i])
	}); err != nil {
		return err
	}
	if err := verifySet("forbidden_move_finding", req.ForbiddenMoveFindingDigests, len(rec.ForbiddenMoveFindings), func(i int) (string, error) {
		return closureprotocol.SemanticDigest(rec.ForbiddenMoveFindings[i])
	}); err != nil {
		return err
	}
	return nil
}

func verifySingle(kind, claimed string, present bool, digest func() (string, error)) error {
	claimed = strings.TrimSpace(claimed)
	if claimed == "" && !present {
		return nil
	}
	if claimed == "" {
		return fmt.Errorf("%w: %s", ErrRecordUnreferenced, kind)
	}
	if !present {
		return fmt.Errorf("%w: %s %s", ErrRecordMissing, kind, claimed)
	}
	actual, err := digest()
	if err != nil {
		return err
	}
	if actual != claimed {
		return fmt.Errorf("%w: %s (claimed %s, actual %s)", ErrRecordDigestMismatch, kind, claimed, actual)
	}
	return nil
}

func verifySet(kind string, claimed []string, count int, digest func(int) (string, error)) error {
	want := map[string]bool{}
	for _, d := range closureprotocol.NormalizeSet(claimed) {
		want[d] = false
	}
	for i := 0; i < count; i++ {
		actual, err := digest(i)
		if err != nil {
			return err
		}
		if _, ok := want[actual]; !ok {
			return fmt.Errorf("%w: %s %s", ErrRecordUnreferenced, kind, actual)
		}
		want[actual] = true
	}
	for d, matched := range want {
		if !matched {
			return fmt.Errorf("%w: %s %s", ErrRecordMissing, kind, d)
		}
	}
	return nil
}

// obligationByID indexes governed obligations for the mandate re-checks.
func obligationByID(obligations []proofdischarge.ProofObligation) map[string]proofdischarge.ProofObligation {
	out := make(map[string]proofdischarge.ProofObligation, len(obligations))
	for _, ob := range obligations {
		out[strings.TrimSpace(ob.ID)] = ob
	}
	return out
}
