// SPDX-License-Identifier: AGPL-3.0-only

package epistemic

import (
	"strings"
	"testing"
)

func seeded(t *testing.T) *Ledger {
	t.Helper()
	l := &Ledger{Version: LedgerVersion}
	if errs := l.AddQuestion(goodQuestion()); errs != nil {
		t.Fatalf("seed question: %v", errs)
	}
	return l
}

func TestLedgerRoundTripsAndSortsDeterministically(t *testing.T) {
	l := seeded(t)
	l.Questions = append(l.Questions, DesignQuestion{ID: "dq.aaa"}, DesignQuestion{ID: "dq.zzz"})
	b, err := Encode(*l)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "NOT canonical knowledge") {
		t.Fatal("the ledger header must say what this file is not; a reader who mistakes it for law is the failure this lane exists to end")
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Questions) != 3 || got.Questions[0].ID != "dq.aaa" || got.Questions[2].ID != "dq.zzz" {
		t.Fatalf("entries must be sorted by id so a diff shows what changed: %+v", got.Questions)
	}
	// Byte-stable: encoding what was decoded reproduces the same file.
	again, err := Encode(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(b) {
		t.Fatal("encode is not byte-stable across a round trip")
	}
}

func TestDecodeEmptyIsAnEmptyLedgerAndUnknownVersionIsRefused(t *testing.T) {
	l, err := Decode(nil)
	if err != nil || l.Version != LedgerVersion || len(l.Questions) != 0 {
		t.Fatalf("an empty lane is the normal first state, got %+v err=%v", l, err)
	}
	if _, err := Decode([]byte("version: 99\ndesign_questions: []\n")); err == nil {
		t.Fatal("a ledger version this binary does not know must be refused, not read positionally")
	}
}

func TestHypothesisMustAttachToADeclaredViableAlternative(t *testing.T) {
	l := seeded(t)

	t.Run("unknown question", func(t *testing.T) {
		h := goodHypothesis()
		h.Question = "dq.never_declared"
		if errs := l.AddHypothesis(h); !containsSubstring(errs, "declare the question before predicting about it") {
			t.Fatalf("got %v", errs)
		}
	})

	t.Run("unknown alternative", func(t *testing.T) {
		h := goodHypothesis()
		h.Alternative = "z"
		if errs := l.AddHypothesis(h); !containsSubstring(errs, "declares no alternative") {
			t.Fatalf("got %v", errs)
		}
	})

	t.Run("an eliminated alternative cannot be experimented on", func(t *testing.T) {
		l2 := &Ledger{Version: LedgerVersion}
		q := goodQuestion()
		q.Alternatives[1].EliminatedBy = "inv.two"
		if errs := l2.AddQuestion(q); errs != nil {
			t.Fatalf("%v", errs)
		}
		h := goodHypothesis()
		if errs := l2.AddHypothesis(h); !containsSubstring(errs, "already made") {
			t.Fatalf("spending an experiment on a settled decision must be refused, got %v", errs)
		}
	})

	t.Run("a viable alternative is accepted, once", func(t *testing.T) {
		if errs := l.AddHypothesis(goodHypothesis()); errs != nil {
			t.Fatalf("%v", errs)
		}
		if errs := l.AddHypothesis(goodHypothesis()); !containsSubstring(errs, "already exists") {
			t.Fatalf("got %v", errs)
		}
	})
}

func TestObservationsAreAppendedNeverRewritten(t *testing.T) {
	l := seeded(t)
	if errs := l.AddHypothesis(goodHypothesis()); errs != nil {
		t.Fatalf("%v", errs)
	}
	o := Observation{
		ID: "o.first", Hypothesis: "h.example", ObservedAt: "2026-09-30T09:00:00Z",
		What: "a count-preserving mutation was refused", Outcome: OutcomeSupports,
		Evidence: []string{"seedmeta verify_test.go"}, ObservedBy: "agent",
	}
	if errs := l.AddObservation(o); errs != nil {
		t.Fatalf("%v", errs)
	}
	o.What = "actually it was not refused"
	if errs := l.AddObservation(o); !containsSubstring(errs, "never rewritten") {
		t.Fatalf("an observation is a record of what happened; got %v", errs)
	}
	o.ID, o.Hypothesis = "o.orphan", "h.nobody"
	if errs := l.AddObservation(o); !containsSubstring(errs, "no hypothesis") {
		t.Fatalf("got %v", errs)
	}
}

func TestQuestionIsDeclaredOnce(t *testing.T) {
	l := seeded(t)
	if errs := l.AddQuestion(goodQuestion()); !containsSubstring(errs, "already exists") {
		t.Fatalf("got %v", errs)
	}
}
