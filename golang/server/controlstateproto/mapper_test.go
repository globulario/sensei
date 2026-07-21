// SPDX-License-Identifier: AGPL-3.0-only

package controlstateproto

import (
	"reflect"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/controlstate"
	"github.com/globulario/sensei/golang/rdf"
)

const (
	rtRepo = "github.com/globulario/sensei"
	rtAuth = "auth-digest-rt"
)

func rtCatalog(t *testing.T, reg controlstate.Registry) controlstate.CatalogSnapshot {
	t.Helper()
	cat, err := controlstate.BuildCatalogSnapshot(reg, controlstate.CatalogScope{
		RepositoryIdentity: rtRepo, DomainIdentity: rtRepo, GraphAuthorityIdentity: rtAuth,
		SnapshotIdentity: "snap-rt",
		Source: controlstate.SourceStatus{Owner: "controlstate.catalog", Schema: "catalog_enumeration", Identity: "snap-rt",
			Availability: controlstate.SourceAvailable, Impact: controlstate.ImpactPrimary},
		AuthoritySource: controlstate.SourceStatus{Owner: "graph_authority", Schema: "graph_authority", Identity: rtAuth,
			Availability: controlstate.SourceAvailable, Impact: controlstate.ImpactRequired},
		DiscoverySource: controlstate.SourceStatus{Owner: "controlstate.catalog", Schema: "unclassified_discovery", Identity: "snap-rt",
			Availability: controlstate.SourceAvailable, Impact: controlstate.ImpactRelevant},
	}, []controlstate.CatalogArtifactObservation{
		{NodeIRI: "aw:c1", Label: "Contract", ObservedClasses: []string{rdf.ClassContract},
			Lifecycle: controlstate.LifecycleSource{Observed: true, Availability: controlstate.SourceAvailable, Owner: "governed", Schema: "governed_status", Identity: "aw:c1", Status: "governed"}},
		{NodeIRI: "aw:f1", Label: "file.go", ObservedClasses: []string{rdf.ClassSourceFile}},
		{NodeIRI: "aw:i1", Label: "Intent", ObservedClasses: []string{rdf.ClassIntent}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

// rtSnapshot builds a RICH canonical snapshot: an observed zero count, an unobserved (nil)
// count, coverage, an attention item, a feedback context, and catalog tallies.
func rtSnapshot(t *testing.T) controlstate.ControlSnapshot {
	t.Helper()
	reg := controlstate.DefaultRegistry()
	item, _, err := controlstate.AttentionForGraphAuthority(rtAuth, "", true, false, true, []string{"aw:c1"})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := controlstate.BuildControlSnapshot(reg, controlstate.ControlSnapshotInput{
		RepositoryIdentity: rtRepo, Domain: rtRepo,
		Authority: controlstate.GraphAuthoritySummary{Observed: true, Current: true, Integrity: true, Identity: rtAuth},
		Catalog:   rtCatalog(t, reg),
		Attention: controlstate.AttentionObservation{Owner: "controlstate.attention", Schema: "attention",
			Identity: "attention:rt", Availability: controlstate.SourceAvailable,
			Items: []controlstate.AttentionItem{item}},
		// An OBSERVED zero (data) …
		OpenQuestions: &controlstate.CountObservation{Owner: "questiondisposition", Schema: "oq", Identity: "count:oq",
			Availability: controlstate.SourceAvailable, Count: 0},
		// … while contradictions / evidence / tests / enforcement stay UNOBSERVED (nil).
		Coverage: &controlstate.CoverageObservation{Owner: "coverage", Schema: "cov", Identity: "coverage:rt",
			Availability: controlstate.SourceAvailable,
			Summary:      controlstate.CoverageSummary{Sufficient: false, BlindSpotCount: 3, HighRiskBlind: 1}},
		Feedback: &controlstate.FeedbackObservation{Owner: "briefingfeedback", Schema: "fb", Identity: "fb:rt",
			Availability: controlstate.SourceAvailable,
			Context:      controlstate.FeedbackContext{Capable: true, Availability: "feedback_available"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// rtArtifactState builds a rich canonical artifact state: open contradiction, one definitive
// blocker on a degraded source, unknown dimensions, attention, lifecycle, exact-scope feedback.
func rtArtifactState(t *testing.T) controlstate.ArtifactState {
	t.Helper()
	reg := controlstate.DefaultRegistry()
	id, res, err := controlstate.BuildArtifactIdentity(reg, "aw:c1", []string{rdf.ClassContract}, rtRepo, rtRepo, rtAuth, []string{"prov:a"})
	if err != nil {
		t.Fatal(err)
	}
	st, err := controlstate.BuildArtifactState(reg, id, res, controlstate.ArtifactSourceBundle{
		GraphAuthority: controlstate.GraphAuthorityObservation{Observed: true, Current: true, Integrity: true, Identity: rtAuth},
		Contradiction: controlstate.ContradictionSource{Owner: "extractor.contradiction", Schema: "contradiction",
			Identity: "contra:rt", Availability: controlstate.SourceAvailable,
			Findings: []controlstate.ContradictionObservation{{Identity: "contra.finding.1", Relevant: true}}},
		Dimensions: map[string]controlstate.DimensionObservation{
			"enforcement": {Dimension: "enforcement", SourceOwner: "closure.enforcement", SourceSchema: "dim",
				SourceIdentity: "s.en", SourceAvailability: controlstate.SourceDegraded, SourceReasonCode: "stale",
				Outcome: controlstate.OutcomeDefinitiveBlocker, BlockerIDs: []string{"blk.1"}},
		},
		Lifecycle: controlstate.LifecycleSource{Observed: true, Availability: controlstate.SourceAvailable,
			Owner: "governed", Schema: "governed_status", Identity: "aw:c1", Status: "governed"},
		Feedback: &controlstate.ScopedFeedbackRef{ScopeIdentity: "scope:rt", ProjectionDigest: "digest:rt",
			Availability: "feedback_available", VerifiedRecordIDs: []string{"invariant:x"}, LineageIDs: []string{"q.1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// Proof: the snapshot maps without semantic loss — the reconstructed model revalidates against
// its own digest (content-losslessness) and DeepEquals the original; nil counts stay absent,
// an observed zero stays zero; digest is copied verbatim.
func TestRoundTrip_ControlSnapshotLossless(t *testing.T) {
	snap := rtSnapshot(t)
	wire, err := ToProtoControlSnapshot(snap)
	if err != nil {
		t.Fatal(err)
	}
	if wire.GetMeta().GetDigestSha256() != snap.DigestSHA256 {
		t.Fatal("wire digest must be the canonical digest, copied verbatim")
	}
	// nil-versus-zero on the wire.
	if wire.OpenQuestionCount == nil || *wire.OpenQuestionCount != 0 {
		t.Fatal("an observed zero must be present as an explicit zero")
	}
	if wire.ContradictionCount != nil || wire.MissingEvidenceCount != nil || wire.MissingTestCount != nil || wire.MissingEnforcementCount != nil {
		t.Fatal("unobserved counts must stay absent on the wire")
	}
	back, err := fromProtoControlSnapshot(wire)
	if err != nil {
		t.Fatal(err)
	}
	// The reconstructed model passes canonical validation — its content recomputes to the SAME
	// digest that was copied across the wire.
	if err := controlstate.ValidateControlSnapshot(back); err != nil {
		t.Fatalf("round-tripped snapshot failed canonical validation (semantic loss): %v", err)
	}
	if !reflect.DeepEqual(snap, back) {
		t.Fatal("round trip must preserve the exact canonical model")
	}
}

// Proof: the artifact state maps without semantic loss (dimensions, blockers, attention,
// lifecycle, exact-scope feedback, sources with impacts and reasons, canonical order).
func TestRoundTrip_ArtifactStateLossless(t *testing.T) {
	st := rtArtifactState(t)
	if st.Closure != controlstate.ClosureOpen {
		t.Fatalf("fixture should be open (contradiction + blocker), got %q", st.Closure)
	}
	wire, err := ToProtoArtifactState(st)
	if err != nil {
		t.Fatal(err)
	}
	if wire.GetMeta().GetDigestSha256() != st.DigestSHA256 {
		t.Fatal("wire digest must be the canonical digest, copied verbatim")
	}
	back, err := fromProtoArtifactState(wire)
	if err != nil {
		t.Fatal(err)
	}
	if err := controlstate.ValidateArtifactState(back); err != nil {
		t.Fatalf("round-tripped artifact state failed canonical validation (semantic loss): %v", err)
	}
	if !reflect.DeepEqual(st, back) {
		t.Fatal("round trip must preserve the exact canonical model")
	}
}

// Proof: partial stays partial and unavailable stays unavailable across the wire; source
// impacts, identities, and typed reasons survive mapping.
func TestRoundTrip_AvailabilityAndSourcesSurvive(t *testing.T) {
	reg := controlstate.DefaultRegistry()
	// A partial snapshot: no coverage/count sources.
	partial, err := controlstate.BuildControlSnapshot(reg, controlstate.ControlSnapshotInput{
		RepositoryIdentity: rtRepo,
		Authority:          controlstate.GraphAuthoritySummary{Observed: true, Current: true, Integrity: true, Identity: rtAuth},
		Catalog:            rtCatalog(t, reg),
		Attention: controlstate.AttentionObservation{Owner: "controlstate.attention", Schema: "attention",
			Identity: "attention:rt", Availability: controlstate.SourceAvailable},
	})
	if err != nil {
		t.Fatal(err)
	}
	if partial.Availability != controlstate.AvailabilityPartial {
		t.Fatalf("fixture must be partial, got %q", partial.Availability)
	}
	wire, err := ToProtoControlSnapshot(partial)
	if err != nil {
		t.Fatal(err)
	}
	back, err := fromProtoControlSnapshot(wire)
	if err != nil {
		t.Fatal(err)
	}
	if back.Availability != controlstate.AvailabilityPartial {
		t.Fatal("partial must remain partial across the wire")
	}
	// Typed unavailable sources survive with impact + reason.
	foundUnavailable := false
	for _, s := range back.Sources {
		if s.Availability == controlstate.SourceUnavailable && s.Impact == controlstate.ImpactRelevant && s.ReasonCode == "source_not_observed" {
			foundUnavailable = true
		}
	}
	if !foundUnavailable {
		t.Fatal("typed unavailable relevant sources must survive mapping with their reasons")
	}
	if !reflect.DeepEqual(partial.Sources, back.Sources) {
		t.Fatal("the source ledger must survive mapping exactly (order, impacts, identities, reasons)")
	}
}

// Proof: the index maps losslessly; the opaque cursor survives unchanged; unknown closure stays
// unknown (never closed/unspecified); the page order survives.
func TestRoundTrip_IndexCursorAndUnknownClosure(t *testing.T) {
	reg := controlstate.DefaultRegistry()
	cat := rtCatalog(t, reg)
	idx, err := controlstate.BuildArtifactIndex(reg, controlstate.ArtifactIndexRequest{
		RepositoryIdentity: rtRepo, Domain: rtRepo, PageSize: 2,
	}, cat)
	if err != nil {
		t.Fatal(err)
	}
	if !idx.Truncated || idx.NextCursor == "" {
		t.Fatalf("fixture must paginate: trunc=%v", idx.Truncated)
	}
	wire, err := ToProtoArtifactIndex(reg, idx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if wire.GetNextCursor() != idx.NextCursor {
		t.Fatal("the opaque cursor must survive unchanged")
	}
	if wire.GetMeta().GetDigestSha256() != idx.DigestSHA256 {
		t.Fatal("wire digest must be the canonical digest")
	}
	if len(wire.GetPage()) != len(idx.Page) {
		t.Fatal("page length must survive")
	}
	for i, row := range idx.Page {
		if wire.GetPage()[i].GetIdentity().GetNodeIri() != row.Identity.NodeIRI {
			t.Fatal("canonical page order must survive mapping")
		}
	}
	// The assessable contract row is honestly unknown — and stays unknown on the wire.
	for i, row := range idx.Page {
		if row.Class == rdf.ClassContract {
			if wire.GetPage()[i].GetClosure().String() != "ARCHITECTURE_ARTIFACT_CLOSURE_UNKNOWN" {
				t.Fatalf("unknown closure must stay unknown on the wire, got %v", wire.GetPage()[i].GetClosure())
			}
		}
	}
}

// Proof: the navigation descriptor maps losslessly (families, classes, fallback, digest).
func TestRoundTrip_NavigationDescriptor(t *testing.T) {
	d, err := controlstate.BuildNavigationDescriptor(controlstate.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	wire, err := ToProtoNavigationDescriptor(d)
	if err != nil {
		t.Fatal(err)
	}
	if wire.GetMeta().GetDigestSha256() != d.DigestSHA256 || wire.GetRegistryDigest() != d.RegistryDigest {
		t.Fatal("descriptor digests must be copied verbatim")
	}
	if len(wire.GetFamilies()) != len(d.Families) {
		t.Fatal("families must survive")
	}
	for i, f := range d.Families {
		if wire.GetFamilies()[i].GetId() != f.ID || len(wire.GetFamilies()[i].GetClasses()) != len(f.Classes) {
			t.Fatal("family order/classes must survive")
		}
	}
	if !wire.GetUnknownClassFallback().GetDefaultVisible() {
		t.Fatal("the unknown-class fallback must stay visible on the wire")
	}
}

// Proof: mapping failures are ERRORS, never silent omissions — a malformed nested attention
// item, an off-vocabulary enum value, and a digest-tampered projection are all rejected.
func TestMapper_FailsClosed(t *testing.T) {
	if _, err := ToProtoAttentionItem(controlstate.AttentionItem{}); err == nil {
		t.Fatal("a malformed attention item must be rejected, not omitted")
	}
	// Off-vocabulary severity inside an otherwise-plausible item.
	item, _, err := controlstate.AttentionForGraphAuthority(rtAuth, "", true, false, true, []string{"aw:c1"})
	if err != nil {
		t.Fatal(err)
	}
	bad := item
	bad.Severity = "shiny" // off-vocabulary
	if _, err := ToProtoAttentionItem(bad); err == nil {
		t.Fatal("an off-vocabulary severity must be rejected")
	}
	// A tampered projection (content no longer matches its canonical digest) is rejected
	// BEFORE mapping — the transport never repairs or recomputes a digest.
	snap := rtSnapshot(t)
	snap.RegistryDigest = "tampered"
	if _, err := ToProtoControlSnapshot(snap); err == nil {
		t.Fatal("a digest-mismatched projection must be rejected before mapping")
	}
	st := rtArtifactState(t)
	st.Closure = controlstate.ClosureClosed // reclassification without re-digest
	if _, err := ToProtoArtifactState(st); err == nil {
		t.Fatal("a reclassified artifact state must be rejected before mapping")
	}
}

// Proof (Phase 9.7 CP3): a boundary's runtime-boundary dimension and its OWNER-severity violation
// attention ride the generic transport verbatim — the runtime dimension, its blocker, and the
// runtime_boundary_violated critical/source_severity attention item survive a full round trip. The
// transport adds no runtime vocabulary; it carries whatever the runtimeboundary owner produced.
func TestRoundTrip_RuntimeBoundaryDimensionSurvives(t *testing.T) {
	reg := controlstate.DefaultRegistry()
	id, res, err := controlstate.BuildArtifactIdentity(reg, "aw:b1", []string{rdf.ClassBoundary}, rtRepo, rtRepo, rtAuth, nil)
	if err != nil {
		t.Fatal(err)
	}
	st, err := controlstate.BuildArtifactState(reg, id, res, controlstate.ArtifactSourceBundle{
		GraphAuthority: controlstate.GraphAuthorityObservation{Observed: true, Current: true, Integrity: true, Identity: rtAuth},
		Contradiction: controlstate.ContradictionSource{Owner: "extractor.contradiction", Schema: "contradiction",
			Identity: "contra:b1", Availability: controlstate.SourceAvailable},
		Dimensions: map[string]controlstate.DimensionObservation{
			"runtime": {Dimension: "runtime", SourceOwner: "runtimeboundary", SourceSchema: "runtime.boundary_assessment/v1",
				SourceIdentity: "aw:b1", SourceAvailability: controlstate.SourceAvailable, SourceReasonCode: "crossing_forbidden",
				Outcome: controlstate.OutcomeDefinitiveBlocker, BlockerIDs: []string{"aw:b1"},
				SourceSeverity: controlstate.SeverityCritical, NextActionOwner: "architect"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The composed state must carry the runtime dimension as an open blocker with an OWNER-severity
	// (source_severity) critical runtime_boundary_violated attention item.
	var sawDim, sawAttn bool
	for _, d := range st.Dimensions {
		if d.Dimension == "runtime" && d.State == controlstate.DimOpen {
			sawDim = true
		}
	}
	for _, a := range st.Attention {
		if a.AttentionClass == controlstate.AttnRuntimeBoundaryViolated &&
			a.Severity == controlstate.SeverityCritical && a.SeverityBasis == "source_severity" {
			sawAttn = true
		}
	}
	if !sawDim || !sawAttn {
		t.Fatalf("runtime dimension/attention not composed (dim=%v attn=%v)", sawDim, sawAttn)
	}
	wire, err := ToProtoArtifactState(st)
	if err != nil {
		t.Fatal(err)
	}
	back, err := fromProtoArtifactState(wire)
	if err != nil {
		t.Fatal(err)
	}
	if err := controlstate.ValidateArtifactState(back); err != nil {
		t.Fatalf("round-tripped runtime state failed canonical validation: %v", err)
	}
	if !reflect.DeepEqual(st, back) {
		t.Fatal("the runtime dimension + owner-severity attention must survive transport verbatim")
	}
}

// Proof: non_authoritative_projection survives as TRUE on every wire projection, and no wire
// output carries certification claims or scores.
func TestMapper_NonAuthoritativeAndNoCertification(t *testing.T) {
	snap := rtSnapshot(t)
	wire, err := ToProtoControlSnapshot(snap)
	if err != nil {
		t.Fatal(err)
	}
	if !wire.GetMeta().GetNonAuthoritativeProjection() {
		t.Fatal("non_authoritative_projection must remain true on the wire")
	}
	blob := strings.ToLower(wire.String())
	for _, needle := range []string{"certified", "\"score\""} {
		if strings.Contains(blob, needle) {
			t.Fatalf("wire snapshot must not carry %q", needle)
		}
	}
	stWire, err := ToProtoArtifactState(rtArtifactState(t))
	if err != nil {
		t.Fatal(err)
	}
	if !stWire.GetMeta().GetNonAuthoritativeProjection() {
		t.Fatal("artifact state must remain non-authoritative on the wire")
	}
}
