// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/graphgeneration"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store"
)

const shortfallDomain = "github.com/globulario/services"

// countingStore answers the domain-scoped count, and can fail it, which is a
// different answer from counting zero.
type countingStore struct {
	fakeStore
	live int64
	err  error
}

func (c countingStore) CountTriplesInDomain(context.Context, string, string) (int64, error) {
	return c.live, c.err
}

// writeCountedProofSet publishes a proof set recording `published` triples for
// one domain, and returns the store URL it describes. HOME is redirected first:
// the proof set is deliberately store-keyed under the operator's home, so a
// test that did not redirect it would write into the real one.
func writeCountedProofSet(t *testing.T, published int64) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	const storeURL = "http://127.0.0.1:7878/query"
	dir, err := graphgeneration.Dir(storeURL)
	if err != nil {
		t.Fatal(err)
	}
	m := seedmeta.Marker{
		Digest:      "00000000000000000000000000000000000000000000000000000000000000ee",
		IRI:         "urn:sensei:seed:ee",
		TripleCount: published,
	}
	err = graphgeneration.Write(dir, storeURL, &graphgeneration.Set{
		Generation: graphgeneration.Generation{
			MarkerDigest: m.Digest, MarkerIRI: m.IRI, TripleCount: m.TripleCount,
			PublishedUnix: 1700000000, PublishedDomain: shortfallDomain,
		},
		Marker: m,
		Domains: map[string]graphgeneration.DomainProof{
			shortfallDomain: {SliceDigest: "deadbeef", SliceTripleCount: published},
		},
	})
	if err != nil {
		t.Fatalf("write proof set: %v", err)
	}
	return storeURL
}

// The state #221 recorded: a domain's compiled knowledge is gone from the store
// while its proof set still says what was published. Nothing else in the system
// can notice, because a store that lost a slice and a store that never held one
// are identical from the inside.
func TestDomainSliceShortfallIsReportedWithItsRepair(t *testing.T) {
	storeURL := writeCountedProofSet(t, 121338)
	st := countingStore{live: 6}

	reports := domainSliceReports(context.Background(), st, storeURL, "", "")
	if len(reports) != 1 {
		t.Fatalf("got %d report(s), want 1", len(reports))
	}
	line := reports[0].line()
	for _, want := range []string{"domain_slice_shortfall", shortfallDomain, "6", "121338", "sensei build --repo " + shortfallDomain} {
		if !strings.Contains(line, want) {
			t.Fatalf("report must contain %q; got %q", want, line)
		}
	}
}

func TestDomainSliceReportsStaySilentWhenTheSliceIsPresent(t *testing.T) {
	storeURL := writeCountedProofSet(t, 121338)
	st := countingStore{live: 121400} // a reader counts shared subjects too

	if reports := domainSliceReports(context.Background(), st, storeURL, "", ""); len(reports) != 0 {
		t.Fatalf("a present slice must not be accused: %q", reports[0].line())
	}
}

// "I could not check" is reported as itself. Rendering it as agreement is the
// equivalence this whole surface exists to refuse.
func TestDomainSliceCountFailureIsReportedNotSwallowed(t *testing.T) {
	storeURL := writeCountedProofSet(t, 121338)
	st := countingStore{err: errors.New("backend refused")}

	reports := domainSliceReports(context.Background(), st, storeURL, "", "")
	if len(reports) != 1 {
		t.Fatalf("got %d report(s), want 1", len(reports))
	}
	if line := reports[0].line(); !strings.Contains(line, "domain_slice_unverified") {
		t.Fatalf("an unmeasurable slice must not read as a measured one; got %q", line)
	}
}

// No proof set is not a shortfall. Absence of an expectation is reported by the
// marker path as its own kind of absence, and must not be turned into an
// accusation here.
func TestNoProofSetProducesNoDomainSliceReports(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	st := countingStore{live: 0}
	if reports := domainSliceReports(context.Background(), st, "http://127.0.0.1:7878/query", "", ""); len(reports) != 0 {
		t.Fatalf("got %d report(s), want none: %q", len(reports), reports[0].line())
	}
}

// A store that cannot answer a domain-scoped count yields no reports rather
// than an unfounded one; there is nothing to compare.
func TestAStoreThatCannotCountDomainsProducesNoReports(t *testing.T) {
	storeURL := writeCountedProofSet(t, 121338)
	if reports := domainSliceReports(context.Background(), fakeStore{}, storeURL, "", ""); len(reports) != 0 {
		t.Fatalf("got %d report(s), want none", len(reports))
	}
}

// Read time, not write time: the operator meets this on the surface consulted
// BEFORE editing, rather than on the refusal after.
func TestPreflightSurfacesTheDomainSliceShortfall(t *testing.T) {
	storeURL := writeCountedProofSet(t, 121338)
	invalidateImplementationPatternCacheForTest()
	s := newTestServer(countingStore{
		live: 6,
		fakeStore: fakeStore{
			impactForFile: func(context.Context, string) ([]store.ImpactFact, error) { return nil, nil },
		},
	})
	s.oxigraphQueryURL = storeURL
	s.homeDomain = shortfallDomain

	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "adjust the helper",
		Files: []string{"internal/x/x.go"},
	})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	found := false
	for _, b := range resp.GetBlindSpots() {
		if strings.Contains(b, "domain_slice_shortfall") && strings.Contains(b, "sensei build --repo") {
			found = true
		}
	}
	if !found {
		t.Fatalf("preflight did not report the missing slice: %v", resp.GetBlindSpots())
	}
}
