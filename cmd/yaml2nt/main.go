// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=build.yaml2nt
// @awareness file_role=build_tool_awareness_compiler
// @awareness implements=globular.awareness_graph:intent.awareness.yaml2nt_produces_deterministic_output
// @awareness implements=globular.platform:intent.awareness.graph_is_compiled_context_not_authority

// Command yaml2nt converts awareness and intent YAML sources into a stream of
// N-Triples on stdout or into a file.
//
// Usage:
//
//	yaml2nt -input <awareness-dir>                            # writes N-Triples to stdout
//	yaml2nt -input <dir1> -input <dir2>                      # multiple awareness dirs
//	yaml2nt -input <awareness-dir> -intent <intent-dir>      # also imports intent files
//	yaml2nt -input <awareness-dir> -output a.nt              # writes to a.nt
//	yaml2nt -input <awareness-dir> -strict                   # fail if any YAML is not imported
//
// The command walks the directory (or directories) recursively. Every .yaml
// file is classified:
//
//	imported          — triples were emitted
//	ignored           — recognized non-authority pipeline/config file
//	known_unsupported — schema recognized; importer not yet implemented
//	unknown_schema    — YAML parsed; top-level key not recognized
//	invalid           — YAML parse or read failure
//
// A per-file import summary is always written to stderr. Use -strict to make
// the command fail (exit 1) if any file is not fully imported — useful in CI.
//
// Behaviour contract:
//
//   - The CLI validates emitted triples with extractor.ValidateNTriples before
//     writing them. If validation fails, nothing is written and the process
//     exits non-zero.
//
//   - Progress and summary output go to stderr so stdout stays a pure
//     N-Triples stream safe to pipe into a SPARQL loader:
//
//     yaml2nt -input ./docs/awareness | curl -X POST ...
//
//   - Exit codes:
//     0  success
//     1  runtime error (import / validation / write / strict failure)
//     2  user error (missing flag, bad path)
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/seedmeta"
)

// multiFlag is a flag.Value that accumulates repeated -flag occurrences into a slice.
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

const (
	exitOK        = 0
	exitRuntime   = 1
	exitUserError = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the testable core of yaml2nt.
// It validates all emitted N-Triples before writing — if validation fails,
// nothing is written and the process exits non-zero.
// Strict mode exits 1 when any YAML file is skipped.
//
// @awareness namespace=globular.awareness_graph
// @awareness component=build.yaml2nt
// @awareness implements=globular.awareness_graph:intent.awareness.yaml2nt_produces_deterministic_output
// @awareness enforces=globular.awareness_graph:invariant.awareness.rdf.ntriples_validated_before_write
// @awareness enforces=globular.awareness_graph:invariant.awareness.rdf.strict_import_must_surface_all_skips
// @awareness protects=globular.awareness_graph:failure_mode.awareness.rdf.unvalidated_ntriples_corrupt_store
// @awareness tested_by=cmd/yaml2nt/main_test.go:TestRunDeterministic
// @awareness risk=medium
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("yaml2nt", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr,
			"usage: yaml2nt -input <awareness-dir> [-input <dir2>] [-intent <intent-dir>] [-output <file.nt>] [-strict]\n\n"+
				"Converts awareness and intent YAML sources into N-Triples.\n"+
				"-input may be repeated to scan multiple awareness directories.\n"+
				"Each directory is walked recursively. Each .yaml file is classified as:\n"+
				"  imported          — triples were emitted\n"+
				"  ignored           — recognized non-authority pipeline/config file\n"+
				"  known_unsupported — schema recognized; importer not yet implemented\n"+
				"  unknown_schema    — YAML parsed; top-level key not recognized\n"+
				"  invalid           — read or YAML parse failure\n\n"+
				"With -strict, exit 1 if any file is skipped.\n\n")
		fs.PrintDefaults()
	}
	var inputs multiFlag
	fs.Var(&inputs, "input", "path to awareness YAML directory (repeatable); nodes stay in the home domain")
	var repoInputs multiFlag
	fs.Var(&repoInputs, "input-repo", "DIR=REPO: import an awareness directory tagging its untagged nodes to REPO (e.g. github.com/globulario/services), so the graph is filterable per repo (repeatable)")
	intent := fs.String("intent", "", "path to intent YAML directory (optional)")
	output := fs.String("output", "", "output file path; if empty, N-Triples go to stdout")
	strict := fs.Bool("strict", false, "fail if any YAML file is not imported")
	validateRefs := fs.Bool("validate-refs", false, "fail on dangling cite-without-define references for ForbiddenFix and Test anchors")
	validatePromotion := fs.Bool("validate-promotion", false, "fail on active/accepted intents or implementation patterns that miss the promotion bar (trigger, related link, must_follow, reference)")
	validateContradictions := fs.Bool("validate-contradictions", false, "fail on structural contradictions (superseded-but-active, authority owner conflict, ungated safety repair plan, duplicate active id)")
	allowedRefs := fs.String("allowed-dangling-refs", "", "baseline file of known dangling references (Class<tab>ID per line); validator fails only on NEW dangling references not listed here")
	dumpAllowedRefs := fs.String("dump-allowed-dangling-refs", "", "write the current set of dangling references to this file in baseline format, then exit; useful for seeding/refreshing the allowlist")
	var pathPrefixes multiFlag
	fs.Var(&pathPrefixes, "path-prefix", "strip this prefix from authoredIn paths (repeatable; longest match wins; makes seed deterministic across checkouts)")

	if err := fs.Parse(args); err != nil {
		return exitUserError
	}

	if len(inputs) == 0 && len(repoInputs) == 0 {
		fmt.Fprintln(stderr, "yaml2nt: -input or -input-repo is required")
		fs.Usage()
		return exitUserError
	}

	// Parse -input-repo DIR=REPO entries. REPO (a domain key like
	// github.com/globulario/services) never contains '=', so split on the LAST
	// '=' to tolerate a '=' in the directory path.
	type repoInput struct{ dir, repo string }
	var taggedInputs []repoInput
	for _, ri := range repoInputs {
		eq := strings.LastIndex(ri, "=")
		if eq <= 0 || eq == len(ri)-1 {
			fmt.Fprintf(stderr, "yaml2nt: -input-repo must be DIR=REPO, got %q\n", ri)
			return exitUserError
		}
		taggedInputs = append(taggedInputs, repoInput{dir: ri[:eq], repo: ri[eq+1:]})
	}

	var buf bytes.Buffer
	var combinedReport *extractor.ImportReport
	totalTriples := 0

	for _, inputDir := range inputs {
		info, err := os.Stat(inputDir)
		if err != nil {
			fmt.Fprintf(stderr, "yaml2nt: -input: %v\n", err)
			return exitUserError
		}
		if !info.IsDir() {
			fmt.Fprintf(stderr, "yaml2nt: -input: %s is not a directory\n", inputDir)
			return exitUserError
		}
		emitter, report, err := extractor.ImportAwarenessDirWithOpts(inputDir, &buf, extractor.ImportDirOptions{
			StripPathPrefixes:   pathPrefixes,
			SkipNestedGenerated: true,
		})
		if err != nil {
			fmt.Fprintf(stderr, "yaml2nt: import %s: %v\n", inputDir, err)
			return exitRuntime
		}
		totalTriples += emitter.Triples
		if combinedReport == nil {
			combinedReport = report
		} else {
			combinedReport.Files = append(combinedReport.Files, report.Files...)
		}
	}

	// Per-repo tagged inputs: untagged nodes are attributed to REPO (DefaultRepo),
	// making the combined graph filterable by which repo a node came from.
	for _, ti := range taggedInputs {
		info, err := os.Stat(ti.dir)
		if err != nil {
			fmt.Fprintf(stderr, "yaml2nt: -input-repo: %v\n", err)
			return exitUserError
		}
		if !info.IsDir() {
			fmt.Fprintf(stderr, "yaml2nt: -input-repo: %s is not a directory\n", ti.dir)
			return exitUserError
		}
		emitter, report, err := extractor.ImportAwarenessDirWithOpts(ti.dir, &buf, extractor.ImportDirOptions{
			StripPathPrefixes:   pathPrefixes,
			SkipNestedGenerated: true,
			DefaultRepo:         ti.repo,
		})
		if err != nil {
			fmt.Fprintf(stderr, "yaml2nt: import %s (repo %s): %v\n", ti.dir, ti.repo, err)
			return exitRuntime
		}
		totalTriples += emitter.Triples
		if combinedReport == nil {
			combinedReport = report
		} else {
			combinedReport.Files = append(combinedReport.Files, report.Files...)
		}
	}

	if *intent != "" {
		iInfo, err := os.Stat(*intent)
		if err != nil {
			fmt.Fprintf(stderr, "yaml2nt: -intent: %v\n", err)
			return exitUserError
		}
		if !iInfo.IsDir() {
			fmt.Fprintf(stderr, "yaml2nt: -intent: %s is not a directory\n", *intent)
			return exitUserError
		}
		iEmitter, iReport, err := extractor.ImportAwarenessDirWithOpts(*intent, &buf, extractor.ImportDirOptions{StripPathPrefixes: pathPrefixes})
		if err != nil {
			fmt.Fprintf(stderr, "yaml2nt: intent import: %v\n", err)
			return exitRuntime
		}
		combinedReport.Files = append(combinedReport.Files, iReport.Files...)
		totalTriples += iEmitter.Triples
	}

	report := combinedReport

	printReport(stderr, report)

	if *strict && (report.HasUnknown() || report.HasInvalid()) {
		// Strict mode rejects unregistered (unknown_schema) and unparseable
		// (invalid) files — these indicate silently-dropped awareness data.
		// known_unsupported files are explicitly classified, not silently skipped,
		// and are allowed under strict mode.
		var bad []extractor.FileReport
		for _, f := range report.Skipped() {
			if f.Status == extractor.StatusUnknownSchema || f.Status == extractor.StatusInvalid {
				bad = append(bad, f)
			}
		}
		fmt.Fprintf(stderr, "yaml2nt: strict mode: %d file(s) with unknown or invalid schema\n", len(bad))
		return exitRuntime
	}

	if errs := extractor.ValidateNTriples(bytes.NewReader(buf.Bytes())); len(errs) > 0 {
		const maxReported = 20
		for i, e := range errs {
			if i >= maxReported {
				fmt.Fprintf(stderr, "yaml2nt: ... %d more validation errors omitted\n", len(errs)-i)
				break
			}
			fmt.Fprintf(stderr, "yaml2nt: %s\n", e)
		}
		fmt.Fprintf(stderr, "yaml2nt: %d N-Triples validation errors — refusing to write invalid output\n", len(errs))
		return exitRuntime
	}

	// Cross-reference validation. A "dangling reference" is a typed
	// anchor (ForbiddenFix, Test) that some YAML cites but no schema
	// defines. Round-3 of the meta-principle audit found two such
	// references (hot_deploy_local_binary_as_break_glass +
	// bypass_cycle_with_direct_storage_write) sitting cited-but-undefined
	// for months. The graph happily emits a typed-stub node for the
	// citation, and briefing-by-file silently returns a label-less
	// anchor — there's no signal until an auditor reads the YAML by hand.
	//
	// Ratchet behaviour:
	//   -validate-refs              report+fail on any dangling reference
	//   -allowed-dangling-refs F    accept entries in F as known-debt;
	//                               fail only on NEW references not in F
	//   -dump-allowed-dangling-refs F
	//                               write current dangling-refs to F and
	//                               exit; used to seed/refresh the baseline
	//
	// -strict does NOT imply -validate-refs. -strict guards against
	// silently-dropped schema; -validate-refs guards against silently-
	// emitted dangling typed nodes. They catch orthogonal authoring
	// bugs and should be enabled together in CI but separately in
	// authoring workflows where one signal at a time is easier to read.
	if *validateRefs || *dumpAllowedRefs != "" {
		refErrs, err := extractor.ValidateReferences(bytes.NewReader(buf.Bytes()))
		if err != nil {
			fmt.Fprintf(stderr, "yaml2nt: reference validator: %v\n", err)
			return exitRuntime
		}
		if *dumpAllowedRefs != "" {
			f, err := os.Create(*dumpAllowedRefs)
			if err != nil {
				fmt.Fprintf(stderr, "yaml2nt: dump-allowed-dangling-refs: %v\n", err)
				return exitRuntime
			}
			if err := extractor.SerializeAllowedRefs(f, refErrs); err != nil {
				f.Close()
				fmt.Fprintf(stderr, "yaml2nt: dump-allowed-dangling-refs write: %v\n", err)
				return exitRuntime
			}
			if err := f.Close(); err != nil {
				fmt.Fprintf(stderr, "yaml2nt: dump-allowed-dangling-refs close: %v\n", err)
				return exitRuntime
			}
			fmt.Fprintf(stderr, "yaml2nt: wrote %d baseline entries to %s\n", len(refErrs), *dumpAllowedRefs)
			return exitOK
		}
		var newErrs []extractor.ReferenceError
		if *allowedRefs != "" {
			f, err := os.Open(*allowedRefs)
			if err != nil {
				fmt.Fprintf(stderr, "yaml2nt: open allowed-dangling-refs: %v\n", err)
				return exitRuntime
			}
			allowed, err := extractor.LoadAllowedRefs(f)
			f.Close()
			if err != nil {
				fmt.Fprintf(stderr, "yaml2nt: %v\n", err)
				return exitRuntime
			}
			var known []extractor.ReferenceError
			newErrs, known = extractor.FilterAllowed(refErrs, allowed)
			fmt.Fprintf(stderr, "yaml2nt: %d dangling reference(s) accepted via baseline; %d need attention\n", len(known), len(newErrs))
			// Warn (don't fail) if the baseline contains stale entries —
			// references that were dangling at baseline-creation time
			// but are no longer present (definition was added, or the
			// cite was removed). Stale entries should be pruned so the
			// baseline tracks reality.
			if extra := staleBaselineEntries(refErrs, allowed); len(extra) > 0 {
				fmt.Fprintf(stderr, "yaml2nt: warning: %d baseline entries are no longer dangling (definition was added or cite removed) — prune them from %s\n", len(extra), *allowedRefs)
			}
		} else {
			newErrs = refErrs
		}
		if len(newErrs) > 0 {
			const maxReported = 50
			for i, e := range newErrs {
				if i >= maxReported {
					fmt.Fprintf(stderr, "yaml2nt: ... %d more dangling references omitted\n", len(newErrs)-i)
					break
				}
				fmt.Fprintf(stderr, "yaml2nt: %s\n", e)
			}
			fmt.Fprintf(stderr, "yaml2nt: %d new dangling reference(s) — every cited anchor must have a matching definition\n", len(newErrs))
			return exitRuntime
		}
		recon, err := extractor.ValidateTestReconciliation(bytes.NewReader(buf.Bytes()))
		if err != nil {
			fmt.Fprintf(stderr, "yaml2nt: test reconciliation validator: %v\n", err)
			return exitRuntime
		}
		if len(recon.AuthoritativeMissingImplementation) > 0 {
			fmt.Fprintf(stderr, "yaml2nt: warning: %d required_tests.yaml Go test(s) have no discovered _test.go implementation\n", len(recon.AuthoritativeMissingImplementation))
			for i, id := range recon.AuthoritativeMissingImplementation {
				if i >= 10 {
					fmt.Fprintf(stderr, "yaml2nt: ... %d more required tests missing implementation omitted\n", len(recon.AuthoritativeMissingImplementation)-i)
					break
				}
				fmt.Fprintf(stderr, "yaml2nt: missing discovered Go test for required test: %s\n", id)
			}
		}
		if len(recon.ReferencedDiscoveredMissingSpec) > 0 {
			fmt.Fprintf(stderr, "yaml2nt: warning: %d discovered Go test(s) referenced by code annotations are not declared in required_tests.yaml\n", len(recon.ReferencedDiscoveredMissingSpec))
			for i, id := range recon.ReferencedDiscoveredMissingSpec {
				if i >= 10 {
					fmt.Fprintf(stderr, "yaml2nt: ... %d more discovered tests missing required_tests.yaml omitted\n", len(recon.ReferencedDiscoveredMissingSpec)-i)
					break
				}
				fmt.Fprintf(stderr, "yaml2nt: missing required_tests.yaml definition for discovered Go test: %s\n", id)
			}
		}
	}

	// Promotion gate. Orthogonal to -strict and -validate-refs: it guards
	// against a candidate silently reaching `active`/`accepted` without the
	// structure that makes it useful (an activation trigger to retrieve it, a
	// related node to ground it, must-follow guidance for a pattern). Opt-in,
	// like -validate-refs — enable in CI; in authoring, run alone for a clean
	// one-signal read.
	if *validatePromotion {
		dirs := append([]string{}, inputs...)
		if *intent != "" {
			dirs = append(dirs, *intent)
		}
		vios, err := extractor.ValidatePromotions(dirs...)
		if err != nil {
			fmt.Fprintf(stderr, "yaml2nt: promotion validator: %v\n", err)
			return exitRuntime
		}
		if len(vios) > 0 {
			const maxReported = 50
			for i, v := range vios {
				if i >= maxReported {
					fmt.Fprintf(stderr, "yaml2nt: ... %d more promotion violations omitted\n", len(vios)-i)
					break
				}
				fmt.Fprintf(stderr, "yaml2nt: %s\n", v)
			}
			fmt.Fprintf(stderr, "yaml2nt: %d promotion violation(s) — an active node must meet the promotion bar\n", len(vios))
			return exitRuntime
		}
	}

	// Contradiction gate (Phase 2E). Detects stale architecture poisoning agent
	// guidance: superseded-but-active nodes, authority owner conflicts, ungated
	// safety repair plans, duplicate active ids. Opt-in.
	if *validateContradictions {
		dirs := append([]string{}, inputs...)
		if *intent != "" {
			dirs = append(dirs, *intent)
		}
		cons, err := extractor.ValidateContradictions(dirs...)
		if err != nil {
			fmt.Fprintf(stderr, "yaml2nt: contradiction validator: %v\n", err)
			return exitRuntime
		}
		if len(cons) > 0 {
			for _, c := range cons {
				fmt.Fprintf(stderr, "yaml2nt: %s\n", c)
			}
			fmt.Fprintf(stderr, "yaml2nt: %d contradiction(s) — stale architecture must not coexist with new truth unrelated\n", len(cons))
			return exitRuntime
		}
	}

	// Dedup before write. Several extractors can legitimately emit the same
	// triple from different sources (e.g. a code symbol's `enforces` edge
	// declared in both its own annotation block and the invariant's
	// authoredIn YAML). Without this, the seed bloats by ~15% and the
	// "triples emitted" log undercounts as lines, not as unique triples —
	// a violation of meta.diagnostic_output_must_be_bounded for build logs.
	deduped, uniqueCount, dupCount := extractor.DedupNTriples(buf.Bytes())
	stamped, _ := seedmeta.AppendMarker(deduped)

	sink := stdout
	if *output != "" {
		f, err := os.Create(*output)
		if err != nil {
			fmt.Fprintf(stderr, "yaml2nt: create output: %v\n", err)
			return exitRuntime
		}
		defer f.Close()
		sink = f
	}
	if _, err := sink.Write(stamped); err != nil {
		fmt.Fprintf(stderr, "yaml2nt: write: %v\n", err)
		return exitRuntime
	}

	if dupCount > 0 {
		fmt.Fprintf(stderr, "yaml2nt: %d unique triples emitted (%d bytes); %d duplicates suppressed (raw=%d)\n",
			uniqueCount+5, len(stamped), dupCount, totalTriples)
	} else {
		fmt.Fprintf(stderr, "yaml2nt: %d unique triples emitted (%d bytes)\n", uniqueCount+5, len(stamped))
	}
	if *output != "" {
		fmt.Fprintf(stderr, "yaml2nt: wrote %s\n", *output)
	}
	return exitOK
}

// printReport writes the import summary to w. Imported files are grouped by
// schema; skipped files are listed individually.
func printReport(w io.Writer, r *extractor.ImportReport) {
	imported := r.Imported()
	ignored := r.Ignored()
	skipped := r.Skipped()

	fmt.Fprintln(w, "yaml2nt: import summary:")

	if len(imported) > 0 {
		bySchema := map[string]struct{ files, triples int }{}
		for _, f := range imported {
			e := bySchema[f.Schema]
			e.files++
			e.triples += f.Count
			bySchema[f.Schema] = e
		}
		schemas := make([]string, 0, len(bySchema))
		for s := range bySchema {
			schemas = append(schemas, s)
		}
		sort.Strings(schemas)
		for _, s := range schemas {
			e := bySchema[s]
			fmt.Fprintf(w, "yaml2nt:   imported  %-24s %d file(s), %d triples\n", s+":", e.files, e.triples)
		}
	}

	if len(skipped) > 0 {
		fmt.Fprintln(w, "yaml2nt:   skipped:")
		for _, f := range skipped {
			fmt.Fprintf(w, "yaml2nt:     [%-18s] %s\n", string(f.Status), f.Path)
			if f.Reason != "" {
				fmt.Fprintf(w, "yaml2nt:       reason: %s\n", f.Reason)
			}
		}
	}
	if len(ignored) > 0 {
		fmt.Fprintln(w, "yaml2nt:   ignored non-authority:")
		for _, f := range ignored {
			fmt.Fprintf(w, "yaml2nt:     [%-18s] %s\n", string(f.Status), f.Path)
			if f.Reason != "" {
				fmt.Fprintf(w, "yaml2nt:       reason: %s\n", f.Reason)
			}
		}
	}
}

// staleBaselineEntries returns baseline entries whose Class\tID key is
// not present in the current dangling-reference set. Operators see
// these as "no longer dangling — please prune from baseline" so the
// allowlist tracks current reality instead of accumulating ghosts.
func staleBaselineEntries(current []extractor.ReferenceError, allowed map[string]bool) []string {
	have := make(map[string]bool, len(current))
	for _, e := range current {
		have[e.Class+"\t"+e.ID] = true
	}
	var stale []string
	for key := range allowed {
		if !have[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	return stale
}
