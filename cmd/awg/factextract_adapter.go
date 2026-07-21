// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/factextract"
)

// The architecture fact + authority-surface extraction now lives in the reusable
// golang/architecture/factextract package (shared with the Phase 7 result
// pipeline). These aliases and wrappers keep the CLI's existing call sites and
// the extract-invariants / extract-authority commands working as thin adapters.

type invariantExtractionReport = factextract.Report
type invariantExtractOptions = factextract.Options
type extractedInvariantCandidate = factextract.Candidate
type authoritySurfaceCandidate = factextract.AuthoritySurface
type invariantRepositoryIdentity = factextract.RepositoryIdentity
type invariantMutationAnalysisState = factextract.MutationAnalysisState
type normalizedInvariantFact = architecture.Fact

func buildInvariantExtractionReport(root string, opts invariantExtractOptions) (invariantExtractionReport, error) {
	return factextract.Extract(root, opts)
}

func resolveInvariantRepositoryIdentity(root string) invariantRepositoryIdentity {
	return factextract.ResolveRepositoryIdentity(root)
}

func renderInvariantExtractionReport(report invariantExtractionReport, format string) ([]byte, error) {
	return factextract.RenderReport(report, format)
}

func renderAuthorityCandidates(root string, cands []authoritySurfaceCandidate) ([]byte, error) {
	return factextract.RenderAuthorityCandidates(root, cands)
}

func runExtractInvariants(args []string) int {
	fs := flag.NewFlagSet("sensei extract-invariants", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	opts := invariantExtractOptions{}
	fs.StringVar(&opts.Repo, "repo", ".", "repository root to inspect")
	fs.StringVar(&opts.Format, "format", "json", "output format: json | yaml")
	fs.StringVar(&opts.Output, "output", "", "write extraction artifact to this path instead of stdout")
	fs.BoolVar(&opts.IncludeHistory, "include-history", false, "inspect recent git history for historical-removal facts")
	fs.BoolVar(&opts.IncludeDocs, "include-docs", true, "extract normative documentation/comment facts")
	fs.BoolVar(&opts.IncludeTests, "include-tests", true, "extract architectural test facts")
	fs.BoolVar(&opts.IncludeMutationAnalysis, "include-mutation-analysis", false, "prepare isolated mutation-analysis workspace (bounded mode placeholder)")
	fs.StringVar(&opts.MinimumConfidence, "minimum-confidence", "low", "minimum candidate confidence: low | medium | high | proven")
	fs.BoolVar(&opts.Explain, "explain", false, "include supporting facts and scoring explanations (always true for JSON/YAML)")
	fs.BoolVar(&opts.Check, "check", false, "compare --output with a fresh deterministic extraction")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei extract-invariants --repo <checkout> [flags]

Extract normalized facts and review-only invariant candidates from repository
evidence. The command never promotes candidates into governed invariants.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 2
	}
	if opts.Check && strings.TrimSpace(opts.Output) == "" {
		fmt.Fprintln(os.Stderr, "sensei extract-invariants: --check requires --output")
		return 2
	}
	root, err := filepath.Abs(opts.Repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei extract-invariants: resolve repo: %v\n", err)
		return 1
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "sensei extract-invariants: --repo must be an existing directory: %s\n", root)
		return 2
	}
	report, err := factextract.Extract(root, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei extract-invariants: %v\n", err)
		return 1
	}
	rendered, err := factextract.RenderReport(report, opts.Format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei extract-invariants: %v\n", err)
		return 2
	}
	if opts.Check {
		existing, err := os.ReadFile(opts.Output)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei extract-invariants: read --output: %v\n", err)
			return 1
		}
		if !bytes.Equal(bytes.TrimSpace(existing), bytes.TrimSpace(rendered)) {
			fmt.Fprintf(os.Stderr, "extract-invariants: STALE — %s differs from fresh extraction\n", opts.Output)
			return 1
		}
		fmt.Fprintf(os.Stderr, "extract-invariants: fresh (%d facts, %d candidates)\n", len(report.Facts), len(report.Candidates))
		return 0
	}
	if strings.TrimSpace(opts.Output) != "" {
		if err := os.MkdirAll(filepath.Dir(opts.Output), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "sensei extract-invariants: mkdir: %v\n", err)
			return 1
		}
		if err := os.WriteFile(opts.Output, rendered, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "sensei extract-invariants: write: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "extract-invariants: wrote %d fact(s), %d candidate(s) to %s\n", len(report.Facts), len(report.Candidates), opts.Output)
		return 0
	}
	fmt.Print(string(rendered))
	return 0
}

func runExtractAuthority(args []string) int {
	fs := flag.NewFlagSet("sensei extract-authority", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoRoot := fs.String("repo-root", ".", "repository root to scan")
	output := fs.String("output", "", "candidate YAML to write (default: <repo>/docs/awareness/candidates/authority_surface_candidates.yaml)")
	check := fs.Bool("check", false, "compare committed candidate YAML to a fresh run; exit 1 if stale")
	minConf := fs.String("minimum-confidence", "", "drop surfaces below this level: low|medium|high|proven (default: keep all). medium keeps routes/lifecycle/guarded-mutations; drops bare mutations.")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei extract-authority [flags]

Extract conservative AuthoritySurface candidates from Go source. The command
emits status:candidate YAML only; nothing is auto-promoted or imported as live
graph authority.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	root, err := filepath.Abs(*repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei extract-authority: resolve repo root: %v\n", err)
		return 1
	}
	cands, err := factextract.ExtractAuthorityCandidates(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei extract-authority: %v\n", err)
		return 1
	}
	cands = factextract.FilterAuthorityByMinConfidence(cands, *minConf)
	out, err := factextract.RenderAuthorityCandidates(root, cands)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei extract-authority: render: %v\n", err)
		return 1
	}

	target := *output
	if target == "" {
		target = filepath.Join(root, "docs", "awareness", "candidates", "authority_surface_candidates.yaml")
	}
	if *check {
		committed, _ := os.ReadFile(target)
		if !bytes.Equal(bytes.TrimSpace(committed), bytes.TrimSpace(out)) {
			fmt.Fprintf(os.Stderr, "extract-authority: STALE — %s differs from a fresh run; rerun to regenerate\n", target)
			return 1
		}
		fmt.Fprintf(os.Stderr, "extract-authority: fresh (%d candidates)\n", len(cands))
		return 0
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "sensei extract-authority: mkdir: %v\n", err)
		return 1
	}
	if err := os.WriteFile(target, out, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "sensei extract-authority: write: %v\n", err)
		return 1
	}

	kinds := map[string]int{}
	for _, c := range cands {
		kinds[c.Kind]++
	}
	fmt.Fprintf(os.Stderr, "extract-authority: wrote %d candidate(s) to %s\n", len(cands), target)
	for _, line := range factextract.AuthorityKindSummary(kinds) {
		fmt.Fprintf(os.Stderr, "  %s\n", line)
	}
	return 0
}
