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

func TestBenchmarkOverallScore(t *testing.T) {
	clean := true
	breakdown := benchmarkCertificationBreakdown{
		Scope:     benchmarkCertificationLane{Status: "ok", Score: 5},
		Proof:     benchmarkCertificationLane{Status: "ok", Score: 5},
		Authority: benchmarkCertificationLane{Status: "ok", Score: 5},
		Evidence:  benchmarkCertificationLane{Status: "ok", Score: 5},
	}
	score, reason := benchmarkOverallScore(benchmarkScoreTask{
		RepairSuccess:      true,
		ContractClean:      &clean,
		ContractConfidence: "high",
	}, benchmarkJudgeResult{
		ContractPreservation: "ok",
		TestDiscipline:       "ok",
		AuthorityDiscipline:  "ok",
	}, breakdown)
	if score != 100 {
		t.Fatalf("score=%d, want 100", score)
	}
	if reason != "" {
		t.Fatalf("unexpected cap reason for perfect score: %q", reason)
	}
	score, reason = benchmarkOverallScore(benchmarkScoreTask{}, benchmarkJudgeResult{
		ContractPreservation: "review_required",
		TestDiscipline:       "insufficient",
		AuthorityDiscipline:  "review_required",
	}, benchmarkCertificationBreakdown{})
	if score != 0 {
		t.Fatalf("score=%d, want 0", score)
	}
	if reason != "" {
		t.Fatalf("unexpected cap reason for zero score: %q", reason)
	}
	breakdown.Evidence = benchmarkCertificationLane{Status: "review_required", Score: 2}
	score, reason = benchmarkOverallScore(benchmarkScoreTask{
		RepairSuccess:      true,
		ContractClean:      &clean,
		ContractConfidence: "medium",
	}, benchmarkJudgeResult{
		ContractPreservation: "ok",
		TestDiscipline:       "ok",
		AuthorityDiscipline:  "ok",
	}, breakdown)
	if score != 89 {
		t.Fatalf("score=%d, want 89 after review-required cap", score)
	}
	if reason == "" {
		t.Fatal("expected cap reason for review-required lane")
	}
}

func TestBenchmarkOverallScore_LowConfidenceCapsHighScore(t *testing.T) {
	clean := true
	breakdown := benchmarkCertificationBreakdown{
		Scope:     benchmarkCertificationLane{Status: "ok", Score: 5},
		Proof:     benchmarkCertificationLane{Status: "ok", Score: 5},
		Authority: benchmarkCertificationLane{Status: "ok", Score: 5},
		Evidence:  benchmarkCertificationLane{Status: "ok", Score: 5},
	}
	score, reason := benchmarkOverallScore(benchmarkScoreTask{
		RepairSuccess:      true,
		ContractClean:      &clean,
		ContractConfidence: "low",
	}, benchmarkJudgeResult{
		ContractPreservation: "ok",
		TestDiscipline:       "ok",
		AuthorityDiscipline:  "ok",
	}, breakdown)
	if score != 89 {
		t.Fatalf("score=%d, want 89 for low confidence cap", score)
	}
	if reason == "" {
		t.Fatal("expected cap reason for low contract confidence")
	}
}

func TestBenchmarkCertificationLanes(t *testing.T) {
	clean := false
	task := benchmarkScoreTask{
		ContractClean:         &clean,
		ContractCleanReasons:  []string{"test_proof_incomplete", "scope_underconstrained", "verification_missing_required_path", "contract_block_missing"},
		ContractCleanWarnings: []string{"verification_missing_review_path"},
		ContractFailureReason: "test_proof_incomplete",
		ContractScopeStatus:   "underconstrained",
		ProofRequired:         true,
		ProofStatus:           "incomplete",
	}
	judge := benchmarkJudgeResult{
		AuthorityDiscipline:  "review_required",
		AuthorityGaps:        []string{"missing authority anchor"},
		EvidenceGaps:         []string{"stale tested_by evidence"},
		MissingRequiredTests: []string{"pkg/foo_test.go:TestFoo"},
	}
	breakdown := benchmarkCertificationLanes(task, judge)
	if breakdown.Scope.Status != "insufficient" || breakdown.Proof.Status != "insufficient" || breakdown.Authority.Status != "insufficient" || breakdown.Evidence.Status != "insufficient" {
		t.Fatalf("unexpected breakdown: %+v", breakdown)
	}
}

func TestBenchmarkCertificationLanes_UsesStructuredRequiredPathEvidence(t *testing.T) {
	clean := false
	task := benchmarkScoreTask{
		Files:                             []string{"pkg/helper/shared.go"},
		ContractClean:                     &clean,
		AllowedRelatedScopeCandidateFiles: []string{"pkg/helper/shared.go"},
		MissingRequiredScopeFiles:         []string{"pkg/required/path.go"},
		MissingVerifiedPaths:              []string{"verify:pkg/required/path.go"},
		RequiredTestPaths:                 []string{"pkg/path_test.go:TestPath"},
		MissingRequiredTestPaths:          []string{"pkg/path_test.go:TestPath"},
		ProofRequired:                     true,
		ProofStatus:                       "",
	}
	breakdown := benchmarkCertificationLanes(task, benchmarkJudgeResult{})
	if breakdown.Scope.Status != "review_required" {
		t.Fatalf("scope status=%q, want review_required", breakdown.Scope.Status)
	}
	if breakdown.Proof.Status != "insufficient" {
		t.Fatalf("proof status=%q, want insufficient", breakdown.Proof.Status)
	}
	if breakdown.Evidence.Status != "insufficient" {
		t.Fatalf("evidence status=%q, want insufficient", breakdown.Evidence.Status)
	}
	if !hasReasonPrefix(breakdown.Scope.Reasons, "allowed_related_scope_candidate_unconfirmed:") {
		t.Fatalf("scope reasons missing structured candidate path: %+v", breakdown.Scope.Reasons)
	}
	if !hasReasonPrefix(breakdown.Proof.Reasons, "missing_required_test_path:") {
		t.Fatalf("proof reasons missing structured required test path: %+v", breakdown.Proof.Reasons)
	}
	if !hasReasonPrefix(breakdown.Evidence.Reasons, "verification_missing_required_path:") {
		t.Fatalf("evidence reasons missing structured verification path: %+v", breakdown.Evidence.Reasons)
	}
}

func TestBenchmarkCertificationLanes_IgnoresUntouchedScopeCandidates(t *testing.T) {
	clean := true
	task := benchmarkScoreTask{
		Files:                             []string{"pkg/cmd/pr/shared/commentable.go"},
		ContractClean:                     &clean,
		AllowedRelatedScopeCandidateFiles: []string{"pkg/cmdutil/file_input.go or equivalent cmdutil.ReadFile helper"},
	}
	breakdown := benchmarkCertificationLanes(task, benchmarkJudgeResult{})
	if breakdown.Scope.Status != "ok" {
		t.Fatalf("scope status=%q, want ok", breakdown.Scope.Status)
	}
	if len(breakdown.Scope.Reasons) != 0 {
		t.Fatalf("unexpected scope reasons: %+v", breakdown.Scope.Reasons)
	}
}

func TestBenchmarkRecommendedFocus(t *testing.T) {
	clean := false
	task := benchmarkScoreTask{
		ContractClean:        &clean,
		ContractCleanReasons: []string{"scope_underconstrained", "test_proof_incomplete"},
		ContractScopeStatus:  "underconstrained",
		ProofRequired:        true,
		ProofStatus:          "incomplete",
	}
	judge := benchmarkJudgeResult{
		MissingRequiredTests: []string{"pkg/foo_test.go:TestFoo"},
		EvidenceGaps:         []string{"stale tested_by evidence"},
	}
	breakdown := benchmarkCertificationBreakdown{
		Scope:    benchmarkCertificationLane{Status: "insufficient", Score: 0, Reasons: []string{"scope_underconstrained"}},
		Proof:    benchmarkCertificationLane{Status: "insufficient", Score: 0, Reasons: []string{"test_proof_incomplete"}},
		Evidence: benchmarkCertificationLane{Status: "review_required", Score: 2, Reasons: []string{"verification_missing_review_path"}},
	}
	got := benchmarkRecommendedFocus(task, judge, breakdown)
	if len(got) < 3 {
		t.Fatalf("focus len=%d, want at least 3: %+v", len(got), got)
	}
	if got[0].Priority != 1 || (got[0].Lane != "proof" && got[0].Lane != "scope") {
		t.Fatalf("unexpected first focus: %+v", got[0])
	}
}

func TestBenchmarkScore_ComposesBriefAndJudge(t *testing.T) {
	prevAtomic := benchmarkAtomicGuard
	benchmarkAtomicGuard = func(string, string) error { return nil }
	defer func() { benchmarkAtomicGuard = prevAtomic }()

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

func Query() {}
`)
	write("golang/server/main_test.go", `package server
import "testing"
func TestQuery_RawSPARQLLikeInputRejected(t *testing.T) {}
`)
	write("docs/awareness/invariants.yaml", `invariants:
  - id: awareness.query.no_arbitrary_sparql
    protects:
      files:
        - golang/server/query.go
    required_tests:
      - golang/server/main_test.go:TestQuery_RawSPARQLLikeInputRejected
`)
	write("docs/awareness/required_tests.yaml", `required_tests:
  - id: golang/server/main_test.go:TestQuery_RawSPARQLLikeInputRejected
    protects:
      files:
        - golang/server/query.go
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

	clean := true
	task := benchmarkBriefTask{
		Issue: "Query must not expose raw sparql passthrough",
		Files: []string{"golang/server/query.go"},
	}
	brief, err := buildBenchmarkBrief(root, task, "test")
	if err != nil {
		t.Fatalf("buildBenchmarkBrief: %v", err)
	}
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
			Authority: &awarenesspb.GraphAuthority{
				Authoritative:       true,
				GraphFreshnessState: awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
			},
		}, nil
	}
	defer func() { repairPlanPreflight = prev }()
	repairPlan, err := buildAuthoritativeRepairPlan(root, "localhost:10120", task.Issue, brief.LikelyImplementationFiles)
	if err != nil {
		t.Fatalf("buildAuthoritativeRepairPlan: %v", err)
	}
	brief.RepairPlan = &repairPlan
	judge, err := buildBenchmarkJudge(root, task, []string{"golang/server/main_test.go:TestQuery_RawSPARQLLikeInputRejected"})
	if err != nil {
		t.Fatalf("buildBenchmarkJudge: %v", err)
	}
	res := benchmarkScoreResult{
		RepoRoot:      root,
		Sequence:      []string{"benchmark-brief", "benchmark-judge"},
		RepairSuccess: benchmarkRepairSuccess(true),
		ContractCertification: benchmarkContractCertification(benchmarkScoreTask{ContractClean: &clean, ContractConfidence: "high"}, benchmarkCertificationBreakdown{
			Scope:     benchmarkCertificationLane{Status: "ok", Score: 5},
			Proof:     benchmarkCertificationLane{Status: "ok", Score: 5},
			Authority: benchmarkCertificationLane{Status: "ok", Score: 5},
			Evidence:  benchmarkCertificationLane{Status: "ok", Score: 5},
		}),
		ContractConfidence: "high",
		ContractClean:      &clean,
		CertificationBreakdown: benchmarkCertificationBreakdown{
			Scope:     benchmarkCertificationLane{Status: "ok", Score: 5},
			Proof:     benchmarkCertificationLane{Status: "ok", Score: 5},
			Authority: benchmarkCertificationLane{Status: "ok", Score: 5},
			Evidence:  benchmarkCertificationLane{Status: "ok", Score: 5},
		},
		OverallScore: func() int {
			score, _ := benchmarkOverallScore(benchmarkScoreTask{
				RepairSuccess:      true,
				ContractClean:      &clean,
				ContractConfidence: "high",
			}, judge, benchmarkCertificationBreakdown{
				Scope:     benchmarkCertificationLane{Status: "ok", Score: 5},
				Proof:     benchmarkCertificationLane{Status: "ok", Score: 5},
				Authority: benchmarkCertificationLane{Status: "ok", Score: 5},
				Evidence:  benchmarkCertificationLane{Status: "ok", Score: 5},
			})
			return score
		}(),
		RepairPlanIDs:  benchmarkRepairPlanIDs(brief.RepairPlan),
		RepairProofIDs: benchmarkRepairProofIDs(brief.RepairPlan),
		Brief:          brief,
		Judge:          judge,
	}
	if res.OverallScore <= 0 {
		t.Fatalf("overall score=%d, want positive composed score", res.OverallScore)
	}
	if len(res.Brief.LikelyImplementationFiles) == 0 {
		t.Fatalf("brief not composed: %+v", res.Brief)
	}
	if res.Judge.TestDiscipline != "ok" {
		t.Fatalf("judge not composed: %+v", res.Judge)
	}
	if len(res.RepairPlanIDs) != 1 || res.RepairPlanIDs[0] != "repair_plan:globular.repair.query_authority" {
		t.Fatalf("repair plan ids = %+v", res.RepairPlanIDs)
	}
	if len(res.RepairProofIDs) != 1 || res.RepairProofIDs[0] != "proof.authority.query.surface" {
		t.Fatalf("repair proof ids = %+v", res.RepairProofIDs)
	}
}

func TestBuildAuthoritativeRepairPlan_AllowsMissingLocalAuthorityMetadata(t *testing.T) {
	root := t.TempDir()
	prev := repairPlanPreflight
	repairPlanPreflight = func(_ context.Context, _ string, req *awarenesspb.PreflightRequest) (*awarenesspb.PreflightResponse, error) {
		return &awarenesspb.PreflightResponse{
			Status:     awarenesspb.PreflightStatus_PREFLIGHT_STATUS_OK,
			RiskClass:  awarenesspb.RiskClass_ARCHITECTURE_SENSITIVE,
			Confidence: awarenesspb.Confidence_CONFIDENCE_HIGH,
			RequiredActions: []string{
				"repair_plan:globular.repair.foreign_repo",
			},
			Authority: &awarenesspb.GraphAuthority{
				Authoritative:       true,
				GraphFreshnessState: awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
			},
		}, nil
	}
	defer func() { repairPlanPreflight = prev }()

	res, err := buildAuthoritativeRepairPlan(root, "localhost:10120", "foreign repo task", []string{"pkg/demo.go"})
	if err != nil {
		t.Fatalf("buildAuthoritativeRepairPlan: %v", err)
	}
	if len(res.RequiredActions) != 1 || res.RequiredActions[0] != "repair_plan:globular.repair.foreign_repo" {
		t.Fatalf("required actions = %+v", res.RequiredActions)
	}
}

func TestLoadBenchmarkScoreTask_FromTaskFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "task.json")
	task := benchmarkScoreTask{
		InstanceID:                "example-query-001",
		Issue:                     "Query must not expose raw sparql passthrough",
		F2PTests:                  []string{"TestQuery_BackendErrorReturnsUnavailable"},
		Files:                     []string{"golang/server/query.go"},
		TestsRun:                  []string{"golang/server/main_test.go:TestQuery_BackendErrorReturnsUnavailable"},
		RepairSuccess:             true,
		ContractConfidence:        "low",
		ContractCleanReasons:      []string{"scope_underconstrained"},
		ContractFailureReason:     "test_proof_incomplete",
		ContractScopeStatus:       "underconstrained",
		MissingRequiredScopeFiles: []string{"pkg/required/path.go"},
		MissingVerifiedPaths:      []string{"verify:pkg/required/path.go"},
		RequiredTestPaths:         []string{"pkg/path_test.go:TestPath"},
		MissingRequiredTestPaths:  []string{"pkg/path_test.go:TestPath"},
		ProofRequired:             true,
		ProofStatus:               "incomplete",
	}
	clean := false
	task.ContractClean = &clean
	raw, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, source, err := loadBenchmarkScoreTask(path, "", nil, nil, nil, false)
	if err != nil {
		t.Fatalf("loadBenchmarkScoreTask: %v", err)
	}
	if source != path {
		t.Fatalf("source=%q, want %q", source, path)
	}
	if got.InstanceID != task.InstanceID || !got.RepairSuccess || len(got.TestsRun) != 1 {
		t.Fatalf("loaded task mismatch: %+v", got)
	}
	if got.ContractClean == nil || *got.ContractClean || got.ContractConfidence != "low" || got.ContractFailureReason != "test_proof_incomplete" {
		t.Fatalf("loaded contract certification mismatch: %+v", got)
	}
	if len(got.MissingRequiredScopeFiles) != 1 || len(got.MissingVerifiedPaths) != 1 || len(got.MissingRequiredTestPaths) != 1 {
		t.Fatalf("loaded structured certification evidence mismatch: %+v", got)
	}
}

func hasReasonPrefix(reasons []string, prefix string) bool {
	for _, item := range reasons {
		if strings.HasPrefix(item, prefix) {
			return true
		}
	}
	return false
}
