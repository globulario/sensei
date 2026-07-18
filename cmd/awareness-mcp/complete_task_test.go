// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/completion"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/internal/resulttestkit"
)

// The complete_task tool is registered as a typed delegation and exposes no raw query
// surface (respecting awareness.no_arbitrary_sparql).
func TestCompleteTaskToolRegistered(t *testing.T) {
	b := &bridge{}
	var found *tool
	for i, tl := range b.tools() {
		if tl.Name == "complete_task" {
			found = &b.tools()[i]
			break
		}
	}
	if found == nil {
		t.Fatal("complete_task MCP tool is not registered")
	}
	props, _ := found.InputSchema["properties"].(map[string]interface{})
	if _, ok := props["sparql"]; ok {
		t.Fatal("complete_task exposed a raw graph query field")
	}
	req, _ := found.InputSchema["required"].([]string)
	seen := map[string]bool{}
	for _, r := range req {
		seen[r] = true
	}
	if !seen["repo"] || !seen["expected_head"] {
		t.Fatalf("complete_task must require repo + expected_head, got %v", req)
	}
}

// The MCP tool only delegates: a seeded-but-unready task is refused, the outcome is
// surfaced in the tool result, and the owner writes nothing — the tool cannot manufacture
// completion.
func TestCompleteTaskToolDelegatesRefusesWritesNothing(t *testing.T) {
	seed, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	report, err := ledger.VerifyTaskLedger(seed.TaskDir)
	if err != nil {
		t.Fatalf("verify ledger: %v", err)
	}
	ctx := context.Background()
	req := completion.Request{RepositoryRoot: seed.Repo, TaskDirectory: seed.TaskDir}

	b := &bridge{}
	res, callErr := b.callTool(ctx, "complete_task", map[string]interface{}{
		"repo":          seed.Repo,
		"task":          seed.TaskDir,
		"expected_head": report.HeadDigestSHA256,
	})
	// The seeded task's refusal is a typed owner outcome, so it MUST surface as a
	// structured result with no Go error — and it must be exactly not_ready.
	if callErr != nil {
		t.Fatalf("a typed refusal must surface as a result, not an error: %v", callErr)
	}
	if res == nil || !strings.Contains(res.Text, "not_ready") {
		t.Fatalf("the seeded task must surface not_ready as a structured outcome, got %+v", res)
	}
	after, err := completion.InspectTerminalState(ctx, req)
	if err != nil {
		t.Fatalf("inspect after: %v", err)
	}
	if after.State != completion.TerminalNotCompleted {
		t.Fatalf("MCP delegation must not manufacture completion; state=%s", after.State)
	}
}

// The tool rejects caller-supplied claims both by schema (additionalProperties:false) and
// at runtime — a schema declaration alone is an advisory lock painted on the door.
func TestCompleteTaskToolRejectsUnknownProperty(t *testing.T) {
	b := &bridge{}
	var declared bool
	for _, tl := range b.tools() {
		if tl.Name == "complete_task" {
			ap, ok := tl.InputSchema["additionalProperties"]
			if !ok || ap != false {
				t.Fatalf("complete_task schema must set additionalProperties:false, got %v", tl.InputSchema["additionalProperties"])
			}
			declared = true
		}
	}
	if !declared {
		t.Fatal("complete_task tool not found")
	}
	_, err := b.callTool(context.Background(), "complete_task", map[string]interface{}{
		"repo":          t.TempDir(),
		"expected_head": strings.Repeat("a", 64),
		"status":        "completed",
	})
	if err == nil || !strings.Contains(err.Error(), "unknown property") {
		t.Fatalf("an unknown property must be rejected at runtime, got %v", err)
	}
}

// A typed outcome (success or refusal) is a structured result with err == nil; an
// infrastructure error is a Go error, never a fabricated outcome. Injected delegate so
// all paths are proven without the readiness world.
func TestCompleteTaskToolOutcomeMapping(t *testing.T) {
	orig := completeTaskDelegate
	defer func() { completeTaskDelegate = orig }()
	b := &bridge{}
	ctx := context.Background()
	args := map[string]interface{}{"repo": t.TempDir(), "task": t.TempDir(), "expected_head": strings.Repeat("a", 64)}

	for _, outcome := range []completion.Outcome{completion.OutcomeCommitted, completion.OutcomeNotReady} {
		completeTaskDelegate = func(ctx context.Context, req completion.CompleteRequest) (completion.CompleteResult, error) {
			return completion.CompleteResult{Outcome: outcome}, nil
		}
		res, err := b.callTool(ctx, "complete_task", args)
		if err != nil {
			t.Fatalf("typed outcome %s must be a result, not an error: %v", outcome, err)
		}
		if res == nil || !strings.Contains(res.Text, string(outcome)) {
			t.Fatalf("tool must surface outcome %s: %+v", outcome, res)
		}
	}

	completeTaskDelegate = func(ctx context.Context, req completion.CompleteRequest) (completion.CompleteResult, error) {
		return completion.CompleteResult{}, fmt.Errorf("lock unavailable")
	}
	if _, err := b.callTool(ctx, "complete_task", args); err == nil {
		t.Fatal("an infrastructure error must surface as a Go error, not a fabricated outcome")
	}
}
