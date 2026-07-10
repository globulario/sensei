// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveServicesRepo_FindsSiblingFromAwarenessGraphRepo(t *testing.T) {
	root := t.TempDir()
	agRepo := filepath.Join(root, "awareness-graph")
	servicesRepo := filepath.Join(root, "services")

	if err := os.MkdirAll(filepath.Join(agRepo, "golang", "server", "embeddata"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(servicesRepo, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(servicesRepo, "docs", "awareness", "namespaces.yaml"), []byte("namespaces: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Chdir(prev)
	}()
	if err := os.Chdir(agRepo); err != nil {
		t.Fatal(err)
	}

	got, err := resolveServicesRepo("")
	if err != nil {
		t.Fatalf("resolveServicesRepo: %v", err)
	}
	if got != servicesRepo {
		t.Fatalf("resolveServicesRepo = %q, want %q", got, servicesRepo)
	}
}

func TestCollectInputDirs_PrefersLocalGeneratedForAwarenessGraph(t *testing.T) {
	root := t.TempDir()
	agRepo := filepath.Join(root, "awareness-graph")
	svcRepo := filepath.Join(root, "services")

	for _, dir := range []string{
		filepath.Join(agRepo, "docs", "awareness"),
		filepath.Join(agRepo, "docs", "awareness", "generated"),
		filepath.Join(agRepo, "eval", "multi-swe-bench", "contracts"),
		filepath.Join(agRepo, "eval", "multi-swe-bench", "notes", "learning_events"),
		filepath.Join(svcRepo, "docs", "awareness"),
		filepath.Join(svcRepo, "docs", "awareness", "generated"),
		filepath.Join(svcRepo, "docs", "intent"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got, intentDir, err := collectInputDirs(svcRepo, agRepo)
	if err != nil {
		t.Fatalf("collectInputDirs: %v", err)
	}
	want := []string{
		filepath.Join(agRepo, "docs", "awareness"),
		filepath.Join(agRepo, "docs", "awareness", "generated"),
		filepath.Join(agRepo, "eval", "multi-swe-bench", "contracts"),
		filepath.Join(agRepo, "eval", "multi-swe-bench", "notes", "learning_events"),
		filepath.Join(svcRepo, "docs", "awareness"),
		filepath.Join(svcRepo, "docs", "awareness", "generated"),
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("collectInputDirs = %q, want %q", got, want)
	}
	if intentDir != filepath.Join(svcRepo, "docs", "intent") {
		t.Fatalf("intentDir = %q, want %q", intentDir, filepath.Join(svcRepo, "docs", "intent"))
	}
}

func TestGenerateNT_FromAwarenessGraphRepo_RetainsPairedServicesAuthority(t *testing.T) {
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	agRepo, err := resolveAGRepo("", "")
	if err != nil {
		t.Fatalf("resolveAGRepo: %v", err)
	}
	if agRepo == "" {
		t.Skip("awareness-graph repo root not discoverable from test cwd")
	}
	// Standalone build: when awareness-graph carries its own namespace registry
	// it is the self-only (open-source) build, whose seed intentionally omits the
	// paired services corpus. This combined-authority contract only applies to the
	// awareness-graph + services build.
	if _, err := os.Stat(filepath.Join(agRepo, "docs", "awareness", "namespaces.yaml")); err == nil {
		t.Skip("standalone awareness-graph (self-only registry present): paired services authority not expected")
	}
	defer func() {
		_ = os.Chdir(prev)
	}()
	if err := os.Chdir(agRepo); err != nil {
		t.Fatal(err)
	}

	svcRepo, err := resolveServicesRepo("")
	if err != nil {
		t.Fatalf("resolveServicesRepo: %v", err)
	}
	if svcRepo == "" {
		t.Skip("sibling services repo not present")
	}

	inputDirs, intentDir, err := collectInputDirs(svcRepo, agRepo)
	if err != nil {
		t.Fatalf("collectInputDirs: %v", err)
	}
	ntBytes, _, _, err := generateNT(inputDirs, intentDir, svcRepo, agRepo)
	if err != nil {
		t.Fatalf("generateNT: %v", err)
	}

	for _, want := range []string{
		"authority.repository_artifact_metadata",
		"globular.repair.repository_artifact_lifecycle_stuck",
		"globular.pattern.repository_metadata_authority",
	} {
		if !bytes.Contains(ntBytes, []byte(want)) {
			t.Fatalf("generated corpus missing %q", want)
		}
	}
	if !bytes.Contains(ntBytes, []byte("golang/repository/repository_server/publish_workflow.go")) {
		t.Fatal("generated corpus missing publish_workflow.go anchors")
	}
	if !strings.Contains(string(ntBytes), "<https://globular.io/awareness#repairPlan/globular.repair.repository_artifact_lifecycle_stuck> <https://globular.io/awareness#coversPath> \"golang/repository/repository_server/\"") {
		t.Fatal("generated corpus missing repository repair-plan coverage path")
	}
}

func TestFilteredServicesGeneratedDir_ExcludesAwarenessGraphArtifacts(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "services", "docs", "awareness", "generated")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	for name := range map[string]string{
		"awareness_graph_code_symbols.yaml":   "skip",
		"awareness_graph_code_edges.yaml":     "skip",
		"platform_repository_code_edges.yaml": "keep",
	} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	relSub := filepath.Join("docs", "awareness", "generated")
	filtered, stripPrefix, cleanup, err := filteredServicesGeneratedDir(src, relSub)
	if err != nil {
		t.Fatalf("filteredServicesGeneratedDir: %v", err)
	}
	defer cleanup()

	// Determinism contract: the staged dir mirrors relSub under the temp root,
	// and the temp root is returned as a strip prefix — so stripping it yields a
	// stable repo-relative authoredIn (relSub/<f>), never a per-run /tmp path.
	if stripPrefix == "" {
		t.Fatal("filteredServicesGeneratedDir must return a non-empty strip prefix (the temp root)")
	}
	if filepath.Join(stripPrefix, relSub) != filepath.Clean(filtered) {
		t.Fatalf("staged dir %q is not %q mirrored under strip prefix %q", filtered, relSub, stripPrefix)
	}

	entries, err := os.ReadDir(filtered)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if strings.Join(names, "\n") != "platform_repository_code_edges.yaml" {
		t.Fatalf("filtered entries = %v, want only platform_repository_code_edges.yaml", names)
	}
}
