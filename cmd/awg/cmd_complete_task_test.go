// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/completion"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/internal/resulttestkit"
)

// The surface validates only its own flags; everything else is the owner's.
func TestRunCompleteTask_RequiresFlags(t *testing.T) {
	if code := runCompleteTask(nil); code != 2 {
		t.Fatalf("missing --expected-head must exit 2, got %d", code)
	}
	if code := runCompleteTask([]string{"--expected-head", strings.Repeat("a", 64), "--format", "xml"}); code != 2 {
		t.Fatalf("bad --format must exit 2, got %d", code)
	}
}

// A completion claim pushed as a positional argument is rejected, not ignored.
func TestRunCompleteTask_RejectsPositional(t *testing.T) {
	if code := runCompleteTask([]string{"--repo", t.TempDir(), "--expected-head", strings.Repeat("a", 64), "completed"}); code != 2 {
		t.Fatalf("positional argument must exit 2, got %d", code)
	}
}

// The surface maps the owner's whole closed outcome set to exit codes: only the two
// success outcomes are 0; every refusal or failure is 1. It invents no outcome.
func TestCompleteTaskExitCodeMapping(t *testing.T) {
	zero := map[completion.Outcome]bool{
		completion.OutcomeCommitted:   true,
		completion.OutcomeExactReplay: true,
	}
	all := []completion.Outcome{
		completion.OutcomeCommitted, completion.OutcomeExactReplay, completion.OutcomeNotReady,
		completion.OutcomeStaleExpectedHead, completion.OutcomeAuthorityRefusal, completion.OutcomeIntegrityFailure,
		completion.OutcomeConflictingCompletion, completion.OutcomeLedgerInvalid, completion.OutcomeInputInvalid,
	}
	if len(all) != 9 {
		t.Fatalf("expected the full closed outcome set of 9, listed %d", len(all))
	}
	for _, o := range all {
		want := 1
		if zero[o] {
			want = 0
		}
		if got := completeTaskExitCode(o); got != want {
			t.Fatalf("exit code for %s = %d, want %d", o, got, want)
		}
	}
}

// Both success outcomes render their outcome + receipt and exit 0. Uses an injected
// delegate so the success paths are proven without reconstructing the readiness world.
func TestRunCompleteTask_SuccessPathsRenderAndExitZero(t *testing.T) {
	orig := completeTaskDelegate
	defer func() { completeTaskDelegate = orig }()

	for _, outcome := range []completion.Outcome{completion.OutcomeCommitted, completion.OutcomeExactReplay} {
		completeTaskDelegate = func(ctx context.Context, req completion.CompleteRequest) (completion.CompleteResult, error) {
			return completion.CompleteResult{
				Outcome:     outcome,
				ReceiptPath: "/store/receipt.json",
				Receipt:     &completion.TerminalCompletionReceipt{ReceiptDigestSHA256: "rd", CausalIdentitySHA256: "ci"},
			}, nil
		}
		var code int
		out := captureStdout(t, func() {
			code = runCompleteTask([]string{"--repo", t.TempDir(), "--task-dir", t.TempDir(), "--expected-head", strings.Repeat("a", 64)})
		})
		if code != 0 {
			t.Fatalf("%s must exit 0, got %d", outcome, code)
		}
		if !strings.Contains(out, string(outcome)) {
			t.Fatalf("%s output must render the outcome: %q", outcome, out)
		}
		if !strings.Contains(out, "/store/receipt.json") {
			t.Fatalf("%s output must render the receipt path: %q", outcome, out)
		}
	}
}

// The invocation surface only delegates: a seeded-but-unready task is refused, the
// refusal is reported (exit 1, never a success outcome), the owner writes nothing, and a
// caller cannot manufacture completion — not with a correct head, a replayed call, or a
// forged head.
func TestRunCompleteTask_RefusalSurfacedWritesNothing(t *testing.T) {
	seed, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	report, err := ledger.VerifyTaskLedger(seed.TaskDir)
	if err != nil {
		t.Fatalf("verify ledger: %v", err)
	}
	head := report.HeadDigestSHA256
	if head == "" {
		t.Fatal("seeded ledger has no head")
	}
	ctx := context.Background()
	req := completion.Request{RepositoryRoot: seed.Repo, TaskDirectory: seed.TaskDir}

	before, err := completion.InspectTerminalState(ctx, req)
	if err != nil {
		t.Fatalf("inspect before: %v", err)
	}
	if before.State != completion.TerminalNotCompleted {
		t.Fatalf("seeded task should start not_completed, got %s", before.State)
	}

	// Correct head, valid inputs, unready task ⇒ refusal (exit 1), nothing written.
	if code := runCompleteTask([]string{"--repo", seed.Repo, "--task-dir", seed.TaskDir, "--expected-head", head}); code != 1 {
		t.Fatalf("unready completion must exit 1, got %d", code)
	}
	assertStillNotCompleted(t, ctx, req, "after first delegation")

	// Idempotent refusal: the surface holds no state; a replay refuses identically.
	if code := runCompleteTask([]string{"--repo", seed.Repo, "--task-dir", seed.TaskDir, "--expected-head", head}); code != 1 {
		t.Fatalf("replayed unready completion must exit 1, got %d", code)
	}
	assertStillNotCompleted(t, ctx, req, "after replay")

	// A forged (well-formed) head is a stale-head refusal, never a bypass.
	if code := runCompleteTask([]string{"--repo", seed.Repo, "--task-dir", seed.TaskDir, "--expected-head", strings.Repeat("b", 64)}); code != 1 {
		t.Fatalf("forged expected-head must exit 1, got %d", code)
	}
	assertStillNotCompleted(t, ctx, req, "after forged head")
}

func assertStillNotCompleted(t *testing.T, ctx context.Context, req completion.Request, when string) {
	t.Helper()
	st, err := completion.InspectTerminalState(ctx, req)
	if err != nil {
		t.Fatalf("inspect %s: %v", when, err)
	}
	if st.State != completion.TerminalNotCompleted {
		t.Fatalf("no refusal may complete the task (%s); state=%s", when, st.State)
	}
}
