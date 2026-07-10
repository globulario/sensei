// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildProofObligations_ConfigAndLifecycleTemplates(t *testing.T) {
	surfaces := []authoritySurfaceCandidate{
		{
			ID:                "candidate.authority.globular.config_write",
			Status:            "candidate",
			Kind:              "state_mutation",
			RequiredAuthority: []string{"config_authority"},
		},
		{
			ID:                "candidate.authority.globular.start_services",
			Status:            "candidate",
			Kind:              "lifecycle_control",
			RequiredAuthority: []string{"service_lifecycle_authority"},
		},
	}
	doc := buildProofObligations(surfaces)
	if len(doc.ProofObligations) != 2 {
		t.Fatalf("len(ProofObligations)=%d, want 2", len(doc.ProofObligations))
	}
	if got := doc.ProofObligations[0].TemplateKind; got != "config_mutation" {
		t.Fatalf("first template=%q, want config_mutation", got)
	}
	if got := doc.ProofObligations[0].EvidenceLane; got != "hybrid" {
		t.Fatalf("first lane=%q, want hybrid", got)
	}
	if got := doc.ProofObligations[1].TemplateKind; got != "service_lifecycle" {
		t.Fatalf("second template=%q, want service_lifecycle", got)
	}
	if got := doc.ProofObligations[1].EvidenceLane; got != "runtime_required" {
		t.Fatalf("second lane=%q, want runtime_required", got)
	}
}

func TestLoadAuthoritySurfaces_CandidateFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "authority_surface_candidates.yaml")
	body := `authority_surface_candidates:
  candidates:
    - id: candidate.authority.globular.config_write
      class: AuthoritySurface
      status: candidate
      confidence: candidate
      kind: state_mutation
      required_authority:
        - config_authority
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := loadAuthoritySurfaces(path)
	if err != nil {
		t.Fatalf("loadAuthoritySurfaces: %v", err)
	}
	if len(got) != 1 || got[0].ID != "candidate.authority.globular.config_write" {
		t.Fatalf("got=%+v", got)
	}
}

func TestRenderProofObligations_HasTopLevelKey(t *testing.T) {
	doc := proofObligationsDoc{
		ProofObligations: []generatedProofObligation{{
			ID:                          "proof.authority.globular.config_write",
			Label:                       "Proof obligation for candidate.authority.globular.config_write",
			Status:                      "candidate",
			DerivedFromStatus:           "candidate",
			DerivedFromAuthoritySurface: "candidate.authority.globular.config_write",
			AppliesToAuthoritySurfaces:  []string{"candidate.authority.globular.config_write"},
			EvidenceLane:                "hybrid",
			TemplateKind:                "config_mutation",
			RequiredSlots: []generatedProofSlot{{
				ID:          "slot.authority.globular.config_write.static_guard",
				Kind:        "static_guard",
				Description: "guard evidence",
				Required:    true,
			}},
		}},
	}
	out, err := renderProofObligations(doc)
	if err != nil {
		t.Fatalf("renderProofObligations: %v", err)
	}
	body := string(out)
	if !strings.Contains(body, "proof_obligations:") {
		t.Fatalf("missing top-level key:\n%s", body)
	}
	if !strings.Contains(body, "TemplateKind") && !strings.Contains(body, "template_kind: config_mutation") {
		t.Fatalf("missing template kind:\n%s", body)
	}
}
