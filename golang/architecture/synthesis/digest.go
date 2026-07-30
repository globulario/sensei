// SPDX-License-Identifier: AGPL-3.0-only

package synthesis

import (
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// SessionDigest returns the canonical content digest of a session document:
// closureprotocol.SemanticDigest of the normalized value with
// SessionDigestSHA256 zeroed first, so the digest never depends on itself.
// Reuses the repository's one existing canonicalization/digest convention.
func SessionDigest(s Session) (string, error) {
	s = NormalizeSession(s)
	s.SessionDigestSHA256 = ""
	return closureprotocol.SemanticDigest(s)
}

// InterpretationDigest returns the canonical content digest of an
// interpretation document.
func InterpretationDigest(in Interpretation) (string, error) {
	in = NormalizeInterpretation(in)
	in.InterpretationDigestSHA256 = ""
	return closureprotocol.SemanticDigest(in)
}

// PlanDigest returns the canonical content digest of a plan document.
func PlanDigest(p Plan) (string, error) {
	p = NormalizePlan(p)
	p.PlanDigestSHA256 = ""
	return closureprotocol.SemanticDigest(p)
}

// AttemptDigest returns the canonical content digest of an attempt
// document. ProducedAt is also zeroed before hashing — attempt identity is
// explicitly defined not to depend on wall-clock time (see the
// ProducedAt field doc comment in types.go and the O1 hard law that
// timestamps cannot influence authoritative transition identity unless
// explicitly included by contract).
func AttemptDigest(a Attempt) (string, error) {
	a = NormalizeAttempt(a)
	a.AttemptDigestSHA256 = ""
	a.ProducedAt = ""
	return closureprotocol.SemanticDigest(a)
}

// EvaluationDigest returns the canonical content digest of an evaluation
// document.
func EvaluationDigest(e Evaluation) (string, error) {
	e = NormalizeEvaluation(e)
	e.EvaluationDigestSHA256 = ""
	return closureprotocol.SemanticDigest(e)
}

// ReceiptDigest returns the canonical content digest of a receipt document.
func ReceiptDigest(r Receipt) (string, error) {
	r = NormalizeReceipt(r)
	r.ReceiptDigestSHA256 = ""
	return closureprotocol.SemanticDigest(r)
}
