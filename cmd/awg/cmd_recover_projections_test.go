// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/completion"
	"github.com/globulario/sensei/internal/resulttestkit"
)

func TestRunRecoverProjections_RejectsPositionalAndBadFormat(t *testing.T) {
	if code := runRecoverProjections([]string{"--repo", t.TempDir(), "rebuild"}); code != 2 {
		t.Fatalf("positional argument must exit 2, got %d", code)
	}
	if code := runRecoverProjections([]string{"--repo", t.TempDir(), "--format", "xml"}); code != 2 {
		t.Fatalf("bad --format must exit 2, got %d", code)
	}
}

// The surface maps the owner's whole closed recovery outcome set to exit codes: only the
// two outcomes where derived projections are current against a valid terminal fact are 0;
// every other outcome — including a contradiction that must NOT be normalized — is 1.
func TestRecoverExitCodeMapping(t *testing.T) {
	zero := map[completion.RecoverOutcome]bool{
		completion.RecoverProjectionsRebuilt: true,
		completion.RecoverAlreadyCurrent:     true,
	}
	all := []completion.RecoverOutcome{
		completion.RecoverProjectionsRebuilt, completion.RecoverAlreadyCurrent, completion.RecoverNothingToRecover,
		completion.RecoverContradictory, completion.RecoverBrokenCompletion, completion.RecoverUnsupported,
		completion.RecoverInputInvalid,
	}
	if len(all) != 7 {
		t.Fatalf("expected the full closed recovery-outcome set of 7, listed %d", len(all))
	}
	for _, o := range all {
		want := 1
		if zero[o] {
			want = 0
		}
		if got := recoverExitCode(o); got != want {
			t.Fatalf("exit code for %s = %d, want %d", o, got, want)
		}
	}
}

// A success outcome renders and exits 0; a contradiction renders and exits 1 without the
// surface normalizing it. Injected delegate so both paths are proven directly.
func TestRunRecoverProjections_SuccessAndRefusalRender(t *testing.T) {
	orig := recoverProjectionsDelegate
	defer func() { recoverProjectionsDelegate = orig }()

	recoverProjectionsDelegate = func(ctx context.Context, req completion.Request) (completion.RecoverResult, error) {
		after := completion.TerminalStateAssessment{State: completion.TerminalCommitted}
		return completion.RecoverResult{Outcome: completion.RecoverProjectionsRebuilt, After: &after}, nil
	}
	var code int
	out := captureStdout(t, func() {
		code = runRecoverProjections([]string{"--repo", t.TempDir(), "--task-dir", t.TempDir()})
	})
	if code != 0 || !strings.Contains(out, "projections_rebuilt") {
		t.Fatalf("rebuilt must exit 0 and render, got code=%d out=%q", code, out)
	}

	recoverProjectionsDelegate = func(ctx context.Context, req completion.Request) (completion.RecoverResult, error) {
		return completion.RecoverResult{Outcome: completion.RecoverContradictory, Detail: "two completed events"}, nil
	}
	out = captureStdout(t, func() {
		code = runRecoverProjections([]string{"--repo", t.TempDir(), "--task-dir", t.TempDir()})
	})
	if code != 1 || !strings.Contains(out, "contradictory_terminal_history") {
		t.Fatalf("contradiction must exit 1 and render, got code=%d out=%q", code, out)
	}
}

// Against the real owner: a seeded-but-not-completed task has nothing to recover, so the
// surface reports nothing_to_recover (exit 1) and the owner writes NOTHING — the terminal
// state stays not_completed across a replay. Recovery can never manufacture a completion.
func TestRunRecoverProjections_RealOwnerRefusesWritesNothingIdempotent(t *testing.T) {
	seed, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if err != nil {
		t.Fatalf("seed: %v", err)
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
	for _, pass := range []string{"first", "replay"} {
		if code := runRecoverProjections([]string{"--repo", seed.Repo, "--task-dir", seed.TaskDir}); code != 1 {
			t.Fatalf("%s: nothing-to-recover must exit 1, got %d", pass, code)
		}
		after, ierr := completion.InspectTerminalState(ctx, req)
		if ierr != nil {
			t.Fatalf("%s inspect: %v", pass, ierr)
		}
		if after.State != completion.TerminalNotCompleted {
			t.Fatalf("%s: recovery must write nothing; state=%s", pass, after.State)
		}
	}
}
