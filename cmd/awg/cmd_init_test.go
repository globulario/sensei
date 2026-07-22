// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestScaffoldProject(t *testing.T) {
	dir := t.TempDir()

	created, err := scaffoldProject(dir, initOptions{hooks: true, claudeMD: true, agentsMD: true, cursor: true, skills: true})
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
		".sensei/skills/sensei-architect/SKILL.md",
		".sensei/skills/sensei-architect/.sensei-managed.json",
		".sensei/skills/sensei-import/SKILL.md",
		".sensei/skills/sensei-admission/SKILL.md",
		".sensei/skills/sensei-closure/SKILL.md",
		".sensei/skills/sensei-benchmark/SKILL.md",
		".agents/skills/sensei-architect/SKILL.md",
		".agents/skills/sensei-admission/SKILL.md",
		".claude/skills/sensei-architect/SKILL.md",
		".claude/skills/sensei-benchmark/SKILL.md",
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

	// Verify CLAUDE.md has the Sensei section.
	claudeData, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	if !strings.Contains(string(claudeData), "## Sensei") {
		t.Error("CLAUDE.md missing Sensei section")
	}

	// Verify idempotent — second run should not overwrite.
	created2, err := scaffoldProject(dir, initOptions{hooks: true, claudeMD: true, agentsMD: true, cursor: true, skills: true})
	if err != nil {
		t.Fatalf("scaffoldProject (2nd run): %v", err)
	}
	if len(created2) != 0 {
		t.Errorf("second scaffoldProject created %d files, expected 0 (idempotent)", len(created2))
	}
}

func TestScaffoldProject_SkillsOptOut(t *testing.T) {
	dir := t.TempDir()

	code := runInit([]string{
		"--dir", dir,
		"--skills=false",
		"--hooks=false",
		"--claude-md=false",
		"--agents-md=false",
		"--cursor=false",
	})
	if code != 0 {
		t.Fatalf("runInit returned %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, ".sensei", "skills", "sensei-architect", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("architect skill installed despite --skills=false: %v", err)
	}
}

func TestScaffoldProject_InitMCPInstallsSkillsAndMCP(t *testing.T) {
	dir := t.TempDir()

	code := runInit([]string{
		"--dir", dir,
		"--mcp",
		"--hooks=false",
		"--claude-md=false",
		"--agents-md=false",
		"--cursor=false",
	})
	if code != 0 {
		t.Fatalf("runInit returned %d", code)
	}
	for _, rel := range []string{
		".sensei/skills/sensei-architect/SKILL.md",
		".sensei/skills/sensei-import/SKILL.md",
		".sensei/skills/sensei-admission/SKILL.md",
		".sensei/skills/sensei-closure/SKILL.md",
		".sensei/skills/sensei-benchmark/SKILL.md",
		".agents/skills/sensei-architect/SKILL.md",
		".agents/skills/sensei-admission/SKILL.md",
		".claude/skills/sensei-architect/SKILL.md",
		".claude/skills/sensei-closure/SKILL.md",
		".mcp.json",
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatalf("expected %s: %v", rel, err)
		}
	}
}

func TestScaffoldProject_SkillUpdateSafety(t *testing.T) {
	dir := t.TempDir()
	if _, err := scaffoldProject(dir, initOptions{skills: true}); err != nil {
		t.Fatalf("scaffoldProject: %v", err)
	}

	skillPath := filepath.Join(dir, ".sensei", "skills", "sensei-architect", "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("local user edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := scaffoldProjectWithReport(dir, initOptions{skills: true})
	if err != nil {
		t.Fatalf("scaffoldProjectWithReport: %v", err)
	}
	if len(report.notices) == 0 || !strings.Contains(strings.Join(report.notices, "\n"), "modified locally") {
		t.Fatalf("expected local modification notice, got %#v", report.notices)
	}
	data, _ := os.ReadFile(skillPath)
	if string(data) != "local user edit\n" {
		t.Fatalf("local skill edit was overwritten: %q", data)
	}

	if _, err := scaffoldProjectWithReport(dir, initOptions{skills: true, skillsForce: true}); err != nil {
		t.Fatalf("force refresh: %v", err)
	}
	data, _ = os.ReadFile(skillPath)
	if strings.Contains(string(data), "local user edit") || !strings.Contains(string(data), "name: sensei-architect") {
		t.Fatalf("force refresh did not restore bundled skill: %q", data)
	}
}

func TestScaffoldProject_UntouchedManagedSkillCanUpdate(t *testing.T) {
	dir := t.TempDir()
	if _, err := scaffoldProject(dir, initOptions{skills: true}); err != nil {
		t.Fatalf("scaffoldProject: %v", err)
	}

	manifestPath := filepath.Join(dir, ".sensei", "skills", "sensei-architect", skillManifestName)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest skillManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Version = "1900.01.01"
	out, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(manifestPath, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := scaffoldProjectWithReport(dir, initOptions{skills: true})
	if err != nil {
		t.Fatalf("scaffoldProjectWithReport: %v", err)
	}
	if len(report.notices) != 0 {
		t.Fatalf("untouched managed update should not warn: %#v", report.notices)
	}
	data, _ = os.ReadFile(manifestPath)
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version == "1900.01.01" {
		t.Fatal("untouched managed skill manifest was not updated")
	}
}

func TestScaffoldProject_UnrelatedAgentSkillsUntouched(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, ".agents", "skills", "other-skill")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	otherSkill := filepath.Join(other, "SKILL.md")
	if err := os.WriteFile(otherSkill, []byte("---\nname: other\ndescription: keep me\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := scaffoldProject(dir, initOptions{skills: true, agentsMD: true}); err != nil {
		t.Fatalf("scaffoldProject: %v", err)
	}
	data, _ := os.ReadFile(otherSkill)
	if !strings.Contains(string(data), "keep me") {
		t.Fatalf("unrelated skill changed: %q", data)
	}
}

func TestBuiltSenseiBinaryInitializesSkillsOutsideSourceTree(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	bin := filepath.Join(t.TempDir(), "sensei")
	cmd := exec.Command("go", "build", "-ldflags=-s -w", "-o", bin, "./cmd/awg")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build sensei: %v\n%s", err, out)
	}

	external := filepath.Join(t.TempDir(), "external-repo")
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(bin, "init", "--dir", external, "--hooks=false", "--claude-md=false", "--agents-md=false", "--cursor=false")
	run.Dir = external
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("built sensei init: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(external, ".sensei", "skills", "sensei-architect", "SKILL.md")); err != nil {
		t.Fatalf("built binary did not install embedded skill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(external, ".sensei", "skills", "sensei-benchmark", "SKILL.md")); err != nil {
		t.Fatalf("built binary did not install benchmark skill: %v", err)
	}
}

func TestSenseiInitInstallsAllManagedSkills(t *testing.T) {
	dir := t.TempDir()
	if _, err := scaffoldProject(dir, initOptions{skills: true}); err != nil {
		t.Fatalf("scaffoldProject: %v", err)
	}
	for _, skill := range builtinSkills {
		for _, target := range skill.Targets {
			if _, err := os.Stat(filepath.Join(dir, target, "SKILL.md")); err != nil {
				t.Fatalf("expected %s in %s: %v", skill.Name, target, err)
			}
		}
	}
}

func TestSenseiInitCreatesManagedManifestForNewSkills(t *testing.T) {
	dir := t.TempDir()
	if _, err := scaffoldProject(dir, initOptions{skills: true}); err != nil {
		t.Fatalf("scaffoldProject: %v", err)
	}
	for _, name := range []string{"sensei-admission", "sensei-closure", "sensei-benchmark"} {
		manifest := readInstalledSkillManifestForTest(t, filepath.Join(dir, ".sensei", "skills", name, skillManifestName))
		if manifest.ManagedBy != "sensei init" || manifest.Skill != name || manifest.Version != builtinSkillVersion {
			t.Fatalf("%s manifest mismatch: %#v", name, manifest)
		}
		if len(manifest.Files) == 0 {
			t.Fatalf("%s manifest has no files", name)
		}
	}
}

func TestManagedManifestHashesEveryInstalledFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := scaffoldProject(dir, initOptions{skills: true}); err != nil {
		t.Fatalf("scaffoldProject: %v", err)
	}
	for _, skill := range builtinSkills {
		base := filepath.Join(dir, ".sensei", "skills", skill.Name)
		manifest := readInstalledSkillManifestForTest(t, filepath.Join(base, skillManifestName))
		files, err := bundledSkillFiles(skill)
		if err != nil {
			t.Fatal(err)
		}
		if len(manifest.Files) != len(files) {
			t.Fatalf("%s manifest file count=%d want %d", skill.Name, len(manifest.Files), len(files))
		}
		for rel, digest := range manifest.Files {
			data, err := os.ReadFile(filepath.Join(base, filepath.FromSlash(rel)))
			if err != nil {
				t.Fatalf("%s read %s: %v", skill.Name, rel, err)
			}
			if sha256Hex(data) != digest {
				t.Fatalf("%s digest mismatch for %s", skill.Name, rel)
			}
		}
	}
}

func TestManagedManifestOrderIsDeterministic(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	if _, err := scaffoldProject(first, initOptions{skills: true}); err != nil {
		t.Fatalf("first scaffoldProject: %v", err)
	}
	if _, err := scaffoldProject(second, initOptions{skills: true}); err != nil {
		t.Fatalf("second scaffoldProject: %v", err)
	}
	for _, skill := range builtinSkills {
		a, err := os.ReadFile(filepath.Join(first, ".sensei", "skills", skill.Name, skillManifestName))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(second, ".sensei", "skills", skill.Name, skillManifestName))
		if err != nil {
			t.Fatal(err)
		}
		if string(a) != string(b) {
			t.Fatalf("%s manifest differs between installs", skill.Name)
		}
	}
}

func TestSecondInitIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	if _, err := scaffoldProject(dir, initOptions{skills: true}); err != nil {
		t.Fatalf("scaffoldProject: %v", err)
	}
	created, err := scaffoldProject(dir, initOptions{skills: true})
	if err != nil {
		t.Fatalf("second scaffoldProject: %v", err)
	}
	for _, path := range created {
		if strings.Contains(path, string(filepath.Separator)+"skills"+string(filepath.Separator)) {
			t.Fatalf("second init rewrote skill path %s", path)
		}
	}
}

func TestNewSkillsInstallWithoutForce(t *testing.T) {
	dir := t.TempDir()
	for _, skill := range builtinSkills[:2] {
		files, err := bundledSkillFiles(skill)
		if err != nil {
			t.Fatal(err)
		}
		if _, notice, err := syncBuiltinSkill(dir, skill, filepath.Join(".sensei", "skills", skill.Name), files, false); err != nil || notice != "" {
			t.Fatalf("seed old skill %s notice=%q err=%v", skill.Name, notice, err)
		}
	}
	if _, err := scaffoldProject(dir, initOptions{skills: true}); err != nil {
		t.Fatalf("scaffoldProject: %v", err)
	}
	for _, name := range []string{"sensei-admission", "sensei-closure", "sensei-benchmark"} {
		if _, err := os.Stat(filepath.Join(dir, ".sensei", "skills", name, "SKILL.md")); err != nil {
			t.Fatalf("new skill %s was not installed without force: %v", name, err)
		}
	}
}

func TestLocalManagedEditIsPreservedByDefault(t *testing.T) {
	dir := t.TempDir()
	if _, err := scaffoldProject(dir, initOptions{skills: true}); err != nil {
		t.Fatalf("scaffoldProject: %v", err)
	}
	skillPath := filepath.Join(dir, ".sensei", "skills", "sensei-admission", "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("local admission edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := scaffoldProjectWithReport(dir, initOptions{skills: true})
	if err != nil {
		t.Fatalf("scaffoldProjectWithReport: %v", err)
	}
	if len(report.notices) == 0 || !strings.Contains(strings.Join(report.notices, "\n"), "modified locally") {
		t.Fatalf("expected local modification notice, got %#v", report.notices)
	}
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "local admission edit\n" {
		t.Fatalf("local managed edit was overwritten: %q", data)
	}
}

func TestSkillsForceReplacesLocalManagedEdit(t *testing.T) {
	dir := t.TempDir()
	if _, err := scaffoldProject(dir, initOptions{skills: true}); err != nil {
		t.Fatalf("scaffoldProject: %v", err)
	}
	skillPath := filepath.Join(dir, ".sensei", "skills", "sensei-closure", "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("local closure edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := scaffoldProjectWithReport(dir, initOptions{skills: true, skillsForce: true}); err != nil {
		t.Fatalf("force scaffoldProjectWithReport: %v", err)
	}
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "local closure edit") || !strings.Contains(string(data), "name: sensei-closure") {
		t.Fatalf("force did not replace local managed edit: %q", data)
	}
}

func TestUnmanagedUserSkillIsUntouched(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, ".sensei", "skills", "user-skill")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	otherSkill := filepath.Join(other, "SKILL.md")
	if err := os.WriteFile(otherSkill, []byte("---\nname: user-skill\ndescription: keep me\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := scaffoldProject(dir, initOptions{skills: true}); err != nil {
		t.Fatalf("scaffoldProject: %v", err)
	}
	data, err := os.ReadFile(otherSkill)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "keep me") {
		t.Fatalf("unmanaged user skill changed: %q", data)
	}
}

func TestExistingArchitectAndImportInstallBehaviorRemainsCompatible(t *testing.T) {
	dir := t.TempDir()
	if _, err := scaffoldProject(dir, initOptions{skills: true}); err != nil {
		t.Fatalf("scaffoldProject: %v", err)
	}
	for _, name := range []string{"sensei-architect", "sensei-import"} {
		manifest := readInstalledSkillManifestForTest(t, filepath.Join(dir, ".sensei", "skills", name, skillManifestName))
		if manifest.Skill != name || manifest.Version != builtinSkillVersion {
			t.Fatalf("%s manifest mismatch: %#v", name, manifest)
		}
	}
}

func readInstalledSkillManifestForTest(t *testing.T, path string) skillManifest {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest skillManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
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

func TestScaffoldProject_MCPMigratesLegacyServerName(t *testing.T) {
	dir := t.TempDir()
	mcpPath := filepath.Join(dir, ".mcp.json")

	seed := `{"mcpServers":{"awg":{"command":"awareness-mcp","args":["--awareness-addr","localhost:10120"]},"other":{"command":"other-mcp"}}}`
	if err := os.WriteFile(mcpPath, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := scaffoldProject(dir, initOptions{mcp: true}); err != nil {
		t.Fatal(err)
	}

	var cfg struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	data, _ := os.ReadFile(mcpPath)
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf(".mcp.json is not valid JSON after migration: %v\n%s", err, data)
	}
	if _, ok := cfg.MCPServers["awg"]; ok {
		t.Fatalf("legacy awg MCP server was not removed: %+v", cfg.MCPServers)
	}
	if s, ok := cfg.MCPServers["sensei"]; !ok || s.Command != "awareness-mcp" {
		t.Fatalf("legacy awg MCP server was not migrated to sensei: %+v", cfg.MCPServers)
	}
	if _, ok := cfg.MCPServers["other"]; !ok {
		t.Fatal("migration clobbered the existing 'other' server")
	}
}

func TestScaffoldProject_MCPRemovesLegacyServerWhenSenseiExists(t *testing.T) {
	dir := t.TempDir()
	mcpPath := filepath.Join(dir, ".mcp.json")

	seed := `{"mcpServers":{"sensei":{"command":"custom-awareness-mcp"},"awg":{"command":"awareness-mcp"},"awareness-graph":{"command":"old-awareness-mcp"},"other":{"command":"other-mcp"}}}`
	if err := os.WriteFile(mcpPath, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

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
		t.Fatalf(".mcp.json is not valid JSON after cleanup: %v\n%s", err, data)
	}
	if _, ok := cfg.MCPServers["awg"]; ok {
		t.Fatalf("legacy awg MCP server was not removed: %+v", cfg.MCPServers)
	}
	if _, ok := cfg.MCPServers["awareness-graph"]; ok {
		t.Fatalf("legacy awareness-graph MCP server was not removed: %+v", cfg.MCPServers)
	}
	if s, ok := cfg.MCPServers["sensei"]; !ok || s.Command != "custom-awareness-mcp" {
		t.Fatalf("existing sensei MCP server was clobbered: %+v", cfg.MCPServers)
	}
	if _, ok := cfg.MCPServers["other"]; !ok {
		t.Fatal("cleanup clobbered the existing 'other' server")
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
