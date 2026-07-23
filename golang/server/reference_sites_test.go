// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"reflect"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store"
)

// inboundRef builds one inbound aw:references triple: siteID references the
// queried target (siteID is the subject of the edge).
func inboundRef(siteID string) store.InboundTriple {
	return store.InboundTriple{
		Subject:   mintedIRI(rdf.ClassCodeSymbol, siteID),
		Predicate: rdf.PropReferences,
	}
}

// TestReferenceSites exercises the completeness primitive end to end over the
// RPC: it must return exactly the code symbols that reference each target via
// aw:references — filtering non-reference inbound edges, excluding the target
// itself, de-duplicating, skipping externals, and decoding IRIs back to
// "file:symbol" ids.
func TestReferenceSites(t *testing.T) {
	target := "pkg/x.go:Foo"
	targetIRI := mintedIRI(rdf.ClassCodeSymbol, target)

	inbound := map[string][]store.InboundTriple{
		targetIRI: {
			inboundRef("a.go:f1"),
			inboundRef("b.go:f2"),
			inboundRef("a.go:f1"), // duplicate — must collapse
			inboundRef(target),    // self-reference — must be excluded
			// a non-reference inbound edge (e.g. definedInFile) must be ignored
			{Subject: mintedIRI(rdf.ClassSourceFile, "pkg/x.go"), Predicate: rdf.PropDefinedInFile},
		},
	}

	s := newServer(fakeStore{
		describeInbound: func(_ context.Context, iri string) ([]store.InboundTriple, error) {
			return inbound[iri], nil
		},
	})

	resp, err := s.ReferenceSites(context.Background(), &awarenesspb.ReferenceSitesRequest{
		// "external:*" must be skipped entirely (no family emitted); the
		// duplicate target must be de-duplicated to a single family.
		SymbolIds: []string{target, "external:fmt.Sprintf", target},
	})
	if err != nil {
		t.Fatalf("ReferenceSites: %v", err)
	}
	if len(resp.GetFamilies()) != 1 {
		t.Fatalf("want 1 family (externals skipped, target deduped), got %d: %+v", len(resp.GetFamilies()), resp.GetFamilies())
	}
	fam := resp.GetFamilies()[0]
	if fam.GetSymbolId() != target {
		t.Errorf("family target = %q, want %q", fam.GetSymbolId(), target)
	}
	want := []string{"a.go:f1", "b.go:f2"}
	if !reflect.DeepEqual(fam.GetSiteIds(), want) {
		t.Errorf("site ids = %v, want %v (deduped, self excluded, non-ref filtered, sorted)", fam.GetSiteIds(), want)
	}
	if resp.GetAuthority() == nil {
		t.Error("response must carry a graph-authority stamp")
	}
}

func TestCodeSymbolIDFromIRI(t *testing.T) {
	id := "golang/server/server.go:Impact"
	iri := mintedIRI(rdf.ClassCodeSymbol, id)
	got, ok := codeSymbolIDFromIRI(iri)
	if !ok {
		t.Fatalf("codeSymbolIDFromIRI(%q) not ok", iri)
	}
	if got != id {
		t.Errorf("round-trip = %q, want %q", got, id)
	}
	if _, ok := codeSymbolIDFromIRI("not-an-awareness-iri"); ok {
		t.Error("non-awareness IRI must return ok=false")
	}

	t.Run("qualified task target excludes neighboring symbols", func(t *testing.T) {
		intentIRI := mintedIRI(rdf.ClassIntent, "render.json_preserves_http_contract")
		testIRI := mintedIRI(rdf.ClassTestSymbol, "render/json_test.go:TestJSONRender")
		syms := []codeSymbol{
			{id: "render/json.go:AsciiJSON.Render", label: "AsciiJSON.Render", language: "go"},
			{id: "render/json.go:JSON.Render", label: "JSON.Render", language: "go", implements: []string{intentIRI}, testedBy: []string{testIRI}},
		}

		focused := focusCodeSymbolsForTask("preserve JSON.Render response behavior", syms)
		if len(focused) != 1 || focused[0].id != "render/json.go:JSON.Render" || !focused[0].targeted {
			t.Fatalf("focused symbols = %+v, want exact targeted JSON.Render", focused)
		}
		if !reflect.DeepEqual(focused[0].implements, []string{intentIRI}) || !reflect.DeepEqual(focused[0].testedBy, []string{testIRI}) {
			t.Fatalf("target evidence was not preserved: %+v", focused[0])
		}
	})

	t.Run("ambiguous simple target preserves file context", func(t *testing.T) {
		syms := []codeSymbol{
			{id: "render/json.go:JSON.Render", label: "Render"},
			{id: "render/xml.go:XML.Render", label: "Render"},
		}
		focused := focusCodeSymbolsForTask("change Render behavior", syms)
		if !reflect.DeepEqual(focused, syms) {
			t.Fatalf("ambiguous simple name must preserve all symbols: got %+v", focused)
		}
	})

	t.Run("symbol boundaries prevent substring matches", func(t *testing.T) {
		syms := []codeSymbol{
			{id: "render/json.go:JSON.Render", label: "JSON.Render"},
			{id: "render/json.go:AsciiJSON.Render", label: "AsciiJSON.Render"},
		}
		focused := focusCodeSymbolsForTask("change AsciiJSON.Render", syms)
		if len(focused) != 1 || focused[0].id != "render/json.go:AsciiJSON.Render" {
			t.Fatalf("substring boundary selected the wrong symbol: %+v", focused)
		}
	})

	t.Run("go visibility is descriptive not API authority", func(t *testing.T) {
		if visibility, ok := goSymbolVisibility("go", "Context.Bind"); !ok || visibility != "exported" {
			t.Fatalf("Context.Bind visibility = %q, %v; want exported", visibility, ok)
		}
		if visibility, ok := goSymbolVisibility("go", "context.bind"); !ok || visibility != "unexported" {
			t.Fatalf("context.bind visibility = %q, %v; want unexported", visibility, ok)
		}
	})
}

// A store failure while reading inbound edges must surface as an error, never
// as an empty (falsely "complete") family. GraphFreshness stays current so the
// failure under test is the inbound read, not the authority gate.
func TestReferenceSites_StoreErrorSurfaces(t *testing.T) {
	s := newServer(fakeStore{
		describeInbound: func(_ context.Context, _ string) ([]store.InboundTriple, error) {
			return nil, context.DeadlineExceeded
		},
	})
	_, err := s.ReferenceSites(context.Background(), &awarenesspb.ReferenceSitesRequest{
		SymbolIds: []string{"pkg/x.go:Foo"},
	})
	if err == nil {
		t.Fatal("want an error when the inbound read fails, got nil")
	}
}
