// SPDX-License-Identifier: AGPL-3.0-only

package certification

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

func TestReceipt_DigestStableAndSelfExcluding(t *testing.T) {
	req, rec := greenBundle(t)
	first := mustEvaluate(t, req, rec, DefaultPolicy())
	second := mustEvaluate(t, req, rec, DefaultPolicy())
	if first.Receipt.DigestSHA256 == "" {
		t.Fatal("receipt digest is empty")
	}
	if first.Receipt.DigestSHA256 != second.Receipt.DigestSHA256 {
		t.Fatalf("digest not stable: %s vs %s", first.Receipt.DigestSHA256, second.Receipt.DigestSHA256)
	}
	recomputed, err := closureprotocol.CertificationReceiptDigest(first.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	if recomputed != first.Receipt.DigestSHA256 {
		t.Fatalf("self-excluding digest mismatch: %s vs %s", recomputed, first.Receipt.DigestSHA256)
	}
}

func TestReceipt_VerifyDetectsTamper(t *testing.T) {
	req, rec := greenBundle(t)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	if err := VerifyReceipt(result.Receipt); err != nil {
		t.Fatalf("valid receipt rejected: %v", err)
	}
	tampered := result.Receipt
	tampered.CertificationVerdict = closureprotocol.Certified
	tampered.ScopeLane = closureprotocol.DimensionPass
	tampered.Limitations = nil
	if tampered.DigestSHA256 == "" {
		t.Fatal("test setup: digest missing")
	}
	// Flip a lane on a copy while keeping the old digest.
	tampered.EvidenceLane = closureprotocol.DimensionBlocked
	if err := VerifyReceipt(tampered); err == nil {
		t.Fatal("tampered receipt passed verification")
	}
}

func TestReceipt_LaneDetailFoldsIntoLimitations(t *testing.T) {
	req, rec := greenBundle(t)
	rec.ProofDischarges = nil
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	found := false
	for _, limitation := range result.Receipt.Limitations {
		if limitation == ReasonProofMissingObligation+":proof.core" {
			found = true
		}
	}
	if !found {
		t.Fatalf("lane reason not folded into receipt limitations: %v", result.Receipt.Limitations)
	}
}
