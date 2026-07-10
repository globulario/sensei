// SPDX-License-Identifier: AGPL-3.0-only

package main

// Signal-quality contract tests, born from dogfooding the offline -preflight
// CLI on this repo's own work. Two properties:
//
//  1. No weak-match noise: a medium/narrow keyword-overlap pattern match must
//     not inject its required/forbidden calls into the action lists. (Found
//     live: a repository-publish task pulled grpc-client "Call InitClient"
//     actions off a two-keyword overlap, burying the repair guidance.)
//  2. Ordering: the leading actions carry the decision-critical signals —
//     change-risk first, then evidence/repair/authority — so an agent reading
//     only the top of the list still gets the verdict.

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
)

func preflightQuality(t *testing.T, task, file string) *awarenesspb.PreflightResponse {
	t.Helper()
	invalidateImplementationPatternCacheForTest()
	invalidateAuthorityDomainCacheForTest()
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

// The live regression: an awareness-graph-flavoured task on a repository file
// keyword-overlaps the grpc-client pattern at medium tier. Its calls must not
// surface as actions or forbidden fixes.
func TestPreflightWeakPatternMatchDoesNotPolluteActions(t *testing.T) {
	requireCombinedSeed(t)
	resp := preflightQuality(t,
		"extend the awareness-graph preflight handler with an offline CLI mode",
		"golang/repository/repository_server/publish_workflow.go")

	for _, a := range resp.GetRequiredActions() {
		if strings.Contains(a, "globular.InitClient") || strings.Contains(a, "echo_client") {
			t.Errorf("weak pattern match leaked into required_actions: %q", a)
		}
	}
	for _, f := range resp.GetForbiddenFixes() {
		if strings.Contains(f, "grpc.Dial") {
			t.Errorf("weak pattern match leaked into forbidden_fixes: %q", f)
		}
	}
	// The real guidance must still be there.
	if !anyContains(resp.GetRequiredActions(), "globular.repair.repository_artifact_lifecycle_stuck") {
		t.Errorf("repair guidance missing after noise gating: %v", resp.GetRequiredActions())
	}
}

// A STRONG pattern match must still drive actions — the gate removes noise,
// not the recipe.
func TestPreflightStrongPatternMatchStillDrivesActions(t *testing.T) {
	requireCombinedSeed(t)
	resp := preflightQuality(t,
		"creating a new Go client for a Globular gRPC service",
		"")
	if !anyContains(resp.GetRequiredActions(), "globular.InitClient") {
		t.Errorf("strong pattern match should drive actions; got %v", resp.GetRequiredActions())
	}
}

// Ordering contract: change-risk leads, and the decision-critical block
// (evidence + repair plan) appears within the first 8 actions for a covered
// repair task. Reading only the top of the list must yield the verdict.
func TestPreflightCriticalSignalsLeadTheActionList(t *testing.T) {
	requireCombinedSeed(t)
	resp := preflightQuality(t,
		"fix a stuck repository artifact that won't become installable",
		"golang/repository/repository_server/publish_workflow.go")
	actions := resp.GetRequiredActions()
	if len(actions) == 0 {
		t.Fatal("no required_actions")
	}
	if !strings.HasPrefix(actions[0], "Change risk:") {
		t.Errorf("first action must be the change-risk verdict, got %q", actions[0])
	}
	head := actions
	if len(head) > 8 {
		head = head[:8]
	}
	if !anyContains(head, "Evidence required") {
		t.Errorf("evidence requirement not in the first 8 actions: %v", head)
	}
	if !anyContains(head, "Repair plan:") {
		t.Errorf("repair plan not in the first 8 actions: %v", head)
	}
}
