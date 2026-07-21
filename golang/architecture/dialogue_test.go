// SPDX-License-Identifier: AGPL-3.0-only

package architecture

import (
	"strings"
	"testing"
)

func sampleOpenQuestion() OpenQuestion {
	return OpenQuestion{
		ID:                     "question.config_writer",
		Label:                  "Config writer",
		QuestionText:           "Who is intended to write config state?",
		Scope:                  ClaimScope{Repository: "github.com/example/project", Domain: "repo", Files: []string{"golang/config.go"}, Components: []string{"component.config"}},
		BlocksClosureDimension: ClosureAuthority,
		BlocksClaims:           []string{"claim.config_writer"},
		AcceptedAnswerTypes:    []string{AnswerTypeIntentStatement, AnswerTypeUnknownAcknowledgement, AnswerTypeQuestionReframing},
		ReasonsOpen:            []string{"Two writers are observed."},
		KnownFactIDs:           []string{"fact.config.writer"},
		KnownEvidence:          []string{"evidence:evidence.config.writer"},
		CompetingHypotheses: []QuestionHypothesis{
			{ID: "hypothesis.owner_a", Statement: "Component A owns the state."},
			{ID: "hypothesis.owner_b", Statement: "Component B owns the state."},
		},
		MissingEvidence:   []string{"A governed decision."},
		Priority:          QuestionPriorityHigh,
		RiskIfUnresolved:  "Agents may preserve an authority split.",
		ArchitectRequired: true,
		Status:            QuestionStatusAwaitingArchitect,
		CreatedAt:         "2026-07-13T12:00:00Z",
	}
}

func sampleArchitectAnswer() ArchitectAnswer {
	return ArchitectAnswer{
		ID:               "answer.config_writer",
		AnswersQuestions: []string{"question.config_writer"},
		Author:           AnswerAuthor{Role: "project_architect", ID: "architect.local"},
		Statement:        "Component A is the intended writer.\nComponent B is temporary.",
		Classifications:  []string{AnswerTypeIntentStatement},
		Scope:            ClaimScope{Repository: "github.com/example/project", Domain: "repo", Files: []string{"golang/config.go"}, Components: []string{"component.config"}},
		RecordedAt:       "2026-07-13T12:15:00Z",
		GovernanceStatus: AnswerGovernanceRecorded,
	}
}

func TestOpenQuestionIDIsDeterministic(t *testing.T) {
	q := sampleOpenQuestion()
	q.ID = ""
	if StableOpenQuestionID(q) != StableOpenQuestionID(q) {
		t.Fatal("question ID is not deterministic")
	}
}

func TestOpenQuestionIDIgnoresStatus(t *testing.T) {
	q := sampleOpenQuestion()
	q.ID = ""
	a, b := q, q
	a.Status = QuestionStatusOpen
	b.Status = QuestionStatusAwaitingEvidence
	if StableOpenQuestionID(a) != StableOpenQuestionID(b) {
		t.Fatal("question ID changed with status")
	}
}

func TestOpenQuestionIDIgnoresPriority(t *testing.T) {
	q := sampleOpenQuestion()
	q.ID = ""
	a, b := q, q
	a.Priority = QuestionPriorityCritical
	b.Priority = QuestionPriorityLow
	if StableOpenQuestionID(a) != StableOpenQuestionID(b) {
		t.Fatal("question ID changed with priority")
	}
}

func TestOpenQuestionIDIgnoresTimestamps(t *testing.T) {
	q := sampleOpenQuestion()
	q.ID = ""
	a, b := q, q
	a.CreatedAt = "2026-07-13T12:00:00Z"
	b.CreatedAt = "2027-07-13T12:00:00Z"
	b.LastReviewedAt = "2027-07-14T12:00:00Z"
	if StableOpenQuestionID(a) != StableOpenQuestionID(b) {
		t.Fatal("question ID changed with timestamps")
	}
}

func TestNormalizeOpenQuestionsSortsAndDeduplicatesLists(t *testing.T) {
	q := sampleOpenQuestion()
	q.BlocksClaims = []string{"claim.b", "claim.a", "claim.a"}
	q.Scope.Files = []string{"b\\file.go", "a/file.go", "a/file.go"}
	q.AcceptedAnswerTypes = []string{AnswerTypeUnknownAcknowledgement, AnswerTypeIntentStatement, AnswerTypeIntentStatement}
	got, err := NormalizeOpenQuestions([]OpenQuestion{q})
	if err != nil {
		t.Fatalf("NormalizeOpenQuestions: %v", err)
	}
	if strings.Join(got[0].BlocksClaims, ",") != "claim.a,claim.b" {
		t.Fatalf("blocks_claims=%v", got[0].BlocksClaims)
	}
	if strings.Join(got[0].Scope.Files, ",") != "a/file.go,b/file.go" {
		t.Fatalf("files=%v", got[0].Scope.Files)
	}
}

func TestNormalizeOpenQuestionsDeduplicatesIdenticalQuestion(t *testing.T) {
	q := sampleOpenQuestion()
	got, err := NormalizeOpenQuestions([]OpenQuestion{q, q})
	if err != nil {
		t.Fatalf("NormalizeOpenQuestions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d, want 1", len(got))
	}
}

func TestNormalizeOpenQuestionsRejectsIDDivergence(t *testing.T) {
	a, b := sampleOpenQuestion(), sampleOpenQuestion()
	b.QuestionText = "A different question?"
	if _, err := NormalizeOpenQuestions([]OpenQuestion{a, b}); err == nil {
		t.Fatal("expected ID collision error")
	}
}

func TestValidateOpenQuestionRejectsMissingText(t *testing.T) {
	q := sampleOpenQuestion()
	q.QuestionText = ""
	assertQuestionInvalid(t, q)
}

func TestValidateOpenQuestionRejectsUnknownClosureDimension(t *testing.T) {
	q := sampleOpenQuestion()
	q.BlocksClosureDimension = "other"
	assertQuestionInvalid(t, q)
}

func TestValidateOpenQuestionRejectsMissingGrounding(t *testing.T) {
	q := sampleOpenQuestion()
	q.BlocksClaims = nil
	assertQuestionInvalid(t, q)
}

func TestValidateOpenQuestionAcceptsNodeGrounding(t *testing.T) {
	q := sampleOpenQuestion()
	q.BlocksClaims = nil
	q.BlocksNodes = []string{"component:component.config"}
	if err := ValidateOpenQuestion(q); err != nil {
		t.Fatalf("ValidateOpenQuestion: %v", err)
	}
}

func TestValidateOpenQuestionRejectsMalformedNodeGrounding(t *testing.T) {
	q := sampleOpenQuestion()
	q.BlocksClaims = nil
	q.BlocksNodes = []string{"component.config"}
	assertQuestionInvalid(t, q)
}

func TestValidateOpenQuestionAcceptsClosureBlockerGrounding(t *testing.T) {
	q := sampleOpenQuestion()
	q.BlocksClaims = nil
	q.BlocksClosureBlockers = []string{"blocker.authority.abcdef012345"}
	if err := ValidateOpenQuestion(q); err != nil {
		t.Fatalf("ValidateOpenQuestion: %v", err)
	}
}

func TestGeneratedOpenQuestionRequiresCompleteMetadata(t *testing.T) {
	q := sampleOpenQuestion()
	q.BlocksClosureBlockers = []string{"blocker.authority.abcdef012345"}
	q.QuestionTemplateID = "question.authority_definition.v1"
	assertQuestionInvalid(t, q)
}

func TestGeneratedOpenQuestionRejectsDigestInIDInput(t *testing.T) {
	q := sampleOpenQuestion()
	q.ID = ""
	q.BlocksClosureBlockers = []string{"blocker.authority.abcdef012345"}
	q.QuestionTemplateID = "question.authority_definition.v1"
	q.QuestionTemplateVersion = "v1"
	q.SourceClosureAssessmentDigestSHA256 = strings.Repeat("a", 64)
	a, b := q, q
	a.SourceClosureAssessmentDigestSHA256 = strings.Repeat("a", 64)
	b.SourceClosureAssessmentDigestSHA256 = strings.Repeat("b", 64)
	if StableOpenQuestionID(a) != StableOpenQuestionID(b) {
		t.Fatal("question ID changed with source closure assessment digest")
	}
}

func TestValidateOpenQuestionRejectsUnknownAnswerType(t *testing.T) {
	q := sampleOpenQuestion()
	q.AcceptedAnswerTypes = []string{"guess"}
	assertQuestionInvalid(t, q)
}

func TestValidateOpenQuestionRejectsSingleLeadingHypothesis(t *testing.T) {
	q := sampleOpenQuestion()
	q.CompetingHypotheses = q.CompetingHypotheses[:1]
	assertQuestionInvalid(t, q)
}

func TestValidateOpenQuestionRejectsDuplicateHypothesisID(t *testing.T) {
	q := sampleOpenQuestion()
	q.CompetingHypotheses[1].ID = q.CompetingHypotheses[0].ID
	assertQuestionInvalid(t, q)
}

func TestValidateOpenQuestionRejectsUnknownPriority(t *testing.T) {
	q := sampleOpenQuestion()
	q.Priority = "later"
	assertQuestionInvalid(t, q)
}

func TestValidateOpenQuestionRejectsEscapingPath(t *testing.T) {
	q := sampleOpenQuestion()
	q.Scope.Files = []string{"../config.go"}
	assertQuestionInvalid(t, q)
}

func TestAwaitingArchitectRequiresArchitect(t *testing.T) {
	q := sampleOpenQuestion()
	q.ArchitectRequired = false
	assertQuestionInvalid(t, q)
}

func TestAwaitingEvidenceRequiresMissingEvidence(t *testing.T) {
	q := sampleOpenQuestion()
	q.Status = QuestionStatusAwaitingEvidence
	q.MissingEvidence = nil
	assertQuestionInvalid(t, q)
}

func TestSupersededQuestionRequiresReplacement(t *testing.T) {
	q := sampleOpenQuestion()
	q.Status = QuestionStatusSuperseded
	q.SupersededByQuestion = ""
	assertQuestionInvalid(t, q)
}

func TestArchitectAnswerIDIsDeterministic(t *testing.T) {
	a := sampleArchitectAnswer()
	a.ID = ""
	if StableArchitectAnswerID(a) != StableArchitectAnswerID(a) {
		t.Fatal("answer ID is not deterministic")
	}
}

func TestArchitectAnswerIDIgnoresGovernanceStatus(t *testing.T) {
	a := sampleArchitectAnswer()
	a.ID = ""
	b := a
	a.GovernanceStatus = AnswerGovernanceRecorded
	b.GovernanceStatus = AnswerGovernanceRejected
	if StableArchitectAnswerID(a) != StableArchitectAnswerID(b) {
		t.Fatal("answer ID changed with governance status")
	}
}

func TestArchitectAnswerPreservesExactStatement(t *testing.T) {
	a := sampleArchitectAnswer()
	a.Statement = "  First line.\n\nSecond line.  "
	got, err := NormalizeArchitectAnswers([]ArchitectAnswer{a})
	if err != nil {
		t.Fatalf("NormalizeArchitectAnswers: %v", err)
	}
	if got[0].Statement != "First line.\n\nSecond line." {
		t.Fatalf("statement=%q", got[0].Statement)
	}
}

func TestNormalizeArchitectAnswersSortsAndDeduplicatesLists(t *testing.T) {
	a := sampleArchitectAnswer()
	a.AnswersQuestions = []string{"question.b", "question.a", "question.a"}
	a.Classifications = []string{AnswerTypeHistoricalContext, AnswerTypeIntentStatement, AnswerTypeIntentStatement}
	got, err := NormalizeArchitectAnswers([]ArchitectAnswer{a})
	if err != nil {
		t.Fatalf("NormalizeArchitectAnswers: %v", err)
	}
	if strings.Join(got[0].AnswersQuestions, ",") != "question.a,question.b" {
		t.Fatalf("answers_questions=%v", got[0].AnswersQuestions)
	}
}

func TestNormalizeArchitectAnswersDeduplicatesIdenticalAnswer(t *testing.T) {
	a := sampleArchitectAnswer()
	got, err := NormalizeArchitectAnswers([]ArchitectAnswer{a, a})
	if err != nil {
		t.Fatalf("NormalizeArchitectAnswers: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d, want 1", len(got))
	}
}

func TestNormalizeArchitectAnswersRejectsIDDivergence(t *testing.T) {
	a, b := sampleArchitectAnswer(), sampleArchitectAnswer()
	b.Statement = "Different."
	if _, err := NormalizeArchitectAnswers([]ArchitectAnswer{a, b}); err == nil {
		t.Fatal("expected ID collision error")
	}
}

func TestValidateArchitectAnswerRejectsMissingQuestion(t *testing.T) {
	a := sampleArchitectAnswer()
	a.AnswersQuestions = nil
	assertAnswerInvalid(t, a)
}

func TestValidateArchitectAnswerRejectsMissingAuthorRole(t *testing.T) {
	a := sampleArchitectAnswer()
	a.Author.Role = ""
	assertAnswerInvalid(t, a)
}

func TestValidateArchitectAnswerRejectsMissingStatement(t *testing.T) {
	a := sampleArchitectAnswer()
	a.Statement = ""
	assertAnswerInvalid(t, a)
}

func TestValidateArchitectAnswerRejectsUnknownClassification(t *testing.T) {
	a := sampleArchitectAnswer()
	a.Classifications = []string{"truth"}
	assertAnswerInvalid(t, a)
}

func TestValidateArchitectAnswerRejectsUnknownGovernanceStatus(t *testing.T) {
	a := sampleArchitectAnswer()
	a.GovernanceStatus = "promoted"
	assertAnswerInvalid(t, a)
}

func TestUnknownAcknowledgementMustBeExclusive(t *testing.T) {
	a := sampleArchitectAnswer()
	a.Classifications = []string{AnswerTypeUnknownAcknowledgement, AnswerTypeIntentStatement}
	assertAnswerInvalid(t, a)
}

func TestQuestionReframingRequiresReplacementText(t *testing.T) {
	a := sampleArchitectAnswer()
	a.Classifications = []string{AnswerTypeQuestionReframing}
	assertAnswerInvalid(t, a)
}

func TestExceptionAuthorizationRequiresCondition(t *testing.T) {
	a := sampleArchitectAnswer()
	a.Classifications = []string{AnswerTypeExceptionAuthorization}
	assertAnswerInvalid(t, a)
}

func TestEvidencePointerClassificationRequiresPointer(t *testing.T) {
	a := sampleArchitectAnswer()
	a.Classifications = []string{AnswerTypeEvidencePointer}
	assertAnswerInvalid(t, a)
}

func TestAwaitingGovernanceRequiresGovernanceClassification(t *testing.T) {
	a := sampleArchitectAnswer()
	a.GovernanceStatus = AnswerGovernanceAwaitingGovernance
	a.Classifications = []string{AnswerTypeHistoricalContext}
	assertAnswerInvalid(t, a)
}

func TestSupersededAnswerRequiresReplacement(t *testing.T) {
	a := sampleArchitectAnswer()
	a.GovernanceStatus = AnswerGovernanceSuperseded
	assertAnswerInvalid(t, a)
}

func TestArchitectAnswerRejectsEscapingPath(t *testing.T) {
	a := sampleArchitectAnswer()
	a.Scope.Files = []string{"../config.go"}
	assertAnswerInvalid(t, a)
}

func assertQuestionInvalid(t *testing.T, q OpenQuestion) {
	t.Helper()
	if err := ValidateOpenQuestion(canonicalizeOpenQuestion(q)); err == nil {
		t.Fatal("expected question validation error")
	}
}

func assertAnswerInvalid(t *testing.T, a ArchitectAnswer) {
	t.Helper()
	if err := ValidateArchitectAnswer(canonicalizeArchitectAnswer(a)); err == nil {
		t.Fatal("expected answer validation error")
	}
}
