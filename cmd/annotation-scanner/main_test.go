// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness-graph/golang/scanner"
)

// TestAnnotationScanner_Deterministic verifies that scanning the same source
// tree twice produces byte-identical output — a prerequisite for the CI
// freshness check to be reliable across different environments and run orders.
func TestAnnotationScanner_Deterministic(t *testing.T) {
	// Locate the repo root relative to this test file.
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	registryPath := filepath.Join(repoRoot, "..", "services", "docs", "awareness", "namespaces.yaml")
	if _, err := os.Stat(registryPath); err != nil {
		t.Skipf("services repo not found at %s (set SERVICES_REPO or clone as sibling): %v", registryPath, err)
	}

	reg, err := scanner.LoadRegistry(registryPath)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}

	srcPath := filepath.Join(repoRoot, "golang")
	sc := &scanner.Scanner{Registry: reg, RepoRoot: repoRoot, Strict: true}

	run := func() ([]scanner.CodeSymbol, []scanner.CodeEdge, []scanner.TestSymbol) {
		t.Helper()
		result, err := sc.Scan(srcPath)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if result.HasErrors() {
			for _, e := range result.Errors {
				t.Logf("scan error: %s", e.Message)
			}
			t.Fatal("scan produced errors in strict mode")
		}
		syms, edges, tests := scanner.BuildSymbolsAndEdges(result)
		return syms, edges, tests
	}

	syms1, edges1, tests1 := run()
	syms2, edges2, tests2 := run()

	// Serialize both runs to YAML bytes and compare.
	marshal := func(syms []scanner.CodeSymbol, edges []scanner.CodeEdge, tests []scanner.TestSymbol) []byte {
		var buf bytes.Buffer
		if err := scanner.WriteSymbolsYAML(&buf, syms, tests); err != nil {
			t.Fatalf("serialize symbols: %v", err)
		}
		if err := scanner.WriteEdgesYAML(&buf, edges); err != nil {
			t.Fatalf("serialize edges: %v", err)
		}
		return buf.Bytes()
	}

	b1 := marshal(syms1, edges1, tests1)
	b2 := marshal(syms2, edges2, tests2)

	if !bytes.Equal(b1, b2) {
		t.Fatal("annotation scanner output is non-deterministic: two runs on identical source produced different YAML")
	}
	if len(b1) == 0 {
		t.Fatal("scanner produced empty output — no annotations found")
	}
}
