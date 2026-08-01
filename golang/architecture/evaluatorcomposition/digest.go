// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import "github.com/globulario/sensei/golang/architecture/closureprotocol"

// EvaluationPolicyDigest returns the canonical content digest of an
// evaluation-policy document: closureprotocol.SemanticDigest of the
// normalized value with PolicyDigestSHA256 zeroed first, so the digest
// never depends on itself. Reuses the repository's one existing
// canonicalization/digest convention -- the same one O1, O2, and O3 use.
func EvaluationPolicyDigest(p EvaluationPolicy) (string, error) {
	p = NormalizeEvaluationPolicy(p)
	p.PolicyDigestSHA256 = ""
	return closureprotocol.SemanticDigest(p)
}

// EvaluatorDescriptorDigest returns the canonical content digest of an
// evaluator-descriptor document.
func EvaluatorDescriptorDigest(d EvaluatorDescriptor) (string, error) {
	d = NormalizeEvaluatorDescriptor(d)
	d.DescriptorDigestSHA256 = ""
	return closureprotocol.SemanticDigest(d)
}

// EvaluationInputDigest returns the canonical content digest of an
// evaluation-input document.
func EvaluationInputDigest(i EvaluationInput) (string, error) {
	i = NormalizeEvaluationInput(i)
	i.EvaluationInputDigestSHA256 = ""
	return closureprotocol.SemanticDigest(i)
}

// EvaluatorResultDigest returns the canonical content digest of an
// evaluator-result document.
func EvaluatorResultDigest(r EvaluatorResult) (string, error) {
	r = NormalizeEvaluatorResult(r)
	r.ResultDigestSHA256 = ""
	return closureprotocol.SemanticDigest(r)
}

// EvaluationReceiptDigest returns the canonical content digest of an
// evaluation-receipt document. CompletedAt is also zeroed before hashing --
// receipt identity is explicitly defined not to depend on wall-clock time,
// the same convention synthesis.Receipt and runnercomposition.RunnerReceipt
// already follow for their own completion timestamps.
func EvaluationReceiptDigest(r EvaluationReceipt) (string, error) {
	r = NormalizeEvaluationReceipt(r)
	r.ReceiptDigestSHA256 = ""
	r.CompletedAt = ""
	return closureprotocol.SemanticDigest(r)
}
