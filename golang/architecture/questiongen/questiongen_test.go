// SPDX-License-Identifier: Apache-2.0

package questiongen

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
)

func questionGenContext() Context {
	binding := architecture.ClaimDocumentBinding{
		RepositoryDomain:  "github.com/example/project",
		Revision:          "0123456789abcdef",
		RevisionStatus:    architecture.RevisionResolved,
		GraphDigestSHA256: strings.Repeat("a", 64),
		GraphDigestStatus: architecture.GraphDigestResolved,
	}
	return Context{
		Closure: closure.Report{
			SchemaVersion: closure.SchemaVersion,
			Request: closure.Request{
				Binding: binding,
				Scope: closure.Scope{
					Domain: "repo", TaskClass: "edit_config", RiskClass: closure.RiskArchitectureSensitive,
					AccessMode: closure.AccessWrite, DirectionRequirement: closure.DirectionPreserve,
				},
			},
			ScopeReceipt: closure.ScopeReceipt{Files: []string{"config.go"}},
			Blockers: []closure.Blocker{{
				ID: "blocker.authority.abcdef012345", Dimension: closure.DimensionAuthority, Severity: architecture.QuestionPriorityHigh,
				Code: "closure.authority.owner_missing", Summary: "Config owner is not defined.",
				NodeIDs: []string{"component.config"}, Files: []string{"config.go"}, RequiredNextAction: "define_authority",
			}},
		},
		Claims: architecture.ClaimDocument{SchemaVersion: "1", Binding: binding},
		Graph: closure.GraphIndex{Nodes: map[string]closure.Node{
			"component.config": {ID: "component.config", Classes: []string{"component"}, SourcePath: "config.go"},
		}},
		CreatedAt:                     "2026-07-13T12:00:00Z",
		ClosureAssessmentDigestSHA256: strings.Repeat("b", 64),
	}
}

func TestGenerateQuestionsCreatesGroundedOpenQuestion(t *testing.T) {
	result, err := Generate(questionGenContext(), nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(result.Dialogue.OpenQuestions) != 1 {
		t.Fatalf("questions=%d", len(result.Dialogue.OpenQuestions))
	}
	q := result.Dialogue.OpenQuestions[0]
	if q.QuestionTemplateID != "question.authority_definition.v1" || q.QuestionTemplateVersion != "v1" {
		t.Fatalf("template=%s/%s", q.QuestionTemplateID, q.QuestionTemplateVersion)
	}
	if len(q.BlocksClosureBlockers) != 1 || q.BlocksClosureBlockers[0] != "blocker.authority.abcdef012345" {
		t.Fatalf("blockers=%v", q.BlocksClosureBlockers)
	}
	if len(q.BlocksNodes) != 1 || q.BlocksNodes[0] != "component:component.config" {
		t.Fatalf("nodes=%v", q.BlocksNodes)
	}
	if q.SourceClosureAssessmentDigestSHA256 != strings.Repeat("b", 64) {
		t.Fatalf("digest=%s", q.SourceClosureAssessmentDigestSHA256)
	}
}

func TestHypothesesUseDistinctIDsAcrossClaims(t *testing.T) {
	claims := []architecture.Claim{
		{ID: "claim.one", AlternativeExplanations: []string{"shared plumbing", "migration path"}},
		{ID: "claim.two", AlternativeExplanations: []string{"shared plumbing", "migration path"}},
	}
	got := hypotheses("question.behavior.v1", closure.Blocker{}, claims)
	if len(got) != 4 {
		t.Fatalf("hypotheses=%#v", got)
	}
	seen := map[string]bool{}
	for _, hypothesis := range got {
		if seen[hypothesis.ID] {
			t.Fatalf("duplicate hypothesis ID %s", hypothesis.ID)
		}
		seen[hypothesis.ID] = true
	}
	if first := hypotheses("question.behavior.v1", closure.Blocker{}, claims); len(first) != len(got) || first[0].ID != got[0].ID {
		t.Fatalf("hypothesis identities are not deterministic: %#v != %#v", first, got)
	}
}

func TestEvidenceQuestionNamesExactClaimProposition(t *testing.T) {
	ctx := questionGenContext()
	ctx.Claims.Claims = []architecture.Claim{{
		ID: "claim.route", Statement: architecture.ClaimStatement{
			Subject: "gin.Engine.HandleContext", Predicate: "shares_entrypoint_behavior_with", Object: "gin.Engine.ServeHTTP",
		},
	}}
	blocker := closure.Blocker{ClaimIDs: []string{"claim.route"}}

	got := evidenceQuestionText(ctx, blocker)
	want := "Which current test, runtime observation, or source evidence establishes or refutes this proposition: gin.Engine.HandleContext shares_entrypoint_behavior_with gin.Engine.ServeHTTP?"
	if got != want {
		t.Fatalf("question text = %q, want %q", got, want)
	}
}

func TestGenerateQuestionsSkipsMechanicalBlocker(t *testing.T) {
	ctx := questionGenContext()
	ctx.Closure.Blockers[0].ID = "blocker.agent.abcdef012345"
	ctx.Closure.Blockers[0].Dimension = closure.DimensionAgent
	ctx.Closure.Blockers[0].Code = "closure.agent.task_class_missing"
	ctx.Closure.Blockers[0].RequiredNextAction = "reassess_scope"
	result, err := Generate(ctx, nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(result.Dialogue.OpenQuestions) != 0 {
		t.Fatalf("questions=%d", len(result.Dialogue.OpenQuestions))
	}
	if len(result.Report.Skipped) != 1 || result.Report.Skipped[0].Disposition != DispositionSkippedMechanical {
		t.Fatalf("skipped=%+v", result.Report.Skipped)
	}
}

func TestGenerateQuestionsExistingQuestionCoversSameNode(t *testing.T) {
	ctx := questionGenContext()
	existing := architecture.OpenQuestion{
		ID: "question.existing", QuestionText: "Who owns config state?",
		Scope:                  architecture.ClaimScope{Repository: "github.com/example/project", Domain: "repo", Files: []string{"config.go"}},
		BlocksClosureDimension: architecture.ClosureAuthority,
		BlocksNodes:            []string{"component:component.config"},
		AcceptedAnswerTypes:    []string{architecture.AnswerTypeIntentStatement},
		ReasonsOpen:            []string{"Owner is missing."},
		Priority:               architecture.QuestionPriorityHigh,
		RiskIfUnresolved:       "Authority remains unclear.",
		ArchitectRequired:      true,
		Status:                 architecture.QuestionStatusAwaitingArchitect,
		CreatedAt:              "2026-07-13T11:00:00Z",
	}
	doc := architecture.DialogueDocument{
		SchemaVersion: "1", CompiledBy: "test", Binding: ctx.Closure.Request.Binding, OpenQuestions: []architecture.OpenQuestion{existing},
	}
	normalized, err := architecture.NormalizeDialogueDocument(doc)
	if err != nil {
		t.Fatalf("NormalizeDialogueDocument: %v", err)
	}
	ctx.Existing = &normalized
	result, err := Generate(ctx, nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(result.Dialogue.OpenQuestions) != 1 {
		t.Fatalf("questions=%d", len(result.Dialogue.OpenQuestions))
	}
	if len(result.Report.ExistingCoverage) != 1 || result.Report.ExistingCoverage[0].QuestionID != "question.existing" {
		t.Fatalf("existing coverage=%+v", result.Report.ExistingCoverage)
	}
}

func TestRecordAnswerMarksQuestionAnsweredOnly(t *testing.T) {
	result, err := Generate(questionGenContext(), nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	doc, report, err := RecordAnswer(result.Dialogue, RecordAnswerOptions{
		QuestionID: result.Dialogue.OpenQuestions[0].ID,
		Statement:  "Component config owns config state.",
		Classifications: []string{
			architecture.AnswerTypeIntentStatement,
		},
		AuthorRole: "project_architect",
		RecordedAt: "2026-07-13T12:30:00Z",
	})
	if err != nil {
		t.Fatalf("RecordAnswer: %v", err)
	}
	if doc.OpenQuestions[0].Status != architecture.QuestionStatusAnswered || len(doc.OpenQuestions[0].ResolvedByAnswers) != 0 {
		t.Fatalf("question=%+v", doc.OpenQuestions[0])
	}
	if len(doc.Answers) != 1 || doc.Answers[0].GovernanceStatus != architecture.AnswerGovernanceRecorded {
		t.Fatalf("answers=%+v", doc.Answers)
	}
	if report.AnswerID == "" {
		t.Fatal("missing answer report ID")
	}
}

func TestAdjudicateUnknownAnswerSetsAcceptedUnknown(t *testing.T) {
	ctx := questionGenContext()
	result, err := Generate(ctx, nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	doc, _, err := RecordAnswer(result.Dialogue, RecordAnswerOptions{
		QuestionID:      result.Dialogue.OpenQuestions[0].ID,
		Statement:       "The intended owner is explicitly unknown.",
		Classifications: []string{architecture.AnswerTypeUnknownAcknowledgement},
		AuthorRole:      "project_architect",
		RecordedAt:      "2026-07-13T12:30:00Z",
	})
	if err != nil {
		t.Fatalf("RecordAnswer: %v", err)
	}
	doc, report, err := AdjudicateAnswer(doc, AdjudicateAnswerOptions{AnswerID: doc.Answers[0].ID, Status: architecture.AnswerGovernanceAcceptedForQuestion})
	if err != nil {
		t.Fatalf("AdjudicateAnswer: %v", err)
	}
	if doc.OpenQuestions[0].Status != architecture.QuestionStatusAcceptedUnknown {
		t.Fatalf("question status=%s", doc.OpenQuestions[0].Status)
	}
	if report.GovernanceStatus != architecture.AnswerGovernanceAcceptedForQuestion {
		t.Fatalf("report=%+v", report)
	}
}

func TestStableDigestUsesSHA256(t *testing.T) {
	if got := StableDigest([]byte("closure")); len(got) != 64 {
		t.Fatalf("digest len=%d value=%s", len(got), got)
	}
}
