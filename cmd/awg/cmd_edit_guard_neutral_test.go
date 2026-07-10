// SPDX-License-Identifier: Apache-2.0

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

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
)

// The neutral input seam: any agent/CI drives the guard with --file, not the
// Claude Code hook JSON.
func TestResolveGuardInput(t *testing.T) {
	dir := t.TempDir()
	cf := filepath.Join(dir, "content.txt")
	if err := os.WriteFile(cf, []byte("from content file"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("file flag + content-file", func(t *testing.T) {
		f, c, ok := resolveGuardInput("pkg/x.go", cf, []byte("stdin ignored"))
		if !ok || f != "pkg/x.go" || c != "from content file" {
			t.Fatalf("got f=%q c=%q ok=%v", f, c, ok)
		}
	})
	t.Run("file flag + stdin content", func(t *testing.T) {
		f, c, ok := resolveGuardInput("pkg/x.go", "", []byte("from stdin"))
		if !ok || f != "pkg/x.go" || c != "from stdin" {
			t.Fatalf("got f=%q c=%q ok=%v", f, c, ok)
		}
	})
	t.Run("no file flag falls back to Claude Code payload", func(t *testing.T) {
		payload, _ := json.Marshal(map[string]any{"tool_input": map[string]any{"file_path": "a.go", "content": "z"}})
		f, c, ok := resolveGuardInput("", "", payload)
		if !ok || f != "a.go" || c != "z" {
			t.Fatalf("CC fallback failed: f=%q c=%q ok=%v", f, c, ok)
		}
	})
}

// runGuardCode runs runEditGuard capturing stdout AND the exit code (unlike
// runGuardWithStdin, which pins exit 0 — the exit-code adapter returns 2).
func runGuardCode(t *testing.T, args []string, stdin string) (int, string) {
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
	return code, buf.String()
}

// The neutral output adapters: same decision core, different renderings —
// zero Claude Code dependency.
func TestRunEditGuard_NeutralAdapters(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := editGuardCheckRPC
	t.Cleanup(func() { editGuardCheckRPC = orig })

	blockResp := func() (*awarenesspb.EditCheckResponse, error) {
		return &awarenesspb.EditCheckResponse{
			RulesEvaluated: 1,
			Warnings:       []*awarenesspb.EditWarning{warnEnf("warning", "Invariant", "block")},
		}, nil
	}
	absFile := filepath.Join(root, "pkg", "x.go")

	t.Run("json format: structured verdict, exit 0", func(t *testing.T) {
		editGuardCheckRPC = func(_ context.Context, _, _, _, _ string) (*awarenesspb.EditCheckResponse, error) {
			return blockResp()
		}
		code, out := runGuardCode(t, []string{"--root", root, "--file", absFile, "--format", "json"}, "some content")
		if code != 0 {
			t.Fatalf("json format must fail-open exit 0, got %d", code)
		}
		var v struct {
			File     string              `json:"file"`
			Decision string              `json:"decision"`
			Reason   string              `json:"reason"`
			Warnings []map[string]string `json:"warnings"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &v); err != nil {
			t.Fatalf("stdout not JSON: %q (%v)", out, err)
		}
		if v.Decision != "block" || v.File != filepath.Join("pkg", "x.go") {
			t.Errorf("verdict = %+v", v)
		}
		if len(v.Warnings) != 1 || v.Warnings[0]["enforcement"] != "block" {
			t.Errorf("warnings not surfaced structurally: %+v", v.Warnings)
		}
	})

	t.Run("exit-code format: block => exit 2", func(t *testing.T) {
		editGuardCheckRPC = func(_ context.Context, _, _, _, _ string) (*awarenesspb.EditCheckResponse, error) {
			return blockResp()
		}
		code, out := runGuardCode(t, []string{"--root", root, "--file", absFile, "--format", "exit-code"}, "some content")
		if code != 2 {
			t.Fatalf("exit-code block must exit 2, got %d (stdout=%q)", code, out)
		}
	})

	t.Run("exit-code format: allow => exit 0", func(t *testing.T) {
		editGuardCheckRPC = func(_ context.Context, _, _, _, _ string) (*awarenesspb.EditCheckResponse, error) {
			return &awarenesspb.EditCheckResponse{RulesEvaluated: 0}, nil
		}
		code, _ := runGuardCode(t, []string{"--root", root, "--file", absFile, "--format", "exit-code"}, "some content")
		if code != 0 {
			t.Fatalf("exit-code allow must exit 0, got %d", code)
		}
	})

	t.Run("exit-code fails open on server error (exit 0)", func(t *testing.T) {
		editGuardCheckRPC = func(_ context.Context, _, _, _, _ string) (*awarenesspb.EditCheckResponse, error) {
			return nil, context.DeadlineExceeded
		}
		code, _ := runGuardCode(t, []string{"--root", root, "--file", absFile, "--format", "exit-code"}, "some content")
		if code != 0 {
			t.Fatalf("guard must fail OPEN even in exit-code mode, got %d", code)
		}
	})

	t.Run("json format allow: empty warnings, decision allow", func(t *testing.T) {
		editGuardCheckRPC = func(_ context.Context, _, _, _, _ string) (*awarenesspb.EditCheckResponse, error) {
			return &awarenesspb.EditCheckResponse{RulesEvaluated: 0}, nil
		}
		code, out := runGuardCode(t, []string{"--root", root, "--file", absFile, "--format", "json"}, "some content")
		if code != 0 {
			t.Fatalf("code=%d", code)
		}
		if !strings.Contains(out, `"decision":"allow"`) {
			t.Errorf("expected allow verdict, got %q", out)
		}
	})
}
