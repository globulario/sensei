// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"reflect"
	"testing"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
	"github.com/globulario/awareness-graph/golang/rdf"
	"github.com/globulario/awareness-graph/golang/store"
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
