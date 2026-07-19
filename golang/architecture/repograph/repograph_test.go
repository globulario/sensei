// SPDX-License-Identifier: Apache-2.0

package repograph

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/governedmutation"
	"github.com/globulario/sensei/golang/architecture/graphsnapshot"
	"github.com/globulario/sensei/golang/propose"
)

const testDomain = "github.com/globulario/sensei"

// seedRepo builds a temp repo with governed sources (an invariant + a failure
// mode linking it), so the compiled graph carries real nodes, IRI edges, and
// literals. Returns the root and the current governed-source manifest digest.
func seedRepo(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}
	apply := func(p propose.Request) {
		if _, err := governedmutation.Apply(governedmutation.Request{RepositoryRoot: root, Proposal: p}); err != nil {
			t.Fatalf("seed apply: %v", err)
		}
	}
	apply(propose.Request{
		Kind: "invariant", ID: "invariant.test.reload_validates",
		Title: "Reload validates before serving", Description: "x",
		SourceFiles: []string{"golang/server/reload.go"}, RelatedFailures: []string{"failure.test.stale_seed"},
		Domain: testDomain,
	})
	apply(propose.Request{
		Kind: "failure_mode", ID: "failure.test.stale_seed",
		Title: "Stale seed served after reload", Description: "x", Severity: "high",
		RelatedInvariants: []string{"invariant.test.reload_validates"}, Evidence: []string{"observed stale"},
		Domain: testDomain,
	})
	m, err := governedmutation.GovernedManifestDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, m
}

func buildOK(t *testing.T, root, manifest string) VerifiedProjection {
	t.Helper()
	vp, err := Build(context.Background(), BuildRequest{RepositoryRoot: root, RepositoryDomain: testDomain, ExpectedManifestDigestSHA256: manifest})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !vp.Verified {
		t.Fatal("projection not verified")
	}
	return vp
}

// 1 — deterministic replay produces identical identities.
func TestDeterministicReplayIdenticalIdentities(t *testing.T) {
	root, m := seedRepo(t)
	first := buildOK(t, root, m)
	if first.Disposition != DispositionBuilt {
		t.Fatalf("first disposition = %s, want built", first.Disposition)
	}
	second := buildOK(t, root, m)
	if second.Disposition != DispositionReplayed {
		t.Fatalf("second disposition = %s, want replayed", second.Disposition)
	}
	if first.CompiledGraphByteDigestSHA256 != second.CompiledGraphByteDigestSHA256 ||
		first.GraphSemanticDigestSHA256 != second.GraphSemanticDigestSHA256 ||
		first.MarkerDigestSHA256 != second.MarkerDigestSHA256 ||
		first.MarkerIRI != second.MarkerIRI ||
		first.GraphBuildInputDigestSHA256 != second.GraphBuildInputDigestSHA256 {
		t.Fatal("replay produced different identities")
	}
}

// 2 + 3 — semantic digest and marker correspond to the independently reloaded graph.
func TestSemanticDigestAndMarkerMatchReloaded(t *testing.T) {
	root, m := seedRepo(t)
	vp := buildOK(t, root, m)
	reloaded, err := VerifyPersisted(context.Background(), root)
	if err != nil {
		t.Fatalf("verify persisted: %v", err)
	}
	if reloaded.GraphSemanticDigestSHA256 != vp.GraphSemanticDigestSHA256 {
		t.Fatal("reloaded semantic digest != built")
	}
	if reloaded.CompiledGraphByteDigestSHA256 != vp.CompiledGraphByteDigestSHA256 {
		t.Fatal("reloaded byte digest != built")
	}
	if reloaded.MarkerDigestSHA256 != vp.GraphSemanticDigestSHA256 {
		t.Fatal("marker digest != graph semantic digest")
	}
}

// 4 — stale expected manifest fails closed before compile/persist.
func TestStaleManifestBeforeCompileFailsClosed(t *testing.T) {
	root, _ := seedRepo(t)
	_, err := Build(context.Background(), BuildRequest{
		RepositoryRoot: root, RepositoryDomain: testDomain,
		ExpectedManifestDigestSHA256: "0000000000000000000000000000000000000000000000000000000000000000",
	})
	var se *StaleManifestError
	if !errors.As(err, &se) || se.Phase != "before_compile" {
		t.Fatalf("err = %v, want StaleManifestError(before_compile)", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(GraphRelPath))); !os.IsNotExist(statErr) {
		t.Fatal("stale manifest must not persist a graph")
	}
}

// 5 — a source mutation during the compile window fails closed, not verified.
func TestSourceMutationDuringCompileFailsClosed(t *testing.T) {
	root, m := seedRepo(t)
	_, err := buildWith(context.Background(),
		BuildRequest{RepositoryRoot: root, RepositoryDomain: testDomain, ExpectedManifestDigestSHA256: m},
		buildDeps{afterCompile: func() {
			// A concurrent writer changes a governed source mid-build.
			_, _ = governedmutation.Apply(governedmutation.Request{RepositoryRoot: root, Proposal: propose.Request{
				Kind: "forbidden_fix", ID: "forbidden_fix.test.race", Title: "raced in",
				Description: "changed mid-compile", RelatedInvariants: []string{"invariant.test.reload_validates"},
				Domain: testDomain,
			}})
		}},
	)
	var se *StaleManifestError
	if !errors.As(err, &se) || se.Phase != "after_compile" {
		t.Fatalf("err = %v, want StaleManifestError(after_compile)", err)
	}
}

// 6 — graph persisted but marker write fails is typed incomplete, never verified.
func TestGraphWriteOkMarkerFailIncomplete(t *testing.T) {
	root, m := seedRepo(t)
	// Make the marker path unwritable by occupying it with a directory.
	markerPath := filepath.Join(root, ".sensei", "graph-authority.json")
	if err := os.MkdirAll(markerPath, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Build(context.Background(), BuildRequest{RepositoryRoot: root, RepositoryDomain: testDomain, ExpectedManifestDigestSHA256: m})
	var pe *PersistError
	if !errors.As(err, &pe) || pe.Target != "marker" || !pe.GraphDurable {
		t.Fatalf("err = %v, want PersistError(marker, graph_durable)", err)
	}
	// The graph is durable, but the projection was never reported verified.
	if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(GraphRelPath))); statErr != nil {
		t.Fatal("graph should be durable after a marker failure")
	}
}

// 7 — tampered graph bytes fail reload verification.
func TestTamperedGraphFailsReload(t *testing.T) {
	root, m := seedRepo(t)
	buildOK(t, root, m)
	graphPath := filepath.Join(root, filepath.FromSlash(GraphRelPath))
	data, _ := os.ReadFile(graphPath)
	tampered := bytes.Replace(data, []byte("Reload validates before serving"), []byte("Reload TAMPERED before serving"), 1)
	if bytes.Equal(tampered, data) {
		t.Fatal("tamper had no effect (fixture changed)")
	}
	if err := os.WriteFile(graphPath, tampered, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := VerifyPersisted(context.Background(), root)
	var re *ReloadVerifyError
	if !errors.As(err, &re) {
		t.Fatalf("err = %v, want ReloadVerifyError", err)
	}
}

// 8 — tampered marker file fails reload verification.
func TestTamperedMarkerFailsReload(t *testing.T) {
	root, m := seedRepo(t)
	buildOK(t, root, m)
	markerPath := filepath.Join(root, ".sensei", "graph-authority.json")
	if err := os.WriteFile(markerPath, []byte(`{"digest_sha256":"deadbeef`+strings.Repeat("0", 56)+`","marker_iri":"x","triple_count":9}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := VerifyPersisted(context.Background(), root)
	var re *ReloadVerifyError
	if !errors.As(err, &re) {
		t.Fatalf("err = %v, want ReloadVerifyError", err)
	}
}

// 9 — malformed N-Triples fail verification.
func TestMalformedNTFailsReload(t *testing.T) {
	root, m := seedRepo(t)
	buildOK(t, root, m)
	graphPath := filepath.Join(root, filepath.FromSlash(GraphRelPath))
	if err := os.WriteFile(graphPath, []byte("this is not n-triples\n<a> <b>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := VerifyPersisted(context.Background(), root)
	var re *ReloadVerifyError
	if !errors.As(err, &re) {
		t.Fatalf("err = %v, want ReloadVerifyError", err)
	}
}

// 10 — offline adapter parity with graphsnapshot (the canonical NT interpretation
// the repo-scoped store consumers use): outbound, inbound, IRI, literal, set/dup,
// missing node.
func TestStoreAdapterParityWithGraphsnapshot(t *testing.T) {
	root, m := seedRepo(t)
	buildOK(t, root, m)
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(GraphRelPath)))
	if err != nil {
		t.Fatal(err)
	}
	triples, err := graphsnapshot.Read(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := ReadGraph(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Reference indexes from graphsnapshot, mirroring the store's inverse-edge rule.
	refOut := map[string][]string{}
	refIn := map[string][]string{}
	subjects := map[string]bool{}
	objects := map[string]bool{}
	for _, tr := range triples {
		subjects[tr.Subject] = true
		refOut[tr.Subject] = append(refOut[tr.Subject], tr.Predicate+"|"+tr.Object+"|"+boolStr(tr.ObjectIsIRI))
		if tr.ObjectIsIRI && tr.Predicate != rdfType {
			objects[tr.Object] = true
			refIn[tr.Object] = append(refIn[tr.Object], tr.Subject+"|"+tr.Predicate)
		}
	}
	if len(subjects) == 0 {
		t.Fatal("fixture produced no triples")
	}
	sawLiteral := false
	for s := range subjects {
		got, _ := adapter.Describe(ctx, s)
		var gotKeys []string
		for _, tr := range got {
			gotKeys = append(gotKeys, tr.Predicate+"|"+tr.Object+"|"+boolStr(tr.ObjectIsIRI))
			if !tr.ObjectIsIRI {
				sawLiteral = true
				if strings.HasPrefix(tr.Object, `"`) {
					t.Fatalf("literal object not unquoted: %q", tr.Object)
				}
			}
		}
		if !equalMultiset(gotKeys, refOut[s]) {
			t.Fatalf("Describe(%s) parity mismatch:\n got %v\n ref %v", s, gotKeys, refOut[s])
		}
	}
	if !sawLiteral {
		t.Fatal("fixture had no literal object to prove literal handling")
	}
	for o := range objects {
		in, _ := adapter.DescribeInbound(ctx, o)
		var gotKeys []string
		for _, it := range in {
			gotKeys = append(gotKeys, it.Subject+"|"+it.Predicate)
		}
		if !equalMultiset(gotKeys, refIn[o]) {
			t.Fatalf("DescribeInbound(%s) parity mismatch:\n got %v\n ref %v", o, gotKeys, refIn[o])
		}
	}
	// Missing node → empty, matching the store contract.
	if got, _ := adapter.Describe(ctx, "https://globular.io/awareness#invariant/does-not-exist"); len(got) != 0 {
		t.Fatal("missing node Describe should be empty")
	}
	if in, _ := adapter.DescribeInbound(ctx, "https://globular.io/awareness#does-not-exist"); len(in) != 0 {
		t.Fatal("missing node DescribeInbound should be empty")
	}
}

// 11 — the owner never mutates the combined embedded seed.
func TestNoEmbeddedSeedMutation(t *testing.T) {
	root, m := seedRepo(t)
	buildOK(t, root, m)
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(CombinedSeedRelPath))); err == nil {
		t.Fatal("owner wrote the combined embedded seed")
	}
}

// 12 — no promotion receipt/journal/CLI/cert/completion side effects, and the
// production package does not import cmd/awg.
func TestBoundaryNoLaterPhaseArtifactsOrCLIImport(t *testing.T) {
	root, m := seedRepo(t)
	buildOK(t, root, m)
	for _, forbidden := range []string{
		".sensei/project/promotions",
		".sensei/project/receipts",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(forbidden))); err == nil {
			t.Fatalf("owner produced a later-phase artifact: %s", forbidden)
		}
	}
	// Production source must not import cmd/awg and must not write the seed path.
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		if strings.Contains(string(data), "globulario/sensei/cmd/awg") {
			t.Errorf("%s imports cmd/awg", e.Name())
		}
		// The combined-seed path may only appear as the obligation constant, never in a write.
		src := string(data)
		if strings.Contains(src, "WriteFile") && strings.Contains(src, CombinedSeedRelPath) {
			t.Errorf("%s appears to write the combined seed path", e.Name())
		}
	}
}

func boolStr(b bool) string {
	if b {
		return "iri"
	}
	return "lit"
}

func equalMultiset(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, x := range a {
		m[x]++
	}
	for _, x := range b {
		m[x]--
		if m[x] < 0 {
			return false
		}
	}
	return true
}
