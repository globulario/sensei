// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/contractassess"
)

func runContractAssess(args []string) int {
	fs := flag.NewFlagSet("awg contract-assess", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	explicitContract := fs.Bool("explicit-contract", false, "existing explicit contract already governs the change")
	hasGoverningTest := fs.Bool("governing-test", false, "a governing test already exists for the assessed scope")
	asJSON := fs.Bool("json", false, "output as JSON")

	directSourceAnnotation := fs.Int("direct-source-annotation", 0, "evidence score 0-3")
	existingTestsProvingBehavior := fs.Int("existing-tests-proving-behavior", 0, "evidence score 0-4")
	repeatedImplementationPattern := fs.Int("repeated-implementation-pattern", 0, "evidence score 0-2")
	ownershipAuthorityPath := fs.Int("ownership-authority-path", 0, "evidence score 0-3")
	failureModeOrIncidentHistory := fs.Int("failure-mode-or-incident-history", 0, "evidence score 0-2")
	nearbyHumanIntent := fs.Int("nearby-human-intent", 0, "evidence score 0-3")
	crossRepoConsistency := fs.Int("cross-repo-consistency", 0, "evidence score 0-2")
	absenceOfConflictingContracts := fs.Int("absence-of-conflicting-contracts", 0, "evidence score 0-3")

	var blockers stringSlice
	fs.Var(&blockers, "blocker", "hard blocker (repeatable): conflicting-explicit-contract | conflicting-test | missing-ownership-authority | product-ambiguity | weak-pattern-only | generic-evidence-only")

	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg contract-assess [flags]

Report-only contract synthesis assessment. This command does NOT query the
graph, infer evidence, generate contracts, or change runtime behavior. It
classifies an assessment from explicitly supplied evidence scores and blockers.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 2
	}

	parsedBlockers, err := parseAssessmentBlockers(blockers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg contract-assess: %v\n", err)
		return 2
	}

	input := contractassess.AssessmentInput{
		ExplicitContractExists: *explicitContract,
		HasGoverningTest:       *hasGoverningTest,
		Scores: contractassess.EvidenceScores{
			DirectSourceAnnotation:        *directSourceAnnotation,
			ExistingTestsProvingBehavior:  *existingTestsProvingBehavior,
			RepeatedImplementationPattern: *repeatedImplementationPattern,
			OwnershipAuthorityPath:        *ownershipAuthorityPath,
			FailureModeOrIncidentHistory:  *failureModeOrIncidentHistory,
			NearbyHumanIntent:             *nearbyHumanIntent,
			CrossRepoConsistency:          *crossRepoConsistency,
			AbsenceOfConflictingContracts: *absenceOfConflictingContracts,
		},
		Blockers: parsedBlockers,
	}
	result := contractassess.Assess(input)

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(struct {
			Outcome         contractassess.Outcome          `json:"outcome"`
			Score           int                             `json:"score"`
			Scores          contractassess.EvidenceScores   `json:"scores"`
			Blockers        []contractassess.Blocker        `json:"blockers,omitempty"`
			RequiredActions []contractassess.RequiredAction `json:"required_actions,omitempty"`
		}{
			Outcome:         result.Outcome,
			Score:           result.Score,
			Scores:          result.Scores,
			Blockers:        result.Blockers,
			RequiredActions: result.RequiredActions,
		})
		return 0
	}

	fmt.Printf("Outcome: %s\n", result.Outcome)
	fmt.Printf("Score: %d\n", result.Score)
	fmt.Printf("Explicit contract: %v\n", input.ExplicitContractExists)
	fmt.Printf("Governing test present: %v\n", input.HasGoverningTest)

	fmt.Println("\nEvidence scores:")
	printAssessmentScore("direct_source_annotation", result.Scores.DirectSourceAnnotation)
	printAssessmentScore("existing_tests_proving_behavior", result.Scores.ExistingTestsProvingBehavior)
	printAssessmentScore("repeated_implementation_pattern", result.Scores.RepeatedImplementationPattern)
	printAssessmentScore("ownership_authority_path", result.Scores.OwnershipAuthorityPath)
	printAssessmentScore("failure_mode_or_incident_history", result.Scores.FailureModeOrIncidentHistory)
	printAssessmentScore("nearby_human_intent", result.Scores.NearbyHumanIntent)
	printAssessmentScore("cross_repo_consistency", result.Scores.CrossRepoConsistency)
	printAssessmentScore("absence_of_conflicting_contracts", result.Scores.AbsenceOfConflictingContracts)

	fmt.Println("\nBlockers:")
	if len(result.Blockers) == 0 {
		fmt.Println("  - none")
	} else {
		names := make([]string, 0, len(result.Blockers))
		for _, blocker := range result.Blockers {
			names = append(names, string(blocker))
		}
		sort.Strings(names)
		for _, blocker := range names {
			fmt.Printf("  - %s\n", blocker)
		}
	}

	fmt.Println("\nRequired actions:")
	if len(result.RequiredActions) == 0 {
		fmt.Println("  - none")
	} else {
		for _, action := range result.RequiredActions {
			fmt.Printf("  - %s\n", action)
		}
	}

	return 0
}

func parseAssessmentBlockers(values []string) ([]contractassess.Blocker, error) {
	if len(values) == 0 {
		return nil, nil
	}
	seen := make(map[contractassess.Blocker]bool, len(values))
	out := make([]contractassess.Blocker, 0, len(values))
	for _, value := range values {
		blocker := contractassess.Blocker(strings.TrimSpace(value))
		switch blocker {
		case contractassess.BlockerConflictingExplicitContract,
			contractassess.BlockerConflictingTest,
			contractassess.BlockerMissingOwnershipAuthority,
			contractassess.BlockerProductAmbiguity,
			contractassess.BlockerWeakPatternOnly,
			contractassess.BlockerGenericEvidenceOnly:
		default:
			return nil, fmt.Errorf("unknown blocker %q", value)
		}
		if seen[blocker] {
			continue
		}
		seen[blocker] = true
		out = append(out, blocker)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func printAssessmentScore(name string, score int) {
	fmt.Printf("  - %s: %d\n", name, score)
}
