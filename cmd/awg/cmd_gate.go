// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/globulario/awareness-graph/golang/client"
	"github.com/globulario/awareness-graph/golang/evidence"
	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
)

// printReportHeader prints the report-only header: domain, diff range, the count
// of changed files actually evaluated, and the warn / would-block tallies.
func printReportHeader(domain, diff string, filesEvaluated, warns, wouldBlock int) {
	fmt.Println("AWG gate (report-only, non-blocking)")
	fmt.Printf("  domain: %s\n", domain)
	fmt.Printf("  diff:   %s\n", diff)
	fmt.Printf("  changed files evaluated: %d\n", filesEvaluated)
	fmt.Printf("  warnings: %d   would-block: %d\n\n", warns, wouldBlock)
}

// finalReportLine prints the canonical closing line and returns 0 — the gate is
// report-only, so it never fails.
func finalReportLine(wouldBlock int, degradedNote string) int {
	if degradedNote != "" {
		fmt.Printf("AWG gate report-only: 0 hard failures, %d would-block findings (degraded: %s)\n", wouldBlock, degradedNote)
	} else {
		fmt.Printf("AWG gate report-only: 0 hard failures, %d would-block findings\n", wouldBlock)
	}
	return 0
}

// reportDegraded prints a degraded (fail-open) report and returns 0. Used when
// the gate could not run — AWG unavailable, a git error, etc. A degraded gate
// must never fail the PR.
func reportDegraded(domain, diff, reason string) int {
	fmt.Println("AWG gate (report-only, non-blocking) — DEGRADED")
	fmt.Printf("  domain: %s\n", domain)
	fmt.Printf("  diff:   %s\n", diff)
	fmt.Printf("  reason: %s\n", reason)
	return finalReportLine(0, reason)
}

// fileFinding is one changed file's EditCheck result: the advisory/blocking
// warnings its added lines tripped, or a scope error if it could not be checked.
type fileFinding struct {
	File       string                     `json:"file"`
	Warnings   []*awarenesspb.EditWarning `json:"warnings,omitempty"`
	ScopeError string                     `json:"scope_error,omitempty"`
}

// gateVerdict decides the ENFORCING gate's exit code from the run's tallies. It
// is pure so the enforce decision is unit-tested without a server.
//
//	0 — PASS: no enforcement:block finding tripped.
//	1 — BLOCKED: at least one enforcement:block finding on the diff.
//	2 — CANNOT VERIFY: AWG was unavailable for the whole diff. A control gate
//	    fails CLOSED here — it must not silently pass a change it couldn't check.
//	    (Use --report-only for the fail-open advisory mode.)
func gateVerdict(wouldBlock, warns, filesEvaluated int, unavailable bool) (int, string) {
	switch {
	case unavailable && filesEvaluated == 0:
		return 2, "CANNOT VERIFY: AWG unavailable — gate failed closed (nothing evaluated); use --report-only to fail open"
	case wouldBlock > 0:
		return 1, fmt.Sprintf("BLOCKED: %d enforcement:block finding(s) on the diff — revise or waive", wouldBlock)
	default:
		return 0, fmt.Sprintf("PASS: 0 blocking findings (%d advisory warning(s))", warns)
	}
}

// policyJSON is the machine-readable view of the active policy for --json.
func policyJSON(p gatePolicy) map[string]interface{} {
	def := p.Default
	if def == "" {
		def = enforceInherit
	}
	return map[string]interface{}{
		"source":         p.loadedFrom,
		"default":        def,
		"rule_overrides": len(p.Rules),
	}
}

// printPolicyLine notes the active enforcement policy so a re-leveled verdict is
// never a surprise. Silent when no policy is configured (pure inherit).
func printPolicyLine(p gatePolicy) {
	if p.loadedFrom == "" && p.Default == "" && len(p.Rules) == 0 {
		return
	}
	src := p.loadedFrom
	if src == "" {
		src = "(inline)"
	}
	def := p.Default
	if def == "" {
		def = enforceInherit
	}
	fmt.Printf("  policy: %s (default: %s, %d rule override(s))\n", src, def, len(p.Rules))
}

// decisionFromCode maps an enforcing gate exit code to an evidence decision.
func decisionFromCode(code, warns int) string {
	switch code {
	case 1:
		return evidence.DecisionBlock
	case 2:
		return evidence.DecisionCannotVerify
	default:
		if warns > 0 {
			return evidence.DecisionWarn
		}
		return evidence.DecisionAllow
	}
}

// reportDecision maps a non-enforcing run's tallies to an evidence decision.
func reportDecision(wouldBlock, warns int) string {
	switch {
	case wouldBlock > 0:
		return evidence.DecisionWouldBlock
	case warns > 0:
		return evidence.DecisionWarn
	default:
		return evidence.DecisionAllow
	}
}

// gateFindingRules splits the findings' rule ids into blocking vs advisory by
// their EFFECTIVE (policy-resolved) enforcement.
func gateFindingRules(findings []fileFinding) (blocked, warned []string) {
	for _, fr := range findings {
		for _, w := range fr.Warnings {
			if w.GetEnforcement() == "block" {
				blocked = append(blocked, w.GetRuleId())
			} else {
				warned = append(warned, w.GetRuleId())
			}
		}
	}
	return blocked, warned
}

// emitGateEvent appends one outcome event to the evidence ledger (best-effort;
// a logging failure never affects the gate). No-op when logPath is empty.
func emitGateEvent(logPath, domain, diffRange string, enforced bool, decision string, findings []fileFinding, files []string) {
	if strings.TrimSpace(logPath) == "" {
		return
	}
	blocked, warned := gateFindingRules(findings)
	_ = evidence.Append(logPath, evidence.Event{
		TS:           time.Now().UTC().Format(time.RFC3339),
		Tool:         "gate",
		Repo:         domain,
		Decision:     decision,
		Enforced:     enforced,
		BlockedRules: blocked,
		WarnedRules:  warned,
		Files:        files,
		DiffRange:    diffRange,
	})
}

// printGateFindings prints per-file findings for the human report.
func printGateFindings(findings []fileFinding, withProvenance bool) {
	for _, fr := range findings {
		if fr.ScopeError != "" {
			fmt.Printf("  %s\n    [scope] %s\n", fr.File, fr.ScopeError)
			continue
		}
		fmt.Printf("  %s\n", fr.File)
		for _, w := range fr.Warnings {
			tag := "warn"
			if w.GetEnforcement() == "block" {
				tag = "BLOCK"
			}
			fmt.Printf("    [%s] %s — %s (enforcement: %s)\n", tag, w.GetRuleId(), w.GetMessage(), w.GetEnforcement())
			if d := w.GetDetail(); d != "" {
				fmt.Printf("      %s\n", d)
			}
			if withProvenance {
				if p := w.GetProvenance(); p != "" {
					fmt.Printf("      provenance: %s\n", p)
				}
			}
		}
	}
}

// runGate is the diff-gate entry point. By default it is a DRY-RUN report over a
// git diff: it reuses the EditCheck engine per changed file (added/changed lines
// only) and reports which findings WOULD block versus which are advisory. With
// --enforce it becomes a REAL gate: it exits non-zero on any enforcement:block
// finding (and fails closed if AWG could not verify the diff). --report-only is
// the fail-open advisory CI mode (always exit 0).
func runGate(args []string) int {
	fs := flag.NewFlagSet("awg gate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	diff := fs.String("diff", "HEAD", "git diff range to gate, e.g. 'origin/main...HEAD' or 'HEAD' (working tree vs HEAD)")
	domain := fs.String("domain", "", "domain/repo scope (e.g. github.com/caddyserver/caddy); required when the graph hosts >1 domain")
	addr := fs.String("addr", "localhost:10120", "AWG gRPC server address")
	repoRoot := fs.String("repo-root", ".", "path to the git repo to diff")
	asJSON := fs.Bool("json", false, "output as JSON")
	reportOnly := fs.Bool("report-only", false, "CI mode: always exit 0 (fail-open on any error), print a non-blocking report with a summary line")
	contractsPath := fs.String("contracts", "", "path to a frozen contract-set YAML file or directory; enables frozen-contract gate mode (does not use the AWG server)")
	enforce := fs.Bool("enforce", false, "REAL gate: exit non-zero on any enforcement:block finding (and fail closed if AWG cannot verify the diff). Works for both the EditCheck flow and --contracts mode. Default is dry-run.")
	completeness := fs.Bool("completeness", false, "run the advisory sibling-site completeness check (SCIP aw:references based): flag reference families the diff touched incompletely. Opt-in: discovery is file-level so it over-fires on broad diffs — best on a focused 'update all callers of X' change.")
	policyPath := fs.String("policy", "", "path to a per-repo enforcement-policy YAML (rule_id -> warn|block|off, plus optional default); overrides each rule's declared level. Default: <repo-root>/.awg/gate-policy.yaml when present.")
	eventLog := fs.String("event-log", os.Getenv("AWG_EVENT_LOG"), "append a JSONL outcome event (block/warn/allow + rules) to this ledger for evidence; see `awg evidence`. Default: $AWG_EVENT_LOG (off when empty).")
	maxFanout := fs.Int("completeness-max-fanout", 12, "completeness: ignore reference families larger than this (likely shared types/utilities, not must-change-together conventions)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg gate [--diff <range>] [--domain <repo>] [--enforce] [flags]

Evaluates a git diff's added/changed lines against the in-scope detect rules
(the same EditCheck engine agents call). Three modes:

  default        DRY-RUN: report which findings WOULD block vs advisory; exit 0.
  --enforce      REAL gate: exit 1 on any enforcement:block finding; exit 2 if
                 AWG could not verify the diff (fail closed). Package as a CI/PR
                 step. Never edits code.
  --report-only  Fail-open advisory CI mode: always exit 0, print a summary.

Enforcement is per-repo. A rule's declared level (warn|block) lives in the
graph, but a repo can re-level or silence it via a policy YAML (--policy, or
<repo-root>/.awg/gate-policy.yaml) with no code change:

    default: inherit          # inherit | warn | block | off
    rules:
      some.rule_id: block     # make advisory rule blocking here
      noisy.rule_id: off      # silence a rule for this repo

With --completeness (opt-in, all modes) it also runs an ADVISORY sibling-site
check: using the SCIP aw:references edges it flags reference families the diff
touched incompletely ("N call-sites reference X; you changed M, missed the
rest"). It never affects the exit code. Discovery is file-level, so it is
sharpest on a focused "update all callers of X" diff and noisier on broad ones.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// Frozen-contract gate (Phase-2): a self-contained, server-independent path
	// that activates only with --contracts. The default EditCheck/gRPC flow
	// below is untouched when --contracts is absent.
	if *contractsPath != "" {
		return runGateContracts(*repoRoot, *diff, *contractsPath, *enforce, *asJSON)
	}

	changes, err := gitAddedLinesByFile(*repoRoot, *diff)
	if err != nil {
		// Fail-open in report-only/CI mode: a git/diff error must never fail the
		// PR. Report degraded and exit 0.
		if *reportOnly {
			return reportDegraded(*domain, *diff, fmt.Sprintf("git diff failed: %v", err))
		}
		fmt.Fprintf(os.Stderr, "awg gate: %v\n", err)
		return 1
	}
	if len(changes) == 0 {
		if *reportOnly {
			printReportHeader(*domain, *diff, 0, 0, 0)
			fmt.Println("  (no added/changed lines to evaluate)")
			return finalReportLine(0, "")
		}
		fmt.Printf("awg gate (dry-run): no added/changed lines in %s — nothing to check.\n", *diff)
		return 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := client.DialConn(*addr)
	if err != nil {
		if *reportOnly {
			return reportDegraded(*domain, *diff, fmt.Sprintf("cannot reach AWG at %s: %v", *addr, err))
		}
		fmt.Fprintf(os.Stderr, "awg gate: connect %s: %v\n", *addr, err)
		return 1
	}
	defer conn.Close()
	client := awarenesspb.NewAwarenessGraphClient(conn)

	// Per-repo enforcement policy (Pillar 2.3): resolve BEFORE evaluating so a
	// repo can re-level or silence rules with no code change. A bad/missing
	// explicit policy fails loudly (fail-open only under --report-only).
	policy, err := resolveGatePolicy(*policyPath, *repoRoot)
	if err != nil {
		if *reportOnly {
			return reportDegraded(*domain, *diff, err.Error())
		}
		fmt.Fprintf(os.Stderr, "awg gate: %v\n", err)
		return 1
	}

	var findings []fileFinding
	files := make([]string, 0, len(changes))
	for f := range changes {
		files = append(files, f)
	}
	sort.Strings(files)

	wouldBlock, warns, scopeErrs := 0, 0, 0
	unavailable := false
	for _, f := range files {
		resp, err := client.EditCheck(ctx, &awarenesspb.EditCheckRequest{
			File:            f,
			ProposedContent: changes[f],
			Domain:          *domain,
		})
		if err != nil {
			// Fail-open posture for the dry-run: record the per-file scope/backend
			// error and keep going. (A multi-domain graph with no --domain reports
			// here instead of silently mixing.)
			if status.Code(err) == codes.Unavailable {
				unavailable = true
			}
			findings = append(findings, fileFinding{File: f, ScopeError: err.Error()})
			scopeErrs++
			continue
		}
		// Apply the repo's enforcement policy: re-level per rule and drop any
		// the policy set to "off". Downstream tally/print then reads the
		// EFFECTIVE level.
		ws := applyGatePolicy(resp.GetWarnings(), policy)
		if len(ws) == 0 {
			continue
		}
		findings = append(findings, fileFinding{File: f, Warnings: ws})
		for _, w := range ws {
			if w.GetEnforcement() == "block" {
				wouldBlock++
			} else {
				warns++
			}
		}
	}

	// Completeness: the advisory sibling-site check (persistent-graph-only
	// capability). Never affects the exit code; skipped if the backend was down
	// for the whole diff (nothing to build targets from).
	var compFindings []completenessFinding
	compNote := ""
	if *completeness && !(unavailable && scopeErrs == len(files)) {
		changedFiles := make(map[string]bool, len(changes))
		for f := range changes {
			changedFiles[f] = true
		}
		compFindings, compNote = runCompleteness(ctx, client, changedFiles, *domain, *maxFanout)
	}

	// Enforcing mode: a REAL gate. Exit non-zero on any enforcement:block
	// finding; fail closed if AWG could not verify the diff at all. This is what
	// turns "informs" into "controls" — package it as a CI/PR step.
	if *enforce {
		// filesEvaluated must exclude the files that errored, so an
		// all-unreachable diff fails CLOSED (cannot_verify) instead of falsely
		// passing when len(changes)>0.
		code, verdict := gateVerdict(wouldBlock, warns, len(changes)-scopeErrs, unavailable)
		emitGateEvent(*eventLog, *domain, *diff, true, decisionFromCode(code, warns), findings, files)
		if *asJSON {
			out := map[string]interface{}{
				"diff": *diff, "domain": *domain, "enforced": true,
				"blocked": code == 1, "would_block": wouldBlock, "warn": warns,
				"scope_errors": scopeErrs, "verdict": verdict, "files": findings,
				"completeness": compFindings, "policy": policyJSON(policy),
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(out)
			return code
		}
		fmt.Printf("AWG gate (ENFORCING) — diff %s, %d file(s) evaluated\n", *diff, len(changes))
		printPolicyLine(policy)
		fmt.Println()
		printGateFindings(findings, true)
		printCompleteness(compFindings, compNote)
		fmt.Printf("\n%s\n", verdict)
		return code
	}

	// Non-enforcing modes (report-only, dry-run) record one advisory event.
	nonEnforceDecision := reportDecision(wouldBlock, warns)
	if unavailable && (len(changes)-scopeErrs) == 0 {
		nonEnforceDecision = evidence.DecisionCannotVerify
	}
	emitGateEvent(*eventLog, *domain, *diff, false, nonEnforceDecision, findings, files)

	// Report-only / CI mode: non-blocking report with the canonical summary
	// line, always exit 0. If the backend was unavailable for every file and
	// nothing could be evaluated, report degraded rather than a false "0
	// findings".
	if *reportOnly {
		if unavailable && wouldBlock == 0 && warns == 0 {
			return reportDegraded(*domain, *diff, "AWG store/server unavailable — gate did not run")
		}
		printReportHeader(*domain, *diff, len(changes), warns, wouldBlock)
		printPolicyLine(policy)
		for _, fr := range findings {
			if fr.ScopeError != "" {
				fmt.Printf("  %s\n    [scope] %s\n", fr.File, fr.ScopeError)
				continue
			}
			fmt.Printf("  %s\n", fr.File)
			for _, w := range fr.Warnings {
				tag := "warn"
				if w.GetEnforcement() == "block" {
					tag = "WOULD-BLOCK"
				}
				fmt.Printf("    [%s] %s — %s (enforcement: %s)\n", tag, w.GetRuleId(), w.GetMessage(), w.GetEnforcement())
				if d := w.GetDetail(); d != "" {
					fmt.Printf("      %s\n", d)
				}
				if p := w.GetProvenance(); p != "" {
					fmt.Printf("      provenance: %s\n", p)
				}
			}
		}
		printCompleteness(compFindings, compNote)
		return finalReportLine(wouldBlock, "")
	}

	if *asJSON {
		out := map[string]interface{}{
			"diff": *diff, "domain": *domain, "dry_run": true,
			"would_block": wouldBlock, "warn": warns, "scope_errors": scopeErrs,
			"files": findings, "completeness": compFindings, "policy": policyJSON(policy),
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out)
		return 0
	}

	fmt.Printf("awg gate (DRY-RUN) — diff %s, %d file(s) with changes\n", *diff, len(changes))
	printPolicyLine(policy)
	fmt.Println()
	for _, fr := range findings {
		if fr.ScopeError != "" {
			fmt.Printf("  %s\n    [scope] %s\n", fr.File, fr.ScopeError)
			continue
		}
		fmt.Printf("  %s\n", fr.File)
		for _, w := range fr.Warnings {
			tag := "warn"
			if w.GetEnforcement() == "block" {
				tag = "WOULD-BLOCK"
			}
			fmt.Printf("    [%s] %s — %s\n", tag, w.GetRuleId(), w.GetMessage())
			if d := w.GetDetail(); d != "" {
				fmt.Printf("      %s\n", d)
			}
		}
	}

	printCompleteness(compFindings, compNote)

	fmt.Printf("\nSummary (dry-run): %d would-block, %d warn", wouldBlock, warns)
	if scopeErrs > 0 {
		fmt.Printf(", %d scope/backend error(s)", scopeErrs)
	}
	fmt.Println(" — exit 0 (nothing blocked, nothing edited).")
	if wouldBlock > 0 {
		fmt.Println("Note: under a hard gate these would-block findings would fail the merge. This is a dry run; they did not.")
	}
	return 0
}

// gitAddedLinesByFile runs `git diff --unified=0` over the range and returns, per
// changed file (repo-relative path), the concatenated added/changed lines (the
// blast-radius the design gates on — never pre-existing code). Pure deletions
// and /dev/null targets are skipped.
func gitAddedLinesByFile(repoRoot, diffRange string) (map[string]string, error) {
	gitArgs := []string{"-C", repoRoot, "diff", "--unified=0", "--no-color", "--no-ext-diff"}
	if strings.TrimSpace(diffRange) != "" {
		gitArgs = append(gitArgs, diffRange)
	}
	cmd := exec.Command("git", gitArgs...)
	var out, errBuf strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git diff %s: %v: %s", diffRange, err, strings.TrimSpace(errBuf.String()))
	}
	return parseAddedLinesFromDiff(out.String()), nil
}

// parseAddedLinesFromDiff extracts, per changed file (repo-relative path), the
// concatenated added/changed lines from `git diff --unified=0` output. Pure (no
// I/O) so it is exhaustively unit-testable. Pure deletions and /dev/null targets
// are skipped.
func parseAddedLinesFromDiff(diffText string) map[string]string {
	added := map[string][]string{}
	curFile := ""
	for _, line := range strings.Split(diffText, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ "):
			target := strings.TrimPrefix(line, "+++ ")
			if target == "/dev/null" {
				curFile = ""
				continue
			}
			curFile = strings.TrimPrefix(target, "b/")
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			if curFile != "" {
				added[curFile] = append(added[curFile], strings.TrimPrefix(line, "+"))
			}
		}
	}
	res := make(map[string]string, len(added))
	for f, lines := range added {
		res[f] = strings.Join(lines, "\n")
	}
	return res
}
