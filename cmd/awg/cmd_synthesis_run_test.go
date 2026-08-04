// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/agentcommand"
	"github.com/globulario/sensei/golang/architecture/synthesisdriver"
	"github.com/globulario/sensei/golang/architecture/taskcontrol"
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

// TestValidateCurrentBinding_CurrentAndAdmittedPasses covers the ordinary
// case: a current binding with an admitted (or admitted-with-conditions)
// mutation capability must not be refused.
func TestValidateCurrentBinding_CurrentAndAdmittedPasses(t *testing.T) {
	for _, modify := range []string{admission.CapabilityAdmitted, admission.CapabilityAdmittedWithConditions} {
		control := taskcontrol.TaskControlState{
			BindingHealth: "current",
			Permission:    taskcontrol.PermissionSummary{Modify: modify},
		}
		if err := validateCurrentBinding(control); err != nil {
			t.Fatalf("modify=%q: unexpected error: %v", modify, err)
		}
	}
}

// TestValidateCurrentBinding_StaleIsRefused is the direct regression test
// for a live review finding: a task that converged with zero closure
// blockers before its binding went stale has no PrimaryBlocker to trip the
// separate --force-unconverged gate, so BindingHealth must be checked on
// its own, and unconditionally (no override flag bypasses it).
func TestValidateCurrentBinding_StaleIsRefused(t *testing.T) {
	control := taskcontrol.TaskControlState{
		BindingHealth: "stale",
		Permission:    taskcontrol.PermissionSummary{Modify: admission.CapabilityAdmitted},
	}
	err := validateCurrentBinding(control)
	if err == nil {
		t.Fatal("expected an error for a stale binding")
	}
	if !containsAll(err.Error(), "stale", "prepare-change") {
		t.Fatalf("error should name the staleness and the repair path: %v", err)
	}
}

// TestValidateCurrentBinding_RefusedMutationCapabilityIsRefused covers the
// companion condition named in the same finding: even with a current
// binding, a task admission decision that itself refuses mutation
// capability must not proceed into synthesis.
func TestValidateCurrentBinding_RefusedMutationCapabilityIsRefused(t *testing.T) {
	control := taskcontrol.TaskControlState{
		BindingHealth: "current",
		Permission:    taskcontrol.PermissionSummary{Modify: admission.CapabilityRefused},
	}
	err := validateCurrentBinding(control)
	if err == nil {
		t.Fatal("expected an error for a refused mutation capability")
	}
	if !containsAll(err.Error(), "mutation capability") {
		t.Fatalf("error should name the refused capability: %v", err)
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

func TestValidateNoDecisionProofObligations_EmptyPasses(t *testing.T) {
	if err := validateNoDecisionProofObligations(nil); err != nil {
		t.Fatalf("unexpected error for zero task-decision obligations: %v", err)
	}
	if err := validateNoDecisionProofObligations([]admission.ProofReceipt{}); err != nil {
		t.Fatalf("unexpected error for zero task-decision obligations: %v", err)
	}
}

// TestValidateNoDecisionProofObligations_NonEmptyIsRefused covers a live
// review finding: the task's own admission decision (already computed by
// `sensei prepare-change`, authoritative, and never something the
// caller-authored interpretation may erase or override) declaring a real
// proof obligation must refuse the run -- this is a distinct check from
// validateNoRequiredProofObligations, which only ever sees what the
// interpretation file itself claims.
func TestValidateNoDecisionProofObligations_NonEmptyIsRefused(t *testing.T) {
	err := validateNoDecisionProofObligations([]admission.ProofReceipt{{ID: "obligation.security_review"}})
	if err == nil {
		t.Fatal("expected an error for a task admission decision declaring a required proof obligation")
	}
	if !containsAll(err.Error(), "obligation.security_review", "EvidenceResolver", "prepare-change") {
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

func TestResolveStoreDir_EmptyUsesDefault(t *testing.T) {
	got := resolveStoreDir("", "/repo", "/repo/.sensei/tasks/t1/synthesis-run/candidates")
	want := "/repo/.sensei/tasks/t1/synthesis-run/candidates"
	if got != want {
		t.Fatalf("resolveStoreDir(\"\", ...) = %q, want %q", got, want)
	}
}

func TestResolveStoreDir_AbsoluteExplicitIsUsedAsIs(t *testing.T) {
	got := resolveStoreDir("/elsewhere/candidates", "/repo", "/repo/default")
	if got != "/elsewhere/candidates" {
		t.Fatalf("resolveStoreDir with an absolute explicit value = %q, want it unchanged", got)
	}
}

// TestResolveStoreDir_RelativeExplicitIsResolvedAgainstRepo is the direct
// regression test for a live review finding: NewFSCandidateArtifactStore
// and NewFSEvidenceSink both require an absolute root, but a caller-
// supplied relative --candidate-store/--evidence-store (e.g.
// ".sensei/output") was passed through unresolved -- os.MkdirAll accepts
// a relative path just fine, so the directory got created, and only the
// later store constructor failed with a confusing "must be absolute"
// error. A relative explicit value must resolve against absRepo, the same
// convention --task itself already uses.
func TestResolveStoreDir_RelativeExplicitIsResolvedAgainstRepo(t *testing.T) {
	got := resolveStoreDir(".sensei/output", "/repo", "/repo/default")
	want := "/repo/.sensei/output"
	if got != want {
		t.Fatalf("resolveStoreDir(%q, ...) = %q, want %q", ".sensei/output", got, want)
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
		// Both with and without a sealed candidate digest: admit-change
		// must never be suggested for any non-candidate-ready disposition,
		// regardless of whether a (non-admission-ready) candidate exists.
		for _, digest := range []*string{nil, strPtr("cand0000000000000000000000000000000000000000000000000000000000")} {
			if s := nextStep(d, digest); contains(s, "admit-change") {
				t.Fatalf("nextStep(%q, digest=%v) suggests admit-change, but this disposition is not candidate-ready: %s", d, digest, s)
			}
		}
	}
	if s := nextStep(synthesisdriver.DispositionCandidateReady, strPtr("cand0000000000000000000000000000000000000000000000000000000000")); !contains(s, "admit-change") {
		t.Fatalf("nextStep(candidate-ready) should point at admit-change, got: %s", s)
	}
}

// TestNextStep_AcknowledgesASealedCandidateOnNonCandidateReadyDispositions
// is the direct regression test for a live review finding:
// DispositionTerminalFailure's text unconditionally claimed "No candidate
// exists," directly contradicting the same report's candidate_path field
// whenever a candidate actually was sealed. runnercomposition.Run seals a
// candidate unconditionally on every O3-verified attempt, strictly before
// O4 evaluation runs -- so a candidate can be sealed and the run still end
// in DispositionTerminalFailure (this attempt's own O4 recommends abort)
// or DispositionRunnerStopped (an earlier attempt in the same retry loop
// sealed one, then a later attempt's O3 generation itself failed) --
// confirmed via dedicated golang/architecture/synthesisdriver tests
// (TestRunFinalizesTerminalFailureReachedOnTheLastAllowedStep and direct
// empirical verification for RunnerStopped) before this fix.
func TestNextStep_AcknowledgesASealedCandidateOnNonCandidateReadyDispositions(t *testing.T) {
	digest := strPtr("cand0000000000000000000000000000000000000000000000000000000000")
	for _, d := range []synthesisdriver.Disposition{
		synthesisdriver.DispositionTerminalFailure,
		synthesisdriver.DispositionRunnerStopped,
		synthesisdriver.DispositionStepLimitReached,
	} {
		withCandidate := nextStep(d, digest)
		if contains(withCandidate, "No candidate exists") {
			t.Fatalf("nextStep(%q) with a sealed candidate still claims none exists: %s", d, withCandidate)
		}
		if !contains(withCandidate, "candidate_path above") {
			t.Fatalf("nextStep(%q) with a sealed candidate should point at candidate_path: %s", d, withCandidate)
		}
		if !contains(withCandidate, "not admission-ready") {
			t.Fatalf("nextStep(%q) with a sealed candidate should say it is not admission-ready: %s", d, withCandidate)
		}
		withoutCandidate := nextStep(d, nil)
		if !contains(withoutCandidate, "No candidate") {
			t.Fatalf("nextStep(%q) without a sealed candidate should say none exists: %s", d, withoutCandidate)
		}
	}
	// ProviderStopped can never carry a candidate (fires strictly before
	// O3/PhaseAttempting could ever run) -- its text stays unconditional.
	if s := nextStep(synthesisdriver.DispositionProviderStopped, digest); !contains(s, "No candidate exists") {
		t.Fatalf("nextStep(ProviderStopped) should unconditionally say no candidate exists, got: %s", s)
	}
}

// TestNextStep_CandidateReadyDoesNotClaimAdmitChangeConsumesLineage is the
// direct regression test for a live review finding: the candidate-ready
// text previously told the operator to "run admit-change / verify-admission
// ... to review and apply it" using the lineage bundle, as if those
// commands already read it -- but neither runAdmitChange (cmd_admit_change.go,
// which takes --bundle/--request/--graph-nt/--repo) nor the v2 admit-change
// (cmd_admission_v2.go, which takes --repo/--task-dir) accepts a lineage
// file or calls admissioncomposition.ComposeInput anywhere in this
// package. The text must say so honestly, not imply a wiring that does
// not exist yet.
// TestBuildSynthesisRunReport_CandidatePathIsTheArtifactFileNotTheStoreDir
// is the direct regression test for a live review finding: CandidatePath
// previously named the candidate store DIRECTORY while the field name and
// downstream automation expectations promised a path to the candidate
// itself. It must be exactly <candidateStoreDir>/<digest>.json, matching
// the same filename Put seals under and the lineage bundle's own
// CandidateArtifactPath already computes.
func TestBuildSynthesisRunReport_CandidatePathIsTheArtifactFileNotTheStoreDir(t *testing.T) {
	const digest = "cand0000000000000000000000000000000000000000000000000000000000"
	result := synthesisdriver.Result{
		Receipt: synthesisdriver.RunReceipt{
			Disposition:                   synthesisdriver.DispositionCandidateReady,
			CandidateArtifactDigestSHA256: strPtr(digest),
		},
	}
	report := buildSynthesisRunReport(result, "task.test", "/store", "/store/"+digest+".lineage.json", 20)
	want := "/store/" + digest + ".json"
	if report.CandidatePath != want {
		t.Fatalf("CandidatePath = %q, want %q", report.CandidatePath, want)
	}
}

// TestBuildSynthesisRunReport_NonCandidateReadyLeavesCandidatePathEmpty
// covers the companion case: with no sealed candidate, CandidatePath must
// stay empty rather than naming a file that does not exist.
func TestBuildSynthesisRunReport_NonCandidateReadyLeavesCandidatePathEmpty(t *testing.T) {
	result := synthesisdriver.Result{
		Receipt: synthesisdriver.RunReceipt{Disposition: synthesisdriver.DispositionTerminalFailure},
	}
	report := buildSynthesisRunReport(result, "task.test", "/store", "", 20)
	if report.CandidatePath != "" {
		t.Fatalf("CandidatePath = %q, want empty for a disposition with no sealed candidate", report.CandidatePath)
	}
}

// TestHelp_DoesNotClaimAdmitChangeConsumesLineage is the direct
// regression test for a live review finding: the "--help" text (separate
// from nextStep's candidate-ready text, fixed in an earlier round) still
// told the operator to run admit-change/verify-admission "to review and
// apply" a sealed candidate, the same misleading implication that neither
// command currently consumes the lineage bundle.
func TestHelp_DoesNotClaimAdmitChangeConsumesLineage(t *testing.T) {
	out, _ := captureSynthesisRunStderr(t, []string{"--help"})
	if !contains(out, "does not currently consume") && !contains(out, "not-yet-built") {
		t.Fatalf("--help should honestly say admit-change/verify-admission do not yet consume the lineage bundle, got: %s", out)
	}
}

func TestNextStep_CandidateReadyDoesNotClaimAdmitChangeConsumesLineage(t *testing.T) {
	s := nextStep(synthesisdriver.DispositionCandidateReady, strPtr("cand0000000000000000000000000000000000000000000000000000000000"))
	if !contains(s, "does not currently consume") && !contains(s, "not-yet-built") {
		t.Fatalf("nextStep(candidate-ready) should honestly say admit-change/verify-admission do not yet consume the lineage bundle, got: %s", s)
	}
}
