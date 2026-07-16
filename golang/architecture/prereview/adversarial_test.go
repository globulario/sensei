// SPDX-License-Identifier: Apache-2.0

package prereview

import "testing"

func TestModelCandidateCannotBecomeGovernedFinding(t *testing.T) {
	t.Run("protection item", func(t *testing.T) {
		d := baseDraft()
		d.Protection.Invariants = []ProtectionItem{
			{ID: "inv.candidate", Title: "Predicted invariant", Severity: SeverityMedium, Status: "at_risk", Epistemic: EpistemicModelCandidate, EvidenceRefs: []string{"model:x"}},
		}
		if _, err := Finalize(d); err == nil {
			t.Fatal("finalized a model candidate as a governed protection item")
		}
	})
	t.Run("impact item", func(t *testing.T) {
		d := baseDraft()
		d.Impact.ChangedBoundaries = []ImpactItem{{ID: "b.x", Epistemic: EpistemicModelCandidate, EvidenceRefs: []string{"model:x"}}}
		if _, err := Finalize(d); err == nil {
			t.Fatal("finalized a model candidate as a governed impact item")
		}
	})
	t.Run("blocking attention", func(t *testing.T) {
		d := baseDraft()
		d.ReviewerAttention = []ReviewerAttentionItem{
			{ID: "a", Category: AttentionModelCandidate, Question: "shift?", Blocking: true, Severity: SeverityHigh, Epistemic: EpistemicModelCandidate},
		}
		if _, err := Finalize(d); err == nil {
			t.Fatal("finalized a blocking model-candidate attention item")
		}
	})
}

func TestCallerBooleanCannotForgeVerdict(t *testing.T) {
	// A caller pre-sets a favourable disposition and summary. Finalize must
	// derive both from evidence and ignore the forged values.
	d := governedDraft()
	d.Disposition = DispositionCertified
	d.Summary.CurrentDisposition = "certified"
	d.Summary.ReviewerAttentionCount = 99

	r := mustFinalize(t, d)
	if r.Disposition == DispositionCertified {
		t.Fatal("caller-forged certified disposition survived finalize")
	}
	if r.Disposition != DispositionReadyForHumanReview {
		t.Fatalf("disposition = %q, want ready_for_human_review", r.Disposition)
	}
	if r.Summary.CurrentDisposition != string(r.Disposition) {
		t.Fatalf("summary disposition %q not re-derived", r.Summary.CurrentDisposition)
	}
	if r.Summary.ReviewerAttentionCount != 0 {
		t.Fatalf("summary attention count %d not re-derived", r.Summary.ReviewerAttentionCount)
	}
}

func TestFixturesPositiveValidate(t *testing.T) {
	for name := range positiveBuilders(t) {
		t.Run(name, func(t *testing.T) {
			if err := Validate(loadFixture(t, name)); err != nil {
				t.Fatalf("positive fixture %s failed validation: %v", name, err)
			}
		})
	}
}

func TestFixturesInvalidRejected(t *testing.T) {
	for name := range invalidBuilders(t) {
		t.Run(name, func(t *testing.T) {
			if err := Validate(loadFixture(t, "invalid/"+name)); err == nil {
				t.Fatalf("invalid fixture %s passed validation", name)
			}
		})
	}
}
