// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

func TestResolveScope(t *testing.T) {
	cases := []struct {
		name      string
		available []string
		requested string
		want      string
		ambiguous bool
	}{
		// Host project's current single-domain graph: no request, one domain →
		// resolves trivially. This is what keeps existing Globular briefings
		// (and the enforce-briefing hook) working unchanged.
		{"single_domain_unscoped_ok", []string{"globular"}, "", "globular", false},
		// Only shared meta-principles present, nothing repo-specific.
		{"only_shared_unscoped_ok", []string{}, "", "", false},
		{"only_shared_with_shared_listed", []string{rdf.DomainShared}, "", "", false},
		// The moment a foreign domain lands, an unscoped query is ambiguous →
		// fail closed (never mix domains).
		{"two_domains_unscoped_fails", []string{"globular", "github.com/caddyserver/caddy"}, "", "", true},
		{"three_domains_unscoped_fails", []string{"globular", "caddy", "etcd"}, "", "", true},
		// Explicit request always wins, even amid multiple domains.
		{"explicit_disambiguates", []string{"globular", "caddy"}, "caddy", "caddy", false},
		// Explicit request for a domain with no repo-specific rules yet: returned
		// verbatim (filter will then yield only shared).
		{"explicit_absent_domain_ok", []string{"globular"}, "newrepo", "newrepo", false},
		// Duplicates collapse; shared is never counted as selectable.
		{"dupes_and_shared_ignored", []string{"caddy", "caddy", rdf.DomainShared}, "", "caddy", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ResolveScope(c.available, c.requested)
			if c.ambiguous {
				var ae *AmbiguousScopeError
				if !errors.As(err, &ae) {
					t.Fatalf("expected AmbiguousScopeError, got err=%v got=%q", err, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("ResolveScope(%v, %q) = %q, want %q", c.available, c.requested, got, c.want)
			}
		})
	}
}

func TestInScope(t *testing.T) {
	cases := []struct {
		name       string
		nodeDomain string
		resolved   string
		want       bool
	}{
		// Shared meta-principles surface in every scope.
		{"shared_visible_in_repo_scope", rdf.DomainShared, "github.com/caddyserver/caddy", true},
		{"shared_visible_when_unscoped", rdf.DomainShared, "", true},
		// A repo node is visible only in its own scope.
		{"repo_visible_in_own_scope", "github.com/caddyserver/caddy", "github.com/caddyserver/caddy", true},
		{"caddy_invisible_in_globular_scope", "github.com/caddyserver/caddy", "globular", false},
		{"globular_invisible_in_caddy_scope", "globular", "github.com/caddyserver/caddy", false},
		// Repo node never leaks into the only-shared (empty) scope.
		{"repo_invisible_when_unscoped", "github.com/caddyserver/caddy", "", false},
		// An untagged/empty-domain node is not shared and matches no real scope.
		{"empty_domain_not_shared", "", "globular", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := InScope(c.nodeDomain, c.resolved); got != c.want {
				t.Errorf("InScope(%q, %q) = %v, want %v", c.nodeDomain, c.resolved, got, c.want)
			}
		})
	}
}

// The core isolation guarantee end-to-end on the pure layer: given a mixed-
// domain node set, an unscoped query fails closed; a caddy-scoped query returns
// caddy + shared and NEVER a globular rule; a globular-scoped query returns
// globular + shared and NEVER a caddy rule.
func TestScopeIsolation_NoCrossDomainLeak(t *testing.T) {
	type node struct{ id, domain string }
	nodes := []node{
		{"globular.rule.a", "globular"},
		{"caddy.rule.rewrite", "github.com/caddyserver/caddy"},
		{"meta.absence_scope", rdf.DomainShared},
	}
	available := []string{"globular", "github.com/caddyserver/caddy"}

	// Unscoped over a 2-domain graph → fail closed.
	if _, err := ResolveScope(available, ""); err == nil {
		t.Fatal("unscoped query over 2 domains must fail closed")
	}

	// Caddy scope: caddy + shared, no globular.
	resolved, err := ResolveScope(available, "github.com/caddyserver/caddy")
	if err != nil {
		t.Fatalf("explicit caddy scope must resolve: %v", err)
	}
	var visible []string
	for _, n := range nodes {
		if InScope(n.domain, resolved) {
			visible = append(visible, n.id)
		}
	}
	assertVisible(t, "caddy scope", visible,
		[]string{"caddy.rule.rewrite", "meta.absence_scope"},
		[]string{"globular.rule.a"})

	// Globular scope: globular + shared, no caddy.
	resolved, _ = ResolveScope(available, "globular")
	visible = nil
	for _, n := range nodes {
		if InScope(n.domain, resolved) {
			visible = append(visible, n.id)
		}
	}
	assertVisible(t, "globular scope", visible,
		[]string{"globular.rule.a", "meta.absence_scope"},
		[]string{"caddy.rule.rewrite"})
}

func assertVisible(t *testing.T, label string, got, mustHave, mustNotHave []string) {
	t.Helper()
	set := map[string]bool{}
	for _, g := range got {
		set[g] = true
	}
	for _, m := range mustHave {
		if !set[m] {
			t.Errorf("%s: expected %q visible, got %v", label, m, got)
		}
	}
	for _, m := range mustNotHave {
		if set[m] {
			t.Errorf("%s: %q LEAKED across domains, got %v", label, m, got)
		}
	}
}
