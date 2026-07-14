// SPDX-License-Identifier: Apache-2.0

package extractor_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/rdf"
)

const validDialogueYAML = `
architecture_dialogue:
  schema_version: "1"
  compiled_by: "test"
  binding:
    repository_domain: github.com/example/project
    revision: 0123456789abcdef
    revision_status: resolved
    graph_digest_sha256: abcdef0123456789
    graph_digest_status: resolved
  open_questions:
    - id: question.config_writer
      label: Config writer
      question_text: Who is intended to write config state?
      scope:
        repository: github.com/example/project
        domain: repo
        files: [golang/config.go]
        symbols: [symbol.SaveConfig]
        components: [component.config]
      blocks_closure_dimension: authority
      blocks_claims: [claim.config_writer]
      blocks_nodes: [component:component.config]
      blocks_closure_blockers: [blocker.authority.abcdef012345]
      question_template_id: question.authority_definition.v1
      question_template_version: v1
      source_closure_assessment_digest_sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
      accepted_answer_types: [intent_statement, unknown_acknowledgement]
      reasons_open: [Two writers are observed.]
      known_fact_ids: [fact.config.writer]
      known_evidence: [evidence:evidence.config.writer]
      competing_hypotheses:
        - id: hypothesis.owner_a
          statement: Component A owns the state.
        - id: hypothesis.owner_b
          statement: Component B owns the state.
      missing_evidence: [A governed decision.]
      priority: high
      risk_if_unresolved: Agents may preserve an authority split.
      architect_required: true
      status: resolved
      resolved_by_answers: [answer.config_writer]
      created_at: "2026-07-13T12:00:00Z"
  architect_answers:
    - id: answer.config_writer
      label: Config writer answer
      answers_questions: [question.config_writer]
      author:
        role: project_architect
        id: architect.local
      statement: |
        Component A is the intended writer.
        Component B is temporary.
      classifications: [intent_statement]
      scope:
        repository: github.com/example/project
        domain: repo
        files: [golang/config.go]
        symbols: [symbol.SaveConfig]
        components: [component.config]
      evidence_refs: [evidence:evidence.config.writer]
      evidence_pointers: [docs/decisions/config_writer.md]
      selected_hypotheses:
        - question_id: question.config_writer
          hypothesis_id: hypothesis.owner_a
      recorded_at: "2026-07-13T12:15:00Z"
      governance_status: accepted_for_question
`

func importDialogueFixture(t *testing.T, yamlText string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dialogue.yaml"), []byte(yamlText), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	var buf bytes.Buffer
	_, report, err := extractor.ImportAwarenessDir(dir, &buf)
	if err != nil {
		t.Fatalf("ImportAwarenessDir: %v", err)
	}
	if len(report.Imported()) != 1 {
		t.Fatalf("imported=%+v skipped=%+v", report.Imported(), report.Skipped())
	}
	return buf.String()
}

func TestArchitectureDialogueSchemaDetected(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dialogue.yaml"), []byte(validDialogueYAML), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	var buf bytes.Buffer
	_, report, err := extractor.ImportAwarenessDir(dir, &buf)
	if err != nil {
		t.Fatalf("ImportAwarenessDir: %v", err)
	}
	if got := report.Imported()[0].Schema; got != "architecture_dialogue" {
		t.Fatalf("schema=%q", got)
	}
}

func TestArchitectureDialogueImporterEmitsOpenQuestionType(t *testing.T) {
	out := importDialogueFixture(t, validDialogueYAML)
	if !strings.Contains(out, rdf.ClassOpenQuestion) {
		t.Fatal("missing OpenQuestion type")
	}
}

func TestArchitectureDialogueImporterEmitsArchitectAnswerType(t *testing.T) {
	out := importDialogueFixture(t, validDialogueYAML)
	if !strings.Contains(out, rdf.ClassArchitectAnswer) {
		t.Fatal("missing ArchitectAnswer type")
	}
}

func TestArchitectureDialogueImporterEmitsBlockedClaimEdges(t *testing.T) {
	out := importDialogueFixture(t, validDialogueYAML)
	if !strings.Contains(out, rdf.PropBlocksClaim) || !strings.Contains(out, "architectureClaim/claim.config_writer") {
		t.Fatal("missing blocksClaim edge")
	}
}

func TestArchitectureDialogueImporterEmitsGeneratedQuestionMetadata(t *testing.T) {
	out := importDialogueFixture(t, validDialogueYAML)
	for _, want := range []string{
		rdf.PropBlocksNode,
		rdf.PropBlocksClosureBlocker,
		rdf.PropQuestionTemplateID,
		rdf.PropQuestionTemplateVersion,
		rdf.PropSourceClosureAssessmentDigest,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s", want)
		}
	}
}

func TestArchitectureDialogueImporterEmitsAnswerQuestionEdges(t *testing.T) {
	out := importDialogueFixture(t, validDialogueYAML)
	if !strings.Contains(out, rdf.PropAnswersQuestion) || !strings.Contains(out, "openQuestion/question.config_writer") {
		t.Fatal("missing answersQuestion edge")
	}
}

func TestArchitectureDialogueImporterEmitsEvidenceEdges(t *testing.T) {
	out := importDialogueFixture(t, validDialogueYAML)
	if !strings.Contains(out, rdf.PropGroundedByEvidence) || !strings.Contains(out, rdf.PropCitesEvidence) {
		t.Fatal("missing evidence edges")
	}
}

func TestArchitectureDialogueImporterKeepsEvidencePointersLiteral(t *testing.T) {
	out := importDialogueFixture(t, validDialogueYAML)
	if !strings.Contains(out, rdf.PropEvidencePointer) || !strings.Contains(out, `"docs/decisions/config_writer.md"`) {
		t.Fatal("missing literal evidence pointer")
	}
}

func TestArchitectureDialogueImporterEmitsOneWayAnchorsOnly(t *testing.T) {
	out := importDialogueFixture(t, validDialogueYAML)
	if !strings.Contains(out, rdf.PropAnchoredIn) {
		t.Fatal("missing anchor")
	}
	if strings.Contains(out, rdf.PropImplements) {
		t.Fatal("dialogue importer emitted reverse implements edge")
	}
}

func TestArchitectureDialogueImporterRejectsMalformedDocument(t *testing.T) {
	dir := t.TempDir()
	bad := strings.Replace(validDialogueYAML, "question_text: Who is intended to write config state?", "question_text: ''", 1)
	if err := os.WriteFile(filepath.Join(dir, "dialogue.yaml"), []byte(bad), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	var buf bytes.Buffer
	_, report, err := extractor.ImportAwarenessDir(dir, &buf)
	if err != nil {
		t.Fatalf("ImportAwarenessDir: %v", err)
	}
	if !report.HasInvalid() {
		t.Fatalf("report=%+v", report)
	}
}

func TestArchitectureDialogueImporterNeverTypesReferencedClaim(t *testing.T) {
	out := importDialogueFixture(t, validDialogueYAML)
	claimIRI := strings.Trim(rdf.MintIRI(rdf.ClassArchitectureClaim, "claim.config_writer"), "<>")
	if strings.Contains(out, "<"+claimIRI+"> <"+rdf.PropType+">") {
		t.Fatal("referenced claim was typed")
	}
}

func TestArchitectureDialogueImporterNeverTypesReferencedEvidence(t *testing.T) {
	out := importDialogueFixture(t, validDialogueYAML)
	evIRI := strings.Trim(rdf.MintIRI(rdf.ClassEvidence, "evidence.config.writer"), "<>")
	if strings.Contains(out, "<"+evIRI+"> <"+rdf.PropType+">") {
		t.Fatal("referenced evidence was typed")
	}
}

func TestArchitectureDialogueImporterEmitsExpectedSourceKinds(t *testing.T) {
	out := importDialogueFixture(t, validDialogueYAML)
	if !strings.Contains(out, `"generated_candidate"`) || !strings.Contains(out, `"architect_dialogue"`) {
		t.Fatal("missing dialogue source kinds")
	}
}
