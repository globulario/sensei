// SPDX-License-Identifier: AGPL-3.0-only

package evalsample

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

func testOptions() Options {
	return Options{
		ProtocolID:           "phase10-reference-protocol-v1",
		ProtocolDigestSHA256: "aaaabbbbccccdddd",
		Seed:                 "seed-under-test",
		GeneratedAt:          "2026-08-22T05:00:00Z",
	}
}

func fact(extractor, subject, predicate, object, file string, line int) architecture.Fact {
	return architecture.Fact{
		Kind: "topology", Subject: subject, Predicate: predicate, Object: object,
		Extractor: extractor,
		Evidence:  architecture.Evidence{SourceFile: file, LineStart: line, LineEnd: line},
	}
}

// skewedWorld mirrors the real world-1 shape: one provider dominating by
// orders of magnitude, one thin provider that matters architecturally.
func skewedWorld() World {
	w := World{
		Name: "world1_sensei_self",
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain: "github.com/globulario/sensei",
			Revision:         "cd76ae3f94a4",
			RevisionStatus:   architecture.RevisionResolved,
		},
		RecallInventory: []string{"pkg/a", "pkg/b", "pkg/c"},
	}
	for i := 0; i < 500; i++ {
		w.Observations = append(w.Observations, fact("state_extractor", "s", "holds", objectN(i), "a.go", i+1))
	}
	for i := 0; i < 4; i++ {
		w.Observations = append(w.Observations, fact("contract_extractor", "c", "requires", objectN(i), "b.go", i+1))
	}
	return w
}

func objectN(i int) string { return "obj-" + string(rune('a'+i%26)) + itoa(i) }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// TestThinProviderIsNotErasedByALargeOne is the reason the protocol's section
// 6 exists, expressed as a test.
//
// world 1 really is skewed about a thousand to one — 159,729 state_extractor
// observations against 160 contract_extractor ones. A sample drawn uniformly
// over observations would be a measurement of the largest extractor wearing
// the name of a measurement of Sensei, and the thinnest lane, which is the one
// carrying contracts, would contribute almost nothing.
func TestThinProviderIsNotErasedByALargeOne(t *testing.T) {
	m, _, err := Build([]World{skewedWorld()}, testOptions())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	perProvider := map[string]int{}
	for _, it := range m.Items {
		if it.Lane == LanePrecision {
			perProvider[it.ProviderID]++
		}
	}
	if perProvider["state_extractor"] != DefaultPrecisionPerProvider {
		t.Errorf("large provider contributed %d precision items, want the stratum target %d",
			perProvider["state_extractor"], DefaultPrecisionPerProvider)
	}
	// The thin provider has only 4 distinct claims, so all four are taken.
	if perProvider["contract_extractor"] != 4 {
		t.Errorf("thin provider contributed %d precision items, want all 4 it has", perProvider["contract_extractor"])
	}

	for _, st := range m.Strata {
		if st.Lane == LanePrecision && st.ProviderID == "contract_extractor" && st.Status != StatusSampledAll {
			t.Errorf("a provider whose whole population was taken is recorded as %q, want %q", st.Status, StatusSampledAll)
		}
	}
}

// TestSelectionIsReproducibleFromTheSeed. "Deterministic" has to be checkable,
// not asserted: the manifest publishes the seed and every selection key so a
// reader can recompute the draw and dispute it.
func TestSelectionIsReproducibleFromTheSeed(t *testing.T) {
	a, _, err := Build([]World{skewedWorld()}, testOptions())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	b, _, err := Build([]World{skewedWorld()}, testOptions())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if a.DigestSHA256 != b.DigestSHA256 {
		t.Fatalf("two builds of the same input produced different manifests: %s vs %s", a.DigestSHA256, b.DigestSHA256)
	}
	if a.DigestSHA256 == "" {
		t.Fatal("manifest carries no digest; a release cannot name a ruler it cannot identify")
	}
}

// TestChangingTheSeedIsANewSample, per section 6.2. If a different seed drew
// the same items, the seed would be decoration and re-drawing a sample after
// seeing a score would be undetectable.
func TestChangingTheSeedIsANewSample(t *testing.T) {
	opts := testOptions()
	a, _, err := Build([]World{skewedWorld()}, opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	opts.Seed = "a-different-committed-seed"
	b, _, err := Build([]World{skewedWorld()}, opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if a.DigestSHA256 == b.DigestSHA256 {
		t.Fatal("two seeds produced the same manifest identity; the seed is not part of the sample")
	}
	if sameItems(a, b) {
		t.Error("two seeds drew exactly the same items; selection does not depend on the seed")
	}
}

func sameItems(a, b Manifest) bool {
	if len(a.Items) != len(b.Items) {
		return false
	}
	seen := map[string]bool{}
	for _, it := range a.Items {
		seen[it.ItemKey] = true
	}
	for _, it := range b.Items {
		if !seen[it.ItemKey] {
			return false
		}
	}
	return true
}

// TestSelectionCannotSeeAClaimsContent is the anti-self-grading property.
//
// If the draw could be moved by a claim's text, confidence or plausibility,
// the sample would be an opinion about the output being sampled. Here two
// worlds differ in every observation's OBJECT and confidence while the
// identities the key is built from are unchanged, and the same items must be
// drawn.
func TestSelectionCannotSeeAClaimsContent(t *testing.T) {
	base := World{
		Name:    "w",
		Binding: architecture.ClaimDocumentBinding{RepositoryDomain: "d", Revision: "r", RevisionStatus: architecture.RevisionResolved},
	}
	for i := 0; i < 50; i++ {
		base.Observations = append(base.Observations, fact("p", "s", "holds", objectN(i), "a.go", i+1))
	}

	drawn, _, err := Build([]World{base}, testOptions())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Same identities, different confidence. Confidence is not part of the
	// identity, so the draw must not move.
	tweaked := base
	tweaked.Observations = append([]architecture.Fact(nil), base.Observations...)
	for i := range tweaked.Observations {
		tweaked.Observations[i].Confidence = 0.99
	}
	after, _, err := Build([]World{tweaked}, testOptions())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !sameItems(drawn, after) {
		t.Error("changing observation confidence changed which items were sampled; the draw can see content it must not see")
	}
}

// TestRepeatedEmissionsBecomeOneQuestionWithItsCountKept.
//
// Adjudicating the same claim about the same anchor twice returns the same
// answer, so it adds no evidence while doubling that claim's weight in a
// precision denominator. Collapsed for that reason — and the count is kept, so
// the collapse is visible rather than discovered later inside a ratio.
func TestRepeatedEmissionsBecomeOneQuestionWithItsCountKept(t *testing.T) {
	w := World{Name: "w", Binding: architecture.ClaimDocumentBinding{RepositoryDomain: "d", Revision: "r", RevisionStatus: architecture.RevisionResolved}}
	for i := 0; i < 7; i++ {
		w.Observations = append(w.Observations, fact("p", "s", "holds", "same-object", "a.go", 10))
	}
	m, _, err := Build([]World{w}, testOptions())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	var precision []Item
	for _, it := range m.Items {
		if it.Lane == LanePrecision {
			precision = append(precision, it)
		}
	}
	if len(precision) != 1 {
		t.Fatalf("seven emissions of one claim produced %d adjudication items, want 1", len(precision))
	}
	if precision[0].Multiplicity != 7 {
		t.Errorf("multiplicity is %d, want 7; the collapse hid how many emissions it stood for", precision[0].Multiplicity)
	}
	for _, st := range m.Strata {
		if st.Lane == LanePrecision {
			if st.Emissions != 7 || st.Population != 1 {
				t.Errorf("stratum reports emissions=%d population=%d, want 7 and 1 so a reader can see the collapse", st.Emissions, st.Population)
			}
		}
	}
}

// TestAnEmptyLaneIsReportedNotOmitted. A lane that vanishes reads as a lane
// nobody needed, and no later reader can tell that from a lane whose
// population was genuinely zero. Sensei types its absences.
func TestAnEmptyLaneIsReportedNotOmitted(t *testing.T) {
	w := World{
		Name:         "w",
		Binding:      architecture.ClaimDocumentBinding{RepositoryDomain: "d", Revision: "r", RevisionStatus: architecture.RevisionResolved},
		Observations: []architecture.Fact{fact("p", "s", "holds", "o", "a.go", 1)},
		// no recall inventory, no counterexamples, no contradictions
	}
	m, _, err := Build([]World{w}, testOptions())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, lane := range []string{LaneRecallUnit, LaneContradiction, LaneChallenge} {
		var found *Stratum
		for i := range m.Strata {
			if m.Strata[i].Lane == lane {
				found = &m.Strata[i]
			}
		}
		if found == nil {
			t.Errorf("lane %s produced nothing and was omitted entirely; an absent lane must still be reported", lane)
			continue
		}
		if found.Status != StatusAbsent {
			t.Errorf("lane %s has an empty population but status %q", lane, found.Status)
		}
		if strings.TrimSpace(found.Reason) == "" {
			t.Errorf("lane %s claims an empty population without saying why", lane)
		}
	}
}

// TestContradictionCasesAreSelectedNotDecided. Section 8 gives the human three
// possible states and no winner. The manifest must therefore present both
// sides and record no verdict.
func TestContradictionCasesAreSelectedNotDecided(t *testing.T) {
	w := World{
		Name:    "w",
		Binding: architecture.ClaimDocumentBinding{RepositoryDomain: "d", Revision: "r", RevisionStatus: architecture.RevisionResolved},
		// "owns" is declared single-valued; "depends_on" is not, so the pair
		// below it must NOT open a case.
		FunctionalPredicates: []string{"owns"},
		Observations: []architecture.Fact{
			fact("p", "component.x", "owns", "state.a", "a.go", 1),
			fact("q", "component.x", "owns", "state.b", "b.go", 2),
			fact("p", "component.x", "depends_on", "lib.a", "c.go", 3),
			fact("p", "component.x", "depends_on", "lib.b", "c.go", 4),
		},
	}
	m, blind, err := Build([]World{w}, testOptions())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	var cases []Item
	for _, it := range m.Items {
		if it.Lane == LaneContradiction {
			cases = append(cases, it)
		}
	}
	if len(cases) != 1 {
		t.Fatalf("expected exactly the one functional-predicate disagreement to open a case, got %d; a multi-valued relation is not a contradiction", len(cases))
	}
	if len(cases[0].EvidenceIDs) < 2 {
		t.Errorf("a contradiction case carries %d evidence anchors, want both sides pinned", len(cases[0].EvidenceIDs))
	}

	items := blind[blindKey("w", LaneContradiction)]
	if len(items) != 1 || len(items[0].Alternatives) != 2 {
		t.Fatalf("the adjudicator's view does not present both sides: %+v", items)
	}

	// Nothing in the manifest may express a verdict.
	raw, _ := json.Marshal(m)
	for _, forbidden := range []string{"supported", "unsupported", "verdict", "correct", "wrong"} {
		if strings.Contains(strings.ToLower(string(raw)), forbidden) {
			t.Errorf("the sample manifest contains %q; selection must be frozen before answers exist", forbidden)
		}
	}
}

// TestAMultiValuedRelationIsNotADisagreement is the defect this package
// shipped and then removed.
//
// Treating every repeated (subject, predicate) with differing objects as a
// contradiction produced 23,641 "cases" from world 1, essentially all of them
// components legitimately depending on many things. That would have handed an
// adjudicator thousands of non-questions and let the lane report a healthy
// denominator built from nothing.
//
// Whether a predicate is single-valued is a statement about the ontology, so
// it comes from outside — and when nobody supplies it, the lane must say it is
// UNDRAWN rather than clean.
func TestAMultiValuedRelationIsNotADisagreement(t *testing.T) {
	w := World{
		Name:    "w",
		Binding: architecture.ClaimDocumentBinding{RepositoryDomain: "d", Revision: "r", RevisionStatus: architecture.RevisionResolved},
		Observations: []architecture.Fact{
			fact("p", "component.x", "depends_on", "lib.a", "c.go", 1),
			fact("p", "component.x", "depends_on", "lib.b", "c.go", 2),
			fact("p", "component.x", "depends_on", "lib.c", "c.go", 3),
		},
	}
	m, _, err := Build([]World{w}, testOptions())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, it := range m.Items {
		if it.Lane == LaneContradiction {
			t.Fatalf("a multi-valued relation opened a contradiction case: %+v", it)
		}
	}

	var st *Stratum
	for i := range m.Strata {
		if m.Strata[i].Lane == LaneContradiction {
			st = &m.Strata[i]
		}
	}
	if st == nil {
		t.Fatal("the contradiction lane vanished instead of reporting why it drew nothing")
	}
	if !strings.Contains(st.Reason, "undrawn") {
		t.Errorf("an undeclared ontology reads as a clean result: %q", st.Reason)
	}
}

// TestTheBlindViewCannotLeakTheProvider, per section 12. The provider label is
// absent by construction rather than blanked, so a reader who was not meant to
// have it cannot restore it from the payload they were given.
func TestTheBlindViewCannotLeakTheProvider(t *testing.T) {
	m, blind, err := Build([]World{skewedWorld()}, testOptions())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	raw, err := json.Marshal(blind)
	if err != nil {
		t.Fatalf("marshal blind view: %v", err)
	}
	for _, provider := range []string{"state_extractor", "contract_extractor"} {
		if strings.Contains(string(raw), provider) {
			t.Errorf("the blinded view names provider %q, so support labelling is no longer blind to it", provider)
		}
	}

	// The claim and its anchor MUST survive: blinding is an anti-bias tool,
	// never a reason to withhold the evidence a judgement needs.
	items := blind[blindKey("world1_sensei_self", LanePrecision)]
	if len(items) == 0 {
		t.Fatal("no blinded precision items were materialized")
	}
	for _, it := range items {
		if strings.TrimSpace(it.Claim) == "" {
			t.Fatal("a blinded item carries no claim, so support cannot be judged at all")
		}
		if len(it.EvidenceIDs) == 0 {
			t.Fatal("a blinded item carries no evidence anchor, so support cannot be checked against anything")
		}
	}

	// Every manifest item binds the payload actually emitted.
	byKey := map[string]BlindItem{}
	for _, group := range blind {
		for _, it := range group {
			byKey[it.ItemKey] = it
		}
	}
	for _, it := range m.Items {
		got, ok := byKey[it.ItemKey]
		if !ok {
			t.Fatalf("manifest item %s has no blinded payload", it.ItemKey)
		}
		if d := digestOf(got); d != it.BlindPayloadDigestSHA256 {
			t.Errorf("item %s binds payload digest %s but the emitted payload hashes to %s", it.ItemKey, it.BlindPayloadDigestSHA256, d)
		}
	}
}

// TestOneVerdictCannotMigrateBetweenWorlds.
//
// Two pinned worlds can produce a byte-identical claim that the evidence
// supports in one and refutes in the other. If the key were content alone, one
// adjudicated verdict would silently answer a question it was never asked —
// the worst failure available to an answer key, because the result still looks
// correct.
func TestOneVerdictCannotMigrateBetweenWorlds(t *testing.T) {
	obs := []architecture.Fact{fact("p", "s", "holds", "o", "a.go", 1)}
	a := World{Name: "w1", Binding: architecture.ClaimDocumentBinding{RepositoryDomain: "d1", Revision: "r1", RevisionStatus: architecture.RevisionResolved}, Observations: obs}
	b := World{Name: "w2", Binding: architecture.ClaimDocumentBinding{RepositoryDomain: "d2", Revision: "r2", RevisionStatus: architecture.RevisionResolved}, Observations: obs}

	m, _, err := Build([]World{a, b}, testOptions())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	keys := map[string]string{}
	for _, it := range m.Items {
		if it.Lane != LanePrecision {
			continue
		}
		if prior, ok := keys[it.ItemKey]; ok && prior != it.World {
			t.Fatalf("the identical claim in worlds %s and %s shares item key %s; one verdict would answer both", prior, it.World, it.ItemKey)
		}
		keys[it.ItemKey] = it.World
	}
	if len(keys) != 2 {
		t.Fatalf("expected one precision item per world, got %d", len(keys))
	}
}

// TestAnUnseededOrUnstampedSampleIsRefused. Both are identity, not ceremony: a
// sample nobody can recompute cannot be audited, and a self-stamped artifact
// is not reproducible.
func TestAnUnseededOrUnstampedSampleIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mut   func(*Options)
		wants string
	}{
		{"no seed", func(o *Options) { o.Seed = "" }, "seed"},
		{"no timestamp", func(o *Options) { o.GeneratedAt = "" }, "generated-at"},
		{"no protocol digest", func(o *Options) { o.ProtocolDigestSHA256 = "" }, "protocol digest"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := testOptions()
			tc.mut(&opts)
			if _, _, err := Build([]World{skewedWorld()}, opts); err == nil {
				t.Fatalf("a sample with %s was accepted", tc.name)
			} else if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("refusal does not say what is missing: %v", err)
			}
		})
	}
}

// TestChallengeLaneCarriesCounterexamples keeps the lane wired to real input
// rather than only proving its absence.
func TestChallengeLaneCarriesCounterexamples(t *testing.T) {
	w := World{
		Name:    "w",
		Binding: architecture.ClaimDocumentBinding{RepositoryDomain: "d", Revision: "r", RevisionStatus: architecture.RevisionResolved},
		Counterexamples: []investigation.Counterexample{
			{ID: "ce-1", ClaimID: "claim-1", Description: "the boundary is crossed in the test helper", EvidenceRefIDs: []string{"x.go:1-2"}},
		},
		CandidateQuestions: []architecture.OpenQuestion{
			{ID: "q-1", QuestionText: "who owns the retry budget?"},
		},
	}
	m, blind, err := Build([]World{w}, testOptions())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	n := 0
	for _, it := range m.Items {
		if it.Lane == LaneChallenge {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("challenge lane drew %d items from one counterexample and one question, want 2", n)
	}
	if len(blind[blindKey("w", LaneChallenge)]) != 2 {
		t.Error("the challenge lane materialized no adjudicator view")
	}
}

// TestDuplicateWorldNamesAreRefused.
//
// Two worlds under one name keep both sets of items in the manifest while the
// second world's blinded views overwrite the first's under the same key. The
// result is a manifest whose digest describes items that have no adjudication
// payload left — a sample that looks complete and cannot be adjudicated.
func TestDuplicateWorldNamesAreRefused(t *testing.T) {
	obs := []architecture.Fact{fact("p", "s", "holds", "o", "a.go", 1)}
	worlds := []World{
		{Name: "w", Binding: architecture.ClaimDocumentBinding{RepositoryDomain: "d1", Revision: "r1", RevisionStatus: architecture.RevisionResolved}, Observations: obs},
		{Name: "w", Binding: architecture.ClaimDocumentBinding{RepositoryDomain: "d2", Revision: "r2", RevisionStatus: architecture.RevisionResolved}, Observations: obs},
	}
	if _, _, err := Build(worlds, testOptions()); err == nil {
		t.Fatal("two worlds under one name were accepted; the second silently replaces the first's blinded views")
	} else if !strings.Contains(err.Error(), "twice") {
		t.Errorf("the refusal does not say what went wrong: %v", err)
	}
}

// TestEveryManifestItemKeepsAnAdjudicationPayload is the property the check
// above protects, asserted directly rather than only through its refusal.
func TestEveryManifestItemKeepsAnAdjudicationPayload(t *testing.T) {
	m, blind, err := Build([]World{skewedWorld()}, testOptions())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	payloads := map[string]bool{}
	for _, group := range blind {
		for _, it := range group {
			payloads[it.ItemKey] = true
		}
	}
	for _, it := range m.Items {
		if !payloads[it.ItemKey] {
			t.Fatalf("manifest item %s (%s/%s) has no blinded payload to adjudicate", it.ItemKey, it.World, it.Lane)
		}
	}
}

// TestAWorldNameCannotEscapeTheOutputDirectory.
//
// The blinded views are written one file per (world, lane), so a world name is
// a path component. A name carrying a separator or a parent reference would
// put the adjudicator's payload somewhere the run never said it wrote.
func TestAWorldNameCannotEscapeTheOutputDirectory(t *testing.T) {
	for _, name := range []string{"../escape", "a/b", `a\b`, "..", "ok/../bad"} {
		t.Run(name, func(t *testing.T) {
			worlds := []World{{
				Name:         name,
				Binding:      architecture.ClaimDocumentBinding{RepositoryDomain: "d", Revision: "r", RevisionStatus: architecture.RevisionResolved},
				Observations: []architecture.Fact{fact("p", "s", "holds", "o", "a.go", 1)},
			}}
			if _, _, err := Build(worlds, testOptions()); err == nil {
				t.Fatalf("world name %q was accepted as a path component", name)
			}
		})
	}
}
