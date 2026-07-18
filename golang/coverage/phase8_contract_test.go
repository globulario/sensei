// SPDX-License-Identifier: Apache-2.0

package coverage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	yaml "gopkg.in/yaml.v3"
)

// TestPhase8GovernedContractPresent is the govern-first gate for Phase 8 (terminal
// architectural closure). It proves the Phase-8 governed contract (Slice 8.0) is
// authored in the canonical source YAML — every invariant, failure mode, and
// forbidden fix is present and schema-valid (id + title) — and that the Phase-8
// authority surfaces are high-risk covered so a future Phase-8 edit requires a
// briefing/preflight. The contract exists before any Phase-8 implementation.
func TestPhase8GovernedContractPresent(t *testing.T) {
	root := repoRootForHighRisk(t)

	wantInvariants := []string{
		"closure.only_question_disposition_owner_records_question_outcomes",
		"closure.only_promotion_owner_establishes_reusable_truth",
		"closure.dialogue_answer_is_not_authoritative_by_existence",
		"closure.reusable_promotion_binds_full_question_answer_lineage",
		"closure.task_local_answers_never_enter_repository_briefings",
		"closure.no_blocking_question_satisfied_by_omission",
		"closure.certification_requires_verified_question_resolution_summary",
		"closure.completion_requires_closed_current_question_loop",
		"closure.completion_never_mutates_governed_or_certification_truth",
		"closure.completed_tasks_are_terminal_and_replay_idempotent",
	}
	wantFailureModes := []string{
		"closure.accepted_answer_stranded_outside_governed_promotion",
		"closure.dialogue_answer_mistaken_for_governed_truth",
		"closure.unauthorized_stale_or_contradictory_promotion",
		"closure.task_local_answer_leaks_into_global_briefing",
		"closure.graph_rebuilt_without_question_answer_provenance",
		"closure.status_blind_unresolved_question_computation",
		"closure.certification_or_completion_over_unresolved_or_stale_promotion",
		"closure.duplicate_promotion_or_duplicate_completion",
		"closure.partial_governed_source_or_graph_commit",
		"closure.caller_supplied_disposition_terminal_status_or_correctness",
	}
	wantForbiddenFixes := []string{
		"phase8_direct_graph_or_projection_write",
		"phase8_production_calls_cli_handler_or_duplicates_policy",
		"phase8_arbitrary_actor_or_implicit_authority_reuse",
		"phase8_adoption_receipt_or_node_alone_as_promotion_proof",
		"phase8_delete_question_to_improve_closure",
		"phase8_convert_deferred_dismissed_to_resolved_without_disposition",
		"phase8_copy_task_local_answer_into_governed_sources",
		"phase8_completion_repairs_the_feedback_loop",
	}

	assertGovernedIDs(t, filepath.Join(root, "docs", "awareness", "invariants.yaml"), "invariants", wantInvariants)
	assertGovernedIDs(t, filepath.Join(root, "docs", "awareness", "failure_modes.yaml"), "failure_modes", wantFailureModes)
	assertGovernedIDs(t, filepath.Join(root, "docs", "awareness", "forbidden_fixes.yaml"), "forbidden_fixes", wantForbiddenFixes)

	// The Phase-8 authority surfaces must require a briefing before an edit — the
	// future completion owner (under the already-covered golang/architecture/ prefix)
	// and the governed authority sources that carry the contract.
	hrData, err := os.ReadFile(filepath.Join(root, "docs", "awareness", "high_risk_files.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var hr struct {
		Files []string `yaml:"files"`
	}
	if err := yaml.Unmarshal(hrData, &hr); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		"golang/architecture/completion/completion.go", // future Phase-8 completion owner
		"docs/awareness/invariants.yaml",
		"docs/awareness/failure_modes.yaml",
		"docs/awareness/forbidden_fixes.yaml",
		"docs/awareness/required_tests.yaml",
	} {
		if !requiresBriefing(hr.Files, p) {
			t.Errorf("Phase-8 authority surface %q is not high-risk covered", p)
		}
	}
}

// assertGovernedIDs requires every id in wantIDs to be present under topKey with a
// non-empty title (schema-valid).
func assertGovernedIDs(t *testing.T, path, topKey string, wantIDs []string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string][]map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	byID := map[string]map[string]any{}
	for _, e := range doc[topKey] {
		if id, _ := e["id"].(string); id != "" {
			byID[id] = e
		}
	}
	for _, id := range wantIDs {
		e, ok := byID[id]
		if !ok {
			t.Errorf("%s: missing Phase-8 governed record %q", filepath.Base(path), id)
			continue
		}
		// Governed records carry a "title"; authority records carry a "label".
		title, _ := e["title"].(string)
		label, _ := e["label"].(string)
		if strings.TrimSpace(title) == "" && strings.TrimSpace(label) == "" {
			t.Errorf("%s: %q has no title/label (schema-invalid)", filepath.Base(path), id)
		}
	}
}
