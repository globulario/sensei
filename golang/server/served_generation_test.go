// SPDX-License-Identifier: AGPL-3.0-only

package main

// LiveStoreGraphDigestSha256 is documented, in its own contract, as "the digest of
// the graph actually being served". It was not.
//
// Found by the Phase 7 disposable-store proof of the graph-identity front, which is
// what that proof exists for. A disposable store was loaded with a freshly built
// generation, a reader was pointed at it, and the canonical report said:
//
//	Marker verdict:      cannot be verified: the served graph states no digest
//	Generation verdict:  ... the served graph states no generation
//
// The store demonstrably held marker triples for digest b6657e8d. The cause is in
// seedmeta.VerifyLiveStore: it locates the live marker with Describe(expected.IRI) —
// it looks up ONLY the generation it expected. When the store serves a different one
// that describe returns nothing, the function exits early as STALE, and ver.Live is
// never populated. So the field is empty EXACTLY when the served graph is not the
// expected graph.
//
// That is the whole identity chain's evidence, blank in the only case it matters:
//
//	markerAgreement(liveDigest="")        -> "cannot be verified"
//	verifyActiveGeneration(declared, "")  -> unverifiable, never a mismatch
//	sensei-code's pinned-generation check -> falls into its "older server" branch
//
// The repair uses the owner that already exists. seedmeta.DiscoverLiveMarkers finds
// markers BY CLASS — its own comment says "never by looking up the expected IRI" —
// and AdmitLiveMarker returns an AuthorityObservation whose LiveIdentity is the
// independently discovered served digest. The control-state provider already reads it.
// Only the two response builders did not.
//
// Cost is respected: the discovery runs ONLY when the cheap lookup established no
// served identity. On a healthy store the expected marker is present, so nothing extra
// is asked.

import (
	"context"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store"
)

// servedGenerationStore serves ONE generation and is asked about another. It counts
// class-discovery calls so the cost rule can be asserted rather than assumed.
type servedGenerationStore struct {
	runtimeMarkerStore
	serves      []seedmeta.Marker // what the store actually holds
	total       int64
	classCalls  atomic.Int64
	describeFor string // the IRI Describe will answer for; "" answers for none
}

func (s *servedGenerationStore) Describe(_ context.Context, iri string) ([]store.Triple, error) {
	for _, m := range s.serves {
		if m.IRI == iri && iri == s.describeFor {
			return markerTriples(m), nil
		}
	}
	return nil, nil
}

func (s *servedGenerationStore) CountTriples(context.Context) (int64, error) { return s.total, nil }

func (s *servedGenerationStore) ClassFacts(_ context.Context, classIRI string, _ int) ([]store.ImpactFact, error) {
	if classIRI != seedmeta.NamespaceIRI+"SeedBuild" {
		return nil, nil
	}
	s.classCalls.Add(1)
	var out []store.ImpactFact
	for _, m := range s.serves {
		out = append(out,
			store.ImpactFact{NodeIRI: m.IRI, Predicate: seedmeta.NamespaceIRI + "seedDigestSha256", Object: m.Digest},
			store.ImpactFact{NodeIRI: m.IRI, Predicate: seedmeta.NamespaceIRI + "seedTripleCount", Object: strconv.FormatInt(m.TripleCount, 10)},
		)
	}
	return out, nil
}

func (s *servedGenerationStore) CountByClass(_ context.Context, classIRI string) (int64, error) {
	if classIRI == seedmeta.NamespaceIRI+"SeedBuild" {
		return int64(len(s.serves)), nil
	}
	return 0, nil
}

func markerTriples(m seedmeta.Marker) []store.Triple {
	return []store.Triple{
		{Predicate: seedmeta.NamespaceIRI + "seedDigestSha256", Object: m.Digest},
		{Predicate: seedmeta.NamespaceIRI + "seedTripleCount", Object: strconv.FormatInt(m.TripleCount, 10)},
	}
}

// generationFor builds a self-consistent marker whose IRI derives from its digest,
// which is what AdmitLiveMarker requires before it will admit an identity.
func generationFor(t *testing.T, content string) seedmeta.Marker {
	t.Helper()
	_, m := seedmeta.AppendMarker([]byte(content))
	return m
}

func expectMarkerFile(t *testing.T, m seedmeta.Marker) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "graph-authority.json")
	if err := seedmeta.WriteMarkerFile(path, m); err != nil {
		t.Fatal(err)
	}
	return path
}

// THE DEFECT. The store serves generation S while the marker file expects E.
func TestTheServedGenerationIsReportedWhenItIsNotTheExpectedOne(t *testing.T) {
	expected := generationFor(t, "<https://example.test/a> <https://example.test/p> <https://example.test/x> .\n")
	served := generationFor(t, "<https://example.test/b> <https://example.test/p> <https://example.test/y> .\n")
	if expected.Digest == served.Digest {
		t.Fatal("fixture error: the two generations must differ")
	}

	st := &servedGenerationStore{serves: []seedmeta.Marker{served}, total: served.TripleCount}
	s := newTestServer(st)
	s.graphMarkerFile = expectMarkerFile(t, expected)

	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if got := resp.GetLiveStoreGraphDigestSha256(); got != served.Digest {
		t.Errorf("live store digest = %q, want the generation actually served (%s)", got, served.Digest)
	}
	// It must never be the expected digest: that would report agreement with a graph
	// that is not there, which is worse than reporting nothing.
	if resp.GetLiveStoreGraphDigestSha256() == expected.Digest {
		t.Error("the expected digest was substituted for the served one")
	}
	// And the freshness verdict must still say STALE. Reporting what is served is not
	// the same as accepting it.
	if resp.GetGraphFreshnessState() == awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT {
		t.Error("a store serving another generation was reported CURRENT")
	}
}

// The cost rule: a healthy store is asked nothing extra.
func TestTheHealthyPathDoesNoExtraDiscovery(t *testing.T) {
	m := generationFor(t, "<https://example.test/a> <https://example.test/p> <https://example.test/x> .\n")
	st := &servedGenerationStore{serves: []seedmeta.Marker{m}, total: m.TripleCount, describeFor: m.IRI}
	s := newTestServer(st)
	s.graphMarkerFile = expectMarkerFile(t, m)

	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if got := resp.GetLiveStoreGraphDigestSha256(); got != m.Digest {
		t.Errorf("live store digest = %q, want %s", got, m.Digest)
	}
	if n := st.classCalls.Load(); n != 0 {
		t.Errorf("the healthy path performed %d marker discoveries; the cheap lookup already identified the served graph", n)
	}
}

// Law 12: two instances claiming the same domain are an error, not a choice. Two
// markers in one store is the same fault inside one instance, and the report must not
// pick one.
func TestTwoServedGenerationsReportNoIdentity(t *testing.T) {
	expected := generationFor(t, "<https://example.test/a> <https://example.test/p> <https://example.test/x> .\n")
	one := generationFor(t, "<https://example.test/b> <https://example.test/p> <https://example.test/y> .\n")
	two := generationFor(t, "<https://example.test/c> <https://example.test/p> <https://example.test/z> .\n")

	st := &servedGenerationStore{serves: []seedmeta.Marker{one, two}, total: one.TripleCount}
	s := newTestServer(st)
	s.graphMarkerFile = expectMarkerFile(t, expected)

	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if got := resp.GetLiveStoreGraphDigestSha256(); got != "" {
		t.Errorf("live store digest = %q; with two generations present no identity is established", got)
	}
}

// An empty store invents nothing.
func TestAnEmptyStoreReportsNoServedGeneration(t *testing.T) {
	expected := generationFor(t, "<https://example.test/a> <https://example.test/p> <https://example.test/x> .\n")
	st := &servedGenerationStore{total: 0}
	s := newTestServer(st)
	s.graphMarkerFile = expectMarkerFile(t, expected)

	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if got := resp.GetLiveStoreGraphDigestSha256(); got != "" {
		t.Errorf("an empty store reported a served generation: %q", got)
	}
}

// An integrity-failed store carries a digest in its observation, and it must NOT become
// a served identity. Its self-description is exactly what cannot be trusted: a marker
// whose subject IRI disagrees with its own digest is not "an older graph", it is a
// contradiction, and handing that digest downstream would make it comparable — the
// wrong graph passing an equality check is the failure this whole front exists to stop.
func TestAnIncoherentMarkerIsNotAServedIdentity(t *testing.T) {
	expected := generationFor(t, "<https://example.test/a> <https://example.test/p> <https://example.test/x> .\n")
	served := generationFor(t, "<https://example.test/b> <https://example.test/p> <https://example.test/y> .\n")
	// The digest literal is attached to a subject that is not its digest-derived IRI.
	incoherent := seedmeta.Marker{IRI: seedmeta.NamespaceIRI + "seedBuild/sha256-not-this-digest", Digest: served.Digest, TripleCount: served.TripleCount}

	st := &servedGenerationStore{serves: []seedmeta.Marker{incoherent}, total: served.TripleCount}
	s := newTestServer(st)
	s.graphMarkerFile = expectMarkerFile(t, expected)

	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if got := resp.GetLiveStoreGraphDigestSha256(); got != "" {
		t.Errorf("an incoherent marker was reported as the served identity: %q", got)
	}
}

// A store whose marker count contradicts the actual live graph count is the same class:
// self-inconsistent, so no identity.
func TestAMarkerCountThatContradictsTheStoreIsNotAnIdentity(t *testing.T) {
	expected := generationFor(t, "<https://example.test/a> <https://example.test/p> <https://example.test/x> .\n")
	served := generationFor(t, "<https://example.test/b> <https://example.test/p> <https://example.test/y> .\n")

	st := &servedGenerationStore{serves: []seedmeta.Marker{served}, total: served.TripleCount + 99}
	s := newTestServer(st)
	s.graphMarkerFile = expectMarkerFile(t, expected)

	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if got := resp.GetLiveStoreGraphDigestSha256(); got != "" {
		t.Errorf("a marker contradicting the live count was reported as the served identity: %q", got)
	}
}
