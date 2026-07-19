// SPDX-License-Identifier: Apache-2.0

package evidencereceipt

import (
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// ProofRequest is the context a validator needs to decide whether a receipt is
// current and valid: the governing profile, the authoritative result binding
// the proof is about, an optional expected runtime target, and the evaluation
// time. Empty fields on ExpectedResult are treated as "do not pin".
type ProofRequest struct {
	Profile        Profile
	ExpectedResult ResultBinding
	RuntimeTarget  *RuntimeTarget
	Now            time.Time
}

// Validate computes the effective status of a receipt against a proof request.
// It is fail-closed: any failure yields INVALID / STALE / UNKNOWN and never a
// silent PASS. The receipt's self-declared status is ignored except for the
// terminal registry states revoked and superseded, which the validator honors.
//
// Check order (most fundamental first):
//  1. terminal registry status (revoked / superseded)
//  2. structural shape
//  3. profile identity
//  4. owner-path (non-owner-path receipts cannot satisfy an owner-only profile)
//  5. result-binding identity (wrong repository / result tree / graph)
//  6. runtime binding (missing / wrong cluster or generation)
//  7. freshness (expired -> stale; unobservable -> unknown)
func Validate(req ProofRequest, receipt Receipt) Assessment {
	switch receipt.Status {
	case closureprotocol.ReceiptRevoked:
		return assess(receipt, closureprotocol.ReceiptRevoked, ReasonReceiptRevoked)
	case closureprotocol.ReceiptSuperseded:
		return assess(receipt, closureprotocol.ReceiptSuperseded, ReasonReceiptSuperseded)
	}

	if strings.TrimSpace(receipt.ReceiptID) == "" ||
		strings.TrimSpace(receipt.ProfileID) == "" ||
		!isEvidenceKind(receipt.EvidenceKind) ||
		strings.TrimSpace(receipt.PayloadDigestSHA256) == "" {
		return assess(receipt, closureprotocol.ReceiptInvalid, ReasonReceiptMalformed)
	}

	if pid := strings.TrimSpace(req.Profile.ProfileID); pid != "" && pid != strings.TrimSpace(receipt.ProfileID) {
		return assess(receipt, closureprotocol.ReceiptInvalid, ReasonProfileMismatch)
	}

	if !observationPathSatisfies(req.Profile.LegalObservationPath, receipt.ObservationPath) {
		return assess(receipt, closureprotocol.ReceiptInvalid, ReasonOwnerPathViolation)
	}

	if code, ok := checkResultBinding(req.ExpectedResult, receipt.ResultBinding); !ok {
		return assess(receipt, closureprotocol.ReceiptInvalid, code)
	}

	if code, ok := checkRuntime(req.Profile, req.RuntimeTarget, receipt); !ok {
		return assess(receipt, closureprotocol.ReceiptInvalid, code)
	}

	win, err := ParseFreshness(firstNonEmpty(req.Profile.Freshness, string(FreshnessPerResult)))
	if err != nil {
		// An unparseable freshness window cannot establish currency.
		return assess(receipt, closureprotocol.ReceiptUnknown, ReasonFreshnessUnobserved)
	}
	if status, code := evaluateFreshness(win, receipt, req.Now); status != "" {
		return assess(receipt, status, code)
	}

	return assess(receipt, closureprotocol.ReceiptValid)
}

// observationPathSatisfies reports whether a receipt's observation path is the
// profile's legal (owner) path. The legal path may be namespaced as
// "<mechanism>.<path>"; a receipt path equal to the full string, to a trailing
// segment, or extending it is accepted. An empty legal or actual path never
// satisfies an owner-only profile.
func observationPathSatisfies(legal, actual string) bool {
	legal = strings.TrimSpace(legal)
	actual = strings.TrimSpace(actual)
	if legal == "" || actual == "" {
		return false
	}
	if legal == actual {
		return true
	}
	if strings.HasSuffix(legal, "."+actual) {
		return true
	}
	if strings.HasPrefix(actual, legal+".") {
		return true
	}
	return false
}

// checkResultBinding compares a receipt's result binding against the
// authoritative one. Only fields the expected binding pins are compared.
func checkResultBinding(expected, actual ResultBinding) (string, bool) {
	if e := strings.TrimSpace(expected.BaseRevision); e != "" && e != strings.TrimSpace(actual.BaseRevision) {
		return ReasonRepositoryMismatch, false
	}
	if e := strings.TrimSpace(expected.ResultTreeDigestSHA256); e != "" && e != strings.TrimSpace(actual.ResultTreeDigestSHA256) {
		return ReasonResultTreeMismatch, false
	}
	if e := strings.TrimSpace(expected.GraphDigestSHA256); e != "" && e != strings.TrimSpace(actual.GraphDigestSHA256) {
		return ReasonGraphMismatch, false
	}
	return "", true
}

// evaluateFreshness returns ("", "") when the receipt is fresh, or a
// non-valid status with a reason code otherwise.
func evaluateFreshness(win FreshnessWindow, receipt Receipt, now time.Time) (Status, string) {
	observed, obsErr := time.Parse(time.RFC3339, strings.TrimSpace(receipt.ObservedAt))

	expires := time.Time{}
	hasExpiry := false
	if s := strings.TrimSpace(receipt.ExpiresAt); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return closureprotocol.ReceiptUnknown, ReasonFreshnessUnobserved
		}
		expires, hasExpiry = t, true
	}

	switch win.Mode {
	case FreshnessSelfDeclared:
		// A self-declared receipt is only trusted with a real observation and a
		// bounded expiry; without an observed time it is UNKNOWN, never a PASS.
		if obsErr != nil || !hasExpiry {
			return closureprotocol.ReceiptUnknown, ReasonFreshnessUnobserved
		}
	case FreshnessDuration:
		if obsErr != nil {
			return closureprotocol.ReceiptUnknown, ReasonFreshnessUnobserved
		}
		if !hasExpiry {
			expires, hasExpiry = observed.Add(win.Duration), true
		}
	case FreshnessPerResult:
		// Validity is tied to the (already-verified) result binding. An explicit
		// expiry still bounds it; without either an observed time or an expiry
		// there is nothing to establish currency.
		if obsErr != nil && !hasExpiry {
			return closureprotocol.ReceiptUnknown, ReasonFreshnessUnobserved
		}
	}

	if hasExpiry {
		ref := now
		if ref.IsZero() {
			ref = time.Now().UTC()
		}
		if !expires.After(ref) {
			return closureprotocol.ReceiptStale, ReasonReceiptExpired
		}
	}
	return "", ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
