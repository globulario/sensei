// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaintainClaimsRequiresClaimsInput(t *testing.T) {
	code, _, _ := captureStdoutStderr(t, func() int {
		return runMaintainClaims(nil)
	})
	if code != 2 {
		t.Fatalf("code=%d", code)
	}
}

func TestMaintainClaimsDefaultWritesStdoutOnly(t *testing.T) {
	root, claims := inferredClaimsFixture(t, false)
	code, stdout, stderr := captureStdoutStderr(t, func() int {
		return runMaintainClaims([]string{"--claims", claims, "--repo", root})
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "architecture_claims:") || !strings.Contains(stdout, "epistemic_status: unknown") {
		t.Fatalf("bad stdout:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "awareness", "candidates")); !os.IsNotExist(err) {
		t.Fatalf("default run wrote candidates dir: %v", err)
	}
}

func TestMaintainClaimsCheckFresh(t *testing.T) {
	root, claims := inferredClaimsFixture(t, false)
	out := filepath.Join(root, "maintained.yaml")
	if code := runMaintainClaims([]string{"--claims", claims, "--repo", root, "--output", out}); code != 0 {
		t.Fatalf("write code=%d", code)
	}
	code, _, stderr := captureStdoutStderr(t, func() int {
		return runMaintainClaims([]string{"--claims", claims, "--repo", root, "--output", out, "--check"})
	})
	if code != 0 || !strings.Contains(stderr, "fresh") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
}

func TestMaintainClaimsCheckStale(t *testing.T) {
	root, claims := inferredClaimsFixture(t, false)
	out := filepath.Join(root, "maintained.yaml")
	writeFile(t, out, "stale\n")
	code, _, stderr := captureStdoutStderr(t, func() int {
		return runMaintainClaims([]string{"--claims", claims, "--repo", root, "--output", out, "--check"})
	})
	if code != 1 || !strings.Contains(stderr, "STALE") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
}

func TestMaintainClaimsRejectsActiveAwarenessOutputPath(t *testing.T) {
	root, claims := inferredClaimsFixture(t, false)
	out := filepath.Join(root, "docs", "awareness", "architecture_claims.yaml")
	code, _, _ := captureStdoutStderr(t, func() int {
		return runMaintainClaims([]string{"--claims", claims, "--repo", root, "--output", out})
	})
	if code != 2 {
		t.Fatalf("code=%d", code)
	}
}

func TestMaintainClaimsAllowsCandidateDirectoryOutput(t *testing.T) {
	root, claims := inferredClaimsFixture(t, false)
	out := filepath.Join(root, "docs", "awareness", "candidates", "maintained_claims.yaml")
	code, _, stderr := captureStdoutStderr(t, func() int {
		return runMaintainClaims([]string{"--claims", claims, "--repo", root, "--output", out})
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("missing output: %v", err)
	}
}

func TestMaintainClaimsWithoutGraphDigestProducesUnknown(t *testing.T) {
	root, claims := inferredClaimsFixture(t, true)
	code, stdout, stderr := captureStdoutStderr(t, func() int {
		return runMaintainClaims([]string{"--claims", claims, "--repo", root})
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "epistemic_status: unknown") {
		t.Fatalf("expected unknown:\n%s", stdout)
	}
}

func TestMaintainClaimsWithDigestMismatchProducesStale(t *testing.T) {
	root, claims := inferredClaimsFixture(t, true)
	code, stdout, stderr := captureStdoutStderr(t, func() int {
		return runMaintainClaims([]string{"--claims", claims, "--repo", root, "--graph-digest-status", "resolved", "--graph-digest", strings.Repeat("b", 64)})
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "epistemic_status: stale") {
		t.Fatalf("expected stale:\n%s", stdout)
	}
}

func TestMaintainClaimsWritesSeparateReport(t *testing.T) {
	root, claims := inferredClaimsFixture(t, false)
	report := filepath.Join(root, "report.yaml")
	code, _, stderr := captureStdoutStderr(t, func() int {
		return runMaintainClaims([]string{"--claims", claims, "--repo", root, "--report-output", report})
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	raw, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "claim_truth_maintenance:") {
		t.Fatalf("bad report:\n%s", raw)
	}
}

func TestMaintainClaimsDoesNotModifyInput(t *testing.T) {
	root, claims := inferredClaimsFixture(t, false)
	before, err := os.ReadFile(claims)
	if err != nil {
		t.Fatal(err)
	}
	if code := runMaintainClaims([]string{"--claims", claims, "--repo", root}); code != 0 {
		t.Fatalf("code=%d", code)
	}
	after, err := os.ReadFile(claims)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("input claims mutated")
	}
}

func TestMaintainClaimsDoesNotMutateGraph(t *testing.T) {
	root, claims := inferredClaimsFixture(t, false)
	before := statOptional(t, filepath.Join(root, ".sensei", "graph-authority.json"))
	if code := runMaintainClaims([]string{"--claims", claims, "--repo", root}); code != 0 {
		t.Fatalf("code=%d", code)
	}
	after := statOptional(t, filepath.Join(root, ".sensei", "graph-authority.json"))
	if before != after {
		t.Fatalf("graph marker changed: %s -> %s", before, after)
	}
}

func TestMaintainClaimsDoesNotWriteSeed(t *testing.T) {
	root, claims := inferredClaimsFixture(t, false)
	seed := filepath.Join(root, "golang", "server", "embeddata", "awareness.nt")
	if code := runMaintainClaims([]string{"--claims", claims, "--repo", root}); code != 0 {
		t.Fatalf("code=%d", code)
	}
	if _, err := os.Stat(seed); !os.IsNotExist(err) {
		t.Fatalf("seed exists unexpectedly: %v", err)
	}
}

func TestMaintainClaimsDoesNotQueryLiveBackend(t *testing.T) {
	root, claims := inferredClaimsFixture(t, false)
	code, _, stderr := captureStdoutStderr(t, func() int {
		return runMaintainClaims([]string{"--claims", claims, "--repo", root, "--graph-digest-status", "unavailable"})
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if strings.Contains(stderr, "awareness-graph") || strings.Contains(stderr, "Oxigraph") {
		t.Fatalf("backend appeared in stderr: %s", stderr)
	}
}

func inferredClaimsFixture(t *testing.T, git bool) (string, string) {
	t.Helper()
	root := writeInferClaimsFixture(t, git)
	out := filepath.Join(root, "claims.yaml")
	args := []string{"--repo", root, "--output", out}
	if git {
		args = append(args, "--graph-digest-status", "resolved", "--graph-digest", strings.Repeat("a", 64))
	}
	if code := runInferClaims(args); code != 0 {
		t.Fatalf("infer code=%d", code)
	}
	return root, out
}
