// SPDX-License-Identifier: Apache-2.0

// Package importgraph extracts a language's import dependency graph as
// architecture Component edges.
//
// It is the first implementation of a generic AWG extraction pattern:
//
//	language parser -> normalized import facts -> component dependency edges ->
//	optional classifier upgrades -> deterministic generated YAML.
//
// The pipeline is split into a LANGUAGE-SPECIFIC parser and a LANGUAGE-NEUTRAL
// core. A parser (see golang.go) knows one language's syntax and resolution and
// emits normalized ImportFacts; it never decides components, edges, or meaning.
// This shared core rolls source files up to Components, turns internal
// cross-component imports into aw:dependsOn edges, applies an optional
// config-driven classifier, and renders deterministic YAML — identically for
// every language.
//
// Design law: AWG core extracts STRUCTURE; classifier config maps PROJECT
// CONVENTIONS; humans author INTENT. A language extractor may know language
// syntax; it may not know project-specific meaning. Every emitted node/edge is
// assertion: inferred — observable facts, never a claim about "why".
package importgraph

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// Version is stamped into the generated header so a regenerated file is
// recognisably the product of a given extractor revision.
const Version = "1"

// ── normalized model (language-neutral) ─────────────────────────────────────

// ImportFact is what a language parser emits per import statement. It is the
// single contract between the language-specific layer and the shared core.
type ImportFact struct {
	Language   string // "go" | "typescript" | "python" | "rust" | ...
	SourceFile string // repo-relative path (forward slashes) of the importing file
	Raw        string // the raw import / module / package string as written
	Kind       string // static | side_effect | require | dynamic | ...
	Resolved   string // stdlib | internal | external | unresolved
	TargetPath string // repo-relative package dir of the import; set only when Resolved=="internal"
}

// SourceFile is a scanned file. IsEntrypoint marks a language entrypoint
// (e.g. Go main.go) so its component is classified as a service.
type SourceFile struct {
	Path         string
	IsEntrypoint bool
}

// ParseResult is the full output of a language parser for one repo.
type ParseResult struct {
	Files   []SourceFile
	Imports []ImportFact
}

// Parser scans a repo root and returns its files + import facts for one language.
type Parser func(root string) (ParseResult, error)

var parsers = map[string]Parser{}

// register wires a language parser into the shared core (called from each
// language file's init). Languages: see the parsers map.
func register(language string, p Parser) { parsers[language] = p }

// Languages returns the registered language names (sorted).
func Languages() []string {
	out := make([]string, 0, len(parsers))
	for l := range parsers {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// ── classifier (language-neutral mechanism) ─────────────────────────────────

// validEdges maps a rule's edge keyword to the components: YAML field that
// carries it (and, via the importer, the RDF predicate). exposes_contracts
// targets a Contract; the others target a Component.
var validEdges = map[string]bool{
	"depends_on": true, "reads_from": true, "writes_to": true, "exposes_contracts": true,
}

// Rule is one classifier rule. It upgrades a raw import (matched by regexp) into
// a semantic edge to a templated target — the only place project-specific
// conventions live. A rule applies only to its declared language.
type Rule struct {
	ID          string `yaml:"id"`
	Language    string `yaml:"language"`
	Match       string `yaml:"match"`
	Edge        string `yaml:"edge"`
	Target      string `yaml:"target"`
	TargetClass string `yaml:"target_class,omitempty"` // component (default) | contract — informational
}

// Config is the optional, language-neutral classifier configuration.
type Config struct {
	Classifiers []Rule `yaml:"classifiers"`
}

type compiledRule struct {
	re     *regexp.Regexp
	edge   string
	target string
}

// LoadConfig reads a classifier config file.
func LoadConfig(p string) (Config, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse classifier config %s: %w", p, err)
	}
	return cfg, nil
}

// compileFor returns the compiled rules that apply to a given language, in
// declaration order (first match wins). Go regexp is RE2 — no backreferences.
func (c Config) compileFor(language string) ([]compiledRule, error) {
	var out []compiledRule
	for i, r := range c.Classifiers {
		if r.Language != language {
			continue
		}
		if strings.TrimSpace(r.Match) == "" || strings.TrimSpace(r.Target) == "" {
			return nil, fmt.Errorf("classifier %d (%q): match and target are required", i, r.ID)
		}
		if !validEdges[r.Edge] {
			return nil, fmt.Errorf("classifier %d (%q): invalid edge %q (want depends_on|reads_from|writes_to|exposes_contracts)", i, r.ID, r.Edge)
		}
		re, err := regexp.Compile(r.Match)
		if err != nil {
			return nil, fmt.Errorf("classifier %d (%q): bad match regexp: %w", i, r.ID, err)
		}
		out = append(out, compiledRule{re: re, edge: r.Edge, target: r.Target})
	}
	return out, nil
}

// ── output model (reuses the components: schema → no importer changes) ───────

// UML is the optional UML profile block emitted on inferred components.
type UML struct {
	Kind       string `yaml:"kind"`
	Stereotype string `yaml:"stereotype"`
	View       string `yaml:"view"`
	Confidence string `yaml:"confidence"`
}

// Component mirrors the subset of the components: YAML the spine importer reads
// (importComponents), plus a review-only external_imports list the importer
// ignores. Field order is the YAML key order — keep it stable for determinism.
type Component struct {
	ID               string   `yaml:"id"`
	Name             string   `yaml:"name"`
	Kind             string   `yaml:"kind"`
	Assertion        string   `yaml:"assertion"`
	SourceFiles      []string `yaml:"source_files"`
	DependsOn        []string `yaml:"depends_on,omitempty"`
	ReadsFrom        []string `yaml:"reads_from,omitempty"`
	WritesTo         []string `yaml:"writes_to,omitempty"`
	ExposesContracts []string `yaml:"exposes_contracts,omitempty"`
	ExternalImports  []string `yaml:"external_imports,omitempty"` // review-only; importer ignores
	Uml              *UML     `yaml:"uml,omitempty"`
}

// Doc is the top-level components: document.
type Doc struct {
	Components []Component `yaml:"components"`
}

// ── component rollup (language-neutral) ──────────────────────────────────────

// knownSourceRoots: a dir one level under one of these rolls up to its own
// component; otherwise a top-level dir does. Language-neutral (paths, not code).
var knownSourceRoots = map[string]bool{
	"cmd": true, "internal": true, "pkg": true, "src": true, "lib": true,
	"app": true, "services": true, "golang": true, "packages": true,
	"apps": true, "modules": true, "crates": true, // crates: standard Rust workspace layout
}

func excludedDir(name string) bool {
	switch name {
	case "vendor", "node_modules", ".git", "dist", "build", "bin", "out",
		"third_party", "thirdparty", "generated", "candidates", ".awg",
		"testdata", "target", ".venv", "venv", "__pycache__", ".idea", ".vscode",
		// Illustrative sample trees are not the project's own architecture
		// (matches coldsource/surfaces.go's exclusion).
		"example", "examples":
		return true
	}
	return false
}

// isNestedRepo reports whether dir is its own repository boundary — it contains
// a .git entry (a submodule, vendored repo, or a CI-placed nested checkout).
// Such a subtree belongs to another repository and must not be scanned. The
// rule is generic: any nested .git, no project-specific directory names.
func isNestedRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// dotSlug turns a repo-relative dir into an id segment: "golang/server" ->
// "golang.server" (matches the bootstrap layout extractor's scheme).
func dotSlug(rel string) string {
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		var b strings.Builder
		for _, r := range strings.ToLower(p) {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				b.WriteRune(r)
			} else {
				b.WriteByte('_')
			}
		}
		parts[i] = strings.Trim(b.String(), "_")
	}
	return strings.Join(parts, ".")
}

// ComponentForFile returns the Component id that a repo-relative source file
// rolls up to, using the shared language-neutral rollup. ok=false for files
// that do not map to a component (the repo root, or a file directly under a
// source root). It lets sibling extractors attribute facts to the SAME
// component ids this package emits, so their edges land on real component nodes.
func ComponentForFile(relPath string) (id string, ok bool) {
	id, _, ok = componentForDir(path.Dir(relPath))
	return id, ok
}

// componentForDir rolls a repo-relative directory up to its Component id +
// canonical component directory. ok=false for paths that do not map to a
// component (repo root, or a file directly in a source root).
func componentForDir(dir string) (id, compDir string, ok bool) {
	dir = filepath.ToSlash(dir)
	if dir == "." || dir == "" {
		return "", "", false
	}
	segs := strings.Split(dir, "/")
	var compSegs []string
	if knownSourceRoots[segs[0]] {
		if len(segs) < 2 {
			return "", "", false // e.g. a file directly in golang/ — no component
		}
		compSegs = segs[:2]
	} else {
		compSegs = segs[:1]
	}
	compDir = strings.Join(compSegs, "/")
	id = "component." + dotSlug(compDir)
	if id == "component." {
		return "", "", false
	}
	return id, compDir, true
}

// ── resolver: ImportFacts -> Doc ─────────────────────────────────────────────

type agg struct {
	compDir   string
	isService bool
	sources   map[string]bool
	dependsOn map[string]bool
	readsFrom map[string]bool
	writesTo  map[string]bool
	exposes   map[string]bool
	external  map[string]bool
}

func newAgg(compDir string) *agg {
	return &agg{
		compDir: compDir, sources: map[string]bool{}, dependsOn: map[string]bool{},
		readsFrom: map[string]bool{}, writesTo: map[string]bool{},
		exposes: map[string]bool{}, external: map[string]bool{},
	}
}

func (a *agg) addEdge(edge, target string) {
	switch edge {
	case "depends_on":
		a.dependsOn[target] = true
	case "reads_from":
		a.readsFrom[target] = true
	case "writes_to":
		a.writesTo[target] = true
	case "exposes_contracts":
		a.exposes[target] = true
	}
}

// classify routes one fact: classifier rule (first match wins) → internal
// cross-component dependsOn → external_imports. stdlib/unresolved are dropped
// (never fatal). known is the set of component ids that have scanned source
// files; an inferred internal edge is added only when its target is one of
// them, so an import of a package that rolls up to no scanned component (e.g. a
// .pb.go-only proto package) never yields a dangling dependsOn edge. Classifier
// rule edges are NOT filtered — their targets may be hand-authored components
// the importer merges in later.
func (a *agg) classify(imp ImportFact, selfID string, rules []compiledRule, known map[string]bool) {
	switch imp.Resolved {
	case "stdlib", "unresolved", "":
		return
	}
	for _, r := range rules {
		if r.re.MatchString(imp.Raw) {
			a.addEdge(r.edge, r.re.ReplaceAllString(imp.Raw, r.target))
			return
		}
	}
	if imp.Resolved == "internal" {
		if tid, _, ok := componentForDir(imp.TargetPath); ok && tid != selfID && known[tid] {
			a.dependsOn[tid] = true
		}
		return
	}
	a.external[imp.Raw] = true // external
}

// Scan runs the registered parser for language, rolls its facts up to
// Components, applies the (language-filtered) classifier, and returns the Doc.
func Scan(root, language string, cfg Config) (Doc, error) {
	parse, ok := parsers[language]
	if !ok {
		return Doc{}, fmt.Errorf("importgraph: no parser registered for language %q (have %v)", language, Languages())
	}
	rules, err := cfg.compileFor(language)
	if err != nil {
		return Doc{}, err
	}
	res, err := parse(root)
	if err != nil {
		return Doc{}, err
	}

	comps := map[string]*agg{}
	ensure := func(id, compDir string) *agg {
		a := comps[id]
		if a == nil {
			a = newAgg(compDir)
			comps[id] = a
		}
		return a
	}
	for _, f := range res.Files {
		id, compDir, ok := componentForDir(path.Dir(f.Path))
		if !ok {
			continue
		}
		a := ensure(id, compDir)
		a.sources[f.Path] = true
		if f.IsEntrypoint {
			a.isService = true
		}
	}
	// The component ids that actually have scanned source files. Inferred edges
	// may only target these — every importing file already created its own
	// component above, so this set is complete before the imports pass.
	known := make(map[string]bool, len(comps))
	for id := range comps {
		known[id] = true
	}
	for _, imp := range res.Imports {
		id, compDir, ok := componentForDir(path.Dir(imp.SourceFile))
		if !ok {
			continue
		}
		ensure(id, compDir).classify(imp, id, rules, known)
	}
	return buildDoc(comps), nil
}

func buildDoc(comps map[string]*agg) Doc {
	ids := make([]string, 0, len(comps))
	for id := range comps {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	doc := Doc{Components: make([]Component, 0, len(ids))}
	for _, id := range ids {
		a := comps[id]
		kind := "module"
		if a.isService {
			kind = "service"
		}
		doc.Components = append(doc.Components, Component{
			ID:               id,
			Name:             path.Base(a.compDir),
			Kind:             kind,
			Assertion:        "inferred",
			SourceFiles:      sortedKeys(a.sources),
			DependsOn:        sortedKeys(a.dependsOn),
			ReadsFrom:        sortedKeys(a.readsFrom),
			WritesTo:         sortedKeys(a.writesTo),
			ExposesContracts: sortedKeys(a.exposes),
			ExternalImports:  sortedKeys(a.external),
			Uml:              &UML{Kind: "Component", Stereotype: kind, View: "structural", Confidence: "inferred"},
		})
	}
	return doc
}

func sortedKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Render produces the deterministic generated YAML for one language. The header
// + encoding are byte-stable so CI freshness gates can compare committed output.
func Render(doc Doc, language string) ([]byte, error) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "# GENERATED by cmd/import-scan -lang %s — DO NOT EDIT.\n", language)
	buf.WriteString("# Run `make import-graph` to regenerate from source imports.\n")
	fmt.Fprintf(&buf, "# Component dependency edges inferred from %s imports (assertion: inferred). importgraph v%s.\n", language, Version)
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	enc.Close()
	return buf.Bytes(), nil
}
