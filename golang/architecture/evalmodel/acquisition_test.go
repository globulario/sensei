// SPDX-License-Identifier: AGPL-3.0-only

package evalmodel

import (
	"encoding/json"
	"testing"

	"github.com/globulario/sensei/golang/architecture/investigation"
	"github.com/globulario/sensei/golang/architecture/modelexec"
)

const sha = "4a8e63db7cc5173b82bd3ba6019d30ce9e22db84d852bd3ba6019d30ce922db8"

func resolvedOutcome(text string) modelexec.Outcome {
	return modelexec.Outcome{
		Binding: investigation.ModelBinding{
			Status:                    investigation.ModelStatusResolved,
			ProviderID:                "bridge",
			ProviderVersion:           "v1",
			ModelName:                 "a-model",
			ModelDigestAbsence:        investigation.ModelDigestAbsent,
			RequestDigestSHA256:       sha,
			ArtifactDigestSHA256:      sha,
			NondeterminismDeclaration: "model_response_not_replayable",
		},
		ProviderCalls: 1,
		Artifact: &modelexec.Artifact{
			SchemaVersion:             modelexec.ArtifactSchemaVersion,
			NondeterminismDeclaration: "model_response_not_replayable",
			Items: []modelexec.ArtifactItem{{
				Kind: modelexec.ItemKindCandidateClaim, Text: text, CitedEvidenceIDs: []string{"ev-1"},
			}},
		},
	}
}

func baseline() DeterministicBaseline {
	return DeterministicBaseline{DocumentDigestSHA256: sha, ObservationCount: 42, CandidateCount: 7}
}

// The acquisition identity must cover everything that makes one measurement
// different from another.
func TestAcquisitionIdentityCoversBaselineBindingAndItems(t *testing.T) {
	base := NewAcquisition("2026-01-01T00:00:00Z", baseline(), resolvedOutcome("A calls B"))
	if base.AcquisitionDigestSHA256 == "" {
		t.Fatal("acquisition carries no identity")
	}

	same := NewAcquisition("2026-06-06T00:00:00Z", baseline(), resolvedOutcome("A calls B"))
	if same.AcquisitionDigestSHA256 != base.AcquisitionDigestSHA256 {
		t.Error("the same measurement at a different moment produced a different identity; the clock is not part of what was measured")
	}

	for _, tc := range []struct {
		name string
		got  Acquisition
	}{
		{"a different model answer", NewAcquisition("t", baseline(), resolvedOutcome("A does not call B"))},
		{"a different deterministic baseline", NewAcquisition("t", DeterministicBaseline{DocumentDigestSHA256: sha, ObservationCount: 43}, resolvedOutcome("A calls B"))},
		{"a different terminal status", NewAcquisition("t", baseline(), modelexec.Outcome{
			Binding: investigation.ModelBinding{Status: investigation.ModelStatusRefused, Reason: investigation.ModelReasonProviderRefused, ProviderID: "bridge", RequestDigestSHA256: sha},
		})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got.AcquisitionDigestSHA256 == base.AcquisitionDigestSHA256 {
				t.Error("a different measurement carries the same acquisition identity")
			}
		})
	}
}

// A live re-acquisition that answers differently is a NEW measurement, not a
// replay failure. This is the distinction the acquisition/scoring split exists
// to preserve.
func TestReAcquisitionWithADifferentAnswerIsANewMeasurement(t *testing.T) {
	first := NewAcquisition("t", baseline(), resolvedOutcome("first answer"))
	second := NewAcquisition("t", baseline(), resolvedOutcome("second answer"))
	if first.AcquisitionDigestSHA256 == second.AcquisitionDigestSHA256 {
		t.Fatal("two different model answers share one identity")
	}
	// And each scores deterministically against the same reference set.
	ref := frozenRef()
	ref.Labels = []ReferenceLabel{{ItemKey: "x", Verdict: VerdictSupported}}
	ref.DigestSHA256 = ReferenceDigest(ref)
	if a, b := ScoreAcquisition(first, ref), ScoreAcquisition(first, ref); !equalJSON(t, a, b) {
		t.Error("scoring the same frozen bundle twice was not identical")
	}
}

// The scorer is the half that MUST replay byte-identically.
func TestScorerOverAFrozenBundleIsByteIdentical(t *testing.T) {
	a := NewAcquisition("t", baseline(), resolvedOutcome("A calls B"))
	ref := frozenRef()
	ref.Labels = []ReferenceLabel{
		{ItemKey: ItemKey(a, a.Items[0]), Verdict: VerdictSupported},
		{ItemKey: "unrelated", Verdict: VerdictUnsupported},
	}
	ref.DigestSHA256 = ReferenceDigest(ref)

	first, err := json.Marshal(ScoreAcquisition(a, ref))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		again, err := json.Marshal(ScoreAcquisition(a, ref))
		if err != nil {
			t.Fatal(err)
		}
		if string(again) != string(first) {
			t.Fatalf("scoring replay %d differed:\n%s\n%s", i, first, again)
		}
	}
	var s Score
	if err := json.Unmarshal(first, &s); err != nil {
		t.Fatal(err)
	}
	if !s.Scored || s.ModelSupported != 1 {
		t.Errorf("score = %+v, want one supported item", s)
	}
	if s.ReferenceDigestSHA256 == "" {
		t.Error("a score does not name the ruler it used")
	}
}

// No answer key means NO score. Inferring correctness from the system's own
// output is the one thing the protocol forbids.
func TestNoReferenceSetProducesNoScore(t *testing.T) {
	a := NewAcquisition("t", baseline(), resolvedOutcome("A calls B"))
	s := ScoreAcquisition(a, ReferenceSet{})
	if s.Scored {
		t.Fatal("a score was produced with no human reference set")
	}
	if s.Reason != ReasonReferenceSetAbsent {
		t.Errorf("reason = %q, want %q", s.Reason, ReasonReferenceSetAbsent)
	}
	// The deterministic lane is still reported: it does not need the model.
	if s.DeterministicObservations != 42 {
		t.Errorf("deterministic observations = %d, want 42", s.DeterministicObservations)
	}
}

// A refusal or an error is an evaluation RESULT, not a zero score that would
// read like a bad model.
func TestNonResolvedOutcomeIsReportedAsItselfNotAsZero(t *testing.T) {
	a := NewAcquisition("t", baseline(), modelexec.Outcome{
		Binding: investigation.ModelBinding{
			Status: investigation.ModelStatusRefused, Reason: investigation.ModelReasonProviderRefused,
			ProviderID: "bridge", RequestDigestSHA256: sha,
		},
	})
	ref := frozenRef()
	ref.Labels = []ReferenceLabel{{ItemKey: "x", Verdict: VerdictSupported}}
	ref.DigestSHA256 = ReferenceDigest(ref)
	s := ScoreAcquisition(a, ref)
	if s.Scored {
		t.Error("a refused model produced a score")
	}
	if s.ModelStatus != investigation.ModelStatusRefused || s.Reason != ReasonModelDidNotResolve {
		t.Errorf("status=%q reason=%q, want the refusal reported as itself", s.ModelStatus, s.Reason)
	}
}

// Deterministic and model-derived counts must never be merged into one number.
func TestDeterministicAndModelCountsStaySeparate(t *testing.T) {
	a := NewAcquisition("t", baseline(), resolvedOutcome("A calls B"))
	s := ScoreAcquisition(a, ReferenceSet{})
	if s.DeterministicCandidates != 7 {
		t.Errorf("deterministic candidates = %d, want 7", s.DeterministicCandidates)
	}
	if s.ModelItemsByKind[modelexec.ItemKindCandidateClaim] != 1 {
		t.Errorf("model items = %v, want one candidate_claim", s.ModelItemsByKind)
	}
	if s.DeterministicCandidates == 0 || s.ModelItemsByKind == nil {
		t.Error("the two lanes are not separately visible")
	}
}

func equalJSON(t *testing.T, a, b Score) bool {
	t.Helper()
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// A digest stored inside a frozen file is a CLAIM that file makes about itself.
// Scoring an edited bundle while reporting the old identity is exactly how a
// moved answer key would go undetected.
func TestScorerRefusesFrozenInputsThatDoNotMatchTheirDigest(t *testing.T) {
	a := NewAcquisition("t", baseline(), resolvedOutcome("A calls B"))
	ref := frozenRef()
	ref.Labels = []ReferenceLabel{{ItemKey: ItemKey(a, a.Items[0]), Verdict: VerdictSupported}}
	ref.DigestSHA256 = ReferenceDigest(ref)
	if s := ScoreAcquisition(a, ref); !s.Scored {
		t.Fatalf("intact frozen inputs were not scored: %+v", s)
	}

	tampered := a
	tampered.Items = append([]AcquiredItem{}, a.Items...)
	tampered.Items[0].Text = "something the model never said"
	if s := ScoreAcquisition(tampered, ref); s.Scored || s.Reason != ReasonAcquisitionAltered {
		t.Errorf("an edited acquisition scored anyway: scored=%v reason=%q", s.Scored, s.Reason)
	}

	movedKey := ref
	movedKey.Labels = append([]ReferenceLabel{}, ref.Labels...)
	movedKey.Labels[0].Verdict = VerdictUnsupported
	if s := ScoreAcquisition(a, movedKey); s.Scored || s.Reason != ReasonReferenceSetAltered {
		t.Errorf("an answer key edited after the fact scored anyway: scored=%v reason=%q", s.Scored, s.Reason)
	}
}

// Two items with the same words but different file attribution are different
// claims. A label written for one must not migrate to the other.
func TestItemKeyDistinguishesFileAttribution(t *testing.T) {
	acq := NewAcquisition("t", baseline(), resolvedOutcome("x"))
	a := AcquiredItem{Kind: "candidate_claim", Text: "this boundary leaks", CitedEvidenceIDs: []string{"ev-1"}, FilePaths: []string{"a.go"}}
	b := a
	b.FilePaths = []string{"b.go"}
	if ItemKey(acq, a) == ItemKey(acq, b) {
		t.Error("claims about different files share one identity; a human label could migrate between them")
	}
}

// An answer key with no identity is not frozen, and an unfrozen ruler cannot
// support a defensible score. Validating the digest only when one happened to
// be present let labels-with-no-digest through to Scored=true.
func TestLabelsWithoutAFrozenIdentityCannotScore(t *testing.T) {
	a := NewAcquisition("t", baseline(), resolvedOutcome("A calls B"))
	unfrozen := frozenRef()
	unfrozen.DigestSHA256 = ""
	unfrozen.Labels = []ReferenceLabel{{ItemKey: ItemKey(a, a.Items[0]), Verdict: VerdictSupported}}

	s := ScoreAcquisition(a, unfrozen)
	if s.Scored {
		t.Fatal("an answer key with no frozen identity produced a score")
	}
	if s.Reason != ReasonReferenceSetUnfrozen {
		t.Errorf("reason = %q, want %q", s.Reason, ReasonReferenceSetUnfrozen)
	}

	// The same labels, frozen, do score.
	frozen := unfrozen
	frozen.DigestSHA256 = ReferenceDigest(frozen)
	if s := ScoreAcquisition(a, frozen); !s.Scored {
		t.Errorf("a frozen answer key did not score: %+v", s)
	}
}

// A baseline that changed WHAT it produced without changing HOW MUCH is a
// different baseline. Counts are not an identity.
func TestBaselineIdentityCoversTheComposedResultNotJustCounts(t *testing.T) {
	a := NewAcquisition("t", DeterministicBaseline{
		DocumentDigestSHA256: sha, ComposedResultDigestSHA256: "composed-a", ObservationCount: 42, CandidateCount: 7,
	}, resolvedOutcome("A calls B"))
	b := NewAcquisition("t", DeterministicBaseline{
		DocumentDigestSHA256: sha, ComposedResultDigestSHA256: "composed-b", ObservationCount: 42, CandidateCount: 7,
	}, resolvedOutcome("A calls B"))
	if a.AcquisitionDigestSHA256 == b.AcquisitionDigestSHA256 {
		t.Error("two different composed baselines with equal counts share one acquisition identity")
	}
}

// Reordering an unchanged answer must not mint a new measurement.
func TestAcquisitionItemOrderIsTotal(t *testing.T) {
	mk := func(cited, files []string) modelexec.ArtifactItem {
		return modelexec.ArtifactItem{Kind: modelexec.ItemKindCandidateClaim, Text: "same words", CitedEvidenceIDs: cited, FilePaths: files}
	}
	one := mk([]string{"ev-1"}, []string{"a.go"})
	two := mk([]string{"ev-2"}, []string{"b.go"})

	forward := resolvedOutcome("ignored")
	forward.Artifact.Items = []modelexec.ArtifactItem{one, two}
	reversed := resolvedOutcome("ignored")
	reversed.Artifact.Items = []modelexec.ArtifactItem{two, one}

	if NewAcquisition("t", baseline(), forward).AcquisitionDigestSHA256 !=
		NewAcquisition("t", baseline(), reversed).AcquisitionDigestSHA256 {
		t.Error("reordering identical items produced a different acquisition identity; a reordered answer is not a new measurement")
	}
}

// The recomputed identity must be the one the protocol defines, or two
// genuinely different rulers can share an accepted identity.
func TestReferenceIdentityCoversTheProtocolConstituents(t *testing.T) {
	base := ReferenceSet{
		SchemaVersion: "v1", ProtocolID: "phase10-reference-protocol-v1",
		ProtocolDigestSHA256:       "proto-1",
		SampleManifestDigestSHA256: "sample-1",
		LabelFileDigestsSHA256:     []string{"labels-1"},
		WorldBindingDigestsSHA256:  []string{"world-1"},
		Labels:                     []ReferenceLabel{{ItemKey: "k", Verdict: VerdictSupported}},
	}
	for _, tc := range []struct {
		name   string
		mutate func(*ReferenceSet)
	}{
		{"a different sample manifest", func(r *ReferenceSet) { r.SampleManifestDigestSHA256 = "sample-2" }},
		{"different label files", func(r *ReferenceSet) { r.LabelFileDigestsSHA256 = []string{"labels-2"} }},
		{"a different world binding", func(r *ReferenceSet) { r.WorldBindingDigestsSHA256 = []string{"world-2"} }},
		{"a different protocol digest", func(r *ReferenceSet) { r.ProtocolDigestSHA256 = "proto-2" }},
		{"an adjudicator overlap manifest", func(r *ReferenceSet) { r.AdjudicatorOverlapDigestSHA256 = "overlap-1" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			other := base
			tc.mutate(&other)
			if ReferenceDigest(other) == ReferenceDigest(base) {
				t.Errorf("two rulers differing in %s share one identity", tc.name)
			}
		})
	}
}

// Two adjudicators disagreeing about one item is the signal the overlap sample
// exists to produce. Silently keeping the last verdict would erase it.
func TestDuplicateReferenceLabelsInvalidateTheSet(t *testing.T) {
	a := NewAcquisition("t", baseline(), resolvedOutcome("A calls B"))
	key := ItemKey(a, a.Items[0])
	conflicted := frozenRef()
	conflicted.Labels = []ReferenceLabel{
		{ItemKey: key, Verdict: VerdictSupported},
		{ItemKey: key, Verdict: VerdictUnsupported},
	}
	conflicted.DigestSHA256 = ReferenceDigest(conflicted)

	s := ScoreAcquisition(a, conflicted)
	if s.Scored {
		t.Fatal("a reference set labelling one item twice produced a score")
	}
	if s.Reason != ReasonReferenceSetConflicted {
		t.Errorf("reason = %q, want %q", s.Reason, ReasonReferenceSetConflicted)
	}
}

// frozenRef is a reference set carrying the section 17 constituents a real
// release must have. Tests use it so a fixture cannot accidentally assert that
// an under-specified ruler is acceptable.
func frozenRef() ReferenceSet {
	r := ReferenceSet{
		SchemaVersion:              "v1",
		ProtocolID:                 "phase10-reference-protocol-v1",
		ProtocolDigestSHA256:       "protocol-digest",
		SampleManifestDigestSHA256: "sample-manifest-digest",
		LabelFileDigestsSHA256:     []string{"label-file-digest"},
		WorldBindingDigestsSHA256:  []string{"world-binding-digest"},
	}
	r.DigestSHA256 = ReferenceDigest(r)
	return r
}

// A populated release that omits a required section 17 constituent must not
// score, however self-consistent its own digest is.
func TestPopulatedReferenceSetMustCarryEveryRequiredConstituent(t *testing.T) {
	a := NewAcquisition("t", baseline(), resolvedOutcome("A calls B"))
	for _, tc := range []struct {
		name  string
		strip func(*ReferenceSet)
	}{
		{"protocol digest", func(r *ReferenceSet) { r.ProtocolDigestSHA256 = "" }},
		{"sample manifest digest", func(r *ReferenceSet) { r.SampleManifestDigestSHA256 = "" }},
		{"label file digests", func(r *ReferenceSet) { r.LabelFileDigestsSHA256 = nil }},
		{"world binding digests", func(r *ReferenceSet) { r.WorldBindingDigestsSHA256 = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ref := frozenRef()
			ref.Labels = []ReferenceLabel{{ItemKey: ItemKey(a, a.Items[0]), Verdict: VerdictSupported}}
			tc.strip(&ref)
			ref.DigestSHA256 = ReferenceDigest(ref)
			s := ScoreAcquisition(a, ref)
			if s.Scored {
				t.Fatalf("a release omitting the %s scored anyway", tc.name)
			}
			if s.Reason != ReasonReferenceSetIncomplete {
				t.Errorf("reason = %q, want %q", s.Reason, ReasonReferenceSetIncomplete)
			}
		})
	}
}

// cannot_adjudicate is a human DECISION, not a missing label.
func TestCannotAdjudicateIsPreservedNotCollapsedIntoUnlabelled(t *testing.T) {
	a := NewAcquisition("t", baseline(), resolvedOutcome("A calls B"))
	ref := frozenRef()
	ref.Labels = []ReferenceLabel{{ItemKey: ItemKey(a, a.Items[0]), Verdict: VerdictCannotAdjudicate}}
	ref.DigestSHA256 = ReferenceDigest(ref)

	s := ScoreAcquisition(a, ref)
	if !s.Scored {
		t.Fatalf("a cannot_adjudicate label prevented scoring: %+v", s)
	}
	if s.ModelCannotAdjudicate != 1 {
		t.Errorf("cannot_adjudicate count = %d, want 1", s.ModelCannotAdjudicate)
	}
	if s.ModelUnlabelled != 0 {
		t.Error("an explicit human decision was reported as a missing label")
	}
}

// The deterministic lane must be scoreable, or there is no delta to report.
func TestDeterministicLaneIsScoredAgainstTheSameReferenceSet(t *testing.T) {
	base := baseline()
	base.Candidates = []BaselineItem{{Kind: "candidate_claim", Text: "deterministic finding", CitedEvidenceIDs: []string{"ev-1"}}}
	a := NewAcquisition("t", base, resolvedOutcome("model finding"))

	ref := frozenRef()
	ref.Labels = []ReferenceLabel{
		{ItemKey: BaselineItemKey(a, a.Baseline.Candidates[0]), Verdict: VerdictSupported},
		{ItemKey: ItemKey(a, a.Items[0]), Verdict: VerdictUnsupported},
	}
	ref.DigestSHA256 = ReferenceDigest(ref)

	s := ScoreAcquisition(a, ref)
	if !s.Scored {
		t.Fatalf("not scored: %+v", s)
	}
	if s.BaselineSupported != 1 {
		t.Errorf("deterministic supported = %d, want 1; without it there is no model delta", s.BaselineSupported)
	}
	if s.ModelUnsupported != 1 {
		t.Errorf("model unsupported = %d, want 1", s.ModelUnsupported)
	}
}

// An identical claim about a DIFFERENT experiment must not inherit the verdict.
func TestItemKeysAreScopedToTheExperiment(t *testing.T) {
	one := NewAcquisition("t", baseline(), resolvedOutcome("same claim"))
	otherBase := baseline()
	otherBase.ComposedResultDigestSHA256 = "a-different-world"
	two := NewAcquisition("t", otherBase, resolvedOutcome("same claim"))

	if ItemKey(one, one.Items[0]) == ItemKey(two, two.Items[0]) {
		t.Error("identical claims from different experiments share a label key; one verdict could migrate to a question it was never asked about")
	}
}

// outside_scope is an exclusion the protocol requires reported as a count and
// a rate. An item that vanishes from the score also vanishes from its
// denominator, and a reader cannot tell an exclusion from an item never made.
func TestOutsideScopeIsReportedNotDropped(t *testing.T) {
	a := NewAcquisition("t", baseline(), resolvedOutcome("A calls B"))
	ref := frozenRef()
	ref.Labels = []ReferenceLabel{{ItemKey: ItemKey(a, a.Items[0]), Verdict: VerdictOutsideScope}}
	ref.DigestSHA256 = ReferenceDigest(ref)

	s := ScoreAcquisition(a, ref)
	if !s.Scored {
		t.Fatalf("not scored: %+v", s)
	}
	if s.ModelOutsideScope != 1 {
		t.Errorf("outside_scope count = %d, want 1", s.ModelOutsideScope)
	}
	if s.ModelUnlabelled != 0 || s.ModelSupported != 0 || s.ModelUnsupported != 0 {
		t.Error("an excluded item was counted somewhere it does not belong")
	}
}

// An empty entry satisfies a length test while binding nothing. Presence must
// be checked by content.
func TestConstituentDigestsMustCarryContentNotJustLength(t *testing.T) {
	a := NewAcquisition("t", baseline(), resolvedOutcome("A calls B"))
	for _, tc := range []struct {
		name  string
		blank func(*ReferenceSet)
	}{
		{"blank label file digest", func(r *ReferenceSet) { r.LabelFileDigestsSHA256 = []string{""} }},
		{"blank world binding digest", func(r *ReferenceSet) { r.WorldBindingDigestsSHA256 = []string{"  "} }},
		{"blank protocol digest", func(r *ReferenceSet) { r.ProtocolDigestSHA256 = "   " }},
		{"one good and one blank", func(r *ReferenceSet) { r.LabelFileDigestsSHA256 = []string{"real", ""} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ref := frozenRef()
			ref.Labels = []ReferenceLabel{{ItemKey: ItemKey(a, a.Items[0]), Verdict: VerdictSupported}}
			tc.blank(&ref)
			ref.DigestSHA256 = ReferenceDigest(ref)
			if s := ScoreAcquisition(a, ref); s.Scored {
				t.Fatalf("a release with a %s scored anyway", tc.name)
			}
		})
	}
}

// The vocabulary is closed, so a verdict outside it is a malformed answer key
// rather than a missing label.
func TestVerdictsOutsideTheClosedVocabularyInvalidateTheKey(t *testing.T) {
	a := NewAcquisition("t", baseline(), resolvedOutcome("A calls B"))
	ref := frozenRef()
	ref.Labels = []ReferenceLabel{{ItemKey: ItemKey(a, a.Items[0]), Verdict: "suported"}}
	ref.DigestSHA256 = ReferenceDigest(ref)

	s := ScoreAcquisition(a, ref)
	if s.Scored {
		t.Fatal("a reference set with an unrecognised verdict scored anyway")
	}
	if s.Reason != ReasonReferenceSetInvalidVerdict {
		t.Errorf("reason = %q, want %q", s.Reason, ReasonReferenceSetInvalidVerdict)
	}
}
