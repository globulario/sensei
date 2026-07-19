// SPDX-License-Identifier: AGPL-3.0-only

package resultpipeline

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/questiongen"
)

func rep(blockerIDs ...string) closure.Report {
	var r closure.Report
	for _, id := range blockerIDs {
		r.Blockers = append(r.Blockers, closure.Blocker{ID: id})
	}
	return r
}

func item(blockerID, disposition string) questiongen.Item {
	return questiongen.Item{BlockerID: blockerID, Disposition: disposition}
}

// §6: every current blocker with exactly one current disposition is accounted;
// no_longer_backed is historical and never accounts for a current blocker.
func TestQuestionAccountingExactCoverage(t *testing.T) {
	res := questiongen.Result{Report: questiongen.Report{
		Generated:        []questiongen.Item{item("b1", questiongen.DispositionGenerated)},
		ExistingCoverage: []questiongen.Item{item("b2", questiongen.DispositionExistingCovers)},
		Skipped:          []questiongen.Item{item("b3", questiongen.DispositionSkippedMechanical)},
		NoLongerBacked:   []questiongen.Item{item("old", questiongen.DispositionNoLongerBacked)},
	}}
	got := architectQuestionsBundle(res, rep("b1", "b2", "b3"))
	if !got.AllBlockersAccountedFor || !got.ArchitectQuestionsActionable {
		t.Fatalf("expected fully accounted+actionable, got %+v", got)
	}
	if len(got.AccountedBlockerIDs) != 3 || len(got.UnaccountedBlockerIDs) != 0 || len(got.DuplicateAccountingIDs) != 0 {
		t.Fatalf("accounting sets wrong: %+v", got)
	}
	if len(got.HistoricalNoLongerBacked) != 1 || got.HistoricalNoLongerBacked[0] != "old" {
		t.Fatalf("no_longer_backed must be historical, got %+v", got.HistoricalNoLongerBacked)
	}
}

// §6: a current blocker with no current disposition is unaccounted, even if the
// disposition counts happen to balance via a historical or foreign entry.
func TestQuestionAccountingCatchesUnaccounted(t *testing.T) {
	res := questiongen.Result{Report: questiongen.Report{
		Generated:      []questiongen.Item{item("b1", questiongen.DispositionGenerated)},
		NoLongerBacked: []questiongen.Item{item("b2", questiongen.DispositionNoLongerBacked)},
	}}
	got := architectQuestionsBundle(res, rep("b1", "b2"))
	if got.AllBlockersAccountedFor {
		t.Fatal("b2 has only a historical disposition and must be unaccounted")
	}
	if len(got.UnaccountedBlockerIDs) != 1 || got.UnaccountedBlockerIDs[0] != "b2" {
		t.Fatalf("expected b2 unaccounted, got %+v", got.UnaccountedBlockerIDs)
	}
}

// §6: two dispositions for one current blocker is a duplicate, not "accounted".
func TestQuestionAccountingCatchesDuplicate(t *testing.T) {
	res := questiongen.Result{Report: questiongen.Report{
		Generated:        []questiongen.Item{item("b1", questiongen.DispositionGenerated)},
		ExistingCoverage: []questiongen.Item{item("b1", questiongen.DispositionExistingCovers)},
	}}
	got := architectQuestionsBundle(res, rep("b1"))
	if got.AllBlockersAccountedFor {
		t.Fatal("b1 disposed twice must not be accounted")
	}
	if len(got.DuplicateAccountingIDs) != 1 {
		t.Fatalf("expected duplicate, got %+v", got.DuplicateAccountingIDs)
	}
}

// §6: an unsupported-template / insufficient-grounding blocker is accounted for
// but keeps architect questions non-actionable (proof stays blocked).
func TestQuestionAccountingUnsupportedCriticalNotActionable(t *testing.T) {
	res := questiongen.Result{Report: questiongen.Report{
		Skipped: []questiongen.Item{item("b1", questiongen.DispositionUnsupportedTemplate)},
	}}
	got := architectQuestionsBundle(res, rep("b1"))
	if !got.AllBlockersAccountedFor {
		t.Fatal("b1 has a disposition, so it is accounted for")
	}
	if got.ArchitectQuestionsActionable {
		t.Fatal("an unsupported-template blocker must keep questions non-actionable")
	}
	if len(got.UnsupportedCritical) != 1 {
		t.Fatalf("expected unsupported-critical b1, got %+v", got.UnsupportedCritical)
	}
}

// §16 zero questions: no blockers means fully accounted and actionable, with a
// produced (empty) accounting rather than an inferred absence.
func TestQuestionAccountingZeroBlockers(t *testing.T) {
	got := architectQuestionsBundle(questiongen.Result{}, rep())
	if !got.AllBlockersAccountedFor || !got.ArchitectQuestionsActionable || got.GeneratedCount != 0 {
		t.Fatalf("zero blockers must be accounted+actionable+zero-count, got %+v", got)
	}
}
