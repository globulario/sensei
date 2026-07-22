// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

// EvidenceRequest reason codes
const (
	ReasonSupportingEvidenceMissing  = "supporting_evidence_missing"
	ReasonRefutingEvidenceUnsearched = "refuting_evidence_unsearched"
	ReasonOwnerAuthorityUnresolved   = "owner_authority_unresolved"
	ReasonBoundaryScopeUnresolved    = "boundary_scope_unresolved"
	ReasonRuntimeConfirmationRequired = "runtime_confirmation_required"
	ReasonHistoricalRationaleUnresolved = "historical_rationale_unresolved"
	ReasonCounterexampleExecutionRequired = "counterexample_execution_required"
)

// IsValidEvidenceRequestReason checks if a reason matches the closed vocabulary.
func IsValidEvidenceRequestReason(r string) bool {
	switch r {
	case ReasonSupportingEvidenceMissing, ReasonRefutingEvidenceUnsearched,
		ReasonOwnerAuthorityUnresolved, ReasonBoundaryScopeUnresolved,
		ReasonRuntimeConfirmationRequired, ReasonHistoricalRationaleUnresolved,
		ReasonCounterexampleExecutionRequired:
		return true
	default:
		return false
	}
}

// EvidenceRequest defines an internal evidence-gap request.
type EvidenceRequest struct {
	ID          string `json:"id" yaml:"id"`
	CandidateID string `json:"candidate_id" yaml:"candidate_id"`

	Category investigation.EvidenceCategory `json:"category" yaml:"category"`
	Scope    architecture.ClaimScope        `json:"scope" yaml:"scope"`

	ReasonCode  string `json:"reason_code" yaml:"reason_code"`
	Description string `json:"description" yaml:"description"`

	RequiredProofStrength  investigation.ProofStrength `json:"required_proof_strength" yaml:"required_proof_strength"`
	ExistingCoverageRefIDs []string                    `json:"existing_coverage_ref_ids,omitempty" yaml:"existing_coverage_ref_ids,omitempty"`
}
