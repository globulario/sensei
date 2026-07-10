// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=server.authority_domains
// @awareness file_role=preflight_authority_matcher
// @awareness implements=globular.awareness_graph:intent.awg.graph_is_compiled_context_not_authority
// @awareness risk=low
package main

// authority_domains.go — surfaces AuthorityDomain nodes in the Preflight
// response. A domain matches when a requested file path falls under one of
// its aw:coversPath prefixes; the matched domain's owner service, legal
// mutation/read paths, evidence freshness requirement, and forbidden
// bypasses are appended to required_actions / forbidden_fixes.
//
// Matching is deterministic path-prefix containment — no keyword scoring,
// no inference. When several domains cover the same file, the LONGEST
// matching prefix wins for that file (most specific domain); distinct files
// may still match distinct domains in one request.
//
// What this file does NOT do:
//   - assert authority (the owner services enforce their own boundaries)
//   - mint triples or write to the store
//   - fire on task text alone — only on file paths

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store"
)

// loadedAuthorityDomain is the unpacked form of an AuthorityDomain node.
type loadedAuthorityDomain struct {
	IRI               string
	ID                string // bare id, e.g. "authority.repository_artifact_metadata"
	Label             string
	Status            string
	TruthLayer        string
	OwnerService      string
	EvidenceFreshness string
	CoversPaths       []string
	OwnsState         []string
	MayWrite          []string
	MayRead           []string
	MustMutateVia     []string
	MustReadVia       []string
	ObservesVia       []string
	ForbidsBypass     []string
}

// authorityDomainCache mirrors the implementation-pattern cache: domains are
// few and matching is read-heavy, so one load + RW mutex is enough.
type authorityDomainCache struct {
	mu      sync.RWMutex
	loaded  bool
	domains []loadedAuthorityDomain
}

var globalAuthorityDomainCache = &authorityDomainCache{}

// loadAuthorityDomains populates the cache from store.ClassFacts. A backend
// error surfaces so Preflight can skip authority guidance without inventing
// it; the cache stays unloaded so a later call retries.
func (s *server) loadAuthorityDomains(ctx context.Context) ([]loadedAuthorityDomain, error) {
	globalAuthorityDomainCache.mu.RLock()
	if globalAuthorityDomainCache.loaded {
		out := globalAuthorityDomainCache.domains
		globalAuthorityDomainCache.mu.RUnlock()
		return out, nil
	}
	globalAuthorityDomainCache.mu.RUnlock()

	globalAuthorityDomainCache.mu.Lock()
	defer globalAuthorityDomainCache.mu.Unlock()
	if globalAuthorityDomainCache.loaded {
		return globalAuthorityDomainCache.domains, nil
	}

	if s.store == nil {
		return nil, nil
	}
	facts, err := s.store.ClassFacts(ctx, rdf.ClassAuthorityDomain, 100)
	if err != nil {
		return nil, err
	}
	domains := classFactsToAuthorityDomains(facts)
	globalAuthorityDomainCache.domains = domains
	globalAuthorityDomainCache.loaded = true
	return domains, nil
}

// classFactsToAuthorityDomains reifies flat ClassFacts rows into one
// loadedAuthorityDomain per node. Only active domains survive.
func classFactsToAuthorityDomains(facts []store.ImpactFact) []loadedAuthorityDomain {
	byNode := map[string]*loadedAuthorityDomain{}
	for _, f := range facts {
		d, ok := byNode[f.NodeIRI]
		if !ok {
			d = &loadedAuthorityDomain{IRI: f.NodeIRI, ID: bareIDFromIRI(f.NodeIRI)}
			byNode[f.NodeIRI] = d
		}
		switch f.Predicate {
		case rdf.PropLabel:
			d.Label = f.Object
		case rdf.PropStatus:
			d.Status = f.Object
		case rdf.PropHasTruthLayer:
			d.TruthLayer = f.Object
		case rdf.PropOwnerService:
			d.OwnerService = f.Object
		case rdf.PropHasEvidenceFreshnessWindow:
			d.EvidenceFreshness = f.Object
		case rdf.PropCoversPath:
			d.CoversPaths = append(d.CoversPaths, f.Object)
		case rdf.PropOwnsState:
			d.OwnsState = append(d.OwnsState, f.Object)
		case rdf.PropMayWrite:
			d.MayWrite = append(d.MayWrite, f.Object)
		case rdf.PropMayRead:
			d.MayRead = append(d.MayRead, f.Object)
		case rdf.PropMustMutateVia:
			d.MustMutateVia = append(d.MustMutateVia, f.Object)
		case rdf.PropMustReadVia:
			d.MustReadVia = append(d.MustReadVia, f.Object)
		case rdf.PropObservesVia:
			d.ObservesVia = append(d.ObservesVia, f.Object)
		case rdf.PropForbidsBypass:
			d.ForbidsBypass = append(d.ForbidsBypass, f.Object)
		}
	}
	out := make([]loadedAuthorityDomain, 0, len(byNode))
	for _, d := range byNode {
		if d.Status != "" && d.Status != "active" {
			continue
		}
		out = append(out, *d)
	}
	// Deterministic order for stable downstream assembly.
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// matchAuthorityDomains returns the domains covering the requested files.
// Per file, the domain with the LONGEST matching coversPath prefix wins;
// the result is the deduplicated union across files, sorted by domain id.
func matchAuthorityDomains(files []string, domains []loadedAuthorityDomain) []loadedAuthorityDomain {
	if len(files) == 0 || len(domains) == 0 {
		return nil
	}
	matched := map[string]loadedAuthorityDomain{}
	for _, file := range files {
		f := strings.TrimPrefix(strings.TrimSpace(file), "./")
		if f == "" {
			continue
		}
		bestLen := 0
		var best *loadedAuthorityDomain
		for i := range domains {
			for _, prefix := range domains[i].CoversPaths {
				p := strings.TrimPrefix(strings.TrimSpace(prefix), "./")
				if p == "" || !strings.HasPrefix(f, p) {
					continue
				}
				if len(p) > bestLen {
					bestLen = len(p)
					best = &domains[i]
				}
			}
		}
		if best != nil {
			matched[best.ID] = *best
		}
	}
	out := make([]loadedAuthorityDomain, 0, len(matched))
	for _, d := range matched {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// authorityRequiredActions renders a matched domain's ownership facts as
// bounded action strings. Order: owner+mutation path first (the fact that
// prevents the worst bug class), then read path, then freshness.
func authorityRequiredActions(domains []loadedAuthorityDomain) []string {
	var out []string
	for _, d := range domains {
		name := d.Label
		if name == "" {
			name = d.ID
		}
		if d.OwnerService != "" {
			line := "Authority [" + name + "]: state owner is " + d.OwnerService
			if len(d.MustMutateVia) > 0 {
				line += " — mutate only via " + strings.Join(d.MustMutateVia, "; ")
			}
			out = append(out, line)
		}
		if len(d.MustReadVia) > 0 {
			out = append(out, "Authority ["+name+"]: read via "+strings.Join(d.MustReadVia, "; "))
		} else if len(d.ObservesVia) > 0 {
			out = append(out, "Authority ["+name+"]: observe via "+strings.Join(d.ObservesVia, "; "))
		}
		if d.EvidenceFreshness != "" {
			out = append(out, "Authority ["+name+"]: evidence freshness — "+d.EvidenceFreshness)
		}
	}
	return out
}

// authorityForbiddenBypasses renders the matched domains' forbidden
// shortcuts for the forbidden_fixes list.
func authorityForbiddenBypasses(domains []loadedAuthorityDomain) []string {
	var out []string
	for _, d := range domains {
		name := d.Label
		if name == "" {
			name = d.ID
		}
		for _, b := range d.ForbidsBypass {
			out = append(out, "Authority bypass forbidden ["+name+"]: "+b)
		}
	}
	return out
}

// authorityCoversPaths flattens the coversPath prefixes across the given
// domains. Used by the risk-weighting (Phase 4) to treat authority-domain
// membership as a high-risk signal even outside the static directory list.
func authorityCoversPaths(domains []loadedAuthorityDomain) []string {
	var out []string
	for _, d := range domains {
		out = append(out, d.CoversPaths...)
	}
	return out
}

// invalidateAuthorityDomainCacheForTest resets the cache. Used by tests that
// swap the underlying store between calls.
func invalidateAuthorityDomainCacheForTest() {
	globalAuthorityDomainCache.mu.Lock()
	defer globalAuthorityDomainCache.mu.Unlock()
	globalAuthorityDomainCache.loaded = false
	globalAuthorityDomainCache.domains = nil
}
