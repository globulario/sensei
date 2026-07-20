// SPDX-License-Identifier: AGPL-3.0-only

package coverage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	yaml "gopkg.in/yaml.v3"
)

// TestPhase95GovernedContractPresent is the govern-first gate for Phase 9.5 Checkpoint 1 (the
// canonical architectural-state projection owner). It proves every Checkpoint-1 invariant,
// failure mode, and forbidden fix is authored + schema-valid in the canonical source YAML, the
// opening contract exists, and the controlstate surface is high-risk covered — all before the
// owner is exercised.
func TestPhase95GovernedContractPresent(t *testing.T) {
	root := repoRootForHighRisk(t)

	wantInvariants := []string{
		"controlstate.semantic_projection_has_one_transport_neutral_owner",
		"controlstate.task_closure_must_not_be_projected_as_artifact_closure",
		"controlstate.artifact_closure_requires_explicit_class_policy",
		"controlstate.missing_source_must_not_synthesize_zero_or_closed",
		"controlstate.unknown_class_remains_visible_and_unknown",
		"controlstate.lifecycle_absence_must_not_synthesize_active",
		"controlstate.attention_severity_is_typed_owner_assigned_and_deterministic",
		"controlstate.navigation_descriptor_is_derived_from_the_canonical_registry",
		"controlstate.artifact_index_cursor_is_bound_to_snapshot_identity",
		"controlstate.feedback_is_consumed_only_at_exact_verified_scope",
	}
	wantFailureModes := []string{
		"controlstate.server_or_editor_reclassifies_control_state",
		"controlstate.task_verdict_copied_to_arbitrary_artifact",
		"controlstate.unsupported_class_hidden_or_marked_closed",
		"controlstate.missing_source_rendered_as_zero",
		"controlstate.lifecycle_defaulted_to_active",
		"controlstate.graph_adjacency_treated_as_closure_evidence",
		"controlstate.attention_severity_derived_from_text_or_color",
		"controlstate.pagination_cursor_reused_against_another_snapshot",
		"controlstate.repository_wide_feedback_scan_invented",
	}
	wantForbiddenFixes := []string{
		"phase9_5_hardcode_semantic_class_membership_in_editor",
		"phase9_5_infer_closure_from_edge_counts",
		"phase9_5_infer_not_applicable_from_missing_policy",
		"phase9_5_infer_active_from_absent_lifecycle",
		"phase9_5_parse_error_text_for_attention_severity",
		"phase9_5_replace_unknown_with_friendly_closed",
		"phase9_5_use_closure_report_verdict_as_artifact_closure",
		"phase9_5_derive_repository_identity_from_cwd_or_workspace",
	}

	assertGovernedIDs(t, filepath.Join(root, "docs", "awareness", "invariants.yaml"), "invariants", wantInvariants)
	assertGovernedIDs(t, filepath.Join(root, "docs", "awareness", "failure_modes.yaml"), "failure_modes", wantFailureModes)
	assertGovernedIDs(t, filepath.Join(root, "docs", "awareness", "forbidden_fixes.yaml"), "forbidden_fixes", wantForbiddenFixes)

	if _, err := os.Stat(filepath.Join(root, "docs", "design", "phase9.5-architecture-control-panel.md")); err != nil {
		t.Fatalf("Phase 9.5 opening contract is missing: %v", err)
	}

	// The canonical architectural-state owner surface must require a briefing before an edit.
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
		"golang/architecture/controlstate/registry.go",
		"docs/awareness/invariants.yaml",
		"docs/awareness/failure_modes.yaml",
		"docs/awareness/forbidden_fixes.yaml",
	} {
		if !requiresBriefing(hr.Files, p) {
			t.Errorf("Phase 9.5 surface %q is not high-risk covered", p)
		}
	}
}

// TestPhase95Checkpoint2GovernedContractPresent is the govern-first gate for Phase 9.5
// Checkpoint 2 (read-only protobuf + server surfaces). Every Checkpoint-2 invariant, failure
// mode, and forbidden fix is authored + schema-valid in the canonical source YAML, and the
// Checkpoint-2 rulings section exists in the opening contract — before the transport is built.
// Two directive items are satisfied by existing Checkpoint-1 records and are asserted here under
// their canonical IDs: server reclassification (controlstate.server_or_editor_reclassifies_
// control_state) and cwd/workspace repository authority (phase9_5_derive_repository_identity_
// from_cwd_or_workspace).
func TestPhase95Checkpoint2GovernedContractPresent(t *testing.T) {
	root := repoRootForHighRisk(t)

	wantInvariants := []string{
		"controlstate.protobuf_is_lossless_transport_not_semantic_owner",
		"controlstate.server_read_handler_must_consume_canonical_projection",
		"controlstate.semantic_unavailability_must_remain_response_data",
		"controlstate.transport_must_preserve_unknown_distinct_from_zero",
		"controlstate.transport_must_not_recompute_projection_digest",
		"controlstate.artifact_class_must_not_be_caller_selected",
		"controlstate.repository_context_must_not_be_derived_from_cwd",
		"controlstate.read_surfaces_must_be_structurally_non_mutating",
		"controlstate.cursor_must_remain_opaque_and_owner_validated",
		"controlstate.proto_evolution_must_be_additive",
	}
	wantFailureModes := []string{
		"controlstate.server_or_editor_reclassifies_control_state", // CP1 record covers server reclassification
		"controlstate.protobuf_mapping_collapses_unknown_to_zero",
		"controlstate.partial_projection_returned_as_transport_failure",
		"controlstate.client_supplied_class_treated_as_artifact_authority",
		"controlstate.rpc_accepts_caller_selected_filesystem_root",
		"controlstate.handler_infers_dimension_state_from_raw_graph_edges",
		"controlstate.transport_recomputes_a_different_digest",
		"controlstate.read_rpc_mutates_dialogue_or_completion_state",
		"controlstate.proto_field_renumbering_breaks_existing_consumers",
	}
	wantForbiddenFixes := []string{
		"phase9_5_duplicate_closure_or_severity_tables_in_server",
		"phase9_5_parse_reason_prose_to_choose_proto_enum",
		"phase9_5_convert_missing_optional_counts_to_zero",
		"phase9_5_treat_partial_projection_as_empty_success",
		"phase9_5_derive_repository_identity_from_cwd_or_workspace", // CP1 record covers cwd/workspace authority
		"phase9_5_accept_artifact_class_from_client",
		"phase9_5_reconstruct_models_from_labels_or_colors",
		"phase9_5_expose_raw_errors_or_absolute_paths",
	}

	assertGovernedIDs(t, filepath.Join(root, "docs", "awareness", "invariants.yaml"), "invariants", wantInvariants)
	assertGovernedIDs(t, filepath.Join(root, "docs", "awareness", "failure_modes.yaml"), "failure_modes", wantFailureModes)
	assertGovernedIDs(t, filepath.Join(root, "docs", "awareness", "forbidden_fixes.yaml"), "forbidden_fixes", wantForbiddenFixes)

	// The Checkpoint-2 rulings are frozen in the opening contract.
	design, err := os.ReadFile(filepath.Join(root, "docs", "design", "phase9.5-architecture-control-panel.md"))
	if err != nil {
		t.Fatalf("Phase 9.5 opening contract is missing: %v", err)
	}
	if !strings.Contains(string(design), "## 25. Checkpoint 2 rulings (frozen)") {
		t.Fatal("Phase 9.5 design contract is missing the Checkpoint-2 rulings section")
	}

	// The wire contract surface must require a briefing before an edit.
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
	if !requiresBriefing(hr.Files, "proto/awareness_graph.proto") {
		t.Error("Phase 9.5 CP2 surface \"proto/awareness_graph.proto\" is not high-risk covered")
	}
}
