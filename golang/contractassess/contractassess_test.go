// SPDX-License-Identifier: AGPL-3.0-only

package contractassess

import "testing"

func TestAssess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   AssessmentInput
		outcome Outcome
		actions []RequiredAction
	}{
		{
			name: "explicit contract present returns contract found",
			input: AssessmentInput{
				ExplicitContractExists: true,
				HasGoverningTest:       true,
				Scores: EvidenceScores{
					ExistingTestsProvingBehavior: 4,
					OwnershipAuthorityPath:       3,
				},
			},
			outcome: ContractFound,
		},
		{
			name: "score at least sixteen with anchors and no blockers is synthesis safe",
			input: AssessmentInput{
				HasGoverningTest: true,
				Scores: EvidenceScores{
					DirectSourceAnnotation:        2,
					ExistingTestsProvingBehavior:  4,
					RepeatedImplementationPattern: 1,
					OwnershipAuthorityPath:        3,
					FailureModeOrIncidentHistory:  1,
					NearbyHumanIntent:             3,
					CrossRepoConsistency:          1,
					AbsenceOfConflictingContracts: 2,
				},
			},
			outcome: ContractSynthesisSafe,
			actions: []RequiredAction{
				DraftContractWithCitations,
			},
		},
		{
			name: "score ten to fifteen is proposal only",
			input: AssessmentInput{
				HasGoverningTest: true,
				Scores: EvidenceScores{
					DirectSourceAnnotation:        1,
					ExistingTestsProvingBehavior:  2,
					RepeatedImplementationPattern: 1,
					OwnershipAuthorityPath:        2,
					FailureModeOrIncidentHistory:  1,
					NearbyHumanIntent:             2,
					CrossRepoConsistency:          1,
					AbsenceOfConflictingContracts: 1,
				},
			},
			outcome: ContractProposalOnly,
			actions: []RequiredAction{
				DraftContractWithCitations,
				ReviewRequired,
			},
		},
		{
			name: "score below ten is unknown",
			input: AssessmentInput{
				Scores: EvidenceScores{
					ExistingTestsProvingBehavior: 1,
					OwnershipAuthorityPath:       2,
					NearbyHumanIntent:            1,
				},
			},
			outcome: ContractUnknown,
			actions: []RequiredAction{
				EscalateToHuman,
			},
		},
		{
			name: "conflicting explicit contract blocks safe classification",
			input: AssessmentInput{
				HasGoverningTest: true,
				Scores: EvidenceScores{
					DirectSourceAnnotation:        3,
					ExistingTestsProvingBehavior:  4,
					RepeatedImplementationPattern: 2,
					OwnershipAuthorityPath:        3,
					FailureModeOrIncidentHistory:  2,
					NearbyHumanIntent:             3,
					CrossRepoConsistency:          2,
					AbsenceOfConflictingContracts: 0,
				},
				Blockers: []Blocker{BlockerConflictingExplicitContract},
			},
			outcome: ContractUnknown,
			actions: []RequiredAction{
				EscalateToHuman,
			},
		},
		{
			name: "generic evidence only is at most proposal only",
			input: AssessmentInput{
				HasGoverningTest: true,
				Scores: EvidenceScores{
					DirectSourceAnnotation:        2,
					ExistingTestsProvingBehavior:  4,
					RepeatedImplementationPattern: 2,
					OwnershipAuthorityPath:        2,
					FailureModeOrIncidentHistory:  1,
					NearbyHumanIntent:             2,
					CrossRepoConsistency:          2,
					AbsenceOfConflictingContracts: 3,
				},
				Blockers: []Blocker{BlockerGenericEvidenceOnly},
			},
			outcome: ContractProposalOnly,
			actions: []RequiredAction{
				DraftContractWithCitations,
				ReviewRequired,
			},
		},
		{
			name: "safe outcome without governing test requires attach action",
			input: AssessmentInput{
				Scores: EvidenceScores{
					DirectSourceAnnotation:        2,
					ExistingTestsProvingBehavior:  4,
					RepeatedImplementationPattern: 1,
					OwnershipAuthorityPath:        3,
					FailureModeOrIncidentHistory:  1,
					NearbyHumanIntent:             2,
					CrossRepoConsistency:          1,
					AbsenceOfConflictingContracts: 2,
				},
			},
			outcome: ContractSynthesisSafe,
			actions: []RequiredAction{
				AttachGoverningTest,
				DraftContractWithCitations,
			},
		},
		{
			name: "high total score missing required test anchor is proposal only",
			input: AssessmentInput{
				HasGoverningTest: true,
				Scores: EvidenceScores{
					DirectSourceAnnotation:        3,
					ExistingTestsProvingBehavior:  2,
					RepeatedImplementationPattern: 2,
					OwnershipAuthorityPath:        3,
					FailureModeOrIncidentHistory:  2,
					NearbyHumanIntent:             3,
					CrossRepoConsistency:          2,
					AbsenceOfConflictingContracts: 3,
				},
			},
			outcome: ContractProposalOnly,
			actions: []RequiredAction{
				DraftContractWithCitations,
				ReviewRequired,
			},
		},
		{
			name: "high total score missing ownership authority path is unknown",
			input: AssessmentInput{
				HasGoverningTest: true,
				Scores: EvidenceScores{
					DirectSourceAnnotation:        3,
					ExistingTestsProvingBehavior:  4,
					RepeatedImplementationPattern: 2,
					OwnershipAuthorityPath:        1,
					FailureModeOrIncidentHistory:  2,
					NearbyHumanIntent:             3,
					CrossRepoConsistency:          2,
					AbsenceOfConflictingContracts: 3,
				},
			},
			outcome: ContractUnknown,
			actions: []RequiredAction{
				EscalateToHuman,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := Assess(tt.input)
			if got.Outcome != tt.outcome {
				t.Fatalf("outcome = %q, want %q", got.Outcome, tt.outcome)
			}
			if !sameActions(got.RequiredActions, tt.actions) {
				t.Fatalf("required actions = %v, want %v", got.RequiredActions, tt.actions)
			}
		})
	}
}

func sameActions(got, want []RequiredAction) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
