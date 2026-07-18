// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/completion"
	"github.com/globulario/sensei/internal/resulttestkit"
)

// Both recovery tools are registered as typed delegations with a closed schema and no
// raw query surface.
func TestRecoveryToolsRegistered(t *testing.T) {
	b := &bridge{}
	byName := map[string]tool{}
	for _, tl := range b.tools() {
		byName[tl.Name] = tl
	}
	for _, name := range []string{"inspect_terminal", "recover_projections"} {
		tl, ok := byName[name]
		if !ok {
			t.Fatalf("%s MCP tool is not registered", name)
		}
		props, _ := tl.InputSchema["properties"].(map[string]interface{})
		if _, bad := props["sparql"]; bad {
			t.Fatalf("%s exposed a raw graph query field", name)
		}
		if ap, ok := tl.InputSchema["additionalProperties"]; !ok || ap != false {
			t.Fatalf("%s must set additionalProperties:false", name)
		}
		req, _ := tl.InputSchema["required"].([]string)
		if len(req) != 1 || req[0] != "repo" {
			t.Fatalf("%s must require exactly repo, got %v", name, req)
		}
	}
}

// inspect_terminal delegates and reports the reconstructed state; an unknown property is
// rejected at runtime, not just by schema.
func TestInspectTerminalToolDelegatesAndRejectsUnknown(t *testing.T) {
	orig := inspectTerminalDelegate
	defer func() { inspectTerminalDelegate = orig }()
	inspectTerminalDelegate = func(ctx context.Context, req completion.Request) (completion.TerminalStateAssessment, error) {
		return completion.TerminalStateAssessment{State: completion.TerminalContradictoryHistory}, nil
	}
	b := &bridge{}
	res, err := b.callTool(context.Background(), "inspect_terminal", map[string]interface{}{"repo": t.TempDir(), "task": t.TempDir()})
	if err != nil {
		t.Fatalf("delegation must not error: %v", err)
	}
	if res == nil || !strings.Contains(res.Text, "contradictory_terminal_history") {
		t.Fatalf("tool must surface the reconstructed state: %+v", res)
	}
	if _, err := b.callTool(context.Background(), "inspect_terminal", map[string]interface{}{"repo": t.TempDir(), "status": "committed"}); err == nil || !strings.Contains(err.Error(), "unknown property") {
		t.Fatalf("unknown property must be rejected at runtime, got %v", err)
	}
}

// recover_projections maps a typed outcome to a result (err nil) and an infrastructure
// error to a Go error; and against the real owner a seeded not-completed task is
// nothing_to_recover with the terminal state left untouched. Unknown property rejected.
func TestRecoverProjectionsToolMappingAndWritesNothing(t *testing.T) {
	orig := recoverProjectionsDelegate
	defer func() { recoverProjectionsDelegate = orig }()
	b := &bridge{}
	ctx := context.Background()

	recoverProjectionsDelegate = func(ctx context.Context, req completion.Request) (completion.RecoverResult, error) {
		return completion.RecoverResult{Outcome: completion.RecoverNothingToRecover}, nil
	}
	res, err := b.callTool(ctx, "recover_projections", map[string]interface{}{"repo": t.TempDir(), "task": t.TempDir()})
	if err != nil || res == nil || !strings.Contains(res.Text, "nothing_to_recover") {
		t.Fatalf("typed outcome must be a result surfacing the outcome, got res=%+v err=%v", res, err)
	}
	recoverProjectionsDelegate = func(ctx context.Context, req completion.Request) (completion.RecoverResult, error) {
		return completion.RecoverResult{}, context.DeadlineExceeded
	}
	if _, err := b.callTool(ctx, "recover_projections", map[string]interface{}{"repo": t.TempDir()}); err == nil {
		t.Fatal("an infrastructure error must surface as a Go error")
	}
	if _, err := b.callTool(ctx, "recover_projections", map[string]interface{}{"repo": t.TempDir(), "receipt": "x"}); err == nil || !strings.Contains(err.Error(), "unknown property") {
		t.Fatalf("unknown property must be rejected at runtime, got %v", err)
	}

	// Real owner: nothing to recover, and nothing written.
	recoverProjectionsDelegate = orig
	seed, serr := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if serr != nil {
		t.Fatalf("seed: %v", serr)
	}
	req := completion.Request{RepositoryRoot: seed.Repo, TaskDirectory: seed.TaskDir}
	if _, err := b.callTool(ctx, "recover_projections", map[string]interface{}{"repo": seed.Repo, "task": seed.TaskDir}); err != nil {
		t.Fatalf("real-owner recover must not error on a not-completed task: %v", err)
	}
	after, err := completion.InspectTerminalState(ctx, req)
	if err != nil {
		t.Fatalf("inspect after: %v", err)
	}
	if after.State != completion.TerminalNotCompleted {
		t.Fatalf("recovery must not manufacture completion; state=%s", after.State)
	}
}
