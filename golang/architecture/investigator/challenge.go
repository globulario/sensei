// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

type defaultChallenger struct{}

// NewChallenger returns the default Challenger implementation.
func NewChallenger() Challenger {
	return &defaultChallenger{}
}

func (c *defaultChallenger) Challenge(ctx context.Context, input Input, claims []architecture.Claim, opts Options) ([]investigation.Counterexample, []architecture.OpenQuestion, error) {
	var counterexamples []investigation.Counterexample
	var questions []architecture.OpenQuestion

	capturedAt := opts.CapturedAt
	if capturedAt == "" {
		capturedAt = time.Now().Format(time.RFC3339)
	}

	for _, claim := range claims {
		var refutingEvidence []string
		var refutingText string

		for _, rec := range input.Document.RawEvidence {
			content := strings.ToLower(rec.CapturedContent)
			if (strings.Contains(content, "incident") || strings.Contains(content, "violation") || strings.Contains(content, "failure")) &&
				(strings.Contains(content, claim.Statement.Subject) || containsSymbol(claim.Scope, rec.Scope)) {
				refutingEvidence = append(refutingEvidence, rec.ID)
				refutingText = rec.CapturedContent
			}
		}

		if len(refutingEvidence) > 0 {
			ceID := fmt.Sprintf("counterexample_%s", claim.ID)
			counterexamples = append(counterexamples, investigation.Counterexample{
				ID:             ceID,
				ClaimID:        claim.ID,
				Description:    fmt.Sprintf("Adversarial counterexample refuting candidate %q: %s", claim.Label, refutingText),
				Scope:          claim.Scope,
				EvidenceRefIDs: refutingEvidence,
			})

			// Class-qualify known evidence
			knownEvidenceRefs := make([]string, len(refutingEvidence))
			for i, evID := range refutingEvidence {
				knownEvidenceRefs[i] = "evidence:" + evID
			}

			questions = append(questions, architecture.OpenQuestion{
				Label:                               fmt.Sprintf("Adversarial Challenge for %s", claim.Label),
				QuestionText:                        fmt.Sprintf("Skeptic challenge: Counterexample %s contradicts candidate %q. How is this resolved?", ceID, claim.Label),
				Scope:                               claim.Scope,
				BlocksClosureDimension:              architecture.ClosureContradiction,
				BlocksClaims:                        []string{claim.ID},
				BlocksClosureBlockers:               []string{"blocker.contradiction.000000000000"},
				QuestionTemplateID:                  "question.counterexample_validation.v1",
				QuestionTemplateVersion:             "1.0",
				SourceClosureAssessmentDigestSHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
				AcceptedAnswerTypes:                 []string{architecture.AnswerTypeQuestionReframing},
				ReasonsOpen:                         []string{fmt.Sprintf("Refuting evidence ID: %s", refutingEvidence[0])},
				KnownEvidence:                       knownEvidenceRefs,
				Priority:                            architecture.QuestionPriorityHigh,
				RiskIfUnresolved:                    "Promoting an contradicted/refuted claim violates architectural invariants.",
				Status:                              architecture.QuestionStatusOpen,
				CreatedAt:                           capturedAt,
			})
		}
	}

	return counterexamples, questions, nil
}

func containsSymbol(claimScope, evidenceScope architecture.ClaimScope) bool {
	for _, sym1 := range claimScope.Symbols {
		for _, sym2 := range evidenceScope.Symbols {
			if sym1 == sym2 {
				return true
			}
		}
	}
	return false
}
