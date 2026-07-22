// SPDX-License-Identifier: AGPL-3.0-only

package investigator

// ChallengeStatus represents the closed status vocabulary for a challenge.
type ChallengeStatus string

const (
	ChallengePending              ChallengeStatus = "pending"
	ChallengeSurvived             ChallengeStatus = "survived"
	ChallengeContested            ChallengeStatus = "contested"
	ChallengeRefuted              ChallengeStatus = "refuted"
	ChallengeInsufficientEvidence ChallengeStatus = "insufficient_evidence"
)

// IsValidChallengeStatus checks if status is a valid vocabulary term.
func IsValidChallengeStatus(s ChallengeStatus) bool {
	switch s {
	case ChallengePending, ChallengeSurvived, ChallengeContested, ChallengeRefuted, ChallengeInsufficientEvidence:
		return true
	default:
		return false
	}
}

// IsValidChallengeReasonCode checks the deterministic challenge reason vocabulary.
func IsValidChallengeReasonCode(reason string) bool {
	switch reason {
	case ChallengeReasonRefutingEvidence, ChallengeReasonEvidenceMissing, ChallengeReasonNoCounterexample:
		return true
	default:
		return false
	}
}

// ChallengeReceipt documents the result of an adversarial challenge.
type ChallengeReceipt struct {
	ID          string `json:"id" yaml:"id"`
	CandidateID string `json:"candidate_id" yaml:"candidate_id"`

	StrategyVersion string          `json:"strategy_version" yaml:"strategy_version"`
	Status          ChallengeStatus `json:"status" yaml:"status"`
	ReasonCode      string          `json:"reason_code" yaml:"reason_code"`

	SupportingEvidenceRefIDs []string `json:"supporting_evidence_ref_ids,omitempty" yaml:"supporting_evidence_ref_ids,omitempty"`
	RefutingEvidenceRefIDs   []string `json:"refuting_evidence_ref_ids,omitempty" yaml:"refuting_evidence_ref_ids,omitempty"`
	CounterexampleIDs        []string `json:"counterexample_ids,omitempty" yaml:"counterexample_ids,omitempty"`
	EvidenceRequestIDs       []string `json:"evidence_request_ids,omitempty" yaml:"evidence_request_ids,omitempty"`
}
