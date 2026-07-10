// SPDX-License-Identifier: Apache-2.0

package client

import (
	"fmt"
	"strings"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
)

// AuthorityVerdict is the interpreted honesty summary of a graph-backed
// response: whether the answer is authoritative, the freshness state, and a
// human-readable warning when it is not.
//
// It is the single source of truth for "can I trust this answer, and if not
// why". Every client surface (CLI, MCP bridge, editor extensions) must render
// this verdict rather than recompute it, so the honesty signal cannot drift
// between surfaces — a non-authoritative answer must never look authoritative
// on one surface and not another.
type AuthorityVerdict struct {
	Authoritative bool
	Verdict       string // "authoritative" | "non_authoritative"
	State         string // freshness label, e.g. "current", "stale", "empty"
	Warning       string // empty when authoritative and current
}

// InterpretAuthority summarizes the GraphAuthority stamp that rides on a
// Briefing / Impact / Preflight / Resolve / Query response. A nil authority is
// treated as non-authoritative — absence of the stamp is never trust.
func InterpretAuthority(a *awarenesspb.GraphAuthority) AuthorityVerdict {
	if a == nil {
		return AuthorityVerdict{
			Verdict: "non_authoritative",
			State:   "unknown",
			Warning: "graph authority metadata unavailable",
		}
	}
	state := FreshnessLabel(a.GetGraphFreshnessState())
	if a.GetAuthoritative() && a.GetGraphFreshnessState() == awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT {
		return AuthorityVerdict{Authoritative: true, Verdict: "authoritative", State: state}
	}
	if detail := strings.TrimSpace(a.GetGraphFreshnessDetail()); detail != "" {
		return AuthorityVerdict{Verdict: "non_authoritative", State: state, Warning: detail}
	}
	return AuthorityVerdict{
		Verdict: "non_authoritative",
		State:   state,
		Warning: fmt.Sprintf("graph-backed answer is not authoritative (%s)", state),
	}
}

// InterpretMetadataAuthority summarizes the standalone Metadata() response,
// which carries the same authority signals as GraphAuthority but as top-level
// fields.
func InterpretMetadataAuthority(m *awarenesspb.MetadataResponse) AuthorityVerdict {
	if m == nil {
		return AuthorityVerdict{Verdict: "non_authoritative", State: "unknown", Warning: "metadata unavailable"}
	}
	state := FreshnessLabel(EffectiveMetadataFreshness(m))
	if isCurrentMetadataAuthority(m) {
		return AuthorityVerdict{Authoritative: true, Verdict: "authoritative", State: state}
	}
	return AuthorityVerdict{Verdict: "non_authoritative", State: state, Warning: metadataAuthorityWarning(m, state)}
}

// FreshnessLabel renders a GraphFreshnessState as a short lowercase token
// (e.g. GRAPH_FRESHNESS_STATE_CURRENT -> "current").
func FreshnessLabel(state awarenesspb.GraphFreshnessState) string {
	return strings.ToLower(strings.TrimPrefix(state.String(), "GRAPH_FRESHNESS_STATE_"))
}

// EffectiveMetadataFreshness resolves the freshness state for a Metadata
// response, deriving it from provenance/seed/store signals when the server did
// not set an explicit state.
func EffectiveMetadataFreshness(m *awarenesspb.MetadataResponse) awarenesspb.GraphFreshnessState {
	if m == nil {
		return awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_UNKNOWN
	}
	if state := m.GetGraphFreshnessState(); state != awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_UNSPECIFIED {
		return state
	}
	switch {
	case m.GetTripleCount() == 0:
		return awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_EMPTY
	case m.GetBuildProvenanceState() == awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED &&
		m.GetSeedState() == awarenesspb.SeedState_SEED_STATE_CURRENT &&
		m.GetLiveStoreContainsEmbeddedSeedMarker():
		return awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT
	case m.GetSeedState() == awarenesspb.SeedState_SEED_STATE_STALE || !m.GetLiveStoreContainsEmbeddedSeedMarker():
		return awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_STALE
	case m.GetBuildProvenanceState() == awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_INCOMPLETE:
		return awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CHECK_ERROR
	default:
		return awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_UNKNOWN
	}
}

func isCurrentMetadataAuthority(m *awarenesspb.MetadataResponse) bool {
	return EffectiveMetadataFreshness(m) == awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT &&
		m.GetBuildProvenanceState() == awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED &&
		m.GetSeedState() == awarenesspb.SeedState_SEED_STATE_CURRENT &&
		m.GetLiveStoreContainsEmbeddedSeedMarker() &&
		m.GetTripleCount() > 0
}

func metadataAuthorityWarning(m *awarenesspb.MetadataResponse, state string) string {
	if detail := strings.TrimSpace(m.GetGraphFreshnessDetail()); detail != "" {
		return detail
	}
	if m.GetTripleCount() == 0 || EffectiveMetadataFreshness(m) == awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_EMPTY {
		return "live graph is empty and cannot answer as authority"
	}
	if !m.GetLiveStoreContainsEmbeddedSeedMarker() {
		return "live store does not contain the embedded seed marker for the expected artifact"
	}
	if m.GetBuildProvenanceState() != awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED {
		return fmt.Sprintf("build provenance is %s", strings.ToLower(strings.TrimPrefix(m.GetBuildProvenanceState().String(), "BUILD_PROVENANCE_STATE_")))
	}
	if m.GetSeedState() != awarenesspb.SeedState_SEED_STATE_CURRENT {
		return fmt.Sprintf("embedded seed state is %s", strings.ToLower(strings.TrimPrefix(m.GetSeedState().String(), "SEED_STATE_")))
	}
	return fmt.Sprintf("graph metadata is not authoritative (%s)", state)
}
