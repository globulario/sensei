// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=cli.feedback_check
// @awareness file_role=session_feedback_gap_detector
// @awareness risk=medium
//
// awg feedback-check is the backing logic for the AWG Stop-hook. It inspects
// the files a session changed and warns — advisory only, never blocking — when
// a fix likely produced graph-worthy knowledge (a scar) but no awareness graph
// feedback was written. It is the nudge that keeps the write path (awg propose)
// from being deferred.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// feedbackGapResult is the structured verdict over a session's changed files.
type feedbackGapResult struct {
	Warn            bool     `json:"warn"`
	FeedbackWritten bool     `json:"feedback_written"`
	RiskSignals     []string `json:"risk_signals"`
	Reminder        string   `json:"reminder,omitempty"`
	Suggestions     []string `json:"suggestions,omitempty"`
}

// feedbackReminderText is the exact advisory the hook surfaces. Kept as a const
// so the hook contract is stable and assertable.
const feedbackReminderText = "Possible missing AWG feedback: this session fixed a durable error class but did not add graph feedback."

func runFeedbackCheck(args []string) int {
	fs := flag.NewFlagSet("awg feedback-check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	repoRoot := fs.String("repo-root", "", "repo root (auto-detect)")
	var changedFiles multiString
	fs.Var(&changedFiles, "changed-file", "explicit changed file (repeatable; for tests/CI). When none are given, files are derived from git status.")
	useGit := fs.Bool("git", true, "derive changed files from git status when none are passed")
	strict := fs.Bool("strict", false, "exit non-zero when a feedback gap is detected (default: advisory, always exit 0)")
	format := fs.String("format", "text", "output format: text | json")
	quiet := fs.Bool("quiet", false, "print nothing when there is no gap")

	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg feedback-check [flags]

Advisory check (Stop-hook backing): warns when a session changed risky code or
added an incident/regression test but wrote no awareness graph feedback.

It NEVER blocks by default — it prints a reminder and exits 0. Use --strict to
make a detected gap exit non-zero (e.g. in a pre-push check).

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}

	root, err := resolveProjectRoot(*repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg feedback-check: %v\n", err)
		return 0 // advisory: never fail the session on our own error
	}

	changed := []string(changedFiles)
	if len(changed) == 0 && *useGit {
		changed = gitChangedFiles(root)
	}

	highRisk := readHighRiskPrefixes(root)
	res := evaluateFeedbackGap(changed, highRisk)

	if *format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
	} else {
		printFeedbackGap(res, *quiet)
	}

	if res.Warn && *strict {
		return 1
	}
	return 0
}

// ── pure core ─────────────────────────────────────────────────────────────

var (
	feedbackFileRe = regexp.MustCompile(`(?i)docs/awareness/(failure_modes|invariants|required_tests|forbidden_fixes|incident_patterns)\.ya?ml$`)
	candidateCUre  = regexp.MustCompile(`(?i)docs/awareness/candidates/contract_unknown_.*\.ya?ml$`)
	testFileRe     = regexp.MustCompile(`(?i)(_test\.go|\.test\.[tj]sx?|_test\.py|test_.*\.py)$`)
	incidentRe     = regexp.MustCompile(`(?i)(incident|regression|repro|_bug|bug_|cve|hotfix|postmortem)`)
	codeFileRe     = regexp.MustCompile(`(?i)\.(go|ts|tsx|js|jsx|py|rs)$`)
	// Subsystems whose behavior, when changed, almost always carries a durable
	// invariant worth recording.
	sensitiveRe = regexp.MustCompile(`(?i)(^|/)(auth|authn|authz|rbac|security|cluster|storage|store|backup|runtime|infra|etcd|oxigraph|interceptor|identity|pki|attestation|session|credential|secret|token)`)
)

// evaluateFeedbackGap is the deterministic, side-effect-free decision used by
// both the CLI and the tests. It classifies each changed path and decides
// whether the session left a graph-worthy gap.
func evaluateFeedbackGap(changed []string, highRiskPrefixes []string) feedbackGapResult {
	res := feedbackGapResult{}
	signals := map[string]bool{}

	var codeChanged, testChanged bool
	for _, raw := range changed {
		p := filepath.ToSlash(strings.TrimSpace(raw))
		if p == "" {
			continue
		}

		if feedbackFileRe.MatchString(p) || candidateCUre.MatchString(p) {
			res.FeedbackWritten = true
			continue // a feedback file is not itself a risk signal
		}

		isTest := testFileRe.MatchString(p)
		if isTest {
			testChanged = true
			if incidentRe.MatchString(p) {
				signals["incident/regression test added: "+p] = true
			}
		} else if codeFileRe.MatchString(p) {
			codeChanged = true
		}

		if matchesAnyPrefix(p, highRiskPrefixes) {
			signals["high-risk path edited: "+p] = true
		}
		if codeFileRe.MatchString(p) && sensitiveRe.MatchString(p) {
			signals["infra/runtime/auth/storage/cluster behavior changed: "+p] = true
		}
	}

	// The classic scar shape: an error class was fixed (production code changed)
	// AND a guarding test was added in the same session.
	if codeChanged && testChanged {
		signals["error class fixed with a test, but no invariant/failure_mode/required_test/forbidden_fix added"] = true
	}

	res.RiskSignals = sortedKeys(signals)
	res.Warn = len(res.RiskSignals) > 0 && !res.FeedbackWritten
	if res.Warn {
		res.Reminder = feedbackReminderText
		res.Suggestions = feedbackSuggestions()
	}
	return res
}

func feedbackSuggestions() []string {
	return []string{
		`awg propose --kind failure_mode --title "<what broke>" --related-invariant <inv.id> --evidence "<observed>" --source-file <path>`,
		`awg propose --kind required_test --id "<path>_test.go:TestName" --title "<what it proves>" --related-failure <fm.id>`,
		`awg propose --kind forbidden_fix --title "<the wrong fix>" --related-invariant <inv.id> --description "<why it's wrong>"`,
		`awg propose --kind invariant --title "<rule that must hold>" --source-file <path> --related-failure <fm.id>`,
	}
}

func matchesAnyPrefix(path string, prefixes []string) bool {
	for _, pre := range prefixes {
		pre = strings.TrimSpace(filepath.ToSlash(pre))
		if pre == "" {
			continue
		}
		if path == pre || strings.HasPrefix(path, strings.TrimRight(pre, "/")+"/") || strings.HasPrefix(path, pre) {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ── git + config readers ──────────────────────────────────────────────────

// gitChangedFiles returns staged + unstaged + untracked paths from git status,
// relative to the repo root. Returns nil when git is unavailable.
func gitChangedFiles(root string) []string {
	out, err := exec.Command("git", "-C", root, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil {
		return nil
	}
	var files []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		// Porcelain: "XY <path>" or "XY <old> -> <new>" for renames.
		path := strings.TrimSpace(line[3:])
		if idx := strings.Index(path, " -> "); idx >= 0 {
			path = path[idx+4:]
		}
		path = strings.Trim(path, `"`)
		if path != "" && !seen[path] {
			seen[path] = true
			files = append(files, path)
		}
	}
	return files
}

// readHighRiskPrefixes loads docs/awareness/high_risk_files.yaml (the same list
// the enforce-briefing hook reads). Missing file → no prefixes.
func readHighRiskPrefixes(root string) []string {
	path := filepath.Join(root, "docs", "awareness", "high_risk_files.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var prefixes []string
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "- ") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(t, "- "))
		if i := strings.Index(v, "#"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		v = strings.Trim(v, `"'`)
		if v != "" {
			prefixes = append(prefixes, v)
		}
	}
	return prefixes
}

func printFeedbackGap(res feedbackGapResult, quiet bool) {
	if !res.Warn {
		if !quiet {
			if res.FeedbackWritten {
				fmt.Println("awg feedback-check: graph feedback was added this session — ok")
			} else {
				fmt.Println("awg feedback-check: no graph-worthy changes detected — ok")
			}
		}
		return
	}
	fmt.Println(res.Reminder)
	fmt.Println("\nWhy this fired:")
	for _, s := range res.RiskSignals {
		fmt.Printf("  - %s\n", s)
	}
	fmt.Println("\nAdd the scar with one typed call, e.g.:")
	for _, s := range res.Suggestions {
		fmt.Printf("  %s\n", s)
	}
}
