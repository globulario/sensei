// SPDX-License-Identifier: AGPL-3.0-only

package proofdischarge

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

func slotResult(d closureprotocol.ProofDischarge, id string) (closureprotocol.ProofSlotResult, bool) {
	for _, s := range d.SlotResults {
		if s.SlotID == id {
			return s, true
		}
	}
	return closureprotocol.ProofSlotResult{}, false
}

func mustDischargeOne(t *testing.T, ctx Context) (closureprotocol.ProofDischarge, DischargeReport) {
	t.Helper()
	discharges, report, err := Discharge(ctx)
	if err != nil {
		t.Fatalf("Discharge error: %v", err)
	}
	if len(discharges) != 1 {
		t.Fatalf("expected 1 discharge, got %d", len(discharges))
	}
	return discharges[0], report
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// Row 1: arbitrary shell observation path is rejected.
func TestRow1_ArbitraryShellRejected(t *testing.T) {
	prof := okProfile("prof.t", string(closureprotocol.EvidenceTest))
	r := okReceipt("receipt.shell", prof.ProfileID, string(closureprotocol.EvidenceTest))
	r.ObservationPath = "bash -c 'go test ./...'"
	ctx := testCtx(map[string]closureprotocol.EvidenceProfile{prof.ProfileID: prof}, []closureprotocol.EvidenceReceipt{r})
	ctx.Obligations = []ProofObligation{testOb("ob.1", testSlot("slot.a", SlotKindTestOrRuntime))}

	d, _ := mustDischargeOne(t, ctx)
	if !contains(d.IncompatibleReceipts, "receipt.shell") {
		t.Fatalf("expected receipt.shell in incompatible_receipts, got %v", d.IncompatibleReceipts)
	}
	sr, _ := slotResult(d, "slot.a")
	if sr.Status != closureprotocol.DimensionBlocked {
		t.Fatalf("slot status = %v, want blocked", sr.Status)
	}
	if d.Status != closureprotocol.ReceiptInvalid {
		t.Fatalf("obligation status = %v, want invalid", d.Status)
	}
}

// Row 2: test ID resolves only to the governed runner (A passes, B broad fails).
func TestRow2_GovernedRunnerOnly(t *testing.T) {
	prof := okProfile("prof.t", string(closureprotocol.EvidenceTest))
	a := okReceipt("receipt.a", prof.ProfileID, string(closureprotocol.EvidenceTest)) // exact legal path
	b := okReceipt("receipt.b", prof.ProfileID, string(closureprotocol.EvidenceTest))
	b.ObservationPath = "go test -run . ./..." // broader invocation, not the legal path
	ctx := testCtx(map[string]closureprotocol.EvidenceProfile{prof.ProfileID: prof}, []closureprotocol.EvidenceReceipt{a, b})
	ctx.Obligations = []ProofObligation{testOb("ob.2", testSlot("slot.a", SlotKindTestOrRuntime))}

	d, _ := mustDischargeOne(t, ctx)
	sr, _ := slotResult(d, "slot.a")
	if sr.Status != closureprotocol.DimensionPass {
		t.Fatalf("slot status = %v, want pass", sr.Status)
	}
	if len(sr.ReceiptIDs) != 1 || sr.ReceiptIDs[0] != "receipt.a" {
		t.Fatalf("receipt_ids = %v, want [receipt.a]", sr.ReceiptIDs)
	}
	if !contains(d.IncompatibleReceipts, "receipt.b") {
		t.Fatalf("expected receipt.b incompatible, got %v", d.IncompatibleReceipts)
	}
	if d.Status != closureprotocol.ReceiptValid {
		t.Fatalf("obligation status = %v, want valid", d.Status)
	}
}

// Row 3: unsafe probe requires approval — unattested trust cannot discharge.
func TestRow3_UnsafeProbeRequiresApproval(t *testing.T) {
	prof := okProfile("prof.t", string(closureprotocol.EvidenceTest))
	r := okReceipt("receipt.unsafe", prof.ProfileID, string(closureprotocol.EvidenceTest))
	r.Trust = TrustUnattested
	ctx := testCtx(map[string]closureprotocol.EvidenceProfile{prof.ProfileID: prof}, []closureprotocol.EvidenceReceipt{r})
	ctx.Obligations = []ProofObligation{testOb("ob.3", testSlot("slot.a", SlotKindTestOrRuntime))}

	d, report := mustDischargeOne(t, ctx)
	sr, _ := slotResult(d, "slot.a")
	if sr.Status != closureprotocol.DimensionBlocked {
		t.Fatalf("slot status = %v, want blocked", sr.Status)
	}
	// reason surfaced in the report
	found := false
	for _, ob := range report.Obligations {
		for _, s := range ob.Slots {
			for _, c := range s.Candidates {
				if c.ReceiptID == "receipt.unsafe" && contains(c.ReasonCodes, ReasonTrustInsufficient) {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("expected trust_insufficient reason in report")
	}
}

// Row 4: manual result without approval receipt — Phase-4 boundary simulated by a
// non-valid status; Step 0.5 drops it entirely.
func TestRow4_ManualWithoutApprovalDropped(t *testing.T) {
	prof := okProfile("prof.t", string(closureprotocol.EvidenceTest))
	r := okReceipt("receipt.manual", prof.ProfileID, string(closureprotocol.EvidenceTest))
	r.Status = closureprotocol.ReceiptUnknown
	ctx := testCtx(map[string]closureprotocol.EvidenceProfile{prof.ProfileID: prof}, []closureprotocol.EvidenceReceipt{r})
	ctx.Obligations = []ProofObligation{testOb("ob.4", testSlot("slot.a", SlotKindTestOrRuntime))}

	d, report := mustDischargeOne(t, ctx)
	if len(report.DroppedReceipts) != 1 || report.DroppedReceipts[0].ReceiptID != "receipt.manual" {
		t.Fatalf("expected receipt.manual dropped, got %v", report.DroppedReceipts)
	}
	if contains(d.IncompatibleReceipts, "receipt.manual") {
		t.Fatal("dropped receipt must not appear in incompatible_receipts")
	}
	if !contains(d.MissingSlots, "slot.a") {
		t.Fatalf("missing_slots = %v, want slot.a", d.MissingSlots)
	}
	if d.Status != closureprotocol.ReceiptInvalid {
		t.Fatalf("status = %v, want invalid", d.Status)
	}
}

// Row 5: evidence-kind mismatch cannot discharge (both coverage branches).
func TestRow5_EvidenceKindMismatch(t *testing.T) {
	prof := okProfile("prof.s", string(closureprotocol.EvidenceStatic))
	r := okReceipt("receipt.static", prof.ProfileID, string(closureprotocol.EvidenceStatic))
	profiles := map[string]closureprotocol.EvidenceProfile{prof.ProfileID: prof}

	// static_test_runtime: runtime slot required -> blocked, invalid.
	ctxRT := testCtx(profiles, []closureprotocol.EvidenceReceipt{r})
	ctxRT.CoverageProfile = CoverageStaticTestRuntime
	ctxRT.RuntimeTarget = &closureprotocol.RuntimeTarget{Platform: "linux", EnvironmentID: "env1"}
	ctxRT.Obligations = []ProofObligation{testOb("ob.5", testSlot("slot.rt", SlotKindRuntime))}
	d, _ := mustDischargeOne(t, ctxRT)
	sr, _ := slotResult(d, "slot.rt")
	if sr.Status != closureprotocol.DimensionBlocked {
		t.Fatalf("runtime slot under static_test_runtime = %v, want blocked", sr.Status)
	}
	if contains(d.IncompatibleReceipts, "receipt.static") {
		t.Fatal("kind-mismatched receipt is filtered, not a candidate; must not be incompatible")
	}
	if d.Status != closureprotocol.ReceiptInvalid {
		t.Fatalf("status = %v, want invalid", d.Status)
	}

	// static_test (default): runtime slot relaxed to not_applicable -> valid.
	ctxST := testCtx(profiles, []closureprotocol.EvidenceReceipt{r})
	ctxST.Obligations = []ProofObligation{testOb("ob.5", testSlot("slot.rt", SlotKindRuntime))}
	d2, _ := mustDischargeOne(t, ctxST)
	sr2, _ := slotResult(d2, "slot.rt")
	if sr2.Status != closureprotocol.DimensionNotApplicable {
		t.Fatalf("runtime slot under static_test = %v, want not_applicable", sr2.Status)
	}
	if d2.Status != closureprotocol.ReceiptValid {
		t.Fatalf("status = %v, want valid", d2.Status)
	}
}

// Row 6: optional slot may remain open.
func TestRow6_OptionalSlotOpen(t *testing.T) {
	ctx := testCtx(map[string]closureprotocol.EvidenceProfile{}, nil)
	ob := testOb("ob.6", ProofSlotSpec{ID: "slot.opt", Kind: SlotKindStaticGuard, Required: false})
	ctx.Obligations = []ProofObligation{ob}

	d, _ := mustDischargeOne(t, ctx)
	sr, _ := slotResult(d, "slot.opt")
	if sr.Status != closureprotocol.DimensionUnknown {
		t.Fatalf("optional slot = %v, want unknown", sr.Status)
	}
	if contains(d.MissingSlots, "slot.opt") {
		t.Fatal("optional slot must not be in missing_slots")
	}
	if d.Status != closureprotocol.ReceiptValid {
		t.Fatalf("status = %v, want valid", d.Status)
	}
}

// Row 7: required slot blocks with no receipts and no waivers.
func TestRow7_RequiredSlotBlocks(t *testing.T) {
	ctx := testCtx(map[string]closureprotocol.EvidenceProfile{}, nil)
	ctx.Obligations = []ProofObligation{testOb("ob.7", testSlot("slot.req", SlotKindStaticGuard))}

	d, _ := mustDischargeOne(t, ctx)
	sr, _ := slotResult(d, "slot.req")
	if sr.Status != closureprotocol.DimensionBlocked {
		t.Fatalf("required slot = %v, want blocked", sr.Status)
	}
	if !contains(d.MissingSlots, "slot.req") {
		t.Fatalf("missing_slots = %v, want slot.req", d.MissingSlots)
	}
	if d.Status != closureprotocol.ReceiptInvalid {
		t.Fatalf("status = %v, want invalid", d.Status)
	}
}

// Row 8: a valid waiver satisfies only its exact slot.
func TestRow8_WaiverExactSlotOnly(t *testing.T) {
	ctx := testCtx(map[string]closureprotocol.EvidenceProfile{}, nil)
	ob := testOb("ob.8", testSlot("slot.a", SlotKindStaticGuard), testSlot("slot.b", SlotKindStaticGuard))
	ctx.Obligations = []ProofObligation{ob}
	ctx.Waivers = []closureprotocol.WaiverReceipt{{
		WaiverID:      "waiver.a",
		Dimension:     closureprotocol.DimensionProof,
		PolicyID:      "policy.p1",
		Justification: "reviewed exception",
		ExpiresAt:     "2026-08-01T00:00:00Z",
		AppliesTo:     []string{"slot.a"}, // slot A only, not slot.b, not ob.8
		Status:        closureprotocol.ReceiptValid,
	}}
	ctx.GovernanceExceptions = map[string]GovernanceException{
		"policy.p1": {ID: "ge.1", Status: "governed", Dimension: "proof", AppliesToSlotIDs: []string{"slot.a"}},
	}

	d, _ := mustDischargeOne(t, ctx)
	sa, _ := slotResult(d, "slot.a")
	if sa.Status != closureprotocol.DimensionPassWithException {
		t.Fatalf("slot.a = %v, want pass_with_exception", sa.Status)
	}
	sb, _ := slotResult(d, "slot.b")
	if sb.Status != closureprotocol.DimensionBlocked {
		t.Fatalf("slot.b = %v, want blocked", sb.Status)
	}
	if !contains(d.MissingSlots, "slot.b") || contains(d.MissingSlots, "slot.a") {
		t.Fatalf("missing_slots = %v, want only slot.b", d.MissingSlots)
	}
	if d.Status != closureprotocol.ReceiptInvalid {
		t.Fatalf("status = %v, want invalid (waiver did not blanket obligation)", d.Status)
	}
}

// Row 8b: an obligation-wide waiver (applies_to lists only the obligation) does
// NOT satisfy an individual slot.
func TestRow8_ObligationWideWaiverRejected(t *testing.T) {
	ctx := testCtx(map[string]closureprotocol.EvidenceProfile{}, nil)
	ob := testOb("ob.8b", testSlot("slot.a", SlotKindStaticGuard))
	ctx.Obligations = []ProofObligation{ob}
	ctx.Waivers = []closureprotocol.WaiverReceipt{{
		WaiverID:      "waiver.wide",
		Dimension:     closureprotocol.DimensionProof,
		PolicyID:      "policy.p1",
		Justification: "blanket",
		ExpiresAt:     "2026-08-01T00:00:00Z",
		AppliesTo:     []string{"ob.8b"}, // obligation id, not the slot id
		Status:        closureprotocol.ReceiptValid,
	}}
	ctx.GovernanceExceptions = map[string]GovernanceException{
		"policy.p1": {ID: "ge.1", Status: "governed", Dimension: "proof", AppliesToObligationIDs: []string{"ob.8b"}},
	}

	d, _ := mustDischargeOne(t, ctx)
	sa, _ := slotResult(d, "slot.a")
	if sa.Status != closureprotocol.DimensionBlocked {
		t.Fatalf("slot.a = %v, want blocked (obligation-wide waiver not accepted)", sa.Status)
	}
}

// Row 8c: an ungoverned governance exception cannot back a waiver.
func TestRow8_UngovernedExceptionRejected(t *testing.T) {
	ctx := testCtx(map[string]closureprotocol.EvidenceProfile{}, nil)
	ctx.Obligations = []ProofObligation{testOb("ob.8c", testSlot("slot.a", SlotKindStaticGuard))}
	ctx.Waivers = []closureprotocol.WaiverReceipt{{
		WaiverID: "waiver.a", Dimension: closureprotocol.DimensionProof, PolicyID: "policy.p1",
		Justification: "x", ExpiresAt: "2026-08-01T00:00:00Z", AppliesTo: []string{"slot.a"},
		Status: closureprotocol.ReceiptValid,
	}}
	ctx.GovernanceExceptions = map[string]GovernanceException{
		"policy.p1": {ID: "ge.1", Status: "proposed", Dimension: "proof", AppliesToSlotIDs: []string{"slot.a"}},
	}
	d, _ := mustDischargeOne(t, ctx)
	sa, _ := slotResult(d, "slot.a")
	if sa.Status != closureprotocol.DimensionBlocked {
		t.Fatalf("slot.a = %v, want blocked (exception not governed)", sa.Status)
	}
}

// Row 9: static_test discharges with no runtime evidence at all.
func TestRow9_StaticTestNoRuntime(t *testing.T) {
	prof := okProfile("prof.t", string(closureprotocol.EvidenceTest))
	testReceipt := okReceipt("receipt.test", prof.ProfileID, string(closureprotocol.EvidenceTest))
	ctx := testCtx(map[string]closureprotocol.EvidenceProfile{prof.ProfileID: prof}, []closureprotocol.EvidenceReceipt{testReceipt})
	ob := testOb("ob.9",
		testSlot("slot.runtime", SlotKindRuntime),
		testSlot("slot.process", SlotKindProcessArtifact),
		testSlot("slot.log", SlotKindLogArtifact),
		testSlot("slot.failure", SlotKindFailureEvidence),
		testSlot("slot.test", SlotKindTestOrRuntime),
	)
	ctx.Obligations = []ProofObligation{ob}

	d, _ := mustDischargeOne(t, ctx)
	for _, id := range []string{"slot.runtime", "slot.process", "slot.log", "slot.failure"} {
		sr, _ := slotResult(d, id)
		if sr.Status != closureprotocol.DimensionNotApplicable {
			t.Fatalf("%s = %v, want not_applicable", id, sr.Status)
		}
		if contains(d.MissingSlots, id) {
			t.Fatalf("%s must not be in missing_slots", id)
		}
	}
	st, _ := slotResult(d, "slot.test")
	if st.Status != closureprotocol.DimensionPass {
		t.Fatalf("slot.test = %v, want pass", st.Status)
	}
	if d.Status != closureprotocol.ReceiptValid {
		t.Fatalf("status = %v, want valid despite total runtime absence", d.Status)
	}
}

// Row 9a: an obligation that MANDATES runtime evidence keeps its runtime slot
// required under static_test — a missing runtime receipt does NOT get
// rubber-stamped to not_applicable; the obligation is not discharged.
func TestRow9a_MandatedRuntimeStaticTestNoReceipt(t *testing.T) {
	prof := okProfile("prof.t", string(closureprotocol.EvidenceTest))
	testReceipt := okReceipt("receipt.test", prof.ProfileID, string(closureprotocol.EvidenceTest))
	ctx := testCtx(map[string]closureprotocol.EvidenceProfile{prof.ProfileID: prof}, []closureprotocol.EvidenceReceipt{testReceipt})
	ob := testOb("ob.9a",
		testSlot("slot.runtime", SlotKindRuntime),
		testSlot("slot.test", SlotKindTestOrRuntime),
	)
	ob.RequiresRuntimeEvidence = true // governed mandate
	ctx.Obligations = []ProofObligation{ob}

	d, _ := mustDischargeOne(t, ctx)
	sr, _ := slotResult(d, "slot.runtime")
	if sr.Status == closureprotocol.DimensionNotApplicable {
		t.Fatal("mandated runtime slot must NOT be relaxed to not_applicable under static_test")
	}
	if sr.Status != closureprotocol.DimensionBlocked {
		t.Fatalf("mandated runtime slot = %v, want blocked", sr.Status)
	}
	if !contains(d.MissingSlots, "slot.runtime") {
		t.Fatalf("missing_slots = %v, want slot.runtime", d.MissingSlots)
	}
	if d.Status == closureprotocol.ReceiptValid {
		t.Fatal("obligation must NOT be discharged when a mandated runtime slot is unsatisfied")
	}
	if d.Status != closureprotocol.ReceiptInvalid {
		t.Fatalf("status = %v, want invalid", d.Status)
	}
}

// Row 9b: the same mandated obligation discharges once a valid compatible runtime
// receipt is supplied, even under static_test.
func TestRow9b_MandatedRuntimeStaticTestWithReceipt(t *testing.T) {
	tProf := okProfile("prof.t", string(closureprotocol.EvidenceTest))
	rtProf := okProfile("prof.rt", string(closureprotocol.EvidenceRuntime))
	testReceipt := okReceipt("receipt.test", tProf.ProfileID, string(closureprotocol.EvidenceTest))
	rtReceipt := okReceipt("receipt.rt", rtProf.ProfileID, string(closureprotocol.EvidenceRuntime))
	ctx := testCtx(map[string]closureprotocol.EvidenceProfile{
		tProf.ProfileID:  tProf,
		rtProf.ProfileID: rtProf,
	}, []closureprotocol.EvidenceReceipt{testReceipt, rtReceipt})
	ob := testOb("ob.9b",
		testSlot("slot.runtime", SlotKindRuntime),
		testSlot("slot.test", SlotKindTestOrRuntime),
	)
	ob.RequiresRuntimeEvidence = true
	ctx.Obligations = []ProofObligation{ob}

	d, _ := mustDischargeOne(t, ctx)
	sr, _ := slotResult(d, "slot.runtime")
	if sr.Status != closureprotocol.DimensionPass {
		t.Fatalf("mandated runtime slot with valid receipt = %v, want pass", sr.Status)
	}
	if len(sr.ReceiptIDs) != 1 || sr.ReceiptIDs[0] != "receipt.rt" {
		t.Fatalf("receipt_ids = %v, want [receipt.rt]", sr.ReceiptIDs)
	}
	if d.Status != closureprotocol.ReceiptValid {
		t.Fatalf("status = %v, want valid", d.Status)
	}
}

// Row 9c: a NON-mandated runtime slot under static_test stays not_applicable
// (unchanged behavior) — the relaxation still applies when the obligation does
// not mandate runtime evidence.
func TestRow9c_NonMandatedRuntimeStaticTestNotApplicable(t *testing.T) {
	ctx := testCtx(map[string]closureprotocol.EvidenceProfile{}, nil)
	ob := testOb("ob.9c", testSlot("slot.runtime", SlotKindRuntime))
	// RequiresRuntimeEvidence defaults to false.
	ctx.Obligations = []ProofObligation{ob}

	d, _ := mustDischargeOne(t, ctx)
	sr, _ := slotResult(d, "slot.runtime")
	if sr.Status != closureprotocol.DimensionNotApplicable {
		t.Fatalf("non-mandated runtime slot under static_test = %v, want not_applicable", sr.Status)
	}
	if contains(d.MissingSlots, "slot.runtime") {
		t.Fatal("non-mandated relaxed slot must not be in missing_slots")
	}
	if d.Status != closureprotocol.ReceiptValid {
		t.Fatalf("status = %v, want valid", d.Status)
	}
}

// Row 10: revoked receipt rejected regardless of every other field matching.
func TestRow10_RevokedReceipt(t *testing.T) {
	prof := okProfile("prof.t", string(closureprotocol.EvidenceTest))
	r := okReceipt("receipt.revoked", prof.ProfileID, string(closureprotocol.EvidenceTest))
	ctx := testCtx(map[string]closureprotocol.EvidenceProfile{prof.ProfileID: prof}, []closureprotocol.EvidenceReceipt{r})
	ctx.RevokedReceiptIDs = map[string]bool{"receipt.revoked": true}
	ctx.Obligations = []ProofObligation{testOb("ob.10", testSlot("slot.a", SlotKindTestOrRuntime))}

	d, _ := mustDischargeOne(t, ctx)
	sr, _ := slotResult(d, "slot.a")
	if sr.Status != closureprotocol.DimensionBlocked {
		t.Fatalf("slot = %v, want blocked", sr.Status)
	}
}

// Row 11: expired receipt is stale even if Status still says valid.
func TestRow11_ExpiredIsStale(t *testing.T) {
	prof := okProfile("prof.t", string(closureprotocol.EvidenceTest))
	r := okReceipt("receipt.stale", prof.ProfileID, string(closureprotocol.EvidenceTest))
	r.ExpiresAt = "2026-07-15T11:00:00Z" // before observedNow, but Status stays valid
	ctx := testCtx(map[string]closureprotocol.EvidenceProfile{prof.ProfileID: prof}, []closureprotocol.EvidenceReceipt{r})
	ctx.Obligations = []ProofObligation{testOb("ob.11", testSlot("slot.a", SlotKindTestOrRuntime))}

	d, _ := mustDischargeOne(t, ctx)
	sr, _ := slotResult(d, "slot.a")
	if sr.Status != closureprotocol.DimensionStale {
		t.Fatalf("slot = %v, want stale", sr.Status)
	}
	if d.Status != closureprotocol.ReceiptStale {
		t.Fatalf("status = %v, want stale", d.Status)
	}
}

// Row 12: unresolved conflict blocks both receipts.
func TestRow12_UnresolvedConflict(t *testing.T) {
	prof := okProfile("prof.t", string(closureprotocol.EvidenceTest))
	a := okReceipt("receipt.a", prof.ProfileID, string(closureprotocol.EvidenceTest))
	b := okReceipt("receipt.b", prof.ProfileID, string(closureprotocol.EvidenceTest))
	a.Conflicts = []string{"receipt.b"}
	b.Conflicts = []string{"receipt.a"}
	ctx := testCtx(map[string]closureprotocol.EvidenceProfile{prof.ProfileID: prof}, []closureprotocol.EvidenceReceipt{a, b})
	ctx.Obligations = []ProofObligation{testOb("ob.12", testSlot("slot.a", SlotKindTestOrRuntime))}

	d, _ := mustDischargeOne(t, ctx)
	sr, _ := slotResult(d, "slot.a")
	if sr.Status != closureprotocol.DimensionConflicted {
		t.Fatalf("slot = %v, want conflicted", sr.Status)
	}
	if d.Status != closureprotocol.ReceiptConflicted {
		t.Fatalf("status = %v, want conflicted", d.Status)
	}
}

// Row 13: runtime evidence from another target never discharges.
func TestRow13_WrongRuntimeTarget(t *testing.T) {
	prof := okProfile("prof.rt", string(closureprotocol.EvidenceRuntime))
	r := okReceipt("receipt.rt", prof.ProfileID, string(closureprotocol.EvidenceRuntime))
	r.RuntimeTarget = &closureprotocol.RuntimeTarget{Platform: "linux", EnvironmentID: "other-env"}
	ctx := testCtx(map[string]closureprotocol.EvidenceProfile{prof.ProfileID: prof}, []closureprotocol.EvidenceReceipt{r})
	ctx.CoverageProfile = CoverageStaticTestRuntime
	ctx.RuntimeTarget = &closureprotocol.RuntimeTarget{Platform: "linux", EnvironmentID: "env1"}
	ctx.Obligations = []ProofObligation{testOb("ob.13", testSlot("slot.rt", SlotKindRuntime))}

	d, report := mustDischargeOne(t, ctx)
	sr, _ := slotResult(d, "slot.rt")
	if sr.Status != closureprotocol.DimensionBlocked {
		t.Fatalf("slot = %v, want blocked", sr.Status)
	}
	found := false
	for _, ob := range report.Obligations {
		for _, s := range ob.Slots {
			for _, c := range s.Candidates {
				if contains(c.ReasonCodes, ReasonRuntimeTargetMismatch) {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("expected runtime_target_mismatch reason")
	}
}

// Row 14: evidence profile governed for the wrong authority domain.
func TestRow14_WrongAuthorityDomain(t *testing.T) {
	prof := okProfile("prof.t", string(closureprotocol.EvidenceTest))
	prof.GovernedTarget = "surface.other"
	r := okReceipt("receipt.t", prof.ProfileID, string(closureprotocol.EvidenceTest))
	ctx := testCtx(map[string]closureprotocol.EvidenceProfile{prof.ProfileID: prof}, []closureprotocol.EvidenceReceipt{r})
	ob := testOb("ob.14", testSlot("slot.a", SlotKindTestOrRuntime))
	ob.AppliesToAuthoritySurfaces = []string{"surface.a"}
	ctx.Obligations = []ProofObligation{ob}

	d, report := mustDischargeOne(t, ctx)
	sr, _ := slotResult(d, "slot.a")
	if sr.Status != closureprotocol.DimensionBlocked {
		t.Fatalf("slot = %v, want blocked", sr.Status)
	}
	found := false
	for _, o := range report.Obligations {
		for _, s := range o.Slots {
			for _, c := range s.Candidates {
				if contains(c.ReasonCodes, ReasonAuthorityDomainMismatch) {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("expected authority_domain_mismatch reason")
	}
}

// Determinism: canonicalize + digest twice yields identical output.
func TestDischargeDeterminism(t *testing.T) {
	prof := okProfile("prof.t", string(closureprotocol.EvidenceTest))
	a := okReceipt("receipt.a", prof.ProfileID, string(closureprotocol.EvidenceTest))
	b := okReceipt("receipt.b", prof.ProfileID, string(closureprotocol.EvidenceTest))
	profiles := map[string]closureprotocol.EvidenceProfile{prof.ProfileID: prof}

	// two obligations, receipts supplied in different orders each run
	obs := []ProofObligation{
		testOb("ob.z", testSlot("slot.a", SlotKindTestOrRuntime)),
		testOb("ob.a", testSlot("slot.b", SlotKindTestOrRuntime)),
	}
	ctx1 := testCtx(profiles, []closureprotocol.EvidenceReceipt{a, b})
	ctx1.Obligations = obs
	ctx2 := testCtx(profiles, []closureprotocol.EvidenceReceipt{b, a})
	ctx2.Obligations = []ProofObligation{obs[1], obs[0]}

	d1, _, err := Discharge(ctx1)
	if err != nil {
		t.Fatal(err)
	}
	d2, _, err := Discharge(ctx2)
	if err != nil {
		t.Fatal(err)
	}
	if len(d1) != 2 || len(d2) != 2 {
		t.Fatalf("expected 2 discharges each, got %d and %d", len(d1), len(d2))
	}
	// sorted by obligation_id deterministically
	if d1[0].ObligationID != "ob.a" || d1[1].ObligationID != "ob.z" {
		t.Fatalf("discharges not sorted by obligation_id: %s, %s", d1[0].ObligationID, d1[1].ObligationID)
	}
	for i := range d1 {
		if d1[i].DischargeDigestSHA256 == "" {
			t.Fatal("digest not set")
		}
		if d1[i].DischargeDigestSHA256 != d2[i].DischargeDigestSHA256 {
			t.Fatalf("non-deterministic digest at %d: %s vs %s", i, d1[i].DischargeDigestSHA256, d2[i].DischargeDigestSHA256)
		}
		j1, _ := closureprotocol.CanonicalJSON(d1[i])
		j2, _ := closureprotocol.CanonicalJSON(d2[i])
		if string(j1) != string(j2) {
			t.Fatalf("non-deterministic canonical JSON at %d", i)
		}
	}
}

// Context validation: missing runtime target under static_test_runtime is refused.
func TestRuntimeTargetRequired(t *testing.T) {
	ctx := testCtx(map[string]closureprotocol.EvidenceProfile{}, nil)
	ctx.CoverageProfile = CoverageStaticTestRuntime
	ctx.Obligations = []ProofObligation{testOb("ob.rt", testSlot("slot.rt", SlotKindRuntime))}
	if _, _, err := Discharge(ctx); err != ErrRuntimeTargetMissing {
		t.Fatalf("err = %v, want ErrRuntimeTargetMissing", err)
	}
}

func TestObservedAtRequired(t *testing.T) {
	ctx := testCtx(map[string]closureprotocol.EvidenceProfile{}, nil)
	ctx.ObservedAt = "not-a-time"
	ctx.Obligations = []ProofObligation{testOb("ob.x", testSlot("slot.a", SlotKindStaticGuard))}
	if _, _, err := Discharge(ctx); err != ErrObservedAtInvalid {
		t.Fatalf("err = %v, want ErrObservedAtInvalid", err)
	}
}
