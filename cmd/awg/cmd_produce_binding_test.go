// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Without a supported GitHub pull_request event (i.e. local execution), the producer CLI
// fails loudly (exit 1) and writes a typed failed audit — local is never promoted to
// authoritative, and the failure is a producer reason, never a 9.4b runtime reason.
func TestProduceChangeBinding_LocalIsNotAuthoritative(t *testing.T) {
	t.Setenv("GITHUB_EVENT_NAME", "") // no authoritative event
	dir := t.TempDir()
	audit := filepath.Join(dir, "audit.json")
	code := runProduceChangeBinding([]string{
		"--repo-root", dir, "--output", filepath.Join(dir, "binding.yaml"), "--audit", audit,
		"--task-dir", ".sensei/tasks/task.x", "--task-id", "task.x", "--session-id", "session.x",
		"--completion-digest", "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		"--completion-task", "task.x", "--completion-session", "session.x",
	})
	if code != 1 {
		t.Fatalf("local (no authoritative event) must fail with exit 1, got %d", code)
	}
	data, err := os.ReadFile(audit)
	if err != nil {
		t.Fatalf("a typed audit must be written even on failure: %v", err)
	}
	var a struct {
		Outcome string `json:"outcome"`
		Failure string `json:"failure"`
	}
	if err := json.Unmarshal(data, &a); err != nil {
		t.Fatal(err)
	}
	if a.Outcome != "failed" || a.Failure != "unsupported_github_event" {
		t.Fatalf("expected failed/unsupported_github_event audit, got %+v", a)
	}
	if _, err := os.Stat(filepath.Join(dir, "binding.yaml")); err == nil {
		t.Fatal("no binding artifact must be published on failure")
	}
}
