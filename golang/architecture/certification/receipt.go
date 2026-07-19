// SPDX-License-Identifier: Apache-2.0

package certification

import (
	"fmt"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// buildReceipt folds the per-lane results onto the frozen CertificationReceipt
// shape: the four lane statuses, the applicable forbidden moves, the union of
// unresolved contradictions, and every reason/limitation code flattened into
// Limitations (the frozen schema keeps no per-lane detail; Result.Lanes does).
// The receipt digest is the frozen self-excluding semantic digest.
func buildReceipt(req Request, policy CertificationPolicy, lanes [4]LaneResult, forbidden []string, verdict closureprotocol.CertificationVerdict) (closureprotocol.CertificationReceipt, error) {
	var contradictions, limitations []string
	for _, lane := range lanes {
		contradictions = append(contradictions, lane.Contradictions...)
		limitations = append(limitations, lane.ReasonCodes...)
		limitations = append(limitations, lane.Limitations...)
	}
	receipt := closureprotocol.CertificationReceipt{
		ResultBinding:            req.ResultBinding,
		CertificationPolicy:      policy.PolicyID,
		ScopeLane:                lanes[0].Status,
		AuthorityLane:            lanes[1].Status,
		ProofLane:                lanes[2].Status,
		EvidenceLane:             lanes[3].Status,
		ForbiddenMoves:           closureprotocol.NormalizeSet(forbidden),
		UnresolvedContradictions: closureprotocol.NormalizeSet(contradictions),
		Limitations:              closureprotocol.NormalizeSet(limitations),
		CertificationVerdict:     verdict,
	}
	digest, err := closureprotocol.CertificationReceiptDigest(receipt)
	if err != nil {
		return closureprotocol.CertificationReceipt{}, err
	}
	receipt.DigestSHA256 = digest
	if err := closureprotocol.ValidateCertificationReceipt(receipt); err != nil {
		return closureprotocol.CertificationReceipt{}, err
	}
	return receipt, nil
}

// VerifyReceipt re-validates a persisted certification receipt: frozen shape
// plus an honest self-excluding digest. Phase 8 must call this (and recompute
// the digest from the persisted bytes) before trusting a receipt reference.
func VerifyReceipt(receipt closureprotocol.CertificationReceipt) error {
	if err := closureprotocol.ValidateCertificationReceipt(receipt); err != nil {
		return err
	}
	digest, err := closureprotocol.CertificationReceiptDigest(receipt)
	if err != nil {
		return err
	}
	if receipt.DigestSHA256 != digest {
		return fmt.Errorf("%w: certification receipt (claimed %s, actual %s)", ErrRecordDigestMismatch, receipt.DigestSHA256, digest)
	}
	return nil
}
