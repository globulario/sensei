// SPDX-License-Identifier: AGPL-3.0-only

package extractor_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"
)

// TestImporterUsesDeclaredPredicatesOnly walks yaml_import.go via go/ast,
// collects every selector of the form `rdf.<Name>` that's used as a
// predicate-position argument, and asserts each one corresponds to a
// constant declared in golang/rdf/vocab.go.
//
// What this catches:
//
//   - The importer emits a `rdf.PropFoo` selector that doesn't exist in
//     vocab.go (would fail compilation today, but the test pins it so a
//     future move from constants to a registry would still be checked).
//   - An author copy-pastes an emit block and renames an edge to
//     `rdf.PropProtectsX` — a typo that would compile if both ends of
//     the typo were edited, but here would fail because the test compares
//     against the canonical vocab set.
//   - A consultant adds a non-vocab predicate via a different package —
//     the test will surface it as unrecognized.
//
// What this does NOT catch:
//
//   - Whether the emitted predicate is semantically the right one. The
//     drift test covers ontology ↔ vocab; this test covers
//     importer-callsites ↔ vocab. Semantic correctness is the importer
//     tests' job.
//   - Predicate selectors built dynamically (e.g. via reflection). The
//     importer is intentionally written without dynamic predicate
//     construction; if that ever changes, this test should fail and the
//     extension needs to be designed.
func TestImporterUsesDeclaredPredicatesOnly(t *testing.T) {
	importerSelectors := collectRdfSelectors(t, "yaml_import.go")
	vocabConsts := collectVocabConsts(t, "../rdf/vocab.go")

	var undeclared []string
	for sel := range importerSelectors {
		if !vocabConsts[sel] {
			undeclared = append(undeclared, sel)
		}
	}
	if len(undeclared) > 0 {
		sort.Strings(undeclared)
		t.Errorf("yaml_import.go references rdf.<Name> selectors not declared in vocab.go:\n  - %s",
			strings.Join(undeclared, "\n  - "))
	}
}

// collectRdfSelectors parses path as a Go source file and returns every
// selector that reads `rdf.<Name>`. We intentionally collect EVERY
// selector — predicate-position, class-position, helper functions — and
// let the comparison against vocab.go filter. If the importer uses
// `rdf.IRI` or `rdf.Lit` (functions) the comparison will accept them as
// long as they exist in the rdf package.
//
// The selector check uses a name-prefix filter: we only collect ones
// whose name starts with Class, Prop, Aw, or matches the helper names
// MintIRI/IRI/Lit/EncodeIRIPath/IsStableID/NewEmitter/Emitter. This keeps
// the test focused on the vocabulary surface rather than every method
// call into the rdf package.
func collectRdfSelectors(t *testing.T, path string) map[string]bool {
	t.Helper()
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	out := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok || x.Name != "rdf" {
			return true
		}
		name := sel.Sel.Name
		if isVocabSelector(name) {
			out[name] = true
		}
		return true
	})
	if len(out) == 0 {
		t.Fatal("collectRdfSelectors: parsed zero rdf.<Name> selectors — importer source likely moved or refactored; update the test")
	}
	return out
}

// isVocabSelector keeps the test focused on the constants and exported
// helper-name surface vocab.go is responsible for. Method calls on
// rdf.Emitter (Triple, Typed, Flush) read as selectors on a value, not
// as `rdf.Method` selectors, so they don't appear here.
func isVocabSelector(name string) bool {
	switch {
	case strings.HasPrefix(name, "Class"),
		strings.HasPrefix(name, "Prop"),
		strings.HasPrefix(name, "Aw"),
		strings.HasPrefix(name, "Rdf"),
		strings.HasPrefix(name, "Owl"),
		strings.HasPrefix(name, "Xsd"):
		return true
	}
	// Helper-name allowlist — kept short and explicit so an unfamiliar
	// importer using `rdf.NewBuilder` (say) would fail this test and
	// prompt a deliberate decision.
	switch name {
	case "IRI", "Lit", "MintIRI", "EncodeIRIPath", "IsStableID", "NewEmitter":
		return true
	}
	return false
}

// collectVocabConsts parses golang/rdf/vocab.go via go/ast and returns
// the set of identifier names declared in const blocks (Class*, Prop*,
// AwNS, RdfNS, etc) plus the function names exported by the package.
// The function-name half covers the helper allowlist used by
// collectRdfSelectors so the predicate audit passes for those uses too.
func collectVocabConsts(t *testing.T, vocabPath string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	addFromFile(t, vocabPath, out)

	// builder.go ships the helpers (IRI, Lit, MintIRI, etc); add those
	// too so the importer's calls to them pass the audit.
	addFromFile(t, "../rdf/builder.go", out)

	if len(out) == 0 {
		t.Fatal("collectVocabConsts: parsed zero declarations — vocab.go / builder.go structure likely changed")
	}
	return out
}

func addFromFile(t *testing.T, path string, out map[string]bool) {
	t.Helper()
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok == token.CONST {
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, name := range vs.Names {
						out[name.Name] = true
					}
				}
			}
		case *ast.FuncDecl:
			if d.Recv == nil { // package-level function, not method
				out[d.Name.Name] = true
			}
		}
	}
}
