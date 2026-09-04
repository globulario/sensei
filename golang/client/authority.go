// SPDX-License-Identifier: AGPL-3.0-only

package client

import (
	"fmt"
	"strings"

	awarenesspb "github.com/globulario/sensei/golang/pb"
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

// A MetadataResponse carries TWO propositions, and they are not the same
// question.
//
//	graph answer authority       are the answers this graph gives trustworthy
//	                             in this world?
//	workspace provenance         can Sensei state the source-provenance chain
//	readiness                    behind the facts it is answering with?
//
// They were conflated into one bool called Authoritative, which then travelled
// under the same NAME as the proto field GraphAuthority.Authoritative -- a
// field the server computes from a different predicate entirely (freshness and
// semantic closure; see golang/server/graph_authority.go). So one graph
// published `authoritative: true` on a preflight and `authoritative: false` on
// a workspace receipt, from identical evidence, and nothing in either document
// said the two words meant different things.
//
// Observed on 2026-09-03 against github.com/globulario/sensei-code: freshness
// CURRENT, seed CURRENT, build provenance INCOMPLETE, transaction stamp
// missing. Both readings were correct about their own question. The reader had
// no way to know there were two questions.
//
// Splitting them here rather than at the surfaces is what keeps one answer per
// proposition: the conjunction below is DERIVED from the two, so a caller that
// wants the old combined meaning gets exactly the old value and cannot compute
// a third one.
type MetadataAuthority struct {
	// AnswerAuthority reports whether the live graph is the current, validated
	// artifact and can be trusted to answer. It says nothing about how that
	// artifact was built.
	AnswerAuthority bool
	// ProvenanceReadiness reports whether Sensei can state which source
	// commits produced the graph it is answering from.
	//
	// It is a property of the SERVING BINARY as much as of the graph:
	// classifyBuildProvenance reads the server's own link-time SourceRepoCommit
	// and build time, so a binary compiled without those stamps reports
	// INCOMPLETE no matter how the graph was built or how many runtime
	// transaction certifications exist beside it.
	ProvenanceReadiness bool
	// Reason explains the first proposition that does not hold, or is empty.
	Reason string
}

// InterpretMetadataScoped answers both propositions separately.
func InterpretMetadataScoped(m *awarenesspb.MetadataResponse) MetadataAuthority {
	if m == nil {
		return MetadataAuthority{Reason: "metadata unavailable"}
	}
	answer := EffectiveMetadataFreshness(m) == awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT &&
		m.GetSeedState() == awarenesspb.SeedState_SEED_STATE_CURRENT &&
		m.GetLiveStoreContainsEmbeddedSeedMarker() &&
		m.GetTripleCount() > 0
	provenance := m.GetBuildProvenanceState() == awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED

	out := MetadataAuthority{AnswerAuthority: answer, ProvenanceReadiness: provenance}
	switch {
	case !answer:
		out.Reason = metadataAuthorityWarning(m, FreshnessLabel(EffectiveMetadataFreshness(m)))
	case !provenance:
		out.Reason = "the graph answers as the current validated artifact, and the source provenance behind it " +
			"is " + strings.ToLower(strings.TrimPrefix(m.GetBuildProvenanceState().String(), "BUILD_PROVENANCE_STATE_")) +
			": Sensei cannot state which source commits produced it"
	}
	return out
}

// isCurrentMetadataAuthority is the CONJUNCTION of the two, kept because it is
// what the existing compatibility surface means. It is derived rather than
// recomputed, so it cannot become a third answer.
func isCurrentMetadataAuthority(m *awarenesspb.MetadataResponse) bool {
	scoped := InterpretMetadataScoped(m)
	return scoped.AnswerAuthority && scoped.ProvenanceReadiness
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
