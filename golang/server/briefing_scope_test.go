// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"testing"

	"github.com/globulario/awareness-graph/golang/rdf"
	"github.com/globulario/awareness-graph/golang/store"
)

// briefing #5 regression: implementation-pattern and intent-trigger sections must
// honor the briefing's domain scope, exactly like collectImpact does — a
// domain-scoped briefing must not leak another repo's patterns/intents, while
// shared nodes always pass and home nodes are never dropped for an unanchored
// file.

func TestBriefingScope_Precedence(t *testing.T) {
	cases := []struct {
		requested, resolved, home, want string
	}{
		{"github.com/caddyserver/caddy", "globular", "globular", "github.com/caddyserver/caddy"}, // explicit wins
		{"", "globular", "globular", "globular"},                                                 // file's resolved domain
		{"", "", "globular", "globular"},                                                         // unanchored file → home (not "")
	}
	for _, c := range cases {
		if got := briefingScope(c.requested, c.resolved, c.home); got != c.want {
			t.Errorf("briefingScope(%q,%q,%q)=%q, want %q", c.requested, c.resolved, c.home, got, c.want)
		}
	}
}

func TestInScopePatterns_NoForeignLeak(t *testing.T) {
	loaded := []loadedPattern{
		{ID: "home.pattern", Domain: "globular"},
		{ID: "caddy.pattern", Domain: "github.com/caddyserver/caddy"},
		{ID: "shared.pattern", Domain: rdf.DomainShared},
	}
	// Home-scoped briefing: home + shared, NEVER the caddy pattern.
	got := idsOfPatterns(inScopePatterns(loaded, "globular"))
	assertSet(t, "home scope", got, []string{"home.pattern", "shared.pattern"})

	// Caddy-scoped briefing: caddy + shared, never home.
	got = idsOfPatterns(inScopePatterns(loaded, "github.com/caddyserver/caddy"))
	assertSet(t, "caddy scope", got, []string{"caddy.pattern", "shared.pattern"})
}

func TestInScopeIntents_NoForeignLeak(t *testing.T) {
	loaded := []loadedIntent{
		{ID: "home.intent", Domain: "globular"},
		{ID: "caddy.intent", Domain: "github.com/caddyserver/caddy"},
		{ID: "shared.intent", Domain: rdf.DomainShared},
	}
	got := idsOfIntents(inScopeIntents(loaded, "globular"))
	assertSet(t, "home scope", got, []string{"home.intent", "shared.intent"})
}

// Loaders must default an untagged node to the home domain and let aw:repo /
// aw:domain override — so untagged patterns stay visible in a home briefing.
func TestClassFactsToPatterns_DomainResolution(t *testing.T) {
	facts := []store.ImpactFact{
		{NodeIRI: "n:untagged", Predicate: rdf.PropStatus, Object: "active"},
		{NodeIRI: "n:repo", Predicate: rdf.PropStatus, Object: "active"},
		{NodeIRI: "n:repo", Predicate: rdf.PropRepo, Object: "github.com/caddyserver/caddy"},
		{NodeIRI: "n:shared", Predicate: rdf.PropStatus, Object: "active"},
		{NodeIRI: "n:shared", Predicate: rdf.PropDomain, Object: rdf.DomainShared},
	}
	byID := map[string]string{}
	for _, p := range classFactsToPatterns(facts, "globular") {
		byID[p.IRI] = p.Domain
	}
	if byID["n:untagged"] != "globular" {
		t.Errorf("untagged → home domain, got %q", byID["n:untagged"])
	}
	if byID["n:repo"] != "github.com/caddyserver/caddy" {
		t.Errorf("aw:repo → repo domain, got %q", byID["n:repo"])
	}
	if byID["n:shared"] != rdf.DomainShared {
		t.Errorf("aw:domain=shared → shared, got %q", byID["n:shared"])
	}
}

func idsOfPatterns(ps []loadedPattern) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return out
}

func idsOfIntents(is []loadedIntent) []string {
	out := make([]string, 0, len(is))
	for _, i := range is {
		out = append(out, i.ID)
	}
	return out
}

func assertSet(t *testing.T, label string, got, want []string) {
	t.Helper()
	g := map[string]bool{}
	for _, s := range got {
		g[s] = true
	}
	if len(got) != len(want) {
		t.Errorf("%s: got %v, want %v", label, got, want)
		return
	}
	for _, w := range want {
		if !g[w] {
			t.Errorf("%s: missing %q (got %v)", label, w, got)
		}
	}
}
