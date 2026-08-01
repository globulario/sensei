// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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

func setupCoherentProjectFixture(t *testing.T) (string, string) {
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
	_, err := reconstructImportedProject(root, domain, false)
	if err != nil {
		t.Fatalf("setup coherent fixture failed: %v", err)
	}

	activeProjectDir := filepath.Join(root, ".sensei", "project")
	class, err := classifyActiveProjectFamily(activeProjectDir)
	if err != nil {
		t.Fatalf("classify coherent failed: %v", err)
	}
	if class.Status != projectFamilyCoherent {
		t.Fatalf("setup coherent fixture got status %q (detail: %s), want coherent", class.Status, class.Detail)
	}

	return root, domain
}

func cloneProjectFamily(t *testing.T, src string) string {
	dst := t.TempDir()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(dstPath, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dstPath, data, info.Mode())
	})
	if err != nil {
		t.Fatalf("clone project family: %v", err)
	}
	return dst
}

func copyDir(t *testing.T, src, dst string) {
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(dstPath, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dstPath, data, info.Mode())
	})
	if err != nil {
		t.Fatalf("copyDir: %v", err)
	}
}

func mutateReceipt(t *testing.T, dir string, fn func(*projectReconstructionReceipt)) {
	path := filepath.Join(dir, "reconstruction-receipt.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var r projectReconstructionReceipt
	if err := yaml.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	fn(&r)
	out, err := yaml.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

type claimDocumentEnvelope struct {
	ArchitectureClaims architecture.ClaimDocument `yaml:"architecture_claims"`
}

func mutateClaims(t *testing.T, dir string, fn func(*architecture.ClaimDocument)) {
	path := filepath.Join(dir, "claims.yaml")
	doc, err := architecture.LoadClaimDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	fn(&doc)
	env := claimDocumentEnvelope{ArchitectureClaims: doc}
	out, err := yaml.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mutateReadiness(t *testing.T, dir string, fn func(*phase2Readiness)) {
	path := filepath.Join(dir, "readiness.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var env phase2ReadinessEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	fn(&env.Phase2Readiness)
	out, err := yaml.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func updateReceiptDigest(t *testing.T, dir, relPath string, body []byte) {
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	mutateReceipt(t, dir, func(r *projectReconstructionReceipt) {
		for i, art := range r.Artifacts {
			if art.Path == relPath {
				r.Artifacts[i].SHA256Digest = digest
				return
			}
		}
	})
}

func TestProjectFamilyClassifier(t *testing.T) {
	fixtureRoot, _ := setupCoherentProjectFixture(t)
	srcDir := filepath.Join(fixtureRoot, ".sensei", "project")

	cases := []struct {
		name           string
		mutate         func(t *testing.T, dir string)
		expectedReason projectFamilyInvalidReason
	}{
		{
			name: "malformed graph",
			mutate: func(t *testing.T, dir string) {
				body := []byte("invalid ntriples line")
				os.WriteFile(filepath.Join(dir, "graph.nt"), body, 0o644)
				updateReceiptDigest(t, dir, ".sensei/project/graph.nt", body)
			},
			expectedReason: invalidGraph,
		},
		{
			name: "graph digest mismatch",
			mutate: func(t *testing.T, dir string) {
				mutateReceipt(t, dir, func(r *projectReconstructionReceipt) {
					r.FinalGraphDigestSHA256 = "mismatchedhash"
				})
			},
			expectedReason: invalidGraph,
		},
		{
			name: "missing graph",
			mutate: func(t *testing.T, dir string) {
				os.Remove(filepath.Join(dir, "graph.nt"))
			},
			expectedReason: invalidDigest,
		},
		{
			name: "missing claims",
			mutate: func(t *testing.T, dir string) {
				os.Remove(filepath.Join(dir, "claims.yaml"))
			},
			expectedReason: invalidDigest,
		},
		{
			name: "missing audit",
			mutate: func(t *testing.T, dir string) {
				os.Remove(filepath.Join(dir, "claim-audit.yaml"))
			},
			expectedReason: invalidDigest,
		},
		{
			name: "missing readiness",
			mutate: func(t *testing.T, dir string) {
				os.Remove(filepath.Join(dir, "readiness.yaml"))
			},
			expectedReason: invalidDigest,
		},
		{
			name: "missing adoption artifact",
			mutate: func(t *testing.T, dir string) {
				os.Remove(filepath.Join(dir, "knowledge", "adoption-report.yaml"))
			},
			expectedReason: invalidDigest,
		},
		{
			name: "duplicate receipt path",
			mutate: func(t *testing.T, dir string) {
				mutateReceipt(t, dir, func(r *projectReconstructionReceipt) {
					if len(r.Artifacts) > 0 {
						r.Artifacts = append(r.Artifacts, r.Artifacts[0])
					}
				})
			},
			expectedReason: invalidPath,
		},
		{
			name: "non-canonical receipt path",
			mutate: func(t *testing.T, dir string) {
				mutateReceipt(t, dir, func(r *projectReconstructionReceipt) {
					if len(r.Artifacts) > 0 {
						r.Artifacts[0].Path = "graph.nt"
					}
				})
			},
			expectedReason: invalidPath,
		},
		{
			name: "path traversal",
			mutate: func(t *testing.T, dir string) {
				mutateReceipt(t, dir, func(r *projectReconstructionReceipt) {
					if len(r.Artifacts) > 0 {
						r.Artifacts[0].Path = ".sensei/project/../../graph.nt"
					}
				})
			},
			expectedReason: invalidPath,
		},
		{
			name: "artifact digest mismatch",
			mutate: func(t *testing.T, dir string) {
				mutateReceipt(t, dir, func(r *projectReconstructionReceipt) {
					if len(r.Artifacts) > 0 {
						r.Artifacts[0].SHA256Digest = "mismatchedsha"
					}
				})
			},
			expectedReason: invalidDigest,
		},
		{
			name: "claims domain mismatch",
			mutate: func(t *testing.T, dir string) {
				mutateClaims(t, dir, func(c *architecture.ClaimDocument) {
					c.Binding.RepositoryDomain = "mismatched.com"
				})
				body, _ := os.ReadFile(filepath.Join(dir, "claims.yaml"))
				updateReceiptDigest(t, dir, ".sensei/project/claims.yaml", body)
			},
			expectedReason: invalidClaims,
		},
		{
			name: "claims revision mismatch",
			mutate: func(t *testing.T, dir string) {
				mutateClaims(t, dir, func(c *architecture.ClaimDocument) {
					c.Binding.Revision = "mismatchedrevision"
				})
				body, _ := os.ReadFile(filepath.Join(dir, "claims.yaml"))
				updateReceiptDigest(t, dir, ".sensei/project/claims.yaml", body)
			},
			expectedReason: invalidClaims,
		},
		{
			name: "unresolved claims revision",
			mutate: func(t *testing.T, dir string) {
				mutateClaims(t, dir, func(c *architecture.ClaimDocument) {
					c.Binding.RevisionStatus = "unresolved"
				})
				body, _ := os.ReadFile(filepath.Join(dir, "claims.yaml"))
				updateReceiptDigest(t, dir, ".sensei/project/claims.yaml", body)
			},
			expectedReason: invalidClaims,
		},
		{
			name: "claims graph mismatch",
			mutate: func(t *testing.T, dir string) {
				mutateClaims(t, dir, func(c *architecture.ClaimDocument) {
					c.Binding.GraphDigestSHA256 = "mismatchedgraphdigest"
				})
				body, _ := os.ReadFile(filepath.Join(dir, "claims.yaml"))
				updateReceiptDigest(t, dir, ".sensei/project/claims.yaml", body)
			},
			expectedReason: invalidClaims,
		},
		{
			name: "unresolved claims graph binding",
			mutate: func(t *testing.T, dir string) {
				mutateClaims(t, dir, func(c *architecture.ClaimDocument) {
					c.Binding.GraphDigestStatus = "unresolved"
				})
				body, _ := os.ReadFile(filepath.Join(dir, "claims.yaml"))
				updateReceiptDigest(t, dir, ".sensei/project/claims.yaml", body)
			},
			expectedReason: invalidClaims,
		},
		{
			name: "readiness domain mismatch",
			mutate: func(t *testing.T, dir string) {
				mutateReadiness(t, dir, func(r *phase2Readiness) {
					r.RepositoryDomain = "mismatched.com"
				})
				body, _ := os.ReadFile(filepath.Join(dir, "readiness.yaml"))
				updateReceiptDigest(t, dir, ".sensei/project/readiness.yaml", body)
			},
			expectedReason: invalidReadiness,
		},
		{
			name: "readiness graph mismatch",
			mutate: func(t *testing.T, dir string) {
				mutateReadiness(t, dir, func(r *phase2Readiness) {
					r.GraphDigestSHA256 = "mismatchedgraphdigest"
				})
				body, _ := os.ReadFile(filepath.Join(dir, "readiness.yaml"))
				updateReceiptDigest(t, dir, ".sensei/project/readiness.yaml", body)
			},
			expectedReason: invalidReadiness,
		},
		{
			name: "readiness state mismatch",
			mutate: func(t *testing.T, dir string) {
				mutateReadiness(t, dir, func(r *phase2Readiness) {
					r.State = "mismatchedstate"
				})
				body, _ := os.ReadFile(filepath.Join(dir, "readiness.yaml"))
				updateReceiptDigest(t, dir, ".sensei/project/readiness.yaml", body)
			},
			expectedReason: invalidReadiness,
		},
		{
			name: "readiness graph path mismatch",
			mutate: func(t *testing.T, dir string) {
				mutateReadiness(t, dir, func(r *phase2Readiness) {
					r.GraphPath = "mismatchedpath"
				})
				body, _ := os.ReadFile(filepath.Join(dir, "readiness.yaml"))
				updateReceiptDigest(t, dir, ".sensei/project/readiness.yaml", body)
			},
			expectedReason: invalidReadiness,
		},
		{
			name: "readiness claims path mismatch",
			mutate: func(t *testing.T, dir string) {
				mutateReadiness(t, dir, func(r *phase2Readiness) {
					r.ClaimsPath = "mismatchedpath"
				})
				body, _ := os.ReadFile(filepath.Join(dir, "readiness.yaml"))
				updateReceiptDigest(t, dir, ".sensei/project/readiness.yaml", body)
			},
			expectedReason: invalidReadiness,
		},
		{
			name: "readiness audit path mismatch",
			mutate: func(t *testing.T, dir string) {
				mutateReadiness(t, dir, func(r *phase2Readiness) {
					r.ClaimAuditPath = "mismatchedpath"
				})
				body, _ := os.ReadFile(filepath.Join(dir, "readiness.yaml"))
				updateReceiptDigest(t, dir, ".sensei/project/readiness.yaml", body)
			},
			expectedReason: invalidReadiness,
		},
		{
			name: "readiness adoption path mismatch",
			mutate: func(t *testing.T, dir string) {
				mutateReadiness(t, dir, func(r *phase2Readiness) {
					r.AdoptionReportPath = "mismatchedpath"
				})
				body, _ := os.ReadFile(filepath.Join(dir, "readiness.yaml"))
				updateReceiptDigest(t, dir, ".sensei/project/readiness.yaml", body)
			},
			expectedReason: invalidReadiness,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cloned := cloneProjectFamily(t, srcDir)
			tc.mutate(t, cloned)
			class, err := classifyActiveProjectFamily(cloned)
			if err != nil {
				if class.Status != projectFamilyInvalid {
					t.Fatalf("expected invalid status, got %q, err: %v", class.Status, err)
				}
				return
			}
			if class.Status != projectFamilyInvalid {
				t.Fatalf("expected status invalid, got %q", class.Status)
			}
			if class.Reason != tc.expectedReason {
				t.Fatalf("expected reason %q, got %q (detail: %s)", tc.expectedReason, class.Reason, class.Detail)
			}
		})
	}
}

func TestProjectPublication(t *testing.T) {
	root, domain := setupCoherentProjectFixture(t)
	projectParent := filepath.Join(root, ".sensei")
	activeDir := filepath.Join(projectParent, "project")
	txMarkerPath := filepath.Join(projectParent, "tx-marker.yaml")

	class, err := classifyActiveProjectFamily(activeDir)
	if err != nil || class.Status != projectFamilyCoherent {
		t.Fatalf("initial state not coherent: %+v, err: %v", class, err)
	}

	// Create a backup of the coherent project parent directory.
	backupParent := t.TempDir()
	copyDir(t, projectParent, backupParent)

	failurePoints := []string{
		failAfterReceipt,
		failAfterMoveCoherent,
		failDuringActivation,
		failAfterActivation,
	}

	for _, fp := range failurePoints {
		t.Run("fail_"+fp, func(t *testing.T) {
			os.RemoveAll(projectParent)
			copyDir(t, backupParent, projectParent)

			if _, err := os.Stat(txMarkerPath); err == nil {
				t.Fatal("marker should not exist initially")
			}
			class, err = classifyActiveProjectFamily(activeDir)
			if err != nil || class.Status != projectFamilyCoherent {
				t.Fatalf("recreated active state is not coherent: %+v", class)
			}

			testFailureInjectionPoint = fp
			defer func() { testFailureInjectionPoint = "" }()

			_, err = reconstructImportedProject(root, domain, false)
			if err == nil {
				t.Fatalf("expected failure at point %q, got nil", fp)
			}

			testFailureInjectionPoint = ""

			if _, statErr := os.Stat(txMarkerPath); os.IsNotExist(statErr) {
				t.Fatalf("expected transaction marker at %s", txMarkerPath)
			}

			if err := recoverTransaction(projectParent, txMarkerPath); err != nil {
				t.Fatalf("recovery failed: %v", err)
			}

			if _, statErr := os.Stat(txMarkerPath); !os.IsNotExist(statErr) {
				t.Fatal("transaction marker was not deleted by recovery")
			}

			finalClass, err := classifyActiveProjectFamily(activeDir)
			if err != nil || finalClass.Status != projectFamilyCoherent {
				t.Fatalf("active project not restored to coherent: %+v, err: %v", finalClass, err)
			}

			files, err := os.ReadDir(projectParent)
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range files {
				if strings.HasPrefix(file.Name(), ".project-staging-") {
					t.Fatalf("stale staging directory %s not cleaned up", file.Name())
				}
			}
		})
	}
}

func TestProjectPublicationWithInvalidAndAbsentPrior(t *testing.T) {
	t.Run("absent_prior", func(t *testing.T) {
		root := t.TempDir()
		projectParent := filepath.Join(root, ".sensei")
		activeDir := filepath.Join(projectParent, "project")
		txMarkerPath := filepath.Join(projectParent, "tx-marker.yaml")

		testFailureInjectionPoint = failAfterReceipt
		defer func() { testFailureInjectionPoint = "" }()

		writeFile(t, filepath.Join(root, "go.mod"), "module example.com/gin\n")
		writeFile(t, filepath.Join(root, "docs", "awareness", "generated", "components.yaml"), "components: []\n")
		runGit(t, root, "init")
		runGit(t, root, "config", "user.email", "test@example.com")
		runGit(t, root, "config", "user.name", "Test User")
		runGit(t, root, "add", ".")
		runGit(t, root, "commit", "-m", "initial")

		_, err := reconstructImportedProject(root, "github.com/example/gin", false)
		if err == nil {
			t.Fatal("expected failure after receipt")
		}

		if _, err := os.Stat(txMarkerPath); err != nil {
			t.Fatal("marker should exist")
		}

		testFailureInjectionPoint = ""
		if err := recoverTransaction(projectParent, txMarkerPath); err != nil {
			t.Fatalf("recovery failed: %v", err)
		}

		if _, err := os.Stat(txMarkerPath); !os.IsNotExist(err) {
			t.Fatal("marker should be deleted")
		}

		class, err := classifyActiveProjectFamily(activeDir)
		if err != nil {
			t.Fatal(err)
		}
		if class.Status != projectFamilyAbsent {
			t.Fatalf("expected absent status, got %q", class.Status)
		}
	})

	t.Run("invalid_prior", func(t *testing.T) {
		root, domain := setupCoherentProjectFixture(t)
		projectParent := filepath.Join(root, ".sensei")
		activeDir := filepath.Join(projectParent, "project")
		txMarkerPath := filepath.Join(projectParent, "tx-marker.yaml")

		os.WriteFile(filepath.Join(activeDir, "graph.nt"), []byte("invalid graph"), 0o644)

		class, err := classifyActiveProjectFamily(activeDir)
		if err != nil || class.Status != projectFamilyInvalid {
			t.Fatalf("expected invalid status, got %+v", class)
		}

		testFailureInjectionPoint = failAfterMoveInvalid
		defer func() { testFailureInjectionPoint = "" }()

		_, err = reconstructImportedProject(root, domain, false)
		if err == nil {
			t.Fatal("expected failure after move invalid prior")
		}

		testFailureInjectionPoint = ""
		if err := recoverTransaction(projectParent, txMarkerPath); err != nil {
			t.Fatalf("recovery failed: %v", err)
		}

		if _, err := os.Stat(txMarkerPath); !os.IsNotExist(err) {
			t.Fatal("marker should be deleted")
		}

		finalClass, err := classifyActiveProjectFamily(activeDir)
		if err != nil {
			t.Fatal(err)
		}
		if finalClass.Status != projectFamilyAbsent {
			t.Fatalf("expected absent status for recovered invalid prior, got %q", finalClass.Status)
		}

		files, err := os.ReadDir(projectParent)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if strings.HasPrefix(file.Name(), ".project-staging-") {
				t.Fatalf("stale staging directory %s not cleaned up", file.Name())
			}
		}
	})
}

func TestProjectPublicationRevisionChange(t *testing.T) {
	root, domain := setupCoherentProjectFixture(t)
	projectParent := filepath.Join(root, ".sensei")
	activeDir := filepath.Join(projectParent, "project")

	testFailureHook = func(point string) {
		if point == failAfterReceipt {
			writeFile(t, filepath.Join(root, "extra.go"), "package gin\n")
			runGit(t, root, "add", "extra.go")
			runGit(t, root, "commit", "-m", "revision change")
		}
	}
	defer func() { testFailureHook = nil }()

	_, err := reconstructImportedProject(root, domain, false)
	if err == nil {
		t.Fatal("expected reconstruction to fail because revision changed")
	}
	if !strings.Contains(err.Error(), "repository revision changed during staging") {
		t.Fatalf("expected revision change error, got: %v", err)
	}

	class, err := classifyActiveProjectFamily(activeDir)
	if err != nil || class.Status != projectFamilyCoherent {
		t.Fatalf("expected prior coherent project to remain active, got: %+v", class)
	}
}

func TestProjectPublicationConcurrency(t *testing.T) {
	root, domain := setupCoherentProjectFixture(t)
	projectParent := filepath.Join(root, ".sensei")
	lockPath := filepath.Join(projectParent, "project.lock")

	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lockFile.Close()

	acquireTestLock(t, lockFile.Fd())
	defer releaseTestLock(t, lockFile.Fd())

	_, err = reconstructImportedProject(root, domain, false)
	if err == nil {
		t.Fatal("expected concurrent reconstruction attempt to fail, but it succeeded")
	}

	if !strings.Contains(err.Error(), "concurrent reconstruction in progress") {
		t.Fatalf("expected concurrent reconstruction in progress error, got: %v", err)
	}
}
