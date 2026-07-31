// SPDX-License-Identifier: AGPL-3.0-only

package providerport

import "context"

// Provider is the O2 provider-neutral execution port: the single
// capability-driven interface any intelligence provider implements to
// participate in a governed synthesis session, per
// docs/design/provider-neutral-execution-port-o2.md. A Provider grants
// itself nothing by implementing this interface -- Describe returns
// untrusted self-description (hard law 2), and Execute's Result is a
// distinct, untrusted document until a later, explicit, separately bounded
// mapping step accepts it into O1 (hard laws 1 and 3). Run (execution.go),
// not the Provider itself, decides the terminal outcome of any given
// attempt.
type Provider interface {
	// Describe returns the provider's self-declared capability snapshot.
	// The returned Capabilities is untrusted self-description: it proves
	// only that the provider claims support for the listed operations,
	// never eligibility, authority, or routing. Implementations MUST return
	// promptly once ctx is done -- Run bounds Describe under the same
	// request-deadline/cancellation mechanism as Execute (describeBounded in
	// execution.go), and does not wait for a Provider that ignores it.
	Describe(ctx context.Context) (Capabilities, error)

	// Execute performs one bounded request. Implementations MUST return
	// promptly once ctx is done -- Run enforces the request's precommitted
	// deadline and the caller's cancellation via ctx, and does not wait for
	// a Provider that ignores it. Implementations report bounded streaming
	// evidence through obs.Observe; Run enforces the request's
	// precommitted observation count/byte limits regardless of what a
	// Provider attempts to report.
	//
	// A non-nil error is for local/infrastructure failure only (the
	// Provider could not be reached, crashed, and so on) -- Run maps it to
	// OutcomeUnavailable. A Provider that ran to completion and has an
	// opinion about the outcome (success, invalid input it detected, and so
	// on) MUST report that opinion as data on the returned Result, never as
	// a Go error.
	Execute(ctx context.Context, request Request, obs Observer) (Result, error)
}

// Observer is how a Provider reports bounded streaming evidence during
// Execute. Run's implementation enforces the request's precommitted
// MaxObservationCount/MaxObservationBytes -- Observe returns an error once
// either limit would be exceeded, and no Provider can enlarge either limit
// by any means. Observations are evidence attached to the eventual
// Receipt/ObservationBatch; they never drive an O1 transition directly.
type Observer interface {
	Observe(detail string) error
}
