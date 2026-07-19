// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanupLegacyBootstrapArtifacts_RemovesLegacyImportGraphsOnly(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"go_import_graph.yaml",
		"python_import_graph.yaml",
		"components.yaml",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := cleanupLegacyBootstrapArtifacts(dir)
	if err != nil {
		t.Fatalf("cleanupLegacyBootstrapArtifacts: %v", err)
	}
	if len(removed) != 2 {
		t.Fatalf("removed=%v want 2 legacy files", removed)
	}
	for _, name := range []string{"go_import_graph.yaml", "python_import_graph.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should be removed, err=%v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "components.yaml")); err != nil {
		t.Fatalf("components.yaml should remain: %v", err)
	}
}

func TestHasCuratedGenerated(t *testing.T) {
	dir := t.TempDir()
	if hasCuratedGenerated(dir) {
		t.Error("empty generated dir must not read as curated")
	}
	// A bootstrap-owned, unprefixed file is not a curated signal.
	if err := os.WriteFile(filepath.Join(dir, "components.yaml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if hasCuratedGenerated(dir) {
		t.Error("an unprefixed generated file must not count as curated")
	}
	// A targeted-extractor awareness_graph_* file marks a curated corpus, so
	// bootstrap must defer instead of clobbering it.
	if err := os.WriteFile(filepath.Join(dir, "awareness_graph_go_import_graph.yaml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !hasCuratedGenerated(dir) {
		t.Error("an awareness_graph_* file must mark the corpus curated")
	}
}

func TestBootstrapOwnedGenerated_CoversEmittedFiles(t *testing.T) {
	// The curated-repo deferral is only sound if every generated file bootstrap
	// emits is in the owned set — otherwise a stray file would still clobber a
	// curated corpus. Keep this list in sync with the extractor render sites.
	for _, name := range []string{
		"contracts.yaml", "rest_contracts.yaml", "components.yaml",
		"web_components.yaml", "contract_consumption.yaml",
		"source_symbols.yaml", "source_edges.yaml",
		"scip_symbols.yaml", "scip_references.yaml", "tests.yaml",
	} {
		if !bootstrapOwnedGenerated[name] {
			t.Errorf("%s is emitted by bootstrap but missing from bootstrapOwnedGenerated", name)
		}
	}
}

func TestSyncBootstrapScaffoldInstallsAgentAwarenessForExistingRepository(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}
	report := &bootstrapReport{}
	if err := syncBootstrapScaffold(root, report); err != nil {
		t.Fatalf("syncBootstrapScaffold: %v", err)
	}
	if len(report.scaffolded) == 0 {
		t.Fatal("expected managed skills to be installed")
	}
	for _, path := range []string{
		filepath.Join(".sensei", "skills", "sensei-import", "SKILL.md"),
		filepath.Join(".agents", "skills", "sensei-architect", "SKILL.md"),
		filepath.Join(".claude", "skills", "sensei-admission", "SKILL.md"),
	} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("read installed %s: %v", path, err)
		}
		text := string(data)
		if !strings.Contains(text, "sensei prepare-change") && !strings.Contains(text, "live_loaded") {
			t.Fatalf("installed %s lacks reconstructed-awareness workflow", path)
		}
	}
	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if !strings.Contains(string(agents), "## Sensei") {
		t.Fatal("AGENTS.md does not activate Sensei awareness")
	}
}

func TestRepairLegacyStarterTemplates_RefreshesUntouchedExamplesOnly(t *testing.T) {
	root := t.TempDir()
	if _, err := scaffoldProject(root, initOptions{}); err != nil {
		t.Fatalf("scaffoldProject: %v", err)
	}

	legacyInvariant := `invariants:
  - id: example.config_must_not_use_env_vars
    title: legacy
`
	invPath := filepath.Join(root, "docs", "awareness", "invariants.yaml")
	if err := os.WriteFile(invPath, []byte(legacyInvariant), 0o644); err != nil {
		t.Fatal(err)
	}
	customFailure := `failure_modes:
  - id: real.failure
    title: keep me
`
	fmPath := filepath.Join(root, "docs", "awareness", "failure_modes.yaml")
	if err := os.WriteFile(fmPath, []byte(customFailure), 0o644); err != nil {
		t.Fatal(err)
	}

	refreshed, err := repairLegacyStarterTemplates(root)
	if err != nil {
		t.Fatalf("repairLegacyStarterTemplates: %v", err)
	}
	if len(refreshed) != 1 || refreshed[0] != invPath {
		t.Fatalf("refreshed=%v want [%s]", refreshed, invPath)
	}
	invBytes, err := os.ReadFile(invPath)
	if err != nil {
		t.Fatal(err)
	}
	wantTmpl, err := templates.ReadFile("templates/awareness/invariants.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(invBytes) == legacyInvariant || string(invBytes) != string(wantTmpl) {
		t.Fatalf("legacy invariants template was not refreshed to the current template:\n%s", invBytes)
	}
	fmBytes, err := os.ReadFile(fmPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(fmBytes) != customFailure {
		t.Fatalf("custom failure_modes should remain untouched:\n%s", fmBytes)
	}
}
