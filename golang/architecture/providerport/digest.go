// SPDX-License-Identifier: AGPL-3.0-only

package providerport

import "github.com/globulario/sensei/golang/architecture/closureprotocol"

// CapabilitiesDigest returns the canonical content digest of a capabilities
// document: closureprotocol.SemanticDigest of the normalized value with
// CapabilitiesDigestSHA256 zeroed first, so the digest never depends on
// itself. Reuses the repository's one existing canonicalization/digest
// convention.
func CapabilitiesDigest(c Capabilities) (string, error) {
	c = NormalizeCapabilities(c)
	c.CapabilitiesDigestSHA256 = ""
	return closureprotocol.SemanticDigest(c)
}

// RequestDigest returns the canonical content digest of a request document.
func RequestDigest(r Request) (string, error) {
	r = NormalizeRequest(r)
	r.RequestDigestSHA256 = ""
	return closureprotocol.SemanticDigest(r)
}

// ResultDigest returns the canonical content digest of a result document.
func ResultDigest(r Result) (string, error) {
	r = NormalizeResult(r)
	r.ResultDigestSHA256 = ""
	return closureprotocol.SemanticDigest(r)
}

// ObservationBatchDigest returns the canonical content digest of an
// observation-batch document.
func ObservationBatchDigest(b ObservationBatch) (string, error) {
	b = NormalizeObservationBatch(b)
	b.ObservationBatchDigestSHA256 = ""
	return closureprotocol.SemanticDigest(b)
}

// ReceiptDigest returns the canonical content digest of a receipt document.
// StartedAt/CompletedAt are also zeroed before hashing -- receipt identity
// is explicitly defined not to depend on wall-clock time (see the
// StartedAt/CompletedAt field doc comment in types.go).
func ReceiptDigest(r Receipt) (string, error) {
	r = NormalizeReceipt(r)
	r.ReceiptDigestSHA256 = ""
	r.StartedAt = ""
	r.CompletedAt = ""
	return closureprotocol.SemanticDigest(r)
}
