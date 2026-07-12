// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/contractassess"
	"github.com/globulario/sensei/golang/coverage"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/repoeval"
	"gopkg.in/yaml.v3"
)

func runRepoEval(args []string) int {
	if len(args) > 0 && args[0] == "fix" {
		return runRepoEvalFix(args[1:])
	}
	if len(args) > 0 && args[0] == "draft-upgrade" {
		return runRepoEvalDraftUpgrade(args[1:])
	}

	fs := flag.NewFlagSet("sensei repo-eval", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "output as JSON")
	repoFlag := fs.String("repo", "", "target repository root to evaluate (defaults to current project root)")
	svcRepoFlag := fs.String("services-repo", "", "path to services repo (auto-detect)")
	agRepoFlag := fs.String("ag-repo", "", "path to awareness-graph repo (auto-detect)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei repo-eval [flags]
       sensei repo-eval fix [flags]

Evaluate a repository's architecture and awareness quality from explicit evidence.
The report is evidence-based: visible subscores, findings, and recommendations.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	targetRepo, _ := resolveProjectRoot(*repoFlag)
	svcRepo, _ := resolveServicesRepo(*svcRepoFlag)
	agRepo, _ := resolveAGRepo(*agRepoFlag, svcRepo)
	if svcRepo == "" && agRepo == "" && targetRepo == "" {
		fmt.Fprintln(os.Stderr, "sensei repo-eval: cannot find repos; use --services-repo / --ag-repo")
		return 1
	}
	target, err := resolveRepoEvalTarget(targetRepo, svcRepo, agRepo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei repo-eval: %v\n", err)
		return 1
	}
	inputDirs, intentDir, err := repoEvalInputDirs(target, svcRepo, agRepo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei repo-eval: %v\n", err)
		return 1
	}
	graphSvcRepo, graphAgRepo := repoEvalGraphRoots(target, svcRepo, agRepo)
	servicesOwnershipRepo := graphSvcRepo
	if target.kind == "generic" {
		servicesOwnershipRepo = ""
	}
	ntBytes, _, _, err := generateNTWithOwnership(inputDirs, intentDir, []string{graphAgRepo, graphSvcRepo}, servicesOwnershipRepo, nil, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei repo-eval: generate graph: %v\n", err)
		return 1
	}

	seedPath := ""
	if agRepo != "" && target.kind != "generic" {
		seedPath = filepath.Join(agRepo, "golang", "server", "embeddata", "awareness.nt")
	}
	var integrityChecks []auditResult
	if seedPath != "" {
		integrityChecks = append(integrityChecks, checkEmbeddataFreshness(ntBytes, seedPath, generateAgOnlyNT(agRepo)))
	}
	integrityChecks = append(integrityChecks,
		checkYAMLValidity(inputDirs, intentDir, graphSvcRepo, graphAgRepo, 0),
		checkNTValidity(ntBytes, bytes.Count(ntBytes, []byte("\n"))),
		checkStaleFileRefs(graphSvcRepo, graphAgRepo, ntBytes),
	)
	validateChecks, err := repoEvalValidateChecks(target, inputDirs, intentDir, graphSvcRepo, graphAgRepo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei repo-eval: validate: %v\n", err)
		return 1
	}
	integrityChecks = append(integrityChecks, validateChecks...)

	coverageRep, err := buildRepoCoverageReport(target.root, ntBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei repo-eval: coverage: %v\n", err)
		return 1
	}
	testStats, err := collectTestCoverageStats(target.root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei repo-eval: test coverage: %v\n", err)
		return 1
	}
	contractStats, err := collectContractAssessmentStats(target.intentDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei repo-eval: contract posture: %v\n", err)
		return 1
	}
	seedStats := collectSeedStats(ntBytes)
	upgradePath, err := collectRepoEvalUpgradePath(target.root, target.intentDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei repo-eval: upgrade path: %v\n", err)
		return 1
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

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
		return 0
	}

	fmt.Println("Repository evaluation")
	fmt.Println()
	fmt.Printf("Overall posture: %s (%d/100, confidence: %s)\n", rep.Posture, rep.OverallScore, rep.Confidence)
	fmt.Printf("Agent readiness: %s (%d/100, confidence: %s)\n", rep.AgentReadiness.Verdict, rep.AgentReadiness.Score, rep.AgentReadiness.Confidence)
	fmt.Printf("  %s\n", rep.AgentReadiness.Summary)
	if len(rep.Caveats) > 0 {
		fmt.Printf("\nBasis of this verdict — what it does NOT verify (confidence: %s):\n", rep.Confidence)
		for _, c := range rep.Caveats {
			fmt.Printf("  - %s\n", c)
		}
	}
	if len(rep.AgentReadiness.Blockers) > 0 {
		fmt.Println("  Blockers:")
		for _, b := range rep.AgentReadiness.Blockers {
			fmt.Printf("    - %s\n", b)
		}
	}
	if len(rep.AgentReadiness.AllowedModes) > 0 {
		fmt.Printf("  Allowed modes: %s\n", strings.Join(rep.AgentReadiness.AllowedModes, ", "))
	}
	if len(rep.AgentReadiness.Requirements) > 0 {
		fmt.Println("  Requirements:")
		for _, req := range rep.AgentReadiness.Requirements {
			fmt.Printf("    - %s\n", req)
		}
	}
	if len(rep.IntegrityFindings) > 0 {
		fmt.Println("  Integrity issues:")
		for _, issue := range rep.IntegrityFindings {
			fmt.Printf("    - [%s] %s: %s\n", issue.Severity, issue.Check, issue.Summary)
			for _, ev := range issue.Evidence {
				fmt.Printf("      evidence: %s\n", ev)
			}
		}
	}
	if len(rep.UpgradePath.Invariants) > 0 || len(rep.UpgradePath.Contracts) > 0 {
		fmt.Println("  Upgrade path:")
		if rep.UpgradePath.Summary != "" {
			fmt.Printf("    %s\n", rep.UpgradePath.Summary)
		}
		for _, candidate := range rep.UpgradePath.Invariants {
			fmt.Printf("    - [invariant] %s\n", candidate.Title)
			fmt.Printf("      rationale: %s\n", candidate.Rationale)
			if candidate.SuggestedFile != "" {
				fmt.Printf("      suggested file: %s\n", candidate.SuggestedFile)
			}
			for _, path := range candidate.Paths {
				fmt.Printf("      path: %s\n", path)
			}
		}
		for _, candidate := range rep.UpgradePath.Contracts {
			fmt.Printf("    - [contract] %s\n", candidate.Title)
			fmt.Printf("      rationale: %s\n", candidate.Rationale)
			if candidate.SuggestedFile != "" {
				fmt.Printf("      suggested file: %s\n", candidate.SuggestedFile)
			}
			for _, path := range candidate.Paths {
				fmt.Printf("      path: %s\n", path)
			}
		}
	}
	fmt.Println()
	fmt.Println("Dimensions:")
	for _, d := range rep.Dimensions {
		fmt.Printf("  - %s: %d/100 (%s) — %s\n", d.Label, d.Score, d.Status, d.Summary)
	}
	if len(rep.Findings) > 0 {
		fmt.Println()
		fmt.Println("Findings:")
		for _, f := range rep.Findings {
			fmt.Printf("  - [%s] %s — %s\n", f.Severity, f.Title, f.Summary)
			for _, ev := range f.Evidence {
				fmt.Printf("      evidence: %s\n", ev)
			}
			if f.Recommendation != "" {
				fmt.Printf("      recommendation: %s\n", f.Recommendation)
			}
		}
	}
	if len(rep.Recommendations) > 0 {
		fmt.Println()
		fmt.Println("Top recommendations:")
		for _, r := range rep.Recommendations {
			fmt.Printf("  - %s\n", r)
		}
	}
	return 0
}

type repoEvalTarget struct {
	root      string
	intentDir string
	kind      string
}

func resolveRepoEvalTarget(targetRepo, svcRepo, agRepo string) (repoEvalTarget, error) {
	targetRepo = filepath.Clean(targetRepo)
	svcRepo = filepath.Clean(svcRepo)
	agRepo = filepath.Clean(agRepo)
	switch {
	case targetRepo != "" && agRepo != "" && targetRepo == agRepo:
		return repoEvalTarget{
			root:      agRepo,
			intentDir: filepath.Join(agRepo, "docs", "intent"),
			kind:      "awareness-graph",
		}, nil
	case targetRepo != "" && svcRepo != "" && targetRepo == svcRepo:
		return repoEvalTarget{
			root:      svcRepo,
			intentDir: filepath.Join(svcRepo, "docs", "intent"),
			kind:      "services",
		}, nil
	}
	if targetRepo == "" && agRepo != "" {
		return repoEvalTarget{
			root:      agRepo,
			intentDir: filepath.Join(agRepo, "docs", "intent"),
			kind:      "awareness-graph",
		}, nil
	}
	if targetRepo == "" && svcRepo != "" {
		return repoEvalTarget{
			root:      svcRepo,
			intentDir: filepath.Join(svcRepo, "docs", "intent"),
			kind:      "services",
		}, nil
	}
	if targetRepo != "" {
		if repoHasGoSignals(targetRepo) || repoHasAwarenessCorpus(targetRepo) {
			return repoEvalTarget{
				root:      targetRepo,
				intentDir: filepath.Join(targetRepo, "docs", "intent"),
				kind:      "generic",
			}, nil
		}
		if _, err := os.Stat(filepath.Join(targetRepo, "go.mod")); err == nil {
			return repoEvalTarget{
				root:      targetRepo,
				intentDir: filepath.Join(targetRepo, "docs", "intent"),
				kind:      "generic",
			}, nil
		}
		if _, err := os.Stat(filepath.Join(targetRepo, "docs", "awareness")); err == nil {
			return repoEvalTarget{
				root:      targetRepo,
				intentDir: filepath.Join(targetRepo, "docs", "intent"),
				kind:      "generic",
			}, nil
		}
	}
	return repoEvalTarget{}, fmt.Errorf("cannot map target repo %q to a supported repository root", targetRepo)
}

func repoEvalInputDirs(target repoEvalTarget, svcRepo, agRepo string) (inputDirs []string, intentDir string, err error) {
	if target.kind != "generic" {
		return collectInputDirs(svcRepo, agRepo)
	}

	inputDirs = appendExistingDir(nil,
		filepath.Join(target.root, "docs", "awareness"),
		filepath.Join(target.root, "docs", "awareness", "generated"),
	)
	if len(inputDirs) == 0 {
		return nil, "", fmt.Errorf("target repo %q has no docs/awareness directory", target.root)
	}
	if _, statErr := os.Stat(target.intentDir); statErr == nil {
		intentDir = target.intentDir
	}
	return inputDirs, intentDir, nil
}

func repoEvalGraphRoots(target repoEvalTarget, svcRepo, agRepo string) (string, string) {
	if target.kind == "generic" {
		return target.root, ""
	}
	return svcRepo, agRepo
}

func collectRepoEvalInputs(target repoEvalTarget, svcRepo, agRepo string) (inputDirs []string, intentDir, graphSvcRepo, graphAGRepo string, err error) {
	if target.kind == "generic" {
		inputDirs = appendExistingDir(nil,
			filepath.Join(target.root, "docs", "awareness"),
			filepath.Join(target.root, "docs", "awareness", "generated"),
		)
		if len(inputDirs) == 0 {
			return nil, "", "", "", fmt.Errorf("target repo %q has no docs/awareness corpus", target.root)
		}
		intentDir = existingDir(filepath.Join(target.root, "docs", "intent"))
		return inputDirs, intentDir, target.root, "", nil
	}
	inputDirs, intentDir, err = collectInputDirs(svcRepo, agRepo)
	if err != nil {
		return nil, "", "", "", err
	}
	return inputDirs, intentDir, svcRepo, agRepo, nil
}

func repoHasGoSignals(root string) bool {
	for _, path := range []string{
		filepath.Join(root, "go.mod"),
		filepath.Join(root, "golang"),
	} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func repoHasAwarenessCorpus(root string) bool {
	_, err := os.Stat(filepath.Join(root, "docs", "awareness"))
	return err == nil
}

func existingDir(path string) string {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return ""
	}
	return path
}

type repoEvalTestStats struct {
	criticalHighCount int
	missingCount      int
}

func collectTestCoverageStats(repoRoot string) (repoEvalTestStats, error) {
	if repoRoot == "" {
		return repoEvalTestStats{}, fmt.Errorf("repository root required")
	}
	raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "awareness", "invariants.yaml"))
	if err != nil {
		return repoEvalTestStats{}, err
	}
	var doc struct {
		Invariants []struct {
			Severity                string   `yaml:"severity"`
			RequiredTests           []string `yaml:"required_tests"`
			TestNotApplicableReason string   `yaml:"test_not_applicable_reason"`
		} `yaml:"invariants"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return repoEvalTestStats{}, err
	}
	var out repoEvalTestStats
	for _, inv := range doc.Invariants {
		if inv.Severity != "critical" && inv.Severity != "high" {
			continue
		}
		out.criticalHighCount++
		if len(inv.RequiredTests) == 0 && strings.TrimSpace(inv.TestNotApplicableReason) == "" {
			out.missingCount++
		}
	}
	return out, nil
}

type repoEvalContractStats struct {
	found, synthesisSafe, proposalOnly, unknown int
}

func collectContractAssessmentStats(intentDir string) (repoEvalContractStats, error) {
	if intentDir == "" {
		return repoEvalContractStats{}, nil
	}
	if _, err := os.Stat(intentDir); err != nil {
		if os.IsNotExist(err) {
			return repoEvalContractStats{}, nil
		}
		return repoEvalContractStats{}, err
	}
	entries, err := os.ReadDir(intentDir)
	if err != nil {
		return repoEvalContractStats{}, err
	}
	var out repoEvalContractStats
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(intentDir, entry.Name()))
		if err != nil {
			return out, err
		}
		var doc auditIntentDoc
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return out, err
		}
		if strings.TrimSpace(doc.ID) == "" || strings.EqualFold(strings.TrimSpace(doc.Status), "deprecated") {
			continue
		}
		switch contractassess.Assess(assessmentInputForIntent(doc)).Outcome {
		case contractassess.ContractFound:
			out.found++
		case contractassess.ContractSynthesisSafe:
			out.synthesisSafe++
		case contractassess.ContractProposalOnly:
			out.proposalOnly++
		case contractassess.ContractUnknown:
			out.unknown++
		}
	}
	return out, nil
}

type repoEvalSeedStats struct {
	anchored           map[string]bool
	covers             []string
	patternMisuseCount int
	patternMisuseIDs   []string
}

func collectSeedStats(ntBytes []byte) repoEvalSeedStats {
	out := repoEvalSeedStats{anchored: map[string]bool{}}
	patternSet := map[string]bool{}
	patternLabels := map[string]string{}
	patternStatuses := map[string]string{}
	const sourceFilePrefix = "https://globular.io/awareness#sourceFile/"

	scanner := bufio.NewScanner(bytes.NewReader(ntBytes))
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "<") {
			continue
		}
		sEnd := strings.IndexByte(line, '>')
		if sEnd < 0 {
			continue
		}
		subj := line[1:sEnd]
		rest := strings.TrimSpace(line[sEnd+1:])
		switch {
		case strings.HasPrefix(rest, "<"+rdf.PropImplements+">") && strings.HasPrefix(subj, sourceFilePrefix):
			if p := decodeSourceFilePath(subj, sourceFilePrefix); p != "" {
				out.anchored[p] = true
			}
		case strings.HasPrefix(rest, "<"+rdf.PropCoversPath+">"):
			if v := firstLiteral(rest); v != "" {
				out.covers = append(out.covers, v)
			}
		case strings.HasPrefix(rest, "<"+rdf.PropType+">") && strings.Contains(rest, "<"+rdf.ClassPatternMisuse+">"):
			if id := bareIDFromSeedIRI(subj); id != "" {
				patternSet[id] = true
			}
		case strings.HasPrefix(rest, "<"+rdf.PropLabel+">"):
			if id := bareIDFromSeedIRI(subj); id != "" {
				if lbl := firstLiteral(rest); lbl != "" {
					patternLabels[id] = lbl
				}
			}
		case strings.HasPrefix(rest, "<"+rdf.PropStatus+">"):
			if id := bareIDFromSeedIRI(subj); id != "" {
				if status := firstLiteral(rest); status != "" {
					patternStatuses[id] = strings.ToLower(status)
				}
			}
		}
	}
	sort.Strings(out.covers)
	for id := range patternSet {
		if !isActivePatternMisuseStatus(patternStatuses[id]) {
			continue
		}
		if lbl := patternLabels[id]; lbl != "" {
			out.patternMisuseIDs = append(out.patternMisuseIDs, id+" — "+lbl)
		} else {
			out.patternMisuseIDs = append(out.patternMisuseIDs, id)
		}
	}
	sort.Strings(out.patternMisuseIDs)
	out.patternMisuseCount = len(out.patternMisuseIDs)
	return out
}

func isActivePatternMisuseStatus(status string) bool {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "", "active", "live", "detected":
		return true
	default:
		return false
	}
}

func buildRepoCoverageReport(repoRoot string, ntBytes []byte) (*coverage.Report, error) {
	if repoRoot == "" {
		return nil, fmt.Errorf("repository root required")
	}
	stats := collectSeedStats(ntBytes)
	files, err := walkRepoGoFiles(repoRoot)
	if err != nil {
		if isAwarenessOnlyRepo(repoRoot) {
			return &coverage.Report{WeightedOverallPercent: 100}, nil
		}
		return nil, err
	}
	inv := make([]coverage.FileCoverage, 0, len(files))
	for _, f := range files {
		inv = append(inv, coverage.FileCoverage{Path: f, HasDirectAnchor: stats.anchored[f]})
	}
	return coverage.BuildReport(inv, stats.covers), nil
}

func isAwarenessOnlyRepo(repoRoot string) bool {
	if repoRoot == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "docs", "awareness")); err != nil {
		return false
	}
	if _, err := repoEvalGoSourceRoot(repoRoot); err == nil {
		return false
	}
	return true
}

func walkRepoGoFiles(repoRoot string) ([]string, error) {
	root, err := repoEvalGoSourceRoot(repoRoot)
	if err != nil {
		return nil, err
	}
	var out []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", "dist", "bin":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func repoEvalGoSourceRoot(repoRoot string) (string, error) {
	if repoRoot == "" {
		return "", fmt.Errorf("repository root required")
	}
	golangRoot := filepath.Join(repoRoot, "golang")
	if info, err := os.Stat(golangRoot); err == nil && info.IsDir() {
		return golangRoot, nil
	}
	goMod := filepath.Join(repoRoot, "go.mod")
	if info, err := os.Stat(goMod); err == nil && !info.IsDir() {
		return repoRoot, nil
	}
	return "", fmt.Errorf("cannot find Go source root under %q", repoRoot)
}

func decodeSourceFilePath(subj, prefix string) string {
	enc := strings.TrimPrefix(subj, prefix)
	dec, err := url.PathUnescape(enc)
	if err != nil {
		return enc
	}
	return dec
}

func firstLiteral(rest string) string {
	i := strings.IndexByte(rest, '"')
	if i < 0 {
		return ""
	}
	j := strings.LastIndexByte(rest, '"')
	if j <= i {
		return ""
	}
	r := strings.NewReplacer(`\\`, `\`, `\"`, `"`, `\n`, "\n", `\t`, "\t", `\r`, "\r")
	return r.Replace(rest[i+1 : j])
}

func bareIDFromSeedIRI(iri string) string {
	slash := strings.LastIndexByte(iri, '/')
	if slash < 0 || slash == len(iri)-1 {
		return ""
	}
	return iri[slash+1:]
}

func countAuditLevel(checks []auditResult, level auditLevel) int {
	n := 0
	for _, c := range checks {
		if c.level == level {
			n++
		}
	}
	return n
}

func repoEvalIntegrityIssues(checks []auditResult) []repoeval.IntegrityIssue {
	var issues []repoeval.IntegrityIssue
	for _, check := range checks {
		if check.level != auditFAIL && check.level != auditWARN {
			continue
		}
		issues = append(issues, repoeval.IntegrityIssue{
			Check:    check.name,
			Severity: strings.ToLower(check.level.String()),
			Summary:  check.summary,
			Evidence: capAuditDetails(check.details, 5),
		})
	}
	return issues
}

func capAuditDetails(in []string, n int) []string {
	if len(in) <= n {
		return in
	}
	return in[:n]
}

func repoEvalValidateChecks(target repoEvalTarget, inputDirs []string, intentDir, svcRepo, agRepo string) ([]auditResult, error) {
	// Eval benchmark fixtures (multi-swe-bench contracts, learning events) are
	// included in inputDirs for graph building but are test scaffolding, not
	// production governance corpus. Exclude them from the duplicate/integrity
	// validation scan to avoid false-positive duplicate_id across test scenarios.
	var dirs []string
	for _, d := range inputDirs {
		if !strings.Contains(filepath.ToSlash(d), "/eval/") {
			dirs = append(dirs, d)
		}
	}
	// intentDir is typically from a different repo (e.g. svcRepo/docs/intent).
	// When the target repo's own intent dir is already in inputDirs (we added it
	// in collectInputDirs), putting intentDir in dirs would duplicate-check
	// cross-repo mirrors of the same files. Use extraDefDirs instead so the
	// foreign intent dir resolves references without being duplicate-checked.
	var extraDefDirs []string
	if intentDir != "" {
		targetIntent := filepath.Join(target.root, "docs", "intent")
		ownIntentInDirs := false
		for _, d := range dirs {
			if filepath.Clean(d) == filepath.Clean(targetIntent) {
				ownIntentInDirs = true
				break
			}
		}
		if ownIntentInDirs {
			extraDefDirs = append(extraDefDirs, intentDir)
		} else {
			dirs = append(dirs, intentDir)
		}
	}
	sourceRoots := []string{target.root}
	for _, r := range []string{svcRepo, agRepo} {
		if r != "" && r != target.root {
			sourceRoots = append(sourceRoots, r)
		}
	}
	// http_contracts.yaml references Globular gateway paths; include it when present.
	if g := siblingRepo(target.root, "Globular"); g != "" {
		sourceRoots = append(sourceRoots, g)
	}
	report, err := doValidate(target.root, dirs, extraDefDirs, sourceRoots, validateScopeLocal)
	if err != nil {
		return nil, err
	}
	type groupedFinding struct {
		severity string
		details  []string
	}
	grouped := map[string]*groupedFinding{}
	for _, finding := range report.Findings {
		if finding.Severity != "error" && finding.Severity != "warn" {
			continue
		}
		group := grouped[finding.Check]
		if group == nil {
			group = &groupedFinding{severity: finding.Severity}
			grouped[finding.Check] = group
		}
		if group.severity != "error" && finding.Severity == "error" {
			group.severity = "error"
		}
		group.details = append(group.details, formatValidateFinding(finding))
	}
	var checks []auditResult
	for check, group := range grouped {
		level := auditWARN
		if group.severity == "error" {
			level = auditFAIL
		}
		checks = append(checks, auditResult{
			name:    check,
			level:   level,
			summary: fmt.Sprintf("%d validation finding(s) in %s", len(group.details), check),
			details: capAuditDetails(group.details, 5),
		})
	}
	sort.Slice(checks, func(i, j int) bool {
		return checks[i].name < checks[j].name
	})
	return checks, nil
}

func formatValidateFinding(f validateFinding) string {
	parts := []string{f.File}
	if f.EntityID != "" {
		parts = append(parts, f.EntityID)
	}
	if f.Ref != "" {
		parts = append(parts, f.Ref)
	}
	parts = append(parts, f.Message)
	return strings.Join(parts, " — ")
}

func staleRefCount(checks []auditResult) int {
	for _, c := range checks {
		if c.name == "stale-file-refs" {
			return len(c.details)
		}
	}
	return 0
}

func staleRefDetails(checks []auditResult) []string {
	for _, c := range checks {
		if c.name == "stale-file-refs" {
			return c.details
		}
	}
	return nil
}

func surfacePercent(rep *coverage.Report, surface string) int {
	if rep == nil {
		return 0
	}
	for _, s := range rep.Surfaces {
		if s.Surface == surface {
			return s.Percent
		}
	}
	return 0
}

func surfaceTotal(rep *coverage.Report, surface string) int {
	if rep == nil {
		return 0
	}
	for _, s := range rep.Surfaces {
		if s.Surface == surface {
			return s.TotalFiles
		}
	}
	return 0
}

type repoEvalComponentDoc struct {
	Components []struct {
		ID          string   `yaml:"id"`
		Name        string   `yaml:"name"`
		Kind        string   `yaml:"kind"`
		SourceFiles []string `yaml:"source_files"`
		DependsOn   []string `yaml:"depends_on"`
	} `yaml:"components"`
}

func collectRepoEvalUpgradePath(repoRoot, intentDir string) (repoeval.UpgradePath, error) {
	components, err := readRepoEvalComponents(filepath.Join(repoRoot, "docs", "awareness", "generated", "components.yaml"))
	if err != nil {
		return repoeval.UpgradePath{}, err
	}
	highRisk, err := readRepoEvalHighRiskPrefixes(filepath.Join(repoRoot, "docs", "awareness", "high_risk_files.yaml"))
	if err != nil {
		return repoeval.UpgradePath{}, err
	}
	invariants, err := readRepoEvalInvariantSurfaces(filepath.Join(repoRoot, "docs", "awareness", "invariants.yaml"))
	if err != nil {
		return repoeval.UpgradePath{}, err
	}
	return repoeval.UpgradePath{
		Invariants: collectInvariantUpgradeCandidates(components, highRisk, invariants),
		Contracts:  collectContractUpgradeCandidates(components, invariants, intentDir),
	}, nil
}

func readRepoEvalComponents(path string) ([]repoEvalComponent, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var doc repoEvalComponentDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	out := make([]repoEvalComponent, 0, len(doc.Components))
	for _, c := range doc.Components {
		if strings.TrimSpace(c.ID) == "" {
			continue
		}
		out = append(out, repoEvalComponent{
			ID:          strings.TrimSpace(c.ID),
			Name:        strings.TrimSpace(c.Name),
			Kind:        strings.TrimSpace(c.Kind),
			SourceFiles: dedupeSortedStrings(c.SourceFiles),
			DependsOn:   dedupeSortedStrings(c.DependsOn),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if repoEvalComponentPriority(out[i]) != repoEvalComponentPriority(out[j]) {
			return repoEvalComponentPriority(out[i]) < repoEvalComponentPriority(out[j])
		}
		if len(out[i].SourceFiles) != len(out[j].SourceFiles) {
			return len(out[i].SourceFiles) > len(out[j].SourceFiles)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

type repoEvalComponent struct {
	ID          string
	Name        string
	Kind        string
	SourceFiles []string
	DependsOn   []string
}

type repoEvalInvariantSurface struct {
	ID       string
	Severity string
	Paths    []string
}

func repoEvalComponentPriority(c repoEvalComponent) int {
	if c.Kind == "service" {
		return 0
	}
	return 1
}

func readRepoEvalHighRiskPrefixes(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var doc struct {
		Files []string `yaml:"files"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	return dedupeSortedStrings(doc.Files), nil
}

func readRepoEvalInvariantSurfaces(path string) ([]repoEvalInvariantSurface, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var doc struct {
		Invariants []struct {
			ID       string `yaml:"id"`
			Severity string `yaml:"severity"`
			Protects struct {
				Files []string `yaml:"files"`
			} `yaml:"protects"`
		} `yaml:"invariants"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	var out []repoEvalInvariantSurface
	for _, inv := range doc.Invariants {
		if strings.TrimSpace(inv.ID) == "" {
			continue
		}
		paths := dedupeSortedStrings(inv.Protects.Files)
		if len(paths) == 0 {
			continue
		}
		out = append(out, repoEvalInvariantSurface{
			ID:       strings.TrimSpace(inv.ID),
			Severity: strings.TrimSpace(inv.Severity),
			Paths:    paths,
		})
	}
	return out, nil
}

func collectInvariantUpgradeCandidates(components []repoEvalComponent, highRisk []string, invariants []repoEvalInvariantSurface) []repoeval.UpgradeCandidate {
	if len(components) == 0 {
		return collectInvariantUpgradeCandidatesFromInvariantSurfaces(invariants)
	}
	var candidates []repoeval.UpgradeCandidate
	for _, prefix := range highRisk {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" || strings.HasPrefix(prefix, "#") {
			continue
		}
		if repoEvalPathGovernedByInvariant(prefix, invariants) {
			continue
		}
		candidates = append(candidates, repoeval.UpgradeCandidate{
			ID:            "invariant." + repoEvalSlug(prefix),
			Kind:          "invariant",
			Title:         "Govern edits under " + prefix,
			Rationale:     "This path is explicitly marked high-risk and should get a named invariant before broader agent changes are allowed.",
			SuggestedFile: "docs/awareness/invariants.yaml",
			Paths:         []string{prefix},
		})
	}
	if len(candidates) > 0 {
		return dedupeUpgradeCandidates(candidates)
	}
	for _, component := range components {
		if len(component.SourceFiles) == 0 {
			continue
		}
		if repoEvalComponentGovernedByInvariant(component, invariants) {
			continue
		}
		candidates = append(candidates, repoeval.UpgradeCandidate{
			ID:            "invariant." + strings.TrimPrefix(component.ID, "component."),
			Kind:          "invariant",
			Title:         "Protect " + component.ID,
			Rationale:     repoEvalInvariantRationale(component),
			SuggestedFile: "docs/awareness/invariants.yaml",
			Paths:         capAuditDetails(component.SourceFiles, 3),
		})
	}
	return dedupeUpgradeCandidates(candidates)
}

func repoEvalPathGovernedByInvariant(path string, invariants []repoEvalInvariantSurface) bool {
	path = repoEvalNormalizeSurfacePath(path)
	if path == "" {
		return false
	}
	for _, inv := range invariants {
		if repoEvalInvariantSeverityPriority(inv.Severity) < repoEvalInvariantSeverityPriority("high") {
			continue
		}
		for _, protected := range inv.Paths {
			protected = repoEvalNormalizeSurfacePath(protected)
			if protected == "" {
				continue
			}
			if path == protected || strings.HasPrefix(path, protected) || strings.HasPrefix(protected, path) {
				return true
			}
		}
	}
	return false
}

func repoEvalComponentGovernedByInvariant(component repoEvalComponent, invariants []repoEvalInvariantSurface) bool {
	if len(component.SourceFiles) == 0 {
		return false
	}
	for _, source := range component.SourceFiles {
		if !repoEvalPathGovernedByInvariant(source, invariants) {
			return false
		}
	}
	return true
}

func repoEvalNormalizeSurfacePath(path string) string {
	path = strings.TrimSpace(filepath.ToSlash(path))
	if path == "" {
		return ""
	}
	if strings.HasSuffix(path, "/") {
		return path
	}
	if strings.HasSuffix(path, ".go") {
		return path
	}
	return path + "/"
}

func collectContractUpgradeCandidates(components []repoEvalComponent, invariants []repoEvalInvariantSurface, intentDir string) []repoeval.UpgradeCandidate {
	existing := map[string]bool{}
	if intentDir != "" {
		if entries, err := os.ReadDir(intentDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := strings.TrimSuffix(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())), ".yaml")
				existing[name] = true
			}
		}
	}
	if len(components) == 0 {
		return collectContractUpgradeCandidatesFromInvariantSurfaces(invariants, existing)
	}
	var candidates []repoeval.UpgradeCandidate
	for _, component := range components {
		intentID := "component." + strings.TrimPrefix(component.ID, "component.")
		intentFile := filepath.Join("docs", "intent", intentID+".yaml")
		if existing[intentID] {
			continue
		}
		candidates = append(candidates, repoeval.UpgradeCandidate{
			ID:            intentID,
			Kind:          "contract",
			Title:         "Contract for " + component.ID,
			Rationale:     repoEvalContractRationale(component),
			SuggestedFile: intentFile,
			Paths:         capAuditDetails(component.SourceFiles, 3),
		})
	}
	return dedupeUpgradeCandidates(candidates)
}

func collectInvariantUpgradeCandidatesFromInvariantSurfaces(invariants []repoEvalInvariantSurface) []repoeval.UpgradeCandidate {
	surfaces := selectInvariantFallbackSurfaces(invariants)
	var candidates []repoeval.UpgradeCandidate
	for _, surface := range surfaces {
		candidates = append(candidates, repoeval.UpgradeCandidate{
			ID:            "invariant." + repoEvalSlug(surface.Label),
			Kind:          "invariant",
			Title:         "Consolidate governing invariants for " + surface.Label,
			Rationale:     "This surface already carries real invariants; consolidating the governing rule set here makes the repair boundary clearer before expanding agent scope.",
			SuggestedFile: "docs/awareness/invariants.yaml",
			Paths:         surface.Paths,
		})
	}
	return dedupeUpgradeCandidates(candidates)
}

func collectContractUpgradeCandidatesFromInvariantSurfaces(invariants []repoEvalInvariantSurface, existing map[string]bool) []repoeval.UpgradeCandidate {
	surfaces := selectInvariantFallbackSurfaces(invariants)
	var candidates []repoeval.UpgradeCandidate
	for _, surface := range surfaces {
		intentID := "component." + repoEvalSlug(surface.Label)
		if existing[intentID] {
			continue
		}
		candidates = append(candidates, repoeval.UpgradeCandidate{
			ID:            intentID,
			Kind:          "contract",
			Title:         "Contract for " + surface.Label,
			Rationale:     "This surface already has explicit invariant coverage but still lacks a named behavioral contract, so AWG cannot yet judge broader repairs against an owned boundary.",
			SuggestedFile: filepath.Join("docs", "intent", intentID+".yaml"),
			Paths:         surface.Paths,
		})
	}
	return dedupeUpgradeCandidates(candidates)
}

type repoEvalFallbackSurface struct {
	Label    string
	Paths    []string
	Priority int
}

func selectInvariantFallbackSurfaces(invariants []repoEvalInvariantSurface) []repoEvalFallbackSurface {
	surfacesByLabel := map[string]*repoEvalFallbackSurface{}
	for _, inv := range invariants {
		priority := repoEvalInvariantSeverityPriority(inv.Severity)
		for _, path := range inv.Paths {
			label := repoEvalSurfaceLabel(path)
			if label == "" {
				continue
			}
			surface := surfacesByLabel[label]
			if surface == nil {
				surface = &repoEvalFallbackSurface{Label: label}
				surfacesByLabel[label] = surface
			}
			surface.Paths = append(surface.Paths, path)
			surface.Priority += priority
		}
	}
	var surfaces []repoEvalFallbackSurface
	for _, surface := range surfacesByLabel {
		surface.Paths = capAuditDetails(dedupeSortedStrings(surface.Paths), 3)
		surfaces = append(surfaces, *surface)
	}
	sort.Slice(surfaces, func(i, j int) bool {
		if surfaces[i].Priority != surfaces[j].Priority {
			return surfaces[i].Priority > surfaces[j].Priority
		}
		if len(surfaces[i].Paths) != len(surfaces[j].Paths) {
			return len(surfaces[i].Paths) > len(surfaces[j].Paths)
		}
		return surfaces[i].Label < surfaces[j].Label
	})
	return capRepoEvalFallbackSurfaces(surfaces, 3)
}

func repoEvalInvariantSeverityPriority(severity string) int {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return 3
	case "high":
		return 2
	default:
		return 1
	}
}

func repoEvalSurfaceLabel(path string) string {
	path = strings.Trim(strings.TrimSpace(filepath.ToSlash(path)), "/")
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	if len(parts) == 1 {
		return path
	}
	if parts[0] == "cmd" && len(parts) >= 2 {
		return "cmd/" + parts[1]
	}
	if strings.HasSuffix(path, ".go") {
		return strings.TrimSuffix(path, ".go")
	}
	return parts[0]
}

func capRepoEvalFallbackSurfaces(in []repoEvalFallbackSurface, n int) []repoEvalFallbackSurface {
	if len(in) <= n {
		return in
	}
	return in[:n]
}

func repoEvalInvariantRationale(component repoEvalComponent) string {
	if component.Kind == "service" {
		return "This is the entrypoint/service surface, so a governing invariant here gives AWG a first fail-closed rule on the repo's externally visible behavior."
	}
	if len(component.DependsOn) > 0 {
		return fmt.Sprintf("This component already coordinates %d dependency edge(s); invariant coverage here raises confidence on a load-bearing internal surface.", len(component.DependsOn))
	}
	return "This component is one of the largest generated code surfaces and is a good first place to turn structural awareness into an explicit behavior rule."
}

func repoEvalContractRationale(component repoEvalComponent) string {
	if component.Kind == "service" {
		return "This service/entrypoint needs an explicit contract so AWG can distinguish legitimate repairs from edits that silently change the runtime surface."
	}
	if len(component.DependsOn) > 0 {
		return fmt.Sprintf("This component depends on %d other component(s); an explicit contract here will make its behavioral boundaries and required tests governable.", len(component.DependsOn))
	}
	return "This component is large enough that structural awareness alone is not sufficient; an explicit contract will give AWG a real governing boundary."
}

func dedupeSortedStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func dedupeUpgradeCandidates(in []repoeval.UpgradeCandidate) []repoeval.UpgradeCandidate {
	seen := map[string]bool{}
	var out []repoeval.UpgradeCandidate
	for _, candidate := range in {
		key := candidate.Kind + "\x00" + candidate.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, candidate)
	}
	return capUpgradePathCandidates(out, 3)
}

func capUpgradePathCandidates(in []repoeval.UpgradeCandidate, n int) []repoeval.UpgradeCandidate {
	if len(in) <= n {
		return in
	}
	return in[:n]
}

func repoEvalSlug(s string) string {
	s = strings.Trim(strings.ToLower(filepath.ToSlash(s)), "/")
	var b strings.Builder
	lastUnderscore := false
	for _, r := range s {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}
