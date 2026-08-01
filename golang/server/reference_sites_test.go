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
		if len(focused) != 2 || focused[0].targeted || focused[1].targeted {
			t.Fatalf("ambiguous simple name must preserve two unfocused symbols: got %+v", focused)
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

type domainAwareCallerStore struct {
	fakeStore
	domains map[string][]string
}

func (s *domainAwareCallerStore) ClassNodeDomains(_ context.Context, classIRI string) (map[string][]string, error) {
	if classIRI != rdf.ClassCodeSymbol {
		return map[string][]string{}, nil
	}
	out := make(map[string][]string, len(s.domains))
	for iri, domains := range s.domains {
		out[iri] = append([]string(nil), domains...)
	}
	return out, nil
}

func TestFocusCodeSymbols_ReconcilesAnnotatedAndSCIPTwin(t *testing.T) {
	intentIRI := mintedIRI(rdf.ClassIntent, "server.briefing_contract")
	syms := []codeSymbol{
		{id: "golang/server/briefing.go:Briefing", label: "Briefing", language: "go", references: []string{"external:context.Context"}},
		{id: "golang/server/briefing.go:server.Briefing", label: "server.Briefing", language: "go", implements: []string{intentIRI}},
	}
	focused := focusCodeSymbolsForTask("change server.Briefing behavior", syms)
	if len(focused) != 1 || !focused[0].targeted || focused[0].label != "server.Briefing" {
		t.Fatalf("focused twins = %+v, want one targeted server.Briefing", focused)
	}
	wantIDs := []string{"golang/server/briefing.go:Briefing", "golang/server/briefing.go:server.Briefing"}
	if !reflect.DeepEqual(focused[0].lookupIDs, wantIDs) {
		t.Fatalf("lookup aliases = %v, want %v", focused[0].lookupIDs, wantIDs)
	}
	if !reflect.DeepEqual(focused[0].implements, []string{intentIRI}) || !reflect.DeepEqual(focused[0].references, []string{"external:context.Context"}) {
		t.Fatalf("twin evidence was not merged: %+v", focused[0])
	}
}

func TestFocusCodeSymbols_UniqueQualifiedLeafAlias(t *testing.T) {
	syms := []codeSymbol{{id: "render/json.go:JSON.Render", label: "JSON.Render"}}
	focused := focusCodeSymbolsForTask("change Render behavior", syms)
	if len(focused) != 1 || !focused[0].targeted {
		t.Fatalf("unique leaf alias did not focus JSON.Render: %+v", focused)
	}

	ambiguous := []codeSymbol{
		{id: "render/json.go:JSON.Render", label: "JSON.Render"},
		{id: "render/json.go:AsciiJSON.Render", label: "AsciiJSON.Render"},
	}
	got := focusCodeSymbolsForTask("change Render behavior", ambiguous)
	if len(got) != 2 || got[0].targeted || got[1].targeted {
		t.Fatalf("ambiguous leaf alias must preserve unfocused context: %+v", got)
	}
}

func TestAttachKnownStaticCallers_UsesEquivalentLookupIDs(t *testing.T) {
	bareID := "golang/server/briefing.go:Briefing"
	qualifiedID := "golang/server/briefing.go:server.Briefing"
	callerID := "golang/server/api.go:callBriefing"
	s := newServer(fakeStore{
		describeInbound: func(_ context.Context, iri string) ([]store.InboundTriple, error) {
			if iri == mintedIRI(rdf.ClassCodeSymbol, bareID) {
				return []store.InboundTriple{inboundRef(callerID)}, nil
			}
			if iri == mintedIRI(rdf.ClassCodeSymbol, qualifiedID) {
				return nil, nil
			}
			t.Fatalf("unexpected caller lookup IRI %q", iri)
			return nil, nil
		},
	})
	focused := focusCodeSymbolsForTask("change server.Briefing", []codeSymbol{
		{id: bareID, label: "Briefing"},
		{id: qualifiedID, label: "server.Briefing"},
	})
	got, err := s.attachKnownStaticCallers(context.Background(), focused, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !reflect.DeepEqual(got[0].knownCallers, []string{callerID}) {
		t.Fatalf("alias caller evidence = %+v, want %q", got, callerID)
	}
}

func TestReferencingSitesInScope_ExcludesForeignDomainCallers(t *testing.T) {
	const localDomain = "github.com/example/local"
	const foreignDomain = "github.com/example/foreign"
	targetID := "pkg/x.go:Foo"
	localCaller := "local/use.go:callFoo"
	foreignCaller := "foreign/use.go:callFoo"
	store := &domainAwareCallerStore{
		fakeStore: fakeStore{
			describeInbound: func(_ context.Context, iri string) ([]store.InboundTriple, error) {
				if iri != mintedIRI(rdf.ClassCodeSymbol, targetID) {
					t.Fatalf("lookup IRI = %q", iri)
				}
				return []store.InboundTriple{inboundRef(localCaller), inboundRef(foreignCaller)}, nil
			},
		},
		domains: map[string][]string{
			mintedIRI(rdf.ClassCodeSymbol, localCaller):   {localDomain},
			mintedIRI(rdf.ClassCodeSymbol, foreignCaller): {foreignDomain},
		},
	}
	s := newServer(store)
	s.homeDomain = localDomain
	got, err := s.referencingSitesInScope(context.Background(), targetID, localDomain)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{localCaller}) {
		t.Fatalf("scoped callers = %v, want only %q", got, localCaller)
	}
}
