// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/globulario/sensei/golang/architecture/resulttestkit"
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
	if o.CorrectnessCertified {
		t.Fatal("correctness_certified must be false")
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
