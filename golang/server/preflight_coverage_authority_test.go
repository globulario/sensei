// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store"
)

// coverage.sufficient exists to separate "the graph looked and found nothing"
// from "the graph never looked". A pattern match is neither: it recognises the
// shape of code from task text, without examining the files asked about. Before
// #218 a strong-tier match alone reported sufficient coverage on zero anchors
// and zero indexed files, so the second answer could wear the first one's
// clothes — including for an ImplementationPattern nobody promoted.
func TestPatternMatchAloneCannotManufactureSufficientCoverage(t *testing.T) {
	files := []string{"internal/x/x.go"}
	indexed := 0
	var invariants, failureModes, intents []*awarenesspb.KnowledgeNode

	patterns := []*awarenesspb.MatchedImplementationPattern{{
		Id:            "implementation_pattern:example.unpromoted_candidate",
		Label:         "a review-only candidate nobody promoted",
		MatchStrength: "strong",
		MatchReason:   []string{"shape match"},
	}}

	got := computePreflightCoverage(files, indexed, mergeAnchors(invariants, failureModes, intents), patterns)

	if got.GetSufficient() {
		t.Fatalf("coverage was reported sufficient on zero anchors and zero indexed files, "+
			"from a pattern match alone: note=%q", got.GetNote())
	}
	if got.GetNote() == "" {
		t.Fatal("an insufficient verdict must say what was and was not established")
	}
}

// The lookups coverage does rest on keep working, and the pattern-only case is
// the only branch that changes.
func TestPreflightCoverageRestsOnAnchorsOrIndexedFiles(t *testing.T) {
	anchor := []*awarenesspb.KnowledgeNode{mkNode("invariant.x", "x", "high")}
	strong := []*awarenesspb.MatchedImplementationPattern{mkPattern("strong")}

	cases := []struct {
		name    string
		files   []string
		indexed int
		anchors []*awarenesspb.KnowledgeNode
		matched []*awarenesspb.MatchedImplementationPattern
		want    bool
	}{
		{"anchor fired", []string{"a.go"}, 0, anchor, nil, true},
		{"file indexed", []string{"a.go"}, 1, nil, nil, true},
		{"anchor fired with a pattern alongside", []string{"a.go"}, 0, anchor, strong, true},
		{"pattern only, files given", []string{"a.go"}, 0, nil, strong, false},
		{"pattern only, task-only request", nil, 0, nil, strong, false},
		{"nothing at all", []string{"a.go"}, 0, nil, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computePreflightCoverage(tc.files, tc.indexed, tc.anchors, tc.matched)
			if got.GetSufficient() != tc.want {
				t.Fatalf("sufficient=%v, want %v (note=%q)", got.GetSufficient(), tc.want, got.GetNote())
			}
		})
	}
}

// The other half of the same chain: `sensei skill-ingest` writes candidates with
// status "candidate", and the loader must keep them out of the matcher entirely.
// Only `active` (or unstated, which the authored corpus treats as active)
// patterns are selectable.
func TestUnpromotedPatternCandidatesNeverReachTheMatcher(t *testing.T) {
	fact := func(id, predicate, object string) store.ImpactFact {
		return store.ImpactFact{
			NodeIRI:   "<https://globular.io/awareness#implementationPattern/" + id + ">",
			TypeIRI:   rdf.ClassImplementationPattern,
			Predicate: predicate,
			Object:    object,
		}
	}
	facts := []store.ImpactFact{
		fact("promoted", rdf.PropLabel, "promoted pattern"),
		fact("promoted", rdf.PropStatus, "active"),
		fact("candidate", rdf.PropLabel, "review-only candidate"),
		fact("candidate", rdf.PropStatus, "candidate"),
		fact("draft", rdf.PropLabel, "draft pattern"),
		fact("draft", rdf.PropStatus, "draft"),
	}

	var ids []string
	for _, p := range classFactsToPatterns(facts, "globular") {
		ids = append(ids, p.ID)
	}
	if len(ids) != 1 || ids[0] != "promoted" {
		t.Fatalf("only promoted patterns may load; got %v", ids)
	}
}
