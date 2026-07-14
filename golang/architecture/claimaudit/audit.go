// SPDX-License-Identifier: AGPL-3.0-only

// Package claimaudit reports signal distribution and coverage for a complete
// ArchitectureClaim document. It deliberately produces counts and warnings,
// never a composite score.
package claimaudit

import (
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/plane"
)

type Options struct {
	RootComponentID string
	CoreFiles       []string
}

type Count struct {
	Key   string `yaml:"key"`
	Count int    `yaml:"count"`
}

type CoreFileCount struct {
	File  string `yaml:"file"`
	Count int    `yaml:"count"`
}

type DuplicateGroup struct {
	PropositionKey string   `yaml:"proposition_key"`
	ClaimIDs       []string `yaml:"claim_ids"`
	Count          int      `yaml:"count"`
}

type Warning struct {
	Code   string `yaml:"code"`
	Detail string `yaml:"detail"`
}

type Report struct {
	SchemaVersion                 string           `yaml:"schema_version"`
	GeneratedBy                   string           `yaml:"generated_by"`
	TotalClaims                   int              `yaml:"total_claims"`
	DistinctPropositionKeys       int              `yaml:"distinct_proposition_keys"`
	ClaimsByInferenceRule         []Count          `yaml:"claims_by_inference_rule"`
	ClaimsByPredicate             []Count          `yaml:"claims_by_predicate"`
	ClaimsByArchitecturalPlane    []Count          `yaml:"claims_by_architectural_plane"`
	ClaimsByStatus                []Count          `yaml:"claims_by_status"`
	ClaimsAnchoredToRootComponent int              `yaml:"claims_anchored_to_root_component"`
	ClaimsAnchoredToCoreFiles     []CoreFileCount  `yaml:"claims_anchored_to_core_files"`
	ClaimsWithSupportingTests     int              `yaml:"claims_with_supporting_tests"`
	ClaimsWithEvidence            int              `yaml:"claims_with_evidence"`
	ClaimsWithUnknowns            int              `yaml:"claims_with_unknowns"`
	ClaimsWithAlternatives        int              `yaml:"claims_with_alternatives"`
	DuplicatePropositionGroups    []DuplicateGroup `yaml:"duplicate_proposition_groups"`
	LargestClaimGroup             int              `yaml:"largest_claim_group"`
	UnanchoredClaims              []string         `yaml:"unanchored_claims"`
	Warnings                      []Warning        `yaml:"warnings,omitempty"`
}

func Build(doc architecture.ClaimDocument, opts Options) Report {
	report := Report{
		SchemaVersion: "1", GeneratedBy: "sensei claim audit", TotalClaims: len(doc.Claims),
		ClaimsByInferenceRule: []Count{}, ClaimsByPredicate: []Count{},
		ClaimsByArchitecturalPlane: []Count{}, ClaimsByStatus: []Count{},
		ClaimsAnchoredToCoreFiles: []CoreFileCount{}, DuplicatePropositionGroups: []DuplicateGroup{},
	}
	rules, predicates, planes, statuses := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	groups := map[string][]string{}
	coreCounts := map[string]int{}
	for _, file := range normalized(opts.CoreFiles) {
		coreCounts[file] = 0
	}
	facts := map[string]architecture.ClaimFactReceipt{}
	for _, receipt := range doc.FactReceipts {
		facts[receipt.Fact.ID] = receipt
	}
	claimsMissingReceipts := 0

	for _, claim := range doc.Claims {
		rules[valueOrUnknown(claim.InferenceRule)]++
		predicates[valueOrUnknown(claim.Statement.Predicate)]++
		planes[valueOrUnknown(claim.ArchitecturalPlane)]++
		statuses[valueOrUnknown(claim.EpistemicStatus)]++
		key := plane.PropositionKey(claim)
		groups[key] = append(groups[key], claim.ID)

		if contains(claim.Scope.Components, opts.RootComponentID) && opts.RootComponentID != "" {
			report.ClaimsAnchoredToRootComponent++
		}
		for _, file := range claim.Scope.Files {
			if _, ok := coreCounts[file]; ok {
				coreCounts[file]++
			}
		}
		if len(claim.Scope.Files)+len(claim.Scope.Symbols)+len(claim.Scope.Components)+len(claim.AboutNodes) == 0 {
			report.UnanchoredClaims = append(report.UnanchoredClaims, claim.ID)
		}
		if len(claim.SupportingEvidence)+len(claim.RefutingEvidence) > 0 {
			report.ClaimsWithEvidence++
		}
		if len(claim.Unknowns) > 0 {
			report.ClaimsWithUnknowns++
		}
		if len(claim.AlternativeExplanations) > 0 {
			report.ClaimsWithAlternatives++
		}
		if claimHasSupportingTest(claim, facts) {
			report.ClaimsWithSupportingTests++
		}
		if !claimHasCompleteReceipt(claim, facts) {
			claimsMissingReceipts++
		}
	}

	report.ClaimsByInferenceRule = counts(rules)
	report.ClaimsByPredicate = counts(predicates)
	report.ClaimsByArchitecturalPlane = counts(planes)
	report.ClaimsByStatus = counts(statuses)
	for _, file := range normalized(opts.CoreFiles) {
		report.ClaimsAnchoredToCoreFiles = append(report.ClaimsAnchoredToCoreFiles, CoreFileCount{File: file, Count: coreCounts[file]})
	}
	sort.Strings(report.UnanchoredClaims)

	groupKeys := make([]string, 0, len(groups))
	for key := range groups {
		groupKeys = append(groupKeys, key)
	}
	sort.Strings(groupKeys)
	report.DistinctPropositionKeys = len(groupKeys)
	for _, key := range groupKeys {
		ids := groups[key]
		sort.Strings(ids)
		if len(ids) > report.LargestClaimGroup {
			report.LargestClaimGroup = len(ids)
		}
		if len(ids) > 1 {
			report.DuplicatePropositionGroups = append(report.DuplicatePropositionGroups, DuplicateGroup{PropositionKey: key, ClaimIDs: ids, Count: len(ids)})
		}
	}
	addWarnings(&report, rules, coreCounts, claimsMissingReceipts)
	return report
}

func addWarnings(report *Report, rules, coreCounts map[string]int, claimsMissingReceipts int) {
	if report.TotalClaims >= 10 {
		for rule, count := range rules {
			if float64(count)/float64(report.TotalClaims) > .80 {
				report.Warnings = append(report.Warnings, Warning{Code: "claim_audit.rule_overwhelming_share", Detail: rule + " produces more than 80 percent of claims"})
			}
		}
	}
	if report.TotalClaims > 0 && float64(report.TotalClaims-report.DistinctPropositionKeys)/float64(report.TotalClaims) > .25 {
		report.Warnings = append(report.Warnings, Warning{Code: "claim_audit.high_duplicate_proposition_ratio", Detail: "more than 25 percent of claims share proposition keys"})
	}
	coreRelevant := 0
	for file, count := range coreCounts {
		coreRelevant += count
		if count == 0 {
			report.Warnings = append(report.Warnings, Warning{Code: "claim_audit.root_core_file_without_claims", Detail: file + " has no anchored claim"})
		}
	}
	if report.TotalClaims > 0 && len(coreCounts) > 0 && coreRelevant == 0 {
		report.Warnings = append(report.Warnings, Warning{Code: "claim_audit.no_task_relevant_claims", Detail: "claims exist but none anchor a task-ready root core file"})
	}
	if claimsMissingReceipts > 0 {
		report.Warnings = append(report.Warnings, Warning{Code: "claim_audit.missing_fact_receipts", Detail: "one or more claims lack a resolvable Fact or Evidence receipt"})
	}
	sort.Slice(report.Warnings, func(i, j int) bool {
		if report.Warnings[i].Code != report.Warnings[j].Code {
			return report.Warnings[i].Code < report.Warnings[j].Code
		}
		return report.Warnings[i].Detail < report.Warnings[j].Detail
	})
}

func claimHasSupportingTest(claim architecture.Claim, facts map[string]architecture.ClaimFactReceipt) bool {
	for _, ref := range claim.SupportingEvidence {
		if strings.HasPrefix(ref, "test:") {
			return true
		}
	}
	for _, file := range claim.Scope.Files {
		if strings.HasSuffix(file, "_test.go") {
			return true
		}
	}
	for _, id := range claim.PremiseFacts {
		fact, ok := facts[id]
		if ok && (fact.Fact.Evidence.TestName != "" || strings.HasSuffix(fact.Fact.Evidence.SourceFile, "_test.go")) {
			return true
		}
	}
	return false
}

func claimHasCompleteReceipt(claim architecture.Claim, facts map[string]architecture.ClaimFactReceipt) bool {
	if len(claim.PremiseFacts) == 0 {
		return len(claim.SupportingEvidence)+len(claim.RefutingEvidence) > 0
	}
	for _, id := range claim.PremiseFacts {
		if _, ok := facts[id]; !ok {
			return false
		}
	}
	return true
}

func counts(values map[string]int) []Count {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]Count, 0, len(keys))
	for _, key := range keys {
		out = append(out, Count{Key: key, Count: values[key]})
	}
	return out
}

func valueOrUnknown(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return "unknown"
}

func normalized(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		value := strings.TrimSpace(raw)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
