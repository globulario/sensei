// SPDX-License-Identifier: Apache-2.0

package main

// Phase 2B golden tests: Preflight and Briefing surface the safe repair route
// for a broken-system task. They run the full pipeline against the real
// compiled graph (embeddedSeedStore), so a regression in repair-plan retrieval
// fails CI. Assertions key on the repair plan ID, not prose.

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
	"github.com/globulario/awareness-graph/golang/rdf"
	"github.com/globulario/awareness-graph/golang/store"
)

func newRepairTestServer(t *testing.T) *server {
	t.Helper()
	invalidateImplementationPatternCacheForTest()
	invalidateAuthorityDomainCacheForTest()
	invalidateRepairPlanCacheForTest()
	return newServer(newEmbeddedSeedStore())
}

var repairPreflightCases = []struct {
	name     string
	task     string
	file     string
	wantPlan string
}{
	{
		name:     "repository installability",
		task:     "fix a stuck repository artifact that won't become installable",
		file:     "golang/repository/repository_server/publish_workflow.go",
		wantPlan: "globular.repair.repository_artifact_lifecycle_stuck",
	},
	{
		name:     "doctor remediation",
		task:     "remediate a cluster-doctor finding that needs a mutation",
		file:     "golang/cluster_doctor/cluster_doctor_server/handler_remediation_workflow.go",
		wantPlan: "globular.repair.doctor_finding_requires_remediation",
	},
	{
		name:     "workflow resume",
		task:     "fix a workflow run blocked on a failed step",
		file:     "golang/workflow/workflow_server/executor_resume.go",
		wantPlan: "globular.repair.workflow_resume_blocked_step",
	},
	{
		name:     "rbac access",
		task:     "repair an RBAC permission regression",
		file:     "golang/rbac/rbac_server/rbac_access.go",
		wantPlan: "globular.repair.rbac_permission_regression",
	},
}

func TestPreflightReturnsRepairPlanForTasks(t *testing.T) {
	requireCombinedSeed(t)
	for _, tc := range repairPreflightCases {
		t.Run(tc.name, func(t *testing.T) {
			s := newRepairTestServer(t)
			resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
				Task:  tc.task,
				Files: []string{tc.file},
				Mode:  awarenesspb.PreflightMode_PREFLIGHT_STANDARD,
			})
			if err != nil {
				t.Fatalf("Preflight: %v", err)
			}
			if !anyContains(resp.GetRequiredActions(), tc.wantPlan) {
				t.Errorf("required_actions missing repair plan %q\n  got: %v", tc.wantPlan, resp.GetRequiredActions())
			}
		})
	}
}

// Named cases the doc calls out individually — kept as explicit tests so a
// failure points at the exact surface.
func TestPreflightReturnsRepairPlanForRepositoryInstallabilityTask(t *testing.T) {
	requireCombinedSeed(t)
	assertPreflightRepairPlan(t, repairPreflightCases[0].task, repairPreflightCases[0].file, repairPreflightCases[0].wantPlan)
}
func TestPreflightReturnsRepairPlanForDoctorRemediationTask(t *testing.T) {
	requireCombinedSeed(t)
	assertPreflightRepairPlan(t, repairPreflightCases[1].task, repairPreflightCases[1].file, repairPreflightCases[1].wantPlan)
}
func TestPreflightReturnsRepairPlanForWorkflowResumeTask(t *testing.T) {
	requireCombinedSeed(t)
	assertPreflightRepairPlan(t, repairPreflightCases[2].task, repairPreflightCases[2].file, repairPreflightCases[2].wantPlan)
}
func TestPreflightReturnsRepairPlanForRBACAccessTask(t *testing.T) {
	requireCombinedSeed(t)
	assertPreflightRepairPlan(t, repairPreflightCases[3].task, repairPreflightCases[3].file, repairPreflightCases[3].wantPlan)
}

func assertPreflightRepairPlan(t *testing.T, task, file, wantPlan string) {
	t.Helper()
	s := newRepairTestServer(t)
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task: task, Files: []string{file}, Mode: awarenesspb.PreflightMode_PREFLIGHT_STANDARD,
	})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if !anyContains(resp.GetRequiredActions(), wantPlan) {
		t.Errorf("required_actions missing repair plan %q\n  got: %v", wantPlan, resp.GetRequiredActions())
	}
}

// Briefing for a file in an authority domain includes its repair plan, both in
// the referenced_ids and the prose.
func TestBriefingIncludesRepairPlanForAuthorityFile(t *testing.T) {
	requireCombinedSeed(t)
	s := newRepairTestServer(t)
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		File: "golang/repository/repository_server/publish_workflow.go",
	})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	wantRef := "repair_plan:globular.repair.repository_artifact_lifecycle_stuck"
	if !containsStr(resp.GetReferencedIds(), wantRef) {
		t.Errorf("referenced_ids missing %q\n  got: %v", wantRef, resp.GetReferencedIds())
	}
	if !strings.Contains(resp.GetProse(), "globular.repair.repository_artifact_lifecycle_stuck") {
		t.Errorf("prose missing repair plan id; prose:\n%s", resp.GetProse())
	}
}

func TestBriefingIncludesContractCenteredRepairContext(t *testing.T) {
	invalidateImplementationPatternCacheForTest()
	invalidateAuthorityDomainCacheForTest()
	invalidateRepairPlanCacheForTest()
	s := newServer(fakeStore{
		classFacts: func(_ context.Context, classIRI string, _ int) ([]store.ImpactFact, error) {
			if classIRI != rdf.ClassRepairPlan {
				return nil, nil
			}
			planIRI := rdf.MintIRI(rdf.ClassRepairPlan, "globular.repair.foreach_guard_before_collection")
			return []store.ImpactFact{
				{NodeIRI: planIRI, Predicate: rdf.PropLabel, Object: "Evaluate foreach guard before collection resolution"},
				{NodeIRI: planIRI, Predicate: rdf.PropStatus, Object: "active"},
				{NodeIRI: planIRI, Predicate: rdf.PropGovernedByContract, Object: rdf.MintIRI(rdf.ClassContract, "contract.workflow.foreach_guard_order"), ObjectIsIRI: true},
				{NodeIRI: planIRI, Predicate: rdf.PropExpressedBy, Object: rdf.MintIRI(rdf.ClassSourceFile, "golang/workflow/engine/engine.go"), ObjectIsIRI: true},
			}, nil
		},
	})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		File: "golang/workflow/engine/engine.go",
		Task: "repair foreach guard ordering",
	})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if !containsStr(resp.GetReferencedIds(), "repair_plan:globular.repair.foreach_guard_before_collection") {
		t.Fatalf("referenced_ids missing repair plan; got %v", resp.GetReferencedIds())
	}
	for _, want := range []string{
		"globular.repair.foreach_guard_before_collection",
		"contracts: contract.workflow.foreach_guard_order",
		"expressed by: golang/workflow/engine/engine.go",
	} {
		if !strings.Contains(resp.GetProse(), want) {
			t.Fatalf("briefing prose missing %q\n%s", want, resp.GetProse())
		}
	}
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
