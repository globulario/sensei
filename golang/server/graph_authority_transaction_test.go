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

// The composed verdict has three conjuncts, and one of them was computed and
// then dropped on the floor.
//
// proto/awareness_graph.proto, MetadataResponse.authority:
//
//	"The SAME composed authority verdict every graph-backed surface returns:
//	 freshness AND the closure proof bound to this publication AND the
//	 transaction certification — not freshness alone."
//
// graphAuthorityFromSnapshotFor computed transactionMatchesSeed, returned it as
// an evidence field, and left it out of the verdict. That permits exactly the
// state observed live on 2026-09-03 against github.com/globulario/sensei-code:
//
//	authoritative                      true
//	embedded_transaction_matches_seed  false
//
// A graph vouched for by no transaction certification is a graph whose
// publication nobody signed. The evidence was on the wire the whole time and
// the conclusion ignored it.

// transactionAuthorityServer is FRESH and CLOSED -- the two conjuncts that were
// already enforced -- so the transaction is the only variable left.
func transactionAuthorityServer(t *testing.T, writeStamp bool) *server {
	t.Helper()
	_, marker := seedmeta.AppendMarker([]byte("<https://example.test/s> <https://example.test/p> <https://example.test/x> .\n"))
	markerPath := filepath.Join(t.TempDir(), "graph-authority.json")
	if err := seedmeta.WriteMarkerFile(markerPath, marker); err != nil {
		t.Fatalf("write marker file: %v", err)
	}
	if writeStamp {
		// A stamp that certifies THIS publication. The awareness-graph and
		// services commits are deliberately different values: they are
		// different repositories, and a fixture that reuses one SHA for both
		// would pass while proving nothing about which field carries which.
		stamp := "format\tv1\n" +
			"seed\tdigest_sha256\t" + marker.Digest + "\n" +
			"seed\ttriple_count\t" + strconv.FormatInt(marker.TripleCount, 10) + "\n" +
			"repo\tawareness-graph\t58c055fbd9d65ad2f5a8c965f728897012e75f09\n" +
			"repo\tservices\tb98c91eb540a3e5aa9d97e7b3c08005e08cb1897\n"
		if err := os.WriteFile(seedmeta.RuntimeTransactionPath(markerPath), []byte(stamp), 0o644); err != nil {
			t.Fatalf("write transaction stamp: %v", err)
		}
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
	s.closureEval = func(string) (closure.SemanticState, string) {
		return closure.SemanticClosureProven, "closure report vouches for this publication"
	}
	return s
}

// Fresh, closed, and NOT certified by any transaction: the wire contract says
// that is not authoritative.
func TestAnUncertifiedPublicationIsNotAuthoritative(t *testing.T) {
	s := transactionAuthorityServer(t, false)

	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	// The fixture's premise: the other two conjuncts hold, so nothing else
	// explains a refusal.
	if resp.GetGraphFreshnessState() != awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT {
		t.Fatalf("fixture is not fresh: %s", resp.GetGraphFreshnessState())
	}
	a := resp.GetAuthority()
	if a == nil {
		t.Fatal("no authority verdict")
	}
	if a.GetEmbeddedTransactionMatchesSeed() {
		t.Fatal("fixture wrote no stamp and the evidence says one certifies the seed")
	}

	if a.GetVerdict() != awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE {
		t.Fatalf("verdict = %s; a publication no transaction certifies is not authoritative -- "+
			"the evidence field said so and the conclusion ignored it", a.GetVerdict())
	}
	if a.GetAuthoritative() {
		t.Fatal("authoritative = true with no transaction certification")
	}
}

// The same world WITH a stamp certifying this publication is authoritative.
// Without this, the test above would pass for a server that refuses everything.
func TestACertifiedPublicationIsAuthoritative(t *testing.T) {
	s := transactionAuthorityServer(t, true)

	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	a := resp.GetAuthority()
	if a == nil {
		t.Fatal("no authority verdict")
	}
	if !a.GetEmbeddedTransactionMatchesSeed() {
		t.Fatalf("the stamp does not certify the fixture's seed: %s", a.GetEmbeddedTransactionDetail())
	}
	if a.GetVerdict() != awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_AUTHORITATIVE {
		t.Fatalf("verdict = %s with all three conjuncts satisfied: %s",
			a.GetVerdict(), a.GetGraphFreshnessDetail())
	}
	// And the certification names the two repositories separately, so a reader
	// can tell which commit is which.
	if a.GetCertifiedAwarenessGraphCommit() == a.GetCertifiedServicesRepoCommit() {
		t.Fatal("the awareness-graph and services commits are the same value; " +
			"a fixture that cannot tell them apart proves nothing about which field carries which")
	}
}

// A stamp that certifies a DIFFERENT publication is not certification of this
// one. This is the case the conjunct exists for: a stamp is present, so a
// presence check would pass, and it vouches for other bytes.
func TestAStampForAnotherPublicationDoesNotCertifyThisOne(t *testing.T) {
	s := transactionAuthorityServer(t, false)
	stamp := "format\tv1\n" +
		"seed\tdigest_sha256\t0000000000000000000000000000000000000000000000000000000000000000\n" +
		"seed\ttriple_count\t1\n" +
		"repo\tawareness-graph\t58c055fbd9d65ad2f5a8c965f728897012e75f09\n" +
		"repo\tservices\tb98c91eb540a3e5aa9d97e7b3c08005e08cb1897\n"
	if err := os.WriteFile(seedmeta.RuntimeTransactionPath(s.graphMarkerFile), []byte(stamp), 0o644); err != nil {
		t.Fatalf("write transaction stamp: %v", err)
	}

	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	a := resp.GetAuthority()
	if a.GetVerdict() != awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE {
		t.Fatalf("a stamp certifying other bytes was accepted as certification: %s",
			a.GetEmbeddedTransactionDetail())
	}
}
