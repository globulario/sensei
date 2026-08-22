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
	ref := ReferenceSet{Labels: []ReferenceLabel{{ItemKey: "x", Verdict: VerdictSupported}}}
	if a, b := ScoreAcquisition(first, ref), ScoreAcquisition(first, ref); !equalJSON(t, a, b) {
		t.Error("scoring the same frozen bundle twice was not identical")
	}
}

// The scorer is the half that MUST replay byte-identically.
func TestScorerOverAFrozenBundleIsByteIdentical(t *testing.T) {
	a := NewAcquisition("t", baseline(), resolvedOutcome("A calls B"))
	ref := ReferenceSet{SchemaVersion: "v1", ProtocolID: "phase10-reference-protocol-v1"}
	ref.Labels = []ReferenceLabel{
		{ItemKey: ItemKey(a.Items[0]), Verdict: VerdictSupported},
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
	s := ScoreAcquisition(a, ReferenceSet{Labels: []ReferenceLabel{{ItemKey: "x", Verdict: VerdictSupported}}})
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
	ref := ReferenceSet{Labels: []ReferenceLabel{{ItemKey: ItemKey(a.Items[0]), Verdict: VerdictSupported}}}
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
	a := AcquiredItem{Kind: "candidate_claim", Text: "this boundary leaks", CitedEvidenceIDs: []string{"ev-1"}, FilePaths: []string{"a.go"}}
	b := a
	b.FilePaths = []string{"b.go"}
	if ItemKey(a) == ItemKey(b) {
		t.Error("claims about different files share one identity; a human label could migrate between them")
	}
}
