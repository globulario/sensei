// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"testing"

	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store"
)

// factsFixture: five nodes across three domains + shared, as ClassFacts would
// return them (multiple facts per node, repo/domain among other predicates).
//
//	n:shared — a portable meta-principle (aw:domain "shared")
//	n:a1, n:a2 — repo A (aw:repo)
//	n:b1 — repo B
//	n:home1 — untagged → resolves to the home domain
func factsFixture() []store.ImpactFact {
	return []store.ImpactFact{
		{NodeIRI: "n:shared", Predicate: "label", Object: "shared rule"},
		{NodeIRI: "n:shared", Predicate: rdf.PropDomain, Object: rdf.DomainShared},
		{NodeIRI: "n:a1", Predicate: rdf.PropRepo, Object: "github.com/o/a"},
		{NodeIRI: "n:a1", Predicate: "label", Object: "A one"},
		{NodeIRI: "n:a2", Predicate: rdf.PropRepo, Object: "github.com/o/a"},
		{NodeIRI: "n:b1", Predicate: rdf.PropRepo, Object: "github.com/o/b"},
		{NodeIRI: "n:home1", Predicate: "label", Object: "untagged"},
	}
}

const homeDom = "github.com/o/home"

func TestNodeDomainsFromFacts(t *testing.T) {
	dom := nodeDomainsFromFacts(factsFixture(), homeDom)
	want := map[string]string{
		"n:shared": rdf.DomainShared,
		"n:a1":     "github.com/o/a",
		"n:a2":     "github.com/o/a",
		"n:b1":     "github.com/o/b",
		"n:home1":  homeDom, // untagged → home
	}
	if len(dom) != len(want) {
		t.Fatalf("got %d nodes, want %d: %v", len(dom), len(want), dom)
	}
	for k, v := range want {
		if dom[k] != v {
			t.Errorf("domain[%s] = %q, want %q", k, dom[k], v)
		}
	}
}

func TestCountClassInScope(t *testing.T) {
	f := factsFixture()
	cases := []struct {
		scope string
		want  int64
	}{
		{"github.com/o/a", 3},      // a1 + a2 + shared
		{"github.com/o/b", 2},      // b1 + shared
		{homeDom, 2},               // home1 + shared
		{"github.com/o/absent", 1}, // a known repo with no local rules → shared only
	}
	for _, c := range cases {
		if got := countClassInScope(f, homeDom, c.scope); got != c.want {
			t.Errorf("countClassInScope(scope=%s) = %d, want %d", c.scope, got, c.want)
		}
	}
}

// The leak test: filtering to one domain must never surface another domain's
// nodes — the invariant the whole design exists to protect.
func TestKeepIRIsInScope_NoCrossDomainLeak(t *testing.T) {
	keep := keepIRIsInScope(factsFixture(), homeDom, "github.com/o/a")
	for _, want := range []string{"n:a1", "n:a2", "n:shared"} {
		if !keep[want] {
			t.Errorf("in-scope node %s missing from repo-A scope", want)
		}
	}
	if keep["n:b1"] {
		t.Error("LEAK: repo-B node visible in a repo-A scope")
	}
	if keep["n:home1"] {
		t.Error("LEAK: home-domain node visible in a repo-A scope")
	}
}

func TestEmbeddedClassScoped(t *testing.T) {
	// Two invariants tagged to repo A, one to repo B, one untagged (→ home),
	// one shared. Class = Invariant.
	inv := rdf.ClassInvariant
	g := &seedGraph{
		bySubject: map[string][]seedTriple{
			"i:a1":   {{pred: rdf.PropType, obj: inv, isIRI: true}, {pred: rdf.PropRepo, obj: "repo/a"}},
			"i:a2":   {{pred: rdf.PropType, obj: inv, isIRI: true}, {pred: rdf.PropRepo, obj: "repo/a"}},
			"i:b1":   {{pred: rdf.PropType, obj: inv, isIRI: true}, {pred: rdf.PropRepo, obj: "repo/b"}},
			"i:home": {{pred: rdf.PropType, obj: inv, isIRI: true}},
			"i:sh":   {{pred: rdf.PropType, obj: inv, isIRI: true}, {pred: rdf.PropDomain, obj: rdf.DomainShared}},
		},
		byClass: map[string][]string{inv: {"i:a1", "i:a2", "i:b1", "i:home", "i:sh"}},
	}
	st := embeddedSeedStore{g: g}

	// ClassNodeDomains: the SET of domains per node.
	nd, err := st.ClassNodeDomains(t.Context(), inv)
	if err != nil {
		t.Fatal(err)
	}
	first := func(v []string) string {
		if len(v) == 0 {
			return ""
		}
		return v[0]
	}
	if first(nd["i:a1"]) != "repo/a" || first(nd["i:b1"]) != "repo/b" || len(nd["i:home"]) != 0 || first(nd["i:sh"]) != "shared" {
		t.Errorf("ClassNodeDomains = %v", nd)
	}

	// ClassFactsScoped to repo/a → a1 + a2 + shared (not b1, not home).
	facts, err := st.ClassFactsScoped(t.Context(), inv, "repo/a", "home", 0)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range facts {
		got[f.NodeIRI] = true
	}
	for _, want := range []string{"i:a1", "i:a2", "i:sh"} {
		if !got[want] {
			t.Errorf("repo/a scope missing %s", want)
		}
	}
	if got["i:b1"] {
		t.Error("LEAK: repo/b node in repo/a scope")
	}
	if got["i:home"] {
		t.Error("LEAK: home node in repo/a scope")
	}
}

// A node authored in TWO repos (two aw:repo tags) must be visible when scoped to
// EITHER repo — the forbidden-fix resolve/count bug.
func TestMultiRepoNodeVisibleInBothScopes(t *testing.T) {
	// anyDomainInScope is the count/list predicate.
	dual := []string{"repo/a", "repo/b"}
	if !anyDomainInScope(dual, "home", "repo/a") {
		t.Error("dual-repo node not in scope repo/a")
	}
	if !anyDomainInScope(dual, "home", "repo/b") {
		t.Error("dual-repo node not in scope repo/b")
	}
	if anyDomainInScope(dual, "home", "repo/c") {
		t.Error("LEAK: dual-repo node in unrelated scope repo/c")
	}
	if !anyDomainInScope(nil, "home", "home") {
		t.Error("untagged node not in home scope")
	}

	// nodeInScopeFromTriples is the resolve predicate — same node, as triples.
	triples := []store.Triple{
		{Predicate: rdf.PropRepo, Object: "repo/a"},
		{Predicate: rdf.PropRepo, Object: "repo/b"},
		{Predicate: rdf.PropDomain, Object: rdf.DomainRepo},
	}
	if !nodeInScopeFromTriples(triples, "home", "repo/a") {
		t.Error("resolve: dual-repo node not in scope repo/a")
	}
	if !nodeInScopeFromTriples(triples, "home", "repo/b") {
		t.Error("resolve: dual-repo node not in scope repo/b")
	}
	if nodeInScopeFromTriples(triples, "home", "repo/c") {
		t.Error("resolve LEAK: dual-repo node in unrelated scope repo/c")
	}
}

func TestEmbeddedSeedStoreDomains(t *testing.T) {
	g := &seedGraph{
		bySubject: map[string][]seedTriple{
			"n:a1":     {{pred: rdf.PropRepo, obj: "github.com/o/a"}},
			"n:a2":     {{pred: rdf.PropRepo, obj: "github.com/o/a"}}, // duplicate domain
			"n:b1":     {{pred: rdf.PropRepo, obj: "github.com/o/b"}},
			"n:shared": {{pred: rdf.PropDomain, obj: rdf.DomainShared}},
		},
	}
	got, err := embeddedSeedStore{g: g}.Domains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// distinct aw:repo values only (shared excluded — it carries no aw:repo)
	if len(got) != 2 {
		t.Fatalf("Domains() = %v, want 2 distinct repos", got)
	}
	seen := map[string]bool{got[0]: true, got[1]: true}
	if !seen["github.com/o/a"] || !seen["github.com/o/b"] {
		t.Errorf("Domains() = %v, want github.com/o/a + github.com/o/b", got)
	}
}
