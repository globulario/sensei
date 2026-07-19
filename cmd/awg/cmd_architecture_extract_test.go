// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestArchitectureExtractLayersEvidence(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/project\n")
	writeFile(t, filepath.Join(root, "admin.go"), "package project\n")
	writeFile(t, filepath.Join(root, "admin_test.go"), "package project\n")
	writeFile(t, filepath.Join(root, "docs", "awareness", "contracts.yaml"), `
contracts:
  - id: contract.admin_change
    name: Admin change authority
    description: Admin changes go through the owner path.
    source_files:
      - admin.go
    required_tests:
      - admin_test.go:TestAdminChange
`)
	writeFile(t, filepath.Join(root, "docs", "awareness", "candidates", "contract_candidate.yaml"), `
contracts:
  - id: contract.candidate_only
    name: Candidate only
    description: Candidate must not be treated as governed.
    source_files:
      - admin.go
`)
	writeFile(t, filepath.Join(root, "docs", "awareness", "candidates", "proposal.yaml"), `
proposal:
  status: awaiting_review
  kind: invariant
  id: invariant.proposed
  title: Proposed invariant
  description: A singular proposal remains inferred.
  source_files:
    - admin.go
`)
	writeFile(t, filepath.Join(root, "docs", "awareness", "candidates", "cold.yaml"), `
id: candidate.cold
class: FailureModeCandidate
status: candidate
reason: Cold-source evidence.
source_paths:
  - file: admin.go
  - commit: abc123
`)
	writeFile(t, filepath.Join(root, "docs", "awareness", "generated", "source_symbols.yaml"), "code_symbols: []\n")
	writeFile(t, filepath.Join(root, ".github", "workflows", "awg-gate.yml"), `
jobs:
  awg-gate:
    steps:
      - run: awg gate --enforce --diff origin/main...HEAD
`)

	report, err := buildArchitectureExtractionReport(root, "example.com/project", 0)
	if err != nil {
		t.Fatalf("buildArchitectureExtractionReport: %v", err)
	}
	if !hasArchitectureRecord(report.GovernedContractSet, "contract.admin_change") {
		t.Fatalf("governed records missing contract.admin_change: %#v", report.GovernedContractSet)
	}
	if !hasArchitectureRecord(report.InferredContractSet, "contract.candidate_only") {
		t.Fatalf("inferred records missing candidate: %#v", report.InferredContractSet)
	}
	if !hasArchitectureRecord(report.InferredContractSet, "invariant.proposed") {
		t.Fatalf("inferred records missing proposal: %#v", report.InferredContractSet)
	}
	if !hasArchitectureRecord(report.InferredContractSet, "candidate.cold") {
		t.Fatalf("inferred records missing cold-source candidate: %#v", report.InferredContractSet)
	}
	if !hasArchitectureRecordPrefix(report.ObservedContractSet, "observed.generated.") {
		t.Fatalf("observed records missing generated artifact: %#v", report.ObservedContractSet)
	}
	if !hasArchitectureRecordPrefix(report.GovernedContractSet, "governed.ci.") {
		t.Fatalf("governed records missing CI gate: %#v", report.GovernedContractSet)
	}
	for _, rec := range report.InferredContractSet {
		if rec.ID == "contract.candidate_only" && rec.Classification.Layer != "inferred" {
			t.Fatalf("candidate layer=%q, want inferred", rec.Classification.Layer)
		}
	}
}

func TestRenderArchitectureExtractionJSON(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "docs", "awareness", "contracts.yaml"), `
contracts:
  - id: contract.example
    name: Example
    description: Example contract.
`)
	report, err := buildArchitectureExtractionReport(root, "example.com/project", 0)
	if err != nil {
		t.Fatalf("buildArchitectureExtractionReport: %v", err)
	}
	out, err := renderArchitectureExtractionReport(report, "json")
	if err != nil {
		t.Fatalf("renderArchitectureExtractionReport: %v", err)
	}
	if !strings.Contains(string(out), `"governed_contract_set"`) {
		t.Fatalf("json missing governed_contract_set:\n%s", string(out))
	}
	if !strings.Contains(string(out), `"contract.example"`) {
		t.Fatalf("json missing contract id:\n%s", string(out))
	}
}

func hasArchitectureRecord(records []architectureContractRecord, id string) bool {
	for _, rec := range records {
		if rec.ID == id {
			return true
		}
	}
	return false
}

func hasArchitectureRecordPrefix(records []architectureContractRecord, prefix string) bool {
	for _, rec := range records {
		if strings.HasPrefix(rec.ID, prefix) {
			return true
		}
	}
	return false
}
