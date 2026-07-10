// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/globulario/awareness-graph/golang/coverage"
	"github.com/globulario/awareness-graph/golang/repoeval"
	"gopkg.in/yaml.v3"
)

func runRepoEvalDraftUpgrade(args []string) int {
	fs := flag.NewFlagSet("awg repo-eval draft-upgrade", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "output as JSON")
	repoFlag := fs.String("repo", "", "target repository root to evaluate and draft from")
	svcRepoFlag := fs.String("services-repo", "", "path to services repo (auto-detect)")
	agRepoFlag := fs.String("ag-repo", "", "path to awareness-graph repo (auto-detect)")
	dryRun := fs.Bool("dry-run", false, "print draft contents; do not write files")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg repo-eval draft-upgrade [flags]

Draft review-only upgrade candidates from repo-eval's guarded upgrade path.
This command NEVER edits live authority. It writes only under
docs/awareness/candidates/repo_eval_upgrade/ so the importer skips the drafts
until a human reviews and promotes them separately.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	rep, target, err := evaluateRepoForDraft(*repoFlag, *svcRepoFlag, *agRepoFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg repo-eval draft-upgrade: %v\n", err)
		return 1
	}
	drafts, err := buildRepoEvalUpgradeDrafts(target.root, rep)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg repo-eval draft-upgrade: %v\n", err)
		return 1
	}

	report := repoEvalDraftUpgradeReport{
		RepoRoot:   target.root,
		DryRun:     *dryRun,
		Verdict:    rep.AgentReadiness.Verdict,
		DraftCount: len(drafts),
		Drafts:     drafts,
	}
	if *dryRun && !*asJSON {
		for _, draft := range drafts {
			fmt.Printf("# %s\n%s\n", draft.Path, draft.Content)
		}
	}
	if !*dryRun {
		for _, draft := range drafts {
			full := filepath.Join(target.root, draft.Path)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				fmt.Fprintf(os.Stderr, "awg repo-eval draft-upgrade: %v\n", err)
				return 1
			}
			if err := os.WriteFile(full, []byte(draft.Content), 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "awg repo-eval draft-upgrade: %v\n", err)
				return 1
			}
		}
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
		return 0
	}
	if *dryRun {
		fmt.Printf("drafted %d review-only upgrade candidate(s) for %s (dry-run)\n", len(drafts), target.root)
		return 0
	}
	fmt.Printf("drafted %d review-only upgrade candidate(s) under docs/awareness/candidates/repo_eval_upgrade/\n", len(drafts))
	for _, draft := range drafts {
		fmt.Printf("  - %s\n", draft.Path)
	}
	return 0
}

func evaluateRepoForDraft(repoFlag, svcRepoFlag, agRepoFlag string) (repoeval.Report, repoEvalTarget, error) {
	targetRepo, _ := resolveProjectRoot(repoFlag)
	svcRepo, _ := resolveServicesRepo(svcRepoFlag)
	agRepo, _ := resolveAGRepo(agRepoFlag, svcRepo)
	target, err := resolveRepoEvalTarget(targetRepo, svcRepo, agRepo)
	if err != nil {
		return repoeval.Report{}, repoEvalTarget{}, err
	}
	inputDirs, intentDir, graphSvcRepo, graphAGRepo, err := collectRepoEvalInputs(target, svcRepo, agRepo)
	if err != nil {
		return repoeval.Report{}, repoEvalTarget{}, err
	}
	ntBytes, _, _, err := generateNT(inputDirs, intentDir, graphSvcRepo, graphAGRepo)
	if err != nil {
		return repoeval.Report{}, repoEvalTarget{}, err
	}
	var integrityChecks []auditResult
	integrityChecks = append(integrityChecks,
		checkYAMLValidity(inputDirs, intentDir, graphSvcRepo, graphAGRepo, 0),
		checkNTValidity(ntBytes, bytesCountLines(ntBytes)),
		checkStaleFileRefs(graphSvcRepo, graphAGRepo, ntBytes),
	)
	validateChecks, err := repoEvalValidateChecks(target, inputDirs, intentDir, graphSvcRepo, graphAGRepo)
	if err != nil {
		return repoeval.Report{}, repoEvalTarget{}, err
	}
	integrityChecks = append(integrityChecks, validateChecks...)
	coverageRep, err := buildRepoCoverageReport(target.root, ntBytes)
	if err != nil {
		return repoeval.Report{}, repoEvalTarget{}, err
	}
	testStats, err := collectTestCoverageStats(target.root)
	if err != nil {
		return repoeval.Report{}, repoEvalTarget{}, err
	}
	contractStats, err := collectContractAssessmentStats(target.intentDir)
	if err != nil {
		return repoeval.Report{}, repoEvalTarget{}, err
	}
	seedStats := collectSeedStats(ntBytes)
	upgradePath, err := collectRepoEvalUpgradePath(target.root, target.intentDir)
	if err != nil {
		return repoeval.Report{}, repoEvalTarget{}, err
	}
	rep := repoeval.Evaluate(repoeval.Inputs{
		IntegrityFails:                    countAuditLevel(integrityChecks, auditFAIL),
		IntegrityWarns:                    countAuditLevel(integrityChecks, auditWARN),
		IntegrityIssues:                   repoEvalIntegrityIssues(integrityChecks),
		UpgradePath:                       upgradePath,
		WeightedCoveragePercent:           coverageRep.WeightedOverallPercent,
		CriticalCoveragePercent:           surfacePercent(coverageRep, coverage.SurfaceCritical),
		CriticalSurfaceTotal:              surfaceTotal(coverageRep, coverage.SurfaceCritical),
		HighRiskCoveragePercent:           surfacePercent(coverageRep, coverage.SurfaceHighRisk),
		HighRiskSurfaceTotal:              surfaceTotal(coverageRep, coverage.SurfaceHighRisk),
		AuthorityCoveragePercent:          surfacePercent(coverageRep, coverage.SurfaceAuthority),
		AuthoritySurfaceTotal:             surfaceTotal(coverageRep, coverage.SurfaceAuthority),
		UnknownHighRiskCount:              coverageRep.UnknownHighRiskCount,
		UnknownHighRiskFiles:              coverageRep.UnknownHighRiskFiles,
		CriticalHighInvariantCount:        testStats.criticalHighCount,
		MissingCriticalHighInvariantTests: testStats.missingCount,
		ContractFoundCount:                contractStats.found,
		ContractSynthesisSafeCount:        contractStats.synthesisSafe,
		ContractProposalOnlyCount:         contractStats.proposalOnly,
		ContractUnknownCount:              contractStats.unknown,
		StaleFileRefCount:                 staleRefCount(integrityChecks),
		StaleFileRefs:                     staleRefDetails(integrityChecks),
		PatternMisuseCount:                seedStats.patternMisuseCount,
		PatternMisuseIDs:                  seedStats.patternMisuseIDs,
	})
	return rep, target, nil
}

func bytesCountLines(b []byte) int {
	n := 0
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}

type repoEvalDraftUpgradeReport struct {
	RepoRoot   string                 `json:"repo_root"`
	DryRun     bool                   `json:"dry_run"`
	Verdict    string                 `json:"verdict"`
	DraftCount int                    `json:"draft_count"`
	Drafts     []repoEvalUpgradeDraft `json:"drafts"`
}

type repoEvalUpgradeDraft struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Target  string `json:"target_file"`
	Content string `json:"content,omitempty"`
}

type repoEvalUpgradeCandidateFile struct {
	Candidates []repoEvalUpgradeCandidateDoc `yaml:"candidates"`
}

type repoEvalUpgradeCandidateDoc struct {
	ID               string   `yaml:"id"`
	Class            string   `yaml:"class"`
	Status           string   `yaml:"status"`
	Confidence       string   `yaml:"confidence"`
	Title            string   `yaml:"title"`
	Rationale        string   `yaml:"rationale"`
	SuggestedTarget  string   `yaml:"suggested_target_file"`
	SourceFiles      []string `yaml:"source_files,omitempty"`
	Evidence         []string `yaml:"evidence,omitempty"`
	MissingFields    []string `yaml:"missing_fields,omitempty"`
	ReviewTodo       string   `yaml:"review_todo"`
	DoNotAutoPromote bool     `yaml:"do_not_auto_promote"`
}

func buildRepoEvalUpgradeDrafts(repoRoot string, rep repoeval.Report) ([]repoEvalUpgradeDraft, error) {
	var drafts []repoEvalUpgradeDraft
	for _, candidate := range rep.UpgradePath.Invariants {
		draft, err := renderRepoEvalUpgradeDraft(repoRoot, rep.AgentReadiness.Verdict, candidate)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, draft)
	}
	for _, candidate := range rep.UpgradePath.Contracts {
		draft, err := renderRepoEvalUpgradeDraft(repoRoot, rep.AgentReadiness.Verdict, candidate)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, draft)
	}
	sort.Slice(drafts, func(i, j int) bool { return drafts[i].Path < drafts[j].Path })
	return drafts, nil
}

func renderRepoEvalUpgradeDraft(repoRoot, verdict string, candidate repoeval.UpgradeCandidate) (repoEvalUpgradeDraft, error) {
	doc := repoEvalUpgradeCandidateDoc{
		ID:               candidate.ID,
		Class:            repoEvalDraftClass(candidate.Kind),
		Status:           "candidate",
		Confidence:       "structural",
		Title:            candidate.Title,
		Rationale:        candidate.Rationale,
		SuggestedTarget:  candidate.SuggestedFile,
		SourceFiles:      candidate.Paths,
		Evidence:         repoEvalDraftEvidence(verdict, candidate),
		MissingFields:    repoEvalDraftMissingFields(candidate.Kind),
		ReviewTodo:       repoEvalDraftTodo(candidate.Kind),
		DoNotAutoPromote: true,
	}
	body, err := yaml.Marshal(repoEvalUpgradeCandidateFile{Candidates: []repoEvalUpgradeCandidateDoc{doc}})
	if err != nil {
		return repoEvalUpgradeDraft{}, err
	}
	header := fmt.Sprintf("# DRAFT from `awg repo-eval draft-upgrade` for repo %q.\n# status:candidate — importer must skip this file until a human reviews and promotes it.\n", repoRoot)
	relPath := filepath.Join("docs", "awareness", "candidates", "repo_eval_upgrade", candidate.Kind+"_"+repoEvalSlug(candidate.ID)+".yaml")
	return repoEvalUpgradeDraft{
		Path:    relPath,
		Kind:    candidate.Kind,
		ID:      candidate.ID,
		Target:  candidate.SuggestedFile,
		Content: header + string(body),
	}, nil
}

func repoEvalDraftClass(kind string) string {
	switch kind {
	case "contract":
		return "Intent"
	default:
		return "Invariant"
	}
}

func repoEvalDraftEvidence(verdict string, candidate repoeval.UpgradeCandidate) []string {
	out := []string{
		"discovered_from: repo-eval upgrade_path",
		"repo_eval_verdict: " + verdict,
		"upgrade_rationale: " + candidate.Rationale,
	}
	for _, p := range candidate.Paths {
		out = append(out, "observed_path: "+p)
	}
	return out
}

func repoEvalDraftMissingFields(kind string) []string {
	switch kind {
	case "contract":
		return []string{
			"statement",
			"level",
			"required_tests",
			"expressed_by",
		}
	default:
		return []string{
			"statement",
			"protects.files",
			"required_tests",
			"forbidden_fixes",
		}
	}
}

func repoEvalDraftTodo(kind string) string {
	switch kind {
	case "contract":
		return "Replace the structural placeholder with an explicit behavior contract grounded in code evidence and named tests; then promote into docs/intent/."
	default:
		return "Replace the structural placeholder with a real invariant statement, explicit protected paths, and only observed required_tests; then promote into docs/awareness/."
	}
}
