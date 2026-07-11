// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=cmd.awg.runtime_gate
// @awareness file_role=runtime_repair_gate_phase5

// Runtime proof lane — Phase 5: the CI/operator gate.
//
// `sensei runtime-gate --report <file>` reads a runtime verdict (from
// cluster-diagnose or runtime-repair-report) and FAILS CLOSED. Only a clean pass
// (valid_runtime_repair / cluster_converged) authorizes (exit 0). Every other
// verdict blocks (exit non-zero) — there is NO implicit green: an empty or
// unknown verdict blocks too. runtime_evidence_stale blocks UNLESS the operator
// explicitly opts in with `--allow external_stale_allowed`.
//
// gateDecision is pure (fixture-tested). The gate adds POLICY on top of the
// Phase-3/4 verdicts; it does not re-diagnose. (Named runtime-gate to avoid the
// existing `gate` (git-diff dry-run) and `repair-gate` (governed post-edit).)
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	gateAuthorize = "authorize"
	gateBlocked   = "blocked"
	gateWarn      = "warn"
)

// gateDecision maps a runtime verdict + an explicit allow-set to a gate decision.
// Fail-closed: only a clean pass authorizes; stale is warn-allowable by explicit
// opt-in; everything else (including empty/unknown) blocks.
func gateDecision(verdict string, allow map[string]bool) (decision, reason string) {
	switch verdict {
	case rrValid, vConverged:
		return gateAuthorize, "clean pass (" + verdict + ")"
	case vEvidenceStale:
		if allow["external_stale_allowed"] {
			return gateWarn, "runtime_evidence_stale tolerated by explicit --allow external_stale_allowed"
		}
		return gateBlocked, "runtime_evidence_stale and external_stale_allowed is not configured (stale evidence cannot authorize)"
	case "":
		return gateBlocked, "empty verdict — no implicit green"
	default:
		return gateBlocked, "verdict " + verdict + " is a blocking state"
	}
}

func exitCodeForGate(decision string) int {
	if decision == gateBlocked {
		return 1
	}
	return 0
}

// gateReport is the minimal shape the gate reads from a report file. yaml.Unmarshal
// parses both JSON and YAML, so it accepts cluster-diagnose or runtime-repair-report
// output (both carry a `verdict`).
type gateReport struct {
	Verdict string `yaml:"verdict" json:"verdict"`
	Subject string `yaml:"subject" json:"subject"`
}

func runRuntimeGate(args []string) int {
	fs := flag.NewFlagSet("sensei runtime-gate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	report := fs.String("report", "", "path to a runtime report (cluster-diagnose or runtime-repair-report output; yaml/json) (required)")
	allowCSV := fs.String("allow", "", "comma-separated explicitly-tolerated non-blocking states (e.g. external_stale_allowed)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei runtime-gate --report <file> [--allow external_stale_allowed,...]

Fail-closed CI/operator gate over a runtime verdict. Exit 0 ONLY for a clean pass
(valid_runtime_repair / cluster_converged) or an explicitly-allowed warn state.
Every other verdict — including empty/unknown — exits non-zero. No implicit green.
`)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *report == "" {
		fmt.Fprintln(os.Stderr, "sensei runtime-gate: --report <file> is required")
		return 2
	}
	raw, err := os.ReadFile(*report)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei runtime-gate: read %s: %v\n", *report, err)
		return 1 // cannot read the report → fail closed
	}
	var rep gateReport
	if err := yaml.Unmarshal(raw, &rep); err != nil {
		fmt.Fprintf(os.Stderr, "sensei runtime-gate: parse %s: %v\n", *report, err)
		return 1
	}
	allow := map[string]bool{}
	for _, a := range strings.Split(*allowCSV, ",") {
		if a = strings.TrimSpace(a); a != "" {
			allow[a] = true
		}
	}

	decision, reason := gateDecision(rep.Verdict, allow)
	allowed := make([]string, 0, len(allow))
	for a := range allow {
		allowed = append(allowed, a)
	}
	sort.Strings(allowed)

	subj := rep.Subject
	if subj == "" {
		subj = "(unknown subject)"
	}
	fmt.Printf("runtime-gate: %s\n", subj)
	fmt.Printf("  verdict:  %s\n", valueOrNone(rep.Verdict))
	if len(allowed) > 0 {
		fmt.Printf("  allow:    %s\n", strings.Join(allowed, ", "))
	}
	fmt.Printf("  DECISION: %s\n", strings.ToUpper(decision))
	fmt.Printf("  reason:   %s\n", reason)
	return exitCodeForGate(decision)
}

func valueOrNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
}
