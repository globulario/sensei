// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBenchmarkEventMeta_UnwrapsLearningEvent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "learning_event.yaml")
	body := `learning_event:
  id: cli__cli-1388-d-1
  task: cli__cli-1388
  learning_evidence: usable
  certification_status: certified_clean_repair
  retry_result_classification: retry_recovered
  diagnosis:
    primary_failure_mode: clean_contract_repair
  decision:
    action: stop_and_report
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := loadBenchmarkRetryDoc(path)
	if err != nil {
		t.Fatalf("loadBenchmarkRetryDoc: %v", err)
	}
	event := benchmarkRetryUnwrapEvent(doc)
	if got := benchmarkString(event["id"]); got != "cli__cli-1388-d-1" {
		t.Fatalf("event id=%q", got)
	}
	if got := benchmarkString(benchmarkMap(event["decision"])["action"]); got != "stop_and_report" {
		t.Fatalf("decision action=%q", got)
	}
	if got := benchmarkString(benchmarkMap(event["diagnosis"])["primary_failure_mode"]); got != "clean_contract_repair" {
		t.Fatalf("primary_failure_mode=%q", got)
	}
	if got := benchmarkString(event["certification_status"]); got != "certified_clean_repair" {
		t.Fatalf("certification_status=%q", got)
	}
}
