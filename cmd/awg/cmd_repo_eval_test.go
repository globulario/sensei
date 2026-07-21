// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/repoeval"
)

func TestCollectSeedStatsCountsActivePatternMisuses(t *testing.T) {
	nt := strings.Join([]string{
		`<https://globular.io/awareness#patternMisuse/pattern_misuse.cross_domain_leak> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#PatternMisuse> .`,
		`<https://globular.io/awareness#patternMisuse/pattern_misuse.cross_domain_leak> <http://www.w3.org/2000/01/rdf-schema#label> "Cross domain leak" .`,
		`<https://globular.io/awareness#patternMisuse/pattern_misuse.cross_domain_leak> <https://globular.io/awareness#status> "active" .`,
	}, "\n")

	stats := collectSeedStats([]byte(nt))
	if stats.patternMisuseCount != 1 {
		t.Fatalf("patternMisuseCount=%d want 1", stats.patternMisuseCount)
	}
	if got := stats.patternMisuseIDs[0]; !strings.Contains(got, "Cross domain leak") {
		t.Fatalf("patternMisuseIDs[0]=%q", got)
	}
}

func TestCollectSeedStatsIgnoresGuardrailPatternMisuses(t *testing.T) {
	nt := strings.Join([]string{
		`<https://globular.io/awareness#patternMisuse/pattern_misuse.ui_direct_promotion> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#PatternMisuse> .`,
		`<https://globular.io/awareness#patternMisuse/pattern_misuse.ui_direct_promotion> <http://www.w3.org/2000/01/rdf-schema#label> "UI direct promotion" .`,
		`<https://globular.io/awareness#patternMisuse/pattern_misuse.ui_direct_promotion> <https://globular.io/awareness#status> "guardrail" .`,
	}, "\n")

	stats := collectSeedStats([]byte(nt))
	if stats.patternMisuseCount != 0 {
		t.Fatalf("patternMisuseCount=%d want 0 ids=%v", stats.patternMisuseCount, stats.patternMisuseIDs)
	}
}

func TestResolveRepoEvalTargetAcceptsRootGoModule(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/rootmod\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	target, err := resolveRepoEvalTarget(root, "", "")
	if err != nil {
		t.Fatalf("resolveRepoEvalTarget: %v", err)
	}
	if target.root != root {
		t.Fatalf("root = %q, want %q", target.root, root)
	}
	if target.kind != "generic" {
		t.Fatalf("kind = %q, want generic", target.kind)
	}
	if target.intentDir != filepath.Join(root, "docs", "intent") {
		t.Fatalf("intentDir = %q", target.intentDir)
	}
}

func TestResolveRepoEvalTargetAcceptsAwarenessOnlyRepo(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}

	target, err := resolveRepoEvalTarget(root, "", "")
	if err != nil {
		t.Fatalf("resolveRepoEvalTarget: %v", err)
	}
	if target.root != root {
		t.Fatalf("root = %q, want %q", target.root, root)
	}
	if target.kind != "generic" {
		t.Fatalf("kind = %q, want generic", target.kind)
	}
}

func TestRepoEvalInputDirsUsesGenericTargetAwareness(t *testing.T) {
	root := t.TempDir()
	awarenessDir := filepath.Join(root, "docs", "awareness")
	generatedDir := filepath.Join(awarenessDir, "generated")
	intentDir := filepath.Join(root, "docs", "intent")
	for _, dir := range []string{awarenessDir, generatedDir, intentDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	target := repoEvalTarget{root: root, intentDir: intentDir, kind: "generic"}
	inputDirs, gotIntentDir, err := repoEvalInputDirs(target, "/unused/services", "/unused/awareness-graph")
	if err != nil {
		t.Fatalf("repoEvalInputDirs: %v", err)
	}
	if gotIntentDir != intentDir {
		t.Fatalf("intentDir = %q, want %q", gotIntentDir, intentDir)
	}
	if len(inputDirs) != 2 {
		t.Fatalf("len(inputDirs) = %d, want 2: %#v", len(inputDirs), inputDirs)
	}
	if inputDirs[0] != awarenessDir || inputDirs[1] != generatedDir {
		t.Fatalf("inputDirs = %#v", inputDirs)
	}
}

func TestWalkRepoGoFiles_ScansRepoRootNotOnlyGolangDir(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		"cmd/app/main.go":     "package main\n",
		"internal/lib/lib.go": "package lib\n",
		"vendor/skip/skip.go": "package skip\n",
		".git/skip/skip.go":   "package skip\n",
		"README.md":           "# readme\n",
	} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := walkRepoGoFiles(root)
	if err != nil {
		t.Fatalf("walkRepoGoFiles: %v", err)
	}
	want := []string{
		"cmd/app/main.go",
		"internal/lib/lib.go",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("walkRepoGoFiles=\n%s\nwant=\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestRepoEvalGoSourceRootAcceptsRootGoModule(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/rootmod\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := repoEvalGoSourceRoot(root)
	if err != nil {
		t.Fatalf("repoEvalGoSourceRoot: %v", err)
	}
	if got != root {
		t.Fatalf("source root = %q, want %q", got, root)
	}
}

func TestRepoEvalGoSourceRootPrefersGolangLayout(t *testing.T) {
	root := t.TempDir()
	golangRoot := filepath.Join(root, "golang")
	if err := os.MkdirAll(golangRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/rootmod\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := repoEvalGoSourceRoot(root)
	if err != nil {
		t.Fatalf("repoEvalGoSourceRoot: %v", err)
	}
	if got != golangRoot {
		t.Fatalf("source root = %q, want %q", got, golangRoot)
	}
}

func TestBuildRepoCoverageReportTreatsAwarenessOnlyRepoAsNotApplicable(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}

	report, err := buildRepoCoverageReport(root, nil)
	if err != nil {
		t.Fatalf("buildRepoCoverageReport: %v", err)
	}
	if report.WeightedOverallPercent != 100 {
		t.Fatalf("WeightedOverallPercent = %d, want 100", report.WeightedOverallPercent)
	}
	if report.TotalFiles != 0 {
		t.Fatalf("TotalFiles = %d, want 0", report.TotalFiles)
	}
}

func TestGenerateNTWithOwnershipDoesNotApplyServicesFilterForGenericRepo(t *testing.T) {
	root := t.TempDir()
	generatedDir := filepath.Join(root, "docs", "awareness", "generated")
	if err := os.MkdirAll(generatedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`
code_symbols:
  - id: generated.symbol
    language: go
    kind: function
    defined_in: cmd/example/main.go
`)
	if err := os.WriteFile(filepath.Join(generatedDir, "awareness_graph_code_symbols.yaml"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	genericNT, _, _, err := generateNTWithOwnership([]string{generatedDir}, "", []string{root}, "", nil, "")
	if err != nil {
		t.Fatalf("generate generic NT: %v", err)
	}
	if !strings.Contains(string(genericNT), "generated.symbol") {
		t.Fatalf("generic repo generation filtered target-owned generated file:\n%s", string(genericNT))
	}

	servicesNT, _, _, err := generateNTWithOwnership([]string{generatedDir}, "", []string{root}, root, nil, "")
	if err != nil {
		t.Fatalf("generate services NT: %v", err)
	}
	if strings.Contains(string(servicesNT), "generated.symbol") {
		t.Fatalf("services ownership filter did not exclude awareness_graph_* file:\n%s", string(servicesNT))
	}
}

func TestRepoEvalIntegrityIssues_MapsAuditChecks(t *testing.T) {
	got := repoEvalIntegrityIssues([]auditResult{
		{name: "yaml-validity", level: auditFAIL, summary: "2 invalid", details: []string{"a.yaml", "b.yaml"}},
		{name: "stale-file-refs", level: auditWARN, summary: "1 missing", details: []string{"missing.go"}},
		{name: "ntriples-validity", level: auditPASS, summary: "ok"},
	})
	want := []repoeval.IntegrityIssue{
		{Check: "yaml-validity", Severity: "fail", Summary: "2 invalid", Evidence: []string{"a.yaml", "b.yaml"}},
		{Check: "stale-file-refs", Severity: "warn", Summary: "1 missing", Evidence: []string{"missing.go"}},
	}
	if len(got) != len(want) {
		t.Fatalf("issues=%d want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Check != want[i].Check || got[i].Severity != want[i].Severity || got[i].Summary != want[i].Summary {
			t.Fatalf("issue[%d]=%+v want %+v", i, got[i], want[i])
		}
		if strings.Join(got[i].Evidence, "\n") != strings.Join(want[i].Evidence, "\n") {
			t.Fatalf("issue[%d].evidence=%v want %v", i, got[i].Evidence, want[i].Evidence)
		}
	}
}

func TestCollectRepoEvalUpgradePath_FallsBackToLoadBearingComponents(t *testing.T) {
	root := t.TempDir()
	writeRepoEvalFile := func(rel, content string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeRepoEvalFile("docs/awareness/high_risk_files.yaml", "files: []\n")
	writeRepoEvalFile("docs/awareness/generated/components.yaml", `components:
  - id: component.cmd.caddy
    name: caddy
    kind: service
    source_files:
      - cmd/caddy/main.go
    depends_on:
      - component.modules.caddyhttp
  - id: component.modules.caddyhttp
    name: caddyhttp
    kind: module
    source_files:
      - modules/caddyhttp/app.go
      - modules/caddyhttp/server.go
      - modules/caddyhttp/routes.go
    depends_on:
      - component.modules.caddytls
  - id: component.internal.metrics
    name: metrics
    kind: module
    source_files:
      - internal/metrics/metrics.go
`)

	path, err := collectRepoEvalUpgradePath(root, "")
	if err != nil {
		t.Fatalf("collectRepoEvalUpgradePath: %v", err)
	}
	if len(path.Invariants) != 3 || len(path.Contracts) != 3 {
		t.Fatalf("upgrade path sizes = %d/%d want 3/3: %+v", len(path.Invariants), len(path.Contracts), path)
	}
	if path.Invariants[0].ID != "invariant.cmd.caddy" {
		t.Fatalf("first invariant candidate=%q want invariant.cmd.caddy", path.Invariants[0].ID)
	}
	if path.Contracts[0].ID != "component.cmd.caddy" {
		t.Fatalf("first contract candidate=%q want component.cmd.caddy", path.Contracts[0].ID)
	}
	if path.Contracts[0].SuggestedFile != filepath.Join("docs", "intent", "component.cmd.caddy.yaml") {
		t.Fatalf("suggested file=%q", path.Contracts[0].SuggestedFile)
	}
}

func TestCollectRepoEvalUpgradePath_FallsBackToExistingInvariantSurfaces(t *testing.T) {
	root := t.TempDir()
	writeRepoEvalFile := func(rel, content string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeRepoEvalFile("docs/awareness/invariants.yaml", `invariants:
  - id: join.token.validated.before.phase
    severity: critical
    protects:
      files:
        - gateway_server/
        - cmd/gateway/main.go
  - id: xds.mtls.required.in.production
    severity: critical
    protects:
      files:
        - xds/
        - cmd/xds/main.go
  - id: etcd.ghost.cleared.before.member.add
    severity: high
    protects:
      files:
        - internal/server/
        - gateway_server/
`)

	path, err := collectRepoEvalUpgradePath(root, "")
	if err != nil {
		t.Fatalf("collectRepoEvalUpgradePath: %v", err)
	}
	if len(path.Contracts) == 0 {
		t.Fatalf("expected fallback contract candidates, got none: %+v", path)
	}
	if path.Contracts[0].ID != "component.gateway_server" {
		t.Fatalf("first fallback contract=%q want component.gateway_server", path.Contracts[0].ID)
	}
	if path.Contracts[0].SuggestedFile != filepath.Join("docs", "intent", "component.gateway_server.yaml") {
		t.Fatalf("suggested file=%q", path.Contracts[0].SuggestedFile)
	}
	if len(path.Invariants) == 0 || path.Invariants[0].ID != "invariant.gateway_server" {
		t.Fatalf("first fallback invariant=%q want invariant.gateway_server", path.Invariants[0].ID)
	}
}

func TestCollectRepoEvalUpgradePath_SkipsHighRiskPathsAlreadyGovernedByInvariant(t *testing.T) {
	root := t.TempDir()
	writeRepoEvalFile := func(rel, content string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeRepoEvalFile("docs/awareness/high_risk_files.yaml", `files:
  - cmd/xds/
  - internal/config/
`)
	writeRepoEvalFile("docs/awareness/generated/components.yaml", `components:
  - id: component.cmd.xds
    name: xds
    kind: service
    source_files:
      - cmd/xds/main.go
  - id: component.internal.config
    name: config
    kind: module
    source_files:
      - internal/config/runtime.go
`)
	writeRepoEvalFile("docs/awareness/invariants.yaml", `invariants:
  - id: xds.authority.must.flow.through.controller
    severity: critical
    protects:
      files:
        - cmd/xds/
        - internal/xds/
`)

	path, err := collectRepoEvalUpgradePath(root, "")
	if err != nil {
		t.Fatalf("collectRepoEvalUpgradePath: %v", err)
	}
	if len(path.Invariants) == 0 {
		t.Fatalf("expected invariant candidates, got none")
	}
	for _, candidate := range path.Invariants {
		if len(candidate.Paths) == 1 && candidate.Paths[0] == "cmd/xds/" {
			t.Fatalf("unexpected governed high-risk candidate still present: %+v", candidate)
		}
	}
	if path.Invariants[0].Paths[0] != "internal/config/" {
		t.Fatalf("first remaining invariant path=%q want internal/config/", path.Invariants[0].Paths[0])
	}
}

func TestCollectRepoEvalUpgradePath_SkipsComponentsAlreadyGovernedByInvariant(t *testing.T) {
	root := t.TempDir()
	writeRepoEvalFile := func(rel, content string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeRepoEvalFile("docs/awareness/high_risk_files.yaml", "files: []\n")
	writeRepoEvalFile("docs/awareness/generated/components.yaml", `components:
  - id: component.cmd.gateway
    name: gateway
    kind: service
    source_files:
      - cmd/gateway/main.go
      - cmd/gateway/faststart.go
  - id: component.cmd.xds
    name: xds
    kind: service
    source_files:
      - cmd/xds/main.go
`)
	writeRepoEvalFile("docs/awareness/invariants.yaml", `invariants:
  - id: gateway.entrypoint.network.identity.must.remain.controller_managed
    severity: critical
    protects:
      files:
        - cmd/gateway/
`)

	path, err := collectRepoEvalUpgradePath(root, "")
	if err != nil {
		t.Fatalf("collectRepoEvalUpgradePath: %v", err)
	}
	for _, candidate := range path.Invariants {
		if candidate.ID == "invariant.cmd.gateway" {
			t.Fatalf("unexpected governed component candidate still present: %+v", candidate)
		}
	}
	if len(path.Invariants) == 0 || path.Invariants[0].ID != "invariant.cmd.xds" {
		t.Fatalf("remaining invariant candidate=%q want invariant.cmd.xds", path.Invariants[0].ID)
	}
}
