// SPDX-License-Identifier: Apache-2.0

package extractor_test

import (
	"bytes"
	"testing"

	"github.com/globulario/awareness-graph/golang/extractor"
	"github.com/globulario/awareness-graph/golang/rdf"
)

const sampleAgentRunJSONL = `{"id":"run.example","agent_name":"claude","model_name":"claude-opus-4-8","task_summary":"did a thing","used_preflight":true,"preflight_status":"OK","tests_required":["test:TestFoo"],"tests_run":["test:TestFoo"],"tests_skipped":[],"patch_status":"merged","created_outcome_feedback":["outcome.example"]}
{"id":"run.bad","agent_name":"x","used_preflight":false,"tests_required":["test:TestBar"],"tests_skipped":["test:TestBar"],"warnings_ignored":["ignored a thing"],"patch_status":"reverted","caused_incident":"INC-X"}
`

func TestAgentRunJSONLImporter(t *testing.T) {
	var buf bytes.Buffer
	root := makeDir(t, map[string]string{"agent_runs/runs.jsonl": sampleAgentRunJSONL})
	_, report, err := extractor.ImportAwarenessDir(root, &buf)
	if err != nil {
		t.Fatalf("ImportAwarenessDir: %v", err)
	}
	out := buf.String()
	assertValidNT(t, out)

	if len(report.Imported()) != 1 || report.Imported()[0].Schema != "agent_run" {
		t.Fatalf("expected one agent_run file imported, got %+v", report.Imported())
	}

	subj := rdf.MintIRI(rdf.ClassAgentRun, "run.example")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassAgentRun)+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropAgentName)+` "claude"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropUsedPreflight)+` "true"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropTestsRequired)+` "test:TestFoo"`)
}

func TestAgentScorecardLinksToOutcomeFeedback(t *testing.T) {
	var buf bytes.Buffer
	root := makeDir(t, map[string]string{"agent_runs/runs.jsonl": sampleAgentRunJSONL})
	if _, _, err := extractor.ImportAwarenessDir(root, &buf); err != nil {
		t.Fatalf("ImportAwarenessDir: %v", err)
	}
	out := buf.String()
	subj := rdf.MintIRI(rdf.ClassAgentRun, "run.example")
	outcome := rdf.MintIRI(rdf.ClassOutcomeFeedback, "outcome.example")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropCreatedOutcomeFeedback)+" "+outcome+" .")
	// Linking is not authoring — the outcome target is not typed here.
	mustNotContain(t, out, outcome+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassOutcomeFeedback))
}

func TestSkippedRequiredTestsEscalateRisk(t *testing.T) {
	// A run that skipped a required test must escalate.
	bad := `{"id":"r","tests_required":["test:TestBar"],"tests_skipped":["test:TestBar"]}`
	esc, reasons, err := extractor.AgentRunRiskFromJSON(bad)
	if err != nil {
		t.Fatalf("AgentRunRiskFromJSON: %v", err)
	}
	if !esc {
		t.Errorf("skipping a required test must escalate; reasons=%v", reasons)
	}

	// A disciplined run does not escalate.
	good := `{"id":"r","used_preflight":true,"tests_required":["test:TestBar"],"tests_run":["test:TestBar"],"tests_skipped":[]}`
	esc, _, err = extractor.AgentRunRiskFromJSON(good)
	if err != nil {
		t.Fatalf("AgentRunRiskFromJSON: %v", err)
	}
	if esc {
		t.Errorf("a disciplined run must not escalate")
	}
}
