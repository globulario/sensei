// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type benchmarkJudgeResult struct {
	RepoRoot             string   `json:"repo_root"`
	ChangedFiles         []string `json:"changed_files,omitempty"`
	TestsRun             []string `json:"tests_run,omitempty"`
	RequiredTests        []string `json:"required_tests,omitempty"`
	MissingRequiredTests []string `json:"missing_required_tests,omitempty"`
	ForbiddenFixes       []string `json:"forbidden_fixes,omitempty"`
	AuthorityGaps        []string `json:"authority_gaps,omitempty"`
	EvidenceGaps         []string `json:"evidence_gaps,omitempty"`
	ContractPreservation string   `json:"contract_preservation"`
	TestDiscipline       string   `json:"test_discipline"`
	AuthorityDiscipline  string   `json:"authority_discipline"`
}

func runBenchmarkJudge(args []string) int {
	fs := flag.NewFlagSet("sensei benchmark-judge", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoRoot := fs.String("repo-root", ".", "repository root to analyze")
	taskFile := fs.String("task-file", "", "task JSON containing issue/f2p_tests/files")
	issue := fs.String("issue", "", "issue text when not using --task-file")
	format := fs.String("format", "text", "output format: text | json")
	asJSON := fs.Bool("json", false, "output as JSON (deprecated: same as --format json)")
	var tests stringSlice
	var files stringSlice
	var testsRun stringSlice
	fs.Var(&tests, "f2p-test", "fail-to-pass test name (repeatable)")
	fs.Var(&files, "file", "changed or judged file path (repeatable)")
	fs.Var(&testsRun, "test-run", "executed test id, function, or file path (repeatable)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei benchmark-judge [flags]

Judge a benchmark or PR patch envelope locally from touched files, executed
tests, and authored awareness metadata.

This command does not inspect a live server or infer patch correctness beyond
repository contracts and test discipline.

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

	task, _, err := loadBenchmarkBriefTask(*taskFile, *issue, tests, files)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei benchmark-judge: %v\n", err)
		return 2
	}
	root, err := filepath.Abs(*repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei benchmark-judge: resolve repo root: %v\n", err)
		return 1
	}
	res, err := buildBenchmarkJudge(root, task, testsRun)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei benchmark-judge: %v\n", err)
		return 1
	}

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
	default:
		fmt.Print(renderBenchmarkJudgeText(res))
	}
	return 0
}

func buildBenchmarkJudge(repoRoot string, task benchmarkBriefTask, testsRun []string) (benchmarkJudgeResult, error) {
	brief, err := buildBenchmarkBrief(repoRoot, task, "judge")
	if err != nil {
		return benchmarkJudgeResult{}, err
	}
	requiredTests := benchmarkJudgeRequiredTests(brief.AWGFiles)
	missingRequiredTests := benchmarkMissingRequiredTests(requiredTests, testsRun)
	contractPreservation := "ok"
	if len(brief.ForbiddenFixes) > 0 || len(brief.EvidenceGaps) > 0 {
		contractPreservation = "review_required"
	}
	testDiscipline := "ok"
	if len(missingRequiredTests) > 0 {
		testDiscipline = "insufficient"
	}
	authorityDiscipline := "ok"
	if len(brief.AuthorityGaps) > 0 || len(brief.EvidenceGaps) > 0 {
		authorityDiscipline = "review_required"
	}
	return benchmarkJudgeResult{
		RepoRoot:             repoRoot,
		ChangedFiles:         dedupeStrings(task.Files),
		TestsRun:             dedupeStrings(testsRun),
		RequiredTests:        requiredTests,
		MissingRequiredTests: missingRequiredTests,
		ForbiddenFixes:       brief.ForbiddenFixes,
		AuthorityGaps:        brief.AuthorityGaps,
		EvidenceGaps:         brief.EvidenceGaps,
		ContractPreservation: contractPreservation,
		TestDiscipline:       testDiscipline,
		AuthorityDiscipline:  authorityDiscipline,
	}, nil
}

func benchmarkJudgeRequiredTests(files []benchmarkBriefFile) []string {
	var out []string
	for _, file := range files {
		out = append(out, file.RequiredTests...)
	}
	return dedupeStrings(out)
}

func benchmarkMissingRequiredTests(requiredTests, testsRun []string) []string {
	runSet := map[string]bool{}
	runFiles := map[string]bool{}
	runSymbols := map[string]bool{}
	for _, item := range dedupeStrings(testsRun) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		runSet[item] = true
		if strings.HasSuffix(item, ".go") {
			runFiles[filepath.ToSlash(item)] = true
		}
		if strings.Contains(item, ":") {
			_, sym, _ := strings.Cut(item, ":")
			runSymbols[sym] = true
		} else {
			runSymbols[item] = true
		}
	}
	var missing []string
	for _, req := range dedupeStrings(requiredTests) {
		if runSet[req] {
			continue
		}
		file, sym, ok := strings.Cut(req, ":")
		if ok {
			if runFiles[filepath.ToSlash(file)] || runSymbols[sym] {
				continue
			}
		} else if runFiles[filepath.ToSlash(req)] {
			continue
		}
		missing = append(missing, req)
	}
	sort.Strings(missing)
	return missing
}

func renderBenchmarkJudgeText(res benchmarkJudgeResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Repository: %s\n", res.RepoRoot)
	fmt.Fprintf(&b, "Contract preservation: %s\n", res.ContractPreservation)
	fmt.Fprintf(&b, "Test discipline: %s\n", res.TestDiscipline)
	fmt.Fprintf(&b, "Authority discipline: %s\n", res.AuthorityDiscipline)
	if len(res.ChangedFiles) > 0 {
		fmt.Fprintf(&b, "\nChanged files:\n")
		for _, item := range res.ChangedFiles {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	if len(res.TestsRun) > 0 {
		fmt.Fprintf(&b, "\nTests run:\n")
		for _, item := range res.TestsRun {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	if len(res.RequiredTests) > 0 {
		fmt.Fprintf(&b, "\nRequired tests:\n")
		for _, item := range res.RequiredTests {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	if len(res.MissingRequiredTests) > 0 {
		fmt.Fprintf(&b, "\nMissing required tests:\n")
		for _, item := range res.MissingRequiredTests {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	if len(res.ForbiddenFixes) > 0 {
		fmt.Fprintf(&b, "\nForbidden fixes in scope:\n")
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
