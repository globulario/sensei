// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runEditBriefWithStdin(t *testing.T, args []string, stdin string) string {
	t.Helper()
	oldIn, oldOut := os.Stdin, os.Stdout
	inR, inW, _ := os.Pipe()
	outR, outW, _ := os.Pipe()
	os.Stdin, os.Stdout = inR, outW
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()

	go func() { _, _ = io.WriteString(inW, stdin); _ = inW.Close() }()
	code := runEditBrief(args)
	_ = outW.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, outR)
	if code != 0 {
		t.Fatalf("runEditBrief exit = %d, want 0 (always fail-open)", code)
	}
	return buf.String()
}

func TestEditBrief(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, "pkg", "x.go")
	payload := func(f string) string {
		b, _ := json.Marshal(map[string]any{"tool_input": map[string]any{"file_path": f, "new_string": "y := 1"}})
		return string(b)
	}
	type hookOut struct {
		HookSpecificOutput struct {
			HookEventName      string `json:"hookEventName"`
			PermissionDecision string `json:"permissionDecision"`
			AdditionalContext  string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}

	t.Run("pushes briefing prose as additionalContext (allow)", func(t *testing.T) {
		var gotFile, gotDepth string
		editBriefRPC = func(_ context.Context, _, file, depth, _ string) (string, error) {
			gotFile, gotDepth = file, depth
			return "INVARIANT payments.paid_state — money truth comes from the processor's confirmation.", nil
		}
		out := runEditBriefWithStdin(t, []string{"--root", root}, payload(abs))
		var d hookOut
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &d); err != nil {
			t.Fatalf("stdout not JSON: %q (%v)", out, err)
		}
		hso := d.HookSpecificOutput
		if hso.HookEventName != "PreToolUse" || hso.PermissionDecision != "allow" {
			t.Errorf("got event=%q decision=%q, want PreToolUse/allow", hso.HookEventName, hso.PermissionDecision)
		}
		if !strings.Contains(hso.AdditionalContext, "payments.paid_state") {
			t.Errorf("additionalContext missing briefing prose: %q", hso.AdditionalContext)
		}
		if !strings.Contains(hso.AdditionalContext, filepath.Join("pkg", "x.go")) {
			t.Errorf("additionalContext missing file: %q", hso.AdditionalContext)
		}
		if gotFile != filepath.Join("pkg", "x.go") {
			t.Errorf("briefing called with %q, want pkg/x.go", gotFile)
		}
		if gotDepth != "agent_compact" {
			t.Errorf("depth = %q, want agent_compact (default)", gotDepth)
		}
	})

	t.Run("silent when nothing anchors (empty prose)", func(t *testing.T) {
		editBriefRPC = func(_ context.Context, _, _, _, _ string) (string, error) { return "   \n ", nil }
		if out := runEditBriefWithStdin(t, []string{"--root", root}, payload(abs)); strings.TrimSpace(out) != "" {
			t.Errorf("expected no output for empty briefing, got %q", out)
		}
	})

	t.Run("silent and fail-open when the server is unreachable", func(t *testing.T) {
		editBriefRPC = func(_ context.Context, _, _, _, _ string) (string, error) { return "", context.DeadlineExceeded }
		if out := runEditBriefWithStdin(t, []string{"--root", root}, payload(abs)); strings.TrimSpace(out) != "" {
			t.Errorf("expected no stdout when server unreachable, got %q", out)
		}
	})

	t.Run("edit outside the project is ignored (no RPC, no output)", func(t *testing.T) {
		called := false
		editBriefRPC = func(_ context.Context, _, _, _, _ string) (string, error) { called = true; return "x", nil }
		out := runEditBriefWithStdin(t, []string{"--root", root}, payload("/etc/passwd"))
		if strings.TrimSpace(out) != "" || called {
			t.Errorf("out-of-project edit should be ignored (out=%q called=%v)", out, called)
		}
	})
}
