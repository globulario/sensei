// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildProofPlan_ByAuthoritySurface(t *testing.T) {
	authorities := []authoritySurfaceCandidate{{
		ID:          "candidate.authority.globular.config_write",
		Kind:        "guarded_mutation_handler",
		SourceFiles: []string{"golang/workflow/engine/engine.go"},
	}}
	proofDoc := proofObligationsDoc{
		ProofObligations: []generatedProofObligation{{
			ID:                          "proof.authority.globular.config_write",
			DerivedFromAuthoritySurface: "candidate.authority.globular.config_write",
			AppliesToAuthoritySurfaces:  []string{"candidate.authority.globular.config_write"},
			EvidenceLane:                "hybrid",
			RequiredSlots: []generatedProofSlot{
				{Kind: "static_guard", Required: true},
				{Kind: "before_after", Required: true},
			},
		}},
	}
	forbidden := []proofPlanForbiddenMove{{
		ID:    "special_case_cluster_reconcile_foreach_guard",
		Title: "Special-case one workflow for foreach guard ordering",
	}}
	forbidden[0].Protects.Files = []string{"golang/workflow/engine/engine.go"}

	got, err := buildProofPlan(t.TempDir(), authorities, proofDoc, forbidden, "", "candidate.authority.globular.config_write", "", "")
	if err != nil {
		t.Fatalf("buildProofPlan: %v", err)
	}
	if got.Subject != "candidate.authority.globular.config_write" {
		t.Fatalf("subject=%q", got.Subject)
	}
	if len(got.Obligations) != 1 || got.Obligations[0].ID != "proof.authority.globular.config_write" {
		t.Fatalf("obligations=%+v", got.Obligations)
	}
	if len(got.ForbiddenMoves) != 1 || got.ForbiddenMoves[0].ID != "special_case_cluster_reconcile_foreach_guard" {
		t.Fatalf("forbidden=%+v", got.ForbiddenMoves)
	}
}

func TestBuildProofPlan_ByFile(t *testing.T) {
	authorities := []authoritySurfaceCandidate{
		{
			ID:          "candidate.authority.globular.config_write",
			Kind:        "guarded_mutation_handler",
			SourceFiles: []string{"server.go"},
		},
		{
			ID:          "candidate.authority.globular.gateway",
			Kind:        "lifecycle_control",
			SourceFiles: []string{"other.go"},
		},
	}
	proofDoc := proofObligationsDoc{
		ProofObligations: []generatedProofObligation{{
			ID:                          "proof.authority.globular.config_write",
			DerivedFromAuthoritySurface: "candidate.authority.globular.config_write",
			AppliesToAuthoritySurfaces:  []string{"candidate.authority.globular.config_write"},
			EvidenceLane:                "hybrid",
		}},
	}
	got, err := buildProofPlan(t.TempDir(), authorities, proofDoc, nil, "server.go", "", "", "")
	if err != nil {
		t.Fatalf("buildProofPlan: %v", err)
	}
	if len(got.Authority) != 1 || got.Authority[0].ID != "candidate.authority.globular.config_write" {
		t.Fatalf("authority=%+v", got.Authority)
	}
	if len(got.Obligations) != 1 || got.Obligations[0].ID != "proof.authority.globular.config_write" {
		t.Fatalf("obligations=%+v", got.Obligations)
	}
}

func TestBuildProofPlan_ByRepairClaim(t *testing.T) {
	root := t.TempDir()
	eventPath := filepath.Join(root, "learning_event.yaml")
	body := `learning_event:
  repair_claim:
    id: claim.globular.gateway
    authority_surface_ids:
      - candidate.authority.globular.gateway
`
	if err := os.WriteFile(eventPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	authorities := []authoritySurfaceCandidate{{
		ID:          "candidate.authority.globular.gateway",
		Kind:        "lifecycle_control",
		SourceFiles: []string{"cmd/awg/cmd_serve.go"},
	}}
	proofDoc := proofObligationsDoc{
		ProofObligations: []generatedProofObligation{{
			ID:                          "proof.authority.globular.gateway",
			DerivedFromAuthoritySurface: "candidate.authority.globular.gateway",
			AppliesToAuthoritySurfaces:  []string{"candidate.authority.globular.gateway"},
			EvidenceLane:                "runtime_required",
		}},
	}
	got, err := buildProofPlan(root, authorities, proofDoc, nil, "", "", "", eventPath)
	if err != nil {
		t.Fatalf("buildProofPlan: %v", err)
	}
	if got.Subject != "claim.globular.gateway" {
		t.Fatalf("subject=%q", got.Subject)
	}
	if len(got.Authority) != 1 || len(got.Obligations) != 1 {
		t.Fatalf("authority=%+v obligations=%+v", got.Authority, got.Obligations)
	}
}

func TestRenderProofPlanText_IncludesPromotionChecklist(t *testing.T) {
	out := renderProofPlanText(proofPlanResult{
		Subject: "candidate.authority.globular.gateway",
		Obligations: []generatedProofObligation{{
			ID:           "proof.authority.globular.gateway",
			EvidenceLane: "runtime_required",
			RequiredSlots: []generatedProofSlot{
				{Kind: "runtime", Required: true},
				{Kind: "log_artifact", Required: true},
			},
		}},
	})
	body := out
	if !strings.Contains(body, "Proof plan: candidate.authority.globular.gateway") {
		t.Fatalf("missing subject:\n%s", body)
	}
	if !strings.Contains(body, "Promotion requires:") {
		t.Fatalf("missing checklist:\n%s", body)
	}
	if !strings.Contains(body, "required_slots: runtime, log_artifact") {
		t.Fatalf("missing slots:\n%s", body)
	}
}
