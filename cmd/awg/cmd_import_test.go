// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/adoption"
	"github.com/globulario/sensei/golang/rdf"
	"gopkg.in/yaml.v3"
)

func TestDeriveDomain(t *testing.T) {
	cases := map[string]string{
		"https://github.com/gin-gonic/gin":     "github.com/gin-gonic/gin",
		"https://github.com/gin-gonic/gin.git": "github.com/gin-gonic/gin",
		"http://gitlab.com/a/b/c":              "gitlab.com/a/b/c",
		"git@github.com:gin-gonic/gin.git":     "github.com/gin-gonic/gin",
		"github.com/owner/repo":                "github.com/owner/repo",
		"not-a-url":                            "", // no slash → cannot derive
	}
	for in, want := range cases {
		if got := deriveDomain(in); got != want {
			t.Errorf("deriveDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeriveSlug(t *testing.T) {
	cases := map[string]string{
		"github.com/gin-gonic/gin": "gin-gonic/gin",
		"gitlab.com/a/b/c":         "b/c",
		"owner/repo":               "owner/repo",
		"single":                   "",
	}
	for in, want := range cases {
		if got := deriveSlug(in); got != want {
			t.Errorf("deriveSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeriveRepoBaseAndSanitize(t *testing.T) {
	if got := deriveRepoBase("https://github.com/gin-gonic/gin.git"); got != "gin" {
		t.Errorf("deriveRepoBase = %q, want gin", got)
	}
	if got := sanitizeName("gin-gonic/gin@x"); got != "gin-gonicginx" {
		t.Errorf("sanitizeName = %q", got)
	}
	if got := sanitizeName("////"); got != "repo" {
		t.Errorf("sanitizeName empty fallback = %q, want repo", got)
	}
}

func TestResolveImportRefreshCheckoutRequiresExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	got, code := resolveImportRefreshCheckout(dir)
	if code != 0 {
		t.Fatalf("resolveImportRefreshCheckout code=%d, want 0", code)
	}
	if got != dir {
		t.Fatalf("checkout=%q, want %q", got, dir)
	}

	_, code = resolveImportRefreshCheckout(filepath.Join(dir, "missing"))
	if code == 0 {
		t.Fatal("missing refresh checkout should fail")
	}
}

func TestResolveImportDomainUsesExplicitTargetThenRemote(t *testing.T) {
	old := gitRemoteDomain
	defer func() { gitRemoteDomain = old }()
	checkout := filepath.Join(t.TempDir(), "owner", "repo")
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRemoteDomain = func(path string) string {
		if path == checkout {
			return "github.com/from/remote"
		}
		return ""
	}

	if got := resolveImportDomain("github.com/explicit/repo", "not-a-url", checkout); got != "github.com/explicit/repo" {
		t.Fatalf("explicit domain=%q", got)
	}
	if got := resolveImportDomain("", "https://github.com/from/url.git", checkout); got != "github.com/from/url" {
		t.Fatalf("target-derived domain=%q", got)
	}
	if got := resolveImportDomain("", checkout, checkout); got != "github.com/from/remote" {
		t.Fatalf("remote-derived domain=%q", got)
	}

	gitRemoteDomain = func(path string) string { return "" }
	if got := resolveImportDomain("", checkout, checkout); got != "" {
		t.Fatalf("local path without remote should not become domain, got %q", got)
	}
}

func TestImportDryRunAutoReportsClaudeCLIBackend(t *testing.T) {
	repo := t.TempDir()
	fakeDir := t.TempDir()
	fakeClaude := filepath.Join(fakeDir, "claude")
	if err := os.WriteFile(fakeClaude, []byte("#!/bin/sh\nprintf '%s' '{\"is_error\":false,\"result\":\"{}\"}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-poison")

	code, _, stderr := captureStdoutStderr(t, func() int {
		return runImport([]string{"--refresh", repo, "--domain", "github.com/example/repo", "--depth", "full", "--drafter", "auto", "--dry-run"})
	})
	if code != 0 {
		t.Fatalf("runImport dry-run code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "--drafter claude-cli") {
		t.Fatalf("dry-run did not report selected claude-cli backend:\n%s", stderr)
	}
	if !strings.Contains(stderr, "--adopt") || strings.Contains(stderr, "--apply") {
		t.Fatalf("dry-run should use machine adoption policy, not legacy apply:\n%s", stderr)
	}
	if !strings.Contains(stderr, "authentication: Claude CLI subscription login") {
		t.Fatalf("dry-run did not report CLI authentication:\n%s", stderr)
	}
	if !strings.Contains(stderr, "--input "+repo+"/.sensei/project") {
		t.Fatalf("dry-run load handoff omitted project claims and adopted knowledge:\n%s", stderr)
	}
}

func TestImportDryRunReportsCodexCLIBackend(t *testing.T) {
	repo := t.TempDir()
	fakeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeDir, "codex"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-poison")

	code, _, stderr := captureStdoutStderr(t, func() int {
		return runImport([]string{"--refresh", repo, "--domain", "github.com/example/repo", "--depth", "full", "--drafter", "codex-cli", "--dry-run"})
	})
	if code != 0 {
		t.Fatalf("runImport dry-run code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "--drafter codex-cli") {
		t.Fatalf("dry-run did not report selected codex-cli backend:\n%s", stderr)
	}
	if !strings.Contains(stderr, "--adopt") || strings.Contains(stderr, "--apply") {
		t.Fatalf("dry-run should use machine adoption policy, not legacy apply:\n%s", stderr)
	}
	if !strings.Contains(stderr, "authentication: Codex CLI subscription login") {
		t.Fatalf("dry-run did not report Codex CLI authentication:\n%s", stderr)
	}
}

func TestImportExplicitLLMNoFallbackWithoutCredential(t *testing.T) {
	repo := t.TempDir()
	fakeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeDir, "claude"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeDir, "codex"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	code, _, stderr := captureStdoutStderr(t, func() int {
		return runImport([]string{"--refresh", repo, "--domain", "github.com/example/repo", "--depth", "full", "--drafter", "llm", "--dry-run"})
	})
	if code != 2 {
		t.Fatalf("explicit llm without credential code=%d stderr=%s", code, stderr)
	}
	if strings.Contains(stderr, "--drafter claude-cli") {
		t.Fatalf("explicit llm silently fell back to claude-cli:\n%s", stderr)
	}
	if strings.Contains(stderr, "--drafter codex-cli") {
		t.Fatalf("explicit llm silently fell back to codex-cli:\n%s", stderr)
	}
}

func TestAssessPhase2ReadinessReportsMissingRootPackageAsStructurallyThin(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "gin.go"), "package gin\n")
	writeFile(t, filepath.Join(root, "tree.go"), "package gin\n")
	writeFile(t, filepath.Join(root, "render", "render.go"), "package render\n")
	graph := readinessGraph("gin.go", "render/render.go")
	report, err := assessPhase2Readiness(root, "github.com/gin-gonic/gin", filepath.Join(root, "graph.nt"), filepath.Join(root, "claims.yaml"), graph, architecture.ClaimDocument{Claims: []architecture.Claim{{ID: "claim.one"}}})
	if err != nil {
		t.Fatalf("assess readiness: %v", err)
	}
	if report.State != readinessStructurallyThin {
		t.Fatalf("state=%q want %q", report.State, readinessStructurallyThin)
	}
	if report.StructuralSourceCoverage != "2/3" || report.RootPackageCoverage != "1/2" {
		t.Fatalf("coverage=%s root=%s", report.StructuralSourceCoverage, report.RootPackageCoverage)
	}
	if len(report.UnrepresentedCoreFiles) != 1 || report.UnrepresentedCoreFiles[0] != "tree.go" {
		t.Fatalf("unrepresented core=%v", report.UnrepresentedCoreFiles)
	}
}

func TestProjectBaseGraphExcludesMachineAdoptedIntentSubjects(t *testing.T) {
	intent := strings.Trim(rdf.MintIRI(rdf.ClassIntent, "intent.route"), "<>")
	file := strings.Trim(rdf.MintIRI(rdf.ClassSourceFile, "gin.go"), "<>")
	raw := []byte(strings.Join([]string{
		"<" + intent + "> <" + rdf.PropType + "> <" + rdf.ClassIntent + "> .",
		"<" + intent + "> <" + rdf.PropStatus + "> \"machine_adopted\" .",
		"<" + intent + "> <" + rdf.PropLabel + "> \"route\" .",
		"<" + file + "> <" + rdf.PropType + "> <" + rdf.ClassSourceFile + "> .",
	}, "\n") + "\n")
	base, err := stripMachineAdoptedIntentSubjects(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(base), intent) || !strings.Contains(string(base), file) {
		t.Fatalf("provisional graph did not isolate machine Intent:\n%s", base)
	}
}

func TestImportFinalizesMachineAdoptedIntentGraphReceipt(t *testing.T) {
	awareness := t.TempDir()
	path := filepath.Join(awareness, "intent_route.yaml")
	writeFile(t, path, `status: machine_adopted
promotion_status: machine_adopted
assertion_origin: model_inferred
epistemic_status: supported
architectural_plane: intended
decision_actor: sensei.intent_mine
decision_context: delegated_machine_adoption
decision_policy: adoption.intent.strong_grounding.v1
decision_timestamp: "2025-01-01T00:00:00Z"
valid_for_revision: old
review_status: not_human_reviewed
id: intent.route
level: contract
title: Route
intent: Route behavior remains stable.
`)
	if err := finalizeMachineAdoptedIntentReceipts(awareness, "rev", strings.Repeat("a", 64), "2026-01-02T03:04:05Z"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var receipt adoption.Receipt
	if err := yaml.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	if err := adoption.ValidateMachineAdoption(receipt); err != nil {
		t.Fatalf("final receipt invalid: %v\n%s", err, raw)
	}
	if receipt.ValidForRevision != "rev" || receipt.ValidForGraphDigest != strings.Repeat("a", 64) || receipt.DecisionTimestamp != "2026-01-02T03:04:05Z" {
		t.Fatalf("final receipt=%+v", receipt)
	}
}

func TestAssessPhase2ReadinessReportsInferenceMissingAfterCompleteStructure(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "gin.go"), "package gin\n")
	graph := readinessGraph("gin.go")
	report, err := assessPhase2Readiness(root, "github.com/gin-gonic/gin", filepath.Join(root, "graph.nt"), filepath.Join(root, "claims.yaml"), graph, architecture.ClaimDocument{})
	if err != nil {
		t.Fatalf("assess readiness: %v", err)
	}
	if report.State != readinessInferenceMissing {
		t.Fatalf("state=%q want %q", report.State, readinessInferenceMissing)
	}
}

func TestReconstructImportedProjectWritesBoundClaimsAndReadiness(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/gin\n")
	writeFile(t, filepath.Join(root, "state.go"), `package gin
func Apply(state string) error {
	if state == "bad" { return errInvalid }
	Value = state
	return nil
}
var Value string
var errInvalid error
`)
	writeFile(t, filepath.Join(root, "state_test.go"), `package gin
func TestApplyMustRejectBadState(t *testing.T) {
	if Apply("bad") == nil { t.Fatal("must reject") }
}
`)
	writeFile(t, filepath.Join(root, "docs", "awareness", "generated", "components.yaml"), `components:
  - id: component.gin
    name: gin
    kind: module
    assertion: inferred
    source_files:
      - state.go
`)
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")

	domain := "github.com/example/gin"
	report, err := reconstructImportedProject(root, domain, false)
	if err != nil {
		t.Fatalf("reconstruct project: %v", err)
	}
	if report.ClaimCount == 0 || report.State != readinessPartiallyReady {
		t.Fatalf("readiness=%+v", report)
	}
	doc, err := architecture.LoadClaimDocument(filepath.Join(root, ".sensei", "project", "claims.yaml"))
	if err != nil {
		t.Fatalf("load project claims: %v", err)
	}
	if doc.Binding.RepositoryDomain != domain || doc.Binding.GraphDigestSHA256 != report.GraphDigestSHA256 {
		t.Fatalf("claim binding=%+v readiness digest=%s", doc.Binding, report.GraphDigestSHA256)
	}
	if _, err := os.Stat(filepath.Join(root, ".sensei", "project", "readiness.yaml")); err != nil {
		t.Fatalf("readiness artifact: %v", err)
	}
	receiptData, err := os.ReadFile(filepath.Join(root, ".sensei", "project", "reconstruction-receipt.yaml"))
	if err != nil {
		t.Fatalf("reconstruction receipt: %v", err)
	}
	var receipt projectReconstructionReceipt
	if err := yaml.Unmarshal(receiptData, &receipt); err != nil {
		t.Fatalf("parse reconstruction receipt: %v", err)
	}
	if receipt.FinalGraphDigestSHA256 != report.GraphDigestSHA256 || !receipt.ClaimsBoundToFinalGraph || !receipt.DeterministicSecondPass || receipt.ExternalProofCreatedByImport {
		t.Fatalf("reconstruction receipt=%+v", receipt)
	}
	loadGraph, _, err := compileAwarenessInputs([]string{
		filepath.Join(root, "docs", "awareness"),
		filepath.Join(root, "docs", "awareness", "generated"),
		filepath.Join(root, ".sensei", "project"),
	}, domain, "", "", false)
	if err != nil {
		t.Fatalf("compile store handoff: %v", err)
	}
	if !bytes.Contains(loadGraph, []byte(rdf.ClassArchitectureClaim)) {
		t.Fatalf("store handoff omitted derived ArchitectureClaims")
	}
}

func TestImportRefreshBuildsTaskReadyProjectKnowledge(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/gin\n")
	writeFile(t, filepath.Join(root, "state.go"), `package gin
type Engine struct{ state string }
func (e *Engine) Apply(state string) error {
	if state == "bad" { return errInvalid }
	e.state = state
	return nil
}
var errInvalid error
`)
	writeFile(t, filepath.Join(root, "state_test.go"), `package gin
func TestApplyMustRejectBadState(t *testing.T) {
	if new(Engine).Apply("bad") == nil { t.Fatal("must reject") }
}
`)
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")

	code, _, stderr := captureStdoutStderr(t, func() int {
		return runImport([]string{"--refresh", root, "--domain", "github.com/example/gin", "--depth", "basic"})
	})
	if code != 0 {
		t.Fatalf("import code=%d stderr=%s", code, stderr)
	}
	for _, rel := range []string{"graph.nt", "claims.yaml", "readiness.yaml", "reconstruction-receipt.yaml"} {
		if _, err := os.Stat(filepath.Join(root, ".sensei", "project", rel)); err != nil {
			t.Fatalf("missing project %s: %v", rel, err)
		}
	}
	readinessData, err := os.ReadFile(filepath.Join(root, ".sensei", "project", "readiness.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readinessData), "state: ready") || !strings.Contains(stderr, "phase 2 readiness: ready") {
		t.Fatalf("import did not report ready:\n%s\n%s", readinessData, stderr)
	}
	symbols, err := os.ReadFile(filepath.Join(root, "docs", "awareness", "generated", "source_symbols.yaml"))
	if err != nil {
		t.Fatalf("structural symbols missing: %v", err)
	}
	if !strings.Contains(string(symbols), "gin.Engine.Apply") {
		t.Fatalf("root method missing from structural symbols:\n%s", symbols)
	}
}

func readinessGraph(files ...string) []byte {
	var lines []string
	for _, file := range files {
		lines = append(lines, fmt.Sprintf("%s %s %s .", rdf.MintIRI(rdf.ClassSourceFile, file), rdf.IRI(rdf.PropType), rdf.IRI(rdf.ClassSourceFile)))
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}
