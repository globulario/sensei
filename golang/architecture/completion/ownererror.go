// SPDX-License-Identifier: AGPL-3.0-only

package completion

import "errors"

// Phase 9.4b: typed classification of why a completion projection could not be
// established. An enforcement surface must treat an absent/invalid task IDENTITY
// (a caller failure — block under enforce) differently from a RUNTIME failure of an
// already-resolved-and-invoked owner (a Sensei outage — degraded pass). The two must
// never be conflated, and the distinction must NOT be recovered by parsing error text.
//
// The runtime class is granted ONLY on POSITIVE evidence — a typed *ProjectionRuntimeError
// minted at the owner-invocation boundary AFTER identity was validated. It is never
// granted by the mere ABSENCE of an identity error. Every error lacking that positive
// evidence — an untyped error, a pre-invocation error, an unknown cause — falls to the
// generic projection_owner_error class, which is fail-closed (blocks) under enforcement.

// ProjectionIdentityError marks a completion build failure caused by an absent,
// malformed, noncanonical, out-of-scope, contradictory, or otherwise invalid task
// identity — a condition fatal BEFORE a legitimate owner invocation can begin. It is
// never a runtime/availability failure and must never reach a degraded-pass lane.
type ProjectionIdentityError struct {
	// Reason is a short, stable identity-failure reason (not a free-form message).
	Reason string
	err    error
}

func (e *ProjectionIdentityError) Error() string {
	if e == nil {
		return "projection identity error"
	}
	if e.err != nil {
		return e.err.Error()
	}
	return e.Reason
}

func (e *ProjectionIdentityError) Unwrap() error { return e.err }

// identityError wraps err as a typed identity failure with a stable reason. A nil err
// yields nil so callers can `return identityError(reason, validate(...))` transparently.
func identityError(reason string, err error) error {
	if err == nil {
		return nil
	}
	return &ProjectionIdentityError{Reason: reason, err: err}
}

// IsProjectionIdentityError reports whether err (or anything it wraps) is a typed
// identity failure. It is the TYPED test — never a string match.
func IsProjectionIdentityError(err error) bool {
	var id *ProjectionIdentityError
	return errors.As(err, &id)
}

// ProjectionRuntimeError is POSITIVE evidence that the canonical owner was resolved and
// its invocation was ATTEMPTED and then failed at runtime (execution, transport, I/O,
// timeout, decoding). It is minted ONLY at the owner-invocation boundary (see
// invokeCompletionOwner), which runs only AFTER identity has been validated — so its
// presence proves invocation occurred. It is the ONLY error type that earns the runtime
// (degraded-pass) class.
type ProjectionRuntimeError struct {
	err error
}

func (e *ProjectionRuntimeError) Error() string {
	if e == nil || e.err == nil {
		return "projection runtime error"
	}
	return e.err.Error()
}

func (e *ProjectionRuntimeError) Unwrap() error { return e.err }

// IsProjectionRuntimeError reports whether err (or anything it wraps) carries positive
// runtime evidence.
func IsProjectionRuntimeError(err error) bool {
	var rt *ProjectionRuntimeError
	return errors.As(err, &rt)
}

// runtimeError marks err as positive runtime evidence — UNLESS err is itself a typed
// identity failure, in which case identity WINS and the error stays identity (an
// identity failure can never be laundered into runtime, even at the invocation
// boundary). A nil err yields nil.
func runtimeError(err error) error {
	if err == nil {
		return nil
	}
	if IsProjectionIdentityError(err) {
		return err
	}
	return &ProjectionRuntimeError{err: err}
}

// ProjectionOwnerErrorClass classifies a projection-owner build error into its typed
// availability cause by POSITIVE typed evidence, never by absence:
//   - a typed identity failure               → identity (block under enforce);
//   - positive runtime evidence              → runtime (degraded pass);
//   - anything else (untyped, pre-invocation,
//     unknown)                               → the generic projection_owner_error class,
//     which is fail-closed (blocks) under enforcement.
//
// Runtime is NEVER assigned merely because the error is not an identity error.
func ProjectionOwnerErrorClass(err error) CompletionUnavailableClass {
	if err == nil {
		return ""
	}
	if IsProjectionIdentityError(err) {
		return UnavailableProjectionOwnerIdentityError
	}
	if IsProjectionRuntimeError(err) {
		return UnavailableProjectionOwnerRuntimeError
	}
	// No positive runtime evidence — fail closed.
	return UnavailableProjectionOwnerError
}
