// SPDX-License-Identifier: Apache-2.0

package main

// Phase 2D tests: Preflight prefers accepted/active knowledge, never presents
// deprecated/superseded knowledge as primary, points superseded knowledge at
// its replacement, and flags low-confidence knowledge as a caution.

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
	"github.com/globulario/awareness-graph/golang/rdf"
	"github.com/globulario/awareness-graph/golang/store"
)

// invariantWithScore builds the ImpactForFile + Describe facts for an invariant
// carrying a status and optional scoring properties.
func scoredInvariantFacts(id, label, severity, status, confidence, supersededBy string) []store.ImpactFact {
	iri := mintTestIRI(rdf.ClassInvariant, id)
	mk := func(p, o string, isIRI bool) store.ImpactFact {
		return store.ImpactFact{NodeIRI: iri, TypeIRI: rdf.ClassInvariant, Predicate: p, Object: o, ObjectIsIRI: isIRI}
	}
	out := []store.ImpactFact{
		mk(rdf.PropLabel, label, false),
		mk(rdf.PropSeverity, severity, false),
		mk(rdf.PropStatus, status, false),
	}
	if confidence != "" {
		out = append(out, mk(rdf.PropConfidence, confidence, false))
	}
	if supersededBy != "" {
		out = append(out, mk(rdf.PropSupersededBy, mintTestIRI(rdf.ClassInvariant, supersededBy), true))
	}
	return out
}

// scoringStore answers ImpactForFile with the given facts and Describe with the
// per-node subset (so the trust scorer can read confidence/supersededBy).
func scoringStore(t *testing.T, facts []store.ImpactFact) fakeStore {
	t.Helper()
	bySubject := map[string][]store.Triple{}
	for _, f := range facts {
		bySubject[f.NodeIRI] = append(bySubject[f.NodeIRI], store.Triple{Predicate: f.Predicate, Object: f.Object, ObjectIsIRI: f.ObjectIsIRI})
	}
	return fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) { return facts, nil },
		describe:      func(_ context.Context, iri string) ([]store.Triple, error) { return bySubject[iri], nil },
	}
}

func preflightForScoring(t *testing.T, facts []store.ImpactFact) *awarenesspb.PreflightResponse {
	t.Helper()
	invalidateImplementationPatternCacheForTest()
	invalidateAuthorityDomainCacheForTest()
	invalidateRepairPlanCacheForTest()
	invalidateRuntimeEvidenceCacheForTest()
	s := newServer(scoringStore(t, facts))
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "edit this file",
		Files: []string{"golang/example/example_server/x.go"},
		Mode:  awarenesspb.PreflightMode_PREFLIGHT_STANDARD,
	})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	return resp
}

func invariantIDsInOrder(resp *awarenesspb.PreflightResponse) []string {
	var out []string
	for _, n := range resp.GetDirectInvariants() {
		out = append(out, n.GetId())
	}
	return out
}

func TestPreflightPrefersAcceptedOverCandidate(t *testing.T) {
	facts := append(
		scoredInvariantFacts("x.candidate", "Candidate rule", "high", "extracted_candidate", "", ""),
		scoredInvariantFacts("x.accepted", "Accepted rule", "high", "accepted", "", "")...,
	)
	resp := preflightForScoring(t, facts)
	order := invariantIDsInOrder(resp)
	if len(order) < 2 {
		t.Fatalf("expected both invariants surfaced, got %v", order)
	}
	if order[0] != "x.accepted" {
		t.Errorf("accepted knowledge should lead, got order %v", order)
	}
}

func TestDeprecatedKnowledgeNotPrimaryGuidance(t *testing.T) {
	facts := append(
		scoredInvariantFacts("x.deprecated", "Old rule", "high", "deprecated", "", ""),
		scoredInvariantFacts("x.active", "Live rule", "high", "active", "", "")...,
	)
	resp := preflightForScoring(t, facts)
	for _, id := range invariantIDsInOrder(resp) {
		if id == "x.deprecated" {
			t.Errorf("deprecated knowledge must not be primary; got %v", invariantIDsInOrder(resp))
		}
	}
	if !anyContains(resp.GetBlindSpots(), "x.deprecated") {
		t.Errorf("deprecated knowledge should be surfaced as a caution; blind_spots: %v", resp.GetBlindSpots())
	}
}

func TestSupersededKnowledgePointsToReplacement(t *testing.T) {
	facts := scoredInvariantFacts("x.old", "Superseded rule", "high", "superseded", "", "x.new")
	resp := preflightForScoring(t, facts)
	found := false
	for _, b := range resp.GetBlindSpots() {
		if strings.Contains(b, "x.old") && strings.Contains(b, "x.new") {
			found = true
		}
	}
	if !found {
		t.Errorf("superseded caution should point at the replacement x.new; blind_spots: %v", resp.GetBlindSpots())
	}
}

func TestLowConfidenceKnowledgeRenderedAsCaution(t *testing.T) {
	facts := scoredInvariantFacts("x.shaky", "Shaky rule", "high", "active", "low", "")
	resp := preflightForScoring(t, facts)
	if !anyContains(resp.GetBlindSpots(), "low-confidence") || !anyContains(resp.GetBlindSpots(), "x.shaky") {
		t.Errorf("low-confidence knowledge should be cautioned; blind_spots: %v", resp.GetBlindSpots())
	}
}
