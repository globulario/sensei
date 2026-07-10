// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check
// @awareness file_role=conformance_scanner_for_named_meta_principles
// @awareness implements=globular.platform:invariant.workflow.every_state_mutation_belongs_to_a_workflow_instance

// Command principle-check sweeps source code for candidate violations of a
// named meta-principle. It is the mechanical implementation of step 3 of the
// CLAUDE.md PRINCIPLE EXTRACTION PROTOCOL ("SEARCH FOR SIBLINGS").
//
// v0 scope (this prototype):
//
//   - One principle: meta.state_mutations_must_be_durably_committed_before_side_effects
//   - One pattern:   direct etcd writes (cli.Put / cli.Delete / kv.Put / kv.Delete)
//
// Each candidate site is classified into one of four buckets:
//
//	CONFORMANT  — the site is in a file the graph anchors to a workflow
//	              invariant or is inside a known workflow-step handler
//	              (heuristic: file lives in golang/workflow/ or under
//	              golang/<actor>/.../workflow_*.go).
//
//	EXCEPTION   — the site is in a file matching one of the six structural
//	              exceptions named in
//	              workflow.every_state_mutation_belongs_to_a_workflow_instance
//	              (heartbeat, observer-only self-state, leader election,
//	              service self-config, event-bus ephemera, bounded auto-heal).
//
//	DRIFT       — the site is in actor-writer territory, has none of the
//	              exception markers, and the file is not anchored to a
//	              workflow invariant in the graph. Candidate violation.
//
//	UNKNOWN     — file matches no positive classifier; insufficient signal.
//	              Caller should re-classify by hand.
//
// Exit code is 1 if any DRIFT sites are found; 0 otherwise.
//
// v1 ambitions (not in this prototype):
//
//   - Read the principle's `summary` from Oxigraph via SPARQL rather than
//     hardcoding patterns.
//   - Read `protects.files` from each related workflow invariant to
//     derive the CONFORMANT set automatically.
//   - Support multiple principles via a registry.
//   - JSON output mode for CI consumption.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	exitOK        = 0
	exitDrift     = 1
	exitUserError = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("principle-check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr,
			"usage: principle-check -principle <id> -repo <services-repo-path> [-mode summary|detail]\n\n"+
				"Sweeps Go source code for candidate violations of the named principle.\n"+
				"The -principle flag accepts either:\n"+
				"  - a meta-principle ID (e.g. meta.state_mutations_must_be_durably_committed_before_side_effects)\n"+
				"    — the loader finds a per-instance invariant linked via related_invariants\n"+
				"  - a per-instance invariant ID directly\n"+
				"The invariant must declare actor_writer_dirs + scan_pattern + the\n"+
				"file classification fields in docs/awareness/invariants.yaml.\n\n"+
				"Exit codes: 0 = no drift; 1 = drift or hidden-workflow found; 2 = user error.")
		fs.PrintDefaults()
	}
	principle := fs.String("principle", "", "meta-principle or per-instance invariant ID to check (required)")
	repo := fs.String("repo", "", "path to services repo root (required)")
	mode := fs.String("mode", "summary", "summary | detail")

	if err := fs.Parse(args); err != nil {
		return exitUserError
	}
	if *principle == "" || *repo == "" {
		fs.Usage()
		return exitUserError
	}
	if info, err := os.Stat(*repo); err != nil || !info.IsDir() {
		fmt.Fprintf(stderr, "principle-check: -repo: %s is not a directory\n", *repo)
		return exitUserError
	}

	loaded, err := loadPrinciple(*repo, *principle)
	if err != nil {
		fmt.Fprintf(stderr, "principle-check: %v\n", err)
		return exitUserError
	}
	if len(loaded.ActorWriterDirs) == 0 {
		fmt.Fprintf(stderr, "principle-check: %s has no actor_writer_dirs declared in the invariant YAML — refusing to sweep (would scan everything and report nothing meaningful)\n", *principle)
		return exitUserError
	}

	var sites []site
	siteNoun := "candidate sites"
	switch loaded.AnalysisMode {
	case "ruleguard":
		siteNoun = "ruleguard findings"
		// awareness-graph repo is assumed to be a sibling of services
		// under the same parent dir (e.g. .../globulario/services and
		// .../globulario/awareness-graph). If the layout differs, set
		// AWARENESS_GRAPH_ROOT to override.
		awarenessGraphRoot := filepath.Join(filepath.Dir(*repo), "awareness-graph")
		if env := os.Getenv("AWARENESS_GRAPH_ROOT"); env != "" {
			awarenessGraphRoot = env
		}
		rgSites, err := runRuleguard(stderr, *repo, awarenessGraphRoot, loaded.RuleguardRulesFile, loaded.ActorWriterDirs)
		if err != nil {
			fmt.Fprintf(stderr, "principle-check: %v\n", err)
			return exitUserError
		}
		sites = rgSites
	default:
		// "regex" or empty — original line-by-line sweep.
		sites = scan(*repo, loaded.ActorWriterDirs, loaded.ScanPattern)
		siteNoun = "candidate write sites"
	}

	classified := classify(sites, loaded)
	report(stdout, classified, *mode, *principle, loaded.AnalysisMode, siteNoun, len(loaded.ActorWriterDirs))

	for _, s := range classified {
		if s.bucket == bucketDrift || s.bucket == bucketHiddenWorkflow {
			return exitDrift
		}
	}
	return exitOK
}

// ── Scanning ────────────────────────────────────────────────────────────────

// The scan regex is now per-principle, loaded from YAML by loader.go.
// See the scan_pattern field on the per-instance invariant.

type site struct {
	path   string // repo-relative
	line   int
	column int
	text   string
	bucket bucket
	reason string
}

func scan(repoRoot string, actorScopes []string, scanRe *regexp.Regexp) []site {
	var sites []site
	for _, scope := range actorScopes {
		root := filepath.Join(repoRoot, scope)
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, openErr := os.Open(path)
			if openErr != nil {
				return nil
			}
			defer f.Close()
			rel, _ := filepath.Rel(repoRoot, path)
			scanner := bufio.NewScanner(f)
			scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
			lineNo := 0
			for scanner.Scan() {
				lineNo++
				line := scanner.Text()
				if loc := scanRe.FindStringIndex(line); loc != nil {
					sites = append(sites, site{
						path:   rel,
						line:   lineNo,
						column: loc[0],
						text:   strings.TrimSpace(line),
					})
				}
			}
			return nil
		})
	}
	return sites
}

// ── Classification ──────────────────────────────────────────────────────────

type bucket int

const (
	bucketUnknown bucket = iota
	bucketConformant
	bucketException
	bucketHiddenWorkflow
	bucketDrift
)

func (b bucket) String() string {
	switch b {
	case bucketConformant:
		return "CONFORMANT"
	case bucketException:
		return "EXCEPTION"
	case bucketHiddenWorkflow:
		return "HIDDEN_WORKFLOW"
	case bucketDrift:
		return "DRIFT"
	default:
		return "UNKNOWN"
	}
}

// classify assigns each scanned site to a bucket using the loaded
// principle metadata. Match order: HIDDEN_WORKFLOW > EXCEPTION >
// CONFORMANT > UNKNOWN (explicit helpers) > DRIFT (default).
func classify(sites []site, loaded *loadedPrinciple) []site {
	out := make([]site, len(sites))
	for i, s := range sites {
		out[i] = s
		// Hidden-workflow check first — explicit acknowledgement of
		// known multi-step orchestrations awaiting a lift.
		for _, p := range loaded.HiddenWorkflow {
			if p.re.MatchString(s.path) {
				out[i].bucket = bucketHiddenWorkflow
				out[i].reason = p.reason
				goto next
			}
		}
		// Exception check second.
		for _, p := range loaded.Exception {
			if p.re.MatchString(s.path) {
				out[i].bucket = bucketException
				out[i].reason = p.reason
				goto next
			}
		}
		// Conformant — file is documented as a workflow step handler
		// or workflow-bound orchestration.
		for _, p := range loaded.Conformant {
			if p.re.MatchString(s.path) {
				out[i].bucket = bucketConformant
				out[i].reason = p.reason
				goto next
			}
		}
		// UNKNOWN — generic helpers whose classification depends on
		// the caller, not the file itself.
		for _, p := range loaded.Unknown {
			if p.re.MatchString(s.path) {
				out[i].bucket = bucketUnknown
				out[i].reason = p.reason
				goto next
			}
		}
		// Default: DRIFT. Adding a new actor-writer file with state
		// mutations now requires naming it in the invariant YAML.
		out[i].bucket = bucketDrift
		out[i].reason = "no workflow-step anchor, no named exception, not an UNKNOWN helper — candidate violation"
	next:
	}
	return out
}

// ── Reporting ───────────────────────────────────────────────────────────────

func report(w io.Writer, sites []site, mode, principleID, analysisMode, siteNoun string, scopeCount int) {
	counts := map[bucket]int{}
	for _, s := range sites {
		counts[s.bucket]++
	}
	if siteNoun == "" {
		siteNoun = "candidate sites"
	}
	fmt.Fprintf(w, "principle-check: %s\n", principleID)
	fmt.Fprintf(w, "  analysis_mode=%s — scanned %d %s across %d actor-writer scope(s).\n\n", analysisMode, len(sites), siteNoun, scopeCount)

	fmt.Fprintf(w, "classification summary:\n")
	for _, b := range []bucket{bucketConformant, bucketException, bucketUnknown, bucketHiddenWorkflow, bucketDrift} {
		fmt.Fprintf(w, "  %-16s %d\n", b.String()+":", counts[b])
	}
	fmt.Fprintln(w)

	if mode == "summary" && counts[bucketDrift] == 0 && counts[bucketUnknown] == 0 && counts[bucketHiddenWorkflow] == 0 {
		fmt.Fprintf(w, "no drift, hidden workflows, or unknown candidates. Architecture conforms to %s for the scanned pattern.\n", principleID)
		return
	}

	if counts[bucketHiddenWorkflow] > 0 {
		fmt.Fprintf(w, "HIDDEN_WORKFLOW sites (%d) — must be lifted into workflow definitions:\n", counts[bucketHiddenWorkflow])
		for _, s := range sites {
			if s.bucket != bucketHiddenWorkflow {
				continue
			}
			fmt.Fprintf(w, "  %s:%d\n", s.path, s.line)
			fmt.Fprintf(w, "    %s\n", s.text)
			fmt.Fprintf(w, "    → %s\n", s.reason)
		}
		fmt.Fprintln(w)
	}

	// Detail: list all non-conformant non-exception sites.
	sort.SliceStable(sites, func(i, j int) bool {
		if sites[i].bucket != sites[j].bucket {
			return sites[i].bucket > sites[j].bucket // DRIFT > UNKNOWN > EXCEPTION > CONFORMANT
		}
		if sites[i].path != sites[j].path {
			return sites[i].path < sites[j].path
		}
		return sites[i].line < sites[j].line
	})

	if counts[bucketDrift] > 0 {
		fmt.Fprintf(w, "DRIFT candidates (%d):\n", counts[bucketDrift])
		for _, s := range sites {
			if s.bucket != bucketDrift {
				continue
			}
			fmt.Fprintf(w, "  %s:%d\n", s.path, s.line)
			fmt.Fprintf(w, "    %s\n", s.text)
			fmt.Fprintf(w, "    → %s\n", s.reason)
		}
		fmt.Fprintln(w)
	}

	if counts[bucketUnknown] > 0 {
		fmt.Fprintf(w, "UNKNOWN — needs caller-trace (%d):\n", counts[bucketUnknown])
		for _, s := range sites {
			if s.bucket != bucketUnknown {
				continue
			}
			fmt.Fprintf(w, "  %s:%d  %s\n", s.path, s.line, s.text)
		}
		fmt.Fprintln(w)
	}

	if mode == "detail" {
		if counts[bucketException] > 0 {
			fmt.Fprintf(w, "EXCEPTION sites (%d):\n", counts[bucketException])
			for _, s := range sites {
				if s.bucket != bucketException {
					continue
				}
				fmt.Fprintf(w, "  %s:%d  %s\n", s.path, s.line, s.reason)
			}
			fmt.Fprintln(w)
		}
		if counts[bucketConformant] > 0 {
			fmt.Fprintf(w, "CONFORMANT sites (%d):\n", counts[bucketConformant])
			for _, s := range sites {
				if s.bucket != bucketConformant {
					continue
				}
				fmt.Fprintf(w, "  %s:%d  %s\n", s.path, s.line, s.reason)
			}
		}
	}
}
