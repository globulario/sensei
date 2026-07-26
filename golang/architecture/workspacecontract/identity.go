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
	return NormalizeIdentity(id)
}

// deriveCompositionState is the single source of truth both ComposeIdentity
// and ValidateIdentity use: an identity receipt is "complete" only when the
// repository domain came from configured checkout identity, revision and
// graph digest are both resolved, and graph_authority is present and
// authoritative. Any configured-domain receipt short of that is "partial".
// An unbound repository domain always means "unavailable" — without a
// governed checkout identity, no other fact in the receipt can be
// meaningfully attributed to "this checkout" at all. Task identity is
// deliberately independent of this derivation: a requested-but-unavailable
// task does not, by itself, downgrade an otherwise complete receipt.
func deriveCompositionState(id Identity) CompositionState {
	if id.RepositoryDomainSource != RepositoryDomainConfigured || id.Binding.RepositoryDomain == "" {
		return CompositionUnavailable
	}
	if id.Binding.RevisionStatus == RevisionResolved &&
		id.Binding.GraphDigestStatus == GraphDigestResolved &&
		id.GraphAuthority != nil &&
		id.GraphAuthority.Authoritative {
		return CompositionComplete
	}
	return CompositionPartial
}
