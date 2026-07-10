// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
	"github.com/globulario/awareness-graph/golang/rdf"
	"github.com/globulario/awareness-graph/golang/store"
)

// componentFacts builds the ImpactForFile rows for a foreign repo's structural
// Component anchored to a file: the typed node, a label, a dependsOn edge to
// another component, and (when repo != "") the aw:domain/aw:repo scope tags the
// foreign-repo bootstrap mints. repo == "" → untagged (home) component.
func componentFacts(id, dependsOnID, repo string) []store.ImpactFact {
	iri := mintedIRI(rdf.ClassComponent, id)
	dep := mintedIRI(rdf.ClassComponent, dependsOnID)
	facts := []store.ImpactFact{
		{NodeIRI: iri, TypeIRI: rdf.ClassComponent, Predicate: rdf.PropLabel, Object: id},
		{NodeIRI: iri, TypeIRI: rdf.ClassComponent, Predicate: rdf.PropDependsOn, Object: dep, ObjectIsIRI: true},
	}
	if repo != "" {
		facts = append(facts,
			store.ImpactFact{NodeIRI: iri, TypeIRI: rdf.ClassComponent, Predicate: rdf.PropDomain, Object: rdf.DomainRepo},
			store.ImpactFact{NodeIRI: iri, TypeIRI: rdf.ClassComponent, Predicate: rdf.PropRepo, Object: repo},
		)
	}
	return facts
}

func archIDs(resp *awarenesspb.ImpactResponse) []string {
	return collectIDs(resp.GetDirectArchitecture())
}

// Impact for an extracted foreign-repo file, scoped to the repo, returns the
// file's component (membership) AND surfaces a dependency in related ids. This
// is the minimum structural Mode-C context the eval requires.
func TestStructuralImpact_ComponentMembershipAndDependency(t *testing.T) {
	facts := componentFacts("component.cli.api", "component.cli.ghinstance", "github.com/cli/cli")
	s := newScopeServer(facts, "globular")
	resp, _, _, err := s.collectImpact(context.Background(), "api/client.go", "github.com/cli/cli")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ids := archIDs(resp); !hasID(ids, "component.cli.api") {
		t.Fatalf("component membership missing for extracted file, got %v", ids)
	}
	// At least one dependency must be visible (related ids carry the edges).
	var deps []string
	for _, n := range resp.GetDirectArchitecture() {
		if n.GetId() == "component.cli.api" {
			deps = n.GetRelatedIds()
		}
	}
	if !hasID(deps, "component:component.cli.ghinstance") {
		t.Fatalf("dependency missing from component related ids, got %v", deps)
	}
}

// A foreign-repo component must NOT surface under a different repo's scope.
func TestStructuralImpact_WrongDomain_NoLeak(t *testing.T) {
	facts := componentFacts("component.cli.api", "component.cli.ghinstance", "github.com/cli/cli")
	s := newScopeServer(facts, "globular")
	resp, _, _, err := s.collectImpact(context.Background(), "api/client.go", "github.com/caddyserver/caddy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ids := archIDs(resp); len(ids) != 0 {
		t.Fatalf("foreign component LEAKED into a different repo's scope: %v", ids)
	}
}

// With BOTH home (untagged) and foreign (repo-scoped) components anchored to a
// file, each scope sees only its own — no leak in either direction.
func TestStructuralImpact_HomeAndForeign_NoCrossLeak(t *testing.T) {
	facts := append(
		componentFacts("component.home.svc", "component.home.store", ""),                         // home
		componentFacts("component.cli.api", "component.cli.ghinstance", "github.com/cli/cli")..., // foreign
	)
	s := newScopeServer(facts, "globular")

	// Home scope → home component only.
	home, _, _, err := s.collectImpact(context.Background(), "f.go", "globular")
	if err != nil {
		t.Fatalf("home scope error: %v", err)
	}
	if ids := archIDs(home); hasID(ids, "component.cli.api") {
		t.Fatalf("foreign component LEAKED into home scope: %v", ids)
	}
	if ids := archIDs(home); !hasID(ids, "component.home.svc") {
		t.Fatalf("home component missing from home scope: %v", ids)
	}

	// Foreign scope → foreign component only.
	foreign, _, _, err := s.collectImpact(context.Background(), "f.go", "github.com/cli/cli")
	if err != nil {
		t.Fatalf("foreign scope error: %v", err)
	}
	if ids := archIDs(foreign); hasID(ids, "component.home.svc") {
		t.Fatalf("home component LEAKED into foreign scope: %v", ids)
	}
	if ids := archIDs(foreign); !hasID(ids, "component.cli.api") {
		t.Fatalf("foreign component missing from foreign scope: %v", ids)
	}
}
