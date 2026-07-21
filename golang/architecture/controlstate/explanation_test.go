// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

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

func openStateWithExplanation(t *testing.T, expl *DimensionExplanation) ArtifactState {
	t.Helper()
	reg := DefaultRegistry()
	id, res, err := BuildArtifactIdentity(reg, "aw:x", []string{rdf.ClassContract}, "repo", "repo", "auth", nil)
	if err != nil {
		t.Fatal(err)
	}
	st, err := BuildArtifactState(reg, id, res, ArtifactSourceBundle{
		GraphAuthority: GraphAuthorityObservation{Observed: true, Current: true, Integrity: true, Identity: "auth"},
		Contradiction:  ContradictionSource{Owner: "extractor.contradiction", Schema: "c", Identity: "s", Availability: SourceAvailable},
		Dimensions: map[string]DimensionObservation{
			"enforcement": {Dimension: "enforcement", SourceOwner: "closure.enforcement", SourceSchema: "dim",
				SourceIdentity: "s.en", SourceAvailability: SourceAvailable, Outcome: OutcomeDefinitiveBlocker,
				BlockerIDs: []string{"blk.1"}, Explanation: expl},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// Reviewer point 1: changing explanation WORDING (same Kind) never alters a semantic comparison —
// the dimension state, artifact closure, and every attention identity are invariant to the prose.
// (The projection digest legitimately reflects the full rendered content; semantics do not.)
func TestExplanationProseDoesNotAlterSemantics(t *testing.T) {
	a := openStateWithExplanation(t, &DimensionExplanation{Kind: "definitive_blocker", Known: "wording ALPHA"})
	b := openStateWithExplanation(t, &DimensionExplanation{Kind: "definitive_blocker", Known: "completely different wording BETA"})

	if a.Closure != b.Closure {
		t.Fatalf("closure must not depend on explanation prose: %q vs %q", a.Closure, b.Closure)
	}
	stateOf := func(st ArtifactState, dim string) DimensionState {
		for _, d := range st.Dimensions {
			if d.Dimension == dim {
				return d.State
			}
		}
		return ""
	}
	if stateOf(a, "enforcement") != stateOf(b, "enforcement") {
		t.Fatal("dimension state must not depend on explanation prose")
	}
	// Attention identities are content digests of the canonical semantic fields — never the prose.
	ids := func(st ArtifactState) []string {
		var out []string
		for _, at := range st.Attention {
			out = append(out, at.ID)
		}
		return out
	}
	ia, ib := ids(a), ids(b)
	if len(ia) == 0 || strings.Join(ia, ",") != strings.Join(ib, ",") {
		t.Fatalf("attention identities must be invariant to explanation prose: %v vs %v", ia, ib)
	}
}

// Reviewer point 2: the generic fallback explanation never implies evidence EXISTS — it names what
// is still missing and needed, and never asserts an observed/admitted crossing or a positive state.
func TestGenericFallbackNeverImpliesEvidence(t *testing.T) {
	e := composeDimensionExplanation(DimUnknown, "a_reason_no_owner_registered")
	if e == nil || e.Kind != "generic_incomplete" {
		t.Fatalf("unrecognized reason must fall back to generic_incomplete, got %+v", e)
	}
	if e.Missing == "" || e.NextEvidence == "" {
		t.Fatal("the fallback must still name what is missing and needed (admit incompleteness)")
	}
	low := strings.ToLower(e.Known + " " + e.WhyNotImprovable)
	for _, claim := range []string{"evidence was observed", "evidence exists", "admitted", "satisfied", "compliant"} {
		if strings.Contains(low, claim) {
			t.Fatalf("the fallback must not imply evidence/compliance exists (found %q)", claim)
		}
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
