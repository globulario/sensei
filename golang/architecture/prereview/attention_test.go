// SPDX-License-Identifier: Apache-2.0

package prereview

import "testing"

func TestReviewerAttentionRanksBlockingUnknownFirst(t *testing.T) {
	items := []ReviewerAttentionItem{
		{ID: "low", Category: AttentionResultGraphChange, Question: "Graph changed here?", Blocking: false, Severity: SeverityLow, Epistemic: EpistemicDeterministicallyInferred},
		{ID: "candidate", Category: AttentionModelCandidate, Question: "Possible ownership shift?", Blocking: false, Severity: SeverityMedium, Epistemic: EpistemicModelCandidate},
		{ID: "blocking-unknown", Category: AttentionUnknownDirection, Question: "Permanent writer or migration exception?", Blocking: true, Severity: SeverityHigh, Epistemic: EpistemicUnknown, ArchitecturalReach: 3, TaskRelevance: 3},
		{ID: "med", Category: AttentionMissingProof, Question: "Runtime evidence mandatory?", Blocking: false, Severity: SeverityMedium, Epistemic: EpistemicUnknown},
	}
	ranked := RankReviewerAttention(items)
	if ranked[0].ID != "blocking-unknown" {
		t.Fatalf("first ranked = %q, want blocking-unknown", ranked[0].ID)
	}
	// The non-authoritative candidate must not outrank real human decisions.
	if ranked[len(ranked)-1].ID != "candidate" && ranked[len(ranked)-1].ID != "low" {
		t.Fatalf("last ranked = %q, want a mechanical/candidate item", ranked[len(ranked)-1].ID)
	}
}

func TestReviewerAttentionDeduplicatesEquivalentQuestions(t *testing.T) {
	items := []ReviewerAttentionItem{
		{ID: "a", Category: AttentionArchitectQuestion, Question: "Is this a permanent writer?", Blocking: true, Severity: SeverityHigh, Epistemic: EpistemicUnknown},
		{ID: "b", Category: AttentionArchitectQuestion, Question: "Is  this a  permanent writer?", Blocking: false, Severity: SeverityLow, Epistemic: EpistemicUnknown},
	}
	ranked := RankReviewerAttention(items)
	if len(ranked) != 1 {
		t.Fatalf("expected equivalent questions to collapse, got %d items", len(ranked))
	}
	// The higher-ranked occurrence survives.
	if ranked[0].ID != "a" {
		t.Fatalf("surviving item = %q, want the higher-ranked a", ranked[0].ID)
	}
}

func TestFinalizeRanksAndDedupsAttention(t *testing.T) {
	d := baseDraft()
	d.ReviewerAttention = []ReviewerAttentionItem{
		{ID: "b", Category: AttentionMissingProof, Question: "Runtime evidence?", Blocking: false, Severity: SeverityMedium, Epistemic: EpistemicUnknown},
		{ID: "a", Category: AttentionUnknownDirection, Question: "Permanent writer?", Blocking: true, Severity: SeverityHigh, Epistemic: EpistemicUnknown, ArchitecturalReach: 3},
	}
	r := mustFinalize(t, d)
	if r.ReviewerAttention[0].ID != "a" {
		t.Fatalf("finalize did not rank blocking item first: %q", r.ReviewerAttention[0].ID)
	}
	if r.Summary.ReviewerAttentionCount != 2 {
		t.Fatalf("attention count = %d, want 2", r.Summary.ReviewerAttentionCount)
	}
	if r.Summary.HighestPriorityBlocker != "Permanent writer?" {
		t.Fatalf("highest priority blocker = %q", r.Summary.HighestPriorityBlocker)
	}
}
