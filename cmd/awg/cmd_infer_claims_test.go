// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
)

func TestInferClaimsDefaultWritesStdoutOnly(t *testing.T) {
	root := writeInferClaimsFixture(t, false)
	code, stdout, stderr := captureStdoutStderr(t, func() int {
		return runInferClaims([]string{"--repo", root})
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "architecture_claims:") || !strings.Contains(stdout, "promotion_status: candidate") {
		t.Fatalf("missing claim yaml:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "awareness", "candidates")); !os.IsNotExist(err) {
		t.Fatalf("default run wrote files, stat err=%v", err)
	}
}

func TestInferClaimsListRulesDoesNotScanRepository(t *testing.T) {
	code, stdout, stderr := captureStdoutStderr(t, func() int {
		return runInferClaims([]string{"--repo", filepath.Join(t.TempDir(), "missing"), "--list-rules", "--format", "json"})
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "rule.observed_guard_behavior.v1") || strings.Contains(stderr, "must be an existing directory") {
		t.Fatalf("list-rules scanned repo or missed rules:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
}

func TestInferClaimsRejectsUnknownRule(t *testing.T) {
	root := writeInferClaimsFixture(t, false)
	code, _, _ := captureStdoutStderr(t, func() int {
		return runInferClaims([]string{"--repo", root, "--rule", "rule.missing.v1"})
	})
	if code != 2 {
		t.Fatalf("code=%d, want 2 for invalid rule", code)
	}
}

func TestInferClaimsRuleFilter(t *testing.T) {
	root := writeInferClaimsFixture(t, false)
	code, stdout, stderr := captureStdoutStderr(t, func() int {
		return runInferClaims([]string{"--repo", root, "--rule", "rule.rule_signaling_test_expectation.v1"})
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "asserts_rule") || strings.Contains(stdout, "has_observed_writer_set") {
		t.Fatalf("rule filter failed:\n%s", stdout)
	}
}

func TestInferClaimsCheckFresh(t *testing.T) {
	root := writeInferClaimsFixture(t, false)
	out := filepath.Join(root, "claims.yaml")
	if code := runInferClaims([]string{"--repo", root, "--output", out}); code != 0 {
		t.Fatalf("write code=%d", code)
	}
	code, _, stderr := captureStdoutStderr(t, func() int {
		return runInferClaims([]string{"--repo", root, "--output", out, "--check"})
	})
	if code != 0 || !strings.Contains(stderr, "fresh") {
		t.Fatalf("check code=%d stderr=%s", code, stderr)
	}
}

func TestInferClaimsCheckStale(t *testing.T) {
	root := writeInferClaimsFixture(t, false)
	out := filepath.Join(root, "claims.yaml")
	writeFile(t, out, "stale\n")
	code, _, stderr := captureStdoutStderr(t, func() int {
		return runInferClaims([]string{"--repo", root, "--output", out, "--check"})
	})
	if code != 1 || !strings.Contains(stderr, "STALE") {
		t.Fatalf("check code=%d stderr=%s", code, stderr)
	}
}

func TestInferClaimsRejectsActiveAwarenessOutputPath(t *testing.T) {
	root := writeInferClaimsFixture(t, false)
	out := filepath.Join(root, "docs", "awareness", "architecture_claims.yaml")
	code, _, _ := captureStdoutStderr(t, func() int {
		return runInferClaims([]string{"--repo", root, "--output", out})
	})
	if code != 2 {
		t.Fatalf("code=%d, want 2", code)
	}
}

func TestInferClaimsAllowsCandidateDirectoryOutput(t *testing.T) {
	root := writeInferClaimsFixture(t, false)
	out := filepath.Join(root, "docs", "awareness", "candidates", "architecture_claims.yaml")
	code, _, stderr := captureStdoutStderr(t, func() int {
		return runInferClaims([]string{"--repo", root, "--output", out})
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("candidate output missing: %v", err)
	}
}

func TestInferClaimsWithoutGraphDigestEmitsUnknownClaims(t *testing.T) {
	root := writeInferClaimsFixture(t, false)
	code, stdout, stderr := captureStdoutStderr(t, func() int {
		return runInferClaims([]string{"--repo", root})
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "epistemic_status: unknown") || strings.Contains(stdout, "epistemic_status: supported") {
		t.Fatalf("expected unknown claims:\n%s", stdout)
	}
}

func TestInferClaimsWithResolvedBindingEmitsSupportedClaims(t *testing.T) {
	root := writeInferClaimsFixture(t, true)
	code, stdout, stderr := captureStdoutStderr(t, func() int {
		return runInferClaims([]string{"--repo", root, "--graph-digest-status", "resolved", "--graph-digest", strings.Repeat("a", 64)})
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "epistemic_status: supported") {
		t.Fatalf("expected supported claim:\n%s", stdout)
	}
}

func TestInferClaimsUsesExplicitRepositoryDomainForBindingAndReceipts(t *testing.T) {
	root := writeInferClaimsFixture(t, true)
	out := filepath.Join(root, "claims.yaml")
	domain := "github.com/example/canonical"
	code := runInferClaims([]string{
		"--repo", root,
		"--repo-domain", domain,
		"--graph-digest-status", "resolved",
		"--graph-digest", strings.Repeat("b", 64),
		"--output", out,
	})
	if code != 0 {
		t.Fatalf("infer-claims code=%d", code)
	}
	doc, err := architecture.LoadClaimDocument(out)
	if err != nil {
		t.Fatalf("load claims: %v", err)
	}
	if doc.Binding.RepositoryDomain != domain {
		t.Fatalf("binding domain=%q want %q", doc.Binding.RepositoryDomain, domain)
	}
	for _, receipt := range doc.FactReceipts {
		if receipt.Provenance.RepositoryDomain != domain || receipt.Fact.Scope.Repository != domain {
			t.Fatalf("receipt domain mismatch: %+v", receipt)
		}
	}
}

func TestInferClaimsDoesNotMutateGraph(t *testing.T) {
	root := writeInferClaimsFixture(t, false)
	before := statOptional(t, filepath.Join(root, ".sensei", "graph-authority.json"))
	code, _, stderr := captureStdoutStderr(t, func() int {
		return runInferClaims([]string{"--repo", root})
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	after := statOptional(t, filepath.Join(root, ".sensei", "graph-authority.json"))
	if before != after {
		t.Fatalf("graph marker changed: %q -> %q", before, after)
	}
}

func TestInferClaimsDoesNotWriteSeed(t *testing.T) {
	root := writeInferClaimsFixture(t, false)
	seed := filepath.Join(root, "golang", "server", "embeddata", "awareness.nt")
	code, _, stderr := captureStdoutStderr(t, func() int {
		return runInferClaims([]string{"--repo", root})
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if _, err := os.Stat(seed); !os.IsNotExist(err) {
		t.Fatalf("seed was written or exists unexpectedly: %v", err)
	}
}

func TestInferClaimsUsesSingleASTPass(t *testing.T) {
	raw, err := os.ReadFile("cmd_infer_claims.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "parser.ParseFile") {
		t.Fatal("infer-claims must reuse extraction, not parse Go separately")
	}
}

func writeInferClaimsFixture(t *testing.T, git bool) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/infer\n")
	writeFile(t, filepath.Join(root, "state.go"), `package infer
func Apply(state string) error {
	if state == "bad" { return errInvalid }
	Value = state
	return nil
}
var Value string
var errInvalid error
`)
	writeFile(t, filepath.Join(root, "state_test.go"), `package infer
func TestApplyMustRejectBadState(t *testing.T) {
	if Apply("bad") == nil { t.Fatal("must reject") }
}
`)
	if git {
		runGit(t, root, "init")
		runGit(t, root, "config", "user.email", "test@example.com")
		runGit(t, root, "config", "user.name", "Test User")
		runGit(t, root, "add", ".")
		runGit(t, root, "commit", "-m", "initial")
	}
	return root
}

func statOptional(t *testing.T, path string) string {
	t.Helper()
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "missing"
	}
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime().String()
}
