// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/globulario/sensei/golang/closure"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store"
)

// metadataAuthorityServer builds a server whose store is FRESH -- the live
// marker matches the expected one exactly -- with no closure report bound to
// the publication. That is precisely the state issue #176 reports:
//
//	awareness_metadata   freshness only                    -> "authoritative, CURRENT"
//	awareness_preflight  freshness + closure + transaction -> non-authoritative
func metadataAuthorityServer(t *testing.T) *server {
	t.Helper()
	_, marker := seedmeta.AppendMarker([]byte("<https://example.test/s> <https://example.test/p> <https://example.test/x> .\n"))
	markerPath := filepath.Join(t.TempDir(), "graph-authority.json")
	if err := seedmeta.WriteMarkerFile(markerPath, marker); err != nil {
		t.Fatalf("write marker file: %v", err)
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
	// Fresh, and NOT closed: the closure report does not vouch for this
	// publication. This is the exact split the issue reports -- the cheap
	// surface says CURRENT while the governing surface refuses.
	s.closureEval = func() (closure.SemanticState, string) {
		return closure.SemanticClosureUnproven,
			"closure report describes publication 9ab8ce5af578 but the live marker is c377aab38bb7"
	}
	return s
}

// TestMetadataCarriesTheComposedAuthorityVerdictNotFreshnessAlone is #176's
// monitoring blind spot: Metadata reported freshness and nothing else, so a
// health check built on the cheap surface read green for a graph that could
// not govern -- right up to the moment a governed run started and preflight
// refused.
func TestMetadataCarriesTheComposedAuthorityVerdictNotFreshnessAlone(t *testing.T) {
	s := metadataAuthorityServer(t)

	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}

	// The fixture's whole point: freshness alone says CURRENT.
	if resp.GetGraphFreshnessState() != awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT {
		t.Fatalf("fixture is not fresh: graph freshness = %s", resp.GetGraphFreshnessState())
	}

	authority := resp.GetAuthority()
	if authority == nil {
		t.Fatal("Metadata carries no authority verdict, so a reader sees freshness and nothing else")
	}
	if authority.GetAuthoritative() {
		t.Error("Metadata reports authoritative for a publication with no closure proof bound to it")
	}
	if authority.GetVerdict() != awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE {
		t.Errorf("verdict = %s, want NOT_AUTHORITATIVE", authority.GetVerdict())
	}
	// And it must say WHY, or an operator sees a bare negative and has
	// nowhere to go.
	if authority.GetGraphFreshnessDetail() == "" {
		t.Error("the non-authoritative verdict carries no detail")
	}
}

// The two surfaces must answer from the same evaluation. The issue's finding
// was not that metadata was wrong in isolation -- it was that the cheap
// surface and the governing surface DISAGREED, and monitoring reaches for the
// cheap one.
func TestMetadataAndPreflightCannotDisagreeAboutAuthority(t *testing.T) {
	s := metadataAuthorityServer(t)

	metadata, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	governing := s.graphAuthority(context.Background())

	if got, want := metadata.GetAuthority().GetVerdict(), governing.GetVerdict(); got != want {
		t.Errorf("metadata verdict = %s, governing surfaces = %s -- monitoring would read a different answer than a governed run gets", got, want)
	}
	if got, want := metadata.GetAuthority().GetAuthoritative(), governing.GetAuthoritative(); got != want {
		t.Errorf("metadata authoritative = %v, governing surfaces = %v", got, want)
	}
	if got, want := metadata.GetAuthority().GetGraphFreshnessState(), governing.GetGraphFreshnessState(); got != want {
		t.Errorf("metadata freshness = %s, governing surfaces = %s", got, want)
	}
}
