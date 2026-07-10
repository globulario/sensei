// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
	"github.com/globulario/awareness-graph/golang/rdf"
	"github.com/globulario/awareness-graph/golang/store"
)

// provenancedCaddyFacts returns ImpactForFile facts for a promoted Caddy rule
// carrying a full provenance receipt, anchored to the queried file.
func provenancedCaddyFacts() []store.ImpactFact {
	iri := mintedIRI(rdf.ClassInvariant, caddyRuleID)
	f := func(p, o string) store.ImpactFact {
		return store.ImpactFact{NodeIRI: iri, TypeIRI: rdf.ClassInvariant, Predicate: p, Object: o}
	}
	return []store.ImpactFact{
		f(rdf.PropLabel, "Caddyfile errors must use dispenser.Errf"),
		f(rdf.PropRepo, caddyDomain),
		f(rdf.PropOrigin, "coldsource"),
		f(rdf.PropReviewLabel, "load-bearing"),
		f(rdf.PropProvenanceBundleID, "caddy-reverseproxy-forwardauth-2026-06"),
		f(rdf.PropProvenanceCommitRange, "HEAD~500..HEAD"),
		f(rdf.PropProvenanceCitation, "github.com/caddyserver/caddy#7814 c3390101816"),
	}
}

// A promoted repo-scoped rule's briefing shows a compact provenance block —
// repo, origin, review label, bundle, range, citations — so an agent can see
// why a foreign rule should be trusted.
func TestBriefing_Provenance_RenderedForPromotedRule(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return provenancedCaddyFacts(), nil
		},
	})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: caddyFile, Domain: caddyDomain})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	prose := resp.GetProse()
	for _, want := range []string{"Provenance", caddyRuleID, "origin coldsource", "review load-bearing", "HEAD~500..HEAD"} {
		if !strings.Contains(prose, want) {
			t.Errorf("briefing prose missing %q\n---\n%s", want, prose)
		}
	}
}

// An untagged (home-domain) briefing carries NO provenance block — provenance is
// only for promoted repo-scoped rules.
func TestBriefing_NoProvenanceBlockForUntagged(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return scopeFacts(map[string]string{"globular.repository.publish_is_scylla_first": ""}), nil
		},
	})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "golang/repository/repository_server/publish.go"})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if strings.Contains(resp.GetProse(), "Provenance (promoted") {
		t.Errorf("untagged briefing must not render a provenance block\n---\n%s", resp.GetProse())
	}
}

// Resolve honours domain scope: a foreign repo node is invisible (found=false)
// when resolved under another domain, but visible in its own domain.
func TestResolve_DomainScope_ForeignNodeInvisible(t *testing.T) {
	caddyTriples := []store.Triple{
		{Predicate: rdf.PropLabel, Object: "Caddyfile errors must use dispenser.Errf"},
		{Predicate: rdf.PropRepo, Object: caddyDomain},
	}
	s := newServer(fakeStore{
		describe: func(_ context.Context, _ string) ([]store.Triple, error) { return caddyTriples, nil },
	})

	// Under the Globular domain → invisible.
	resp, err := s.Resolve(context.Background(), &awarenesspb.ResolveRequest{Class: "invariant", Id: caddyRuleID, Domain: "globular"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resp.GetFound() {
		t.Errorf("Caddy node must resolve to not-found under the globular domain")
	}

	// Under its own domain → visible.
	resp2, err := s.Resolve(context.Background(), &awarenesspb.ResolveRequest{Class: "invariant", Id: caddyRuleID, Domain: caddyDomain})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !resp2.GetFound() {
		t.Errorf("Caddy node must be found in its own domain")
	}
}
