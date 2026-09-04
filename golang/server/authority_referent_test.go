// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/globulario/sensei/golang/closure"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store"
)

// authorityReferentServer builds a server whose HOME domain is proven and whose
// REQUESTED domain is not — the shape of a real multi-domain store where one
// repository has been published and another has not.
//
// It records every domain the closure evaluator is asked about, so a test can
// prove the surface asked about the domain it answered for rather than merely
// producing an agreeable verdict.
func authorityReferentServer(t *testing.T) (*server, *[]string) {
	t.Helper()
	// THE STORE MUST BE FRESH, or authority can never be AUTHORITATIVE and the
	// closure domain never decides anything. A first version of this fixture
	// was not fresh: the defect could be reintroduced and the test still
	// passed, because the assertion was unreachable. The positive control
	// below exists to keep that from recurring silently.
	_, marker := seedmeta.AppendMarker([]byte("<https://example.test/s> <https://example.test/p> <https://example.test/x> .\n"))
	markerPath := filepath.Join(t.TempDir(), "graph-authority.json")
	if err := seedmeta.WriteMarkerFile(markerPath, marker); err != nil {
		t.Fatalf("write marker file: %v", err)
	}
	// AND CERTIFIED BY A MATCHING TRANSACTION, for exactly the reason stated
	// above about freshness: authority requires all three conjuncts, so a
	// fixture missing one can never reach AUTHORITATIVE and every positive
	// assertion below becomes unreachable. The transaction conjunct was absent
	// from the implementation until 2026-09-04 while the wire contract had
	// always required it; adding it to the verdict made that unreachability
	// real here, which is the fixture doing its job rather than a regression.
	//
	// The two repository commits are deliberately different values: they are
	// different repositories, and a fixture reusing one SHA for both would pass
	// while proving nothing about which field carries which.
	if err := os.WriteFile(seedmeta.RuntimeTransactionPath(markerPath), []byte(
		"format\tv1\n"+
			"seed\tdigest_sha256\t"+marker.Digest+"\n"+
			"seed\ttriple_count\t"+strconv.FormatInt(marker.TripleCount, 10)+"\n"+
			"repo\tawareness-graph\t58c055fbd9d65ad2f5a8c965f728897012e75f09\n"+
			"repo\tservices\tb98c91eb540a3e5aa9d97e7b3c08005e08cb1897\n"), 0o644); err != nil {
		t.Fatalf("write transaction stamp: %v", err)
	}
	s := newTestServer(runtimeMarkerStore{
		describeFn: func(_ context.Context, iri string) ([]store.Triple, error) {
			return []store.Triple{
				{Predicate: seedmeta.NamespaceIRI + "seedDigestSha256", Object: marker.Digest},
				{Predicate: seedmeta.NamespaceIRI + "seedTripleCount", Object: strconv.FormatInt(marker.TripleCount, 10)},
			}, nil
		},
		countFn: func(context.Context) (int64, error) { return marker.TripleCount, nil },
	})
	s.graphMarkerFile = markerPath
	s.homeDomain = "github.com/globulario/sensei"
	asked := &[]string{}
	s.closureEval = func(domain string) (closure.SemanticState, string) {
		*asked = append(*asked, domain)
		if domain == pubTestDomain {
			return closure.SemanticClosureUnproven,
				"no closure proof for " + pubTestDomain + " was available to carry forward"
		}
		return closure.SemanticClosureProven, "the home domain is proven"
	}
	return s, asked
}

// A DOMAIN-SCOPED ANSWER MAY NOT CARRY ANOTHER DOMAIN'S AUTHORITY.
//
// Query scopes every row to the requested domain and then stamped the response
// with s.graphAuthority(ctx), which resolves closure for the server's HOME
// domain. On a multi-domain store that pairs an AUTHORITATIVE verdict earned by
// one domain with rows drawn from another.
//
// Observed live before this repair:
//
//	awareness_query(domain=github.com/globulario/sensei)  -> authoritative: true
//	awareness_metadata(domain=github.com/globulario/sensei) -> non_authoritative
//
// while the store's own proof set held github.com_globulario_sensei.unproven.json.
// Two surfaces, one referent, opposite verdicts.
func TestQueryAuthorityDescribesTheRequestedDomainNotTheHomeDomain(t *testing.T) {
	s, asked := authorityReferentServer(t)

	resp, err := s.Query(context.Background(), &awarenesspb.QueryRequest{
		Mode:   awarenesspb.QueryMode_QUERY_MODE_BY_ID,
		Id:     "invariant:does.not.matter",
		Domain: pubTestDomain,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(*asked) == 0 || (*asked)[0] != pubTestDomain {
		t.Fatalf("closure was evaluated for %v, not the requested domain %q — "+
			"the attestation describes a different referent than the answer", *asked, pubTestDomain)
	}
	if resp.GetAuthority().GetVerdict() == awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_AUTHORITATIVE {
		t.Fatal("AUTHORITATIVE reported for a domain with no closure proof, using the home domain's proof")
	}
	// THE WORST SPECIMEN, stated explicitly: an empty result carrying a
	// positive authority claim turns "this surface returned nothing" into an
	// apparently authoritative claim that nothing exists.
	if len(resp.GetRows()) == 0 && resp.GetAuthority().GetAuthoritative() {
		t.Fatal("rows: [] reported as AUTHORITATIVE — absence of returned evidence became an authoritative negative")
	}
}

// POSITIVE CONTROL: the fixture must be able to produce AUTHORITATIVE at all.
//
// Without this, a fixture that is merely never-fresh would make the test above
// pass whether or not the defect is present — which is exactly what the first
// version of it did.
func TestQueryAuthorityIsAuthoritativeForAProvenDomain(t *testing.T) {
	s, asked := authorityReferentServer(t)

	resp, err := s.Query(context.Background(), &awarenesspb.QueryRequest{
		Mode:   awarenesspb.QueryMode_QUERY_MODE_BY_ID,
		Id:     "invariant:does.not.matter",
		Domain: s.homeDomain,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(*asked) == 0 || (*asked)[0] != s.homeDomain {
		t.Fatalf("closure was evaluated for %v, not %q", *asked, s.homeDomain)
	}
	if resp.GetAuthority().GetVerdict() != awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_AUTHORITATIVE {
		t.Fatalf("a proven, fresh domain was not reported AUTHORITATIVE (%v): the fixture "+
			"cannot reach the state the negative case rules out, so that case proves nothing",
			resp.GetAuthority().GetVerdict())
	}
}

// EVERY SURFACE MUST PROJECT THE SAME AUTHORITY FOR THE SAME REFERENT.
//
// This is the drift detector. Each surface can look individually reasonable and
// still disagree with its siblings — which is exactly what happened: query,
// briefing, impact and resolve stamped the HOME domain's authority onto answers
// scoped to a requested domain, while metadata read the requested domain
// correctly. Asserting each surface separately would not have caught it,
// because each was self-consistent.
//
// A surface added later that forgets to scope its attestation fails here
// without anyone remembering to write a new test for it.
func TestAllDomainScopedSurfacesProjectTheSameAuthority(t *testing.T) {
	s, _ := authorityReferentServer(t)
	ctx := context.Background()
	const d = pubTestDomain // UNPROVEN, while the home domain is proven

	type surface struct {
		name string
		get  func() *awarenesspb.GraphAuthority
	}
	surfaces := []surface{
		{"query", func() *awarenesspb.GraphAuthority {
			r, err := s.Query(ctx, &awarenesspb.QueryRequest{
				Mode: awarenesspb.QueryMode_QUERY_MODE_BY_ID, Id: "invariant:x", Domain: d})
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			return r.GetAuthority()
		}},
		{"resolve", func() *awarenesspb.GraphAuthority {
			r, err := s.Resolve(ctx, &awarenesspb.ResolveRequest{Class: "invariant", Id: "x", Domain: d})
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			return r.GetAuthority()
		}},
		{"briefing", func() *awarenesspb.GraphAuthority {
			r, err := s.Briefing(ctx, &awarenesspb.BriefingRequest{File: "test/example.go", Domain: d})
			if err != nil {
				t.Fatalf("briefing: %v", err)
			}
			return r.GetAuthority()
		}},
		{"impact", func() *awarenesspb.GraphAuthority {
			r, err := s.Impact(ctx, &awarenesspb.ImpactRequest{File: "test/example.go", Domain: d})
			if err != nil {
				t.Fatalf("impact: %v", err)
			}
			return r.GetAuthority()
		}},
		// METADATA IS HERE BECAUSE THE COMMENT ABOVE WAS WRONG ABOUT IT.
		//
		// It recorded metadata as the surface that "read the requested domain
		// correctly", and that was true of its COUNTS and false of its
		// authority: it called graphAuthorityFromSnapshot -- closureDomain ""
		// -- before it had even read req.GetDomain(). The one surface excluded
		// from the drift detector on the grounds that it was already right was
		// the one still carrying the defect.
		{"metadata", func() *awarenesspb.GraphAuthority {
			r, err := s.Metadata(ctx, &awarenesspb.MetadataRequest{Domain: d})
			if err != nil {
				t.Fatalf("metadata: %v", err)
			}
			return r.GetAuthority()
		}},
		{"graphAuthorityFor", func() *awarenesspb.GraphAuthority {
			return s.graphAuthorityFor(ctx, d)
		}},
	}
	// The names above are the surfaces this test drives.
	// TestEveryAuthorityProjectingSurfaceIsCovered requires that set to account
	// for every file in the package that projects authority, so a surface added
	// later cannot quietly skip this check.
	driven := map[string]bool{}
	for _, sf := range surfaces {
		driven[sf.name] = true
	}
	for file, name := range authorityCoveredFiles {
		if name == "" {
			continue // deliberately not domain-scoped; the entry records why
		}
		if !driven[name] {
			t.Errorf("authorityCoveredFiles maps %s to surface %q, which this test does "+
				"not drive: the covered set claims coverage this test does not provide",
				file, name)
		}
	}

	want := surfaces[len(surfaces)-1].get() // the canonical computation
	for _, sf := range surfaces {
		got := sf.get()
		if got.GetVerdict() != want.GetVerdict() {
			t.Errorf("%s projects verdict %v for domain %q; the canonical computation says %v — "+
				"two surfaces disagree about one referent", sf.name, got.GetVerdict(), d, want.GetVerdict())
		}
		if got.GetAuthoritative() != want.GetAuthoritative() {
			t.Errorf("%s projects authoritative=%v for domain %q; canonical says %v",
				sf.name, got.GetAuthoritative(), d, want.GetAuthoritative())
		}
	}
	// And the specimen that matters for F: an unproven domain must never
	// inherit a proven sibling's authority, whatever the rows say.
	if want.GetAuthoritative() {
		t.Fatalf("the fixture's unproven domain reported AUTHORITATIVE; the case under test was not constructed")
	}
}
