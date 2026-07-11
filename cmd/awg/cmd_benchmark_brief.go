// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"gopkg.in/yaml.v3"
)

type benchmarkBriefTask struct {
	InstanceID string   `json:"instance_id"`
	Issue      string   `json:"issue"`
	F2PTests   []string `json:"f2p_tests"`
	Files      []string `json:"files"`
}

type benchmarkBriefFile struct {
	File           string   `json:"file"`
	Invariants     []string `json:"invariants,omitempty"`
	FailureModes   []string `json:"failure_modes,omitempty"`
	Intents        []string `json:"intents,omitempty"`
	RequiredTests  []string `json:"required_tests,omitempty"`
	ForbiddenFixes []string `json:"forbidden_fixes,omitempty"`
}

type benchmarkBriefResult struct {
	RepoRoot                  string               `json:"repo_root"`
	IssueSource               string               `json:"issue_source,omitempty"`
	Issue                     string               `json:"issue,omitempty"`
	ChangedFiles              []string             `json:"changed_files,omitempty"`
	LikelyImplementationFiles []string             `json:"likely_implementation_files,omitempty"`
	LikelyProvingTests        []string             `json:"likely_proving_tests,omitempty"`
	CandidateFiles            []string             `json:"candidate_files,omitempty"`
	MechanicalEvidence        []bootstrapEvidence  `json:"mechanical_evidence,omitempty"`
	AWGFiles                  []benchmarkBriefFile `json:"awg_files,omitempty"`
	TestsToRun                []string             `json:"tests_to_run,omitempty"`
	ForbiddenFixes            []string             `json:"forbidden_fixes,omitempty"`
	AuthorityGaps             []string             `json:"authority_gaps,omitempty"`
	EvidenceGaps              []string             `json:"evidence_gaps,omitempty"`
	RepairPlan                *repairPlanResult    `json:"repair_plan,omitempty"`
}

type benchmarkInvariantDoc struct {
	Invariants []struct {
		ID             string   `yaml:"id"`
		RequiredTests  []string `yaml:"required_tests"`
		ForbiddenFixes []string `yaml:"forbidden_fixes"`
		Protects       struct {
			Files         []string `yaml:"files"`
			EnforcesFiles []string `yaml:"enforces_files"`
		} `yaml:"protects"`
	} `yaml:"invariants"`
}

type benchmarkFailureModeDoc struct {
	FailureModes []struct {
		ID             string   `yaml:"id"`
		RequiredTests  []string `yaml:"required_tests"`
		ForbiddenFixes []string `yaml:"forbidden_fixes"`
		Protects       struct {
			Files []string `yaml:"files"`
		} `yaml:"protects"`
	} `yaml:"failure_modes"`
}

type benchmarkRequiredTestsDoc struct {
	RequiredTests []struct {
		ID       string `yaml:"id"`
		Protects struct {
			Files []string `yaml:"files"`
		} `yaml:"protects"`
	} `yaml:"required_tests"`
}

type benchmarkForbiddenFixesDoc struct {
	ForbiddenFixes []struct {
		ID       string `yaml:"id"`
		Protects struct {
			Files []string `yaml:"files"`
		} `yaml:"protects"`
	} `yaml:"forbidden_fixes"`
}

func runBenchmarkBrief(args []string) int {
	fs := flag.NewFlagSet("sensei benchmark-brief", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoRoot := fs.String("repo-root", ".", "repository root to analyze")
	addr := fs.String("addr", "localhost:10120", "AWG gRPC server address for authoritative repair-plan resolution")
	svcRepoFlag := fs.String("services-repo", "", "path to services repo for cross-repo atomicity (auto-detect)")
	agRepoFlag := fs.String("ag-repo", "", "path to awareness-graph repo for cross-repo atomicity (auto-detect)")
	taskFile := fs.String("task-file", "", "task JSON containing issue/f2p_tests/files")
	issue := fs.String("issue", "", "issue text when not using --task-file")
	format := fs.String("format", "text", "output format: text | json")
	asJSON := fs.Bool("json", false, "output as JSON (deprecated: same as --format json)")
	var tests stringSlice
	var files stringSlice
	fs.Var(&tests, "f2p-test", "fail-to-pass test name (repeatable)")
	fs.Var(&files, "file", "changed or suspect file path (repeatable)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei benchmark-brief [flags]

Build a governed repair envelope for benchmark or PR fixing from issue text,
fail-to-pass tests, changed files, authored awareness metadata, and an
authoritative repair plan from the current AWG server.

This command fails closed if the server cannot prove current graph authority.

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

	task, source, err := loadBenchmarkBriefTask(*taskFile, *issue, tests, files)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei benchmark-brief: %v\n", err)
		return 2
	}
	root, err := filepath.Abs(*repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei benchmark-brief: resolve repo root: %v\n", err)
		return 1
	}
	svcRepo, _ := resolveServicesRepo(*svcRepoFlag)
	agRepo, _ := resolveAGRepo(*agRepoFlag, svcRepo)
	if err := benchmarkAtomicGuard(agRepo, svcRepo); err != nil {
		fmt.Fprintf(os.Stderr, "sensei benchmark-brief: %v\n", err)
		return 1
	}
	res, err := buildBenchmarkBrief(root, task, source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei benchmark-brief: %v\n", err)
		return 1
	}
	repairPlan, err := buildAuthoritativeRepairPlan(root, *addr, strings.TrimSpace(task.Issue), res.LikelyImplementationFiles)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei benchmark-brief: %v\n", err)
		return 1
	}
	res.RepairPlan = &repairPlan

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
	default:
		fmt.Print(renderBenchmarkBriefText(res))
	}
	return 0
}

func loadBenchmarkBriefTask(taskFile, issue string, tests, files []string) (benchmarkBriefTask, string, error) {
	if strings.TrimSpace(taskFile) != "" {
		data, err := os.ReadFile(taskFile)
		if err != nil {
			return benchmarkBriefTask{}, "", err
		}
		var task benchmarkBriefTask
		if err := json.Unmarshal(data, &task); err != nil {
			return benchmarkBriefTask{}, "", fmt.Errorf("parse task file: %w", err)
		}
		return task, taskFile, nil
	}
	if strings.TrimSpace(issue) == "" && len(tests) == 0 && len(files) == 0 {
		return benchmarkBriefTask{}, "", fmt.Errorf("provide --task-file or at least one of --issue, --f2p-test, --file")
	}
	return benchmarkBriefTask{
		Issue:    issue,
		F2PTests: tests,
		Files:    files,
	}, "flags", nil
}

func buildBenchmarkBrief(repoRoot string, task benchmarkBriefTask, source string) (benchmarkBriefResult, error) {
	testFiles, evidence, err := collectTestAnchors(repoRoot, task.F2PTests)
	if err != nil {
		return benchmarkBriefResult{}, err
	}
	testFiles, evidence = filterBenchmarkAnchorsByChangedFiles(testFiles, evidence, task.Files)
	implScores, implFiles, err := implementationCandidatesFromTests(repoRoot, testFiles)
	if err != nil {
		return benchmarkBriefResult{}, err
	}
	for _, f := range task.Files {
		f = filepath.ToSlash(strings.TrimSpace(f))
		if f == "" {
			continue
		}
		implScores[f] += 4
	}
	candidates, err := candidateFiles(repoRoot, task.Issue, implScores)
	if err != nil {
		return benchmarkBriefResult{}, err
	}
	candidates = dedupeStrings(append(dedupeStrings(task.Files), candidates...))
	likelyImpl := rankImplementationFiles(candidates, dedupeStrings(append(implFiles, task.Files...)))
	if len(likelyImpl) == 0 {
		likelyImpl = dedupeStrings(task.Files)
	}

	scopedFiles := dedupeStrings(append(dedupeStrings(task.Files), likelyImpl...))
	awgFiles, testsToRun, forbiddenFixes, invariantIDs, intentIDs, err := collectLocalBenchmarkFileContext(repoRoot, scopedFiles)
	if err != nil {
		return benchmarkBriefResult{}, err
	}
	authorityGaps, err := collectBenchmarkAuthorityGaps(repoRoot, intentIDs)
	if err != nil {
		return benchmarkBriefResult{}, err
	}
	evidenceGaps, err := collectBenchmarkEvidenceGaps(repoRoot, scopedFiles, invariantIDs)
	if err != nil {
		return benchmarkBriefResult{}, err
	}

	return benchmarkBriefResult{
		RepoRoot:                  repoRoot,
		IssueSource:               source,
		Issue:                     strings.TrimSpace(task.Issue),
		ChangedFiles:              dedupeStrings(task.Files),
		LikelyImplementationFiles: dedupeStrings(likelyImpl),
		LikelyProvingTests:        dedupeStrings(testFiles),
		CandidateFiles:            dedupeStrings(candidates),
		MechanicalEvidence:        evidence,
		AWGFiles:                  awgFiles,
		TestsToRun:                dedupeStrings(append(dedupeStrings(testFiles), testsToRun...)),
		ForbiddenFixes:            forbiddenFixes,
		AuthorityGaps:             authorityGaps,
		EvidenceGaps:              evidenceGaps,
	}, nil
}

func collectLocalBenchmarkFileContext(repoRoot string, files []string) ([]benchmarkBriefFile, []string, []string, []string, []string, error) {
	type agg struct {
		invariants     []string
		failureModes   []string
		intents        []string
		requiredTests  []string
		forbiddenFixes []string
	}
	byFile := map[string]*agg{}
	for _, file := range files {
		file = filepath.ToSlash(strings.TrimSpace(file))
		if file == "" {
			continue
		}
		byFile[file] = &agg{}
	}
	add := func(file string, fn func(*agg)) {
		for target, entry := range byFile {
			if pathMatchesBenchmarkAnchor(target, file) {
				fn(entry)
			}
		}
	}

	err := filepath.WalkDir(filepath.Join(repoRoot, "docs"), func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var top map[string]any
		if err := yaml.Unmarshal(raw, &top); err != nil {
			return nil
		}
		switch {
		case top["invariants"] != nil:
			var doc benchmarkInvariantDoc
			if err := yaml.Unmarshal(raw, &doc); err != nil {
				return nil
			}
			for _, inv := range doc.Invariants {
				for _, file := range dedupeStrings(append(inv.Protects.Files, inv.Protects.EnforcesFiles...)) {
					id := strings.TrimSpace(inv.ID)
					tests := nonEmptyStrings(inv.RequiredTests)
					fixes := nonEmptyStrings(inv.ForbiddenFixes)
					add(file, func(a *agg) {
						a.invariants = append(a.invariants, id)
						a.requiredTests = append(a.requiredTests, tests...)
						a.forbiddenFixes = append(a.forbiddenFixes, fixes...)
					})
				}
			}
		case top["failure_modes"] != nil:
			var doc benchmarkFailureModeDoc
			if err := yaml.Unmarshal(raw, &doc); err != nil {
				return nil
			}
			for _, fm := range doc.FailureModes {
				for _, file := range nonEmptyStrings(fm.Protects.Files) {
					id := strings.TrimSpace(fm.ID)
					tests := nonEmptyStrings(fm.RequiredTests)
					fixes := nonEmptyStrings(fm.ForbiddenFixes)
					add(file, func(a *agg) {
						a.failureModes = append(a.failureModes, id)
						a.requiredTests = append(a.requiredTests, tests...)
						a.forbiddenFixes = append(a.forbiddenFixes, fixes...)
					})
				}
			}
		case top["required_tests"] != nil:
			var doc benchmarkRequiredTestsDoc
			if err := yaml.Unmarshal(raw, &doc); err != nil {
				return nil
			}
			for _, rt := range doc.RequiredTests {
				for _, file := range nonEmptyStrings(rt.Protects.Files) {
					id := strings.TrimSpace(rt.ID)
					add(file, func(a *agg) {
						a.requiredTests = append(a.requiredTests, id)
					})
				}
			}
		case top["forbidden_fixes"] != nil:
			var doc benchmarkForbiddenFixesDoc
			if err := yaml.Unmarshal(raw, &doc); err != nil {
				return nil
			}
			for _, ff := range doc.ForbiddenFixes {
				for _, file := range nonEmptyStrings(ff.Protects.Files) {
					id := strings.TrimSpace(ff.ID)
					add(file, func(a *agg) {
						a.forbiddenFixes = append(a.forbiddenFixes, id)
					})
				}
			}
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, nil, nil, nil, err
	}

	intentDir := filepath.Join(repoRoot, "docs", "intent")
	if entries, err := os.ReadDir(intentDir); err == nil {
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
				return nil, nil, nil, nil, nil, err
			}
			var doc auditIntentDoc
			if err := yaml.Unmarshal(raw, &doc); err != nil {
				continue
			}
			if strings.TrimSpace(doc.ID) == "" || strings.EqualFold(strings.TrimSpace(doc.Status), "deprecated") {
				continue
			}
			for _, file := range nonEmptyStrings(doc.ExpressedBy) {
				id := strings.TrimSpace(doc.ID)
				tests := nonEmptyStrings(doc.RequiredTests)
				add(file, func(a *agg) {
					a.intents = append(a.intents, id)
					a.requiredTests = append(a.requiredTests, tests...)
				})
			}
		}
	}

	out := make([]benchmarkBriefFile, 0, len(byFile))
	var testsToRun, forbiddenFixes, invariantIDs, intentIDs []string
	for _, file := range dedupeStrings(files) {
		entry := byFile[file]
		if entry == nil {
			continue
		}
		item := benchmarkBriefFile{
			File:           file,
			Invariants:     dedupeStrings(entry.invariants),
			FailureModes:   dedupeStrings(entry.failureModes),
			Intents:        dedupeStrings(entry.intents),
			RequiredTests:  dedupeStrings(entry.requiredTests),
			ForbiddenFixes: dedupeStrings(entry.forbiddenFixes),
		}
		if len(item.Invariants) == 0 && len(item.FailureModes) == 0 && len(item.Intents) == 0 && len(item.RequiredTests) == 0 && len(item.ForbiddenFixes) == 0 {
			continue
		}
		out = append(out, item)
		testsToRun = append(testsToRun, item.RequiredTests...)
		forbiddenFixes = append(forbiddenFixes, item.ForbiddenFixes...)
		invariantIDs = append(invariantIDs, item.Invariants...)
		intentIDs = append(intentIDs, item.Intents...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out, dedupeStrings(testsToRun), dedupeStrings(forbiddenFixes), dedupeStrings(invariantIDs), dedupeStrings(intentIDs), nil
}

func collectBenchmarkAuthorityGaps(repoRoot string, intentIDs []string) ([]string, error) {
	proposals, err := collectAuthorityLane(repoRoot)
	if err != nil {
		return nil, err
	}
	intentSet := map[string]bool{}
	for _, id := range intentIDs {
		intentSet[id] = true
	}
	var out []string
	for _, p := range proposals {
		if len(intentSet) > 0 && !intentSet[p.IntentID] {
			continue
		}
		out = append(out, fmt.Sprintf("%s: %s", p.IntentID, p.Reason))
	}
	sort.Strings(out)
	return out, nil
}

func collectBenchmarkEvidenceGaps(repoRoot string, files, invariants []string) ([]string, error) {
	_, proposals, err := collectAnnotationInvariantTests(repoRoot)
	if err != nil {
		return nil, err
	}
	invSet := map[string]bool{}
	for _, id := range invariants {
		invSet[id] = true
	}
	fileSet := map[string]bool{}
	for _, file := range files {
		fileSet[file] = true
	}
	var out []string
	for _, p := range proposals {
		if len(invSet) > 0 && invSet[p.InvariantID] {
			out = append(out, fmt.Sprintf("%s: %s", p.InvariantID, p.Reason))
			continue
		}
		for _, ev := range p.Evidence {
			file, _, ok := strings.Cut(ev, ":")
			if ok && fileSet[file] {
				out = append(out, fmt.Sprintf("%s: %s", p.InvariantID, p.Reason))
				break
			}
		}
	}
	return dedupeStrings(out), nil
}

func pathMatchesBenchmarkAnchor(file, anchor string) bool {
	file = filepath.ToSlash(strings.TrimSpace(file))
	anchor = filepath.ToSlash(strings.TrimSpace(anchor))
	if file == "" || anchor == "" {
		return false
	}
	if file == anchor {
		return true
	}
	anchor = strings.TrimSuffix(anchor, "/")
	return strings.HasPrefix(file, anchor+"/")
}

func filterBenchmarkAnchorsByChangedFiles(testFiles []string, evidence []bootstrapEvidence, changedFiles []string) ([]string, []bootstrapEvidence) {
	if len(changedFiles) == 0 {
		return dedupeStrings(testFiles), evidence
	}
	dirSet := map[string]bool{}
	for _, file := range changedFiles {
		file = filepath.ToSlash(strings.TrimSpace(file))
		if file == "" {
			continue
		}
		dirSet[filepath.Dir(file)] = true
	}
	var filteredFiles []string
	for _, file := range dedupeStrings(testFiles) {
		if dirSet[filepath.Dir(file)] {
			filteredFiles = append(filteredFiles, file)
		}
	}
	if len(filteredFiles) == 0 {
		return dedupeStrings(testFiles), evidence
	}
	var filteredEvidence []bootstrapEvidence
	fileSet := map[string]bool{}
	for _, file := range filteredFiles {
		fileSet[file] = true
	}
	for _, ev := range evidence {
		if fileSet[ev.File] {
			filteredEvidence = append(filteredEvidence, ev)
		}
	}
	return filteredFiles, filteredEvidence
}

func renderBenchmarkBriefText(res benchmarkBriefResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Repository: %s\n", res.RepoRoot)
	if res.IssueSource != "" {
		fmt.Fprintf(&b, "Issue source: %s\n", res.IssueSource)
	}
	if res.Issue != "" {
		fmt.Fprintf(&b, "Issue: %s\n", res.Issue)
	}
	if len(res.ChangedFiles) > 0 {
		fmt.Fprintf(&b, "\nChanged files:\n")
		for _, item := range res.ChangedFiles {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	fmt.Fprintf(&b, "\nLikely implementation files:\n")
	for _, item := range res.LikelyImplementationFiles {
		fmt.Fprintf(&b, "  - %s\n", item)
	}
	fmt.Fprintf(&b, "\nLikely proving tests:\n")
	for _, item := range res.LikelyProvingTests {
		fmt.Fprintf(&b, "  - %s\n", item)
	}
	fmt.Fprintf(&b, "\nCandidate files:\n")
	for _, item := range res.CandidateFiles {
		fmt.Fprintf(&b, "  - %s\n", item)
	}
	if len(res.MechanicalEvidence) > 0 {
		fmt.Fprintf(&b, "\nMechanical evidence:\n")
		for _, ev := range res.MechanicalEvidence {
			fmt.Fprintf(&b, "  - %s:%d:%s\n", ev.File, ev.Line, strings.TrimSpace(ev.Text))
		}
	}
	if len(res.AWGFiles) > 0 {
		fmt.Fprintf(&b, "\nAWG context by file:\n")
		for _, item := range res.AWGFiles {
			fmt.Fprintf(&b, "  - %s\n", item.File)
			if len(item.Invariants) > 0 {
				fmt.Fprintf(&b, "    invariants: %s\n", strings.Join(item.Invariants, ", "))
			}
			if len(item.FailureModes) > 0 {
				fmt.Fprintf(&b, "    failure_modes: %s\n", strings.Join(item.FailureModes, ", "))
			}
			if len(item.Intents) > 0 {
				fmt.Fprintf(&b, "    intents: %s\n", strings.Join(item.Intents, ", "))
			}
			if len(item.RequiredTests) > 0 {
				fmt.Fprintf(&b, "    required_tests: %s\n", strings.Join(item.RequiredTests, ", "))
			}
			if len(item.ForbiddenFixes) > 0 {
				fmt.Fprintf(&b, "    forbidden_fixes: %s\n", strings.Join(item.ForbiddenFixes, ", "))
			}
		}
	}
	if res.RepairPlan != nil {
		fmt.Fprintf(&b, "\nAuthoritative repair plan:\n")
		for _, item := range res.RepairPlan.OrderedSteps {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
		if len(res.RepairPlan.Proof.Authority) > 0 {
			fmt.Fprintf(&b, "  authority_surfaces: ")
			var ids []string
			for _, authority := range res.RepairPlan.Proof.Authority {
				ids = append(ids, authority.ID)
			}
			fmt.Fprintf(&b, "%s\n", strings.Join(ids, ", "))
		}
		if len(res.RepairPlan.Proof.Obligations) > 0 {
			fmt.Fprintf(&b, "  proof_obligations: ")
			var ids []string
			for _, obligation := range res.RepairPlan.Proof.Obligations {
				ids = append(ids, obligation.ID)
			}
			fmt.Fprintf(&b, "%s\n", strings.Join(ids, ", "))
		}
	}
	if len(res.TestsToRun) > 0 {
		fmt.Fprintf(&b, "\nTests to run:\n")
		for _, item := range res.TestsToRun {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	if len(res.ForbiddenFixes) > 0 {
		fmt.Fprintf(&b, "\nForbidden fixes:\n")
		for _, item := range res.ForbiddenFixes {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	if len(res.AuthorityGaps) > 0 {
		fmt.Fprintf(&b, "\nAuthority gaps:\n")
		for _, item := range res.AuthorityGaps {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	if len(res.EvidenceGaps) > 0 {
		fmt.Fprintf(&b, "\nEvidence gaps:\n")
		for _, item := range res.EvidenceGaps {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	return b.String()
}

func buildAuthoritativeRepairPlan(repoRoot, addr, task string, files []string) (repairPlanResult, error) {
	authPath, proofObPath, forbiddenFixPath := defaultProofPlanPaths(repoRoot, "", "", "")
	authorities, err := loadAuthoritySurfaces(authPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return repairPlanResult{}, fmt.Errorf("load authority surfaces: %w", err)
		}
	}
	proofDoc, err := loadProofObligationsForCertify(proofObPath)
	if err != nil {
		return repairPlanResult{}, fmt.Errorf("load proof obligations: %w", err)
	}
	forbiddenFixes, err := loadForbiddenFixes(forbiddenFixPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return repairPlanResult{}, fmt.Errorf("load forbidden fixes: %w", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := repairPlanPreflight(ctx, addr, &awarenesspb.PreflightRequest{
		Task:  task,
		Files: dedupeStrings(files),
		Mode:  awarenesspb.PreflightMode_PREFLIGHT_STANDARD,
	})
	if err != nil {
		return repairPlanResult{}, err
	}
	if err := requireAuthoritativeGraph(resp.GetAuthority(), "benchmark-brief"); err != nil {
		return repairPlanResult{}, err
	}
	proof, err := buildProofPlanForFiles(repoRoot, authorities, proofDoc, forbiddenFixes, dedupeStrings(files))
	if err != nil {
		return repairPlanResult{}, fmt.Errorf("build proof plan: %w", err)
	}
	result := buildRepairPlanResult(task, dedupeStrings(files), resp, proof)
	return result, nil
}
