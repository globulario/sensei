// SPDX-License-Identifier: AGPL-3.0-only

package architecture

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	ClosureStructural    = "structural"
	ClosureAuthority     = "authority"
	ClosureContract      = "contract"
	ClosureBehavioral    = "behavioral"
	ClosureEvidence      = "evidence"
	ClosureContradiction = "contradiction"
	ClosureDirection     = "direction"
	ClosureAgent         = "agent"

	QuestionPriorityCritical = "critical"
	QuestionPriorityHigh     = "high"
	QuestionPriorityMedium   = "medium"
	QuestionPriorityLow      = "low"

	QuestionStatusOpen              = "open"
	QuestionStatusAwaitingArchitect = "awaiting_architect"
	QuestionStatusAwaitingEvidence  = "awaiting_evidence"
	QuestionStatusAnswered          = "answered"
	QuestionStatusResolved          = "resolved"
	QuestionStatusAcceptedUnknown   = "accepted_unknown"
	QuestionStatusSuperseded        = "superseded"

	SourceClosureBlocker         QuestionSourceKind = "closure_blocker"
	SourceInvestigationCandidate QuestionSourceKind = "investigation_candidate"
	SourceCounterexample         QuestionSourceKind = "counterexample"
	SourceEvidenceGap            QuestionSourceKind = "evidence_gap"
	SourceDeviationPattern       QuestionSourceKind = "deviation_pattern"

	AnswerTypeIntentStatement           = "intent_statement"
	AnswerTypeDesiredDirection          = "desired_direction"
	AnswerTypeGovernedDecisionCandidate = "governed_decision_candidate"
	AnswerTypeHistoricalContext         = "historical_context"
	AnswerTypeScopeClarification        = "scope_clarification"
	AnswerTypeExceptionAuthorization    = "exception_authorization"
	AnswerTypeEvidencePointer           = "evidence_pointer"
	AnswerTypeUnknownAcknowledgement    = "unknown_acknowledgement"
	AnswerTypeQuestionReframing         = "question_reframing"
	AnswerGovernanceRecorded            = "recorded"
	AnswerGovernanceAwaitingEvidence    = "awaiting_evidence"
	AnswerGovernanceAwaitingGovernance  = "awaiting_governance"
	AnswerGovernanceAcceptedForQuestion = "accepted_for_question"
	AnswerGovernanceRejected            = "rejected"
	AnswerGovernanceSuperseded          = "superseded"
)

var dialogueTokenRE = regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)
var closureBlockerIDRE = regexp.MustCompile(`^blocker\.(structural|authority|contract|behavioral|evidence|contradiction|direction|agent)\.[a-f0-9]{12}$`)
var lowercaseSHA256RE = regexp.MustCompile(`^[a-f0-9]{64}$`)

// QuestionSourceKind identifies the governed source class that produced a question.
type QuestionSourceKind string

func isQuestionSourceKind(kind QuestionSourceKind) bool {
	switch kind {
	case SourceClosureBlocker, SourceInvestigationCandidate, SourceCounterexample, SourceEvidenceGap, SourceDeviationPattern:
		return true
	default:
		return false
	}
}

type OpenQuestion struct {
	ID                                  string               `json:"id" yaml:"id"`
	Label                               string               `json:"label,omitempty" yaml:"label,omitempty"`
	QuestionText                        string               `json:"question_text" yaml:"question_text"`
	Scope                               ClaimScope           `json:"scope,omitempty" yaml:"scope,omitempty"`
	BlocksClosureDimension              string               `json:"blocks_closure_dimension" yaml:"blocks_closure_dimension"`
	BlocksClaims                        []string             `json:"blocks_claims,omitempty" yaml:"blocks_claims,omitempty"`
	BlocksNodes                         []string             `json:"blocks_nodes,omitempty" yaml:"blocks_nodes,omitempty"`
	BlocksClosureBlockers               []string             `json:"blocks_closure_blockers,omitempty" yaml:"blocks_closure_blockers,omitempty"`
	QuestionTemplateID                  string               `json:"question_template_id,omitempty" yaml:"question_template_id,omitempty"`
	QuestionTemplateVersion             string               `json:"question_template_version,omitempty" yaml:"question_template_version,omitempty"`
	SourceClosureAssessmentDigestSHA256 string               `json:"source_closure_assessment_digest_sha256,omitempty" yaml:"source_closure_assessment_digest_sha256,omitempty"`
	QuestionSourceKind                  QuestionSourceKind   `json:"question_source_kind,omitempty" yaml:"question_source_kind,omitempty"`
	SourceArtifactDigestSHA256          string               `json:"source_artifact_digest_sha256,omitempty" yaml:"source_artifact_digest_sha256,omitempty"`
	SourceReferenceIDs                  []string             `json:"source_reference_ids,omitempty" yaml:"source_reference_ids,omitempty"`
	AcceptedAnswerTypes                 []string             `json:"accepted_answer_types" yaml:"accepted_answer_types"`
	ReasonsOpen                         []string             `json:"reasons_open" yaml:"reasons_open"`
	KnownFactIDs                        []string             `json:"known_fact_ids,omitempty" yaml:"known_fact_ids,omitempty"`
	KnownEvidence                       []string             `json:"known_evidence,omitempty" yaml:"known_evidence,omitempty"`
	SupportingEvidence                  []string             `json:"supporting_evidence,omitempty" yaml:"supporting_evidence,omitempty"`
	RefutingEvidence                    []string             `json:"refuting_evidence,omitempty" yaml:"refuting_evidence,omitempty"`
	FalsificationConditions             []string             `json:"falsification_conditions,omitempty" yaml:"falsification_conditions,omitempty"`
	SuggestedAnswerOwner                string               `json:"suggested_answer_owner,omitempty" yaml:"suggested_answer_owner,omitempty"`
	CompetingHypotheses                 []QuestionHypothesis `json:"competing_hypotheses,omitempty" yaml:"competing_hypotheses,omitempty"`
	MissingEvidence                     []string             `json:"missing_evidence,omitempty" yaml:"missing_evidence,omitempty"`
	Priority                            string               `json:"priority" yaml:"priority"`
	RiskIfUnresolved                    string               `json:"risk_if_unresolved" yaml:"risk_if_unresolved"`
	ArchitectRequired                   bool                 `json:"architect_required" yaml:"architect_required"`
	Status                              string               `json:"status" yaml:"status"`
	ResolvedByAnswers                   []string             `json:"resolved_by_answers,omitempty" yaml:"resolved_by_answers,omitempty"`
	SupersededByQuestion                string               `json:"superseded_by_question,omitempty" yaml:"superseded_by_question,omitempty"`
	CreatedAt                           string               `json:"created_at" yaml:"created_at"`
	LastReviewedAt                      string               `json:"last_reviewed_at,omitempty" yaml:"last_reviewed_at,omitempty"`
}

type QuestionHypothesis struct {
	ID        string `json:"id" yaml:"id"`
	Statement string `json:"statement" yaml:"statement"`
}

type ArchitectAnswer struct {
	ID                   string                `json:"id" yaml:"id"`
	Label                string                `json:"label,omitempty" yaml:"label,omitempty"`
	AnswersQuestions     []string              `json:"answers_questions" yaml:"answers_questions"`
	Author               AnswerAuthor          `json:"author" yaml:"author"`
	Statement            string                `json:"statement" yaml:"statement"`
	Classifications      []string              `json:"classifications" yaml:"classifications"`
	Scope                ClaimScope            `json:"scope,omitempty" yaml:"scope,omitempty"`
	Conditions           []string              `json:"conditions,omitempty" yaml:"conditions,omitempty"`
	EvidenceRefs         []string              `json:"evidence_refs,omitempty" yaml:"evidence_refs,omitempty"`
	EvidencePointers     []string              `json:"evidence_pointers,omitempty" yaml:"evidence_pointers,omitempty"`
	SelectedHypotheses   []HypothesisSelection `json:"selected_hypotheses,omitempty" yaml:"selected_hypotheses,omitempty"`
	ReframedQuestion     string                `json:"reframed_question,omitempty" yaml:"reframed_question,omitempty"`
	RecordedAt           string                `json:"recorded_at" yaml:"recorded_at"`
	GovernanceStatus     string                `json:"governance_status" yaml:"governance_status"`
	SupersededByAnswer   string                `json:"superseded_by_answer,omitempty" yaml:"superseded_by_answer,omitempty"`
	RequiresEvidence     bool                  `json:"requires_evidence,omitempty" yaml:"requires_evidence,omitempty"`
	MissingEvidenceNotes []string              `json:"missing_evidence,omitempty" yaml:"missing_evidence,omitempty"`
}

type AnswerAuthor struct {
	Role string `json:"role" yaml:"role"`
	ID   string `json:"id,omitempty" yaml:"id,omitempty"`
}

type HypothesisSelection struct {
	QuestionID   string `json:"question_id" yaml:"question_id"`
	HypothesisID string `json:"hypothesis_id" yaml:"hypothesis_id"`
}

func StableOpenQuestionID(q OpenQuestion) string {
	q = canonicalizeOpenQuestion(q)
	repo := q.Scope.Repository
	if repo == "" {
		repo = q.Scope.Repo
	}
	ids := make([]string, 0, len(q.CompetingHypotheses))
	for _, h := range q.CompetingHypotheses {
		ids = append(ids, h.ID)
	}
	sort.Strings(ids)
	parts := []string{
		repo,
		q.Scope.Domain,
		q.BlocksClosureDimension,
		q.QuestionText,
		strings.Join(q.BlocksClaims, ","),
		strings.Join(ids, ","),
	}
	if q.QuestionTemplateID != "" || q.QuestionTemplateVersion != "" || len(q.BlocksNodes) > 0 || len(q.BlocksClosureBlockers) > 0 {
		parts = append(parts,
			q.QuestionTemplateID,
			q.QuestionTemplateVersion,
			strings.Join(q.BlocksNodes, ","),
			strings.Join(q.BlocksClosureBlockers, ","),
		)
	}
	if q.QuestionSourceKind != "" || q.SourceArtifactDigestSHA256 != "" || len(q.SourceReferenceIDs) > 0 {
		parts = append(parts,
			string(q.QuestionSourceKind),
			q.SourceArtifactDigestSHA256,
			strings.Join(q.SourceReferenceIDs, ","),
			strings.Join(q.SupportingEvidence, ","),
			strings.Join(q.RefutingEvidence, ","),
			strings.Join(q.FalsificationConditions, ","),
			q.SuggestedAnswerOwner,
		)
	}
	return "question." + shortDialogueHash(strings.Join(parts, "|"))
}

func StableArchitectAnswerID(a ArchitectAnswer) string {
	a = canonicalizeArchitectAnswer(a)
	parts := []string{
		strings.Join(a.AnswersQuestions, ","),
		a.Statement,
		strings.Join(a.Classifications, ","),
		a.Author.Role,
		a.Author.ID,
		a.RecordedAt,
	}
	return "answer." + shortDialogueHash(strings.Join(parts, "|"))
}

func NormalizeOpenQuestions(questions []OpenQuestion) ([]OpenQuestion, error) {
	out := make([]OpenQuestion, 0, len(questions))
	for _, in := range questions {
		q := canonicalizeOpenQuestion(in)
		if q.ID == "" {
			q.ID = StableOpenQuestionID(q)
		}
		if err := ValidateOpenQuestion(q); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	seen := map[string]OpenQuestion{}
	dedup := out[:0]
	for _, q := range out {
		if existing, ok := seen[q.ID]; ok {
			if !openQuestionsEqual(existing, q) {
				return nil, fmt.Errorf("open question id collision for %s", q.ID)
			}
			continue
		}
		seen[q.ID] = q
		dedup = append(dedup, q)
	}
	return dedup, nil
}

func NormalizeArchitectAnswers(answers []ArchitectAnswer) ([]ArchitectAnswer, error) {
	out := make([]ArchitectAnswer, 0, len(answers))
	for _, in := range answers {
		a := canonicalizeArchitectAnswer(in)
		if a.ID == "" {
			a.ID = StableArchitectAnswerID(a)
		}
		if err := ValidateArchitectAnswer(a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	seen := map[string]ArchitectAnswer{}
	dedup := out[:0]
	for _, a := range out {
		if existing, ok := seen[a.ID]; ok {
			if !architectAnswersEqual(existing, a) {
				return nil, fmt.Errorf("architect answer id collision for %s", a.ID)
			}
			continue
		}
		seen[a.ID] = a
		dedup = append(dedup, a)
	}
	return dedup, nil
}

func ValidateOpenQuestion(q OpenQuestion) error {
	var errs []string
	if q.QuestionText == "" {
		errs = append(errs, "question_text is required")
	}
	if !oneOf(q.BlocksClosureDimension, ClosureStructural, ClosureAuthority, ClosureContract, ClosureBehavioral, ClosureEvidence, ClosureContradiction, ClosureDirection, ClosureAgent) {
		errs = append(errs, "unknown closure dimension")
	}
	if len(q.BlocksClaims)+len(q.BlocksNodes)+len(q.BlocksClosureBlockers) == 0 {
		errs = append(errs, "at least one blocked claim, node, or closure blocker is required")
	}
	for _, id := range q.BlocksClaims {
		if strings.TrimSpace(id) == "" {
			errs = append(errs, "blocked claim ID must not be empty")
			break
		}
	}
	for _, ref := range q.BlocksNodes {
		if _, _, ok := ParseClassQualifiedReference(ref); !ok {
			errs = append(errs, "blocks_nodes must contain class-qualified graph references")
			break
		}
	}
	for _, id := range q.BlocksClosureBlockers {
		if !closureBlockerIDRE.MatchString(id) {
			errs = append(errs, "blocks_closure_blockers contains malformed blocker ID")
			break
		}
	}
	hasTemplateMetadata := q.QuestionTemplateID != "" || q.QuestionTemplateVersion != ""
	hasClosureSource := q.SourceClosureAssessmentDigestSHA256 != "" || q.QuestionSourceKind == SourceClosureBlocker
	hasArtifactSource := (q.QuestionSourceKind != "" && q.QuestionSourceKind != SourceClosureBlocker) || q.SourceArtifactDigestSHA256 != "" || len(q.SourceReferenceIDs) > 0
	if hasClosureSource && hasArtifactSource {
		errs = append(errs, "closure and investigation question sources are mutually exclusive")
	}
	if hasTemplateMetadata || hasClosureSource || hasArtifactSource {
		if !dialogueTokenRE.MatchString(q.QuestionTemplateID) {
			errs = append(errs, "question_template_id must be a conservative token")
		}
		if q.QuestionTemplateVersion == "" {
			errs = append(errs, "question_template_version is required")
		}
	}
	if hasClosureSource {
		generatedFields := 0
		if q.QuestionTemplateID != "" {
			generatedFields++
		}
		if q.QuestionTemplateVersion != "" {
			generatedFields++
		}
		if q.SourceClosureAssessmentDigestSHA256 != "" {
			generatedFields++
		}
		if generatedFields != 3 {
			errs = append(errs, "generated question metadata must be all-or-none")
		}
		if q.QuestionSourceKind != "" && q.QuestionSourceKind != SourceClosureBlocker {
			errs = append(errs, "closure-generated question source kind must be closure_blocker")
		}
		if len(q.BlocksClosureBlockers) == 0 {
			errs = append(errs, "generated question requires at least one closure blocker")
		}
		if !lowercaseSHA256RE.MatchString(q.SourceClosureAssessmentDigestSHA256) {
			errs = append(errs, "source_closure_assessment_digest_sha256 must be lowercase SHA-256 hex")
		}
	}
	if hasArtifactSource {
		if q.QuestionSourceKind == "" || !isQuestionSourceKind(q.QuestionSourceKind) || q.QuestionSourceKind == SourceClosureBlocker {
			errs = append(errs, "unknown question_source_kind")
		}
		if !lowercaseSHA256RE.MatchString(q.SourceArtifactDigestSHA256) {
			errs = append(errs, "source_artifact_digest_sha256 must be lowercase SHA-256 hex")
		}
		if len(q.SourceReferenceIDs) == 0 {
			errs = append(errs, "source_reference_ids is required")
		}
		for _, id := range q.SourceReferenceIDs {
			if strings.TrimSpace(id) == "" {
				errs = append(errs, "source_reference_ids must not contain empty values")
				break
			}
		}
		if len(q.FalsificationConditions) == 0 {
			errs = append(errs, "investigation-backed question requires falsification_conditions")
		}
		if q.SuggestedAnswerOwner == "" {
			errs = append(errs, "investigation-backed question requires suggested_answer_owner")
		}
	}
	if hasTemplateMetadata && !hasClosureSource && !hasArtifactSource {
		errs = append(errs, "generated question source binding is required")
	}
	if len(q.AcceptedAnswerTypes) == 0 {
		errs = append(errs, "accepted_answer_types is required")
	}
	for _, typ := range q.AcceptedAnswerTypes {
		if !isAnswerClassification(typ) {
			errs = append(errs, "unknown accepted answer type")
			break
		}
	}
	if len(q.ReasonsOpen) == 0 {
		errs = append(errs, "reasons_open is required")
	}
	if !oneOf(q.Priority, QuestionPriorityCritical, QuestionPriorityHigh, QuestionPriorityMedium, QuestionPriorityLow) {
		errs = append(errs, "unknown priority")
	}
	if q.RiskIfUnresolved == "" {
		errs = append(errs, "risk_if_unresolved is required")
	}
	if err := validateClaimScopePaths(q.Scope); err != nil {
		errs = append(errs, err.Error())
	}
	for _, ref := range q.KnownEvidence {
		class, _, ok := ParseClassQualifiedReference(ref)
		if !ok || class != "evidence" {
			errs = append(errs, "known evidence must be class-qualified as evidence:<id>")
			break
		}
	}
	for _, refs := range [][]string{q.SupportingEvidence, q.RefutingEvidence} {
		for _, ref := range refs {
			class, _, ok := ParseClassQualifiedReference(ref)
			if !ok || class != "evidence" {
				errs = append(errs, "supporting and refuting evidence must be class-qualified as evidence:<id>")
				break
			}
		}
	}
	for _, ref := range q.SupportingEvidence {
		if contains(q.RefutingEvidence, ref) {
			errs = append(errs, "supporting and refuting evidence must remain distinct")
			break
		}
	}
	for _, condition := range q.FalsificationConditions {
		if strings.TrimSpace(condition) == "" {
			errs = append(errs, "falsification_conditions must not contain empty values")
			break
		}
	}
	hypSeen := map[string]bool{}
	for _, h := range q.CompetingHypotheses {
		if h.ID == "" || !dialogueTokenRE.MatchString(h.ID) {
			errs = append(errs, "hypothesis ID must be a conservative token")
		}
		if h.Statement == "" {
			errs = append(errs, "hypothesis statement is required")
		}
		if hypSeen[h.ID] {
			errs = append(errs, "duplicate hypothesis ID")
		}
		hypSeen[h.ID] = true
	}
	if len(q.CompetingHypotheses) == 1 {
		errs = append(errs, "exactly one competing hypothesis is not allowed")
	}
	if q.ID != "" && contains(q.ResolvedByAnswers, q.ID) {
		errs = append(errs, "question must not resolve itself")
	}
	if q.ID != "" && q.SupersededByQuestion == q.ID {
		errs = append(errs, "question must not supersede itself")
	}
	if _, err := time.Parse(time.RFC3339, q.CreatedAt); err != nil {
		errs = append(errs, "created_at must be RFC3339")
	}
	if q.LastReviewedAt != "" {
		if _, err := time.Parse(time.RFC3339, q.LastReviewedAt); err != nil {
			errs = append(errs, "last_reviewed_at must be RFC3339")
		}
	}
	if err := validateOpenQuestionStatusShape(q); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func ValidateArchitectAnswer(a ArchitectAnswer) error {
	var errs []string
	if len(a.AnswersQuestions) == 0 {
		errs = append(errs, "answers_questions is required")
	}
	for _, id := range a.AnswersQuestions {
		if id == "" {
			errs = append(errs, "question ID must not be empty")
			break
		}
	}
	if a.Author.Role == "" || !dialogueTokenRE.MatchString(a.Author.Role) {
		errs = append(errs, "author role must be a conservative token")
	}
	if a.Statement == "" {
		errs = append(errs, "statement is required")
	}
	if len(a.Classifications) == 0 {
		errs = append(errs, "classifications is required")
	}
	for _, cls := range a.Classifications {
		if !isAnswerClassification(cls) {
			errs = append(errs, "unknown answer classification")
			break
		}
	}
	if _, err := time.Parse(time.RFC3339, a.RecordedAt); err != nil {
		errs = append(errs, "recorded_at must be RFC3339")
	}
	if !isAnswerGovernanceStatus(a.GovernanceStatus) {
		errs = append(errs, "unknown governance status")
	}
	if err := validateClaimScopePaths(a.Scope); err != nil {
		errs = append(errs, err.Error())
	}
	for _, ref := range a.EvidenceRefs {
		class, _, ok := ParseClassQualifiedReference(ref)
		if !ok || class != "evidence" {
			errs = append(errs, "evidence_refs must be class-qualified as evidence:<id>")
			break
		}
	}
	if a.ID != "" && a.SupersededByAnswer == a.ID {
		errs = append(errs, "answer must not supersede itself")
	}
	for _, sel := range a.SelectedHypotheses {
		if sel.QuestionID == "" || sel.HypothesisID == "" {
			errs = append(errs, "hypothesis selection requires question_id and hypothesis_id")
			break
		}
	}
	if err := validateArchitectAnswerShape(a); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func canonicalizeOpenQuestion(in OpenQuestion) OpenQuestion {
	q := in
	q.ID = strings.TrimSpace(q.ID)
	q.Label = strings.TrimSpace(q.Label)
	q.QuestionText = strings.TrimSpace(q.QuestionText)
	q.Scope = canonicalizeClaimScope(q.Scope)
	q.BlocksClosureDimension = strings.TrimSpace(q.BlocksClosureDimension)
	q.BlocksClaims = cleanStringList(q.BlocksClaims, false)
	q.BlocksNodes = normalizeClassRefs(q.BlocksNodes)
	q.BlocksClosureBlockers = cleanStringList(q.BlocksClosureBlockers, false)
	q.QuestionTemplateID = strings.TrimSpace(q.QuestionTemplateID)
	q.QuestionTemplateVersion = strings.TrimSpace(q.QuestionTemplateVersion)
	q.SourceClosureAssessmentDigestSHA256 = strings.TrimSpace(q.SourceClosureAssessmentDigestSHA256)
	q.QuestionSourceKind = QuestionSourceKind(strings.TrimSpace(string(q.QuestionSourceKind)))
	q.SourceArtifactDigestSHA256 = strings.TrimSpace(q.SourceArtifactDigestSHA256)
	q.SourceReferenceIDs = cleanStringList(q.SourceReferenceIDs, false)
	q.AcceptedAnswerTypes = cleanStringList(q.AcceptedAnswerTypes, false)
	q.ReasonsOpen = cleanStringList(q.ReasonsOpen, false)
	q.KnownFactIDs = cleanStringList(q.KnownFactIDs, false)
	q.KnownEvidence = normalizeClassRefs(q.KnownEvidence)
	q.SupportingEvidence = normalizeClassRefs(q.SupportingEvidence)
	q.RefutingEvidence = normalizeClassRefs(q.RefutingEvidence)
	q.FalsificationConditions = cleanStringList(q.FalsificationConditions, false)
	q.SuggestedAnswerOwner = strings.TrimSpace(q.SuggestedAnswerOwner)
	q.CompetingHypotheses = canonicalizeQuestionHypotheses(q.CompetingHypotheses)
	q.MissingEvidence = cleanStringList(q.MissingEvidence, false)
	q.Priority = strings.TrimSpace(q.Priority)
	q.RiskIfUnresolved = strings.TrimSpace(q.RiskIfUnresolved)
	q.Status = strings.TrimSpace(q.Status)
	q.ResolvedByAnswers = cleanStringList(q.ResolvedByAnswers, false)
	q.SupersededByQuestion = strings.TrimSpace(q.SupersededByQuestion)
	q.CreatedAt = strings.TrimSpace(q.CreatedAt)
	q.LastReviewedAt = strings.TrimSpace(q.LastReviewedAt)
	return q
}

func canonicalizeArchitectAnswer(in ArchitectAnswer) ArchitectAnswer {
	a := in
	a.ID = strings.TrimSpace(a.ID)
	a.Label = strings.TrimSpace(a.Label)
	a.AnswersQuestions = cleanStringList(a.AnswersQuestions, false)
	a.Author.Role = strings.TrimSpace(a.Author.Role)
	a.Author.ID = strings.TrimSpace(a.Author.ID)
	a.Statement = strings.TrimSpace(a.Statement)
	a.Classifications = cleanStringList(a.Classifications, false)
	a.Scope = canonicalizeClaimScope(a.Scope)
	a.Conditions = cleanStringList(a.Conditions, false)
	a.EvidenceRefs = normalizeClassRefs(a.EvidenceRefs)
	a.EvidencePointers = cleanStringList(a.EvidencePointers, false)
	a.SelectedHypotheses = canonicalizeHypothesisSelections(a.SelectedHypotheses)
	a.ReframedQuestion = strings.TrimSpace(a.ReframedQuestion)
	a.RecordedAt = strings.TrimSpace(a.RecordedAt)
	a.GovernanceStatus = strings.TrimSpace(a.GovernanceStatus)
	a.SupersededByAnswer = strings.TrimSpace(a.SupersededByAnswer)
	a.MissingEvidenceNotes = cleanStringList(a.MissingEvidenceNotes, false)
	return a
}

func canonicalizeClaimScope(in ClaimScope) ClaimScope {
	s := in
	s.Repository = strings.TrimSpace(s.Repository)
	s.Repo = strings.TrimSpace(s.Repo)
	if s.Repository == "" {
		s.Repository = s.Repo
	}
	if s.Repo == "" {
		s.Repo = s.Repository
	}
	s.Domain = strings.TrimSpace(s.Domain)
	s.SourceSet = strings.TrimSpace(s.SourceSet)
	s.Files = cleanStringList(s.Files, true)
	s.Symbols = cleanStringList(s.Symbols, false)
	s.Components = cleanStringList(s.Components, false)
	return s
}

func canonicalizeQuestionHypotheses(in []QuestionHypothesis) []QuestionHypothesis {
	out := make([]QuestionHypothesis, 0, len(in))
	for _, h := range in {
		h.ID = strings.TrimSpace(h.ID)
		h.Statement = strings.TrimSpace(h.Statement)
		if h.ID != "" || h.Statement != "" {
			out = append(out, h)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func canonicalizeHypothesisSelections(in []HypothesisSelection) []HypothesisSelection {
	seen := map[HypothesisSelection]bool{}
	for _, sel := range in {
		sel.QuestionID = strings.TrimSpace(sel.QuestionID)
		sel.HypothesisID = strings.TrimSpace(sel.HypothesisID)
		if sel.QuestionID != "" || sel.HypothesisID != "" {
			seen[sel] = true
		}
	}
	out := make([]HypothesisSelection, 0, len(seen))
	for sel := range seen {
		out = append(out, sel)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].QuestionID != out[j].QuestionID {
			return out[i].QuestionID < out[j].QuestionID
		}
		return out[i].HypothesisID < out[j].HypothesisID
	})
	return out
}

func validateOpenQuestionStatusShape(q OpenQuestion) error {
	switch q.Status {
	case QuestionStatusOpen:
		if len(q.ResolvedByAnswers) > 0 || q.SupersededByQuestion != "" {
			return errors.New("open question must not list resolution or supersession")
		}
	case QuestionStatusAwaitingArchitect:
		if !q.ArchitectRequired {
			return errors.New("awaiting_architect requires architect_required=true")
		}
		if len(q.ResolvedByAnswers) > 0 {
			return errors.New("awaiting_architect question must not list resolution answers")
		}
	case QuestionStatusAwaitingEvidence:
		if len(q.MissingEvidence) == 0 {
			return errors.New("awaiting_evidence requires missing_evidence")
		}
		if len(q.ResolvedByAnswers) > 0 {
			return errors.New("awaiting_evidence question must not list resolution answers")
		}
	case QuestionStatusAnswered:
	case QuestionStatusResolved, QuestionStatusAcceptedUnknown:
		if len(q.ResolvedByAnswers) == 0 {
			return errors.New(q.Status + " question requires resolved_by_answers")
		}
	case QuestionStatusSuperseded:
		if q.SupersededByQuestion == "" {
			return errors.New("superseded question requires superseded_by_question")
		}
	default:
		return errors.New("unknown question status")
	}
	return nil
}

func validateArchitectAnswerShape(a ArchitectAnswer) error {
	if contains(a.Classifications, AnswerTypeUnknownAcknowledgement) {
		if len(a.Classifications) != 1 {
			return errors.New("unknown_acknowledgement must be the only classification")
		}
		if len(a.SelectedHypotheses) > 0 {
			return errors.New("unknown_acknowledgement must not select a hypothesis")
		}
	}
	if contains(a.Classifications, AnswerTypeQuestionReframing) && a.ReframedQuestion == "" {
		return errors.New("question_reframing requires reframed_question")
	}
	if contains(a.Classifications, AnswerTypeExceptionAuthorization) && len(a.Conditions) == 0 {
		return errors.New("exception_authorization requires at least one condition")
	}
	if contains(a.Classifications, AnswerTypeEvidencePointer) && len(a.EvidenceRefs)+len(a.EvidencePointers) == 0 {
		return errors.New("evidence_pointer requires evidence_refs or evidence_pointers")
	}
	if contains(a.Classifications, AnswerTypeScopeClarification) && !claimScopeExplicit(a.Scope) {
		return errors.New("scope_clarification requires explicit scope")
	}
	switch a.GovernanceStatus {
	case AnswerGovernanceRecorded, AnswerGovernanceRejected:
	case AnswerGovernanceAwaitingEvidence:
		if len(a.EvidencePointers)+len(a.MissingEvidenceNotes) == 0 {
			return errors.New("awaiting_evidence requires unresolved evidence pointer or missing evidence")
		}
	case AnswerGovernanceAwaitingGovernance:
		if !hasAnyClassification(a, AnswerTypeIntentStatement, AnswerTypeDesiredDirection, AnswerTypeGovernedDecisionCandidate, AnswerTypeExceptionAuthorization) {
			return errors.New("awaiting_governance requires a governance classification")
		}
	case AnswerGovernanceAcceptedForQuestion:
	case AnswerGovernanceSuperseded:
		if a.SupersededByAnswer == "" {
			return errors.New("superseded answer requires superseded_by_answer")
		}
	default:
		return errors.New("unknown governance status")
	}
	return nil
}

func validateClaimScopePaths(s ClaimScope) error {
	for _, f := range s.Files {
		if filepath.IsAbs(f) || strings.HasPrefix(f, "../") || strings.Contains(f, "/../") || f == ".." {
			return errors.New("file path must be repository-relative and non-escaping")
		}
	}
	return nil
}

func isAnswerClassification(v string) bool {
	return oneOf(v,
		AnswerTypeIntentStatement,
		AnswerTypeDesiredDirection,
		AnswerTypeGovernedDecisionCandidate,
		AnswerTypeHistoricalContext,
		AnswerTypeScopeClarification,
		AnswerTypeExceptionAuthorization,
		AnswerTypeEvidencePointer,
		AnswerTypeUnknownAcknowledgement,
		AnswerTypeQuestionReframing,
	)
}

func isAnswerGovernanceStatus(v string) bool {
	return oneOf(v,
		AnswerGovernanceRecorded,
		AnswerGovernanceAwaitingEvidence,
		AnswerGovernanceAwaitingGovernance,
		AnswerGovernanceAcceptedForQuestion,
		AnswerGovernanceRejected,
		AnswerGovernanceSuperseded,
	)
}

func hasAnyClassification(a ArchitectAnswer, want ...string) bool {
	for _, w := range want {
		if contains(a.Classifications, w) {
			return true
		}
	}
	return false
}

func claimScopeExplicit(s ClaimScope) bool {
	return s.Repository != "" || s.Repo != "" || s.Domain != "" || len(s.Files)+len(s.Symbols)+len(s.Components) > 0
}

func openQuestionsEqual(a, b OpenQuestion) bool {
	aj, _ := json.Marshal(canonicalizeOpenQuestion(a))
	bj, _ := json.Marshal(canonicalizeOpenQuestion(b))
	return string(aj) == string(bj)
}

func architectAnswersEqual(a, b ArchitectAnswer) bool {
	aj, _ := json.Marshal(canonicalizeArchitectAnswer(a))
	bj, _ := json.Marshal(canonicalizeArchitectAnswer(b))
	return string(aj) == string(bj)
}

func shortDialogueHash(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}
