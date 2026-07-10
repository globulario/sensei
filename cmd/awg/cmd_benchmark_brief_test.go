// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func TestBuildBenchmarkBrief_CollectsScopedContextAndGaps(t *testing.T) {
	prevAtomic := benchmarkAtomicGuard
	benchmarkAtomicGuard = func(string, string) error { return nil }
	defer func() { benchmarkAtomicGuard = prevAtomic }()

	root := t.TempDir()
	mkdirAll := func(rel string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, rel)
		mkdirAll(filepath.Dir(rel))
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
`)
	write("docs/awareness/invariants.yaml", `invariants:
  - id: awareness.query.no_arbitrary_sparql
    protects:
      files:
        - golang/server/query.go
    required_tests:
      - golang/server/main_test.go:TestQuery_BackendErrorReturnsUnavailable
    forbidden_fixes:
      - reenable_raw_sparql
`)
	write("docs/awareness/failure_modes.yaml", `failure_modes:
  - id: awareness.raw_sparql_exposed_to_agent
    protects:
      files:
        - golang/server/query.go
    required_tests:
      - golang/server/main_test.go:TestQuery_BackendErrorReturnsUnavailable
    forbidden_fixes:
      - trust_caller_sparql
`)
	write("docs/awareness/required_tests.yaml", `required_tests:
  - id: golang/server/main_test.go:TestQuery_BackendErrorReturnsUnavailable
    protects:
      files:
        - golang/server/query.go
`)
	write("docs/awareness/forbidden_fixes.yaml", `forbidden_fixes:
  - id: inline_raw_sparql_passthrough
    protects:
      files:
        - golang/server/query.go
`)
	write("docs/intent/awareness.query_does_not_expose_arbitrary_sparql.yaml", `id: awareness.query_does_not_expose_arbitrary_sparql
level: mechanism
status: active
expressed_by:
  - golang/server/query.go
  - docs/awareness/missing.yaml
required_tests:
  - golang/server/main_test.go:TestQuery_BackendErrorReturnsUnavailable
`)

	res, err := buildBenchmarkBrief(root, benchmarkBriefTask{
		Issue:    "Query must not expose raw sparql passthrough",
		F2PTests: []string{"TestQuery_BackendErrorReturnsUnavailable"},
		Files:    []string{"golang/server/query.go"},
	}, "test")
	if err != nil {
		t.Fatalf("buildBenchmarkBrief: %v", err)
	}
	if len(res.LikelyImplementationFiles) == 0 || res.LikelyImplementationFiles[0] != "golang/server/query.go" {
		t.Fatalf("likely implementation files = %+v", res.LikelyImplementationFiles)
	}
	if len(res.AWGFiles) == 0 {
		t.Fatalf("expected scoped AWG context, got none")
	}
	entry := res.AWGFiles[0]
	if entry.File != "golang/server/query.go" {
		t.Fatalf("file entry = %+v", entry)
	}
	for _, want := range []string{
		"awareness.query.no_arbitrary_sparql",
		"awareness.raw_sparql_exposed_to_agent",
		"awareness.query_does_not_expose_arbitrary_sparql",
		"golang/server/main_test.go:TestQuery_BackendErrorReturnsUnavailable",
		"inline_raw_sparql_passthrough",
	} {
		all := strings.Join(append(append(append(append(entry.Invariants, entry.FailureModes...), entry.Intents...), entry.RequiredTests...), entry.ForbiddenFixes...), ",")
		if !strings.Contains(all, want) {
			t.Fatalf("scoped entry missing %q: %+v", want, entry)
		}
	}
	if len(res.AuthorityGaps) == 0 || !strings.Contains(res.AuthorityGaps[0], "awareness.query_does_not_expose_arbitrary_sparql") {
		t.Fatalf("authority gaps = %+v", res.AuthorityGaps)
	}
	if len(res.EvidenceGaps) == 0 || !strings.Contains(res.EvidenceGaps[0], "awareness.query.no_arbitrary_sparql") {
		t.Fatalf("evidence gaps = %+v", res.EvidenceGaps)
	}
}

func TestRunBenchmarkBrief_AttachesAuthoritativeRepairPlan(t *testing.T) {
	root := t.TempDir()
	writeBenchmarkBriefFixtures(t, root)

	prevAtomic := benchmarkAtomicGuard
	benchmarkAtomicGuard = func(string, string) error { return nil }
	defer func() { benchmarkAtomicGuard = prevAtomic }()

	prev := repairPlanPreflight
	repairPlanPreflight = func(_ context.Context, _ string, req *awarenesspb.PreflightRequest) (*awarenesspb.PreflightResponse, error) {
		if got := strings.Join(req.GetFiles(), ","); got != "golang/server/query.go" {
			t.Fatalf("files=%q", got)
		}
		return &awarenesspb.PreflightResponse{
			Status:     awarenesspb.PreflightStatus_PREFLIGHT_STATUS_OK,
			RiskClass:  awarenesspb.RiskClass_ARCHITECTURE_SENSITIVE,
			Confidence: awarenesspb.Confidence_CONFIDENCE_HIGH,
			RequiredActions: []string{
				"repair_plan:globular.repair.query_authority",
			},
			FilesToRead: []string{"docs/design.md"},
			TestsToRun:  []string{"go test ./golang/server -run TestQuery_BackendErrorReturnsUnavailable"},
			Authority: &awarenesspb.GraphAuthority{
				Authoritative:              true,
				GraphFreshnessState:        awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
				LiveStoreGraphDigestSha256: "briefdigest",
				LiveStoreGraphTripleCount:  88,
			},
		}, nil
	}
	defer func() { repairPlanPreflight = prev }()

	code, out, errOut := captureStdoutStderr(t, func() int {
		return runBenchmarkBrief([]string{
			"--repo-root", root,
			"--issue", "Query must not expose raw sparql passthrough",
			"--file", "golang/server/query.go",
			"--f2p-test", "TestQuery_BackendErrorReturnsUnavailable",
			"--format", "json",
		})
	})
	if code != 0 {
		t.Fatalf("runBenchmarkBrief exit=%d stderr=%q", code, errOut)
	}
	var got benchmarkBriefResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal json: %v\n%s", err, out)
	}
	if got.RepairPlan == nil {
		t.Fatal("expected repair plan in benchmark brief")
	}
	if got.RepairPlan.Authority == nil || !got.RepairPlan.Authority.GetAuthoritative() {
		t.Fatalf("repair plan authority = %+v", got.RepairPlan.Authority)
	}
	if len(got.RepairPlan.OrderedSteps) == 0 {
		t.Fatalf("ordered steps missing: %+v", got.RepairPlan)
	}
}

func TestRunBenchmarkBrief_FailsClosedOnStaleAuthority(t *testing.T) {
	root := t.TempDir()
	writeBenchmarkBriefFixtures(t, root)

	prevAtomic := benchmarkAtomicGuard
	benchmarkAtomicGuard = func(string, string) error { return nil }
	defer func() { benchmarkAtomicGuard = prevAtomic }()

	prev := repairPlanPreflight
	repairPlanPreflight = func(context.Context, string, *awarenesspb.PreflightRequest) (*awarenesspb.PreflightResponse, error) {
		return &awarenesspb.PreflightResponse{
			Authority: &awarenesspb.GraphAuthority{
				Authoritative:       false,
				GraphFreshnessState: awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_STALE,
			},
		}, nil
	}
	defer func() { repairPlanPreflight = prev }()

	code, _, errOut := captureStdoutStderr(t, func() int {
		return runBenchmarkBrief([]string{
			"--repo-root", root,
			"--issue", "Query must not expose raw sparql passthrough",
			"--file", "golang/server/query.go",
		})
	})
	if code == 0 {
		t.Fatal("expected stale authority to fail benchmark-brief")
	}
	if !strings.Contains(errOut, "requires current graph authority") {
		t.Fatalf("stderr missing stale authority refusal:\n%s", errOut)
	}
}

func writeBenchmarkBriefFixtures(t *testing.T, root string) {
	t.Helper()
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
func Query() {}
`)
	write("docs/awareness/invariants.yaml", `invariants:
  - id: awareness.query.no_arbitrary_sparql
    protects:
      files:
        - golang/server/query.go
    required_tests:
      - golang/server/main_test.go:TestQuery_BackendErrorReturnsUnavailable
    forbidden_fixes:
      - reenable_raw_sparql
`)
	write("docs/awareness/forbidden_fixes.yaml", `forbidden_fixes:
  - id: inline_raw_sparql_passthrough
    protects:
      files:
        - golang/server/query.go
`)
	write("docs/intent/awareness.query_does_not_expose_arbitrary_sparql.yaml", `id: awareness.query_does_not_expose_arbitrary_sparql
level: mechanism
status: active
expressed_by:
  - golang/server/query.go
required_tests:
  - golang/server/main_test.go:TestQuery_BackendErrorReturnsUnavailable
`)
	write("docs/awareness/candidates/authority_surface_candidates.yaml", `authority_surface_candidates:
  candidates:
    - id: candidate.authority.query.surface
      class: AuthoritySurface
      status: candidate
      confidence: candidate
      kind: guarded_mutation_handler
      owner: demo
      source_files:
        - golang/server/query.go
`)
	write("docs/awareness/generated/proof_obligations.yaml", `proof_obligations:
  - id: proof.authority.query.surface
    derived_from_authority_surface: candidate.authority.query.surface
    applies_to_authority_surfaces:
      - candidate.authority.query.surface
    evidence_lane: static_required
    required_slots:
      - id: slot.authority.query.surface.static_guard
        kind: static_guard
        required: true
`)
	write("docs/awareness/architecture/forbidden_fixes.yaml", `forbidden_fixes:
  - id: remove_query_guard
    protects:
      files:
        - golang/server/query.go
`)
}
