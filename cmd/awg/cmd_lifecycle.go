// SPDX-License-Identifier: Apache-2.0

package main

// cmd_lifecycle.go — `awg lifecycle`: the governed-memory state surface.
//
// One consolidated readout of WHERE the project's learned knowledge sits in the
// scar lifecycle, so the system can SEE its own enforcement gaps instead of
// pretending everything is "learned":
//
//   observation -> candidate -> reviewed -> authored -> generated -> embedded
//                -> validated -> enforced -> protected
//
// A rule is only "learned" once it reaches `validated`; only "enforced" once a
// mechanical gate fails when it is violated; only "protected" once branch
// protection prevents a bypassing merge. This command reports each state with its
// evidence source and an honest label (local / runtime / external), and breaks
// the meta.* principles down by enforcement tier (the #8 acceptance: list which
// principles are hard-enforced, advisory, review-only, or unenforced).
//
// It re-derives nothing: enforcement tiers come from meta_principle_coverage.yaml
// (ratchet-enforced by TestMetaPrincipleCoverage); artifact coherence comes from
// `awg audit` (run with --audit). It refuses false certainty — states it cannot
// determine offline (observed/candidate/reviewed = behavioral runtime;
// protected = branch protection) are labelled as such, never claimed.

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

type coveragePrinciple struct {
	Principle    string `yaml:"principle"`
	Tier         string `yaml:"tier"`
	IntendedTier string `yaml:"intended_tier"`
}

type coverageFile struct {
	EnforcementRatchet struct {
		MaxReviewOnly int `yaml:"max_review_only"`
	} `yaml:"enforcement_ratchet"`
	Coverage []coveragePrinciple `yaml:"meta_principle_coverage"`
}

// tierTally is the per-tier classification of the residual coverage registry.
type tierTally struct {
	Behavioral  []string
	Declaration []string
	Planned     []string // each entry includes "  (intended: <tier>)"
	ReviewOnly  []string
	Other       []string // unexpected tier — a coverage defect
	Conflicts   []string // a principle listed more than once (directive #7)
}

// tallyCoverageTiers classifies the listed meta-principle entries by tier.
//
// Only `code_scanner` is auto-derived-and-not-listed; every other residual tier
// — including `declaration` (the principle is AWG-owned even though its
// validating artifact, architectural_declarations.yaml, lives in the services
// repo) — IS listed in meta_principle_coverage.yaml and counted here. A tier the
// switch does not recognise lands in Other (UNCLASSIFIED), and a principle listed
// more than once lands in Conflicts. Pure (no I/O) so the classification is
// unit-tested; this is the function whose missing `declaration` case made
// `awg lifecycle` mis-report 7 correctly-tiered principles as UNCLASSIFIED.
func tallyCoverageTiers(entries []coveragePrinciple) tierTally {
	var t tierTally
	tiersByID := map[string][]string{}
	seen := map[string]bool{}
	for _, p := range entries {
		id := p.Principle
		tiersByID[id] = append(tiersByID[id], p.Tier)
		if seen[id] {
			continue // count each principle once; duplicates reported separately
		}
		seen[id] = true
		switch p.Tier {
		case "behavioral":
			t.Behavioral = append(t.Behavioral, id)
		case "declaration":
			t.Declaration = append(t.Declaration, id)
		case "planned":
			t.Planned = append(t.Planned, id+"  (intended: "+p.IntendedTier+")")
		case "review_only":
			t.ReviewOnly = append(t.ReviewOnly, id)
		default:
			t.Other = append(t.Other, id+"  (tier="+p.Tier+")")
		}
	}
	for id, tiers := range tiersByID {
		if len(tiers) > 1 {
			sort.Strings(tiers)
			t.Conflicts = append(t.Conflicts, fmt.Sprintf("%s  (%d entries; tiers: %v)", id, len(tiers), tiers))
		}
	}
	sort.Strings(t.Behavioral)
	sort.Strings(t.Declaration)
	sort.Strings(t.Planned)
	sort.Strings(t.ReviewOnly)
	sort.Strings(t.Other)
	sort.Strings(t.Conflicts)
	return t
}

func runLifecycle(args []string) int {
	fs := flag.NewFlagSet("awg lifecycle", flag.ContinueOnError)
	svcRepo := fs.String("services-repo", "", "path to services repo (default: ../services next to ag-repo)")
	agRepo := fs.String("ag-repo", "", "path to awareness-graph repo (default: current dir)")
	withAudit := fs.Bool("audit", false, "also run `awg audit -check` for live artifact-coherence states")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ag := *agRepo
	if ag == "" {
		ag = "."
	}
	svc := *svcRepo
	if svc == "" {
		if c, err := filepath.Abs(filepath.Join(ag, "..", "services")); err == nil {
			svc = c
		}
	}

	covPath := filepath.Join(svc, "docs", "awareness", "meta_principle_coverage.yaml")
	raw, err := os.ReadFile(covPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg lifecycle: cannot read %s: %v\n  pass --services-repo <path>\n", covPath, err)
		return 1
	}
	var cov coverageFile
	if err := yaml.Unmarshal(raw, &cov); err != nil {
		fmt.Fprintf(os.Stderr, "awg lifecycle: parse %s: %v\n", covPath, err)
		return 1
	}

	// Classify the listed tiers (pure + unit-tested). code_scanner is the only
	// auto-derived tier not listed here; declaration IS listed and counted.
	t := tallyCoverageTiers(cov.Coverage)
	behavioral, declaration, planned, reviewOnly, other, conflicts :=
		t.Behavioral, t.Declaration, t.Planned, t.ReviewOnly, t.Other, t.Conflicts

	fmt.Println("awg lifecycle — governed-memory state")
	fmt.Println("=====================================")

	fmt.Println("\nENFORCEMENT TIERS  (meta.* principles; meta_principle_coverage.yaml)")
	fmt.Println("  code_scanner   auto-derived  ENFORCED  (ruleguard/regex in principle-check-all)")
	fmt.Printf("  declaration    %3d           ENFORCED  (listed; validated via architectural_declarations.yaml)\n", len(declaration))
	fmt.Printf("  behavioral     %3d           ENFORCED  (gated by runtime/integration tests)\n", len(behavioral))
	fmt.Printf("  planned        %3d           ADVISORY  (mechanizable, NOT yet built — tracked gaps)\n", len(planned))
	fmt.Printf("  review_only    %3d / %-3d     REVIEW    (terminal human design philosophy; ceiling)\n", len(reviewOnly), cov.EnforcementRatchet.MaxReviewOnly)
	if len(other) > 0 {
		fmt.Printf("  UNCLASSIFIED   %3d           CHECK     (unexpected tier — investigate)\n", len(other))
	}

	if len(planned) > 0 {
		fmt.Println("\nADVISORY (planned — unenforced until the named mechanism is built):")
		for _, p := range planned {
			fmt.Println("  - " + p)
		}
	}
	if len(other) > 0 {
		fmt.Println("\nUNCLASSIFIED tiers (should not exist — fix coverage):")
		for _, p := range other {
			fmt.Println("  - " + p)
		}
	}
	if len(conflicts) > 0 {
		fmt.Printf("\nDUPLICATE / CONFLICTING principles (%d — self-coherence defect, directive #7):\n", len(conflicts))
		for _, c := range conflicts {
			fmt.Println("  ! " + c)
		}
		fmt.Println("  → a principle listed twice (esp. with different tiers) is ambiguous about its")
		fmt.Println("    own enforcement. De-duplicate in meta_principle_coverage.yaml.")
	}

	fmt.Println("\nLIFECYCLE STATES  (must reach 'validated' to be learned; 'enforced' needs a gate; 'protected' needs branch protection)")
	rows := [][3]string{
		{"observed", "runtime", "ai-memory / behavioral signals (memory_query, behavioral_record_signal)"},
		{"candidate", "runtime", "behavioral promotion queue (behavioral_list_promotion_candidates)"},
		{"reviewed", "runtime", "behavioral_promote_principle (review-gated; not auto)"},
		{"authored", "local", "docs/awareness/*.yaml (the rule exists in source)"},
		{"generated", "local", "awg rebuild / awg learn (deterministic regen)"},
		{"embedded", "local", "golang/server/embeddata/awareness.nt (committed)"},
		{"validated", "CI hard gate", "awg validate (dangling refs, dup ids, missing sources)"},
		{"enforced", "CI hard gate", "principle-check-all + behavioral ratchets + awg audit freshness"},
		{"protected", "external", "branch protection — verify: gh api repos/<owner>/<repo>/branches/master/protection"},
	}
	for _, r := range rows {
		fmt.Printf("  %-10s [%-12s]  %s\n", r[0], r[1], r[2])
	}

	fmt.Println("\nHONEST GAPS")
	fmt.Printf("  - %d principles are ADVISORY (planned) — enforced only by review until mechanized.\n", len(planned))
	fmt.Printf("  - %d principles are REVIEW_ONLY (terminal) — no artifact can prove them.\n", len(reviewOnly))
	fmt.Println("  - 'protected' is per-repo and external: services/master is settable; private repos on")
	fmt.Println("    GitHub free tier cannot use branch protection (Pro or public required) — those gates")
	fmt.Println("    are enforced-in-CI but BYPASSABLE until protected. Treat that as an enforcement gap.")

	if *withAudit {
		fmt.Println("\nARTIFACT COHERENCE  (awg audit -check)")
		fmt.Println("---------------------------------------")
		auditArgs := []string{"-check"}
		if *svcRepo != "" {
			auditArgs = append(auditArgs, "-services-repo", *svcRepo)
		}
		if *agRepo != "" {
			auditArgs = append(auditArgs, "-ag-repo", *agRepo)
		}
		if rc := runAudit(auditArgs); rc != 0 {
			fmt.Fprintln(os.Stderr, "\nawg lifecycle: artifact coherence FAILED — generated/embedded artifacts drift from source.")
			return rc
		}
	}
	return 0
}
