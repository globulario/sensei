// SPDX-License-Identifier: AGPL-3.0-only

package closure

import (
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/architecture/plane"
)

// ClaimResolution is the ONE authoritative reading of how an extracted claim
// can be resolved (R-CLOSURE-01). Closure computes it once and stamps it on
// every blocker that names the claim; question generation consumes the stamp
// and never re-reads the claim. Two readers each judging "is this claim
// answerable?" is what produced the deadlock: closure demanded a question the
// generator had decided nobody could answer, about a source that had changed
// since it was read.
type ClaimResolution string

const (
	// Every premise is a source-backed fact whose pinned digest still matches
	// the file (maintenance verified it this assessment) AND the plane
	// assessment justifies the claim. Freshness alone is not sufficiency:
	// current source proves the source did not change, not that every
	// inference drawn from it is sound. Closes with the fact receipts recorded
	// as evidence; no question.
	ResolutionEvidencedCurrent ClaimResolution = "evidenced_current"
	// Same shape of claim, but maintenance found it stale: the evidence that
	// once supported it is no longer the source. Resolution is re-extraction
	// and reassessment — which may confirm, contradict, replace or retire the
	// claim. Refreshing never establishes truth by itself.
	ResolutionStaleSource ClaimResolution = "stale_source"
	// The claim reaches past the source (authored, intended/desired, or a
	// premise that is not a pinned source fact): someone must answer it.
	ResolutionNeedsQuestion ClaimResolution = "needs_question"
	// Source-shaped, but its freshness or support is unknown or conflicting.
	ResolutionNeedsInvestigation ClaimResolution = "needs_investigation"
	// The machinery itself cannot offer a path (INV-CLOSURE-LIVENESS).
	ResolutionToolingDefect ClaimResolution = "tooling_defect"
)

// ResolveClaim decides the resolution of one claim from its premise receipts,
// its maintenance evaluation and its plane state for THIS assessment. A nil
// evaluation means freshness was not verified, which is never treated as
// current; a plane state other than justified is never treated as sufficient.
func ResolveClaim(c architecture.Claim, receipts map[string]architecture.ClaimFactReceipt, ev *maintenance.ClaimEvaluation, planeState string) ClaimResolution {
	if !sourceShaped(c, receipts) {
		return ResolutionNeedsQuestion
	}
	if ev == nil {
		return ResolutionNeedsInvestigation
	}
	switch ev.EvaluatedStatus {
	case architecture.StatusStale:
		return ResolutionStaleSource
	case architecture.StatusSupported:
		if ev.ProofLanes.PremiseFacts.State == maintenance.LaneCurrent && planeState == plane.StateJustified {
			return ResolutionEvidencedCurrent
		}
	}
	return ResolutionNeedsInvestigation
}

// sourceShaped: a mechanically derived observed claim whose every premise is a
// source-file fact carrying a pinned digest. This is the exact shape the
// earlier self-evidenced rule accepted (#230) — not wider. Whether the pinned
// digest is still the file's content is NOT decided here: that is freshness,
// and only maintenance's source verification answers it.
func sourceShaped(c architecture.Claim, receipts map[string]architecture.ClaimFactReceipt) bool {
	if c.AssertionOrigin != architecture.OriginDerived || c.ArchitecturalPlane != architecture.PlaneObserved || len(c.PremiseFacts) == 0 {
		return false
	}
	for _, id := range c.PremiseFacts {
		r, ok := receipts[id]
		if !ok || r.Provenance.SourceKind != "source_file" || strings.TrimSpace(r.Fact.Evidence.SourceFile) == "" ||
			!r.Provenance.SourceDigestPinsContent() {
			return false
		}
	}
	return true
}

// combineResolutions folds the resolutions of every claim a blocker names into
// the one that governs it: the most demanding wins. A claim with no resolution
// (not in scope, unknown ID) counts as needs_investigation, so an incomplete
// set can never produce evidenced_current — the old generator required EVERY
// referenced claim to be resolved, and that guarantee is kept.
func combineResolutions(claimIDs []string, by map[string]ClaimResolution) ClaimResolution {
	rank := map[ClaimResolution]int{ResolutionEvidencedCurrent: 1, ResolutionStaleSource: 2,
		ResolutionNeedsInvestigation: 3, ResolutionNeedsQuestion: 4, ResolutionToolingDefect: 5}
	var best ClaimResolution
	for _, id := range claimIDs {
		d, ok := by[id]
		if !ok || rank[d] == 0 {
			d = ResolutionNeedsInvestigation
		}
		if rank[d] > rank[best] {
			best = d
		}
	}
	return best
}

// ResolutionAttestation proves that a Report's claim resolutions were computed
// by Evaluate in this process and that the report has not changed since. Its
// field is unexported: no other package can construct a non-zero value, so a
// report loaded from a caller-editable file carries no attestation and its
// stamped claim_resolution fields cannot suppress anything (fail-closed). This
// verifies the AUTHORITY of the decision instead of recomputing it.
type ResolutionAttestation struct {
	digest string
}

// Covers reports whether the attestation was issued for exactly this report.
func (a ResolutionAttestation) Covers(r Report) bool {
	if a.digest == "" {
		return false
	}
	d, err := closureprotocol.SemanticDigest(r)
	return err == nil && d == a.digest
}

// EvaluateAttested is Evaluate plus the attestation of its claim resolutions.
func EvaluateAttested(ctx Context) (Report, ResolutionAttestation, error) {
	r, err := Evaluate(ctx)
	if err != nil {
		return r, ResolutionAttestation{}, err
	}
	d, err := closureprotocol.SemanticDigest(r)
	if err != nil {
		return r, ResolutionAttestation{}, err
	}
	return r, ResolutionAttestation{digest: d}, nil
}

// Responsible parties for a blocker's next action.
const (
	PartySensei       = "sensei"
	PartyMaintainer   = "sensei_maintainer"
	PartyImplementor  = "implementor"
	PartyArchitect    = "architect"
	PartyHumanOrAgent = "appropriate_human_or_agent"
)

const (
	ActionRefreshExtraction   = "refresh_extraction"
	ActionReportToolingDefect = "report_tooling_defect"
)

// DischargePaths names, for every required_next_action closure may emit, who
// can discharge it (INV-CLOSURE-LIVENESS). An action absent from this table
// has no known path out and is reported as a tooling defect, never as a gap in
// the implementor's or architect's knowledge.
var DischargePaths = map[string]string{
	"create_open_question":                          PartySensei, // question generation creates it; the question then has its own addressee
	"answer_open_question":                          PartyHumanOrAgent,
	"add_evidence":                                  PartyImplementor,
	"add_evidence_or_govern_knowledge":              PartyImplementor,
	"add_failure_mode":                              PartyImplementor,
	"add_test":                                      PartyImplementor,
	"provide_input":                                 PartyImplementor,
	"reassess_scope":                                PartyImplementor,
	"define_authority":                              PartyArchitect,
	"define_contract":                               PartyArchitect,
	"promote_architectural_knowledge":               PartyArchitect,
	"resolve_contradiction":                         PartyArchitect,
	"acknowledge_bootstrap_direction_authorization": PartyArchitect,
	"review_machine_adopted_knowledge":              PartyArchitect,
	"repair_claim":                                  PartySensei,
	"repair_binding":                                PartySensei,
	"execute_awareness_mutation_verification":       PartySensei,
	ActionRefreshExtraction:                         PartySensei,
	ActionReportToolingDefect:                       PartyMaintainer,
}

// questionOnly: actions whose only discharge is a question (or the evidence a
// question asks a human for). For a claim the generator will not ask about,
// demanding one of these is an impossible obligation.
var questionOnly = map[string]bool{"create_open_question": true, "add_evidence": true}

// livenessDefect reports why a blocker has no legitimate path out, or "".
func livenessDefect(bl Blocker) string {
	if _, ok := DischargePaths[bl.RequiredNextAction]; !ok {
		return "required_next_action " + bl.RequiredNextAction + " has no registered discharge path"
	}
	switch ClaimResolution(bl.ClaimResolution) {
	case ResolutionEvidencedCurrent, ResolutionStaleSource:
		if questionOnly[bl.RequiredNextAction] {
			return bl.Code + " demands " + bl.RequiredNextAction + " for a claim disposed " + bl.ClaimResolution + ", which no question can resolve"
		}
	}
	return ""
}
