// SPDX-License-Identifier: Apache-2.0

// Importer for AgentRun JSONL logs (Phase 2G).
//
// If Claude / Codex / local agents operate on Globular, awareness should
// remember whether they used preflight, ignored warnings, skipped required
// tests, caused regressions, or produced good fixes. AgentRun records one such
// run. The importer is offline: one JSON object per line under
// docs/awareness/agent_runs/*.jsonl. It does NOT require any live agent API.
//
// JSON shape (all fields optional except id):
//
//	{
//	  "id": "run.2026-06-11-001",
//	  "agent_name": "claude", "model_name": "claude-opus-4-8",
//	  "task_summary": "...",
//	  "used_preflight": true, "preflight_status": "OK",
//	  "warnings_ignored": ["..."],
//	  "tests_required": ["test:Foo"], "tests_run": ["test:Foo"], "tests_skipped": [],
//	  "patch_status": "merged",
//	  "caused_incident": "", "resolved_incident": "INC-2026-0017",
//	  "created_outcome_feedback": ["outcome.x"],
//	  "created_candidate_knowledge": []
//	}
package extractor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/globulario/sensei/golang/rdf"
)

type jsonAgentRun struct {
	ID                        string   `json:"id"`
	AgentName                 string   `json:"agent_name"`
	ModelName                 string   `json:"model_name"`
	TaskSummary               string   `json:"task_summary"`
	UsedPreflight             *bool    `json:"used_preflight"`
	PreflightStatus           string   `json:"preflight_status"`
	WarningsIgnored           []string `json:"warnings_ignored"`
	TestsRequired             []string `json:"tests_required"`
	TestsRun                  []string `json:"tests_run"`
	TestsSkipped              []string `json:"tests_skipped"`
	PatchStatus               string   `json:"patch_status"`
	CausedIncident            string   `json:"caused_incident"`
	ResolvedIncident          string   `json:"resolved_incident"`
	CreatedOutcomeFeedback    []string `json:"created_outcome_feedback"`
	CreatedCandidateKnowledge []string `json:"created_candidate_knowledge"`
}

// importAgentRunsFile imports one .jsonl file (one AgentRun per non-blank line)
// and returns a FileReport. A malformed line is skipped (best-effort); the file
// is reported as imported if at least one line produced a node.
func importAgentRunsFile(e *rdf.Emitter, path string) FileReport {
	f, err := os.Open(path)
	if err != nil {
		return FileReport{Path: path, Status: StatusInvalid, Schema: "agent_run", Phase: "B", Reason: err.Error()}
	}
	defer f.Close()

	before := e.Triples
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var parseErr error
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		var run jsonAgentRun
		if err := json.Unmarshal([]byte(line), &run); err != nil {
			parseErr = err
			continue
		}
		emitAgentRun(e, path, run)
	}
	if err := sc.Err(); err != nil {
		return FileReport{Path: path, Status: StatusInvalid, Schema: "agent_run", Phase: "B", Reason: err.Error()}
	}
	if e.Triples == before && parseErr != nil {
		return FileReport{Path: path, Status: StatusInvalid, Schema: "agent_run", Phase: "B", Reason: parseErr.Error()}
	}
	return FileReport{Path: path, Status: StatusImported, Schema: "agent_run", Phase: "B", Count: e.Triples - before}
}

func emitAgentRun(e *rdf.Emitter, path string, run jsonAgentRun) {
	if run.ID == "" {
		return
	}
	subj := rdf.MintIRI(rdf.ClassAgentRun, run.ID)
	e.Typed(subj, rdf.ClassAgentRun)

	e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(run.TaskSummary, run.ID)))
	emitOptLit(e, subj, rdf.PropAgentName, run.AgentName)
	emitOptLit(e, subj, rdf.PropModelName, run.ModelName)
	emitOptLit(e, subj, rdf.PropTaskSummary, run.TaskSummary)
	if run.UsedPreflight != nil {
		v := "false"
		if *run.UsedPreflight {
			v = "true"
		}
		e.Triple(subj, rdf.IRI(rdf.PropUsedPreflight), rdf.Lit(v))
	}
	emitOptLit(e, subj, rdf.PropPreflightStatus, run.PreflightStatus)
	emitOptLit(e, subj, rdf.PropPatchStatus, run.PatchStatus)
	emitOptLit(e, subj, rdf.PropCausedIncident, run.CausedIncident)
	emitOptLit(e, subj, rdf.PropResolvedIncident, run.ResolvedIncident)
	e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

	emitOptLits(e, subj, rdf.PropWarningsIgnored, run.WarningsIgnored)
	emitOptLits(e, subj, rdf.PropTestsRequired, run.TestsRequired)
	emitOptLits(e, subj, rdf.PropTestsRun, run.TestsRun)
	emitOptLits(e, subj, rdf.PropTestsSkipped, run.TestsSkipped)
	emitOptLits(e, subj, rdf.PropCreatedCandidateKnowledge, run.CreatedCandidateKnowledge)

	// Outcome feedback this run created — object link (never types the target).
	// Entries may be bare ids ("outcome.x") or class-qualified ("outcome:..."
	// / "outcome_feedback:...").
	for _, o := range run.CreatedOutcomeFeedback {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		slug := o
		if i := strings.IndexByte(o, ':'); i >= 0 {
			slug = o[i+1:]
		}
		e.Triple(subj, rdf.IRI(rdf.PropCreatedOutcomeFeedback), rdf.MintIRI(rdf.ClassOutcomeFeedback, slug))
	}
}

// AgentRunRisk classifies whether an agent run should escalate review based on
// its discipline. Returns (escalate, reasons). Pure — used by tests and any
// scorecard roll-up. Skipping a required test, ignoring warnings, causing an
// incident, or not using preflight on a non-trivial change all escalate.
func AgentRunRiskFromJSON(line string) (bool, []string, error) {
	var run jsonAgentRun
	if err := json.Unmarshal([]byte(line), &run); err != nil {
		return false, nil, fmt.Errorf("parse: %w", err)
	}
	return agentRunRisk(run)
}

func agentRunRisk(run jsonAgentRun) (bool, []string, error) {
	var reasons []string
	// Skipped a REQUIRED test (intersection of required and skipped).
	required := map[string]bool{}
	for _, r := range run.TestsRequired {
		required[strings.TrimSpace(r)] = true
	}
	for _, s := range run.TestsSkipped {
		if required[strings.TrimSpace(s)] {
			reasons = append(reasons, "skipped required test "+s)
		}
	}
	if len(run.WarningsIgnored) > 0 {
		reasons = append(reasons, fmt.Sprintf("ignored %d warning(s)", len(run.WarningsIgnored)))
	}
	if strings.TrimSpace(run.CausedIncident) != "" {
		reasons = append(reasons, "caused incident "+run.CausedIncident)
	}
	if run.UsedPreflight != nil && !*run.UsedPreflight {
		reasons = append(reasons, "did not use preflight")
	}
	return len(reasons) > 0, reasons, nil
}
