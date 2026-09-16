// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// exportingStore is a store that can materialize one domain's graph.
//
// It embeds fakeStore so freshness, marker discovery and counts behave exactly
// as every other server test's store does; only the export capability is added.
// The bytes it returns are the fixture's whole subject, because the handler's
// job is to certify THOSE bytes rather than to trust a digest reported beside
// them.
type exportingStore struct {
	fakeStore
	nt      []byte
	err     error
	asked   []string
	refuses bool
}

func (e *exportingStore) ExportDomainGraph(_ context.Context, domain string) ([]byte, error) {
	e.asked = append(e.asked, domain)
	if e.err != nil {
		return nil, e.err
	}
	return e.nt, nil
}

// servedNT is a tiny graph plus the marker that names its own identity, which is
// what makes the fixture's served digest and its exported bytes the same claim.
func servedNT(t *testing.T) ([]byte, seedmeta.Marker) {
	t.Helper()
	base := []byte("<https://example.org/a> <https://example.org/p> \"A\" .\n")
	stamped, marker := seedmeta.AppendMarker(base)
	return stamped, marker
}

// currentFor makes the server report the fixture's graph as its current,
// coherent served identity.
func currentFor(marker seedmeta.Marker) func(context.Context) seedmeta.Verification {
	return func(context.Context) seedmeta.Verification {
		return seedmeta.Verification{
			State:           seedmeta.FreshnessCurrent,
			Expected:        marker,
			Live:            marker,
			MarkerPresent:   true,
			LiveTripleCount: marker.TripleCount,
			Detail:          "test fixture: the store holds exactly the marker it certifies",
		}
	}
}

// The capability itself: bytes and identity together, from one resolution.
func TestGetDomainGraphReturnsTheServedGraphWithItsIdentity(t *testing.T) {
	nt, marker := servedNT(t)
	st := &exportingStore{nt: nt}
	st.graphFreshness = currentFor(marker)
	srv := newTestServer(st)

	resp, err := srv.GetDomainGraph(context.Background(), &awarenesspb.GetDomainGraphRequest{
		Domain: "github.com/globulario/sensei-code",
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if string(resp.GetNtriples()) != string(nt) {
		t.Fatalf("the response did not carry the served bytes verbatim")
	}
	if resp.GetGeneration() != marker.Digest || resp.GetGraphDigest() != marker.Digest {
		t.Fatalf("generation=%s digest=%s, want the served identity %s",
			resp.GetGeneration(), resp.GetGraphDigest(), marker.Digest)
	}
	if resp.GetDomain() != "github.com/globulario/sensei-code" {
		t.Fatalf("the response does not name the domain it is the graph of: %q", resp.GetDomain())
	}
	if resp.GetFormat() != "ntriples" || resp.GetTripleCount() != marker.TripleCount {
		t.Fatalf("format=%q triples=%d, want ntriples and %d", resp.GetFormat(), resp.GetTripleCount(), marker.TripleCount)
	}
	if len(st.asked) != 1 || st.asked[0] != "github.com/globulario/sensei-code" {
		t.Fatalf("the store was asked for %v", st.asked)
	}
}

// Exporting the same unchanged graph twice yields the same identity, so anything
// binding to a snapshot binds to a stable graph.
func TestGetDomainGraphIsDeterministicForAnUnchangedGraph(t *testing.T) {
	nt, marker := servedNT(t)
	st := &exportingStore{nt: nt}
	st.graphFreshness = currentFor(marker)
	srv := newTestServer(st)

	first, err := srv.GetDomainGraph(context.Background(), &awarenesspb.GetDomainGraphRequest{Domain: "github.com/globulario/sensei-code"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := srv.GetDomainGraph(context.Background(), &awarenesspb.GetDomainGraphRequest{Domain: "github.com/globulario/sensei-code"})
	if err != nil {
		t.Fatal(err)
	}
	if first.GetGraphDigest() != second.GetGraphDigest() || string(first.GetNtriples()) != string(second.GetNtriples()) {
		t.Fatal("two exports of one unchanged graph identify different graphs")
	}
}

// Every way of being unable to certify an export refuses, and refuses BEFORE
// producing bytes a caller could mistake for a certified snapshot.
func TestGetDomainGraphRefusesWhatItCannotCertify(t *testing.T) {
	nt, marker := servedNT(t)

	for name, c := range map[string]struct {
		store    store.Store
		domain   string
		code     codes.Code
		want     string
		neverAsk bool
	}{
		"an unnamed domain": {
			store:    func() store.Store { s := &exportingStore{nt: nt}; s.graphFreshness = currentFor(marker); return s }(),
			domain:   "   ",
			code:     codes.InvalidArgument,
			want:     "export requires a domain",
			neverAsk: true,
		},
		"a store that cannot export": {
			store: func() store.Store {
				s := fakeStore{graphFreshness: currentFor(marker)}
				return s
			}(),
			domain: "github.com/globulario/sensei-code",
			code:   codes.Unavailable,
			want:   "cannot materialize one domain's graph",
		},
		"the store and its marker disagree": {
			store: func() store.Store {
				s := &exportingStore{nt: nt}
				s.graphFreshness = func(context.Context) seedmeta.Verification {
					return seedmeta.Verification{
						State: seedmeta.FreshnessStale, Expected: marker, Live: marker,
						MarkerPresent: true, Detail: "test fixture: the store does not hold what its marker certifies",
					}
				}
				return s
			}(),
			domain: "github.com/globulario/sensei-code",
			code:   codes.FailedPrecondition,
			want:   "disagree",
		},
		// Freshness is checked before identity, so an unknown-freshness store
		// refuses HERE and names the state. The next case reaches the identity
		// branch on its own.
		"the store's freshness cannot be established": {
			store: func() store.Store {
				s := &exportingStore{nt: nt}
				s.graphFreshness = func(context.Context) seedmeta.Verification {
					return seedmeta.Verification{
						State:  seedmeta.FreshnessUnknown,
						Detail: "test fixture: this server cannot state a served identity",
					}
				}
				return s
			}(),
			domain: "github.com/globulario/sensei-code",
			code:   codes.FailedPrecondition,
			want:   "disagree (unknown)",
		},
		// THE E3 WITNESS, and why it took a real one to reach.
		//
		// servedGraphDigest falls through to the independent live-authority
		// observation whenever the verification is not current, and
		// AdmitLiveMarker declines to admit an identity for several real store
		// conditions. Ambiguity is the one this branch's message describes: TWO
		// live SeedBuild markers, each internally coherent at its own
		// digest-derived IRI, so neither is rejected for incoherence and the
		// store simply cannot say which generation it serves. The observation is
		// integrity-failed, the served digest is empty, and the handler refuses
		// BEFORE the freshness check it would otherwise reach.
		//
		// The earlier attempt at this case was contrived and reached the identity
		// comparison instead, which is why it was deleted rather than tuned: a
		// fixture that passes through a different branch proves nothing about
		// this one.
		"the live authority is ambiguous, so no identity is served": {
			store: func() store.Store {
				_, first := seedmeta.AppendMarker([]byte("<https://example.org/one> <https://example.org/p> \"1\" .\n"))
				_, second := seedmeta.AppendMarker([]byte("<https://example.org/two> <https://example.org/p> \"2\" .\n"))
				s := &exportingStore{nt: nt}
				s.seedMarkerFacts = func(context.Context) []store.ImpactFact {
					return append(
						seedBuildMarkerFacts(first.IRI, first.Digest, first.TripleCount),
						seedBuildMarkerFacts(second.IRI, second.Digest, second.TripleCount)...,
					)
				}
				s.graphFreshness = func(context.Context) seedmeta.Verification {
					return seedmeta.Verification{
						State:    seedmeta.FreshnessStale,
						Expected: marker,
						Detail:   "test fixture: the store holds two live markers and matches neither",
					}
				}
				return s
			}(),
			domain:   "github.com/globulario/sensei-code",
			code:     codes.FailedPrecondition,
			want:     "no coherent graph identity",
			neverAsk: true,
		},
		"the bytes are not the graph whose identity is served": {
			store: func() store.Store {
				other, _ := seedmeta.AppendMarker([]byte("<https://example.org/z> <https://example.org/p> \"Z\" .\n"))
				s := &exportingStore{nt: other}
				s.graphFreshness = currentFor(marker)
				return s
			}(),
			domain: "github.com/globulario/sensei-code",
			code:   codes.FailedPrecondition,
			want:   "certified only when they are the same graph",
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv := newTestServer(c.store)
			resp, err := srv.GetDomainGraph(context.Background(), &awarenesspb.GetDomainGraphRequest{Domain: c.domain})
			if err == nil {
				t.Fatalf("the export succeeded and returned %d bytes", len(resp.GetNtriples()))
			}
			if got := status.Code(err); got != c.code {
				t.Fatalf("code=%s, want %s (%v)", got, c.code, err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not say %q", err, c.want)
			}
			if resp != nil {
				t.Fatalf("a refusal still returned a response: %+v", resp)
			}
			if c.neverAsk {
				if es, ok := c.store.(*exportingStore); ok && len(es.asked) != 0 {
					t.Fatalf("a refused export still asked the store for %v", es.asked)
				}
			}
		})
	}
}
