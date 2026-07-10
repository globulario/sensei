// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildCertifyResult_CleanLegacyEvent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "learning_event.yaml")
	body := `learning_event:
  id: learning.mode_d.cli__cli-1388.test
  task: cli__cli-1388
  promotion_allowed: true
  certification_status: certified_clean_repair
  diagnosis:
    primary_failure_mode: clean_contract_repair
  certification:
    certification_status: certified_clean_repair
    governing_contract_id: contract.repo_fork_and_view_nontty_scriptability
    frozen_contract_present: true
    contract_block_valid: true
    contract_block_maps_to_frozen_contract: true
    scope_valid: true
    evidence_sufficient: true
    required_paths_satisfied: true
    promotion_allowed: true
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := loadBenchmarkRetryDoc(path)
	if err != nil {
		t.Fatalf("loadBenchmarkRetryDoc: %v", err)
	}
	got := buildCertifyResult(benchmarkRetryUnwrapEvent(doc), proofObligationsDoc{})
	if got.GovernanceCertification.Verdict != "certified_clean_repair" {
		t.Fatalf("verdict=%q", got.GovernanceCertification.Verdict)
	}
	if got.GovernanceCertification.Promotion != "allowed" {
		t.Fatalf("promotion=%q", got.GovernanceCertification.Promotion)
	}
	if got.GovernanceCertification.ScoreUsedForCertification {
		t.Fatal("score_used_for_certification=true, want false")
	}
}

func TestBuildCertifyResult_ArtifactMappingMissing(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "learning_event.yaml")
	body := `learning_event:
  id: learning.mode_d.cli__cli-3192.test
  task: cli__cli-3192
  certification_status: evidence_mapping_missing
  missing_evidence:
    - verification_file_unavailable
  diagnosis:
    primary_failure_mode: verification_impossible
  certification:
    certification_status: evidence_mapping_missing
    governing_contract_id: contract.body_file_supported_where_body_is_supported
    frozen_contract_present: true
    contract_block_valid: true
    contract_block_maps_to_frozen_contract: true
    scope_valid: true
    evidence_sufficient: false
    required_paths_satisfied: false
    missing_evidence:
      - verification_file_unavailable
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := loadBenchmarkRetryDoc(path)
	if err != nil {
		t.Fatalf("loadBenchmarkRetryDoc: %v", err)
	}
	got := buildCertifyResult(benchmarkRetryUnwrapEvent(doc), proofObligationsDoc{})
	if got.GovernanceCertification.Verdict != "artifact_mapping_missing" {
		t.Fatalf("verdict=%q", got.GovernanceCertification.Verdict)
	}
	if got.GovernanceCertification.Promotion != "blocked" {
		t.Fatalf("promotion=%q", got.GovernanceCertification.Promotion)
	}
}

func TestBuildCertifyResult_ForbiddenMoveBlocksHighScore(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "learning_event.yaml")
	body := `learning_event:
  id: learning.mode_d.globular.test
  task: globular-1
  promotion_allowed: true
  certification_status: certified_clean_repair
  repair_claim:
    id: claim.globular.config_write
    summary: Preserve token-gated config writes
    contract_ids:
      - contract.globular.config_write_requires_auth
    forbidden_move_ids:
      - forbidden.auth_bypass_for_config_write
  current:
    score: 100
  certification:
    certification_status: certified_clean_repair
    governing_contract_id: contract.globular.config_write_requires_auth
    frozen_contract_present: true
    contract_block_valid: true
    contract_block_maps_to_frozen_contract: true
    scope_valid: true
    evidence_sufficient: true
    required_paths_satisfied: true
    promotion_allowed: true
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := loadBenchmarkRetryDoc(path)
	if err != nil {
		t.Fatalf("loadBenchmarkRetryDoc: %v", err)
	}
	got := buildCertifyResult(benchmarkRetryUnwrapEvent(doc), proofObligationsDoc{})
	if got.GovernanceCertification.Verdict != "forbidden_move_detected" {
		t.Fatalf("verdict=%q", got.GovernanceCertification.Verdict)
	}
	if got.GovernanceCertification.Promotion != "blocked" {
		t.Fatalf("promotion=%q", got.GovernanceCertification.Promotion)
	}
	if got.GovernanceCertification.ScoreUsedForCertification {
		t.Fatal("score_used_for_certification=true, want false")
	}
}

func TestBuildCertifyResult_DetectedForbiddenMoveOverridesSatisfiedProof(t *testing.T) {
	event := map[string]any{
		"id":      "learning.mode_d.globular.test",
		"task":    "globular-1",
		"current": map[string]any{"score": 100},
		"repair_claim": map[string]any{
			"id":                    "claim.globular.gateway",
			"authority_surface_ids": []any{"candidate.authority.globular.gateway"},
		},
		"proof_mapping": map[string]any{
			"runtime": []any{"artifacts/runtime.log"},
		},
		"evidence_artifacts": []any{
			map[string]any{
				"id":                            "artifact.runtime_log",
				"kind":                          "runtime_log",
				"path":                          "artifacts/runtime.log",
				"related_authority_surface_ids": []any{"candidate.authority.globular.gateway"},
				"related_proof_obligation_ids":  []any{"proof.authority.globular.gateway"},
			},
			map[string]any{
				"id":                            "artifact.process_snapshot",
				"kind":                          "process_snapshot",
				"path":                          "artifacts/process.json",
				"related_authority_surface_ids": []any{"candidate.authority.globular.gateway"},
				"related_proof_obligation_ids":  []any{"proof.authority.globular.gateway"},
			},
			map[string]any{
				"id":                            "artifact.failure_evidence",
				"kind":                          "failure_evidence",
				"path":                          "artifacts/failure.txt",
				"related_authority_surface_ids": []any{"candidate.authority.globular.gateway"},
				"related_proof_obligation_ids":  []any{"proof.authority.globular.gateway"},
			},
		},
		"detected_forbidden_moves": []any{
			map[string]any{
				"id":     "forbidden.auth_bypass_for_config_write",
				"reason": "token validation removed before guarded mutation",
				"evidence": map[string]any{
					"kind": "diff",
					"path": "artifacts/patch.diff",
				},
			},
		},
		"certification": map[string]any{
			"certification_status":                   "certified_clean_repair",
			"governing_contract_id":                  "contract.globular.gateway",
			"frozen_contract_present":                true,
			"contract_block_valid":                   true,
			"contract_block_maps_to_frozen_contract": true,
			"scope_valid":                            true,
			"evidence_sufficient":                    true,
			"required_paths_satisfied":               true,
			"promotion_allowed":                      true,
		},
	}
	proofDoc := proofObligationsDoc{
		ProofObligations: []generatedProofObligation{{
			ID:                          "proof.authority.globular.gateway",
			DerivedFromAuthoritySurface: "candidate.authority.globular.gateway",
			AppliesToAuthoritySurfaces:  []string{"candidate.authority.globular.gateway"},
			EvidenceLane:                "runtime_required",
			RequiredSlots: []generatedProofSlot{
				{ID: "slot.gateway.runtime", Kind: "runtime", Required: true},
				{ID: "slot.gateway.process_artifact", Kind: "process_artifact", Required: true},
				{ID: "slot.gateway.log_artifact", Kind: "log_artifact", Required: true},
				{ID: "slot.gateway.failure_evidence", Kind: "failure_evidence", Required: true},
			},
		}},
	}
	got := buildCertifyResult(event, proofDoc)
	if got.GovernanceCertification.Verdict != "forbidden_move_detected" {
		t.Fatalf("verdict=%q", got.GovernanceCertification.Verdict)
	}
	if got.GovernanceCertification.Promotion != "blocked" {
		t.Fatalf("promotion=%q", got.GovernanceCertification.Promotion)
	}
	if len(got.GovernanceCertification.Obligations) != 1 || got.GovernanceCertification.Obligations[0].Status != "satisfied" {
		t.Fatalf("obligations=%+v", got.GovernanceCertification.Obligations)
	}
	if len(got.DetectedForbiddenMoves) != 1 {
		t.Fatalf("detected_forbidden_moves=%d, want 1", len(got.DetectedForbiddenMoves))
	}
	if got.DetectedForbiddenMoves[0].EvidencePath != "artifacts/patch.diff" {
		t.Fatalf("evidence path=%q", got.DetectedForbiddenMoves[0].EvidencePath)
	}
	if !containsStringCertify(got.GovernanceCertification.BlockedByForbiddenMoveIDs, "forbidden.auth_bypass_for_config_write") {
		t.Fatalf("blocked ids=%v", got.GovernanceCertification.BlockedByForbiddenMoveIDs)
	}
}

func TestBuildCertifyResult_ConsumesProofObligationsMissingSource(t *testing.T) {
	event := map[string]any{
		"id":   "learning.mode_d.globular.test",
		"task": "globular-1",
		"repair_claim": map[string]any{
			"id":                    "claim.globular.gateway",
			"authority_surface_ids": []any{"candidate.authority.globular.gateway"},
		},
		"current": map[string]any{"score": 83},
		"proof_mapping": map[string]any{
			"static": []any{"ref:static_guard"},
		},
		"certification": map[string]any{
			"certification_status":                   "evidence_mapping_missing",
			"governing_contract_id":                  "contract.globular.gateway",
			"frozen_contract_present":                true,
			"contract_block_valid":                   true,
			"contract_block_maps_to_frozen_contract": true,
			"scope_valid":                            true,
			"evidence_sufficient":                    true,
			"required_paths_satisfied":               true,
		},
	}
	proofDoc := proofObligationsDoc{
		ProofObligations: []generatedProofObligation{{
			ID:                          "proof.authority.globular.gateway",
			DerivedFromAuthoritySurface: "candidate.authority.globular.gateway",
			AppliesToAuthoritySurfaces:  []string{"candidate.authority.globular.gateway"},
			EvidenceLane:                "runtime_required",
			RequiredSlots: []generatedProofSlot{
				{ID: "slot.gateway.runtime", Kind: "runtime", Required: true},
				{ID: "slot.gateway.log_artifact", Kind: "log_artifact", Required: true},
			},
		}},
	}
	got := buildCertifyResult(event, proofDoc)
	if got.GovernanceCertification.Verdict != "runtime_evidence_missing" {
		t.Fatalf("verdict=%q", got.GovernanceCertification.Verdict)
	}
	if len(got.GovernanceCertification.Obligations) != 1 {
		t.Fatalf("obligations=%d, want 1", len(got.GovernanceCertification.Obligations))
	}
	if len(got.GovernanceCertification.Obligations[0].MissingSlots) != 2 {
		t.Fatalf("missing slots=%v", got.GovernanceCertification.Obligations[0].MissingSlots)
	}
}

func containsStringCertify(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestBuildCertifyResult_ConsumesProofObligationsUnmappedEvidence(t *testing.T) {
	event := map[string]any{
		"id":   "learning.mode_d.globular.test",
		"task": "globular-1",
		"repair_claim": map[string]any{
			"id":                    "claim.globular.config",
			"authority_surface_ids": []any{"candidate.authority.globular.config"},
		},
		"proof_mapping": map[string]any{
			"artifacts": []any{"verification.md"},
		},
		"certification": map[string]any{
			"certification_status":                   "evidence_mapping_missing",
			"governing_contract_id":                  "contract.globular.config",
			"frozen_contract_present":                true,
			"contract_block_valid":                   true,
			"contract_block_maps_to_frozen_contract": true,
			"scope_valid":                            true,
			"evidence_sufficient":                    true,
			"required_paths_satisfied":               true,
		},
	}
	proofDoc := proofObligationsDoc{
		ProofObligations: []generatedProofObligation{{
			ID:                          "proof.authority.globular.config",
			DerivedFromAuthoritySurface: "candidate.authority.globular.config",
			AppliesToAuthoritySurfaces:  []string{"candidate.authority.globular.config"},
			EvidenceLane:                "hybrid",
			RequiredSlots: []generatedProofSlot{
				{ID: "slot.config.artifact", Kind: "artifact", Required: true},
			},
		}},
	}
	got := buildCertifyResult(event, proofDoc)
	if got.GovernanceCertification.Verdict != "artifact_mapping_missing" {
		t.Fatalf("verdict=%q", got.GovernanceCertification.Verdict)
	}
	slot := got.GovernanceCertification.Obligations[0].SlotResults[0]
	if slot.Status != "available_unmapped" {
		t.Fatalf("slot status=%q, want available_unmapped", slot.Status)
	}
}

func TestBuildCertifyResult_ExplicitArtifactSatisfiesSlot(t *testing.T) {
	event := map[string]any{
		"id":   "learning.mode_d.globular.test",
		"task": "globular-1",
		"repair_claim": map[string]any{
			"id":                    "claim.globular.gateway",
			"authority_surface_ids": []any{"candidate.authority.globular.gateway"},
		},
		"evidence_artifacts": []any{
			map[string]any{
				"id":   "artifact.runtime_log",
				"kind": "runtime_log",
				"path": "artifacts/runtime.log",
				"satisfies": []any{
					map[string]any{
						"proof_obligation_id": "proof.authority.globular.gateway",
						"slot":                "log_artifact",
					},
				},
				"related_authority_surface_ids": []any{"candidate.authority.globular.gateway"},
				"related_proof_obligation_ids":  []any{"proof.authority.globular.gateway"},
			},
		},
		"certification": map[string]any{
			"certification_status":                   "evidence_mapping_missing",
			"governing_contract_id":                  "contract.globular.gateway",
			"frozen_contract_present":                true,
			"contract_block_valid":                   true,
			"contract_block_maps_to_frozen_contract": true,
			"scope_valid":                            true,
			"evidence_sufficient":                    true,
			"required_paths_satisfied":               true,
		},
	}
	proofDoc := proofObligationsDoc{
		ProofObligations: []generatedProofObligation{{
			ID:                          "proof.authority.globular.gateway",
			DerivedFromAuthoritySurface: "candidate.authority.globular.gateway",
			AppliesToAuthoritySurfaces:  []string{"candidate.authority.globular.gateway"},
			EvidenceLane:                "runtime_required",
			RequiredSlots: []generatedProofSlot{
				{ID: "slot.gateway.log_artifact", Kind: "log_artifact", Required: true},
			},
		}},
	}
	got := buildCertifyResult(event, proofDoc)
	slot := got.GovernanceCertification.Obligations[0].SlotResults[0]
	if slot.Status != "satisfied" {
		t.Fatalf("slot status=%q, want satisfied", slot.Status)
	}
	if slot.MappingSource != "explicit" {
		t.Fatalf("mapping source=%q, want explicit", slot.MappingSource)
	}
	if slot.ArtifactID != "artifact.runtime_log" {
		t.Fatalf("artifact id=%q", slot.ArtifactID)
	}
}

func TestBuildCertifyResult_InferredArtifactSatisfiesSlot(t *testing.T) {
	event := map[string]any{
		"id":   "learning.mode_d.globular.test",
		"task": "globular-1",
		"repair_claim": map[string]any{
			"id":                    "claim.globular.gateway",
			"authority_surface_ids": []any{"candidate.authority.globular.gateway"},
		},
		"evidence_artifacts": []any{
			map[string]any{
				"id":                            "artifact.runtime_log",
				"kind":                          "runtime_log",
				"path":                          "artifacts/runtime.log",
				"related_authority_surface_ids": []any{"candidate.authority.globular.gateway"},
			},
		},
		"certification": map[string]any{
			"certification_status":                   "evidence_mapping_missing",
			"governing_contract_id":                  "contract.globular.gateway",
			"frozen_contract_present":                true,
			"contract_block_valid":                   true,
			"contract_block_maps_to_frozen_contract": true,
			"scope_valid":                            true,
			"evidence_sufficient":                    true,
			"required_paths_satisfied":               true,
		},
	}
	proofDoc := proofObligationsDoc{
		ProofObligations: []generatedProofObligation{{
			ID:                          "proof.authority.globular.gateway",
			DerivedFromAuthoritySurface: "candidate.authority.globular.gateway",
			AppliesToAuthoritySurfaces:  []string{"candidate.authority.globular.gateway"},
			EvidenceLane:                "runtime_required",
			RequiredSlots: []generatedProofSlot{
				{ID: "slot.gateway.log_artifact", Kind: "log_artifact", Required: true},
			},
		}},
	}
	got := buildCertifyResult(event, proofDoc)
	slot := got.GovernanceCertification.Obligations[0].SlotResults[0]
	if slot.Status != "satisfied" {
		t.Fatalf("slot status=%q, want satisfied", slot.Status)
	}
	if slot.MappingSource != "inferred" {
		t.Fatalf("mapping source=%q, want inferred", slot.MappingSource)
	}
}

func TestBuildCertifyResult_HighScoreArtifactPresentNoMappingNotCertified(t *testing.T) {
	event := map[string]any{
		"id":      "learning.mode_d.globular.test",
		"task":    "globular-1",
		"current": map[string]any{"score": 100},
		"repair_claim": map[string]any{
			"id":                    "claim.globular.gateway",
			"authority_surface_ids": []any{"candidate.authority.globular.gateway"},
		},
		"evidence_artifacts": []any{
			map[string]any{
				"id":   "artifact.patch",
				"kind": "patch",
				"path": "artifacts/model.patch",
			},
		},
		"certification": map[string]any{
			"certification_status":                   "certified_clean_repair",
			"governing_contract_id":                  "contract.globular.gateway",
			"frozen_contract_present":                true,
			"contract_block_valid":                   true,
			"contract_block_maps_to_frozen_contract": true,
			"scope_valid":                            true,
			"evidence_sufficient":                    true,
			"required_paths_satisfied":               true,
			"promotion_allowed":                      true,
		},
	}
	proofDoc := proofObligationsDoc{
		ProofObligations: []generatedProofObligation{{
			ID:                          "proof.authority.globular.gateway",
			DerivedFromAuthoritySurface: "candidate.authority.globular.gateway",
			AppliesToAuthoritySurfaces:  []string{"candidate.authority.globular.gateway"},
			EvidenceLane:                "runtime_required",
			RequiredSlots: []generatedProofSlot{
				{ID: "slot.gateway.runtime", Kind: "runtime", Required: true},
			},
		}},
	}
	got := buildCertifyResult(event, proofDoc)
	if got.GovernanceCertification.Verdict == "certified_clean_repair" {
		t.Fatal("artifact presence plus high score must not certify the repair")
	}
	if got.GovernanceCertification.Promotion != "blocked" {
		t.Fatalf("promotion=%q, want blocked", got.GovernanceCertification.Promotion)
	}
}

func TestBuildCertifyResult_WrongArtifactKindStaysAvailableUnmapped(t *testing.T) {
	event := map[string]any{
		"id":   "learning.mode_d.globular.test",
		"task": "globular-1",
		"repair_claim": map[string]any{
			"id":                    "claim.globular.gateway",
			"authority_surface_ids": []any{"candidate.authority.globular.gateway"},
		},
		"evidence_artifacts": []any{
			map[string]any{
				"id":                            "artifact.process_snapshot",
				"kind":                          "process_snapshot",
				"path":                          "artifacts/process.json",
				"related_authority_surface_ids": []any{"candidate.authority.globular.gateway"},
			},
		},
		"certification": map[string]any{
			"certification_status":                   "evidence_mapping_missing",
			"governing_contract_id":                  "contract.globular.gateway",
			"frozen_contract_present":                true,
			"contract_block_valid":                   true,
			"contract_block_maps_to_frozen_contract": true,
			"scope_valid":                            true,
			"evidence_sufficient":                    true,
			"required_paths_satisfied":               true,
		},
	}
	proofDoc := proofObligationsDoc{
		ProofObligations: []generatedProofObligation{{
			ID:                          "proof.authority.globular.gateway",
			DerivedFromAuthoritySurface: "candidate.authority.globular.gateway",
			AppliesToAuthoritySurfaces:  []string{"candidate.authority.globular.gateway"},
			EvidenceLane:                "runtime_required",
			RequiredSlots: []generatedProofSlot{
				{ID: "slot.gateway.log_artifact", Kind: "log_artifact", Required: true},
			},
		}},
	}
	got := buildCertifyResult(event, proofDoc)
	slot := got.GovernanceCertification.Obligations[0].SlotResults[0]
	if slot.Status != "available_unmapped" {
		t.Fatalf("slot status=%q, want available_unmapped", slot.Status)
	}
	if slot.MappingSource != "" {
		t.Fatalf("mapping source=%q, want empty", slot.MappingSource)
	}
	if slot.ArtifactID != "" {
		t.Fatalf("artifact id=%q, want empty", slot.ArtifactID)
	}
}

func TestBuildCertifyResult_StaticOnlyObligationDoesNotReportRuntimeMissing(t *testing.T) {
	event := map[string]any{
		"id":   "learning.mode_d.globular.test",
		"task": "globular-1",
		"repair_claim": map[string]any{
			"id":                    "claim.globular.auth",
			"authority_surface_ids": []any{"candidate.authority.globular.auth"},
		},
		"certification": map[string]any{
			"certification_status":                   "evidence_mapping_missing",
			"governing_contract_id":                  "contract.globular.auth",
			"frozen_contract_present":                true,
			"contract_block_valid":                   true,
			"contract_block_maps_to_frozen_contract": true,
			"scope_valid":                            true,
			"evidence_sufficient":                    true,
			"required_paths_satisfied":               true,
		},
	}
	proofDoc := proofObligationsDoc{
		ProofObligations: []generatedProofObligation{{
			ID:                          "proof.authority.globular.auth",
			DerivedFromAuthoritySurface: "candidate.authority.globular.auth",
			AppliesToAuthoritySurfaces:  []string{"candidate.authority.globular.auth"},
			EvidenceLane:                "static_only",
			RequiredSlots: []generatedProofSlot{
				{ID: "slot.auth.static_guard", Kind: "static_guard", Required: true},
			},
		}},
	}
	got := buildCertifyResult(event, proofDoc)
	if got.GovernanceCertification.Verdict != "proof_missing" {
		t.Fatalf("verdict=%q, want proof_missing", got.GovernanceCertification.Verdict)
	}
	if got.GovernanceCertification.Lanes[2].Lane != "proof" || got.GovernanceCertification.Lanes[2].Status != "blocked" {
		t.Fatalf("proof lane=%+v", got.GovernanceCertification.Lanes[2])
	}
	if got.GovernanceCertification.Lanes[3].Lane != "evidence" || got.GovernanceCertification.Lanes[3].Status != "pass" {
		t.Fatalf("evidence lane=%+v", got.GovernanceCertification.Lanes[3])
	}
}

func TestBuildCertifyResult_HybridRuntimeSlotMissingReportsRuntimeEvidenceMissing(t *testing.T) {
	event := map[string]any{
		"id":   "learning.mode_d.globular.test",
		"task": "globular-1",
		"repair_claim": map[string]any{
			"id":                    "claim.globular.config",
			"authority_surface_ids": []any{"candidate.authority.globular.config"},
		},
		"proof_mapping": map[string]any{
			"static": []any{"ref:static_guard"},
		},
		"certification": map[string]any{
			"certification_status":                   "evidence_mapping_missing",
			"governing_contract_id":                  "contract.globular.config",
			"frozen_contract_present":                true,
			"contract_block_valid":                   true,
			"contract_block_maps_to_frozen_contract": true,
			"scope_valid":                            true,
			"evidence_sufficient":                    true,
			"required_paths_satisfied":               true,
		},
	}
	proofDoc := proofObligationsDoc{
		ProofObligations: []generatedProofObligation{{
			ID:                          "proof.authority.globular.config",
			DerivedFromAuthoritySurface: "candidate.authority.globular.config",
			AppliesToAuthoritySurfaces:  []string{"candidate.authority.globular.config"},
			EvidenceLane:                "hybrid",
			RequiredSlots: []generatedProofSlot{
				{ID: "slot.config.static_guard", Kind: "static_guard", Required: true},
				{ID: "slot.config.test_or_runtime", Kind: "test_or_runtime", Required: true},
			},
		}},
	}
	got := buildCertifyResult(event, proofDoc)
	if got.GovernanceCertification.Verdict != "runtime_evidence_missing" {
		t.Fatalf("verdict=%q, want runtime_evidence_missing", got.GovernanceCertification.Verdict)
	}
	slot := got.GovernanceCertification.Obligations[0].SlotResults[1]
	if slot.Kind != "test_or_runtime" || slot.Status != "missing_source" {
		t.Fatalf("slot=%+v", slot)
	}
}
