// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture/prereview"
)

// mkPreReviewRepo builds a temp git repo with a base and head commit. gin.go is
// modified in the head commit and is protected by a seeded invariant and
// forbidden fix. It returns the repo root and the base commit revision.
func mkPreReviewRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	write := func(rel, content string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	run("remote", "add", "origin", "https://github.com/example/project.git")

	write("gin.go", "package gin\n\nfunc Route() {}\n")
	write("docs/awareness/invariants.yaml", `invariants:
  - id: gin.route_ownership
    title: Gin route ownership is preserved
    severity: high
    status: active
    protects:
      files:
        - gin.go
`)
	write("docs/awareness/forbidden_fixes.yaml", `forbidden_fixes:
  - id: cache_the_route_table
    title: cache the route table
    reason: caching the route table caused the stale-route failure
    protects:
      files:
        - gin.go
`)
	run("add", "-A")
	run("commit", "-q", "-m", "base")
	base := trimNL(run("rev-parse", "HEAD"))

	write("gin.go", "package gin\n\nfunc Route() {}\n\nfunc NewRoute() {}\n")
	run("add", "-A")
	run("commit", "-q", "-m", "head")
	return dir, base
}

func trimNL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func TestGitDiffSourceBindsChange(t *testing.T) {
	dir, base := mkPreReviewRepo(t)
	diff, err := gitDiffSource{}.ResolveReviewDiff(context.Background(), prereview.DiffRequest{RepoRoot: dir, Base: base, Head: "HEAD"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if diff.RepositoryDomain != "github.com/example/project" {
		t.Fatalf("domain = %q", diff.RepositoryDomain)
	}
	if diff.BaseTreeDigestSHA256 == "" || diff.HeadTreeDigestSHA256 == "" || diff.DiffDigestSHA256 == "" {
		t.Fatalf("missing digests: %+v", diff)
	}
	if diff.BaseTreeDigestSHA256 == diff.HeadTreeDigestSHA256 {
		t.Fatal("base and head trees should differ")
	}
	if len(diff.FilesModified) != 1 || diff.FilesModified[0] != "gin.go" {
		t.Fatalf("modified files = %v, want [gin.go]", diff.FilesModified)
	}
}

func TestLocalGraphSourceResolvesApplicableRules(t *testing.T) {
	dir, _ := mkPreReviewRepo(t)
	gc, err := localGraphSource{repoRoot: dir}.CollectArchitecturalContext(context.Background(), prereview.GraphRequest{ChangedPaths: []string{"gin.go"}})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(gc.Invariants) != 1 || gc.Invariants[0].ID != "gin.route_ownership" {
		t.Fatalf("invariants = %+v", gc.Invariants)
	}
	if len(gc.ForbiddenFixes) != 1 || gc.ForbiddenFixes[0].ID != "cache_the_route_table" {
		t.Fatalf("forbidden fixes = %+v", gc.ForbiddenFixes)
	}
	if gc.RiskClass != "architecture_sensitive" {
		t.Fatalf("risk class = %q", gc.RiskClass)
	}
	// An unrelated path resolves nothing.
	empty, err := localGraphSource{repoRoot: dir}.CollectArchitecturalContext(context.Background(), prereview.GraphRequest{ChangedPaths: []string{"unrelated/file.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Invariants) != 0 || empty.RiskClass != "standard" {
		t.Fatalf("unrelated change resolved rules: %+v", empty)
	}
}

func TestPreReviewCommandProducesAdvisoryReport(t *testing.T) {
	dir, base := mkPreReviewRepo(t)
	out := filepath.Join(t.TempDir(), "report.json")
	code := runPreReview([]string{"--repo", dir, "--base", base, "--head", "HEAD", "--format", "json", "--output", out, "--purpose", "Add a route."})
	if code != 0 {
		t.Fatalf("pre-review exit = %d, want 0", code)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var report prereview.PreReviewReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := prereview.Validate(report); err != nil {
		t.Fatalf("generated report invalid: %v", err)
	}
	if report.Coverage.Level != prereview.CoverageAdvisory {
		t.Fatalf("coverage = %q, want advisory", report.Coverage.Level)
	}
	if report.Binding.RepositoryDomain != "github.com/example/project" {
		t.Fatalf("domain = %q", report.Binding.RepositoryDomain)
	}
	if len(report.Protection.Invariants) != 1 || len(report.Protection.ForbiddenFixes) != 1 {
		t.Fatalf("protection not carried: %+v", report.Protection)
	}
	if len(report.ReviewerAttention) != 1 {
		t.Fatalf("expected one forbidden-fix concern, got %d", len(report.ReviewerAttention))
	}
}

func TestPreReviewCommandDeterministicSARIF(t *testing.T) {
	dir, base := mkPreReviewRepo(t)
	sarifPath := filepath.Join(t.TempDir(), "out.sarif")
	nullOut := filepath.Join(t.TempDir(), "r.json")
	// The seeded forbidden-fix concern has a related file (gin.go) only if the
	// graph adapter attaches one; this exercises the SARIF path end to end.
	code := runPreReview([]string{"--repo", dir, "--base", base, "--head", "HEAD", "--format", "json", "--output", nullOut, "--sarif", sarifPath})
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if _, err := os.Stat(sarifPath); err != nil {
		t.Fatalf("sarif not written: %v", err)
	}
	var log map[string]any
	raw, _ := os.ReadFile(sarifPath)
	if err := json.Unmarshal(raw, &log); err != nil {
		t.Fatalf("sarif not valid json: %v", err)
	}
	if log["version"] != "2.1.0" {
		t.Fatalf("sarif version = %v", log["version"])
	}
}

func TestPreReviewCommandNotAGitRepoFails(t *testing.T) {
	dir := t.TempDir() // no git
	code := runPreReview([]string{"--repo", dir, "--base", "HEAD", "--head", "HEAD"})
	if code == 0 {
		t.Fatal("pre-review succeeded outside a git repository")
	}
}
