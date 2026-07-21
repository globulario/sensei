// SPDX-License-Identifier: AGPL-3.0-only

package main

// Task-only repair-plan matching (dogfooding probe P4). An operator mid-incident
// has a symptom, not a file path — the plan must surface from unauthored,
// natural phrasing via when_to_use triggers, and unrelated tasks must surface
// nothing (no false positives to erode trust).

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func taskOnlyPreflight(t *testing.T, task string) *awarenesspb.PreflightResponse {
	t.Helper()
	invalidateImplementationPatternCacheForTest()
	invalidateAuthorityDomainCacheForTest()
	invalidateRepairPlanCacheForTest()
	invalidateRuntimeEvidenceCacheForTest()
	s := newServer(newEmbeddedSeedStore())
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task: task, Mode: awarenesspb.PreflightMode_PREFLIGHT_STANDARD,
	})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	return resp
}

func surfacedPlans(resp *awarenesspb.PreflightResponse) []string {
	var out []string
	for _, a := range resp.GetRequiredActions() {
		if strings.HasPrefix(a, "Repair plan: ") {
			out = append(out, a)
		}
	}
	return out
}

// The P4 incident shape: symptom-only, phrasing never authored verbatim.
func TestRepairPlanSurfacesFromSymptomOnlyTask(t *testing.T) {
	requireCombinedSeed(t)
	cases := []struct {
		task     string
		wantPlan string
	}{
		{
			task:     "the doctor found a bad etcd key and wants to delete it, how do I proceed safely",
			wantPlan: "globular.repair.doctor_finding_requires_remediation",
		},
		{
			task:     "an artifact is stuck and won't become installable on any node",
			wantPlan: "globular.repair.repository_artifact_lifecycle_stuck",
		},
		{
			task:     "workflow run blocked on a failed step and never finishes",
			wantPlan: "globular.repair.workflow_resume_blocked_step",
		},
	}
	for _, tc := range cases {
		t.Run(tc.wantPlan, func(t *testing.T) {
			resp := taskOnlyPreflight(t, tc.task)
			if !anyContains(resp.GetRequiredActions(), tc.wantPlan) {
				t.Errorf("symptom task %q did not surface %s\n  plans: %v",
					tc.task, tc.wantPlan, surfacedPlans(resp))
			}
		})
	}
}

// No false positives: an unrelated task must surface zero repair plans.
// A repair plan firing on everything is worse than one firing on nothing.
func TestRepairPlanNotSurfacedForUnrelatedTask(t *testing.T) {
	for _, task := range []string{
		"add a dark mode toggle to the web console settings page",
		"update the README badges",
	} {
		resp := taskOnlyPreflight(t, task)
		if plans := surfacedPlans(resp); len(plans) > 0 {
			t.Errorf("unrelated task %q surfaced repair plans: %v", task, plans)
		}
	}
}
