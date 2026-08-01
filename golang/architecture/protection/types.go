// SPDX-License-Identifier: AGPL-3.0-only

// Package protection is the ONE canonical owner of effective file-protection
// classification: the deterministic union of manual registry entries,
// unconditional governed-source protection, structural contract/invariant
// signals, candidate-derived provisional caution, and direct governed
// relationships (protects/enforces/configures/observes, required tests).
//
// Every consumer — the pre-edit hook, `sensei protection-status`/
// `protection-check`, `sensei init`, `sensei bootstrap`, the preflight risk
// classifier, and `sensei audit` — MUST derive its answer through this
// package. None may re-parse docs/awareness/high_risk_files.yaml, maintain a
// second path-matching table, or reimplement this decision in shell.
//
// Protection is a caution signal ("must an agent consult Sensei before
// editing this file?"), never architectural authority: it does not promote a
// candidate, certify correctness, or infer an owner/mutation path. Empty or
// incomplete coverage is an explicit, inspectable state — never presented as
// "no high-risk files" or otherwise conflated with safety.
package protection

import "sort"

// ProtectionOrigin is the closed vocabulary of reasons a path can be
// protected. Ordering below is the canonical origin precedence used when
// sorting reasons within one protected path (see ProtectionReason.Less).
type ProtectionOrigin string

const (
	OriginManual             ProtectionOrigin = "manual"
	OriginGovernedSource     ProtectionOrigin = "governed_source"
	OriginStructuralContract ProtectionOrigin = "structural_contract"
	OriginGovernedRelation   ProtectionOrigin = "governed_relation"
	OriginCandidateSignal    ProtectionOrigin = "candidate_signal"
)

// originOrder is the fixed sort precedence for origins (deterministic
// ordering law, contract §4). Authoritative origins sort before provisional
// ones so a path's strongest reason is always first.
var originOrder = map[ProtectionOrigin]int{
	OriginManual:             0,
	OriginGovernedSource:     1,
	OriginStructuralContract: 2,
	OriginGovernedRelation:   3,
	OriginCandidateSignal:    4,
}

// ProtectionReason is one typed cause a path is protected. Provisional
// reasons (candidate-derived) are procedural caution only — they never make
// the underlying candidate true, governed, promoted, or authoritative.
type ProtectionReason struct {
	// Origin is the closed-vocabulary category this reason belongs to.
	Origin ProtectionOrigin
	// Kind is a short, stable, origin-scoped label (e.g. "protects.files",
	// "required_test", "high_risk_files.yaml", "authority_surface_candidate").
	Kind string
	// Source is the repo-relative file that carries this reason (e.g. the
	// invariants.yaml that declared protects.files, or high_risk_files.yaml
	// itself).
	Source string
	// KnowledgeRef is the governed-knowledge identity this reason traces to
	// (an invariant/failure-mode/candidate id), when one exists.
	KnowledgeRef string
	// Provisional is true for candidate-derived reasons: caution without
	// promotion. A path protected ONLY by provisional reasons is still
	// effectively protected, but callers may want to say so distinctly.
	Provisional bool
}

// Less orders two reasons within the same protected path: origin precedence,
// then kind, then source, then knowledge ref — all ascending. Deterministic
// regardless of input/derivation order.
func (r ProtectionReason) Less(o ProtectionReason) bool {
	if originOrder[r.Origin] != originOrder[o.Origin] {
		return originOrder[r.Origin] < originOrder[o.Origin]
	}
	if r.Kind != o.Kind {
		return r.Kind < o.Kind
	}
	if r.Source != o.Source {
		return r.Source < o.Source
	}
	return r.KnowledgeRef < o.KnowledgeRef
}

// ProtectedPath is one repo-relative path and every reason it is protected.
type ProtectedPath struct {
	Path    string
	Reasons []ProtectionReason
}

// AllProvisional reports whether every reason for this path is provisional
// (candidate-derived only, no manual/governed/structural/relation reason).
func (p ProtectedPath) AllProvisional() bool {
	if len(p.Reasons) == 0 {
		return false
	}
	for _, r := range p.Reasons {
		if !r.Provisional {
			return false
		}
	}
	return true
}

// SortReasons orders p.Reasons deterministically in place.
func (p *ProtectedPath) SortReasons() {
	sort.Slice(p.Reasons, func(i, j int) bool { return p.Reasons[i].Less(p.Reasons[j]) })
}

// ProtectionCoverageStatus is the closed vocabulary for overall coverage
// health (contract §3.5). Never collapse EMPTY/PARTIAL/DEGRADED into "no
// high-risk files" — each is a distinct, inspectable state.
type ProtectionCoverageStatus string

const (
	// CoverageComplete: every supported input was evaluated successfully and
	// either at least one effective protected path exists, or the repository
	// was conclusively scanned and no supported protection signal exists.
	CoverageComplete ProtectionCoverageStatus = "COMPLETE"
	// CoveragePartial: usable protection exists, but one or more supported
	// inputs were unavailable, stale, invalid, or not yet evaluated.
	CoveragePartial ProtectionCoverageStatus = "PARTIAL"
	// CoverageDegraded: contract/invariant/governed-source or contract-like
	// structural signals exist but the effective protected set could not be
	// established safely.
	CoverageDegraded ProtectionCoverageStatus = "DEGRADED"
	// CoverageEmpty: no protection signal was observed after a successful
	// bounded scan. This is an awareness gap, never a low-risk verdict.
	CoverageEmpty ProtectionCoverageStatus = "EMPTY"
)

// ProtectionCoverage is the full derived result for one repository.
type ProtectionCoverage struct {
	// SchemaVersion identifies the snapshot/wire shape.
	SchemaVersion string
	Status        ProtectionCoverageStatus
	// ProtectedPaths is sorted by normalized path ascending (contract §4
	// ordering law); each entry's Reasons are sorted via SortReasons.
	ProtectedPaths []ProtectedPath
	ManualCount    int
	DerivedCount   int
	// ProvisionalCount is the number of protected paths whose protection is
	// entirely candidate-derived (AllProvisional).
	ProvisionalCount int
	// Gaps lists supported-input problems that degrade Status below
	// COMPLETE (unavailable/stale/invalid inputs, bounded-scan failures).
	Gaps []string
	// GenerationIdentity is a deterministic digest of every input this
	// derivation consumed, sufficient to detect staleness. It intentionally
	// carries no timestamp.
	GenerationIdentity string
}

// SortPaths orders c.ProtectedPaths by normalized path ascending and sorts
// each path's reasons. Call after assembling paths from any input order.
func (c *ProtectionCoverage) SortPaths() {
	sort.Slice(c.ProtectedPaths, func(i, j int) bool { return c.ProtectedPaths[i].Path < c.ProtectedPaths[j].Path })
	for i := range c.ProtectedPaths {
		c.ProtectedPaths[i].SortReasons()
	}
}

// Find returns the ProtectedPath for an already-normalized repo-relative
// path, and whether it exists.
func (c ProtectionCoverage) Find(normalizedPath string) (ProtectedPath, bool) {
	// ProtectedPaths is sorted; a linear scan is simple and the set is small
	// enough (bounded by repository file count) that a map is not needed for
	// the in-process case. Snapshot-backed lookups build their own index.
	for _, p := range c.ProtectedPaths {
		if p.Path == normalizedPath {
			return p, true
		}
	}
	return ProtectedPath{}, false
}
