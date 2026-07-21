// SPDX-License-Identifier: AGPL-3.0-only

package rdf_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestVocabMatchesOntology asserts the set of aw: terms declared in
// ontology/awareness.ttl exactly matches the set of constants declared in
// golang/rdf/vocab.go. Drift between the two is a real authoring risk
// (we discovered it in the v0.0 review — aw:relatedInvariant lived in
// vocab.go after the ontology stopped wanting it), and the only reliable
// guard is a build-time text diff.
//
// What the test does:
//
//  1. Read ontology/awareness.ttl. Collect every term that appears in
//     subject position with rdf:type one of {owl:Class,
//     owl:ObjectProperty, owl:DatatypeProperty}. That gives the
//     authoritative set of aw: classes and properties.
//
//  2. Read golang/rdf/vocab.go via go/ast. Collect every const whose
//     value is AwNS + "<Name>" — that gives the Go-side set.
//
//  3. Symmetric-difference the two sets. Any difference is a hard fail
//     with a clear message naming the side missing the term.
//
// What the test does NOT do:
//
//   - Validate Turtle grammar. We aren't building a parser; we just look
//     for the prefix declaration patterns that the ontology uses today.
//     If the ontology layout changes substantially the test will start
//     missing terms — that's a deliberate trip-wire pointing at this test.
//   - Compare W3C-namespaced terms (rdf:type, rdfs:label, etc). They live
//     in vocab.go for use by emitters but the ontology only references
//     them via @prefix. Drift between W3C term constants and reality is
//     the W3C's problem, not ours.
func TestVocabMatchesOntology(t *testing.T) {
	ontologyTerms := readOntologyTerms(t, "../../ontology/awareness.ttl")
	vocabTerms := readVocabTerms(t, "vocab.go")

	if diff := termDiff(vocabTerms, ontologyTerms); diff != "" {
		t.Errorf("vocab.go declares terms missing from ontology/awareness.ttl:\n%s", diff)
	}
	if diff := termDiff(ontologyTerms, vocabTerms); diff != "" {
		t.Errorf("ontology/awareness.ttl declares terms missing from vocab.go:\n%s", diff)
	}
}

// readOntologyTerms parses Turtle the way a grep would: it looks for
// lines of the form `aw:<Name> a owl:<Kind> ...` where Kind is Class,
// ObjectProperty, or DatatypeProperty. That captures every term the
// ontology declares at the same level of formality. owl:Ontology header
// blocks and rdfs metadata are intentionally NOT collected — they're
// metadata about the ontology itself, not terms vocab.go would mirror.
func readOntologyTerms(t *testing.T, path string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	// Pattern matches lines like:
	//   aw:Foo       a owl:Class ;
	//   aw:fooBar    a owl:ObjectProperty ;
	//   aw:bazQux    a owl:DatatypeProperty ;
	// The regex tolerates arbitrary whitespace between the prefix-name
	// pair and the `a owl:<Kind>` clause.
	re := regexp.MustCompile(`(?m)^aw:([A-Za-z][A-Za-z0-9_]*)\s+a\s+owl:(Class|ObjectProperty|DatatypeProperty)\b`)
	out := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(string(data), -1) {
		out[m[1]] = true
	}
	if len(out) == 0 {
		t.Fatal("readOntologyTerms: parsed zero aw: terms — the ontology layout likely changed; update the regex in drift_test.go")
	}
	return out
}

// readVocabTerms parses vocab.go via go/ast and collects every const
// whose value expression looks like `AwNS + "<Name>"`. The const's name
// (e.g. ClassInvariant, PropAffects) is discarded — we extract the
// trailing string literal, which is the actual aw: term.
func readVocabTerms(t *testing.T, path string) map[string]bool {
	t.Helper()
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	out := map[string]bool{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, val := range vs.Values {
				name := extractAwTerm(val)
				if name != "" {
					out[name] = true
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("readVocabTerms: parsed zero AwNS+\"...\" constants — vocab.go structure likely changed")
	}
	return out
}

// extractAwTerm pattern-matches the AST shape `AwNS + "<Name>"` and
// returns Name. Returns "" for any other expression.
func extractAwTerm(e ast.Expr) string {
	be, ok := e.(*ast.BinaryExpr)
	if !ok || be.Op != token.ADD {
		return ""
	}
	id, ok := be.X.(*ast.Ident)
	if !ok || id.Name != "AwNS" {
		return ""
	}
	lit, ok := be.Y.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	s, err := strconvUnquote(lit.Value)
	if err != nil {
		return ""
	}
	return s
}

// strconvUnquote is a tiny local copy that handles the only quote forms
// vocab.go uses (double-quoted strings). Avoiding an explicit strconv
// import lets the test file stay focused.
func strconvUnquote(s string) (string, error) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", os.ErrInvalid
	}
	return s[1 : len(s)-1], nil
}

// termDiff returns a printable list of terms in `have` that are missing
// from `want`. Empty string when they match.
func termDiff(have, want map[string]bool) string {
	var missing []string
	for term := range have {
		if !want[term] {
			missing = append(missing, term)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	sort.Strings(missing)
	return "  - " + strings.Join(missing, "\n  - ")
}
