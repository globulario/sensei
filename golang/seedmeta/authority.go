// SPDX-License-Identifier: AGPL-3.0-only

package seedmeta

import (
	"context"
	"sort"
	"strconv"

	"github.com/globulario/sensei/golang/store"
)

// Typed graph-authority admissibility (Phase 9.5 CP2 independent live-marker admission repair).
//
// AuthorityStaleAdmissible must come ONLY from a genuinely discovered, self-consistent live
// SeedBuild marker — never from a conflicting digest literal attached to the expected marker IRI.
// The freshness owner therefore discovers the live marker INDEPENDENTLY (by class, over the whole
// live store) rather than by looking up expected.IRI, and admits it only when it is internally
// coherent. AdmitLiveMarker is that derivation. It decides:
//
//   - whether a live authority identity was actually OBSERVED;
//   - whether that identity is ADMISSIBLE;
//   - whether marker/content INTEGRITY was established;
//   - WHY freshness is stale or unverifiable (typed reason).
//
// The expected marker is COMPARISON METADATA ONLY. LiveIdentity is populated exclusively from the
// independently discovered live marker digest — the expected value is never substituted as a
// fallback source of observed identity. Expected/observed digests may legitimately be EQUAL when
// the independent discovery proves that equality (the current case).

// AuthorityObservationState is the closed admissibility vocabulary.
type AuthorityObservationState int

const (
	// AuthorityUnobserved: no admissible live authority identity was established.
	AuthorityUnobserved AuthorityObservationState = iota
	// AuthorityCurrent: the independently discovered live marker matches the expected artifact.
	AuthorityCurrent
	// AuthorityStaleAdmissible: an internally coherent live marker WAS discovered at its own
	// digest-derived IRI, but it is not the expected artifact — an older/other verified graph.
	AuthorityStaleAdmissible
	// AuthorityIntegrityFailed: a live marker was discovered, but it is not self-consistent
	// (multiple markers, a subject IRI disagreeing with its digest, a missing count, or a count
	// disagreeing with the actual live graph) — the content cannot be trusted.
	AuthorityIntegrityFailed
)

// Typed authority reasons (closed; no prose).
const (
	AuthorityReasonExpectedMarkerAbsent        = "expected_marker_absent"
	AuthorityReasonLiveMarkerAbsent            = "live_marker_absent"
	AuthorityReasonLiveMarkerDigestMissing     = "live_marker_digest_missing"
	AuthorityReasonLiveMarkerDigestMismatch    = "live_marker_digest_mismatch"
	AuthorityReasonLiveMarkerCountMissing      = "live_marker_count_missing"
	AuthorityReasonLiveMarkerCountMismatch     = "live_marker_count_mismatch"
	AuthorityReasonLiveGraphCountMismatch      = "live_graph_count_mismatch"
	AuthorityReasonLiveMarkerIRIDigestMismatch = "live_marker_iri_digest_mismatch"
	AuthorityReasonMultipleLiveMarkers         = "multiple_live_markers"
	AuthorityReasonVerificationQueryFailed     = "verification_query_failed"
	AuthorityReasonLiveStoreEmpty              = "live_store_empty"
	AuthorityReasonVerificationUnclassified    = "verification_unclassified"
)

// AuthorityObservation is the typed admissibility result.
type AuthorityObservation struct {
	State AuthorityObservationState
	// Reason is the typed cause for every non-current state ("" only for current).
	Reason string
	// LiveIdentity is the INDEPENDENTLY DISCOVERED live marker digest — never the expected
	// digest substituted as a fallback. Empty exactly when no admissible/observed live marker
	// identity was established.
	LiveIdentity string
}

// LiveMarker is one independently discovered live SeedBuild marker candidate.
type LiveMarker struct {
	IRI         string
	Digest      string
	TripleCount int64
}

// markerDiscoveryLimit bounds the number of distinct SeedBuild markers fetched during discovery.
// One canonical marker is expected; the bound only needs to be large enough to observe that more
// than one exists (an integrity failure) while keeping the query cheap.
const markerDiscoveryLimit = 16

// MarkerDiscoverer independently discovers live SeedBuild markers BY CLASS (never by looking up
// the expected IRI) and reports the actual live graph triple count. It is satisfied structurally
// by any store exposing ClassFacts + CountTriples (the existing repository convention).
type MarkerDiscoverer interface {
	ClassFacts(ctx context.Context, classIRI string, limit int) ([]store.ImpactFact, error)
	CountTriples(ctx context.Context) (int64, error)
}

// DiscoverLiveMarkers independently enumerates the live SeedBuild markers (subject IRI, digest
// literal, marker triple count) and the total live graph count. Markers are returned in
// deterministic (sorted-IRI) order; the length of the returned slice is the number of live
// SeedBuild markers observed.
func DiscoverLiveMarkers(ctx context.Context, disc MarkerDiscoverer) ([]LiveMarker, int64, error) {
	liveCount, err := disc.CountTriples(ctx)
	if err != nil {
		return nil, 0, err
	}
	facts, err := disc.ClassFacts(ctx, markerClassIRI, markerDiscoveryLimit)
	if err != nil {
		return nil, 0, err
	}
	byIRI := map[string]*LiveMarker{}
	order := make([]string, 0)
	for _, f := range facts {
		if f.NodeIRI == "" {
			continue
		}
		m := byIRI[f.NodeIRI]
		if m == nil {
			m = &LiveMarker{IRI: f.NodeIRI}
			byIRI[f.NodeIRI] = m
			order = append(order, f.NodeIRI)
		}
		switch f.Predicate {
		case markerDigestIRI:
			if !f.ObjectIsIRI {
				m.Digest = f.Object
			}
		case markerTripleCountIRI:
			if n, perr := strconv.ParseInt(f.Object, 10, 64); perr == nil && n >= 0 {
				m.TripleCount = n
			}
		}
	}
	sort.Strings(order)
	out := make([]LiveMarker, 0, len(order))
	for _, iri := range order {
		out = append(out, *byIRI[iri])
	}
	return out, liveCount, nil
}

// AdmitLiveMarker derives the typed admissibility from an INDEPENDENT live-marker discovery.
// The expected marker is comparison metadata only; a differing digest literal is stale-admissible
// ONLY when it belongs to a self-consistent marker at its own digest-derived IRI. Any marker
// identity contradiction is integrity-failed. Fail-closed throughout.
func AdmitLiveMarker(ctx context.Context, disc MarkerDiscoverer, expected Marker) AuthorityObservation {
	// The expected marker is comparison metadata. Without it no authority relationship can be
	// established (an absent expected marker is not a live observation).
	if expected.Digest == "" || expected.IRI == "" || expected.TripleCount <= 0 {
		return AuthorityObservation{State: AuthorityUnobserved, Reason: AuthorityReasonExpectedMarkerAbsent}
	}

	markers, liveCount, err := DiscoverLiveMarkers(ctx, disc)
	if err != nil {
		return AuthorityObservation{State: AuthorityUnobserved, Reason: AuthorityReasonVerificationQueryFailed}
	}
	if liveCount <= 0 {
		return AuthorityObservation{State: AuthorityUnobserved, Reason: AuthorityReasonLiveStoreEmpty}
	}
	switch len(markers) {
	case 0:
		return AuthorityObservation{State: AuthorityUnobserved, Reason: AuthorityReasonLiveMarkerAbsent}
	case 1:
		// Exactly one canonical candidate — proceed to admissibility.
	default:
		// More than one live SeedBuild marker: the live authority identity is ambiguous.
		return AuthorityObservation{State: AuthorityIntegrityFailed, Reason: AuthorityReasonMultipleLiveMarkers}
	}
	m := markers[0]

	if m.Digest == "" {
		return AuthorityObservation{State: AuthorityUnobserved, Reason: AuthorityReasonLiveMarkerDigestMissing}
	}
	// Marker identity coherence: the subject IRI MUST be the canonical digest-derived IRI. A
	// digest literal attached to a non-derived subject (e.g. the expected IRI) is a marker
	// identity contradiction — integrity-failed, never stale-admissible.
	if m.IRI != NamespaceIRI+"seedBuild/sha256-"+m.Digest {
		return AuthorityObservation{State: AuthorityIntegrityFailed, Reason: AuthorityReasonLiveMarkerIRIDigestMismatch, LiveIdentity: m.Digest}
	}
	if m.TripleCount <= 0 {
		return AuthorityObservation{State: AuthorityIntegrityFailed, Reason: AuthorityReasonLiveMarkerCountMissing, LiveIdentity: m.Digest}
	}
	// The marker's self-declared count must equal the ACTUAL live graph triple count.
	if m.TripleCount != liveCount {
		return AuthorityObservation{State: AuthorityIntegrityFailed, Reason: AuthorityReasonLiveGraphCountMismatch, LiveIdentity: m.Digest}
	}

	// The live marker is now internally coherent and self-consistent with the live graph.
	// Compare the independently admitted live marker to the expected marker (metadata only).
	if m.Digest == expected.Digest {
		if m.TripleCount != expected.TripleCount {
			// Same content digest but a contradicting expected size: a marker/expected
			// contradiction, not a genuine older graph.
			return AuthorityObservation{State: AuthorityIntegrityFailed, Reason: AuthorityReasonLiveMarkerCountMismatch, LiveIdentity: m.Digest}
		}
		return AuthorityObservation{State: AuthorityCurrent, LiveIdentity: m.Digest}
	}
	// A genuinely different, internally coherent live marker at its own digest-derived IRI.
	return AuthorityObservation{State: AuthorityStaleAdmissible, Reason: AuthorityReasonLiveMarkerDigestMismatch, LiveIdentity: m.Digest}
}
