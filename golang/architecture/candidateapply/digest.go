// SPDX-License-Identifier: AGPL-3.0-only

package candidateapply

import (
	"sort"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

func NormalizeRequest(in Request) Request {
	r := in
	if r.ModifyPaths == nil {
		r.ModifyPaths = []string{}
	}
	sort.Strings(r.ModifyPaths)
	return r
}

func NormalizeReceipt(in Receipt) Receipt {
	r := in
	if r.AppliedPaths == nil {
		r.AppliedPaths = []string{}
	}
	sort.Strings(r.AppliedPaths)
	return r
}

func RequestDigest(in Request) (string, error) {
	r := NormalizeRequest(in)
	r.RequestDigestSHA256 = ""
	return closureprotocol.SemanticDigest(r)
}

func ReceiptDigest(in Receipt) (string, error) {
	r := NormalizeReceipt(in)
	r.ReceiptDigestSHA256 = ""
	r.CompletedAt = ""
	return closureprotocol.SemanticDigest(r)
}
