// SPDX-License-Identifier: AGPL-3.0-only

package evidencereceipt

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

func staticProfile() Profile {
	return Profile{
		ProfileID:            "profile.static.closure",
		Owner:                "component.closure",
		LegalObservationPath: "static_analyzer.govet",
		EvidenceKind:         closureprotocol.EvidenceStatic,
		Freshness:            "per-result",
		Trust:                "high",
		GovernedTarget:       "authority.closure",
		Status:               closureprotocol.ReceiptValid,
	}
}

func staticReceipt() Receipt {
	r := testReceipt()
	r.ReceiptID = "receipt.static.closure"
	r.EvidenceKind = closureprotocol.EvidenceStatic
	r.ProfileID = "profile.static.closure"
	r.ObservationPath = "govet"
	return r
}

func TestEvaluateCoverage(t *testing.T) {
	base := CoverageRequest{ExpectedResult: expectedResult(), Now: evalNow}

	t.Run("static_test with no runtime evidence reaches a valid state", func(t *testing.T) {
		req := base
		req.Coverage = CoverageStaticTest
		items := []EvidenceItem{
			{Profile: staticProfile(), Receipt: staticReceipt()},
			{Profile: testProfile(), Receipt: testReceipt()},
		}
		got := EvaluateCoverage(req, items)
		if got.Lanes.Static != closureprotocol.DimensionPass {
			t.Fatalf("static lane = %q, want pass", got.Lanes.Static)
		}
		if got.Lanes.Test != closureprotocol.DimensionPass {
			t.Fatalf("test lane = %q, want pass", got.Lanes.Test)
		}
		if got.Lanes.Runtime != closureprotocol.DimensionNotApplicable {
			t.Fatalf("runtime lane = %q, want not_applicable", got.Lanes.Runtime)
		}
		if got.EvidenceLane != closureprotocol.DimensionPass {
			t.Fatalf("evidence lane = %q, want pass", got.EvidenceLane)
		}
	})

	t.Run("empty coverage defaults to static_test", func(t *testing.T) {
		items := []EvidenceItem{
			{Profile: staticProfile(), Receipt: staticReceipt()},
			{Profile: testProfile(), Receipt: testReceipt()},
		}
		got := EvaluateCoverage(base, items)
		if got.Coverage != CoverageStaticTest {
			t.Fatalf("coverage = %q, want static_test default", got.Coverage)
		}
		if got.Lanes.Runtime != closureprotocol.DimensionNotApplicable {
			t.Fatalf("runtime lane = %q, want not_applicable", got.Lanes.Runtime)
		}
	})

	t.Run("conflicting receipts open a blocker", func(t *testing.T) {
		req := base
		req.Coverage = CoverageStaticTest
		a := staticReceipt()
		a.ReceiptID = "receipt.static.a"
		b := staticReceipt()
		b.ReceiptID = "receipt.static.b"
		b.PayloadDigestSHA256 = "payload999"
		items := []EvidenceItem{
			{Profile: staticProfile(), Receipt: a},
			{Profile: staticProfile(), Receipt: b},
			{Profile: testProfile(), Receipt: testReceipt()},
		}
		got := EvaluateCoverage(req, items)
		if got.Lanes.Static != closureprotocol.DimensionConflicted {
			t.Fatalf("static lane = %q, want conflicted", got.Lanes.Static)
		}
		if got.EvidenceLane != closureprotocol.DimensionConflicted {
			t.Fatalf("evidence lane = %q, want conflicted", got.EvidenceLane)
		}
		if len(got.Conflicts) == 0 {
			t.Fatalf("expected conflicts to be reported")
		}
	})

	t.Run("static_test_runtime requires runtime evidence", func(t *testing.T) {
		req := base
		req.Coverage = CoverageStaticTestRuntime
		items := []EvidenceItem{
			{Profile: staticProfile(), Receipt: staticReceipt()},
			{Profile: testProfile(), Receipt: testReceipt()},
		}
		got := EvaluateCoverage(req, items)
		if got.Lanes.Runtime != closureprotocol.DimensionBlocked {
			t.Fatalf("runtime lane = %q, want blocked", got.Lanes.Runtime)
		}
		if got.EvidenceLane != closureprotocol.DimensionBlocked {
			t.Fatalf("evidence lane = %q, want blocked", got.EvidenceLane)
		}
		if !hasReason(got.ReasonCodes, ReasonLaneMissing) {
			t.Fatalf("expected missing-lane reason, got %v", got.ReasonCodes)
		}
	})

	t.Run("static_test_runtime with owner-path runtime evidence certifies", func(t *testing.T) {
		req := base
		req.Coverage = CoverageStaticTestRuntime
		req.RuntimeTarget = &RuntimeTarget{Platform: "globular", DeploymentID: "clusterA", ConfigurationGeneration: "gen-7"}
		items := []EvidenceItem{
			{Profile: staticProfile(), Receipt: staticReceipt()},
			{Profile: testProfile(), Receipt: testReceipt()},
			{Profile: runtimeProfile(), Receipt: runtimeReceipt()},
		}
		got := EvaluateCoverage(req, items)
		if got.Lanes.Runtime != closureprotocol.DimensionPass {
			t.Fatalf("runtime lane = %q, want pass (assessments %v)", got.Lanes.Runtime, got.Assessments)
		}
		if got.EvidenceLane != closureprotocol.DimensionPass {
			t.Fatalf("evidence lane = %q, want pass", got.EvidenceLane)
		}
	})

	t.Run("stale receipt degrades its lane", func(t *testing.T) {
		req := base
		req.Coverage = CoverageStaticTest
		stale := testReceipt()
		stale.ExpiresAt = "2026-07-15T18:00:00Z"
		items := []EvidenceItem{
			{Profile: staticProfile(), Receipt: staticReceipt()},
			{Profile: testProfile(), Receipt: stale},
		}
		got := EvaluateCoverage(req, items)
		if got.Lanes.Test != closureprotocol.DimensionStale {
			t.Fatalf("test lane = %q, want stale", got.Lanes.Test)
		}
		if got.EvidenceLane != closureprotocol.DimensionStale {
			t.Fatalf("evidence lane = %q, want stale", got.EvidenceLane)
		}
	})
}
