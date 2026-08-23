// SPDX-License-Identifier: AGPL-3.0-only

package epistemic

import (
	"strings"
	"testing"
)

// adoptable builds a ledger whose hypothesis about alternative "b" is SUPPORTED.
func adoptable(t *testing.T) *Ledger {
	t.Helper()
	l := &Ledger{Version: LedgerVersion}
	if errs := l.AddQuestion(goodQuestion()); errs != nil {
		t.Fatalf("question: %v", errs)
	}
	h := goodHypothesis()
	h.Horizon.DueAt = "2026-09-01T00:00:00Z"
	h.ExperimentalScope = []string{"golang/placement/v2"}
	if errs := l.AddHypothesis(h); errs != nil {
		t.Fatalf("hypothesis: %v", errs)
	}
	o := Observation{
		ID: "o.support", Hypothesis: h.ID, ObservedAt: "2026-09-02T00:00:00Z",
		What: "converged without stale ownership across 40 fault runs", Outcome: OutcomeSupports,
		Evidence: []string{"fault suite F1-F9"}, ObservedBy: "an independent run",
	}
	if errs := l.AddObservation(o); errs != nil {
		t.Fatalf("observation: %v", errs)
	}
	return l
}

func goodAdoption() Adoption {
	return Adoption{
		ID: "ad.example", Question: "dq.example", Alternative: "b",
		Hypotheses:           []string{"h.example"},
		RemainingUncertainty: "unmeasured above 10k writes/sec",
		AdoptedBy:            "claude", AdoptedAt: "2026-10-01T00:00:00Z",
	}
}

func TestAdoptionRequiresASupportedBelief(t *testing.T) {
	cases := []struct {
		name    string
		horizon string
		obs     *Observation
		want    string
	}{
		{"before the horizon", "2027-06-01T00:00:00Z", nil, "promotion on silence"},
		{"past the horizon with nothing observed", "2026-09-01T00:00:00Z", nil, "promotion in place of observing"},
		{"refuted", "2026-09-01T00:00:00Z", &Observation{
			ID: "o.no", Hypothesis: "h.example", ObservedAt: "2026-09-02T00:00:00Z",
			What: "stale ownership survived", Outcome: OutcomeRefutes, ObservedBy: "run",
			Evidence: []string{"F7"}, FailureConditions: []string{"partition with leader turnover"},
		}, "promotion against the evidence"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := &Ledger{Version: LedgerVersion}
			if errs := l.AddQuestion(goodQuestion()); errs != nil {
				t.Fatal(errs)
			}
			h := goodHypothesis()
			h.Horizon.DueAt = tc.horizon
			if errs := l.AddHypothesis(h); errs != nil {
				t.Fatal(errs)
			}
			if tc.obs != nil {
				if errs := l.AddObservation(*tc.obs); errs != nil {
					t.Fatal(errs)
				}
			}
			errs := l.AddAdoption(goodAdoption(), now())
			if !containsSubstring(errs, tc.want) {
				t.Fatalf("want %q, got %v", tc.want, errs)
			}
		})
	}

	t.Run("a supported belief carries the adoption", func(t *testing.T) {
		l := adoptable(t)
		if errs := l.AddAdoption(goodAdoption(), now()); errs != nil {
			t.Fatalf("unexpected: %v", errs)
		}
		if l.AdoptionFor("dq.example", "b") == nil {
			t.Fatal("adoption not recorded")
		}
	})
}

// Adopting an alternative established knowledge already eliminated would let an
// experiment overrule a constraint.
func TestEliminatedAlternativeCannotBeAdopted(t *testing.T) {
	l := &Ledger{Version: LedgerVersion}
	q := goodQuestion()
	q.Alternatives[1].EliminatedBy = "inv.two"
	if errs := l.AddQuestion(q); errs != nil {
		t.Fatal(errs)
	}
	errs := l.AddAdoption(goodAdoption(), now())
	if !containsSubstring(errs, "overrule established knowledge") {
		t.Fatalf("got %v", errs)
	}
}

// Alternatives are competing answers to one question. Adopting two says it was
// never really a question.
func TestOneQuestionAdoptsOneAlternative(t *testing.T) {
	l := adoptable(t)
	if errs := l.AddAdoption(goodAdoption(), now()); errs != nil {
		t.Fatal(errs)
	}
	second := goodAdoption()
	second.ID, second.Alternative = "ad.other", "a"
	if errs := l.AddAdoption(second, now()); !containsSubstring(errs, "never really a question") {
		t.Fatalf("got %v", errs)
	}
}

// AUTHORITY is reached by consequence. When it is, the adoption has to name the
// authority it was made under — though nothing here VERIFIES that a person
// agreed, and the record says so rather than implying proof.
func TestAuthorityDispositionRequiresANamedAuthority(t *testing.T) {
	l := &Ledger{Version: LedgerVersion}
	q := goodQuestion()
	q.Consequences = append(q.Consequences, Consequence{Effect: "publishes a release artifact", Reversible: false})
	if errs := l.AddQuestion(q); errs != nil {
		t.Fatal(errs)
	}
	h := goodHypothesis()
	h.Horizon.DueAt = "2026-09-01T00:00:00Z"
	if errs := l.AddHypothesis(h); errs != nil {
		t.Fatal(errs)
	}
	if errs := l.AddObservation(Observation{
		ID: "o.s", Hypothesis: "h.example", ObservedAt: "2026-09-02T00:00:00Z",
		What: "held", Outcome: OutcomeSupports, Evidence: []string{"run"}, ObservedBy: "run",
	}); errs != nil {
		t.Fatal(errs)
	}

	errs := l.AddAdoption(goodAdoption(), now())
	if !containsSubstring(errs, "must name the authority") {
		t.Fatalf("got %v", errs)
	}

	withAuthority := goodAdoption()
	withAuthority.Authority = "repository owner, in the 2026-10-01 review"
	if errs := l.AddAdoption(withAuthority, now()); errs != nil {
		t.Fatalf("unexpected: %v", errs)
	}
}

// A reversible question does not need one: the agent that ran the experiments
// may adopt. What matters is not who typed the command but that the record
// carries what, why, from what evidence, and under what uncertainty.
func TestReversibleQuestionNeedsNoSeparateAuthority(t *testing.T) {
	l := adoptable(t)
	a := goodAdoption()
	a.AdoptedBy = "claude-opus-5"
	if errs := l.AddAdoption(a, now()); errs != nil {
		t.Fatalf("an agent may adopt inside its consequence boundary: %v", errs)
	}
}

func TestAdoptionMustStateWhatIsStillUnknown(t *testing.T) {
	l := adoptable(t)
	a := goodAdoption()
	a.RemainingUncertainty = ""
	if errs := l.AddAdoption(a, now()); !containsSubstring(errs, "remaining_uncertainty is required") {
		t.Fatalf("got %v", errs)
	}
	// "none identified" is acceptable — the point is that the sentence gets
	// written, not that uncertainty must exist.
	a.RemainingUncertainty = "none identified"
	if errs := l.AddAdoption(a, now()); errs != nil {
		t.Fatalf("unexpected: %v", errs)
	}
}

func TestAdoptionMustRestOnAHypothesisAboutTheSameDesign(t *testing.T) {
	l := adoptable(t)

	a := goodAdoption()
	a.Hypotheses = nil
	if errs := l.AddAdoption(a, now()); !containsSubstring(errs, "no evidential basis") {
		t.Fatalf("got %v", errs)
	}

	a = goodAdoption()
	a.Alternative = "a"
	if errs := l.AddAdoption(a, now()); !containsSubstring(errs, "not the design being adopted") {
		t.Fatalf("got %v", errs)
	}

	a = goodAdoption()
	a.Question = "dq.never_declared"
	if errs := l.AddAdoption(a, now()); !containsSubstring(errs, "not by appearing") {
		t.Fatalf("got %v", errs)
	}
}

// An adoption with no explicit scope inherits the experimental scope of the
// beliefs it rests on. Otherwise adopting a belief without adopting the code
// that embodies it would leave that code permanently unadoptable.
func TestAdoptionInheritsTheExperimentalScopeItRestsOn(t *testing.T) {
	l := adoptable(t)
	if errs := l.AddAdoption(goodAdoption(), now()); errs != nil {
		t.Fatal(errs)
	}
	paths := l.adoptedPaths()
	if id, ok := paths["golang/placement/v2"]; !ok || id != "ad.example" {
		t.Fatalf("adopted paths = %+v", paths)
	}
}

func TestLedgerRoundTripsAdoptions(t *testing.T) {
	l := adoptable(t)
	if errs := l.AddAdoption(goodAdoption(), now()); errs != nil {
		t.Fatal(errs)
	}
	b, err := Encode(*l)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "until an `adoptions` entry says so") {
		t.Fatal("the ledger header must say that nothing is established without an adoption")
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Adoptions) != 1 || got.Adoptions[0].RemainingUncertainty == "" {
		t.Fatalf("round trip lost the adoption: %+v", got.Adoptions)
	}
}

// A CONSERVATION question was settled by established knowledge — no experiment
// was ever needed. Demanding a SUPPORTED hypothesis would force a fake one, and
// refusing to adopt it at all would leave its code permanently unadoptable:
// sediment by a different route.
func TestConservationIsAdoptedOnItsConstraints(t *testing.T) {
	l := &Ledger{Version: LedgerVersion}
	q := goodQuestion()
	q.Alternatives[0].EliminatedBy = "inv.one"
	if errs := l.AddQuestion(q); errs != nil {
		t.Fatal(errs)
	}
	if d, _ := Dispose(q); d != DispositionConservation {
		t.Fatalf("fixture is %q, not CONSERVATION", d)
	}

	a := goodAdoption()
	a.Hypotheses = nil
	if errs := l.AddAdoption(a, now()); errs != nil {
		t.Fatalf("a conservation question must be adoptable on its constraints: %v", errs)
	}

	// An open question still needs evidence — the exemption is narrow.
	l2 := &Ledger{Version: LedgerVersion}
	if errs := l2.AddQuestion(goodQuestion()); errs != nil {
		t.Fatal(errs)
	}
	b := goodAdoption()
	b.Hypotheses = nil
	if errs := l2.AddAdoption(b, now()); !containsSubstring(errs, "no evidential basis") {
		t.Fatalf("got %v", errs)
	}
}
