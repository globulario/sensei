// SPDX-License-Identifier: Apache-2.0

package coverage

import (
	"os"
	"path/filepath"
	"testing"

	yaml "gopkg.in/yaml.v3"
)

// TestPhase9GovernedContractPresent is the govern-first gate for Phase 9 (operational
// surfacing of terminal-completion truth). It proves the Phase-9 governed contract
// (Slice 9.0) is authored in the canonical source YAML — every invariant, failure
// mode, and forbidden fix is present and schema-valid — and that the surfaces the
// roadmap governs are high-risk covered so a future Phase-9 edit requires a
// briefing/preflight. The contract exists before any Phase-9 implementation.
func TestPhase9GovernedContractPresent(t *testing.T) {
	root := repoRootForHighRisk(t)

	wantInvariants := []string{
		"closure.phase9_surfaces_consume_completion_truth_never_reinterpret_it",
		"closure.phase9_invocation_surfaces_only_delegate_to_the_owner",
		"closure.phase9_projections_are_not_terminal_authority",
		"closure.phase9_preserves_the_three_completion_distinctions",
		"closure.phase9_surfaces_locked_until_a_reviewed_slice",
		// Slice 9.4 — CI/GitHub completion gate.
		"closure.completion_gate_fails_open_on_unavailability_and_closed_on_a_computed_verdict",
		"closure.completion_gate_requires_explicit_identity_when_enforcement_applies",
		// Slice 9.4c — authoritative change-to-task binding.
		"closure.change_task_binding_is_exact_typed_and_positively_authorized",
		"closure.change_task_binding_producer_is_authoritative_and_deterministic",
	}
	wantFailureModes := []string{
		"closure.phase9_surface_manufactures_or_reinterprets_completion",
		"closure.phase9_projection_or_report_treated_as_terminal_authority",
		"closure.phase9_surface_shipped_without_a_reviewed_slice",
		"closure.completion_gate_conflates_unavailability_with_a_broken_verdict",
		"closure.change_task_binding_launders_a_completion_onto_an_unrelated_change",
	}
	wantForbiddenFixes := []string{
		"phase9_surface_appends_completed_or_writes_receipt",
		"phase9_reinterpret_or_rederive_completion_truth_in_a_surface",
		"phase9_treat_projection_or_closure_report_as_completion_authority",
		"phase9_ship_a_surface_without_a_reviewed_slice",
		"phase9_invent_a_gnn_or_ml_capability_without_repository_evidence",
		"phase9_gate_fails_closed_on_sensei_unavailability",
		"phase9_gate_enforces_without_per_domain_opt_in",
		"phase9_gate_treats_missing_required_task_identity_as_runtime_unavailability",
		"phase9_change_task_binding_normalizes_or_infers_identity",
		"phase9_change_task_binding_producer_failure_enters_runtime_degradation",
	}

	assertGovernedIDs(t, filepath.Join(root, "docs", "awareness", "invariants.yaml"), "invariants", wantInvariants)
	assertGovernedIDs(t, filepath.Join(root, "docs", "awareness", "failure_modes.yaml"), "failure_modes", wantFailureModes)
	assertGovernedIDs(t, filepath.Join(root, "docs", "awareness", "forbidden_fixes.yaml"), "forbidden_fixes", wantForbiddenFixes)

	// The governed roadmap and the opened slice contracts must be authored.
	for _, doc := range []string{"phase9-contract.md", "phase9.4-contract.md", "phase9.4c-change-task-binding.md"} {
		if _, err := os.Stat(filepath.Join(root, "docs", "design", doc)); err != nil {
			t.Fatalf("Phase-9 governed contract docs/design/%s is missing: %v", doc, err)
		}
	}

	// The Phase-9 surfaces the roadmap governs (the completion owner it consumes, and
	// the CLI/server surfaces it will thin-client) must require a briefing before an
	// edit. Per-slice surfaces (cmd/awareness-mcp, editor/vscode, .github) are added to
	// high_risk_files.yaml when their slice opens.
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
		"golang/architecture/completion/complete.go", // the completion owner Phase-9 consumes
		"cmd/awg/main.go",           // the CLI invocation surface
		"cmd/awareness-mcp/main.go", // the MCP invocation surface (9.2, added when the slice opened)
		"golang/server/briefing.go", // the server read-model surface
		"docs/awareness/invariants.yaml",
		"docs/awareness/failure_modes.yaml",
		"docs/awareness/forbidden_fixes.yaml",
		"docs/awareness/required_tests.yaml",
	} {
		if !requiresBriefing(hr.Files, p) {
			t.Errorf("Phase-9 surface %q is not high-risk covered", p)
		}
	}
}
