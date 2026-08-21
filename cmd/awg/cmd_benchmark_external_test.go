// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/benchmark"
	"gopkg.in/yaml.v3"
)

func TestBenchmarkFreezeRequiresTaskRepoOracleAndOutput(t *testing.T) {
	if code := runBenchmarkFreezeExternal(nil); code != 2 {
		t.Fatalf("expected usage exit 2, got %d", code)
	}
}

func TestBenchmarkReconstructRequiresFrozenWorkspace(t *testing.T) {
	if code := runBenchmarkReconstruct(nil); code != 2 {
		t.Fatalf("expected usage exit 2, got %d", code)
	}
}

func TestBenchmarkEvaluateRequiresOracleReviewAndMapping(t *testing.T) {
	if code := runBenchmarkEvaluateExternal(nil); code != 2 {
		t.Fatalf("expected usage exit 2, got %d", code)
	}
}

func TestBenchmarkStatusRequiresWorkspace(t *testing.T) {
	if code := runBenchmarkStatusExternal(nil); code != 2 {
		t.Fatalf("expected usage exit 2, got %d", code)
	}
}

// frozenWorkspaceWithUnboundAuthority builds a real frozen benchmark workspace
// whose authority could not be bound (no --sensei-repo at freeze time), so any
// replay against it is authority_unverifiable and therefore not comparable.
func frozenWorkspaceWithUnboundAuthority(t *testing.T) string {
	t.Helper()
	gitRun := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	repo := t.TempDir()
	gitRun(repo, "init")
	gitRun(repo, "config", "user.email", "test@example.com")
	gitRun(repo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(repo, "add", "main.go")
	gitRun(repo, "commit", "-m", "base")
	head, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	taskPath := filepath.Join(dir, "task.yaml")
	oraclePath := filepath.Join(dir, "oracle.yaml")
	task, err := yaml.Marshal(map[string]interface{}{"architecture_benchmark_task": benchmark.Task{
		SchemaVersion:        benchmark.SchemaVersion,
		TaskID:               "fixture-task",
		RepositoryID:         "fixture",
		RepositoryDomain:     "github.com/example/fixture",
		BaseRevision:         strings.TrimSpace(string(head)),
		BaseRevisionStatus:   architecture.RevisionResolved,
		TaskClass:            "modify_fixture",
		RiskClass:            "architecture_sensitive",
		AccessMode:           benchmark.AccessWrite,
		DirectionRequirement: benchmark.DirectionPreserve,
		TaskText:             "Preserve fixture behavior.",
		InitialScope:         benchmark.Scope{Files: []string{"main.go"}},
		AllowedSources:       []string{benchmark.AllowedSourceSource, benchmark.AllowedSourceTests},
		ProhibitedSources:    []string{"network", "future_commits"},
		ExpectedActionMode:   benchmark.ExpectedModeModify,
	}})
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := yaml.Marshal(map[string]interface{}{"architecture_benchmark_oracle": benchmark.Oracle{
		SchemaVersion: benchmark.SchemaVersion, TaskID: "fixture-task", RepositoryID: "fixture",
		OracleKind: "git_revision", OraclePatchSHA256: strings.Repeat("b", 64),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(taskPath, task, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oraclePath, oracle, 0o644); err != nil {
		t.Fatal(err)
	}

	workspace := filepath.Join(t.TempDir(), "workspace")
	// SenseiRepo deliberately empty: the freeze records a TYPED unavailable
	// authority rather than silently omitting one.
	if _, _, err := benchmark.Freeze(benchmark.FreezeOptions{
		TaskPath: taskPath, SourceRepo: repo, OraclePath: oraclePath, OutputDir: workspace,
	}); err != nil {
		t.Fatal(err)
	}
	return workspace
}

// TestBenchmarkStatusReplayGateAppliesToEveryFormat is the regression for a
// gate that guarded only human-readable output.
//
// The fail-closed exit exists so a harness cannot scrape numbers from a replay
// that ran under changed authority — and a harness is exactly the caller that
// passes --format json. Guarding only the text branch therefore exempted the
// single caller the gate was written to stop, while still reporting the
// guarantee as implemented.
func TestBenchmarkStatusReplayGateAppliesToEveryFormat(t *testing.T) {
	workspace := frozenWorkspaceWithUnboundAuthority(t)
	senseiRepo := t.TempDir() // not a git checkout: replay authority is unverifiable

	for _, format := range []string{"text", "json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			code := runBenchmarkStatusExternal([]string{
				"--workspace", workspace, "--sensei-repo", senseiRepo, "--format", format,
			})
			if code != 3 {
				t.Fatalf("--format %s exit = %d, want 3; an uncertifiable replay must fail closed in every format", format, code)
			}
		})
	}

	// Without --sensei-repo nothing is claimed about replay, so the gate must
	// not fire: refusing to print status is not the same as refusing to certify.
	for _, format := range []string{"text", "json", "yaml"} {
		t.Run("ungated_"+format, func(t *testing.T) {
			if code := runBenchmarkStatusExternal([]string{"--workspace", workspace, "--format", format}); code != 0 {
				t.Fatalf("--format %s without --sensei-repo exit = %d, want 0", format, code)
			}
		})
	}
}
