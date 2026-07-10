// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestClassifyRepairReport_ValidRepair(t *testing.T) {
	report := sampleRepairReport()
	report.FinalClassification = classifyRepairReport(report)
	if report.FinalClassification != repairClassificationValidRepair {
		t.Fatalf("classification=%q", report.FinalClassification)
	}
}

func TestClassifyRepairReport_MissingContract(t *testing.T) {
	report := sampleRepairReport()
	report.GoverningContract = repairContractSummary{Status: repairClassificationMissingContract}
	report.FinalClassification = classifyRepairReport(report)
	if report.FinalClassification != repairClassificationMissingContract {
		t.Fatalf("classification=%q", report.FinalClassification)
	}
}

func TestClassifyRepairReport_StaleAuthority(t *testing.T) {
	report := sampleRepairReport()
	report.Authority.State = "stale"
	report.Authority.Authoritative = false
	report.FinalClassification = classifyRepairReport(report)
	if report.FinalClassification != repairClassificationStaleAuthority {
		t.Fatalf("classification=%q", report.FinalClassification)
	}
}

func TestClassifyRepairReport_ForbiddenMoveOverridesEvidence(t *testing.T) {
	report := sampleRepairReport()
	report.ForbiddenMoveFindings = []repairFinding{{ID: "forbidden.fix", Message: "bad move"}}
	report.Authority.State = "stale"
	report.Evidence.Status = "missing_required_tests"
	report.FinalClassification = classifyRepairReport(report)
	if report.FinalClassification != repairClassificationForbiddenMoveDetected {
		t.Fatalf("classification=%q", report.FinalClassification)
	}
}

func TestClassifyRepairReport_InsufficientEvidence(t *testing.T) {
	report := sampleRepairReport()
	report.Evidence.Status = "missing_required_tests"
	report.Evidence.MissingRequiredTests = []string{"pkg/demo_test.go:TestCriticalPath"}
	report.FinalClassification = classifyRepairReport(report)
	if report.FinalClassification != repairClassificationInsufficientEvidence {
		t.Fatalf("classification=%q", report.FinalClassification)
	}
}

func TestWriteRepairReportArtifact_DeterministicJSON(t *testing.T) {
	report := sampleRepairReport()
	report.FinalClassification = classifyRepairReport(report)
	got, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{
  "schema_version": "repair_report.v1",
  "repair_target": {
    "task_summary": "repair payment confirmation flow",
    "issue_summary": "repair payment confirmation flow"
  },
  "touched_files": [
    "src/payment_processor.py"
  ],
  "explicit_scope": [
    "src/payment_processor.py"
  ],
  "guarded_paths": [
    "src/payment_processor.py"
  ],
  "governing_contract": {
    "status": "present",
    "summary": [
      "invariant: payments.paid_state_requires_processor_confirmation"
    ],
    "contract_ids": [
      "payments.paid_state_requires_processor_confirmation"
    ]
  },
  "authority": {
    "state": "current",
    "authoritative": true,
    "graph_freshness_state": "current",
    "build_provenance_state": "stamped",
    "coverage_state": "sufficient",
    "detail": "current graph authority",
    "live_graph_digest_sha256": "abc123"
  },
  "evidence": {
    "status": "required_tests_satisfied",
    "required_tests": [
      "src/payment_processor_test.py:TestPaidRequiresProcessorConfirmation"
    ],
    "tests_run": [
      "src/payment_processor_test.py:TestPaidRequiresProcessorConfirmation"
    ],
    "checks_passed": [
      "pytest"
    ],
    "summary": [
      "required test evidence is present"
    ]
  },
  "required_actions": [
    "read payment authority",
    "run processor confirmation test"
  ],
  "preflight_status": "ok",
  "risk_class": "architecture_sensitive",
  "confidence": "high",
  "final_classification": "valid_repair"
}`
	if string(got) != want {
		t.Fatalf("unstable JSON.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRunRepairReport_WritesArtifact(t *testing.T) {
	root := t.TempDir()
	writeRepairReportFile(t, root, "src/payment_processor.py", "print('ok')\n")
	writeRepairReportFile(t, root, "docs/awareness/high_risk_files.yaml", "files:\n  - src/\n")

	prevMeta := repairReportMetadata
	prevPreflight := repairReportPreflight
	prevEdit := repairReportEditCheck
	defer func() {
		repairReportMetadata = prevMeta
		repairReportPreflight = prevPreflight
		repairReportEditCheck = prevEdit
	}()
	repairReportMetadata = func(context.Context, string) (*awarenesspb.MetadataResponse, error) {
		return &awarenesspb.MetadataResponse{
			GraphFreshnessState:        awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
			BuildProvenanceState:       awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED,
			CoverageState:              awarenesspb.CoverageState_COVERAGE_STATE_SUFFICIENT,
			GraphFreshnessDetail:       "current graph authority",
			LiveStoreGraphDigestSha256: "abc123",
		}, nil
	}
	repairReportPreflight = func(context.Context, string, *awarenesspb.PreflightRequest) (*awarenesspb.PreflightResponse, error) {
		return &awarenesspb.PreflightResponse{
			Status:     awarenesspb.PreflightStatus_PREFLIGHT_STATUS_OK,
			RiskClass:  awarenesspb.RiskClass_ARCHITECTURE_SENSITIVE,
			Confidence: awarenesspb.Confidence_CONFIDENCE_HIGH,
			DirectInvariants: []*awarenesspb.KnowledgeNode{{
				Id:    "payments.paid_state_requires_processor_confirmation",
				Label: "payments.paid_state_requires_processor_confirmation",
			}},
			TestsToRun:      []string{"src/payment_processor_test.py:TestPaidRequiresProcessorConfirmation"},
			RequiredActions: []string{"read payment authority"},
			Authority: &awarenesspb.GraphAuthority{
				Authoritative:       true,
				GraphFreshnessState: awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
			},
		}, nil
	}
	repairReportEditCheck = func(context.Context, string, *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error) {
		return &awarenesspb.EditCheckResponse{}, nil
	}

	out := filepath.Join(root, "artifacts", "repair-report.json")
	code, stdout, stderr := captureStdoutStderr(t, func() int {
		return runRepairReport([]string{
			"--repo-root", root,
			"--file", "src/payment_processor.py",
			"--task", "repair payment confirmation flow",
			"--test-run", "src/payment_processor_test.py:TestPaidRequiresProcessorConfirmation",
			"--check-pass", "pytest",
			"--scope-file", "src/payment_processor.py",
			"--out", out,
			"--format", "json",
		})
	})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, `"final_classification": "valid_repair"`) {
		t.Fatalf("stdout missing classification:\n%s", stdout)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if !bytes.Equal(data[len(data)-1:], []byte("\n")) {
		t.Fatalf("artifact must end with newline")
	}
	if !strings.Contains(string(data), `"schema_version": "repair_report.v1"`) {
		t.Fatalf("artifact missing schema_version:\n%s", data)
	}
}

func TestRunRepairGate_ReportFile_FailsClosedOnMissingContractForGuardedPath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "report.json")
	report := sampleRepairReport()
	report.GoverningContract = repairContractSummary{Status: repairClassificationMissingContract}
	report.FinalClassification = repairClassificationMissingContract
	if err := writeRepairReportArtifact(path, report); err != nil {
		t.Fatalf("write report: %v", err)
	}
	code, stdout, _ := captureStdoutStderr(t, func() int {
		return runRepairGate([]string{"--report", path})
	})
	if code == 0 {
		t.Fatal("expected non-zero gate exit")
	}
	if !strings.Contains(stdout, "guarded path has no governing contract") {
		t.Fatalf("stdout missing fail-closed reason:\n%s", stdout)
	}
}

func TestGenerateRepairReport_BackingStoreUnavailable(t *testing.T) {
	root := t.TempDir()
	writeRepairReportFile(t, root, "src/payment_processor.py", "print('ok')\n")

	prevMeta := repairReportMetadata
	defer func() { repairReportMetadata = prevMeta }()
	repairReportMetadata = func(context.Context, string) (*awarenesspb.MetadataResponse, error) {
		return nil, status.Error(codes.Unavailable, "store is unavailable")
	}
	report, err := generateRepairReport(repairReportOptions{
		Task:     "repair payment confirmation flow",
		RepoRoot: root,
		Files:    []string{"src/payment_processor.py"},
	})
	if err != nil {
		t.Fatalf("generateRepairReport: %v", err)
	}
	if report.FinalClassification != repairClassificationBackingStoreUnavailable {
		t.Fatalf("classification=%q", report.FinalClassification)
	}
}

func TestGenerateRepairReport_StaleAuthorityArtifact(t *testing.T) {
	root := t.TempDir()
	writeRepairReportFile(t, root, "src/payment_processor.py", "print('ok')\n")

	prevMeta := repairReportMetadata
	prevPreflight := repairReportPreflight
	defer func() {
		repairReportMetadata = prevMeta
		repairReportPreflight = prevPreflight
	}()
	repairReportMetadata = func(context.Context, string) (*awarenesspb.MetadataResponse, error) {
		return &awarenesspb.MetadataResponse{
			GraphFreshnessState:        awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_STALE,
			BuildProvenanceState:       awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED,
			CoverageState:              awarenesspb.CoverageState_COVERAGE_STATE_SUFFICIENT,
			GraphFreshnessDetail:       "live triple count mismatch",
			LiveStoreGraphDigestSha256: "abc123",
		}, nil
	}
	repairReportPreflight = func(context.Context, string, *awarenesspb.PreflightRequest) (*awarenesspb.PreflightResponse, error) {
		return nil, status.Error(codes.FailedPrecondition, "graph freshness stale for preflight: live triple count 117026 != expected 96504")
	}

	report, err := generateRepairReport(repairReportOptions{
		Task:     "repair payment confirmation flow",
		RepoRoot: root,
		Files:    []string{"src/payment_processor.py"},
	})
	if err != nil {
		t.Fatalf("generateRepairReport: %v", err)
	}
	if report.FinalClassification != repairClassificationStaleAuthority {
		t.Fatalf("classification=%q", report.FinalClassification)
	}
	if report.Evidence.Status != "not_evaluated" {
		t.Fatalf("evidence.status=%q", report.Evidence.Status)
	}
}

func sampleRepairReport() governedRepairReport {
	return governedRepairReport{
		SchemaVersion: "repair_report.v1",
		RepairTarget: repairReportTarget{
			TaskSummary:  "repair payment confirmation flow",
			IssueSummary: "repair payment confirmation flow",
		},
		TouchedFiles:  []string{"src/payment_processor.py"},
		ExplicitScope: []string{"src/payment_processor.py"},
		GuardedPaths:  []string{"src/payment_processor.py"},
		GoverningContract: repairContractSummary{
			Status:      "present",
			Summary:     []string{"invariant: payments.paid_state_requires_processor_confirmation"},
			ContractIDs: []string{"payments.paid_state_requires_processor_confirmation"},
		},
		Authority: repairAuthoritySummary{
			State:                 "current",
			Authoritative:         true,
			GraphFreshnessState:   "current",
			BuildProvenanceState:  "stamped",
			CoverageState:         "sufficient",
			Detail:                "current graph authority",
			LiveGraphDigestSha256: "abc123",
		},
		Evidence: repairEvidenceSummary{
			Status:        "required_tests_satisfied",
			RequiredTests: []string{"src/payment_processor_test.py:TestPaidRequiresProcessorConfirmation"},
			TestsRun:      []string{"src/payment_processor_test.py:TestPaidRequiresProcessorConfirmation"},
			ChecksPassed:  []string{"pytest"},
			Summary:       []string{"required test evidence is present"},
		},
		RequiredActions: []string{"read payment authority", "run processor confirmation test"},
		PreflightStatus: "ok",
		RiskClass:       "architecture_sensitive",
		Confidence:      "high",
	}
}

func writeRepairReportFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
