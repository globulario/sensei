// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import "testing"

// Every reason code that dimensionStateFor / assessContradictionDimension can emit for a non-positive
// state. The composer must be TOTAL over these (non-nil, non-empty Kind) and fail-honest on anything
// unrecognized (an explicit generic_incomplete, never empty strings).
func TestComposeDimensionExplanation_TotalAndFailHonest(t *testing.T) {
	reasons := []string{
		"source_not_observed", "source_unavailable", "insufficient_evidence", "definitive_blocker",
		"degraded_source", "degraded", "not_applicable", "contradiction_present",
		"contradiction_source_degraded", "contradiction_source_unavailable",
	}
	for _, r := range reasons {
		e := composeDimensionExplanation(DimUnknown, r)
		if e == nil || e.Kind == "" {
			t.Fatalf("reason %q produced no explanation", r)
		}
		if e.WhyNotImprovable == "" || e.NextEvidence == "" {
			t.Fatalf("reason %q produced an empty explanation body", r)
		}
	}
	// Fail-honest: an unrecognized reason still yields an explicit explanation, never empty.
	e := composeDimensionExplanation(DimUnknown, "some_reason_the_owner_added_later")
	if e == nil || e.Kind != "generic_incomplete" || e.WhyNotImprovable == "" {
		t.Fatal("an unrecognized reason must produce an explicit generic explanation, not empty")
	}
	// A positive dimension carries NO explanation.
	if composeDimensionExplanation(DimSatisfied, "") != nil {
		t.Fatal("a satisfied dimension must carry no explanation")
	}
}

// Proof: the explanation never contradicts the state — its presence tracks non-positive states only,
// and it is projection, so it cannot upgrade a state to satisfied/compliant.
func TestExplanation_NeverImpliesSatisfied(t *testing.T) {
	for _, st := range []DimensionState{DimOpen, DimDegraded, DimUnknown, DimNotApplicable} {
		e := composeDimensionExplanation(st, "insufficient_evidence")
		if e == nil {
			t.Fatalf("non-positive state %q must carry an explanation", st)
		}
		if e.Kind == "satisfied" || e.Kind == "compliant" || e.Kind == "complete" {
			t.Fatalf("explanation Kind %q must never assert a positive outcome", e.Kind)
		}
	}
}
