// SPDX-License-Identifier: Apache-2.0

package completion

import "errors"

// Phase 9.4b: typed classification of why a completion projection could not be
// established. An enforcement surface must treat an absent/invalid task IDENTITY
// (a caller failure — block under enforce) differently from a RUNTIME failure of an
// already-resolved owner (a Sensei outage — degraded pass). The two must never be
// conflated, and the distinction must NOT be recovered later by parsing error text.
//
// The mechanism: identity failures are minted as a typed *ProjectionIdentityError at
// the point they originate (the repository/task binding check, before any owner
// invocation). Any other build error is, by construction, a failure that occurred
// after identity was validated and the in-process owner was invoked — i.e. runtime.
// Classification is therefore by error TYPE, at the boundary, never by message text.

// ProjectionIdentityError marks a completion build failure caused by an absent,
// malformed, noncanonical, out-of-scope, contradictory, or otherwise invalid task
// identity — a condition that is fatal BEFORE a legitimate owner invocation can begin.
// It is never a runtime/availability failure and must never reach a degraded-pass lane.
type ProjectionIdentityError struct {
	// Reason is a short, stable identity-failure reason (not a free-form message).
	Reason string
	// err is the underlying detail, preserved for %w unwrapping.
	err error
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

// ProjectionOwnerErrorClass classifies a projection-owner build error into its typed
// availability cause. A typed identity failure (minted before owner invocation) is an
// identity error; ANY other error is a runtime failure of the invoked owner — because
// identity validation is the gate that precedes invocation, a non-identity error
// implies the owner was resolved and invoked and then failed at runtime. The
// classification depends only on the error's TYPE, never on its message, so identical
// outer wrapping over an identity vs a runtime cause still classifies distinctly.
func ProjectionOwnerErrorClass(err error) CompletionUnavailableClass {
	if err == nil {
		return ""
	}
	if IsProjectionIdentityError(err) {
		return UnavailableProjectionOwnerIdentityError
	}
	return UnavailableProjectionOwnerRuntimeError
}
