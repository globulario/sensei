// SPDX-License-Identifier: AGPL-3.0-only

package questiongen

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/deviation"
)

const (
	DeviationQuestionSchemaVersion = "1"
	DeviationQuestionGeneratedBy   = "sensei generate-deviation-questions"
)

// DeviationQuestionContext binds exact Phase 10.6 analysis to the canonical
// dialogue owner. It does not promote candidates or mutate governed sources.
type DeviationQuestionContext struct {
	Analysis  deviation.Analysis
	Existing  *architecture.DialogueDocument
	CreatedAt string
}

// DeviationQuestionResult returns normalized dialogue and explicit accounting.
type DeviationQuestionResult struct {
	Dialogue architecture.DialogueDocument
	Report   DeviationQuestionReport
}

// DeviationQuestionReport accounts for every repeated-deviation candidate.
type DeviationQuestionReport struct {
	SchemaVersion                 string                              `json:"schema_version" yaml:"schema_version"`
	GeneratedBy                   string                              `json:"generated_by" yaml:"generated_by"`
	Binding                       architecture.ClaimDocumentBinding   `json:"binding" yaml:"binding"`
	SourceDeviationDigestSHA256   string                              `json:"source_deviation_digest_sha256" yaml:"source_deviation_digest_sha256"`
	Generated                     []DeviationQuestionItem             `json:"generated,omitempty" yaml:"generated,omitempty"`
	ExistingCoverage              []DeviationQuestionItem             `json:"existing_coverage,omitempty" yaml:"existing_coverage,omitempty"`
}

// DeviationQuestionItem binds one question disposition to its exact pattern.
type DeviationQuestionItem struct {
	PatternID   string `json:"pattern_id" yaml:"pattern_id"`
	CandidateID string `json:"candidate_id" yaml:"candidate_id"`
	ClaimID     string `json:"claim_id" yaml:"claim_id"`
	QuestionID  string `json:"question_id" yaml:"question_id"`
	Disposition string `json:"disposition" yaml:"disposition"`
}

// GenerateFromDeviation routes repeated independent deviations through the
// existing architecture dialogue. Repetition increases priority, not authority.
func GenerateFromDeviation(ctx DeviationQuestionContext) (DeviationQuestionResult, error) {
	if strings.TrimSpace(ctx.CreatedAt) == "" {
		return DeviationQuestionResult{}, errors.New("created_at is required")
	}
	if err := deviation.ValidateAnalysis(ctx.Analysis); err != nil {
		return DeviationQuestionResult{}, fmt.Errorf("deviation analysis is not receipt-valid: %w", err)
	}
	actualDigest, err := deviation.AnalysisDigest(ctx.Analysis)
	if err != nil {
		return DeviationQuestionResult{}, err
	}
	if ctx.Analysis.Receipt.ExactAnalysisDigestSHA256 != actualDigest {
		return DeviationQuestionResult{}, errors.New("deviation analysis digest does not match its receipt")
	}

	binding := ctx.Analysis.Binding
	dialogue := architecture.DialogueDocument{
		SchemaVersion: "1",
		CompiledBy:    DeviationQuestionGeneratedBy,
		Binding:       binding,
	}
	if ctx.Existing != nil {
		if !bindingsEqual(ctx.Existing.Binding, binding) {
			return DeviationQuestionResult{}, errors.New("existing dialogue binding does not match deviation analysis")
		}
		dialogue = *ctx.Existing
		dialogue.OpenQuestions = append([]architecture.OpenQuestion(nil), ctx.Existing.OpenQuestions...)
		dialogue.Answers = append([]architecture.ArchitectAnswer(nil), ctx.Existing.Answers...)
	}
	report := DeviationQuestionReport{
		SchemaVersion:               DeviationQuestionSchemaVersion,
		GeneratedBy:                 DeviationQuestionGeneratedBy,
		Binding:                     binding,
		SourceDeviationDigestSHA256: actualDigest,
	}

	patternsByID := make(map[string]deviation.Pattern, len(ctx.Analysis.Patterns))
	for _, pattern := range ctx.Analysis.Patterns {
		patternsByID[pattern.ID] = pattern
	}
	candidates := append([]deviation.Candidate(nil), ctx.Analysis.Candidates...)
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	for _, candidate := range candidates {
		pattern, ok := patternsByID[candidate.PatternID]
		if !ok {
			return DeviationQuestionResult{}, fmt.Errorf("deviation candidate %s references missing pattern %s", candidate.ID, candidate.PatternID)
		}
		question := repeatedDeviationQuestion(ctx.CreatedAt, actualDigest, pattern, candidate)
		disposition, appendErr := appendInvestigationQuestion(&dialogue, question)
		if appendErr != nil {
			return DeviationQuestionResult{}, appendErr
		}
		item := DeviationQuestionItem{
			PatternID:   pattern.ID,
			CandidateID: candidate.ID,
			ClaimID:     candidate.Claim.ID,
			QuestionID:  question.ID,
			Disposition: disposition,
		}
		if disposition == InvestigationDispositionExistingCovers {
			report.ExistingCoverage = append(report.ExistingCoverage, item)
		} else {
			report.Generated = append(report.Generated, item)
		}
	}

	normalized, err := architecture.NormalizeDialogueDocument(dialogue)
	if err != nil {
		return DeviationQuestionResult{}, err
	}
	normalizeDeviationQuestionItems(report.Generated)
	normalizeDeviationQuestionItems(report.ExistingCoverage)
	return DeviationQuestionResult{Dialogue: normalized, Report: report}, nil
}

func repeatedDeviationQuestion(createdAt, sourceDigest string, pattern deviation.Pattern, candidate deviation.Candidate) architecture.OpenQuestion {
	claimIDs := append([]string{candidate.Claim.ID}, pattern.RelatedClaimIDs...)
	claimIDs = cleanStrings(claimIDs)
	nodeRefs := make([]string, 0, len(claimIDs))
	for _, claimID := range claimIDs {
		nodeRefs = append(nodeRefs, "architecture_claim:"+claimID)
	}
	sourceRefs := append([]string{pattern.ID, candidate.ID, candidate.Claim.ID}, pattern.ReceiptIDs...)
	question := architecture.OpenQuestion{
		QuestionText: fmt.Sprintf(
			"Across %d independent implementation tasks, the same deviation recurred: %s %s %s. Does this reveal incorrect architecture, incorrect scope, a missing governed exception or contract, or repeated noncompliance?",
			pattern.IndependentOccurrenceCount,
			pattern.Shape.Subject,
			pattern.Shape.Predicate,
			pattern.Shape.Object,
		),
		Scope:                      candidate.Claim.Scope,
		BlocksClosureDimension:     deviationQuestionDimension(candidate.Kind),
		BlocksClaims:               claimIDs,
		BlocksNodes:                nodeRefs,
		QuestionTemplateID:         TemplateRepeatedDeviation,
		QuestionTemplateVersion:    "v1",
		QuestionSourceKind:         architecture.SourceDeviationPattern,
		SourceArtifactDigestSHA256: sourceDigest,
		SourceReferenceIDs:         cleanStrings(sourceRefs),
		AcceptedAnswerTypes: []string{
			architecture.AnswerTypeIntentStatement,
			architecture.AnswerTypeGovernedDecisionCandidate,
			architecture.AnswerTypeHistoricalContext,
			architecture.AnswerTypeScopeClarification,
			architecture.AnswerTypeExceptionAuthorization,
			architecture.AnswerTypeEvidencePointer,
			architecture.AnswerTypeUnknownAcknowledgement,
			architecture.AnswerTypeQuestionReframing,
		},
		ReasonsOpen: []string{
			fmt.Sprintf("%d independent occurrences exceeded the configured threshold of %d", pattern.IndependentOccurrenceCount, pattern.MinimumIndependentOccurrences),
			"repetition raises investigation priority but does not weaken architecture",
		},
		KnownEvidence:           append([]string(nil), pattern.EvidenceRefs...),
		SupportingEvidence:      append([]string(nil), pattern.EvidenceRefs...),
		RefutingEvidence:        append([]string(nil), candidate.Claim.RefutingEvidence...),
		FalsificationConditions: append([]string(nil), candidate.Claim.InvalidationConditions...),
		SuggestedAnswerOwner:    deviationAnswerOwner(candidate.Kind),
		CompetingHypotheses: []architecture.QuestionHypothesis{
			{ID: "deviation.architecture_gap", Statement: "The repeated deviation exposes an incorrect or incomplete architectural contract."},
			{ID: "deviation.scope_or_exception_missing", Statement: "The architecture is valid but its scope or governed exception path is incomplete."},
			{ID: "deviation.repeated_noncompliance", Statement: "The architecture is valid and implementations repeatedly choose a forbidden shortcut."},
		},
		MissingEvidence:    append([]string(nil), candidate.Claim.Unknowns...),
		Priority:           deviationQuestionPriority(candidate.Kind),
		RiskIfUnresolved:   "Sensei cannot distinguish architectural debt from repeated implementation noncompliance, so no rule may be weakened or promoted safely.",
		ArchitectRequired:  true,
		Status:             architecture.QuestionStatusAwaitingArchitect,
		CreatedAt:          strings.TrimSpace(createdAt),
	}
	question.ID = architecture.StableOpenQuestionID(question)
	return question
}

func deviationQuestionDimension(kind deviation.CandidateKind) string {
	switch kind {
	case deviation.CandidateOwner:
		return architecture.ClosureAuthority
	case deviation.CandidateContract:
		return architecture.ClosureContract
	case deviation.CandidateBoundary:
		return architecture.ClosureStructural
	case deviation.CandidateFailureMode:
		return architecture.ClosureBehavioral
	default:
		return architecture.ClosureEvidence
	}
}

func deviationQuestionPriority(kind deviation.CandidateKind) string {
	switch kind {
	case deviation.CandidateOwner, deviation.CandidateBoundary:
		return architecture.QuestionPriorityHigh
	case deviation.CandidateContract, deviation.CandidateFailureMode:
		return architecture.QuestionPriorityMedium
	default:
		return architecture.QuestionPriorityLow
	}
}

func deviationAnswerOwner(kind deviation.CandidateKind) string {
	switch kind {
	case deviation.CandidateOwner:
		return "authority owner or architect"
	case deviation.CandidateBoundary:
		return "boundary owner or architect"
	case deviation.CandidateContract:
		return "contract owner or architect"
	case deviation.CandidateFailureMode:
		return "runtime owner or architect"
	default:
		return "governance owner or architect"
	}
}

func normalizeDeviationQuestionItems(items []DeviationQuestionItem) {
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i].PatternID + "\x00" + items[i].CandidateID + "\x00" + items[i].QuestionID
		right := items[j].PatternID + "\x00" + items[j].CandidateID + "\x00" + items[j].QuestionID
		return left < right
	})
}
