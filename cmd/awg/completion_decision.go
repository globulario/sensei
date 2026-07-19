// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/globulario/sensei/golang/architecture/completion"
)

// Phase 9.4b, Checkpoint 2: the pure completion-enforcement decision.
//
// Given a RESOLVED completion policy state (a malformed policy is rejected loudly by
// the caller, before this function), the publication validity, and the typed envelope
// (availability + verdict when available + unavailable cause when not), this decides
// pass / degraded pass / block and carries a STABLE typed reason code — callers never
// recover semantics from human-readable text.
//
// It is a pure function of its typed inputs: no I/O, no clock, no policy loading, and
// it never inspects any error message. All the frozen 9.4b safety rules live here.

// completionEnforceResult is the outcome of the enforcement decision.
type completionEnforceResult int

const (
	decisionPass completionEnforceResult = iota
	decisionDegradedPass
	decisionBlock
)

func (r completionEnforceResult) String() string {
	switch r {
	case decisionPass:
		return "pass"
	case decisionDegradedPass:
		return "degraded_pass"
	case decisionBlock:
		return "block"
	default:
		return "unknown"
	}
}

// completionEnforceReason is a STABLE, machine-stable reason code. Callers branch on
// this, never on the detail string.
type completionEnforceReason string

const (
	reasonNotEnforced           completionEnforceReason = "not_enforced"
	reasonAuthoritative         completionEnforceReason = "authoritative_completion"
	reasonNotCompletedRequired  completionEnforceReason = "not_completed_required"
	reasonBrokenCompletion      completionEnforceReason = "broken_completion"
	reasonContradictoryHistory  completionEnforceReason = "contradictory_terminal_history"
	reasonUnsupported           completionEnforceReason = "unsupported"
	reasonInvalidPublication    completionEnforceReason = "invalid_publication"
	reasonIdentityInvalid       completionEnforceReason = "identity_invalid"
	reasonRuntimeDegraded       completionEnforceReason = "runtime_unavailable_degraded"
	reasonUnknownClassification completionEnforceReason = "unknown_classification"
)

// completionDecision is the typed decision result.
type completionDecision struct {
	Result completionEnforceResult
	Reason completionEnforceReason
}

// completionEnforceInput is the typed input to the decision. It carries no error text
// and no policy file — only resolved, typed facts.
type completionEnforceInput struct {
	// PolicyState is the resolved policy state. It is never completionPolicyInvalid here:
	// a malformed policy is rejected by the caller before any decision is made.
	PolicyState completionPolicyState
	// PublicationValid reports whether the completion publication is canonically valid.
	PublicationValid bool
	// Availability, Verdict, and UnavailableClass are read straight from the typed
	// envelope. Verdict is meaningful only when available; UnavailableClass only when not.
	Availability     completion.CompletionAvailability
	Verdict          completion.ClosureVerdict
	UnavailableClass completion.CompletionUnavailableClass
}

// decideCompletionEnforcement applies the frozen 9.4b decision table.
func decideCompletionEnforcement(in completionEnforceInput) completionDecision {
	// Policy absent or completion not required → preserve existing (advisory) behavior:
	// the enforcement path creates no block. authoritative and not_completed both pass;
	// pathological verdicts are surfaced advisory elsewhere, not blocked here.
	if in.PolicyState != completionPolicyPresentRequired {
		return completionDecision{Result: decisionPass, Reason: reasonNotEnforced}
	}

	// From here, completion is REQUIRED for this domain.

	// A surface that cannot even present a canonical verdict must not silently pass —
	// invalid publication blocks and can never degrade to a pass.
	if !in.PublicationValid {
		return completionDecision{Result: decisionBlock, Reason: reasonInvalidPublication}
	}

	switch in.Availability {
	case completion.CompletionAvailable:
		switch in.Verdict {
		case completion.ClosureAuthoritativeCompletion:
			return completionDecision{Result: decisionPass, Reason: reasonAuthoritative}
		case completion.ClosureNotCompleted:
			return completionDecision{Result: decisionBlock, Reason: reasonNotCompletedRequired}
		case completion.ClosureBroken:
			return completionDecision{Result: decisionBlock, Reason: reasonBrokenCompletion}
		case completion.ClosureContradictory:
			return completionDecision{Result: decisionBlock, Reason: reasonContradictoryHistory}
		case completion.ClosureUnsupported:
			return completionDecision{Result: decisionBlock, Reason: reasonUnsupported}
		default:
			// An unknown/off-vocabulary verdict fails closed — never reclassified.
			return completionDecision{Result: decisionBlock, Reason: reasonUnknownClassification}
		}
	case completion.CompletionUnavailable:
		switch in.UnavailableClass {
		case completion.UnavailableProjectionOwnerRuntimeError:
			// The ONLY degraded lane: identity was valid, the owner was invoked, and it
			// failed at runtime. This is the already-frozen degraded-runtime behavior.
			return completionDecision{Result: decisionDegradedPass, Reason: reasonRuntimeDegraded}
		case completion.UnavailableTaskDirectoryUnresolved,
			completion.UnavailableProjectionOwnerIdentityError:
			// An absent/invalid task identity blocks and can NEVER reach the degraded lane.
			return completionDecision{Result: decisionBlock, Reason: reasonIdentityInvalid}
		default:
			// Any other unavailable class — including the generic, unclassified
			// projection_owner_error and any unknown class — fails closed. No new
			// permissive runtime fallback is created.
			return completionDecision{Result: decisionBlock, Reason: reasonUnknownClassification}
		}
	default:
		// Off-vocabulary availability fails closed.
		return completionDecision{Result: decisionBlock, Reason: reasonUnknownClassification}
	}
}
