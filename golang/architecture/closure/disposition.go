// SPDX-License-Identifier: AGPL-3.0-only

package closure

import (
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/maintenance"
)

// ClaimDisposition is the ONE authoritative reading of how an extracted claim
// can be resolved (R-CLOSURE-01). Closure computes it once and stamps it on
// every blocker that names the claim; question generation consumes the stamp
// and never re-reads the claim. Two readers each judging "is this claim
// answerable?" is what produced the deadlock: closure demanded a question the
// generator had decided nobody could answer, about a source that had changed
// since it was read.
type ClaimDisposition string

const (
	// Every premise is a source-backed fact whose pinned digest still matches
	// the file (maintenance verified it this assessment). Closes with the fact
	// receipts recorded as evidence; no question.
	DispositionEvidencedCurrent ClaimDisposition = "evidenced_current"
	// Same shape of claim, but maintenance found it stale: the evidence that
	// once supported it is no longer the source. Resolution is re-extraction
	// and reassessment — which may confirm, contradict, replace or retire the
	// claim. Refreshing never establishes truth by itself.
	DispositionStaleSource ClaimDisposition = "stale_source"
	// The claim reaches past the source (authored, intended/desired, or a
	// premise that is not a pinned source fact): someone must answer it.
	DispositionNeedsQuestion ClaimDisposition = "needs_question"
	// Source-shaped, but its freshness or support is unknown or conflicting.
	DispositionNeedsInvestigation ClaimDisposition = "needs_investigation"
	// The machinery itself cannot offer a path (INV-CLOSURE-LIVENESS).
	DispositionToolingDefect ClaimDisposition = "tooling_defect"
)

// DisposeClaim decides the disposition of one claim from its premise receipts
// and its maintenance evaluation for THIS assessment. A nil evaluation means
// freshness was not verified, which is never treated as current.
func DisposeClaim(c architecture.Claim, receipts map[string]architecture.ClaimFactReceipt, ev *maintenance.ClaimEvaluation) ClaimDisposition {
	if !sourceShaped(c, receipts) {
		return DispositionNeedsQuestion
	}
	if ev == nil {
		return DispositionNeedsInvestigation
	}
	switch ev.EvaluatedStatus {
	case architecture.StatusStale:
		return DispositionStaleSource
	case architecture.StatusSupported:
		if ev.ProofLanes.PremiseFacts.State == maintenance.LaneCurrent {
			return DispositionEvidencedCurrent
		}
	}
	return DispositionNeedsInvestigation
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

// combineDispositions folds the dispositions of a blocker's claims into the
// one that governs its resolution: the most demanding wins.
func combineDispositions(ds []ClaimDisposition) ClaimDisposition {
	rank := map[ClaimDisposition]int{DispositionEvidencedCurrent: 1, DispositionStaleSource: 2,
		DispositionNeedsInvestigation: 3, DispositionNeedsQuestion: 4, DispositionToolingDefect: 5}
	var best ClaimDisposition
	for _, d := range ds {
		if rank[d] > rank[best] {
			best = d
		}
	}
	return best
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
	switch ClaimDisposition(bl.ClaimDisposition) {
	case DispositionEvidencedCurrent, DispositionStaleSource:
		if questionOnly[bl.RequiredNextAction] {
			return bl.Code + " demands " + bl.RequiredNextAction + " for a claim disposed " + bl.ClaimDisposition + ", which no question can resolve"
		}
	}
	return ""
}
