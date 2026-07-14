// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanProbesListTemplatesDoesNotRequireInputs(t *testing.T) {
	if code := runPlanProbes([]string{"--list-templates", "--format", "json"}); code != 0 {
		t.Fatalf("runPlanProbes --list-templates exit=%d", code)
	}
}

func TestRecordProbeResultBuildsOfflineReceipt(t *testing.T) {
	dir := t.TempDir()
	probesPath := filepath.Join(dir, "probes.yaml")
	if err := os.WriteFile(probesPath, []byte(testProbeDocumentYAML()), 0o644); err != nil {
		t.Fatal(err)
	}
	out, report, evidence, err := buildRecordProbeResultOutput(recordProbeResultOptions{
		Probes:       probesPath,
		ProbeID:      "probe.manual_observation",
		ResultStatus: "completed",
		ExecutedBy:   "tester",
		ObservedAt:   "2026-07-13T12:00:00Z",
		Format:       "yaml",
	})
	if err != nil {
		t.Fatalf("buildRecordProbeResultOutput: %v", err)
	}
	if !strings.Contains(string(out), "architecture_probe_results:") || !strings.Contains(string(out), "probe_result.") {
		t.Fatalf("missing result receipt:\n%s", out)
	}
	if !strings.Contains(string(report), "diagnostic_only") {
		t.Fatalf("expected diagnostic-only evidence disposition:\n%s", report)
	}
	if len(evidence) != 0 {
		t.Fatalf("recording without evidence-state output should not emit evidence state:\n%s", evidence)
	}
}

func testProbeDocumentYAML() string {
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
    - id: probe.manual_observation
      label: Manual observation probe
      status: proposed
      question_id: question.config_writer
      closure_blocker_ids: [blocker.evidence.abcdef012345]
      template_id: probe.manual_observation.v1
      template_version: v1
      probe_kind: manual_observation
      evidence_lane: diagnostic
      evidence_role: diagnostic
      node_refs: [component:component.config]
      safety_class: static_read
      approval_gate: none
      automatic_execution_allowed: false
      steps:
        - kind: record_manual_observation
          target: component.config
          description: Record the observation supplied by a human operator.
      expected_artifact_kinds: [manual_note]
`
}
