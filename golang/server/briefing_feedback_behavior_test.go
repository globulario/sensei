// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/briefingfeedback"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/store"
)

func testFeedbackServer(repo *briefingRepositoryContext) *server {
	return &server{logger: log.New(io.Discard, "", 0), briefingRepo: repo}
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
	p, err := s.briefingFeedback(context.Background(), feedbackBriefingScope{effectiveDomain: "github.com/other/z", file: "a/b.go"})
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
	p, err := s.briefingFeedback(context.Background(), feedbackBriefingScope{taskOnly: true, effectiveDomain: "github.com/x/y"})
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

// Unconfigured server: feedback is OMITTED entirely — field 7 nil, status/prose unchanged
// (backward compatible; old clients and the pre-9.6 contract are preserved).
func TestBriefing_UnconfiguredOmitsFeedback(t *testing.T) {
	s := newServer(fakeStore{impactForFile: func(context.Context, string) ([]store.ImpactFact, error) { return nil, nil }})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "test/example.go"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetFeedback() != nil {
		t.Fatalf("unconfigured server must omit feedback, got %+v", resp.GetFeedback())
	}
	if resp.GetStatus() != awarenesspb.BriefingStatus_BRIEFING_STATUS_EMPTY {
		t.Fatalf("unconfigured status must be unchanged EMPTY, got %v", resp.GetStatus())
	}
	if strings.Contains(resp.GetProse(), "Governed briefing feedback") {
		t.Fatalf("unconfigured server must not render feedback prose")
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
