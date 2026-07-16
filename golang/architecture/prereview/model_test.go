// SPDX-License-Identifier: Apache-2.0

package prereview

import (
	"encoding/json"
	"testing"
)

func TestPreReviewReportCanonicalRoundTrip(t *testing.T) {
	r := mustFinalize(t, governedDraft())

	raw, err := RenderJSON(r)
	if err != nil {
		t.Fatalf("render json: %v", err)
	}
	var back PreReviewReport
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := Validate(back); err != nil {
		t.Fatalf("round-tripped report invalid: %v", err)
	}
	if back.ReportDigestSHA256 != r.ReportDigestSHA256 {
		t.Fatalf("digest changed across round trip: %q -> %q", r.ReportDigestSHA256, back.ReportDigestSHA256)
	}
	// Re-finalizing an already-finalized report is a no-op on identity.
	again, err := Finalize(back)
	if err != nil {
		t.Fatalf("re-finalize: %v", err)
	}
	if again.ReportDigestSHA256 != r.ReportDigestSHA256 {
		t.Fatalf("finalize not idempotent: %q -> %q", r.ReportDigestSHA256, again.ReportDigestSHA256)
	}
}

func TestUnknownVocabularyRejected(t *testing.T) {
	cases := map[string]func(*PreReviewReport){
		"coverage":    func(r *PreReviewReport) { r.Coverage.Level = "bogus" },
		"disposition": func(r *PreReviewReport) { r.Disposition = "bogus" },
		"severity": func(r *PreReviewReport) {
			r.Protection.Invariants = []ProtectionItem{{ID: "x", Severity: "bogus", Status: "holds", Epistemic: EpistemicGoverned, EvidenceRefs: []string{"e"}}}
		},
		"epistemic": func(r *PreReviewReport) { r.Impact.AffectedComponents[0].Epistemic = "bogus" },
		"attention": func(r *PreReviewReport) {
			r.ReviewerAttention = []ReviewerAttentionItem{{ID: "a", Category: "bogus", Question: "q", Severity: SeverityLow, Epistemic: EpistemicUnknown}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := mustFinalize(t, baseDraft())
			mutate(&r)
			if err := Validate(r); err == nil {
				t.Fatalf("unknown %s vocabulary accepted", name)
			}
		})
	}
}
