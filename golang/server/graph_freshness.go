// SPDX-License-Identifier: Apache-2.0

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
	return snap
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
