// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import "github.com/globulario/sensei/golang/architecture/closureprotocol"

// CandidateArtifactDigest returns the canonical content digest of a
// candidate-artifact document: closureprotocol.SemanticDigest of the
// normalized value with CandidateArtifactDigestSHA256 zeroed first, so the
// digest never depends on itself. Reuses the repository's one existing
// canonicalization/digest convention -- the same one O1 and O2 use.
func CandidateArtifactDigest(a CandidateArtifact) (string, error) {
	a = NormalizeCandidateArtifact(a)
	a.CandidateArtifactDigestSHA256 = ""
	return closureprotocol.SemanticDigest(a)
}

// RunnerReceiptDigest returns the canonical content digest of a runner-
// receipt document. CompletedAt is also zeroed before hashing -- receipt
// identity is explicitly defined not to depend on wall-clock time, the same
// convention synthesis.Receipt and providerport.Receipt already follow for
// their own completion timestamps.
func RunnerReceiptDigest(r RunnerReceipt) (string, error) {
	r = NormalizeRunnerReceipt(r)
	r.RunnerReceiptDigestSHA256 = ""
	r.CompletedAt = ""
	return closureprotocol.SemanticDigest(r)
}
