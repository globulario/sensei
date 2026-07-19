// SPDX-License-Identifier: Apache-2.0

package binding

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"gopkg.in/yaml.v3"
)

func TestResolveBaseSameCommittedRepositoryAcrossCheckouts(t *testing.T) {
	root := testRepo(t)
	other := filepath.Join(t.TempDir(), "clone")
	git(t, "", "clone", root, other)
	graphA := writeGraph(t, root, "graph-a.nt", "<a> <b> \"c\" .\n")
	graphB := writeGraph(t, other, "graph-b.nt", "<a> <b> \"c\" .\n")
	policies := testPolicies()

	baseA, err := ResolveBase(ResolveBaseOptions{
		RepoRoot: root, RepositoryDomain: "github.com/example/project", GraphPath: graphA,
		TaskID: "task.example", SessionID: "session.example", IterationDigest: "iter", Policies: policies,
	})
	if err != nil {
		t.Fatal(err)
	}
	baseB, err := ResolveBase(ResolveBaseOptions{
		RepoRoot: other, RepositoryDomain: "github.com/example/project", GraphPath: graphB,
		TaskID: "task.example", SessionID: "session.example", IterationDigest: "iter", Policies: policies,
	})
	if err != nil {
		t.Fatal(err)
	}
	if digestA, digestB := mustBaseDigest(t, baseA), mustBaseDigest(t, baseB); digestA != digestB {
		t.Fatalf("base digest mismatch across checkouts: %s vs %s", digestA, digestB)
	}
	if baseA.Repository.TreeDigestSHA256 != baseB.Repository.TreeDigestSHA256 {
		t.Fatalf("tree digests differ: %s vs %s", baseA.Repository.TreeDigestSHA256, baseB.Repository.TreeDigestSHA256)
	}
}

func TestResolveBaseSameGraphBytesDifferentPaths(t *testing.T) {
	root := testRepo(t)
	graphA := writeGraph(t, root, "graphs/a.nt", "<a> <b> \"c\" .\n")
	graphB := writeGraph(t, root, "graphs/b.nt", "<a> <b> \"c\" .\n")
	opts := ResolveBaseOptions{
		RepoRoot: root, RepositoryDomain: "github.com/example/project", TaskID: "task.example",
		SessionID: "session.example", IterationDigest: "iter", Policies: testPolicies(),
	}
	baseA, err := ResolveBase(withGraph(opts, graphA))
	if err != nil {
		t.Fatal(err)
	}
	baseB, err := ResolveBase(withGraph(opts, graphB))
	if err != nil {
		t.Fatal(err)
	}
	if baseA.Graph.DigestSHA256 != baseB.Graph.DigestSHA256 {
		t.Fatalf("graph digest mismatch: %s vs %s", baseA.Graph.DigestSHA256, baseB.Graph.DigestSHA256)
	}
	if digestA, digestB := mustBaseDigest(t, baseA), mustBaseDigest(t, baseB); digestA != digestB {
		t.Fatalf("base digest mismatch across graph paths: %s vs %s", digestA, digestB)
	}
}

func TestDifferentRevisionChangesRepositoryBinding(t *testing.T) {
	root := testRepo(t)
	graph := writeGraph(t, root, "graph.nt", "<a> <b> \"c\" .\n")
	baseA, err := ResolveBase(ResolveBaseOptions{
		RepoRoot: root, RepositoryDomain: "github.com/example/project", GraphPath: graph,
		TaskID: "task.example", SessionID: "session.example", Policies: testPolicies(),
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "new.txt", "next")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "next")
	baseB, err := ResolveBase(ResolveBaseOptions{
		RepoRoot: root, RepositoryDomain: "github.com/example/project", GraphPath: graph,
		TaskID: "task.example", SessionID: "session.example", Policies: testPolicies(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if baseA.Repository.Revision == baseB.Repository.Revision {
		t.Fatal("revision did not change")
	}
	if mustBaseDigest(t, baseA) == mustBaseDigest(t, baseB) {
		t.Fatal("base binding digest did not change with revision")
	}
}

func TestDifferentCommittedTreeChangesTreeDigest(t *testing.T) {
	root := testRepo(t)
	graph := writeGraph(t, root, "graph.nt", "<a> <b> \"c\" .\n")
	baseA, err := ResolveBase(ResolveBaseOptions{
		RepoRoot: root, RepositoryDomain: "github.com/example/project", GraphPath: graph,
		TaskID: "task.example", SessionID: "session.example", Policies: testPolicies(),
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "tracked.txt", "changed")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "tracked change")
	baseB, err := ResolveBase(ResolveBaseOptions{
		RepoRoot: root, RepositoryDomain: "github.com/example/project", GraphPath: graph,
		TaskID: "task.example", SessionID: "session.example", Policies: testPolicies(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if baseA.Repository.TreeDigestSHA256 == baseB.Repository.TreeDigestSHA256 {
		t.Fatal("tree digest did not change")
	}
}

func TestDifferentPolicyIDsChangeBindingDigest(t *testing.T) {
	root := testRepo(t)
	graph := writeGraph(t, root, "graph.nt", "<a> <b> \"c\" .\n")
	baseA, err := ResolveBase(ResolveBaseOptions{
		RepoRoot: root, RepositoryDomain: "github.com/example/project", GraphPath: graph,
		TaskID: "task.example", SessionID: "session.example", Policies: testPolicies(),
	})
	if err != nil {
		t.Fatal(err)
	}
	policies := testPolicies()
	policies.Completion = "completion.architectural_closure.v2"
	baseB, err := ResolveBase(ResolveBaseOptions{
		RepoRoot: root, RepositoryDomain: "github.com/example/project", GraphPath: graph,
		TaskID: "task.example", SessionID: "session.example", Policies: policies,
	})
	if err != nil {
		t.Fatal(err)
	}
	if mustBaseDigest(t, baseA) == mustBaseDigest(t, baseB) {
		t.Fatal("policy change did not change base digest")
	}
}

func TestMissingGraphDigestFails(t *testing.T) {
	root := testRepo(t)
	_, err := ResolveBase(ResolveBaseOptions{
		RepoRoot: root, RepositoryDomain: "github.com/example/project", GraphPath: "",
		TaskID: "task.example", SessionID: "session.example", Policies: testPolicies(),
	})
	if err == nil {
		t.Fatal("expected missing graph path to fail")
	}
}

func TestUnresolvedRevisionFailsForMutationCapableUse(t *testing.T) {
	root := t.TempDir()
	graph := writeGraph(t, root, "graph.nt", "<a> <b> \"c\" .\n")
	_, err := ResolveBase(ResolveBaseOptions{
		RepoRoot: root, RepositoryDomain: "github.com/example/project", GraphPath: graph,
		TaskID: "task.example", SessionID: "session.example", Policies: testPolicies(),
	})
	if err == nil {
		t.Fatal("expected unresolved revision to fail")
	}
}

func TestBaseBindingCannotValidateAsResultBinding(t *testing.T) {
	base := closureprotocol.BaseBinding{
		Repository: closureprotocol.RepositorySnapshot{
			Domain: "github.com/example/project", Revision: "abc", RevisionStatus: architecture.RevisionResolved, TreeDigestSHA256: "tree",
		},
		Graph: closureprotocol.GraphSnapshot{
			DigestSHA256: "graph", DigestStatus: architecture.GraphDigestResolved, SchemaVersion: DefaultGraphSchemaVersion,
		},
		Task:     closureprotocol.TaskBinding{ID: "task.example", SessionID: "session.example"},
		Policies: testPolicies(),
	}
	result := closureprotocol.ResultBinding{BaseRevision: base.Repository.Revision}
	if err := ValidateResult(result); err == nil {
		t.Fatal("expected incomplete result binding to fail")
	}
}

func TestResultBindingWithUnrelatedBaseRevisionFails(t *testing.T) {
	base := closureprotocol.BaseBinding{
		Repository: closureprotocol.RepositorySnapshot{
			Domain: "github.com/example/project", Revision: "abc", RevisionStatus: architecture.RevisionResolved, TreeDigestSHA256: "tree",
		},
		Graph: closureprotocol.GraphSnapshot{
			DigestSHA256: "graph", DigestStatus: architecture.GraphDigestResolved, SchemaVersion: DefaultGraphSchemaVersion,
		},
		Task:     closureprotocol.TaskBinding{ID: "task.example", SessionID: "session.example"},
		Policies: testPolicies(),
	}
	result := closureprotocol.ResultBinding{
		BaseRevision: "other", PatchDigestSHA256: "patch", ResultTreeDigestSHA256: "tree2", GraphDigestSHA256: "graph2",
	}
	if err := CompareBaseAndResult(base, result); err == nil {
		t.Fatal("expected unrelated base revision to fail")
	}
}

func TestRepositoryOnlyBaseBindingHasNoRuntimeTarget(t *testing.T) {
	root := testRepo(t)
	graph := writeGraph(t, root, "graph.nt", "<a> <b> \"c\" .\n")
	base, err := ResolveBase(ResolveBaseOptions{
		RepoRoot: root, RepositoryDomain: "github.com/example/project", GraphPath: graph,
		TaskID: "task.example", SessionID: "session.example", Policies: testPolicies(),
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := closureprotocol.CanonicalJSON(base)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("runtime_target")) {
		t.Fatalf("base binding unexpectedly contains runtime target: %s", data)
	}
}

func TestCanonicalYAMLAndJSONProduceSameSemanticDigest(t *testing.T) {
	root := testRepo(t)
	graph := writeGraph(t, root, "graph.nt", "<a> <b> \"c\" .\n")
	base, err := ResolveBase(ResolveBaseOptions{
		RepoRoot: root, RepositoryDomain: "github.com/example/project", GraphPath: graph,
		TaskID: "task.example", SessionID: "session.example", Policies: testPolicies(),
	})
	if err != nil {
		t.Fatal(err)
	}
	yamlData, err := CanonicalYAML(base)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip closureprotocol.BaseBinding
	if err := yaml.Unmarshal(yamlData, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if got, want := mustBaseDigest(t, roundTrip), mustBaseDigest(t, base); got != want {
		t.Fatalf("yaml round trip changed semantic digest: %s vs %s", got, want)
	}
}

func TestAbsolutePathsDoNotAffectSemanticIdentity(t *testing.T) {
	root := testRepo(t)
	other := t.TempDir()
	graphBytes := []byte("<a> <b> \"c\" .\n")
	graphA := writeGraphBytes(t, filepath.Join(root, "graph.nt"), graphBytes)
	graphB := writeGraphBytes(t, filepath.Join(other, "graph.nt"), graphBytes)
	opts := ResolveBaseOptions{
		RepoRoot: root, RepositoryDomain: "github.com/example/project", TaskID: "task.example",
		SessionID: "session.example", Policies: testPolicies(),
	}
	baseA, err := ResolveBase(withGraph(opts, graphA))
	if err != nil {
		t.Fatal(err)
	}
	baseB, err := ResolveBase(withGraph(opts, graphB))
	if err != nil {
		t.Fatal(err)
	}
	if mustBaseDigest(t, baseA) != mustBaseDigest(t, baseB) {
		t.Fatal("absolute graph path changed semantic identity")
	}
}

func withGraph(opts ResolveBaseOptions, graph string) ResolveBaseOptions {
	opts.GraphPath = graph
	return opts
}

func mustBaseDigest(t *testing.T, base closureprotocol.BaseBinding) string {
	t.Helper()
	digest, err := SemanticDigestBase(base)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func testPolicies() closureprotocol.PolicyBinding {
	return closureprotocol.PolicyBinding{
		Admission: "admission.strict.v2", Certification: "certification.architectural_closure.v1",
		Completion: "completion.architectural_closure.v1", Revocation: "revocation.architectural_closure.v1",
		Ledger: "ledger.task.v1", Canonicalization: "canonicalization.architectural_closure.v1",
	}
}

func testRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	git(t, root, "init")
	git(t, root, "config", "user.email", "sensei@example.test")
	git(t, root, "config", "user.name", "Sensei Test")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial")
	return root
}

func writeGraph(t *testing.T, root, rel, content string) string {
	t.Helper()
	return writeGraphBytes(t, filepath.Join(root, rel), []byte(content))
}

func writeGraphBytes(t *testing.T, path string, data []byte) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if root != "" {
		cmd.Dir = root
	}
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Sensei Test",
		"GIT_AUTHOR_EMAIL=sensei@example.test",
		"GIT_COMMITTER_NAME=Sensei Test",
		"GIT_COMMITTER_EMAIL=sensei@example.test",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, strings.TrimSpace(string(out)))
	}
}
