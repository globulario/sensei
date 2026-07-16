// SPDX-License-Identifier: Apache-2.0

package prereview

import "context"

// The source interfaces let adapters supply typed inputs without coupling this
// package to Git, gRPC, Oxigraph, or the filesystem. The package assembles and
// finalizes reports; adapters (built in the CLI and later milestones) fetch the
// live data.

// DiffRequest asks a DiffSource to resolve and bind a proposed change.
type DiffRequest struct {
	RepoRoot string
	Base     string // base revision, e.g. "origin/main"
	Head     string // head revision, e.g. "HEAD"
}

// BoundDiff is the digest-bound result of resolving a proposed change. It is the
// report's semantic anchor: the tree and diff digests bind the report to an
// exact repository state.
type BoundDiff struct {
	RepositoryDomain     string
	BaseRevision         string
	BaseTreeDigestSHA256 string
	HeadRevision         string
	HeadTreeDigestSHA256 string
	MergeBaseRevision    string
	DiffDigestSHA256     string

	// Native Git tree object ids, diagnostic only (distinct from the canonical
	// *_digest_sha256 fields; never used as identity).
	BaseTreeObjectID string
	HeadTreeObjectID string

	FilesCreated  []string
	FilesModified []string
	FilesDeleted  []string
	FilesRenamed  []RenamePair
}

// ChangedPaths returns every repo-relative path the diff touches (rename targets
// included), de-duplicated and sorted.
func (d BoundDiff) ChangedPaths() []string {
	paths := make([]string, 0, len(d.FilesCreated)+len(d.FilesModified)+len(d.FilesDeleted)+len(d.FilesRenamed))
	paths = append(paths, d.FilesCreated...)
	paths = append(paths, d.FilesModified...)
	paths = append(paths, d.FilesDeleted...)
	for _, r := range d.FilesRenamed {
		paths = append(paths, r.To)
	}
	return sortedUnique(paths)
}

// DiffSource resolves a proposed change into a bound diff.
type DiffSource interface {
	ResolveReviewDiff(ctx context.Context, req DiffRequest) (BoundDiff, error)
}

// GraphRequest asks a GraphSource for architectural context on the changed
// paths.
type GraphRequest struct {
	RepositoryDomain string
	ChangedPaths     []string
}

// GraphContext is the deterministic, governed architectural context for a set of
// changed paths: impact reach, applicable protection rules, and any reviewer
// concerns (e.g. forbidden-fix or edit-check matches). Available/Unavailable
// name what the source could and could not provide; Degraded names what it tried
// but could not verify, which becomes report limitations.
type GraphContext struct {
	RiskClass          string
	AffectedComponents []ImpactItem
	ChangedBoundaries  []ImpactItem
	AffectedContracts  []ImpactItem

	Invariants     []ProtectionItem
	FailureModes   []ProtectionItem
	ForbiddenFixes []ProtectionItem
	RequiredTests  []ProtectionItem

	ReviewerConcerns []ReviewerAttentionItem

	Available   []string
	Unavailable []string
	Degraded    []string
}

// GraphSource collects deterministic architectural context for changed paths. It
// reads governed sources locally; it never establishes authorization or
// correctness.
type GraphSource interface {
	CollectArchitecturalContext(ctx context.Context, req GraphRequest) (GraphContext, error)
}
