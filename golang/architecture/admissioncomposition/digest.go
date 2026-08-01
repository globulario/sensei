// SPDX-License-Identifier: AGPL-3.0-only

package admissioncomposition

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

func NormalizeRequest(in Request) Request {
	r := in
	if r.DerivedScope.Files == nil {
		r.DerivedScope.Files = []admission.FileOperation{}
	}
	if r.DerivedScope.Symbols == nil {
		r.DerivedScope.Symbols = []string{}
	}
	if r.DerivedScope.Components == nil {
		r.DerivedScope.Components = []string{}
	}
	if r.DerivedScope.ClaimIDs == nil {
		r.DerivedScope.ClaimIDs = []string{}
	}
	if r.DerivedScope.PropositionKeys == nil {
		r.DerivedScope.PropositionKeys = []string{}
	}
	if r.UnsupportedOperations == nil {
		r.UnsupportedOperations = []UnsupportedOperation{}
	}
	return r
}

func RequestDigest(in Request) (string, error) {
	r := NormalizeRequest(in)
	r.RequestDigestSHA256 = ""
	return closureprotocol.SemanticDigest(r)
}

func ReceiptDigest(in Receipt) (string, error) {
	r := in
	r.ReceiptDigestSHA256 = ""
	r.CompletedAt = ""
	return closureprotocol.SemanticDigest(r)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
