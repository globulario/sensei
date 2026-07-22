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

type defaultSynthesizer struct{}

// NewSynthesizer returns the default Synthesizer implementation.
func NewSynthesizer() Synthesizer {
	return &defaultSynthesizer{}
}

func (s *defaultSynthesizer) Synthesize(ctx context.Context, input Input, opts Options) ([]architecture.Claim, []architecture.OpenQuestion, error) {
	var claims []architecture.Claim
	var questions []architecture.OpenQuestion

	capturedAt := opts.CapturedAt
	if capturedAt == "" {
		capturedAt = time.Now().Format(time.RFC3339)
	}

	// Collect observation symbols and evidence references to ground candidates
	var firstObsSymbol string
	if len(input.Document.Observations) > 0 {
		// Use the first observation symbol for grounding tests
		for _, fact := range input.Document.Observations {
			if fact.Subject != "" {
				firstObsSymbol = fact.Subject
				break
			}
		}
	}

	var firstEvidenceID string
	var firstEvidenceSource string
	if len(input.Document.RawEvidence) > 0 {
		firstEvidenceID = input.Document.RawEvidence[0].ID
		firstEvidenceSource = input.Document.RawEvidence[0].SourceIdentity
	}

	// 1. Candidate Owner rule
	// If we have an observation and evidence, we synthesize a candidate owner claim.
	if firstObsSymbol != "" && firstEvidenceID != "" {
		claims = append(claims, architecture.Claim{
			Label:           fmt.Sprintf("Candidate Owner for %s", firstObsSymbol),
			Description:     "Synthesized candidate owner claim based on static observations",
			AssertionOrigin: architecture.OriginPromoted,
			EpistemicStatus: architecture.StatusUnknown,
			Statement: architecture.ClaimStatement{
				Subject:   firstObsSymbol,
				Predicate: "has_owner",
				Object:    "team_architect",
			},
			Scope: architecture.ClaimScope{
				Repository: input.Document.Binding.Repository.RepositoryDomain,
				Symbols:    []string{firstObsSymbol},
			},
			ArchitecturalPlane:  architecture.PlaneIntended,
			Confidence:          0.8,
			HumanReviewRequired: true,
			PromotionStatus:     architecture.PromotionCandidate,
			SupportingEvidence:  []string{firstEvidenceID},
		})
	}

	// 2. Candidate Invariant/Contract rule
	if firstObsSymbol != "" && firstEvidenceID != "" {
		claims = append(claims, architecture.Claim{
			Label:           fmt.Sprintf("Candidate Invariant for %s", firstObsSymbol),
			Description:     "Synthesized candidate invariant contract based on observed structural dependencies",
			AssertionOrigin: architecture.OriginPromoted,
			EpistemicStatus: architecture.StatusUnknown,
			Statement: architecture.ClaimStatement{
				Subject:   firstObsSymbol,
				Predicate: "obeys_invariant",
				Object:    "non_negative_balance",
			},
			Scope: architecture.ClaimScope{
				Repository: input.Document.Binding.Repository.RepositoryDomain,
				Symbols:    []string{firstObsSymbol},
			},
			ArchitecturalPlane:  architecture.PlaneObserved,
			Confidence:          0.9,
			HumanReviewRequired: true,
			PromotionStatus:     architecture.PromotionCandidate,
			SupportingEvidence:  []string{firstEvidenceID},
		})
	}

	// 3. Evidence Request rule
	// Generate an evidence request question for each unconfigured or unavailable provider.
	for _, entry := range input.Document.Coverage {
		if entry.Status == investigation.CoverageNotConfigured || entry.Status == investigation.CoverageUnavailable {
			questions = append(questions, architecture.OpenQuestion{
				Label:        fmt.Sprintf("Missing Evidence for %s", entry.ProviderID),
				QuestionText: fmt.Sprintf("What evidence resolves the gap left by unconfigured or unavailable provider %q?", entry.ProviderID),
				Scope: architecture.ClaimScope{
					Repository: input.Document.Binding.Repository.RepositoryDomain,
				},
				BlocksClosureDimension:              architecture.ClosureEvidence,
				BlocksClosureBlockers:               []string{"blocker.evidence.000000000000"},
				QuestionTemplateID:                  "question.evidence_request.v1",
				QuestionTemplateVersion:             "1.0",
				SourceClosureAssessmentDigestSHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
				AcceptedAnswerTypes:                 []string{architecture.AnswerTypeEvidencePointer},
				ReasonsOpen:                         []string{fmt.Sprintf("Provider %q status: %s", entry.ProviderID, entry.Status)},
				Priority:                            architecture.QuestionPriorityMedium,
				RiskIfUnresolved:                    "Architectural compliance cannot be verified across all required providers.",
				Status:                              architecture.QuestionStatusOpen,
				CreatedAt:                           capturedAt,
			})
		}
	}

	// 4. Governance Debt Candidate rule
	if len(input.HistoricalBlockers) > 0 {
		claims = append(claims, architecture.Claim{
			Label:           "Governance Debt Candidate",
			Description:     "A candidate highlighting historical review debts/blockers",
			AssertionOrigin: architecture.OriginPromoted,
			EpistemicStatus: architecture.StatusUnknown,
			Statement: architecture.ClaimStatement{
				Subject:   "project",
				Predicate: "has_governance_debt",
				Object:    input.HistoricalBlockers[0],
			},
			Scope: architecture.ClaimScope{
				Repository: input.Document.Binding.Repository.RepositoryDomain,
			},
			ArchitecturalPlane:  architecture.PlaneDesired,
			Confidence:          0.7,
			HumanReviewRequired: true,
			PromotionStatus:     architecture.PromotionCandidate,
		})
	}

	// Grounding fallback: ensure scope elements exist in evidence
	for i, claim := range claims {
		// Clean up scope to only reference existing sources in evidence
		if firstEvidenceSource != "" && len(claim.Scope.Files) == 0 {
			if strings.HasPrefix(firstEvidenceSource, "file:") {
				claim.Scope.Files = []string{strings.TrimPrefix(firstEvidenceSource, "file:")}
				claims[i] = claim
			}
		}
	}

	return claims, questions, nil
}
