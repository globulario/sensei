// SPDX-License-Identifier: AGPL-3.0-only

package candidateapply

import (
	"sort"
	"strings"

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

func NormalizeVerificationRecord(in VerificationRecord) VerificationRecord {
	r := in
	r.SchemaVersion = strings.TrimSpace(r.SchemaVersion)
	r.RecordID = strings.TrimSpace(r.RecordID)
	r.GeneratedBy = strings.TrimSpace(r.GeneratedBy)
	r.AdmissionVerificationStatus = strings.TrimSpace(r.AdmissionVerificationStatus)
	r.ObservedAt = strings.TrimSpace(r.ObservedAt)
	return r
}

// VerificationRecordDigest is the record's semantic identity.
//
// ObservedAt is excluded for the same reason the application receipt excludes
// CompletedAt: recording the same verification against the same application at
// a different moment is the same fact, and letting the clock into the identity
// would make an idempotent re-record look like a second, different record.
func VerificationRecordDigest(in VerificationRecord) (string, error) {
	r := NormalizeVerificationRecord(in)
	r.RecordDigestSHA256 = ""
	r.ObservedAt = ""
	return closureprotocol.SemanticDigest(r)
}
