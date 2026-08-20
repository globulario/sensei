// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/graphgeneration"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store"
)

const shortfallDomain = "github.com/globulario/services"

// countingStore answers the domain-OWNED count, and can fail it, which is a
// different answer from counting zero.
type countingStore struct {
	fakeStore
	live int64
	err  error
}

func (c countingStore) CountTriplesOwnedByDomain(context.Context, string) (int64, error) {
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

	reports := domainSliceReports(context.Background(), st, storeURL, "")
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

	if reports := domainSliceReports(context.Background(), st, storeURL, ""); len(reports) != 0 {
		t.Fatalf("a present slice must not be accused: %q", reports[0].line())
	}
}

// "I could not check" is reported as itself. Rendering it as agreement is the
// equivalence this whole surface exists to refuse.
func TestDomainSliceCountFailureIsReportedNotSwallowed(t *testing.T) {
	storeURL := writeCountedProofSet(t, 121338)
	st := countingStore{err: errors.New("backend refused")}

	reports := domainSliceReports(context.Background(), st, storeURL, "")
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
	if reports := domainSliceReports(context.Background(), st, "http://127.0.0.1:7878/query", ""); len(reports) != 0 {
		t.Fatalf("got %d report(s), want none: %q", len(reports), reports[0].line())
	}
}

// A store that cannot answer a domain-scoped count yields no reports rather
// than an unfounded one; there is nothing to compare.
func TestAStoreThatCannotCountDomainsProducesNoReports(t *testing.T) {
	storeURL := writeCountedProofSet(t, 121338)
	if reports := domainSliceReports(context.Background(), fakeStore{}, storeURL, ""); len(reports) != 0 {
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

// The live count must measure the same set the expectation describes. A count
// that also draws in shared subjects lets the store's unrelated content stand in
// for a domain's own missing content: a 100-triple domain deleted from a store
// retaining 50 shared triples would report nothing at all.
func TestTheLiveCountMeasuresOnlyWhatTheDomainOwns(t *testing.T) {
	storeURL := writeCountedProofSet(t, 100)

	// A store that answers the SCOPED count (shared subjects included) does not
	// satisfy the capability this check requires, so it produces no reports
	// rather than a wrong one.
	scopedOnly := scopedCountingStore{live: 50}
	if reports := domainSliceReports(context.Background(), scopedOnly, storeURL, ""); len(reports) != 0 {
		t.Fatalf("a store that cannot count owned triples must not be asked to: %q", reports[0].line())
	}

	// The owned count sees the domain for what it is: empty.
	owned := countingStore{live: 0}
	reports := domainSliceReports(context.Background(), owned, storeURL, "")
	if len(reports) != 1 {
		t.Fatalf("got %d report(s), want 1 — the loss must not hide behind shared content", len(reports))
	}
	if line := reports[0].line(); !strings.Contains(line, "holds 0 triple(s) live but its proof set recorded 100") {
		t.Fatalf("got %q", line)
	}
}

// scopedCountingStore has the domain-SCOPE count and not the owned count — the
// shape that would mask a loss if it were accepted.
type scopedCountingStore struct {
	fakeStore
	live int64
}

func (c scopedCountingStore) CountTriplesInDomain(context.Context, string, string) (int64, error) {
	return c.live, nil
}

// -no-seed is how the deployments with the most to lose are started: the
// appliance entrypoint and the documented project flow both use it, and it
// skips enforceCurrentSeed entirely. Whether a domain's published knowledge is
// still in the store is a question about the STORE, so the report must not live
// in the branch that those deployments skip.
func TestTheStartupShortfallReportIsNotGuardedBySeeding(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	const call = "reportDomainSliceShortfalls"

	var serveBody *ast.BlockStmt
	nested := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}
		if fn.Name.Name == "serve" {
			serveBody = fn.Body
		}
		if fn.Name.Name == "enforceCurrentSeed" {
			ast.Inspect(fn.Body, func(inner ast.Node) bool {
				if id, ok := inner.(*ast.Ident); ok && id.Name == call {
					nested[fn.Name.Name] = true
				}
				return true
			})
		}
		return true
	})
	if serveBody == nil {
		t.Fatal("serve not found")
	}
	if nested["enforceCurrentSeed"] {
		t.Fatal("the report lives inside enforceCurrentSeed, which -no-seed never reaches")
	}

	unconditional := false
	for _, stmt := range serveBody.List {
		expr, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		if c, ok := expr.X.(*ast.CallExpr); ok {
			if id, ok := c.Fun.(*ast.Ident); ok && id.Name == call {
				unconditional = true
			}
		}
	}
	if !unconditional {
		t.Fatalf("%s must be called from serve's own body, not from a branch a deployment can skip", call)
	}
}
