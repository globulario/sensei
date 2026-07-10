// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
	"github.com/globulario/awareness-graph/golang/rdf"
	"github.com/globulario/awareness-graph/golang/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// factsFor builds the ImpactFact rows ImpactForFile would return for a set of
// nodes, tagging each with a domain via aw:repo / aw:domain. domainTag == ""
// means an untagged (legacy/home) node; "shared" emits aw:domain=shared; any
// other value emits aw:repo=<value>.
func scopeFacts(nodes map[string]string) []store.ImpactFact {
	var out []store.ImpactFact
	for id, domainTag := range nodes {
		iri := mintedIRI(rdf.ClassInvariant, id)
		// type + a label fact so the node materializes
		out = append(out, store.ImpactFact{NodeIRI: iri, TypeIRI: rdf.ClassInvariant, Predicate: rdf.PropLabel, Object: id})
		switch domainTag {
		case "":
			// untagged → home domain (no domain/repo fact emitted)
		case rdf.DomainShared:
			out = append(out, store.ImpactFact{NodeIRI: iri, TypeIRI: rdf.ClassInvariant, Predicate: rdf.PropDomain, Object: rdf.DomainShared})
		default:
			out = append(out, store.ImpactFact{NodeIRI: iri, TypeIRI: rdf.ClassInvariant, Predicate: rdf.PropRepo, Object: domainTag})
		}
	}
	return out
}

func collectIDs(invs []*awarenesspb.KnowledgeNode) []string {
	out := make([]string, 0, len(invs))
	for _, n := range invs {
		out = append(out, n.GetId())
	}
	return out
}

func hasID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func newScopeServer(facts []store.ImpactFact, home string) *server {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return facts, nil
		},
	})
	if home != "" {
		s.homeDomain = home
	}
	return s
}

// Single-domain graph (today's Globular reality: all nodes untagged): an
// unscoped query returns everything. This is the regression guard that domain
// scoping does NOT break existing single-domain briefings.
func TestCollectImpact_SingleDomainUnscoped_ReturnsAll(t *testing.T) {
	facts := scopeFacts(map[string]string{
		"globular.rule.a": "",
		"globular.rule.b": "",
	})
	s := newScopeServer(facts, "globular")
	resp, _, _, err := s.collectImpact(context.Background(), "f.go", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids := collectIDs(resp.GetDirectInvariants())
	if len(ids) != 2 || !hasID(ids, "globular.rule.a") || !hasID(ids, "globular.rule.b") {
		t.Fatalf("single-domain unscoped should return all, got %v", ids)
	}
}

// Mixed-domain result with no scope → fail closed (FailedPrecondition), never
// a mixed result set.
func TestCollectImpact_MixedDomainUnscoped_FailsClosed(t *testing.T) {
	facts := scopeFacts(map[string]string{
		"globular.rule.a":    "",                             // home
		"caddy.rule.rewrite": "github.com/caddyserver/caddy", // repo
	})
	s := newScopeServer(facts, "globular")
	_, _, _, err := s.collectImpact(context.Background(), "f.go", "")
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("mixed-domain unscoped must fail closed with FailedPrecondition, got %v", err)
	}
}

// Explicit caddy scope over a mixed result → only caddy + shared, NEVER the
// globular (home) rule. This is the core no-cross-domain-leak guarantee.
func TestCollectImpact_CaddyScope_NoGlobularLeak(t *testing.T) {
	facts := scopeFacts(map[string]string{
		"globular.rule.a":    "",                             // home — must NOT appear
		"caddy.rule.rewrite": "github.com/caddyserver/caddy", // must appear
		"meta.absence_scope": rdf.DomainShared,               // shared — must appear
	})
	s := newScopeServer(facts, "globular")
	resp, _, _, err := s.collectImpact(context.Background(), "f.go", "github.com/caddyserver/caddy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids := collectIDs(resp.GetDirectInvariants())
	if hasID(ids, "globular.rule.a") {
		t.Fatalf("globular rule LEAKED into caddy scope: %v", ids)
	}
	if !hasID(ids, "caddy.rule.rewrite") {
		t.Fatalf("caddy rule missing from caddy scope: %v", ids)
	}
	if !hasID(ids, "meta.absence_scope") {
		t.Fatalf("shared meta-principle must be visible in any scope: %v", ids)
	}
}

// Explicit globular scope over a mixed result → only globular + shared, NEVER
// the caddy rule.
func TestCollectImpact_GlobularScope_NoCaddyLeak(t *testing.T) {
	facts := scopeFacts(map[string]string{
		"globular.rule.a":    "",
		"caddy.rule.rewrite": "github.com/caddyserver/caddy",
		"meta.absence_scope": rdf.DomainShared,
	})
	s := newScopeServer(facts, "globular")
	resp, _, _, err := s.collectImpact(context.Background(), "f.go", "globular")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids := collectIDs(resp.GetDirectInvariants())
	if hasID(ids, "caddy.rule.rewrite") {
		t.Fatalf("caddy rule LEAKED into globular scope: %v", ids)
	}
	if !hasID(ids, "globular.rule.a") || !hasID(ids, "meta.absence_scope") {
		t.Fatalf("globular scope must include home + shared, got %v", ids)
	}
}

// A pure-caddy result (single repo domain, no home node) resolves trivially
// even unscoped — shared still visible.
func TestCollectImpact_SingleRepoDomainUnscoped_OK(t *testing.T) {
	facts := scopeFacts(map[string]string{
		"caddy.rule.rewrite": "github.com/caddyserver/caddy",
		"meta.absence_scope": rdf.DomainShared,
	})
	s := newScopeServer(facts, "globular")
	resp, _, _, err := s.collectImpact(context.Background(), "f.go", "")
	if err != nil {
		t.Fatalf("single repo domain unscoped should resolve trivially, got %v", err)
	}
	ids := collectIDs(resp.GetDirectInvariants())
	if !hasID(ids, "caddy.rule.rewrite") || !hasID(ids, "meta.absence_scope") {
		t.Fatalf("expected caddy + shared, got %v", ids)
	}
}
