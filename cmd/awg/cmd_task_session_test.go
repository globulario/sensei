// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/rdf"
)

func TestPrepareChangeAndTaskStatusCLI(t *testing.T) {
	repo, graph := taskSessionTestRepo(t)
	code, stdout, stderr := captureTaskSessionCommand(t, func() int {
		return runPrepareChange([]string{
			"--repo", repo,
			"--repo-domain", "github.com/example/project",
			"--description", "Ensure literal colon routes resolve consistently.",
			"--mode", "modify",
			"--task-class", "literal_colon_route_consistency",
			"--risk-class", "architecture_sensitive",
			"--direction", "preserve",
			"--graph-nt", graph,
			"--file", "modify:gin.go",
			"--file", "modify:gin_test.go",
		})
	})
	if code != 0 {
		t.Fatalf("prepare-change exit=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "Task: task.literal-colon-route-consistency.") || !strings.Contains(stdout, "Next:") {
		t.Fatalf("unexpected prepare output:\n%s", stdout)
	}
	code, stdout, stderr = captureTaskSessionCommand(t, func() int {
		return runTaskStatus([]string{"--repo", repo, "--active", "--verify"})
	})
	if code != 0 {
		t.Fatalf("task-status exit=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "Verified: true") || !strings.Contains(stdout, "Next:") {
		t.Fatalf("unexpected task-status output:\n%s", stdout)
	}
	code, stdout, stderr = captureTaskSessionCommand(t, func() int {
		return runTaskStatus([]string{"--repo", repo, "--active", "--compact"})
	})
	if code != 0 {
		t.Fatalf("compact task-status exit=%d stderr=%s", code, stderr)
	}
	if lines := strings.Count(strings.TrimSpace(stdout), "\n") + 1; lines > 12 {
		t.Fatalf("compact status has %d lines:\n%s", lines, stdout)
	}
	for _, want := range []string{"Task:", "Binding:", "Inspect:", "Modify:", "Blocking:", "Automatic evidence:", "Next:"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("compact status missing %q:\n%s", want, stdout)
		}
	}
	code, stdout, stderr = captureTaskSessionCommand(t, func() int {
		return runTaskStatus([]string{"--repo", repo, "--active", "--compact", "--json"})
	})
	if code != 0 || !strings.Contains(stdout, `"task_control"`) || !strings.Contains(stdout, `"binding_health"`) {
		t.Fatalf("stable JSON status exit=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
}

func TestPrepareChangeCLIRejectsMissingScope(t *testing.T) {
	repo, graph := taskSessionTestRepo(t)
	code, _, stderr := captureTaskSessionCommand(t, func() int {
		return runPrepareChange([]string{
			"--repo", repo,
			"--repo-domain", "github.com/example/project",
			"--description", "Missing scope.",
			"--mode", "modify",
			"--task-class", "missing_scope",
			"--risk-class", "architecture_sensitive",
			"--direction", "preserve",
			"--graph-nt", graph,
		})
	})
	if code == 0 {
		t.Fatal("prepare-change accepted missing scope")
	}
	if !strings.Contains(stderr, "scope anchor") {
		t.Fatalf("unexpected stderr: %s", stderr)
	}
}

func TestPrepareChangeCLIRejectsMissingProjectInference(t *testing.T) {
	repo, graph := taskSessionTestRepo(t)
	if err := os.Remove(filepath.Join(repo, ".sensei", "project", "claims.yaml")); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := captureTaskSessionCommand(t, func() int {
		return runPrepareChange([]string{
			"--repo", repo,
			"--repo-domain", "github.com/example/project",
			"--description", "Inspect route ownership.",
			"--mode", "inspect",
			"--task-class", "route_ownership",
			"--risk-class", "architecture_sensitive",
			"--direction", "preserve",
			"--graph-nt", graph,
			"--file", "read:gin.go",
		})
	})
	if code == 0 || !strings.Contains(stderr, "task input incomplete: inference not run") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
}

func captureTaskSessionCommand(t *testing.T, fn func() int) (int, string, string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = outW, errW
	code := fn()
	_ = outW.Close()
	_ = errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	var outBuf, errBuf bytes.Buffer
	_, _ = io.Copy(&outBuf, outR)
	_, _ = io.Copy(&errBuf, errR)
	return code, outBuf.String(), errBuf.String()
}

func taskSessionTestRepo(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	taskSessionWrite(t, root, "gin.go", "package gin\n")
	taskSessionWrite(t, root, "gin_test.go", "package gin\n")
	taskSessionGit(t, root, "init")
	taskSessionGit(t, root, "config", "user.email", "sensei@example.test")
	taskSessionGit(t, root, "config", "user.name", "Sensei Test")
	taskSessionGit(t, root, "add", ".")
	taskSessionGit(t, root, "commit", "-m", "initial")
	graph := strings.Join([]string{
		taskSessionTriple("https://globular.io/awareness#sourceFile/gin.go", rdf.PropType, rdf.ClassSourceFile, true),
		taskSessionTriple("https://globular.io/awareness#sourceFile/gin.go", rdf.PropSourcePath, "gin.go", false),
		taskSessionTriple("https://globular.io/awareness#sourceFile/gin_test.go", rdf.PropType, rdf.ClassSourceFile, true),
		taskSessionTriple("https://globular.io/awareness#sourceFile/gin_test.go", rdf.PropSourcePath, "gin_test.go", false),
		"",
	}, "\n")
	graphPath := filepath.Join(root, "graph.nt")
	if err := os.WriteFile(graphPath, []byte(graph), 0o644); err != nil {
		t.Fatalf("write graph: %v", err)
	}
	taskSessionWriteProjectClaims(t, root, graphPath)
	return root, graphPath
}

func taskSessionWriteProjectClaims(t *testing.T, root, graphPath string) {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("resolve revision: %v", err)
	}
	revision := strings.TrimSpace(string(out))
	domain := "github.com/example/project"
	provenance := architecture.Provenance{
		RepositoryDomain:       domain,
		RepositoryDomainStatus: architecture.RepositoryDomainResolved,
		Revision:               revision,
		RevisionStatus:         architecture.RevisionResolved,
		SourceDigest:           taskSessionDigest(t, filepath.Join(root, "gin.go")),
		SourceDigestStatus:     architecture.SourceDigestResolved,
		SourceKind:             "source_file",
	}
	fact := architecture.Fact{
		ID:         "fact.task-session-cli-test",
		Kind:       "guard",
		Subject:    "gin.Engine",
		Predicate:  "refuses_when",
		Object:     "route state is invalid",
		Scope:      architecture.Scope{Repository: domain, Files: []string{"gin.go"}, Symbols: []string{"gin.Engine"}},
		Evidence:   architecture.Evidence{SourceFile: "gin.go", LineStart: 1, LineEnd: 1},
		Confidence: 0.6,
		Extractor:  "task_session_cli_test",
		Provenance: &provenance,
	}
	claim := architecture.Claim{
		ID:                     "claim.task-session-cli-test",
		Label:                  "Engine rejects invalid route state",
		Statement:              architecture.ClaimStatement{Subject: "gin.Engine", Predicate: "refuses_when", Object: "route state is invalid"},
		Scope:                  architecture.ClaimScope{Repository: domain, Repo: domain, Files: []string{"gin.go"}, Symbols: []string{"gin.Engine"}},
		ArchitecturalPlane:     architecture.PlaneObserved,
		AssertionOrigin:        architecture.OriginDerived,
		EpistemicStatus:        architecture.StatusSupported,
		InferenceRule:          "rule.task_session_cli_test.v1",
		PremiseFacts:           []string{fact.ID},
		InvalidationConditions: []string{"The premise fact changes."},
		Confidence:             0.6,
		HumanReviewRequired:    true,
		PromotionStatus:        architecture.PromotionCandidate,
	}
	doc := architecture.ClaimDocument{
		SchemaVersion: "1",
		GeneratedBy:   "task session CLI test",
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  domain,
			Revision:          revision,
			RevisionStatus:    architecture.RevisionResolved,
			GraphDigestSHA256: taskSessionDigest(t, graphPath),
			GraphDigestStatus: architecture.GraphDigestResolved,
		},
		FactReceipts: []architecture.ClaimFactReceipt{{Fact: fact, Provenance: provenance}},
		Claims:       []architecture.Claim{claim},
	}
	data, err := architecture.MarshalCanonicalClaimDocumentYAML(doc)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	path := filepath.Join(root, ".sensei", "project", "claims.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write claims: %v", err)
	}
}

func taskSessionDigest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func taskSessionTriple(s, p, o string, iri bool) string {
	obj := taskSessionQuote(o)
	if iri {
		obj = "<" + o + ">"
	}
	return "<" + s + "> <" + p + "> " + obj + " ."
}

func taskSessionQuote(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return `"` + v + `"`
}

func taskSessionWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func taskSessionGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
