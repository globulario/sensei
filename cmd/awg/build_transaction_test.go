// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvaluateBuildTransactionFreshness_Current(t *testing.T) {
	stamp := []byte("format\tv1\nrepo\tawareness-graph\tabc\n")
	got := evaluateBuildTransactionFreshness(stamp, append([]byte(nil), stamp...))
	if got.level != auditPASS {
		t.Fatalf("level=%v, want PASS", got.level)
	}
	if got.summary != "current" {
		t.Fatalf("summary=%q, want current", got.summary)
	}
}

func TestRunRebuildCheck_AllowsAdvisoryCrossRepoTransactionDrift(t *testing.T) {
	agRepo := t.TempDir()
	svcRepo := t.TempDir()

	if err := os.MkdirAll(filepath.Join(agRepo, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(agRepo, "golang", "server", "embeddata"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFixtureYAMLs(filepath.Join("..", "..", "golang", "extractor", "testdata"), filepath.Join(agRepo, "docs", "awareness")); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(svcRepo, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(svcRepo, "docs", "intent"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, filepath.Join(svcRepo, "docs", "awareness", "namespaces.yaml"), "namespaces:\n  - id: globular.services\n    path: docs/awareness\n")
	writeRepoFile(t, filepath.Join(svcRepo, "docs", "awareness", "required_tests.yaml"), "required_tests:\n  - id: svc.test.one\n    title: Service test\n")

	initGitRepo(t, agRepo)
	initGitRepo(t, svcRepo)

	if code := runRebuild([]string{"--ag-repo", agRepo, "--services-repo", svcRepo, "--no-runtime-reload"}); code != 0 {
		t.Fatalf("initial rebuild code=%d, want 0", code)
	}

	txPath := defaultTransactionPath(agRepo)
	b, err := os.ReadFile(txPath)
	if err != nil {
		t.Fatalf("read transaction stamp: %v", err)
	}
	if !strings.Contains(string(b), "repo\tservices\t") {
		t.Fatalf("transaction stamp missing services head:\n%s", string(b))
	}

	writeRepoFile(t, filepath.Join(svcRepo, "docs", "awareness", "namespaces.yaml"), "namespaces:\n  - id: globular.services\n    path: docs/awareness\n  - id: globular.services.extra\n    path: docs/awareness/generated\n")
	commitAll(t, svcRepo, "change services namespace registry")

	if code := runRebuild([]string{"--check", "--ag-repo", agRepo, "--services-repo", svcRepo, "--no-runtime-reload"}); code != 0 {
		t.Fatalf("advisory transaction rebuild check code=%d, want 0", code)
	}
}

func TestRunRebuild_FailsClosedWhenCombinedSeedLosesServicesRepo(t *testing.T) {
	agRepo, svcRepo := setupSeedStatusRepos(t)
	if code := runRebuild([]string{"--ag-repo", agRepo, "--services-repo", svcRepo, "--no-runtime-reload"}); code != 0 {
		t.Fatalf("initial rebuild code=%d, want 0", code)
	}

	if code := runRebuild([]string{"--ag-repo", agRepo, "--services-repo", filepath.Join(agRepo, "not-services"), "--no-runtime-reload"}); code != 1 {
		t.Fatalf("rebuild without services repo code=%d, want 1", code)
	}
}

func writeRepoFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")
	commitAll(t, dir, "initial")
}

func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", msg)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(out))
	}
}
