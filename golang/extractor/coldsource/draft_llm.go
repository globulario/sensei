// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/extractor"
)

// LLMDrafter is the real (opt-in) Drafter. It asks an LLMClient to turn a
// triangulated bundle into a candidate, enforces strict structured output, and
// maps it to a status:candidate PromotionProposal. Citation membership and
// resolution are enforced downstream by ValidateDraft + CheckCitations exactly
// as for EchoDrafter — this drafter does not relax any of those checks.
type LLMDrafter struct {
	Client    LLMClient
	MaxTokens int // 0 → client default
}

// llmCandidate is the strict JSON shape the model returns (see candidateSchema).
type llmCandidate struct {
	CandidateClass    string   `json:"candidate_class"`
	Theme             string   `json:"theme"`
	Reason            string   `json:"reason"`
	Confidence        string   `json:"confidence"`
	ActivationTrigger string   `json:"activation_trigger"`
	RequiredTests     []string `json:"required_tests"`
	SourcePaths       []string `json:"source_paths"`
}

// Draft implements Drafter.
func (d LLMDrafter) Draft(ctx context.Context, b Bundle) (*extractor.PromotionProposal, error) {
	if !b.IsTriangulated() {
		return nil, DraftError{Kind: "untriangulated", Reason: "bundle not triangulated"}
	}
	if d.Client == nil {
		return nil, DraftError{Kind: "llm_error", Reason: "no LLM client configured"}
	}

	text, err := d.Client.Complete(ctx, LLMRequest{
		System:    promptContract,
		User:      buildBundlePrompt(b),
		Schema:    candidateSchema,
		MaxTokens: d.MaxTokens,
	})
	if err != nil {
		return nil, DraftError{Kind: "llm_error", Reason: err.Error()}
	}

	var raw llmCandidate
	if err := json.Unmarshal([]byte(stripCodeFence(text)), &raw); err != nil {
		return nil, DraftError{Kind: "malformed", Reason: "non-JSON model output: " + err.Error()}
	}

	class, ok := normalizeCandidateClass(raw.CandidateClass)
	if !ok {
		return nil, DraftError{Kind: "bad_class", Reason: "unknown candidate class: " + raw.CandidateClass}
	}

	conf := normalizeConfidence(raw.Confidence)
	trigger := strings.TrimSpace(raw.ActivationTrigger)
	if trigger == "" {
		trigger = "edit under " + strings.ReplaceAll(b.ThemeKey, ".", "/")
	}

	// Status is FORCED to candidate regardless of anything the model said — the
	// schema doesn't even expose status, but this is the belt-and-suspenders
	// guarantee that an LLM can never mint an active node.
	return &extractor.PromotionProposal{
		CandidateID:       "candidate." + b.ThemeKey,
		CandidateClass:    class,
		Status:            "candidate",
		Theme:             b.ThemeKey,
		SourcePaths:       raw.SourcePaths, // validated against the bundle downstream
		Reason:            strings.TrimSpace(raw.Reason),
		Confidence:        conf,
		ActivationTrigger: trigger,
		RequiredTests:     raw.RequiredTests,
		NonAuthorityScope: true, // experiment: never claims an authority domain
	}, nil
}

// normalizeCandidateClass maps the model's enum string to a known extractor
// candidate class. Unknown classes are rejected (no silent coercion).
func normalizeCandidateClass(s string) (string, bool) {
	switch strings.TrimSpace(s) {
	case extractor.CandidateInvariant:
		return extractor.CandidateInvariant, true
	case extractor.CandidateForbiddenFix:
		return extractor.CandidateForbiddenFix, true
	case extractor.CandidateFailureMode:
		return extractor.CandidateFailureMode, true
	default:
		return "", false
	}
}

// normalizeConfidence clamps to the allowed set, defaulting to medium.
func normalizeConfidence(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "low":
		return "low"
	case "high":
		return "high"
	default:
		return "medium"
	}
}

// stripCodeFence tolerates a model that wraps JSON in ```...``` despite the
// instruction not to. It does not tolerate any other deviation.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	if i := strings.IndexByte(s, '\n'); i >= 0 { // drop a leading ```json line
		s = s[i+1:]
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
}

// buildBundlePrompt renders the evidence and the exact allowed-citation set the
// model must choose source_paths from.
func buildBundlePrompt(b Bundle) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "THEME: %s\n\n", b.ThemeKey)
	sb.WriteString("EVIDENCE (each line: [source] citations — quote):\n")
	for _, s := range b.Signals {
		cites := strings.Join(s.Citations(), " , ")
		fmt.Fprintf(&sb, "- [%s] %s — %q\n", s.SourceType, cites, strings.TrimSpace(s.MatchedText))
	}
	sb.WriteString("\nALLOWED CITATIONS (copy verbatim into source_paths; cite at least one):\n")
	for _, c := range sortedKeys(b.AllowedCitations()) {
		fmt.Fprintf(&sb, "  %s\n", c)
	}
	sb.WriteString("\nReturn the candidate object now.")
	return sb.String()
}
