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
	"fmt"
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

// matchRuntimeEvidence returns EVERY evidence profile for the authority domains
// a touched file belongs to. It matches; it does not present.
//
// It used to cap the result at maxEvidenceSurfaced and return the prefix, which
// put a presentation bound inside the matching step. The bound then reached
// Preflight's RequiredActions with nothing saying it had been applied, so a
// third required evidence profile was dropped in silence: a caller satisfied
// two requirements, saw no others, and had no way to learn a third existed.
//
// The requirement is not softened -- the rendering is still bounded, because an
// unbounded action list is its own defect. What changes is that the omission is
// STATED, and that the complete set survives long enough for a caller to be
// told how much of it it is seeing. Deciding from the whole set and projecting
// afterwards is the shape this repository already records; capping first is the
// shape it records as the defect.
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
	return out
}

// evidenceRequirementActions renders matched evidence profiles as bounded
// required-action lines for Preflight.
//
// The bound lives here, at the presentation edge, and it announces itself. A
// required action that is silently absent is worse than a long list: the reader
// cannot distinguish "these are the requirements" from "these are two of the
// requirements", and only the first is safe to act on.
func evidenceRequirementActions(profiles []loadedRuntimeEvidence) []string {
	shown, omitted := profiles, 0
	if len(shown) > maxEvidenceSurfaced {
		omitted = len(shown) - maxEvidenceSurfaced
		shown = shown[:maxEvidenceSurfaced]
	}
	var out []string
	for _, ev := range shown {
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
	// NAME THE OMISSION, and name it as incompleteness rather than as a count.
	// "+2 more" reads as detail a reader may skip; this list is requirements,
	// and a reader who skips it believes it satisfied.
	if omitted > 0 {
		out = append(out, fmt.Sprintf(
			"Evidence requirements INCOMPLETE: %d further requirement(s) apply to this change and are not listed here — this list is bounded at %d, so satisfying it is not sufficient",
			omitted, maxEvidenceSurfaced))
	}
	return out
}

func invalidateRuntimeEvidenceCacheForTest() {
	globalRuntimeEvidenceCache.mu.Lock()
	defer globalRuntimeEvidenceCache.mu.Unlock()
	globalRuntimeEvidenceCache.loaded = false
	globalRuntimeEvidenceCache.profile = nil
}
