// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store"
)

type fakeDomainTripleStore struct {
	fakeStore
	domainTripleCount int64
}

func (f fakeDomainTripleStore) CountTriplesInDomain(_ context.Context, _, _ string) (int64, error) {
	return f.domainTripleCount, nil
}

func TestMetadata_DomainScopeUsesScopedTripleCount(t *testing.T) {
	st := fakeDomainTripleStore{
		fakeStore: fakeStore{
			countTriples: func(context.Context) (int64, error) {
				return 99, nil
			},
			classFacts: func(_ context.Context, classIRI string, _ int) ([]store.ImpactFact, error) {
				if classIRI != rdf.ClassInvariant {
					return nil, nil
				}
				return []store.ImpactFact{
					{NodeIRI: "n:repo", Predicate: rdf.PropRepo, Object: "github.com/globulario/sensei"},
					{NodeIRI: "n:other", Predicate: rdf.PropRepo, Object: "github.com/elsewhere/repo"},
					{NodeIRI: "n:shared", Predicate: rdf.PropDomain, Object: rdf.DomainShared},
				}, nil
			},
		},
		domainTripleCount: 7,
	}
	s := newServer(st)
	s.homeDomain = "github.com/globulario/sensei"

	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{
		Domain: "github.com/globulario/sensei",
	})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if resp.GetTripleCount() != 7 {
		t.Fatalf("triple_count=%d, want scoped count 7", resp.GetTripleCount())
	}
	if resp.GetInvariantCount() != 2 {
		t.Fatalf("invariant_count=%d, want scoped repo+shared count 2", resp.GetInvariantCount())
	}
}

func TestMetadata_DomainScopeDoesNotFallBackToGraphWideTripleCount(t *testing.T) {
	st := fakeStore{
		countTriples: func(context.Context) (int64, error) {
			return 99, nil
		},
	}
	s := newServer(st)
	s.homeDomain = "github.com/globulario/sensei"

	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{
		Domain: "github.com/globulario/sensei",
	})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if resp.GetTripleCount() != 0 {
		t.Fatalf("triple_count=%d, want 0 rather than graph-wide fallback", resp.GetTripleCount())
	}
}
