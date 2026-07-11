// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScaffoldProject(t *testing.T) {
	dir := t.TempDir()

	created, err := scaffoldProject(dir, initOptions{hooks: true, claudeMD: true, agentsMD: true, cursor: true})
	if err != nil {
		t.Fatalf("scaffoldProject: %v", err)
	}
	if len(created) == 0 {
		t.Fatal("scaffoldProject returned no files")
	}

	// Verify key files exist.
	expectedFiles := []string{
		"docs/awareness/invariants.yaml",
		"docs/awareness/failure_modes.yaml",
		"docs/awareness/incident_patterns.yaml",
		"docs/awareness/high_risk_files.yaml",
		"docs/awareness/activation_rules.yaml",
		"docs/awareness/meta_principles.yaml",
		".sensei/config.yaml",
		".claude/hooks/enforce-briefing.sh",
		".claude/hooks/record-briefing.sh",
		".claude/hooks/edit-check-guard.sh",
		"CLAUDE.md",
		"AGENTS.md",
		".cursor/rules/sensei.mdc",
	}

	for _, rel := range expectedFiles {
		path := filepath.Join(dir, rel)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file %s not found", rel)
		}
	}

	// Verify CLAUDE.md has the AWG section.
	claudeData, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	if !strings.Contains(string(claudeData), "## Sensei") {
		t.Error("CLAUDE.md missing AWG section")
	}

	// Verify idempotent — second run should not overwrite.
	created2, err := scaffoldProject(dir, initOptions{hooks: true, claudeMD: true, agentsMD: true, cursor: true})
	if err != nil {
		t.Fatalf("scaffoldProject (2nd run): %v", err)
	}
	if len(created2) != 0 {
		t.Errorf("second scaffoldProject created %d files, expected 0 (idempotent)", len(created2))
	}
}

func TestScaffoldProject_MetaPrinciples(t *testing.T) {
	dir := t.TempDir()

	if _, err := scaffoldProject(dir, initOptions{}); err != nil {
		t.Fatalf("scaffoldProject: %v", err)
	}

	// Verify meta-principles YAML has all 18 entries.
	data, err := os.ReadFile(filepath.Join(dir, "docs/awareness/meta_principles.yaml"))
	if err != nil {
		t.Fatalf("read meta_principles.yaml: %v", err)
	}
	content := string(data)

	expectedPrinciples := []string{
		"meta.storage_is_not_semantic_authority",
		"meta.identity_computation_must_be_invariant",
		"meta.competing_writers_must_converge_or_be_fenced",
		"meta.structure_must_not_be_stripped_in_projection",
		"meta.fallback_must_degrade_semantics",
		"meta.authority_must_express_uncertainty",
		"meta.absence_scope_must_be_explicit",
		"meta.connection_errors_must_not_be_absorbed",
		"meta.assertions_must_carry_their_scope",
		"meta.write_creates_completion_obligation",
		"meta.half_done_must_not_look_done",
		"meta.silence_is_not_valid_for_unexpected",
		"meta.failure_response_must_contract_not_amplify",
		"meta.diagnostic_output_must_be_bounded",
		"meta.binding_outlives_evidence_until_invalidated",
		"meta.state_mutations_must_be_durably_committed_before_side_effects",
		"meta.critical_path_no_non_critical_dependency",
		"meta.circular_dependency_must_have_break_glass",
	}

	for _, id := range expectedPrinciples {
		if !strings.Contains(content, id) {
			t.Errorf("meta_principles.yaml missing %s", id)
		}
	}
}

func TestScaffoldProject_HooksExecutable(t *testing.T) {
	dir := t.TempDir()

	if _, err := scaffoldProject(dir, initOptions{hooks: true}); err != nil {
		t.Fatalf("scaffoldProject: %v", err)
	}

	hooks := []string{
		".claude/hooks/enforce-briefing.sh",
		".claude/hooks/record-briefing.sh",
		".claude/hooks/edit-check-guard.sh",
	}
	for _, rel := range hooks {
		info, err := os.Stat(filepath.Join(dir, rel))
		if err != nil {
			t.Errorf("hook %s not found: %v", rel, err)
			continue
		}
		if info.Mode()&0o111 == 0 {
			t.Errorf("hook %s is not executable (mode %o)", rel, info.Mode())
		}
	}
}

func TestScaffoldProject_MCPMergeOptIn(t *testing.T) {
	dir := t.TempDir()
	mcpPath := filepath.Join(dir, ".mcp.json")

	// Pre-existing config with another server — must be preserved, not clobbered.
	seed := `{"mcpServers":{"other":{"command":"other-mcp"}}}`
	if err := os.WriteFile(mcpPath, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	// mcp: false must NOT touch .mcp.json.
	if _, err := scaffoldProject(dir, initOptions{}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(mcpPath); strings.Contains(string(data), "sensei") {
		t.Fatal("mcp defaulted on — .mcp.json got a sensei entry without --mcp")
	}

	// mcp: true merges the sensei server, keeping the existing one.
	if _, err := scaffoldProject(dir, initOptions{mcp: true}); err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		MCPServers map[string]struct {
			Command string `json:"command"`
		} `json:"mcpServers"`
	}
	data, _ := os.ReadFile(mcpPath)
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf(".mcp.json is not valid JSON after merge: %v\n%s", err, data)
	}
	if _, ok := cfg.MCPServers["other"]; !ok {
		t.Error("merge clobbered the existing 'other' server")
	}
	if s, ok := cfg.MCPServers["sensei"]; !ok || s.Command == "" {
		t.Errorf("sensei server not added: %+v", cfg.MCPServers)
	}

	// Idempotent: a second run doesn't duplicate or clobber the sensei entry.
	before := data
	if created, err := scaffoldProject(dir, initOptions{mcp: true}); err != nil {
		t.Fatal(err)
	} else {
		for _, f := range created {
			if strings.HasSuffix(f, ".mcp.json") {
				t.Error("second --mcp run rewrote .mcp.json (not idempotent)")
			}
		}
	}
	if after, _ := os.ReadFile(mcpPath); string(after) != string(before) {
		t.Error(".mcp.json changed on the second --mcp run")
	}
}

func TestScaffoldProject_StarterCorpusValidatesCleanly(t *testing.T) {
	dir := t.TempDir()
	if _, err := scaffoldProject(dir, initOptions{}); err != nil {
		t.Fatalf("scaffoldProject: %v", err)
	}

	report, err := doValidate(
		dir,
		[]string{filepath.Join(dir, "docs", "awareness")},
		nil,
		[]string{dir},
		validateScopeLocal,
	)
	if err != nil {
		t.Fatalf("doValidate: %v", err)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("starter corpus must validate cleanly, got findings: %+v", report.Findings)
	}
}
