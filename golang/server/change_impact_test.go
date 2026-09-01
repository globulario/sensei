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
// THE OWED FIXTURE, NOW BUILT. The earlier version of this test used
// golang/workflow/engine.go, which matches a path-class rule in
// assessChangeRisk, so the escalation fired anyway and reverting the repair
// still passed. The fixture it needed is a file AnyFileHighRiskWeighted treats
// as high risk that matches NONE of those rules: FileRiskTier returns RiskHigh
// for authority-domain membership alone, and golang/server/ is in no
// path-class prefix. So the domain makes the file high-risk and contributes
// nothing else. Counting the merged `forbidden` set as coverage now measurably
// suppresses the thin-coverage escalation.
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
		CoversPaths:   []string{"golang/server/"},
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
		"remediate a doctor.finding_requires_mutation", []string{"golang/server/change_impact.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !sliceHas(plan.AffectedAuthorityDomains, "authority.some_domain") {
		t.Fatalf("fixture did not match the authority domain, so this test proves nothing: %+v", plan.AffectedAuthorityDomains)
	}
	if plan.ApprovalGate == "manual_only" {
		t.Fatalf("bypass guidance was counted as coverage and let a task-matched plan vote: approval=%q", plan.ApprovalGate)
	}
	// The half that was previously unprovable: with no real anchor, coverage is
	// thin and a high-risk file must escalate. Counting the synthetic
	// authority_bypass entry as an anchor makes coverage sufficient and this
	// escalation silently disappears.
	if plan.ApprovalGate != "review_required" {
		t.Fatalf("thin coverage did not escalate a high-risk file: approval=%q blast=%q",
			plan.ApprovalGate, plan.BlastRadius)
	}
}

// seedAuthorityDomains installs authority domains for one test. Same reason as
// seedRepairPlans: the loader reads a package-level cache, not the store.
func seedAuthorityDomains(t *testing.T, domains ...loadedAuthorityDomain) {
	t.Helper()
	invalidateAuthorityDomainCacheForTest()
	globalAuthorityDomainCache.mu.Lock()
	globalAuthorityDomainCache.loaded = true
	globalAuthorityDomainCache.domains = domains
	globalAuthorityDomainCache.mu.Unlock()
	t.Cleanup(invalidateAuthorityDomainCacheForTest)
}

// A retired anchor set is a DETERMINED result, not a missing one.
//
// The index fallback exists (#220) to answer "did the graph ever look at this
// file" for a file with no anchors at all. It queries the same SourceFile
// subject that produced the anchors, so for a retired-only file it always says
// yes -- re-admitting as examined a file whose governance was deliberately
// withdrawn, making coverage sufficient, and suppressing the thin-coverage
// escalation on a file that is high-risk by authority membership and matches
// no path-class rule. Found by review at 2f38ae57, not by the audit.
//
// Table-driven over EVERY class that counts as a governed anchor. The first
// version covered only retired invariants, so narrowing the raw count to
// invariants/failure-modes/intents still passed it while a retired-only
// forbidden fix or contract was handed to the index and resurrected (#318
// review). A falsifier that covers one member of a closed set does not
// establish the rule for the set.
func TestRetiredOnlyAnchorsAreNotReExaminedByTheIndex(t *testing.T) {
	for _, class := range []struct{ name, iri string }{
		{"invariant", rdf.ClassInvariant},
		{"forbidden fix", rdf.ClassForbiddenFix},
		{"contract", rdf.ClassContract},
	} {
		t.Run(class.name, func(t *testing.T) {
			retiredOnlyAnchorEscalates(t, class.iri)
		})
	}
}

func retiredOnlyAnchorEscalates(t *testing.T, class string) {
	t.Helper()
	invalidateRepairPlanCacheForTest()
	t.Cleanup(invalidateRepairPlanCacheForTest)
	seedAuthorityDomains(t, loadedAuthorityDomain{
		ID:          "authority.some_domain",
		CoversPaths: []string{"golang/server/"},
	})

	s := newTestServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return statusAnchorFacts(class, "retired.rule", "Retired rule", "critical", "retired"), nil
		},
		// The graph does hold a SourceFile node for this path -- that is
		// exactly the condition that made the fallback re-admit it.
		sourceFileIRIs: func(_ context.Context, _ string) ([]string, error) {
			return []string{"urn:test:sourcefile:1"}, nil
		},
	})
	plan, err := s.planChangeImpact(context.Background(),
		"adjust change impact planning", []string{"golang/server/change_impact.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !sliceHas(plan.AffectedAuthorityDomains, "authority.some_domain") {
		t.Fatalf("fixture did not match the domain, so the file is not high-risk and this proves nothing: %+v",
			plan.AffectedAuthorityDomains)
	}
	if plan.ApprovalGate != "review_required" {
		t.Fatalf("a retired-only file was re-admitted as examined and its escalation vanished: approval=%q blast=%q",
			plan.ApprovalGate, plan.BlastRadius)
	}
}

// The safety signals read PRIMARY anchors, so a retired anchor cannot answer
// "who owns this file". golang/mcp/ is high-risk by directory and matches none
// of the path-class rules, so the owner-unknown escalation is isolated here:
// deriving hasAnchors from the raw lists makes it disappear.
func TestSafetySignalsDoNotAcceptARetiredOwner(t *testing.T) {
	invalidateRepairPlanCacheForTest()
	t.Cleanup(invalidateRepairPlanCacheForTest)
	seedAuthorityDomains(t) // no domains: the owner is genuinely unknown

	s := newTestServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return statusAnchorFacts(rdf.ClassInvariant, "retired.rule", "Retired rule", "critical", "retired"), nil
		},
	})
	plan, err := s.planChangeImpact(context.Background(),
		"adjust the mcp surface", []string{"golang/mcp/server.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !sliceHas(plan.Unknowns, "authority owner unknown for a high-risk file") {
		t.Fatalf("a retired anchor answered the ownership question: unknowns=%v", plan.Unknowns)
	}
}

// DirectArchitecture carries components, boundaries, decisions, evidence and
// patterns as well as contracts. Only a contract is a class a repair plan can
// name, so admitting the rest lets a plan that names a COMPONENT id claim
// applicability it was never granted.
func TestNonContractArchitectureDoesNotAnchorAPlan(t *testing.T) {
	seedRepairPlans(t, loadedRepairPlan{
		ID: "plan.contract_governed", BlastRadius: "cluster", ApprovalGate: "manual_only",
		FindingClasses:    []string{"doctor.finding_requires_mutation"},
		GovernedContracts: []string{"some.architecture.node"},
	})
	seedAuthorityDomains(t)

	s := newTestServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			// A Component, not a Contract, carrying the id the plan names.
			return anchorFacts(rdf.ClassComponent, "some.architecture.node", "A component", "high"), nil
		},
	})
	plan, err := s.planChangeImpact(context.Background(),
		"remediate a doctor.finding_requires_mutation", []string{"golang/server/change_impact.go"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ApprovalGate == "manual_only" {
		t.Fatalf("a component id anchored a plan that only names contracts: approval=%q blast=%q",
			plan.ApprovalGate, plan.BlastRadius)
	}
}

// NOT COVERED, STATED. A retired-only anchor must not suppress the unknown-owner
// and thin-coverage fallbacks. Proving it needs a file that AnyFileHighRiskWeighted
// treats as high risk WITHOUT matching one of the path-class rules in
// assessChangeRisk -- otherwise the path rule escalates and the test passes
// regardless of the safety signals. A first attempt used golang/rbac/, which is
// exactly such a path, so it proved nothing and was removed rather than kept.
