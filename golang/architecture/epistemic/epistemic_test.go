// SPDX-License-Identifier: AGPL-3.0-only

package epistemic

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func goodQuestion() DesignQuestion {
	return DesignQuestion{
		ID: "dq.example", Question: "How should X be established?",
		DeclaredBy: "agent", DeclaredAt: "2026-08-23T12:00:00Z",
		Constraints: []string{"inv.one", "inv.two"},
		Alternatives: []Alternative{
			{ID: "a", Statement: "marker plus a triple count"},
			{ID: "b", Statement: "a canonical content digest recomputed at load"},
		},
		Consequences: []Consequence{{Effect: "a full scan on startup", Reversible: true}},
	}
}

// -----------------------------------------------------------------------------
// Disposition is computed, and reachable only through structure
// -----------------------------------------------------------------------------

func TestDispositionFollowsFromStructure(t *testing.T) {
	base := goodQuestion()

	t.Run("two viable alternatives with reversible consequences is an exploration candidate", func(t *testing.T) {
		if got, _ := Dispose(base); got != DispositionExploration {
			t.Fatalf("got %q, want %q", got, DispositionExploration)
		}
	})

	t.Run("constraints leaving one alternative is conservation", func(t *testing.T) {
		q := base
		q.Alternatives = []Alternative{
			{ID: "a", Statement: "marker plus a triple count", EliminatedBy: "inv.one"},
			{ID: "b", Statement: "a canonical content digest recomputed at load"},
		}
		if got, why := Dispose(q); got != DispositionConservation {
			t.Fatalf("got %q (%s), want %q", got, why, DispositionConservation)
		}
	})

	t.Run("an irreversible consequence is authority", func(t *testing.T) {
		q := base
		q.Consequences = []Consequence{
			{Effect: "a full scan on startup", Reversible: true},
			{Effect: "publishes an artifact to the release bucket", Reversible: false},
		}
		got, why := Dispose(q)
		if got != DispositionAuthority {
			t.Fatalf("got %q, want %q", got, DispositionAuthority)
		}
		if !strings.Contains(why, "release bucket") {
			t.Fatalf("the reason must name the consequence that reached authority, got %q", why)
		}
	})

	t.Run("every alternative eliminated is over-constrained, not conservation", func(t *testing.T) {
		q := base
		q.Alternatives = []Alternative{
			{ID: "a", Statement: "marker plus a triple count", EliminatedBy: "inv.one"},
			{ID: "b", Statement: "a canonical content digest recomputed at load", EliminatedBy: "inv.two"},
		}
		if got, _ := Dispose(q); got != DispositionOverConstrained {
			t.Fatalf("got %q, want %q — folding this into CONSERVATION would report a decision that made itself", got, DispositionOverConstrained)
		}
	})
}

// AUTHORITY must be reached by consequence and by nothing else. The enforcement
// is structural: there is no field on DesignQuestion an agent could use to say
// "this is hard, a human should decide", so routing a difficult technical
// question to a human is not expressible. This test fails the moment somebody
// adds one.
func TestDesignQuestionCannotExpressTechnicalDifficulty(t *testing.T) {
	forbidden := []string{
		"difficulty", "hard", "complexity", "confidence", "escalate",
		"needs_human", "requires_human", "ask", "disposition", "regime",
	}
	rt := reflect.TypeOf(DesignQuestion{})
	for i := 0; i < rt.NumField(); i++ {
		name := strings.ToLower(rt.Field(i).Name)
		for _, bad := range forbidden {
			if strings.Contains(name, bad) {
				t.Fatalf("DesignQuestion.%s: AUTHORITY is reached by consequence or value, never by technical difficulty, "+
					"and a disposition is computed rather than authored — this field makes one of those expressible",
					rt.Field(i).Name)
			}
		}
	}
}

// -----------------------------------------------------------------------------
// §4 — the DesignQuestion rule
// -----------------------------------------------------------------------------

func TestQuestionValidation(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*DesignQuestion)
		want string
	}{
		{"one alternative is a decision already made", func(q *DesignQuestion) {
			q.Alternatives = q.Alternatives[:1]
		}, "at least 2 alternatives"},
		{"no alternatives is a topic heading", func(q *DesignQuestion) {
			q.Alternatives = nil
		}, "at least 2 alternatives"},
		{"verbatim repetition is refused", func(q *DesignQuestion) {
			q.Alternatives[1].Statement = "  Marker  Plus A Triple Count  "
		}, "repeats an earlier alternative verbatim"},
		{"an alternative may only be eliminated by a bound constraint", func(q *DesignQuestion) {
			q.Alternatives[0].EliminatedBy = "inv.never_bound"
		}, "not one of the question's bound constraints"},
		{"consequences are not optional", func(q *DesignQuestion) {
			q.Consequences = nil
		}, "consequences is required"},
		{"declared_at must be RFC3339", func(q *DesignQuestion) {
			q.DeclaredAt = "yesterday"
		}, "not RFC3339"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := goodQuestion()
			tc.mut(&q)
			errs := ValidateQuestion(q)
			if !containsSubstring(errs, tc.want) {
				t.Fatalf("want an error containing %q, got %v", tc.want, errs)
			}
		})
	}

	t.Run("a well-formed question passes", func(t *testing.T) {
		if errs := ValidateQuestion(goodQuestion()); len(errs) != 0 {
			t.Fatalf("unexpected errors: %v", errs)
		}
	})
}

// The verbatim check is all the validator claims. Two alternatives that differ
// only by a renamed type are NOT materially distinct, and this must still pass
// — because a check that pretended to settle "materially distinct" would put a
// green tick next to the manufactured-alternatives failure it cannot detect.
func TestMateriallyDistinctIsNotClaimedMechanically(t *testing.T) {
	q := goodQuestion()
	q.Alternatives = []Alternative{
		{ID: "a", Statement: "an append-only receipt record"},
		{ID: "b", Statement: "an append-only receipt record, with the type renamed"},
	}
	if errs := ValidateQuestion(q); len(errs) != 0 {
		t.Fatalf("the validator must not claim to decide material distinctness; got %v", errs)
	}
}

// -----------------------------------------------------------------------------
// §5 — the Hypothesis rule
// -----------------------------------------------------------------------------

func goodHypothesis() Hypothesis {
	return Hypothesis{
		ID: "h.example", Question: "dq.example", Alternative: "b",
		Prediction: "recomputing the digest at load detects a count-preserving mutation",
		Falsifier:  "a store whose content drifted is still reported authoritative, with every gate green",
		Horizon:    Horizon{DueAt: "2026-09-30T00:00:00Z"},
		DeclaredBy: "agent", DeclaredAt: "2026-08-23T12:00:00Z",
	}
}

func TestFalsifierMayNotRestateAGate(t *testing.T) {
	for _, bad := range []string{
		"the tests fail",
		"CI is red",
		"The build breaks after the change",
		"go test fails on the new package",
	} {
		t.Run(bad, func(t *testing.T) {
			h := goodHypothesis()
			h.Falsifier = bad
			if errs := ValidateHypothesis(h); !containsSubstring(errs, "restates an existing gate") {
				t.Fatalf("want a gate-restatement error for %q, got %v", bad, errs)
			}
		})
	}

	t.Run("a falsifier that could fire while every gate passes is accepted", func(t *testing.T) {
		if errs := ValidateHypothesis(goodHypothesis()); len(errs) != 0 {
			t.Fatalf("unexpected errors: %v", errs)
		}
	})
}

func TestHypothesisRequiresADetectableHorizon(t *testing.T) {
	h := goodHypothesis()
	h.Horizon = Horizon{Condition: "after 100 successful reconciliation cycles"}
	errs := ValidateHypothesis(h)
	if !containsSubstring(errs, "horizon.due_at is required") {
		t.Fatalf("a condition-only horizon can never be reported overdue; got %v", errs)
	}
	// The condition is still carried — the date is the backstop, not a
	// replacement for the real trigger.
	h.Horizon.DueAt = "2026-12-01T00:00:00Z"
	if errs := ValidateHypothesis(h); len(errs) != 0 {
		t.Fatalf("a condition alongside a date must be accepted: %v", errs)
	}
}

// -----------------------------------------------------------------------------
// §6 — unrefuted is not supported, and the horizon does not leak
// -----------------------------------------------------------------------------

func TestStateNeverReachesSupportedWithoutAnObservation(t *testing.T) {
	h := goodHypothesis()

	t.Run("before the horizon, with nothing observed", func(t *testing.T) {
		if got := StateOf(h, nil, at("2026-08-24T00:00:00Z")); got != StateAwaitingHorizon {
			t.Fatalf("got %q, want %q", got, StateAwaitingHorizon)
		}
	})

	t.Run("after the horizon, with nothing observed, is OVERDUE not supported", func(t *testing.T) {
		if got := StateOf(h, nil, at("2026-10-01T00:00:00Z")); got != StateOverdue {
			t.Fatalf("got %q, want %q — 'nothing broke, therefore it works' is the horizon leak", got, StateOverdue)
		}
	})

	t.Run("an inconclusive observation after the horizon is not supported", func(t *testing.T) {
		obs := []Observation{{ID: "o1", Hypothesis: h.ID, Outcome: OutcomeInconclusive}}
		if got := StateOf(h, obs, at("2026-10-01T00:00:00Z")); got != StateInconclusive {
			t.Fatalf("got %q, want %q", got, StateInconclusive)
		}
	})

	t.Run("supported requires a supporting observation past the horizon", func(t *testing.T) {
		obs := []Observation{{ID: "o1", Hypothesis: h.ID, Outcome: OutcomeSupports}}
		if got := StateOf(h, obs, at("2026-10-01T00:00:00Z")); got != StateSupported {
			t.Fatalf("got %q, want %q", got, StateSupported)
		}
	})

	t.Run("a supporting observation cannot mature the clock", func(t *testing.T) {
		obs := []Observation{{ID: "o1", Hypothesis: h.ID, Outcome: OutcomeSupports}}
		if got := StateOf(h, obs, at("2026-08-24T00:00:00Z")); got != StateAwaitingHorizon {
			t.Fatalf("got %q, want %q — a long-horizon claim whose clock has not matured is never SUPPORTED", got, StateAwaitingHorizon)
		}
	})

	t.Run("refutation wins before the horizon", func(t *testing.T) {
		obs := []Observation{{ID: "o1", Hypothesis: h.ID, Outcome: OutcomeRefutes}}
		if got := StateOf(h, obs, at("2026-08-24T00:00:00Z")); got != StateRefuted {
			t.Fatalf("got %q, want %q — a belief contradicted early is still contradicted", got, StateRefuted)
		}
	})

	t.Run("another hypothesis's observations are ignored", func(t *testing.T) {
		obs := []Observation{{ID: "o1", Hypothesis: "h.someone_else", Outcome: OutcomeSupports}}
		if got := StateOf(h, obs, at("2026-10-01T00:00:00Z")); got != StateOverdue {
			t.Fatalf("got %q, want %q", got, StateOverdue)
		}
	})
}

// A horizon that cannot be parsed must surface, not hide. Treating it as
// not-yet-due would let a malformed date buy indefinite silence, which is the
// same escape the overdue tripwire exists to close.
func TestUnparseableHorizonIsOverdueRatherThanSilent(t *testing.T) {
	h := goodHypothesis()
	h.Horizon.DueAt = "sometime next quarter"
	if got := StateOf(h, nil, at("2026-08-24T00:00:00Z")); got != StateOverdue {
		t.Fatalf("got %q, want %q", got, StateOverdue)
	}
}

// -----------------------------------------------------------------------------
// Liveness
// -----------------------------------------------------------------------------

func TestMeasureCountsAndRefusesARateWithNoDenominator(t *testing.T) {
	t.Run("an empty table has no ratio", func(t *testing.T) {
		l := Measure(nil, nil, at("2026-10-01T00:00:00Z"))
		if l.Ratio != nil {
			t.Fatalf("an empty denominator has no rate; got %v — 0.000 would report a healthy empty table as total failure", *l.Ratio)
		}
	})

	t.Run("settled hypotheses leave the active denominator", func(t *testing.T) {
		mk := func(id, due string) Hypothesis {
			h := goodHypothesis()
			h.ID, h.Horizon.DueAt = id, due
			return h
		}
		hs := []Hypothesis{
			mk("h.overdue", "2026-09-01T00:00:00Z"),
			mk("h.waiting", "2027-01-01T00:00:00Z"),
			mk("h.refuted", "2026-09-01T00:00:00Z"),
			mk("h.supported", "2026-09-01T00:00:00Z"),
		}
		obs := []Observation{
			{ID: "o1", Hypothesis: "h.refuted", Outcome: OutcomeRefutes},
			{ID: "o2", Hypothesis: "h.supported", Outcome: OutcomeSupports},
		}
		l := Measure(hs, obs, at("2026-10-01T00:00:00Z"))
		if l.Total != 4 || l.PastDue != 1 || l.AwaitingHorizon != 1 || l.Refuted != 1 || l.Supported != 1 {
			t.Fatalf("counts wrong: %+v", l)
		}
		// Active is overdue + awaiting only: keeping settled beliefs in the
		// denominator is how this ratio gets made to look good without
		// observing anything.
		if l.Active != 2 {
			t.Fatalf("active = %d, want 2", l.Active)
		}
		if l.Ratio == nil || *l.Ratio != 0.5 {
			t.Fatalf("ratio = %v, want 0.5", l.Ratio)
		}
	})
}

// -----------------------------------------------------------------------------
// Observation
// -----------------------------------------------------------------------------

func TestObservationRequiresCheckableEvidence(t *testing.T) {
	o := Observation{
		ID: "o.example", Hypothesis: "h.example", ObservedAt: "2026-08-23T12:00:00Z",
		What: "the digest matched on three live stores", Outcome: OutcomeSupports,
		ObservedBy: "agent",
	}
	if errs := ValidateObservation(o); !containsSubstring(errs, "evidence is required") {
		t.Fatalf("want an evidence error, got %v", errs)
	}
	o.Evidence = []string{"PR #283, three stores recomputed content=match"}
	if errs := ValidateObservation(o); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	o.Outcome = "probably"
	if errs := ValidateObservation(o); !containsSubstring(errs, "refutes|supports|inconclusive") {
		t.Fatalf("want an outcome error, got %v", errs)
	}
}

func containsSubstring(errs []string, want string) bool {
	for _, e := range errs {
		if strings.Contains(e, want) {
			return true
		}
	}
	return false
}

// A belief that only its own author ever confirmed is the shape of a fake
// reasoning loop: declare a question, predict an answer, observe that the
// answer was right, call it evidence. It is ALSO the shape of an honest
// one-person project, which is why this is counted and never gated. The test
// pins that it is counted, and that one independent observer clears it.
func TestSelfConfirmationIsCountedNotGated(t *testing.T) {
	mk := func(id, who string) Hypothesis {
		h := goodHypothesis()
		h.ID, h.DeclaredBy, h.Horizon.DueAt = id, who, "2026-09-01T00:00:00Z"
		return h
	}
	hs := []Hypothesis{mk("h.alone", "claude"), mk("h.checked", "claude")}
	obs := []Observation{
		{ID: "o1", Hypothesis: "h.alone", Outcome: OutcomeSupports, ObservedBy: "claude"},
		{ID: "o2", Hypothesis: "h.checked", Outcome: OutcomeSupports, ObservedBy: "claude"},
		{ID: "o3", Hypothesis: "h.checked", Outcome: OutcomeSupports, ObservedBy: "an independent run"},
	}
	l := Measure(hs, obs, at("2026-10-01T00:00:00Z"))
	if l.Supported != 2 {
		t.Fatalf("supported = %d, want 2", l.Supported)
	}
	if l.SelfConfirmed != 1 {
		t.Fatalf("self_confirmed = %d, want 1 — one independent supporting observation clears it", l.SelfConfirmed)
	}
	if l.SelfConfirmedRatio == nil || *l.SelfConfirmedRatio != 0.5 {
		t.Fatalf("ratio = %v, want 0.5", l.SelfConfirmedRatio)
	}

	// Nothing supported yet has no rate, for the same reason past_due/active
	// has none on an empty table.
	empty := Measure(hs, nil, at("2026-10-01T00:00:00Z"))
	if empty.SelfConfirmedRatio != nil {
		t.Fatalf("an empty denominator has no rate, got %v", *empty.SelfConfirmedRatio)
	}
}
