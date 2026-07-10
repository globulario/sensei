// SPDX-License-Identifier: Apache-2.0

package main

// seed_store.go — a small, read-only in-memory store.Store backed by the
// embedded awareness seed (seedNT). It serves two consumers: golden tests that
// exercise the FULL Preflight pipeline against the REAL compiled graph with no
// live Oxigraph, and the -preflight offline CLI mode that makes the freshly
// built graph usable before any cluster deploy.
//
// It is deliberately faithful to only the two query shapes Preflight uses:
//   - ImpactForFile: nodes reached by `<file> aw:implements <node>` (the direct
//     anchors), each carrying its rdf:type + literal facts.
//   - ClassFacts: nodes of a given rdf:type, with their facts.
// The multi-hop affects/forbids/requiresTest expansion the production Oxigraph
// query performs is intentionally NOT reproduced — these tests assert the
// direct anchors and the pattern/authority surfaces, which is what the golden
// usefulness contract is about.

import (
	"context"
	"strings"
	"sync"

	"github.com/globulario/awareness-graph/golang/rdf"
	"github.com/globulario/awareness-graph/golang/seedmeta"
	"github.com/globulario/awareness-graph/golang/store"
)

type seedTriple struct {
	pred  string
	obj   string
	isIRI bool
}

type seedGraph struct {
	bySubject  map[string][]seedTriple          // subject (bare IRI) -> outgoing triples
	byObject   map[string][]store.InboundTriple // object IRI -> (subject, predicate) pointing at it
	typesOf    map[string][]string              // subject -> all rdf:type objects (bare IRIs)
	byClass    map[string][]string              // class IRI -> member subjects
	implements map[string][]string              // file subject -> anchored node subjects
}

var (
	seedGraphOnce sync.Once
	seedGraphInst *seedGraph
)

// loadSeedGraph parses the embedded seedNT exactly once.
func loadSeedGraph() *seedGraph {
	seedGraphOnce.Do(func() {
		g := &seedGraph{
			bySubject:  map[string][]seedTriple{},
			byObject:   map[string][]store.InboundTriple{},
			typesOf:    map[string][]string{},
			byClass:    map[string][]string{},
			implements: map[string][]string{},
		}
		for _, line := range strings.Split(string(seedNT), "\n") {
			subj, t, ok := parseSeedLine(line)
			if !ok {
				continue
			}
			g.bySubject[subj] = append(g.bySubject[subj], t)
			if t.isIRI {
				// Inverse index: lets DescribeInbound traverse from the
				// pointed-at side (a test an invariant requiresTest, a file a
				// symbol is definedInFile). rdf:type edges are excluded — class
				// membership is not a "related node" relationship.
				if t.pred != rdf.PropType {
					g.byObject[t.obj] = append(g.byObject[t.obj], store.InboundTriple{Subject: subj, Predicate: t.pred})
				}
			}
			switch {
			case t.pred == rdf.PropType && t.isIRI:
				g.typesOf[subj] = append(g.typesOf[subj], t.obj)
				g.byClass[t.obj] = append(g.byClass[t.obj], subj)
			case t.pred == rdf.PropImplements && t.isIRI:
				g.implements[subj] = append(g.implements[subj], t.obj)
			}
		}
		seedGraphInst = g
	})
	return seedGraphInst
}

// parseSeedLine parses one N-Triples line into (subject, triple). Returns
// ok=false for blanks/comments/malformed lines. Bare IRIs (no angle brackets)
// match what mintedIRI / awarenessIDFromIRI / classFromTypeIRI expect.
func parseSeedLine(line string) (string, seedTriple, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "<") {
		return "", seedTriple{}, false
	}
	sEnd := strings.IndexByte(line, '>')
	if sEnd < 0 {
		return "", seedTriple{}, false
	}
	subj := line[1:sEnd]
	rest := strings.TrimSpace(line[sEnd+1:])
	if !strings.HasPrefix(rest, "<") {
		return "", seedTriple{}, false
	}
	pEnd := strings.IndexByte(rest, '>')
	if pEnd < 0 {
		return "", seedTriple{}, false
	}
	pred := rest[1:pEnd]
	obj := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest[pEnd+1:]), "."))

	switch {
	case strings.HasPrefix(obj, "<") && strings.HasSuffix(obj, ">"):
		return subj, seedTriple{pred: pred, obj: obj[1 : len(obj)-1], isIRI: true}, true
	case strings.HasPrefix(obj, `"`):
		last := strings.LastIndexByte(obj, '"')
		if last <= 0 {
			return "", seedTriple{}, false
		}
		r := strings.NewReplacer(`\\`, `\`, `\"`, `"`, `\n`, "\n", `\t`, "\t", `\r`, "\r")
		return subj, seedTriple{pred: pred, obj: r.Replace(obj[1:last])}, true
	}
	return "", seedTriple{}, false
}

// embeddedSeedStore is the read-only store.Store over the parsed seed graph.
type embeddedSeedStore struct{ g *seedGraph }

func newEmbeddedSeedStore() embeddedSeedStore { return embeddedSeedStore{g: loadSeedGraph()} }

func (embeddedSeedStore) Close() error                   { return nil }
func (embeddedSeedStore) Health(_ context.Context) error { return nil }

func (s embeddedSeedStore) Describe(_ context.Context, iri string) ([]store.Triple, error) {
	var out []store.Triple
	for _, t := range s.g.bySubject[iri] {
		out = append(out, store.Triple{Predicate: t.pred, Object: t.obj, ObjectIsIRI: t.isIRI})
	}
	return out, nil
}

func (s embeddedSeedStore) DescribeInbound(_ context.Context, iri string) ([]store.InboundTriple, error) {
	in := s.g.byObject[iri]
	out := make([]store.InboundTriple, len(in))
	copy(out, in)
	return out, nil
}

// classifiableType picks the node's rdf:type that collectImpact can classify
// (Invariant/FailureMode/Intent/...), preferring it over subclass types like
// OperationalIntent that classFromTypeIRI does not recognise. Falls back to the
// first type for nodes (patterns, authority domains) whose type is not part of
// the impact-classification set but whose TypeIRI is unused downstream.
func (s embeddedSeedStore) classifiableType(node string) string {
	types := s.g.typesOf[node]
	for _, ty := range types {
		if _, ok := classFromTypeIRI(ty); ok {
			return ty
		}
	}
	if len(types) > 0 {
		return types[0]
	}
	return ""
}

func (s embeddedSeedStore) nodeFacts(node string) []store.ImpactFact {
	typeIRI := s.classifiableType(node)
	var out []store.ImpactFact
	for _, t := range s.g.bySubject[node] {
		out = append(out, store.ImpactFact{
			NodeIRI:     node,
			TypeIRI:     typeIRI,
			Predicate:   t.pred,
			Object:      t.obj,
			ObjectIsIRI: t.isIRI,
		})
	}
	return out
}

func (s embeddedSeedStore) ImpactForFile(_ context.Context, fileIRI string) ([]store.ImpactFact, error) {
	var out []store.ImpactFact
	for _, node := range s.g.implements[fileIRI] {
		out = append(out, s.nodeFacts(node)...)
	}
	return out, nil
}

func (s embeddedSeedStore) ClassFacts(_ context.Context, classIRI string, limit int) ([]store.ImpactFact, error) {
	members := s.g.byClass[classIRI]
	if limit > 0 && len(members) > limit {
		members = members[:limit]
	}
	var out []store.ImpactFact
	for _, node := range members {
		out = append(out, s.nodeFacts(node)...)
	}
	return out, nil
}

func (s embeddedSeedStore) CodeSymbolFacts(_ context.Context, _ string) ([]store.ImpactFact, error) {
	return nil, nil
}

func (s embeddedSeedStore) CountTriples(_ context.Context) (int64, error) {
	var count int64
	for _, triples := range s.g.bySubject {
		count += int64(len(triples))
	}
	return count, nil
}

func (s embeddedSeedStore) CountByClass(_ context.Context, classIRI string) (int64, error) {
	return int64(len(s.g.byClass[classIRI])), nil
}

func (s embeddedSeedStore) GraphFreshness(_ context.Context) seedmeta.Verification {
	expected, ok := normalizedEmbeddedSeedMarker()
	if !ok {
		return seedmeta.Verification{State: seedmeta.FreshnessUnknown, Detail: "embedded seed carries no graph marker"}
	}
	count, _ := s.CountTriples(context.Background())
	return seedmeta.Verification{
		State:           seedmeta.FreshnessCurrent,
		Expected:        expected,
		Live:            expected,
		LiveTripleCount: count,
		MarkerPresent:   true,
		SeedBuildCount:  1,
		Detail:          "embedded seed store is loaded directly from the embedded artifact",
	}
}

func (s embeddedSeedStore) RenderingGroupsForFile(_ context.Context, _ string) ([]store.RenderingGroupInfo, error) {
	return nil, nil
}

// DetectFacts returns the facts of every node carrying a detect block. The
// embedded seed normally has none (pilot/repo-scoped rules are loaded into
// Oxigraph, not embedded), but scanning keeps the in-memory store behaviourally
// equivalent to the Oxigraph backend for offline EditCheck.
func (s embeddedSeedStore) DetectFacts(_ context.Context) ([]store.ImpactFact, error) {
	var out []store.ImpactFact
	for node, triples := range s.g.bySubject {
		for _, t := range triples {
			if t.pred == rdf.PropDetectForbiddenPattern || t.pred == rdf.PropDetectRequiredPattern {
				out = append(out, s.nodeFacts(node)...)
				break
			}
		}
	}
	return out, nil
}
