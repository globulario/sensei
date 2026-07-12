// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"

	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store"
)

// Domain scoping for Metadata counts and Query lists. The counting/filtering
// reuses the pure, tested InScope core (scope.go) applied over the per-node
// facts ClassFacts already returns — no hand-rolled domain SPARQL FILTERs, which
// is where a cross-domain leak would hide. The only new store capability is
// enumerating the selectable domains, behind an optional interface so test
// stubs and future stores need not implement it (they degrade to graph-wide).

// domainLister is the optional store capability that enumerates the distinct
// selectable domain keys present in the graph (the aw:repo literals). Absent →
// the server offers no domain list and stays graph-wide.
type domainLister interface {
	Domains(ctx context.Context) ([]string, error)
}

// tripleDomainCounter is the optional store capability that counts triples whose
// subject is in a domain scope (per-repo triple_count).
type tripleDomainCounter interface {
	CountTriplesInDomain(ctx context.Context, domain, home string) (int64, error)
}

// classFactsScoper is the optional store capability that returns a class's facts
// restricted to a domain scope (LIMIT applied to in-scope nodes). Absent → the
// server falls back to fetch-then-filter (capped).
type classFactsScoper interface {
	ClassFactsScoped(ctx context.Context, classIRI, domain, home string, limit int) ([]store.ImpactFact, error)
}

// classDomainLister is the optional store capability that returns, for a class,
// each node's raw domain (aw:repo value, "shared", or "" for untagged) UNCAPPED.
// This is what makes domain-scoped counts and lists accurate — ClassFacts caps
// at 300, which silently truncates scoped results.
type classDomainLister interface {
	ClassNodeDomains(ctx context.Context, classIRI string) (map[string]string, error)
}

// resolvedDomain applies the home-domain default to a raw domain value from
// ClassNodeDomains ("" untagged → home).
func resolvedDomain(raw, home string) string {
	if raw == "" {
		return home
	}
	return raw
}

// countClassInScopeUncapped counts, without the ClassFacts cap, the nodes of a
// class visible to scope — via ClassNodeDomains. Returns (count, true) when the
// store supports it, else (0, false) so the caller can fall back.
func (s *server) countClassInScopeUncapped(ctx context.Context, classIRI, home, scope string) (int64, bool) {
	cdl, ok := s.store.(classDomainLister)
	if !ok {
		return 0, false
	}
	nodes, err := cdl.ClassNodeDomains(ctx, classIRI)
	if err != nil {
		return 0, false
	}
	var n int64
	for _, raw := range nodes {
		if InScope(resolvedDomain(raw, home), scope) {
			n++
		}
	}
	return n, true
}

// inScopeClassIRIs returns the set of a class's node IRIs visible to scope,
// UNCAPPED, via ClassNodeDomains. (nil, false) when unsupported.
func (s *server) inScopeClassIRIs(ctx context.Context, classIRI, home, scope string) (map[string]bool, bool) {
	cdl, ok := s.store.(classDomainLister)
	if !ok {
		return nil, false
	}
	nodes, err := cdl.ClassNodeDomains(ctx, classIRI)
	if err != nil {
		return nil, false
	}
	keep := make(map[string]bool)
	for iri, raw := range nodes {
		if InScope(resolvedDomain(raw, home), scope) {
			keep[iri] = true
		}
	}
	return keep, true
}

// availableDomains returns the sorted, de-duplicated selectable domains: the
// store's aw:repo keys plus the host's home domain (both are domains a caller
// may scope to). Shared and empties are excluded (uniqueSorted). Returns nil
// when the store can't enumerate.
func (s *server) availableDomains(ctx context.Context) []string {
	lister, ok := s.store.(domainLister)
	if !ok {
		return nil
	}
	keys, err := lister.Domains(ctx)
	if err != nil {
		return nil
	}
	if s.homeDomain != "" {
		keys = append(keys, s.homeDomain)
	}
	out := uniqueSorted(keys) // drops "" and rdf.DomainShared, sorts, de-dups
	if len(out) == 0 {
		return nil
	}
	return out
}

// nodeDomainsFromFacts maps each node in a ClassFacts result to its resolved
// domain key, applying the home-domain default for untagged nodes. Shared wins
// over a repo tag (a portable meta-principle is visible everywhere); mirrors
// nodeDomainFromTriples (resolve.go) and the impact.go extraction.
func nodeDomainsFromFacts(facts []store.ImpactFact, home string) map[string]string {
	dom := make(map[string]string)
	for _, f := range facts {
		if f.NodeIRI == "" {
			continue
		}
		if _, seen := dom[f.NodeIRI]; !seen {
			dom[f.NodeIRI] = home
		}
		switch f.Predicate {
		case rdf.PropRepo:
			if f.Object != "" && dom[f.NodeIRI] != rdf.DomainShared {
				dom[f.NodeIRI] = f.Object
			}
		case rdf.PropDomain:
			if f.Object == rdf.DomainShared {
				dom[f.NodeIRI] = rdf.DomainShared
			}
		}
	}
	return dom
}

// countClassInScope counts the distinct nodes of one class visible to scope,
// from the facts ClassFacts returned. Reuses InScope so the visibility rule is
// exactly the one every other scoped handler applies.
func countClassInScope(facts []store.ImpactFact, home, scope string) int64 {
	var n int64
	for _, d := range nodeDomainsFromFacts(facts, home) {
		if InScope(d, scope) {
			n++
		}
	}
	return n
}

// keepIRIsInScope returns the set of node IRIs (from ClassFacts) visible to
// scope — used to filter Query by_class rows.
func keepIRIsInScope(facts []store.ImpactFact, home, scope string) map[string]bool {
	keep := make(map[string]bool)
	for iri, d := range nodeDomainsFromFacts(facts, home) {
		if InScope(d, scope) {
			keep[iri] = true
		}
	}
	return keep
}
