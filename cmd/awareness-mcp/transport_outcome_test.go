// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The reproducer from #319, verbatim: this is what the three configured
// addresses actually returned when a six-file scoped preflight exceeded the
// budget while the evaluator was healthy the whole time.
func TestTheReportedFailureIsNotAnUnreachableBackend(t *testing.T) {
	failures := []addressOutcome{
		{Address: "localhost:10122", Outcome: string(classifyTransportError(
			status.Error(codes.Unavailable, "stream terminated by RST_STREAM with error code: CANCEL"))), Detail: "rst"},
		{Address: "localhost:10120", Outcome: string(classifyTransportError(
			status.Error(codes.Unavailable, "context deadline exceeded"))), Detail: "deadline"},
		{Address: "localhost:9090", Outcome: string(classifyTransportError(
			status.Error(codes.Unavailable, "context deadline exceeded"))), Detail: "deadline"},
	}
	if got := failures[0].Outcome; got != string(outcomeTransportReset) {
		t.Errorf("a CANCEL/RST_STREAM was classified %q; it is a reset, not an unreachable backend", got)
	}
	for _, f := range failures[1:] {
		if f.Outcome != string(outcomeDeadlineExceeded) {
			t.Errorf("%s: a context-deadline failure was classified %q", f.Address, f.Outcome)
		}
	}
	te := &transportError{Surface: "preflight", Aggregate: aggregateOutcome(failures), Addresses: failures}
	if te.Aggregate.assertsUnreachable() {
		t.Fatalf("the reported failure still claims the backend is unreachable: %q", te.Error())
	}
	if strings.Contains(te.Error(), "backend is unreachable") {
		t.Fatalf("the prose still asserts unreachability: %q", te.Error())
	}
	// The evaluator was available and correct throughout; the client gave up.
	if !strings.Contains(te.Error(), "not an empty/no-guidance result") {
		t.Error("the message no longer says this is not an absence of guidance, which is the other half of the claim")
	}
}

// A deadline is not unreachability, and the wire code must say so too.
func TestAllAddressesTimingOutIsDeadlineNotUnavailable(t *testing.T) {
	failures := []addressOutcome{
		{Address: "a", Outcome: string(outcomeDeadlineExceeded)},
		{Address: "b", Outcome: string(outcomeDeadlineExceeded)},
	}
	agg := aggregateOutcome(failures)
	if agg != outcomeDeadlineExceeded {
		t.Fatalf("unanimous deadlines aggregated to %q", agg)
	}
	if got := codeFor(agg); got != codes.DeadlineExceeded {
		t.Fatalf("unanimous deadlines carry gRPC code %v; Unavailable would relabel a timeout as a dead backend", got)
	}
}

// Disagreement must not become certainty. A set of addresses that failed in
// DIFFERENT ways supports no single sentence about the backend, so the
// aggregate is `unclassified` rather than the worst or the first member.
func TestMixedOutcomesDoNotBecomeAConfidentClaim(t *testing.T) {
	agg := aggregateOutcome([]addressOutcome{
		{Address: "a", Outcome: string(outcomeUnreachable)},
		{Address: "b", Outcome: string(outcomeDeadlineExceeded)},
	})
	if agg != outcomeUnclassified {
		t.Fatalf("mixed outcomes aggregated to %q — one address's failure spoke for the whole set", agg)
	}
	if agg.assertsUnreachable() {
		t.Fatal("an unclassified aggregate claimed the backend is unreachable")
	}
}

// The closed set is read by MEMBERSHIP. An unrecognised code is its own member,
// never folded into unreachable: "we could not tell" and "the backend is down"
// are the two answers this repair exists to separate.
func TestAnUnrecognisedFailureIsUnclassifiedNotUnreachable(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
	}{
		{"internal", status.Error(codes.Internal, "boom")},
		{"not found", status.Error(codes.NotFound, "nope")},
	} {
		if got := classifyTransportError(c.err); got != outcomeUnclassified {
			t.Errorf("%s classified as %q, want unclassified", c.name, got)
		}
		if classifyTransportError(c.err).assertsUnreachable() {
			t.Errorf("%s was allowed to assert unreachability", c.name)
		}
	}
	// A genuinely refused connection is still allowed to say so.
	if got := classifyTransportError(status.Error(codes.Unavailable, "connection refused")); got != outcomeUnreachable {
		t.Errorf("a refused connection classified as %q, want unreachable", got)
	}
}

// The classification must reach the caller as DATA. Parsing the prose in a
// replaceable downstream adapter is the repair that must not be applied.
func TestTheClassificationReachesTheJSONRPCSurfaceAsData(t *testing.T) {
	te := &transportError{
		Surface:   "preflight",
		Aggregate: outcomeDeadlineExceeded,
		Addresses: []addressOutcome{{Address: "localhost:10120", Outcome: string(outcomeDeadlineExceeded), Detail: "context deadline exceeded"}},
		Code:      codes.DeadlineExceeded,
	}
	raw, err := responsePayload(1, nil, te)
	if err != nil {
		t.Fatalf("responsePayload: %v", err)
	}
	var out struct {
		Error *struct {
			Message string                 `json:"message"`
			Data    map[string]interface{} `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Error == nil || out.Error.Data == nil {
		t.Fatalf("the error carries no structured data, so an agent can only regex the prose: %s", raw)
	}
	if out.Error.Data["outcome"] != string(outcomeDeadlineExceeded) {
		t.Errorf("outcome=%v, want %q", out.Error.Data["outcome"], outcomeDeadlineExceeded)
	}
	if out.Error.Data["asserts_unreachable"] != false {
		t.Errorf("asserts_unreachable=%v for a deadline", out.Error.Data["asserts_unreachable"])
	}
	if _, ok := out.Error.Data["addresses"].([]interface{}); !ok {
		t.Errorf("per-address outcomes did not survive to the surface: %s", raw)
	}
}

// The failover membership must not drift from the classification.
//
// These were two closed vocabularies -- a three-code list inside
// isTransportFailure and the transportOutcome set -- that had to agree with
// nothing making them agree, and they already disagreed. The predicate is now
// derived; this pins the membership so a future edit to either cannot silently
// change which failures trigger failover.
func TestFailoverMembershipIsExactlyTheClassifiedCodes(t *testing.T) {
	transport := []codes.Code{codes.Unavailable, codes.DeadlineExceeded, codes.Unauthenticated}
	for _, c := range transport {
		err := status.Error(c, "x")
		if !isTransportFailure(err) {
			t.Errorf("%v is no longer a transport failure — failover membership changed", c)
		}
		if classifyTransportError(err) == outcomeUnclassified {
			t.Errorf("%v is admitted by the gate but the classifier cannot name it", c)
		}
	}
	for _, c := range []codes.Code{
		codes.Internal, codes.NotFound, codes.PermissionDenied, codes.Canceled, codes.InvalidArgument,
	} {
		if isTransportFailure(status.Error(c, "x")) {
			t.Errorf("%v became a transport failure — failover now retries a call it used to return", c)
		}
	}
	// A bare non-gRPC error is not a transport failure, including a raw
	// context deadline: retrying another address on a dead context is waste.
	if isTransportFailure(errors.New("plain")) || isTransportFailure(context.DeadlineExceeded) {
		t.Error("a non-gRPC error became a transport failure")
	}
}
