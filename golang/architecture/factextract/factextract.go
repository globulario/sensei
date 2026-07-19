// SPDX-License-Identifier: Apache-2.0

// Package factextract is the deterministic, reusable core of Sensei's
// architecture fact and authority-surface extraction. It was lifted out of the
// cmd/awg CLI so both the `sensei extract-invariants` / `extract-authority`
// commands and the Phase 7 result pipeline drive one implementation. The CLI
// commands are thin adapters over the exported entrypoints here.
//
// The exported names are aliases onto the package's internal types and thin
// wrappers over its internal functions, so the extraction logic keeps its
// original identifiers and behavior byte-for-byte.
package factextract

import "github.com/globulario/sensei/golang/architecture"

// Report is the full extraction result: normalized facts, review-only invariant
// candidates, authority surfaces, mutation-analysis state, and limitations.
type Report = invariantExtractionReport

// Options selects what the extractor inspects.
type Options = invariantExtractOptions

// Candidate is one review-only invariant candidate.
type Candidate = extractedInvariantCandidate

// AuthoritySurface is one conservative authority-surface candidate.
type AuthoritySurface = authoritySurfaceCandidate

// RepositoryIdentity is the resolved repository domain/revision identity used to
// bind extracted facts.
type RepositoryIdentity = invariantRepositoryIdentity

// MutationAnalysisState is the bounded mutation-analysis placeholder state.
type MutationAnalysisState = invariantMutationAnalysisState

// Fact is a normalized architecture fact.
type Fact = architecture.Fact

// Extract runs the full deterministic fact + candidate + authority-surface
// extraction over a repository root.
func Extract(root string, opts Options) (Report, error) {
	return buildInvariantExtractionReport(root, opts)
}

// ResolveRepositoryIdentity resolves the repository domain/revision identity for
// a root.
func ResolveRepositoryIdentity(root string) RepositoryIdentity {
	return resolveInvariantRepositoryIdentity(root)
}

// RenderReport renders an extraction report to json or yaml.
func RenderReport(report Report, format string) ([]byte, error) {
	return renderInvariantExtractionReport(report, format)
}

// ExtractAuthorityCandidates extracts conservative authority-surface candidates
// from a repository root.
func ExtractAuthorityCandidates(root string) ([]AuthoritySurface, error) {
	return extractAuthorityCandidates(root)
}

// FilterAuthorityByMinConfidence drops surfaces below the given confidence level.
func FilterAuthorityByMinConfidence(cands []AuthoritySurface, min string) []AuthoritySurface {
	return filterAuthorityByMinConfidence(cands, min)
}

// RenderAuthorityCandidates renders authority-surface candidates to candidate YAML.
func RenderAuthorityCandidates(root string, cands []AuthoritySurface) ([]byte, error) {
	return renderAuthorityCandidates(root, cands)
}

// AuthorityKindSummary summarizes candidate counts by kind for CLI reporting.
func AuthorityKindSummary(kinds map[string]int) []string {
	return authorityKindSummary(kinds)
}
