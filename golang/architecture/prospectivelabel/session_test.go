// SPDX-License-Identifier: AGPL-3.0-only

package prospectivelabel

import (
	"fmt"
	"strings"
	"testing"
)

func newTestSession(t *testing.T, corpusSize int) *Session {
	t.Helper()
	var corpus []string
	for i := 0; i < corpusSize; i++ {
		corpus = append(corpus, fmt.Sprintf("invariant:inv%03d", i))
	}
	s, err := New("manifest-digest", "blind-digest", "a-human", []string{"pr1:aaa", "pr1:bbb"}, corpus)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return s
}

// A pair begins UNSET. If opening a package defaulted 866 pairs to
// not_applicable, absence of action would masquerade as judgement and the
// denominator would be manufactured by the act of looking.
func TestNothingHasADefaultLabel(t *testing.T) {
	s := newTestSession(t, 10)
	c := s.Coverage("pr1:aaa")
	if c.Unlabelled != 10 {
		t.Fatalf("a fresh change has %d unlabelled pairs, want 10", c.Unlabelled)
	}
	if c.IndividuallyAssigned != 0 || c.BulkSweptNotApplicable != 0 {
		t.Fatal("a fresh change already carries labels")
	}
	if len(s.Labels()) != 0 {
		t.Fatal("a fresh session emitted labels nobody made")
	}
	if c.AdjudicationCoverageComplete {
		t.Fatal("an untouched change reports complete coverage")
	}
}

// The sweep is an act of judgement over a remainder, and it is recorded as one.
func TestSweepIsRecordedAsASweepNotAsIndividualReview(t *testing.T) {
	s := newTestSession(t, 10)
	if err := s.Present("pr1:aaa", s.CorpusIDs...); err != nil {
		t.Fatal(err)
	}
	if err := s.Assign("pr1:aaa", "invariant:inv003", LabelApplicable); err != nil {
		t.Fatal(err)
	}
	if err := s.Assign("pr1:aaa", "invariant:inv004", LabelAmbiguous); err != nil {
		t.Fatal(err)
	}
	n, err := s.Sweep("pr1:aaa")
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 8 {
		t.Fatalf("swept %d, want the 8 still-unset pairs", n)
	}
	c := s.Coverage("pr1:aaa")
	if c.IndividuallyAssigned != 2 || c.BulkSweptNotApplicable != 8 || c.Unlabelled != 0 {
		t.Fatalf("coverage conflates the two kinds of decision: %+v", c)
	}
	if !c.AdjudicationCoverageComplete {
		t.Fatal("a fully labelled, fully presented change is not complete")
	}
	if c.IndividualReviewComplete {
		t.Fatal("a change closed with a sweep claims individual review of every item")
	}
	// The sweep must not overwrite a decision already made.
	for _, l := range s.Labels() {
		if l.CorpusItemID == "invariant:inv003" && (l.Label != LabelApplicable || l.AssignmentMode != ModeIndividual) {
			t.Fatalf("the sweep overwrote an individual decision: %+v", l)
		}
		if l.AssignmentMode == ModeBulkSweep && l.Label != LabelNotApplicable {
			t.Fatalf("a swept pair carries %q; a sweep is negative by definition", l.Label)
		}
	}
}

// Without the presentation gate an adjudicator could filter to a handful of
// items, sweep, and emit hundreds of negatives for items the software never
// showed — indistinguishable in the output from having considered them.
func TestSweepIsGatedOnTheWholeCorpusHavingBeenPresented(t *testing.T) {
	s := newTestSession(t, 10)
	if err := s.Present("pr1:aaa", "invariant:inv000", "invariant:inv001"); err != nil {
		t.Fatal(err)
	}
	_, err := s.Sweep("pr1:aaa")
	if err == nil {
		t.Fatal("a sweep was allowed after a filtered view; finalization must not rely on a filter")
	}
	if !strings.Contains(err.Error(), "never showed") {
		t.Fatalf("the refusal does not say what is wrong: %v", err)
	}
	if len(s.Labels()) != 0 {
		t.Fatal("the refused sweep still wrote labels")
	}
	if got := len(s.NotPresented("pr1:aaa")); got != 8 {
		t.Fatalf("NotPresented reports %d, want the 8 unseen items", got)
	}
	if err := s.Present("pr1:aaa", s.CorpusIDs...); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sweep("pr1:aaa"); err != nil {
		t.Fatalf("a sweep after full presentation was refused: %v", err)
	}
}

// Individual review of every pair is reported as such, and only then.
func TestIndividualReviewCompleteOnlyWithoutASweep(t *testing.T) {
	s := newTestSession(t, 3)
	if err := s.Present("pr1:bbb", s.CorpusIDs...); err != nil {
		t.Fatal(err)
	}
	for _, id := range s.CorpusIDs {
		if err := s.Assign("pr1:bbb", id, LabelNotApplicable); err != nil {
			t.Fatal(err)
		}
	}
	c := s.Coverage("pr1:bbb")
	if !c.IndividualReviewComplete || !c.AdjudicationCoverageComplete {
		t.Fatalf("a genuinely item-by-item change is not reported as one: %+v", c)
	}
	if c.BulkSweptNotApplicable != 0 {
		t.Fatal("individual assignments were counted as swept")
	}
}

// Unresolved labels are counted apart from the resolved ones, because only
// applicable enters the recall numerator and only the resolved pair enters the
// primary nuisance denominator.
func TestUnresolvedLabelsAreCountedSeparately(t *testing.T) {
	s := newTestSession(t, 5)
	if err := s.Present("pr1:aaa", s.CorpusIDs...); err != nil {
		t.Fatal(err)
	}
	pairs := map[string]string{
		"invariant:inv000": LabelApplicable,
		"invariant:inv001": LabelNotApplicable,
		"invariant:inv002": LabelAmbiguous,
		"invariant:inv003": LabelOutsideScope,
		"invariant:inv004": LabelCannotAdjudicate,
	}
	for id, l := range pairs {
		if err := s.Assign("pr1:aaa", id, l); err != nil {
			t.Fatal(err)
		}
	}
	c := s.Coverage("pr1:aaa")
	if c.Unresolved != 3 {
		t.Fatalf("unresolved=%d, want the 3 that can never reach the numerator", c.Unresolved)
	}
	if c.IndividuallyAssigned != 5 || c.Unlabelled != 0 {
		t.Fatalf("coverage: %+v", c)
	}
}

// The vocabulary is closed, and the session refuses anything outside the frozen
// sample — a label attached to an unknown pair would score against nothing.
func TestTheSessionRefusesWhatIsNotInTheSample(t *testing.T) {
	s := newTestSession(t, 3)
	if err := s.Assign("pr1:aaa", "invariant:inv000", "probably"); err == nil {
		t.Fatal("an invented label was accepted")
	}
	if err := s.Assign("pr1:zzz", "invariant:inv000", LabelApplicable); err == nil {
		t.Fatal("a label was attached to a change outside the sample")
	}
	if err := s.Assign("pr1:aaa", "invariant:nope", LabelApplicable); err == nil {
		t.Fatal("a label was attached to an item outside the blind corpus")
	}
	if err := s.Present("pr1:aaa", "invariant:nope"); err == nil {
		t.Fatal("an item outside the blind corpus was presented")
	}
	if _, err := New("m", "b", "  ", []string{"a"}, []string{"b"}); err == nil {
		t.Fatal("an unnamed adjudicator was accepted")
	}
}

// Work survives a restart without being re-done or silently lost, and a
// restored sweep is still a sweep.
func TestRestorePreservesProvenance(t *testing.T) {
	s := newTestSession(t, 4)
	if err := s.Restore([]Label{
		{ItemKey: "pr1:aaa", CorpusItemID: "invariant:inv000", Label: LabelApplicable, AssignmentMode: ModeIndividual},
		{ItemKey: "pr1:aaa", CorpusItemID: "invariant:inv001", Label: LabelNotApplicable, AssignmentMode: ModeBulkSweep},
	}, map[string][]string{"pr1:aaa": {"invariant:inv000", "invariant:inv001"}}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	c := s.Coverage("pr1:aaa")
	if c.IndividuallyAssigned != 1 || c.BulkSweptNotApplicable != 1 || c.Presented != 2 {
		t.Fatalf("restore lost provenance: %+v", c)
	}
	if err := s.Restore([]Label{{ItemKey: "pr1:aaa", CorpusItemID: "invariant:inv002", Label: LabelApplicable, AssignmentMode: "guessed"}}, nil); err == nil {
		t.Fatal("a restored label with an unknown assignment mode was accepted")
	}
}

// No ordering anywhere depends on anything but the deterministic keys the
// reference set fixed. A relevance order would make some machine the oracle
// and let the rest of the corpus disappear.
func TestOrderingIsDeterministicAndCarriesNoRelevance(t *testing.T) {
	s := newTestSession(t, 5)
	if err := s.Present("pr1:aaa", s.CorpusIDs...); err != nil {
		t.Fatal(err)
	}
	if err := s.Assign("pr1:aaa", "invariant:inv004", LabelApplicable); err != nil {
		t.Fatal(err)
	}
	if err := s.Assign("pr1:aaa", "invariant:inv000", LabelApplicable); err != nil {
		t.Fatal(err)
	}
	got := s.Labels()
	if got[0].CorpusItemID != "invariant:inv000" || got[1].CorpusItemID != "invariant:inv004" {
		t.Fatalf("labels are not in deterministic key order: %+v", got)
	}
	pres := s.PresentedIDs("pr1:aaa")
	for i := range pres {
		if pres[i] != s.CorpusIDs[i] {
			t.Fatal("presented order drifted from the corpus order the reference set fixed")
		}
	}
}
