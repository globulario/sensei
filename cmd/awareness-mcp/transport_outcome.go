// SPDX-License-Identifier: AGPL-3.0-only

package main

// A timeout is not an unreachable backend (#319).
//
// toolRPCError tested for Unavailable OR DeadlineExceeded and then emitted one
// sentence asserting "the awareness-graph backend is unreachable". The
// condition DISTINGUISHED THE TWO CODES AND THEN DISCARDED THE DISTINCTION.
// DeadlineExceeded means the call did not finish inside the budget; it says
// nothing whatever about reachability, and the sentence asserted reachability
// as a fact.
//
// callWithFailover collapsed it a second time and earlier: whatever the
// per-address failures actually were, it returned a MANUFACTURED
// codes.Unavailable. So every address timing out arrived at toolRPCError
// already relabelled as unreachable, and no amount of care downstream could
// recover what had been thrown away upstream.
//
// The cost is not cosmetic. A caller cannot tell "Sensei is down" from "this
// call was too slow", so the honest reactions -- retry with a longer budget,
// narrow the scope, report degraded coverage -- become indistinguishable from
// the one reaction that is not honest: conclude absence. Measured on the
// reporting machine, a 2-file scoped preflight took 2.7s and a 6-file one took
// 7.6s and failed every time, while the identical call succeeded by hand. An
// envelope of about six files could not be governed, for a reason nobody chose
// and nobody could see in the message.
//
// RAISING THE TIMEOUT IS NOT THE REPAIR. It moves the threshold and hides the
// false attribution until a larger envelope crosses the new one. Nothing here
// changes a deadline.

import (
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// transportOutcome is a CLOSED set, read by membership and never by exclusion.
// An error that matches no member is `unclassified` -- its own member -- rather
// than being folded into `unreachable`, because "we could not tell" and "the
// backend is down" are exactly the two answers this file exists to separate.
type transportOutcome string

const (
	outcomeUnreachable      transportOutcome = "unreachable"
	outcomeTransportReset   transportOutcome = "transport_reset"
	outcomeDeadlineExceeded transportOutcome = "client_deadline_exceeded"
	outcomeServerRefusal    transportOutcome = "server_refusal"
	outcomeUnclassified     transportOutcome = "unclassified"
)

// assertsUnreachable reports whether an outcome may be described to a caller as
// the backend being unreachable. Only one member may.
func (o transportOutcome) assertsUnreachable() bool { return o == outcomeUnreachable }

// isTransportFailure reports whether this outcome is one the failover loop
// should try the next address for.
//
// THIS IS THE ONE PLACE THAT DECIDES IT. There were two closed vocabularies
// here -- a three-code list in isTransportFailure and this five-member set --
// which had to agree and had nothing making them agree. They already disagreed:
// classifyTransportError answered for Canceled and PermissionDenied, codes the
// gate never admits, so those branches could not execute and a test covering
// them proved a path production never takes.
//
// Deriving the predicate from the classification makes every member reachable
// and leaves one vocabulary. The membership is unchanged: Unavailable,
// DeadlineExceeded and Unauthenticated are exactly the codes that map to a
// non-unclassified outcome.
func (o transportOutcome) isTransportFailure() bool { return o != outcomeUnclassified }

// sentence renders the outcome as a claim narrow enough to be true.
func (o transportOutcome) sentence() string {
	switch o {
	case outcomeUnreachable:
		return "the awareness-graph backend is unreachable"
	case outcomeTransportReset:
		return "the connection to the awareness-graph backend was reset"
	case outcomeDeadlineExceeded:
		return "the call did not finish inside the client deadline; the backend may be healthy and merely slower than the budget"
	case outcomeServerRefusal:
		return "the awareness-graph backend refused the call"
	default:
		return "the call failed for a reason this bridge could not classify"
	}
}

// classifyTransportError maps one gRPC error onto the closed set.
//
// DeadlineExceeded is checked BEFORE Unavailable. gRPC reports a client-side
// deadline on a healthy connection as DeadlineExceeded, and the RST_STREAM that
// a cancelled stream produces arrives as Unavailable with a CANCEL message --
// so the message is inspected to keep a cancellation caused by our own deadline
// from being reported as the backend going away.
func classifyTransportError(err error) transportOutcome {
	if err == nil {
		return outcomeUnclassified
	}
	st, ok := status.FromError(err)
	if !ok {
		// A NON-gRPC error stays unclassified, deliberately. Now that the
		// failover predicate is derived from this function, classifying a bare
		// context.DeadlineExceeded as a transport failure would make the loop
		// try the next address on a context that is already dead -- a
		// behaviour change smuggled in by a tidier classifier. The failover
		// membership is exactly what it was.
		return outcomeUnclassified
	}
	msg := strings.ToLower(st.Message())
	switch st.Code() {
	case codes.DeadlineExceeded:
		return outcomeDeadlineExceeded
	case codes.Unauthenticated:
		return outcomeServerRefusal
	case codes.Unavailable:
		switch {
		case strings.Contains(msg, "context deadline exceeded"):
			return outcomeDeadlineExceeded
		case strings.Contains(msg, "rst_stream"), strings.Contains(msg, "cancel"):
			return outcomeTransportReset
		default:
			return outcomeUnreachable
		}
	default:
		return outcomeUnclassified
	}
}

// addressOutcome is what happened at ONE configured address. The failover loop
// tries several, and reporting only the aggregate is how a single slow address
// came to speak for the whole set.
type addressOutcome struct {
	Address string `json:"address"`
	Outcome string `json:"outcome"`
	Detail  string `json:"detail"`
}

// transportError carries the per-address classification all the way to the
// JSON-RPC surface, so an agent reads a typed outcome instead of parsing prose.
//
// Parsing this prose downstream is the repair that MUST NOT be applied: the
// consuming adapter is replaceable and the classification originates here.
type transportError struct {
	Surface   string
	Aggregate transportOutcome
	Addresses []addressOutcome
	Code      codes.Code
}

func (e *transportError) Error() string {
	var b strings.Builder
	if e.Surface != "" {
		// "unavailable" is kept for the ONE outcome it is true of, so the
		// existing contract for a genuinely unreachable backend is unchanged.
		// Every other outcome says "failed" instead, because that is the word
		// whose meaning survives when the backend is up and merely slow.
		if e.Aggregate.assertsUnreachable() {
			b.WriteString(e.Surface + " unavailable: ")
		} else {
			b.WriteString(e.Surface + " failed: ")
		}
	}
	b.WriteString(e.Aggregate.sentence())
	b.WriteString("; this is not an empty/no-guidance result")
	if len(e.Addresses) > 0 {
		parts := make([]string, 0, len(e.Addresses))
		for _, a := range e.Addresses {
			if a.Address == "" {
				parts = append(parts, fmt.Sprintf("%s (%s)", a.Outcome, a.Detail))
				continue
			}
			parts = append(parts, fmt.Sprintf("%s: %s (%s)", a.Address, a.Outcome, a.Detail))
		}
		b.WriteString(": " + strings.Join(parts, "; "))
	}
	return b.String()
}

// GRPCStatus keeps the wire code honest. A set of addresses that all timed out
// is DeadlineExceeded, not Unavailable.
func (e *transportError) GRPCStatus() *status.Status {
	code := e.Code
	if code == codes.OK {
		code = codes.Unavailable
	}
	return status.New(code, e.Error())
}

// structured is the machine-readable surface #319 asks for.
func (e *transportError) structured() map[string]interface{} {
	addrs := make([]interface{}, 0, len(e.Addresses))
	for _, a := range e.Addresses {
		addrs = append(addrs, map[string]interface{}{
			"address": a.Address, "outcome": a.Outcome, "detail": a.Detail,
		})
	}
	return map[string]interface{}{
		"surface":             e.Surface,
		"outcome":             string(e.Aggregate),
		"asserts_unreachable": e.Aggregate.assertsUnreachable(),
		"addresses":           addrs,
	}
}

// aggregateOutcome reduces per-address outcomes to one claim WITHOUT inventing
// certainty. Unanimity carries; disagreement does not become the worst member,
// it becomes `unclassified`, because a set of addresses that failed differently
// supports no single sentence about the backend.
func aggregateOutcome(addrs []addressOutcome) transportOutcome {
	if len(addrs) == 0 {
		return outcomeUnclassified
	}
	first := transportOutcome(addrs[0].Outcome)
	for _, a := range addrs[1:] {
		if transportOutcome(a.Outcome) != first {
			return outcomeUnclassified
		}
	}
	return first
}

// codeFor maps an aggregate outcome back to the gRPC code that is true of it.
func codeFor(o transportOutcome) codes.Code {
	switch o {
	case outcomeDeadlineExceeded:
		return codes.DeadlineExceeded
	case outcomeServerRefusal:
		return codes.PermissionDenied
	case outcomeUnreachable, outcomeTransportReset:
		return codes.Unavailable
	default:
		return codes.Unknown
	}
}
