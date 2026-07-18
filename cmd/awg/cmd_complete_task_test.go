// SPDX-License-Identifier: AGPL-3.0-only

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
