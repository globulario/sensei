// SPDX-License-Identifier: AGPL-3.0-only

package certification

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/proofdischarge"
)

func reverseStrings(in []string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[len(in)-1-i] = v
	}
	return out
}

// A richer bundle (two evidence receipts, waiver, obligation slots) so that
// reordering actually has material to shuffle.
func richBundle(t *testing.T) (Request, Records) {
	t.Helper()
	req, rec := greenBundle(t)
	rec = withRuntimeObligation(t, rec, true, closureprotocol.DimensionPass, []string{"receipt.runtime.core"})
	rec.EvidenceReceipts = append(rec.EvidenceReceipts, runtimeReceipt())
	req = rebindGreen(t, rec)
	return req, rec
}

func TestDeterminism_ReorderedInputsProduceIdenticalReceipt(t *testing.T) {
	req, rec := richBundle(t)
	baseline := mustEvaluate(t, req, rec, DefaultPolicy())

	// Reverse every slice on the request and the records.
	shuffledReq := req
	shuffledReq.AuthorityResolutionDigests = reverseStrings(req.AuthorityResolutionDigests)
	shuffledReq.ProofDischargeDigests = reverseStrings(req.ProofDischargeDigests)
	shuffledReq.ProofObligationDigests = reverseStrings(req.ProofObligationDigests)
	shuffledReq.EvidenceProfileDigests = reverseStrings(req.EvidenceProfileDigests)
	shuffledReq.EvidenceReceiptDigests = reverseStrings(req.EvidenceReceiptDigests)

	shuffledRec := rec
	shuffledRec.EvidenceReceipts = append([]closureprotocol.EvidenceReceipt(nil), rec.EvidenceReceipts...)
	for i, j := 0, len(shuffledRec.EvidenceReceipts)-1; i < j; i, j = i+1, j-1 {
		shuffledRec.EvidenceReceipts[i], shuffledRec.EvidenceReceipts[j] = shuffledRec.EvidenceReceipts[j], shuffledRec.EvidenceReceipts[i]
	}
	shuffledRec.EvidenceProfiles = append([]closureprotocol.EvidenceProfile(nil), rec.EvidenceProfiles...)
	for i, j := 0, len(shuffledRec.EvidenceProfiles)-1; i < j; i, j = i+1, j-1 {
		shuffledRec.EvidenceProfiles[i], shuffledRec.EvidenceProfiles[j] = shuffledRec.EvidenceProfiles[j], shuffledRec.EvidenceProfiles[i]
	}
	shuffledRec.Obligations = append([]proofdischarge.ProofObligation(nil), rec.Obligations...)

	shuffled := mustEvaluate(t, shuffledReq, shuffledRec, DefaultPolicy())
	if baseline.Receipt.DigestSHA256 != shuffled.Receipt.DigestSHA256 {
		t.Fatalf("reordering changed the receipt digest: %s vs %s", baseline.Receipt.DigestSHA256, shuffled.Receipt.DigestSHA256)
	}
	if baseline.Receipt.CertificationVerdict != shuffled.Receipt.CertificationVerdict {
		t.Fatalf("reordering changed the verdict")
	}
}

func TestDeterminism_RepeatedEvaluationsMatch(t *testing.T) {
	req, rec := richBundle(t)
	var digests []string
	for i := 0; i < 3; i++ {
		result := mustEvaluate(t, req, rec, DefaultPolicy())
		digests = append(digests, result.Receipt.DigestSHA256)
	}
	if digests[0] != digests[1] || digests[1] != digests[2] {
		t.Fatalf("repeated evaluations diverged: %v", digests)
	}
}

func TestDeterminism_DifferentTaskDirsProduceIdenticalReceipt(t *testing.T) {
	req, rec := richBundle(t)
	digestA := certifyInFreshDir(t, req, rec)
	digestB := certifyInFreshDir(t, req, rec)
	if digestA != digestB {
		t.Fatalf("temp path leaked into the receipt: %s vs %s", digestA, digestB)
	}
}
