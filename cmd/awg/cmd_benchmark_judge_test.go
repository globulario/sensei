// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildBenchmarkJudge_FlagsMissingRequiredTestsAndEvidenceGaps(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("golang/server/query.go", `package server

// @awareness enforces=globular.awareness_graph:invariant.awareness.query.no_arbitrary_sparql
// @awareness tested_by=golang/server/query_test.go:TestQueryUnknownMode
func Query() {}
`)
	write("golang/server/main_test.go", `package server

import "testing"

func TestQuery_BackendErrorReturnsUnavailable(t *testing.T) {}
func TestQuery_RawSPARQLLikeInputRejected(t *testing.T) {}
`)
	write("docs/awareness/invariants.yaml", `invariants:
  - id: awareness.query.no_arbitrary_sparql
    protects:
      files:
        - golang/server/query.go
    required_tests:
      - golang/server/main_test.go:TestQuery_RawSPARQLLikeInputRejected
    forbidden_fixes:
      - reenable_raw_sparql
`)
	write("docs/awareness/required_tests.yaml", `required_tests:
  - id: golang/server/main_test.go:TestQuery_RawSPARQLLikeInputRejected
    protects:
      files:
        - golang/server/query.go
`)

	res, err := buildBenchmarkJudge(root, benchmarkBriefTask{
		Issue:    "Query must not expose raw sparql passthrough",
		F2PTests: []string{"TestQuery_BackendErrorReturnsUnavailable"},
		Files:    []string{"golang/server/query.go"},
	}, []string{"golang/server/main_test.go:TestQuery_BackendErrorReturnsUnavailable"})
	if err != nil {
		t.Fatalf("buildBenchmarkJudge: %v", err)
	}
	if res.TestDiscipline != "insufficient" {
		t.Fatalf("test discipline = %q", res.TestDiscipline)
	}
	if len(res.MissingRequiredTests) != 1 || res.MissingRequiredTests[0] != "golang/server/main_test.go:TestQuery_RawSPARQLLikeInputRejected" {
		t.Fatalf("missing required tests = %+v", res.MissingRequiredTests)
	}
	if res.ContractPreservation != "review_required" {
		t.Fatalf("contract preservation = %q", res.ContractPreservation)
	}
	if len(res.EvidenceGaps) == 0 || !strings.Contains(res.EvidenceGaps[0], "awareness.query.no_arbitrary_sparql") {
		t.Fatalf("evidence gaps = %+v", res.EvidenceGaps)
	}
	if len(res.ForbiddenFixes) != 1 || res.ForbiddenFixes[0] != "reenable_raw_sparql" {
		t.Fatalf("forbidden fixes = %+v", res.ForbiddenFixes)
	}
}

func TestBenchmarkMissingRequiredTests_AllowsFileOrSymbolCoverage(t *testing.T) {
	required := []string{
		"golang/server/main_test.go:TestOne",
		"golang/server/main_test.go:TestTwo",
	}
	if got := benchmarkMissingRequiredTests(required, []string{"golang/server/main_test.go"}); len(got) != 0 {
		t.Fatalf("file-level run should satisfy both tests, got %+v", got)
	}
	if got := benchmarkMissingRequiredTests(required, []string{"TestOne"}); len(got) != 1 || got[0] != "golang/server/main_test.go:TestTwo" {
		t.Fatalf("symbol-level run mismatch: %+v", got)
	}
}
