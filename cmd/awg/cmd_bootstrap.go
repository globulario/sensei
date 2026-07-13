// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/extractor/grpcwebscan"
	"github.com/globulario/sensei/golang/extractor/importgraph"
	"github.com/globulario/sensei/golang/extractor/openapiscan"
	"github.com/globulario/sensei/golang/extractor/protoscan"
	"github.com/globulario/sensei/golang/extractor/webcompscan"
	"github.com/globulario/sensei/golang/scanner"
	"github.com/globulario/sensei/golang/statedir"
)

// runBootstrap is the PRODUCTION repo-initialization path: it scaffolds AWG if
// missing, runs deterministic architecture extraction (proto/contracts,
// components, code symbols, tests) into docs/awareness/generated/, optionally
// mines history signals into docs/awareness/candidates/ (never auto-promoted),
// then runs the validate/build gates and prints a report.
//
// This is NOT cold-bootstrap. cold-bootstrap is a history-signal miner;
// `sensei bootstrap` initializes architectural awareness for an existing repo and
// may call coldsource only as an optional candidate stage.
func runBootstrap(args []string) int {
	fs := flag.NewFlagSet("sensei bootstrap", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repo := "."
	fs.StringVar(&repo, "path", ".", "path to the repository to bootstrap")
	fs.StringVar(&repo, "repo", ".", "deprecated alias for --path")
	skipHistory := fs.Bool("skip-history", false, "do not run coldsource/history mining")
	skipBuild := fs.Bool("skip-build", false, "run extraction + validate, but do not build the graph")
	check := fs.Bool("check", false, "compare generated output to committed files; exit non-zero if stale")
	dryRun := fs.Bool("dry-run", false, "print the report without writing generated/candidate files")
	scipPath := fs.String("scip", "", "path to a SCIP index to ingest symbol-level nodes; defaults to <repo>/index.scip when present")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei bootstrap --path <checkout> [flags]

Initialize AWG for an existing repository: scaffold if missing, run deterministic
architecture extraction (proto contracts, components, code symbols, tests) into
docs/awareness/generated/, optionally mine history candidates into
docs/awareness/candidates/ (never auto-promoted), then validate and build.

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

	warnDeprecatedRepoPathAlias(fs, "bootstrap")
	warnIfDomainLikeExtractorPath("bootstrap", repo)

	root, err := filepath.Abs(repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei bootstrap: %v\n", err)
		return 1
	}
	awarenessDir := filepath.Join(root, "docs", "awareness")
	generatedDir := filepath.Join(awarenessDir, "generated")
	candidatesDir := filepath.Join(awarenessDir, "candidates")
	rep := &bootstrapReport{root: root, dryRun: *dryRun, check: *check}

	// ── Stage 1: scaffold if missing ──
	if _, statErr := os.Stat(awarenessDir); os.IsNotExist(statErr) {
		if *dryRun || *check {
			rep.notes = append(rep.notes, "scaffold: docs/awareness/ missing — would run `sensei init` scaffold (skipped in dry-run/check)")
		} else {
			created, serr := scaffoldProject(root, initOptions{hooks: true, claudeMD: true, agentsMD: true, cursor: true})
			if serr != nil {
				fmt.Fprintf(os.Stderr, "sensei bootstrap: scaffold: %v\n", serr)
				return 1
			}
			rep.scaffolded = created
		}
	}
	if !*dryRun && !*check {
		refreshed, rerr := repairLegacyStarterTemplates(root)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "sensei bootstrap: repair starter templates: %v\n", rerr)
			return 1
		}
		for _, path := range refreshed {
			rep.notes = append(rep.notes, "refreshed legacy starter template: "+relTo(root, path))
		}
	}

	// ── Stage 2–3: deterministic extractors → generated/ ──
	var generated []genFile

	// Proto / contracts.
	var protoContracts []protoscan.Contract
	protoFiles, _ := protoscan.FindProtoFiles(root)
	if len(protoFiles) > 0 {
		var doc protoscan.Doc
		for _, pf := range protoFiles {
			cs, perr := protoscan.ScanProto(pf, root, nil)
			if perr != nil {
				rep.notes = append(rep.notes, "proto: "+filepath.Base(pf)+": "+perr.Error())
				continue
			}
			doc.Contracts = append(doc.Contracts, cs...)
		}
		for _, c := range doc.Contracts {
			rep.contracts++
			if c.Uml != nil && c.Uml.Kind == "Operation" {
				rep.operations++
			}
		}
		protoContracts = doc.Contracts
		if data, rerr := protoscan.Render(doc); rerr == nil {
			generated = append(generated, genFile{filepath.Join(generatedDir, "contracts.yaml"), data})
		}
	} else {
		rep.notes = append(rep.notes, "contracts: no .proto files found")
	}

	// REST / OpenAPI contracts (spec-file driven; one Interface per spec + one
	// Operation per path×method). Same contracts: schema as proto. Absent spec →
	// nothing written.
	if specs, _ := openapiscan.FindSpecFiles(root); len(specs) > 0 {
		var doc openapiscan.Doc
		for _, sf := range specs {
			cs, serr := openapiscan.ScanSpec(sf, root)
			if serr != nil {
				rep.notes = append(rep.notes, "openapi: "+filepath.Base(sf)+": "+serr.Error())
				continue
			}
			doc.Contracts = append(doc.Contracts, cs...)
		}
		for _, c := range doc.Contracts {
			rep.contracts++
			if c.Uml != nil && c.Uml.Kind == "Operation" {
				rep.operations++
			}
		}
		if data, rerr := openapiscan.Render(doc); rerr == nil {
			generated = append(generated, genFile{filepath.Join(generatedDir, "rest_contracts.yaml"), data})
		}
	}

	// Web components (native custom elements: customElements.define / Lit
	// @customElement). Same components: schema. Absent → nothing written.
	if wfiles, _ := webcompscan.FindSourceFiles(root); len(wfiles) > 0 {
		var all []webcompscan.Component
		for _, wf := range wfiles {
			cs, werr := webcompscan.ScanFile(wf, root)
			if werr != nil {
				continue
			}
			all = append(all, cs...)
		}
		if doc := (webcompscan.Doc{Components: webcompscan.Dedupe(all)}); len(doc.Components) > 0 {
			rep.webComponents = len(doc.Components)
			if data, rerr := webcompscan.Render(doc); rerr == nil {
				generated = append(generated, genFile{filepath.Join(generatedDir, "web_components.yaml"), data})
			}
		}
	}

	// gRPC-web contract consumption (consumed_by edges from a consuming
	// component to the backend Contract proto-scan defines, linked by id).
	// Observable client usage only; absent → nothing written.
	if gfiles, _ := grpcwebscan.FindSourceFiles(root); len(gfiles) > 0 {
		var usages []grpcwebscan.Usage
		for _, gf := range gfiles {
			us, gerr := grpcwebscan.ScanFile(gf, root)
			if gerr != nil {
				continue
			}
			usages = append(usages, us...)
		}
		if doc := (grpcwebscan.Doc{Contracts: grpcwebscan.Aggregate(usages)}); len(doc.Contracts) > 0 {
			rep.contractConsumptions = len(doc.Contracts)
			if data, rerr := grpcwebscan.Render(doc); rerr == nil {
				generated = append(generated, genFile{filepath.Join(generatedDir, "contract_consumption.yaml"), data})
			}
		}
	}

	// Components (deterministic structural extraction).
	comps := extractComponents(root)

	// Import graph → component dependency edges. Generic core; an optional
	// language-neutral classifier (docs/awareness/import_classifiers.yaml)
	// upgrades raw imports into semantic edges. Absent config → pure internal
	// dependsOn. Bootstrap merges those inferred edges into generated
	// components.yaml so the same component nodes are not declared twice.
	var importCfg importgraph.Config
	if cfgPath := filepath.Join(root, "docs", "awareness", "import_classifiers.yaml"); fileExists(cfgPath) {
		if c, cerr := importgraph.LoadConfig(cfgPath); cerr != nil {
			rep.notes = append(rep.notes, "import_classifiers: "+cerr.Error())
		} else {
			importCfg = c
		}
	}
	var igComponents []importgraph.Component // kept for boundary inference below
	for _, lang := range importgraph.Languages() {
		idoc, ierr := importgraph.Scan(root, lang, importCfg)
		if ierr != nil {
			rep.notes = append(rep.notes, "import_graph("+lang+"): "+ierr.Error())
			continue
		}
		for _, c := range idoc.Components {
			rep.importEdges += len(c.DependsOn)
			rep.importEdgesClassified += len(c.ReadsFrom) + len(c.WritesTo) + len(c.ExposesContracts)
		}
		igComponents = append(igComponents, idoc.Components...)
		comps = mergeImportGraphComponents(comps, idoc.Components)
	}
	rep.components = len(comps)
	if data, rerr := renderGenerated("Components inferred from repository layout and source imports (assertion: inferred).", componentsDoc{Components: comps}); rerr == nil {
		generated = append(generated, genFile{filepath.Join(generatedDir, "components.yaml"), data})
	}

	// Code symbols / annotations (only when a namespaces.yaml registry exists).
	if reg := findRegistry(root); reg != "" {
		syms, serr := extractCodeSymbols(root, reg)
		if serr != nil {
			rep.notes = append(rep.notes, "source_symbols: scan failed: "+serr.Error())
		} else {
			rep.sourceAnchors = len(syms.symbols)
			generated = append(generated, genFile{filepath.Join(generatedDir, "source_symbols.yaml"), syms.symbolsYAML})
			generated = append(generated, genFile{filepath.Join(generatedDir, "source_edges.yaml"), syms.edgesYAML})
		}
	} else {
		rep.notes = append(rep.notes, "source_symbols: skipped — no docs/awareness/namespaces.yaml registry (code-symbol extraction needs one)")
	}

	// SCIP symbol ingestion: when a SCIP index is present, map its symbols and
	// references into the graph (function/method nodes + aw:references edges).
	// This is language-agnostic and does not need an @awareness annotation, so
	// it fills the symbol layer the annotation scanner leaves empty. Explicit
	// --scip wins; otherwise auto-detect <repo>/index.scip.
	scipIndex := *scipPath
	if scipIndex == "" {
		if def := filepath.Join(root, "index.scip"); fileExists(def) {
			scipIndex = def
		}
	}
	if scipIndex != "" {
		if symbolsYAML, refsYAML, res, nDocs, serr := ingestScipFile(scipIndex, "", true); serr != nil {
			rep.notes = append(rep.notes, "scip: ingest failed: "+serr.Error())
		} else {
			generated = append(generated,
				genFile{filepath.Join(generatedDir, "scip_symbols.yaml"), symbolsYAML},
				genFile{filepath.Join(generatedDir, "scip_references.yaml"), refsYAML})
			rep.notes = append(rep.notes, fmt.Sprintf("scip: %d symbols, %d references from %d document(s) (%s)",
				len(res.Symbols), len(res.Refs), nDocs, filepath.Base(scipIndex)))
		}
	}

	// Tests.
	tests := extractTests(root)
	rep.tests = len(tests)
	if data, rerr := renderGenerated("Tests inferred from test files.", testsDoc{RequiredTests: tests}); rerr == nil {
		generated = append(generated, genFile{filepath.Join(generatedDir, "tests.yaml"), data})
	}
	// Distinct source anchors fallback when no annotation scan ran.
	if rep.sourceAnchors == 0 {
		rep.sourceAnchors = distinctSourceFiles(comps)
	}

	// Defer to a curated corpus. When the repo already carries the targeted
	// extractors' output (awareness_graph_*.yaml from make import-graph /
	// proto-contracts / scip / annotation-scan), bootstrap must NOT regenerate
	// parallel copies (which would duplicate component/contract/symbol/test node
	// ids) or delete the curated import graphs. It drops its own conflicting
	// generated files and skips the legacy cleanup, staying additive: the
	// candidate extractors below still run, so bootstrap proposes NEW knowledge
	// without regressing the hand-curated graph. This is what makes a re-run
	// idempotent and non-destructive on an already-governed repo.
	curated := hasCuratedGenerated(generatedDir)
	if curated {
		kept := generated[:0]
		for _, gf := range generated {
			if bootstrapOwnedGenerated[filepath.Base(gf.path)] {
				continue // a curated awareness_graph_* file owns this category
			}
			kept = append(kept, gf)
		}
		if dropped := len(generated) - len(kept); dropped > 0 {
			rep.notes = append(rep.notes, fmt.Sprintf(
				"curated corpus detected (awareness_graph_*.yaml present): deferred %d generated file(s) to the targeted extractors; kept import graphs; nothing deleted", dropped))
		}
		generated = kept
	}

	// ── Apply generated files (write / compare / skip) ──
	switch {
	case *check:
		for _, gf := range generated {
			committed, rerr := os.ReadFile(gf.path)
			if rerr != nil || !bytes.Equal(committed, gf.data) {
				rep.stale = append(rep.stale, relTo(root, gf.path))
			}
		}
	case *dryRun:
		// no writes
	default:
		if err := os.MkdirAll(generatedDir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "sensei bootstrap: %v\n", err)
			return 1
		}
		var removed []string
		if !curated {
			// Only clean up legacy bootstrap artifacts on a non-curated repo. A
			// curated repo's *_import_graph.yaml are the canonical import-scan
			// output — deleting them would regress the graph and break CI.
			var cerr error
			removed, cerr = cleanupLegacyBootstrapArtifacts(generatedDir)
			if cerr != nil {
				fmt.Fprintf(os.Stderr, "sensei bootstrap: cleanup generated artifacts: %v\n", cerr)
				return 1
			}
		}
		for _, path := range removed {
			rep.notes = append(rep.notes, "removed legacy generated artifact: "+relTo(root, path))
		}
		for _, gf := range generated {
			if werr := os.WriteFile(gf.path, gf.data, 0o644); werr != nil {
				fmt.Fprintf(os.Stderr, "sensei bootstrap: write %s: %v\n", gf.path, werr)
				return 1
			}
			rep.writtenGenerated = append(rep.writtenGenerated, relTo(root, gf.path))
		}
	}

	// ── Stage 4–5: candidate extractors → candidates/ ──
	rep.historyCandidates = -1 // sentinel: skipped
	if *skipHistory {
		rep.notes = append(rep.notes, "history: skipped (--skip-history)")
	} else if _, gerr := os.Stat(filepath.Join(root, ".git")); os.IsNotExist(gerr) {
		rep.notes = append(rep.notes, "history: skipped (not a git repository)")
	} else if *check {
		rep.notes = append(rep.notes, "history: skipped (--check does not mine candidates)")
	} else {
		before := countYAML(candidatesDir)
		cbArgs := []string{"-repo", root, "-out", candidatesDir, "-drafter", "echo"}
		if *dryRun {
			cbArgs = append(cbArgs, "-dry-run")
		}
		fmt.Fprintln(os.Stderr, "── history-signal mining (coldsource, offline echo drafter) ──")
		if rc := runColdBootstrap(cbArgs); rc != 0 {
			rep.notes = append(rep.notes, fmt.Sprintf("history: coldsource mining returned %d (non-fatal)", rc))
		}
		if *dryRun {
			rep.historyCandidates = 0
		} else {
			rep.historyCandidates = countYAML(candidatesDir) - before
		}
	}
	// Minimum candidate extractors (conservative; status: candidate, never
	// promoted). Pattern candidates derive from the proto API shape; misuse
	// candidates from direct storage-driver imports.
	writeCands := !*dryRun && !*check
	if n, cerr := writeCandidateFiles(candidatesDir, extractPatternCandidates(protoContracts), writeCands); cerr == nil {
		rep.candidatePatterns = n
	} else {
		rep.notes = append(rep.notes, "pattern candidates: "+cerr.Error())
	}
	if n, cerr := writeCandidateFiles(candidatesDir, extractMisuseCandidates(root), writeCands); cerr == nil {
		rep.candidateMisuses = n
	} else {
		rep.notes = append(rep.notes, "misuse candidates: "+cerr.Error())
	}
	// Invariants AND authority surfaces from ONE Go-AST pass, both gated at medium
	// confidence. The single extractor (extractGoArchitecture behind
	// buildInvariantExtractionReport) parses each .go file once and feeds both the
	// invariant synthesizer and the authority-surface scanner — no double parse.
	// medium floor keeps only corroborated results: for invariants, a guard with a
	// test / owned write path / rule-signaling test; for authority, a route,
	// lifecycle control, or guarded mutation (bare unguarded mutations score low
	// and drop). status: candidate, never promoted.
	if report, ierr := buildInvariantExtractionReport(root, invariantExtractOptions{
		Repo:              root,
		IncludeTests:      true,
		MinimumConfidence: "medium",
	}); ierr != nil {
		rep.notes = append(rep.notes, "invariant/authority extraction: "+ierr.Error())
	} else {
		rep.candidateInvariants = len(report.Candidates)
		rep.candidateAuthority = len(report.AuthoritySurfaces)
		if writeCands && len(report.Candidates) > 0 {
			doc := struct {
				Invariants []extractedInvariantCandidate `yaml:"invariants"`
			}{report.Candidates}
			if data, rerr := renderGenerated("Invariant candidates from `sensei extract-invariants` at medium confidence (corroborated only; status: candidate).", doc); rerr != nil {
				rep.notes = append(rep.notes, "invariant candidates: render: "+rerr.Error())
			} else if merr := os.MkdirAll(candidatesDir, 0o755); merr != nil {
				rep.notes = append(rep.notes, "invariant candidates: mkdir: "+merr.Error())
			} else if werr := os.WriteFile(filepath.Join(candidatesDir, "invariant_candidates.yaml"), data, 0o644); werr != nil {
				rep.notes = append(rep.notes, "invariant candidates: write: "+werr.Error())
			}
		}
		if writeCands && len(report.AuthoritySurfaces) > 0 {
			if out, rerr := renderAuthorityCandidates(root, report.AuthoritySurfaces); rerr != nil {
				rep.notes = append(rep.notes, "authority candidates: render: "+rerr.Error())
			} else if merr := os.MkdirAll(candidatesDir, 0o755); merr != nil {
				rep.notes = append(rep.notes, "authority candidates: mkdir: "+merr.Error())
			} else if werr := os.WriteFile(filepath.Join(candidatesDir, "authority_surface_candidates.yaml"), out, 0o644); werr != nil {
				rep.notes = append(rep.notes, "authority candidates: write: "+werr.Error())
			}
		}
	}
	// Boundary candidates inferred from the import graph: Go internal/ visibility
	// boundaries (compiler-enforced) and contract-exposure API seams. Conservative,
	// status: candidate, never promoted.
	if bnds := extractBoundaryCandidates(igComponents); len(bnds) > 0 {
		rep.candidateBoundaries = len(bnds)
		if writeCands {
			if data, rerr := renderGenerated("Boundary candidates inferred from the import graph (assertion: inferred, status: candidate).", boundaryCandidateDoc{Boundaries: bnds}); rerr != nil {
				rep.notes = append(rep.notes, "boundary candidates: render: "+rerr.Error())
			} else if merr := os.MkdirAll(candidatesDir, 0o755); merr != nil {
				rep.notes = append(rep.notes, "boundary candidates: mkdir: "+merr.Error())
			} else if werr := os.WriteFile(filepath.Join(candidatesDir, "boundary_candidates.yaml"), data, 0o644); werr != nil {
				rep.notes = append(rep.notes, "boundary candidates: write: "+werr.Error())
			}
		}
	}

	// ── Stage 7: gates ──
	if !*check {
		// validate (read-only).
		scanDirs := []string{awarenessDir}
		if _, ierr := os.Stat(filepath.Join(root, "docs", "intent")); ierr == nil {
			scanDirs = append(scanDirs, filepath.Join(root, "docs", "intent"))
		}
		var extraDef []string
		if ag := agAwarenessDir(root); ag != "" {
			extraDef = append(extraDef, ag)
		}
		sourceRoots := []string{root}
		if svc, _ := resolveServicesRepo(""); svc != "" {
			if absSvc, _ := filepath.Abs(svc); absSvc != "" {
				if absRoot, _ := filepath.Abs(root); absSvc != absRoot {
					extraDef = appendExistingDir(extraDef,
						filepath.Join(svc, "docs", "awareness"),
						filepath.Join(svc, "docs", "intent"),
						filepath.Join(svc, "docs", "awareness", "generated"),
					)
					sourceRoots = append(sourceRoots, svc)
				}
			}
		}
		if vr, verr := doValidate(root, scanDirs, extraDef, sourceRoots, validateScopeLocal); verr == nil {
			rep.validationByCheck = vr.Counts
			rep.validationFindings = len(vr.Findings)
		} else {
			rep.notes = append(rep.notes, "validate: "+verr.Error())
		}

		// build (in-process compile; no store required).
		if *skipBuild {
			rep.buildStatus = "skipped (--skip-build)"
		} else if *dryRun {
			rep.buildStatus = "skipped (--dry-run)"
		} else {
			var buf bytes.Buffer
			em, _, berr := extractor.ImportAwarenessDir(awarenessDir, &buf)
			if berr != nil {
				rep.buildStatus = "failed: " + berr.Error()
			} else if ntErrs := extractor.ValidateNTriples(bytes.NewReader(buf.Bytes())); len(ntErrs) > 0 {
				rep.buildStatus = fmt.Sprintf("failed: %d invalid N-Triples", len(ntErrs))
			} else {
				rep.buildStatus = fmt.Sprintf("ok (%d triples)", em.Triples)
			}
		}
	} else {
		rep.buildStatus = "skipped (--check)"
	}

	rep.computeNextActions()
	rep.print(os.Stdout)

	if *check && len(rep.stale) > 0 {
		return 1
	}
	return 0
}

// ── code-symbol extraction (reuses golang/scanner) ──

type codeSymbolResult struct {
	symbols     []scanner.CodeSymbol
	symbolsYAML []byte
	edgesYAML   []byte
}

func findRegistry(root string) string {
	for _, c := range []string{
		filepath.Join(root, "docs", "awareness", "namespaces.yaml"),
		statedir.Path(root, "namespaces.yaml"),
	} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func extractCodeSymbols(root, registryPath string) (codeSymbolResult, error) {
	var res codeSymbolResult
	reg, err := scanner.LoadRegistry(registryPath)
	if err != nil {
		return res, err
	}
	sc := &scanner.Scanner{Registry: reg, RepoRoot: root, Strict: false}
	result, err := sc.Scan(root)
	if err != nil {
		return res, err
	}
	syms, edges, tests := scanner.BuildSymbolsAndEdges(result)
	res.symbols = syms
	var sb, eb bytes.Buffer
	sb.WriteString("# GENERATED by `sensei bootstrap` — DO NOT EDIT.\n")
	if err := scanner.WriteSymbolsYAML(&sb, syms, tests); err != nil {
		return res, err
	}
	eb.WriteString("# GENERATED by `sensei bootstrap` — DO NOT EDIT.\n")
	if err := scanner.WriteEdgesYAML(&eb, edges); err != nil {
		return res, err
	}
	res.symbolsYAML = sb.Bytes()
	res.edgesYAML = eb.Bytes()
	return res, nil
}

// agAwarenessDir returns the awareness-graph repo's docs/awareness dir (for the
// shared meta corpus) when the target repo is NOT the awareness-graph repo
// itself. Defaults to the cwd's docs/awareness when distinct from root.
func agAwarenessDir(root string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	ag := filepath.Join(cwd, "docs", "awareness")
	if absRoot, _ := filepath.Abs(root); filepath.Join(absRoot, "docs", "awareness") == ag {
		return "" // bootstrapping the awareness-graph repo itself
	}
	if _, err := os.Stat(filepath.Join(ag, "namespaces.yaml")); err == nil {
		return ag
	}
	if _, err := os.Stat(ag); err == nil {
		return ag
	}
	return ""
}

// ── helpers ──

type genFile struct {
	path string
	data []byte
}

func countYAML(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && (strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml")) {
			n++
		}
		return nil
	})
	return n
}

// bootstrapOwnedGenerated is the set of generated/ filenames bootstrap emits.
// On a curated repo (awareness_graph_* present) these are deferred to the
// targeted extractors so bootstrap never duplicates their node ids.
var bootstrapOwnedGenerated = map[string]bool{
	"contracts.yaml":            true,
	"rest_contracts.yaml":       true,
	"components.yaml":           true,
	"web_components.yaml":       true,
	"contract_consumption.yaml": true,
	"source_symbols.yaml":       true,
	"source_edges.yaml":         true,
	"scip_symbols.yaml":         true,
	"scip_references.yaml":      true,
	"tests.yaml":                true,
}

// hasCuratedGenerated reports whether the generated dir already carries the
// targeted extractors' output (awareness_graph_*.yaml from make import-graph /
// proto-contracts / scip / annotation-scan). Its presence means the repo
// maintains its committed generated corpus deliberately, so bootstrap defers to
// it rather than regenerating parallel, colliding copies.
func hasCuratedGenerated(generatedDir string) bool {
	matches, _ := filepath.Glob(filepath.Join(generatedDir, "awareness_graph_*.yaml"))
	return len(matches) > 0
}

func cleanupLegacyBootstrapArtifacts(generatedDir string) ([]string, error) {
	pattern := filepath.Join(generatedDir, "*_import_graph.yaml")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, path := range matches {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return removed, err
		}
		removed = append(removed, path)
	}
	sort.Strings(removed)
	return removed, nil
}

func repairLegacyStarterTemplates(root string) ([]string, error) {
	type legacyStarter struct {
		name   string
		marker string
	}
	starters := []legacyStarter{
		{name: "invariants.yaml", marker: "example.config_must_not_use_env_vars"},
		{name: "failure_modes.yaml", marker: "config.env_var_overrides_production_setting"},
		{name: "incident_patterns.yaml", marker: "pat.env_var_fallback_added"},
	}
	var refreshed []string
	for _, starter := range starters {
		path := filepath.Join(root, "docs", "awareness", starter.name)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if !looksLikeLegacyStarterTemplate(string(data), starter.marker) {
			continue
		}
		content, err := templates.ReadFile("templates/awareness/" + starter.name)
		if err != nil {
			return nil, err
		}
		if bytes.Equal(data, content) {
			continue
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			return nil, err
		}
		refreshed = append(refreshed, path)
	}
	sort.Strings(refreshed)
	return refreshed, nil
}

func looksLikeLegacyStarterTemplate(content, marker string) bool {
	if !strings.Contains(content, marker) {
		return false
	}
	return strings.Count(content, "\n  - id: ") == 1
}

func distinctSourceFiles(comps []bootstrapComponent) int {
	seen := map[string]bool{}
	for _, c := range comps {
		for _, f := range c.SourceFiles {
			seen[f] = true
		}
	}
	return len(seen)
}

// ── report ──

type bootstrapReport struct {
	root                  string
	dryRun                bool
	check                 bool
	scaffolded            []string
	writtenGenerated      []string
	components            int
	contracts             int
	operations            int
	webComponents         int // native custom elements (customElements.define / @customElement)
	contractConsumptions  int // contracts consumed via gRPC-web service clients (consumed_by edges)
	importEdges           int // component dependsOn edges from Go imports
	importEdgesClassified int // semantic edges (reads_from/writes_to/exposes) from the classifier
	tests                 int
	sourceAnchors         int
	candidatePatterns     int
	candidateMisuses      int
	candidateAuthority    int // AuthoritySurface candidates from Go source (handlers/guards/lifecycle/state)
	candidateBoundaries   int // Boundary candidates inferred from the import graph (internal/ + contract exposure)
	candidateInvariants   int // Invariant candidates inferred from rule-signaling test names
	historyCandidates     int // -1 = skipped
	validationFindings    int
	validationByCheck     map[string]int
	buildStatus           string
	stale                 []string
	notes                 []string
	nextActions           []string
}

func (r *bootstrapReport) computeNextActions() {
	r.nextActions = append(r.nextActions,
		"Review and refine generated components in docs/awareness/generated/components.yaml (kind/owner are inferred).")
	if r.historyCandidates > 0 {
		r.nextActions = append(r.nextActions,
			fmt.Sprintf("Review %d history candidate(s) in docs/awareness/candidates/ and promote with `sensei promote` (none auto-promoted).", r.historyCandidates))
	}
	r.nextActions = append(r.nextActions,
		"Hand-author MetaPrinciple / Invariant / Decision / Boundary links the deterministic pass cannot infer.")
	if r.validationFindings > 0 {
		r.nextActions = append(r.nextActions,
			fmt.Sprintf("Resolve %d validation finding(s) (`sensei validate`).", r.validationFindings))
	}
	if strings.HasPrefix(r.buildStatus, "failed") {
		r.nextActions = append(r.nextActions, "Fix build errors before serving the graph.")
	}
}

func (r *bootstrapReport) print(w *os.File) {
	hist := "skipped"
	if r.historyCandidates >= 0 {
		hist = fmt.Sprintf("%d", r.historyCandidates)
	}
	mode := "write"
	if r.dryRun {
		mode = "dry-run (no writes)"
	} else if r.check {
		mode = "check"
	}
	fmt.Fprintf(w, "\nAWG bootstrap report — %s\n  mode:                       %s\n", r.root, mode)
	if len(r.scaffolded) > 0 {
		fmt.Fprintf(w, "  scaffolded:                 %d file(s)\n", len(r.scaffolded))
	}
	fmt.Fprintf(w, "  components found:           %d\n", r.components)
	fmt.Fprintf(w, "  contracts found:            %d\n", r.contracts)
	fmt.Fprintf(w, "  operations found:           %d\n", r.operations)
	fmt.Fprintf(w, "  web components found:       %d\n", r.webComponents)
	fmt.Fprintf(w, "  contracts consumed:         %d\n", r.contractConsumptions)
	fmt.Fprintf(w, "  import dependencies:        %d (%d classified)\n", r.importEdges, r.importEdgesClassified)
	fmt.Fprintf(w, "  tests found:                %d\n", r.tests)
	fmt.Fprintf(w, "  source anchors found:       %d\n", r.sourceAnchors)
	fmt.Fprintf(w, "  candidate patterns found:   %d\n", r.candidatePatterns)
	fmt.Fprintf(w, "  candidate misuses found:    %d\n", r.candidateMisuses)
	fmt.Fprintf(w, "  authority surfaces found:   %d\n", r.candidateAuthority)
	fmt.Fprintf(w, "  boundary candidates found:  %d\n", r.candidateBoundaries)
	fmt.Fprintf(w, "  invariant candidates found: %d\n", r.candidateInvariants)
	fmt.Fprintf(w, "  history-derived candidates: %s\n", hist)
	fmt.Fprintf(w, "  validation findings:        %d\n", r.validationFindings)
	if len(r.validationByCheck) > 0 {
		checks := make([]string, 0, len(r.validationByCheck))
		for k := range r.validationByCheck {
			checks = append(checks, k)
		}
		sort.Strings(checks)
		for _, k := range checks {
			fmt.Fprintf(w, "      %-32s %d\n", k+":", r.validationByCheck[k])
		}
	}
	fmt.Fprintf(w, "  build status:               %s\n", r.buildStatus)
	if len(r.writtenGenerated) > 0 {
		fmt.Fprintf(w, "  wrote generated:            %s\n", strings.Join(r.writtenGenerated, ", "))
	}
	if r.check {
		if len(r.stale) == 0 {
			fmt.Fprintf(w, "  generated freshness:        FRESH\n")
		} else {
			fmt.Fprintf(w, "  generated freshness:        STALE — %s\n", strings.Join(r.stale, ", "))
		}
	}
	if len(r.notes) > 0 {
		fmt.Fprintln(w, "  notes:")
		for _, n := range r.notes {
			fmt.Fprintf(w, "      - %s\n", n)
		}
	}
	fmt.Fprintln(w, "  next recommended actions:")
	for _, a := range r.nextActions {
		fmt.Fprintf(w, "      - %s\n", a)
	}
	fmt.Fprintln(w)
}
