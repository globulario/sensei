// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func TestBuildProofPlanForFiles_MergesAuthoritiesAndObligations(t *testing.T) {
	authorities := []authoritySurfaceCandidate{
		{
			ID:          "candidate.authority.demo.one",
			Kind:        "guarded_mutation_handler",
			SourceFiles: []string{"one.go"},
		},
		{
			ID:          "candidate.authority.demo.two",
			Kind:        "lifecycle_control",
			SourceFiles: []string{"two.go"},
		},
	}
	proofDoc := proofObligationsDoc{
		ProofObligations: []generatedProofObligation{
			{
				ID:                          "proof.authority.demo.one",
				DerivedFromAuthoritySurface: "candidate.authority.demo.one",
				AppliesToAuthoritySurfaces:  []string{"candidate.authority.demo.one"},
			},
			{
				ID:                          "proof.authority.demo.two",
				DerivedFromAuthoritySurface: "candidate.authority.demo.two",
				AppliesToAuthoritySurfaces:  []string{"candidate.authority.demo.two"},
			},
		},
	}
	forbidden := []proofPlanForbiddenMove{
		{ID: "forbidden.one"},
		{ID: "forbidden.two"},
	}
	forbidden[0].Protects.Files = []string{"one.go"}
	forbidden[1].Protects.Files = []string{"two.go"}

	got, err := buildProofPlanForFiles(t.TempDir(), authorities, proofDoc, forbidden, []string{"two.go", "one.go"})
	if err != nil {
		t.Fatalf("buildProofPlanForFiles: %v", err)
	}
	if len(got.Authority) != 2 {
		t.Fatalf("authority=%+v", got.Authority)
	}
	if len(got.Obligations) != 2 {
		t.Fatalf("obligations=%+v", got.Obligations)
	}
	if len(got.ForbiddenMoves) != 2 {
		t.Fatalf("forbidden=%+v", got.ForbiddenMoves)
	}
}

func TestRunRepairPlan_RendersAuthoritativeGovernedPlan(t *testing.T) {
	root := t.TempDir()
	writeRepairPlanFixtures(t, root)

	prev := repairPlanPreflight
	repairPlanPreflight = func(_ context.Context, _ string, req *awarenesspb.PreflightRequest) (*awarenesspb.PreflightResponse, error) {
		if got := strings.Join(req.GetFiles(), ","); got != "service.go" {
			t.Fatalf("files=%q", got)
		}
		return &awarenesspb.PreflightResponse{
			Status:     awarenesspb.PreflightStatus_PREFLIGHT_STATUS_OK,
			RiskClass:  awarenesspb.RiskClass_ARCHITECTURE_SENSITIVE,
			Confidence: awarenesspb.Confidence_CONFIDENCE_HIGH,
			RequiredActions: []string{
				"repair_plan:globular.repair.demo",
			},
			FilesToRead: []string{"docs/design.md"},
			TestsToRun:  []string{"go test ./demo/..."},
			BlindSpots:  []string{"runtime evidence still required"},
			Authority: &awarenesspb.GraphAuthority{
				Authoritative:              true,
				GraphFreshnessState:        awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
				LiveStoreGraphDigestSha256: "abc123",
				LiveStoreGraphTripleCount:  42,
			},
		}, nil
	}
	defer func() { repairPlanPreflight = prev }()

	code, out, errOut := captureStdoutStderr(t, func() int {
		return runRepairPlan([]string{
			"--repo-root", root,
			"--task", "repair demo service lifecycle",
			"--file", "service.go",
		})
	})
	if code != 0 {
		t.Fatalf("runRepairPlan exit=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{
		"Repair plan",
		"task: repair demo service lifecycle",
		"authority: authoritative (current)",
		"live_digest: abc123",
		"preserve authority surface: candidate.authority.demo.start_service",
		"prove obligation: proof.authority.demo.start_service",
		"avoid forbidden move: remove_runtime_guard_before_start",
		"run test: go test ./demo/...",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("repair-plan output missing %q:\n%s", want, out)
		}
	}
}

func TestRunRepairPlan_FailsClosedOnNonAuthoritativeGraph(t *testing.T) {
	root := t.TempDir()
	writeRepairPlanFixtures(t, root)

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
		return runRepairPlan([]string{
			"--repo-root", root,
			"--file", "service.go",
		})
	})
	if code == 0 {
		t.Fatal("expected non-zero exit for stale/non-authoritative graph")
	}
	if !strings.Contains(errOut, "requires current graph authority") {
		t.Fatalf("stderr missing authority refusal:\n%s", errOut)
	}
}

func writeRepairPlanFixtures(t *testing.T, root string) {
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

	write("service.go", "package demo\nfunc start() {}\n")
	write("docs/awareness/candidates/authority_surface_candidates.yaml", `authority_surface_candidates:
  candidates:
    - id: candidate.authority.demo.start_service
      class: AuthoritySurface
      status: candidate
      confidence: candidate
      kind: lifecycle_control
      owner: demo
      source_files:
        - service.go
`)
	write("docs/awareness/generated/proof_obligations.yaml", `proof_obligations:
  - id: proof.authority.demo.start_service
    derived_from_authority_surface: candidate.authority.demo.start_service
    applies_to_authority_surfaces:
      - candidate.authority.demo.start_service
    evidence_lane: runtime_required
    required_slots:
      - id: slot.authority.demo.start_service.runtime
        kind: runtime
        required: true
`)
	write("docs/awareness/architecture/forbidden_fixes.yaml", `forbidden_fixes:
  - id: remove_runtime_guard_before_start
    protects:
      files:
        - service.go
`)
}
