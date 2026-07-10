// SPDX-License-Identifier: AGPL-3.0-only

// Auto-promotion proposal pipeline (Phase 2H).
//
// The graph should learn from repeated outcomes, but promotion must be
// disciplined or it becomes folklore soup. This generates candidate knowledge
// proposals from accumulated signals (outcomes, incidents, agent runs) and
// gates whether a candidate is eligible to be proposed for `active`. It NEVER
// auto-promotes: every proposal is emitted as a candidate for human review.
package extractor

import (
	"fmt"
	"sort"
	"strings"
)

// Candidate classes a proposal can target.
const (
	CandidateInvariant             = "InvariantCandidate"
	CandidateFailureMode           = "FailureModeCandidate"
	CandidateImplementationPattern = "ImplementationPatternCandidate"
	CandidateRepairPlan            = "RepairPlanCandidate"
	CandidateForbiddenFix          = "ForbiddenFixCandidate"
	CandidateAuthorityDomain       = "AuthorityDomainCandidate"
	CandidateRuntimeEvidence       = "RuntimeEvidenceCandidate"
)

// proposalStatusCandidate is the ONLY status a generated proposal may carry.
// Promotion to active is a separate, human-gated step.
const proposalStatusCandidate = "candidate"

// OutcomeSignal is one observed outcome that may support a proposal.
type OutcomeSignal struct {
	OutcomeID      string
	Theme          string // a grouping key (e.g. the knowledge node or finding class)
	Status         string // success | failure | blocked | reverted
	IncidentID     string // set when the outcome was promoted from an incident
	SevereIncident bool
	HumanMarked    bool
	SourcePath     string
}

// PromotionProposal is a generated candidate for human review.
type PromotionProposal struct {
	CandidateID         string
	CandidateClass      string
	Status              string // always proposalStatusCandidate
	Theme               string
	SupportingOutcomes  []string
	SupportingIncidents []string
	SevereHumanMarked   bool
	AuthorityDomain     string
	NonAuthorityScope   bool
	ActivationTrigger   string
	RequiredTests       []string
	Reason              string
	Confidence          string
	SourcePaths         []string
}

// GeneratePromotionProposals groups outcome signals by theme and proposes one
// candidate per theme that has enough support. Proposals are ALWAYS candidates.
func GeneratePromotionProposals(signals []OutcomeSignal, candidateClass string) []PromotionProposal {
	byTheme := map[string][]OutcomeSignal{}
	order := []string{}
	for _, s := range signals {
		if s.Theme == "" {
			continue
		}
		if _, ok := byTheme[s.Theme]; !ok {
			order = append(order, s.Theme)
		}
		byTheme[s.Theme] = append(byTheme[s.Theme], s)
	}

	var out []PromotionProposal
	for _, theme := range order {
		group := byTheme[theme]
		if !hasEnoughSupport(group) {
			continue
		}
		p := PromotionProposal{
			CandidateID:    "candidate." + slugifyTheme(theme),
			CandidateClass: candidateClass,
			Status:         proposalStatusCandidate, // never active
			Theme:          theme,
		}
		seenSrc := map[string]bool{}
		for _, s := range group {
			if s.IncidentID != "" {
				p.SupportingIncidents = append(p.SupportingIncidents, s.IncidentID)
				if s.SevereIncident && s.HumanMarked {
					p.SevereHumanMarked = true
				}
			} else if s.OutcomeID != "" {
				p.SupportingOutcomes = append(p.SupportingOutcomes, s.OutcomeID)
			}
			if s.SourcePath != "" && !seenSrc[s.SourcePath] {
				seenSrc[s.SourcePath] = true
				p.SourcePaths = append(p.SourcePaths, s.SourcePath)
			}
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CandidateID < out[j].CandidateID })
	return out
}

// hasEnoughSupport: >=2 supporting outcomes/incidents, OR one severe incident
// that a human has marked.
func hasEnoughSupport(group []OutcomeSignal) bool {
	if len(group) >= 2 {
		return true
	}
	for _, s := range group {
		if s.SevereIncident && s.HumanMarked {
			return true
		}
	}
	return false
}

// EvaluatePromotionEligibility decides whether a proposal MAY be put forward for
// promotion to active. It never performs the promotion. Returns (eligible,
// blocking reasons).
func EvaluatePromotionEligibility(p PromotionProposal, hasActiveContradiction bool) (bool, []string) {
	var blockers []string

	// A proposal must never already be active — promotion is a separate step.
	if strings.EqualFold(p.Status, "active") || strings.EqualFold(p.Status, "accepted") {
		blockers = append(blockers, "proposal is already active/accepted — promotion must be a separate human-gated step")
	}

	// Support: >=2 outcomes/incidents OR one severe human-marked incident.
	support := len(p.SupportingOutcomes) + len(p.SupportingIncidents)
	if support < 2 && !p.SevereHumanMarked {
		blockers = append(blockers, "needs >=2 supporting outcomes/incidents or one severe human-marked incident")
	}

	// Owner/domain or explicit non-authority scope.
	if strings.TrimSpace(p.AuthorityDomain) == "" && !p.NonAuthorityScope {
		blockers = append(blockers, "needs an owner/authority domain or an explicit non-authority scope")
	}

	// Activation trigger.
	if strings.TrimSpace(p.ActivationTrigger) == "" {
		blockers = append(blockers, "needs an activation trigger")
	}

	// Required tests OR explicit reason not testable.
	if len(p.RequiredTests) == 0 && strings.TrimSpace(p.Reason) == "" {
		blockers = append(blockers, "needs required tests or an explicit reason why not testable")
	}

	// No active contradiction.
	if hasActiveContradiction {
		blockers = append(blockers, "blocked by an active contradiction in the graph")
	}

	// Confidence at least medium.
	switch strings.ToLower(strings.TrimSpace(p.Confidence)) {
	case "medium", "high":
	default:
		blockers = append(blockers, "confidence must be at least medium")
	}

	// Source paths known.
	if len(p.SourcePaths) == 0 {
		blockers = append(blockers, "source paths must be known")
	}

	return len(blockers) == 0, blockers
}

func slugifyTheme(theme string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(theme) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	s := b.String()
	if s == "" {
		return "unnamed"
	}
	return s
}

func (p PromotionProposal) String() string {
	return fmt.Sprintf("%s [%s] status=%s support=%d/%d", p.CandidateID, p.CandidateClass,
		p.Status, len(p.SupportingOutcomes), len(p.SupportingIncidents))
}
