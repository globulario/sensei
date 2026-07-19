// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store"
)

// grpcClientStandardFacts returns the canonical pattern data as it would
// come back from ClassFacts. The contents mirror the live YAML at
// docs/awareness/implementation_patterns/grpc_client_standard.yaml so
// truncation regressions surface in unit tests rather than in production
// briefings.
func grpcClientStandardFacts() []store.ImpactFact {
	const subj = "<https://globular.io/awareness#implementationPattern/globular.pattern.grpc_client_standard>"
	mk := func(p, o string) store.ImpactFact {
		return store.ImpactFact{NodeIRI: subj, TypeIRI: rdf.ClassImplementationPattern, Predicate: p, Object: o}
	}
	return []store.ImpactFact{
		mk(rdf.PropLabel, "Standard Globular gRPC service client"),
		mk(rdf.PropStatus, "active"),
		mk(rdf.PropComment, "Globular service clients must share the central bootstrap so TLS\nstays consistent."),
		// 6 activation triggers (matches the live YAML).
		mk(rdf.PropActivationTrigger, "creating a new Go client for a Globular gRPC service"),
		mk(rdf.PropActivationTrigger, "adding client package for a new service"),
		mk(rdf.PropActivationTrigger, "refactoring service client connection logic"),
		mk(rdf.PropActivationTrigger, "implementing service client for awareness-graph or similar"),
		mk(rdf.PropActivationTrigger, "new gRPC client wrapper for a Globular service"),
		mk(rdf.PropActivationTrigger, "wrapping a protobuf client with Globular helpers"),
		// 3 must_follow steps — enough to prove the structured field
		// carries multiple entries unaltered (full YAML has 10).
		mk(rdf.PropMustFollow, "Constructor calls globular.InitClient(client, address, id)"),
		mk(rdf.PropMustFollow, "Reconnect() calls globular.GetClientConnection(client)"),
		mk(rdf.PropMustFollow, "Invoke delegates to globular.InvokeClientRequest"),
		// All 4 required_calls.
		mk(rdf.PropRequiresCall, "globular.InitClient"),
		mk(rdf.PropRequiresCall, "globular.GetClientConnection"),
		mk(rdf.PropRequiresCall, "globular.InvokeClientRequest"),
		mk(rdf.PropRequiresCall, "globular.GetClientContext"),
		// All 4 forbidden_calls.
		mk(rdf.PropForbidsCall, "grpc.Dial"),
		mk(rdf.PropForbidsCall, "grpc.NewClient"),
		mk(rdf.PropForbidsCall, "credentials.NewClientTLSFromFile"),
		mk(rdf.PropForbidsCall, "credentials.NewTLS"),
		// Both reference files.
		mk(rdf.PropReferenceFile, "canonical_minimal:golang/echo/echo_client/echo_client.go"),
		mk(rdf.PropReferenceFile, "richer_reference:golang/monitoring/monitoring_client/monitoring_client.go"),
	}
}

// newPatternTestServer wires a fakeStore whose ClassFacts returns the
// grpc_client_standard pattern, and resets the global cache so each test
// gets a clean load.
func newPatternTestServer(t *testing.T) *server {
	t.Helper()
	invalidateImplementationPatternCacheForTest()
	invalidateIntentTriggerCacheForTest()
	s := newServer(fakeStore{
		classFacts: func(ctx context.Context, classIRI string, limit int) ([]store.ImpactFact, error) {
			if classIRI == rdf.ClassImplementationPattern {
				return grpcClientStandardFacts(), nil
			}
			return nil, nil
		},
		impactForFile: func(ctx context.Context, _ string) ([]store.ImpactFact, error) {
			return nil, nil // no file-anchored impact for these tests
		},
	})
	// Configure a matching empty repository so FILE-scoped briefings get feedback_empty and
	// preserve their base status; task-only briefings remain feedback_unavailable (DEGRADED).
	s.briefingRepo = &briefingRepositoryContext{Root: t.TempDir(), Domain: defaultHomeDomain}
	return s
}

// ────────────────────────────────────────────────────────────────────────
// 1. Strong match — explicit activation trigger contained in task text
// ────────────────────────────────────────────────────────────────────────

func TestBriefing_ImplementationPattern_StrongTriggerMatch(t *testing.T) {
	s := newPatternTestServer(t)
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		File: "golang/new_service/new_service_client/new_service_client.go",
		Task: "create a new Go client for a Globular gRPC service",
	})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	pats := resp.GetImplementationPatterns()
	if len(pats) != 1 {
		t.Fatalf("want 1 pattern, got %d", len(pats))
	}
	p := pats[0]
	if p.GetMatchStrength() != "strong" {
		t.Errorf("strength: want strong, got %q (reasons=%v)", p.GetMatchStrength(), p.GetMatchReason())
	}
	if p.GetId() != "implementation_pattern:globular.pattern.grpc_client_standard" {
		t.Errorf("id: got %q", p.GetId())
	}
	if len(p.GetReferenceFiles()) != 2 {
		t.Errorf("reference_files: want 2, got %d", len(p.GetReferenceFiles()))
	}
	// Structured field carries the full fixture set — see
	// TestBriefing_ImplementationPattern_StructuredFieldsNotTruncated for
	// the explicit regression guard.
	if len(p.GetRequiredCalls()) != 4 {
		t.Errorf("required_calls: want 4 (matches fixture), got %d", len(p.GetRequiredCalls()))
	}
	if len(p.GetForbiddenCalls()) != 4 {
		t.Errorf("forbidden_calls: want 4 (matches fixture), got %d", len(p.GetForbiddenCalls()))
	}
}

// ────────────────────────────────────────────────────────────────────────
// 2. Strong match — second authored trigger phrase
// ────────────────────────────────────────────────────────────────────────

func TestBriefing_ImplementationPattern_RefactorPhraseStrongMatch(t *testing.T) {
	s := newPatternTestServer(t)
	resp, _ := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		File: "golang/foo/foo_client/foo_client.go",
		Task: "refactor service client connection logic",
	})
	pats := resp.GetImplementationPatterns()
	if len(pats) != 1 {
		t.Fatalf("want 1 pattern, got %d (resp=%+v)", len(pats), resp)
	}
	if pats[0].GetMatchStrength() != "strong" {
		t.Errorf("strength: want strong, got %q", pats[0].GetMatchStrength())
	}
}

// ────────────────────────────────────────────────────────────────────────
// 3. Unrelated task — pattern does NOT surface
// ────────────────────────────────────────────────────────────────────────

func TestBriefing_ImplementationPattern_UnrelatedTaskNoMatch(t *testing.T) {
	s := newPatternTestServer(t)
	resp, _ := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		File: "golang/scylla_manager/operations.go",
		Task: "rotate scylla manager TLS certificate",
	})
	if pats := resp.GetImplementationPatterns(); len(pats) != 0 {
		t.Errorf("want 0 patterns for unrelated task, got %d: %v", len(pats), pats)
	}
}

// ────────────────────────────────────────────────────────────────────────
// 4. Output includes reference files, required calls, forbidden calls,
//    AND the prose carries the pattern section
// ────────────────────────────────────────────────────────────────────────

func TestBriefing_ImplementationPattern_OutputCompleteness(t *testing.T) {
	s := newPatternTestServer(t)
	resp, _ := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		File: "golang/foo/foo_client/foo_client.go",
		Task: "create a new Go client for a Globular gRPC service",
	})
	if len(resp.GetImplementationPatterns()) == 0 {
		t.Fatalf("expected pattern to match")
	}
	p := resp.GetImplementationPatterns()[0]
	wantRef := []string{
		"canonical_minimal:golang/echo/echo_client/echo_client.go",
		"richer_reference:golang/monitoring/monitoring_client/monitoring_client.go",
	}
	for _, w := range wantRef {
		found := false
		for _, r := range p.GetReferenceFiles() {
			if r == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("reference_files missing %q: got %v", w, p.GetReferenceFiles())
		}
	}
	if p.GetRationaleSummary() == "" {
		t.Errorf("rationale_summary empty")
	}
	// Prose carries the pattern section.
	prose := resp.GetProse()
	for _, want := range []string{
		"Implementation patterns:",
		"globular.pattern.grpc_client_standard [strong]",
		"Required calls:",
		"globular.InitClient",
		"Forbidden calls:",
		"grpc.Dial",
		"golang/echo/echo_client/echo_client.go",
	} {
		if !strings.Contains(prose, want) {
			t.Errorf("prose missing %q. Full prose:\n%s", want, prose)
		}
	}
	// Pattern id also appears in referenced_ids so callers can Resolve it.
	want := "implementation_pattern:globular.pattern.grpc_client_standard"
	found := false
	for _, id := range resp.GetReferencedIds() {
		if id == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("referenced_ids missing %q: got %v", want, resp.GetReferencedIds())
	}
}

// ────────────────────────────────────────────────────────────────────────
// 5. Bounded output — at most 3 patterns are returned no matter what
// ────────────────────────────────────────────────────────────────────────

func TestBriefing_ImplementationPattern_BoundedToThree(t *testing.T) {
	invalidateImplementationPatternCacheForTest()
	// Build a fake store that returns 5 patterns, all matching strongly.
	mk := func(id string) []store.ImpactFact {
		subj := "<https://globular.io/awareness#implementationPattern/" + id + ">"
		return []store.ImpactFact{
			{NodeIRI: subj, Predicate: rdf.PropLabel, Object: "Pattern " + id},
			{NodeIRI: subj, Predicate: rdf.PropStatus, Object: "active"},
			{NodeIRI: subj, Predicate: rdf.PropActivationTrigger, Object: "create a new Go client"},
		}
	}
	var all []store.ImpactFact
	for _, id := range []string{"p.one", "p.two", "p.three", "p.four", "p.five"} {
		all = append(all, mk(id)...)
	}
	s := newServer(fakeStore{
		classFacts: func(ctx context.Context, classIRI string, limit int) ([]store.ImpactFact, error) {
			if classIRI == rdf.ClassImplementationPattern {
				return all, nil
			}
			return nil, nil
		},
		impactForFile: func(ctx context.Context, _ string) ([]store.ImpactFact, error) {
			return nil, nil
		},
	})
	resp, _ := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		File: "golang/anywhere/anywhere.go",
		Task: "create a new Go client for everything",
	})
	if got := len(resp.GetImplementationPatterns()); got != 3 {
		t.Errorf("bounded: want 3 patterns, got %d", got)
	}
}

// ────────────────────────────────────────────────────────────────────────
// 6. Narrow file-neighbour rule — does NOT trigger without a task signal
// ────────────────────────────────────────────────────────────────────────

func TestBriefing_ImplementationPattern_FileShapeAloneInsufficient(t *testing.T) {
	s := newPatternTestServer(t)
	// File path has the _client/_client.go shape but the task is unrelated.
	resp, _ := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		File: "golang/echo/echo_client/echo_client.go",
		Task: "rotate the Scylla cert",
	})
	if pats := resp.GetImplementationPatterns(); len(pats) != 0 {
		t.Errorf("file shape alone (no task signal) must NOT surface pattern; got %d", len(pats))
	}
}

// ────────────────────────────────────────────────────────────────────────
// 7a. Task-only briefing (no file) STILL surfaces implementation patterns.
//     Regression guard: the task-only branch in briefing.go used to return
//     EMPTY immediately, before the pattern matcher was invoked. Phase C
//     v1 had this oversight; fixed when verifying live cluster deploy.
// ────────────────────────────────────────────────────────────────────────

func TestBriefing_ImplementationPattern_TaskOnlyStillMatches(t *testing.T) {
	s := newPatternTestServer(t)
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		// NO file set.
		Task: "create a new Go client for a Globular gRPC service",
	})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	pats := resp.GetImplementationPatterns()
	if len(pats) != 1 {
		t.Fatalf("task-only mode: want 1 pattern, got %d", len(pats))
	}
	if pats[0].GetMatchStrength() != "strong" {
		t.Errorf("task-only mode strength: want strong, got %q", pats[0].GetMatchStrength())
	}
	// Task-only feedback can never be established (task text is not canonical task identity),
	// so the combined status is DEGRADED even though patterns matched.
	if resp.GetStatus() != awarenesspb.BriefingStatus_BRIEFING_STATUS_DEGRADED {
		t.Errorf("status: want DEGRADED (task-only feedback unavailable), got %v", resp.GetStatus())
	}
	if !strings.Contains(resp.GetProse(), "Implementation patterns:") {
		t.Errorf("task-only prose missing Implementation patterns section:\n%s", resp.GetProse())
	}
}

func TestBriefing_ImplementationPattern_FileTaskUnanchoredStillMatchesTaskPattern(t *testing.T) {
	s := newPatternTestServer(t)
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		File: "golang/unanchored/extract.go",
		Task: "create a new Go client for a Globular gRPC service",
	})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if resp.GetStatus() != awarenesspb.BriefingStatus_BRIEFING_STATUS_OK {
		t.Fatalf("status: want OK when task pattern matches for unanchored file, got %v\n%s", resp.GetStatus(), resp.GetProse())
	}
	pats := resp.GetImplementationPatterns()
	if len(pats) != 1 {
		t.Fatalf("file+task unanchored: want 1 task-matched pattern, got %d", len(pats))
	}
	if pats[0].GetId() != "implementation_pattern:globular.pattern.grpc_client_standard" {
		t.Fatalf("pattern id=%q", pats[0].GetId())
	}
	if !strings.Contains(resp.GetProse(), "No direct awareness anchors found for this file") {
		t.Fatalf("prose should be honest about missing file anchors:\n%s", resp.GetProse())
	}
	if !strings.Contains(resp.GetProse(), "Implementation patterns:") {
		t.Fatalf("prose missing task-matched pattern section:\n%s", resp.GetProse())
	}
}

func TestBriefing_ImplementationPattern_FileOnlyUnanchoredMayRemainEmpty(t *testing.T) {
	s := newPatternTestServer(t)
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		File: "golang/unanchored/extract.go",
	})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if resp.GetStatus() != awarenesspb.BriefingStatus_BRIEFING_STATUS_EMPTY {
		t.Fatalf("file-only unanchored: want EMPTY, got %v", resp.GetStatus())
	}
	if len(resp.GetImplementationPatterns()) != 0 {
		t.Fatalf("file-only unanchored must not invent task patterns, got %d", len(resp.GetImplementationPatterns()))
	}
}

func TestBriefing_ImplementationPattern_FileFactsAndTaskPatternsUnion(t *testing.T) {
	invalidateImplementationPatternCacheForTest()
	invalidateIntentTriggerCacheForTest()
	invIRI := mintedIRI(rdf.ClassInvariant, "test.file_anchor")
	s := newServer(fakeStore{
		classFacts: func(ctx context.Context, classIRI string, limit int) ([]store.ImpactFact, error) {
			if classIRI == rdf.ClassImplementationPattern {
				return grpcClientStandardFacts(), nil
			}
			return nil, nil
		},
		impactForFile: func(ctx context.Context, _ string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{
				{NodeIRI: invIRI, TypeIRI: rdf.ClassInvariant, Predicate: rdf.PropLabel, Object: "File anchor invariant"},
				{NodeIRI: invIRI, TypeIRI: rdf.ClassInvariant, Predicate: rdf.PropSeverity, Object: "high"},
			}, nil
		},
	})
	s.briefingRepo = &briefingRepositoryContext{Root: t.TempDir(), Domain: defaultHomeDomain}
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		File: "golang/anchored/example.go",
		Task: "create a new Go client for a Globular gRPC service",
	})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if resp.GetStatus() != awarenesspb.BriefingStatus_BRIEFING_STATUS_OK {
		t.Fatalf("status=%v want OK", resp.GetStatus())
	}
	if len(resp.GetImplementationPatterns()) != 1 {
		t.Fatalf("want task pattern plus file facts, got %d patterns", len(resp.GetImplementationPatterns()))
	}
	for _, want := range []string{
		"Direct invariants:",
		"test.file_anchor",
		"Implementation patterns:",
		"globular.pattern.grpc_client_standard",
	} {
		if !strings.Contains(resp.GetProse(), want) {
			t.Fatalf("prose missing %q:\n%s", want, resp.GetProse())
		}
	}
}

func TestBriefing_ImplementationPattern_TaskOnlyNoMatchReturnsEmpty(t *testing.T) {
	s := newPatternTestServer(t)
	resp, _ := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		Task: "rotate the Scylla TLS certificate",
	})
	if len(resp.GetImplementationPatterns()) != 0 {
		t.Errorf("unrelated task: want 0 patterns, got %d", len(resp.GetImplementationPatterns()))
	}
	// Task-only feedback is unavailable, so an unmatched task-only briefing is DEGRADED.
	if resp.GetStatus() != awarenesspb.BriefingStatus_BRIEFING_STATUS_DEGRADED {
		t.Errorf("status: want DEGRADED for unrelated task-only briefing, got %v", resp.GetStatus())
	}
}

// ────────────────────────────────────────────────────────────────────────
// 7b. Structured field MUST NOT truncate required_calls or forbidden_calls
//     (regression guard for the hardening pass).
// ────────────────────────────────────────────────────────────────────────

func TestBriefing_ImplementationPattern_StructuredFieldsNotTruncated(t *testing.T) {
	s := newPatternTestServer(t)
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		File: "golang/foo/foo_client/foo_client.go",
		Task: "create a new Go client for a Globular gRPC service",
	})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if len(resp.GetImplementationPatterns()) != 1 {
		t.Fatalf("want exactly 1 pattern, got %d", len(resp.GetImplementationPatterns()))
	}
	p := resp.GetImplementationPatterns()[0]

	// All 4 required_calls must be present in the structured field.
	wantRequired := []string{
		"globular.InitClient",
		"globular.GetClientConnection",
		"globular.InvokeClientRequest",
		"globular.GetClientContext",
	}
	if got := p.GetRequiredCalls(); len(got) != len(wantRequired) {
		t.Errorf("required_calls truncated: want %d entries, got %d (%v)",
			len(wantRequired), len(got), got)
	}
	for _, w := range wantRequired {
		found := false
		for _, g := range p.GetRequiredCalls() {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("required_calls missing %q (got %v)", w, p.GetRequiredCalls())
		}
	}

	// All 4 forbidden_calls must be present in the structured field.
	wantForbidden := []string{
		"grpc.Dial",
		"grpc.NewClient",
		"credentials.NewClientTLSFromFile",
		"credentials.NewTLS",
	}
	if got := p.GetForbiddenCalls(); len(got) != len(wantForbidden) {
		t.Errorf("forbidden_calls truncated: want %d entries, got %d (%v)",
			len(wantForbidden), len(got), got)
	}
	for _, w := range wantForbidden {
		found := false
		for _, g := range p.GetForbiddenCalls() {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("forbidden_calls missing %q (got %v)", w, p.GetForbiddenCalls())
		}
	}

	// must_follow MUST also reach the structured field in full (3 from the
	// fixture). Prose intentionally omits this section to keep the briefing
	// compact, but agents reading the structured response see everything.
	if got := p.GetMustFollow(); len(got) != 3 {
		t.Errorf("must_follow truncated: want 3 entries (matching fixture), got %d (%v)",
			len(got), got)
	}
}

// ────────────────────────────────────────────────────────────────────────
// 8. Narrow file-neighbour rule — fires when shape matches + ≥1 keyword
// ────────────────────────────────────────────────────────────────────────

func TestBriefing_ImplementationPattern_FileShapePlusOneKeyword(t *testing.T) {
	s := newPatternTestServer(t)
	// Task has only one trigger-keyword ("client") — not enough on its own.
	// Combined with the file path shape (_client/_client.go) it qualifies
	// as a narrow match.
	resp, _ := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		File: "golang/foo/foo_client/foo_client.go",
		Task: "tweak the foo client metadata accessors",
	})
	pats := resp.GetImplementationPatterns()
	if len(pats) != 1 {
		t.Fatalf("want 1 narrow match, got %d. Reasons may show absence of file-shape match.", len(pats))
	}
	if pats[0].GetMatchStrength() != "narrow" {
		t.Errorf("strength: want narrow, got %q (reasons=%v)", pats[0].GetMatchStrength(), pats[0].GetMatchReason())
	}
}
