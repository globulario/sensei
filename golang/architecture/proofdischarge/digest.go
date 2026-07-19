// SPDX-License-Identifier: AGPL-3.0-only

package proofdischarge

import "github.com/globulario/sensei/golang/architecture/closureprotocol"

// WithDigest sets discharge_digest_sha256 via the frozen
// closureprotocol.ProofDischargeDigest (computed over the canonical form with the
// digest field cleared) and re-validates the result. It is a thin wrapper — the
// canonicalization and digest live in closureprotocol, not here.
func WithDigest(d closureprotocol.ProofDischarge) (closureprotocol.ProofDischarge, error) {
	digest, err := closureprotocol.ProofDischargeDigest(d)
	if err != nil {
		return closureprotocol.ProofDischarge{}, err
	}
	d.DischargeDigestSHA256 = digest
	if err := closureprotocol.ValidateProofDischarge(d); err != nil {
		return closureprotocol.ProofDischarge{}, err
	}
	return d, nil
}
