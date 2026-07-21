// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture/graphbuild"
	"github.com/globulario/sensei/golang/seedmeta"
)

const migInvariants = `invariants:
  - id: test.mig.one
    title: Migration invariant one
    severity: high
    status: active
    protects:
      files:
        - golang/one.go
  - id: test.mig.two
    title: Migration invariant two
    severity: critical
    status: active
    protects:
      files:
        - golang/two.go
`

func migCorpus(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "docs", "awareness")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "invariants.yaml"), []byte(migInvariants), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// The rebuild triple count is now the true artifact total (graph + 6 marker
// triples), matching the embedded marker — not the historic "+5" undercount.
func TestRebuildCountIsMarkerCountNotOffByOne(t *testing.T) {
	root := migCorpus(t)
	dir := filepath.Join(root, "docs", "awareness")
	nt, count, _, err := generateNTWithOwnership([]string{dir}, "", []string{root}, "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	marker, ok := seedmeta.ParseMarker(nt)
	if !ok {
		t.Fatal("generated graph carries no marker")
	}
	if count != int(marker.TripleCount) {
		t.Fatalf("returned count %d != marker triple count %d (the old +5 bug returned count-1)", count, marker.TripleCount)
	}
	if count == int(marker.TripleCount)-1 {
		t.Fatal("regression: rebuild count is off-by-one (the old +5)")
	}
}

// The CLI build helpers (compile + finalize) delegate to graphbuild and produce
// byte-identical output to graphbuild.Build for the same sources.
func TestBuildHelpersDelegateToGraphbuild(t *testing.T) {
	root := migCorpus(t)
	dir := filepath.Join(root, "docs", "awareness")

	raw, _, err := compileAwarenessInputs([]string{dir}, "", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	cliNT, _, _, _ := finalizeBuildArtifact(raw)

	art, err := graphbuild.Build(context.Background(), graphbuild.CompileRequest{
		Sources: []graphbuild.SourceRoot{{FilesystemPath: dir, IdentityRoot: dir}},
		Policy:  graphbuild.ValidationPolicy{},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(cliNT, art.NTriples) {
		t.Fatal("CLI build path is not byte-identical to graphbuild.Build")
	}
}
