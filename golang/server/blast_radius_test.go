// SPDX-License-Identifier: Apache-2.0

package main

// Phase 2F tests: Preflight gives a clear blast-radius + approval-gate signal.
// Real-corpus cases run against the embedded seed; the unknown-authority case
// uses an empty store on a high-risk path.

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/store"
)

func changeRiskLine(resp *awarenesspb.PreflightResponse) string {
	for _, a := range resp.GetRequiredActions() {
		if strings.HasPrefix(a, "Change risk:") {
			return a
		}
	}
	return ""
}

func preflightFile(t *testing.T, st store.Store, task, file string) *awarenesspb.PreflightResponse {
	t.Helper()
	invalidateImplementationPatternCacheForTest()
	invalidateAuthorityDomainCacheForTest()
	invalidateIntentTriggerCacheForTest()
	invalidateRepairPlanCacheForTest()
	invalidateRuntimeEvidenceCacheForTest()
	s := newServer(st)
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task: task, Files: []string{file}, Mode: awarenesspb.PreflightMode_PREFLIGHT_STANDARD,
	})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	return resp
}

func TestBlastRadiusForRepositoryPublishIsClusterOrService(t *testing.T) {
	resp := preflightFile(t, newEmbeddedSeedStore(),
		"change repository publish workflow installability behavior",
		"golang/repository/repository_server/publish_workflow.go")
	line := changeRiskLine(resp)
	if !strings.Contains(line, "blast=cluster") && !strings.Contains(line, "blast=service") {
		t.Errorf("repository publish change should be cluster/service blast; got %q", line)
	}
}

func TestRBACChangeRequiresHumanApproval(t *testing.T) {
	resp := preflightFile(t, newEmbeddedSeedStore(),
		"change RBAC access validation",
		"golang/rbac/rbac_server/rbac_access.go")
	line := changeRiskLine(resp)
	if !strings.Contains(line, "approval=human_approval_required") &&
		!strings.Contains(line, "approval=multi_step_approval_required") &&
		!strings.Contains(line, "approval=manual_only") {
		t.Errorf("RBAC change should require human approval; got %q", line)
	}
}

func TestLowRiskHelperDoesNotRequireApproval(t *testing.T) {
	resp := preflightFile(t, newEmbeddedSeedStore(),
		"tweak echo helper logging",
		"golang/echo/echo_server/echo.go")
	line := changeRiskLine(resp)
	if !strings.Contains(line, "approval=none") {
		t.Errorf("low-risk helper should need no approval; got %q", line)
	}
}

func TestUnknownAuthorityEscalatesApproval(t *testing.T) {
	// nopStore: no anchors, no authority domains — but the file is under a
	// high-risk directory (mcp) not covered by any authority domain.
	resp := preflightFile(t, nopStore{}, "edit an mcp tool",
		"golang/mcp/mcp_server/tools.go")
	line := changeRiskLine(resp)
	if strings.Contains(line, "approval=none") {
		t.Errorf("unknown authority on a high-risk file should escalate approval; got %q", line)
	}
}
