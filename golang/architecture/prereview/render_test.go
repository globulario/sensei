// SPDX-License-Identifier: Apache-2.0

package prereview

import (
	"strings"
	"testing"
)

func TestMarkdownContainsReviewerAttentionBeforeDetail(t *testing.T) {
	r := mustFinalize(t, positiveBuilders(t)["advisory-blocked"])
	md, err := RenderMarkdown(r, RenderOptions{})
	if err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	s := string(md)
	attn := strings.Index(s, "## Reviewer attention")
	impact := strings.Index(s, "## Architectural impact")
	governance := strings.Index(s, "## Governance and scope")
	if attn < 0 || impact < 0 || governance < 0 {
		t.Fatalf("missing expected sections: attn=%d impact=%d gov=%d", attn, impact, governance)
	}
	if attn > impact || attn > governance {
		t.Fatalf("reviewer attention (%d) must precede detail (impact=%d, gov=%d)", attn, impact, governance)
	}
	// The blocking question must be surfaced.
	if !strings.Contains(s, "permanent ownership path") {
		t.Fatal("reviewer question not rendered")
	}
}

func TestMarkdownCapsReviewerItemsButJSONKeepsAll(t *testing.T) {
	d := baseDraft()
	for i := 0; i < DefaultMaxReviewerItems+3; i++ {
		d.ReviewerAttention = append(d.ReviewerAttention, ReviewerAttentionItem{
			ID: "attn." + string(rune('a'+i)), Category: AttentionArchitectQuestion,
			Question: "Question number " + string(rune('a'+i)) + "?",
			Blocking: false, Severity: SeverityMedium, Epistemic: EpistemicUnknown,
		})
	}
	r := mustFinalize(t, d)

	md, err := RenderMarkdown(r, RenderOptions{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(string(md), "more reviewer question(s) in machine-readable output") {
		t.Fatal("markdown did not note truncated reviewer items")
	}
	// The full set survives in the model (hence JSON).
	if len(r.ReviewerAttention) != DefaultMaxReviewerItems+3 {
		t.Fatalf("model dropped reviewer items: %d", len(r.ReviewerAttention))
	}
}

func TestMarkdownAdvisoryHidesGovernance(t *testing.T) {
	r := mustFinalize(t, baseDraft())
	md, err := RenderMarkdown(r, RenderOptions{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(string(md), "Typed governance is unavailable at advisory coverage") {
		t.Fatal("advisory report did not mark governance unavailable")
	}
}
