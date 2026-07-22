// SPDX-License-Identifier: AGPL-3.0-only

package questiongen

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/deviation"
)

const deviationQuestionFixtureTime = "2026-07-22T15:00:00Z"

func TestGenerateFromDeviationRoutesOnlyRepeatedPatterns(t *testing.T) {
	analysis := deviationQuestionAnalysis(t, 2)
	generated, err := GenerateFromDeviation(DeviationQuestionContext{
		Analysis:  analysis,
		CreatedAt: deviationQuestionFixtureTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(generated.Dialogue.OpenQuestions) != 1 || len(generated.Report.Generated) != 1 {
		t.Fatalf("expected one repeated-deviation question: %+v", generated)
	}
	question := generated.Dialogue.OpenQuestions[0]
	if question.QuestionSourceKind != architecture.SourceDeviationPattern || question.QuestionTemplateID != TemplateRepeatedDeviation {
		t.Fatalf("question did not use governed repeated-deviation source and template: %+v", question)
	}
	if question.SourceArtifactDigestSHA256 != analysis.Receipt.ExactAnalysisDigestSHA256 {
		t.Fatal("question lost exact deviation analysis binding")
	}
	if !question.ArchitectRequired || question.Status != architecture.QuestionStatusAwaitingArchitect {
		t.Fatal("repeated deviation must await governed architect review")
	}
	if len(question.CompetingHypotheses) != 3 || len(question.FalsificationConditions) == 0 || len(question.SupportingEvidence) == 0 {
		t.Fatalf("question omitted competing explanations, evidence, or falsification: %+v", question)
	}
}

func TestGenerateFromDeviationOneOccurrenceRemainsEvidenceOnly(t *testing.T) {
	analysis := deviationQuestionAnalysis(t, 1)
	generated, err := GenerateFromDeviation(DeviationQuestionContext{
		Analysis:  analysis,
		CreatedAt: deviationQuestionFixtureTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(generated.Dialogue.OpenQuestions) != 0 || len(generated.Report.Generated) != 0 {
		t.Fatalf("one local deviation must not create a governed question: %+v", generated)
	}
}

func TestGenerateFromDeviationIsDeterministicAndDeduplicates(t *testing.T) {
	analysis := deviationQuestionAnalysis(t, 2)
	first, err := GenerateFromDeviation(DeviationQuestionContext{Analysis: analysis, CreatedAt: deviationQuestionFixtureTime})
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateFromDeviation(DeviationQuestionContext{
		Analysis:  analysis,
		Existing:  &first.Dialogue,
		CreatedAt: "2026-07-22T16:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Dialogue, second.Dialogue) {
		t.Fatal("repeated generation changed canonical dialogue lifecycle state")
	}
	if len(second.Report.ExistingCoverage) != 1 || len(second.Report.Generated) != 0 {
		t.Fatalf("repeat was not accounted as existing coverage: %+v", second.Report)
	}
}

func TestDeviationQuestionAnswerPathDoesNotPromoteOrWeakenCandidate(t *testing.T) {
	analysis := deviationQuestionAnalysis(t, 2)
	before := analysis.Candidates[0].Claim
	generated, err := GenerateFromDeviation(DeviationQuestionContext{Analysis: analysis, CreatedAt: deviationQuestionFixtureTime})
	if err != nil {
		t.Fatal(err)
	}
	question := generated.Dialogue.OpenQuestions[0]
	doc, recording, err := RecordAnswer(generated.Dialogue, RecordAnswerOptions{
		QuestionID:       question.ID,
		Statement:        "The architecture remains valid; implementations used an ungoverned shortcut and require a stronger refusal gate.",
		Classifications:  []string{architecture.AnswerTypeGovernedDecisionCandidate},
		AuthorRole:       "architect",
		AuthorID:         "fixture",
		RecordedAt:       deviationQuestionFixtureTime,
		GovernanceStatus: architecture.AnswerGovernanceRecorded,
	})
	if err != nil {
		t.Fatal(err)
	}
	doc, _, err = AdjudicateAnswer(doc, AdjudicateAnswerOptions{
		AnswerID:      recording.AnswerID,
		Status:        architecture.AnswerGovernanceAcceptedForQuestion,
		AdjudicatedAt: deviationQuestionFixtureTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	if doc.OpenQuestions[questionIndex(doc.OpenQuestions, question.ID)].Status != architecture.QuestionStatusResolved {
		t.Fatal("canonical answer path did not resolve deviation question")
	}
	after := analysis.Candidates[0].Claim
	if !reflect.DeepEqual(before, after) || after.PromotionStatus != architecture.PromotionCandidate || after.Confidence != 0 {
		t.Fatal("answering a deviation question promoted or weakened its candidate")
	}
}

func TestGenerateFromDeviationRefusesStaleAnalysisReceipt(t *testing.T) {
	analysis := deviationQuestionAnalysis(t, 2)
	analysis.Patterns[0].Shape.Object = "changed.after.receipt"
	_, err := GenerateFromDeviation(DeviationQuestionContext{Analysis: analysis, CreatedAt: deviationQuestionFixtureTime})
	if err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("stale deviation analysis must fail closed, got %v", err)
	}
}

func deviationQuestionAnalysis(t *testing.T, occurrences int) deviation.Analysis {
	t.Helper()
	binding := architecture.ClaimDocumentBinding{
		RepositoryDomain:  "example/repo",
		Revision:          "abc123",
		RevisionStatus:    architecture.RevisionResolved,
		TreeDigestSHA256:  deviationQuestionDigest("tree"),
		GraphDigestSHA256: deviationQuestionDigest("graph"),
		GraphDigestStatus: architecture.GraphDigestResolved,
	}
	receipts := make([]deviation.Receipt, 0, occurrences)
	for i := 0; i < occurrences; i++ {
		label := string(rune('a' + i))
		receipt, err := deviation.Record(deviation.RecordInput{
			Kind:    deviation.KindBypassedOwnerPath,
			Binding: binding,
			Scope: architecture.ClaimScope{
				Repository: binding.RepositoryDomain,
				Files:      []string{"api.go", "store.go"},
				Components: []string{"component.api", "component.store"},
			},
			Shape:         deviation.Shape{Subject: "component.api", Predicate: "bypassed_owner_path", Object: "component.store"},
			Expected:      "mutate through the governed owner path",
			Observed:      "implementation used a non-owner write path",
			TaskID:        "task." + label,
			TaskSessionID: "session." + label,
			AgentID:       "codex." + label,
			ChangeDigest:  deviationQuestionDigest("change." + label),
			SourceDigest:  deviationQuestionDigest("source." + label),
			EvidenceRefs:  []string{"evidence:deviation_" + label},
			RecordedAt:    "2026-07-22T1" + string(rune('2'+i)) + ":00:00Z",
			Timestamp:     "fixture",
		})
		if err != nil {
			t.Fatal(err)
		}
		receipts = append(receipts, receipt)
	}
	analysis, err := deviation.Analyze(binding, receipts, 2)
	if err != nil {
		t.Fatal(err)
	}
	return analysis
}

func deviationQuestionDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
