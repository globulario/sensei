// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/globulario/sensei/golang/graphgeneration"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store"
)

type graphFreshnessSnapshot struct {
	verification seedmeta.Verification
}

type graphFreshnessProvider interface {
	GraphFreshness(context.Context) seedmeta.Verification
}

func snapshotGraphFreshness(ctx context.Context, s *server) graphFreshnessSnapshot {
	snap := graphFreshnessSnapshot{}
	if s == nil || s.store == nil {
		snap.verification = seedmeta.Verification{
			State:  seedmeta.FreshnessCheckError,
			Detail: "store is unavailable",
		}
		return snap
	}
	if provider, ok := s.store.(graphFreshnessProvider); ok {
		snap.verification = provider.GraphFreshness(ctx)
		return snap
	}
	expected, detail, ok := expectedGraphMarker(s)
	if !ok {
		snap.verification = seedmeta.Verification{
			State:  seedmeta.FreshnessUnknown,
			Detail: detail,
		}
		return snap
	}
	snap.verification.Expected = expected
	verifier, ok := s.store.(interface {
		Describe(context.Context, string) ([]store.Triple, error)
		CountTriples(context.Context) (int64, error)
		CountByClass(context.Context, string) (int64, error)
	})
	if !ok {
		snap.verification = seedmeta.Verification{
			State:    seedmeta.FreshnessUnknown,
			Expected: expected,
			Detail:   "store backend cannot verify graph identity",
		}
		return snap
	}
	snap.verification = seedmeta.VerifyLiveStore(ctx, verifier, expected)
	return snap
}

// expectedMarkerProvenance names WHERE the expected marker came from, in the
// words a reader needs to act.
//
// A freshness refusal used to name one hash and nothing else:
//
//	live store missing expected graph marker f87c6912...
//
// True, fail-closed, and nearly useless. The hash appears in neither the store
// nor the marker file, so the reader has no way to learn that the expectation
// came from a store-scoped proof set published DAYS EARLIER, possibly by a
// different project, and that rebuilding cannot help. Measured 2026-09-02:
// three abandoned attempts and a wrong public diagnosis, on a store whose
// marker file and contents agreed with each other exactly.
//
// The proof set already records PublishedUnix and PublishedDomain. Saying so
// costs one line and identifies BOTH sides of the comparison instead of one.
func expectedMarkerProvenance(s *server) string {
	if s == nil || strings.TrimSpace(s.oxigraphQueryURL) == "" {
		return ""
	}
	dir, err := graphgeneration.Dir(s.oxigraphQueryURL)
	if err != nil {
		return ""
	}
	set, err := graphgeneration.Load(dir)
	if err != nil || set == nil || set.Marker.Digest == "" {
		return ""
	}
	when := "an unrecorded time"
	if set.Generation.PublishedUnix > 0 {
		when = time.Unix(set.Generation.PublishedUnix, 0).UTC().Format(time.RFC3339)
	}
	by := set.Generation.PublishedDomain
	if strings.TrimSpace(by) == "" {
		by = "an unnamed domain"
	}
	return fmt.Sprintf("expectation comes from the store's published generation at %s, "+
		"published %s by %s; if that publication is not this project's, rebuilding this "+
		"project will not change it", dir, when, by)
}

func expectedGraphMarker(s *server) (seedmeta.Marker, string, bool) {
	// The marker is a property of the STORE, not of this repository.
	//
	// Reading it from a per-repository file is one half of the defect in #176:
	// publishing any domain recomputes the whole-graph marker and rewrites only
	// the built repository's copy, so every other server keeps comparing the
	// live store against a marker that no longer describes it and reports
	// "missing expected graph marker". Prefer the store's own published
	// generation, which every server reading that store resolves identically.
	if marker, ok := storeScopedExpectedMarker(s); ok {
		return marker, "", true
	}
	if s != nil && strings.TrimSpace(s.graphMarkerFile) != "" {
		marker, err := seedmeta.ReadMarkerFile(s.graphMarkerFile)
		if err != nil {
			if os.IsNotExist(err) {
				return seedmeta.Marker{}, fmt.Sprintf("runtime graph marker file missing: %s", s.graphMarkerFile), false
			}
			return seedmeta.Marker{}, fmt.Sprintf("read runtime graph marker file %s: %v", s.graphMarkerFile, err), false
		}
		return marker, "", true
	}
	expected, ok := normalizedEmbeddedSeedMarker()
	if !ok {
		return seedmeta.Marker{}, "embedded seed carries no graph marker", false
	}
	return expected, "", true
}

// snapshotLiveAuthority derives the control-panel graph-authority admissibility by INDEPENDENTLY
// discovering the live SeedBuild marker from the store (never by trusting a handed-in freshness
// verification's Live identity, and never by looking up the expected IRI). The expected marker is
// comparison metadata only. It is intentionally SEPARATE from snapshotGraphFreshness: freshness
// answers "is the live graph the expected artifact"; authority answers "did we independently
// observe a self-consistent live authority identity, and is it admissible".
func snapshotLiveAuthority(ctx context.Context, s *server) (seedmeta.AuthorityObservation, seedmeta.Marker) {
	expected, _, ok := expectedGraphMarker(s)
	if !ok {
		return seedmeta.AuthorityObservation{State: seedmeta.AuthorityUnobserved, Reason: seedmeta.AuthorityReasonExpectedMarkerAbsent}, seedmeta.Marker{}
	}
	disc, ok := storeAsMarkerDiscoverer(s)
	if !ok {
		return seedmeta.AuthorityObservation{State: seedmeta.AuthorityUnobserved, Reason: seedmeta.AuthorityReasonVerificationUnclassified}, expected
	}
	return seedmeta.AdmitLiveMarker(ctx, disc, expected), expected
}

// storeAsMarkerDiscoverer adapts the server store to the seedmeta marker-discovery capability
// (ClassFacts + CountTriples). A nil store yields no discoverer.
func storeAsMarkerDiscoverer(s *server) (seedmeta.MarkerDiscoverer, bool) {
	if s == nil || s.store == nil {
		return nil, false
	}
	disc, ok := s.store.(seedmeta.MarkerDiscoverer)
	return disc, ok
}

func graphFreshnessStateProto(state seedmeta.FreshnessState) awarenesspb.GraphFreshnessState {
	switch state {
	case seedmeta.FreshnessCurrent:
		return awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT
	case seedmeta.FreshnessStale:
		return awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_STALE
	case seedmeta.FreshnessUnknown:
		return awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_UNKNOWN
	case seedmeta.FreshnessEmpty:
		return awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_EMPTY
	case seedmeta.FreshnessCheckError:
		return awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CHECK_ERROR
	default:
		return awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_UNSPECIFIED
	}
}

func (s *server) requireCurrentGraphAuthority(ctx context.Context, surface string) error {
	snap := snapshotGraphFreshness(ctx, s)
	switch snap.verification.State {
	case seedmeta.FreshnessCurrent:
		return nil
	case seedmeta.FreshnessCheckError:
		return status.Errorf(codes.Unavailable, "graph freshness check error for %s: %s", surface, snap.verification.Detail)
	case seedmeta.FreshnessEmpty:
		return status.Errorf(codes.FailedPrecondition, "graph freshness empty for %s: %s", surface, snap.verification.Detail)
	case seedmeta.FreshnessStale:
		return status.Errorf(codes.FailedPrecondition, "graph freshness stale for %s: %s%s",
			surface, snap.verification.Detail, detailSuffix(expectedMarkerProvenance(s)))
	case seedmeta.FreshnessUnknown:
		return status.Errorf(codes.FailedPrecondition, "graph freshness unknown for %s: %s%s",
			surface, snap.verification.Detail, detailSuffix(expectedMarkerProvenance(s)))
	default:
		return status.Errorf(codes.FailedPrecondition, "graph freshness unspecified for %s", surface)
	}
}

// detailSuffix appends provenance only when there is some. An empty suffix
// leaves the message exactly as it was, so a deployment with no published
// generation reads no differently than before.
func detailSuffix(p string) string {
	if strings.TrimSpace(p) == "" {
		return ""
	}
	return " — " + p
}

func graphFreshnessSeedState(ver seedmeta.Verification) awarenesspb.SeedState {
	if ver.Expected.Digest == "" {
		return awarenesspb.SeedState_SEED_STATE_UNSTAMPED
	}
	if ver.State == seedmeta.FreshnessCurrent {
		return awarenesspb.SeedState_SEED_STATE_CURRENT
	}
	return awarenesspb.SeedState_SEED_STATE_STALE
}

func graphFreshnessSummary(ver seedmeta.Verification) string {
	switch ver.State {
	case seedmeta.FreshnessCurrent:
		if ver.Content == seedmeta.ContentMatch {
			return fmt.Sprintf("current digest=%s triples=%d content=verified", ver.Expected.Digest, ver.Expected.TripleCount)
		}
		// Naming the weaker evidence is the point: this summary is quoted into
		// operator output, and "current" from a count comparison is not the
		// same finding as "current" from a recomputed content digest.
		return fmt.Sprintf("current digest=%s triples=%d content=%s", ver.Expected.Digest, ver.Expected.TripleCount, ver.Content)
	case seedmeta.FreshnessStale:
		return "stale: " + ver.Detail
	case seedmeta.FreshnessEmpty:
		return "empty: " + ver.Detail
	case seedmeta.FreshnessCheckError:
		return "check_error: " + ver.Detail
	case seedmeta.FreshnessUnknown:
		return "unknown: " + ver.Detail
	default:
		return ver.Detail
	}
}

// storeScopedExpectedMarker resolves the marker from the store's published
// generation rather than from this repository's copy of it.
//
// Returns false when no proof set has been published for this store yet, so a
// deployment that has not rebuilt since the change keeps working through the
// per-repository path.
func storeScopedExpectedMarker(s *server) (seedmeta.Marker, bool) {
	if s == nil || strings.TrimSpace(s.oxigraphQueryURL) == "" {
		return seedmeta.Marker{}, false
	}
	dir, err := graphgeneration.Dir(s.oxigraphQueryURL)
	if err != nil {
		return seedmeta.Marker{}, false
	}
	set, err := graphgeneration.Load(dir)
	if err != nil || set == nil || set.Marker.Digest == "" || set.Marker.IRI == "" {
		return seedmeta.Marker{}, false
	}
	return set.Marker, true
}
