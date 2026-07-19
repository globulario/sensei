// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/convergence"
)

func TestAdvanceConvergenceRequiresExplicitInputs(t *testing.T) {
	if code := runAdvanceConvergence(nil); code != 2 {
		t.Fatalf("exit=%d, want 2", code)
	}
}

func TestAdvanceConvergenceRequiresQuestionCreatedAt(t *testing.T) {
	args := []string{
		"--closure-request", "request.yaml",
		"--claims", "claims.yaml",
		"--dialogue", "dialogue.yaml",
		"--evidence-state", "evidence.yaml",
		"--graph-nt", "graph.nt",
		"--repo", ".",
		"--output-dir", "bundle",
	}
	if code := runAdvanceConvergence(args); code != 2 {
		t.Fatalf("exit=%d, want 2", code)
	}
}

func TestConvergenceStatusShowsCompactSummary(t *testing.T) {
	dir := t.TempDir()
	session := convergence.Session{
		SchemaVersion: "1",
		GeneratedBy:   convergence.GeneratedBy,
		SessionID:     "convergence.test.abc123",
		PolicyID:      convergence.PolicyStrictV1,
		PolicyVersion: "v1",
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  "github.com/example/project",
			Revision:          strings.Repeat("a", 40),
			RevisionStatus:    architecture.RevisionResolved,
			GraphDigestSHA256: strings.Repeat("b", 64),
			GraphDigestStatus: architecture.GraphDigestResolved,
		},
		LatestStatus: convergence.StatusWaiting,
	}
	data, err := convergence.MarshalSessionYAML(session)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "session.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureConvergenceStdout(t, func() {
		if code := runConvergenceStatus([]string{"--session", path}); code != 0 {
			t.Fatalf("exit=%d", code)
		}
	})
	if !strings.Contains(out, "Session: convergence.test.abc123") || !strings.Contains(out, "Status: waiting") {
		t.Fatalf("output=%q", out)
	}
}

func TestAdvanceConvergenceCommandDoesNotUseCommandExecutionAPIs(t *testing.T) {
	data, err := os.ReadFile("cmd_advance_convergence.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{`"os/` + `exec"`, "exec." + "Command", "sh" + " -c", "ba" + "sh"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("cmd_advance_convergence.go contains forbidden token %q", forbidden)
		}
	}
}

func captureConvergenceStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
