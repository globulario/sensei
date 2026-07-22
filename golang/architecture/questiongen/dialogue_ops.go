// SPDX-License-Identifier: AGPL-3.0-only

package questiongen

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"gopkg.in/yaml.v3"
)

const (
	AnswerRecordingBy    = "sensei record-answer"
	AnswerAdjudicationBy = "sensei adjudicate-answer"
)

type RecordAnswerOptions struct {
	QuestionID           string
	Statement            string
	Classifications      []string
	AuthorRole           string
	AuthorID             string
	RecordedAt           string
	GovernanceStatus     string
	Scope                architecture.ClaimScope
	Conditions           []string
	EvidenceRefs         []string
	EvidencePointers     []string
	SelectedHypotheses   []architecture.HypothesisSelection
	ReframedQuestion     string
	RequiresEvidence     bool
	MissingEvidenceNotes []string
}

type AdjudicateAnswerOptions struct {
	AnswerID                  string
	Status                    string
	AdjudicatedAt             string
	ReplacementQuestionText   string
	ReplacementQuestionStatus string
	SupersededByAnswer        string
}

type AnswerRecordingReport struct {
	SchemaVersion    string                    `json:"schema_version" yaml:"schema_version"`
	GeneratedBy      string                    `json:"generated_by" yaml:"generated_by"`
	QuestionID       string                    `json:"question_id" yaml:"question_id"`
	AnswerID         string                    `json:"answer_id" yaml:"answer_id"`
	GovernanceStatus string                    `json:"governance_status" yaml:"governance_status"`
	QuestionStatus   string                    `json:"question_status" yaml:"question_status"`
	Classifications  []string                  `json:"classifications" yaml:"classifications"`
	Limitations      []architecture.Limitation `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type AnswerAdjudicationReport struct {
	SchemaVersion         string                    `json:"schema_version" yaml:"schema_version"`
	GeneratedBy           string                    `json:"generated_by" yaml:"generated_by"`
	QuestionID            string                    `json:"question_id,omitempty" yaml:"question_id,omitempty"`
	AnswerID              string                    `json:"answer_id" yaml:"answer_id"`
	GovernanceStatus      string                    `json:"governance_status" yaml:"governance_status"`
	QuestionStatus        string                    `json:"question_status,omitempty" yaml:"question_status,omitempty"`
	SupersedingQuestionID string                    `json:"superseding_question_id,omitempty" yaml:"superseding_question_id,omitempty"`
	SupersedingAnswerID   string                    `json:"superseding_answer_id,omitempty" yaml:"superseding_answer_id,omitempty"`
	Limitations           []architecture.Limitation `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type answerRecordingEnvelope struct {
	ArchitectureAnswerRecording AnswerRecordingReport `json:"architecture_answer_recording" yaml:"architecture_answer_recording"`
}

type answerAdjudicationEnvelope struct {
	ArchitectureAnswerAdjudication AnswerAdjudicationReport `json:"architecture_answer_adjudication" yaml:"architecture_answer_adjudication"`
}

func RecordAnswer(doc architecture.DialogueDocument, opts RecordAnswerOptions) (architecture.DialogueDocument, AnswerRecordingReport, error) {
	opts = normalizeRecordAnswerOptions(opts)
	if opts.QuestionID == "" {
		return architecture.DialogueDocument{}, AnswerRecordingReport{}, errors.New("question id is required")
	}
	if opts.Statement == "" {
		return architecture.DialogueDocument{}, AnswerRecordingReport{}, errors.New("statement is required")
	}
	if len(opts.Classifications) == 0 {
		return architecture.DialogueDocument{}, AnswerRecordingReport{}, errors.New("at least one explicit classification is required")
	}
	if opts.RecordedAt == "" {
		return architecture.DialogueDocument{}, AnswerRecordingReport{}, errors.New("recorded_at is required")
	}
	if opts.AuthorRole == "" {
		return architecture.DialogueDocument{}, AnswerRecordingReport{}, errors.New("author role is required")
	}
	if opts.GovernanceStatus == "" {
		opts.GovernanceStatus = architecture.AnswerGovernanceRecorded
	}
	if !oneOf(opts.GovernanceStatus, architecture.AnswerGovernanceRecorded, architecture.AnswerGovernanceAwaitingEvidence, architecture.AnswerGovernanceAwaitingGovernance) {
		return architecture.DialogueDocument{}, AnswerRecordingReport{}, errors.New("record-answer may only create recorded, awaiting_evidence, or awaiting_governance answers")
	}
	qi := questionIndex(doc.OpenQuestions, opts.QuestionID)
	if qi < 0 {
		return architecture.DialogueDocument{}, AnswerRecordingReport{}, fmt.Errorf("question %s not found", opts.QuestionID)
	}
	q := doc.OpenQuestions[qi]
	if !oneOf(q.Status, architecture.QuestionStatusOpen, architecture.QuestionStatusAwaitingArchitect, architecture.QuestionStatusAwaitingEvidence, architecture.QuestionStatusAnswered) {
		return architecture.DialogueDocument{}, AnswerRecordingReport{}, fmt.Errorf("question %s status %s cannot record a new answer", q.ID, q.Status)
	}
	if !classificationsAcceptedByQuestion(opts.Classifications, q.AcceptedAnswerTypes) {
		return architecture.DialogueDocument{}, AnswerRecordingReport{}, fmt.Errorf("classification is not accepted by question %s", q.ID)
	}
	scope := opts.Scope
	if !claimScopeExplicit(scope) {
		scope = q.Scope
	}
	answer := architecture.ArchitectAnswer{
		AnswersQuestions:     []string{q.ID},
		Author:               architecture.AnswerAuthor{Role: opts.AuthorRole, ID: opts.AuthorID},
		Statement:            opts.Statement,
		Classifications:      opts.Classifications,
		Scope:                scope,
		Conditions:           opts.Conditions,
		EvidenceRefs:         opts.EvidenceRefs,
		EvidencePointers:     opts.EvidencePointers,
		SelectedHypotheses:   opts.SelectedHypotheses,
		ReframedQuestion:     opts.ReframedQuestion,
		RecordedAt:           opts.RecordedAt,
		GovernanceStatus:     opts.GovernanceStatus,
		RequiresEvidence:     opts.RequiresEvidence,
		MissingEvidenceNotes: opts.MissingEvidenceNotes,
	}
	answer.ID = architecture.StableArchitectAnswerID(answer)
	if existing := answerIndex(doc.Answers, answer.ID); existing >= 0 {
		if !architectAnswersEquivalent(doc.Answers[existing], answer) {
			return architecture.DialogueDocument{}, AnswerRecordingReport{}, fmt.Errorf("architect answer id collision for %s", answer.ID)
		}
	} else {
		doc.Answers = append(doc.Answers, answer)
	}
	if q.Status != architecture.QuestionStatusAnswered {
		q.Status = architecture.QuestionStatusAnswered
		q.ResolvedByAnswers = nil
		doc.OpenQuestions[qi] = q
	}
	normalized, err := architecture.NormalizeDialogueDocument(doc)
	if err != nil {
		return architecture.DialogueDocument{}, AnswerRecordingReport{}, err
	}
	report := AnswerRecordingReport{
		SchemaVersion: "1", GeneratedBy: AnswerRecordingBy, QuestionID: q.ID, AnswerID: answer.ID,
		GovernanceStatus: answer.GovernanceStatus, QuestionStatus: architecture.QuestionStatusAnswered, Classifications: answer.Classifications,
	}
	return normalized, normalizeAnswerRecordingReport(report), nil
}

func AdjudicateAnswer(doc architecture.DialogueDocument, opts AdjudicateAnswerOptions) (architecture.DialogueDocument, AnswerAdjudicationReport, error) {
	opts.AnswerID = strings.TrimSpace(opts.AnswerID)
	opts.Status = strings.TrimSpace(opts.Status)
	opts.AdjudicatedAt = strings.TrimSpace(opts.AdjudicatedAt)
	opts.ReplacementQuestionText = strings.TrimSpace(opts.ReplacementQuestionText)
	opts.ReplacementQuestionStatus = strings.TrimSpace(opts.ReplacementQuestionStatus)
	opts.SupersededByAnswer = strings.TrimSpace(opts.SupersededByAnswer)
	if opts.AnswerID == "" {
		return architecture.DialogueDocument{}, AnswerAdjudicationReport{}, errors.New("answer id is required")
	}
	if opts.Status == "" {
		return architecture.DialogueDocument{}, AnswerAdjudicationReport{}, errors.New("adjudication status is required")
	}
	ai := answerIndex(doc.Answers, opts.AnswerID)
	if ai < 0 {
		return architecture.DialogueDocument{}, AnswerAdjudicationReport{}, fmt.Errorf("answer %s not found", opts.AnswerID)
	}
	answer := doc.Answers[ai]
	qid := ""
	if len(answer.AnswersQuestions) > 0 {
		qid = answer.AnswersQuestions[0]
	}
	qi := questionIndex(doc.OpenQuestions, qid)
	if qi < 0 {
		return architecture.DialogueDocument{}, AnswerAdjudicationReport{}, fmt.Errorf("answer %s references missing question %s", answer.ID, qid)
	}
	q := doc.OpenQuestions[qi]
	report := AnswerAdjudicationReport{SchemaVersion: "1", GeneratedBy: AnswerAdjudicationBy, AnswerID: answer.ID, QuestionID: q.ID}
	switch opts.Status {
	case architecture.AnswerGovernanceAcceptedForQuestion:
		answer.GovernanceStatus = architecture.AnswerGovernanceAcceptedForQuestion
		if len(answer.Classifications) == 1 && answer.Classifications[0] == architecture.AnswerTypeQuestionReframing {
			if opts.ReplacementQuestionText == "" || opts.AdjudicatedAt == "" {
				return architecture.DialogueDocument{}, AnswerAdjudicationReport{}, errors.New("question_reframing acceptance requires replacement question text and adjudicated_at")
			}
			replacementStatus := opts.ReplacementQuestionStatus
			if replacementStatus == "" {
				replacementStatus = replacementStatusFor(q)
			}
			replacement := q
			replacement.ID = ""
			replacement.QuestionText = opts.ReplacementQuestionText
			replacement.Status = replacementStatus
			replacement.ResolvedByAnswers = nil
			replacement.SupersededByQuestion = ""
			replacement.CreatedAt = opts.AdjudicatedAt
			replacement.LastReviewedAt = ""
			replacement.QuestionTemplateID = ""
			replacement.QuestionTemplateVersion = ""
			replacement.SourceClosureAssessmentDigestSHA256 = ""
			replacement.QuestionSourceKind = ""
			replacement.SourceArtifactDigestSHA256 = ""
			replacement.SourceReferenceIDs = nil
			replacement.ID = architecture.StableOpenQuestionID(replacement)
			if existing := questionIndex(doc.OpenQuestions, replacement.ID); existing < 0 {
				doc.OpenQuestions = append(doc.OpenQuestions, replacement)
			} else if !openQuestionsEquivalent(doc.OpenQuestions[existing], replacement) {
				return architecture.DialogueDocument{}, AnswerAdjudicationReport{}, fmt.Errorf("replacement question id collision for %s", replacement.ID)
			}
			q.Status = architecture.QuestionStatusSuperseded
			q.SupersededByQuestion = replacement.ID
			q.ResolvedByAnswers = []string{answer.ID}
			report.SupersedingQuestionID = replacement.ID
		} else {
			q.ResolvedByAnswers = appendUnique(q.ResolvedByAnswers, answer.ID)
			if len(answer.Classifications) == 1 && answer.Classifications[0] == architecture.AnswerTypeUnknownAcknowledgement {
				q.Status = architecture.QuestionStatusAcceptedUnknown
			} else {
				q.Status = architecture.QuestionStatusResolved
			}
		}
	case architecture.AnswerGovernanceRejected:
		answer.GovernanceStatus = architecture.AnswerGovernanceRejected
	case architecture.AnswerGovernanceAwaitingEvidence:
		answer.GovernanceStatus = architecture.AnswerGovernanceAwaitingEvidence
	case architecture.AnswerGovernanceAwaitingGovernance:
		answer.GovernanceStatus = architecture.AnswerGovernanceAwaitingGovernance
	case architecture.AnswerGovernanceSuperseded:
		if opts.SupersededByAnswer == "" {
			return architecture.DialogueDocument{}, AnswerAdjudicationReport{}, errors.New("superseded adjudication requires superseded_by_answer")
		}
		answer.GovernanceStatus = architecture.AnswerGovernanceSuperseded
		answer.SupersededByAnswer = opts.SupersededByAnswer
		report.SupersedingAnswerID = opts.SupersededByAnswer
	default:
		return architecture.DialogueDocument{}, AnswerAdjudicationReport{}, fmt.Errorf("unsupported adjudication status %s", opts.Status)
	}
	doc.Answers[ai] = answer
	doc.OpenQuestions[qi] = q
	normalized, err := architecture.NormalizeDialogueDocument(doc)
	if err != nil {
		return architecture.DialogueDocument{}, AnswerAdjudicationReport{}, err
	}
	report.GovernanceStatus = answer.GovernanceStatus
	report.QuestionStatus = normalized.OpenQuestions[questionIndex(normalized.OpenQuestions, q.ID)].Status
	return normalized, normalizeAnswerAdjudicationReport(report), nil
}

func MarshalAnswerRecordingReportYAML(report AnswerRecordingReport) ([]byte, error) {
	return yaml.Marshal(answerRecordingEnvelope{ArchitectureAnswerRecording: normalizeAnswerRecordingReport(report)})
}

func MarshalAnswerRecordingReportJSON(report AnswerRecordingReport) ([]byte, error) {
	return marshalJSON(answerRecordingEnvelope{ArchitectureAnswerRecording: normalizeAnswerRecordingReport(report)})
}

func MarshalAnswerAdjudicationReportYAML(report AnswerAdjudicationReport) ([]byte, error) {
	return yaml.Marshal(answerAdjudicationEnvelope{ArchitectureAnswerAdjudication: normalizeAnswerAdjudicationReport(report)})
}

func MarshalAnswerAdjudicationReportJSON(report AnswerAdjudicationReport) ([]byte, error) {
	return marshalJSON(answerAdjudicationEnvelope{ArchitectureAnswerAdjudication: normalizeAnswerAdjudicationReport(report)})
}

func normalizeRecordAnswerOptions(in RecordAnswerOptions) RecordAnswerOptions {
	in.QuestionID = strings.TrimSpace(in.QuestionID)
	in.Statement = strings.TrimSpace(in.Statement)
	in.Classifications = cleanStrings(in.Classifications)
	in.AuthorRole = strings.TrimSpace(in.AuthorRole)
	in.AuthorID = strings.TrimSpace(in.AuthorID)
	in.RecordedAt = strings.TrimSpace(in.RecordedAt)
	in.GovernanceStatus = strings.TrimSpace(in.GovernanceStatus)
	in.Conditions = cleanStrings(in.Conditions)
	in.EvidenceRefs = cleanStrings(in.EvidenceRefs)
	in.EvidencePointers = cleanStrings(in.EvidencePointers)
	in.ReframedQuestion = strings.TrimSpace(in.ReframedQuestion)
	in.MissingEvidenceNotes = cleanStrings(in.MissingEvidenceNotes)
	return in
}

func normalizeAnswerRecordingReport(in AnswerRecordingReport) AnswerRecordingReport {
	in.SchemaVersion = "1"
	if in.GeneratedBy == "" {
		in.GeneratedBy = AnswerRecordingBy
	}
	in.Classifications = cleanStrings(in.Classifications)
	return in
}

func normalizeAnswerAdjudicationReport(in AnswerAdjudicationReport) AnswerAdjudicationReport {
	in.SchemaVersion = "1"
	if in.GeneratedBy == "" {
		in.GeneratedBy = AnswerAdjudicationBy
	}
	return in
}

func replacementStatusFor(q architecture.OpenQuestion) string {
	if q.ArchitectRequired {
		return architecture.QuestionStatusAwaitingArchitect
	}
	if len(q.MissingEvidence) > 0 {
		return architecture.QuestionStatusAwaitingEvidence
	}
	return architecture.QuestionStatusOpen
}

func questionIndex(questions []architecture.OpenQuestion, id string) int {
	for i := range questions {
		if questions[i].ID == id {
			return i
		}
	}
	return -1
}

func answerIndex(answers []architecture.ArchitectAnswer, id string) int {
	for i := range answers {
		if answers[i].ID == id {
			return i
		}
	}
	return -1
}

func classificationsAcceptedByQuestion(classes, accepted []string) bool {
	want := map[string]bool{}
	for _, acceptedClass := range accepted {
		want[acceptedClass] = true
	}
	for _, class := range classes {
		if !want[class] {
			return false
		}
	}
	return true
}

func claimScopeExplicit(s architecture.ClaimScope) bool {
	return s.Repository != "" || s.Repo != "" || s.Domain != "" || s.SourceSet != "" ||
		len(s.Files)+len(s.Symbols)+len(s.Components) > 0
}

func appendUnique(in []string, value string) []string {
	if value == "" {
		return cleanStrings(in)
	}
	in = append(in, value)
	return cleanStrings(in)
}

func architectAnswersEquivalent(a, b architecture.ArchitectAnswer) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return bytes.Equal(aj, bj)
}

func openQuestionsEquivalent(a, b architecture.OpenQuestion) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return bytes.Equal(aj, bj)
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func marshalJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func stableHypothesisSelections(questionID string, hypotheses []string) []architecture.HypothesisSelection {
	hypotheses = cleanStrings(hypotheses)
	out := make([]architecture.HypothesisSelection, 0, len(hypotheses))
	for _, id := range hypotheses {
		out = append(out, architecture.HypothesisSelection{QuestionID: questionID, HypothesisID: id})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].HypothesisID < out[j].HypothesisID })
	return out
}
