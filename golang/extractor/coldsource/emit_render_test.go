// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"bytes"
	"strings"
	"testing"
)

// Regression for the AWG-on-itself audit finding: RenderReport must surface
// review-only leads even when ZERO candidates were accepted. The previous
// empty-candidates early return hid them — a run that produced only leads
// silently showed nothing.
func TestRenderReport_SurfacesReviewOnlyLeadsWhenZeroAccepted(t *testing.T) {
	var buf bytes.Buffer
	RenderReport(&buf, Report{
		DryRun:               true,
		SegregatedReviewOnly: 1,
		ReviewOnlyLeads: []CandidateSummary{
			{CandidateID: "lead.1", Class: "forbidden_fix", Theme: "pkg.only.a.lead", Citations: []string{"pr:1:2"}, Tier: "review_suggestion"},
		},
		// no accepted Candidates
	})
	out := buf.String()
	if strings.Contains(out, "nothing to review") {
		t.Errorf("must NOT short-circuit to 'nothing to review' when leads exist:\n%s", out)
	}
	if !strings.Contains(out, "Review-only leads") || !strings.Contains(out, "pkg.only.a.lead") {
		t.Errorf("review-only lead was hidden with 0 accepted candidates:\n%s", out)
	}
}

// Truly empty (no candidates, no leads) still short-circuits cleanly.
func TestRenderReport_EmptyStillShortCircuits(t *testing.T) {
	var buf bytes.Buffer
	RenderReport(&buf, Report{DryRun: true})
	if !strings.Contains(buf.String(), "nothing to review") {
		t.Errorf("empty report should say 'nothing to review':\n%s", buf.String())
	}
}
