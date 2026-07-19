// SPDX-License-Identifier: AGPL-3.0-only

// Package repograph owns the canonical repository-scoped graph projection:
// deterministic compile + stamp of the governed sources, atomic persistence of
// .sensei/project/graph.nt and its .sensei/graph-authority.json marker,
// independent reload + verification from disk, and a narrow read-only store.Store
// adapter over the persisted graph so Phase-8 provenance queries run offline with
// the same semantics as the normal graph owner.
//
// It never writes the combined embedded seed (golang/server/embeddata/awareness.nt)
// — that remains a separate cross-repository convergence obligation — and it never
// calls a cmd/awg handler. This sub-slice builds and verifies the graph-owner
// primitive; it establishes no promotion receipt, journal, or graph_verified claim
// (those belong to the later promotion transaction).
package repograph

import (
	"bytes"
	"context"
	"io"
	"os"

	"github.com/globulario/sensei/golang/architecture/graphsnapshot"
	"github.com/globulario/sensei/golang/store"
)

// rdfType is the RDF type predicate; type edges populate the class index rather
// than the inbound index, matching the normal seed store's semantics.
const rdfType = "http://www.w3.org/1999/02/22-rdf-syntax-ns#type"

// Graph is a read-only, in-process store.Store over a persisted repository graph.
// It parses N-Triples through graphsnapshot (the canonical reader the repo-scoped
// store consumers use) and indexes them exactly as the normal seed store does —
// it does not introduce a second RDF interpretation. It also satisfies
// seedmeta.VerifierStore so the persisted graph can be verified through the same
// freshness/authority semantics as the live store.
type Graph struct {
	bySubject map[string][]store.Triple
	byObject  map[string][]store.InboundTriple
	byClass   map[string]map[string]struct{}
	total     int
}

// ReadGraph builds the adapter from N-Triples bytes.
func ReadGraph(r io.Reader) (*Graph, error) {
	triples, err := graphsnapshot.Read(r)
	if err != nil {
		return nil, err
	}
	g := &Graph{
		bySubject: map[string][]store.Triple{},
		byObject:  map[string][]store.InboundTriple{},
		byClass:   map[string]map[string]struct{}{},
	}
	for _, t := range triples {
		g.bySubject[t.Subject] = append(g.bySubject[t.Subject], store.Triple{
			Predicate: t.Predicate, Object: t.Object, ObjectIsIRI: t.ObjectIsIRI,
		})
		g.total++
		if !t.ObjectIsIRI {
			continue
		}
		if t.Predicate == rdfType {
			set := g.byClass[t.Object]
			if set == nil {
				set = map[string]struct{}{}
				g.byClass[t.Object] = set
			}
			set[t.Subject] = struct{}{}
			continue
		}
		g.byObject[t.Object] = append(g.byObject[t.Object], store.InboundTriple{
			Subject: t.Subject, Predicate: t.Predicate,
		})
	}
	return g, nil
}

// LoadGraph builds the adapter from a persisted graph file.
func LoadGraph(path string) (*Graph, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ReadGraph(bytes.NewReader(data))
}

// ── store.Store ─────────────────────────────────────────────────────────────

func (g *Graph) Describe(_ context.Context, iri string) ([]store.Triple, error) {
	out := make([]store.Triple, len(g.bySubject[iri]))
	copy(out, g.bySubject[iri])
	return out, nil
}

func (g *Graph) DescribeInbound(_ context.Context, iri string) ([]store.InboundTriple, error) {
	out := make([]store.InboundTriple, len(g.byObject[iri]))
	copy(out, g.byObject[iri])
	return out, nil
}

func (g *Graph) Close() error                 { return nil }
func (g *Graph) Health(context.Context) error { return nil }

// The remaining store.Store methods are not part of the Phase-8 provenance query
// surface; the adapter is deliberately narrow and returns nothing for them rather
// than inventing a second interpretation of those queries.
func (g *Graph) ImpactForFile(context.Context, string) ([]store.ImpactFact, error) { return nil, nil }
func (g *Graph) ClassFacts(context.Context, string, int) ([]store.ImpactFact, error) {
	return nil, nil
}
func (g *Graph) CodeSymbolFacts(context.Context, string) ([]store.ImpactFact, error) {
	return nil, nil
}
func (g *Graph) RenderingGroupsForFile(context.Context, string) ([]store.RenderingGroupInfo, error) {
	return nil, nil
}
func (g *Graph) DetectFacts(context.Context) ([]store.ImpactFact, error) { return nil, nil }

// ── seedmeta.VerifierStore ──────────────────────────────────────────────────

func (g *Graph) CountTriples(context.Context) (int64, error) { return int64(g.total), nil }

func (g *Graph) CountByClass(_ context.Context, classIRI string) (int64, error) {
	return int64(len(g.byClass[classIRI])), nil
}

// staticInterfaceChecks proves the adapter satisfies both interfaces at compile time.
var (
	_ store.Store = (*Graph)(nil)
)
