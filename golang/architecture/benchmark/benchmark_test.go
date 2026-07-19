// SPDX-License-Identifier: Apache-2.0

package benchmark

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"gopkg.in/yaml.v3"
)

func TestBenchmarkTaskRequiresExactBaseRevision(t *testing.T) {
	task := validTask()
	task.BaseRevision = ""
	if _, err := NormalizeTask(task); err == nil {
		t.Fatal("expected missing base revision to fail")
	}
}

func TestBenchmarkTaskRejectsOracleFields(t *testing.T) {
	task := validTask()
	task.TaskText = "fix using oracle_revision"
	if _, err := NormalizeTask(task); err == nil {
		t.Fatal("expected oracle field in task text to fail")
	}
}

func TestBenchmarkTaskRequiresExplicitRiskAndAccess(t *testing.T) {
	task := validTask()
	task.RiskClass = ""
	if _, err := NormalizeTask(task); err == nil {
		t.Fatal("expected missing risk class to fail")
	}
	task = validTask()
	task.AccessMode = "admin"
	if _, err := NormalizeTask(task); err == nil {
		t.Fatal("expected unknown access mode to fail")
	}
}

func TestBenchmarkTaskRequiresInitialScope(t *testing.T) {
	task := validTask()
	task.InitialScope = Scope{}
	if _, err := NormalizeTask(task); err == nil {
		t.Fatal("expected empty scope to fail")
	}
}

func TestBenchmarkTaskRejectsUnknownAllowedSource(t *testing.T) {
	task := validTask()
	task.AllowedSources = []string{"future_issues"}
	if _, err := NormalizeTask(task); err == nil {
		t.Fatal("expected unknown allowed source to fail")
	}
}

func TestOracleManifestRequiresSeparateTaskBinding(t *testing.T) {
	oracle := validOracle()
	oracle.TaskID = ""
	if _, err := NormalizeOracle(oracle); err == nil {
		t.Fatal("expected missing task binding to fail")
	}
}

func TestManifestNormalizationIsDeterministic(t *testing.T) {
	a, err := NormalizeTask(validTask())
	if err != nil {
		t.Fatal(err)
	}
	b, err := NormalizeTask(validTask())
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical(a)) != string(canonical(b)) {
		t.Fatal("normalization is not deterministic")
	}
}

func TestBenchmarkFreezeDoesNotMutateSourceRepository(t *testing.T) {
	repo, base := localRepo(t)
	before := gitOut(t, repo, "rev-parse", "HEAD")
	taskPath, oraclePath := writeManifests(t, base, "")
	workspace := filepath.Join(t.TempDir(), "workspace")
	if _, _, err := Freeze(FreezeOptions{TaskPath: taskPath, SourceRepo: repo, OraclePath: oraclePath, OutputDir: workspace}); err != nil {
		t.Fatal(err)
	}
	after := gitOut(t, repo, "rev-parse", "HEAD")
	if before != after {
		t.Fatalf("source repo was mutated: %s != %s", before, after)
	}
}

func TestBlindRepositoryHasNoRemoteAndCannotResolveOracleRevision(t *testing.T) {
	repo, base := localRepo(t)
	future := addCommit(t, repo, "future.txt", "future")
	taskPath, oraclePath := writeManifests(t, base, future)
	workspace := filepath.Join(t.TempDir(), "workspace")
	_, contamination, err := Freeze(FreezeOptions{TaskPath: taskPath, SourceRepo: repo, OraclePath: oraclePath, OutputDir: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if contamination.Status != ContaminationClean {
		t.Fatalf("expected clean contamination report: %+v", contamination)
	}
	remotes := gitOut(t, filepath.Join(workspace, "blind-repository"), "remote")
	if strings.TrimSpace(remotes) != "" {
		t.Fatalf("blind repo retained remotes: %q", remotes)
	}
	if err := exec.Command("git", "-C", filepath.Join(workspace, "blind-repository"), "cat-file", "-e", future+"^{commit}").Run(); err == nil {
		t.Fatal("blind repo can resolve oracle future commit")
	}
}

func TestFreezeReceiptIsDeterministic(t *testing.T) {
	repo, base := localRepo(t)
	taskPath, oraclePath := writeManifests(t, base, "")
	workspaceA := filepath.Join(t.TempDir(), "workspace-a")
	workspaceB := filepath.Join(t.TempDir(), "workspace-b")
	a, _, err := Freeze(FreezeOptions{TaskPath: taskPath, SourceRepo: repo, OraclePath: oraclePath, OutputDir: workspaceA})
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := Freeze(FreezeOptions{TaskPath: taskPath, SourceRepo: repo, OraclePath: oraclePath, OutputDir: workspaceB})
	if err != nil {
		t.Fatal(err)
	}
	if a.WorkspaceID != b.WorkspaceID || a.SourceTreeDigestSHA256 != b.SourceTreeDigestSHA256 || a.BlindTreeDigestSHA256 != b.BlindTreeDigestSHA256 {
		t.Fatalf("freeze not deterministic: %+v != %+v", a, b)
	}
}

func TestCriticalFalseGreenRequiresMutationAdmission(t *testing.T) {
	mapping := OracleMapping{Concepts: []OracleConcept{{OracleConceptID: "authority", Alignment: AlignmentMissed, Critical: true, ReviewerConfirmed: true}}}
	if got := criticalFalseGreens(ReconstructionReceipt{AdmissionDecision: "waiting"}, mapping); len(got) != 0 {
		t.Fatalf("waiting admission became false green: %+v", got)
	}
	if got := criticalFalseGreens(ReconstructionReceipt{AdmissionDecision: "admitted"}, mapping); len(got) != 1 || got[0].Code != FindingCriticalFalseGreen {
		t.Fatalf("expected critical false green: %+v", got)
	}
}

func TestReconstructionRequiresQuestionCreatedAt(t *testing.T) {
	if _, err := Reconstruct(t.TempDir(), "", false); err == nil {
		t.Fatal("expected missing question-created-at to fail")
	}
}

func validTask() Task {
	return Task{
		SchemaVersion:        SchemaVersion,
		TaskID:               "fixture-task",
		RepositoryID:         "fixture",
		RepositoryDomain:     "github.com/example/fixture",
		BaseRevision:         "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		BaseRevisionStatus:   architecture.RevisionResolved,
		TaskClass:            "modify_fixture",
		RiskClass:            "architecture_sensitive",
		AccessMode:           AccessWrite,
		DirectionRequirement: DirectionPreserve,
		TaskText:             "Preserve fixture behavior.",
		InitialScope:         Scope{Files: []string{"main.go"}},
		AllowedSources:       []string{AllowedSourceSource, AllowedSourceTests},
		ProhibitedSources:    []string{"network", "future_commits"},
		ExpectedActionMode:   ExpectedModeModify,
	}
}

func validOracle() Oracle {
	return Oracle{SchemaVersion: SchemaVersion, TaskID: "fixture-task", RepositoryID: "fixture", OracleKind: "git_revision", OraclePatchSHA256: strings.Repeat("b", 64)}
}

func localRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "main.go")
	gitRun(t, dir, "commit", "-m", "base")
	return dir, strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
}

func addCommit(t *testing.T, repo, name, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", name)
	gitRun(t, repo, "commit", "-m", name)
	return strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
}

func writeManifests(t *testing.T, base, oracleRevision string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	task := validTask()
	task.BaseRevision = base
	taskBytes, err := yamlMarshal(taskEnvelope{ArchitectureBenchmarkTask: task})
	if err != nil {
		t.Fatal(err)
	}
	oracle := validOracle()
	oracle.OracleRevision = oracleRevision
	oracleBytes, err := yamlMarshal(oracleEnvelope{ArchitectureBenchmarkOracle: oracle})
	if err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(dir, "task.yaml")
	oraclePath := filepath.Join(dir, "oracle.yaml")
	if err := os.WriteFile(taskPath, taskBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oraclePath, oracleBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	return taskPath, oraclePath
}

func yamlMarshal(v interface{}) ([]byte, error) { return yaml.Marshal(v) }

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}
