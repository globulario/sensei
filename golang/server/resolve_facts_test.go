// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// An implementation pattern governs by literal rules (requiresCall, mustFollow,
// …) that are not edges to other nodes, so they never appear as graph links.
// Resolve must surface them as facts so the node reads as governed, not bare.
func TestResolve_ImplementationPatternSurfacesLiteralRuleFacts(t *testing.T) {
	requireCombinedSeed(t)
	s := newServer(newEmbeddedSeedStore())
	resp, err := s.Resolve(context.Background(), &awarenesspb.ResolveRequest{
		Class: "implementation_pattern",
		Id:    "globular.pattern.doctor_rule_diagnostic_only",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !resp.GetFound() {
		t.Fatal("impl-pattern not found in seed")
	}
	facts := resp.GetNode().GetFacts()
	if len(facts) == 0 {
		t.Fatal("expected literal-rule facts (requiresCall/mustFollow/…), got none")
	}
	// Every fact must carry a label and a non-empty value, and none may be an
	// invariant's long prose (title/summary/enforcement are excluded by design).
	seen := map[string]bool{}
	for _, f := range facts {
		if f.GetPredicate() == "" || f.GetValue() == "" {
			t.Errorf("malformed fact: %q = %q", f.GetPredicate(), f.GetValue())
		}
		seen[f.GetPredicate()] = true
	}
	if !seen["must follow"] && !seen["requires call"] && !seen["forbids call"] {
		t.Errorf("expected at least one rule fact (must follow / requires call / forbids call), got %v", seen)
	}
}
