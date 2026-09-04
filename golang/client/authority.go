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

	// One evaluation, one reason. The verdict took its BOOLEAN from the scoped
	// reading and then recomputed its WHY from the top-level fields, which carry
	// neither the closure proof nor the transaction certification the canonical
	// verdict weighs. A graph refused for a missing transaction was explained as
	// "graph metadata is not authoritative (current)" -- a sentence naming the
	// state the reader can already see and none of the reason they need. This
	// type promises to be the single source of truth for "can I trust this
	// answer, and if not why"; half of that was true.
	scoped := InterpretMetadataScoped(m)
	if scoped.AnswerAuthority && scoped.BinaryBuildStampComplete {
		return AuthorityVerdict{Authoritative: true, Verdict: "authoritative", State: state}
	}
	return AuthorityVerdict{Verdict: "non_authoritative", State: state, Warning: scoped.Reason}
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
// under the same NAME as the proto field GraphAuthority.Authoritative -- and
// the server's predicate for that field was itself missing a conjunct the wire
// contract required. So one graph published `authoritative: true` on a
// preflight and `authoritative: false` on a workspace receipt, from identical
// evidence, and nothing in either document said the two words meant different
// things.
//
// Observed on 2026-09-03 against github.com/globulario/sensei-code: freshness
// CURRENT, seed CURRENT, build provenance INCOMPLETE, transaction stamp
// MISSING. Only the workspace reading was correct. The preflight reading was
// the missing transaction conjunct in golang/server/graph_authority.go, since
// repaired -- that graph is not authoritative, and was not on the day it was
// observed.
//
// The propositions really are two, and the coherent specimen is the
// github.com/globulario/sensei graph of the same day: certified transaction,
// authoritative answers, and an unstamped serving binary. See
// authority_scoped_test.go.
//
// Splitting them here rather than at the surfaces is what keeps one answer per
// proposition: the conjunction below is DERIVED from the two, so a caller that
// wants the old combined meaning gets exactly the old value and cannot compute
// a third one.
type MetadataAuthority struct {
	// AnswerAuthority is the CANONICAL graph-answer verdict, consumed rather
	// than reconstructed.
	//
	// An earlier version of this type rebuilt it from the response's top-level
	// freshness/seed/marker/count fields. That was a second authority
	// evaluation living beside the canonical one, and it was WEAKER: it omitted
	// the closure proof and the transaction certification, so a graph the
	// canonical verdict refuses could have read healthy here while every
	// top-level field looked fine. That is the precise false-green shape this
	// whole repair exists to remove, reintroduced by the repair.
	//
	// InterpretAuthority is documented as the single source of truth for
	// whether a graph-backed answer is authoritative. This consumes it.
	AnswerAuthority bool
	// BinaryBuildStampComplete reports whether the SERVING BINARY carries the
	// link-time stamps classifyBuildProvenance requires.
	//
	// Named for exactly what it proves, which is less than it sounds like. It
	// is BUILD_PROVENANCE_STATE == STAMPED, and that state is computed from the
	// server's own -X main.SourceCommit / main.BuildTimeUnix, set when the
	// BINARY was linked. It does not establish that those commits produced the
	// graph now being served: a server started with -no-seed against an
	// existing store can be rebuilt and restamped today while the store it
	// answers from was published long ago by inputs nobody recorded.
	//
	// It was called ProvenanceReadiness and described as "can Sensei state
	// which source commits produced the graph it is answering from". It cannot.
	// The proto is clearer than that name was: graph_build_commit,
	// graph_build_time_unix and source_repo_commit are documented as facts
	// about when awareness.nt was GENERATED. Graph provenance lives in the
	// publication and transaction evidence bound to the served generation --
	// which is now a conjunct of AnswerAuthority above, where it belongs.
	BinaryBuildStampComplete bool
	// Reason explains the first proposition that does not hold, or is empty.
	Reason string
}

// InterpretMetadataScoped answers both propositions separately.
func InterpretMetadataScoped(m *awarenesspb.MetadataResponse) MetadataAuthority {
	if m == nil {
		return MetadataAuthority{Reason: "metadata unavailable"}
	}
	stamped := m.GetBuildProvenanceState() == awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED

	// The canonical verdict decides whenever there IS one. A present refusal is
	// never overridden by healthy-looking top-level fields, which is the whole
	// point: those fields carry neither the closure proof nor the transaction
	// certification the composed verdict weighs.
	answer := false
	reason := ""
	if m.GetAuthority() != nil {
		canonical := InterpretAuthority(m.GetAuthority())
		answer = canonical.Authoritative
		reason = canonical.Warning
		if !answer && reason == "" {
			reason = "the graph-backed answer is not authoritative (" + canonical.State + ")"
		}
	} else {
		// No composed verdict on the wire. Servers have carried one since
		// aa0e757d (#176), so this is the compatibility path for an older one,
		// and it is a FALLBACK rather than a second opinion: it is consulted
		// only when there is nothing to consume. It infers from the same
		// top-level signals EffectiveMetadataFreshness already derives from,
		// and it is weaker by construction -- it can see neither the closure
		// proof nor the transaction certification. A response that carries a
		// verdict never reaches it.
		answer = EffectiveMetadataFreshness(m) == awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT &&
			m.GetSeedState() == awarenesspb.SeedState_SEED_STATE_CURRENT &&
			m.GetLiveStoreContainsEmbeddedSeedMarker() &&
			m.GetTripleCount() > 0
		if !answer {
			reason = metadataAuthorityWarning(m, FreshnessLabel(EffectiveMetadataFreshness(m)))
		}
	}

	out := MetadataAuthority{AnswerAuthority: answer, BinaryBuildStampComplete: stamped}
	switch {
	case !answer:
		out.Reason = reason
	case !stamped:
		out.Reason = "the graph answers authoritatively, and the serving binary carries no complete build " +
			"stamp (build_provenance_state " +
			strings.ToLower(strings.TrimPrefix(m.GetBuildProvenanceState().String(), "BUILD_PROVENANCE_STATE_")) +
			"): this says nothing about which commits produced the graph, only that the SERVER was linked " +
			"without its own source stamp"
	}
	return out
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
