// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"math/rand"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/globulario/sensei/golang/architecture/briefingfeedback"
	"github.com/globulario/sensei/golang/architecture/controlstate"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store"
)

// ── Fake stores for the authority matrix ──
//
// Authority admissibility is now derived from an INDEPENDENT live-marker discovery (ClassFacts on
// the SeedBuild class), so these fixtures drive the store's discovered marker via seedMarkerFacts
// — never by handing the provider a pre-baked freshness verification. The expected marker is the
// embedded-seed marker (testEmbeddedSeedMarker); a genuine older live graph carries its own
// digest-derived marker.

// staleLiveDigest is a genuine, self-consistent older live identity (distinct from the embedded
// expected digest). Its marker lives at ITS OWN digest-derived IRI.
const staleLiveDigest = "older-live-graph-digest"

// noLiveMarker exposes an empty SeedBuild class → no independently observed live authority.
func noLiveMarker(context.Context) []store.ImpactFact { return nil }

// staleLiveMarker exposes a genuine older marker at its digest-derived IRI whose count equals the
// default live count (testEmbeddedSeedMarker().TripleCount), so it is internally coherent and
// therefore stale-ADMISSIBLE, not integrity-failed.
func staleLiveMarker(context.Context) []store.ImpactFact {
	return seedBuildMarkerFacts(derivedMarkerIRI(staleLiveDigest), staleLiveDigest, testEmbeddedSeedMarker().TripleCount)
}

// Proof 1+4: an UNOBSERVED authority cannot produce an available catalog/index; the expected
// seed digest surfaces ONLY as expected-authority metadata, never as an observed identity.
func TestTrust_UnobservedAuthorityNeverAvailable(t *testing.T) {
	fs := controlFakeStore()
	fs.seedMarkerFacts = noLiveMarker
	s := &server{store: fs}
	expectedDigest := testEmbeddedSeedMarker().Digest
	resp, err := s.ListArchitectureArtifacts(context.Background(), &awarenesspb.ListArchitectureArtifactsRequest{
		RepositoryIdentity: ctrlRepo, PageSize: 10,
	})
	if err != nil {
		t.Fatalf("unobserved authority is semantic state, not a transport failure: %v", err)
	}
	idx := resp.GetIndex()
	if len(idx.GetPage()) != 0 {
		t.Fatal("no trusted rows under an unobserved authority")
	}
	if a := idx.GetMeta().GetAvailability(); a == awarenesspb.ArchitectureAvailability_ARCHITECTURE_AVAILABILITY_AVAILABLE ||
		a == awarenesspb.ArchitectureAvailability_ARCHITECTURE_AVAILABILITY_PARTIAL {
		t.Fatalf("unobserved authority must not read available/partial, got %v", a)
	}
	// The expected seed digest never appears as a source identity — only in the limitations.
	for _, src := range idx.GetMeta().GetSources() {
		if src.GetIdentity() == expectedDigest {
			t.Fatal("expected seed digest presented as an observed source identity")
		}
	}
	limFound := false
	for _, l := range idx.GetMeta().GetLimitations() {
		if strings.Contains(l, "expected_authority_seed_digest:"+expectedDigest) {
			limFound = true
		}
	}
	if !limFound {
		t.Fatal("expected seed digest must surface as expected-authority metadata (limitation)")
	}
	// The snapshot: authority summary honestly unobserved.
	snapResp, err := s.GetArchitectureControlSnapshot(context.Background(), &awarenesspb.GetArchitectureControlSnapshotRequest{RepositoryIdentity: ctrlRepo})
	if err != nil {
		t.Fatal(err)
	}
	if snapResp.GetSnapshot().GetGraphAuthority().GetObserved() {
		t.Fatal("authority must be unobserved")
	}
	if snapResp.GetSnapshot().GetGraphAuthority().GetIdentity() != "" {
		t.Fatal("an unobserved authority carries no identity")
	}
	if len(snapResp.GetSnapshot().GetCountsByClass()) != 0 {
		t.Fatal("no trusted tallies under an unobserved authority")
	}
}

// Proof 2: a STALE authority produces PARTIAL — never AVAILABLE — while keeping known rows, with
// the typed graph_authority_stale reason on the required source.
func TestTrust_StaleAuthorityPartialWithRows(t *testing.T) {
	fs := controlFakeStore()
	fs.seedMarkerFacts = staleLiveMarker
	s := &server{store: fs}
	resp, err := s.ListArchitectureArtifacts(context.Background(), &awarenesspb.ListArchitectureArtifactsRequest{
		RepositoryIdentity: ctrlRepo, PageSize: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	idx := resp.GetIndex()
	if len(idx.GetPage()) == 0 {
		t.Fatal("known observed rows must remain visible under a stale authority")
	}
	if idx.GetMeta().GetAvailability() != awarenesspb.ArchitectureAvailability_ARCHITECTURE_AVAILABILITY_PARTIAL {
		t.Fatalf("stale authority must yield PARTIAL, got %v", idx.GetMeta().GetAvailability())
	}
	found := false
	for _, src := range idx.GetMeta().GetSources() {
		if src.GetOwner() == "graph_authority" &&
			src.GetAvailability() == awarenesspb.ArchitectureSourceAvailability_ARCHITECTURE_SOURCE_AVAILABILITY_DEGRADED &&
			src.GetReasonCode() == "graph_authority_stale" {
			found = true
			if src.GetIdentity() != staleLiveDigest {
				t.Fatalf("the stale authority identity must stay the LIVE identity, got %q", src.GetIdentity())
			}
		}
	}
	if !found {
		t.Fatal("the stale authority must be a degraded required source with its typed reason")
	}
	// Rows carry the LIVE authority identity, never the expected seed digest.
	for _, row := range idx.GetPage() {
		if row.GetIdentity().GetGraphAuthorityIdentity() != staleLiveDigest {
			t.Fatalf("row authority identity must be the live identity, got %q", row.GetIdentity().GetGraphAuthorityIdentity())
		}
	}
}

// Proof 13 + 19: STALE absence is NOT NotFound — the artifact returns as a visible unknown-class
// state whose degraded authority is response data; and the snapshot still carries the known
// graph-authority attention item as partial data.
func TestTrust_StaleAbsenceNotNotFoundAndAttentionPartial(t *testing.T) {
	fs := controlFakeStore()
	fs.seedMarkerFacts = staleLiveMarker
	s := &server{store: fs}
	resp, err := s.GetArchitectureArtifactState(context.Background(), &awarenesspb.GetArchitectureArtifactStateRequest{
		RepositoryIdentity: ctrlRepo, NodeIri: "aw:does/not/exist",
	})
	if err != nil {
		t.Fatalf("stale absence must not be a transport error (unproven): %v", err)
	}
	st := resp.GetState()
	if st.GetClosure() != awarenesspb.ArchitectureArtifactClosure_ARCHITECTURE_ARTIFACT_CLOSURE_UNKNOWN {
		t.Fatalf("unproven absence must stay unknown, got %v", st.GetClosure())
	}
	// The snapshot's degraded attention collection still exposes the KNOWN stale-authority item.
	snapResp, err := s.GetArchitectureControlSnapshot(context.Background(), &awarenesspb.GetArchitectureControlSnapshotRequest{RepositoryIdentity: ctrlRepo})
	if err != nil {
		t.Fatal(err)
	}
	snap := snapResp.GetSnapshot()
	itemFound := false
	for _, a := range snap.GetTopAttention() {
		if a.GetAttentionClass() == "graph_authority_unavailable" {
			itemFound = true
		}
	}
	if !itemFound {
		t.Fatal("known graph-authority attention must remain visible as partial data")
	}
	// Proof 18: the collection is explicitly incomplete — never an authoritative complete zero.
	incomplete := false
	for _, src := range snap.GetMeta().GetSources() {
		if src.GetOwner() == "controlstate.attention" &&
			src.GetAvailability() == awarenesspb.ArchitectureSourceAvailability_ARCHITECTURE_SOURCE_AVAILABILITY_DEGRADED &&
			src.GetReasonCode() == "attention_sources_incomplete" {
			incomplete = true
		}
	}
	if !incomplete {
		t.Fatal("the attention collection must be typed DEGRADED attention_sources_incomplete while families are missing")
	}
}

// Proof 12: current-but-INTEGRITY-FAILED absence is not NotFound (via an injected provider — no
// production freshness state reports integrity failure yet, but the law is enforced).
func TestTrust_IntegrityFailedAbsenceNotNotFound(t *testing.T) {
	reg := controlstate.DefaultRegistry()
	obs := controlstate.GraphAuthorityObservation{Observed: true, Current: true, Integrity: false, Identity: "live-x", Digest: "live-x"}
	id, res, err := controlstate.BuildArtifactIdentity(reg, "aw:gone", nil, ctrlRepo, "", "live-x", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := &server{store: controlFakeStore(), controlProvider: &fixedCtrlProvider{
		obs: obs, expected: "expected-seed-d",
		id: id, res: res,
		bundle: controlstate.ArtifactSourceBundle{
			GraphAuthority: obs,
			Contradiction:  controlstate.ContradictionSource{Availability: controlstate.SourceUnavailable, ReasonCode: "source_not_observed"},
		},
		observed: false,
	}}
	resp, err := s.GetArchitectureArtifactState(context.Background(), &awarenesspb.GetArchitectureArtifactStateRequest{
		RepositoryIdentity: ctrlRepo, NodeIri: "aw:gone",
	})
	if status.Code(err) == codes.NotFound {
		t.Fatal("integrity-failed absence must never be NotFound")
	}
	if err != nil {
		t.Fatalf("the canonical unknown/unavailable projection must be returned: %v", err)
	}
	// The invalid authority is response data.
	invalid := false
	for _, src := range resp.GetState().GetMeta().GetSources() {
		if src.GetOwner() == "graph_authority" && src.GetAvailability() == awarenesspb.ArchitectureSourceAvailability_ARCHITECTURE_SOURCE_AVAILABILITY_INVALID {
			invalid = true
		}
	}
	if !invalid {
		t.Fatal("the integrity-failed authority must surface as an INVALID source")
	}
}

// Proof (matrix): an unobserved authority makes artifact state unconstructible → the closed
// transport law (Unavailable), never NotFound, never an expected-digest-backed identity.
func TestTrust_UnobservedArtifactStateUnavailable(t *testing.T) {
	fs := controlFakeStore()
	fs.seedMarkerFacts = noLiveMarker
	s := &server{store: fs}
	_, err := s.GetArchitectureArtifactState(context.Background(), &awarenesspb.GetArchitectureArtifactStateRequest{
		RepositoryIdentity: ctrlRepo, NodeIri: "aw:contract/one",
	})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("unobserved authority must map to Unavailable, got %v", err)
	}
}

// fixedCtrlProvider injects an exact provider state (the seam the handler law is proven
// against).
type fixedCtrlProvider struct {
	obs      controlstate.GraphAuthorityObservation
	expected string
	catalog  controlstate.CatalogSnapshot
	input    controlstate.ControlSnapshotInput
	id       controlstate.ArtifactIdentity
	res      controlstate.ClassResolution
	bundle   controlstate.ArtifactSourceBundle
	observed bool
}

func (p *fixedCtrlProvider) GraphAuthority(context.Context) (controlstate.GraphAuthorityObservation, string, error) {
	return p.obs, p.expected, nil
}
func (p *fixedCtrlProvider) CatalogSnapshot(context.Context, string, string) (controlstate.CatalogSnapshot, error) {
	return p.catalog, nil
}
func (p *fixedCtrlProvider) ControlSnapshotInput(context.Context, string, string) (controlstate.ControlSnapshotInput, error) {
	return p.input, nil
}
func (p *fixedCtrlProvider) ArtifactSourceBundle(context.Context, string, string, string) (controlstate.ArtifactIdentity, controlstate.ClassResolution, controlstate.ArtifactSourceBundle, bool, error) {
	return p.id, p.res, p.bundle, p.observed, nil
}

// ── Unknown-class discovery ──

// discoveryFakeStore adds the typedNodeLister capability over controlFakeStore, exposing one
// node typed ONLY as an unregistered class.
type discoveryFakeStore struct {
	fakeStore
	truncate bool
}

func (d discoveryFakeStore) TypedNodeFactsPage(ctx context.Context, afterIRI string, limit int) ([]store.ImpactFact, string, error) {
	if d.truncate {
		// Every page claims more remain: the sweep can never complete.
		iri := "aw:endless/" + afterIRI + "x"
		return []store.ImpactFact{{NodeIRI: iri, Predicate: rdf.PropType, Object: "https://example.org/Unregistered", ObjectIsIRI: true}}, iri, nil
	}
	if afterIRI != "" {
		return nil, "", nil
	}
	return []store.ImpactFact{
		{NodeIRI: "aw:strange/node", Predicate: rdf.PropType, Object: "https://example.org/Unregistered", ObjectIsIRI: true},
		{NodeIRI: "aw:strange/node", Predicate: rdf.PropLabel, Object: "Strange"},
	}, "", nil
}

// Proof 5: a node typed only as an UNREGISTERED class is discovered and stays visible under the
// unclassified fallback with unknown coverage/closure.
func TestTrust_UnknownOnlyNodeVisibleUnderUnclassified(t *testing.T) {
	s := &server{store: discoveryFakeStore{fakeStore: controlFakeStore()}}
	resp, err := s.ListArchitectureArtifacts(context.Background(), &awarenesspb.ListArchitectureArtifactsRequest{
		RepositoryIdentity: ctrlRepo, PageSize: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range resp.GetIndex().GetPage() {
		if row.GetIdentity().GetNodeIri() == "aw:strange/node" {
			found = true
			if row.GetClass() != controlstate.UnclassifiedClassSentinel {
				t.Fatalf("unknown-only node must resolve to the unclassified fallback, got %q", row.GetClass())
			}
			if row.GetAssessmentCoverage() != awarenesspb.ArchitectureAssessmentCoverage_ARCHITECTURE_ASSESSMENT_COVERAGE_UNKNOWN {
				t.Fatal("unknown-only node must stay CoverageUnknown")
			}
			if row.GetClosure() != awarenesspb.ArchitectureArtifactClosure_ARCHITECTURE_ARTIFACT_CLOSURE_UNKNOWN {
				t.Fatal("unknown-only node must stay ClosureUnknown")
			}
		}
	}
	if !found {
		t.Fatal("the unknown-only typed node must be discovered")
	}
}

// Proofs 6+7: a truncated unknown-class sweep is EXPLICIT (degraded unclassified_discovery with
// its typed reason) while known-class rows stay visible and the projection is PARTIAL.
func TestTrust_TruncatedDiscoveryExplicitKnownRowsVisible(t *testing.T) {
	s := &server{store: discoveryFakeStore{fakeStore: controlFakeStore(), truncate: true}}
	resp, err := s.ListArchitectureArtifacts(context.Background(), &awarenesspb.ListArchitectureArtifactsRequest{
		RepositoryIdentity: ctrlRepo, PageSize: 250,
	})
	if err != nil {
		t.Fatal(err)
	}
	idx := resp.GetIndex()
	knownFound := false
	for _, row := range idx.GetPage() {
		if row.GetIdentity().GetNodeIri() == "aw:contract/one" {
			knownFound = true
		}
	}
	if !knownFound {
		t.Fatal("known rows must remain visible while unknown discovery is partial")
	}
	degraded := false
	for _, src := range idx.GetMeta().GetSources() {
		if src.GetSchema() == "unclassified_discovery" &&
			src.GetAvailability() == awarenesspb.ArchitectureSourceAvailability_ARCHITECTURE_SOURCE_AVAILABILITY_DEGRADED &&
			src.GetReasonCode() == "unclassified_discovery_truncated" {
			degraded = true
		}
	}
	if !degraded {
		t.Fatal("a capped discovery must be a typed DEGRADED source, never a silent omission")
	}
	if idx.GetMeta().GetAvailability() != awarenesspb.ArchitectureAvailability_ARCHITECTURE_AVAILABILITY_PARTIAL {
		t.Fatalf("truncated discovery must yield PARTIAL, got %v", idx.GetMeta().GetAvailability())
	}
}

// ── IRI validation ──

// fullTripwireStore implements EVERY store.Store method AND every optional capability the
// server can reach (counts, domains, scoped class facts, typed-node discovery, freshness) —
// each one fails the test. It embeds nothing: no default behavior can leak through.
type fullTripwireStore struct{ t *testing.T }

func (w fullTripwireStore) trip(m string) {
	w.t.Errorf("rejected request must perform zero store calls (%s)", m)
}

func (w fullTripwireStore) Close() error                 { w.trip("Close"); return nil }
func (w fullTripwireStore) Health(context.Context) error { w.trip("Health"); return nil }
func (w fullTripwireStore) Describe(_ context.Context, iri string) ([]store.Triple, error) {
	w.trip("Describe " + iri)
	return nil, nil
}
func (w fullTripwireStore) DescribeInbound(_ context.Context, iri string) ([]store.InboundTriple, error) {
	w.trip("DescribeInbound " + iri)
	return nil, nil
}
func (w fullTripwireStore) ImpactForFile(_ context.Context, iri string) ([]store.ImpactFact, error) {
	w.trip("ImpactForFile " + iri)
	return nil, nil
}
func (w fullTripwireStore) ClassFacts(_ context.Context, classIRI string, _ int) ([]store.ImpactFact, error) {
	w.trip("ClassFacts " + classIRI)
	return nil, nil
}
func (w fullTripwireStore) CodeSymbolFacts(_ context.Context, iri string) ([]store.ImpactFact, error) {
	w.trip("CodeSymbolFacts " + iri)
	return nil, nil
}
func (w fullTripwireStore) RenderingGroupsForFile(_ context.Context, iri string) ([]store.RenderingGroupInfo, error) {
	w.trip("RenderingGroupsForFile " + iri)
	return nil, nil
}
func (w fullTripwireStore) DetectFacts(context.Context) ([]store.ImpactFact, error) {
	w.trip("DetectFacts")
	return nil, nil
}
func (w fullTripwireStore) CountTriples(context.Context) (int64, error) {
	w.trip("CountTriples")
	return 0, nil
}
func (w fullTripwireStore) CountByClass(_ context.Context, classIRI string) (int64, error) {
	w.trip("CountByClass " + classIRI)
	return 0, nil
}
func (w fullTripwireStore) Domains(context.Context) ([]string, error) {
	w.trip("Domains")
	return nil, nil
}
func (w fullTripwireStore) ClassFactsScoped(_ context.Context, classIRI, _, _ string, _ int) ([]store.ImpactFact, error) {
	w.trip("ClassFactsScoped " + classIRI)
	return nil, nil
}
func (w fullTripwireStore) TypedNodeFactsPage(_ context.Context, _ string, _ int) ([]store.ImpactFact, string, error) {
	w.trip("TypedNodeFactsPage")
	return nil, "", nil
}
func (w fullTripwireStore) GraphFreshness(context.Context) seedmeta.Verification {
	w.trip("GraphFreshness")
	return seedmeta.Verification{}
}

var _ store.Store = fullTripwireStore{}

// Proofs 8+9: a malicious node IRI is InvalidArgument (sanitized) and causes ZERO store calls.
func TestTrust_MaliciousIRIRejectedWithoutStoreAccess(t *testing.T) {
	for _, bad := range []string{
		"aw:x> . ?s ?p ?o . FILTER(<aw:y",
		"/etc/passwd",
		`C:/Users/x`,
		"aw:with space",
		" aw:padded ",
		"aw:x\"y",
		"",
	} {
		s := &server{store: fullTripwireStore{t: t}}
		_, err := s.GetArchitectureArtifactState(context.Background(), &awarenesspb.GetArchitectureArtifactStateRequest{
			RepositoryIdentity: ctrlRepo, NodeIri: bad,
		})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("IRI %q must be InvalidArgument, got %v", bad, err)
		}
		if strings.Contains(status.Convert(err).Message(), bad) && bad != "" {
			t.Fatalf("the rejection message must be sanitized, got %q", status.Convert(err).Message())
		}
	}
	// The store boundary rejects independently (defense in depth).
	if err := store.ValidateQueryIRI("aw:x> ?p"); err == nil {
		t.Fatal("the store-boundary validator must reject independently")
	}
}

// ── Multi-repository scope ──

// multiRepoStore serves one node tagged to TWO repositories, with shuffled triple order.
func multiRepoStore(seed int64) fakeStore {
	fs := controlFakeStore()
	base := fs.describe
	fs.describe = func(ctx context.Context, iri string) ([]store.Triple, error) {
		if iri != "aw:shared/tool" {
			return base(ctx, iri)
		}
		triples := []store.Triple{
			{Predicate: rdf.PropType, Object: rdf.ClassContract, ObjectIsIRI: true},
			{Predicate: rdf.PropRepo, Object: "github.com/globulario/sensei"},
			{Predicate: rdf.PropRepo, Object: "github.com/globulario/globular"},
			{Predicate: rdf.PropLabel, Object: "Shared Tool"},
			{Predicate: rdf.PropStatus, Object: "governed"},
		}
		r := rand.New(rand.NewSource(seed))
		r.Shuffle(len(triples), func(i, j int) { triples[i], triples[j] = triples[j], triples[i] })
		return triples, nil
	}
	return fs
}

// Proofs 10+11: a node tagged to two repositories is visible in EACH tagged scope, and triple
// ordering can never change visibility or the projection digest.
func TestTrust_MultiRepoNodeVisibleInEachScopeOrderInvariant(t *testing.T) {
	digests := map[string]string{}
	for _, scope := range []string{"github.com/globulario/sensei", "github.com/globulario/globular"} {
		for seed := int64(0); seed < 6; seed++ {
			s := &server{store: multiRepoStore(seed)}
			resp, err := s.GetArchitectureArtifactState(context.Background(), &awarenesspb.GetArchitectureArtifactStateRequest{
				RepositoryIdentity: ctrlRepo, Domain: scope, NodeIri: "aw:shared/tool",
			})
			if err != nil {
				t.Fatalf("scope %q seed %d: %v", scope, seed, err)
			}
			st := resp.GetState()
			if st.GetCanonicalClass() != rdf.ClassContract {
				t.Fatalf("multi-repo node must stay visible+typed in scope %q", scope)
			}
			prev, ok := digests[scope]
			if ok && prev != st.GetMeta().GetDigestSha256() {
				t.Fatalf("shuffled triple order changed the projection digest in scope %q", scope)
			}
			digests[scope] = st.GetMeta().GetDigestSha256()
		}
	}
	// A foreign-only node stays invisible: absence in a current scope → NotFound.
	s := &server{store: multiRepoStore(0)}
	fs := s.store.(fakeStore)
	base := fs.describe
	fs.describe = func(ctx context.Context, iri string) ([]store.Triple, error) {
		if iri == "aw:foreign/only" {
			return []store.Triple{
				{Predicate: rdf.PropType, Object: rdf.ClassContract, ObjectIsIRI: true},
				{Predicate: rdf.PropRepo, Object: "github.com/other/elsewhere"},
			}, nil
		}
		return base(ctx, iri)
	}
	s.store = fs
	if _, err := s.GetArchitectureArtifactState(context.Background(), &awarenesspb.GetArchitectureArtifactStateRequest{
		RepositoryIdentity: ctrlRepo, Domain: "github.com/globulario/sensei", NodeIri: "aw:foreign/only",
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("a foreign-only node must be authoritatively absent in this scope, got %v", err)
	}
}

// ── Feedback scope binding ──

// domainsFake adds the domainLister capability.
type domainsFake struct {
	fakeStore
	domains []string
}

func (d domainsFake) Domains(ctx context.Context) ([]string, error) { return d.domains, nil }

// Proofs 14+15: feedback capability binds to the EFFECTIVE scope — a domain mismatch is typed
// unavailable with the Phase 9.6 reason; an EMPTY requested domain first resolves through the
// single-domain rule and only then matches.
func TestTrust_FeedbackBoundToEffectiveScope(t *testing.T) {
	repoCtx := &briefingRepositoryContext{Root: t.TempDir(), Domain: "github.com/globulario/sensei"}

	// Mismatch: effective scope names another repository.
	s := &server{store: domainsFake{fakeStore: controlFakeStore(), domains: []string{"github.com/other/repo"}}, briefingRepo: repoCtx}
	resp, err := s.GetArchitectureControlSnapshot(context.Background(), &awarenesspb.GetArchitectureControlSnapshotRequest{
		RepositoryIdentity: ctrlRepo, Domain: "github.com/other/repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetSnapshot().GetFeedbackContext() != nil {
		t.Fatal("a mismatched feedback scope must withhold the payload")
	}
	mismatch := false
	for _, src := range resp.GetSnapshot().GetMeta().GetSources() {
		if src.GetOwner() == "briefingfeedback" &&
			src.GetAvailability() == awarenesspb.ArchitectureSourceAvailability_ARCHITECTURE_SOURCE_AVAILABILITY_UNAVAILABLE &&
			src.GetReasonCode() == string(briefingfeedback.RepositoryContextDomainMismatch) {
			mismatch = true
		}
	}
	if !mismatch {
		t.Fatal("the mismatch must be a typed unavailable source with the Phase 9.6 reason")
	}

	// Empty requested domain: resolves through the single-domain rule FIRST, then matches.
	s2 := &server{store: domainsFake{fakeStore: controlFakeStore(), domains: []string{"github.com/globulario/sensei"}}, briefingRepo: repoCtx}
	resp2, err := s2.GetArchitectureControlSnapshot(context.Background(), &awarenesspb.GetArchitectureControlSnapshotRequest{
		RepositoryIdentity: ctrlRepo, // Domain intentionally empty
	})
	if err != nil {
		t.Fatal(err)
	}
	fc := resp2.GetSnapshot().GetFeedbackContext()
	if fc == nil || !fc.GetCapable() {
		t.Fatal("an empty domain resolving to the configured repository must yield feedback capability")
	}
}

// ── Deterministic lifecycle observation ──

// Proof 16: multiple DISTINCT governed statuses are a typed conflict — the lifecycle source is
// INVALID with lifecycle_status_ambiguous and the lifecycle stays unknown (never first-row-wins).
func TestTrust_ConflictingStatusesInvalidUnknown(t *testing.T) {
	fs := controlFakeStore()
	base := fs.describe
	fs.describe = func(ctx context.Context, iri string) ([]store.Triple, error) {
		if iri != "aw:conflicted" {
			return base(ctx, iri)
		}
		return []store.Triple{
			{Predicate: rdf.PropType, Object: rdf.ClassContract, ObjectIsIRI: true},
			{Predicate: rdf.PropStatus, Object: "governed"},
			{Predicate: rdf.PropStatus, Object: "deprecated"},
		}, nil
	}
	s := &server{store: fs}
	resp, err := s.GetArchitectureArtifactState(context.Background(), &awarenesspb.GetArchitectureArtifactStateRequest{
		RepositoryIdentity: ctrlRepo, NodeIri: "aw:conflicted",
	})
	if err != nil {
		t.Fatal(err)
	}
	lc := resp.GetState().GetLifecycle()
	if lc.GetState() != awarenesspb.ArchitectureLifecycleState_ARCHITECTURE_LIFECYCLE_STATE_UNKNOWN {
		t.Fatalf("conflicting statuses must yield unknown, got %v", lc.GetState())
	}
	if lc.GetSourceAvailability() != awarenesspb.ArchitectureSourceAvailability_ARCHITECTURE_SOURCE_AVAILABILITY_INVALID {
		t.Fatal("the conflicted lifecycle source must be INVALID")
	}
	if lc.GetReasonCode() != "lifecycle_status_ambiguous" {
		t.Fatalf("the typed conflict reason must be preserved, got %q", lc.GetReasonCode())
	}
}

// Proof 17: shuffled status/label fact order produces the IDENTICAL index digest (catalog-level
// determinism).
func TestTrust_ShuffledFactsSameCatalogDigest(t *testing.T) {
	build := func(seed int64) string {
		fs := controlFakeStore()
		baseClassFacts := fs.classFacts
		fs.classFacts = func(ctx context.Context, classIRI string, limit int) ([]store.ImpactFact, error) {
			facts, err := baseClassFacts(ctx, classIRI, limit)
			if err != nil {
				return nil, err
			}
			extra := []store.ImpactFact{}
			if classIRI == rdf.ClassContract {
				extra = []store.ImpactFact{
					{NodeIRI: "aw:multi", TypeIRI: classIRI, Predicate: rdf.PropType, Object: classIRI, ObjectIsIRI: true},
					{NodeIRI: "aw:multi", TypeIRI: classIRI, Predicate: rdf.PropLabel, Object: "Zed"},
					{NodeIRI: "aw:multi", TypeIRI: classIRI, Predicate: rdf.PropLabel, Object: "Alpha"},
					{NodeIRI: "aw:multi", TypeIRI: classIRI, Predicate: rdf.PropStatus, Object: "governed"},
					{NodeIRI: "aw:multi", TypeIRI: classIRI, Predicate: rdf.PropStatus, Object: "governed"},
				}
			}
			all := append(append([]store.ImpactFact{}, facts...), extra...)
			r := rand.New(rand.NewSource(seed))
			r.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
			return all, nil
		}
		s := &server{store: fs}
		resp, err := s.ListArchitectureArtifacts(context.Background(), &awarenesspb.ListArchitectureArtifactsRequest{
			RepositoryIdentity: ctrlRepo, PageSize: 50,
		})
		if err != nil {
			t.Fatal(err)
		}
		// The deterministic label rule picked the lexically-smallest candidate.
		for _, row := range resp.GetIndex().GetPage() {
			if row.GetIdentity().GetNodeIri() == "aw:multi" && row.GetLabel() != "Alpha" {
				t.Fatalf("label choice must be deterministic (lexical), got %q", row.GetLabel())
			}
		}
		return resp.GetIndex().GetMeta().GetDigestSha256()
	}
	first := build(1)
	for seed := int64(2); seed < 7; seed++ {
		if build(seed) != first {
			t.Fatal("shuffled fact order changed the catalog/index digest")
		}
	}
}

// Proof 3 (the headline repair, via the REAL independent-discovery path): a differing digest
// literal ATTACHED TO THE EXPECTED MARKER IRI is a marker identity contradiction — it can never
// masquerade as a stale-admissible live identity. It derives INTEGRITY-FAILED with the observed
// (tampered) identity and exposes no trusted rows, carrying the typed integrity reason on the
// sources.
func TestTrust_IntegrityFailedViaRealVerifierPath(t *testing.T) {
	expectedIRI := derivedMarkerIRI(testEmbeddedSeedMarker().Digest)
	const tamperedDigest = "tampered-live-digest"
	fs := controlFakeStore()
	// A marker sitting at the EXPECTED IRI but carrying a DIFFERENT digest literal: the subject
	// IRI no longer equals the digest-derived IRI → identity contradiction. Its count equals the
	// live count so the failure is proven to be the IRI/digest contradiction, not a count skew.
	fs.seedMarkerFacts = func(context.Context) []store.ImpactFact {
		return seedBuildMarkerFacts(expectedIRI, tamperedDigest, testEmbeddedSeedMarker().TripleCount)
	}
	s := &server{store: fs}
	resp, err := s.ListArchitectureArtifacts(context.Background(), &awarenesspb.ListArchitectureArtifactsRequest{
		RepositoryIdentity: ctrlRepo, PageSize: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	idx := resp.GetIndex()
	if len(idx.GetPage()) != 0 {
		t.Fatal("an integrity-failed authority must expose no trusted rows")
	}
	if a := idx.GetMeta().GetAvailability(); a == awarenesspb.ArchitectureAvailability_ARCHITECTURE_AVAILABILITY_AVAILABLE ||
		a == awarenesspb.ArchitectureAvailability_ARCHITECTURE_AVAILABILITY_PARTIAL {
		t.Fatalf("integrity failure must not read available/partial, got %v", a)
	}
	invalidWithReason := false
	for _, src := range idx.GetMeta().GetSources() {
		if src.GetOwner() == "graph_authority" &&
			src.GetAvailability() == awarenesspb.ArchitectureSourceAvailability_ARCHITECTURE_SOURCE_AVAILABILITY_INVALID &&
			src.GetReasonCode() == seedmeta.AuthorityReasonLiveMarkerIRIDigestMismatch {
			invalidWithReason = true
			if src.GetIdentity() != tamperedDigest {
				t.Fatalf("the independently observed identity must be preserved, got %q", src.GetIdentity())
			}
		}
	}
	if !invalidWithReason {
		t.Fatal("the authority source must be INVALID with the typed seedmeta integrity reason")
	}
	// A stale-admissible classification here would be the exact defect this repair closes: a
	// conflicting digest literal under the expected IRI must never read as a genuine live identity.
}
