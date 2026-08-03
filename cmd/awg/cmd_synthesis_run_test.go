// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture/agentcommand"
	"github.com/globulario/sensei/golang/architecture/synthesisdriver"
)

func captureSynthesisRunStderr(t *testing.T, args []string) (string, int) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()

	code := runSynthesisRun(args)

	_ = w.Close()
	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	_ = r.Close()
	return string(buf[:n]), code
}

// TestPrintUsage_ListsSynthesisRun guards against a real review finding:
// synthesis-run was dispatched in the command switch but never appeared in
// the actual runtime usage listing a user sees from `sensei` / `sensei
// --help` -- only in a developer-facing doc comment above func main that
// nothing prints. Capture the real printUsage() output, not the comment.
func TestPrintUsage_ListsSynthesisRun(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()

	printUsage()

	_ = w.Close()
	buf := make([]byte, 16384)
	n, _ := r.Read(buf)
	_ = r.Close()
	os.Stderr = old

	if !contains(string(buf[:n]), "synthesis-run") {
		t.Fatalf("printUsage() output does not list synthesis-run: %s", buf[:n])
	}
}

// TestResolveSynthesisRunObjective_ExplicitFlagMatches covers explicit
// --objective given, matching the authored interpretation exactly.
func TestResolveSynthesisRunObjective_ExplicitFlagMatches(t *testing.T) {
	got, err := resolveSynthesisRunObjective("Add a doc comment", "task description, unused", "Add a doc comment")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Add a doc comment" {
		t.Fatalf("objective = %q, want %q", got, "Add a doc comment")
	}
}

// TestResolveSynthesisRunObjective_TaskDefaultMatches covers no --objective
// given, falling back to the task's own recorded description, matching the
// authored interpretation exactly (after whitespace normalization on both
// sides).
func TestResolveSynthesisRunObjective_TaskDefaultMatches(t *testing.T) {
	got, err := resolveSynthesisRunObjective("", "  Add a doc comment  ", "Add a doc comment")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Add a doc comment" {
		t.Fatalf("objective = %q, want %q", got, "Add a doc comment")
	}
}

// TestResolveSynthesisRunObjective_MismatchIsRefused covers a real review
// finding: the authored interpretation's objective silently diverging from
// the session objective must refuse, not proceed with either one.
func TestResolveSynthesisRunObjective_MismatchIsRefused(t *testing.T) {
	_, err := resolveSynthesisRunObjective("Add a doc comment", "task description, unused", "Rewrite the whole file")
	if err == nil {
		t.Fatal("expected an error for a mismatched interpretation objective")
	}
	if !containsAll(err.Error(), "Add a doc comment", "Rewrite the whole file") {
		t.Fatalf("error should name both objectives: %v", err)
	}
}

// TestResolveSynthesisRunObjective_TaskDefaultMismatchIsRefused covers the
// same refusal when the objective came from the task's own recorded
// description rather than an explicit flag.
func TestResolveSynthesisRunObjective_TaskDefaultMismatchIsRefused(t *testing.T) {
	_, err := resolveSynthesisRunObjective("", "task description, unused", "Rewrite the whole file")
	if err == nil {
		t.Fatal("expected an error for a mismatched interpretation objective")
	}
	if !containsAll(err.Error(), "task's own recorded description") {
		t.Fatalf("error should name the task's own recorded description as the objective source: %v", err)
	}
}

func TestValidateNoRequiredProofObligations_EmptyPasses(t *testing.T) {
	if err := validateNoRequiredProofObligations(nil); err != nil {
		t.Fatalf("unexpected error for zero declared obligations: %v", err)
	}
	if err := validateNoRequiredProofObligations([]string{}); err != nil {
		t.Fatalf("unexpected error for zero declared obligations: %v", err)
	}
}

// TestValidateNoRequiredProofObligations_NonEmptyIsRefused covers a real
// review finding: a non-empty declared obligation must refuse the run, not
// be silently discarded in favor of an empty
// synthesis.Session.ProofObligationDigests.
func TestValidateNoRequiredProofObligations_NonEmptyIsRefused(t *testing.T) {
	err := validateNoRequiredProofObligations([]string{"obligation.security_review"})
	if err == nil {
		t.Fatal("expected an error for a declared required proof obligation")
	}
	if !containsAll(err.Error(), "obligation.security_review", "EvidenceResolver") {
		t.Fatalf("error should name the obligation and explain why it cannot proceed: %v", err)
	}
}

func TestRunSynthesisRun_RequiresInterpretation(t *testing.T) {
	_, code := captureSynthesisRunStderr(t, []string{"--agent", "codex", "--agent-command", "/bin/true"})
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
}

func TestRunSynthesisRun_RejectsUnknownAgent(t *testing.T) {
	out, code := captureSynthesisRunStderr(t, []string{"--interpretation", "x.json", "--agent", "gemini", "--agent-command", "/bin/true"})
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if out == "" {
		t.Fatal("expected an error message naming the invalid --agent value")
	}
}

func TestRunSynthesisRun_RequiresAgentCommand(t *testing.T) {
	_, code := captureSynthesisRunStderr(t, []string{"--interpretation", "x.json", "--agent", "codex"})
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
}

func TestRunSynthesisRun_RejectsRelativeAgentCommand(t *testing.T) {
	out, code := captureSynthesisRunStderr(t, []string{"--interpretation", "x.json", "--agent", "codex", "--agent-command", "codex"})
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !containsAll(out, "absolute path", "PATH lookup") {
		t.Fatalf("expected error to explain the absolute-path requirement, got: %s", out)
	}
}

func TestRunSynthesisRun_RejectsUnknownFormat(t *testing.T) {
	_, code := captureSynthesisRunStderr(t, []string{"--interpretation", "x.json", "--agent", "codex", "--agent-command", "/bin/true", "--format", "yaml"})
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
}

func TestRunSynthesisRun_RejectsUnexpectedPositionalArg(t *testing.T) {
	_, code := captureSynthesisRunStderr(t, []string{"--interpretation", "x.json", "--agent", "codex", "--agent-command", "/bin/true", "extra-positional-arg"})
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
}

func TestRunSynthesisRun_FailsWithoutActiveOrExplicitTask(t *testing.T) {
	dir := t.TempDir()
	out, code := captureSynthesisRunStderr(t, []string{
		"--repo", dir,
		"--interpretation", "x.json",
		"--agent", "codex",
		"--agent-command", "/bin/true",
	})
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !containsAll(out, "prepare-change") {
		t.Fatalf("expected error to point at 'sensei prepare-change', got: %s", out)
	}
}

func containsAll(haystack string, needles ...string) bool {
	for _, n := range needles {
		if !contains(haystack, n) {
			return false
		}
	}
	return true
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (substr == "" || indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func TestResolveAgentWorkdirs_CreatesTwoDistinctEmptyDirs(t *testing.T) {
	gen, plan, cleanup, err := resolveAgentWorkdirs("")
	if err != nil {
		t.Fatalf("resolveAgentWorkdirs: %v", err)
	}
	defer cleanup()
	if gen == plan {
		t.Fatal("generation and planning workdirs must be distinct")
	}
	if !filepath.IsAbs(gen) || !filepath.IsAbs(plan) {
		t.Fatalf("workdirs must be absolute: gen=%q plan=%q", gen, plan)
	}
	for _, dir := range []string{gen, plan} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		if len(entries) != 0 {
			t.Fatalf("%s is not empty: %v", dir, entries)
		}
	}
}

func TestResolveAgentWorkdirs_RejectsRelativeBase(t *testing.T) {
	if _, _, _, err := resolveAgentWorkdirs("relative/path"); err == nil {
		t.Fatal("expected an error for a relative --agent-workdir")
	}
}

func TestResolveAgentWorkdirs_UsesGivenBase(t *testing.T) {
	base := t.TempDir()
	gen, plan, cleanup, err := resolveAgentWorkdirs(base)
	if err != nil {
		t.Fatalf("resolveAgentWorkdirs: %v", err)
	}
	defer cleanup()
	if filepath.Dir(gen) != base || filepath.Dir(plan) != base {
		t.Fatalf("expected workdirs under %s, got gen=%q plan=%q", base, gen, plan)
	}
}

// Sanity check that the agentcommand package this command depends on rejects
// exactly the misconfigurations the CLI itself pre-checks for -- if this
// ever stops being true, the CLI's own pre-checks would be the only
// remaining defense and should be revisited.
func TestAgentCommandConfig_RejectsRelativeCommand(t *testing.T) {
	_, err := agentcommand.NewCodexAgent(agentcommand.CommandAgentConfig{
		Command:              "codex",
		WorkDir:              t.TempDir(),
		MaxMutationPlanBytes: 1024,
	})
	if err == nil {
		t.Fatal("expected an error for a relative Command path")
	}
}

func TestExitCodeForDisposition_EveryDispositionHasADistinctCode(t *testing.T) {
	seen := map[int]synthesisdriver.Disposition{}
	dispositions := []synthesisdriver.Disposition{
		synthesisdriver.DispositionCandidateReady,
		synthesisdriver.DispositionTerminalFailure,
		synthesisdriver.DispositionProviderStopped,
		synthesisdriver.DispositionRunnerStopped,
		synthesisdriver.DispositionStepLimitReached,
	}
	for _, d := range dispositions {
		code := exitCodeForDisposition(d)
		if other, ok := seen[code]; ok {
			t.Fatalf("dispositions %q and %q both map to exit code %d -- automation cannot distinguish them", other, d, code)
		}
		seen[code] = d
		if code == exitInternalDefect {
			t.Fatalf("disposition %q mapped to exitInternalDefect -- every real disposition must have its own code", d)
		}
	}
}

func TestExitCodeForDisposition_UnknownDispositionIsInternalDefect(t *testing.T) {
	if code := exitCodeForDisposition("some-future-disposition-this-cli-does-not-know-about"); code != exitInternalDefect {
		t.Fatalf("unknown disposition code = %d, want exitInternalDefect (%d)", code, exitInternalDefect)
	}
}

func TestNextStep_NeverSuggestsAdmissionExceptForCandidateReady(t *testing.T) {
	for _, d := range []synthesisdriver.Disposition{
		synthesisdriver.DispositionTerminalFailure,
		synthesisdriver.DispositionProviderStopped,
		synthesisdriver.DispositionRunnerStopped,
		synthesisdriver.DispositionStepLimitReached,
	} {
		if s := nextStep(d); contains(s, "admit-change") {
			t.Fatalf("nextStep(%q) suggests admit-change, but no candidate exists for this disposition: %s", d, s)
		}
	}
	if s := nextStep(synthesisdriver.DispositionCandidateReady); !contains(s, "admit-change") {
		t.Fatalf("nextStep(candidate-ready) should point at admit-change, got: %s", s)
	}
}
