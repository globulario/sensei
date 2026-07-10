// SPDX-License-Identifier: Apache-2.0

package main

// Server skeleton + backend wire-up tests.
//
// Two layers:
//
//   1. The bufconn RPC test — proves the AwarenessGraphServer registers
//      cleanly and every RPC continues to return codes.Unimplemented
//      after the backend wiring landed. This is the regression guard
//      against "we added a field; oops we accidentally implemented a
//      handler too."
//
//   2. The validateBackend unit tests — drive the require-store policy
//      with a fake Store implementation. No TCP, no HTTP, no Oxigraph
//      required. Asserts on log output AND return value, since both
//      are part of the contract operators see.
//
// All tests run in the default `go test ./...` invocation. Live
// Oxigraph integration lives under store/oxigraph behind the
// `integration` build tag.

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store"
)

// Compile-time assertion that *server satisfies the generated
// AwarenessGraphServer interface. Fires the moment a regeneration
// drops or renames a method.
var _ awarenesspb.AwarenessGraphServer = (*server)(nil)

// nopStore is the smallest store.Store implementation that satisfies
// the interface. Used by the bufconn test, which doesn't exercise the
// store at all (every RPC is still Unimplemented). Returning nil from
// Health is the right behaviour for a "store assumed healthy" stub —
// the test isn't about backend behaviour.
type nopStore struct{}

func testEmbeddedSeedMarker() seedmeta.Marker {
	if marker, ok := normalizedEmbeddedSeedMarker(); ok {
		return marker
	}
	if marker, ok := seedmeta.ParseMarker(seedNT); ok {
		return marker
	}
	return seedmeta.Marker{}
}

func testEmbeddedTransactionStamp() seedmeta.TransactionStamp {
	return seedmeta.ParseTransactionStamp(seedTransactionStamp)
}

func (nopStore) Close() error                   { return nil }
func (nopStore) Health(_ context.Context) error { return nil }
func (nopStore) Describe(_ context.Context, _ string) ([]store.Triple, error) {
	return nil, nil
}
func (nopStore) CountTriples(_ context.Context) (int64, error) {
	if marker := testEmbeddedSeedMarker(); marker.IRI != "" {
		return marker.TripleCount, nil
	}
	return 1, nil
}
func (nopStore) CountByClass(_ context.Context, classIRI string) (int64, error) {
	if classIRI == seedmeta.NamespaceIRI+"SeedBuild" {
		return 1, nil
	}
	return 0, nil
}
func (nopStore) DescribeInbound(_ context.Context, _ string) ([]store.InboundTriple, error) {
	return nil, nil
}
func (nopStore) ImpactForFile(_ context.Context, _ string) ([]store.ImpactFact, error) {
	return nil, nil
}
func (nopStore) ClassFacts(_ context.Context, _ string, _ int) ([]store.ImpactFact, error) {
	return nil, nil
}
func (nopStore) CodeSymbolFacts(_ context.Context, _ string) ([]store.ImpactFact, error) {
	return nil, nil
}
func (nopStore) RenderingGroupsForFile(_ context.Context, _ string) ([]store.RenderingGroupInfo, error) {
	return nil, nil
}
func (nopStore) DetectFacts(_ context.Context) ([]store.ImpactFact, error) {
	return nil, nil
}
func (nopStore) GraphFreshness(_ context.Context) seedmeta.Verification {
	marker := testEmbeddedSeedMarker()
	return seedmeta.Verification{
		State:           seedmeta.FreshnessCurrent,
		Expected:        marker,
		Live:            marker,
		LiveTripleCount: marker.TripleCount,
		MarkerPresent:   true,
		SeedBuildCount:  1,
		Detail:          "test nop store treated as current",
	}
}

// failingStore returns the configured error from Health. Used to drive
// the -require-store policy through validateBackend.
type failingStore struct{ err error }

func (failingStore) Close() error                     { return nil }
func (s failingStore) Health(_ context.Context) error { return s.err }
func (failingStore) Describe(_ context.Context, _ string) ([]store.Triple, error) {
	return nil, nil
}
func (f failingStore) CountTriples(_ context.Context) (int64, error) { return 0, f.err }
func (f failingStore) CountByClass(_ context.Context, _ string) (int64, error) {
	return 0, f.err
}
func (failingStore) DescribeInbound(_ context.Context, _ string) ([]store.InboundTriple, error) {
	return nil, nil
}
func (failingStore) ImpactForFile(_ context.Context, _ string) ([]store.ImpactFact, error) {
	return nil, nil
}
func (failingStore) ClassFacts(_ context.Context, _ string, _ int) ([]store.ImpactFact, error) {
	return nil, nil
}
func (failingStore) CodeSymbolFacts(_ context.Context, _ string) ([]store.ImpactFact, error) {
	return nil, nil
}
func (failingStore) RenderingGroupsForFile(_ context.Context, _ string) ([]store.RenderingGroupInfo, error) {
	return nil, nil
}
func (failingStore) DetectFacts(_ context.Context) ([]store.ImpactFact, error) {
	return nil, nil
}
func (f failingStore) GraphFreshness(_ context.Context) seedmeta.Verification {
	marker := testEmbeddedSeedMarker()
	return seedmeta.Verification{
		State:    seedmeta.FreshnessCheckError,
		Expected: marker,
		Detail:   f.err.Error(),
	}
}

type fakeStore struct {
	describe        func(ctx context.Context, iri string) ([]store.Triple, error)
	describeInbound func(ctx context.Context, iri string) ([]store.InboundTriple, error)
	impactForFile   func(ctx context.Context, sourceFileIRI string) ([]store.ImpactFact, error)
	classFacts      func(ctx context.Context, classIRI string, limit int) ([]store.ImpactFact, error)
	codeSymbolFacts func(ctx context.Context, sourceFileIRI string) ([]store.ImpactFact, error)
	detectFacts     func(ctx context.Context) ([]store.ImpactFact, error)
	countTriples    func(ctx context.Context) (int64, error)
	countByClass    func(ctx context.Context, classIRI string) (int64, error)
	graphFreshness  func(ctx context.Context) seedmeta.Verification
}

func captureServerLogger(s *server, buf *bytes.Buffer) {
	s.logger = log.New(buf, "", 0)
}

func (fakeStore) Close() error                   { return nil }
func (fakeStore) Health(_ context.Context) error { return nil }
func (f fakeStore) Describe(ctx context.Context, iri string) ([]store.Triple, error) {
	if f.describe == nil {
		if marker := testEmbeddedSeedMarker(); marker.IRI != "" && iri == marker.IRI {
			return []store.Triple{
				{Predicate: seedmeta.NamespaceIRI + "seedDigestSha256", Object: marker.Digest},
				{Predicate: seedmeta.NamespaceIRI + "seedTripleCount", Object: strconv.FormatInt(marker.TripleCount, 10)},
			}, nil
		}
		return nil, nil
	}
	return f.describe(ctx, iri)
}
func (f fakeStore) CountTriples(ctx context.Context) (int64, error) {
	if f.countTriples != nil {
		return f.countTriples(ctx)
	}
	if marker := testEmbeddedSeedMarker(); marker.IRI != "" {
		return marker.TripleCount, nil
	}
	return 1, nil
}
func (f fakeStore) CountByClass(ctx context.Context, classIRI string) (int64, error) {
	if f.countByClass != nil {
		return f.countByClass(ctx, classIRI)
	}
	if classIRI == seedmeta.NamespaceIRI+"SeedBuild" {
		return 1, nil
	}
	return 0, nil
}
func (f fakeStore) DescribeInbound(ctx context.Context, iri string) ([]store.InboundTriple, error) {
	if f.describeInbound == nil {
		return nil, nil
	}
	return f.describeInbound(ctx, iri)
}
func (f fakeStore) ImpactForFile(ctx context.Context, sourceFileIRI string) ([]store.ImpactFact, error) {
	if f.impactForFile == nil {
		return nil, nil
	}
	return f.impactForFile(ctx, sourceFileIRI)
}
func (f fakeStore) ClassFacts(ctx context.Context, classIRI string, limit int) ([]store.ImpactFact, error) {
	if f.classFacts == nil {
		return nil, nil
	}
	return f.classFacts(ctx, classIRI, limit)
}
func (f fakeStore) CodeSymbolFacts(ctx context.Context, sourceFileIRI string) ([]store.ImpactFact, error) {
	if f.codeSymbolFacts == nil {
		return nil, nil
	}
	return f.codeSymbolFacts(ctx, sourceFileIRI)
}
func (fakeStore) RenderingGroupsForFile(_ context.Context, _ string) ([]store.RenderingGroupInfo, error) {
	return nil, nil
}
func (f fakeStore) DetectFacts(ctx context.Context) ([]store.ImpactFact, error) {
	if f.detectFacts == nil {
		return nil, nil
	}
	return f.detectFacts(ctx)
}
func (f fakeStore) GraphFreshness(ctx context.Context) seedmeta.Verification {
	if f.graphFreshness != nil {
		return f.graphFreshness(ctx)
	}
	marker := testEmbeddedSeedMarker()
	return seedmeta.Verification{
		State:           seedmeta.FreshnessCurrent,
		Expected:        marker,
		Live:            marker,
		LiveTripleCount: marker.TripleCount,
		MarkerPresent:   true,
		SeedBuildCount:  1,
		Detail:          "test fake store treated as current",
	}
}

const bufconnBufSize = 1 << 20

// startBufconnServer is unchanged from Phase 2 step 3 except that
// newServer now takes a Store. We pass nopStore — none of the
// unimplemented RPCs reach into the store, so a stub is sufficient.
func startBufconnServer(t *testing.T) (awarenesspb.AwarenessGraphClient, func()) {
	t.Helper()

	lis := bufconn.Listen(bufconnBufSize)
	s := grpc.NewServer()
	awarenesspb.RegisterAwarenessGraphServer(s, newServer(nopStore{}))

	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = s.Serve(lis)
	}()

	conn, err := grpc.NewClient(
		"passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		s.Stop()
		t.Fatalf("dial bufconn: %v", err)
	}

	cleanup := func() {
		_ = conn.Close()
		s.GracefulStop()
		_ = lis.Close()
		select {
		case <-serveDone:
		case <-time.After(2 * time.Second):
			t.Error("server goroutine did not exit within 2s of GracefulStop")
		}
	}

	return awarenesspb.NewAwarenessGraphClient(conn), cleanup
}

func TestResolve_RejectsEmptyID(t *testing.T) {
	client, cleanup := startBufconnServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := client.Resolve(ctx, &awarenesspb.ResolveRequest{Class: "invariant"})
	if err == nil {
		t.Fatal("expected InvalidArgument for empty id")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Resolve code=%s, want %s", status.Code(err), codes.InvalidArgument)
	}
}

func TestResolve_RejectsEmptyClass(t *testing.T) {
	s := newServer(fakeStore{})
	_, err := s.Resolve(context.Background(), &awarenesspb.ResolveRequest{Id: "x"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Resolve code=%s, want %s", status.Code(err), codes.InvalidArgument)
	}
}

func TestResolve_RejectsUnsupportedClass(t *testing.T) {
	s := newServer(fakeStore{})
	// etcd_key / systemd_unit are real ontology classes but not in the resolve
	// whitelist; the switch must reject anything outside it, never guess.
	_, err := s.Resolve(context.Background(), &awarenesspb.ResolveRequest{Id: "x", Class: "etcd_key"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Resolve code=%s, want %s", status.Code(err), codes.InvalidArgument)
	}
}

func TestResolve_UnavailableWhenStoreNil(t *testing.T) {
	s := newServer(nil)
	_, err := s.Resolve(context.Background(), &awarenesspb.ResolveRequest{Id: "x", Class: "invariant"})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Resolve code=%s, want %s", status.Code(err), codes.Unavailable)
	}
}

func TestResolve_NotFoundOnNoTriples(t *testing.T) {
	s := newServer(fakeStore{
		describe: func(_ context.Context, _ string) ([]store.Triple, error) {
			return nil, nil
		},
	})
	resp, err := s.Resolve(context.Background(), &awarenesspb.ResolveRequest{
		Id: "test.example.invariant", Class: "invariant",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resp.GetFound() {
		t.Fatalf("Resolve found=%v, want false", resp.GetFound())
	}
	assertCurrentAuthority(t, resp.GetAuthority())
}

func TestResolve_FoundMapsCoreFields(t *testing.T) {
	s := newServer(fakeStore{
		describe: func(_ context.Context, iri string) ([]store.Triple, error) {
			wantIRI := "https://globular.io/awareness#invariant/test.example.invariant"
			if iri != wantIRI {
				t.Fatalf("Describe iri=%q, want %q", iri, wantIRI)
			}
			return []store.Triple{
				{Predicate: "http://www.w3.org/2000/01/rdf-schema#label", Object: "Invariant Label"},
				{Predicate: "https://globular.io/awareness#severity", Object: "high"},
				{Predicate: "https://globular.io/awareness#status", Object: "active"},
				{Predicate: "http://www.w3.org/2000/01/rdf-schema#comment", Object: "description"},
				{Predicate: "https://globular.io/awareness#affects", ObjectIsIRI: true, Object: "https://globular.io/awareness#failureMode/test.example.failure"},
			}, nil
		},
	})
	resp, err := s.Resolve(context.Background(), &awarenesspb.ResolveRequest{
		Id: "test.example.invariant", Class: "invariant",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !resp.GetFound() {
		t.Fatal("Resolve found=false, want true")
	}
	n := resp.GetNode()
	if n.GetId() != "test.example.invariant" || n.GetClass() != "invariant" {
		t.Fatalf("node identity mismatch: id=%q class=%q", n.GetId(), n.GetClass())
	}
	if n.GetLabel() != "Invariant Label" || n.GetSeverity() != "high" || n.GetStatus() != "active" {
		t.Fatalf("label/severity/status mismatch: label=%q severity=%q status=%q", n.GetLabel(), n.GetSeverity(), n.GetStatus())
	}
	if n.GetDescription() != "description" {
		t.Fatalf("description=%q, want %q", n.GetDescription(), "description")
	}
	if len(n.GetRelatedIds()) != 1 || n.GetRelatedIds()[0] != "failure_mode:test.example.failure" {
		t.Fatalf("related_ids=%v, want [failure_mode:test.example.failure]", n.GetRelatedIds())
	}
	assertCurrentAuthority(t, resp.GetAuthority())
}

func TestResolve_RelatedIDsAreDedupedAndCapped(t *testing.T) {
	s := newServer(fakeStore{
		describe: func(_ context.Context, _ string) ([]store.Triple, error) {
			triples := make([]store.Triple, 0, maxResolveRelatedIDs+10)
			for i := 0; i < maxResolveRelatedIDs+10; i++ {
				triples = append(triples, store.Triple{
					Predicate:   rdf.PropAffects,
					ObjectIsIRI: true,
					Object:      "https://globular.io/awareness#failureMode/test.example.failure." + strconv.Itoa(i),
				})
			}
			triples = append(triples, store.Triple{
				Predicate:   rdf.PropAffects,
				ObjectIsIRI: true,
				Object:      "https://globular.io/awareness#failureMode/test.example.failure.0",
			})
			return triples, nil
		},
	})
	resp, err := s.Resolve(context.Background(), &awarenesspb.ResolveRequest{
		Id: "test.example.invariant", Class: "invariant",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := len(resp.GetNode().GetRelatedIds()); got != maxResolveRelatedIDs {
		t.Fatalf("related_ids len=%d, want %d", got, maxResolveRelatedIDs)
	}
}

func TestResolve_LogsUsageShape(t *testing.T) {
	s := newServer(fakeStore{
		describe: func(_ context.Context, _ string) ([]store.Triple, error) {
			return []store.Triple{
				{Predicate: rdf.PropAffects, ObjectIsIRI: true, Object: "https://globular.io/awareness#failureMode/test.example.failure"},
			}, nil
		},
	})
	var buf bytes.Buffer
	captureServerLogger(s, &buf)
	_, err := s.Resolve(context.Background(), &awarenesspb.ResolveRequest{
		Id: "test.example.invariant", Class: "invariant",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	got := buf.String()
	for _, want := range []string{"resolve_usage", "class=invariant", "found=true", "related=1", "found_calls=1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("resolve log missing %q:\n%s", want, got)
		}
	}
}

func TestResolve_ClassMappingExamples(t *testing.T) {
	cases := []struct {
		class string
		id    string
		iri   string
	}{
		{class: "invariant", id: "test.example.invariant", iri: "https://globular.io/awareness#invariant/test.example.invariant"},
		{class: "failure_mode", id: "test.example.failure", iri: "https://globular.io/awareness#failureMode/test.example.failure"},
		{class: "incident_pattern", id: "test.example.pattern", iri: "https://globular.io/awareness#incidentPattern/test.example.pattern"},
		{class: "symbol", id: "TestExampleFunc", iri: "https://globular.io/awareness#symbol/TestExampleFunc"},
		{class: "source_file", id: "test/example.go", iri: "https://globular.io/awareness#sourceFile/test%2Fexample.go"},
		{class: "intent", id: "cluster.convergence", iri: "https://globular.io/awareness#intent/cluster.convergence"},
		{class: "forbidden_fix", id: "use_primaryip_for_etcd_endpoint", iri: "https://globular.io/awareness#forbiddenFix/use_primaryip_for_etcd_endpoint"},
		{class: "test", id: "TestExampleVerifiesInvariant", iri: "https://globular.io/awareness#test/TestExampleVerifiesInvariant"},
	}
	for _, tc := range cases {
		got, canonical, err := resolveIRIForClassAndID(tc.class, tc.id)
		if err != nil {
			t.Fatalf("resolveIRIForClassAndID(%q,%q): %v", tc.class, tc.id, err)
		}
		if got != tc.iri {
			t.Fatalf("iri=%q, want %q", got, tc.iri)
		}
		if canonical != tc.class {
			t.Fatalf("canonical class=%q, want %q", canonical, tc.class)
		}
	}
}

func TestImpact_RejectsEmptyFile(t *testing.T) {
	s := newServer(fakeStore{})
	_, err := s.Impact(context.Background(), &awarenesspb.ImpactRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Impact code=%s, want %s", status.Code(err), codes.InvalidArgument)
	}
}

func TestImpact_UnavailableWhenStoreNil(t *testing.T) {
	s := newServer(nil)
	_, err := s.Impact(context.Background(), &awarenesspb.ImpactRequest{File: "test/example.go"})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Impact code=%s, want %s", status.Code(err), codes.Unavailable)
	}
}

func TestImpact_UnavailableWhenStoreErrors(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return nil, errors.New("backend down")
		},
	})
	_, err := s.Impact(context.Background(), &awarenesspb.ImpactRequest{File: "test/example.go"})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Impact code=%s, want %s", status.Code(err), codes.Unavailable)
	}
}

func TestImpact_NoLinkedNodes_ReturnsEmptyDirectLists(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, sourceFileIRI string) ([]store.ImpactFact, error) {
			if sourceFileIRI != "https://globular.io/awareness#sourceFile/test%2Fexample.go" {
				t.Fatalf("sourceFileIRI=%q unexpected", sourceFileIRI)
			}
			return nil, nil
		},
	})
	resp, err := s.Impact(context.Background(), &awarenesspb.ImpactRequest{File: "test/example.go"})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if len(resp.GetDirectInvariants()) != 0 || len(resp.GetDirectFailureModes()) != 0 || len(resp.GetDirectIncidentPatterns()) != 0 {
		t.Fatalf("expected empty direct lists, got inv=%d fm=%d pat=%d", len(resp.GetDirectInvariants()), len(resp.GetDirectFailureModes()), len(resp.GetDirectIncidentPatterns()))
	}
	assertCurrentAuthority(t, resp.GetAuthority())
}

func TestImpact_ReturnsCompleteDirectSets(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			var facts []store.ImpactFact
			for i := 0; i < maxSurfaceNodesPerClass+5; i++ {
				id := "test.example.invariant." + strconv.Itoa(i)
				facts = append(facts, store.ImpactFact{
					NodeIRI:     "https://globular.io/awareness#invariant/" + id,
					TypeIRI:     rdf.ClassInvariant,
					Predicate:   rdf.PropLabel,
					Object:      id,
					ObjectIsIRI: false,
				})
			}
			return facts, nil
		},
	})
	resp, err := s.Impact(context.Background(), &awarenesspb.ImpactRequest{File: "test/example.go"})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if got, want := len(resp.GetDirectInvariants()), maxSurfaceNodesPerClass+5; got != want {
		t.Fatalf("direct_invariants len=%d, want %d (Impact must remain complete)", got, want)
	}
	assertCurrentAuthority(t, resp.GetAuthority())
}

func TestImpact_ReturnsLargeResponseBucketsUncapped(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			var facts []store.ImpactFact
			for i := 0; i < maxSurfaceNodesPerClass+5; i++ {
				id := "test.example.invariant." + strconv.Itoa(i)
				facts = append(facts,
					store.ImpactFact{NodeIRI: "https://globular.io/awareness#invariant/" + id, TypeIRI: rdf.ClassInvariant, Predicate: rdf.PropSeverity, Object: "warning"},
					store.ImpactFact{NodeIRI: "https://globular.io/awareness#invariant/" + id, TypeIRI: rdf.ClassInvariant, Predicate: rdf.PropLabel, Object: id},
				)
			}
			for i := 0; i < maxSurfaceArchitectureNodes+5; i++ {
				id := "contract.example." + strconv.Itoa(i)
				facts = append(facts,
					store.ImpactFact{NodeIRI: "https://globular.io/awareness#contract/" + id, TypeIRI: rdf.ClassContract, Predicate: rdf.PropLabel, Object: id},
				)
			}
			return facts, nil
		},
	})
	resp, err := s.Impact(context.Background(), &awarenesspb.ImpactRequest{File: "test/example.go"})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if got := len(resp.GetDirectInvariants()); got != maxSurfaceNodesPerClass+5 {
		t.Fatalf("direct_invariants len=%d, want %d", got, maxSurfaceNodesPerClass+5)
	}
	if got := len(resp.GetDirectArchitecture()); got != maxSurfaceArchitectureNodes+5 {
		t.Fatalf("direct_architecture len=%d, want %d", got, maxSurfaceArchitectureNodes+5)
	}
}

func TestImpact_FakeInvariant_GoesToDirectInvariants(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{
				{NodeIRI: "https://globular.io/awareness#invariant/test.example.invariant", TypeIRI: "https://globular.io/awareness#Invariant", Predicate: "http://www.w3.org/2000/01/rdf-schema#label", Object: "Invariant Label"},
				{NodeIRI: "https://globular.io/awareness#invariant/test.example.invariant", TypeIRI: "https://globular.io/awareness#Invariant", Predicate: "https://globular.io/awareness#severity", Object: "critical"},
				{NodeIRI: "https://globular.io/awareness#invariant/test.example.invariant", TypeIRI: "https://globular.io/awareness#Invariant", Predicate: "https://globular.io/awareness#status", Object: "active"},
				{NodeIRI: "https://globular.io/awareness#invariant/test.example.invariant", TypeIRI: "https://globular.io/awareness#Invariant", Predicate: "http://www.w3.org/2000/01/rdf-schema#comment", Object: "desc"},
			}, nil
		},
	})
	resp, err := s.Impact(context.Background(), &awarenesspb.ImpactRequest{File: "test/example.go"})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if len(resp.GetDirectInvariants()) != 1 {
		t.Fatalf("direct_invariants=%d, want 1", len(resp.GetDirectInvariants()))
	}
	n := resp.GetDirectInvariants()[0]
	if n.GetClass() != "invariant" || n.GetId() != "test.example.invariant" || n.GetLabel() != "Invariant Label" || n.GetSeverity() != "critical" || n.GetStatus() != "active" || n.GetDescription() != "desc" {
		t.Fatalf("unexpected node: %+v", n)
	}
}

func TestImpact_FakeFailureMode_GoesToDirectFailureModes(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{
				{NodeIRI: "https://globular.io/awareness#failureMode/test.example.failure", TypeIRI: "https://globular.io/awareness#FailureMode"},
			}, nil
		},
	})
	resp, err := s.Impact(context.Background(), &awarenesspb.ImpactRequest{File: "test/example.go"})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if len(resp.GetDirectFailureModes()) != 1 {
		t.Fatalf("direct_failure_modes=%d, want 1", len(resp.GetDirectFailureModes()))
	}
}

func TestImpact_FakeIncidentPattern_GoesToDirectIncidentPatterns(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{
				{NodeIRI: "https://globular.io/awareness#incidentPattern/test.example.pattern", TypeIRI: "https://globular.io/awareness#IncidentPattern"},
			}, nil
		},
	})
	resp, err := s.Impact(context.Background(), &awarenesspb.ImpactRequest{File: "test/example.go"})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if len(resp.GetDirectIncidentPatterns()) != 1 {
		t.Fatalf("direct_incident_patterns=%d, want 1", len(resp.GetDirectIncidentPatterns()))
	}
}

// TestImpact_InferredFieldsEmptyInV0 guards against accidental population of
// inferred fields before the four-layer inference implementation is complete.
// See docs/awareness/decisions/inference-v0-direct-anchors-only.md.
func TestImpact_InferredFieldsEmptyInV0(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{
				{NodeIRI: "https://globular.io/awareness#invariant/test.example.invariant", TypeIRI: "https://globular.io/awareness#Invariant"},
			}, nil
		},
	})
	resp, err := s.Impact(context.Background(), &awarenesspb.ImpactRequest{File: "test/example.go"})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if len(resp.GetDirectInvariants()) != 1 {
		t.Fatalf("direct_invariants=%d, want 1", len(resp.GetDirectInvariants()))
	}
	if n := len(resp.GetInferredInvariants()); n != 0 {
		t.Fatalf("inferred_invariants=%d, want 0 (reserved in v0)", n)
	}
	if n := len(resp.GetInferredFailureModes()); n != 0 {
		t.Fatalf("inferred_failure_modes=%d, want 0 (reserved in v0)", n)
	}
	if n := len(resp.GetInferredIncidentPatterns()); n != 0 {
		t.Fatalf("inferred_incident_patterns=%d, want 0 (reserved in v0)", n)
	}
	if n := len(resp.GetInferredIntents()); n != 0 {
		t.Fatalf("inferred_intents=%d, want 0 (reserved in v0)", n)
	}
}

func TestBriefing_RejectsEmptyFile(t *testing.T) {
	s := newServer(fakeStore{})
	_, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Briefing code=%s, want %s", status.Code(err), codes.InvalidArgument)
	}
}

func TestBriefing_UnavailableWhenStoreNil(t *testing.T) {
	s := newServer(nil)
	_, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "test/example.go"})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Briefing code=%s, want %s", status.Code(err), codes.Unavailable)
	}
}

func TestBriefing_UnavailableWhenStoreErrors(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return nil, errors.New("backend down")
		},
	})
	_, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "test/example.go"})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Briefing code=%s, want %s", status.Code(err), codes.Unavailable)
	}
}

func TestBriefing_EmptyStatusWhenNoNodes(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return nil, nil
		},
	})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "test/example.go"})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if resp.GetStatus() != awarenesspb.BriefingStatus_BRIEFING_STATUS_EMPTY {
		t.Fatalf("status=%v, want EMPTY", resp.GetStatus())
	}
	if !strings.Contains(resp.GetProse(), "No direct awareness anchors found") {
		t.Fatalf("unexpected prose for empty briefing: %q", resp.GetProse())
	}
}

func TestBriefing_OKWithReferencedIDsAndTask(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{
				{NodeIRI: "https://globular.io/awareness#invariant/test.example.invariant", TypeIRI: "https://globular.io/awareness#Invariant", Predicate: "http://www.w3.org/2000/01/rdf-schema#label", Object: "Test fixture invariant"},
				{NodeIRI: "https://globular.io/awareness#invariant/test.example.invariant", TypeIRI: "https://globular.io/awareness#Invariant", Predicate: "https://globular.io/awareness#severity", Object: "high"},
			}, nil
		},
	})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		File: "test/example.go",
		Task: "touch only parser",
	})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if resp.GetStatus() != awarenesspb.BriefingStatus_BRIEFING_STATUS_OK {
		t.Fatalf("status=%v, want OK", resp.GetStatus())
	}
	if len(resp.GetReferencedIds()) != 1 || resp.GetReferencedIds()[0] != "invariant:test.example.invariant" {
		t.Fatalf("referenced_ids=%v, want [invariant:test.example.invariant]", resp.GetReferencedIds())
	}
	if !strings.Contains(resp.GetProse(), "Task: touch only parser") {
		t.Fatalf("task not included in prose: %q", resp.GetProse())
	}
	if !strings.Contains(resp.GetProse(), "[high] test.example.invariant") {
		t.Fatalf("expected invariant bullet not found: %q", resp.GetProse())
	}
	if resp.GetGeneratedInMs() < 0 {
		t.Fatalf("generated_in_ms=%d, want >= 0", resp.GetGeneratedInMs())
	}
}

func TestBriefing_DecisionFocusComesBeforeDetailSections(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{
				{NodeIRI: "https://globular.io/awareness#invariant/test.example.invariant", TypeIRI: rdf.ClassInvariant, Predicate: rdf.PropLabel, Object: "Do not blur state layers"},
				{NodeIRI: "https://globular.io/awareness#invariant/test.example.invariant", TypeIRI: rdf.ClassInvariant, Predicate: rdf.PropSeverity, Object: "critical"},
				{NodeIRI: "https://globular.io/awareness#failureMode/test.example.failure", TypeIRI: rdf.ClassFailureMode, Predicate: rdf.PropLabel, Object: "Layer collapse"},
				{NodeIRI: "https://globular.io/awareness#failureMode/test.example.failure", TypeIRI: rdf.ClassFailureMode, Predicate: rdf.PropSeverity, Object: "high"},
				{NodeIRI: "https://globular.io/awareness#test/test.example.test", TypeIRI: rdf.ClassTest, Predicate: rdf.PropLabel, Object: "TestLayerSeparation"},
				{NodeIRI: "https://globular.io/awareness#intent/test.example.intent", TypeIRI: rdf.ClassIntent, Predicate: rdf.PropLabel, Object: "Keep layer ownership explicit"},
			}, nil
		},
	})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "test/example.go"})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	prose := resp.GetProse()
	focusAt := strings.Index(prose, "Decision focus:")
	detailAt := strings.Index(prose, "Direct invariants:")
	if focusAt < 0 || detailAt < 0 || focusAt > detailAt {
		t.Fatalf("decision focus should precede detail sections:\n%s", prose)
	}
	for _, want := range []string{
		"Respect: [critical] test.example.invariant",
		"Watch for: [high] test.example.failure",
		"Verify with: test.example.test",
		"Keep intent: test.example.intent",
	} {
		if !strings.Contains(prose, want) {
			t.Fatalf("decision focus missing %q:\n%s", want, prose)
		}
	}
}

func TestBriefing_DeterministicAcrossRepeatedCalls(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{
				{NodeIRI: "https://globular.io/awareness#failureMode/test.example.failure", TypeIRI: "https://globular.io/awareness#FailureMode", Predicate: "http://www.w3.org/2000/01/rdf-schema#label", Object: "Failure"},
				{NodeIRI: "https://globular.io/awareness#invariant/test.example.invariant", TypeIRI: "https://globular.io/awareness#Invariant", Predicate: "http://www.w3.org/2000/01/rdf-schema#label", Object: "Invariant"},
				{NodeIRI: "https://globular.io/awareness#invariant/test.example.invariant", TypeIRI: "https://globular.io/awareness#Invariant", Predicate: "https://globular.io/awareness#severity", Object: "critical"},
			}, nil
		},
	})
	a, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "test/example.go"})
	if err != nil {
		t.Fatalf("Briefing first call: %v", err)
	}
	b, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "test/example.go"})
	if err != nil {
		t.Fatalf("Briefing second call: %v", err)
	}
	if a.GetProse() != b.GetProse() {
		t.Fatalf("prose differs across repeated calls:\nA:\n%s\nB:\n%s", a.GetProse(), b.GetProse())
	}
	if strings.Join(a.GetReferencedIds(), ",") != strings.Join(b.GetReferencedIds(), ",") {
		t.Fatalf("referenced_ids differ across repeated calls: %v vs %v", a.GetReferencedIds(), b.GetReferencedIds())
	}
}

func TestBriefing_DirectFailureModeOnly(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{
				{NodeIRI: "https://globular.io/awareness#failureMode/test.example.failure", TypeIRI: "https://globular.io/awareness#FailureMode", Predicate: "https://globular.io/awareness#severity", Object: "medium"},
			}, nil
		},
	})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "test/example.go"})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if resp.GetStatus() != awarenesspb.BriefingStatus_BRIEFING_STATUS_OK {
		t.Fatalf("status=%v, want OK", resp.GetStatus())
	}
	if !strings.Contains(resp.GetProse(), "Direct failure modes:") {
		t.Fatalf("missing failure section in prose: %q", resp.GetProse())
	}
	if len(resp.GetReferencedIds()) != 1 || resp.GetReferencedIds()[0] != "failure_mode:test.example.failure" {
		t.Fatalf("referenced_ids=%v, want [failure_mode:test.example.failure]", resp.GetReferencedIds())
	}
}

func TestBriefing_DirectIncidentPatternOnly(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{
				{NodeIRI: "https://globular.io/awareness#incidentPattern/test.example.pattern", TypeIRI: "https://globular.io/awareness#IncidentPattern"},
			}, nil
		},
	})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "test/example.go"})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if !strings.Contains(resp.GetProse(), "Direct incident patterns:") {
		t.Fatalf("missing pattern section in prose: %q", resp.GetProse())
	}
	if len(resp.GetReferencedIds()) != 1 || resp.GetReferencedIds()[0] != "incident_pattern:test.example.pattern" {
		t.Fatalf("referenced_ids=%v, want [incident_pattern:test.example.pattern]", resp.GetReferencedIds())
	}
}

func TestBriefing_DirectIntentOnly(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{
				{NodeIRI: "https://globular.io/awareness#intent/test.example.intent", TypeIRI: "https://globular.io/awareness#Intent"},
			}, nil
		},
	})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "test/example.go"})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if !strings.Contains(resp.GetProse(), "Direct intents:") {
		t.Fatalf("missing intent section in prose: %q", resp.GetProse())
	}
	if len(resp.GetReferencedIds()) != 1 || resp.GetReferencedIds()[0] != "intent:test.example.intent" {
		t.Fatalf("referenced_ids=%v, want [intent:test.example.intent]", resp.GetReferencedIds())
	}
}

func TestBriefing_MultipleClasses_ReferencedIDFormat(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{
				{NodeIRI: "https://globular.io/awareness#invariant/a.inv", TypeIRI: "https://globular.io/awareness#Invariant"},
				{NodeIRI: "https://globular.io/awareness#failureMode/b.fail", TypeIRI: "https://globular.io/awareness#FailureMode"},
				{NodeIRI: "https://globular.io/awareness#incidentPattern/c.pat", TypeIRI: "https://globular.io/awareness#IncidentPattern"},
				{NodeIRI: "https://globular.io/awareness#intent/d.intent", TypeIRI: "https://globular.io/awareness#Intent"},
			}, nil
		},
	})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "test/example.go"})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	want := []string{
		"invariant:a.inv",
		"failure_mode:b.fail",
		"incident_pattern:c.pat",
		"intent:d.intent",
	}
	if strings.Join(resp.GetReferencedIds(), ",") != strings.Join(want, ",") {
		t.Fatalf("referenced_ids=%v, want %v", resp.GetReferencedIds(), want)
	}
}

func TestBriefing_ReferencedIDsAreDedupedAndCapped(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			var facts []store.ImpactFact
			for i := 0; i < maxSurfaceNodesPerClass+5; i++ {
				id := "test.example.invariant." + strconv.Itoa(i)
				facts = append(facts,
					store.ImpactFact{NodeIRI: "https://globular.io/awareness#invariant/" + id, TypeIRI: rdf.ClassInvariant, Predicate: rdf.PropLabel, Object: id},
				)
			}
			for i := 0; i < maxSurfaceNodesPerClass+5; i++ {
				id := "test.example.failure." + strconv.Itoa(i)
				facts = append(facts,
					store.ImpactFact{NodeIRI: "https://globular.io/awareness#failureMode/" + id, TypeIRI: rdf.ClassFailureMode, Predicate: rdf.PropLabel, Object: id},
				)
			}
			for i := 0; i < maxSurfaceArchitectureNodes+5; i++ {
				id := "test.example.contract." + strconv.Itoa(i)
				facts = append(facts,
					store.ImpactFact{NodeIRI: "https://globular.io/awareness#contract/" + id, TypeIRI: rdf.ClassContract, Predicate: rdf.PropLabel, Object: id},
				)
			}
			facts = append(facts,
				store.ImpactFact{NodeIRI: "https://globular.io/awareness#invariant/test.example.invariant.0", TypeIRI: rdf.ClassInvariant, Predicate: rdf.PropSeverity, Object: "high"},
			)
			return facts, nil
		},
	})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "test/example.go"})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if got := len(resp.GetReferencedIds()); got != maxBriefingReferencedIDs {
		t.Fatalf("referenced_ids len=%d, want %d", got, maxBriefingReferencedIDs)
	}
	if resp.GetReferencedIds()[0] != "invariant:test.example.invariant.0" {
		t.Fatalf("first referenced id=%q, want invariant:test.example.invariant.0", resp.GetReferencedIds()[0])
	}
}

func TestBriefing_AgentCompactUsesTighterCaps(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			var facts []store.ImpactFact
			for i := 0; i < 10; i++ {
				id := "test.example.invariant." + strconv.Itoa(i)
				facts = append(facts,
					store.ImpactFact{NodeIRI: "https://globular.io/awareness#invariant/" + id, TypeIRI: rdf.ClassInvariant, Predicate: rdf.PropLabel, Object: id},
				)
			}
			return facts, nil
		},
	})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		File:  "test/example.go",
		Depth: "agent_compact",
	})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if got := len(resp.GetReferencedIds()); got > agentCompactBriefingProfile.impactNodes {
		t.Fatalf("agent_compact referenced_ids len=%d, want <= %d", got, agentCompactBriefingProfile.impactNodes)
	}
	if strings.Count(resp.GetProse(), "\n  - [") > agentCompactBriefingProfile.impactNodes {
		t.Fatalf("agent_compact prose surfaced too many invariant bullets:\n%s", resp.GetProse())
	}
}

func TestBriefing_DepthProfilesExpandSurface(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			var facts []store.ImpactFact
			for i := 0; i < 40; i++ {
				id := "test.example.invariant." + strconv.Itoa(i)
				facts = append(facts, store.ImpactFact{
					NodeIRI:   "https://globular.io/awareness#invariant/" + id,
					TypeIRI:   rdf.ClassInvariant,
					Predicate: rdf.PropLabel,
					Object:    id,
				})
			}
			return facts, nil
		},
	})
	build := func(depth string) *awarenesspb.BriefingResponse {
		t.Helper()
		resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
			File:  "test/example.go",
			Depth: depth,
		})
		if err != nil {
			t.Fatalf("Briefing(%s): %v", depth, err)
		}
		return resp
	}
	agentCompact := build("agent_compact")
	compact := build("compact")
	standard := build("standard")
	deep := build("deep")
	if len(agentCompact.GetReferencedIds()) >= len(compact.GetReferencedIds()) {
		t.Fatalf("agent_compact should be tighter than compact: %d >= %d", len(agentCompact.GetReferencedIds()), len(compact.GetReferencedIds()))
	}
	if len(compact.GetReferencedIds()) >= len(standard.GetReferencedIds()) {
		t.Fatalf("compact should be tighter than standard: %d >= %d", len(compact.GetReferencedIds()), len(standard.GetReferencedIds()))
	}
	if len(standard.GetReferencedIds()) >= len(deep.GetReferencedIds()) {
		t.Fatalf("standard should be tighter than deep: %d >= %d", len(standard.GetReferencedIds()), len(deep.GetReferencedIds()))
	}
}

func TestBriefing_EmptyDepthNormalizesToStandard(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			var facts []store.ImpactFact
			for i := 0; i < maxBriefingReferencedIDs+5; i++ {
				id := "test.example.invariant." + strconv.Itoa(i)
				facts = append(facts, store.ImpactFact{
					NodeIRI:   "https://globular.io/awareness#invariant/" + id,
					TypeIRI:   rdf.ClassInvariant,
					Predicate: rdf.PropLabel,
					Object:    id,
				})
			}
			return facts, nil
		},
	})
	var buf bytes.Buffer
	captureServerLogger(s, &buf)
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "test/example.go"})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	standardResp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		File:  "test/example.go",
		Depth: "standard",
	})
	if err != nil {
		t.Fatalf("Briefing(standard): %v", err)
	}
	if got, want := len(resp.GetReferencedIds()), len(standardResp.GetReferencedIds()); got != want {
		t.Fatalf("empty depth referenced_ids len=%d, want standard len %d", got, want)
	}
	if got := buf.String(); !strings.Contains(got, "depth=standard") {
		t.Fatalf("briefing log should normalize empty depth to standard, got:\n%s", got)
	}
}

func TestBriefing_LogsUsageShape(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{
				{NodeIRI: "https://globular.io/awareness#invariant/test.example.invariant", TypeIRI: rdf.ClassInvariant, Predicate: rdf.PropLabel, Object: "test.example.invariant"},
			}, nil
		},
	})
	var buf bytes.Buffer
	captureServerLogger(s, &buf)
	_, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		File:  "test/example.go",
		Depth: "agent_compact",
	})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	got := buf.String()
	for _, want := range []string{"briefing_usage", "depth=agent_compact", "refs=1", "agent_compact_calls=1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("briefing log missing %q:\n%s", want, got)
		}
	}
}

func TestMetadata_IncludesSurfaceUsageCounters(t *testing.T) {
	s := newServer(nopStore{})
	_, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		Task:  "write grpc client",
		Depth: "agent_compact",
	})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	_, err = s.Resolve(context.Background(), &awarenesspb.ResolveRequest{
		Id: "x", Class: "invariant",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if resp.GetBriefingCallCount() != 1 || resp.GetBriefingAgentCompactCount() != 1 {
		t.Fatalf("briefing counters=(%d,%d), want (1,1)", resp.GetBriefingCallCount(), resp.GetBriefingAgentCompactCount())
	}
	if resp.GetResolveCallCount() != 1 || resp.GetResolveFoundCount() != 0 || resp.GetResolveMissCount() != 1 {
		t.Fatalf("resolve counters=(%d,%d,%d), want (1,0,1)", resp.GetResolveCallCount(), resp.GetResolveFoundCount(), resp.GetResolveMissCount())
	}
}

func TestMetadata_ExposesEmbeddedSeedMarkerState(t *testing.T) {
	s := newServer(fakeStore{
		describe: func(_ context.Context, iri string) ([]store.Triple, error) {
			if !strings.Contains(iri, "seedBuild/sha256-") {
				t.Fatalf("Describe iri=%q does not look like embedded seed marker", iri)
			}
			return []store.Triple{{Predicate: rdf.PropLabel, Object: "present"}}, nil
		},
	})
	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if resp.GetEmbeddedSeedDigestSha256() == "" {
		t.Fatal("embedded seed digest should be populated")
	}
	if resp.GetEmbeddedSeedMarkerIri() == "" {
		t.Fatal("embedded seed marker IRI should be populated")
	}
	if !resp.GetLiveStoreContainsEmbeddedSeedMarker() {
		t.Fatal("live store marker presence should be true")
	}
	if resp.GetSeedState() != awarenesspb.SeedState_SEED_STATE_CURRENT {
		t.Fatalf("seed state=%s, want CURRENT", resp.GetSeedState())
	}
	if resp.GetGraphFreshnessState() != awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT {
		t.Fatalf("graph freshness=%s, want CURRENT", resp.GetGraphFreshnessState())
	}
	if resp.GetLiveStoreGraphDigestSha256() != resp.GetEmbeddedSeedDigestSha256() {
		t.Fatalf("live digest=%q, want embedded digest %q", resp.GetLiveStoreGraphDigestSha256(), resp.GetEmbeddedSeedDigestSha256())
	}
	if resp.GetLiveStoreGraphTripleCount() == 0 {
		t.Fatal("live store graph triple count should be populated")
	}
	if resp.GetBuildProvenanceState() != awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_DEV {
		t.Fatalf("build provenance state=%s, want DEV for unstamped test binary", resp.GetBuildProvenanceState())
	}
	stamp := testEmbeddedTransactionStamp()
	if !resp.GetEmbeddedTransactionStampPresent() {
		t.Fatal("embedded transaction stamp should be present")
	}
	if resp.GetCertifiedAwarenessGraphCommit() != stamp.AwarenessGraphCommit {
		t.Fatalf("awareness-graph commit=%q, want %q", resp.GetCertifiedAwarenessGraphCommit(), stamp.AwarenessGraphCommit)
	}
	if resp.GetCertifiedServicesRepoCommit() != stamp.ServicesCommit {
		t.Fatalf("services commit=%q, want %q", resp.GetCertifiedServicesRepoCommit(), stamp.ServicesCommit)
	}
	if !resp.GetEmbeddedTransactionMatchesSeed() {
		t.Fatalf("embedded transaction should match seed, detail=%q", resp.GetEmbeddedTransactionDetail())
	}
	if resp.GetEmbeddedTransactionDetail() == "" {
		t.Fatal("embedded transaction detail should be populated")
	}
}

func TestGraphAuthorityCarriesEmbeddedTransactionCertification(t *testing.T) {
	marker := testEmbeddedSeedMarker()
	stamp := testEmbeddedTransactionStamp()
	authority := graphAuthorityFromSnapshot(graphFreshnessSnapshot{
		verification: seedmeta.Verification{
			State:           seedmeta.FreshnessCurrent,
			Expected:        marker,
			Live:            marker,
			LiveTripleCount: marker.TripleCount,
			MarkerPresent:   true,
			Detail:          "current",
		},
	}, nil)
	if !authority.GetEmbeddedTransactionStampPresent() {
		t.Fatal("graph authority should carry transaction stamp presence")
	}
	if authority.GetCertifiedAwarenessGraphCommit() != stamp.AwarenessGraphCommit {
		t.Fatalf("graph authority awareness commit=%q, want %q", authority.GetCertifiedAwarenessGraphCommit(), stamp.AwarenessGraphCommit)
	}
	if authority.GetCertifiedServicesRepoCommit() != stamp.ServicesCommit {
		t.Fatalf("graph authority services commit=%q, want %q", authority.GetCertifiedServicesRepoCommit(), stamp.ServicesCommit)
	}
	if !authority.GetEmbeddedTransactionMatchesSeed() {
		t.Fatalf("graph authority should certify embedded seed, detail=%q", authority.GetEmbeddedTransactionDetail())
	}
	if authority.GetEmbeddedTransactionDetail() == "" {
		t.Fatal("graph authority transaction detail should be populated")
	}
}

func TestEvaluateTransactionForGraph(t *testing.T) {
	marker := seedmeta.Marker{
		Digest:      "abc123",
		TripleCount: 42,
	}

	tests := []struct {
		name      string
		stamp     seedmeta.TransactionStamp
		wantMatch bool
		wantPart  string
	}{
		{
			name:      "missing stamp",
			stamp:     seedmeta.TransactionStamp{},
			wantMatch: false,
			wantPart:  "missing",
		},
		{
			name: "digest mismatch",
			stamp: seedmeta.TransactionStamp{
				Present:         true,
				SeedDigest:      "deadbeef",
				SeedTripleCount: "42",
			},
			wantMatch: false,
			wantPart:  "does not match",
		},
		{
			name: "triple count invalid",
			stamp: seedmeta.TransactionStamp{
				Present:         true,
				SeedDigest:      "abc123",
				SeedTripleCount: "nope",
			},
			wantMatch: false,
			wantPart:  "invalid",
		},
		{
			name: "current",
			stamp: seedmeta.TransactionStamp{
				Present:         true,
				SeedDigest:      "abc123",
				SeedTripleCount: "42",
			},
			wantMatch: true,
			wantPart:  "certifies",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMatch, gotDetail := evaluateTransactionForGraph(marker, tt.stamp, "embedded", "", nil)
			if gotMatch != tt.wantMatch {
				t.Fatalf("match=%v, want %v (detail=%q)", gotMatch, tt.wantMatch, gotDetail)
			}
			if !strings.Contains(gotDetail, tt.wantPart) {
				t.Fatalf("detail=%q, want substring %q", gotDetail, tt.wantPart)
			}
		})
	}
}

func assertCurrentAuthority(t *testing.T, authority *awarenesspb.GraphAuthority) {
	t.Helper()
	if authority == nil {
		t.Fatal("authority should be populated")
	}
	if !authority.GetAuthoritative() {
		t.Fatalf("authority authoritative=false, want true (freshness=%s detail=%q)",
			authority.GetGraphFreshnessState(), authority.GetGraphFreshnessDetail())
	}
	if authority.GetGraphFreshnessState() != awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT {
		t.Fatalf("authority freshness=%s, want CURRENT", authority.GetGraphFreshnessState())
	}
	if authority.GetEmbeddedSeedDigestSha256() == "" {
		t.Fatal("authority embedded seed digest should be populated")
	}
	if authority.GetLiveStoreGraphDigestSha256() == "" {
		t.Fatal("authority live graph digest should be populated")
	}
	if authority.GetLiveStoreGraphTripleCount() == 0 {
		t.Fatal("authority live graph triple count should be populated")
	}
}

func TestGraphBackedRPCs_SuccessResponsesCarryCurrentAuthority(t *testing.T) {
	ctx := context.Background()

	briefingServer := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{
				{
					NodeIRI:   "https://globular.io/awareness#invariant/test.example.invariant",
					TypeIRI:   rdf.ClassInvariant,
					Predicate: rdf.PropLabel,
					Object:    "Test invariant",
				},
				{
					NodeIRI:   "https://globular.io/awareness#invariant/test.example.invariant",
					TypeIRI:   rdf.ClassInvariant,
					Predicate: rdf.PropSeverity,
					Object:    "high",
				},
			}, nil
		},
		describe: func(_ context.Context, iri string) ([]store.Triple, error) {
			if iri == "https://globular.io/awareness#invariant/test.example.invariant" {
				return []store.Triple{{Predicate: rdf.PropLabel, Object: "Test invariant"}}, nil
			}
			return nil, nil
		},
		classFacts: func(_ context.Context, _ string, _ int) ([]store.ImpactFact, error) {
			return nil, nil
		},
	})

	briefingResp, err := briefingServer.Briefing(ctx, &awarenesspb.BriefingRequest{File: "test/example.go"})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	assertCurrentAuthority(t, briefingResp.GetAuthority())

	impactResp, err := briefingServer.Impact(ctx, &awarenesspb.ImpactRequest{File: "test/example.go"})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	assertCurrentAuthority(t, impactResp.GetAuthority())

	resolveResp, err := briefingServer.Resolve(ctx, &awarenesspb.ResolveRequest{Id: "test.example.invariant", Class: "invariant"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	assertCurrentAuthority(t, resolveResp.GetAuthority())

	queryServer := newServer(fakeStore{
		describe: func(_ context.Context, iri string) ([]store.Triple, error) {
			if iri != "https://globular.io/awareness#invariant/test.example.invariant" {
				t.Fatalf("unexpected query iri: %s", iri)
			}
			return []store.Triple{
				{Predicate: rdf.PropLabel, Object: "Invariant Label"},
				{Predicate: rdf.PropSeverity, Object: "high"},
			}, nil
		},
	})
	queryResp, err := queryServer.Query(ctx, &awarenesspb.QueryRequest{
		Mode: awarenesspb.QueryMode_QUERY_MODE_BY_ID,
		Id:   "invariant:test.example.invariant",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	assertCurrentAuthority(t, queryResp.GetAuthority())

	preflightServer := newPreflightTestServer(t, map[string][]store.ImpactFact{
		fileIRI("golang/node_agent/heartbeat.go"): invariantFacts("node_agent.heartbeat.preserves_convergence", "Heartbeat preserves convergence", "high"),
	}, false)
	preflightResp, err := preflightServer.Preflight(ctx, &awarenesspb.PreflightRequest{
		Task:  "change install convergence",
		Files: []string{"golang/node_agent/heartbeat.go"},
	})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	assertCurrentAuthority(t, preflightResp.GetAuthority())
}

func TestBriefing_FailsClosedWhenGraphFreshnessStale(t *testing.T) {
	marker := testEmbeddedSeedMarker()
	s := newServer(fakeStore{
		graphFreshness: func(context.Context) seedmeta.Verification {
			return seedmeta.Verification{
				State:    seedmeta.FreshnessStale,
				Expected: marker,
				Detail:   "live graph digest does not match embedded artifact",
			}
		},
	})
	_, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "test/example.go"})
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), "graph freshness stale") {
		t.Fatalf("Briefing error=%v, want FailedPrecondition stale", err)
	}
}

func TestResolve_FailsClosedWhenGraphFreshnessEmpty(t *testing.T) {
	marker := testEmbeddedSeedMarker()
	s := newServer(fakeStore{
		graphFreshness: func(context.Context) seedmeta.Verification {
			return seedmeta.Verification{
				State:    seedmeta.FreshnessEmpty,
				Expected: marker,
				Detail:   "live store is empty",
			}
		},
	})
	_, err := s.Resolve(context.Background(), &awarenesspb.ResolveRequest{Id: "x", Class: "invariant"})
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), "graph freshness empty") {
		t.Fatalf("Resolve error=%v, want FailedPrecondition empty", err)
	}
}

func TestBriefing_DeterministicOrdering_SeverityIDLabel(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{
				{NodeIRI: "https://globular.io/awareness#invariant/z.inv", TypeIRI: "https://globular.io/awareness#Invariant", Predicate: "https://globular.io/awareness#severity", Object: "warning"},
				{NodeIRI: "https://globular.io/awareness#invariant/z.inv", TypeIRI: "https://globular.io/awareness#Invariant", Predicate: "http://www.w3.org/2000/01/rdf-schema#label", Object: "B"},
				{NodeIRI: "https://globular.io/awareness#invariant/a.inv", TypeIRI: "https://globular.io/awareness#Invariant", Predicate: "https://globular.io/awareness#severity", Object: "critical"},
				{NodeIRI: "https://globular.io/awareness#invariant/a.inv", TypeIRI: "https://globular.io/awareness#Invariant", Predicate: "http://www.w3.org/2000/01/rdf-schema#label", Object: "Z"},
				{NodeIRI: "https://globular.io/awareness#invariant/y.inv", TypeIRI: "https://globular.io/awareness#Invariant", Predicate: "https://globular.io/awareness#severity", Object: "warning"},
				{NodeIRI: "https://globular.io/awareness#invariant/y.inv", TypeIRI: "https://globular.io/awareness#Invariant", Predicate: "http://www.w3.org/2000/01/rdf-schema#label", Object: "A"},
			}, nil
		},
	})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "test/example.go"})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	prose := resp.GetProse()
	ai := strings.Index(prose, "[critical] a.inv")
	yi := strings.Index(prose, "[warning] y.inv")
	zi := strings.Index(prose, "[warning] z.inv")
	if ai < 0 || yi < 0 || zi < 0 {
		t.Fatalf("missing expected invariant bullets in prose: %q", prose)
	}
	if !(ai < yi && yi < zi) {
		t.Fatalf("unexpected order (want severity then id then label): %q", prose)
	}
	assertCurrentAuthority(t, resp.GetAuthority())
}

func TestBriefing_CodeSymbols_OKWhenOnlySymbols(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return nil, nil
		},
		codeSymbolFacts: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{
				{
					NodeIRI:   "https://globular.io/awareness#codeSymbol/golang%2Fserver%2Fbriefing.go:Briefing",
					TypeIRI:   "https://globular.io/awareness#CodeSymbol",
					Predicate: "http://www.w3.org/2000/01/rdf-schema#label",
					Object:    "Briefing",
				},
				{
					NodeIRI:   "https://globular.io/awareness#codeSymbol/golang%2Fserver%2Fbriefing.go:Briefing",
					TypeIRI:   "https://globular.io/awareness#CodeSymbol",
					Predicate: "http://www.w3.org/2000/01/rdf-schema#comment",
					Object:    "component: server.briefing",
				},
			}, nil
		},
	})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "golang/server/briefing.go"})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if resp.GetStatus() != awarenesspb.BriefingStatus_BRIEFING_STATUS_OK {
		t.Fatalf("status=%v, want OK (code symbols alone should yield OK)", resp.GetStatus())
	}
	found := false
	for _, id := range resp.GetReferencedIds() {
		if strings.HasPrefix(id, "code_symbol:") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no code_symbol: entry in referenced_ids: %v", resp.GetReferencedIds())
	}
}

func TestBriefing_CodeSymbols_IncludedInProse(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return nil, nil
		},
		codeSymbolFacts: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			symIRI := "https://globular.io/awareness#codeSymbol/golang%2Fserver%2Fbriefing.go:Briefing"
			return []store.ImpactFact{
				{NodeIRI: symIRI, TypeIRI: "https://globular.io/awareness#CodeSymbol", Predicate: "http://www.w3.org/2000/01/rdf-schema#label", Object: "Briefing"},
				{NodeIRI: symIRI, TypeIRI: "https://globular.io/awareness#CodeSymbol", Predicate: "http://www.w3.org/2000/01/rdf-schema#comment", Object: "component: server.briefing"},
				{NodeIRI: symIRI, TypeIRI: "https://globular.io/awareness#CodeSymbol", Predicate: "https://globular.io/awareness#risk", Object: "high"},
			}, nil
		},
	})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "golang/server/briefing.go"})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	prose := resp.GetProse()
	for _, want := range []string{"Code context:", "Component: server.briefing", "Briefing"} {
		if !strings.Contains(prose, want) {
			t.Fatalf("prose missing %q:\n%s", want, prose)
		}
	}
}

func TestBriefing_CodeSymbols_LinkedIntentInProse(t *testing.T) {
	symIRI := "https://globular.io/awareness#codeSymbol/golang%2Fserver%2Fbriefing.go:Briefing"
	intentIRI := "https://globular.io/awareness#intent/awareness.briefing_returns_explicit_status"
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return nil, nil
		},
		codeSymbolFacts: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{
				{NodeIRI: symIRI, TypeIRI: "https://globular.io/awareness#CodeSymbol", Predicate: "http://www.w3.org/2000/01/rdf-schema#label", Object: "Briefing"},
				{NodeIRI: symIRI, TypeIRI: "https://globular.io/awareness#CodeSymbol", Predicate: "https://globular.io/awareness#implements", Object: intentIRI, ObjectIsIRI: true},
			}, nil
		},
	})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "golang/server/briefing.go"})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if !strings.Contains(resp.GetProse(), "Implements:") {
		t.Fatalf("missing Implements section in prose:\n%s", resp.GetProse())
	}
	if !strings.Contains(resp.GetProse(), "intent:awareness.briefing_returns_explicit_status") {
		t.Fatalf("missing intent ref in prose:\n%s", resp.GetProse())
	}
}

func TestBriefing_CodeSymbols_LinkedInvariantInProse(t *testing.T) {
	symIRI := "https://globular.io/awareness#codeSymbol/golang%2Fserver%2Fbriefing.go:Briefing"
	invIRI := "https://globular.io/awareness#invariant/awareness.store_unavailable_explicit"
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return nil, nil
		},
		codeSymbolFacts: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{
				{NodeIRI: symIRI, TypeIRI: "https://globular.io/awareness#CodeSymbol", Predicate: "http://www.w3.org/2000/01/rdf-schema#label", Object: "Briefing"},
				{NodeIRI: symIRI, TypeIRI: "https://globular.io/awareness#CodeSymbol", Predicate: "https://globular.io/awareness#enforces", Object: invIRI, ObjectIsIRI: true},
			}, nil
		},
	})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "golang/server/briefing.go"})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if !strings.Contains(resp.GetProse(), "Enforces:") {
		t.Fatalf("missing Enforces section in prose:\n%s", resp.GetProse())
	}
	if !strings.Contains(resp.GetProse(), "invariant:awareness.store_unavailable_explicit") {
		t.Fatalf("missing invariant ref in prose:\n%s", resp.GetProse())
	}
}

func TestBriefing_CodeSymbols_PartiallyViolatesInProse(t *testing.T) {
	symIRI := "https://globular.io/awareness#codeSymbol/golang%2Fserver%2Fbriefing.go:Briefing"
	invIRI := "https://globular.io/awareness#invariant/meta.fail_safe_defaults_when_authority_is_uncertain"
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return nil, nil
		},
		codeSymbolFacts: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{
				{NodeIRI: symIRI, TypeIRI: "https://globular.io/awareness#CodeSymbol", Predicate: "http://www.w3.org/2000/01/rdf-schema#label", Object: "Briefing"},
				{NodeIRI: symIRI, TypeIRI: "https://globular.io/awareness#CodeSymbol", Predicate: "https://globular.io/awareness#partiallyViolates", Object: invIRI, ObjectIsIRI: true},
			}, nil
		},
	})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "golang/server/briefing.go"})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if !strings.Contains(resp.GetProse(), "Partially violates (KNOWN GAP):") {
		t.Fatalf("missing Partially violates section in prose:\n%s", resp.GetProse())
	}
	if !strings.Contains(resp.GetProse(), "invariant:meta.fail_safe_defaults_when_authority_is_uncertain") {
		t.Fatalf("missing invariant ref in prose:\n%s", resp.GetProse())
	}
	// referenced_ids must also include the partially-violated invariant so
	// the caller can drill in with awareness.resolve.
	found := false
	for _, id := range resp.GetReferencedIds() {
		if id == "invariant:meta.fail_safe_defaults_when_authority_is_uncertain" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("partially-violated invariant missing from referenced_ids: %v", resp.GetReferencedIds())
	}
}

func TestBriefing_UnknownFile_StillEmpty(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return nil, nil
		},
		codeSymbolFacts: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return nil, nil
		},
	})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "golang/no/such/file.go"})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if resp.GetStatus() != awarenesspb.BriefingStatus_BRIEFING_STATUS_EMPTY {
		t.Fatalf("status=%v, want EMPTY for unknown file", resp.GetStatus())
	}
}

func TestBriefing_CodeSymbols_DeterministicAcrossRuns(t *testing.T) {
	symIRI := "https://globular.io/awareness#codeSymbol/golang%2Fserver%2Fbriefing.go:Briefing"
	makeStore := func() fakeStore {
		return fakeStore{
			impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
				return nil, nil
			},
			codeSymbolFacts: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
				return []store.ImpactFact{
					{NodeIRI: symIRI, TypeIRI: "https://globular.io/awareness#CodeSymbol", Predicate: "http://www.w3.org/2000/01/rdf-schema#label", Object: "Briefing"},
					{NodeIRI: symIRI, TypeIRI: "https://globular.io/awareness#CodeSymbol", Predicate: "http://www.w3.org/2000/01/rdf-schema#comment", Object: "component: server.briefing"},
				}, nil
			},
		}
	}
	s := newServer(makeStore())
	a, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "golang/server/briefing.go"})
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	b, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "golang/server/briefing.go"})
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if a.GetProse() != b.GetProse() {
		t.Fatalf("prose differs across calls:\nA:\n%s\nB:\n%s", a.GetProse(), b.GetProse())
	}
	if strings.Join(a.GetReferencedIds(), ",") != strings.Join(b.GetReferencedIds(), ",") {
		t.Fatalf("referenced_ids differ: %v vs %v", a.GetReferencedIds(), b.GetReferencedIds())
	}
}

func TestQuery_RejectsUnsupportedMode(t *testing.T) {
	s := newServer(fakeStore{})
	_, err := s.Query(context.Background(), &awarenesspb.QueryRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Query code=%s, want %s", status.Code(err), codes.InvalidArgument)
	}
}

func TestQuery_NoLongerUnimplemented(t *testing.T) {
	client, cleanup := startBufconnServer(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := client.Query(ctx, &awarenesspb.QueryRequest{})
	if status.Code(err) == codes.Unimplemented {
		t.Fatalf("Query still unimplemented")
	}
}

func TestQuery_RejectsMissingRequiredFields(t *testing.T) {
	s := newServer(fakeStore{})
	tests := []*awarenesspb.QueryRequest{
		{Mode: awarenesspb.QueryMode_QUERY_MODE_BY_FILE},
		{Mode: awarenesspb.QueryMode_QUERY_MODE_BY_ID},
		{Mode: awarenesspb.QueryMode_QUERY_MODE_BY_CLASS},
		{Mode: awarenesspb.QueryMode_QUERY_MODE_RELATED},
	}
	for _, req := range tests {
		_, err := s.Query(context.Background(), req)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("Query(%v) code=%s, want %s", req.Mode, status.Code(err), codes.InvalidArgument)
		}
	}
}

func TestQuery_ByID_ReturnsOneRow(t *testing.T) {
	s := newServer(fakeStore{
		describe: func(_ context.Context, iri string) ([]store.Triple, error) {
			if iri != "https://globular.io/awareness#invariant/test.example.invariant" {
				t.Fatalf("unexpected iri: %s", iri)
			}
			return []store.Triple{
				{Predicate: "http://www.w3.org/2000/01/rdf-schema#label", Object: "Invariant Label"},
				{Predicate: "https://globular.io/awareness#severity", Object: "high"},
			}, nil
		},
	})
	resp, err := s.Query(context.Background(), &awarenesspb.QueryRequest{
		Mode: awarenesspb.QueryMode_QUERY_MODE_BY_ID,
		Id:   "invariant:test.example.invariant",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(resp.GetRows()) != 1 || resp.GetRows()[0].GetId() != "invariant:test.example.invariant" {
		t.Fatalf("rows=%v", resp.GetRows())
	}
	assertCurrentAuthority(t, resp.GetAuthority())
}

func TestQuery_ByFile_ReturnsDirectRowsOnly(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{
				{NodeIRI: "https://globular.io/awareness#invariant/a", TypeIRI: "https://globular.io/awareness#Invariant"},
				{NodeIRI: "https://globular.io/awareness#failureMode/b", TypeIRI: "https://globular.io/awareness#FailureMode"},
			}, nil
		},
	})
	resp, err := s.Query(context.Background(), &awarenesspb.QueryRequest{
		Mode: awarenesspb.QueryMode_QUERY_MODE_BY_FILE,
		File: "test/example.go",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(resp.GetRows()) != 2 {
		t.Fatalf("rows=%d, want 2", len(resp.GetRows()))
	}
	assertCurrentAuthority(t, resp.GetAuthority())
}

func TestQuery_ByClass_RespectsLimit(t *testing.T) {
	s := newServer(fakeStore{
		classFacts: func(_ context.Context, classIRI string, limit int) ([]store.ImpactFact, error) {
			if classIRI != "https://globular.io/awareness#Invariant" {
				t.Fatalf("classIRI=%q", classIRI)
			}
			if limit != 100 {
				t.Fatalf("limit=%d, want 100 cap", limit)
			}
			return []store.ImpactFact{
				{NodeIRI: "https://globular.io/awareness#invariant/a", TypeIRI: classIRI},
			}, nil
		},
	})
	resp, err := s.Query(context.Background(), &awarenesspb.QueryRequest{
		Mode:  awarenesspb.QueryMode_QUERY_MODE_BY_CLASS,
		Class: awarenesspb.QueryClass_QUERY_CLASS_INVARIANT,
		Limit: 9999,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(resp.GetRows()) != 1 || resp.GetRows()[0].GetClass() != "invariant" {
		t.Fatalf("rows=%v", resp.GetRows())
	}
	assertCurrentAuthority(t, resp.GetAuthority())
}

func TestQuery_Related_DirectOnly(t *testing.T) {
	s := newServer(fakeStore{
		describe: func(_ context.Context, iri string) ([]store.Triple, error) {
			switch iri {
			case "https://globular.io/awareness#invariant/root":
				return []store.Triple{
					{Predicate: "https://globular.io/awareness#affects", ObjectIsIRI: true, Object: "https://globular.io/awareness#failureMode/child"},
				}, nil
			case "https://globular.io/awareness#failureMode/child":
				return []store.Triple{
					{Predicate: "http://www.w3.org/2000/01/rdf-schema#label", Object: "Child"},
					{Predicate: "https://globular.io/awareness#affects", ObjectIsIRI: true, Object: "https://globular.io/awareness#incidentPattern/grandchild"},
				}, nil
			default:
				t.Fatalf("unexpected describe iri: %s", iri)
				return nil, nil
			}
		},
	})
	resp, err := s.Query(context.Background(), &awarenesspb.QueryRequest{
		Mode: awarenesspb.QueryMode_QUERY_MODE_RELATED,
		Id:   "invariant:root",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(resp.GetRows()) != 1 {
		t.Fatalf("rows=%d, want 1 direct row", len(resp.GetRows()))
	}
	if resp.GetRows()[0].GetId() != "failure_mode:child" || resp.GetRows()[0].GetRelation() != "affects" {
		t.Fatalf("row=%+v", resp.GetRows()[0])
	}
	assertCurrentAuthority(t, resp.GetAuthority())
}

func TestQuery_DeterministicOrdering(t *testing.T) {
	s := newServer(fakeStore{
		classFacts: func(_ context.Context, _ string, _ int) ([]store.ImpactFact, error) {
			return []store.ImpactFact{
				{NodeIRI: "https://globular.io/awareness#invariant/z", TypeIRI: "https://globular.io/awareness#Invariant", Predicate: "https://globular.io/awareness#severity", Object: "warning"},
				{NodeIRI: "https://globular.io/awareness#invariant/a", TypeIRI: "https://globular.io/awareness#Invariant", Predicate: "https://globular.io/awareness#severity", Object: "critical"},
			}, nil
		},
	})
	a, err := s.Query(context.Background(), &awarenesspb.QueryRequest{
		Mode:  awarenesspb.QueryMode_QUERY_MODE_BY_CLASS,
		Class: awarenesspb.QueryClass_QUERY_CLASS_INVARIANT,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	b, err := s.Query(context.Background(), &awarenesspb.QueryRequest{
		Mode:  awarenesspb.QueryMode_QUERY_MODE_BY_CLASS,
		Class: awarenesspb.QueryClass_QUERY_CLASS_INVARIANT,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if strings.Join(queryRowIDs(a.GetRows()), ",") != strings.Join(queryRowIDs(b.GetRows()), ",") {
		t.Fatalf("ordering mismatch: %v vs %v", queryRowIDs(a.GetRows()), queryRowIDs(b.GetRows()))
	}
}

func TestQuery_RawSPARQLLikeInputRejected(t *testing.T) {
	s := newServer(fakeStore{})
	_, err := s.Query(context.Background(), &awarenesspb.QueryRequest{
		Mode: awarenesspb.QueryMode_QUERY_MODE_BY_ID,
		Id:   "SELECT * WHERE {?s ?p ?o}",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Query code=%s, want %s", status.Code(err), codes.InvalidArgument)
	}
}

func TestQuery_BackendErrorReturnsUnavailable(t *testing.T) {
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return nil, errors.New("backend down")
		},
	})
	_, err := s.Query(context.Background(), &awarenesspb.QueryRequest{
		Mode: awarenesspb.QueryMode_QUERY_MODE_BY_FILE,
		File: "test/example.go",
	})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Query code=%s, want %s", status.Code(err), codes.Unavailable)
	}
}

func queryRowIDs(rows []*awarenesspb.QueryRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.GetId())
	}
	return out
}

// captureLogger returns a *log.Logger that writes into the provided
// bytes.Buffer. Used by validateBackend tests to assert on the warning
// or info lines emitted, since operators rely on those messages to
// understand whether the server started in a degraded state.
func captureLogger(buf *bytes.Buffer) *log.Logger {
	return log.New(buf, "", 0)
}

// TestValidateBackend_HealthyStore_ReturnsNil pins the happy path. The
// "backend healthy" log line is part of the operator-visible contract:
// it's how the operator knows the configured -oxigraph-url is correct.
func TestValidateBackend_HealthyStore_ReturnsNil(t *testing.T) {
	var buf bytes.Buffer
	logger := captureLogger(&buf)

	err := validateBackend(context.Background(), nopStore{}, false, "http://test/query", logger)
	if err != nil {
		t.Fatalf("validateBackend returned %v on healthy store", err)
	}
	if !strings.Contains(buf.String(), "backend healthy at http://test/query") {
		t.Errorf("expected healthy-confirmation log line; got:\n%s", buf.String())
	}
}

// TestValidateBackend_UnhealthyStore_RequireFalse_LogsWarning is the
// production-default path: backend down, but the operator hasn't asked
// for fail-fast. The server should start; the warning is the only
// signal. Asserting on the warning message keeps the line stable across
// future refactors.
func TestValidateBackend_UnhealthyStore_RequireFalse_LogsWarning(t *testing.T) {
	var buf bytes.Buffer
	logger := captureLogger(&buf)

	err := validateBackend(
		context.Background(),
		failingStore{err: errors.New("connection refused")},
		false, // require-store=false
		"http://unreachable:1/query",
		logger,
	)
	if err != nil {
		t.Fatalf("validateBackend returned %v with require-store=false; expected nil so the server starts in degraded mode", err)
	}
	out := buf.String()
	if !strings.Contains(out, "WARNING") {
		t.Errorf("expected WARNING in log output; got:\n%s", out)
	}
	if !strings.Contains(out, "connection refused") {
		t.Errorf("expected underlying error to surface in warning; got:\n%s", out)
	}
	if !strings.Contains(out, "-require-store") {
		t.Errorf("expected operator hint about -require-store; got:\n%s", out)
	}
}

// TestValidateBackend_UnhealthyStore_RequireTrue_ReturnsError is the
// other arm: the operator explicitly demanded a healthy backend at
// startup, so we must fail fast with a message that names the URL and
// the underlying cause. Anything less makes incident triage harder.
func TestValidateBackend_UnhealthyStore_RequireTrue_ReturnsError(t *testing.T) {
	var buf bytes.Buffer
	logger := captureLogger(&buf)

	err := validateBackend(
		context.Background(),
		failingStore{err: errors.New("connection refused")},
		true, // require-store=true
		"http://unreachable:1/query",
		logger,
	)
	if err == nil {
		t.Fatal("validateBackend returned nil with require-store=true and unhealthy store")
	}
	msg := err.Error()
	if !strings.Contains(msg, "http://unreachable:1/query") {
		t.Errorf("error should name the configured URL; got %v", err)
	}
	if !strings.Contains(msg, "connection refused") {
		t.Errorf("error should wrap the underlying cause; got %v", err)
	}
	if !strings.Contains(msg, "-require-store") {
		t.Errorf("error should mention the policy flag so operators know why the exit is fatal; got %v", err)
	}
}

// Compile-time assertion that the two test doubles satisfy the
// interface. If store.Store grows a method, this fires immediately.
var (
	_ store.Store = nopStore{}
	_ store.Store = failingStore{}
	_ store.Store = fakeStore{}
)
