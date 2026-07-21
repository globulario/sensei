// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// LLMIntentDrafter is the real (opt-in) intent proposer. It asks an LLMClient to
// turn a batch of gathered rule-bearing excerpts into architectural-intent
// candidates, each CITING the excerpts it used. Citation membership is enforced
// downstream by validateIntentDraft, and code anchors are verified by
// GroundIntent — this drafter relaxes neither. It can never mint an accepted
// intent: status is forced to candidate.
type LLMIntentDrafter struct {
	Client    LLMClient
	MaxTokens int
	Max       int // soft cap on how many candidates to ask for
}

const intentPromptContract = `You extract ARCHITECTURAL INTENT from a project's own stated charter.

You are given numbered excerpts of rule-bearing text (docs, code comments, schema
comments, test names, commit messages, PR reviews), each with a citation id.

Propose architectural-intent candidates: durable rules the project means to
preserve (ownership, lifecycle, compatibility/rollback, security/auth boundary,
concurrency/identity, failure-response, API-contract, UI-truth, operational).

HARD RULES:
- Every candidate MUST cite at least one excerpt by its exact citation id in
  "source_citations". NEVER invent a citation that is not in the provided list.
- "code_anchors" are your BEST GUESS of the file(s) the rule lives in, as
  "file:<path>" — these will be verified against the tree; do not fabricate.
- State a short human-readable "title" and the rule as a precise "claim". Pick
  one "category".
- Do NOT provide durable ids. Sensei mints candidate identity deterministically
  after validation.
- Do NOT propose new meta-principles. You MAY list existing ones you are
  confident about in "related_meta_principles".
- You propose only; Sensei validates, grounds, and routes under adoption policy.
  Return ONLY the JSON.`

const intentCandidateSchema = `{
  "type":"object",
  "additionalProperties":false,
  "properties":{
    "candidates":{"type":"array","items":{
      "type":"object",
      "additionalProperties":false,
      "properties":{
        "title":{"type":"string"},
        "claim":{"type":"string"},
        "category":{"type":"string"},
        "source_citations":{"type":"array","items":{"type":"string"}},
        "code_anchors":{"type":"array","items":{"type":"string"}},
        "related_invariants":{"type":"array","items":{"type":"string"}},
        "related_meta_principles":{"type":"array","items":{"type":"string"}}
      },
      "required":["title","claim","category","source_citations"]
    }}
  },
  "required":["candidates"]
}`

type llmIntentResponse struct {
	Candidates []llmIntentCandidate `json:"candidates"`
}

type llmIntentCandidate struct {
	IntentID              string   `json:"intent_id"`
	Title                 string   `json:"title"`
	Claim                 string   `json:"claim"`
	Category              string   `json:"category"`
	SourceCitations       []string `json:"source_citations"`
	CodeAnchors           []string `json:"code_anchors"`
	RelatedInvariants     []string `json:"related_invariants"`
	RelatedMetaPrinciples []string `json:"related_meta_principles"`
}

// DraftIntents implements IntentDrafter.
func (d LLMIntentDrafter) DraftIntents(ctx context.Context, excerpts []IntentExcerpt) ([]intentDraft, error) {
	if d.Client == nil {
		return nil, fmt.Errorf("no LLM client configured")
	}
	if len(excerpts) == 0 {
		return nil, nil
	}
	text, err := d.Client.Complete(ctx, LLMRequest{
		System:    intentPromptContract,
		User:      buildIntentPrompt(excerpts, d.Max),
		Schema:    intentCandidateSchema,
		MaxTokens: d.MaxTokens,
	})
	if err != nil {
		return nil, err
	}
	raw, err := parseLLMIntentResponse(stripCodeFence(text))
	if err != nil {
		return nil, fmt.Errorf("non-JSON intent output: %w", err)
	}
	out := make([]intentDraft, 0, len(raw.Candidates))
	for _, c := range raw.Candidates {
		out = append(out, intentDraft{
			IntentID:              strings.TrimSpace(c.IntentID),
			Title:                 strings.TrimSpace(c.Title),
			Claim:                 strings.TrimSpace(c.Claim),
			Category:              strings.TrimSpace(c.Category),
			SourceCitations:       c.SourceCitations,
			CodeAnchors:           c.CodeAnchors,
			RelatedInvariants:     c.RelatedInvariants,
			RelatedMetaPrinciples: c.RelatedMetaPrinciples,
		})
	}
	return out, nil
}

func parseLLMIntentResponse(text string) (llmIntentResponse, error) {
	var raw llmIntentResponse
	if err := json.Unmarshal([]byte(text), &raw); err == nil {
		return raw, nil
	}
	var candidates []llmIntentCandidate
	if err := json.Unmarshal([]byte(text), &candidates); err != nil {
		return llmIntentResponse{}, err
	}
	return llmIntentResponse{Candidates: candidates}, nil
}

// buildIntentPrompt renders the excerpts as a numbered, cited list for the model.
func buildIntentPrompt(excerpts []IntentExcerpt, max int) string {
	if max <= 0 {
		max = 12
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Propose up to %d architectural-intent candidates from these excerpts.\n", max)
	fmt.Fprintf(&b, "Cite excerpts by their [citation] id in source_citations.\n\n")
	for _, e := range excerpts {
		fmt.Fprintf(&b, "[%s] (%s) %s\n", e.Citation, e.Kind, e.Text)
	}
	return b.String()
}
