// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/extractor/importgraph"
	yaml "gopkg.in/yaml.v3"
)

// Deterministic structural extractors for `sensei bootstrap`. These produce
// generated awareness YAML from repository structure — components from package
// layout, tests from test files. They are honest about provenance: every
// emitted Component carries assertion: inferred (derived from code, not
// hand-authored). No history, no LLM, no network.

// ── shared helpers ─────────────────────────────────────────────────────────

var sourceExts = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".py": true, ".rs": true, ".java": true, ".rb": true, ".kt": true,
	".swift": true, ".c": true, ".cc": true, ".cpp": true, ".h": true,
	".hpp": true, ".cs": true, ".scala": true, ".ex": true, ".clj": true,
	".php": true, ".m": true, ".mm": true,
}

func bootstrapExcludedDir(name string) bool {
	switch name {
	case "vendor", "node_modules", ".git", "dist", "build", "bin", "out",
		"third_party", "thirdparty", "generated", "candidates", ".sensei", ".awg",
		"testdata", "target", ".venv", "venv", "__pycache__", ".idea", ".vscode":
		return true
	}
	return false
}

func isSourceFile(name string) bool {
	if strings.HasSuffix(name, ".pb.go") || strings.HasSuffix(name, "_generated.go") ||
		strings.HasSuffix(name, ".d.ts") {
		return false
	}
	return sourceExts[strings.ToLower(filepath.Ext(name))]
}

func isTestFile(name string) bool {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, "_test.go"):
		return true
	case strings.HasSuffix(lower, ".test.ts") || strings.HasSuffix(lower, ".spec.ts"),
		strings.HasSuffix(lower, ".test.tsx") || strings.HasSuffix(lower, ".spec.tsx"),
		strings.HasSuffix(lower, ".test.js") || strings.HasSuffix(lower, ".spec.js"):
		return true
	case strings.HasPrefix(lower, "test_") && strings.HasSuffix(lower, ".py"),
		strings.HasSuffix(lower, "_test.py"):
		return true
	}
	return false
}

// dotSlug turns a repo-relative dir path into an id segment: "golang/server" ->
// "golang.server"; non-alphanumeric runs collapse to "_".
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

// ── component extraction ───────────────────────────────────────────────────

type bootstrapUML struct {
	Kind       string `yaml:"kind"`
	Stereotype string `yaml:"stereotype"`
	View       string `yaml:"view"`
	Confidence string `yaml:"confidence"`
}

type bootstrapComponent struct {
	ID          string        `yaml:"id"`
	Name        string        `yaml:"name"`
	Description string        `yaml:"description,omitempty"`
	Kind        string        `yaml:"kind"`
	Assertion   string        `yaml:"assertion"`
	SourceFiles []string      `yaml:"source_files"`
	DependsOn   []string      `yaml:"depends_on,omitempty"`
	ReadsFrom   []string      `yaml:"reads_from,omitempty"`
	WritesTo    []string      `yaml:"writes_to,omitempty"`
	Exposes     []string      `yaml:"exposes_contracts,omitempty"`
	External    []string      `yaml:"external_imports,omitempty"`
	Uml         *bootstrapUML `yaml:"uml,omitempty"`
}

type componentsDoc struct {
	Components []bootstrapComponent `yaml:"components"`
}

// dirScan summarises a candidate component directory.
type dirScan struct {
	hasSource bool
	repFile   string // repo-relative representative source file
	isService bool   // has an entrypoint (main.* / index.*)
}

func scanComponentDir(root, dir string) dirScan {
	var res dirScan
	best := ""
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != dir && bootstrapExcludedDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !isSourceFile(name) || isTestFile(name) {
			return nil
		}
		res.hasSource = true
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		// entrypoint detection → service kind + preferred representative.
		base := strings.ToLower(name)
		if base == "main.go" || base == "main.py" || base == "main.rs" ||
			base == "index.ts" || base == "index.js" || base == "main.java" {
			res.isService = true
			res.repFile = rel
		}
		if best == "" {
			best = rel
		}
		return nil
	})
	if res.repFile == "" {
		res.repFile = best
	}
	return res
}

// extractComponents derives a Component per top-level package/unit from the repo
// layout. Returns the records (for rendering) — caller decides whether to write.
func extractComponents(root string) []bootstrapComponent {
	sourceRoots := []string{"cmd", "internal", "pkg", "src", "lib", "app", "services", "golang", "packages", "apps", "modules"}
	srSet := map[string]bool{}
	for _, s := range sourceRoots {
		srSet[s] = true
	}

	var comps []bootstrapComponent
	seen := map[string]bool{}
	if rootComponent, err := importgraph.DetectGoRootComponent(root); err == nil && rootComponent != nil {
		sources := make([]string, 0, len(rootComponent.SourceFiles))
		kind := "module"
		for _, source := range rootComponent.SourceFiles {
			sources = append(sources, source.Path)
			if source.IsEntrypoint {
				kind = "service"
			}
		}
		comps = append(comps, bootstrapComponent{
			ID:          rootComponent.ID,
			Name:        rootComponent.Name,
			Description: "Inferred from root Go package: " + rootComponent.Name,
			Kind:        kind,
			Assertion:   "inferred",
			SourceFiles: sources,
			Uml:         &bootstrapUML{Kind: "Component", Stereotype: kind, View: "structural", Confidence: "inferred"},
		})
		seen[rootComponent.ID] = true
	}

	emitFor := func(dir string) {
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || rel == "" {
			return
		}
		id := "component." + dotSlug(rel)
		if id == "component." || seen[id] {
			return
		}
		sc := scanComponentDir(root, dir)
		if !sc.hasSource {
			return
		}
		seen[id] = true
		kind := "module"
		if sc.isService {
			kind = "service"
		}
		src := sc.repFile
		if src == "" {
			src = rel
		}
		comps = append(comps, bootstrapComponent{
			ID:          id,
			Name:        filepath.Base(rel),
			Description: "Inferred from repository layout: " + rel,
			Kind:        kind,
			Assertion:   "inferred",
			SourceFiles: []string{src},
			Uml:         &bootstrapUML{Kind: "Component", Stereotype: kind, View: "structural", Confidence: "inferred"},
		})
	}

	// Immediate children of the repo root that are NOT recognized source roots.
	if entries, err := os.ReadDir(root); err == nil {
		for _, e := range entries {
			if !e.IsDir() || bootstrapExcludedDir(e.Name()) || srSet[e.Name()] || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			emitFor(filepath.Join(root, e.Name()))
		}
	}
	// Children of recognized source roots (one level deeper).
	for _, sr := range sourceRoots {
		srDir := filepath.Join(root, sr)
		entries, err := os.ReadDir(srDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || bootstrapExcludedDir(e.Name()) {
				continue
			}
			emitFor(filepath.Join(srDir, e.Name()))
		}
	}

	sort.Slice(comps, func(i, j int) bool { return comps[i].ID < comps[j].ID })
	return comps
}

func mergeImportGraphComponents(base []bootstrapComponent, imported []importgraph.Component) []bootstrapComponent {
	byID := make(map[string]*bootstrapComponent, len(base))
	for i := range base {
		byID[base[i].ID] = &base[i]
	}
	for _, comp := range imported {
		dst := byID[comp.ID]
		if dst == nil {
			kind := comp.Kind
			if kind == "" {
				kind = "module"
			}
			dst = &bootstrapComponent{
				ID:          comp.ID,
				Name:        comp.Name,
				Description: "Inferred from source imports: " + comp.ID,
				Kind:        kind,
				Assertion:   "inferred",
				SourceFiles: append([]string(nil), comp.SourceFiles...),
				Uml:         &bootstrapUML{Kind: "Component", Stereotype: kind, View: "structural", Confidence: "inferred"},
			}
			base = append(base, *dst)
			byID[comp.ID] = &base[len(base)-1]
			dst = &base[len(base)-1]
		}
		dst.SourceFiles = mergeSortedStrings(dst.SourceFiles, comp.SourceFiles)
		dst.DependsOn = mergeSortedStrings(dst.DependsOn, comp.DependsOn)
		dst.ReadsFrom = mergeSortedStrings(dst.ReadsFrom, comp.ReadsFrom)
		dst.WritesTo = mergeSortedStrings(dst.WritesTo, comp.WritesTo)
		dst.Exposes = mergeSortedStrings(dst.Exposes, comp.ExposesContracts)
		dst.External = mergeSortedStrings(dst.External, comp.ExternalImports)
		if dst.Uml == nil {
			kind := dst.Kind
			if kind == "" {
				kind = "module"
			}
			dst.Uml = &bootstrapUML{Kind: "Component", Stereotype: kind, View: "structural", Confidence: "inferred"}
		}
	}
	sort.Slice(base, func(i, j int) bool { return base[i].ID < base[j].ID })
	return base
}

func mergeSortedStrings(a, b []string) []string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(a)+len(b))
	for _, s := range append(append([]string(nil), a...), b...) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// bootstrapCodeSymbol is the annotation-free subset accepted by the existing
// code_symbols importer. These declarations are structural facts only: no
// awareness edges are inferred without explicit annotations or a registry.
type bootstrapCodeSymbol struct {
	ID        string `yaml:"id"`
	Namespace string `yaml:"namespace"`
	Language  string `yaml:"language"`
	File      string `yaml:"file"`
	Symbol    string `yaml:"symbol"`
	Kind      string `yaml:"kind"`
	Component string `yaml:"component,omitempty"`
}

type bootstrapCodeSymbolsDoc struct {
	CodeSymbols []bootstrapCodeSymbol `yaml:"code_symbols"`
}

// goLibraryAPICandidate is a review-only description of a Go package's public
// surface. It deliberately does not use the governed Contract schema: an
// exported Go declaration is evidence of a library API, not an assertion about
// application behaviour.
type goLibraryAPICandidate struct {
	ID              string   `yaml:"id"`
	Name            string   `yaml:"name"`
	Kind            string   `yaml:"kind"`
	Status          string   `yaml:"status"`
	Assertion       string   `yaml:"assertion"`
	Description     string   `yaml:"description"`
	Package         string   `yaml:"package"`
	PublicSymbols   []string `yaml:"public_symbols"`
	ExtensionPoints []string `yaml:"extension_points,omitempty"`
	SourceFiles     []string `yaml:"source_files"`
}

type goLibraryAPICandidateDoc struct {
	LibraryAPIs []goLibraryAPICandidate `yaml:"library_apis"`
}

type goLibraryAPIContract struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Kind        string   `yaml:"kind"`
	Status      string   `yaml:"status"`
	Assertion   string   `yaml:"assertion"`
	SourceFiles []string `yaml:"source_files"`
}

type goLibraryAPIContractDoc struct {
	Contracts []goLibraryAPIContract `yaml:"contracts"`
}

func goLibraryAPIContracts(apis []goLibraryAPICandidate) []goLibraryAPIContract {
	out := make([]goLibraryAPIContract, 0, len(apis))
	for _, api := range apis {
		out = append(out, goLibraryAPIContract{
			ID:          "contract." + api.ID,
			Name:        api.Name,
			Description: "Candidate Go library API contract inferred from exported declarations; review required before it can govern behavior.",
			Kind:        "go_library_api",
			Status:      "candidate",
			Assertion:   "inferred",
			SourceFiles: api.SourceFiles,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// goLibraryAPIBoundaryCandidates makes the observed package surface visible to
// graph consumers without promoting it to a Contract. The detailed declaration
// inventory remains in the review-only library API candidate document.
func goLibraryAPIBoundaryCandidates(apis []goLibraryAPICandidate) []boundaryCandidate {
	out := make([]boundaryCandidate, 0, len(apis))
	for _, api := range apis {
		out = append(out, boundaryCandidate{
			ID:          "boundary." + api.ID,
			Name:        api.Name + " boundary",
			Kind:        "library_api",
			Status:      "candidate",
			Assertion:   "inferred",
			Description: "Candidate Go library API boundary inferred from exported declarations; it is structural evidence, not a governed application contract.",
			SourceFiles: api.SourceFiles,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// extractGoLibraryAPICandidates identifies externally importable Go packages
// with exported declarations. An exported interface becomes an extension-point
// candidate only when the semantic extractor has proven a local implementation.
// internal/ packages are intentionally omitted: their visibility boundary is
// already emitted separately and they are not public library API.
func extractGoLibraryAPICandidates(root string, facts []architecture.Fact) ([]goLibraryAPICandidate, error) {
	implemented := map[string]bool{}
	for _, fact := range facts {
		if fact.Predicate == "implements_interface" {
			implemented[fact.Object] = true
		}
	}
	type packageSurface struct {
		name       string
		files      []string
		symbols    []string
		interfaces []string
	}
	surfaces := map[string]*packageSurface{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path != root && (bootstrapExcludedDir(entry.Name()) || strings.HasPrefix(entry.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || isTestFile(entry.Name()) || !isSourceFile(entry.Name()) {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil || isUnderInternal(rel) {
			return nil
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if parseErr != nil || file.Name.Name == "main" {
			return nil
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == "." {
			dir = "root"
		}
		surface := surfaces[dir]
		if surface == nil {
			surface = &packageSurface{name: file.Name.Name}
			surfaces[dir] = surface
		}
		surface.files = append(surface.files, filepath.ToSlash(rel))
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if ast.IsExported(decl.Name.Name) {
					symbol := file.Name.Name + "." + decl.Name.Name
					if decl.Recv != nil && len(decl.Recv.List) > 0 {
						if receiver := goReceiverName(decl.Recv.List[0].Type); receiver != "" {
							symbol = file.Name.Name + "." + receiver + "." + decl.Name.Name
						}
					}
					surface.symbols = append(surface.symbols, symbol)
				}
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						if ast.IsExported(spec.Name.Name) {
							symbol := file.Name.Name + "." + spec.Name.Name
							surface.symbols = append(surface.symbols, symbol)
							if _, ok := spec.Type.(*ast.InterfaceType); ok && implemented[symbol] {
								surface.interfaces = append(surface.interfaces, symbol)
							}
						}
					case *ast.ValueSpec:
						for _, ident := range spec.Names {
							if ast.IsExported(ident.Name) {
								surface.symbols = append(surface.symbols, file.Name.Name+"."+ident.Name)
							}
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var out []goLibraryAPICandidate
	for dir, surface := range surfaces {
		symbols := dedupSorted(surface.symbols)
		if len(symbols) == 0 {
			continue
		}
		extensions := dedupSorted(surface.interfaces)
		name := surface.name
		if dir != "root" {
			name += " (" + dir + ")"
		}
		out = append(out, goLibraryAPICandidate{
			ID:              "library_api." + dotSlug(dir),
			Name:            name + " public API",
			Kind:            "go_library_api",
			Status:          "candidate",
			Assertion:       "inferred",
			Description:     "Exported Go declarations form this package's externally importable library API boundary.",
			Package:         surface.name,
			PublicSymbols:   symbols,
			ExtensionPoints: extensions,
			SourceFiles:     dedupSorted(surface.files),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func isUnderInternal(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == "internal" {
			return true
		}
	}
	return false
}

func extractGoCodeSymbols(root string, components []bootstrapComponent) ([]bootstrapCodeSymbol, error) {
	componentByFile := map[string]string{}
	for _, component := range components {
		for _, file := range component.SourceFiles {
			componentByFile[filepath.ToSlash(file)] = component.ID
		}
	}

	var symbols []bootstrapCodeSymbol
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path != root && (bootstrapExcludedDir(entry.Name()) || strings.HasPrefix(entry.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || isTestFile(name) || !isSourceFile(name) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil
		}
		pkg := file.Name.Name
		emit := func(name, kind string) {
			qualified := pkg + "." + name
			symbols = append(symbols, bootstrapCodeSymbol{
				ID:        rel + ":" + qualified,
				Namespace: pkg,
				Language:  "go",
				File:      rel,
				Symbol:    qualified,
				Kind:      kind,
				Component: componentByFile[rel],
			})
		}
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				name := decl.Name.Name
				kind := "function"
				if decl.Recv != nil && len(decl.Recv.List) > 0 {
					if recv := goReceiverName(decl.Recv.List[0].Type); recv != "" {
						name = recv + "." + name
					}
					kind = "method"
				}
				emit(name, kind)
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						emit(spec.Name.Name, "type")
					case *ast.ValueSpec:
						kind := "variable"
						if decl.Tok == token.CONST {
							kind = "constant"
						}
						for _, ident := range spec.Names {
							emit(ident.Name, kind)
						}
					}
				}
			}
		}
		return nil
	})
	sort.Slice(symbols, func(i, j int) bool { return symbols[i].ID < symbols[j].ID })
	return symbols, err
}

func goReceiverName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.StarExpr:
		return goReceiverName(expr.X)
	case *ast.IndexExpr:
		return goReceiverName(expr.X)
	case *ast.IndexListExpr:
		return goReceiverName(expr.X)
	default:
		return ""
	}
}

// ── test extraction ────────────────────────────────────────────────────────

type bootstrapTest struct {
	ID    string `yaml:"id"`
	Title string `yaml:"title"`
}

type testsDoc struct {
	RequiredTests []bootstrapTest `yaml:"required_tests"`
}

var goTestFuncRe = regexp.MustCompile(`(?m)^func (Test\w+)\s*\(`)

// extractTests derives required_tests from test files. For Go it lists each
// TestXxx function (id "path:TestName"); for other languages it lists the test
// file (file-level). Bounded to avoid pathological repos.
func extractTests(root string) []bootstrapTest {
	const cap = 5000
	var tests []bootstrapTest
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && bootstrapExcludedDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if len(tests) >= cap || !isTestFile(d.Name()) {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(d.Name(), "_test.go") {
			data, rerr := os.ReadFile(p)
			if rerr == nil {
				for _, m := range goTestFuncRe.FindAllStringSubmatch(string(data), -1) {
					if len(tests) >= cap {
						break
					}
					tests = append(tests, bootstrapTest{
						ID:    rel + ":" + m[1],
						Title: m[1] + " (" + rel + ")",
					})
				}
				return nil
			}
		}
		// Non-Go (or unreadable Go): file-level test entry.
		tests = append(tests, bootstrapTest{ID: rel, Title: "test file " + rel})
		return nil
	})
	sort.Slice(tests, func(i, j int) bool { return tests[i].ID < tests[j].ID })
	return tests
}

// ── rendering ──────────────────────────────────────────────────────────────

func renderGenerated(header string, doc any) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("# GENERATED by `sensei bootstrap` — DO NOT EDIT.\n")
	buf.WriteString("# " + header + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	enc.Close()
	return buf.Bytes(), nil
}
