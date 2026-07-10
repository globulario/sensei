// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGovernanceCertificationSpine_EndToEnd freezes the full local governance
// flow:
//  1. proof-plan exposes the runtime-required checklist for a lifecycle surface
//  2. certify blocks when runtime evidence is missing
//  3. certify passes when mapped evidence satisfies the obligation
//  4. certify blocks again when a forbidden move is detected, even at score 100
func TestGovernanceCertificationSpine_EndToEnd(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) string {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
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
      symbols:
        - startService
      controls_lifecycle:
        - start
      required_authority:
        - service_lifecycle_authority
`)
	write("docs/awareness/generated/proof_obligations.yaml", `proof_obligations:
  - id: proof.authority.demo.start_service
    label: Proof obligation for candidate.authority.demo.start_service
    status: candidate
    derived_from_status: candidate
    derived_from_authority_surface: candidate.authority.demo.start_service
    applies_to_authority_surfaces:
      - candidate.authority.demo.start_service
    evidence_lane: runtime_required
    template_kind: service_lifecycle
    required_slots:
      - id: slot.authority.demo.start_service.runtime
        kind: runtime
        description: Runtime evidence that the lifecycle transition occurred as intended.
        required: true
      - id: slot.authority.demo.start_service.process_artifact
        kind: process_artifact
        description: Process artifact for the controlled service state.
        required: true
      - id: slot.authority.demo.start_service.log_artifact
        kind: log_artifact
        description: Log artifact confirming the lifecycle action.
        required: true
      - id: slot.authority.demo.start_service.failure_evidence
        kind: failure_evidence
        description: Failure handling evidence for non-clean lifecycle transitions.
        required: true
    notes: "Lifecycle authority is runtime-governed: score alone must never certify it."
`)
	write("docs/awareness/architecture/forbidden_fixes.yaml", `forbidden_fixes:
  - id: remove_runtime_guard_before_start
    title: Remove runtime guard before start
    summary: Start the service without preserving the runtime guard.
    reason: It bypasses the guarded lifecycle contract and can make a green-looking patch unpromotable.
    protects:
      files:
        - service.go
`)

	missingEvent := write("events/missing.yaml", `learning_event:
  id: learning.mode_d.demo.missing
  task: demo-missing
  repair_claim:
    id: claim.demo.start_service
    authority_surface_ids:
      - candidate.authority.demo.start_service
  certification:
    certification_status: evidence_mapping_missing
    governing_contract_id: contract.demo.start_service
    frozen_contract_present: true
    contract_block_valid: true
    contract_block_maps_to_frozen_contract: true
    scope_valid: true
    evidence_sufficient: true
    required_paths_satisfied: true
`)
	cleanEvent := write("events/clean.yaml", `learning_event:
  id: learning.mode_d.demo.clean
  task: demo-clean
  current:
    score: 100
  repair_claim:
    id: claim.demo.start_service
    authority_surface_ids:
      - candidate.authority.demo.start_service
  evidence_artifacts:
    - id: artifact.runtime_log
      kind: runtime_log
      path: artifacts/runtime.log
      related_authority_surface_ids:
        - candidate.authority.demo.start_service
      related_proof_obligation_ids:
        - proof.authority.demo.start_service
    - id: artifact.process_snapshot
      kind: process_snapshot
      path: artifacts/process.json
      related_authority_surface_ids:
        - candidate.authority.demo.start_service
      related_proof_obligation_ids:
        - proof.authority.demo.start_service
    - id: artifact.failure_evidence
      kind: failure_evidence
      path: artifacts/failure.txt
      related_authority_surface_ids:
        - candidate.authority.demo.start_service
      related_proof_obligation_ids:
        - proof.authority.demo.start_service
  certification:
    certification_status: certified_clean_repair
    governing_contract_id: contract.demo.start_service
    frozen_contract_present: true
    contract_block_valid: true
    contract_block_maps_to_frozen_contract: true
    scope_valid: true
    evidence_sufficient: true
    required_paths_satisfied: true
    promotion_allowed: true
`)
	forbiddenEvent := write("events/forbidden.yaml", `learning_event:
  id: learning.mode_d.demo.forbidden
  task: demo-forbidden
  current:
    score: 100
  repair_claim:
    id: claim.demo.start_service
    authority_surface_ids:
      - candidate.authority.demo.start_service
  evidence_artifacts:
    - id: artifact.runtime_log
      kind: runtime_log
      path: artifacts/runtime.log
      related_authority_surface_ids:
        - candidate.authority.demo.start_service
      related_proof_obligation_ids:
        - proof.authority.demo.start_service
    - id: artifact.process_snapshot
      kind: process_snapshot
      path: artifacts/process.json
      related_authority_surface_ids:
        - candidate.authority.demo.start_service
      related_proof_obligation_ids:
        - proof.authority.demo.start_service
    - id: artifact.failure_evidence
      kind: failure_evidence
      path: artifacts/failure.txt
      related_authority_surface_ids:
        - candidate.authority.demo.start_service
      related_proof_obligation_ids:
        - proof.authority.demo.start_service
  detected_forbidden_moves:
    - id: remove_runtime_guard_before_start
      reason: lifecycle guard removed before service start
      evidence:
        kind: diff
        path: artifacts/patch.diff
  certification:
    certification_status: certified_clean_repair
    governing_contract_id: contract.demo.start_service
    frozen_contract_present: true
    contract_block_valid: true
    contract_block_maps_to_frozen_contract: true
    scope_valid: true
    evidence_sufficient: true
    required_paths_satisfied: true
    promotion_allowed: true
`)

	proofPlanCode, proofPlanOut, proofPlanErr := captureStdoutStderr(t, func() int {
		return runProofPlan([]string{
			"--repo-root", root,
			"--authority-surface-id", "candidate.authority.demo.start_service",
		})
	})
	if proofPlanCode != 0 {
		t.Fatalf("proof-plan exit=%d stderr=%q", proofPlanCode, proofPlanErr)
	}
	for _, want := range []string{
		"Proof plan: candidate.authority.demo.start_service",
		"evidence_lane: runtime_required",
		"required_slots: runtime, process_artifact, log_artifact, failure_evidence",
		"Forbidden moves:",
		"remove_runtime_guard_before_start",
	} {
		if !strings.Contains(proofPlanOut, want) {
			t.Fatalf("proof-plan missing %q:\n%s", want, proofPlanOut)
		}
	}

	missingCode, missingOut, missingErr := captureStdoutStderr(t, func() int {
		return runCertify([]string{
			"--event", missingEvent,
			"--proof-obligations", filepath.Join(root, "docs", "awareness", "generated", "proof_obligations.yaml"),
		})
	})
	if missingCode != 0 {
		t.Fatalf("certify missing exit=%d stderr=%q", missingCode, missingErr)
	}
	for _, want := range []string{
		"Certification: runtime_evidence_missing",
		"missing_slots: runtime, process_artifact, log_artifact, failure_evidence",
	} {
		if !strings.Contains(missingOut, want) {
			t.Fatalf("missing certify output missing %q:\n%s", want, missingOut)
		}
	}

	cleanCode, cleanOut, cleanErr := captureStdoutStderr(t, func() int {
		return runCertify([]string{
			"--event", cleanEvent,
			"--proof-obligations", filepath.Join(root, "docs", "awareness", "generated", "proof_obligations.yaml"),
		})
	})
	if cleanCode != 0 {
		t.Fatalf("certify clean exit=%d stderr=%q", cleanCode, cleanErr)
	}
	for _, want := range []string{
		"Certification: certified_clean_repair",
		"Promotion: allowed",
		"Score used for certification: false",
	} {
		if !strings.Contains(cleanOut, want) {
			t.Fatalf("clean certify output missing %q:\n%s", want, cleanOut)
		}
	}

	forbiddenCode, forbiddenOut, forbiddenErr := captureStdoutStderr(t, func() int {
		return runCertify([]string{
			"--event", forbiddenEvent,
			"--proof-obligations", filepath.Join(root, "docs", "awareness", "generated", "proof_obligations.yaml"),
		})
	})
	if forbiddenCode != 0 {
		t.Fatalf("certify forbidden exit=%d stderr=%q", forbiddenCode, forbiddenErr)
	}
	for _, want := range []string{
		"Certification: forbidden_move_detected",
		"Promotion: blocked",
		"Detected forbidden moves:",
		"remove_runtime_guard_before_start: lifecycle guard removed before service start",
	} {
		if !strings.Contains(forbiddenOut, want) {
			t.Fatalf("forbidden certify output missing %q:\n%s", want, forbiddenOut)
		}
	}
}

func captureStdoutStderr(t *testing.T, fn func() int) (int, string, string) {
	t.Helper()
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}

	os.Stdout = outW
	os.Stderr = errW
	code := fn()
	_ = outW.Close()
	_ = errW.Close()

	var outBuf bytes.Buffer
	var errBuf bytes.Buffer
	_, _ = io.Copy(&outBuf, outR)
	_, _ = io.Copy(&errBuf, errR)
	_ = outR.Close()
	_ = errR.Close()
	return code, outBuf.String(), errBuf.String()
}
