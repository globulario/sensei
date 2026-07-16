// SPDX-License-Identifier: Apache-2.0

// Package prereview defines the Sensei Architectural Pre-Review Report: a
// deterministic, evidence-linked projection generated for a proposed repository
// change before a human begins detailed review.
//
// The report is a projection, never a source of authority. It may reference
// authoritative receipts (admission, certification, completion) but can never
// create or upgrade them. This package represents, validates, canonicalizes,
// ranks, and renders a report from typed inputs. It performs no repository
// access, no LLM invocation, and no architectural mutation, and it depends on
// no other Sensei package — the typed inputs are supplied by adapters built in
// later milestones.
package prereview

// SchemaVersion is the contract version of the pre-review report. Canonical
// identity is scoped to this version: reports of different schema versions are
// never digest-equal.
const SchemaVersion = "architectural-pre-review.v1"

// AttentionPolicyVersion versions the deterministic reviewer-attention ranking
// weights. It is part of the report so a ranking is always reproducible.
const AttentionPolicyVersion = "reviewer-attention.rank.v1"

// CoverageLevel declares how much evidence was available when the report was
// built. A later level's absence must never erase useful earlier findings.
type CoverageLevel string

const (
	// CoverageAdvisory: diff + graph briefing + protection findings only. Cannot
	// claim governed mutation or correctness.
	CoverageAdvisory CoverageLevel = "advisory"
	// CoverageGoverned: additionally task/actor/authority/admission/consumption/
	// observed-change/scope. May state authorization and scope, not correctness.
	CoverageGoverned CoverageLevel = "governed"
	// CoverageProofBound: additionally result binding, evidence, proof discharge,
	// certification receipt. May display an independently verified certification.
	CoverageProofBound CoverageLevel = "proof_bound"
	// CoverageTerminal: additionally fresh result graph/artifacts and a valid
	// completion receipt. May display terminal architectural closure.
	CoverageTerminal CoverageLevel = "terminal"
)

// coverageRank orders coverage levels; higher means more evidence available.
var coverageRank = map[CoverageLevel]int{
	CoverageAdvisory:   0,
	CoverageGoverned:   1,
	CoverageProofBound: 2,
	CoverageTerminal:   3,
}

// ValidCoverage reports whether c is a known coverage level.
func ValidCoverage(c CoverageLevel) bool {
	_, ok := coverageRank[c]
	return ok
}

// AtLeast reports whether the coverage level is at least want.
func (c CoverageLevel) AtLeast(want CoverageLevel) bool {
	return coverageRank[c] >= coverageRank[want]
}

// ReviewDisposition is the report's closed verdict vocabulary. It is distinct
// from certification verdicts: only certified/terminally_closed are backed by
// authoritative receipts, and they may never be inferred.
type ReviewDisposition string

const (
	DispositionReadyForHumanReview       ReviewDisposition = "ready_for_human_review"
	DispositionMechanicalRepairRequired  ReviewDisposition = "mechanical_repair_required"
	DispositionGovernanceRequired        ReviewDisposition = "governance_required"
	DispositionArchitectDecisionRequired ReviewDisposition = "architect_decision_required"
	DispositionEvidenceRequired          ReviewDisposition = "evidence_required"
	DispositionScopeViolation            ReviewDisposition = "scope_violation"
	DispositionCannotVerify              ReviewDisposition = "cannot_verify"
	DispositionCertified                 ReviewDisposition = "certified"
	DispositionTerminallyClosed          ReviewDisposition = "terminally_closed"
)

// dispositionPriority orders dispositions by precedence. A lower number wins
// when several are candidates; cannot_verify dominates and terminally_closed is
// the most settled.
var dispositionPriority = map[ReviewDisposition]int{
	DispositionCannotVerify:              0,
	DispositionScopeViolation:            1,
	DispositionGovernanceRequired:        2,
	DispositionMechanicalRepairRequired:  3,
	DispositionEvidenceRequired:          4,
	DispositionArchitectDecisionRequired: 5,
	DispositionReadyForHumanReview:       6,
	DispositionCertified:                 7,
	DispositionTerminallyClosed:          8,
}

// ValidDisposition reports whether d is a known disposition.
func ValidDisposition(d ReviewDisposition) bool {
	_, ok := dispositionPriority[d]
	return ok
}

// EpistemicStatus classifies the epistemic standing of a load-bearing
// statement. These states must never be merged into one confidence score.
type EpistemicStatus string

const (
	EpistemicObserved                  EpistemicStatus = "observed"
	EpistemicGoverned                  EpistemicStatus = "governed"
	EpistemicDeterministicallyInferred EpistemicStatus = "deterministically_inferred"
	EpistemicModelCandidate            EpistemicStatus = "model_candidate"
	EpistemicContradicted              EpistemicStatus = "contradicted"
	EpistemicUnknown                   EpistemicStatus = "unknown"
	EpistemicStale                     EpistemicStatus = "stale"
	EpistemicNotApplicable             EpistemicStatus = "not_applicable"
	EpistemicUncertifiable             EpistemicStatus = "uncertifiable"
)

var validEpistemic = map[EpistemicStatus]bool{
	EpistemicObserved: true, EpistemicGoverned: true, EpistemicDeterministicallyInferred: true,
	EpistemicModelCandidate: true, EpistemicContradicted: true, EpistemicUnknown: true,
	EpistemicStale: true, EpistemicNotApplicable: true, EpistemicUncertifiable: true,
}

// ValidEpistemic reports whether s is a known epistemic status.
func ValidEpistemic(s EpistemicStatus) bool { return validEpistemic[s] }

// Severity ranks how much a finding or attention item matters, orthogonally to
// whether it blocks. Higher severity contributes more to attention ranking.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

var severityWeight = map[Severity]int{
	SeverityInfo: 0, SeverityLow: 1, SeverityMedium: 2, SeverityHigh: 3, SeverityCritical: 4,
}

// ValidSeverity reports whether s is a known severity.
func ValidSeverity(s Severity) bool {
	_, ok := severityWeight[s]
	return ok
}

// AttentionCategory is the closed set of reviewer-attention question kinds.
type AttentionCategory string

const (
	AttentionClosureBlocker    AttentionCategory = "closure_blocker"
	AttentionContradiction     AttentionCategory = "contradiction"
	AttentionUnknownDirection  AttentionCategory = "unknown_direction"
	AttentionAuthorityConflict AttentionCategory = "authority_conflict"
	AttentionScopeViolation    AttentionCategory = "scope_violation"
	AttentionMissingProof      AttentionCategory = "missing_proof"
	AttentionArchitectQuestion AttentionCategory = "architect_question"
	AttentionWaiverExpiring    AttentionCategory = "waiver_expiring"
	AttentionResultGraphChange AttentionCategory = "result_graph_change"
	AttentionModelCandidate    AttentionCategory = "model_candidate"
)

var validAttentionCategory = map[AttentionCategory]bool{
	AttentionClosureBlocker: true, AttentionContradiction: true, AttentionUnknownDirection: true,
	AttentionAuthorityConflict: true, AttentionScopeViolation: true, AttentionMissingProof: true,
	AttentionArchitectQuestion: true, AttentionWaiverExpiring: true, AttentionResultGraphChange: true,
	AttentionModelCandidate: true,
}

// ValidAttentionCategory reports whether c is a known attention category.
func ValidAttentionCategory(c AttentionCategory) bool { return validAttentionCategory[c] }

// DefaultMaxReviewerItems is the default cap on reviewer-attention items shown
// in a rendered report. The complete unresolved set lives in JSON.
const DefaultMaxReviewerItems = 7
