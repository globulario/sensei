// SPDX-License-Identifier: Apache-2.0

package prereview

import (
	"sort"
	"strings"
)

// Ranking weights, versioned by AttentionPolicyVersion. They are fixed integers
// so ranking is fully deterministic and never depends on a model's confidence.
const (
	weightBlocking      = 100
	weightSeverityUnit  = 10 // multiplied by the severity rank (0..4)
	weightRelevanceUnit = 5  // multiplied by clamp(TaskRelevance, 0, 3)
	weightReachUnit     = 5  // multiplied by clamp(ArchitecturalReach, 0, 3)
	weightHumanDecision = 20 // categories only a human can settle
	penaltyMechanical   = 15 // categories that are mechanically trackable
)

// humanDecisionCategory marks the irreducible-human-decision categories that
// earn the human-decision weight.
var humanDecisionCategory = map[AttentionCategory]bool{
	AttentionArchitectQuestion: true,
	AttentionUnknownDirection:  true,
	AttentionContradiction:     true,
	AttentionAuthorityConflict: true,
}

// mechanicalCategory marks categories that are informational or mechanically
// trackable rather than irreducible human decisions.
var mechanicalCategory = map[AttentionCategory]bool{
	AttentionResultGraphChange: true,
	AttentionWaiverExpiring:    true,
	AttentionModelCandidate:    true,
}

// categoryTieRank gives a stable, deterministic tie-break order across
// categories when two items score equally.
var categoryTieRank = map[AttentionCategory]int{
	AttentionScopeViolation:    0,
	AttentionAuthorityConflict: 1,
	AttentionContradiction:     2,
	AttentionUnknownDirection:  3,
	AttentionMissingProof:      4,
	AttentionClosureBlocker:    5,
	AttentionArchitectQuestion: 6,
	AttentionWaiverExpiring:    7,
	AttentionResultGraphChange: 8,
	AttentionModelCandidate:    9,
}

// attentionScore computes the deterministic rank score for one item.
func attentionScore(a ReviewerAttentionItem) int {
	score := 0
	if a.Blocking {
		score += weightBlocking
	}
	score += severityWeight[a.Severity] * weightSeverityUnit
	score += clamp(a.TaskRelevance, 0, 3) * weightRelevanceUnit
	score += clamp(a.ArchitecturalReach, 0, 3) * weightReachUnit
	if humanDecisionCategory[a.Category] {
		score += weightHumanDecision
	}
	if mechanicalCategory[a.Category] {
		score -= penaltyMechanical
	}
	return score
}

// RankReviewerAttention scores, de-duplicates, and orders attention items
// deterministically: highest score first, blocking-first on ties, then severity,
// category tie-rank, and ID. Equivalent questions (same normalized text, or same
// ID) collapse to the highest-ranked occurrence. The full ranked set is
// returned; callers cap the rendered view, keeping the complete set in JSON.
func RankReviewerAttention(items []ReviewerAttentionItem) []ReviewerAttentionItem {
	if len(items) == 0 {
		return nil
	}
	ranked := make([]ReviewerAttentionItem, len(items))
	copy(ranked, items)
	for i := range ranked {
		ranked[i].RankScore = attentionScore(ranked[i])
	}
	sort.SliceStable(ranked, func(i, j int) bool { return attentionLess(ranked[i], ranked[j]) })

	seen := make(map[string]struct{}, len(ranked))
	out := make([]ReviewerAttentionItem, 0, len(ranked))
	for _, a := range ranked {
		if _, dup := seen[attentionDedupKey(a)]; dup {
			continue
		}
		seen[attentionDedupKey(a)] = struct{}{}
		out = append(out, a)
	}
	return out
}

// attentionLess orders items so the higher-priority item sorts first.
func attentionLess(a, b ReviewerAttentionItem) bool {
	if a.RankScore != b.RankScore {
		return a.RankScore > b.RankScore
	}
	if a.Blocking != b.Blocking {
		return a.Blocking
	}
	if severityWeight[a.Severity] != severityWeight[b.Severity] {
		return severityWeight[a.Severity] > severityWeight[b.Severity]
	}
	if categoryTieRank[a.Category] != categoryTieRank[b.Category] {
		return categoryTieRank[a.Category] < categoryTieRank[b.Category]
	}
	return a.ID < b.ID
}

// attentionDedupKey collapses equivalent questions: the normalized question
// text if present, else the item ID.
func attentionDedupKey(a ReviewerAttentionItem) string {
	q := strings.ToLower(strings.Join(strings.Fields(a.Question), " "))
	if q != "" {
		return "q:" + q
	}
	return "id:" + strings.TrimSpace(a.ID)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
