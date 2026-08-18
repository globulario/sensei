// SPDX-License-Identifier: AGPL-3.0-only

package extractbudget

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func candidates(specs ...[2]string) []Candidate {
	out := make([]Candidate, 0, len(specs))
	for _, s := range specs {
		out = append(out, Candidate{RelPath: s[0], AbsPath: "/repo/" + s[0], Size: int64(len(s[1]))})
	}
	return out
}

func sized(rel string, size int64) Candidate {
	return Candidate{RelPath: rel, AbsPath: "/repo/" + rel, Size: size}
}

// The zero budget must be exactly "no limits", because it is what every
// existing caller passes implicitly. A zero value that quietly meant "bound
// everything to zero" would turn adoption into a repository-wide outage.
func TestZeroBudgetIsUnbounded(t *testing.T) {
	var b Budget
	if b.Bounded() {
		t.Fatal("the zero budget claims to bound something")
	}
	sel := Select(candidates([2]string{"a.go", "aaa"}, [2]string{"b.go", "bbbb"}), b)
	if len(sel.Files) != 2 || len(sel.Truncated) != 0 {
		t.Fatalf("zero budget cut something: %+v", sel)
	}
	if !b.InScope("anything/at/all.go") {
		t.Error("zero budget excluded a path")
	}
}

// Exclusion matches path segments, never raw string prefixes. "internal" must
// not swallow "internal_docs/" -- an operator excluding one directory would
// silently lose an unrelated one, and the receipt would truthfully report a
// complete search of a scope that was not what they wrote.
func TestScopesMatchSegmentsNotStringPrefixes(t *testing.T) {
	b := Budget{ExcludePaths: []string{"internal"}}.Normalize()
	if b.InScope("internal/x.go") {
		t.Error("excluded directory was admitted")
	}
	if !b.InScope("internal_docs/x.go") {
		t.Error("a sibling directory sharing a name prefix was excluded")
	}
}

// Exclude wins over include: the narrower, negative statement is the one a
// caller is more likely to have meant literally.
func TestExcludeBeatsInclude(t *testing.T) {
	b := Budget{IncludePaths: []string{"golang"}, ExcludePaths: []string{"golang/vendor"}}.Normalize()
	if !b.InScope("golang/a.go") {
		t.Error("included path was not admitted")
	}
	if b.InScope("golang/vendor/a.go") {
		t.Error("a path inside an excluded subtree was admitted")
	}
	if b.InScope("cmd/a.go") {
		t.Error("a path outside every include scope was admitted")
	}
}

// A budget whose excludes swallow every include would produce an empty,
// "completed" extraction of a repository nobody searched. That reads as
// evidence of absence, and it is not.
func TestBudgetThatExcludesEverythingItIncludesIsRefused(t *testing.T) {
	err := Budget{IncludePaths: []string{"golang/architecture"}, ExcludePaths: []string{"golang"}}.Validate()
	if err == nil {
		t.Fatal("a budget with no reachable scope was accepted")
	}
	if !strings.Contains(err.Error(), "empty extraction") {
		t.Errorf("the refusal does not say why it matters: %v", err)
	}
}

func TestBudgetRefusesEscapingScopes(t *testing.T) {
	for name, b := range map[string]Budget{
		"absolute include": {IncludePaths: []string{"/etc"}},
		"parent exclude":   {ExcludePaths: []string{"../secrets"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := b.Validate(); err == nil {
				t.Fatal("an escaping scope was accepted")
			}
		})
	}
}

// Scopes are applied BEFORE ceilings. A caller who narrowed the search to one
// directory should get all of it, not a file ceiling already spent on files
// the scope had ruled out.
func TestScopesApplyBeforeCeilings(t *testing.T) {
	in := []Candidate{
		sized("vendor/a.go", 1), sized("vendor/b.go", 1), sized("vendor/c.go", 1),
		sized("wanted/x.go", 1), sized("wanted/y.go", 1),
	}
	sel := Select(in, Budget{MaxFiles: 2, ExcludePaths: []string{"vendor"}})
	if len(sel.Files) != 2 {
		t.Fatalf("kept %d files, want 2", len(sel.Files))
	}
	for _, f := range sel.Files {
		if strings.Contains(f, "vendor") {
			t.Fatalf("a ceiling was spent on an out-of-scope file: %v", sel.Files)
		}
	}
	if sel.OutOfScopeFiles != 3 {
		t.Errorf("out-of-scope count = %d, want 3", sel.OutOfScopeFiles)
	}
}

// The cut has to be deterministic and it has to be REPORTABLE. Selection is
// sorted, so naming the first unsearched path is a complete statement of what
// was skipped -- without a receipt that grows with the repository.
func TestTruncationIsDeterministicAndNamesWhatWasNotSearched(t *testing.T) {
	in := []Candidate{sized("c.go", 10), sized("a.go", 10), sized("b.go", 10), sized("d.go", 10)}
	first := Select(in, Budget{MaxFiles: 2})
	second := Select(in, Budget{MaxFiles: 2})

	if len(first.Files) != 2 || first.Files[0] != "/repo/a.go" || first.Files[1] != "/repo/b.go" {
		t.Fatalf("selection is not the sorted prefix: %v", first.Files)
	}
	if first.FirstUnsearched != "c.go" {
		t.Errorf("FirstUnsearched = %q, want %q", first.FirstUnsearched, "c.go")
	}
	if first.UnsearchedFiles != 2 || first.UnsearchedBytes != 20 {
		t.Errorf("unsearched = %d files / %d bytes, want 2 / 20", first.UnsearchedFiles, first.UnsearchedBytes)
	}
	if len(first.Truncated) != 1 || first.Truncated[0] != "max_files" {
		t.Errorf("truncated = %v, want [max_files]", first.Truncated)
	}
	if first.FirstUnsearched != second.FirstUnsearched || len(first.Files) != len(second.Files) {
		t.Error("two identical calls produced different selections")
	}
}

// The byte ceiling cuts at a whole file. Half a file is not weaker evidence,
// it is evidence about source positions that do not mean what they say.
func TestByteCeilingCutsAtAWholeFile(t *testing.T) {
	sel := Select([]Candidate{sized("a.go", 100), sized("b.go", 100), sized("c.go", 100)}, Budget{MaxSourceBytes: 250})
	if len(sel.Files) != 2 || sel.Consumption.SourceBytes != 200 {
		t.Fatalf("kept %d files / %d bytes, want 2 / 200", len(sel.Files), sel.Consumption.SourceBytes)
	}
	if sel.FirstUnsearched != "c.go" {
		t.Errorf("FirstUnsearched = %q", sel.FirstUnsearched)
	}
}

// A cut must always produce a limitation. A bounded run that reported no
// limitation would be indistinguishable from a complete one.
func TestEveryCutProducesALimitationNamingTheGap(t *testing.T) {
	sel := Select([]Candidate{sized("a.go", 1), sized("b.go", 1), sized("z/c.go", 1)}, Budget{MaxFiles: 1, ExcludePaths: []string{"z"}})
	lims := sel.Limitations("go_semantic_extractor")
	if len(lims) != 2 {
		t.Fatalf("want a limitation for the ceiling and one for the scope, got %d: %+v", len(lims), lims)
	}
	joined := lims[0].Reason + " " + lims[1].Reason
	for _, want := range []string{"max_files", "b.go", "outside the bound include/exclude scopes"} {
		if !strings.Contains(joined, want) {
			t.Errorf("limitations do not mention %q: %s", want, joined)
		}
	}
	for _, l := range lims {
		if l.Blocking {
			t.Error("a bounded extraction is valid evidence; its limitation must not be blocking")
		}
	}
}

// Exhaustion must come from the stage that cut, never from comparing
// consumption against a bound. The comparison was wrong in BOTH directions,
// and both directions matter:
//
//   - three 100-byte files under a 250-byte ceiling keep 200 and cut the third,
//     yet 200 >= 250 is false, so a genuinely partial run reported "completed";
//   - a repository totalling exactly 250 bytes reported "budget_exhausted"
//     while nothing was skipped.
//
// The first is the failure this whole contract exists to prevent: partial
// evidence presented as complete.
func TestExhaustionComesFromActualCutsNotFromComparingNumbers(t *testing.T) {
	b := Budget{MaxSourceBytes: 250}

	cut := Select([]Candidate{sized("a.go", 100), sized("b.go", 100), sized("c.go", 100)}, b)
	if len(cut.Truncated) == 0 {
		t.Fatal("selection did not report the cut it made")
	}
	partial := ComposeReceipt(b, cut.Consumption, RunOutcome{Truncated: cut.Truncated})
	if partial.Status != StatusBudgetExhausted {
		t.Fatalf("a run that cut a file reported %q; consumption was %d against a bound of 250",
			partial.Status, cut.Consumption.SourceBytes)
	}

	// Exactly at the bound, nothing cut: not exhausted.
	whole := Select([]Candidate{sized("a.go", 125), sized("b.go", 125)}, b)
	if len(whole.Truncated) != 0 {
		t.Fatalf("selection reported a cut it did not make: %v", whole.Truncated)
	}
	complete := ComposeReceipt(b, whole.Consumption, RunOutcome{Truncated: whole.Truncated})
	if complete.Status != StatusCompleted {
		t.Fatalf("a run that skipped nothing reported %q at exactly the bound", complete.Status)
	}
}

// A cut site cannot invent a dimension the reader has no rule for, and
// duplicates from two stages collapse.
func TestExhaustedDimensionsAreClosedAndDeduplicated(t *testing.T) {
	r := ComposeReceipt(Budget{MaxFiles: 1}, Consumption{},
		RunOutcome{Truncated: []string{"max_files", "max_files", "not_a_dimension"}})
	if len(r.ExhaustedDimensions) != 1 || r.ExhaustedDimensions[0] != DimensionFiles {
		t.Fatalf("exhausted = %v, want exactly [%s]", r.ExhaustedDimensions, DimensionFiles)
	}
}

// Status precedence is load-bearing. A cancelled run may never have reached a
// limit, and reporting budget_exhausted would send the operator to widen a
// limit that was not the constraint.
func TestStatusPrecedence(t *testing.T) {
	b := Budget{MaxFiles: 1}
	full := Consumption{Files: 1}

	exhausted := RunOutcome{Truncated: []string{DimensionFiles}}
	if r := ComposeReceipt(b, full, exhausted); r.Status != StatusBudgetExhausted {
		t.Errorf("status = %q, want budget_exhausted", r.Status)
	} else if len(r.ExhaustedDimensions) != 1 {
		t.Errorf("budget_exhausted receipt does not name the limit: %+v", r)
	}
	if r := ComposeReceipt(b, full, RunOutcome{Cancelled: true, Truncated: []string{DimensionFiles}}); r.Status != StatusCancelled {
		t.Errorf("cancellation was reported as %q; a limit must not be blamed for the caller's own stop", r.Status)
	} else if len(r.ExhaustedDimensions) != 0 {
		t.Errorf("a cancelled run named budget dimensions: %v", r.ExhaustedDimensions)
	}
	if r := ComposeReceipt(b, full, RunOutcome{Cancelled: true, Truncated: []string{DimensionFiles}, UnavailableReason: "go toolchain absent"}); r.Status != StatusUnavailable {
		t.Errorf("status = %q, want unavailable to outrank everything", r.Status)
	}
	if r := ComposeReceipt(b, Consumption{}, RunOutcome{Degraded: true}); r.Status != StatusPartial {
		t.Errorf("status = %q, want partial", r.Status)
	}
	if r := ComposeReceipt(b, Consumption{}, RunOutcome{}); r.Status != StatusCompleted {
		t.Errorf("status = %q, want completed", r.Status)
	}
}

// The wall clock is the one limit that cannot be reported by comparing a
// measured number against a bound: elapsed time is nondeterministic, and a
// receipt carrying it would never be equal twice. It is reported as an outcome
// instead -- and it must stay distinguishable from the caller stopping the
// run, since the two prescribe opposite responses.
func TestWallClockExhaustionIsADistinctOutcomeFromCancellation(t *testing.T) {
	b := Budget{MaxWallClock: time.Second}

	expired := ComposeReceipt(b, Consumption{}, RunOutcome{WallClockExhausted: true})
	if expired.Status != StatusBudgetExhausted {
		t.Fatalf("status = %q, want budget_exhausted", expired.Status)
	}
	if len(expired.ExhaustedDimensions) != 1 || expired.ExhaustedDimensions[0] != "max_wall_clock" {
		t.Fatalf("exhausted = %v, want [max_wall_clock]", expired.ExhaustedDimensions)
	}

	stopped := ComposeReceipt(b, Consumption{}, RunOutcome{Cancelled: true})
	if stopped.Status != StatusCancelled || len(stopped.ExhaustedDimensions) != 0 {
		t.Fatalf("a caller-cancelled run was reported as %q %v", stopped.Status, stopped.ExhaustedDimensions)
	}
}

// Two identical runs must produce byte-identical receipts, which is why no
// measured duration lives in Consumption.
func TestReceiptIsDeterministic(t *testing.T) {
	b := Budget{MaxFiles: 3, ExcludePaths: []string{"vendor"}}
	c := Consumption{Files: 2, SourceBytes: 40, Observations: 7}
	if !reflect.DeepEqual(ComposeReceipt(b, c, RunOutcome{}), ComposeReceipt(b, c, RunOutcome{})) {
		t.Fatal("two identical compositions produced different receipts")
	}
}

// Only the enumerated dispositions exist; a caller must not be able to invent
// a status the reader has no rule for.
func TestStatusVocabularyIsClosed(t *testing.T) {
	for _, s := range []Status{StatusCompleted, StatusPartial, StatusBudgetExhausted, StatusUnavailable, StatusCancelled} {
		if !IsValidStatus(s) {
			t.Errorf("%q is not accepted by its own validator", s)
		}
	}
	if IsValidStatus("mostly_fine") {
		t.Error("an invented status was accepted")
	}
}

func TestNormalizeMakesEquivalentBudgetsIdentical(t *testing.T) {
	a := Budget{IncludePaths: []string{"./golang/", "golang", " cmd "}, ExcludePaths: []string{""}}.Normalize()
	b := Budget{IncludePaths: []string{"cmd", "golang"}}.Normalize()
	if len(a.IncludePaths) != 2 || a.IncludePaths[0] != "cmd" || a.IncludePaths[1] != "golang" {
		t.Fatalf("include = %v", a.IncludePaths)
	}
	if len(a.IncludePaths) != len(b.IncludePaths) {
		t.Fatal("two equivalent budgets normalized differently")
	}
}

func TestDeadlineOnlyBindsWhenWallClockIsSet(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	if _, bounded := (Budget{}).Deadline(start); bounded {
		t.Error("an unset wall clock produced a deadline")
	}
	d, bounded := Budget{MaxWallClock: 30 * time.Second}.Deadline(start)
	if !bounded || !d.Equal(start.Add(30*time.Second)) {
		t.Errorf("deadline = %v, bounded = %v", d, bounded)
	}
}

// A scope that names nothing must not produce a "completed" run over zero
// observations. That reads as evidence that the repository contains nothing of
// interest, when the real fact is that someone wrote a scope matching no file.
func TestScopesThatMatchNothingAreFlaggedRatherThanReportedComplete(t *testing.T) {
	sel := Select([]Candidate{sized("golang/a.go", 1), sized("cmd/b.go", 1)}, Budget{IncludePaths: []string{"nowhere"}})
	if len(sel.Files) != 0 {
		t.Fatalf("selection was not empty: %v", sel.Files)
	}
	if !sel.ScopesMatchedNothing {
		t.Fatal("an empty selection under a scope was not flagged")
	}
	var said bool
	for _, l := range sel.Limitations("go_semantic_extractor") {
		if strings.Contains(l.Reason, "matched no source file") {
			said = true
		}
	}
	if !said {
		t.Errorf("the limitations do not say the scope matched nothing: %+v", sel.Limitations("go_semantic_extractor"))
	}
}

// An unscoped run over an empty candidate set is a different fact -- there was
// nothing to search, not a scope that named nothing -- and must not be flagged
// as a mis-written scope.
func TestAnEmptyRepositoryIsNotAMisWrittenScope(t *testing.T) {
	sel := Select(nil, Budget{})
	if sel.ScopesMatchedNothing {
		t.Fatal("an empty candidate set with no scopes was reported as a scope that matched nothing")
	}
}
