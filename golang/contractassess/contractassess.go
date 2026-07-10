// SPDX-License-Identifier: Apache-2.0

package contractassess

// Outcome is the classifier result for a contract assessment.
type Outcome string

const (
	ContractFound         Outcome = "contract-found"
	ContractSynthesisSafe Outcome = "contract-synthesis-safe"
	ContractProposalOnly  Outcome = "contract-proposal-only"
	ContractUnknown       Outcome = "contract-unknown"
)

// RequiredAction records the next step the caller must enforce.
type RequiredAction string

const (
	AttachGoverningTest        RequiredAction = "attach-governing-test"
	DraftContractWithCitations RequiredAction = "draft-contract-with-citations"
	ReviewRequired             RequiredAction = "review-required"
	EscalateToHuman            RequiredAction = "escalate-to-human"
)

// Blocker prevents synthesis-safe classification when present.
type Blocker string

const (
	BlockerConflictingExplicitContract Blocker = "conflicting-explicit-contract"
	BlockerConflictingTest             Blocker = "conflicting-test"
	BlockerMissingOwnershipAuthority   Blocker = "missing-ownership-authority"
	BlockerProductAmbiguity            Blocker = "product-ambiguity"
	BlockerWeakPatternOnly             Blocker = "weak-pattern-only"
	BlockerGenericEvidenceOnly         Blocker = "generic-evidence-only"
)

// EvidenceScores is the deterministic evidence summary supplied to the
// classifier. Ranges are defined by the contract synthesis gate spec.
type EvidenceScores struct {
	DirectSourceAnnotation        int
	ExistingTestsProvingBehavior  int
	RepeatedImplementationPattern int
	OwnershipAuthorityPath        int
	FailureModeOrIncidentHistory  int
	NearbyHumanIntent             int
	CrossRepoConsistency          int
	AbsenceOfConflictingContracts int
}

// Total returns the raw evidence score.
func (s EvidenceScores) Total() int {
	return s.DirectSourceAnnotation +
		s.ExistingTestsProvingBehavior +
		s.RepeatedImplementationPattern +
		s.OwnershipAuthorityPath +
		s.FailureModeOrIncidentHistory +
		s.NearbyHumanIntent +
		s.CrossRepoConsistency +
		s.AbsenceOfConflictingContracts
}

// AssessmentInput describes the explicit evidence already gathered by a caller.
type AssessmentInput struct {
	ExplicitContractExists bool
	HasGoverningTest       bool
	Scores                 EvidenceScores
	Blockers               []Blocker
}

// AssessmentResult is the deterministic outcome returned by Assess.
type AssessmentResult struct {
	Outcome         Outcome
	Score           int
	Scores          EvidenceScores
	Blockers        []Blocker
	RequiredActions []RequiredAction
}

// Assess classifies the supplied evidence using the contract synthesis gate.
func Assess(in AssessmentInput) AssessmentResult {
	result := AssessmentResult{
		Score:    in.Scores.Total(),
		Scores:   in.Scores,
		Blockers: append([]Blocker(nil), in.Blockers...),
	}

	if in.ExplicitContractExists {
		result.Outcome = ContractFound
		return result
	}

	if blocksUnknown(in.Blockers) || in.Scores.OwnershipAuthorityPath < 2 {
		result.Outcome = ContractUnknown
		result.RequiredActions = []RequiredAction{EscalateToHuman}
		return result
	}

	if qualifiesForSynthesisSafe(in) {
		result.Outcome = ContractSynthesisSafe
		result.RequiredActions = []RequiredAction{DraftContractWithCitations}
		if !in.HasGoverningTest {
			result.RequiredActions = append([]RequiredAction{AttachGoverningTest}, result.RequiredActions...)
		}
		return result
	}

	if result.Score >= 10 {
		result.Outcome = ContractProposalOnly
		result.RequiredActions = []RequiredAction{DraftContractWithCitations, ReviewRequired}
		return result
	}

	result.Outcome = ContractUnknown
	result.RequiredActions = []RequiredAction{EscalateToHuman}
	return result
}

func qualifiesForSynthesisSafe(in AssessmentInput) bool {
	if len(in.Blockers) > 0 {
		return false
	}
	if in.Scores.Total() < 16 {
		return false
	}
	if in.Scores.ExistingTestsProvingBehavior < 3 {
		return false
	}
	if in.Scores.OwnershipAuthorityPath < 2 {
		return false
	}
	return in.Scores.DirectSourceAnnotation >= 2 || in.Scores.NearbyHumanIntent >= 2
}

func blocksUnknown(blockers []Blocker) bool {
	for _, blocker := range blockers {
		switch blocker {
		case BlockerConflictingExplicitContract,
			BlockerConflictingTest,
			BlockerMissingOwnershipAuthority,
			BlockerProductAmbiguity,
			BlockerWeakPatternOnly:
			return true
		}
	}
	return false
}
