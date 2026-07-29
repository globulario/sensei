// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
	annotateEmbeddedModeDetail(&snap.verification, usingEmbeddedSeedMarker(s))
	return snap
}

// usingEmbeddedSeedMarker reports whether freshness checks are comparing the
// live store against the marker COMPILED INTO THIS BINARY (default: no
// -no-seed / -graph-marker-file flag), as opposed to the runtime marker file
// at .sensei/graph-authority.json that `sensei build` actually updates.
func usingEmbeddedSeedMarker(s *server) bool {
	return s == nil || strings.TrimSpace(s.graphMarkerFile) == ""
}

// annotateEmbeddedModeDetail appends an actionable hint to a non-current
// freshness verdict when the server is running in embedded-seed mode. This
// fixes a real incident: `sensei build` was run repeatedly against a
// server already serving, and every awareness_briefing call kept returning
// "graph freshness stale for briefing: live store missing expected graph
// marker <hash>" with no indication of why — the server was silently
// comparing the live (correctly rebuilt) store against the marker baked into
// the binary at compile time, not the runtime marker file build had just
// updated, because -no-seed had not been passed at startup. Two server
// restarts and reading this file's source were needed to find the actual
// lever. The fix here is deliberately NOT a new default or a hot-reload
// mechanism (embedded-seed mode is an intentional production safeguard
// against a stray local file silently overriding shipped, trusted
// knowledge) — it is making the existing, correct behavior legible at the
// point of failure.
func annotateEmbeddedModeDetail(ver *seedmeta.Verification, embedded bool) {
	if ver == nil || !embedded {
		return
	}
	switch ver.State {
	case seedmeta.FreshnessStale, seedmeta.FreshnessUnknown, seedmeta.FreshnessCheckError, seedmeta.FreshnessEmpty:
	default:
		return
	}
	hint := "server is running in EMBEDDED-SEED mode (no -no-seed / -graph-marker-file at startup) — " +
		"it compares the live store against the marker compiled into this binary, NOT the runtime " +
		"marker file at .sensei/graph-authority.json that `sensei build` updates. If you have run " +
		"`sensei build` and expect that rebuild to be live, restart `sensei serve` with -no-seed."
	if ver.Detail != "" {
		ver.Detail = ver.Detail + " — " + hint
	} else {
		ver.Detail = hint
	}
}

func expectedGraphMarker(s *server) (seedmeta.Marker, string, bool) {
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
		return status.Errorf(codes.FailedPrecondition, "graph freshness stale for %s: %s", surface, snap.verification.Detail)
	case seedmeta.FreshnessUnknown:
		return status.Errorf(codes.FailedPrecondition, "graph freshness unknown for %s: %s", surface, snap.verification.Detail)
	default:
		return status.Errorf(codes.FailedPrecondition, "graph freshness unspecified for %s", surface)
	}
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
		return fmt.Sprintf("current digest=%s triples=%d", ver.Expected.Digest, ver.Expected.TripleCount)
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
