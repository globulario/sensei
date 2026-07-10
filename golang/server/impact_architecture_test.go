// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"strings"
	"testing"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
	"github.com/globulario/awareness-graph/golang/rdf"
)

// TestClassFromTypeIRI_Architecture verifies the spine + pattern classes are
// recognized so file-anchored architecture nodes surface in Impact/Briefing —
// and that MetaPrinciple is deliberately NOT (it surfaces as an invariant).
func TestClassFromTypeIRI_Architecture(t *testing.T) {
	want := map[string]string{
		rdf.ClassComponent:             "component",
		rdf.ClassBoundary:              "boundary",
		rdf.ClassContract:              "contract",
		rdf.ClassDecision:              "decision",
		rdf.ClassEvidence:              "evidence",
		rdf.ClassDesignPattern:         "design_pattern",
		rdf.ClassImplementationPattern: "implementation_pattern",
		rdf.ClassPatternMisuse:         "pattern_misuse",
		// regression: original classes still recognized.
		rdf.ClassInvariant: "invariant",
		rdf.ClassTest:      "test",
	}
	for iri, cls := range want {
		got, ok := classFromTypeIRI(iri)
		if !ok || got != cls {
			t.Errorf("classFromTypeIRI(%q) = (%q,%v), want (%q,true)", iri, got, ok, cls)
		}
	}
	// MetaPrinciple is intentionally unmapped (dual-typed meta.* invariant).
	if _, ok := classFromTypeIRI(rdf.ClassMetaPrinciple); ok {
		t.Error("MetaPrinciple must NOT be a direct architecture class (surfaces as invariant)")
	}
}

func TestAppendArchitectureSection(t *testing.T) {
	var b strings.Builder
	nodes := []*awarenesspb.KnowledgeNode{
		{Class: "boundary", Id: "boundary.domain_scope", Label: "Domain scope boundary"},
		{Class: "design_pattern", Id: "pattern.scope_resolver", Label: "ScopeResolver"},
	}
	appendArchitectureSection(&b, nodes)
	out := b.String()
	for _, want := range []string{
		"Architecture (components, boundaries, contracts, decisions, patterns):",
		"[boundary] boundary.domain_scope — Domain scope boundary",
		"[design_pattern] pattern.scope_resolver — ScopeResolver",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("architecture section missing %q\ngot:\n%s", want, out)
		}
	}
	// Empty input renders nothing.
	var e strings.Builder
	appendArchitectureSection(&e, nil)
	if e.Len() != 0 {
		t.Errorf("empty architecture section should render nothing, got %q", e.String())
	}
}
