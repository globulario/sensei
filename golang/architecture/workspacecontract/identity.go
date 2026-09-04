// SPDX-License-Identifier: AGPL-3.0-only

package workspacecontract

// IdentityInputs is every already-resolved fact ComposeIdentity needs. This
// package never gathers evidence itself — a caller (cmd/awareness-mcp)
// resolves each field through the existing typed owner: repository domain
// via golang/architecture/repodomain, revision/tree digest via
// architecture.ResolveRevision and golang/architecture/binding, graph
// authority/coverage via the gRPC Metadata RPC, and task identity via
// golang/architecture/tasksession. ComposeIdentity only composes and
// derives CompositionState deterministically from what it is handed.
type IdentityInputs struct {
	// RepositoryDomainSource and RepositoryDomain must already reflect the
	// strict governed rule: RepositoryDomain is non-empty if and only if
	// RepositoryDomainSource is RepositoryDomainConfigured. A caller must
	// never pass an environment-derived or git-origin-derived domain here.
	RepositoryDomainSource RepositoryDomainSource
	RepositoryDomain       string

	Revision          string
	RevisionStatus    RevisionStatus
	TreeDigestSHA256  string
	GraphDigestSHA256 string
	GraphDigestStatus GraphDigestStatus

	// GraphAuthority is nil when the metadata backend could not be reached
	// at all.
	GraphAuthority *GraphAuthority
	// CoverageState is the exact proto CoverageState string (e.g.
	// "COVERAGE_STATE_SUFFICIENT"); pass "COVERAGE_STATE_UNSPECIFIED" when
	// unknown/unreachable rather than an empty string.
	CoverageState string

	TaskIdentity TaskIdentity
	Limitations  []Limitation
}

// ComposeIdentity builds a normalized, schema-shaped Identity from already-
// resolved inputs and deterministically derives CompositionState — the
// producer never accepts a caller-supplied CompositionState, so a caller
// cannot assert "complete" without the underlying facts actually being
// resolved.
func ComposeIdentity(in IdentityInputs) Identity {
	binding := Binding{
		RepositoryDomain:  in.RepositoryDomain,
		RevisionStatus:    in.RevisionStatus,
		GraphDigestStatus: in.GraphDigestStatus,
	}
	if in.RevisionStatus == RevisionResolved && in.Revision != "" {
		rev := in.Revision
		binding.Revision = &rev
	}
	if in.TreeDigestSHA256 != "" {
		td := in.TreeDigestSHA256
		binding.TreeDigestSHA256 = &td
	}
	if in.GraphDigestStatus == GraphDigestResolved && in.GraphDigestSHA256 != "" {
		gd := in.GraphDigestSHA256
		binding.GraphDigestSHA256 = &gd
	}

	coverage := in.CoverageState
	if coverage == "" {
		coverage = "COVERAGE_STATE_UNSPECIFIED"
	}

	id := Identity{
		SchemaVersion:          IdentitySchemaVersion,
		GeneratedBy:            GeneratedBy,
		Binding:                binding,
		RepositoryDomainSource: in.RepositoryDomainSource,
		GraphAuthority:         in.GraphAuthority,
		CoverageState:          coverage,
		TaskIdentity:           in.TaskIdentity,
		Limitations:            in.Limitations,
	}
	id.CompositionState = deriveCompositionState(id)
	if id.CompositionState == CompositionPartial {
		id.Limitations = append(id.Limitations, partialCompositionLimitations(id)...)
	}
	return NormalizeIdentity(id)
}

// partialCompositionLimitations names the specific completeness dimension(s)
// deriveCompositionState found lacking when it returns CompositionPartial,
// so a caller printing id.Limitations (e.g. sensei synthesis-run's
// workspace-identity error path) never renders an empty list under a
// non-complete state. Domain-unbound, graph-authority-unreachable, and
// revision-unresolved reasons are already appended by the caller that
// gathers those facts (see composeSynthesisRunIdentity); this only covers
// the two dimensions deriveCompositionState checks that no caller
// currently narrates: a reachable-but-not-authoritative graph, and
// insufficient graph coverage.
func partialCompositionLimitations(id Identity) []Limitation {
	var out []Limitation
	if id.GraphAuthority != nil && !id.GraphAuthority.Authoritative {
		// Name the proposition that failed, not the disjunction of everything
		// that could have.
		//
		// "freshness, seed, or build-provenance state is not current" was true
		// and told a reader nothing: the same sentence whether the graph is
		// stale, the seed is behind, or the SERVING BINARY simply carries no
		// link-time source-repo commit. Three conditions with three different
		// repairs, and the third is not about the graph at all -- it is fixed
		// by rebuilding the server, not by rebuilding any corpus.
		//
		// Derived from fields already on the wire, so no consumer pinned to
		// this schema's digest sees a shape it does not recognise.
		scope, reason := failingAuthorityProposition(*id.GraphAuthority)
		out = append(out, Limitation{
			Source: "golang/architecture/workspacecontract", Scope: scope,
			Reason: reason, Blocking: true,
		})
	}
	if id.CoverageState != coverageStateSufficient {
		out = append(out, Limitation{
			Source: "golang/architecture/workspacecontract", Scope: "coverage_state",
			Reason: "graph coverage for this domain is " + id.CoverageState + ", not " + coverageStateSufficient, Blocking: true,
		})
	}
	return out
}

// coverageStateSufficient is the exact proto CoverageState string
// CoverageState must equal for deriveCompositionState to treat graph
// coverage as sufficient for a governed operation. See CoverageState's own
// doc comment for the full enum this is one member of.
const coverageStateSufficient = "COVERAGE_STATE_SUFFICIENT"

// CoverageStateSufficient is coverageStateSufficient's exported form, so a
// caller outside this package can recognize this one specific value (e.g.
// to tell "Identity is Partial only because coverage is thin" apart from
// every other reason Partial can hold) without duplicating the magic
// string. It changes no derivation: CompositionState is still only ever
// computed by deriveCompositionState, never by a caller re-deciding
// sufficiency for itself.
const CoverageStateSufficient = coverageStateSufficient

// deriveCompositionState is the single source of truth both ComposeIdentity
// and ValidateIdentity use: an identity receipt is "complete" only when the
// repository domain came from configured checkout identity, revision is
// resolved, graph_authority is present and authoritative, AND graph
// coverage is sufficient.
//
// Authority and coverage are deliberately kept as two separate dimensions,
// never merged into one another: GraphAuthority.Authoritative answers "is
// this genuinely the live, current, provenance-stamped graph" (freshness,
// seed state, build provenance -- see golang/client/authority.go's
// isCurrentMetadataAuthority, which this field's value already reflects),
// while CoverageState answers "does that graph actually know enough about
// this repository to ground a governed operation." A graph can be
// authoritative in the first sense while still being COVERAGE_STATE_THIN or
// COVERAGE_STATE_EMPTY in the second -- composing on a technically-genuine
// but functionally-uninformed graph is exactly the kind of degraded-not-safe
// state this package's own non-negotiables forbid treating as complete.
// GraphAuthority.Authoritative's own meaning and computation are untouched
// by this: this function only changed what combination of already-computed
// facts it requires before returning CompositionComplete.
//
// Two states are hard "unavailable" regardless of what else resolved: an
// unbound repository domain (without a governed checkout identity, no other
// fact in the receipt can be meaningfully attributed to "this checkout" at
// all), and an unreachable metadata backend (graph_authority == nil) — the
// receipt could not be meaningfully composed at all, not merely partially,
// when the graph backend itself could not be reached. "partial" is reserved
// for a configured domain with a *reachable* backend that is not fully
// current/authoritative, not sufficiently covered (e.g. a stale or thin
// graph), or an unresolved revision.
//
// binding.graph_digest_status/task_identity are deliberately NOT inputs to
// this derivation: sensei_workspace_status never resolves a task/snapshot-
// scoped local graph.nt digest (see cmd/awareness-mcp/workspace_tools.go),
// so that field is legitimately not_requested on every real call this
// package's only current caller makes, and gating completeness on a field
// that can never be anything but not_requested would make "complete"
// unreachable. A requested-but-unavailable task likewise does not, by
// itself, downgrade an otherwise complete receipt.
func deriveCompositionState(id Identity) CompositionState {
	if id.RepositoryDomainSource != RepositoryDomainConfigured || id.Binding.RepositoryDomain == "" {
		return CompositionUnavailable
	}
	if id.GraphAuthority == nil {
		return CompositionUnavailable
	}
	if id.Binding.RevisionStatus == RevisionResolved && id.GraphAuthority.Authoritative && id.CoverageState == coverageStateSufficient {
		return CompositionComplete
	}
	return CompositionPartial
}

// failingAuthorityProposition names which half of the combined authority
// reading did not hold.
//
// Two propositions travel under one bool:
//
//	graph answer authority        are the answers this graph gives trustworthy
//	                              in this world?  (freshness, seed)
//	workspace provenance          can Sensei state the source-provenance chain
//	readiness                     behind them?    (build provenance)
//
// They are independent, and a live instance proves it rather than arguing it:
// on 2026-09-03 the github.com/globulario/sensei graph answered as the current
// validated artifact WITH a certified runtime transaction, and still reported
// build provenance incomplete, because the serving binary carried no link-time
// source-repo commit.
//
// The answer proposition is checked first. Reporting "provenance incomplete"
// about a stale graph would send a reader to repair the wrong thing.
func failingAuthorityProposition(a GraphAuthority) (scope, reason string) {
	fresh := a.GraphFreshnessState == "" || a.GraphFreshnessState == graphFreshnessCurrent
	seeded := a.SeedState == "" || a.SeedState == seedStateCurrent
	if !fresh || !seeded {
		reason = "the graph is not answering as the current validated artifact"
		if detail := a.GraphFreshnessDetail; detail != "" {
			reason += ": " + detail
		}
		return "graph_answer_authority", reason
	}
	if a.BuildProvenanceState != "" && a.BuildProvenanceState != buildProvenanceStamped {
		return "workspace_provenance_readiness",
			"the graph answers as the current validated artifact, and Sensei cannot state the source " +
				"provenance behind it (build_provenance_state " + a.BuildProvenanceState + "), so this " +
				"workspace is not governance-ready. Build provenance is a property of the SERVING BINARY " +
				"as well as of the graph: a server compiled without its source-repo commit reports " +
				"incomplete however the corpus was built"
	}
	return "graph_authority", "graph_authority is present but not authoritative"
}

const (
	graphFreshnessCurrent  = "GRAPH_FRESHNESS_STATE_CURRENT"
	seedStateCurrent       = "SEED_STATE_CURRENT"
	buildProvenanceStamped = "BUILD_PROVENANCE_STATE_STAMPED"
)
