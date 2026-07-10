// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/awareness-graph/golang/contractassess"
	"gopkg.in/yaml.v3"
)

type repoEvalFixCandidate struct {
	InvariantID string   `json:"invariant_id"`
	Tests       []string `json:"tests"`
	Evidence    []string `json:"evidence"`
}

type repoEvalFixReport struct {
	RepoRoot               string                      `json:"repo_root"`
	DryRun                 bool                        `json:"dry_run"`
	CandidateCount         int                         `json:"candidate_count"`
	AppliedCount           int                         `json:"applied_count"`
	Candidates             []repoEvalFixCandidate      `json:"candidates"`
	ProposalCount          int                         `json:"proposal_count,omitempty"`
	Proposals              []repoEvalFixProposal       `json:"proposals,omitempty"`
	ContractCandidateCount int                         `json:"contract_candidate_count,omitempty"`
	ContractCandidates     []repoEvalContractCandidate `json:"contract_candidates,omitempty"`
	ContractProposalCount  int                         `json:"contract_proposal_count,omitempty"`
	ContractProposals      []repoEvalContractProposal  `json:"contract_proposals,omitempty"`
	AuthorityProposalCount int                         `json:"authority_proposal_count,omitempty"`
	AuthorityProposals     []repoEvalAuthorityProposal `json:"authority_proposals,omitempty"`
	Skipped                []string                    `json:"skipped,omitempty"`
}

type repoEvalFixProposal struct {
	InvariantID              string   `json:"invariant_id"`
	Tests                    []string `json:"tests"`
	Evidence                 []string `json:"evidence"`
	Reason                   string   `json:"reason"`
	ReplacementSuggestions   []string `json:"replacement_suggestions,omitempty"`
	InvariantYAMLSnippet     string   `json:"invariant_yaml_snippet,omitempty"`
	RequiredTestsYAMLSnippet string   `json:"required_tests_yaml_snippet,omitempty"`
}

type requiredTestDefinition struct {
	ID         string
	Title      string
	Invariants []string
	Files      []string
}

type repoEvalContractCandidate struct {
	IntentID         string   `json:"intent_id"`
	File             string   `json:"file"`
	CurrentLevel     string   `json:"current_level"`
	RecommendedLevel string   `json:"recommended_level"`
	RequiredActions  []string `json:"required_actions,omitempty"`
}

type repoEvalContractProposal struct {
	IntentID          string   `json:"intent_id"`
	File              string   `json:"file"`
	CurrentLevel      string   `json:"current_level"`
	RecommendedLevel  string   `json:"recommended_level"`
	Reason            string   `json:"reason"`
	RequiredActions   []string `json:"required_actions,omitempty"`
	ExpressedBy       []string `json:"expressed_by,omitempty"`
	RequiredTests     []string `json:"required_tests,omitempty"`
	IntentYAMLSnippet string   `json:"intent_yaml_snippet,omitempty"`
}

type repoEvalAuthorityProposal struct {
	IntentID           string   `json:"intent_id"`
	File               string   `json:"file"`
	Level              string   `json:"level"`
	Reason             string   `json:"reason"`
	MissingExpressedBy []string `json:"missing_expressed_by,omitempty"`
	DocOnlyExpressedBy []string `json:"doc_only_expressed_by,omitempty"`
	CodeExpressedBy    []string `json:"code_expressed_by,omitempty"`
}

func runRepoEvalFix(args []string) int {
	fs := flag.NewFlagSet("awg repo-eval fix", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "output as JSON")
	format := fs.String("format", "text", "output format: text | json | review")
	apply := fs.Bool("apply", false, "apply safe fixes to the repository")
	proposal := fs.Bool("proposal", false, "emit review-ready non-mutating proposals when evidence is explicit but not safe to apply")
	proposalSnippets := fs.Bool("proposal-snippets", false, "include patch-ready YAML snippets for proposals")
	repoFlag := fs.String("repo", "", "target repository root to fix (defaults to current project root)")
	svcRepoFlag := fs.String("services-repo", "", "path to services repo (auto-detect)")
	agRepoFlag := fs.String("ag-repo", "", "path to awareness-graph repo (auto-detect)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg repo-eval fix [flags]

Apply safe, evidence-backed repository fixes. Current scope:
  - populate missing critical/high invariant required_tests only when code
    annotations already declare both enforces=<invariant> and tested_by=<test>

This command never invents tests, changes architecture, or promotes authority.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *asJSON {
		*format = "json"
	}

	targetRepo, _ := resolveProjectRoot(*repoFlag)
	svcRepo, _ := resolveServicesRepo(*svcRepoFlag)
	agRepo, _ := resolveAGRepo(*agRepoFlag, svcRepo)
	if svcRepo == "" && agRepo == "" {
		fmt.Fprintln(os.Stderr, "awg repo-eval fix: cannot find repos; use --services-repo / --ag-repo")
		return 1
	}
	target, err := resolveRepoEvalTarget(targetRepo, svcRepo, agRepo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg repo-eval fix: %v\n", err)
		return 1
	}

	report, err := runSafeInvariantTestFixWithMode(target.root, *apply, *proposal, *proposalSnippets)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg repo-eval fix: %v\n", err)
		return 1
	}

	if *format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
		return 0
	}
	if *format == "review" {
		fmt.Print(renderRepoEvalFixReview(report, *proposalSnippets))
		return 0
	}

	fmt.Printf("Repository fix plan for %s\n\n", target.root)
	if len(report.Candidates) == 0 {
		if len(report.Proposals) == 0 && len(report.ContractProposals) == 0 && len(report.AuthorityProposals) == 0 {
			fmt.Println("No safe evidence-backed fixes found.")
			return 0
		}
		fmt.Println("No safe auto-fixes found.")
		fmt.Println()
		if len(report.Proposals) > 0 {
			fmt.Printf("proposal: %d candidate(s)\n", report.ProposalCount)
			for _, p := range report.Proposals {
				fmt.Printf("  - %s -> %s\n", p.InvariantID, strings.Join(p.Tests, ", "))
				fmt.Printf("      reason: %s\n", p.Reason)
				for _, ev := range p.Evidence {
					fmt.Printf("      evidence: %s\n", ev)
				}
				for _, repl := range p.ReplacementSuggestions {
					fmt.Printf("      replacement: %s\n", repl)
				}
				if *proposalSnippets {
					if p.InvariantYAMLSnippet != "" {
						fmt.Println("      invariants.yaml:")
						printIndentedBlock(p.InvariantYAMLSnippet, "        ")
					}
					if p.RequiredTestsYAMLSnippet != "" {
						fmt.Println("      required_tests.yaml:")
						printIndentedBlock(p.RequiredTestsYAMLSnippet, "        ")
					}
				}
			}
		}
		if len(report.ContractProposals) > 0 {
			fmt.Println()
			fmt.Printf("contract proposal: %d candidate(s)\n", report.ContractProposalCount)
			for _, p := range report.ContractProposals {
				fmt.Printf("  - %s (%s)\n", p.IntentID, p.File)
				fmt.Printf("      reason: %s\n", p.Reason)
				if len(p.RequiredActions) > 0 {
					fmt.Printf("      actions: %s\n", strings.Join(p.RequiredActions, ", "))
				}
				for _, f := range p.ExpressedBy {
					fmt.Printf("      expressed_by: %s\n", f)
				}
				for _, t := range p.RequiredTests {
					fmt.Printf("      required_test: %s\n", t)
				}
				if *proposalSnippets && p.IntentYAMLSnippet != "" {
					fmt.Println("      intent file:")
					printIndentedBlock(p.IntentYAMLSnippet, "        ")
				}
			}
		}
		if len(report.AuthorityProposals) > 0 {
			fmt.Println()
			fmt.Printf("authority review: %d finding(s)\n", report.AuthorityProposalCount)
			for _, p := range report.AuthorityProposals {
				fmt.Printf("  - %s (%s)\n", p.IntentID, p.File)
				fmt.Printf("      reason: %s\n", p.Reason)
				for _, anchor := range p.MissingExpressedBy {
					fmt.Printf("      missing_expressed_by: %s\n", anchor)
				}
				for _, anchor := range p.DocOnlyExpressedBy {
					fmt.Printf("      doc_expressed_by: %s\n", anchor)
				}
			}
		}
		return 0
	}
	mode := "dry-run"
	if *apply {
		mode = "applied"
	}
	fmt.Printf("%s: %d candidate(s), %d applied\n", mode, report.CandidateCount, report.AppliedCount)
	for _, c := range report.Candidates {
		fmt.Printf("  - %s -> %s\n", c.InvariantID, strings.Join(c.Tests, ", "))
		for _, ev := range c.Evidence {
			fmt.Printf("      evidence: %s\n", ev)
		}
	}
	for _, s := range report.Skipped {
		fmt.Printf("  - skipped: %s\n", s)
	}
	for _, c := range report.ContractCandidates {
		fmt.Printf("  - contract %s (%s): %s -> %s\n", c.IntentID, c.File, c.CurrentLevel, c.RecommendedLevel)
	}
	return 0
}

func runSafeInvariantTestFix(repoRoot string, apply bool) (repoEvalFixReport, error) {
	return runSafeInvariantTestFixWithMode(repoRoot, apply, false, false)
}

func runSafeInvariantTestFixWithMode(repoRoot string, apply, includeProposals, includeProposalSnippets bool) (repoEvalFixReport, error) {
	report := repoEvalFixReport{
		RepoRoot: repoRoot,
		DryRun:   !apply,
	}
	invariantsPath := filepath.Join(repoRoot, "docs", "awareness", "invariants.yaml")
	invariantsRaw, err := os.ReadFile(invariantsPath)
	if err != nil {
		return report, err
	}
	requiredTestsPath := filepath.Join(repoRoot, "docs", "awareness", "required_tests.yaml")
	requiredTestsRaw, err := os.ReadFile(requiredTestsPath)
	if err != nil {
		return report, err
	}
	evidence, proposals, err := collectAnnotationInvariantTests(repoRoot)
	if err != nil {
		return report, err
	}
	updatedInvariants, updatedRequiredTests, candidates, appliedCount, err := applyInvariantRequiredTests(invariantsRaw, requiredTestsRaw, evidence)
	if err != nil {
		return report, err
	}
	report.Candidates = candidates
	report.CandidateCount = len(candidates)
	if includeProposals {
		if includeProposalSnippets {
			for i := range proposals {
				populateProposalSnippets(&proposals[i])
			}
		}
		report.Proposals = proposals
		report.ProposalCount = len(proposals)
	}
	if apply {
		report.AppliedCount = appliedCount
	}
	if apply && appliedCount > 0 {
		if err := os.WriteFile(invariantsPath, updatedInvariants, 0o644); err != nil {
			return report, err
		}
		if err := os.WriteFile(requiredTestsPath, updatedRequiredTests, 0o644); err != nil {
			return report, err
		}
	}
	contractCandidates, contractProposals, err := collectContractLane(repoRoot, includeProposalSnippets)
	if err != nil {
		return report, err
	}
	report.ContractCandidates = contractCandidates
	report.ContractCandidateCount = len(contractCandidates)
	if includeProposals {
		report.ContractProposals = contractProposals
		report.ContractProposalCount = len(contractProposals)
		authorityProposals, err := collectAuthorityLane(repoRoot)
		if err != nil {
			return report, err
		}
		report.AuthorityProposals = authorityProposals
		report.AuthorityProposalCount = len(authorityProposals)
	}
	if apply && len(contractCandidates) > 0 {
		appliedContracts, err := applyContractCandidates(repoRoot, contractCandidates)
		if err != nil {
			return report, err
		}
		report.AppliedCount += appliedContracts
	}
	if len(candidates) == 0 {
		report.Skipped = append(report.Skipped, "no missing critical/high invariants had explicit enforces+tested_by evidence")
	}
	if len(contractCandidates) == 0 {
		report.Skipped = append(report.Skipped, "no contract intents met the safe auto-promotion bar")
	}
	if includeProposals && len(report.AuthorityProposals) == 0 {
		report.Skipped = append(report.Skipped, "no authority evidence review findings")
	}
	return report, nil
}

func collectContractLane(repoRoot string, includeSnippets bool) ([]repoEvalContractCandidate, []repoEvalContractProposal, error) {
	intentDir := filepath.Join(repoRoot, "docs", "intent")
	if _, err := os.Stat(intentDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	entries, err := os.ReadDir(intentDir)
	if err != nil {
		return nil, nil, err
	}
	var candidates []repoEvalContractCandidate
	var proposals []repoEvalContractProposal
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		path := filepath.Join(intentDir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		var doc auditIntentDoc
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(doc.ID) == "" || strings.EqualFold(strings.TrimSpace(doc.Status), "deprecated") {
			continue
		}
		assessment := contractassess.Assess(assessmentInputForIntent(doc))
		rel, _ := filepath.Rel(repoRoot, path)
		rel = filepath.ToSlash(rel)
		if !eligibleForContractPromotion(doc) {
			continue
		}
		if assessment.Outcome == contractassess.ContractSynthesisSafe {
			candidates = append(candidates, repoEvalContractCandidate{
				IntentID:         doc.ID,
				File:             rel,
				CurrentLevel:     strings.TrimSpace(doc.Level),
				RecommendedLevel: "contract",
				RequiredActions:  requiredActionStrings(assessment.RequiredActions),
			})
		}
		if assessment.Outcome == contractassess.ContractSynthesisSafe || assessment.Outcome == contractassess.ContractProposalOnly {
			prop := repoEvalContractProposal{
				IntentID:         doc.ID,
				File:             rel,
				CurrentLevel:     strings.TrimSpace(doc.Level),
				RecommendedLevel: "contract",
				Reason:           contractProposalReason(assessment),
				RequiredActions:  requiredActionStrings(assessment.RequiredActions),
				ExpressedBy:      nonEmptyStrings(doc.ExpressedBy),
				RequiredTests:    nonEmptyStrings(doc.RequiredTests),
			}
			if includeSnippets {
				prop.IntentYAMLSnippet = contractProposalSnippet(doc)
			}
			proposals = append(proposals, prop)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].IntentID < candidates[j].IntentID })
	sort.Slice(proposals, func(i, j int) bool { return proposals[i].IntentID < proposals[j].IntentID })
	return candidates, proposals, nil
}

func collectAuthorityLane(repoRoot string) ([]repoEvalAuthorityProposal, error) {
	intentDir := filepath.Join(repoRoot, "docs", "intent")
	if _, err := os.Stat(intentDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	entries, err := os.ReadDir(intentDir)
	if err != nil {
		return nil, err
	}
	var proposals []repoEvalAuthorityProposal
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		path := filepath.Join(intentDir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var doc auditIntentDoc
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return nil, err
		}
		if strings.TrimSpace(doc.ID) == "" || strings.EqualFold(strings.TrimSpace(doc.Status), "deprecated") {
			continue
		}
		expressedBy := nonEmptyStrings(doc.ExpressedBy)
		if len(expressedBy) == 0 {
			continue
		}
		var missing []string
		var docsOnly []string
		var codeAnchors []string
		for _, anchor := range expressedBy {
			anchor = filepath.ToSlash(strings.TrimSpace(anchor))
			if anchor == "" {
				continue
			}
			exists, err := authorityAnchorExists(repoRoot, anchor)
			if err != nil {
				return nil, err
			}
			if !exists {
				missing = append(missing, anchor)
				continue
			}
			if strings.HasPrefix(anchor, "docs/") {
				docsOnly = append(docsOnly, anchor)
				continue
			}
			codeAnchors = append(codeAnchors, anchor)
		}
		docOnlyNeedsReview := len(docsOnly) > 0 && len(codeAnchors) == 0 && levelNeedsExecutableAuthority(doc.Level)
		if len(missing) == 0 && !docOnlyNeedsReview {
			continue
		}
		reasons := make([]string, 0, 2)
		if len(missing) > 0 {
			reasons = append(reasons, "expressed_by cites missing repo paths")
		}
		if docOnlyNeedsReview {
			reasons = append(reasons, "expressed_by relies only on docs anchors; review whether executable authority is missing")
		}
		rel, _ := filepath.Rel(repoRoot, path)
		proposals = append(proposals, repoEvalAuthorityProposal{
			IntentID:           doc.ID,
			File:               filepath.ToSlash(rel),
			Level:              strings.TrimSpace(doc.Level),
			Reason:             strings.Join(reasons, "; "),
			MissingExpressedBy: missing,
			DocOnlyExpressedBy: docsOnly,
			CodeExpressedBy:    codeAnchors,
		})
	}
	sort.Slice(proposals, func(i, j int) bool { return proposals[i].IntentID < proposals[j].IntentID })
	return proposals, nil
}

func levelNeedsExecutableAuthority(level string) bool {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "contract", "mechanism", "operator_model", "pattern", "invariant":
		return true
	default:
		return false
	}
}

func authorityAnchorExists(repoRoot, anchor string) (bool, error) {
	anchor = filepath.ToSlash(strings.TrimSpace(anchor))
	if anchor == "" || strings.ContainsAny(anchor, "*?[") {
		return false, nil
	}
	candidates := authorityAnchorCandidates(repoRoot, anchor)
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return true, nil
		} else if !os.IsNotExist(err) {
			return false, err
		}
	}
	return false, nil
}

func authorityAnchorCandidates(repoRoot, anchor string) []string {
	anchor = filepath.ToSlash(strings.TrimSpace(anchor))
	seen := map[string]bool{}
	var out []string
	add := func(path string) {
		if path == "" {
			return
		}
		clean := filepath.Clean(path)
		if seen[clean] {
			return
		}
		seen[clean] = true
		out = append(out, clean)
	}
	if filepath.IsAbs(anchor) {
		add(filepath.FromSlash(anchor))
		return out
	}
	add(filepath.Join(repoRoot, filepath.FromSlash(anchor)))
	parts := strings.Split(anchor, "/")
	if len(parts) > 1 {
		repoName := filepath.Base(repoRoot)
		if parts[0] == repoName {
			add(filepath.Join(repoRoot, filepath.Join(parts[1:]...)))
		}
		add(filepath.Join(filepath.Dir(repoRoot), parts[0], filepath.Join(parts[1:]...)))
	}
	return out
}

func eligibleForContractPromotion(doc auditIntentDoc) bool {
	level := strings.ToLower(strings.TrimSpace(doc.Level))
	if level == "contract" {
		return false
	}
	if level != "mechanism" && level != "operator_model" {
		return false
	}
	return len(nonEmptyStrings(doc.ExpressedBy)) > 0 && len(nonEmptyStrings(doc.RequiredTests)) > 0
}

func applyContractCandidates(repoRoot string, candidates []repoEvalContractCandidate) (int, error) {
	applied := 0
	for _, c := range candidates {
		path := filepath.Join(repoRoot, filepath.FromSlash(c.File))
		raw, err := os.ReadFile(path)
		if err != nil {
			return applied, err
		}
		updated, ok := replaceTopLevelYAMLScalar(raw, "level", c.RecommendedLevel)
		if !ok {
			return applied, fmt.Errorf("%s: missing top-level level field", c.File)
		}
		if err := os.WriteFile(path, updated, 0o644); err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
}

func replaceTopLevelYAMLScalar(raw []byte, key, value string) ([]byte, bool) {
	prefix := key + ":"
	lines := strings.SplitAfter(string(raw), "\n")
	for i, line := range lines {
		content := strings.TrimRight(line, "\r\n")
		eol := line[len(content):]
		if !strings.HasPrefix(content, prefix) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(content, prefix))
		comment := ""
		if idx := strings.Index(rest, " #"); idx >= 0 {
			comment = rest[idx:]
		}
		lines[i] = prefix + " " + value + comment + eol
		return []byte(strings.Join(lines, "")), true
	}
	return raw, false
}

func requiredActionStrings(in []contractassess.RequiredAction) []string {
	out := make([]string, 0, len(in))
	for _, a := range in {
		out = append(out, string(a))
	}
	return out
}

func contractProposalReason(a contractassess.AssessmentResult) string {
	switch a.Outcome {
	case contractassess.ContractSynthesisSafe:
		return "intent is evidence-strong enough to draft as a contract; review promotion before applying"
	case contractassess.ContractProposalOnly:
		return "intent has meaningful grounding but does not meet the safe auto-promotion bar yet"
	default:
		return "intent does not yet meet the contract promotion bar"
	}
}

func contractProposalSnippet(doc auditIntentDoc) string {
	var b strings.Builder
	b.WriteString("id: " + doc.ID + "\n")
	b.WriteString("level: contract\n")
	if tests := nonEmptyStrings(doc.RequiredTests); len(tests) > 0 {
		b.WriteString("required_tests:\n")
		for _, t := range tests {
			b.WriteString("  - " + t + "\n")
		}
	}
	if files := nonEmptyStrings(doc.ExpressedBy); len(files) > 0 {
		b.WriteString("expressed_by:\n")
		for _, f := range files {
			b.WriteString("  - " + f + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

type invariantTestEvidence struct {
	tests       map[string]bool
	evidence    map[string]bool
	filesByTest map[string]map[string]bool
}

func collectAnnotationInvariantTests(repoRoot string) (map[string]invariantTestEvidence, []repoEvalFixProposal, error) {
	out := map[string]invariantTestEvidence{}
	discoveredTests, discoveredTestList, err := collectDiscoveredGoTests(repoRoot)
	if err != nil {
		return nil, nil, err
	}
	proposalIndex := map[string]*repoEvalFixProposal{}
	err = filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", "dist", "bin":
				return filepath.SkipDir
			}
			return nil
		}
		if !isAwareSourceFile(path) {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		blocks, err := parseAwarenessBlocks(path)
		if err != nil {
			return err
		}
		for _, block := range blocks {
			invariants := block.Keys["enforces"]
			tests := block.Keys["tested_by"]
			if len(invariants) == 0 || len(tests) == 0 {
				continue
			}
			var normalizedInvariants []string
			for _, inv := range invariants {
				if id, ok := localInvariantID(inv); ok {
					normalizedInvariants = append(normalizedInvariants, id)
				}
			}
			if len(normalizedInvariants) == 0 {
				continue
			}
			var concreteTests []string
			var undiscoveredTests []string
			for _, testID := range tests {
				norm := normalizeTestRef(testID)
				if isConcreteTestRef(testID) && discoveredTests[norm] {
					concreteTests = append(concreteTests, norm)
				} else if isConcreteTestRef(testID) {
					undiscoveredTests = append(undiscoveredTests, norm)
				}
			}
			ev := fmt.Sprintf("%s:%d", filepath.ToSlash(rel), block.Line)
			if len(undiscoveredTests) > 0 {
				for _, inv := range normalizedInvariants {
					p := proposalIndex[inv]
					if p == nil {
						p = &repoEvalFixProposal{
							InvariantID: inv,
							Reason:      "annotation cites tested_by targets that are not discovered on disk; review or repair the stale evidence before promotion",
						}
						proposalIndex[inv] = p
					}
					p.Tests = mergeStrings(p.Tests, undiscoveredTests)
					p.Evidence = mergeStrings(p.Evidence, []string{ev})
					for _, testID := range undiscoveredTests {
						p.ReplacementSuggestions = mergeStrings(p.ReplacementSuggestions, suggestReplacementTests(testID, discoveredTestList))
					}
				}
			}
			if len(concreteTests) == 0 {
				continue
			}
			for _, inv := range normalizedInvariants {
				cur := out[inv]
				if cur.tests == nil {
					cur.tests = map[string]bool{}
				}
				if cur.evidence == nil {
					cur.evidence = map[string]bool{}
				}
				for _, testID := range concreteTests {
					cur.tests[testID] = true
					if cur.filesByTest == nil {
						cur.filesByTest = map[string]map[string]bool{}
					}
					if cur.filesByTest[testID] == nil {
						cur.filesByTest[testID] = map[string]bool{}
					}
					cur.filesByTest[testID][filepath.ToSlash(rel)] = true
					if testPath, _, ok := strings.Cut(testID, ":"); ok {
						cur.filesByTest[testID][filepath.ToSlash(testPath)] = true
					}
				}
				cur.evidence[ev] = true
				out[inv] = cur
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	var proposals []repoEvalFixProposal
	for _, p := range proposalIndex {
		sort.Strings(p.Tests)
		sort.Strings(p.Evidence)
		sort.Strings(p.ReplacementSuggestions)
		proposals = append(proposals, *p)
	}
	sort.Slice(proposals, func(i, j int) bool { return proposals[i].InvariantID < proposals[j].InvariantID })
	return out, proposals, nil
}

func populateProposalSnippets(p *repoEvalFixProposal) {
	if p == nil {
		return
	}
	tests := cappedStrings(p.ReplacementSuggestions, 3)
	if len(tests) == 0 {
		tests = p.Tests
	}
	if len(tests) == 0 {
		return
	}
	var inv strings.Builder
	inv.WriteString("- id: " + p.InvariantID + "\n")
	inv.WriteString("  required_tests:\n")
	for _, testID := range tests {
		inv.WriteString("    - " + testID + "\n")
	}
	p.InvariantYAMLSnippet = strings.TrimRight(inv.String(), "\n")

	var req strings.Builder
	for _, testID := range tests {
		req.WriteString("- id: " + testID + "\n")
		req.WriteString("  title: " + testTitleFromID(testID) + "\n")
		req.WriteString("  protects:\n")
		req.WriteString("    invariants:\n")
		req.WriteString("      - " + p.InvariantID + "\n")
		if testPath, _, ok := strings.Cut(testID, ":"); ok {
			req.WriteString("    files:\n")
			req.WriteString("      - " + filepath.ToSlash(testPath) + "\n")
		}
	}
	p.RequiredTestsYAMLSnippet = strings.TrimRight(req.String(), "\n")
}

func printIndentedBlock(text, indent string) {
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		fmt.Println(indent + line)
	}
}

func renderRepoEvalFixReview(report repoEvalFixReport, includeSnippets bool) string {
	var b strings.Builder
	b.WriteString("Repository review fixes for " + report.RepoRoot + "\n\n")

	type fileGroup struct {
		file  string
		lines []string
	}
	groupMap := map[string][]string{}
	add := func(file, line string) {
		groupMap[file] = append(groupMap[file], line)
	}

	for _, p := range report.Proposals {
		add("docs/awareness/invariants.yaml", fmt.Sprintf("- Review stale invariant test evidence for `%s`: %s.", p.InvariantID, p.Reason))
		if len(p.ReplacementSuggestions) > 0 {
			add("docs/awareness/invariants.yaml", "  Suggested replacements: "+strings.Join(cappedStrings(p.ReplacementSuggestions, 3), ", "))
		}
		if includeSnippets && p.InvariantYAMLSnippet != "" {
			add("docs/awareness/invariants.yaml", "  Patch snippet:\n"+indentBlock(p.InvariantYAMLSnippet, "    "))
		}
		if includeSnippets && p.RequiredTestsYAMLSnippet != "" {
			add("docs/awareness/required_tests.yaml", fmt.Sprintf("- Add or repair required test anchors for `%s`.", p.InvariantID))
			add("docs/awareness/required_tests.yaml", "  Patch snippet:\n"+indentBlock(p.RequiredTestsYAMLSnippet, "    "))
		}
	}
	for _, p := range report.ContractProposals {
		add(p.File, fmt.Sprintf("- Consider promoting `%s` from `%s` to `%s`: %s.", p.IntentID, p.CurrentLevel, p.RecommendedLevel, p.Reason))
		if len(p.RequiredActions) > 0 {
			add(p.File, "  Required actions: "+strings.Join(p.RequiredActions, ", "))
		}
		if includeSnippets && p.IntentYAMLSnippet != "" {
			add(p.File, "  Patch snippet:\n"+indentBlock(p.IntentYAMLSnippet, "    "))
		}
	}
	for _, p := range report.AuthorityProposals {
		add(p.File, fmt.Sprintf("- Review authority evidence for `%s` (`%s`): %s.", p.IntentID, p.Level, p.Reason))
		if len(p.MissingExpressedBy) > 0 {
			add(p.File, "  Missing expressed_by anchors: "+strings.Join(p.MissingExpressedBy, ", "))
		}
		if len(p.DocOnlyExpressedBy) > 0 {
			add(p.File, "  Docs-only expressed_by anchors: "+strings.Join(p.DocOnlyExpressedBy, ", "))
		}
		if len(p.CodeExpressedBy) > 0 {
			add(p.File, "  Existing code anchors: "+strings.Join(p.CodeExpressedBy, ", "))
		}
	}

	files := make([]string, 0, len(groupMap))
	for file := range groupMap {
		files = append(files, file)
	}
	sort.Strings(files)
	if len(files) == 0 {
		b.WriteString("No review-ready proposals.\n")
		return b.String()
	}
	for i, file := range files {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(file + "\n")
		for _, line := range groupMap[file] {
			b.WriteString(line)
			if !strings.HasSuffix(line, "\n") {
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

func indentBlock(text, indent string) string {
	var b strings.Builder
	for i, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(indent)
		b.WriteString(line)
	}
	return b.String()
}

func collectDiscoveredGoTests(repoRoot string) (map[string]bool, []string, error) {
	out := map[string]bool{}
	var list []string
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", "dist", "bin":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name == nil {
				continue
			}
			name := fn.Name.Name
			if !strings.HasPrefix(name, "Test") {
				continue
			}
			id := filepath.ToSlash(rel) + ":" + name
			out[id] = true
			list = append(list, id)
		}
		return nil
	})
	sort.Strings(list)
	return out, list, err
}

func suggestReplacementTests(target string, discovered []string) []string {
	type scored struct {
		id    string
		score int
	}
	targetPath, targetSym, _ := strings.Cut(target, ":")
	targetDir := filepath.ToSlash(filepath.Dir(targetPath))
	var ranked []scored
	for _, cand := range discovered {
		candPath, candSym, ok := strings.Cut(cand, ":")
		if !ok {
			continue
		}
		score := 0
		candDir := filepath.ToSlash(filepath.Dir(candPath))
		switch {
		case candPath == targetPath:
			score += 100
		case candDir == targetDir:
			score += 45
		case pathBaseWithoutTestSuffix(candPath) == pathBaseWithoutTestSuffix(targetPath):
			score += 35
		case sharedDirPrefixDepth(candDir, targetDir) >= 2:
			score += 20
		}
		score += commonPrefixLen(strings.ToLower(candSym), strings.ToLower(targetSym)) * 4
		if strings.Contains(strings.ToLower(candSym), strings.ToLower(trimTestPrefix(targetSym))) {
			score += 15
		}
		if strings.Contains(strings.ToLower(targetSym), strings.ToLower(trimTestPrefix(candSym))) {
			score += 10
		}
		if score >= 45 {
			ranked = append(ranked, scored{id: cand, score: score})
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].id < ranked[j].id
	})
	out := make([]string, 0, min(2, len(ranked)))
	for i := 0; i < len(ranked) && i < 2; i++ {
		out = append(out, ranked[i].id)
	}
	return out
}

func pathBaseWithoutTestSuffix(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".go")
	base = strings.TrimSuffix(base, "_test")
	return base
}

func sharedDirPrefixDepth(a, b string) int {
	ap := strings.Split(strings.Trim(a, "/"), "/")
	bp := strings.Split(strings.Trim(b, "/"), "/")
	n := min(len(ap), len(bp))
	depth := 0
	for i := 0; i < n; i++ {
		if ap[i] != bp[i] {
			break
		}
		depth++
	}
	return depth
}

func commonPrefixLen(a, b string) int {
	n := min(len(a), len(b))
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return i
}

func trimTestPrefix(s string) string {
	return strings.TrimPrefix(s, "Test")
}

type awarenessBlock struct {
	Line int
	Keys map[string][]string
}

func parseAwarenessBlocks(path string) ([]awarenessBlock, error) {
	if strings.EqualFold(filepath.Ext(path), ".go") {
		return parseGoAwarenessBlocks(path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	var out []awarenessBlock
	var cur awarenessBlock
	for i, line := range lines {
		key, value, ok := parseAwarenessLine(line)
		if !ok {
			if len(cur.Keys) > 0 {
				out = append(out, cur)
				cur = awarenessBlock{}
			}
			continue
		}
		if cur.Keys == nil {
			cur = awarenessBlock{Line: i + 1, Keys: map[string][]string{}}
		}
		cur.Keys[key] = append(cur.Keys[key], value)
	}
	if len(cur.Keys) > 0 {
		out = append(out, cur)
	}
	return out, nil
}

func parseGoAwarenessBlocks(path string) ([]awarenessBlock, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var out []awarenessBlock
	for _, group := range file.Comments {
		var block awarenessBlock
		for _, comment := range group.List {
			key, value, ok := parseAwarenessCommentText(stripCommentPrefix(comment.Text))
			if !ok {
				continue
			}
			if block.Keys == nil {
				block = awarenessBlock{
					Line: fset.Position(comment.Pos()).Line,
					Keys: map[string][]string{},
				}
			}
			block.Keys[key] = append(block.Keys[key], value)
		}
		if len(block.Keys) > 0 {
			out = append(out, block)
		}
	}
	return out, nil
}

func parseAwarenessLine(line string) (key, value string, ok bool) {
	text, ok := stripCommentPrefixOK(line)
	if !ok {
		return "", "", false
	}
	return parseAwarenessCommentText(text)
}

func stripCommentPrefix(text string) string {
	text, _ = stripCommentPrefixOK(text)
	return text
}

func stripCommentPrefixOK(text string) (string, bool) {
	text = strings.TrimSpace(text)
	for _, prefix := range []string{"//", "#", "/*", "*", "--"} {
		if strings.HasPrefix(text, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(text, prefix)), true
		}
	}
	return "", false
}

func parseAwarenessCommentText(text string) (key, value string, ok bool) {
	if !strings.HasPrefix(text, "@awareness ") {
		return "", "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(text, "@awareness "))
	key, value, ok = strings.Cut(rest, "=")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(strings.TrimSuffix(value, "*/"))
	if key == "" || value == "" {
		return "", "", false
	}
	return key, value, true
}

func isAwareSourceFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".py", ".rs":
		return true
	default:
		return false
	}
}

func localInvariantID(ref string) (string, bool) {
	_, rhs, ok := strings.Cut(strings.TrimSpace(ref), ":")
	if !ok {
		return "", false
	}
	if !strings.HasPrefix(rhs, "invariant.") {
		return "", false
	}
	id := strings.TrimPrefix(rhs, "invariant.")
	if id == "" {
		return "", false
	}
	return id, true
}

func isConcreteTestRef(ref string) bool {
	ref = normalizeTestRef(ref)
	path, sym, ok := strings.Cut(ref, ":")
	return ok && path != "" && sym != "" && strings.Contains(path, ".")
}

func normalizeTestRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if strings.Contains(ref, "::") {
		return strings.Replace(ref, "::", ":", 1)
	}
	return ref
}

func applyInvariantRequiredTests(invariantsRaw, requiredTestsRaw []byte, evidence map[string]invariantTestEvidence) ([]byte, []byte, []repoEvalFixCandidate, int, error) {
	var invariantsDoc yaml.Node
	if err := yaml.Unmarshal(invariantsRaw, &invariantsDoc); err != nil {
		return nil, nil, nil, 0, err
	}
	var requiredTestsDoc yaml.Node
	if err := yaml.Unmarshal(requiredTestsRaw, &requiredTestsDoc); err != nil {
		return nil, nil, nil, 0, err
	}
	invariantNodes := yamlMapLookup(invariantsDoc.Content[0], "invariants")
	if invariantNodes == nil || invariantNodes.Kind != yaml.SequenceNode {
		return nil, nil, nil, 0, fmt.Errorf("invariants.yaml missing invariants sequence")
	}
	requiredTestNodes := yamlMapLookup(requiredTestsDoc.Content[0], "required_tests")
	if requiredTestNodes == nil {
		requiredTestNodes = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		requiredTestsDoc.Content[0].Content = append(requiredTestsDoc.Content[0].Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "required_tests"},
			requiredTestNodes,
		)
	}
	if requiredTestNodes.Kind == yaml.ScalarNode && strings.TrimSpace(requiredTestNodes.Value) == "" {
		requiredTestNodes.Kind = yaml.SequenceNode
		requiredTestNodes.Tag = "!!seq"
		requiredTestNodes.Value = ""
		requiredTestNodes.Content = nil
	}
	if requiredTestNodes.Kind != yaml.SequenceNode {
		return nil, nil, nil, 0, fmt.Errorf("required_tests.yaml missing required_tests sequence")
	}
	requiredTestIndex := indexRequiredTests(requiredTestNodes)

	var candidates []repoEvalFixCandidate
	applied := 0
	for _, item := range invariantNodes.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		id := yamlScalarValue(item, "id")
		severity := yamlScalarValue(item, "severity")
		if id == "" || (severity != "critical" && severity != "high") {
			continue
		}
		if len(yamlSeqValues(item, "required_tests")) > 0 {
			continue
		}
		if strings.TrimSpace(yamlScalarValue(item, "test_not_applicable_reason")) != "" {
			continue
		}
		ev, ok := evidence[id]
		if !ok || len(ev.tests) == 0 {
			continue
		}
		tests := sortedStringSet(ev.tests)
		candidate := repoEvalFixCandidate{
			InvariantID: id,
			Tests:       tests,
			Evidence:    sortedStringSet(ev.evidence),
		}
		candidates = append(candidates, candidate)
		setYAMLSeq(item, "required_tests", tests)
		for _, testID := range tests {
			defNode := requiredTestIndex[testID]
			files := sortedStringSet(ev.filesByTest[testID])
			if defNode == nil {
				defNode = appendRequiredTest(requiredTestNodes, requiredTestDefinition{
					ID:         testID,
					Title:      testTitleFromID(testID),
					Invariants: []string{id},
					Files:      files,
				})
				requiredTestIndex[testID] = defNode
				continue
			}
			mergeRequiredTestProtection(defNode, id, files)
		}
		applied++
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].InvariantID < candidates[j].InvariantID })

	var invariantsBuf bytes.Buffer
	enc := yaml.NewEncoder(&invariantsBuf)
	enc.SetIndent(2)
	if err := enc.Encode(&invariantsDoc); err != nil {
		return nil, nil, nil, 0, err
	}
	_ = enc.Close()
	var requiredTestsBuf bytes.Buffer
	enc = yaml.NewEncoder(&requiredTestsBuf)
	enc.SetIndent(2)
	if err := enc.Encode(&requiredTestsDoc); err != nil {
		return nil, nil, nil, 0, err
	}
	_ = enc.Close()
	return invariantsBuf.Bytes(), requiredTestsBuf.Bytes(), candidates, applied, nil
}

func yamlMapLookup(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func yamlScalarValue(node *yaml.Node, key string) string {
	val := yamlMapLookup(node, key)
	if val == nil || val.Kind != yaml.ScalarNode {
		return ""
	}
	return strings.TrimSpace(val.Value)
}

func yamlSeqValues(node *yaml.Node, key string) []string {
	val := yamlMapLookup(node, key)
	if val == nil || val.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]string, 0, len(val.Content))
	for _, child := range val.Content {
		if child.Kind == yaml.ScalarNode {
			out = append(out, strings.TrimSpace(child.Value))
		}
	}
	return out
}

func setYAMLSeq(node *yaml.Node, key string, values []string) {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, v := range values {
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v})
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1] = seq
			return
		}
	}
	node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, seq)
}

func setYAMLScalar(node *yaml.Node, key, value string) {
	val := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1] = val
			return
		}
	}
	node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, val)
}

func indexRequiredTests(seq *yaml.Node) map[string]*yaml.Node {
	out := map[string]*yaml.Node{}
	for _, item := range seq.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		id := yamlScalarValue(item, "id")
		if id != "" {
			out[id] = item
		}
	}
	return out
}

func appendRequiredTest(seq *yaml.Node, def requiredTestDefinition) *yaml.Node {
	item := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	appendMapScalar(item, "id", def.ID)
	appendMapScalar(item, "title", def.Title)
	protects := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	appendMapSeq(protects, "invariants", def.Invariants)
	appendMapSeq(protects, "files", def.Files)
	item.Content = append(item.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "protects"}, protects)
	seq.Content = append(seq.Content, item)
	return item
}

func mergeRequiredTestProtection(item *yaml.Node, invariantID string, files []string) {
	protects := yamlMapLookup(item, "protects")
	if protects == nil || protects.Kind != yaml.MappingNode {
		protects = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		item.Content = append(item.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "protects"}, protects)
	}
	appendUniqueSeqValue(protects, "invariants", invariantID)
	for _, f := range files {
		appendUniqueSeqValue(protects, "files", f)
	}
}

func appendMapScalar(node *yaml.Node, key, value string) {
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}

func appendMapSeq(node *yaml.Node, key string, values []string) {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, v := range values {
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v})
	}
	node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, seq)
}

func appendUniqueSeqValue(node *yaml.Node, key, value string) {
	seq := yamlMapLookup(node, key)
	if seq == nil || seq.Kind != yaml.SequenceNode {
		seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, seq)
	}
	for _, child := range seq.Content {
		if child.Kind == yaml.ScalarNode && child.Value == value {
			return
		}
	}
	seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

func testTitleFromID(id string) string {
	_, sym, ok := strings.Cut(id, ":")
	if !ok || strings.TrimSpace(sym) == "" {
		return id
	}
	return sym
}

func sortedStringSet[K comparable](m map[string]K) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func mergeStrings(existing, incoming []string) []string {
	seen := map[string]bool{}
	for _, s := range existing {
		if strings.TrimSpace(s) != "" {
			seen[s] = true
		}
	}
	for _, s := range incoming {
		if strings.TrimSpace(s) != "" {
			seen[s] = true
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func cappedStrings(in []string, max int) []string {
	if max <= 0 || len(in) <= max {
		return append([]string(nil), in...)
	}
	return append([]string(nil), in[:max]...)
}
