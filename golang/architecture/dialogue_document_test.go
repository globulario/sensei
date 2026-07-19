// SPDX-License-Identifier: Apache-2.0

package architecture

import (
	"bytes"
	"testing"
)

func sampleDialogueDocument() DialogueDocument {
	q := sampleOpenQuestion()
	q.Status = QuestionStatusResolved
	q.ResolvedByAnswers = []string{"answer.config_writer"}
	a := sampleArchitectAnswer()
	a.GovernanceStatus = AnswerGovernanceAcceptedForQuestion
	return DialogueDocument{
		SchemaVersion: "1",
		CompiledBy:    "test",
		Binding: ClaimDocumentBinding{
			RepositoryDomain:  "github.com/example/project",
			Revision:          "0123456789abcdef",
			RevisionStatus:    RevisionResolved,
			GraphDigestSHA256: "abcdef0123456789",
			GraphDigestStatus: GraphDigestResolved,
		},
		OpenQuestions: []OpenQuestion{q},
		Answers:       []ArchitectAnswer{a},
	}
}

func TestDialogueDocumentRequiresExplicitRevisionStatus(t *testing.T) {
	doc := sampleDialogueDocument()
	doc.Binding.RevisionStatus = ""
	assertDialogueInvalid(t, doc)
}

func TestDialogueDocumentRequiresExplicitGraphDigestStatus(t *testing.T) {
	doc := sampleDialogueDocument()
	doc.Binding.GraphDigestStatus = ""
	assertDialogueInvalid(t, doc)
}

func TestDialogueDocumentRejectsMissingQuestionReference(t *testing.T) {
	doc := sampleDialogueDocument()
	doc.Answers[0].AnswersQuestions = []string{"question.missing"}
	assertDialogueInvalid(t, doc)
}

func TestDialogueDocumentRejectsMissingResolutionAnswer(t *testing.T) {
	doc := sampleDialogueDocument()
	doc.OpenQuestions[0].ResolvedByAnswers = []string{"answer.missing"}
	assertDialogueInvalid(t, doc)
}

func TestDialogueDocumentRejectsUnacceptedAnswerClassification(t *testing.T) {
	doc := sampleDialogueDocument()
	doc.Answers[0].Classifications = []string{AnswerTypeEvidencePointer}
	doc.Answers[0].EvidencePointers = []string{"docs/evidence.md"}
	assertDialogueInvalid(t, doc)
}

func TestDialogueDocumentRejectsUnknownHypothesisSelection(t *testing.T) {
	doc := sampleDialogueDocument()
	doc.Answers[0].SelectedHypotheses = []HypothesisSelection{{QuestionID: "question.config_writer", HypothesisID: "hypothesis.missing"}}
	assertDialogueInvalid(t, doc)
}

func TestDialogueDocumentRejectsQuestionSupersessionCycle(t *testing.T) {
	doc := sampleDialogueDocument()
	q1 := sampleOpenQuestion()
	q1.ID = "question.one"
	q1.Status = QuestionStatusSuperseded
	q1.SupersededByQuestion = "question.two"
	q2 := sampleOpenQuestion()
	q2.ID = "question.two"
	q2.Status = QuestionStatusSuperseded
	q2.SupersededByQuestion = "question.one"
	doc.OpenQuestions = []OpenQuestion{q1, q2}
	doc.Answers = nil
	assertDialogueInvalid(t, doc)
}

func TestDialogueDocumentRejectsAnswerSupersessionCycle(t *testing.T) {
	doc := sampleDialogueDocument()
	q := sampleOpenQuestion()
	q.Status = QuestionStatusAnswered
	a1 := sampleArchitectAnswer()
	a1.ID = "answer.one"
	a1.GovernanceStatus = AnswerGovernanceSuperseded
	a1.SupersededByAnswer = "answer.two"
	a2 := sampleArchitectAnswer()
	a2.ID = "answer.two"
	a2.GovernanceStatus = AnswerGovernanceSuperseded
	a2.SupersededByAnswer = "answer.one"
	doc.OpenQuestions = []OpenQuestion{q}
	doc.Answers = []ArchitectAnswer{a1, a2}
	assertDialogueInvalid(t, doc)
}

func TestDialogueDocumentRejectsResolvedQuestionWithoutAcceptedAnswer(t *testing.T) {
	doc := sampleDialogueDocument()
	doc.Answers[0].GovernanceStatus = AnswerGovernanceRecorded
	assertDialogueInvalid(t, doc)
}

func TestDialogueDocumentRejectsAcceptedAnswerNotUsedForResolution(t *testing.T) {
	doc := sampleDialogueDocument()
	doc.OpenQuestions[0].Status = QuestionStatusAnswered
	doc.OpenQuestions[0].ResolvedByAnswers = nil
	assertDialogueInvalid(t, doc)
}

func TestDialogueDocumentRejectsRejectedResolutionAnswer(t *testing.T) {
	doc := sampleDialogueDocument()
	doc.Answers[0].GovernanceStatus = AnswerGovernanceRejected
	assertDialogueInvalid(t, doc)
}

func TestAcceptedUnknownRequiresUnknownAcknowledgement(t *testing.T) {
	doc := sampleDialogueDocument()
	doc.OpenQuestions[0].Status = QuestionStatusAcceptedUnknown
	assertDialogueInvalid(t, doc)
	doc.Answers[0].Classifications = []string{AnswerTypeUnknownAcknowledgement}
	doc.OpenQuestions[0].AcceptedAnswerTypes = []string{AnswerTypeUnknownAcknowledgement}
	if _, err := NormalizeDialogueDocument(doc); err != nil {
		t.Fatalf("NormalizeDialogueDocument accepted_unknown: %v", err)
	}
}

func TestUnboundDialogueCannotResolveQuestion(t *testing.T) {
	doc := sampleDialogueDocument()
	doc.Binding.Revision = ""
	doc.Binding.RevisionStatus = RevisionNotGit
	assertDialogueInvalid(t, doc)
}

func TestAnswerScopeCannotCrossRepositoryBinding(t *testing.T) {
	doc := sampleDialogueDocument()
	doc.Answers[0].Scope.Repository = "github.com/other/project"
	assertDialogueInvalid(t, doc)
}

func TestDialogueDocumentOutputIsDeterministic(t *testing.T) {
	doc := sampleDialogueDocument()
	a, err := MarshalCanonicalDialogueDocument(doc)
	if err != nil {
		t.Fatalf("MarshalCanonicalDialogueDocument: %v", err)
	}
	b, err := MarshalCanonicalDialogueDocument(doc)
	if err != nil {
		t.Fatalf("MarshalCanonicalDialogueDocument second: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("canonical output differed\n%s\n---\n%s", a, b)
	}
}

func assertDialogueInvalid(t *testing.T, doc DialogueDocument) {
	t.Helper()
	if _, err := NormalizeDialogueDocument(doc); err == nil {
		t.Fatal("expected dialogue validation error")
	}
}
