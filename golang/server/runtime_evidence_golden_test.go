// SPDX-License-Identifier: Apache-2.0

package main

// Phase 2C golden test: Preflight surfaces the live-evidence requirement for a
// file whose authority domain carries a RuntimeEvidence profile, including the
// hard rule that stale/missing evidence must not be promoted to PASS. Runs
// against the real compiled seed.

import (
	"context"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func TestPreflightSurfacesEvidenceRequirements(t *testing.T) {
	requireCombinedSeed(t)
	invalidateImplementationPatternCacheForTest()
	invalidateAuthorityDomainCacheForTest()
	invalidateRepairPlanCacheForTest()
	invalidateRuntimeEvidenceCacheForTest()
	s := newServer(newEmbeddedSeedStore())

	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "change repository publish workflow installability behavior",
		Files: []string{"golang/repository/repository_server/publish_workflow.go"},
		Mode:  awarenesspb.PreflightMode_PREFLIGHT_STANDARD,
	})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}

	if !anyContains(resp.GetRequiredActions(), "Evidence required") {
		t.Errorf("required_actions missing an evidence requirement\n  got: %v", resp.GetRequiredActions())
	}
	if !anyContains(resp.GetRequiredActions(), "must NOT be promoted to PASS") {
		t.Errorf("required_actions missing the stale-evidence-not-PASS rule\n  got: %v", resp.GetRequiredActions())
	}
	if !anyContains(resp.GetRequiredActions(), "repository (repository.PackageRepository)") {
		t.Errorf("required_actions missing the owner service for the evidence\n  got: %v", resp.GetRequiredActions())
	}
}
