// SPDX-License-Identifier: Apache-2.0

package generatedartifact

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture/graphbuild"
)

const testDomain = "github.com/globulario/sensei"

const testInvariants = `invariants:
  - id: test.example
    title: Example
    severity: critical
    status: active
    protects:
      files:
        - src/model.go
`

// testContext builds a governed repo, compiles its graph, and returns a Context.
func testContext(t *testing.T) (string, Context) {
	t.Helper()
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "docs", "awareness", "invariants.yaml"), testInvariants)
	writeFile(t, filepath.Join(repo, "src", "model.go"), "package src\n\nfunc Publish() {}\n")

	art, err := graphbuild.Build(context.Background(), graphbuild.CompileRequest{
		RepositoryRoot: repo,
		Sources: []graphbuild.SourceRoot{{
			FilesystemPath: filepath.Join(repo, "docs", "awareness"), IdentityRoot: repo,
			StripPathPrefixes: []string{repo}, RepositoryDomain: testDomain, DefaultDomain: testDomain, SkipNestedGenerated: true,
		}},
		Policy: graphbuild.ClosureStrictPolicy(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return repo, Context{
		RepositoryRoot: repo, RepositoryDomain: testDomain,
		GraphInputPolicyID:             "sensei.resultpipeline.graph-inputs/v1",
		GraphInputSnapshotDigestSHA256: "snap",
		SourceManifestDigestSHA256:     "srcman",
		GraphArtifact:                  art,
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func materialize(t *testing.T, repo string, outputs map[string]Output) {
	t.Helper()
	for _, o := range outputs {
		writeFile(t, filepath.Join(repo, filepath.FromSlash(o.Path)), string(o.Bytes))
	}
}

func TestProfileForDomain(t *testing.T) {
	if _, err := ProfileForDomain(testDomain); err != nil {
		t.Fatalf("self domain must have a profile: %v", err)
	}
	if _, err := ProfileForDomain("github.com/other/repo"); err == nil {
		t.Fatal("unregistered domain must fail (never an empty profile)")
	}
}

func TestRegenerateAndVerifyAllMatched(t *testing.T) {
	repo, in := testContext(t)
	profile, _ := ProfileForDomain(testDomain)
	outputs, err := Generate(context.Background(), in, profile)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(outputs) != 3 {
		t.Fatalf("expected 3 outputs, got %d", len(outputs))
	}
	materialize(t, repo, outputs)

	res, err := RegenerateAndVerify(context.Background(), in, profile)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !res.Manifest.AllRequiredMatched || len(res.Manifest.Limitations) != 0 {
		t.Fatalf("not all matched: %+v", res.Manifest)
	}
	if len(res.VerifiedArtifacts) != 3 {
		t.Fatalf("expected 3 verified artifacts, got %d", len(res.VerifiedArtifacts))
	}
	// Sorted by path; the result manifest is present (not excluded).
	for i := 1; i < len(res.VerifiedArtifacts); i++ {
		if res.VerifiedArtifacts[i-1].Path > res.VerifiedArtifacts[i].Path {
			t.Fatal("verified artifacts must be sorted by path")
		}
	}
	// The result-manifest producer runs last (after A and B).
	if res.Manifest.ProducerIDs[len(res.Manifest.ProducerIDs)-1] != resultManifestProducerID {
		t.Fatalf("result manifest producer must run last: %v", res.Manifest.ProducerIDs)
	}
}

func TestRegenerateAndVerifyMissing(t *testing.T) {
	repo, in := testContext(t)
	profile, _ := ProfileForDomain(testDomain)
	outputs, _ := Generate(context.Background(), in, profile)
	materialize(t, repo, outputs)
	// Remove the embedded graph.
	if err := os.Remove(filepath.Join(repo, filepath.FromSlash(embeddedGraphPath))); err != nil {
		t.Fatal(err)
	}
	_, err := RegenerateAndVerify(context.Background(), in, profile)
	if err == nil {
		t.Fatal("a missing required artifact must fail")
	}
	if ge, ok := err.(*Error); !ok || ge.Manifest.AllRequiredMatched {
		t.Fatalf("expected a typed error with an inspectable manifest, got %v", err)
	}
}

func TestRegenerateAndVerifyStale(t *testing.T) {
	repo, in := testContext(t)
	profile, _ := ProfileForDomain(testDomain)
	outputs, _ := Generate(context.Background(), in, profile)
	materialize(t, repo, outputs)
	// Corrupt the proof obligations by one byte.
	p := filepath.Join(repo, filepath.FromSlash(proofObligationsPath))
	writeFile(t, p, "# tampered\n")
	if _, err := RegenerateAndVerify(context.Background(), in, profile); err == nil {
		t.Fatal("a stale required artifact must fail (no reformat tolerance)")
	}
}

// The embedded-graph producer reuses the Stage-3 bytes exactly — its expected
// bytes equal the graph artifact, never a rebuild.
func TestEmbeddedGraphReusesStageThree(t *testing.T) {
	_, in := testContext(t)
	out, err := embeddedGraphProducer{}.Generate(context.Background(), in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Bytes) != string(in.GraphArtifact.NTriples) {
		t.Fatal("embedded graph producer must reuse the exact Stage-3 graph bytes")
	}
	if out.SemanticDigestSHA256 != in.GraphArtifact.GraphSemanticDigestSHA256 {
		t.Fatal("embedded graph semantic digest must be the Stage-3 graph digest")
	}
}
