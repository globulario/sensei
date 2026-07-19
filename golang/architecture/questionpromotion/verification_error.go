// SPDX-License-Identifier: AGPL-3.0-only

package questionpromotion

import "errors"

// VerificationFailureClass is the CLOSED typed cause of a committed-promotion verification
// failure, exposed so a consumer classifies WITHOUT parsing error text. The existing
// human-readable Error() is preserved for compatibility.
type VerificationFailureClass string

const (
	// VerifyIncomplete: the committed-promotion conjunction is not complete (not committed,
	// missing prerequisite event, or a required governed record is absent).
	VerifyIncomplete VerificationFailureClass = "incomplete"
	// VerifyIntegrityFailure: a durable artifact is tampered/corrupt/inconsistent (receipt,
	// commit payload, causal identity, lineage, governed mutation identity, provenance chain).
	VerifyIntegrityFailure VerificationFailureClass = "integrity_failure"
	// VerifyStale: the persisted graph moved on from the receipt-bound world.
	VerifyStale VerificationFailureClass = "stale"
	// VerifyUnverifiable: a read-only verification dependency was unavailable (I/O, graph
	// reverify facility) — distinct from a candidate being invalid.
	VerifyUnverifiable VerificationFailureClass = "unverifiable"
)

// VerificationImpact is the CLOSED typed SCOPE of a verification failure, distinct from its
// cause. It answers "is this failure specific to THIS candidate, or is a shared verification
// facility unavailable so NOTHING can be verified right now?" — so a consumer degrades one
// candidate versus reporting a global outage WITHOUT parsing error text.
type VerificationImpact string

const (
	// VerificationCandidateLocal: the failure is a definitive property of THIS candidate
	// (tampered/incomplete/stale/invalid) or a read of this candidate's own dependency.
	VerificationCandidateLocal VerificationImpact = "candidate_local"
	// VerificationFacilityUnavailable: a SHARED verification facility (the persisted-graph
	// reverify facility, a shared marker dependency) was unavailable, so no candidate can be
	// verified right now. This is a global outage, not a per-candidate defect.
	VerificationFacilityUnavailable VerificationImpact = "facility_unavailable"
)

// VerificationError is the typed verification failure. Callers that only check err != nil
// remain compatible; Error()/Unwrap() are preserved; a typed consumer uses AsVerificationError.
type VerificationError struct {
	Class      VerificationFailureClass
	ReasonCode string
	Impact     VerificationImpact
	Cause      error
	message    string
}

func (e *VerificationError) Error() string {
	if e == nil || e.message == "" {
		return "promotion verification failed"
	}
	return e.message
}

func (e *VerificationError) Unwrap() error { return e.Cause }

// AsVerificationError extracts the typed verification cause, if present.
func AsVerificationError(err error) (*VerificationError, bool) {
	var ve *VerificationError
	if errors.As(err, &ve) {
		return ve, true
	}
	return nil, false
}

// vfail builds a typed CANDIDATE-LOCAL verification failure whose Error() preserves the
// original message. Most verification failures are definitive properties of the candidate.
func vfail(class VerificationFailureClass, reason, msg string, cause error) error {
	return vfailImpact(class, reason, VerificationCandidateLocal, msg, cause)
}

// vfailFacility builds a typed FACILITY-UNAVAILABLE verification failure: a shared
// verification dependency was unavailable, so the outcome is not a property of the candidate.
func vfailFacility(class VerificationFailureClass, reason, msg string, cause error) error {
	return vfailImpact(class, reason, VerificationFacilityUnavailable, msg, cause)
}

func vfailImpact(class VerificationFailureClass, reason string, impact VerificationImpact, msg string, cause error) error {
	full := msg
	switch {
	case msg != "" && cause != nil:
		full = msg + ": " + cause.Error()
	case msg == "" && cause != nil:
		full = cause.Error()
	}
	return &VerificationError{Class: class, ReasonCode: reason, Impact: impact, Cause: cause, message: full}
}
