// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/synthesis"
)

type scriptedCommandRunner struct {
	results  []CommandResult
	requests []CommandRequest
}

func (r *scriptedCommandRunner) Run(_ context.Context, request CommandRequest, _ int64) (CommandResult, error) {
	r.requests = append(r.requests, request)
	if len(r.results) == 0 {
		return CommandResult{Outcome: CommandOutcomeUnavailable, ExitCode: -1, Detail: "no scripted result"}, nil
	}
	result := r.results[0]
	r.results = r.results[1:]
	return result, nil
}

func absoluteTestExecutable(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs("sensei-o4-test-executable")
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMechanicalEvaluatorMapsProcessTruthWithoutRecommendationAuthority(t *testing.T) {
	surface := &recordingEvaluatorSurface{ref: "surface://test/mechanical/plain", root: t.TempDir(), mode: SurfaceModePlain}
	input := evaluationInputForSurface(t, surface)
	runner := &scriptedCommandRunner{results: []CommandResult{
		{Outcome: CommandOutcomeCompleted, ExitCode: 0, Stdout: []byte("ok")},
		{Outcome: CommandOutcomeExited, ExitCode: 3, Stderr: []byte("failed")},
	}}
	sink := NewMemoryEvidenceSink()
	executable := absoluteTestExecutable(t)
	evaluator, err := NewMechanicalEvaluator("mechanical.test", "v1", true, surface, runner, sink, []MechanicalCommand{
		{CheckID: "build", Executable: executable, Args: []string{"build"}, Env: []string{"LANG=C", "TOKEN=super-secret"}},
		{CheckID: "test", Executable: executable, Args: []string{"test"}, Env: []string{"LANG=C", "TOKEN=super-secret"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.TerminalOutcome != EvaluatorOutcomeCompleted {
		t.Fatalf("terminal outcome = %q, want completed", result.TerminalOutcome)
	}
	if len(result.Checks) != 2 || result.Checks[0].Status != synthesis.CheckPassed || result.Checks[1].Status != synthesis.CheckFailed {
		t.Fatalf("mechanical checks = %+v", result.Checks)
	}
	if len(result.ClassifiedFailureReasons) != 1 || result.ClassifiedFailureReasons[0] != string(FailureClassMechanicalCheckFailure) {
		t.Fatalf("failure reasons = %v", result.ClassifiedFailureReasons)
	}
	if result.CleanupSucceeded != nil {
		t.Fatal("adapter wrote cleanup truth instead of leaving it for ExecuteEvaluator")
	}
	if len(result.EvidenceReferences) != 1 || len(result.Checks[0].EvidenceReferences) != 1 || result.Checks[0].EvidenceReferences[0] != result.EvidenceReferences[0].Reference {
		t.Fatalf("mechanical evidence attribution = %+v", result)
	}
	if len(result.EvidenceReferences) != 1 {
		t.Fatal("mechanical result did not bind its evidence bundle")
	}
	evidenceBytes, ok := sink.Get(result.EvidenceReferences[0].DigestSHA256)
	if !ok {
		t.Fatal("mechanical evidence bundle missing from sink")
	}
	if strings.Contains(string(evidenceBytes), "super-secret") || !strings.Contains(string(evidenceBytes), "TOKEN") {
		t.Fatalf("mechanical evidence leaked environment values or omitted keys: %s", evidenceBytes)
	}
	if len(runner.requests) != 2 {
		t.Fatalf("command requests = %d, want 2", len(runner.requests))
	}
	for _, request := range runner.requests {
		if request.Dir != surface.root || request.Executable != executable {
			t.Fatalf("mechanical command escaped exact surface/executable: %+v", request)
		}
		if len(request.Env) != 2 || request.Env[0] != "LANG=C" || request.Env[1] != "TOKEN=super-secret" {
			t.Fatalf("mechanical command inherited or changed environment: %v", request.Env)
		}
	}
}

func TestMechanicalEvaluatorStopsAfterUnavailableCommand(t *testing.T) {
	surface := &recordingEvaluatorSurface{ref: "surface://test/mechanical-unavailable/plain", root: t.TempDir(), mode: SurfaceModePlain}
	input := evaluationInputForSurface(t, surface)
	runner := &scriptedCommandRunner{results: []CommandResult{
		{Outcome: CommandOutcomeUnavailable, ExitCode: -1, Detail: "tool missing"},
	}}
	evaluator, err := NewMechanicalEvaluator("mechanical.unavailable", "v1", false, surface, runner, NewMemoryEvidenceSink(), []MechanicalCommand{
		{CheckID: "first", Executable: absoluteTestExecutable(t)},
		{CheckID: "second", Executable: absoluteTestExecutable(t)},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.TerminalOutcome != EvaluatorOutcomeUnavailable {
		t.Fatalf("terminal outcome = %q", result.TerminalOutcome)
	}
	if result.Checks[0].Status != synthesis.CheckUnavailable || result.Checks[1].Status != synthesis.CheckSkipped {
		t.Fatalf("unavailable/skip mapping = %+v", result.Checks)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("runner invoked %d commands after terminal unavailability", len(runner.requests))
	}
}

func senseiGateOutput(t *testing.T, input EvaluationInput, blocked bool, wouldBlock, scopeErrors int, verdict string) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"diff":         "HEAD",
		"domain":       input.RepositoryDomain,
		"enforced":     true,
		"blocked":      blocked,
		"would_block":  wouldBlock,
		"warn":         0,
		"scope_errors": scopeErrors,
		"verdict":      verdict,
		"files":        []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func newSenseiGateTestEvaluator(t *testing.T, surface EvaluatorSurface, runner CommandRunner) (*SenseiGateEvaluator, string, []byte) {
	t.Helper()
	executable := absoluteTestExecutable(t)
	root, err := surface.RootPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	policyContent := []byte("version: 1\nrules: {}\n")
	policyPath := filepath.Join(t.TempDir(), "gate-policy.yaml")
	if err := os.WriteFile(policyPath, policyContent, 0o600); err != nil {
		t.Fatal(err)
	}
	evaluator, err := NewSenseiGateEvaluator(SenseiGateConfig{
		EvaluatorID:      "sensei.gate",
		EvaluatorVersion: "v1",
		SenseiExecutable: executable,
		Address:          "127.0.0.1:10120",
		PolicyPath:       policyPath,
		Environment:      []string{"LANG=C"},
	}, surface, runner, NewMemoryEvidenceSink())
	if err != nil {
		t.Fatal(err)
	}
	return evaluator, policyPath, policyContent
}

func TestSenseiGateEvaluatorMapsExistingOwnerVerdicts(t *testing.T) {
	tests := []struct {
		name             string
		command          func(EvaluationInput) CommandResult
		wantOutcome      EvaluatorTerminalOutcome
		wantStatus       synthesis.CheckObservationStatus
		wantFailureClass string
	}{
		{
			name: "pass",
			command: func(input EvaluationInput) CommandResult {
				return CommandResult{Outcome: CommandOutcomeCompleted, ExitCode: 0, Stdout: senseiGateOutput(t, input, false, 0, 0, "PASS")}
			},
			wantOutcome: EvaluatorOutcomeCompleted,
			wantStatus:  synthesis.CheckPassed,
		},
		{
			name: "blocked",
			command: func(input EvaluationInput) CommandResult {
				return CommandResult{Outcome: CommandOutcomeExited, ExitCode: 1, Stdout: senseiGateOutput(t, input, true, 1, 0, "BLOCKED")}
			},
			wantOutcome:      EvaluatorOutcomeCompleted,
			wantStatus:       synthesis.CheckFailed,
			wantFailureClass: failureClassSenseiGateBlockingFinding,
		},
		{
			name: "cannot verify",
			command: func(input EvaluationInput) CommandResult {
				return CommandResult{Outcome: CommandOutcomeExited, ExitCode: 2, Stdout: senseiGateOutput(t, input, false, 0, 1, "CANNOT VERIFY")}
			},
			wantOutcome: EvaluatorOutcomeUnavailable,
			wantStatus:  synthesis.CheckUnavailable,
		},
		{
			name: "contradictory zero exit",
			command: func(input EvaluationInput) CommandResult {
				return CommandResult{Outcome: CommandOutcomeCompleted, ExitCode: 0, Stdout: senseiGateOutput(t, input, true, 1, 0, "BLOCKED")}
			},
			wantOutcome: EvaluatorOutcomeUnavailable,
			wantStatus:  synthesis.CheckUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			surface := &recordingEvaluatorSurface{ref: "surface://test/sensei-gate/git-diff", root: t.TempDir(), mode: SurfaceModeGitDiff}
			input := evaluationInputForSurface(t, surface)
			runner := &scriptedCommandRunner{}
			runner.results = []CommandResult{test.command(input)}
			evaluator, _, _ := newSenseiGateTestEvaluator(t, surface, runner)

			result, err := evaluator.Evaluate(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			if result.TerminalOutcome != test.wantOutcome || result.Checks[0].Status != test.wantStatus {
				t.Fatalf("gate mapping outcome/status = %q/%q, want %q/%q", result.TerminalOutcome, result.Checks[0].Status, test.wantOutcome, test.wantStatus)
			}
			if test.wantFailureClass == "" {
				if len(result.ClassifiedFailureReasons) != 0 {
					t.Fatalf("unexpected failure reasons: %v", result.ClassifiedFailureReasons)
				}
			} else if len(result.ClassifiedFailureReasons) != 1 || result.ClassifiedFailureReasons[0] != test.wantFailureClass {
				t.Fatalf("failure reasons = %v, want %q", result.ClassifiedFailureReasons, test.wantFailureClass)
			}
			if len(runner.requests) != 1 {
				t.Fatalf("gate command count = %d", len(runner.requests))
			}
			request := runner.requests[0]
			joined := strings.Join(request.Args, " ")
			for _, required := range []string{
				"gate", "--diff HEAD", "--domain " + input.RepositoryDomain,
				"--repo-root " + surface.root, "--enforce", "--json",
				"--policy " + filepath.Join(surface.root, ".git", "sensei-o4-policy.yaml"),
			} {
				if !strings.Contains(joined, required) {
					t.Errorf("gate args %q do not contain %q", joined, required)
				}
			}
			if request.Dir != surface.root || len(request.Env) != 1 || request.Env[0] != "LANG=C" {
				t.Fatalf("gate command escaped exact surface/environment: %+v", request)
			}
		})
	}
}

func TestSenseiGateEvaluatorFreezesExternalPolicyAndBindsItsDigest(t *testing.T) {
	surface := &recordingEvaluatorSurface{ref: "surface://test/sensei-gate-freeze/git-diff", root: t.TempDir(), mode: SurfaceModeGitDiff}
	input := evaluationInputForSurface(t, surface)
	runner := &scriptedCommandRunner{results: []CommandResult{{
		Outcome:  CommandOutcomeCompleted,
		ExitCode: 0,
		Stdout:   senseiGateOutput(t, input, false, 0, 0, "PASS"),
	}}}
	evaluator, policyPath, originalPolicy := newSenseiGateTestEvaluator(t, surface, runner)
	if err := os.WriteFile(policyPath, []byte("candidate-era replacement\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := evaluator.Evaluate(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	materialized, err := os.ReadFile(filepath.Join(surface.root, ".git", "sensei-o4-policy.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(materialized) != string(originalPolicy) {
		t.Fatalf("gate policy was reread after construction: %q", materialized)
	}
	sum := sha256.Sum256(originalPolicy)
	capability := "sensei-gate-policy-sha256:" + hex.EncodeToString(sum[:])
	found := false
	for _, got := range evaluator.descriptor.RequiredCapabilities {
		if got == capability {
			found = true
		}
	}
	if !found {
		t.Fatalf("descriptor does not bind frozen policy digest %q: %v", capability, evaluator.descriptor.RequiredCapabilities)
	}
}

func TestSenseiGateEvaluatorRejectsCandidateOwnedPolicyPath(t *testing.T) {
	surface := &recordingEvaluatorSurface{ref: "surface://test/sensei-gate-policy/git-diff", root: t.TempDir(), mode: SurfaceModeGitDiff}
	policyDir := filepath.Join(surface.root, ".sensei")
	if err := os.MkdirAll(policyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(policyDir, "policy.yaml")
	if err := os.WriteFile(policyPath, []byte("version: 1\nrules: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewSenseiGateEvaluator(SenseiGateConfig{
		EvaluatorID:      "sensei.gate",
		EvaluatorVersion: "v1",
		SenseiExecutable: absoluteTestExecutable(t),
		PolicyPath:       policyPath,
	}, surface, &scriptedCommandRunner{}, NewMemoryEvidenceSink())
	if err == nil || !strings.Contains(err.Error(), "inside the candidate surface") {
		t.Fatalf("candidate-owned policy rejection = %v", err)
	}
}

func TestSenseiGateEvaluatorRejectsNonGitSurface(t *testing.T) {
	surface := &recordingEvaluatorSurface{ref: "surface://test/plain/plain", root: t.TempDir(), mode: SurfaceModePlain}
	_, err := NewSenseiGateEvaluator(SenseiGateConfig{
		EvaluatorID:      "sensei.gate",
		EvaluatorVersion: "v1",
		SenseiExecutable: absoluteTestExecutable(t),
	}, surface, &scriptedCommandRunner{}, NewMemoryEvidenceSink())
	if err == nil {
		t.Fatal("Sensei gate adapter accepted a non-git-diff surface")
	}
	if !strings.Contains(err.Error(), "git-diff") {
		t.Fatalf("wrong non-git surface error: %v", err)
	}
	if _, err := os.Stat(surface.root); err != nil {
		t.Fatal(err)
	}
}
