// SPDX-License-Identifier: AGPL-3.0-only

package prospectivescore

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/prospective"
	"github.com/globulario/sensei/golang/architecture/prospectivelabel"
)

const (
	testManifestDigest = "manifest-digest"
	testCorpusDigest   = "corpus-digest"
)

func manifestFor(items map[string]string) prospective.Manifest {
	m := prospective.Manifest{
		ProtocolID:              "prospective-recall-protocol-v1",
		DigestSHA256:            testManifestDigest,
		BlindCorpusDigestSHA256: testCorpusDigest,
		RetrievalSurface:        prospective.RetrievalSurface{ID: "sensei.preflight.file_and_task.v1"},
	}
	m.World.Revision = "eac9603e"
	for _, key := range sortedKeys(items) {
		m.Items = append(m.Items, prospective.Item{ItemKey: key, Stratum: items[key]})
	}
	return m
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func labelsFor(t *testing.T, rows ...prospectivelabel.Label) prospectivelabel.LabelSet {
	t.Helper()
	for i := range rows {
		if rows[i].AssignmentMode == "" {
			rows[i].AssignmentMode = prospectivelabel.ModeIndividual
		}
	}
	ls := prospectivelabel.LabelSet{
		SchemaVersion:              prospectivelabel.LabelSetSchemaVersion,
		ProtocolID:                 "prospective-recall-protocol-v1",
		SampleManifestDigestSHA256: testManifestDigest,
		BlindCorpusDigestSHA256:    testCorpusDigest,
		Adjudicator:                "dave",
		SecondAdjudicatorStatus:    prospectivelabel.SecondAdjudicatorUnavailable,
		FrozenAt:                   "2026-08-23T00:00:00Z",
		Labels:                     rows,
	}
	sealed, err := ls.Seal()
	if err != nil {
		t.Fatalf("seal labels: %v", err)
	}
	return sealed
}

func runFor(t *testing.T, labelsDigest string, changes ...ChangeRun) Run {
	t.Helper()
	r := Run{
		SchemaVersion:              RunSchemaVersion,
		SampleManifestDigestSHA256: testManifestDigest,
		BlindCorpusDigestSHA256:    testCorpusDigest,
		LabelsDigestSHA256:         labelsDigest,
		WorldRevision:              "eac9603e",
		GraphDigestSHA256:          "def94857a06a",
		ExecutedAt:                 "2026-08-23T01:00:00Z",
		Changes:                    changes,
	}
	sealed, err := r.Seal()
	if err != nil {
		t.Fatalf("seal run: %v", err)
	}
	return sealed
}

func surfaced(ids ...string) []Surfaced {
	out := make([]Surfaced, 0, len(ids))
	for _, id := range ids {
		out = append(out, Surfaced{CorpusItemID: id, SurfacedAs: id, MatchRule: MatchExact, Channel: "direct_invariants"})
	}
	return out
}

func stratum(t *testing.T, s Score, name string) StratumScore {
	t.Helper()
	for _, st := range s.Strata {
		if st.Stratum == name {
			return st
		}
	}
	t.Fatalf("stratum %s missing from the score", name)
	return StratumScore{}
}

// Only `applicable` may enter the recall denominator. Every other label is
// present in this fixture, and none of them may move the ratio.
func TestCompute_RecallDenominatorCountsApplicableOnly(t *testing.T) {
	m := manifestFor(map[string]string{"pr1:a": prospective.StratumA})
	eligible := []string{"inv:1", "inv:2", "inv:3", "inv:4", "inv:5", "inv:6"}
	ls := labelsFor(t,
		prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:1", Label: prospectivelabel.LabelApplicable},
		prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:2", Label: prospectivelabel.LabelApplicable},
		prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:3", Label: prospectivelabel.LabelNotApplicable},
		prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:4", Label: prospectivelabel.LabelAmbiguous},
		prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:5", Label: prospectivelabel.LabelOutsideScope},
		prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:6", Label: prospectivelabel.LabelCannotAdjudicate},
	)
	run := runFor(t, ls.DigestSHA256, ChangeRun{
		ItemKey: "pr1:a", Stratum: prospective.StratumA, RetrievalStatus: StatusResolved,
		Surfaced: surfaced("inv:1"), ContextAvailable: []string{CtxChangeContents},
	})

	score, err := Compute(Input{Manifest: m, Labels: ls, Run: run, EligibleItemIDs: eligible})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	st := stratum(t, score, prospective.StratumA)
	if st.Recall.Denominator != 2 {
		t.Fatalf("recall denominator=%d, want 2 (only the two applicable labels)", st.Recall.Denominator)
	}
	if st.Recall.Numerator != 1 {
		t.Fatalf("recall numerator=%d, want 1", st.Recall.Numerator)
	}
	got := st.AdjudicatedLabels
	if got.Applicable != 2 || got.NotApplicable != 1 || got.Ambiguous != 1 || got.OutsideScope != 1 || got.CannotAdjudicate != 1 {
		t.Fatalf("label distribution collapsed somewhere: %+v", got)
	}
}

// Flooding with unjudgeable items drives primary nuisance down. The other two
// numbers are what make that visible, and all three must be emitted.
func TestCompute_FloodingWithUnresolvableItemsShowsInTheOtherTwoNuisanceNumbers(t *testing.T) {
	m := manifestFor(map[string]string{"pr1:a": prospective.StratumC})
	eligible := []string{"inv:hit", "inv:bad", "inv:x1", "inv:x2", "inv:x3", "inv:x4"}
	ls := labelsFor(t,
		prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:hit", Label: prospectivelabel.LabelApplicable},
		prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:bad", Label: prospectivelabel.LabelNotApplicable},
		prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:x1", Label: prospectivelabel.LabelAmbiguous},
		prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:x2", Label: prospectivelabel.LabelCannotAdjudicate},
		prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:x3", Label: prospectivelabel.LabelOutsideScope},
		prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:x4", Label: prospectivelabel.LabelCannotAdjudicate},
	)
	run := runFor(t, ls.DigestSHA256, ChangeRun{
		ItemKey: "pr1:a", Stratum: prospective.StratumC, RetrievalStatus: StatusResolved,
		Surfaced: surfaced(eligible...),
	})
	score, err := Compute(Input{Manifest: m, Labels: ls, Run: run, EligibleItemIDs: eligible})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	st := stratum(t, score, prospective.StratumC)

	if st.PrimaryNuisance.Denominator != 2 || st.PrimaryNuisance.Numerator != 1 {
		t.Fatalf("primary nuisance=%d/%d, want 1/2 over resolved labels only", st.PrimaryNuisance.Numerator, st.PrimaryNuisance.Denominator)
	}
	if st.UnresolvedSurfacedRate.Numerator != 4 || st.UnresolvedSurfacedRate.Denominator != 6 {
		t.Fatalf("unresolved surfaced=%d/%d, want 4/6", st.UnresolvedSurfacedRate.Numerator, st.UnresolvedSurfacedRate.Denominator)
	}
	if st.ConservativeNuisance.Numerator != 5 || st.ConservativeNuisance.Denominator != 6 {
		t.Fatalf("conservative nuisance=%d/%d, want 5/6", st.ConservativeNuisance.Numerator, st.ConservativeNuisance.Denominator)
	}
	if *st.PrimaryNuisance.Value >= *st.ConservativeNuisance.Value {
		t.Fatal("the fixture must show primary nuisance below the conservative bound, or it is not testing the flooding case")
	}
}

// An unlabelled pair is never a silent not_applicable, and never a silent hit.
func TestCompute_UnlabelledSurfacedItemCountsAsUnresolvedNotAsNegative(t *testing.T) {
	m := manifestFor(map[string]string{"pr1:a": prospective.StratumB})
	eligible := []string{"inv:1", "inv:2"}
	ls := labelsFor(t, prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:1", Label: prospectivelabel.LabelApplicable})
	run := runFor(t, ls.DigestSHA256, ChangeRun{
		ItemKey: "pr1:a", Stratum: prospective.StratumB, RetrievalStatus: StatusResolved,
		Surfaced: surfaced("inv:1", "inv:2"),
	})
	score, err := Compute(Input{Manifest: m, Labels: ls, Run: run, EligibleItemIDs: eligible})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	st := stratum(t, score, prospective.StratumB)
	if st.SurfacedLabels.Unlabelled != 1 {
		t.Fatalf("unlabelled surfaced=%d, want 1", st.SurfacedLabels.Unlabelled)
	}
	if st.PrimaryNuisance.Numerator != 0 || st.PrimaryNuisance.Denominator != 1 {
		t.Fatalf("primary nuisance=%d/%d: an unlabelled pair must not become a negative label",
			st.PrimaryNuisance.Numerator, st.PrimaryNuisance.Denominator)
	}
	if st.ConservativeNuisance.Numerator != 1 {
		t.Fatalf("conservative nuisance numerator=%d, want the unlabelled item counted as noise", st.ConservativeNuisance.Numerator)
	}
}

// A change production had no prospective channel for is a miss with an honest
// status, never a change quietly dropped from the denominator.
func TestCompute_NoProspectiveChannelIsAMissNotADrop(t *testing.T) {
	m := manifestFor(map[string]string{"pr1:a": prospective.StratumA})
	eligible := []string{"inv:1"}
	ls := labelsFor(t, prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:1", Label: prospectivelabel.LabelApplicable})
	run := runFor(t, ls.DigestSHA256, ChangeRun{
		ItemKey: "pr1:a", Stratum: prospective.StratumA, RetrievalStatus: StatusNoProspectiveChannel,
		StatusDetail: "production exposes no prospective query for a path that does not exist yet",
	})
	score, err := Compute(Input{Manifest: m, Labels: ls, Run: run, EligibleItemIDs: eligible})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	st := stratum(t, score, prospective.StratumA)
	if st.Recall.Denominator != 1 || st.Recall.Numerator != 0 {
		t.Fatalf("recall=%d/%d, want 0/1", st.Recall.Numerator, st.Recall.Denominator)
	}
	if st.RetrievalStatusCounts[StatusNoProspectiveChannel] != 1 {
		t.Fatalf("retrieval status distribution lost the no-channel case: %+v", st.RetrievalStatusCounts)
	}
	if len(st.Misses) != 1 || st.Misses[0].RetrievalStatus != StatusNoProspectiveChannel {
		t.Fatalf("miss set=%+v, want one miss carrying the honest status", st.Misses)
	}
}

// A change the runner never executed still reaches the denominator.
func TestCompute_UnexecutedChangeStaysInTheDenominator(t *testing.T) {
	m := manifestFor(map[string]string{"pr1:a": prospective.StratumA, "pr1:b": prospective.StratumA})
	eligible := []string{"inv:1"}
	ls := labelsFor(t,
		prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:1", Label: prospectivelabel.LabelApplicable},
		prospectivelabel.Label{ItemKey: "pr1:b", CorpusItemID: "inv:1", Label: prospectivelabel.LabelApplicable},
	)
	run := runFor(t, ls.DigestSHA256, ChangeRun{
		ItemKey: "pr1:a", Stratum: prospective.StratumA, RetrievalStatus: StatusResolved, Surfaced: surfaced("inv:1"),
	})
	score, err := Compute(Input{Manifest: m, Labels: ls, Run: run, EligibleItemIDs: eligible})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	st := stratum(t, score, prospective.StratumA)
	if st.ChangeCount != 2 {
		t.Fatalf("changes=%d, want both sampled changes scored", st.ChangeCount)
	}
	if st.Recall.Denominator != 2 || st.Recall.Numerator != 1 {
		t.Fatalf("recall=%d/%d, want 1/2", st.Recall.Numerator, st.Recall.Denominator)
	}
	if st.RetrievalStatusCounts[StatusUnavailable] != 1 {
		t.Fatalf("an unexecuted change must be recorded unavailable: %+v", st.RetrievalStatusCounts)
	}
}

// A and B never merge, and an empty stratum is reported as empty.
func TestCompute_StrataStaySeparateAndEmptyOnesAreReported(t *testing.T) {
	m := manifestFor(map[string]string{"pr1:a": prospective.StratumA, "pr1:b": prospective.StratumB})
	eligible := []string{"inv:1"}
	ls := labelsFor(t,
		prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:1", Label: prospectivelabel.LabelApplicable},
		prospectivelabel.Label{ItemKey: "pr1:b", CorpusItemID: "inv:1", Label: prospectivelabel.LabelApplicable},
	)
	run := runFor(t, ls.DigestSHA256,
		ChangeRun{ItemKey: "pr1:a", Stratum: prospective.StratumA, RetrievalStatus: StatusEmpty},
		ChangeRun{ItemKey: "pr1:b", Stratum: prospective.StratumB, RetrievalStatus: StatusResolved, Surfaced: surfaced("inv:1")},
	)
	score, err := Compute(Input{Manifest: m, Labels: ls, Run: run, EligibleItemIDs: eligible})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(score.Strata) != len(prospective.Strata) {
		t.Fatalf("score carries %d strata, want all %d including the empty ones", len(score.Strata), len(prospective.Strata))
	}
	a, b := stratum(t, score, prospective.StratumA), stratum(t, score, prospective.StratumB)
	if a.Recall.Value == nil || *a.Recall.Value != 0 {
		t.Fatalf("A recall=%v, want 0 with a real denominator", a.Recall.Value)
	}
	if b.Recall.Value == nil || *b.Recall.Value != 1 {
		t.Fatalf("B recall=%v, want 1", b.Recall.Value)
	}
	c := stratum(t, score, prospective.StratumC)
	if c.ChangeCount != 0 || c.Recall.Value != nil {
		t.Fatalf("an empty stratum must report an absent recall, got %+v", c.Recall)
	}
}

// An empty denominator has no rate. Reporting 0.0 would read as failure and
// 1.0 as success, and both would be inventions.
func TestCompute_AbsentMetricIsNilNotZero(t *testing.T) {
	m := manifestFor(map[string]string{"pr1:a": prospective.StratumD})
	eligible := []string{"inv:1"}
	ls := labelsFor(t, prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:1", Label: prospectivelabel.LabelNotApplicable})
	run := runFor(t, ls.DigestSHA256, ChangeRun{ItemKey: "pr1:a", Stratum: prospective.StratumD, RetrievalStatus: StatusResolved})
	score, err := Compute(Input{Manifest: m, Labels: ls, Run: run, EligibleItemIDs: eligible})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	st := stratum(t, score, prospective.StratumD)
	if st.Recall.Value != nil {
		t.Fatalf("recall=%v, want absent: nothing was adjudicated applicable", *st.Recall.Value)
	}
	if !contains(score.Macro.StrataWithoutRecall, prospective.StratumD) {
		t.Fatalf("macro summary must name the strata with no recall: %+v", score.Macro)
	}
	if score.Macro.RecallMacroAverage != nil {
		t.Fatalf("macro average=%v, want absent when no stratum has the metric", *score.Macro.RecallMacroAverage)
	}
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// The run must postdate the exact answer key it is graded by.
func TestCompute_RefusesARunBoundToDifferentLabels(t *testing.T) {
	m := manifestFor(map[string]string{"pr1:a": prospective.StratumA})
	eligible := []string{"inv:1"}
	ls := labelsFor(t, prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:1", Label: prospectivelabel.LabelApplicable})
	run := runFor(t, "some-other-answer-key", ChangeRun{ItemKey: "pr1:a", Stratum: prospective.StratumA, RetrievalStatus: StatusResolved})
	_, err := Compute(Input{Manifest: m, Labels: ls, Run: run, EligibleItemIDs: eligible})
	if err == nil {
		t.Fatal("scoring accepted a run executed against a different answer key")
	}
	if !strings.Contains(err.Error(), "postdate") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Labels answering a different sample are answers to a question nobody asked.
func TestCompute_RefusesLabelsBoundToADifferentSample(t *testing.T) {
	m := manifestFor(map[string]string{"pr1:a": prospective.StratumA})
	eligible := []string{"inv:1"}
	ls := labelsFor(t, prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:1", Label: prospectivelabel.LabelApplicable})
	ls.SampleManifestDigestSHA256 = "another-sample"
	run := runFor(t, ls.DigestSHA256, ChangeRun{ItemKey: "pr1:a", Stratum: prospective.StratumA, RetrievalStatus: StatusResolved})
	if _, err := Compute(Input{Manifest: m, Labels: ls, Run: run, EligibleItemIDs: eligible}); err == nil {
		t.Fatal("scoring accepted labels bound to a different sample")
	}
}

// An unrecognised retrieval status is refused rather than interpreted.
func TestRun_ValidateRefusesUnknownStatus(t *testing.T) {
	run := runFor(t, "labels", ChangeRun{ItemKey: "pr1:a", Stratum: prospective.StratumA, RetrievalStatus: "looks_fine"})
	if err := run.Validate(); err == nil {
		t.Fatal("an unknown retrieval status was accepted")
	}
}

// The report must carry every identity protocol section 12 lists, keep A and B
// apart, and never print a single blended headline number.
func TestRender_CarriesEveryRequiredIdentityAndNoBlendedHeadline(t *testing.T) {
	m := manifestFor(map[string]string{"pr1:a": prospective.StratumA, "pr1:b": prospective.StratumB})
	m.Strata = []prospective.Stratum{
		{Stratum: prospective.StratumA, InventoryDigestSHA256: "aaaaaaaaaaaaaaaa", Population: 209, Target: 12, Selected: 12, Status: prospective.StatusSampled},
		{Stratum: prospective.StratumB, InventoryDigestSHA256: "bbbbbbbbbbbbbbbb", Population: 361, Target: 12, Selected: 12, Status: prospective.StatusSampled},
	}
	m.Exclusions = []prospective.ExclusionCount{{Reason: "no_single_base_revision", Count: 150}}
	eligible := []string{"inv:1", "inv:2"}
	ls := labelsFor(t,
		prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:1", Label: prospectivelabel.LabelApplicable},
		prospectivelabel.Label{ItemKey: "pr1:b", CorpusItemID: "inv:2", Label: prospectivelabel.LabelApplicable},
	)
	run := runFor(t, ls.DigestSHA256,
		ChangeRun{ItemKey: "pr1:a", Stratum: prospective.StratumA, RetrievalStatus: StatusNoProspectiveChannel},
		ChangeRun{ItemKey: "pr1:b", Stratum: prospective.StratumB, RetrievalStatus: StatusResolved,
			Surfaced: surfaced("inv:2"), ContextAvailable: []string{CtxChangeContents, CtxImports}},
	)
	score, err := Compute(Input{Manifest: m, Labels: ls, Run: run, EligibleItemIDs: eligible})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	out := Render(score, m)

	for _, want := range []string{
		score.WorldRevision, score.GraphDigestSHA256, score.SampleManifestDigestSHA256,
		score.BlindCorpusDigestSHA256, score.LabelsDigestSHA256, score.RunDigestSHA256,
		score.DigestSHA256, score.RetrievalSurfaceID, score.Adjudicator,
		prospectivelabel.SecondAdjudicatorUnavailable, score.LabelsFrozenAt, score.RunExecutedAt,
		"no_single_base_revision", "aaaaaaaaaaaa", "bbbbbbbbbbbb",
		prospective.StratumA, prospective.StratumB, prospective.StratumC, prospective.StratumD,
		StatusNoProspectiveChannel, CtxImports,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("report omits %q", want)
		}
	}
	// Every stratum row carries all three nuisance numbers beside recall.
	for _, name := range prospective.Strata {
		line := stratumLine(t, out, name)
		if strings.Count(line, "|") != 7 {
			t.Fatalf("stratum row for %s does not carry recall plus all three nuisance numbers: %q", name, line)
		}
	}
	if strings.Contains(strings.ToLower(out), "overall score") || strings.Contains(strings.ToLower(out), "headline") {
		t.Fatalf("report emits a blended headline number:\n%s", out)
	}
}

func stratumLine(t *testing.T, report, stratum string) string {
	t.Helper()
	for _, line := range strings.Split(report, "\n") {
		if strings.HasPrefix(line, "| "+stratum+" | ") && strings.Contains(line, "(") {
			return line
		}
	}
	t.Fatalf("no result row for %s in:\n%s", stratum, report)
	return ""
}

// A miss set is complete, never a selection made after the scores were visible.
func TestRender_MissSetIsComplete(t *testing.T) {
	m := manifestFor(map[string]string{"pr1:a": prospective.StratumA})
	eligible := []string{"inv:1", "inv:2", "inv:3"}
	ls := labelsFor(t,
		prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:1", Label: prospectivelabel.LabelApplicable},
		prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:2", Label: prospectivelabel.LabelApplicable},
		prospectivelabel.Label{ItemKey: "pr1:a", CorpusItemID: "inv:3", Label: prospectivelabel.LabelApplicable},
	)
	run := runFor(t, ls.DigestSHA256, ChangeRun{ItemKey: "pr1:a", Stratum: prospective.StratumA, RetrievalStatus: StatusEmpty})
	score, err := Compute(Input{Manifest: m, Labels: ls, Run: run, EligibleItemIDs: eligible})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	out := Render(score, m)
	for _, id := range eligible {
		if !strings.Contains(out, id) {
			t.Fatalf("missed item %s is not in the report; the miss set has been trimmed", id)
		}
	}
	if !strings.Contains(out, "3 missed") {
		t.Fatalf("report does not state the complete miss count:\n%s", out)
	}
}
