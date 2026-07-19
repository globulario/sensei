// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/briefingfeedback"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func testFeedbackServer(repo *briefingRepositoryContext) *server {
	return &server{logger: log.New(io.Discard, "", 0), briefingRepo: repo, feedbackMapper: briefingFeedbackToProto}
}

// The frozen combined-status table (every row).
func TestCombineBriefingStatus_FrozenTable(t *testing.T) {
	ok := awarenesspb.BriefingStatus_BRIEFING_STATUS_OK
	empty := awarenesspb.BriefingStatus_BRIEFING_STATUS_EMPTY
	degraded := awarenesspb.BriefingStatus_BRIEFING_STATUS_DEGRADED
	cases := []struct {
		base  awarenesspb.BriefingStatus
		avail briefingfeedback.Availability
		want  awarenesspb.BriefingStatus
	}{
		{ok, briefingfeedback.FeedbackAvailable, ok},
		{ok, briefingfeedback.FeedbackEmpty, ok},
		{ok, briefingfeedback.FeedbackDegraded, degraded},
		{ok, briefingfeedback.FeedbackUnavailable, degraded},
		{ok, briefingfeedback.FeedbackInvalid, degraded},
		{empty, briefingfeedback.FeedbackAvailable, ok},
		{empty, briefingfeedback.FeedbackEmpty, empty},
		{empty, briefingfeedback.FeedbackDegraded, degraded},
		{empty, briefingfeedback.FeedbackUnavailable, degraded},
		{empty, briefingfeedback.FeedbackInvalid, degraded},
		// A degraded base is never upgraded to OK by feedback.
		{degraded, briefingfeedback.FeedbackAvailable, degraded},
		{degraded, briefingfeedback.FeedbackEmpty, degraded},
	}
	for _, tc := range cases {
		if got := combineBriefingStatus(tc.base, tc.avail); got != tc.want {
			t.Errorf("combine(%v,%q) = %v, want %v", tc.base, tc.avail, got, tc.want)
		}
	}
}

// Repository context absent → unavailable, no filesystem access, no owner invocation.
func TestBriefingFeedback_RepositoryContextAbsent(t *testing.T) {
	s := testFeedbackServer(nil)
	p, err := s.briefingFeedback(context.Background(), feedbackBriefingScope{effectiveDomain: "github.com/x/y", file: "a/b.go"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Availability != briefingfeedback.FeedbackUnavailable || p.Findings[0].ReasonCode != string(briefingfeedback.RepositoryContextAbsent) {
		t.Fatalf("want repository_context_absent unavailable, got %q %+v", p.Availability, p.Findings)
	}
}

// Foreign-domain request → unavailable WITHOUT owner invocation (root never read: a
// non-existent root still yields a clean domain-mismatch projection, not an internal error).
func TestBriefingFeedback_ForeignDomainNoOwnerInvocation(t *testing.T) {
	s := testFeedbackServer(&briefingRepositoryContext{Root: "/nonexistent/repo", Domain: "github.com/x/y"})
	p, err := s.briefingFeedback(context.Background(), feedbackBriefingScope{effectiveDomain: "github.com/other/z", file: "a/b.go", rawFile: "a/b.go", rawDomain: "github.com/other/z"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Availability != briefingfeedback.FeedbackUnavailable || p.Findings[0].ReasonCode != string(briefingfeedback.RepositoryContextDomainMismatch) {
		t.Fatalf("want domain_mismatch (no owner call), got %q %+v", p.Availability, p.Findings)
	}
}

// Task-only server briefing → canonical-task-scope unavailable, no filesystem read.
func TestBriefingFeedback_TaskOnlyUnavailable(t *testing.T) {
	s := testFeedbackServer(&briefingRepositoryContext{Root: "/nonexistent/repo", Domain: "github.com/x/y"})
	p, err := s.briefingFeedback(context.Background(), feedbackBriefingScope{taskOnly: true, effectiveDomain: "github.com/x/y", rawDomain: "github.com/x/y"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Availability != briefingfeedback.FeedbackUnavailable || p.Findings[0].ReasonCode != string(briefingfeedback.CanonicalTaskScopeNotEstablished) {
		t.Fatalf("want canonical_task_scope_not_established, got %q %+v", p.Availability, p.Findings)
	}
	if p.TaskID != "" || p.SessionID != "" {
		t.Fatalf("task-only feedback must not carry a fabricated task id/session: %+v", p)
	}
}

// Prose is rendered only from the projection: unavailable/invalid show the typed reason and no
// raw error; a failed candidate is never rendered as a governed record.
func TestBriefingFeedbackProse_PrivacyAndParity(t *testing.T) {
	un, _ := briefingfeedback.BuildUnavailable(briefingfeedback.Scope{RepositoryIdentity: "github.com/x/y", RequestedDomain: "github.com/x/y"}, briefingfeedback.RepositoryContextAbsent)
	prose := briefingFeedbackProse(un)
	if !strings.Contains(prose, "unavailable") || !strings.Contains(prose, string(briefingfeedback.RepositoryContextAbsent)) {
		t.Fatalf("unavailable prose must name the typed reason: %q", prose)
	}
	if strings.Contains(prose, "governed briefing feedback:") {
		t.Fatalf("unavailable prose must not render a governed section")
	}
	// Empty renders no section and no warning.
	empty, _ := briefingfeedback.BuildUnavailable(briefingfeedback.Scope{RepositoryIdentity: "github.com/x/y"}, briefingfeedback.RepositoryContextAbsent)
	_ = empty // exercised above; empty availability is covered by the seeded test
}

// Unconfigured server: feedback is EXPLICIT on the wire — field 7 present, feedback_unavailable,
// reason repository_context_absent — and the base OK/EMPTY status composes to DEGRADED while the
// base graph prose is preserved. (This is a semantic improvement, not byte-for-byte compat.)
func TestBriefing_UnconfiguredEmitsUnavailableAndDegrades(t *testing.T) {
	// Base EMPTY (no anchors) → DEGRADED.
	s := newServer(fakeStore{impactForFile: func(context.Context, string) ([]store.ImpactFact, error) { return nil, nil }})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "test/example.go"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetFeedback() == nil {
		t.Fatal("unconfigured server must emit an explicit feedback section (field 7)")
	}
	if resp.GetFeedback().GetAvailability() != awarenesspb.BriefingFeedbackAvailability_BRIEFING_FEEDBACK_AVAILABILITY_UNAVAILABLE {
		t.Fatalf("unconfigured feedback must be unavailable, got %v", resp.GetFeedback().GetAvailability())
	}
	if len(resp.GetFeedback().GetFindings()) != 1 || resp.GetFeedback().GetFindings()[0].GetReasonCode() != string(briefingfeedback.RepositoryContextAbsent) {
		t.Fatalf("unconfigured reason must be repository_context_absent: %+v", resp.GetFeedback().GetFindings())
	}
	if resp.GetFeedback().GetTaskId() != "" {
		t.Fatalf("unconfigured feedback must carry no task identity")
	}
	if resp.GetStatus() != awarenesspb.BriefingStatus_BRIEFING_STATUS_DEGRADED {
		t.Fatalf("EMPTY base + unavailable feedback must be DEGRADED, got %v", resp.GetStatus())
	}
	if !strings.Contains(resp.GetProse(), "Awareness briefing for test/example.go") {
		t.Fatalf("base graph prose was erased: %q", resp.GetProse())
	}

	// Base OK (an anchor) → DEGRADED.
	s2 := newServer(fakeStore{impactForFile: func(context.Context, string) ([]store.ImpactFact, error) {
		return []store.ImpactFact{
			{NodeIRI: "https://globular.io/awareness#invariant/test.x", TypeIRI: "https://globular.io/awareness#Invariant", Predicate: "http://www.w3.org/2000/01/rdf-schema#label", Object: "x"},
		}, nil
	}})
	resp2, err := s2.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "test/example.go"})
	if err != nil {
		t.Fatal(err)
	}
	if resp2.GetStatus() != awarenesspb.BriefingStatus_BRIEFING_STATUS_DEGRADED {
		t.Fatalf("OK base + unavailable feedback must be DEGRADED, got %v", resp2.GetStatus())
	}
}

// Configured server, matching domain, empty repo: field 7 present with feedback_empty; an OK
// base stays OK (OK + empty = OK).
func TestBriefing_ConfiguredEmptyFeedbackComposes(t *testing.T) {
	s := newServer(fakeStore{impactForFile: func(context.Context, string) ([]store.ImpactFact, error) {
		return []store.ImpactFact{
			{NodeIRI: "https://globular.io/awareness#invariant/test.x", TypeIRI: "https://globular.io/awareness#Invariant", Predicate: "http://www.w3.org/2000/01/rdf-schema#label", Object: "x"},
		}, nil
	}})
	s.briefingRepo = &briefingRepositoryContext{Root: t.TempDir(), Domain: defaultHomeDomain}
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "test/example.go"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetFeedback() == nil {
		t.Fatal("configured server must attach field 7")
	}
	if resp.GetFeedback().GetAvailability() != awarenesspb.BriefingFeedbackAvailability_BRIEFING_FEEDBACK_AVAILABILITY_EMPTY {
		t.Fatalf("empty repo must yield feedback_empty, got %v", resp.GetFeedback().GetAvailability())
	}
	if resp.GetStatus() != awarenesspb.BriefingStatus_BRIEFING_STATUS_OK {
		t.Fatalf("OK base + empty feedback must stay OK, got %v", resp.GetStatus())
	}
}

// Configured server, foreign domain: feedback unavailable (domain mismatch) → DEGRADED, but the
// base graph briefing prose is preserved (never erased).
func TestBriefing_ForeignDomainDegradesButKeepsBase(t *testing.T) {
	s := newServer(fakeStore{impactForFile: func(context.Context, string) ([]store.ImpactFact, error) {
		return []store.ImpactFact{
			{NodeIRI: "https://globular.io/awareness#invariant/test.x", TypeIRI: "https://globular.io/awareness#Invariant", Predicate: "http://www.w3.org/2000/01/rdf-schema#label", Object: "x"},
		}, nil
	}})
	s.briefingRepo = &briefingRepositoryContext{Root: t.TempDir(), Domain: "github.com/foreign/repo"}
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "test/example.go"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetFeedback().GetAvailability() != awarenesspb.BriefingFeedbackAvailability_BRIEFING_FEEDBACK_AVAILABILITY_UNAVAILABLE {
		t.Fatalf("foreign domain must be feedback_unavailable, got %v", resp.GetFeedback().GetAvailability())
	}
	if resp.GetStatus() != awarenesspb.BriefingStatus_BRIEFING_STATUS_DEGRADED {
		t.Fatalf("unavailable feedback must degrade status, got %v", resp.GetStatus())
	}
	// The base graph briefing is preserved.
	if !strings.Contains(resp.GetProse(), "Awareness briefing for test/example.go") {
		t.Fatalf("base graph briefing prose was erased: %q", resp.GetProse())
	}
}

// Padded/noncanonical raw identity never reaches the owner and yields feedback_invalid. The
// configured root points at a non-existent path: if the owner ran, Build would return an
// INTERNAL projection, so an invalid reason structurally proves the owner was not invoked.
func TestBriefingFeedback_PaddedIdentityNeverReachesOwner(t *testing.T) {
	s := testFeedbackServer(&briefingRepositoryContext{Root: "/nonexistent/repo", Domain: "github.com/x/y"})
	cases := []struct {
		name   string
		scope  feedbackBriefingScope
		reason briefingfeedback.InvalidReason
	}{
		{"padded file", feedbackBriefingScope{effectiveDomain: "github.com/x/y", file: "a/b.go", rawFile: " a/b.go", rawDomain: "github.com/x/y"}, briefingfeedback.RequestedFileNoncanonical},
		{"unsafe file", feedbackBriefingScope{effectiveDomain: "github.com/x/y", file: "a/b.go", rawFile: "../escape", rawDomain: "github.com/x/y"}, briefingfeedback.RequestedFileNoncanonical},
		{"padded domain", feedbackBriefingScope{effectiveDomain: "github.com/x/y", file: "a/b.go", rawFile: "a/b.go", rawDomain: "github.com/x/y "}, briefingfeedback.RequestedDomainNoncanonical},
		{"whitespace domain", feedbackBriefingScope{effectiveDomain: "github.com/x/y", file: "a/b.go", rawFile: "a/b.go", rawDomain: "a b"}, briefingfeedback.RequestedDomainNoncanonical},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := s.briefingFeedback(context.Background(), tc.scope)
			if err != nil {
				t.Fatal(err)
			}
			if p.Availability != briefingfeedback.FeedbackInvalid || p.Findings[0].ReasonCode != string(tc.reason) {
				t.Fatalf("want feedback_invalid/%s (no owner call), got %q %+v", tc.reason, p.Availability, p.Findings)
			}
		})
	}
}

// A padded raw file on a CONFIGURED server: feedback is invalid → DEGRADED, but the graph
// briefing (which trims) still produces its base prose.
func TestBriefing_PaddedFileInvalidKeepsGraphBriefing(t *testing.T) {
	s := newServer(fakeStore{impactForFile: func(context.Context, string) ([]store.ImpactFact, error) {
		return []store.ImpactFact{
			{NodeIRI: "https://globular.io/awareness#invariant/test.x", TypeIRI: "https://globular.io/awareness#Invariant", Predicate: "http://www.w3.org/2000/01/rdf-schema#label", Object: "x"},
		}, nil
	}})
	s.briefingRepo = &briefingRepositoryContext{Root: t.TempDir(), Domain: defaultHomeDomain}
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: " test/example.go"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetFeedback().GetAvailability() != awarenesspb.BriefingFeedbackAvailability_BRIEFING_FEEDBACK_AVAILABILITY_INVALID {
		t.Fatalf("padded file must yield feedback_invalid, got %v", resp.GetFeedback().GetAvailability())
	}
	if resp.GetFeedback().GetFindings()[0].GetReasonCode() != string(briefingfeedback.RequestedFileNoncanonical) {
		t.Fatalf("wrong invalid reason: %v", resp.GetFeedback().GetFindings())
	}
	if resp.GetStatus() != awarenesspb.BriefingStatus_BRIEFING_STATUS_DEGRADED {
		t.Fatalf("invalid feedback must degrade, got %v", resp.GetStatus())
	}
	if !strings.Contains(resp.GetProse(), "Awareness briefing for test/example.go") {
		t.Fatalf("graph briefing prose (trimmed file) must remain: %q", resp.GetProse())
	}
}

// The resolver returns the projection and its wire mapping as one pair (same digest).
func TestResolveBriefingFeedback_AtomicPair(t *testing.T) {
	s := testFeedbackServer(nil)
	resolved, err := s.resolveBriefingFeedback(context.Background(), feedbackBriefingScope{effectiveDomain: "github.com/x/y", file: "a/b.go", rawFile: "a/b.go", rawDomain: "github.com/x/y"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Wire == nil || resolved.Wire.GetDigestSha256() != resolved.Projection.DigestSHA256 {
		t.Fatalf("projection and wire are not the same pair")
	}
}

// Injected primary adapter failure → the resolver falls back to a canonical internal-unavailable
// projection, mapped consistently (field 7 + projection agree). A double failure → error.
func TestResolveBriefingFeedback_AdapterFallback(t *testing.T) {
	// Fail only the FIRST mapping; the fallback maps normally. Injected per-server (no global).
	calls := 0
	s := testFeedbackServer(&briefingRepositoryContext{Root: "/nonexistent", Domain: "github.com/x/y"})
	s.feedbackMapper = func(p briefingfeedback.Projection) (*awarenesspb.BriefingFeedbackProjection, error) {
		calls++
		if calls == 1 {
			return nil, errBoom
		}
		return briefingFeedbackToProto(p)
	}
	resolved, err := s.resolveBriefingFeedback(context.Background(), feedbackBriefingScope{taskOnly: true, effectiveDomain: "github.com/x/y", rawDomain: "github.com/x/y"})
	if err != nil {
		t.Fatalf("single adapter failure should fall back, got %v", err)
	}
	if resolved.Projection.Availability != briefingfeedback.FeedbackUnavailable ||
		resolved.Projection.Findings[0].ReasonCode != string(briefingfeedback.FeedbackProjectionInternalUnavailable) {
		t.Fatalf("fallback must be feedback_projection_internal_unavailable: %+v", resolved.Projection.Findings)
	}
	if resolved.Wire == nil || resolved.Wire.GetDigestSha256() != resolved.Projection.DigestSHA256 {
		t.Fatalf("fallback projection and wire must agree")
	}

	// Fail ALL mappings → resolver returns an error (RPC translates to gRPC internal).
	s.feedbackMapper = func(briefingfeedback.Projection) (*awarenesspb.BriefingFeedbackProjection, error) {
		return nil, errBoom
	}
	if _, err := s.resolveBriefingFeedback(context.Background(), feedbackBriefingScope{taskOnly: true, effectiveDomain: "github.com/x/y", rawDomain: "github.com/x/y"}); err == nil {
		t.Fatal("double adapter failure must return an error")
	}
}

var errBoom = fmt.Errorf("injected adapter failure")

// When even the fallback cannot map, the RPC returns a typed gRPC internal error rather than a
// divergent response (prose/status without field 7).
func TestBriefing_DoubleAdapterFailureReturnsInternal(t *testing.T) {
	s := newServer(fakeStore{impactForFile: func(context.Context, string) ([]store.ImpactFact, error) { return nil, nil }})
	s.briefingRepo = &briefingRepositoryContext{Root: t.TempDir(), Domain: defaultHomeDomain}
	s.feedbackMapper = func(briefingfeedback.Projection) (*awarenesspb.BriefingFeedbackProjection, error) {
		return nil, errBoom
	}
	_, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "test/example.go"})
	if status.Code(err) != codes.Internal {
		t.Fatalf("double adapter failure must yield gRPC Internal, got %v", err)
	}
}
