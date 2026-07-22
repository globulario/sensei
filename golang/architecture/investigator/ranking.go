// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"context"
	"sort"

	"github.com/globulario/sensei/golang/architecture"
)

type defaultRanker struct{}

// NewRanker returns the default Ranker implementation.
func NewRanker() Ranker {
	return &defaultRanker{}
}

func (r *defaultRanker) RankClaims(ctx context.Context, claims []architecture.Claim, input Input) []architecture.Claim {
	// 1. Calculate confidence rank score for each claim
	for i, claim := range claims {
		// Base confidence
		score := 0.5

		// Contradiction density: reduce confidence if claim has refuting evidence or conflicts
		refuteCount := len(claim.RefutingEvidence)
		for _, ce := range input.Document.Counterexamples {
			if ce.ClaimID == claim.ID {
				refuteCount++
			}
		}

		if refuteCount > 0 {
			score -= float64(refuteCount) * 0.2
		}

		// Supporting evidence: increase confidence for more evidence receipts
		score += float64(len(claim.SupportingEvidence)) * 0.1

		// Keep confidence strictly bounded between 0.0 and 1.0 (non-authoritative ranking metadata only)
		if score < 0.0 {
			score = 0.0
		}
		if score > 1.0 {
			score = 1.0
		}

		claims[i].Confidence = score
	}

	// 2. Sort claims by confidence in descending order
	sort.SliceStable(claims, func(i, j int) bool {
		return claims[i].Confidence > claims[j].Confidence
	})

	return claims
}

func (r *defaultRanker) RankQuestions(ctx context.Context, questions []architecture.OpenQuestion, input Input) []architecture.OpenQuestion {
	// Sort questions by priority (critical > high > medium > low)
	priorityWeight := func(p string) int {
		switch p {
		case architecture.QuestionPriorityCritical:
			return 4
		case architecture.QuestionPriorityHigh:
			return 3
		case architecture.QuestionPriorityMedium:
			return 2
		case architecture.QuestionPriorityLow:
			return 1
		default:
			return 0
		}
	}

	sort.SliceStable(questions, func(i, j int) bool {
		return priorityWeight(questions[i].Priority) > priorityWeight(questions[j].Priority)
	})

	return questions
}
