// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/tasksession"
	"github.com/globulario/sensei/internal/resulttestkit"
)

// captureAdvance runs runAdvanceResult with args, returning its stdout and exit
// code so the CLI's machine-readable output and exit contract can be asserted.
func captureAdvance(t *testing.T, args []string) (string, int) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	code := runAdvanceResult(args)
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	return string(out), code
}

// TestCLIAdvanceResultReadyPath: over a real scope-verified task, `advance-result`
// records the transition, reports proving, and renders correctness_certified:false;
// exit 0.
func TestCLIAdvanceResultReadyPath(t *testing.T) {
	res, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if err != nil {
		t.Fatal(err)
	}
	out, code := captureAdvance(t, []string{"-repo", res.Repo, "-task-dir", res.TaskDir, "-result-revision", res.ResultRev, "-format", "json"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	var o advanceResultOutput
	if err := json.Unmarshal([]byte(out), &o); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if o.Outcome != "recorded" || o.TaskPhase != "proving" {
		t.Fatalf("outcome=%s phase=%s, want recorded/proving", o.Outcome, o.TaskPhase)
	}
	if o.TransitionEntryDigestSHA256 == "" || o.CurrentLedgerHeadDigestSHA256 == "" {
		t.Fatal("must report the transition entry and current head")
	}
	if o.CorrectnessCertified {
		t.Fatal("correctness_certified must be false")
	}
}

// TestCLIAdvanceResultBlockedPath: a complete-but-blocked result records the
// transition, stays scope_verified, and carries waiting reasons; exit 0, never
// certified/completed.
func TestCLIAdvanceResultBlockedPath(t *testing.T) {
	res, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{
		Direction:   "evolve",
		ResultFiles: map[string]string{"src/model.go": "package src\n\n// evolve\nfunc Publish() {}\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, code := captureAdvance(t, []string{"-repo", res.Repo, "-task-dir", res.TaskDir, "-result-revision", res.ResultRev, "-format", "json"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	var o advanceResultOutput
	if err := json.Unmarshal([]byte(out), &o); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if o.Outcome != "recorded" || o.TaskPhase != "scope_verified" {
		t.Fatalf("outcome=%s phase=%s, want recorded/scope_verified", o.Outcome, o.TaskPhase)
	}
	if len(o.WaitingReasons) == 0 {
		t.Fatal("a blocked result must carry waiting reasons")
	}
	if o.CorrectnessCertified {
		t.Fatal("correctness_certified must be false")
	}
}

// TestCLIAdvanceResultRefusedOnBadResult: a scope-verified task with an invalid
// result revision refuses — never a false success; exit 3.
func TestCLIAdvanceResultRefusedOnBadResult(t *testing.T) {
	res, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if err != nil {
		t.Fatal(err)
	}
	out, code := captureAdvance(t, []string{"-repo", res.Repo, "-task-dir", res.TaskDir, "-result-revision", "does-not-exist", "-format", "json"})
	if code != 3 {
		t.Fatalf("exit %d, want 3: %s", code, out)
	}
	var o advanceResultOutput
	if err := json.Unmarshal([]byte(out), &o); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if o.Outcome != "refused" {
		t.Fatalf("outcome=%s, want refused", o.Outcome)
	}
	if o.TransitionRecorded {
		t.Fatal("a refusal records no transition")
	}
	if o.RefusalCode == "" {
		t.Fatal("a refusal must carry the underlying code")
	}
	// The non-happy output contract: a refusal still reports the actual current
	// verified head, never a silent empty "current" field.
	if !o.CurrentStateAvailable || o.CurrentLedgerHeadDigestSHA256 == "" {
		t.Fatal("a refusal must report the current verified head")
	}
	if o.CorrectnessCertified {
		t.Fatal("correctness_certified must be false")
	}
}

// TestCLIAdvanceResultReplayAcrossInvocations: two separate CLI invocations over
// the same scope-verified task (wall clock advancing between them) record exactly
// one transition — the second reconciles/replays and reports the same entry, never
// a transition_id_conflict.
func TestCLIAdvanceResultReplayAcrossInvocations(t *testing.T) {
	r, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-repo", r.Repo, "-task-dir", r.TaskDir, "-result-revision", r.ResultRev, "-format", "json"}
	out1, c1 := captureAdvance(t, args)
	out2, c2 := captureAdvance(t, args) // a later invocation, wall clock advanced
	if c1 != 0 || c2 != 0 {
		t.Fatalf("exit %d/%d: %s", c1, c2, out2)
	}
	var o1, o2 advanceResultOutput
	if err := json.Unmarshal([]byte(out1), &o1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(out2), &o2); err != nil {
		t.Fatal(err)
	}
	if o2.TransitionDisposition != "replayed" && o2.TransitionDisposition != "reconciled" {
		t.Fatalf("second invocation disposition = %s, want replayed/reconciled", o2.TransitionDisposition)
	}
	if o2.TransitionEntryDigestSHA256 != o1.TransitionEntryDigestSHA256 {
		t.Fatal("second invocation reported a different transition entry")
	}
}

// TestCLIAdvanceStaleRendersAdvancedNotPreAttemptState: the renderer maps a stale
// outcome verbatim from the owner's result, so machine and human output present the
// genuinely current (advanced) phase/status and never pair current_state_available:
// true with the pre-attempt scope_verified phase. Exit code is 3.
func TestCLIAdvanceStaleRendersAdvancedNotPreAttemptState(t *testing.T) {
	// The owner's stale result after a competing writer advanced the head to proving.
	res := tasksession.AdvanceResult{
		Outcome:                 tasksession.OutcomeStale,
		CurrentStateAvailable:   true,
		CurrentLedgerHeadSHA256: "7c513be718f37b0e2e2d749128fe0158e07fc79c5c625e86591d240d47af59fb",
		TaskPhase:               closureprotocol.PhaseProving,
		OperationalStatus:       "ready_for_proving",
		RefusalCode:             "resultrecording.stale_expected_head",
	}
	out := toAdvanceOutput(res)

	if out.CurrentStateAvailable && (out.TaskPhase == "scope_verified" || out.OperationalStatus == "scope_verified") {
		t.Fatal("stale output must never pair current_state_available:true with the pre-attempt scope_verified state")
	}
	if out.TaskPhase != "proving" {
		t.Fatalf("stale output phase = %q, want the advanced current phase proving", out.TaskPhase)
	}
	if out.CorrectnessCertified {
		t.Fatal("correctness_certified must be false")
	}
	// machine output round-trips with the advanced current state.
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var round advanceResultOutput
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatal(err)
	}
	if round.TaskPhase != "proving" || !round.CurrentStateAvailable {
		t.Fatalf("machine output lost the current state: %+v", round)
	}
	if advanceExitCode(res.Outcome) != 3 {
		t.Fatalf("stale exit code = %d, want 3", advanceExitCode(res.Outcome))
	}
}

// TestCLIAdvanceResultMalformed: an unresolvable task directory is a usage error
// (exit 2), never a silent success.
func TestCLIAdvanceResultMalformed(t *testing.T) {
	empty := t.TempDir()
	if _, code := captureAdvance(t, []string{"-repo", empty}); code != 2 {
		t.Fatalf("exit %d, want 2 for an unresolvable task", code)
	}
}
