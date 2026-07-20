// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import (
	"reflect"
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

func catScope() CatalogScope {
	return CatalogScope{
		RepositoryIdentity: tRepo, DomainIdentity: tRepo, GraphAuthorityIdentity: tAuth,
		SnapshotIdentity: "snap-1",
		Source:           srcStatus("controlstate.catalog", "catalog_enumeration", "snap-1", "", SourceAvailable, ImpactPrimary, ""),
		AuthoritySource:  srcStatus("graph_authority", "graph_authority", tAuth, "", SourceAvailable, ImpactRequired, ""),
		DiscoverySource:  srcStatus("controlstate.catalog", "unclassified_discovery", "snap-1", "", SourceAvailable, ImpactRelevant, ""),
	}
}

func governedLifecycle() LifecycleSource {
	return LifecycleSource{Observed: true, Availability: SourceAvailable, Owner: "governed", Schema: "governed_status", Identity: "gs", Status: "governed"}
}

// The catalog builder composes registry-resolved rows from typed observations: canonical class
// from OBSERVED classes, lifecycle via the canonical vocabulary, honest closure per coverage.
func TestBuildCatalogSnapshot_ComposesFromTypedObservations(t *testing.T) {
	reg := DefaultRegistry()
	cat, err := BuildCatalogSnapshot(reg, catScope(), []CatalogArtifactObservation{
		{NodeIRI: "aw:c1", Label: "Contract One", ObservedClasses: []string{rdf.ClassContract}, Lifecycle: governedLifecycle()},
		{NodeIRI: "aw:sf1", Label: "file.go", ObservedClasses: []string{rdf.ClassSourceFile}},
		{NodeIRI: "aw:int1", Label: "Intent", ObservedClasses: []string{rdf.ClassIntent}},
		{NodeIRI: "aw:mystery", Label: "??", ObservedClasses: []string{"https://example.org/NotAClass"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCatalogScope(reg, cat); err != nil {
		t.Fatalf("built catalog must validate: %v", err)
	}
	byIRI := map[string]ArtifactSummary{}
	for _, a := range cat.Artifacts {
		byIRI[a.Identity.NodeIRI] = a
	}
	// Assessable contract: unknown closure (assessment sources not consulted), PARTIAL row,
	// governed lifecycle ACTIVE via the canonical vocabulary.
	c1 := byIRI["aw:c1"]
	if c1.Class != rdf.ClassContract || c1.Closure != ClosureUnknown || c1.Availability != AvailabilityPartial {
		t.Fatalf("contract row wrong: %+v", c1)
	}
	if c1.Lifecycle != LifecycleActive {
		t.Fatalf("governed status must assess to active, got %q", c1.Lifecycle)
	}
	// Explicitly-not-applicable source file: not_applicable closure, available row.
	sf := byIRI["aw:sf1"]
	if sf.Closure != ClosureNotApplicable || sf.Availability != AvailabilityAvailable {
		t.Fatalf("source-file row wrong: %+v", sf)
	}
	// Unsupported intent: unknown closure, available row.
	in := byIRI["aw:int1"]
	if in.Closure != ClosureUnknown || in.Availability != AvailabilityAvailable {
		t.Fatalf("intent row wrong: %+v", in)
	}
	// Unknown graph class: stays visible under the unclassified sentinel, unknown everything.
	my := byIRI["aw:mystery"]
	if my.Class != UnclassifiedClassSentinel || my.Coverage != CoverageUnknown || my.Closure != ClosureUnknown {
		t.Fatalf("unknown-class row must stay visible+unknown: %+v", my)
	}
}

// An ambiguous observed-class pair resolves to the unclassified sentinel (never a fabricated
// concrete class), and a dual-typed meta principle resolves by precedence — both via the
// registry, never via the observer.
func TestBuildCatalogSnapshot_ResolutionStaysInRegistry(t *testing.T) {
	reg := DefaultRegistry()
	cat, err := BuildCatalogSnapshot(reg, catScope(), []CatalogArtifactObservation{
		{NodeIRI: "aw:amb", ObservedClasses: []string{rdf.ClassContract, rdf.ClassComponent}},
		{NodeIRI: "aw:meta", ObservedClasses: []string{rdf.ClassMetaPrinciple, rdf.ClassInvariant}},
	})
	if err != nil {
		t.Fatal(err)
	}
	byIRI := map[string]ArtifactSummary{}
	for _, a := range cat.Artifacts {
		byIRI[a.Identity.NodeIRI] = a
	}
	if byIRI["aw:amb"].Class != UnclassifiedClassSentinel {
		t.Fatalf("ambiguous pair must stay unclassified, got %q", byIRI["aw:amb"].Class)
	}
	if byIRI["aw:meta"].Class != rdf.ClassMetaPrinciple {
		t.Fatalf("dual-typed meta principle must resolve by precedence, got %q", byIRI["aw:meta"].Class)
	}
}

// The builder fails closed: an empty node IRI, a missing scope identity, and a duplicate node
// are rejected; determinism holds across rebuilds.
func TestBuildCatalogSnapshot_FailClosedAndDeterministic(t *testing.T) {
	reg := DefaultRegistry()
	if _, err := BuildCatalogSnapshot(reg, catScope(), []CatalogArtifactObservation{{NodeIRI: ""}}); err == nil {
		t.Fatal("an empty node IRI must be rejected")
	}
	noAuth := catScope()
	noAuth.GraphAuthorityIdentity = ""
	if _, err := BuildCatalogSnapshot(reg, noAuth, nil); err == nil {
		t.Fatal("a missing authority identity must be rejected")
	}
	dup := []CatalogArtifactObservation{
		{NodeIRI: "aw:x", ObservedClasses: []string{rdf.ClassIntent}},
		{NodeIRI: "aw:x", ObservedClasses: []string{rdf.ClassIntent}},
	}
	if _, err := BuildCatalogSnapshot(reg, catScope(), dup); err == nil {
		t.Fatal("a duplicate node must be rejected")
	}
	obs := []CatalogArtifactObservation{
		{NodeIRI: "aw:b", ObservedClasses: []string{rdf.ClassIntent}},
		{NodeIRI: "aw:a", ObservedClasses: []string{rdf.ClassContract}, Lifecycle: governedLifecycle()},
	}
	c1, err := BuildCatalogSnapshot(reg, catScope(), obs)
	if err != nil {
		t.Fatal(err)
	}
	// Reversed input order → identical canonical batch.
	c2, err := BuildCatalogSnapshot(reg, catScope(), []CatalogArtifactObservation{obs[1], obs[0]})
	if err != nil {
		t.Fatal(err)
	}
	if len(c1.Artifacts) != 2 || c1.Artifacts[0].Identity.NodeIRI != "aw:a" {
		t.Fatalf("catalog rows must be canonically ordered: %+v", c1.Artifacts)
	}
	for i := range c1.Artifacts {
		if !reflect.DeepEqual(c1.Artifacts[i], c2.Artifacts[i]) {
			t.Fatal("catalog composition is not deterministic under input reordering")
		}
	}
}
