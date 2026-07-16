// SPDX-License-Identifier: AGPL-3.0-only

package proofdischarge

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// baseResult is the result binding under proof, shared by fixtures.
func baseResult() closureprotocol.ResultBinding {
	return closureprotocol.ResultBinding{
		BaseRevision:           "rev1",
		PatchDigestSHA256:      "patch1",
		ResultTreeDigestSHA256: "tree1",
		GraphDigestSHA256:      "graph1",
	}
}

// okProfile is a governed profile whose legal observation path a passing receipt
// must match exactly.
func okProfile(id, kind string) closureprotocol.EvidenceProfile {
	return closureprotocol.EvidenceProfile{
		ProfileID:            id,
		Owner:                "owner.core",
		LegalObservationPath: "test_runner.go_test::TestFoo",
		EvidenceKind:         closureprotocol.EvidenceKind(kind),
		Status:               closureprotocol.ReceiptValid,
	}
}

// okReceipt is a receipt that satisfies every compatibility check by default.
func okReceipt(id, profileID, kind string) closureprotocol.EvidenceReceipt {
	return closureprotocol.EvidenceReceipt{
		ReceiptID:           id,
		EvidenceKind:        closureprotocol.EvidenceKind(kind),
		ProfileID:           profileID,
		ResultBinding:       baseResult(),
		Producer:            "producer.core",
		ObservationPath:     "test_runner.go_test::TestFoo",
		ObservedAt:          "2026-07-15T10:00:00Z",
		Status:              closureprotocol.ReceiptValid,
		Trust:               "attested",
		PayloadDigestSHA256: "payload-" + id,
	}
}

const observedNow = "2026-07-15T12:00:00Z"

func testCtx(profiles map[string]closureprotocol.EvidenceProfile, receipts []closureprotocol.EvidenceReceipt) Context {
	return Context{
		ResultBinding:   baseResult(),
		CoverageProfile: CoverageStaticTest,
		Profiles:        profiles,
		Receipts:        receipts,
		ObservedAt:      observedNow,
	}
}

func testSlot(id, kind string) ProofSlotSpec { return ProofSlotSpec{ID: id, Kind: kind, Required: true} }

func testOb(id string, slots ...ProofSlotSpec) ProofObligation {
	return ProofObligation{ID: id, Status: "candidate", RequiredSlots: slots}
}

// TestCheckCompatibility exercises one failure mode per compatibility check.
func TestCheckCompatibility(t *testing.T) {
	prof := okProfile("prof.test", string(closureprotocol.EvidenceTest))
	profiles := map[string]closureprotocol.EvidenceProfile{prof.ProfileID: prof}
	slot := testSlot("slot.a", SlotKindTestOrRuntime)
	ob := testOb("ob.a", slot)

	tests := []struct {
		name    string
		mutate  func(r *closureprotocol.EvidenceReceipt, c *Context)
		wantOK  bool
		wantOne string
	}{
		{name: "clean receipt passes", mutate: func(r *closureprotocol.EvidenceReceipt, c *Context) {}, wantOK: true},
		{
			name:    "revoked",
			mutate:  func(r *closureprotocol.EvidenceReceipt, c *Context) { c.RevokedReceiptIDs = map[string]bool{r.ReceiptID: true} },
			wantOne: ReasonReceiptRevoked,
		},
		{
			name:    "evidence kind mismatch",
			mutate:  func(r *closureprotocol.EvidenceReceipt, c *Context) { r.EvidenceKind = closureprotocol.EvidenceStatic },
			wantOne: ReasonEvidenceKindMismatch,
		},
		{
			name:    "authority kind never discharges",
			mutate:  func(r *closureprotocol.EvidenceReceipt, c *Context) { r.EvidenceKind = closureprotocol.EvidenceAuthority },
			wantOne: ReasonEvidenceKindMismatch,
		},
		{
			name: "profile unknown",
			mutate: func(r *closureprotocol.EvidenceReceipt, c *Context) {
				r.ProfileID = "prof.missing"
				c.Profiles = map[string]closureprotocol.EvidenceProfile{}
			},
			wantOne: ReasonProfileUnknown,
		},
		{
			name:    "observation path ungoverned",
			mutate:  func(r *closureprotocol.EvidenceReceipt, c *Context) { r.ObservationPath = "bash -c 'go test ./...'" },
			wantOne: ReasonObservationPathUngoverned,
		},
		{
			name:    "result binding mismatch",
			mutate:  func(r *closureprotocol.EvidenceReceipt, c *Context) { r.ResultBinding.ResultTreeDigestSHA256 = "other-tree" },
			wantOne: ReasonResultBindingMismatch,
		},
		{
			name:    "trust insufficient",
			mutate:  func(r *closureprotocol.EvidenceReceipt, c *Context) { r.Trust = TrustUnattested },
			wantOne: ReasonTrustInsufficient,
		},
		{
			name:    "freshness expired",
			mutate:  func(r *closureprotocol.EvidenceReceipt, c *Context) { r.ExpiresAt = "2026-07-15T11:00:00Z" },
			wantOne: ReasonFreshnessExpired,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := okReceipt("receipt.a", prof.ProfileID, string(closureprotocol.EvidenceTest))
			ctx := testCtx(profiles, nil)
			tc.mutate(&r, &ctx)
			ctx.Receipts = []closureprotocol.EvidenceReceipt{r}
			p := ctx.Profiles[r.ProfileID]
			ok, reasons := CheckCompatibility(ob, slot, r, p, ctx)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (reasons=%v)", ok, tc.wantOK, reasons)
			}
			if !tc.wantOK {
				if len(reasons) == 0 || reasons[0] != tc.wantOne {
					t.Fatalf("reasons = %v, want %q", reasons, tc.wantOne)
				}
			}
		})
	}
}

func TestAllowedEvidenceKinds(t *testing.T) {
	if evidenceKindAllowed(SlotKindRuntime, closureprotocol.EvidenceStatic) {
		t.Fatal("static must not be allowed for runtime slot")
	}
	if !evidenceKindAllowed(SlotKindRuntime, closureprotocol.EvidenceRuntime) {
		t.Fatal("runtime must be allowed for runtime slot")
	}
	if !evidenceKindAllowed(SlotKindFailureEvidence, closureprotocol.EvidenceHybrid) {
		t.Fatal("hybrid must be allowed for failure_evidence")
	}
	if evidenceKindAllowed(SlotKindStaticGuard, closureprotocol.EvidenceAuthority) {
		t.Fatal("authority must never be allowed")
	}
}

func TestResolveSlotDisposition(t *testing.T) {
	ob := testOb("ob.x")
	if got := ResolveSlotDisposition(ob, ProofSlotSpec{Kind: SlotKindRuntime, Required: true}, CoverageStaticTest); got != SlotNotApplicableUnderProfile {
		t.Fatalf("runtime slot under static_test = %v, want not_applicable", got)
	}
	if got := ResolveSlotDisposition(ob, ProofSlotSpec{Kind: SlotKindRuntime, Required: true}, CoverageStaticTestRuntime); got != SlotRequired {
		t.Fatalf("runtime slot under static_test_runtime = %v, want required", got)
	}
	// Governed mandate overrides the default profile: runtime slot stays required
	// even under static_test.
	mandated := ProofObligation{ID: "ob.m", RequiresRuntimeEvidence: true}
	if got := ResolveSlotDisposition(mandated, ProofSlotSpec{Kind: SlotKindRuntime, Required: true}, CoverageStaticTest); got != SlotRequired {
		t.Fatalf("mandated runtime slot under static_test = %v, want required (not relaxed)", got)
	}
	if got := ResolveSlotDisposition(ob, ProofSlotSpec{Kind: SlotKindTestOrRuntime, Required: true}, CoverageStaticTest); got != SlotRequired {
		t.Fatalf("test_or_runtime slot = %v, want required (never relaxed)", got)
	}
	if got := ResolveSlotDisposition(ob, ProofSlotSpec{Kind: SlotKindStaticGuard, Required: false}, CoverageStaticTest); got != SlotOptional {
		t.Fatalf("non-required slot = %v, want optional", got)
	}
}
