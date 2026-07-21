// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=server.runtime_evidence
// @awareness file_role=preflight_evidence_matcher
// @awareness implements=globular.awareness_graph:intent.awg.graph_is_compiled_context_not_authority
// @awareness risk=low
package main

// runtime_evidence.go — surfaces the LIVE proof a touched file's authority
// domain requires before a PASS/convergence claim. Awareness describes the
// evidence contract; it is never the authority. The hard rule it carries:
// stale or non-owner-path evidence must not be promoted to PASS.

import (
	"context"
	"sort"
	"sync"

	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store"
)

const maxEvidenceSurfaced = 2

type loadedRuntimeEvidence struct {
	IRI                          string
	ID                           string
	Label                        string
	Status                       string
	ObservedFromService          string
	FreshnessWindow              string
	TrustLevel                   string
	ObservedViaPaths             []string
	MustComeFromOwnerPath        bool
	CannotPromoteToPassWhenStale bool
	AuthorityDomainIDs           []string
}

type runtimeEvidenceCache struct {
	mu      sync.RWMutex
	loaded  bool
	profile []loadedRuntimeEvidence
}

var globalRuntimeEvidenceCache = &runtimeEvidenceCache{}

func (s *server) loadRuntimeEvidence(ctx context.Context) ([]loadedRuntimeEvidence, error) {
	globalRuntimeEvidenceCache.mu.RLock()
	if globalRuntimeEvidenceCache.loaded {
		out := globalRuntimeEvidenceCache.profile
		globalRuntimeEvidenceCache.mu.RUnlock()
		return out, nil
	}
	globalRuntimeEvidenceCache.mu.RUnlock()

	globalRuntimeEvidenceCache.mu.Lock()
	defer globalRuntimeEvidenceCache.mu.Unlock()
	if globalRuntimeEvidenceCache.loaded {
		return globalRuntimeEvidenceCache.profile, nil
	}
	if s.store == nil {
		return nil, nil
	}
	facts, err := s.store.ClassFacts(ctx, rdf.ClassRuntimeEvidence, 200)
	if err != nil {
		return nil, err
	}
	profiles := classFactsToRuntimeEvidence(facts)
	globalRuntimeEvidenceCache.profile = profiles
	globalRuntimeEvidenceCache.loaded = true
	return profiles, nil
}

func classFactsToRuntimeEvidence(facts []store.ImpactFact) []loadedRuntimeEvidence {
	byNode := map[string]*loadedRuntimeEvidence{}
	for _, f := range facts {
		ev, ok := byNode[f.NodeIRI]
		if !ok {
			ev = &loadedRuntimeEvidence{IRI: f.NodeIRI, ID: bareIDFromIRI(f.NodeIRI)}
			byNode[f.NodeIRI] = ev
		}
		switch f.Predicate {
		case rdf.PropLabel:
			ev.Label = f.Object
		case rdf.PropStatus:
			ev.Status = f.Object
		case rdf.PropObservedFromService:
			ev.ObservedFromService = f.Object
		case rdf.PropHasFreshnessWindow:
			ev.FreshnessWindow = f.Object
		case rdf.PropHasTrustLevel:
			ev.TrustLevel = f.Object
		case rdf.PropObservedViaPath:
			ev.ObservedViaPaths = append(ev.ObservedViaPaths, f.Object)
		case rdf.PropMustComeFromOwnerPath:
			ev.MustComeFromOwnerPath = f.Object == "true"
		case rdf.PropCannotPromoteToPassWhenStale:
			ev.CannotPromoteToPassWhenStale = f.Object == "true"
		case rdf.PropEvidenceForAuthorityDomain:
			ev.AuthorityDomainIDs = append(ev.AuthorityDomainIDs, bareIDFromIRI(f.Object))
		}
	}
	out := make([]loadedRuntimeEvidence, 0, len(byNode))
	for _, ev := range byNode {
		if ev.Status != "" && ev.Status != "active" {
			continue
		}
		out = append(out, *ev)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// matchRuntimeEvidence returns the evidence profiles for the authority domains
// a touched file belongs to.
func matchRuntimeEvidence(matchedDomains []loadedAuthorityDomain, profiles []loadedRuntimeEvidence) []loadedRuntimeEvidence {
	if len(profiles) == 0 || len(matchedDomains) == 0 {
		return nil
	}
	domainIDs := map[string]bool{}
	for _, d := range matchedDomains {
		domainIDs[d.ID] = true
	}
	var out []loadedRuntimeEvidence
	for _, ev := range profiles {
		for _, id := range ev.AuthorityDomainIDs {
			if domainIDs[id] {
				out = append(out, ev)
				break
			}
		}
	}
	if len(out) > maxEvidenceSurfaced {
		out = out[:maxEvidenceSurfaced]
	}
	return out
}

// evidenceRequirementActions renders matched evidence profiles as bounded
// required-action lines for Preflight.
func evidenceRequirementActions(profiles []loadedRuntimeEvidence) []string {
	var out []string
	for _, ev := range profiles {
		name := ev.Label
		if name == "" {
			name = ev.ID
		}
		line := "Evidence required [" + name + "]: from " + orNone(ev.ObservedFromService)
		if len(ev.ObservedViaPaths) > 0 {
			line += " via " + ev.ObservedViaPaths[0]
		}
		if ev.FreshnessWindow != "" {
			line += "; freshness: " + ev.FreshnessWindow
		}
		out = append(out, line)
		if ev.CannotPromoteToPassWhenStale {
			out = append(out, "Evidence ["+name+"]: stale or missing evidence must NOT be promoted to PASS — yield UNKNOWN/CHECK_ERROR/DEGRADED")
		}
	}
	return out
}

func invalidateRuntimeEvidenceCacheForTest() {
	globalRuntimeEvidenceCache.mu.Lock()
	defer globalRuntimeEvidenceCache.mu.Unlock()
	globalRuntimeEvidenceCache.loaded = false
	globalRuntimeEvidenceCache.profile = nil
}
