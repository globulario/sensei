// SPDX-License-Identifier: AGPL-3.0-only

package main

// Phase 2I tests: change-impact planning predicts what a proposed edit affects,
// against the real compiled seed.

import (
	"context"
	"testing"
)

func planFor(t *testing.T, task string, files ...string) *ChangeImpactPlan {
	t.Helper()
	invalidateImplementationPatternCacheForTest()
	invalidateAuthorityDomainCacheForTest()
	invalidateRepairPlanCacheForTest()
	invalidateRuntimeEvidenceCacheForTest()
	s := newServer(newEmbeddedSeedStore())
	plan, err := s.planChangeImpact(context.Background(), task, files)
	if err != nil {
		t.Fatalf("planChangeImpact: %v", err)
	}
	return plan
}

func sliceHas(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestImpactPlanningForRepositoryPublishChange(t *testing.T) {
	requireCombinedSeed(t)
	plan := planFor(t, "change repository publish workflow installability behavior",
		"golang/repository/repository_server/publish_workflow.go")
	if !sliceHas(plan.AffectedServices, "repository") {
		t.Errorf("expected repository service, got %v", plan.AffectedServices)
	}
	if !sliceHas(plan.AffectedAuthorityDomains, "authority.repository_artifact_metadata") {
		t.Errorf("expected repository authority domain, got %v", plan.AffectedAuthorityDomains)
	}
	if !sliceHas(plan.AffectedRepairPlans, "globular.repair.repository_artifact_lifecycle_stuck") {
		t.Errorf("expected repository repair plan, got %v", plan.AffectedRepairPlans)
	}
	if !sliceHas(plan.AffectedInvariants, "repository.artifact.installable_compound_predicate") {
		t.Errorf("expected installability invariant, got %v", plan.AffectedInvariants)
	}
	if len(plan.AffectedStateObjects) == 0 {
		t.Errorf("expected affected state objects from the authority domain")
	}
	if plan.BlastRadius == "" || plan.ApprovalGate == "" {
		t.Errorf("expected blast radius + approval gate, got %q/%q", plan.BlastRadius, plan.ApprovalGate)
	}
}

func TestImpactPlanningForWorkflowResumeChange(t *testing.T) {
	requireCombinedSeed(t)
	plan := planFor(t, "modify workflow resume after a failed step",
		"golang/workflow/workflow_server/executor_resume.go")
	if !sliceHas(plan.AffectedServices, "workflow") {
		t.Errorf("expected workflow service, got %v", plan.AffectedServices)
	}
	if !sliceHas(plan.AffectedRepairPlans, "globular.repair.workflow_resume_blocked_step") {
		t.Errorf("expected workflow repair plan, got %v", plan.AffectedRepairPlans)
	}
}

func TestImpactPlanningForDoctorRuleChange(t *testing.T) {
	requireCombinedSeed(t)
	plan := planFor(t, "add a new cluster-doctor rule",
		"golang/cluster_doctor/cluster_doctor_server/rules/repository_findings.go")
	if !sliceHas(plan.AffectedServices, "cluster_doctor") {
		t.Errorf("expected cluster_doctor service, got %v", plan.AffectedServices)
	}
	if !sliceHas(plan.AffectedRepairPlans, "globular.repair.doctor_finding_requires_remediation") {
		t.Errorf("expected doctor repair plan, got %v", plan.AffectedRepairPlans)
	}
}

func TestImpactPlanningForRBACChange(t *testing.T) {
	requireCombinedSeed(t)
	plan := planFor(t, "change RBAC access validation",
		"golang/rbac/rbac_server/rbac_access.go")
	if !sliceHas(plan.AffectedServices, "rbac") {
		t.Errorf("expected rbac service, got %v", plan.AffectedServices)
	}
	if !sliceHas(plan.AffectedAuthorityDomains, "authority.rbac_permissions") {
		t.Errorf("expected rbac authority domain, got %v", plan.AffectedAuthorityDomains)
	}
	if plan.ApprovalGate != "human_approval_required" && plan.ApprovalGate != "multi_step_approval_required" && plan.ApprovalGate != "manual_only" {
		t.Errorf("rbac change should require human approval, got %q", plan.ApprovalGate)
	}
}

func TestImpactPlanningReportsUnknowns(t *testing.T) {
	// A high-risk directory file with no authority domain and no anchors.
	s := newServer(nopStore{})
	plan, err := s.planChangeImpact(context.Background(), "edit an mcp tool",
		[]string{"golang/mcp/mcp_server/tools.go"})
	if err != nil {
		t.Fatalf("planChangeImpact: %v", err)
	}
	if len(plan.Unknowns) == 0 {
		t.Errorf("expected unknowns for a high-risk file with no authority/anchors")
	}
}
