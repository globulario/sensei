// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/inference"
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

func TestInferClaimsHelpMentionsGraphNT(t *testing.T) {
	code, _, stderr := captureStdoutStderr(t, func() int {
		return runInferClaims([]string{"--help"})
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "-graph-nt") {
		t.Fatalf("help missing --graph-nt:\n%s", stderr)
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

func TestInferClaimsWithoutGraphNTKeepsGovernedDirectionInactive(t *testing.T) {
	root := writeInferClaimsFixture(t, true)
	reg, err := inference.DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	result, err := buildInferClaimsResult(root, inferClaimsOptions{
		Repo:              root,
		GraphDigestStatus: architecture.GraphDigestResolved,
		GraphDigest:       strings.Repeat("a", 64),
	}, reg)
	if err != nil {
		t.Fatal(err)
	}
	for _, claim := range result.Document.Claims {
		if claim.InferenceRule == "rule.governed_direction_record.v1" {
			t.Fatalf("unexpected governed-direction claim without --graph-nt: %+v", claim)
		}
	}
	if !containsLimitationReason(result.Document.Limitations, "governed direction bridge inactive") {
		t.Fatalf("missing inactive bridge diagnostic: %+v", result.Document.Limitations)
	}
}

func TestInferClaimsSynthesizesGovernedDirectionalClaimsFromGraph(t *testing.T) {
	root := writeInferClaimsFixture(t, true)
	graphPath, digest := writeGovernedDirectionGraphFixture(t, root, "active", "active")
	out := filepath.Join(root, "claims.yaml")
	code, _, stderr := captureStdoutStderr(t, func() int {
		return runInferClaims([]string{
			"--repo", root,
			"--graph-nt", graphPath,
			"--graph-digest-status", "resolved",
			"--graph-digest", digest,
			"--output", out,
		})
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	doc, err := architecture.LoadClaimDocument(out)
	if err != nil {
		t.Fatalf("load claims: %v", err)
	}
	var intended, desired int
	for _, claim := range doc.Claims {
		if claim.InferenceRule != "rule.governed_direction_record.v1" {
			continue
		}
		if len(claim.Scope.Files) != 1 || claim.Scope.Files[0] != "state.go" {
			t.Fatalf("governed claim scope=%v want [state.go]", claim.Scope.Files)
		}
		if claim.EpistemicStatus != architecture.StatusSupported {
			t.Fatalf("governed claim status=%s want supported", claim.EpistemicStatus)
		}
		if claim.ArchitecturalPlane == architecture.PlaneIntended {
			intended++
		}
		if claim.ArchitecturalPlane == architecture.PlaneDesired {
			desired++
		}
	}
	if intended == 0 || desired == 0 {
		t.Fatalf("expected governed intended+desired claims, got intended=%d desired=%d", intended, desired)
	}
	if doc.Binding.GraphDigestSHA256 != digest || doc.Binding.GraphDigestStatus != architecture.GraphDigestResolved {
		t.Fatalf("graph binding=%+v want digest=%s resolved", doc.Binding, digest)
	}
	var foundReceipt bool
	for _, receipt := range doc.FactReceipts {
		if receipt.Fact.Extractor != "governed_direction_graph_extractor" {
			continue
		}
		if receipt.Fact.Evidence.SourceFile == "docs/awareness/architecture/decisions.yaml" && receipt.Provenance.SourceDigestStatus == architecture.SourceDigestResolved {
			foundReceipt = true
			break
		}
	}
	if !foundReceipt {
		t.Fatalf("missing governed-direction fact receipt with authored source provenance: %+v", doc.FactReceipts)
	}
}

func TestInferClaimsSkipsDraftGovernedDirectionalRecords(t *testing.T) {
	root := writeInferClaimsFixture(t, true)
	graphPath, digest := writeGovernedDirectionGraphFixture(t, root, "draft", "draft")
	out := filepath.Join(root, "claims.yaml")
	code, _, stderr := captureStdoutStderr(t, func() int {
		return runInferClaims([]string{
			"--repo", root,
			"--graph-nt", graphPath,
			"--graph-digest-status", "resolved",
			"--graph-digest", digest,
			"--output", out,
		})
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	doc, err := architecture.LoadClaimDocument(out)
	if err != nil {
		t.Fatalf("load claims: %v", err)
	}
	for _, claim := range doc.Claims {
		if claim.InferenceRule == "rule.governed_direction_record.v1" {
			t.Fatalf("unexpected governed-direction claim from draft record: %+v", claim)
		}
	}
}

func TestInferClaimsMissingGraphPathFailsClearly(t *testing.T) {
	root := writeInferClaimsFixture(t, true)
	missing := filepath.Join(root, "missing.nt")
	code, _, stderr := captureStdoutStderr(t, func() int {
		return runInferClaims([]string{
			"--repo", root,
			"--graph-nt", missing,
			"--graph-digest-status", "resolved",
			"--graph-digest", strings.Repeat("a", 64),
		})
	})
	if code != 1 || !strings.Contains(stderr, "no such file") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
}

func TestInferClaimsMalformedGraphFailsClearly(t *testing.T) {
	root := writeInferClaimsFixture(t, true)
	graph := "not ntriples\n"
	path := filepath.Join(root, "broken.nt")
	writeFile(t, path, graph)
	sum := sha256.Sum256([]byte(graph))
	code, _, stderr := captureStdoutStderr(t, func() int {
		return runInferClaims([]string{
			"--repo", root,
			"--graph-nt", path,
			"--graph-digest-status", "resolved",
			"--graph-digest", hex.EncodeToString(sum[:]),
		})
	})
	if code != 1 || !strings.Contains(stderr, "line 1") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
}

func TestInferClaimsGovernedDirectionRequiresResolvedRevisionBinding(t *testing.T) {
	root := writeInferClaimsFixture(t, false)
	graphPath, digest := writeGovernedDirectionGraphFixture(t, root, "active", "active")
	code, _, stderr := captureStdoutStderr(t, func() int {
		return runInferClaims([]string{
			"--repo", root,
			"--graph-nt", graphPath,
			"--graph-digest-status", "resolved",
			"--graph-digest", digest,
		})
	})
	if code != 1 || !strings.Contains(stderr, "resolved repository revision binding") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
}

func TestInferClaimsGraphDigestMismatchFailsClearly(t *testing.T) {
	root := writeInferClaimsFixture(t, true)
	graphPath, _ := writeGovernedDirectionGraphFixture(t, root, "active", "active")
	code, _, stderr := captureStdoutStderr(t, func() int {
		return runInferClaims([]string{
			"--repo", root,
			"--graph-nt", graphPath,
			"--graph-digest-status", "resolved",
			"--graph-digest", strings.Repeat("f", 64),
		})
	})
	if code != 1 || !strings.Contains(stderr, "digest does not match") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
}

func TestInferClaimsReportsMalformedGovernedDirectionRecord(t *testing.T) {
	root := writeInferClaimsFixture(t, true)
	graph := strings.Join([]string{
		"<https://globular.io/awareness#decision/decision.bad.intended> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#Decision> .",
		"<https://globular.io/awareness#decision/decision.bad.intended> <https://globular.io/awareness#status> \"accepted\" .",
		"<https://globular.io/awareness#decision/decision.bad.intended> <https://globular.io/awareness#architecturalPlane> \"intended\" .",
		"<https://globular.io/awareness#decision/decision.bad.intended> <https://globular.io/awareness#label> \"Bad intended record\" .",
		"<https://globular.io/awareness#decision/decision.bad.intended> <https://globular.io/awareness#authoredIn> \"docs/awareness/architecture/decisions.yaml\" .",
	}, "\n") + "\n"
	path := filepath.Join(root, "malformed-graph.nt")
	writeFile(t, path, graph)
	sum := sha256.Sum256([]byte(graph))
	reg, err := inference.DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	result, err := buildInferClaimsResult(root, inferClaimsOptions{
		Repo:              root,
		GraphNT:           path,
		GraphDigestStatus: architecture.GraphDigestResolved,
		GraphDigest:       hex.EncodeToString(sum[:]),
	}, reg)
	if err != nil {
		t.Fatal(err)
	}
	if !containsLimitationReason(result.Document.Limitations, "lacks represented source_file or code_symbol anchors") {
		t.Fatalf("missing malformed-record diagnostic: %+v", result.Document.Limitations)
	}
}

func TestInferClaimsPreservesConflictingGovernedDirections(t *testing.T) {
	root := writeInferClaimsFixture(t, true)
	graphLines := []string{
		"<https://globular.io/awareness#decision/decision.x> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#Decision> .",
		"<https://globular.io/awareness#decision/decision.x> <https://globular.io/awareness#status> \"accepted\" .",
		"<https://globular.io/awareness#decision/decision.x> <https://globular.io/awareness#architecturalPlane> \"desired\" .",
		"<https://globular.io/awareness#decision/decision.x> <https://globular.io/awareness#label> \"Desired outcome A\" .",
		"<https://globular.io/awareness#decision/decision.x> <https://globular.io/awareness#authoredIn> \"docs/awareness/architecture/decisions.yaml\" .",
		"<https://globular.io/awareness#decision/decision.x> <https://globular.io/awareness#expressedBy> <https://globular.io/awareness#sourceFile/state.go> .",
		"<https://globular.io/awareness#decision/decision.y> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#Decision> .",
		"<https://globular.io/awareness#decision/decision.y> <https://globular.io/awareness#status> \"accepted\" .",
		"<https://globular.io/awareness#decision/decision.y> <https://globular.io/awareness#architecturalPlane> \"desired\" .",
		"<https://globular.io/awareness#decision/decision.y> <https://globular.io/awareness#label> \"Desired outcome B\" .",
		"<https://globular.io/awareness#decision/decision.y> <https://globular.io/awareness#authoredIn> \"docs/awareness/architecture/decisions.yaml\" .",
		"<https://globular.io/awareness#decision/decision.y> <https://globular.io/awareness#expressedBy> <https://globular.io/awareness#sourceFile/state.go> .",
		"<https://globular.io/awareness#sourceFile/state.go> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#SourceFile> .",
	}
	graphPath, digest := writeGraphFixture(t, root, "conflict.nt", graphLines)
	out := filepath.Join(root, "claims.yaml")
	code, _, stderr := captureStdoutStderr(t, func() int {
		return runInferClaims([]string{
			"--repo", root,
			"--graph-nt", graphPath,
			"--graph-digest-status", "resolved",
			"--graph-digest", digest,
			"--output", out,
		})
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	doc, err := architecture.LoadClaimDocument(out)
	if err != nil {
		t.Fatal(err)
	}
	var governed []architecture.Claim
	for _, claim := range doc.Claims {
		if claim.InferenceRule == "rule.governed_direction_record.v1" {
			governed = append(governed, claim)
		}
	}
	if len(governed) != 2 {
		t.Fatalf("governed claims=%d want 2", len(governed))
	}
	for _, claim := range governed {
		if claim.EpistemicStatus != architecture.StatusContested || len(claim.ConflictsWith) != 1 {
			t.Fatalf("claim did not preserve conflict: %+v", claim)
		}
	}
}

func TestInferClaimsGovernedDirectionClaimDigestIgnoresGraphPathAndStatementOrder(t *testing.T) {
	root := writeInferClaimsFixture(t, true)
	lines := []string{
		"<https://globular.io/awareness#intent/awareness.test.intended> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#Intent> .",
		"<https://globular.io/awareness#intent/awareness.test.intended> <https://globular.io/awareness#status> \"active\" .",
		"<https://globular.io/awareness#intent/awareness.test.intended> <https://globular.io/awareness#architecturalPlane> \"intended\" .",
		"<https://globular.io/awareness#intent/awareness.test.intended> <https://globular.io/awareness#label> \"Current intended awareness mutation architecture\" .",
		"<https://globular.io/awareness#intent/awareness.test.intended> <https://globular.io/awareness#authoredIn> \"docs/awareness/architecture/decisions.yaml\" .",
		"<https://globular.io/awareness#intent/awareness.test.intended> <https://globular.io/awareness#expressedBy> <https://globular.io/awareness#sourceFile/state.go> .",
		"<https://globular.io/awareness#sourceFile/state.go> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#SourceFile> .",
	}
	pathA, digestA := writeGraphFixture(t, root, "a/graph.nt", lines)
	pathB, digestB := writeGraphFixture(t, root, "b/graph.nt", lines)
	docA := runInferClaimsDoc(t, root, pathA, digestA)
	docB := runInferClaimsDoc(t, root, pathB, digestB)
	if claimDocDigest(t, docA) != claimDocDigest(t, docB) {
		t.Fatalf("claim digest changed across graph path:\nA=%s\nB=%s", claimDocDigest(t, docA), claimDocDigest(t, docB))
	}

	reversed := append([]string{}, lines...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	pathC, digestC := writeGraphFixture(t, root, "c/graph.nt", reversed)
	docC := runInferClaimsDoc(t, root, pathC, digestC)
	if governedClaimDigest(t, docA) != governedClaimDigest(t, docC) {
		t.Fatalf("governed claim digest changed across graph statement order:\nA=%s\nC=%s", governedClaimDigest(t, docA), governedClaimDigest(t, docC))
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
	writeFile(t, filepath.Join(root, "docs", "awareness", "architecture", "decisions.yaml"), "decisions: []\n")
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

func writeGovernedDirectionGraphFixture(t *testing.T, root, intendedStatus, desiredStatus string) (string, string) {
	t.Helper()
	return writeGraphFixture(t, root, "graph.nt", []string{
		"<https://globular.io/awareness#intent/awareness.test.intended> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#Intent> .",
		"<https://globular.io/awareness#intent/awareness.test.intended> <https://globular.io/awareness#status> \"" + intendedStatus + "\" .",
		"<https://globular.io/awareness#intent/awareness.test.intended> <https://globular.io/awareness#architecturalPlane> \"intended\" .",
		"<https://globular.io/awareness#intent/awareness.test.intended> <https://globular.io/awareness#label> \"Current intended awareness mutation architecture\" .",
		"<https://globular.io/awareness#intent/awareness.test.intended> <https://globular.io/awareness#authoredIn> \"docs/awareness/architecture/decisions.yaml\" .",
		"<https://globular.io/awareness#intent/awareness.test.intended> <https://globular.io/awareness#expressedBy> <https://globular.io/awareness#sourceFile/state.go> .",
		"<https://globular.io/awareness#intent/awareness.test.desired> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#Intent> .",
		"<https://globular.io/awareness#intent/awareness.test.desired> <https://globular.io/awareness#status> \"" + desiredStatus + "\" .",
		"<https://globular.io/awareness#intent/awareness.test.desired> <https://globular.io/awareness#architecturalPlane> \"desired\" .",
		"<https://globular.io/awareness#intent/awareness.test.desired> <https://globular.io/awareness#label> \"Desired awareness mutation recognition outcome\" .",
		"<https://globular.io/awareness#intent/awareness.test.desired> <https://globular.io/awareness#authoredIn> \"docs/awareness/architecture/decisions.yaml\" .",
		"<https://globular.io/awareness#intent/awareness.test.desired> <https://globular.io/awareness#expressedBy> <https://globular.io/awareness#sourceFile/state.go> .",
		"<https://globular.io/awareness#sourceFile/state.go> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#SourceFile> .",
	})
}

func writeGraphFixture(t *testing.T, root, rel string, lines []string) (string, string) {
	t.Helper()
	graph := strings.Join(lines, "\n") + "\n"
	path := filepath.Join(root, filepath.FromSlash(rel))
	writeFile(t, path, graph)
	sum := sha256.Sum256([]byte(graph))
	return path, hex.EncodeToString(sum[:])
}

func runInferClaimsDoc(t *testing.T, root, graphPath, digest string) architecture.ClaimDocument {
	t.Helper()
	out := filepath.Join(root, filepath.Base(graphPath)+".claims.yaml")
	code, _, stderr := captureStdoutStderr(t, func() int {
		return runInferClaims([]string{
			"--repo", root,
			"--graph-nt", graphPath,
			"--graph-digest-status", "resolved",
			"--graph-digest", digest,
			"--output", out,
		})
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	doc, err := architecture.LoadClaimDocument(out)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func claimDocDigest(t *testing.T, doc architecture.ClaimDocument) string {
	t.Helper()
	raw, err := architecture.MarshalCanonicalClaimDocumentYAML(doc)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func governedClaimDigest(t *testing.T, doc architecture.ClaimDocument) string {
	t.Helper()
	var governed []architecture.Claim
	for _, claim := range doc.Claims {
		if claim.InferenceRule == "rule.governed_direction_record.v1" {
			governed = append(governed, claim)
		}
	}
	normalized, err := architecture.NormalizeClaims(governed)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func containsLimitationReason(limitations []architecture.Limitation, sub string) bool {
	for _, lim := range limitations {
		if strings.Contains(lim.Reason, sub) {
			return true
		}
	}
	return false
}
