// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/gosemantics"
	"gopkg.in/yaml.v3"
)

type invariantExtractionReport struct {
	GeneratedBy       string                         `json:"generated_by" yaml:"generated_by"`
	GeneratedAt       string                         `json:"generated_at" yaml:"generated_at"`
	RepoRoot          string                         `json:"repo_root" yaml:"repo_root"`
	Facts             []normalizedInvariantFact      `json:"facts" yaml:"facts"`
	Candidates        []extractedInvariantCandidate  `json:"candidates" yaml:"candidates"`
	AuthoritySurfaces []authoritySurfaceCandidate    `json:"authority_surfaces,omitempty" yaml:"authority_surfaces,omitempty"`
	MutationAnalysis  invariantMutationAnalysisState `json:"mutation_analysis,omitempty" yaml:"mutation_analysis,omitempty"`
	Limitations       []architecture.Limitation      `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type normalizedInvariantFact = architecture.Fact
type invariantFactScope = architecture.Scope
type invariantFactEvidence = architecture.Evidence

type extractedInvariantCandidate struct {
	ID                      string                        `json:"id" yaml:"id"`
	Statement               string                        `json:"statement" yaml:"statement"`
	Kind                    string                        `json:"kind" yaml:"kind"`
	Status                  string                        `json:"status" yaml:"status"`
	Confidence              extractedInvariantConfidence  `json:"confidence" yaml:"confidence"`
	Scope                   invariantCandidateScope       `json:"scope" yaml:"scope"`
	Authority               invariantCandidateAuthority   `json:"authority" yaml:"authority"`
	Evidence                invariantCandidateEvidence    `json:"evidence" yaml:"evidence"`
	Contradictions          []string                      `json:"contradictions" yaml:"contradictions"`
	AlternativeExplanations []string                      `json:"alternative_explanations" yaml:"alternative_explanations"`
	Unknowns                []string                      `json:"unknowns" yaml:"unknowns"`
	ProofObligations        []string                      `json:"proof_obligations" yaml:"proof_obligations"`
	Promotion               invariantPromotionDisposition `json:"promotion" yaml:"promotion"`
}

type extractedInvariantConfidence struct {
	Level       string   `json:"level" yaml:"level"`
	Score       int      `json:"score" yaml:"score"`
	Explanation []string `json:"explanation" yaml:"explanation"`
}

type invariantCandidateScope struct {
	Repositories []string `json:"repositories" yaml:"repositories"`
	Components   []string `json:"components" yaml:"components"`
	Files        []string `json:"files" yaml:"files"`
	Symbols      []string `json:"symbols" yaml:"symbols"`
}

type invariantCandidateAuthority struct {
	Owner     string   `json:"owner" yaml:"owner"`
	Writers   []string `json:"writers" yaml:"writers"`
	Consumers []string `json:"consumers" yaml:"consumers"`
}

type invariantCandidateEvidence struct {
	Facts         []string `json:"facts" yaml:"facts"`
	Tests         []string `json:"tests" yaml:"tests"`
	Guards        []string `json:"guards" yaml:"guards"`
	Schemas       []string `json:"schemas" yaml:"schemas"`
	Gates         []string `json:"gates" yaml:"gates"`
	Commits       []string `json:"commits" yaml:"commits"`
	Incidents     []string `json:"incidents" yaml:"incidents"`
	Documentation []string `json:"documentation" yaml:"documentation"`
}

type invariantPromotionDisposition struct {
	Eligible bool     `json:"eligible" yaml:"eligible"`
	Missing  []string `json:"missing" yaml:"missing"`
}

type invariantMutationAnalysisState struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Status  string `json:"status" yaml:"status"`
	WorkDir string `json:"work_dir,omitempty" yaml:"work_dir,omitempty"`
}

type invariantExtractOptions struct {
	Repo                    string
	Format                  string
	Output                  string
	IncludeHistory          bool
	IncludeDocs             bool
	IncludeTests            bool
	IncludeMutationAnalysis bool
	MinimumConfidence       string
	Explain                 bool
	Check                   bool
}

type invariantRepositoryIdentity struct {
	Root         string
	Repository   string
	Domain       string
	DomainStatus string
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
	report, err := buildInvariantExtractionReport(root, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei extract-invariants: %v\n", err)
		return 1
	}
	rendered, err := renderInvariantExtractionReport(report, opts.Format)
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

func buildInvariantExtractionReport(root string, opts invariantExtractOptions) (invariantExtractionReport, error) {
	identity := resolveInvariantRepositoryIdentity(root)
	var facts []normalizedInvariantFact
	var limitations []architecture.Limitation
	// One Go-AST pass produces both invariant facts and authority surfaces.
	goFacts, authority, err := extractGoArchitecture(identity, opts)
	if err != nil {
		return invariantExtractionReport{}, err
	}
	facts = append(facts, goFacts...)
	authority = filterAuthorityByMinConfidence(authority, opts.MinimumConfidence)
	semantic, semanticErr := gosemantics.Extract(root)
	if semanticErr != nil {
		limitations = append(limitations, architecture.Limitation{
			Source: "go_semantic_extractor", Scope: "repository", Reason: semanticErr.Error(), Blocking: false,
		})
	} else {
		for _, observation := range semantic.Observations {
			if !opts.IncludeTests && observation.Predicate == gosemantics.PredicateTestCallsSymbol {
				continue
			}
			facts = append(facts, invariantFact(identity, observation.Kind, observation.Subject, observation.Predicate,
				observation.Object, observation.File, observation.Symbol, observation.Line, observation.Line, "",
				"go_semantic_extractor", observation.Confidence, observation.Meta))
		}
		for _, limitation := range semantic.Limitations {
			limitations = append(limitations, architecture.Limitation{
				Source: "go_semantic_extractor", Scope: limitation.Scope, Reason: limitation.Reason, Blocking: false,
			})
		}
	}
	facts = append(facts, extractGeneratedAuthorityFacts(identity)...)
	facts = append(facts, extractCIGateFacts(identity)...)
	if opts.IncludeDocs {
		facts = append(facts, extractAwarenessDocumentationFacts(identity)...)
	}
	if opts.IncludeHistory {
		historyFacts, historyLimitations := extractHistoryInvariantFacts(identity)
		facts = append(facts, historyFacts...)
		limitations = append(limitations, historyLimitations...)
	}
	facts, err = normalizeInvariantFacts(root, facts)
	if err != nil {
		return invariantExtractionReport{}, err
	}
	candidates := synthesizeInvariantCandidates(root, facts)
	candidates = filterInvariantCandidates(candidates, opts.MinimumConfidence)
	sortInvariantCandidates(candidates)
	report := invariantExtractionReport{
		GeneratedBy:       "sensei extract-invariants",
		GeneratedAt:       "deterministic",
		RepoRoot:          root,
		Facts:             facts,
		Candidates:        candidates,
		AuthoritySurfaces: authority,
		Limitations:       limitations,
	}
	if opts.IncludeMutationAnalysis {
		tmp, err := os.MkdirTemp("", "sensei-invariant-mutants-*")
		if err != nil {
			return invariantExtractionReport{}, err
		}
		report.MutationAnalysis = invariantMutationAnalysisState{
			Enabled: true,
			Status:  "prepared_isolated_workspace_no_mutants_run_in_increment_1",
			WorkDir: tmp,
		}
	}
	return report, nil
}

func resolveInvariantRepositoryIdentity(root string) invariantRepositoryIdentity {
	if strings.TrimSpace(root) == "" {
		return invariantRepositoryIdentity{
			Root:         "",
			Repository:   "",
			Domain:       "",
			DomainStatus: architecture.RepositoryDomainFallback,
		}
	}
	identity := invariantRepositoryIdentity{
		Root:       root,
		Repository: filepath.Base(root),
	}
	domain := gitRemoteDomain(root)
	if strings.TrimSpace(domain) == "" {
		identity.Domain = identity.Repository
		identity.DomainStatus = architecture.RepositoryDomainFallback
		return identity
	}
	identity.Domain = domain
	identity.DomainStatus = architecture.RepositoryDomainResolved
	return identity
}

func invariantGoFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && invariantSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == ".go" && !strings.HasSuffix(path, ".pb.go") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func invariantSkipDir(name string) bool {
	switch name {
	case ".git", "vendor", "node_modules", ".cache", "dist", "build":
		return true
	default:
		return false
	}
}

// extractGoFileFactsFromAST is the parse-free core: it derives invariant facts
// from an already-parsed file so one Go-AST pass can feed both this and the
// authority-surface extractor (see extractGoArchitecture).
func extractGoFileFactsFromAST(identity invariantRepositoryIdentity, rel string, file *ast.File, fset *token.FileSet, opts invariantExtractOptions) []normalizedInvariantFact {
	var facts []normalizedInvariantFact
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if ok && gen.Tok == token.TYPE {
			facts = append(facts, extractSchemaFacts(identity, rel, file.Name.Name, gen, fset)...)
		}
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Name == nil {
			continue
		}
		symbol := file.Name.Name + "." + fn.Name.Name
		if opts.IncludeTests && strings.HasPrefix(fn.Name.Name, "Test") {
			facts = append(facts, testAssertionFact(identity, rel, symbol, fn, fset))
		}
		readSeen := map[string]bool{}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.IfStmt:
				if invariantBlockReturnsError(x.Body) {
					cond := invariantExprString(fset, x.Cond)
					kind := "guard"
					predicate := "refuses_when"
					if invariantLooksLikeGeneration(cond) {
						kind = "generation_check"
						predicate = "compares_generation"
					} else if invariantLooksLikeTransition(cond) {
						kind = "transition"
						predicate = "rejects_transition_when"
					}
					facts = append(facts, invariantFact(identity, kind, symbol, predicate, cond, rel, symbol, fset.Position(x.Pos()).Line, fset.Position(x.End()).Line, "", "go_guard_extractor", 0.65, nil))
				}
			case *ast.AssignStmt:
				for i, lhs := range x.Lhs {
					field := invariantWriteTarget(lhs)
					if field == "" {
						continue
					}
					object := ""
					if i < len(x.Rhs) {
						object = invariantExprString(fset, x.Rhs[i])
					}
					meta := map[string]string{}
					kind := "write"
					predicate := "writes"
					if invariantLooksLikeGeneration(field) && (x.Tok == token.ADD_ASSIGN || strings.Contains(object, "+ 1") || strings.Contains(object, "+1")) {
						kind = "generation_check"
						predicate = "increments_generation"
					}
					facts = append(facts, invariantFact(identity, kind, symbol, predicate, field, rel, symbol, fset.Position(x.Pos()).Line, fset.Position(x.End()).Line, "", "go_write_extractor", 0.55, meta))
				}
			case *ast.IncDecStmt:
				field := invariantWriteTarget(x.X)
				if field != "" {
					predicate := "writes"
					kind := "write"
					if invariantLooksLikeGeneration(field) && x.Tok == token.INC {
						predicate = "increments_generation"
						kind = "generation_check"
					}
					facts = append(facts, invariantFact(identity, kind, symbol, predicate, field, rel, symbol, fset.Position(x.Pos()).Line, fset.Position(x.End()).Line, "", "go_write_extractor", 0.55, nil))
				}
			case *ast.SelectorExpr:
				field := invariantSelectorName(x)
				if field != "" && !readSeen[field] {
					readSeen[field] = true
					facts = append(facts, invariantFact(identity, "read", symbol, "reads", field, rel, symbol, fset.Position(x.Pos()).Line, fset.Position(x.End()).Line, "", "go_read_extractor", 0.35, nil))
				}
			case *ast.CallExpr:
				call := invariantCallName(x.Fun)
				if call != "" && invariantLooksLikePersistenceCall(call) {
					facts = append(facts, invariantFact(identity, "write", symbol, "persists_via", call, rel, symbol, fset.Position(x.Pos()).Line, fset.Position(x.End()).Line, "", "go_write_extractor", 0.5, nil))
				}
			}
			return true
		})
	}
	if opts.IncludeDocs {
		for _, group := range file.Comments {
			text := strings.TrimSpace(group.Text())
			if invariantNormativeText(text) {
				pos := fset.Position(group.Pos())
				facts = append(facts, invariantFact(identity, "documentation_claim", rel, "claims_normative_rule", invariantCompact(text), rel, "", pos.Line, fset.Position(group.End()).Line, "", "go_comment_extractor", 0.25, nil))
			}
		}
	}
	return facts
}

// extractGoArchitecture is the single Go-AST pass: it walks the repo's .go files,
// parses each ONCE, and feeds every file to the invariant fact extractor and
// (for non-test files) to the authority-surface extractor. This is the union
// substrate that retires the old double-parse — extract-authority and
// extract-invariants no longer each walk the tree separately.
func extractGoArchitecture(identity invariantRepositoryIdentity, opts invariantExtractOptions) ([]normalizedInvariantFact, []authoritySurfaceCandidate, error) {
	files, err := invariantGoFiles(identity.Root)
	if err != nil {
		return nil, nil, err
	}
	var facts []normalizedInvariantFact
	var authority []authoritySurfaceCandidate
	for _, path := range files {
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", path, perr)
		}
		rel := invariantRel(identity.Root, path)
		facts = append(facts, extractGoFileFactsFromAST(identity, rel, file, fset, opts)...)
		if !isTestFile(filepath.Base(path)) && !strings.HasSuffix(path, ".pb.go") {
			authCandidates, authFacts := scanAuthorityDeclsAndFacts(identity, rel, file, fset)
			authority = append(authority, authCandidates...)
			facts = append(facts, authFacts...)
		}
	}
	sort.SliceStable(authority, func(i, j int) bool { return authority[i].ID < authority[j].ID })
	return facts, authority, nil
}

func extractSchemaFacts(identity invariantRepositoryIdentity, rel, pkg string, gen *ast.GenDecl, fset *token.FileSet) []normalizedInvariantFact {
	var facts []normalizedInvariantFact
	for _, spec := range gen.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok || ts.Name == nil {
			continue
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok || st.Fields == nil {
			continue
		}
		for _, field := range st.Fields.List {
			if field.Tag == nil || len(field.Names) == 0 {
				continue
			}
			tag := strings.Trim(field.Tag.Value, "`")
			name := schemaFieldName(tag)
			if name == "" || name == "-" {
				continue
			}
			subject := pkg + "." + ts.Name.Name + "." + field.Names[0].Name
			facts = append(facts, invariantFact(identity, "schema_constraint", subject, "accepts_field", name, rel, subject, fset.Position(field.Pos()).Line, fset.Position(field.End()).Line, "", "go_schema_extractor", 0.45, nil))
		}
	}
	return facts
}

func schemaFieldName(tag string) string {
	for _, key := range []string{"json", "yaml"} {
		prefix := key + ":\""
		idx := strings.Index(tag, prefix)
		if idx < 0 {
			continue
		}
		rest := tag[idx+len(prefix):]
		end := strings.Index(rest, "\"")
		if end < 0 {
			continue
		}
		name := strings.Split(rest[:end], ",")[0]
		return strings.TrimSpace(name)
	}
	return ""
}

func extractGeneratedAuthorityFacts(identity invariantRepositoryIdentity) []normalizedInvariantFact {
	var facts []normalizedInvariantFact
	for _, rel := range []string{"docs/awareness/generated", "golang/server/embeddata/awareness.nt", "golang/server/embeddata/awareness.transaction.tsv"} {
		path := filepath.Join(identity.Root, filepath.FromSlash(rel))
		if _, err := os.Stat(path); err == nil {
			facts = append(facts, invariantFact(identity, "generation_check", rel, "generated_artifact_exists", rel, rel, "", 0, 0, "", "generated_artifact_extractor", 0.5, nil))
		}
	}
	for _, rel := range []string{".github/workflows/seed-rebuild.yml", ".github/workflows/awg-gate.yml", "Makefile"} {
		path := filepath.Join(identity.Root, filepath.FromSlash(rel))
		raw, err := os.ReadFile(path)
		if err == nil && (bytes.Contains(raw, []byte("diff")) || bytes.Contains(raw, []byte("--check")) || bytes.Contains(raw, []byte("gate"))) {
			facts = append(facts, invariantFact(identity, "ci_gate", rel, "checks_generated_freshness_or_gate", rel, rel, "", 0, 0, invariantFirstLine(string(raw)), "ci_extractor", 0.65, nil))
		}
	}
	return facts
}

func extractCIGateFacts(identity invariantRepositoryIdentity) []normalizedInvariantFact {
	var facts []normalizedInvariantFact
	base := filepath.Join(identity.Root, ".github", "workflows")
	_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yml" && ext != ".yaml" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		text := strings.ToLower(string(raw))
		rel := invariantRel(identity.Root, path)
		switch {
		case strings.Contains(text, "gate --enforce") || strings.Contains(text, "sensei gate") || strings.Contains(text, "awg gate"):
			facts = append(facts, invariantFact(identity, "ci_gate", rel, "enforces_architectural_gate", rel, rel, "", 0, 0, "gate --enforce", "ci_extractor", 0.8, nil))
		case strings.Contains(text, "grep") || strings.Contains(text, "forbidden") || strings.Contains(text, "source-check"):
			facts = append(facts, invariantFact(identity, "ci_gate", rel, "runs_scanner", rel, rel, "", 0, 0, firstScannerCommand(string(raw)), "ci_extractor", 0.6, nil))
		}
		return nil
	})
	return facts
}

func extractAwarenessDocumentationFacts(identity invariantRepositoryIdentity) []normalizedInvariantFact {
	var facts []normalizedInvariantFact
	base := filepath.Join(identity.Root, "docs", "awareness")
	_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.Contains(filepath.ToSlash(path), "/candidates/") {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" && ext != ".md" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel := invariantRel(identity.Root, path)
		for lineNo, line := range strings.Split(string(raw), "\n") {
			if invariantNormativeText(line) {
				facts = append(facts, invariantFact(identity, "documentation_claim", rel, "claims_normative_rule", invariantCompact(line), rel, "", lineNo+1, lineNo+1, "", "awareness_doc_extractor", 0.35, nil))
			}
		}
		return nil
	})
	return facts
}

func extractHistoryInvariantFacts(identity invariantRepositoryIdentity) ([]normalizedInvariantFact, []architecture.Limitation) {
	revision, status, lim := architecture.ResolveRevision(identity.Root, true)
	if status != architecture.RevisionResolved {
		return nil, lim
	}
	cmd := exec.Command("git", "-C", identity.Root, "log", "--oneline", "--name-status", "-n", "80")
	out, err := cmd.Output()
	if err != nil {
		return nil, []architecture.Limitation{{
			Source:   identity.Root,
			Scope:    "git_history",
			Reason:   err.Error(),
			Blocking: false,
		}}
	}
	var facts []normalizedInvariantFact
	var commit string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 8 && !strings.Contains(line[:8], "\t") && !strings.Contains(line[:8], " ") {
			commit = line
			lower := strings.ToLower(line)
			if strings.Contains(lower, "remove") || strings.Contains(lower, "forbid") || strings.Contains(lower, "bypass") || strings.Contains(lower, "direct") {
				facts = append(facts, invariantFactWithProvenance(identity, "historical_removal", "git_history", "records_removal_or_forbidden_pattern", line, "", "", 0, 0, "", "git_history_extractor", 0.45, map[string]string{"commit": strings.Fields(line)[0]}, architecture.Options{Revision: revision, RevisionStatus: status}))
			}
			continue
		}
		if commit != "" && (strings.HasPrefix(line, "D") || strings.HasPrefix(line, "M")) {
			lower := strings.ToLower(commit + " " + line)
			if strings.Contains(lower, "direct") || strings.Contains(lower, "bypass") || strings.Contains(lower, "forbid") || strings.Contains(lower, "remove") {
				facts = append(facts, invariantFactWithProvenance(identity, "historical_removal", "git_history", "changed_file_after_removal_pressure", line, "", "", 0, 0, "", "git_history_extractor", 0.4, map[string]string{"commit": strings.Fields(commit)[0]}, architecture.Options{Revision: revision, RevisionStatus: status}))
			}
		}
	}
	return facts, nil
}

func synthesizeInvariantCandidates(root string, facts []normalizedInvariantFact) []extractedInvariantCandidate {
	var out []extractedInvariantCandidate
	out = append(out, synthesizeGuardCandidates(root, facts)...)
	out = append(out, synthesizeAuthorityCandidates(root, facts)...)
	out = append(out, synthesizeFreshnessCandidates(root, facts)...)
	out = append(out, synthesizeAcceptedButUnconsumedCandidates(root, facts)...)
	out = append(out, synthesizeGeneratedAuthorityCandidates(root, facts)...)
	out = append(out, synthesizeNegativeHistoryCandidates(root, facts)...)
	out = append(out, synthesizeTestAttestedCandidates(root, facts)...)
	return mergeInvariantCandidates(out)
}

// synthesizeTestAttestedCandidates promotes rule-signaling tests to candidate
// invariants. A test whose name encodes a law (asserts_architectural_rule) is a
// behavioral invariant the code's guard clauses may not express at all —
// concurrency, race, panic, idempotency, isolation. The test itself is the
// proof, so it scores like a guard corroborated by a test (20 + 25 = 45 =
// medium). This is the single home for the "tests protect invariants" signal;
// the earlier standalone test-name extractor is retired in favour of it.
func synthesizeTestAttestedCandidates(root string, facts []normalizedInvariantFact) []extractedInvariantCandidate {
	var out []extractedInvariantCandidate
	for _, f := range facts {
		if f.Kind != "assertion" || f.Predicate != "asserts_architectural_rule" {
			continue
		}
		out = append(out, invariantCandidateFromFacts(
			"candidate.invariant.test_attested."+slugify(f.Subject),
			fmt.Sprintf("%s — asserted as a rule by a dedicated test.", f.Object),
			"behavioral",
			45,
			[]string{"a rule-signaling test names and exercises this behavior", "the test is the built-in proof"},
			[]normalizedInvariantFact{f},
			[]string{"Confirm the test covers the rule's full architectural scope.", "Promote only if the rule is load-bearing."},
		))
	}
	return out
}

func synthesizeGuardCandidates(root string, facts []normalizedInvariantFact) []extractedInvariantCandidate {
	var out []extractedInvariantCandidate
	testFacts := factsByKindPredicate(facts, "assertion", "asserts_architectural_rule")
	for _, g := range facts {
		if g.Kind != "guard" && g.Kind != "transition" {
			continue
		}
		tests := relatedTests(g, testFacts)
		score := 20
		expl := []string{"runtime refusal or validation guard"}
		if len(tests) > 0 {
			score += 25
			expl = append(expl, "architectural or regression test corroborates guard")
		} else {
			score -= 10
			expl = append(expl, "single isolated guard; no corroborating test found")
		}
		kind := "state"
		if g.Kind == "transition" {
			kind = "transition"
		}
		out = append(out, invariantCandidateFromFacts(
			"candidate.invariant."+kind+"."+slugify(g.Subject+" "+g.Object),
			fmt.Sprintf("%s refuses operation when %s.", g.Subject, g.Object),
			kind,
			score,
			expl,
			append([]normalizedInvariantFact{g}, tests...),
			[]string{"Confirm the guard applies to the intended architectural scope.", "Add or cite tests for bypass paths before promotion."},
		))
	}
	return out
}

func synthesizeAuthorityCandidates(root string, facts []normalizedInvariantFact) []extractedInvariantCandidate {
	writes := map[string][]normalizedInvariantFact{}
	for _, f := range facts {
		if f.Kind == "write" && (f.Predicate == "writes" || f.Predicate == "persists_via") {
			writes[f.Object] = append(writes[f.Object], f)
		}
	}
	var out []extractedInvariantCandidate
	resources := make([]string, 0, len(writes))
	for resource := range writes {
		resources = append(resources, resource)
	}
	sort.Strings(resources)
	for _, resource := range resources {
		wfacts := writes[resource]
		writers := uniqueFactSubjects(wfacts)
		if resource == "" || len(writers) == 0 {
			continue
		}
		corroboration := authorityCorroborationFacts(resource, facts)
		if len(writers) == 1 && len(corroboration) == 0 {
			continue
		}
		score := 15
		expl := []string{"canonical owner write path evidence"}
		if len(corroboration) > 0 {
			score += 25
			expl = append(expl, "corroborating guard, test, gate, documentation, or history evidence")
		}
		var contradictions []string
		if len(writers) > 1 {
			score -= 25
			contradictions = append(contradictions, "multiple observed writers: "+strings.Join(writers, ", "))
		}
		c := invariantCandidateFromFacts(
			"candidate.invariant.authority."+slugify(resource),
			fmt.Sprintf("%s appears to be mutated through a constrained writer set.", resource),
			"authority",
			score,
			expl,
			append(wfacts, corroboration...),
			[]string{"Prove no bypass writer exists for the scoped state.", "Identify the owning component and allowed mutation API."},
		)
		c.Authority.Writers = writers
		c.Contradictions = contradictions
		out = append(out, c)
	}
	return out
}

func synthesizeFreshnessCandidates(root string, facts []normalizedInvariantFact) []extractedInvariantCandidate {
	var bumps, checks, tests []normalizedInvariantFact
	for _, f := range facts {
		if f.Kind == "generation_check" && f.Predicate == "increments_generation" {
			bumps = append(bumps, f)
		}
		if f.Kind == "generation_check" && f.Predicate == "compares_generation" {
			checks = append(checks, f)
		}
		if f.Kind == "assertion" && strings.Contains(strings.ToLower(f.Object), "generation") {
			tests = append(tests, f)
		}
	}
	if len(bumps) == 0 || len(checks) == 0 {
		return nil
	}
	score := 20 + 20
	expl := []string{"owner mutation appears to increment generation", "consumer or guard compares generation"}
	var supporting []normalizedInvariantFact
	supporting = append(supporting, bumps...)
	supporting = append(supporting, checks...)
	if len(tests) > 0 {
		score += 25
		expl = append(expl, "generation-related test exists")
		supporting = append(supporting, tests...)
	} else {
		score -= 10
		expl = append(expl, "no generation-specific regression test found")
	}
	return []extractedInvariantCandidate{invariantCandidateFromFacts(
		"candidate.invariant.freshness.generation_fence",
		"Generation-derived actions appear valid only when mutation bumps and consumers compare generation state.",
		"freshness",
		score,
		expl,
		supporting,
		[]string{"Prove every semantic mutation increments the generation.", "Prove consumers compare against independently obtained state.", "Prove idempotent writes do not increment generation."},
	)}
}

func synthesizeAcceptedButUnconsumedCandidates(root string, facts []normalizedInvariantFact) []extractedInvariantCandidate {
	reads := map[string]bool{}
	writes := map[string]bool{}
	for _, f := range facts {
		if f.Kind == "read" {
			reads[strings.ToLower(f.Object)] = true
		}
		if f.Kind == "write" {
			writes[strings.ToLower(f.Object)] = true
		}
	}
	var out []extractedInvariantCandidate
	for _, f := range facts {
		if f.Kind != "schema_constraint" || f.Predicate != "accepts_field" {
			continue
		}
		fieldName := strings.ToLower(lastSegment(f.Subject))
		if reads[fieldName] {
			continue
		}
		score := 15
		expl := []string{"typed schema or protocol accepts field", "no behavioral read observed"}
		if writes[fieldName] {
			score += 10
			expl = append(expl, "field is written but not read")
		}
		c := invariantCandidateFromFacts(
			"candidate.invariant.accepted_but_unconsumed."+slugify(f.Subject),
			fmt.Sprintf("%s is accepted by schema tags but no behavioral consumer was observed; unsupported intent may need rejection or implementation.", f.Subject),
			"safety",
			score,
			expl,
			[]normalizedInvariantFact{f},
			[]string{"Confirm all dynamic reads are accounted for.", "Either consume the field behaviorally or reject it before persistence."},
		)
		c.Unknowns = append(c.Unknowns, "Reflection, encoding, or external consumers may read this field outside static selector analysis.")
		out = append(out, c)
	}
	return out
}

func synthesizeGeneratedAuthorityCandidates(root string, facts []normalizedInvariantFact) []extractedInvariantCandidate {
	var generated, gates []normalizedInvariantFact
	for _, f := range facts {
		if f.Kind == "generation_check" && f.Predicate == "generated_artifact_exists" {
			generated = append(generated, f)
		}
		if f.Kind == "ci_gate" && (f.Predicate == "checks_generated_freshness_or_gate" || f.Predicate == "enforces_architectural_gate") {
			gates = append(gates, f)
		}
	}
	if len(generated) == 0 || len(gates) == 0 {
		return nil
	}
	supporting := append(append([]normalizedInvariantFact{}, generated...), gates...)
	return []extractedInvariantCandidate{invariantCandidateFromFacts(
		"candidate.invariant.generated_authority.fresh_artifacts",
		"Generated artifacts appear to be derived state that must remain fresh with their authored sources or configured gates.",
		"generated_authority",
		45,
		[]string{"generated artifact exists", "CI or repository command checks freshness/gates"},
		supporting,
		[]string{"Identify canonical authored inputs.", "Run freshness check or configured gate before accepting changes."},
	)}
}

func synthesizeNegativeHistoryCandidates(root string, facts []normalizedInvariantFact) []extractedInvariantCandidate {
	var hist, gates []normalizedInvariantFact
	for _, f := range facts {
		if f.Kind == "historical_removal" {
			hist = append(hist, f)
		}
		if f.Kind == "ci_gate" && f.Predicate == "runs_scanner" {
			gates = append(gates, f)
		}
	}
	if len(hist) == 0 || len(gates) == 0 {
		return nil
	}
	supporting := append(append([]normalizedInvariantFact{}, hist...), gates...)
	return []extractedInvariantCandidate{invariantCandidateFromFacts(
		"candidate.invariant.negative.repeated_removed_pattern",
		"A historically removed or forbidden pattern appears to be protected by a scanner; reintroducing it is likely regressive.",
		"negative",
		70,
		[]string{"historical removal evidence", "scanner or CI enforcement evidence"},
		supporting,
		[]string{"Inspect commit diffs, not only commit subjects.", "Confirm scanner covers the proposed scope."},
	)}
}

func invariantCandidateFromFacts(id, statement, kind string, score int, explanation []string, facts []normalizedInvariantFact, proof []string) extractedInvariantCandidate {
	score = invariantClamp(score, 0, 100)
	files := map[string]bool{}
	symbols := map[string]bool{}
	tests := []string{}
	guards := []string{}
	schemas := []string{}
	gates := []string{}
	commits := []string{}
	docs := []string{}
	factIDs := []string{}
	repos := map[string]bool{}
	for _, f := range facts {
		factIDs = append(factIDs, f.ID)
		for _, file := range f.Scope.Files {
			files[file] = true
		}
		for _, sym := range f.Scope.Symbols {
			symbols[sym] = true
		}
		if f.Scope.Repository != "" {
			repos[f.Scope.Repository] = true
		}
		switch f.Kind {
		case "assertion":
			if f.Evidence.TestName != "" {
				tests = append(tests, f.Evidence.SourceFile+":"+f.Evidence.TestName)
			}
		case "guard", "transition", "generation_check":
			guards = append(guards, f.ID)
		case "schema_constraint":
			schemas = append(schemas, f.ID)
		case "ci_gate":
			gates = append(gates, f.Evidence.SourceFile)
		case "historical_removal":
			commits = append(commits, firstNonEmpty(f.Evidence.Commit, f.Object))
		case "documentation_claim":
			docs = append(docs, f.Evidence.SourceFile)
		}
	}
	return extractedInvariantCandidate{
		ID:        id,
		Statement: statement,
		Kind:      kind,
		Status:    "candidate",
		Confidence: extractedInvariantConfidence{
			Level:       invariantConfidenceLevel(score),
			Score:       score,
			Explanation: dedupeSorted(explanation),
		},
		Scope: invariantCandidateScope{
			Repositories: sortedMapKeys(repos),
			Files:        sortedMapKeys(files),
			Symbols:      sortedMapKeys(symbols),
		},
		Authority: invariantCandidateAuthority{
			Owner:     "",
			Writers:   []string{},
			Consumers: []string{},
		},
		Evidence: invariantCandidateEvidence{
			Facts:         dedupeSorted(factIDs),
			Tests:         dedupeSorted(tests),
			Guards:        dedupeSorted(guards),
			Schemas:       dedupeSorted(schemas),
			Gates:         dedupeSorted(gates),
			Commits:       dedupeSorted(commits),
			Documentation: dedupeSorted(docs),
		},
		Contradictions:          []string{},
		AlternativeExplanations: []string{"The evidence may describe local behavior rather than an architectural invariant."},
		Unknowns:                []string{},
		ProofObligations:        dedupeSorted(proof),
		Promotion: invariantPromotionDisposition{
			Eligible: false,
			Missing:  []string{"architectural owner approval", "reviewed scope", "explicit promotion into governed awareness"},
		},
	}
}

func renderInvariantExtractionReport(report invariantExtractionReport, format string) ([]byte, error) {
	report.GeneratedAt = "deterministic"
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "json":
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	case "yaml", "yml":
		return yaml.Marshal(report)
	default:
		return nil, fmt.Errorf("--format must be json or yaml")
	}
}

func normalizeInvariantFacts(root string, facts []normalizedInvariantFact) ([]normalizedInvariantFact, error) {
	return architecture.NormalizeFacts(root, facts)
}

func invariantFact(identity invariantRepositoryIdentity, kind, subject, predicate, object, file, symbol string, lineStart, lineEnd int, command, extractor string, confidence float64, meta map[string]string) normalizedInvariantFact {
	return invariantFactWithProvenance(identity, kind, subject, predicate, object, file, symbol, lineStart, lineEnd, command, extractor, confidence, meta, architecture.Options{RevisionStatus: architecture.RevisionNotRequested})
}

func invariantFactWithProvenance(identity invariantRepositoryIdentity, kind, subject, predicate, object, file, symbol string, lineStart, lineEnd int, command, extractor string, confidence float64, meta map[string]string, provenance architecture.Options) normalizedInvariantFact {
	ev := invariantFactEvidence{SourceFile: file, LineStart: lineStart, LineEnd: lineEnd, Command: command}
	if strings.HasPrefix(symbol, "Test") || strings.Contains(symbol, ".Test") {
		ev.TestName = lastSegment(symbol)
	}
	if meta != nil {
		ev.Commit = meta["commit"]
	}
	if provenance.RevisionStatus == "" {
		provenance.RevisionStatus = architecture.RevisionNotRequested
	}
	provenance.Root = identity.Root
	provenance.RepositoryDomain = identity.Domain
	provenance.RepositoryDomainStatus = identity.DomainStatus
	f, _ := architecture.NewFact(normalizedInvariantFact{
		ID:        architecture.StableID(kind, subject, predicate, object, file, lineStart, extractor),
		Kind:      kind,
		Subject:   subject,
		Predicate: predicate,
		Object:    strings.TrimSpace(object),
		Scope: invariantFactScope{
			Repository: identity.Repository,
			Files:      nonEmptySlice(file),
			Symbols:    nonEmptySlice(symbol),
		},
		Evidence:   ev,
		Confidence: confidence,
		Extractor:  extractor,
		Meta:       meta,
	}, provenance)
	return f
}

func testAssertionFact(identity invariantRepositoryIdentity, rel, symbol string, fn *ast.FuncDecl, fset *token.FileSet) normalizedInvariantFact {
	words := splitCamel(strings.TrimPrefix(fn.Name.Name, "Test"))
	predicate := "asserts_behavior_example"
	conf := 0.35
	if anyRuleToken(words) {
		predicate = "asserts_architectural_rule"
		conf = 0.75
	}
	return invariantFact(identity, "assertion", symbol, predicate, humanizeWords(words), rel, symbol, fset.Position(fn.Pos()).Line, fset.Position(fn.End()).Line, "", "go_test_extractor", conf, nil)
}

func invariantBlockReturnsError(block *ast.BlockStmt) bool {
	if block == nil {
		return false
	}
	for _, stmt := range block.List {
		switch s := stmt.(type) {
		case *ast.ReturnStmt:
			return true
		case *ast.ExprStmt:
			call, ok := s.X.(*ast.CallExpr)
			if ok {
				name := strings.ToLower(invariantCallName(call.Fun))
				if strings.Contains(name, "fatal") || strings.Contains(name, "error") || strings.Contains(name, "fail") {
					return true
				}
			}
		}
	}
	return false
}

func invariantExprString(fset *token.FileSet, n ast.Node) string {
	if n == nil {
		return ""
	}
	var b bytes.Buffer
	_ = printer.Fprint(&b, fset, n)
	return strings.TrimSpace(b.String())
}

func invariantRel(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func invariantSelectorName(x *ast.SelectorExpr) string {
	if x == nil || x.Sel == nil {
		return ""
	}
	return x.Sel.Name
}

func invariantWriteTarget(n ast.Node) string {
	switch x := n.(type) {
	case *ast.SelectorExpr:
		return invariantSelectorName(x)
	case *ast.Ident:
		if x.Name == "_" {
			return ""
		}
		return x.Name
	default:
		return ""
	}
}

func invariantCallName(n ast.Node) string {
	switch x := n.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		if x.Sel != nil {
			return x.Sel.Name
		}
	}
	return ""
}

func invariantLooksLikePersistenceCall(name string) bool {
	lower := strings.ToLower(name)
	for _, token := range []string{"save", "store", "persist", "write", "update", "set", "put", "create", "delete", "commit"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func invariantLooksLikeGeneration(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, "generation") || strings.Contains(lower, "revision") || strings.Contains(lower, "epoch")
}

func invariantLooksLikeTransition(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, "state") || strings.Contains(lower, "status") || strings.Contains(lower, "transition")
}

func invariantNormativeText(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	if lower == "" {
		return false
	}
	for _, token := range []string{" must ", " never ", " only ", " required ", " forbidden ", "cannot ", " fails closed", " do not ", " should not "} {
		if strings.Contains(" "+lower+" ", token) {
			return true
		}
	}
	return false
}

func invariantCompact(s string) string {
	fields := strings.Fields(s)
	if len(fields) > 40 {
		fields = fields[:40]
	}
	return strings.Join(fields, " ")
}

func firstScannerCommand(text string) string {
	for _, line := range strings.Split(text, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "grep") || strings.Contains(lower, "source-check") || strings.Contains(lower, "forbidden") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func invariantFirstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func factsByKindPredicate(facts []normalizedInvariantFact, kind, predicate string) []normalizedInvariantFact {
	var out []normalizedInvariantFact
	for _, f := range facts {
		if f.Kind == kind && f.Predicate == predicate {
			out = append(out, f)
		}
	}
	return out
}

func relatedTests(f normalizedInvariantFact, tests []normalizedInvariantFact) []normalizedInvariantFact {
	var out []normalizedInvariantFact
	src := ""
	if len(f.Scope.Files) > 0 {
		src = f.Scope.Files[0]
	}
	testSibling := ""
	if strings.HasSuffix(src, ".go") && !strings.HasSuffix(src, "_test.go") {
		testSibling = strings.TrimSuffix(src, ".go") + "_test.go"
	}
	for _, t := range tests {
		if len(t.Scope.Files) == 0 {
			continue
		}
		tf := t.Scope.Files[0]
		if tf == src || tf == testSibling || strings.TrimSuffix(tf, "_test.go") == strings.TrimSuffix(src, ".go") {
			out = append(out, t)
		}
	}
	return out
}

func uniqueFactSubjects(facts []normalizedInvariantFact) []string {
	seen := map[string]bool{}
	for _, f := range facts {
		if f.Subject != "" {
			seen[f.Subject] = true
		}
	}
	return sortedMapKeys(seen)
}

func authorityCorroborationFacts(resource string, facts []normalizedInvariantFact) []normalizedInvariantFact {
	var out []normalizedInvariantFact
	lowerResource := strings.ToLower(resource)
	for _, f := range facts {
		switch f.Kind {
		case "guard", "assertion", "ci_gate", "historical_removal", "documentation_claim":
			haystack := strings.ToLower(strings.Join([]string{f.Subject, f.Predicate, f.Object}, " "))
			if !strings.Contains(haystack, lowerResource) {
				continue
			}
			out = append(out, f)
		}
	}
	return out
}

func mergeInvariantCandidates(in []extractedInvariantCandidate) []extractedInvariantCandidate {
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].ID != in[j].ID {
			return in[i].ID < in[j].ID
		}
		return in[i].Statement < in[j].Statement
	})
	byID := map[string]extractedInvariantCandidate{}
	for _, c := range in {
		if existing, ok := byID[c.ID]; ok {
			existing.Evidence.Facts = dedupeSorted(append(existing.Evidence.Facts, c.Evidence.Facts...))
			existing.Evidence.Tests = dedupeSorted(append(existing.Evidence.Tests, c.Evidence.Tests...))
			existing.Evidence.Guards = dedupeSorted(append(existing.Evidence.Guards, c.Evidence.Guards...))
			existing.Evidence.Schemas = dedupeSorted(append(existing.Evidence.Schemas, c.Evidence.Schemas...))
			existing.Evidence.Gates = dedupeSorted(append(existing.Evidence.Gates, c.Evidence.Gates...))
			existing.Evidence.Commits = dedupeSorted(append(existing.Evidence.Commits, c.Evidence.Commits...))
			existing.Evidence.Documentation = dedupeSorted(append(existing.Evidence.Documentation, c.Evidence.Documentation...))
			existing.Contradictions = dedupeSorted(append(existing.Contradictions, c.Contradictions...))
			existing.ProofObligations = dedupeSorted(append(existing.ProofObligations, c.ProofObligations...))
			if c.Confidence.Score > existing.Confidence.Score {
				existing.Confidence = c.Confidence
			}
			byID[c.ID] = existing
			continue
		}
		byID[c.ID] = c
	}
	out := make([]extractedInvariantCandidate, 0, len(byID))
	for _, c := range byID {
		out = append(out, c)
	}
	return out
}

func filterInvariantCandidates(candidates []extractedInvariantCandidate, min string) []extractedInvariantCandidate {
	minRank := invariantConfidenceRank(min)
	var out []extractedInvariantCandidate
	for _, c := range candidates {
		if invariantConfidenceRank(c.Confidence.Level) >= minRank {
			out = append(out, c)
		}
	}
	return out
}

func sortInvariantCandidates(candidates []extractedInvariantCandidate) {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Confidence.Score != candidates[j].Confidence.Score {
			return candidates[i].Confidence.Score > candidates[j].Confidence.Score
		}
		return candidates[i].ID < candidates[j].ID
	})
}

func invariantConfidenceLevel(score int) string {
	switch {
	case score >= 85:
		return "proven"
	case score >= 65:
		return "high"
	case score >= 35:
		return "medium"
	default:
		return "low"
	}
}

func invariantConfidenceRank(level string) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "proven":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	default:
		return 1
	}
}

func lastSegment(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[i+1:]
	}
	return s
}

func sortedMapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		if strings.TrimSpace(k) != "" {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func invariantShortHash(s string) string {
	return architecture.ShortHash(s)
}

func invariantClamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
