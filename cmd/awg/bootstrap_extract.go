// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

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
