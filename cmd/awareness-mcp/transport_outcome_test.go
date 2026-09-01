// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"errors"
	awarenesspb "github.com/globulario/sensei/golang/pb"
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

// The two questions are separate, and each is pinned.
//
// An earlier revision of this branch derived the failover predicate FROM the
// classification, on the theory that a second code list was a duplicated
// vocabulary. Review showed they are different questions and cannot be one set:
// server_refusal covers Unauthenticated (which retries) and PermissionDenied
// (which must not). Collapsing them made server_refusal unreachable for a
// permission failure.
func TestClassificationAndRetryAreDifferentQuestions(t *testing.T) {
	// RETRY POLICY: unchanged, and narrow.
	for _, c := range []codes.Code{codes.Unavailable, codes.DeadlineExceeded, codes.Unauthenticated} {
		if !isTransportFailure(status.Error(c, "x")) {
			t.Errorf("%v no longer triggers failover — retry membership changed", c)
		}
	}
	for _, c := range []codes.Code{codes.PermissionDenied, codes.Canceled, codes.Internal, codes.NotFound} {
		if isTransportFailure(status.Error(c, "x")) {
			t.Errorf("%v became a failover retry — a permission failure would just ask a second server to refuse", c)
		}
	}
	if isTransportFailure(errors.New("plain")) || isTransportFailure(context.DeadlineExceeded) {
		t.Error("a non-gRPC error became a transport failure")
	}

	// CLASSIFICATION: wider than retry, on purpose. A caller always deserves to
	// be told what happened, whether or not another address is worth trying.
	if got := classifyTransportError(status.Error(codes.PermissionDenied, "nope")); got != outcomeServerRefusal {
		t.Errorf("PermissionDenied classified %q; a refusal that is not retried is still a refusal", got)
	}
	// Cancellation is its own member. Reporting it as a deadline would assert
	// the call ran out of budget, which nothing established.
	if got := classifyTransportError(status.Error(codes.Canceled, "gone")); got != outcomeCanceled {
		t.Errorf("Canceled classified %q, want canceled", got)
	}
	if classifyTransportError(status.Error(codes.Canceled, "gone")) == outcomeDeadlineExceeded {
		t.Error("a cancellation was relabelled as a deadline")
	}
	if got := classifyTransportError(status.Error(codes.Internal, "boom")); got != outcomeUnclassified {
		t.Errorf("Internal classified %q, want unclassified", got)
	}
}

// Every classified outcome must reach the caller as typed data, including the
// ones failover does not retry. Gating the classification on the retry
// predicate silently dropped the structured surface for those (#319 review).
func TestARefusalThatIsNotRetriedStillArrivesAsTypedData(t *testing.T) {
	err := toolRPCError("preflight", status.Error(codes.PermissionDenied, "propose is disabled"))
	var te *transportError
	if !errors.As(err, &te) {
		t.Fatalf("a permission failure produced an untyped error, so no structured data reaches the caller: %v", err)
	}
	if te.Aggregate != outcomeServerRefusal {
		t.Errorf("outcome=%q, want server_refusal", te.Aggregate)
	}
	if te.Aggregate.assertsUnreachable() {
		t.Error("a refusal claimed the backend is unreachable")
	}
	raw, perr := responsePayload(1, nil, err)
	if perr != nil {
		t.Fatalf("responsePayload: %v", perr)
	}
	var out struct {
		Error *struct {
			Data map[string]interface{} `json:"data"`
		} `json:"error"`
	}
	if jerr := json.Unmarshal(raw, &out); jerr != nil {
		t.Fatalf("unmarshal: %v", jerr)
	}
	if out.Error == nil || out.Error.Data == nil || out.Error.Data["outcome"] != string(outcomeServerRefusal) {
		t.Fatalf("the refusal did not survive to the structured surface: %s", raw)
	}
}

// "We could not classify this" is an ANSWER, and the caller is entitled to it
// in the same shape as every other answer.
//
// Two gates were tried in toolRPCError and each dropped a member of the closed
// set: isTransportFailure made server_refusal unreachable for a permission
// failure, then classifiable() made `unclassified` itself unreachable, so an
// Internal or NotFound produced a bare error with no structured data (#319
// review).
func TestEveryMemberOfTheClosedSetReachesTheCallerAsTypedData(t *testing.T) {
	for _, c := range []struct {
		code codes.Code
		want transportOutcome
	}{
		{codes.Unavailable, outcomeUnreachable},
		{codes.DeadlineExceeded, outcomeDeadlineExceeded},
		{codes.Unauthenticated, outcomeServerRefusal},
		{codes.PermissionDenied, outcomeServerRefusal},
		{codes.Canceled, outcomeCanceled},
		{codes.Internal, outcomeUnclassified},
		{codes.NotFound, outcomeUnclassified},
	} {
		msg := "connection refused"
		if c.code != codes.Unavailable {
			msg = "x"
		}
		err := toolRPCError("preflight", status.Error(c.code, msg))
		var te *transportError
		if !errors.As(err, &te) {
			t.Errorf("%v produced an untyped error — no structured data reaches the caller", c.code)
			continue
		}
		if te.Aggregate != c.want {
			t.Errorf("%v classified %q, want %q", c.code, te.Aggregate, c.want)
		}
		raw, perr := responsePayload(1, nil, err)
		if perr != nil {
			t.Fatalf("responsePayload: %v", perr)
		}
		var out struct {
			Error *struct {
				Data map[string]interface{} `json:"data"`
			} `json:"error"`
		}
		if jerr := json.Unmarshal(raw, &out); jerr != nil {
			t.Fatalf("unmarshal: %v", jerr)
		}
		if out.Error == nil || out.Error.Data == nil || out.Error.Data["outcome"] != string(c.want) {
			t.Errorf("%v did not survive to the structured surface: %s", c.code, raw)
		}
	}
}

// A mixed result must not be reported as a unanimous one.
//
// callWithFailover returned a non-retryable error the moment it saw one,
// BEFORE recording it — so Unavailable at the first address followed by
// PermissionDenied at the second discarded the first entirely and surfaced a
// blank-address server_refusal. Two addresses that failed differently support
// no single sentence, and the aggregate is `unclassified` (#319 review).
func TestATerminalFailureKeepsTheAttemptsBeforeIt(t *testing.T) {
	i := 0
	entries := []clientEntry{{addr: "a:1"}, {addr: "b:2"}}
	_, err := callWithFailover(entries, func(awarenessClient) (int, error) {
		i++
		if i == 1 {
			return 0, status.Error(codes.Unavailable, "connection refused")
		}
		return 0, status.Error(codes.PermissionDenied, "nope")
	})
	var te *transportError
	if !errors.As(err, &te) {
		t.Fatalf("the earlier attempt was discarded and the result is untyped: %v", err)
	}
	if len(te.Addresses) != 2 {
		t.Fatalf("recorded %d attempts, want 2 — an attempt was dropped: %+v", len(te.Addresses), te.Addresses)
	}
	if te.Aggregate != outcomeUnclassified {
		t.Errorf("a mixed result aggregated to %q — the last address spoke for the whole set", te.Aggregate)
	}
	if te.Aggregate.assertsUnreachable() {
		t.Error("a mixed result claimed the backend is unreachable")
	}
	// Both addresses must be named, not just the one that ended the loop.
	seen := map[string]bool{}
	for _, a := range te.Addresses {
		seen[a.Address] = true
	}
	if !seen["a:1"] || !seen["b:2"] {
		t.Errorf("an attempted address is missing from the record: %+v", te.Addresses)
	}
}

// A single non-retryable failure returns the RAW error, exactly as before, so
// callers that inspect its status code keep working.
func TestASoleTerminalFailureIsUnchanged(t *testing.T) {
	_, err := callWithFailover([]clientEntry{{addr: "a:1"}}, func(awarenessClient) (int, error) {
		return 0, status.Error(codes.NotFound, "nope")
	})
	var te *transportError
	if errors.As(err, &te) {
		t.Fatal("a sole non-retryable failure was wrapped, changing what callers can inspect")
	}
	if st, ok := status.FromError(err); !ok || st.Code() != codes.NotFound {
		t.Fatalf("the raw status did not survive: %v", err)
	}
}

// The MCP payload must carry reachability, because agents never see the CLI's
// human authority block.
//
// The CLI computed it while RENDERING, and the MCP tools build their own
// payloads — so every agent using awareness_briefing/preflight/query/impact/
// resolve saw "authoritative" with no indication the corpus had moved past the
// serving graph. The false-green path, left open for the callers most likely to
// act on it unsupervised.
func TestTheMCPAuthorityPayloadCarriesReachability(t *testing.T) {
	obj := authorityStruct(&awarenesspb.GraphAuthority{GraphBuildCommit: "96f19456f5fb"})
	r, ok := obj["reachability"].(map[string]interface{})
	if !ok {
		t.Fatalf("an agent sees no reachability at all: %+v", obj)
	}
	for _, k := range []string{"state", "reachable", "detail", "asserts_absence"} {
		if _, present := r[k]; !present {
			t.Errorf("the agent-facing assessment omits %q", k)
		}
	}
	if r["asserts_absence"] != false {
		t.Error("a reachability state claimed absence of law to an agent")
	}
	// The authority fields themselves must be untouched: this is additive.
	if obj["graph_build_commit"] != "96f19456f5fb" {
		t.Error("adding reachability displaced the authority payload")
	}
}

// A response whose authority states no build commit carries no assessment.
// Absent, not a fabricated unknown: the question was never askable.
func TestAnMCPAuthorityWithoutABuildCommitCarriesNoAssessment(t *testing.T) {
	obj := authorityStruct(&awarenesspb.GraphAuthority{})
	if _, present := obj["reachability"]; present {
		t.Fatalf("an unaskable question was answered anyway: %+v", obj)
	}
}
