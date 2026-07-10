// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// runImpactGate implements `awg impact-gate` (CG-5): the per-change enforcement
// that turns `required_tests` from advisory metadata into a fail-closed gate.
//
// When a PR changes a file, the invariants that protect that file (via
// protects.files / implemented_by) name the tests that MUST guard it. This gate:
//
//	resolve: changed files -> protecting invariants -> their required_tests
//	emit   : a `go test -run` plan so CI runs exactly those tests
//	verify : given `go test -json` output, FAIL if any required test did not pass
//
// Before this, required_tests was documentation: nothing failed a PR that
// touched a protected file without running its tests (see CG-4). This closes
// that gap.
//
// Usage in CI (two-step, fail-closed):
//
//	files=$(git diff --name-only "$BASE"...HEAD)
//	plan=$(awg impact-gate -changed-files "$files" -services-repo "$SVC" -format run)
//	(cd "$SVC/golang" && go test -run "$plan" ./... -json) > results.json || true
//	awg impact-gate -changed-files "$files" -services-repo "$SVC" -ran results.json   # exits 1 on any miss
func runImpactGate(args []string) int {
	fs := flag.NewFlagSet("awg impact-gate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	changed := fs.String("changed-files", "", "changed files (newline/comma/space separated, or '-' for stdin)")
	svcRepoFlag := fs.String("services-repo", "", "path to services repo (auto-detect)")
	ran := fs.String("ran", "", "verify mode: a `go test -json` results file; FAIL if a required test did not pass")
	format := fs.String("format", "plan", "output: plan | run | json (ignored in -ran verify mode)")
	requireCoverage := fs.Bool("require-coverage", false, "also FAIL if a changed protected file has a protecting invariant with NO required_tests")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg impact-gate [flags]

Resolve changed files -> protecting invariants -> required_tests, then either
emit a run-plan (-format run|plan|json) or VERIFY a go-test-json result (-ran)
and fail closed if a required test did not pass.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	svcRepo, _ := resolveServicesRepo(*svcRepoFlag)
	if svcRepo == "" {
		fmt.Fprintln(os.Stderr, "awg impact-gate: cannot resolve services repo; pass -services-repo")
		return 2
	}
	files, err := readChangedFiles(*changed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg impact-gate: %v\n", err)
		return 2
	}
	invs, err := loadImpactInvariants(filepath.Join(svcRepo, "docs", "awareness", "invariants.yaml"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg impact-gate: %v\n", err)
		return 1
	}

	res := resolveImpactTests(invs, files)

	// Verify mode: a required test that did not PASS fails the gate.
	if strings.TrimSpace(*ran) != "" {
		passed, perr := parsePassedTests(*ran)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "awg impact-gate: %v\n", perr)
			return 1
		}
		var missing []string
		for _, tt := range res.UnionTests {
			short := shortTestID(tt)
			if !isRunnableTest(short) {
				continue // guard-rule reference, not a `go test` target — out of scope for this gate
			}
			if !passed[short] && !passed[tt] {
				missing = append(missing, tt)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			fmt.Fprintf(os.Stderr, "awg impact-gate: FAIL — %d required test(s) for changed protected files did not pass:\n", len(missing))
			for _, m := range missing {
				fmt.Fprintf(os.Stderr, "  - %s\n", m)
			}
			return 1
		}
		fmt.Printf("awg impact-gate: PASS — all %d required test(s) for %d changed protected file(s) passed\n",
			len(res.UnionTests), len(res.PerFile))
		return 0
	}

	if *requireCoverage && len(res.Gaps) > 0 {
		fmt.Fprintf(os.Stderr, "awg impact-gate: FAIL — %d protecting invariant(s) on changed files have no required_tests:\n", len(res.Gaps))
		for _, g := range res.Gaps {
			fmt.Fprintf(os.Stderr, "  - %s\n", g)
		}
		return 1
	}

	switch *format {
	case "run":
		// A `go test -run` anchored alternation of the required test names.
		fmt.Println(runRegex(res.UnionTests))
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
	default:
		printImpactPlan(res)
	}
	return 0
}

// ── resolution core (pure, testable) ──────────────────────────────────────

type impactInvariant struct {
	ID            string
	RequiredTests []string
	Files         []string // union of protects.files + implemented_by[].file
}

type impactMatch struct {
	InvariantID   string   `json:"invariant_id"`
	RequiredTests []string `json:"required_tests"`
}

type impactResult struct {
	PerFile    map[string][]impactMatch `json:"per_file"`
	UnionTests []string                 `json:"required_tests"`
	Gaps       []string                 `json:"coverage_gaps"` // matched invariants with no required_tests
}

// resolveImpactTests maps each changed file to the invariants that protect it
// and the union of their required_tests. A protected file whose invariant
// declares no required_tests is recorded as a coverage gap. Deterministic.
func resolveImpactTests(invs []impactInvariant, changedFiles []string) impactResult {
	res := impactResult{PerFile: map[string][]impactMatch{}}
	unionSet := map[string]bool{}
	gapSet := map[string]bool{}

	changed := map[string]bool{}
	for _, f := range changedFiles {
		if f = strings.TrimSpace(f); f != "" {
			changed[normalizeRepoPath(f)] = true
		}
	}

	for _, inv := range invs {
		for _, pf := range inv.Files {
			cf := normalizeRepoPath(pf)
			if !changed[cf] {
				continue
			}
			match := impactMatch{InvariantID: inv.ID, RequiredTests: inv.RequiredTests}
			res.PerFile[cf] = append(res.PerFile[cf], match)
			if len(inv.RequiredTests) == 0 {
				gapSet[inv.ID] = true
			}
			for _, tt := range inv.RequiredTests {
				unionSet[tt] = true
			}
		}
	}

	for t := range unionSet {
		res.UnionTests = append(res.UnionTests, t)
	}
	for g := range gapSet {
		res.Gaps = append(res.Gaps, g)
	}
	sort.Strings(res.UnionTests)
	sort.Strings(res.Gaps)
	for _, m := range res.PerFile {
		sort.Slice(m, func(i, j int) bool { return m[i].InvariantID < m[j].InvariantID })
	}
	return res
}

// isRunnableTest reports whether a (short) required_test id is a Go test that
// `go test -run` can target. Some required_tests are guard-rule references
// (principle-check ids, not test functions); those are enforced by their own
// scanner, not by this gate.
func isRunnableTest(short string) bool {
	return strings.HasPrefix(short, "Test")
}

// normalizeRepoPath strips a leading "services/" so a git diff path (which may
// be repo-prefixed) matches an awareness anchor (which is repo-relative).
func normalizeRepoPath(p string) string {
	p = filepath.ToSlash(strings.TrimSpace(p))
	return strings.TrimPrefix(p, "services/")
}

// runRegex builds an anchored `go test -run` alternation. Empty when there are
// no required tests (caller should treat an empty plan as "nothing to run").
func runRegex(tests []string) string {
	if len(tests) == 0 {
		return ""
	}
	short := make([]string, 0, len(tests))
	seen := map[string]bool{}
	for _, t := range tests {
		s := shortTestID(t)
		if s == "" {
			s = t
		}
		if !isRunnableTest(s) {
			continue // skip non-`go test` guard references (e.g. principle-check rule ids)
		}
		if !seen[s] {
			seen[s] = true
			short = append(short, s)
		}
	}
	if len(short) == 0 {
		return ""
	}
	sort.Strings(short)
	return "^(" + strings.Join(short, "|") + ")$"
}

func printImpactPlan(res impactResult) {
	if len(res.PerFile) == 0 {
		fmt.Println("awg impact-gate: no changed file is protected by an invariant — nothing to enforce")
		return
	}
	files := make([]string, 0, len(res.PerFile))
	for f := range res.PerFile {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		fmt.Printf("%s\n", f)
		for _, m := range res.PerFile[f] {
			tests := "(NO required_tests — coverage gap)"
			if len(m.RequiredTests) > 0 {
				tests = strings.Join(m.RequiredTests, ", ")
			}
			fmt.Printf("  [%s] %s\n", m.InvariantID, tests)
		}
	}
	fmt.Printf("\nrequired tests (%d): %s\n", len(res.UnionTests), runRegex(res.UnionTests))
}

// ── invariant loading ─────────────────────────────────────────────────────

func loadImpactInvariants(path string) ([]impactInvariant, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Invariants []struct {
			ID            string   `yaml:"id"`
			RequiredTests []string `yaml:"required_tests"`
			ImplementedBy []struct {
				File string `yaml:"file"`
			} `yaml:"implemented_by"`
			Protects struct {
				Files []string `yaml:"files"`
			} `yaml:"protects"`
		} `yaml:"invariants"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse invariants.yaml: %w", err)
	}
	out := make([]impactInvariant, 0, len(doc.Invariants))
	for _, inv := range doc.Invariants {
		fileSet := map[string]bool{}
		var files []string
		add := func(f string) {
			f = strings.TrimSpace(f)
			if f != "" && !fileSet[f] {
				fileSet[f] = true
				files = append(files, f)
			}
		}
		for _, ib := range inv.ImplementedBy {
			add(ib.File)
		}
		for _, pf := range inv.Protects.Files {
			add(pf)
		}
		if len(files) == 0 {
			continue // an invariant with no file anchor cannot be impacted by a file change
		}
		out = append(out, impactInvariant{ID: inv.ID, RequiredTests: inv.RequiredTests, Files: files})
	}
	return out, nil
}

// ── inputs ────────────────────────────────────────────────────────────────

func readChangedFiles(src string) ([]string, error) {
	var data string
	switch {
	case src == "-":
		b, err := os.ReadFile("/dev/stdin")
		if err != nil {
			return nil, err
		}
		data = string(b)
	default:
		data = src
	}
	fields := strings.FieldsFunc(data, func(r rune) bool {
		return r == '\n' || r == ',' || r == ' ' || r == '\t' || r == '\r'
	})
	return fields, nil
}

// parsePassedTests reads `go test -json` output and returns the set of tests
// whose terminal Action was "pass". Sub-test and package events are tolerated.
func parsePassedTests(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	passed := map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] != '{' {
			continue
		}
		var ev struct {
			Action string `json:"Action"`
			Test   string `json:"Test"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Test == "" {
			continue
		}
		switch ev.Action {
		case "pass":
			passed[ev.Test] = true
		case "fail":
			delete(passed, ev.Test)
		}
	}
	return passed, sc.Err()
}
