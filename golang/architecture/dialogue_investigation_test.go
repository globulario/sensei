// SPDX-License-Identifier: AGPL-3.0-only

package architecture

import (
	"strings"
	"testing"
)

func TestInvestigationQuestionSourceBindingIsValidWithoutClosureBlocker(t *testing.T) {
	question := investigationBoundQuestionFixture()
	normalized, err := NormalizeOpenQuestions([]OpenQuestion{question})
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized) != 1 || normalized[0].QuestionSourceKind != SourceEvidenceGap {
		t.Fatalf("investigation provenance was not preserved: %+v", normalized)
	}
}

func TestInvestigationQuestionSourceBindingFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*OpenQuestion)
		want   string
	}{
		{
			name: "unknown source kind",
			mutate: func(question *OpenQuestion) {
				question.QuestionSourceKind = "invented"
			},
			want: "unknown question_source_kind",
		},
		{
			name: "missing source digest",
			mutate: func(question *OpenQuestion) {
				question.SourceArtifactDigestSHA256 = ""
			},
			want: "source_artifact_digest_sha256",
		},
		{
			name: "missing source references",
			mutate: func(question *OpenQuestion) {
				question.SourceReferenceIDs = nil
			},
			want: "source_reference_ids",
		},
		{
			name: "mixed closure and investigation source",
			mutate: func(question *OpenQuestion) {
				question.SourceClosureAssessmentDigestSHA256 = strings.Repeat("b", 64)
				question.BlocksClosureBlockers = []string{"blocker.evidence.0123456789ab"}
			},
			want: "mutually exclusive",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			question := investigationBoundQuestionFixture()
			test.mutate(&question)
			err := ValidateOpenQuestion(question)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q refusal, got %v", test.want, err)
			}
		})
	}
}

func TestInvestigationQuestionIdentityBindsExactSourceArtifact(t *testing.T) {
	first := investigationBoundQuestionFixture()
	second := investigationBoundQuestionFixture()
	second.SourceArtifactDigestSHA256 = strings.Repeat("c", 64)
	if StableOpenQuestionID(first) == StableOpenQuestionID(second) {
		t.Fatal("investigation question identity ignored its exact source artifact")
	}
}

func investigationBoundQuestionFixture() OpenQuestion {
	question := OpenQuestion{
		QuestionText:               "Which evidence would establish or refute this candidate?",
		Scope:                      ClaimScope{Repository: "example/repo"},
		BlocksClosureDimension:     ClosureEvidence,
		BlocksClaims:               []string{"claim.candidate.one"},
		BlocksNodes:                []string{"architecture_claim:claim.candidate.one"},
		QuestionTemplateID:         "question.evidence_request.v1",
		QuestionTemplateVersion:    "v1",
		QuestionSourceKind:         SourceEvidenceGap,
		SourceArtifactDigestSHA256: strings.Repeat("a", 64),
		SourceReferenceIDs:         []string{"candidate_one", "evidence_request_one"},
		SupportingEvidence:         []string{"evidence:supporting_one"},
		RefutingEvidence:           []string{"evidence:refuting_one"},
		FalsificationConditions:    []string{"a bound counterexample contradicts the proposition"},
		SuggestedAnswerOwner:       "evidence provider for design_documents",
		AcceptedAnswerTypes:        []string{AnswerTypeEvidencePointer, AnswerTypeUnknownAcknowledgement},
		ReasonsOpen:                []string{"supporting_evidence_missing"},
		Priority:                   QuestionPriorityMedium,
		RiskIfUnresolved:           "The candidate cannot be promoted safely.",
		Status:                     QuestionStatusAwaitingEvidence,
		CreatedAt:                  "2026-07-22T12:00:00Z",
	}
	question.ID = StableOpenQuestionID(question)
	return question
}
