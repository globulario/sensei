// SPDX-License-Identifier: AGPL-3.0-only

package main

// Phase 2J — final-form golden scenarios. End-to-end usefulness tests that
// simulate real agent repair tasks against the real compiled seed, asserting
// the composite awareness output (authority domain, implementation pattern,
// repair plan, evidence requirement, blast radius, approval gate, forbidden
// fixes, and the outcome hook). A regression in agent usefulness fails CI.

import (
	"context"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// preflightScenario runs the full Preflight against the real seed.
func preflightScenario(t *testing.T, task, file string) *awarenesspb.PreflightResponse {
	t.Helper()
	invalidateImplementationPatternCacheForTest()
	invalidateAuthorityDomainCacheForTest()
	invalidateIntentTriggerCacheForTest()
	invalidateRepairPlanCacheForTest()
	invalidateRuntimeEvidenceCacheForTest()
	s := newServer(newEmbeddedSeedStore())
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task: task, Files: []string{file}, Mode: awarenesspb.PreflightMode_PREFLIGHT_STANDARD,
	})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	return resp
}

func actionsHave(resp *awarenesspb.PreflightResponse, sub string) bool {
	return anyContains(resp.GetRequiredActions(), sub)
}
func forbiddenHave(resp *awarenesspb.PreflightResponse, sub string) bool {
	return anyContains(resp.GetForbiddenFixes(), sub)
}
func patternsHave(resp *awarenesspb.PreflightResponse, id string) bool {
	return hasPatternID(resp, id)
}

func TestAwarenessFinalForm_RepositorySplitBrainRepair(t *testing.T) {
	requireCombinedSeed(t)
	resp := preflightScenario(t,
		"repair a repository split-brain where the artifact won't become installable",
		"golang/repository/repository_server/publish_workflow.go")
	checks := map[string]bool{
		"authority domain":       actionsHave(resp, "Repository artifact metadata"),
		"implementation pattern": patternsHave(resp, "implementation_pattern:globular.pattern.repository_metadata_authority"),
		"repair plan":            actionsHave(resp, "globular.repair.repository_artifact_lifecycle_stuck"),
		"evidence requirement":   actionsHave(resp, "Evidence required"),
		"blast/approval":         actionsHave(resp, "Change risk: blast="),
		"forbidden bypass":       forbiddenHave(resp, "object presence in MinIO or CAS as installability authority"),
		"outcome hook":           actionsHave(resp, "record outcome"),
	}
	assertAll(t, checks)
}

func TestAwarenessFinalForm_DoctorRemediationPath(t *testing.T) {
	requireCombinedSeed(t)
	resp := preflightScenario(t,
		"remediate a cluster-doctor finding that requires a mutation",
		"golang/cluster_doctor/cluster_doctor_server/handler_remediation_workflow.go")
	checks := map[string]bool{
		"repair plan":      actionsHave(resp, "globular.repair.doctor_finding_requires_remediation"),
		"approval gate":    actionsHave(resp, "approval=human_approval_required"),
		"forbidden bypass": forbiddenHave(resp, "doctor rule executing mutation inside Evaluate"),
		"evidence":         actionsHave(resp, "Evidence required"),
		"outcome hook":     actionsHave(resp, "record outcome"),
	}
	assertAll(t, checks)
}

func TestAwarenessFinalForm_WorkflowBlockedResume(t *testing.T) {
	requireCombinedSeed(t)
	resp := preflightScenario(t,
		"fix a workflow run blocked on a failed step",
		"golang/workflow/workflow_server/executor_resume.go")
	checks := map[string]bool{
		"repair plan":            actionsHave(resp, "globular.repair.workflow_resume_blocked_step"),
		"implementation pattern": patternsHave(resp, "implementation_pattern:globular.pattern.workflow_durable_step_receipt"),
		"blast/approval":         actionsHave(resp, "Change risk: blast="),
		"outcome hook":           actionsHave(resp, "record outcome"),
	}
	assertAll(t, checks)
}

func TestAwarenessFinalForm_RBACDenyRegression(t *testing.T) {
	requireCombinedSeed(t)
	resp := preflightScenario(t,
		"repair an RBAC permission regression",
		"golang/rbac/rbac_server/rbac_access.go")
	checks := map[string]bool{
		"repair plan":      actionsHave(resp, "globular.repair.rbac_permission_regression"),
		"approval gate":    actionsHave(resp, "approval=human_approval_required"),
		"forbidden bypass": forbiddenHave(resp, "explicit-deny check"),
		"evidence":         actionsHave(resp, "Evidence required"),
		"outcome hook":     actionsHave(resp, "record outcome"),
	}
	assertAll(t, checks)
}

func TestAwarenessFinalForm_RuntimeEvidenceConflict(t *testing.T) {
	requireCombinedSeed(t)
	resp := preflightScenario(t,
		"handle stale or conflicting runtime evidence in the doctor collector",
		"golang/cluster_doctor/cluster_doctor_server/collector/snapshot.go")
	checks := map[string]bool{
		"repair plan":      actionsHave(resp, "globular.repair.runtime_evidence_stale_or_conflicting"),
		"authority domain": actionsHave(resp, "Runtime evidence"),
		"outcome hook":     actionsHave(resp, "record outcome"),
	}
	assertAll(t, checks)
}

func TestAwarenessFinalForm_UnknownHighRiskFile(t *testing.T) {
	// nopStore: no graph facts. A high-risk-dir file with nothing anchored must
	// degrade and escalate — the agent must not read silence as safety.
	invalidateImplementationPatternCacheForTest()
	invalidateAuthorityDomainCacheForTest()
	invalidateIntentTriggerCacheForTest()
	invalidateRepairPlanCacheForTest()
	invalidateRuntimeEvidenceCacheForTest()
	s := newServer(nopStore{})
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "edit an mcp tool",
		Files: []string{"golang/mcp/mcp_server/tools.go"},
		Mode:  awarenesspb.PreflightMode_PREFLIGHT_STANDARD,
	})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if resp.GetStatus() != awarenesspb.PreflightStatus_PREFLIGHT_STATUS_DEGRADED {
		t.Errorf("unknown high-risk file must DEGRADE, got %v", resp.GetStatus())
	}
	if anyContains(resp.GetRequiredActions(), "approval=none") {
		t.Errorf("unknown high-risk file must not say approval=none; got %v", resp.GetRequiredActions())
	}
	if len(resp.GetBlindSpots()) == 0 {
		t.Errorf("unknown high-risk file must surface blind spots")
	}
}

func assertAll(t *testing.T, checks map[string]bool) {
	t.Helper()
	for name, ok := range checks {
		if !ok {
			t.Errorf("scenario missing: %s", name)
		}
	}
}
