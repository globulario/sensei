// SPDX-License-Identifier: AGPL-3.0-only

package admission

import (
	"errors"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// Phase 3 admission v2: bind an exact typed mutation plan to a resolved
// authority decision and mint a single-use capability. This is additive to the
// legacy admission path (plan §9: "extend admission rather than replacing it").
// Admission proves that mutation scope and legal authority hold BEFORE proof
// execution begins; it never establishes correctness — CorrectnessCertified
// stays false and Phase 6 certification remains its sole writer.

const (
	// AdmissionVerdictAdmitted is the per-operation verdict when authority is
	// resolved valid and the selected mechanism is legal for the operation.
	AdmissionVerdictAdmitted = "admitted"
	// AdmissionVerdictRefused is the per-operation verdict otherwise.
	AdmissionVerdictRefused = "refused"
)

// AdmissionV2Policy is the governed policy input for a typed admission decision.
// It carries what the admission decision imposes downstream (proof/evidence/
// rebuild obligations, budgets, completion policy) and how long the minted
// capability stays usable.
type AdmissionV2Policy struct {
	PolicyID                 string
	CompletionPolicyID       string
	RequiredProofSlots       []string
	RequiredEvidenceProfiles []string
	RequiredResultRebuilds   []string
	RiskBudget               int
	OperationBudget          int
	ValidityWindow           time.Duration
}

// DecideAdmission produces a typed AdmissionDecision by binding an
// AdmissionRequest to an already-computed AuthorityResolution.
//
// It never trusts the caller's asserted authority digest: it recomputes the
// resolution digest and requires it to equal the request's, then requires the
// resolution to be about this request's exact actor and base bindings. Each
// operation verdict is derived from the resolution's own per-operation result,
// never from a caller-supplied verdict.
func DecideAdmission(req closureprotocol.AdmissionRequest, resolution closureprotocol.AuthorityResolution, policy AdmissionV2Policy, decidedAt string) (closureprotocol.AdmissionDecision, error) {
	if err := closureprotocol.ValidateAdmissionRequest(req); err != nil {
		return closureprotocol.AdmissionDecision{}, err
	}
	if strings.TrimSpace(policy.PolicyID) == "" || strings.TrimSpace(policy.CompletionPolicyID) == "" {
		return closureprotocol.AdmissionDecision{}, errors.New("admission policy requires policy_id and completion_policy_id")
	}
	if strings.TrimSpace(req.PolicyID) != strings.TrimSpace(policy.PolicyID) {
		return closureprotocol.AdmissionDecision{}, errors.New("admission policy id does not match the request policy")
	}
	decidedTime, err := time.Parse(time.RFC3339, decidedAt)
	if err != nil {
		return closureprotocol.AdmissionDecision{}, errors.New("decided_at must be RFC3339")
	}

	// Anti-forgery: bind the exact authority resolution the request claims.
	resDigest, err := closureprotocol.AuthorityResolutionDigest(resolution)
	if err != nil {
		return closureprotocol.AdmissionDecision{}, err
	}
	if resDigest != strings.TrimSpace(req.AuthorityResolutionDigestSHA256) {
		return closureprotocol.AdmissionDecision{}, errors.New("authority resolution digest does not match the request")
	}

	// The resolution must be about THIS request's actor and base bindings.
	actorDigest, err := closureprotocol.SemanticDigest(req.ActorBinding)
	if err != nil {
		return closureprotocol.AdmissionDecision{}, err
	}
	baseDigest, err := closureprotocol.SemanticDigest(req.BaseBinding)
	if err != nil {
		return closureprotocol.AdmissionDecision{}, err
	}
	if resolution.ActorBindingDigestSHA256 != actorDigest {
		return closureprotocol.AdmissionDecision{}, errors.New("authority resolution actor binding does not match the request actor")
	}
	if resolution.BaseBindingDigestSHA256 != baseDigest {
		return closureprotocol.AdmissionDecision{}, errors.New("authority resolution base binding does not match the request base")
	}

	byOp := make(map[string]closureprotocol.AuthorityResolutionOperation, len(resolution.OperationResults))
	for _, r := range resolution.OperationResults {
		byOp[r.OperationID] = r
	}

	verdicts := make([]closureprotocol.OperationAdmissionVerdict, 0, len(req.ChangePlan.Operations))
	for _, op := range req.ChangePlan.Operations {
		res, ok := byOp[op.OperationID]
		switch {
		case !ok:
			verdicts = append(verdicts, refused(op.OperationID, "admission.authority.unresolved"))
		case res.Status != closureprotocol.ReceiptValid:
			verdicts = append(verdicts, refused(op.OperationID, "admission.authority.not_valid"))
		case strings.TrimSpace(string(res.SelectedMechanism)) == "":
			verdicts = append(verdicts, refused(op.OperationID, "admission.mechanism.missing"))
		case op.SelectedMechanism != res.SelectedMechanism:
			verdicts = append(verdicts, refused(op.OperationID, "admission.mechanism.mismatch"))
		default:
			verdicts = append(verdicts, closureprotocol.OperationAdmissionVerdict{
				OperationID: op.OperationID,
				Verdict:     AdmissionVerdictAdmitted,
			})
		}
	}
	if len(verdicts) == 0 {
		return closureprotocol.AdmissionDecision{}, errors.New("change plan has no operations to admit")
	}

	requestDigest, err := closureprotocol.SemanticDigest(req)
	if err != nil {
		return closureprotocol.AdmissionDecision{}, err
	}

	decision := closureprotocol.AdmissionDecision{
		RequestDigestSHA256:      requestDigest,
		PolicyID:                 policy.PolicyID,
		OperationVerdicts:        verdicts,
		CapabilityID:             "capability." + requestDigest[:16],
		CompletionPolicyID:       policy.CompletionPolicyID,
		RequiredProofSlots:       closureprotocol.NormalizeSet(policy.RequiredProofSlots),
		RequiredEvidenceProfiles: closureprotocol.NormalizeSet(policy.RequiredEvidenceProfiles),
		RequiredResultRebuilds:   closureprotocol.NormalizeSet(policy.RequiredResultRebuilds),
		RiskBudget:               policy.RiskBudget,
		OperationBudget:          policy.OperationBudget,
	}
	if policy.ValidityWindow > 0 {
		decision.CapabilityExpiry = decidedTime.Add(policy.ValidityWindow).UTC().Format(time.RFC3339)
	}

	if err := closureprotocol.ValidateAdmissionDecision(decision); err != nil {
		return closureprotocol.AdmissionDecision{}, err
	}
	return decision, nil
}

// AllAdmitted reports whether every operation in the decision was admitted.
// A capability is only consumable (Phase 3 slice 2) when this holds.
func AllAdmitted(decision closureprotocol.AdmissionDecision) bool {
	if len(decision.OperationVerdicts) == 0 {
		return false
	}
	for _, v := range decision.OperationVerdicts {
		if v.Verdict != AdmissionVerdictAdmitted {
			return false
		}
	}
	return true
}

func refused(operationID, code string) closureprotocol.OperationAdmissionVerdict {
	return closureprotocol.OperationAdmissionVerdict{
		OperationID: operationID,
		Verdict:     AdmissionVerdictRefused,
		ReasonCodes: []string{code},
	}
}
