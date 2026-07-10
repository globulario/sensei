// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectAnnotationInvariantTests_FindsExplicitEvidence(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "golang", "server")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	code := `package server

// @awareness enforces=globular.awareness_graph:invariant.awareness.store_unavailable_explicit
// @awareness tested_by=golang/server/main_test.go:TestBriefingStoreNil
func Briefing() {}
`
	if err := os.WriteFile(filepath.Join(src, "briefing.go"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	testCode := `package server

import "testing"

func TestBriefingStoreNil(t *testing.T) {}
`
	if err := os.WriteFile(filepath.Join(src, "main_test.go"), []byte(testCode), 0o644); err != nil {
		t.Fatal(err)
	}

	got, proposals, err := collectAnnotationInvariantTests(root)
	if err != nil {
		t.Fatalf("collectAnnotationInvariantTests: %v", err)
	}
	if len(proposals) != 0 {
		t.Fatalf("unexpected proposals: %+v", proposals)
	}
	ev, ok := got["awareness.store_unavailable_explicit"]
	if !ok {
		t.Fatalf("missing invariant evidence: %+v", got)
	}
	if !ev.tests["golang/server/main_test.go:TestBriefingStoreNil"] {
		t.Fatalf("missing test evidence: %+v", ev.tests)
	}
}

func TestCollectAnnotationInvariantTests_IgnoresGoStringLiterals(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "golang", "server")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	code := `package server

func fixture() string {
	return "// @awareness enforces=globular.awareness_graph:invariant.awareness.store_unavailable_explicit\n// @awareness tested_by=golang/server/main_test.go:TestBriefingStoreNil"
}
`
	if err := os.WriteFile(filepath.Join(src, "fixture.go"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}

	got, proposals, err := collectAnnotationInvariantTests(root)
	if err != nil {
		t.Fatalf("collectAnnotationInvariantTests: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("string-literal pseudo-annotations must be ignored: %+v", got)
	}
	if len(proposals) != 0 {
		t.Fatalf("unexpected proposals: %+v", proposals)
	}
}

func TestApplyInvariantRequiredTests_AddsMissingRequiredTests(t *testing.T) {
	invariantsRaw := []byte(`invariants:
  - id: awareness.store_unavailable_explicit
    title: Store failures must be explicit
    severity: critical
    status: active
`)
	requiredTestsRaw := []byte("required_tests:\n")
	evidence := map[string]invariantTestEvidence{
		"awareness.store_unavailable_explicit": {
			tests: map[string]bool{
				"golang/server/main_test.go:TestBriefingStoreNil": true,
				"golang/server/impact_test.go:TestImpactStoreNil": true,
			},
			evidence: map[string]bool{
				"golang/server/briefing.go:10": true,
				"golang/server/impact.go:20":   true,
			},
			filesByTest: map[string]map[string]bool{
				"golang/server/main_test.go:TestBriefingStoreNil": {
					"golang/server/briefing.go":  true,
					"golang/server/main_test.go": true,
				},
				"golang/server/impact_test.go:TestImpactStoreNil": {
					"golang/server/impact.go":      true,
					"golang/server/impact_test.go": true,
				},
			},
		},
	}

	updatedInvariants, updatedRequiredTests, candidates, applied, err := applyInvariantRequiredTests(invariantsRaw, requiredTestsRaw, evidence)
	if err != nil {
		t.Fatalf("applyInvariantRequiredTests: %v", err)
	}
	if applied != 1 {
		t.Fatalf("applied=%d, want 1", applied)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates=%d, want 1", len(candidates))
	}
	text := string(updatedInvariants)
	for _, want := range []string{
		"required_tests:",
		"- golang/server/impact_test.go:TestImpactStoreNil",
		"- golang/server/main_test.go:TestBriefingStoreNil",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("updated yaml missing %q:\n%s", want, text)
		}
	}
	requiredTestsText := string(updatedRequiredTests)
	for _, want := range []string{
		"id: golang/server/impact_test.go:TestImpactStoreNil",
		"title: TestImpactStoreNil",
		"- awareness.store_unavailable_explicit",
		"- golang/server/impact.go",
		"- golang/server/impact_test.go",
	} {
		if !strings.Contains(requiredTestsText, want) {
			t.Fatalf("updated required_tests yaml missing %q:\n%s", want, requiredTestsText)
		}
	}
}

func TestCollectAnnotationInvariantTests_EmitsProposalForUndiscoveredTest(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "golang", "server")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	code := `package server

// @awareness enforces=globular.awareness_graph:invariant.awareness.store_unavailable_explicit
// @awareness tested_by=golang/server/missing_test.go:TestMissing
func Briefing() {}
`
	if err := os.WriteFile(filepath.Join(src, "briefing.go"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	testCode := `package server

import "testing"

func TestBriefingStoreStateNil(t *testing.T) {}
func TestBriefingNilState(t *testing.T) {}
`
	if err := os.WriteFile(filepath.Join(src, "briefing_test.go"), []byte(testCode), 0o644); err != nil {
		t.Fatal(err)
	}

	got, proposals, err := collectAnnotationInvariantTests(root)
	if err != nil {
		t.Fatalf("collectAnnotationInvariantTests: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("unexpected safe evidence: %+v", got)
	}
	if len(proposals) != 1 {
		t.Fatalf("proposals=%d, want 1", len(proposals))
	}
	if proposals[0].InvariantID != "awareness.store_unavailable_explicit" {
		t.Fatalf("proposal invariant = %q", proposals[0].InvariantID)
	}
	if len(proposals[0].Tests) != 1 || proposals[0].Tests[0] != "golang/server/missing_test.go:TestMissing" {
		t.Fatalf("proposal tests = %+v", proposals[0].Tests)
	}
	if len(proposals[0].ReplacementSuggestions) == 0 {
		t.Fatalf("expected replacement suggestions, got none: %+v", proposals[0])
	}
	if proposals[0].ReplacementSuggestions[0] != "golang/server/briefing_test.go:TestBriefingNilState" &&
		proposals[0].ReplacementSuggestions[0] != "golang/server/briefing_test.go:TestBriefingStoreStateNil" {
		t.Fatalf("unexpected top replacement suggestion: %+v", proposals[0].ReplacementSuggestions)
	}
}

func TestPopulateProposalSnippets(t *testing.T) {
	p := repoEvalFixProposal{
		InvariantID: "awareness.store_unavailable_explicit",
		Tests:       []string{"golang/server/main_test.go:TestBriefingStoreNil"},
		ReplacementSuggestions: []string{
			"golang/server/briefing_test.go:TestBriefingNilState",
			"golang/server/briefing_test.go:TestBriefingStoreStateNil",
			"golang/server/main_test.go:TestQuery_BackendErrorReturnsUnavailable",
			"golang/server/main_test.go:TestResolve_FoundMapsCoreFields",
		},
	}
	populateProposalSnippets(&p)
	for _, want := range []string{
		"- id: awareness.store_unavailable_explicit",
		"required_tests:",
		"- golang/server/briefing_test.go:TestBriefingNilState",
	} {
		if !strings.Contains(p.InvariantYAMLSnippet, want) {
			t.Fatalf("invariant snippet missing %q:\n%s", want, p.InvariantYAMLSnippet)
		}
	}
	if strings.Contains(p.InvariantYAMLSnippet, "TestResolve_FoundMapsCoreFields") {
		t.Fatalf("snippet should be capped to top replacements:\n%s", p.InvariantYAMLSnippet)
	}
	for _, want := range []string{
		"- id: golang/server/briefing_test.go:TestBriefingNilState",
		"title: TestBriefingNilState",
		"- awareness.store_unavailable_explicit",
		"- golang/server/briefing_test.go",
	} {
		if !strings.Contains(p.RequiredTestsYAMLSnippet, want) {
			t.Fatalf("required_tests snippet missing %q:\n%s", want, p.RequiredTestsYAMLSnippet)
		}
	}
}

func TestCollectContractLane_EmitsCandidateProposalAndSnippet(t *testing.T) {
	root := t.TempDir()
	intentDir := filepath.Join(root, "docs", "intent")
	if err := os.MkdirAll(intentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := `id: awareness.multi_language_extraction
level: mechanism
status: active
expressed_by:
  - golang/scanner/scanner.go
  - golang/scanner/typescript.go
related_invariants:
  - awareness.graph_core_is_language_neutral
required_tests:
  - golang/scanner/typescript_test.go:TestTSScanner_SharedGrammarValidation
`
	if err := os.WriteFile(filepath.Join(intentDir, "awareness.multi_language_extraction.yaml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	candidates, proposals, err := collectContractLane(root, true)
	if err != nil {
		t.Fatalf("collectContractLane: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates=%d, want 1: %+v", len(candidates), candidates)
	}
	if candidates[0].IntentID != "awareness.multi_language_extraction" {
		t.Fatalf("candidate intent id=%q", candidates[0].IntentID)
	}
	if len(proposals) != 1 {
		t.Fatalf("proposals=%d, want 1", len(proposals))
	}
	p := proposals[0]
	if p.IntentID != "awareness.multi_language_extraction" {
		t.Fatalf("intent id=%q", p.IntentID)
	}
	if p.RecommendedLevel != "contract" {
		t.Fatalf("recommended level=%q", p.RecommendedLevel)
	}
	for _, want := range []string{
		"id: awareness.multi_language_extraction",
		"level: contract",
		"required_tests:",
		"expressed_by:",
	} {
		if !strings.Contains(p.IntentYAMLSnippet, want) {
			t.Fatalf("intent snippet missing %q:\n%s", want, p.IntentYAMLSnippet)
		}
	}
}

func TestRunSafeInvariantTestFixWithMode_AppliesContractPromotionLineOnly(t *testing.T) {
	root := t.TempDir()
	intentDir := filepath.Join(root, "docs", "intent")
	awarenessDir := filepath.Join(root, "docs", "awareness")
	if err := os.MkdirAll(intentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(awarenessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(awarenessDir, "invariants.yaml"), []byte("invariants: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(awarenessDir, "required_tests.yaml"), []byte("required_tests: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	intentPath := filepath.Join(intentDir, "awareness.multi_language_extraction.yaml")
	doc := `id: awareness.multi_language_extraction
level: mechanism # promote only this scalar
status: active
expressed_by:
  - golang/scanner/scanner.go
  - golang/scanner/typescript.go
related_invariants:
  - awareness.graph_core_is_language_neutral
required_tests:
  - golang/scanner/typescript_test.go:TestTSScanner_SharedGrammarValidation
`
	if err := os.WriteFile(intentPath, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := runSafeInvariantTestFixWithMode(root, true, true, false)
	if err != nil {
		t.Fatalf("runSafeInvariantTestFixWithMode: %v", err)
	}
	if report.ContractCandidateCount != 1 || report.AppliedCount != 1 {
		t.Fatalf("contract/applied counts = %d/%d, want 1/1", report.ContractCandidateCount, report.AppliedCount)
	}
	for _, skipped := range report.Skipped {
		if strings.Contains(skipped, "no contract intents") {
			t.Fatalf("unexpected contract skip after applying candidate: %+v", report.Skipped)
		}
	}
	got, err := os.ReadFile(intentPath)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(doc, "level: mechanism # promote only this scalar", "level: contract # promote only this scalar", 1)
	if string(got) != want {
		t.Fatalf("intent file was reformatted or incorrectly updated:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRunRepoEvalFixText_ShowsContractProposalsWithoutInvariantProposals(t *testing.T) {
	root := t.TempDir()
	intentDir := filepath.Join(root, "docs", "intent")
	awarenessDir := filepath.Join(root, "docs", "awareness")
	for _, dir := range []string{intentDir, awarenessDir, filepath.Join(root, "golang")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(awarenessDir, "invariants.yaml"), []byte("invariants: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(awarenessDir, "required_tests.yaml"), []byte("required_tests: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := `id: awareness.multi_language_extraction
level: mechanism
status: active
expressed_by:
  - golang/scanner/scanner.go
  - golang/scanner/typescript.go
related_invariants:
  - awareness.graph_core_is_language_neutral
required_tests:
  - golang/scanner/typescript_test.go:TestTSScanner_SharedGrammarValidation
`
	if err := os.WriteFile(filepath.Join(intentDir, "awareness.multi_language_extraction.yaml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := captureStdoutStderr(t, func() int {
		return runRepoEvalFix([]string{
			"--repo", root,
			"--services-repo", root,
			"--ag-repo", root,
			"--proposal",
		})
	})
	if code != 0 {
		t.Fatalf("runRepoEvalFix code=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	for _, want := range []string{
		"No safe auto-fixes found.",
		"contract proposal: 1 candidate(s)",
		"awareness.multi_language_extraction",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "No safe evidence-backed fixes found.") {
		t.Fatalf("stdout incorrectly hid contract proposals:\n%s", stdout)
	}
}

func TestCollectContractLane_SkipsPrincipleAndVisionLevels(t *testing.T) {
	root := t.TempDir()
	intentDir := filepath.Join(root, "docs", "intent")
	if err := os.MkdirAll(intentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"p.yaml": `id: awg.signal_over_noise
level: principle
status: active
expressed_by:
  - golang/server/briefing.go
required_tests:
  - golang/server/main_test.go:TestAnything
`,
		"v.yaml": `id: awg.mission
level: vision
status: active
expressed_by:
  - cmd/awg/main.go
required_tests:
  - golang/server/main_test.go:TestAnything
`,
	} {
		if err := os.WriteFile(filepath.Join(intentDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	candidates, proposals, err := collectContractLane(root, true)
	if err != nil {
		t.Fatalf("collectContractLane: %v", err)
	}
	if len(candidates) != 0 || len(proposals) != 0 {
		t.Fatalf("principle/vision intents must be skipped: candidates=%+v proposals=%+v", candidates, proposals)
	}
}

func TestCollectAuthorityLane_FindsMissingAndDocsOnlyAnchors(t *testing.T) {
	root := t.TempDir()
	intentDir := filepath.Join(root, "docs", "intent")
	awarenessDir := filepath.Join(root, "docs", "awareness")
	if err := os.MkdirAll(intentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(awarenessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(awarenessDir, "invariants.yaml"), []byte("invariants: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := `id: awg.knowledge_earned_not_invented
level: contract
status: active
expressed_by:
  - docs/awareness/invariants.yaml
  - docs/awareness/forbidden_fixes.yaml
`
	if err := os.WriteFile(filepath.Join(intentDir, "awg.knowledge_earned_not_invented.yaml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	proposals, err := collectAuthorityLane(root)
	if err != nil {
		t.Fatalf("collectAuthorityLane: %v", err)
	}
	if len(proposals) != 1 {
		t.Fatalf("proposals=%d, want 1", len(proposals))
	}
	p := proposals[0]
	if p.IntentID != "awg.knowledge_earned_not_invented" {
		t.Fatalf("intent id=%q", p.IntentID)
	}
	if len(p.MissingExpressedBy) != 1 || p.MissingExpressedBy[0] != "docs/awareness/forbidden_fixes.yaml" {
		t.Fatalf("missing anchors=%+v", p.MissingExpressedBy)
	}
	if len(p.DocOnlyExpressedBy) != 1 || p.DocOnlyExpressedBy[0] != "docs/awareness/invariants.yaml" {
		t.Fatalf("docs-only anchors=%+v", p.DocOnlyExpressedBy)
	}
	if !strings.Contains(p.Reason, "missing repo paths") || !strings.Contains(p.Reason, "docs anchors") {
		t.Fatalf("unexpected reason: %q", p.Reason)
	}
}

func TestCollectAuthorityLane_AllowsDocOnlyPrinciples(t *testing.T) {
	root := t.TempDir()
	intentDir := filepath.Join(root, "docs", "intent")
	awarenessDir := filepath.Join(root, "docs", "awareness")
	if err := os.MkdirAll(intentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(awarenessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(awarenessDir, "design_decisions.md"), []byte("# decisions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := `id: awg.knowledge_earned_not_invented
level: principle
status: active
expressed_by:
  - docs/awareness/design_decisions.md
`
	if err := os.WriteFile(filepath.Join(intentDir, "awg.knowledge_earned_not_invented.yaml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	proposals, err := collectAuthorityLane(root)
	if err != nil {
		t.Fatalf("collectAuthorityLane: %v", err)
	}
	if len(proposals) != 0 {
		t.Fatalf("doc-only principles should not require executable authority: %+v", proposals)
	}
}

func TestCollectAuthorityLane_ResolvesSiblingRepoAnchors(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "services")
	intentDir := filepath.Join(root, "docs", "intent")
	for _, dir := range []string{
		intentDir,
		filepath.Join(root, "golang", "cluster_controller"),
		filepath.Join(parent, "packages", "metadata", "etcd", "specs"),
		filepath.Join(parent, "Globular", "internal", "gateway"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(parent, "packages", "metadata", "etcd", "specs", "etcd_service.yaml"), []byte("name: etcd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "Globular", "internal", "gateway", "gateway.go"), []byte("package gateway\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := `id: cross.repo.authority
level: contract
status: active
expressed_by:
  - services/golang/cluster_controller
  - packages/metadata/etcd/specs/etcd_service.yaml
  - ../packages/metadata/etcd/specs/etcd_service.yaml
  - Globular/internal/gateway
`
	if err := os.WriteFile(filepath.Join(intentDir, "cross.repo.authority.yaml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	proposals, err := collectAuthorityLane(root)
	if err != nil {
		t.Fatalf("collectAuthorityLane: %v", err)
	}
	if len(proposals) != 0 {
		t.Fatalf("sibling repo anchors should resolve without findings: %+v", proposals)
	}
}

func TestAuthorityAnchorExists_DoesNotTreatGlobsAsExisting(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "golang", "catalog", "catalog_server"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "golang", "catalog", "catalog_server", "zz_version_generated.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	exists, err := authorityAnchorExists(root, "golang/*/*_server/zz_version_generated.go")
	if err != nil {
		t.Fatalf("authorityAnchorExists: %v", err)
	}
	if exists {
		t.Fatal("authority expressed_by anchors are path references, not glob patterns")
	}
}

func TestRenderRepoEvalFixReview_GroupsByTargetFile(t *testing.T) {
	report := repoEvalFixReport{
		RepoRoot: "/tmp/repo",
		Proposals: []repoEvalFixProposal{
			{
				InvariantID:              "awareness.query.no_arbitrary_sparql",
				Reason:                   "stale test evidence",
				ReplacementSuggestions:   []string{"golang/server/main_test.go:TestQuery_BackendErrorReturnsUnavailable"},
				InvariantYAMLSnippet:     "- id: awareness.query.no_arbitrary_sparql\n  required_tests:\n    - golang/server/main_test.go:TestQuery_BackendErrorReturnsUnavailable",
				RequiredTestsYAMLSnippet: "- id: golang/server/main_test.go:TestQuery_BackendErrorReturnsUnavailable\n  title: TestQuery_BackendErrorReturnsUnavailable",
			},
		},
		ContractProposals: []repoEvalContractProposal{
			{
				IntentID:          "awareness.multi_language_extraction",
				File:              "docs/intent/awareness.multi_language_extraction.yaml",
				CurrentLevel:      "mechanism",
				RecommendedLevel:  "contract",
				Reason:            "grounded but review-required",
				IntentYAMLSnippet: "id: awareness.multi_language_extraction\nlevel: contract",
			},
		},
		AuthorityProposals: []repoEvalAuthorityProposal{
			{
				IntentID:           "awg.knowledge_earned_not_invented",
				File:               "docs/intent/awg.knowledge_earned_not_invented.yaml",
				Level:              "principle",
				Reason:             "expressed_by cites missing repo paths; expressed_by relies only on docs anchors; review whether executable authority is missing",
				MissingExpressedBy: []string{"docs/awareness/forbidden_fixes.yaml"},
				DocOnlyExpressedBy: []string{"docs/awareness/invariants.yaml"},
			},
		},
	}
	out := renderRepoEvalFixReview(report, true)
	for _, want := range []string{
		"docs/awareness/invariants.yaml",
		"docs/awareness/required_tests.yaml",
		"docs/intent/awareness.multi_language_extraction.yaml",
		"docs/intent/awg.knowledge_earned_not_invented.yaml",
		"Review stale invariant test evidence",
		"Consider promoting `awareness.multi_language_extraction`",
		"Review authority evidence for `awg.knowledge_earned_not_invented`",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("review output missing %q:\n%s", want, out)
		}
	}
}

func TestApplyInvariantRequiredTests_SkipsExistingOrNotApplicable(t *testing.T) {
	invariantsRaw := []byte(`invariants:
  - id: awareness.one
    severity: critical
    required_tests:
      - golang/server/one_test.go:TestOne
  - id: awareness.two
    severity: high
    test_not_applicable_reason: integration-only proof
`)
	requiredTestsRaw := []byte(`required_tests:
  - id: golang/server/one_test.go:TestOne
    title: TestOne
    protects:
      invariants:
        - awareness.one
      files:
        - golang/server/one_test.go
`)
	evidence := map[string]invariantTestEvidence{
		"awareness.one": {
			tests:       map[string]bool{"golang/server/x_test.go:TestX": true},
			evidence:    map[string]bool{"a:1": true},
			filesByTest: map[string]map[string]bool{"golang/server/x_test.go:TestX": {"golang/server/x_test.go": true}},
		},
		"awareness.two": {
			tests:       map[string]bool{"golang/server/y_test.go:TestY": true},
			evidence:    map[string]bool{"b:2": true},
			filesByTest: map[string]map[string]bool{"golang/server/y_test.go:TestY": {"golang/server/y_test.go": true}},
		},
	}

	updated, requiredUpdated, candidates, applied, err := applyInvariantRequiredTests(invariantsRaw, requiredTestsRaw, evidence)
	if err != nil {
		t.Fatalf("applyInvariantRequiredTests: %v", err)
	}
	if applied != 0 || len(candidates) != 0 {
		t.Fatalf("expected no applied candidates, got applied=%d candidates=%d", applied, len(candidates))
	}
	if string(updated) == "" {
		t.Fatal("updated yaml must not be empty")
	}
	if string(requiredUpdated) == "" {
		t.Fatal("updated required_tests yaml must not be empty")
	}
}
