// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/globulario/sensei/golang/architecture/controlstate"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store"
)

const (
	ctrlRepo = "github.com/globulario/sensei"
)

// controlFakeStore returns a fake store carrying a tiny typed graph: one governed contract, one
// source file, one dual-typed meta principle. Freshness comes from the embedded-seed marker
// default (current), so the graph authority is observed+current.
func controlFakeStore() fakeStore {
	nodes := map[string][]store.Triple{
		"aw:contract/one": {
			{Predicate: rdf.PropType, Object: rdf.ClassContract, ObjectIsIRI: true},
			{Predicate: rdf.PropLabel, Object: "Contract One"},
			{Predicate: rdf.PropStatus, Object: "governed"},
		},
		"aw:file/main.go": {
			{Predicate: rdf.PropType, Object: rdf.ClassSourceFile, ObjectIsIRI: true},
			{Predicate: rdf.PropLabel, Object: "main.go"},
		},
		"aw:meta/rule": {
			{Predicate: rdf.PropType, Object: rdf.ClassMetaPrinciple, ObjectIsIRI: true},
			{Predicate: rdf.PropType, Object: rdf.ClassInvariant, ObjectIsIRI: true},
			{Predicate: rdf.PropLabel, Object: "Meta Rule"},
		},
	}
	return fakeStore{
		describe: func(ctx context.Context, iri string) ([]store.Triple, error) {
			if t, ok := nodes[iri]; ok {
				return t, nil
			}
			// Fall through to the embedded seed marker for freshness verification.
			if marker := testEmbeddedSeedMarker(); marker.IRI != "" && iri == marker.IRI {
				return fakeStore{}.Describe(ctx, iri)
			}
			return nil, nil
		},
		classFacts: func(ctx context.Context, classIRI string, limit int) ([]store.ImpactFact, error) {
			var out []store.ImpactFact
			for iri, triples := range nodes {
				match := false
				for _, t := range triples {
					if t.Predicate == rdf.PropType && t.Object == classIRI {
						match = true
					}
				}
				if !match {
					continue
				}
				for _, t := range triples {
					out = append(out, store.ImpactFact{NodeIRI: iri, TypeIRI: classIRI, Predicate: t.Predicate, Object: t.Object, ObjectIsIRI: t.ObjectIsIRI})
				}
			}
			return out, nil
		},
	}
}

func controlTestServer() *server {
	return &server{store: controlFakeStore()}
}

// A semantically PARTIAL projection is a successful RPC response carrying its typed state —
// never a transport failure, never an empty success object (unknown stays distinct from zero).
func TestControlSnapshotRPC_PartialIsSuccess(t *testing.T) {
	s := controlTestServer()
	resp, err := s.GetArchitectureControlSnapshot(context.Background(), &awarenesspb.GetArchitectureControlSnapshotRequest{
		RepositoryIdentity: ctrlRepo,
	})
	if err != nil {
		t.Fatalf("partial projection must be RPC success: %v", err)
	}
	snap := resp.GetSnapshot()
	if snap.GetMeta().GetAvailability() != awarenesspb.ArchitectureAvailability_ARCHITECTURE_AVAILABILITY_PARTIAL {
		t.Fatalf("count sources are honestly unavailable → partial, got %v", snap.GetMeta().GetAvailability())
	}
	// Unknown stays distinct from zero: unobserved counts are ABSENT on the wire.
	if snap.OpenQuestionCount != nil || snap.ContradictionCount != nil || snap.MissingEvidenceCount != nil {
		t.Fatal("unobserved counts must be absent on the wire, never zero")
	}
	// Observed catalog tallies are data.
	if len(snap.GetCountsByClass()) == 0 {
		t.Fatal("catalog class tallies must be present (catalog source available)")
	}
	if !snap.GetMeta().GetNonAuthoritativeProjection() {
		t.Fatal("projection must remain non-authoritative on the wire")
	}
	if snap.GetMeta().GetDigestSha256() == "" {
		t.Fatal("canonical digest must be copied to the wire")
	}
	// The graph authority summary is typed truth from the freshness owner.
	if !snap.GetGraphAuthority().GetObserved() || !snap.GetGraphAuthority().GetCurrent() {
		t.Fatalf("fake seed marker verifies current: %+v", snap.GetGraphAuthority())
	}
	// No certification claims, no scores, no absolute paths on the wire.
	blob := snap.String()
	for _, needle := range []string{"certified", "score", "/tmp/", "/home/"} {
		if strings.Contains(strings.ToLower(blob), needle) {
			t.Fatalf("snapshot must not carry %q: %s", needle, blob)
		}
	}
}

// The index pages registry-resolved rows; the cursor is opaque and owner-validated; a duplicate
// dual-typed node resolves by registry precedence (never caller-selected class).
func TestListArtifactsRPC_CursorOpaqueAndRegistryResolved(t *testing.T) {
	s := controlTestServer()
	resp, err := s.ListArchitectureArtifacts(context.Background(), &awarenesspb.ListArchitectureArtifactsRequest{
		RepositoryIdentity: ctrlRepo, PageSize: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	idx := resp.GetIndex()
	if len(idx.GetPage()) != 2 || !idx.GetTruncated() || idx.GetNextCursor() == "" {
		t.Fatalf("expected a truncated 2-row page with a cursor: rows=%d trunc=%v", len(idx.GetPage()), idx.GetTruncated())
	}
	// Page 2 via the opaque cursor.
	resp2, err := s.ListArchitectureArtifacts(context.Background(), &awarenesspb.ListArchitectureArtifactsRequest{
		RepositoryIdentity: ctrlRepo, PageSize: 2, Cursor: idx.GetNextCursor(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp2.GetIndex().GetPage()) != 1 || resp2.GetIndex().GetTruncated() {
		t.Fatalf("expected the final 1-row page: %d", len(resp2.GetIndex().GetPage()))
	}
	// A tampered cursor is rejected as InvalidArgument (owner-validated, opaque).
	if _, err := s.ListArchitectureArtifacts(context.Background(), &awarenesspb.ListArchitectureArtifactsRequest{
		RepositoryIdentity: ctrlRepo, PageSize: 2, Cursor: "tampered-cursor",
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("tampered cursor must be InvalidArgument, got %v", err)
	}
	// The dual-typed node resolved to MetaPrinciple by registry precedence.
	foundMeta := false
	for _, row := range append(idx.GetPage(), resp2.GetIndex().GetPage()...) {
		if row.GetIdentity().GetNodeIri() == "aw:meta/rule" {
			foundMeta = true
			if row.GetClass() != rdf.ClassMetaPrinciple {
				t.Fatalf("dual-typed node must resolve by registry precedence, got %q", row.GetClass())
			}
		}
	}
	if !foundMeta {
		t.Fatal("meta principle row missing from the index")
	}
}

// A PRESENT filter enum set to UNSPECIFIED is rejected: UNSPECIFIED never means "no filter".
func TestListArtifactsRPC_UnspecifiedFilterRejected(t *testing.T) {
	s := controlTestServer()
	unspecified := awarenesspb.ArchitectureArtifactClosure_ARCHITECTURE_ARTIFACT_CLOSURE_UNSPECIFIED
	if _, err := s.ListArchitectureArtifacts(context.Background(), &awarenesspb.ListArchitectureArtifactsRequest{
		RepositoryIdentity: ctrlRepo, ClosureFilter: &unspecified,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("present UNSPECIFIED filter must be InvalidArgument, got %v", err)
	}
}

// Artifact state: registry precondition mismatch → FailedPrecondition; proven absence →
// NotFound; an existing artifact returns honest unknown-closure state (dimension sources
// honestly unavailable) as RPC success.
func TestArtifactStateRPC_PreconditionsAndNotFound(t *testing.T) {
	s := controlTestServer()
	// Registry-digest precondition.
	if _, err := s.GetArchitectureArtifactState(context.Background(), &awarenesspb.GetArchitectureArtifactStateRequest{
		RepositoryIdentity: ctrlRepo, NodeIri: "aw:contract/one", ExpectedRegistryDigest: "not-the-registry",
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("registry-digest mismatch must be FailedPrecondition, got %v", err)
	}
	// Authority-identity precondition.
	if _, err := s.GetArchitectureArtifactState(context.Background(), &awarenesspb.GetArchitectureArtifactStateRequest{
		RepositoryIdentity: ctrlRepo, NodeIri: "aw:contract/one", ExpectedGraphAuthorityIdentity: "not-the-authority",
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("authority mismatch must be FailedPrecondition, got %v", err)
	}
	// Proven absence (authority observed+current, node has no triples) → NotFound.
	if _, err := s.GetArchitectureArtifactState(context.Background(), &awarenesspb.GetArchitectureArtifactStateRequest{
		RepositoryIdentity: ctrlRepo, NodeIri: "aw:does/not/exist",
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("proven absence must be NotFound, got %v", err)
	}
	// An existing governed contract: success; class registry-resolved; dimension sources
	// honestly unavailable → closure unknown, projection partial; lifecycle ACTIVE from the
	// governed status observation.
	resp, err := s.GetArchitectureArtifactState(context.Background(), &awarenesspb.GetArchitectureArtifactStateRequest{
		RepositoryIdentity: ctrlRepo, NodeIri: "aw:contract/one",
	})
	if err != nil {
		t.Fatal(err)
	}
	st := resp.GetState()
	if st.GetCanonicalClass() != rdf.ClassContract {
		t.Fatalf("class must be registry-resolved from observed classes, got %q", st.GetCanonicalClass())
	}
	if st.GetClosure() != awarenesspb.ArchitectureArtifactClosure_ARCHITECTURE_ARTIFACT_CLOSURE_UNKNOWN {
		t.Fatalf("unassessed artifact must stay unknown, got %v", st.GetClosure())
	}
	if st.GetMeta().GetAvailability() != awarenesspb.ArchitectureAvailability_ARCHITECTURE_AVAILABILITY_PARTIAL {
		t.Fatalf("unavailable dimension sources must make the projection partial, got %v", st.GetMeta().GetAvailability())
	}
	if st.GetLifecycle().GetState() != awarenesspb.ArchitectureLifecycleState_ARCHITECTURE_LIFECYCLE_STATE_ACTIVE {
		t.Fatalf("governed status must assess active, got %v", st.GetLifecycle().GetState())
	}
	// Every dimension is typed-unknown with the exact reason — never satisfied by adjacency.
	for _, d := range st.GetDimensions() {
		if d.GetDimension() == "contradiction" {
			continue
		}
		if d.GetState() != awarenesspb.ArchitectureDimensionState_ARCHITECTURE_DIMENSION_STATE_UNKNOWN || d.GetReasonCode() != "source_not_observed" {
			t.Fatalf("dimension %q must be unknown/source_not_observed, got %v/%q", d.GetDimension(), d.GetState(), d.GetReasonCode())
		}
	}
}

// The navigation descriptor needs no store, repository context, or filesystem.
func TestNavigationDescriptorRPC_NoRepositoryContextNeeded(t *testing.T) {
	s := &server{} // no store, no repository context
	resp, err := s.GetOntologyNavigationDescriptor(context.Background(), &awarenesspb.GetOntologyNavigationDescriptorRequest{})
	if err != nil {
		t.Fatal(err)
	}
	d := resp.GetDescriptor_()
	if len(d.GetFamilies()) == 0 || d.GetRegistryDigest() == "" {
		t.Fatal("descriptor must carry registry families + digest")
	}
	if !d.GetUnknownClassFallback().GetDefaultVisible() {
		t.Fatal("unknown-class fallback must stay visible")
	}
	wantReg := controlstate.DefaultRegistry()
	wantDigest, _ := wantReg.Digest()
	if d.GetRegistryDigest() != wantDigest {
		t.Fatal("descriptor registry digest must be the canonical registry digest")
	}
}

// Request-shape law: repository identity is logical (no filesystem paths), required, unpadded.
func TestControlRPCs_RejectFilesystemRepositoryIdentity(t *testing.T) {
	s := controlTestServer()
	for _, bad := range []string{"", " padded ", "/abs/path", `C:\repo`, `\\host\share`} {
		if _, err := s.GetArchitectureControlSnapshot(context.Background(), &awarenesspb.GetArchitectureControlSnapshotRequest{
			RepositoryIdentity: bad,
		}); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("repository identity %q must be InvalidArgument, got %v", bad, err)
		}
	}
}

// A catalog whose class enumeration plausibly hit the store cap is DEGRADED (typed reason), and
// the RPC returns SUCCESS with an empty untrusted page — never a transport failure, never rows
// from an incomplete enumeration (no silent cap).
func TestListArtifactsRPC_TruncatedEnumerationDegradesHonestly(t *testing.T) {
	big := controlFakeStore()
	baseClassFacts := big.classFacts
	big.classFacts = func(ctx context.Context, classIRI string, limit int) ([]store.ImpactFact, error) {
		if classIRI != rdf.ClassContract {
			return baseClassFacts(ctx, classIRI, limit)
		}
		// Exactly the cap of distinct contract nodes → plausible truncation.
		var out []store.ImpactFact
		for i := 0; i < limit; i++ {
			iri := fmt.Sprintf("aw:contract/%04d", i)
			out = append(out,
				store.ImpactFact{NodeIRI: iri, TypeIRI: classIRI, Predicate: rdf.PropType, Object: classIRI, ObjectIsIRI: true},
				store.ImpactFact{NodeIRI: iri, TypeIRI: classIRI, Predicate: rdf.PropLabel, Object: iri},
			)
		}
		return out, nil
	}
	s := &server{store: big}
	resp, err := s.ListArchitectureArtifacts(context.Background(), &awarenesspb.ListArchitectureArtifactsRequest{
		RepositoryIdentity: ctrlRepo, PageSize: 10,
	})
	if err != nil {
		t.Fatalf("a degraded catalog must still be RPC success: %v", err)
	}
	idx := resp.GetIndex()
	if len(idx.GetPage()) != 0 {
		t.Fatalf("rows from a truncated enumeration must not be trusted, got %d", len(idx.GetPage()))
	}
	if idx.GetMeta().GetAvailability() == awarenesspb.ArchitectureAvailability_ARCHITECTURE_AVAILABILITY_AVAILABLE {
		t.Fatal("a degraded catalog must degrade the projection availability")
	}
	found := false
	for _, src := range idx.GetMeta().GetSources() {
		if src.GetReasonCode() == "class_enumeration_truncated" {
			found = true
		}
	}
	if !found {
		t.Fatal("the typed truncation reason must be on the wire")
	}
}

// Exact-scope feedback law at the RPC level: the snapshot exposes feedback CAPABILITY only —
// no verified-record collection ever appears in the snapshot response.
func TestControlSnapshotRPC_NoRepositoryWideFeedback(t *testing.T) {
	s := controlTestServer()
	resp, err := s.GetArchitectureControlSnapshot(context.Background(), &awarenesspb.GetArchitectureControlSnapshotRequest{
		RepositoryIdentity: ctrlRepo,
	})
	if err != nil {
		t.Fatal(err)
	}
	blob := resp.GetSnapshot().String()
	for _, needle := range []string{"verified_record_ids", "lineage_ids"} {
		if strings.Contains(blob, needle) {
			t.Fatalf("snapshot must never carry feedback records (%q found)", needle)
		}
	}
	// Repository context unconfigured on this test server → capability honestly false, source
	// retained as typed-unavailable.
	if resp.GetSnapshot().GetFeedbackContext() != nil {
		t.Fatal("an unavailable feedback source must withhold its payload")
	}
}
