// SPDX-License-Identifier: AGPL-3.0-only

package benchmark

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

// --- real Reconstruct() pipeline tests ---
//
// These use a tiny test-double "fakesensei" binary (testdata/fakesensei),
// not the real cmd/awg CLI: cmd/awg depends on cgo/tree-sitter and this
// repository has already had to fix a "nested go build inside go test"
// performance regression once (see cmd/awg/hook_lifecycle_integration_test.go).
// The fake binary proves this package's OWN orchestration (subprocess
// sequencing, error->Limitation mapping, YAML parsing, digesting) is
// correct and deterministic; genuine end-to-end proof against the real
// sensei CLI is exercised separately, outside the automated test suite.

var (
	fakeSenseiOnce sync.Once
	fakeSenseiPath string
	fakeSenseiErr  error
)

func buildFakeSensei(t *testing.T) string {
	t.Helper()
	fakeSenseiOnce.Do(func() {
		dir, err := os.MkdirTemp("", "fakesensei-*")
		if err != nil {
			fakeSenseiErr = err
			return
		}
		bin := filepath.Join(dir, "fakesensei")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", bin, "./testdata/fakesensei")
		out, err := cmd.CombinedOutput()
		if err != nil {
			fakeSenseiErr = fmt.Errorf("build fakesensei: %v: %s", err, out)
			return
		}
		fakeSenseiPath = bin
	})
	if fakeSenseiErr != nil {
		t.Fatal(fakeSenseiErr)
	}
	return fakeSenseiPath
}

// useFakeSensei points senseiBinaryPath at the fake binary for the duration
// of the calling test, restoring the previous value on cleanup.
func useFakeSensei(t *testing.T) {
	t.Helper()
	bin := buildFakeSensei(t)
	prev := senseiBinaryPath
	senseiBinaryPath = func() (string, error) { return bin, nil }
	t.Cleanup(func() { senseiBinaryPath = prev })
}

func frozenWorkspaceForReconstructTests(t *testing.T) string {
	t.Helper()
	repo, base := localRepo(t)
	taskPath, oraclePath := writeManifests(t, base, "")
	workspace := filepath.Join(t.TempDir(), "workspace")
	if _, _, err := Freeze(FreezeOptions{TaskPath: taskPath, SourceRepo: repo, OraclePath: oraclePath, OutputDir: workspace}); err != nil {
		t.Fatal(err)
	}
	return workspace
}

func TestReconstruct_SuccessfulPipelineProducesRealVerdicts(t *testing.T) {
	useFakeSensei(t)
	workspace := frozenWorkspaceForReconstructTests(t)

	receipt, err := Reconstruct(workspace, "2026-01-01T00:00:00Z", false)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != ReconstructionFrozen {
		t.Fatalf("Status = %q, want %q", receipt.Status, ReconstructionFrozen)
	}
	if receipt.ClosureVerdict != "closed" || receipt.ConvergenceStatus != "closed" || receipt.AdmissionDecision != "admitted" {
		t.Fatalf("unexpected verdicts: closure=%q convergence=%q admission=%q", receipt.ClosureVerdict, receipt.ConvergenceStatus, receipt.AdmissionDecision)
	}
	for _, sentinel := range []string{digest([]byte("not-run")), digest([]byte("isolated-empty"))} {
		if receipt.FactsDigestSHA256 == sentinel || receipt.CandidatesDigestSHA256 == sentinel || receipt.GraphDigestSHA256 == sentinel {
			t.Fatalf("receipt still carries a stub sentinel digest: %+v", receipt)
		}
	}
	if len(receipt.Limitations) != 0 {
		t.Fatalf("expected no limitations on a successful run, got %+v", receipt.Limitations)
	}
}

func TestReconstruct_BootstrapFailureIsUnavailableNotError(t *testing.T) {
	useFakeSensei(t)
	t.Setenv("FAKESENSEI_FAIL_STAGE", "bootstrap")
	workspace := frozenWorkspaceForReconstructTests(t)

	receipt, err := Reconstruct(workspace, "2026-01-01T00:00:00Z", false)
	if err != nil {
		t.Fatalf("Reconstruct returned an error instead of an Unavailable receipt: %v", err)
	}
	if receipt.Status != ReconstructionUnavailable {
		t.Fatalf("Status = %q, want %q", receipt.Status, ReconstructionUnavailable)
	}
	if receipt.AdmissionDecision != "uncertifiable" {
		t.Fatalf("AdmissionDecision = %q, want uncertifiable", receipt.AdmissionDecision)
	}
	found := false
	for _, l := range receipt.Limitations {
		if l.Source == "benchmark.reconstruct.bootstrap" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a benchmark.reconstruct.bootstrap limitation, got %+v", receipt.Limitations)
	}
}

// TestReconstruct_EmptyClaimsIsUnavailableNotError proves the dominant
// real-world cold-repository outcome — mechanical inference produces no
// claims, so prepare-change refuses to start a task — is treated as an
// honest, non-error finding, not a crash. The fake binary's default
// infer-claims behavior already writes an empty claims document; simulate
// prepare-change's real refusal by configuring that stage to fail too,
// exactly as the real CLI does when handed an empty claims.yaml.
func TestReconstruct_EmptyClaimsIsUnavailableNotError(t *testing.T) {
	useFakeSensei(t)
	t.Setenv("FAKESENSEI_FAIL_STAGE", "prepare-change")
	workspace := frozenWorkspaceForReconstructTests(t)

	receipt, err := Reconstruct(workspace, "2026-01-01T00:00:00Z", false)
	if err != nil {
		t.Fatalf("Reconstruct returned an error instead of an Unavailable receipt: %v", err)
	}
	if receipt.Status != ReconstructionUnavailable {
		t.Fatalf("Status = %q, want %q", receipt.Status, ReconstructionUnavailable)
	}
	if receipt.ClosureVerdict != "uncertifiable" || receipt.ConvergenceStatus != "uncertifiable" || receipt.AdmissionDecision != "uncertifiable" {
		t.Fatalf("unexpected verdicts on failure: %+v", receipt)
	}
}

func TestReconstruct_OpenClosureFromColdRepoIsHonestNotUncertifiable(t *testing.T) {
	useFakeSensei(t)
	t.Setenv("FAKESENSEI_PREPARE_YAML", `architecture_prepare_change:
  task_id: task.fake.cold
  session:
    closure_verdict: open
    convergence_status: waiting
    admission_decision: waiting
`)
	workspace := frozenWorkspaceForReconstructTests(t)

	receipt, err := Reconstruct(workspace, "2026-01-01T00:00:00Z", false)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != ReconstructionFrozen {
		t.Fatalf("Status = %q, want %q — a real open/waiting verdict is a completed reconstruction, not an infrastructure failure", receipt.Status, ReconstructionFrozen)
	}
	if receipt.ClosureVerdict != "open" || receipt.ConvergenceStatus != "waiting" || receipt.AdmissionDecision != "waiting" {
		t.Fatalf("unexpected verdicts: %+v", receipt)
	}
}

func TestReconstruct_IsDeterministic(t *testing.T) {
	useFakeSensei(t)
	workspace := frozenWorkspaceForReconstructTests(t)

	a, err := Reconstruct(workspace, "2026-01-01T00:00:00Z", false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Reconstruct(workspace, "2026-01-01T00:00:00Z", false)
	if err != nil {
		t.Fatal(err)
	}
	if a.ReceiptDigestSHA256 != b.ReceiptDigestSHA256 {
		t.Fatalf("reconstruction not deterministic:\n%+v\n!=\n%+v", a, b)
	}
	// --check mode must agree with what was just written.
	if _, err := Reconstruct(workspace, "2026-01-01T00:00:00Z", true); err != nil {
		t.Fatalf("--check failed against a receipt Reconstruct itself just wrote: %v", err)
	}
}

func TestReconstruct_NeverWritesIntoBlindRepository(t *testing.T) {
	useFakeSensei(t)
	workspace := frozenWorkspaceForReconstructTests(t)
	blind := filepath.Join(workspace, "blind-repository")
	before := gitOut(t, blind, "status", "--porcelain")

	if _, err := Reconstruct(workspace, "2026-01-01T00:00:00Z", false); err != nil {
		t.Fatal(err)
	}
	after := gitOut(t, blind, "status", "--porcelain")
	if strings.TrimSpace(before) != strings.TrimSpace(after) {
		t.Fatalf("blind-repository working tree changed during Reconstruct: before=%q after=%q", before, after)
	}
	head := gitOut(t, blind, "rev-parse", "HEAD")
	if strings.TrimSpace(head) == "" {
		t.Fatal("blind-repository HEAD unexpectedly empty")
	}
}

// --- digestDir ---

func TestDigestDir_DeterministicAndPathIndependent(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	for _, base := range []string{dirA, dirB} {
		if err := os.MkdirAll(filepath.Join(base, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(base, "a.yaml"), []byte("a: 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(base, "sub", "b.yaml"), []byte("b: 2\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	digestA, err := digestDir(dirA)
	if err != nil {
		t.Fatal(err)
	}
	digestB, err := digestDir(dirB)
	if err != nil {
		t.Fatal(err)
	}
	if digestA != digestB {
		t.Fatalf("digestDir depends on the absolute directory path: %q != %q (dirA=%s dirB=%s)", digestA, digestB, dirA, dirB)
	}
}

func TestDigestDir_AbsentDirectoryIsEmptyNotError(t *testing.T) {
	got, err := digestDir(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatal(err)
	}
	if got != digest(nil) {
		t.Fatalf("digestDir(absent) = %q, want digest(nil) = %q", got, digest(nil))
	}
}
