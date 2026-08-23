// SPDX-License-Identifier: AGPL-3.0-only

package epistemic

import (
	"testing"
	"time"
)

func scoped(id string, due string, paths ...string) Hypothesis {
	h := goodHypothesis()
	h.ID, h.Horizon.DueAt, h.ExperimentalScope = id, due, paths
	return h
}

var future = "2027-01-01T00:00:00Z"

func now() time.Time { return at("2026-10-01T00:00:00Z") }

// The loop this exists to break: an agent guesses B, implements B, extraction
// records that B exists, B becomes architecture, and the agent can no longer
// replace its own guess.
func TestOpenHypothesisDefendedByCanonicalArchitectureIsSediment(t *testing.T) {
	hs := []Hypothesis{scoped("h.placement", future, "golang/placement/v2")}
	established := map[string][]string{
		"golang/placement/v2/assign.go": {"invariant.placement_is_deterministic"},
	}
	got := CheckSediment(hs, nil, established, now())
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %+v", got)
	}
	if got[0].Kind != KindSediment {
		t.Fatalf("kind = %q, want %q", got[0].Kind, KindSediment)
	}
	if len(got[0].CitedBy) != 1 || got[0].CitedBy[0] != "invariant.placement_is_deterministic" {
		t.Fatalf("a finding must name what is already defending the code: %+v", got[0])
	}
	if got[0].State != StateAwaitingHorizon {
		t.Fatalf("state = %q", got[0].State)
	}
}

// The corpus anchors directories as often as files, so exact matching would be
// defeated by the ordinary way it is written. Overlap has to work both ways.
func TestScopeAndAnchorOverlapInBothDirections(t *testing.T) {
	t.Run("a canonical directory covers an experimental file inside it", func(t *testing.T) {
		hs := []Hypothesis{scoped("h.a", future, "golang/server/reload_v2.go")}
		est := map[string][]string{"golang/server/": {"high_risk_files"}}
		if got := CheckSediment(hs, nil, est, now()); len(got) != 1 {
			t.Fatalf("want 1 finding, got %+v", got)
		}
	})
	t.Run("an experimental directory covers a canonical file inside it", func(t *testing.T) {
		hs := []Hypothesis{scoped("h.b", future, "golang/placement/v2")}
		est := map[string][]string{"golang/placement/v2/assign.go": {"invariant.x"}}
		if got := CheckSediment(hs, nil, est, now()); len(got) != 1 {
			t.Fatalf("want 1 finding, got %+v", got)
		}
	})
	t.Run("a sibling path is not an overlap", func(t *testing.T) {
		hs := []Hypothesis{scoped("h.c", future, "golang/placement/v2")}
		est := map[string][]string{"golang/placement/v20_legacy.go": {"invariant.x"}}
		if got := CheckSediment(hs, nil, est, now()); len(got) != 0 {
			t.Fatalf("a prefix that is not a path boundary must not match: %+v", got)
		}
	})
}

// Experimental code is NOT ungoverned. The established envelope still holds --
// this check says nothing about whether the surrounding invariants apply, only
// that the provisional design inside them has not silently become law.
func TestExperimentalScopeWithNoCanonicalClaimIsClean(t *testing.T) {
	hs := []Hypothesis{scoped("h.free", future, "golang/placement/v2")}
	est := map[string][]string{"golang/server/": {"high_risk_files"}}
	if got := CheckSediment(hs, nil, est, now()); len(got) != 0 {
		t.Fatalf("want no findings, got %+v", got)
	}
}

func TestRefutedHypothesisLeavesItsCodeOrphaned(t *testing.T) {
	hs := []Hypothesis{scoped("h.dead", "2026-09-01T00:00:00Z", "golang/placement/v2")}
	obs := []Observation{{ID: "o1", Hypothesis: "h.dead", Outcome: OutcomeRefutes}}
	got := CheckSediment(hs, obs, nil, now())
	if len(got) != 1 || got[0].Kind != KindOrphaned {
		t.Fatalf("want an orphaned-experiment finding, got %+v", got)
	}
	if got[0].State != StateRefuted {
		t.Fatalf("state = %q", got[0].State)
	}
}

// Once a belief is settled the question is no longer open, and adoption -- not
// this check -- is what should follow. Reporting sediment here would be telling
// the project off for finishing an experiment.
func TestSupportedHypothesisIsNotSediment(t *testing.T) {
	hs := []Hypothesis{scoped("h.done", "2026-09-01T00:00:00Z", "golang/placement/v2")}
	obs := []Observation{{ID: "o1", Hypothesis: "h.done", Outcome: OutcomeSupports}}
	est := map[string][]string{"golang/placement/v2": {"invariant.x"}}
	if got := CheckSediment(hs, obs, est, now()); len(got) != 0 {
		t.Fatalf("want no findings, got %+v", got)
	}
}

// "Design B is bad" is almost never what was observed. Dropping the condition
// turns one experiment into a universal prohibition nobody tested.
func TestRefutationMustCarryItsConditions(t *testing.T) {
	o := Observation{
		ID: "o.x", Hypothesis: "h.x", ObservedAt: "2026-09-30T09:00:00Z",
		What: "stale ownership survived two reconciliation cycles", Outcome: OutcomeRefutes,
		Evidence: []string{"fault run F7"}, ObservedBy: "agent",
	}
	if errs := ValidateObservation(o); !containsSubstring(errs, "failure_conditions") {
		t.Fatalf("want a failure-conditions error, got %v", errs)
	}
	o.FailureConditions = []string{"network partition with leader turnover"}
	o.RemainingApplicability = "may still hold for non-authoritative cache placement"
	if errs := ValidateObservation(o); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	// A supporting observation needs no conditions — there is nothing to
	// over-generalise from.
	o.Outcome, o.FailureConditions = OutcomeSupports, nil
	if errs := ValidateObservation(o); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}
