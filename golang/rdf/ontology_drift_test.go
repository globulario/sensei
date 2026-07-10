// SPDX-License-Identifier: Apache-2.0

package rdf

// ontology_drift_test.go — the CI check that vocab.go's own header comment has
// promised since v0: every aw: class/property constant in this package must be
// declared in ontology/awareness.ttl, and vice versa. Enforces
// invariant:awareness.ontology_vocab_importer_drift_forbidden — drift between
// the Turtle source of truth and the Go mirror is exactly the silent breakage
// the graph warns about, and until this test existed nothing guarded it.
//
// The Go side is read via go/ast (constant declarations of the form
// `Name = AwNS + "local"`), not regex, so formatting changes can't fool it.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// vocabConstants parses vocab.go and returns localName → constName for aw:
// classes (Class*) and properties (Prop*).
func vocabConstants(t *testing.T) (classes, props map[string]string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "vocab.go", nil, 0)
	if err != nil {
		t.Fatalf("parse vocab.go: %v", err)
	}
	classes, props = map[string]string{}, map[string]string{}

	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
				continue
			}
			name := vs.Names[0].Name
			be, ok := vs.Values[0].(*ast.BinaryExpr)
			if !ok || be.Op != token.ADD {
				continue
			}
			ident, ok := be.X.(*ast.Ident)
			if !ok || ident.Name != "AwNS" {
				continue
			}
			lit, ok := be.Y.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			local, err := strconv.Unquote(lit.Value)
			if err != nil {
				continue
			}
			switch {
			case strings.HasPrefix(name, "Class"):
				classes[local] = name
			case strings.HasPrefix(name, "Prop"):
				props[local] = name
			}
		}
	}
	return classes, props
}

// ttlDeclarations parses ontology/awareness.ttl and returns the declared aw:
// class and property local names.
func ttlDeclarations(t *testing.T) (classes, props map[string]bool) {
	t.Helper()
	data, err := os.ReadFile("../../ontology/awareness.ttl")
	if err != nil {
		t.Fatalf("read awareness.ttl: %v", err)
	}
	classRe := regexp.MustCompile(`(?m)^aw:(\w+)\s+a\s+owl:Class`)
	propRe := regexp.MustCompile(`(?m)^aw:(\w+)\s+a\s+owl:(?:Datatype|Object)Property`)
	classes, props = map[string]bool{}, map[string]bool{}
	for _, m := range classRe.FindAllStringSubmatch(string(data), -1) {
		classes[m[1]] = true
	}
	for _, m := range propRe.FindAllStringSubmatch(string(data), -1) {
		props[m[1]] = true
	}
	return classes, props
}

func TestOntologyVocabDriftForbidden(t *testing.T) {
	goClasses, goProps := vocabConstants(t)
	ttlClasses, ttlProps := ttlDeclarations(t)

	if len(goClasses) == 0 || len(goProps) == 0 || len(ttlClasses) == 0 || len(ttlProps) == 0 {
		t.Fatalf("parsed empty vocab sets (go classes=%d props=%d, ttl classes=%d props=%d) — parser broke, do not trust a green run",
			len(goClasses), len(goProps), len(ttlClasses), len(ttlProps))
	}

	for local, constName := range goClasses {
		if !ttlClasses[local] {
			t.Errorf("class aw:%s (%s) is in vocab.go but NOT declared in ontology/awareness.ttl — edit the .ttl first, then mirror", local, constName)
		}
	}
	for local := range ttlClasses {
		if _, ok := goClasses[local]; !ok {
			t.Errorf("class aw:%s is declared in ontology/awareness.ttl but has no Class constant in vocab.go", local)
		}
	}
	for local, constName := range goProps {
		if !ttlProps[local] {
			t.Errorf("property aw:%s (%s) is in vocab.go but NOT declared in ontology/awareness.ttl", local, constName)
		}
	}
	for local := range ttlProps {
		if _, ok := goProps[local]; !ok {
			t.Errorf("property aw:%s is declared in ontology/awareness.ttl but has no Prop constant in vocab.go", local)
		}
	}
}
