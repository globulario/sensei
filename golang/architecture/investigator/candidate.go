// SPDX-License-Identifier: AGPL-3.0-only

package investigator

// CandidateKind defines the vocabulary of candidate types.
type CandidateKind string

const (
	KindInvariant      CandidateKind = "invariant"
	KindContract       CandidateKind = "contract"
	KindBoundary       CandidateKind = "boundary"
	KindOwner          CandidateKind = "owner"
	KindFailureMode    CandidateKind = "failure_mode"
	KindGovernanceDebt CandidateKind = "governance_debt"
)

// IsValidCandidateKind validates if a kind belongs to the closed vocabulary.
func IsValidCandidateKind(k CandidateKind) bool {
	switch k {
	case KindInvariant, KindContract, KindBoundary, KindOwner, KindFailureMode, KindGovernanceDebt:
		return true
	default:
		return false
	}
}

// ConfidenceFactor represents one metric scoring parameter.
type ConfidenceFactor struct {
	Metric string `json:"metric" yaml:"metric"`
	Value  int    `json:"value" yaml:"value"`
}

// CandidateEnvelope holds investigation-only candidate metadata.
type CandidateEnvelope struct {
	CandidateID string        `json:"candidate_id" yaml:"candidate_id"`
	ClaimID     string        `json:"claim_id" yaml:"claim_id"`
	OutputKind  CandidateKind `json:"output_kind" yaml:"output_kind"`

	ObservationRefIDs        []string `json:"observation_ref_ids,omitempty" yaml:"observation_ref_ids,omitempty"`
	SupportingEvidenceRefIDs []string `json:"supporting_evidence_ref_ids,omitempty" yaml:"supporting_evidence_ref_ids,omitempty"`
	RefutingEvidenceRefIDs   []string `json:"refuting_evidence_ref_ids,omitempty" yaml:"refuting_evidence_ref_ids,omitempty"`

	FalsificationConditions   []string           `json:"falsification_conditions" yaml:"falsification_conditions"`
	MissingEvidenceRequestIDs []string           `json:"missing_evidence_request_ids,omitempty" yaml:"missing_evidence_request_ids,omitempty"`
	ConfidenceBasis           []ConfidenceFactor `json:"confidence_basis,omitempty" yaml:"confidence_basis,omitempty"`
}
