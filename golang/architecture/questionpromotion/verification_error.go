// SPDX-License-Identifier: Apache-2.0

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

// VerificationError is the typed verification failure. Callers that only check err != nil
// remain compatible; Error()/Unwrap() are preserved; a typed consumer uses AsVerificationError.
type VerificationError struct {
	Class      VerificationFailureClass
	ReasonCode string
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

// vfail builds a typed verification failure whose Error() preserves the original message.
func vfail(class VerificationFailureClass, reason, msg string, cause error) error {
	full := msg
	switch {
	case msg != "" && cause != nil:
		full = msg + ": " + cause.Error()
	case msg == "" && cause != nil:
		full = cause.Error()
	}
	return &VerificationError{Class: class, ReasonCode: reason, Cause: cause, message: full}
}
