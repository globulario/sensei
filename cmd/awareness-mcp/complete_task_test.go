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
	// A refusal surfaces as a structured outcome (callErr == nil); an infrastructure
	// failure surfaces as a Go error. Either way, completion must NOT be manufactured.
	if callErr == nil {
		if res == nil || !strings.Contains(res.Text, "outcome:") {
			t.Fatalf("tool must surface an outcome, got %+v", res)
		}
		if strings.Contains(res.Text, "committed") {
			t.Fatalf("an unready task must not be committed: %q", res.Text)
		}
	}
	after, err := completion.InspectTerminalState(ctx, req)
	if err != nil {
		t.Fatalf("inspect after: %v", err)
	}
	if after.State != completion.TerminalNotCompleted {
		t.Fatalf("MCP delegation must not manufacture completion; state=%s", after.State)
	}
}
