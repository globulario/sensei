// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
)

func TestRecordAnswerOutputIsDeterministic(t *testing.T) {
	path := writeDialogueFixture(t)
	opts := recordAnswerOptions{
		Dialogue: path, QuestionID: "question.config_writer", Statement: "Component config owns config state.",
		Classification: repeatFlag{architecture.AnswerTypeIntentStatement}, AuthorRole: "project_architect",
		RecordedAt: "2026-07-13T12:30:00Z", GovernanceStatus: architecture.AnswerGovernanceRecorded, Format: "yaml",
	}
	a, _, _, err := buildRecordAnswerOutput(opts)
	if err != nil {
		t.Fatalf("buildRecordAnswerOutput: %v", err)
	}
	b, _, _, err := buildRecordAnswerOutput(opts)
	if err != nil {
		t.Fatalf("buildRecordAnswerOutput: %v", err)
	}
	if string(a) != string(b) {
		t.Fatal("record-answer output is not deterministic")
	}
}

func TestRecordAnswerCheckDetectsStaleOutput(t *testing.T) {
	path := writeDialogueFixture(t)
	out := filepath.Join(t.TempDir(), "docs", "awareness", "candidates", "dialogue.yaml")
	opts := recordAnswerOptions{
		Dialogue: path, QuestionID: "question.config_writer", Statement: "Component config owns config state.",
		Classification: repeatFlag{architecture.AnswerTypeIntentStatement}, AuthorRole: "project_architect",
		RecordedAt: "2026-07-13T12:30:00Z", GovernanceStatus: architecture.AnswerGovernanceRecorded, Format: "yaml",
		Output: out,
	}
	got, _, _, err := buildRecordAnswerOutput(opts)
	if err != nil {
		t.Fatalf("buildRecordAnswerOutput: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, got, 0o644); err != nil {
		t.Fatal(err)
	}
	opts.Check = true
	if checkBytesDiffer(out, got) {
		t.Fatal("expected fresh check bytes")
	}
	if err := os.WriteFile(out, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !checkBytesDiffer(out, got) {
		t.Fatal("expected stale check bytes")
	}
}

func TestAdjudicateAnswerAcceptsRecordedAnswer(t *testing.T) {
	path := writeDialogueFixture(t)
	recorded, _, _, err := buildRecordAnswerOutput(recordAnswerOptions{
		Dialogue: path, QuestionID: "question.config_writer", Statement: "Component config owns config state.",
		Classification: repeatFlag{architecture.AnswerTypeIntentStatement}, AuthorRole: "project_architect",
		RecordedAt: "2026-07-13T12:30:00Z", GovernanceStatus: architecture.AnswerGovernanceRecorded, Format: "yaml",
	})
	if err != nil {
		t.Fatalf("buildRecordAnswerOutput: %v", err)
	}
	dialoguePath := filepath.Join(t.TempDir(), "dialogue.yaml")
	if err := os.WriteFile(dialoguePath, recorded, 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := architecture.LoadDialogueDocument(dialoguePath)
	if err != nil {
		t.Fatal(err)
	}
	updated, _, report, err := buildAdjudicateAnswerOutput(adjudicateAnswerOptions{
		Dialogue: dialoguePath, AnswerID: doc.Answers[0].ID, Status: architecture.AnswerGovernanceAcceptedForQuestion, Format: "yaml",
	})
	if err != nil {
		t.Fatalf("buildAdjudicateAnswerOutput: %v", err)
	}
	if !strings.Contains(string(updated), "status: resolved") {
		t.Fatalf("updated dialogue did not resolve question:\n%s", updated)
	}
	if report.GovernanceStatus != architecture.AnswerGovernanceAcceptedForQuestion {
		t.Fatalf("report=%+v", report)
	}
}

func TestDialogueCommandsRejectActiveAwarenessOutputPath(t *testing.T) {
	if err := ensureCandidateOutputs(filepath.Join("docs", "awareness", "architecture_dialogue.yaml")); err == nil {
		t.Fatal("expected protected output path error")
	}
}

func writeDialogueFixture(t *testing.T) string {
	t.Helper()
	binding := architecture.ClaimDocumentBinding{
		RepositoryDomain: "github.com/example/project", Revision: "0123456789abcdef", RevisionStatus: architecture.RevisionResolved,
		GraphDigestSHA256: strings.Repeat("a", 64), GraphDigestStatus: architecture.GraphDigestResolved,
	}
	q := architecture.OpenQuestion{
		ID: "question.config_writer", QuestionText: "Who is intended to write config state?",
		Scope:                  architecture.ClaimScope{Repository: "github.com/example/project", Domain: "repo", Files: []string{"config.go"}},
		BlocksClosureDimension: architecture.ClosureAuthority, BlocksClaims: []string{"claim.config_writer"},
		AcceptedAnswerTypes: []string{architecture.AnswerTypeIntentStatement, architecture.AnswerTypeUnknownAcknowledgement},
		ReasonsOpen:         []string{"Two writers observed."}, Priority: architecture.QuestionPriorityHigh,
		RiskIfUnresolved: "Authority split persists.", ArchitectRequired: true,
		Status: architecture.QuestionStatusAwaitingArchitect, CreatedAt: "2026-07-13T12:00:00Z",
	}
	data, err := architecture.MarshalCanonicalDialogueDocumentYAML(architecture.DialogueDocument{
		SchemaVersion: "1", CompiledBy: "test", Binding: binding, OpenQuestions: []architecture.OpenQuestion{q},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "dialogue.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
