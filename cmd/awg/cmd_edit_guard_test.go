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

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func sp(s string) *string { return &s }

func TestExtractGuardTarget(t *testing.T) {
	cases := []struct {
		name        string
		payload     string
		wantFile    string
		wantContent string
		wantOK      bool
	}{
		{
			name:        "write uses content",
			payload:     `{"tool_input":{"file_path":"a.go","content":"package a"}}`,
			wantFile:    "a.go",
			wantContent: "package a",
			wantOK:      true,
		},
		{
			name:        "edit uses new_string",
			payload:     `{"tool_input":{"file_path":"b.go","old_string":"x","new_string":"y := fmt.Errorf(\"z\")"}}`,
			wantFile:    "b.go",
			wantContent: "y := fmt.Errorf(\"z\")",
			wantOK:      true,
		},
		{
			name:        "multiedit joins new_strings",
			payload:     `{"tool_input":{"file_path":"c.go","edits":[{"new_string":"one"},{"new_string":"two"}]}}`,
			wantFile:    "c.go",
			wantContent: "one\ntwo",
			wantOK:      true,
		},
		{
			name:    "no file path",
			payload: `{"tool_input":{"content":"x"}}`,
			wantOK:  false,
		},
		{
			name:        "pure deletion yields empty content",
			payload:     `{"tool_input":{"file_path":"d.go","old_string":"gone"}}`,
			wantFile:    "d.go",
			wantContent: "",
			wantOK:      true,
		},
		{
			name:    "garbage payload",
			payload: `not json`,
			wantOK:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file, content, ok := extractGuardTarget([]byte(tc.payload))
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if file != tc.wantFile {
				t.Errorf("file = %q, want %q", file, tc.wantFile)
			}
			if content != tc.wantContent {
				t.Errorf("content = %q, want %q", content, tc.wantContent)
			}
		})
	}
}

func warn(sev, class string) *awarenesspb.EditWarning {
	return &awarenesspb.EditWarning{Severity: sev, RuleId: "rule.x", Class: class, Message: "msg"}
}

func warnEnf(sev, class, enforcement string) *awarenesspb.EditWarning {
	w := warn(sev, class)
	w.Enforcement = enforcement
	return w
}

func TestDecideGuard(t *testing.T) {
	defaults := guardOptions{blockSeverity: parseBlockSeverity("critical,high")}
	advisoryOpt := guardOptions{advisory: true, blockSeverity: parseBlockSeverity("critical,high")}

	t.Run("no warnings allows", func(t *testing.T) {
		d := decideGuard("f.go", nil, defaults)
		if d.block || d.advisoryText != "" {
			t.Fatalf("expected clean allow, got %+v", d)
		}
	})

	t.Run("enforcement block blocks (real edit_check signal)", func(t *testing.T) {
		// edit_check always stamps severity="warning"; the authored block intent
		// rides on Enforcement. This is the path a real detect rule exercises.
		d := decideGuard("f.go", []*awarenesspb.EditWarning{warnEnf("warning", "Invariant", "block")}, defaults)
		if !d.block {
			t.Fatal("expected block on enforcement=block")
		}
		if !strings.Contains(d.reason, "f.go") || !strings.Contains(d.reason, "rule.x") {
			t.Errorf("reason missing context: %q", d.reason)
		}
	})

	t.Run("critical severity blocks (config fallback)", func(t *testing.T) {
		d := decideGuard("f.go", []*awarenesspb.EditWarning{warn("critical", "invariant")}, defaults)
		if !d.block {
			t.Fatal("expected block on critical severity")
		}
	})

	t.Run("forbidden class blocks regardless of severity", func(t *testing.T) {
		d := decideGuard("f.go", []*awarenesspb.EditWarning{warn("info", "forbidden_fix")}, defaults)
		if !d.block {
			t.Fatal("expected block on forbidden-fix class")
		}
	})

	t.Run("low severity warns but allows", func(t *testing.T) {
		d := decideGuard("f.go", []*awarenesspb.EditWarning{warn("warning", "invariant")}, defaults)
		if d.block {
			t.Fatal("did not expect block on low severity")
		}
		if d.advisoryText == "" {
			t.Fatal("expected advisory text for a surfaced warning")
		}
	})

	t.Run("advisory mode never blocks", func(t *testing.T) {
		d := decideGuard("f.go", []*awarenesspb.EditWarning{warn("critical", "forbidden_fix")}, advisoryOpt)
		if d.block {
			t.Fatal("advisory mode must not block")
		}
		if d.advisoryText == "" {
			t.Fatal("advisory mode should still surface the warning")
		}
	})
}

func TestRelWithinRoot(t *testing.T) {
	root := t.TempDir()
	if rel, ok := relWithinRoot(root, filepath.Join(root, "pkg", "x.go")); !ok || rel != filepath.Join("pkg", "x.go") {
		t.Errorf("within-root abs: rel=%q ok=%v", rel, ok)
	}
	if _, ok := relWithinRoot(root, filepath.Join(root, "..", "outside.go")); ok {
		t.Error("expected out-of-root path to be rejected")
	}
}

// TestRunEditGuard_EndToEnd drives runEditGuard with a stubbed edit_check and a
// piped stdin, asserting the block decision reaches stdout as valid hook JSON.
func TestRunEditGuard_EndToEnd(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}

	orig := editGuardCheckRPC
	t.Cleanup(func() { editGuardCheckRPC = orig })

	// Claude Code passes an absolute file path resolved from the workspace.
	absFile := filepath.Join(root, "pkg", "x.go")
	payload := func(f string) string {
		b, _ := json.Marshal(map[string]any{"tool_input": map[string]any{"file_path": f, "content": "x"}})
		return string(b)
	}

	t.Run("blocks on an enforcement=block warning", func(t *testing.T) {
		var gotFile string
		editGuardCheckRPC = func(_ context.Context, _, file, _, _ string) (*awarenesspb.EditCheckResponse, error) {
			gotFile = file
			return &awarenesspb.EditCheckResponse{
				RulesEvaluated: 1,
				Warnings:       []*awarenesspb.EditWarning{warnEnf("warning", "Invariant", "block")},
			}, nil
		}
		out := runGuardWithStdin(t, []string{"--root", root}, payload(absFile))
		var decision struct {
			HookSpecificOutput struct {
				HookEventName            string `json:"hookEventName"`
				PermissionDecision       string `json:"permissionDecision"`
				PermissionDecisionReason string `json:"permissionDecisionReason"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &decision); err != nil {
			t.Fatalf("stdout not JSON: %q (%v)", out, err)
		}
		hso := decision.HookSpecificOutput
		if hso.HookEventName != "PreToolUse" {
			t.Errorf("hookEventName = %q, want PreToolUse", hso.HookEventName)
		}
		if hso.PermissionDecision != "deny" {
			t.Errorf("permissionDecision = %q, want deny", hso.PermissionDecision)
		}
		if gotFile != filepath.Join("pkg", "x.go") {
			t.Errorf("edit_check called with file %q, want repo-relative pkg/x.go", gotFile)
		}
		if !strings.Contains(hso.PermissionDecisionReason, "pkg/x.go") {
			t.Errorf("reason missing file: %q", hso.PermissionDecisionReason)
		}
	})

	t.Run("allows when no warnings (empty stdout)", func(t *testing.T) {
		editGuardCheckRPC = func(_ context.Context, _, _, _, _ string) (*awarenesspb.EditCheckResponse, error) {
			return &awarenesspb.EditCheckResponse{RulesEvaluated: 0}, nil
		}
		out := runGuardWithStdin(t, []string{"--root", root}, payload(absFile))
		if strings.TrimSpace(out) != "" {
			t.Errorf("expected empty stdout on allow, got %q", out)
		}
	})

	t.Run("fails open when server errors", func(t *testing.T) {
		editGuardCheckRPC = func(_ context.Context, _, _, _, _ string) (*awarenesspb.EditCheckResponse, error) {
			return nil, context.DeadlineExceeded
		}
		out := runGuardWithStdin(t, []string{"--root", root}, payload(absFile))
		if strings.TrimSpace(out) != "" {
			t.Errorf("fail-open must not print a block decision, got %q", out)
		}
	})

	t.Run("allows a file outside the project", func(t *testing.T) {
		editGuardCheckRPC = func(_ context.Context, _, _, _, _ string) (*awarenesspb.EditCheckResponse, error) {
			t.Fatal("edit_check must not be called for an out-of-project file")
			return nil, nil
		}
		out := runGuardWithStdin(t, []string{"--root", root}, payload("/etc/passwd"))
		if strings.TrimSpace(out) != "" {
			t.Errorf("out-of-project edit must not block, got %q", out)
		}
	})
}

// runGuardWithStdin runs runEditGuard with the given args and stdin, returning
// captured stdout.
func runGuardWithStdin(t *testing.T, args []string, stdin string) string {
	t.Helper()
	oldIn, oldOut := os.Stdin, os.Stdout
	inR, inW, _ := os.Pipe()
	outR, outW, _ := os.Pipe()
	os.Stdin, os.Stdout = inR, outW
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()

	go func() { _, _ = io.WriteString(inW, stdin); _ = inW.Close() }()
	code := runEditGuard(args)
	_ = outW.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, outR)
	if code != 0 {
		t.Fatalf("runEditGuard exit code = %d, want 0 (always fail-open)", code)
	}
	return buf.String()
}
