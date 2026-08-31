// SPDX-License-Identifier: AGPL-3.0-only

package main

// Phase 2I tests: change-impact planning predicts what a proposed edit affects,
// against the real compiled seed.

import (
	"context"
	"testing"

	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store"
)

func planFor(t *testing.T, task string, files ...string) *ChangeImpactPlan {
	t.Helper()
	invalidateImplementationPatternCacheForTest()
	invalidateAuthorityDomainCacheForTest()
	invalidateRepairPlanCacheForTest()
	invalidateRuntimeEvidenceCacheForTest()
	s := newTestServer(newEmbeddedSeedStore())
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
	s := newTestServer(nopStore{})
	plan, err := s.planChangeImpact(context.Background(), "edit an mcp tool",
		[]string{"golang/mcp/mcp_server/tools.go"})
	if err != nil {
		t.Fatalf("planChangeImpact: %v", err)
	}
	if len(plan.Unknowns) == 0 {
		t.Errorf("expected unknowns for a high-risk file with no authority/anchors")
	}
}

// A file whose only primary anchor is a contract is still an anchored file.
//
// Coverage here counted invariants and failure modes only, so such a file read
// as uncovered and this surface rejected an applicability preflight would have
// granted for the same change -- the two surfaces disagreeing about one
// candidate's authority.
func TestChangeImpactCountsContractAnchorsAsCoverage(t *testing.T) {
	invalidateRepairPlanCacheForTest()
	globalRepairPlanCache.mu.Lock()
	globalRepairPlanCache.loaded = true
	globalRepairPlanCache.plans = []loadedRepairPlan{{
		ID: "plan.contract_governed", BlastRadius: "cluster", ApprovalGate: "manual_only",
		FindingClasses:    []string{"doctor.finding_requires_mutation"},
		GovernedContracts: []string{"some.governed.contract"},
	}}
	globalRepairPlanCache.mu.Unlock()
	t.Cleanup(invalidateRepairPlanCacheForTest)

	s := newTestServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return anchorFacts(rdf.ClassContract, "some.governed.contract", "A governed contract", "high"), nil
		},
	})
	plan, err := s.planChangeImpact(context.Background(),
		"remediate a doctor.finding_requires_mutation", []string{"golang/workflow/engine.go"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ApprovalGate != "manual_only" {
		t.Fatalf("a contract-anchored file did not establish applicability: approval=%q blast=%q",
			plan.ApprovalGate, plan.BlastRadius)
	}
}

// Authority-domain guidance is not coverage.
//
// `forbidden` carries synthetic "authority_bypass:" entries appended for
// presentation alongside real forbidden-fix anchors, and counting the merged set
// as coverage let a domain match manufacture coverage for a file that holds no
// governed anchor at all.
//
// WHAT THIS PROVES, AND WHAT IT DOES NOT. It proves the plan gets no vote here.
// It does NOT prove the other half of the finding -- that inflated coverage
// suppresses the thin-coverage escalation -- because reaching that needs a file
// AnyFileHighRiskWeighted treats as high risk that ALSO matches none of the
// path-class rules in assessChangeRisk; with a path match the escalation fires
// anyway and the test passes regardless. Reverting coverage to the merged set
// still passes this test for that reason. The fixture is owed.
func TestAuthorityBypassGuidanceIsNotCoverage(t *testing.T) {
	invalidateRepairPlanCacheForTest()
	invalidateAuthorityDomainCacheForTest()
	globalRepairPlanCache.mu.Lock()
	globalRepairPlanCache.loaded = true
	globalRepairPlanCache.plans = []loadedRepairPlan{{
		ID: "plan.bypass", BlastRadius: "cluster", ApprovalGate: "manual_only",
		FindingClasses:      []string{"doctor.finding_requires_mutation"},
		PreservedInvariants: []string{"nothing.this.file.holds"},
	}}
	globalRepairPlanCache.mu.Unlock()
	globalAuthorityDomainCache.mu.Lock()
	globalAuthorityDomainCache.loaded = true
	globalAuthorityDomainCache.domains = []loadedAuthorityDomain{{
		ID:            "authority.some_domain",
		CoversPaths:   []string{"golang/workflow/"},
		ForbidsBypass: []string{"direct_etcd_write"},
	}}
	globalAuthorityDomainCache.mu.Unlock()
	t.Cleanup(func() {
		invalidateRepairPlanCacheForTest()
		invalidateAuthorityDomainCacheForTest()
	})

	// The file holds no governed anchor. The only thing in `forbidden` will be
	// the domain's bypass guidance, which is not evidence about this file.
	s := newTestServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) { return nil, nil },
	})
	plan, err := s.planChangeImpact(context.Background(),
		"remediate a doctor.finding_requires_mutation", []string{"golang/workflow/engine.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !sliceHas(plan.AffectedAuthorityDomains, "authority.some_domain") {
		t.Fatalf("fixture did not match the authority domain, so this test proves nothing: %+v", plan.AffectedAuthorityDomains)
	}
	if plan.ApprovalGate == "manual_only" {
		t.Fatalf("bypass guidance was counted as coverage and let a task-matched plan vote: approval=%q", plan.ApprovalGate)
	}
}

// NOT COVERED, STATED. A retired-only anchor must not suppress the unknown-owner
// and thin-coverage fallbacks. Proving it needs a file that AnyFileHighRiskWeighted
// treats as high risk WITHOUT matching one of the path-class rules in
// assessChangeRisk -- otherwise the path rule escalates and the test passes
// regardless of the safety signals. A first attempt used golang/rbac/, which is
// exactly such a path, so it proved nothing and was removed rather than kept.
