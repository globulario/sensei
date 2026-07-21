// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import (
	"strings"
	"testing"
)

// stale/invalid/unobserved catalog fixtures over the three-source ledger.
func ledgerCatalog(reg Registry, n int, mutate func(*CatalogSnapshot)) CatalogSnapshot {
	cat := catalog(reg, n)
	if mutate != nil {
		mutate(&cat)
	}
	return cat
}

// Proof: a STALE authority (degraded required source) yields a PARTIAL index that still exposes
// the known rows — never AVAILABLE, and never an empty page that discards known rows.
func TestLedger_StaleAuthorityPartialWithRows(t *testing.T) {
	reg := DefaultRegistry()
	cat := ledgerCatalog(reg, 4, func(c *CatalogSnapshot) {
		c.AuthoritySource = srcStatus("graph_authority", "graph_authority", tAuth, "", SourceDegraded, ImpactRequired, "graph_authority_stale")
	})
	idx, err := BuildArtifactIndex(reg, indexReq(10), cat)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Page) != 4 {
		t.Fatalf("stale authority must keep known rows, got %d", len(idx.Page))
	}
	if idx.Availability != AvailabilityPartial {
		t.Fatalf("stale authority must yield PARTIAL, got %q", idx.Availability)
	}
	found := false
	for _, s := range idx.Sources {
		if s.Owner == "graph_authority" && s.Availability == SourceDegraded && s.ReasonCode == "graph_authority_stale" {
			found = true
		}
	}
	if !found {
		t.Fatal("the stale authority must appear as a degraded required source with its typed reason")
	}
}

// Proof: an INTEGRITY-FAILED authority can never back an available enumeration; with the
// enumeration invalid, no rows are exposed and the index is not available.
func TestLedger_IntegrityFailedExposesNoTrustedRows(t *testing.T) {
	reg := DefaultRegistry()
	// An available enumeration on an invalid authority is a contract violation.
	bad := ledgerCatalog(reg, 4, func(c *CatalogSnapshot) {
		c.AuthoritySource = srcStatus("graph_authority", "graph_authority", tAuth, "", SourceInvalid, ImpactRequired, "graph_authority_integrity_failure")
	})
	if ValidateCatalogScope(reg, bad) == nil {
		t.Fatal("an integrity-failed authority cannot back an AVAILABLE catalog enumeration")
	}
	// The honest form: enumeration invalid too, zero rows.
	honest := ledgerCatalog(reg, 0, func(c *CatalogSnapshot) {
		c.Source = srcStatus("controlstate.catalog", "catalog_enumeration", "snap-1", "", SourceInvalid, ImpactPrimary, "graph_authority_integrity_failure")
		c.AuthoritySource = srcStatus("graph_authority", "graph_authority", tAuth, "", SourceInvalid, ImpactRequired, "graph_authority_integrity_failure")
	})
	idx, err := BuildArtifactIndex(reg, indexReq(10), honest)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Page) != 0 {
		t.Fatal("an integrity-failed catalog must expose no rows")
	}
	if idx.Availability == AvailabilityAvailable || idx.Availability == AvailabilityPartial {
		t.Fatalf("an invalid primary must not read as available/partial, got %q", idx.Availability)
	}
}

// Proof: an UNOBSERVED authority can never produce an available catalog/index, and the expected
// seed digest never becomes an observed catalog authority identity.
func TestLedger_UnobservedAuthorityNeverAvailable(t *testing.T) {
	reg := DefaultRegistry()
	// An available enumeration on an unobserved authority is a contract violation.
	bad := ledgerCatalog(reg, 4, func(c *CatalogSnapshot) {
		c.AuthoritySource = srcStatus("graph_authority", "graph_authority", "", "", SourceUnavailable, ImpactRequired, "graph_authority_unobserved")
	})
	if ValidateCatalogScope(reg, bad) == nil {
		t.Fatal("an unobserved authority cannot back an AVAILABLE catalog enumeration")
	}
	// Rows without an observed authority identity are a contract violation.
	noID := ledgerCatalog(reg, 4, func(c *CatalogSnapshot) {
		c.GraphAuthorityIdentity = ""
		c.Source = srcStatus("controlstate.catalog", "catalog_enumeration", "snap-1", "", SourceUnavailable, ImpactPrimary, "graph_authority_unobserved")
		c.AuthoritySource = srcStatus("graph_authority", "graph_authority", "", "", SourceUnavailable, ImpactRequired, "graph_authority_unobserved")
	})
	if ValidateCatalogScope(reg, noID) == nil {
		t.Fatal("a catalog without an observed authority identity cannot carry rows")
	}
	// An OBSERVED authority source claiming availability with no identity is a violation (the
	// expected seed digest must not be smuggled in to fill the gap).
	smuggle := ledgerCatalog(reg, 0, func(c *CatalogSnapshot) {
		c.GraphAuthorityIdentity = ""
		c.Source = srcStatus("controlstate.catalog", "catalog_enumeration", "snap-1", "", SourceUnavailable, ImpactPrimary, "graph_authority_unobserved")
		c.AuthoritySource = srcStatus("graph_authority", "graph_authority", "", "", SourceAvailable, ImpactRequired, "")
	})
	if ValidateCatalogScope(reg, smuggle) == nil {
		t.Fatal("an observed authority source requires the observed authority identity")
	}
	// The honest unobserved form: unavailable everywhere, zero rows, expected digest only as a
	// LIMITATION — the index can never be available.
	honest := ledgerCatalog(reg, 0, func(c *CatalogSnapshot) {
		c.GraphAuthorityIdentity = ""
		c.Source = srcStatus("controlstate.catalog", "catalog_enumeration", "snap-1", "", SourceUnavailable, ImpactPrimary, "graph_authority_unobserved")
		c.AuthoritySource = srcStatus("graph_authority", "graph_authority", "", "", SourceUnavailable, ImpactRequired, "graph_authority_unobserved")
		c.DiscoverySource = srcStatus("controlstate.catalog", "unclassified_discovery", "", "", SourceUnavailable, ImpactRelevant, "not_enumerated")
		c.Limitations = []string{"expected_authority_seed_digest:expected-abc"}
	})
	idx, err := BuildArtifactIndex(reg, indexReq(10), honest)
	if err != nil {
		t.Fatal(err)
	}
	if idx.Availability == AvailabilityAvailable || idx.Availability == AvailabilityPartial {
		t.Fatalf("an unobserved authority must not read as available/partial, got %q", idx.Availability)
	}
	if len(idx.Page) != 0 {
		t.Fatal("no trusted rows under an unobserved authority")
	}
	// The expected digest appears ONLY as a limitation — never as any source identity.
	limFound := false
	for _, l := range idx.Limitations {
		if strings.Contains(l, "expected_authority_seed_digest:expected-abc") {
			limFound = true
		}
	}
	if !limFound {
		t.Fatal("the expected seed digest must surface as expected-authority metadata (limitation)")
	}
	for _, s := range idx.Sources {
		if s.Identity == "expected-abc" {
			t.Fatal("the expected seed digest must never appear as a source identity")
		}
	}
}

// Proof: a degraded unknown-class discovery (truncation) keeps known rows visible with a PARTIAL
// projection and the explicit typed reason.
func TestLedger_TruncatedDiscoveryKeepsKnownRows(t *testing.T) {
	reg := DefaultRegistry()
	cat := ledgerCatalog(reg, 4, func(c *CatalogSnapshot) {
		c.DiscoverySource = srcStatus("controlstate.catalog", "unclassified_discovery", "snap-1", "", SourceDegraded, ImpactRelevant, "unclassified_discovery_truncated")
	})
	idx, err := BuildArtifactIndex(reg, indexReq(10), cat)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Page) != 4 {
		t.Fatalf("known rows must stay visible while unknown discovery is partial, got %d", len(idx.Page))
	}
	if idx.Availability != AvailabilityPartial {
		t.Fatalf("degraded discovery must yield PARTIAL, got %q", idx.Availability)
	}
}
