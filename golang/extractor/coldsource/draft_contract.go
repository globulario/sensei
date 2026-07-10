// SPDX-License-Identifier: Apache-2.0

package coldsource

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/awareness-graph/golang/extractor"
)

// Drafter turns a triangulated evidence Bundle into a candidate proposal.
//
// CONTRACT (enforced deterministically by ValidateDraft + citation_check,
// independent of the implementation):
//   - returns *extractor.PromotionProposal with Status == "candidate";
//   - every entry in proposal.SourcePaths MUST be a citation present in the
//     bundle (no fabricated evidence); at least one citation is required;
//   - the proposal must NOT be marked active/accepted.
//
// A REAL implementation calls an LLM with the bundle's quotes and is REQUIRED
// to cite its evidence. It lives behind this interface so the experiment can
// swap models without touching the pipeline. EchoDrafter below is a
// deterministic, non-LLM stand-in that exercises the pipeline end-to-end and
// makes NO quality judgement — it exists to prove plumbing, not to score the
// hypothesis.
type Drafter interface {
	Draft(ctx context.Context, b Bundle) (*extractor.PromotionProposal, error)
}

// DraftError signals a bundle yielded no admissible candidate (e.g. the drafter
// could not cite evidence, or the LLM returned malformed output). It is counted,
// not fatal. Kind buckets the rejection for the scoring report.
type DraftError struct {
	// Kind ∈ "untriangulated" | "no_evidence" | "malformed" | "bad_class" | "llm_error"
	Kind   string
	Reason string
}

func (e DraftError) Error() string { return e.Reason }

// ValidateDraft enforces the citation contract deterministically. Returns the
// list of contract violations (empty == admissible).
func ValidateDraft(p *extractor.PromotionProposal, b Bundle) []string {
	var v []string
	if p == nil {
		return []string{"nil proposal"}
	}
	if !strings.EqualFold(p.Status, "candidate") {
		v = append(v, fmt.Sprintf("status must be candidate, got %q", p.Status))
	}
	if strings.TrimSpace(p.CandidateClass) == "" {
		v = append(v, "missing candidate class")
	}
	if len(p.SourcePaths) == 0 {
		v = append(v, "no citations: every candidate must cite bundle evidence")
	}
	allowed := b.AllowedCitations()
	for _, sp := range p.SourcePaths {
		if !allowed[sp] {
			v = append(v, "fabricated citation not in bundle: "+sp)
		}
	}
	return v
}

// shallowPathDenylist marks themes that are noise for an awareness graph.
var shallowPathDenylist = []string{
	"vendor", "node_modules", "testdata", "generated", ".git",
	"third_party", "dist", "build",
}

// IsShallow is a deterministic guard that trims obvious noise BEFORE a human
// reviews. It is intentionally conservative — the real shallow-vs-load-bearing
// judgement is the reviewer's (and a future LLM drafter's) job. It rejects:
//   - themes under denylisted paths (vendored / generated / test fixtures);
//   - candidates whose entire evidence is shorter than a meaningful phrase.
func IsShallow(p *extractor.PromotionProposal, b Bundle) (bool, string) {
	theme := strings.ToLower(p.Theme)
	for _, d := range shallowPathDenylist {
		if theme == d || strings.Contains(theme, "."+d+".") ||
			strings.HasPrefix(theme, d+".") || strings.HasSuffix(theme, "."+d) {
			return true, "theme under denylisted path: " + d
		}
	}
	longest := 0
	for _, s := range b.Signals {
		if l := len(strings.TrimSpace(s.MatchedText)); l > longest {
			longest = l
		}
	}
	if longest < 12 {
		return true, "evidence too thin to be load-bearing"
	}
	return false, ""
}

// EchoDrafter is the deterministic, non-LLM drafter. It composes a candidate
// from a bundle: class = the dominant ProposedClass hint, citations = all bundle
// citations, confidence = by source-type count. It NEVER invents content beyond
// the bundle, so it always satisfies the citation contract. Swap in an LLM
// drafter to actually test the hypothesis.
type EchoDrafter struct{}

// Draft implements Drafter.
func (EchoDrafter) Draft(_ context.Context, b Bundle) (*extractor.PromotionProposal, error) {
	if !b.IsTriangulated() {
		return nil, DraftError{Kind: "untriangulated", Reason: "bundle not triangulated"}
	}
	cites := sortedKeys(b.AllowedCitations())
	if len(cites) == 0 {
		return nil, DraftError{Kind: "no_evidence", Reason: "no citable evidence"}
	}
	conf := "medium"
	if len(b.SourceTypes) >= 3 {
		conf = "high"
	}
	var quotes []string
	for _, s := range b.Signals {
		if t := strings.TrimSpace(s.MatchedText); t != "" {
			quotes = append(quotes, t)
		}
	}
	reason := "Triangulated from " + strings.Join(sourceTypeStrings(b.SourceTypes), " + ") +
		". Evidence: " + truncate(strings.Join(dedupe(quotes), " | "), 280)

	return &extractor.PromotionProposal{
		CandidateID:       "candidate." + b.ThemeKey,
		CandidateClass:    dominantClass(b),
		Status:            "candidate",
		Theme:             b.ThemeKey,
		SourcePaths:       cites,
		Reason:            reason,
		Confidence:        conf,
		ActivationTrigger: "edit under " + strings.ReplaceAll(b.ThemeKey, ".", "/"),
		NonAuthorityScope: true, // experiment: never claims an authority domain
	}, nil
}

// dominantClass returns the most frequent ProposedClass hint in the bundle,
// breaking ties deterministically by class name.
func dominantClass(b Bundle) string {
	counts := map[string]int{}
	for _, s := range b.Signals {
		if s.ProposedClass != "" {
			counts[s.ProposedClass]++
		}
	}
	best, bestN := extractor.CandidateInvariant, -1
	for c, n := range counts {
		if n > bestN || (n == bestN && c < best) {
			best, bestN = c, n
		}
	}
	return best
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sourceTypeStrings(ts []SourceType) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = string(t)
	}
	return out
}

func dedupe(ss []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
