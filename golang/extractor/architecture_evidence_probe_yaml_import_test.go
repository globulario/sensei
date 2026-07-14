// SPDX-License-Identifier: AGPL-3.0-only

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

func validEvidenceProbeYAML() string {
	return `architecture_evidence_probes:
  schema_version: "1"
  generated_by: sensei plan-probes
  binding:
    repository_domain: github.com/example/project
    revision: "0123456789abcdef"
    revision_status: resolved
    graph_digest_sha256: "abcdef0123456789"
    graph_digest_status: resolved
  source_closure_assessment_digest_sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  source_dialogue_digest_sha256: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
  source_claim_document_digest_sha256: cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
  probes:
    - id: probe.config_writer_test
      label: Config writer test probe
      status: proposed
      question_id: question.config_writer
      closure_blocker_ids: [blocker.evidence.abcdef012345]
      claim_ids: [claim.config_writer]
      template_id: probe.existing_test_execution.v1
      template_version: v1
      probe_kind: test_execution
      evidence_lane: test
      evidence_role: supporting
      target_evidence_id: evidence:evidence.config.writer
      test_ids: [golang/server/config_test.go:TestConfigWriter]
      safety_class: local_test
      approval_gate: review_required
      automatic_execution_allowed: false
      steps:
        - kind: run_existing_test
          target: golang/server/config_test.go:TestConfigWriter
          description: Run the existing config writer test.
      expected_artifact_kinds: [test_output]
`
}

func importEvidenceProbeFixture(t *testing.T, content string) (string, *extractor.ImportReport) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "probes.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	em, report, err := extractor.ImportAwarenessDir(dir, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if err := em.Flush(); err != nil {
		t.Fatal(err)
	}
	return buf.String(), report
}

func TestArchitectureEvidenceProbeSchemaDetected(t *testing.T) {
	_, report := importEvidenceProbeFixture(t, validEvidenceProbeYAML())
	if len(report.Imported()) != 1 || report.Imported()[0].Schema != "architecture_evidence_probes" {
		t.Fatalf("schema not detected/imported: %+v", report.Files)
	}
}

func TestArchitectureEvidenceProbeImporterEmitsProbeType(t *testing.T) {
	nt, _ := importEvidenceProbeFixture(t, validEvidenceProbeYAML())
	requireTriplePart(t, nt, rdf.IRI(rdf.ClassEvidenceProbe))
}

func TestArchitectureEvidenceProbeImporterEmitsPlanMetadata(t *testing.T) {
	nt, _ := importEvidenceProbeFixture(t, validEvidenceProbeYAML())
	for _, prop := range []string{
		rdf.PropProbeForQuestion,
		rdf.PropTargetsClaim,
		rdf.PropProbeKind,
		rdf.PropHasEvidenceLane,
		rdf.PropEvidenceRole,
		rdf.PropSafetyClass,
		rdf.PropRequiresApprovalGate,
		rdf.PropHasProbeStep,
		rdf.PropExpectedArtifactKind,
		rdf.PropSourceDialogueDigest,
		rdf.PropSourceClaimDocumentDigest,
	} {
		requireTriplePart(t, nt, rdf.IRI(prop))
	}
}

func TestArchitectureEvidenceProbeImporterDoesNotExecuteOrMintEvidence(t *testing.T) {
	nt, _ := importEvidenceProbeFixture(t, validEvidenceProbeYAML())
	evIRI := rdf.MintIRI(rdf.ClassEvidence, "evidence.config.writer")
	if strings.Contains(nt, evIRI+" "+rdf.IRI(rdf.PropType)) {
		t.Fatal("probe importer typed referenced Evidence")
	}
	if strings.Contains(nt, "probe_result") || strings.Contains(nt, "observed_at") {
		t.Fatalf("probe importer emitted result-like runtime data:\n%s", nt)
	}
}

func TestArchitectureEvidenceProbeImporterRejectsUnsafeAutomaticExecution(t *testing.T) {
	bad := strings.Replace(validEvidenceProbeYAML(), "automatic_execution_allowed: false", "automatic_execution_allowed: true", 1)
	bad = strings.Replace(bad, "safety_class: local_test", "safety_class: runtime_read", 1)
	_, report := importEvidenceProbeFixture(t, bad)
	if report.Files[0].Status != extractor.StatusInvalid {
		t.Fatalf("unsafe automatic execution imported: %+v", report.Files)
	}
}
