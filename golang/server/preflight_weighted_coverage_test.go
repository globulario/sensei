// SPDX-License-Identifier: AGPL-3.0-only

package main

// Phase 4: Preflight's honest-DEGRADED gate is risk-weighted. A no-anchors
// result on a high-risk OR authority-owned file degrades; a no-anchors result
// on a low-risk helper does not.

import (
	"context"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store"
)

// authorityDomainFacts builds a synthetic AuthorityDomain whose coversPath is a
// directory NOT in the static high-risk list, so the test isolates the
// authority-membership signal.
func authorityDomainFacts(coversPath string) []store.ImpactFact {
	subj := rdf.MintIRI(rdf.ClassAuthorityDomain, "authority.synthetic")
	mk := func(p, o string) store.ImpactFact {
		return store.ImpactFact{NodeIRI: subj, TypeIRI: rdf.ClassAuthorityDomain, Predicate: p, Object: o}
	}
	return []store.ImpactFact{
		mk(rdf.PropLabel, "Synthetic domain"),
		mk(rdf.PropStatus, "active"),
		mk(rdf.PropOwnerService, "synthetic service"),
		mk(rdf.PropCoversPath, coversPath),
	}
}

func preflightWith(t *testing.T, authorityFacts []store.ImpactFact, file string) *awarenesspb.PreflightResponse {
	t.Helper()
	invalidateImplementationPatternCacheForTest()
	invalidateAuthorityDomainCacheForTest()
	s := newServer(fakeStore{
		// No file anchors — the whole point is the no-anchors path.
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) { return nil, nil },
		classFacts: func(_ context.Context, classIRI string, _ int) ([]store.ImpactFact, error) {
			if classIRI == rdf.ClassAuthorityDomain {
				return authorityFacts, nil
			}
			return nil, nil
		},
	})
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "edit this file",
		Files: []string{file},
		Mode:  awarenesspb.PreflightMode_PREFLIGHT_STANDARD,
	})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	return resp
}

// A high-risk file with no direct anchors must degrade.
func TestPreflightWeighted_HighRiskFileNoAnchorsDegrades(t *testing.T) {
	resp := preflightWith(t, nil, "golang/rbac/rbac_server/rbac_access.go")
	if resp.GetStatus() != awarenesspb.PreflightStatus_PREFLIGHT_STATUS_DEGRADED {
		t.Errorf("status = %v, want DEGRADED for a high-risk file with no anchors", resp.GetStatus())
	}
}

// A low-risk helper with no anchors must NOT degrade — same severity as a
// high-risk file would defeat the weighting.
func TestPreflightWeighted_LowRiskHelperNoAnchorsDoesNotDegrade(t *testing.T) {
	resp := preflightWith(t, nil, "golang/echo/echo_server/echo.go")
	if resp.GetStatus() == awarenesspb.PreflightStatus_PREFLIGHT_STATUS_DEGRADED {
		t.Errorf("status = DEGRADED for a low-risk helper; weighting should keep it non-degraded")
	}
}

// A test file inside a high-risk directory is a helper, not production surface:
// no anchors there must NOT degrade (improvement over the old bare-prefix gate).
func TestPreflightWeighted_TestFileInHighRiskDirDoesNotDegrade(t *testing.T) {
	resp := preflightWith(t, nil, "golang/rbac/rbac_server/rbac_deny_override_test.go")
	if resp.GetStatus() == awarenesspb.PreflightStatus_PREFLIGHT_STATUS_DEGRADED {
		t.Errorf("status = DEGRADED for a _test.go helper in a high-risk dir; should not degrade")
	}
}

// Authority-domain membership raises severity: a file outside the static
// high-risk list but owned by an authority domain degrades when it has no
// anchors. This is the Phase 4 "authority membership increases weight" property
// expressed at the Preflight layer.
func TestPreflightWeighted_AuthorityMembershipRaisesSeverity(t *testing.T) {
	file := "golang/echo/echo_server/echo.go"

	// Control: no authority domain -> low risk -> not degraded.
	ctrl := preflightWith(t, nil, file)
	if ctrl.GetStatus() == awarenesspb.PreflightStatus_PREFLIGHT_STATUS_DEGRADED {
		t.Fatalf("control: low-risk file unexpectedly degraded")
	}

	// With a synthetic authority domain covering golang/echo/ -> degrades.
	resp := preflightWith(t, authorityDomainFacts("golang/echo/"), file)
	if resp.GetStatus() != awarenesspb.PreflightStatus_PREFLIGHT_STATUS_DEGRADED {
		t.Errorf("status = %v, want DEGRADED once an authority domain owns the file", resp.GetStatus())
	}
}
