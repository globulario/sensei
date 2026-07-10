// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"

	"github.com/globulario/awareness-graph/golang/rdf"
	"github.com/globulario/awareness-graph/golang/store"
)

// codeSymbolFacts builds the CodeSymbolFacts rows for one symbol defined in a
// file, with a language and a set of outgoing aw:references edges (internal ids
// or "external:<name>"). Mirrors what importCodeSymbols + importCodeReferences
// emit for a SCIP-ingested symbol.
func codeSymbolFacts(id, file, language string, refs ...string) []store.ImpactFact {
	iri := mintedIRI(rdf.ClassCodeSymbol, id)
	out := []store.ImpactFact{
		{NodeIRI: iri, TypeIRI: rdf.ClassCodeSymbol, Predicate: rdf.PropLabel, Object: id},
		{NodeIRI: iri, TypeIRI: rdf.ClassCodeSymbol, Predicate: rdf.PropDefinedInFile, Object: mintedIRI(rdf.ClassSourceFile, file), ObjectIsIRI: true},
		{NodeIRI: iri, TypeIRI: rdf.ClassCodeSymbol, Predicate: rdf.PropLanguage, Object: language},
	}
	for _, r := range refs {
		out = append(out, store.ImpactFact{
			NodeIRI: iri, TypeIRI: rdf.ClassCodeSymbol,
			Predicate: rdf.PropReferences,
			Object:    mintedIRI(rdf.ClassCodeSymbol, r), ObjectIsIRI: true,
		})
	}
	return out
}

// Impact now surfaces symbol-level nodes (functions defined in the file) plus
// the symbols they reference — the Phase 2/3 SCIP ingestion payoff.
func TestImpact_SurfacesSymbolsAndReferences(t *testing.T) {
	symFacts := codeSymbolFacts("command/issue.go:issueClose", "command/issue.go", "go",
		"external:Fprintf", "external:colorableErr")
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return nil, nil // no architectural anchors — isolate the symbol surface
		},
		codeSymbolFacts: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return symFacts, nil
		},
	})

	resp, _, _, err := s.collectImpact(context.Background(), "command/issue.go", "")
	if err != nil {
		t.Fatalf("collectImpact: %v", err)
	}
	if len(resp.GetSymbols()) != 1 {
		t.Fatalf("want 1 symbol, got %d: %+v", len(resp.GetSymbols()), resp.GetSymbols())
	}
	sym := resp.GetSymbols()[0]
	if sym.GetId() != "command/issue.go:issueClose" {
		t.Errorf("symbol id = %q", sym.GetId())
	}
	if sym.GetFile() != "command/issue.go" {
		t.Errorf("symbol file = %q, want command/issue.go", sym.GetFile())
	}
	if sym.GetLanguage() != "go" {
		t.Errorf("symbol language = %q, want go", sym.GetLanguage())
	}
	refs := map[string]bool{}
	for _, r := range sym.GetReferences() {
		refs[r] = true
	}
	if !refs["external:Fprintf"] || !refs["external:colorableErr"] {
		t.Errorf("references = %v, want external:Fprintf and external:colorableErr", sym.GetReferences())
	}
}
