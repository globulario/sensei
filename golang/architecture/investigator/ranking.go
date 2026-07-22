// SPDX-License-Identifier: AGPL-3.0-only

package investigator

// RankingFactorKind defines closed ranking criteria.
type RankingFactorKind string

const (
	FactorBlastRadius               RankingFactorKind = "blast_radius"
	FactorAuthoritySensitivity      RankingFactorKind = "authority_sensitivity"
	FactorContradictionDensity      RankingFactorKind = "contradiction_density"
	FactorIncidentRecurrence        RankingFactorKind = "incident_recurrence"
	FactorTaskRelevance             RankingFactorKind = "task_relevance"
	FactorRuntimeFrequency          RankingFactorKind = "runtime_frequency"
	FactorEvidenceIndependence      RankingFactorKind = "evidence_independence"
	FactorMissingEvidenceCost       RankingFactorKind = "missing_evidence_cost"
	FactorFutureAgentErrorReduction RankingFactorKind = "future_agent_error_reduction"
)

// IsValidRankingFactorKind validates factor kinds.
func IsValidRankingFactorKind(k RankingFactorKind) bool {
	switch k {
	case FactorBlastRadius, FactorAuthoritySensitivity, FactorContradictionDensity, FactorIncidentRecurrence,
		FactorTaskRelevance, FactorRuntimeFrequency, FactorEvidenceIndependence, FactorMissingEvidenceCost,
		FactorFutureAgentErrorReduction:
		return true
	default:
		return false
	}
}

// RankingFactor represents one dimension of candidate ranking score.
type RankingFactor struct {
	Kind           RankingFactorKind `json:"kind" yaml:"kind"`
	Value          int               `json:"value" yaml:"value"`
	EvidenceRefIDs []string          `json:"evidence_ref_ids,omitempty" yaml:"evidence_ref_ids,omitempty"`
}

// RankingRecord aggregates factors into a stable score and rank.
type RankingRecord struct {
	CandidateID string `json:"candidate_id" yaml:"candidate_id"`
	Rank        int    `json:"rank" yaml:"rank"`
	Score       int    `json:"score" yaml:"score"`

	Factors []RankingFactor `json:"factors" yaml:"factors"`
}
