// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=server.blast_radius
// @awareness file_role=preflight_change_risk_scorer
// @awareness risk=low
package main

// blast_radius.go — Phase 2F. Scores a proposed change's blast radius and the
// approval gate it needs, so an agent gets a clear "safe to patch", "needs
// review", or "manual only" signal. Pure: it takes the touched files, matched
// authority domains, matched repair plans, the risk class, and coverage, and
// returns the strongest (max) blast radius + approval gate across all signals.

import (
	"strings"

	"github.com/globulario/sensei/golang/coverage"
	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// Blast radius labels, ordered least → most severe.
var blastRadiusOrder = []string{"local", "service", "node", "cluster", "security", "data_loss", "external"}

// Approval gate labels, ordered least → most strict.
var approvalGateOrder = []string{"none", "review_required", "human_approval_required", "multi_step_approval_required", "manual_only"}

func rank(order []string, v string) int {
	v = strings.ToLower(strings.TrimSpace(v))
	for i, s := range order {
		if s == v {
			return i
		}
	}
	return -1
}

// maxLabel returns the more severe of a and b per order (unknown labels lose).
func maxLabel(order []string, a, b string) string {
	if rank(order, b) > rank(order, a) {
		return b
	}
	if rank(order, a) < 0 {
		return order[0]
	}
	return a
}

// planAppliesToSubject reports whether a matched repair plan may contribute to
// this change's blast radius and approval gate.
//
// Applicability is established by IDENTITY, not by resemblance: the plan must
// name an invariant, failure mode, forbidden fix or governed contract that is
// among the anchors this subject's own files produced. Identities survive
// renames, moved implementations and new call sites, so a plan cannot become
// applicable because someone described the work in similar words, and cannot
// stop being applicable because a function moved.
//
// It also requires the subject to have sufficient coverage. Without it, an
// anchor coincidence is not evidence of anything: thin coverage is already
// represented conservatively further down, on its own terms, and must not be
// upgraded into a borrowed cluster label here.
func planAppliesToSubject(p loadedRepairPlan, subjectAnchors []string, coverageSufficient bool) bool {
	if !coverageSufficient || len(subjectAnchors) == 0 {
		return false
	}
	subject := make(map[string]bool, len(subjectAnchors))
	for _, a := range subjectAnchors {
		if id := bareAnchorID(a); id != "" {
			subject[id] = true
		}
	}
	for _, group := range [][]string{p.PreservedInvariants, p.FailureModes, p.ForbiddenFixes, p.GovernedContracts} {
		for _, id := range group {
			if subject[bareAnchorID(id)] {
				return true
			}
		}
	}
	return false
}

// bareAnchorID reduces an anchor identity to the form both sides can be
// compared in. Repair plans store bare ids (bareIDFromIRI strips the IRI), while
// knowledge nodes carry a class prefix such as "invariant:". Comparing the two
// unnormalised forms would make every intersection empty, which would look
// exactly like a working fix while actually disabling repair-plan authority
// altogether.
func bareAnchorID(id string) string {
	s := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(id, "<"), ">"))
	if slash := strings.LastIndexByte(s, '/'); slash >= 0 && slash < len(s)-1 {
		s = s[slash+1:]
	}
	if colon := strings.IndexByte(s, ':'); colon >= 0 && colon < len(s)-1 {
		s = s[colon+1:]
	}
	return s
}

type changeAssessment struct {
	BlastRadius  string
	ApprovalGate string
	Reasons      []string
}

// assessChangeRisk computes the change's blast radius + approval gate from all
// available signals. Deterministic; the strongest signal wins each axis.
func assessChangeRisk(
	files []string,
	authorityDomains []loadedAuthorityDomain,
	repairPlans []loadedRepairPlan,
	risk awarenesspb.RiskClass,
	coverageSufficient bool,
	hasDirectAnchors bool,
	subjectAnchors []string,
) changeAssessment {
	// hasDirectAnchors and subjectAnchors answer DIFFERENT questions and are
	// deliberately not derived from one another.
	//
	// hasDirectAnchors asks whether this subject is anchored at all, and drives
	// the "we cannot name the owner" escalation below. subjectAnchors asks WHICH
	// governed identities the subject holds, and decides whether a matched
	// repair plan is talking about this subject. Deriving the first from the
	// second was tidier and wrong: widening the applicability set to the classes
	// a plan can name would then have silently stopped the owner-unknown rule
	// from firing.
	blast := "local"
	gate := "none"
	var reasons []string
	bump := func(b, g, why string) {
		nb := maxLabel(blastRadiusOrder, blast, b)
		ng := maxLabel(approvalGateOrder, gate, g)
		if nb != blast || ng != gate {
			reasons = append(reasons, why)
		}
		blast, gate = nb, ng
	}

	authCovers := authorityCoversPaths(authorityDomains)

	// Path-class signals.
	for _, f := range files {
		switch {
		case inAnyPrefixServer(f, []string{"golang/rbac/", "golang/security/"}):
			bump("security", "human_approval_required", "touches RBAC/security path")
		case strings.HasPrefix(f, "golang/repository/"):
			bump("cluster", "review_required", "touches repository publish/installability path")
		case strings.HasPrefix(f, "golang/cluster_doctor/"):
			bump("cluster", "human_approval_required", "touches doctor remediation path")
		case strings.HasPrefix(f, "golang/workflow/"):
			bump("service", "review_required", "touches workflow side-effect/resume path")
		case strings.HasPrefix(f, "golang/cluster_controller/"):
			bump("cluster", "review_required", "touches cluster desired-state path")
		case strings.HasPrefix(f, "golang/node_agent/"):
			bump("node", "review_required", "touches node-agent installed-state path")
		}
	}

	// Repair-plan labels are authored and trustworthy ABOUT THE PLAN. Whether a
	// matched plan applies to THIS subject is a different question, and a match
	// does not answer it: plans are matched partly from task prose, which is
	// guidance rather than coverage. A plan that names nothing this change
	// touches may therefore be perfectly correct and still be about something
	// else, and because bump only ever escalates, letting it vote gave an
	// unrelated subject the last word on this one's authority.
	//
	// So a plan votes only where applicability is ESTABLISHED: the subject has
	// scoped evidence of its own, and the plan names one of the very anchors
	// that evidence produced. An inapplicable plan is not discarded -- it stays
	// in the briefing and the required actions as guidance -- it simply stops
	// deciding how far this change reaches.
	for _, p := range repairPlans {
		if !planAppliesToSubject(p, subjectAnchors, coverageSufficient) {
			continue
		}
		bump(p.BlastRadius, p.ApprovalGate, "matched repair plan "+p.ID)
	}

	// Risk-class signals.
	switch risk {
	case awarenesspb.RiskClass_DATA_LOSS_RISK:
		bump("data_loss", "human_approval_required", "data-loss risk class")
	case awarenesspb.RiskClass_SECURITY_RISK:
		bump("security", "human_approval_required", "security risk class")
	case awarenesspb.RiskClass_CONVERGENCE_RISK:
		bump("cluster", "review_required", "convergence risk class")
	}

	// Unknown authority: a high-risk-by-weight file with no matched authority
	// domain and no anchors — we cannot name the owner, so escalate to review.
	if len(authorityDomains) == 0 && !hasDirectAnchors &&
		coverage.AnyFileHighRiskWeighted(files, authCovers) {
		bump("service", "review_required", "authority owner unknown for a high-risk file")
	}

	// Thin coverage on a non-trivial change escalates to review.
	if !coverageSufficient && coverage.AnyFileHighRiskWeighted(files, authCovers) {
		bump("service", "review_required", "coverage thin for a high-risk file")
	}

	return changeAssessment{BlastRadius: blast, ApprovalGate: gate, Reasons: reasons}
}

func inAnyPrefixServer(path string, prefixes []string) bool {
	path = strings.TrimPrefix(strings.TrimSpace(path), "./")
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// changeAssessmentAction renders the assessment as a single leading action line.
func changeAssessmentAction(a changeAssessment) string {
	line := "Change risk: blast=" + a.BlastRadius + ", approval=" + a.ApprovalGate
	if len(a.Reasons) > 0 {
		line += " (" + strings.Join(a.Reasons, "; ") + ")"
	}
	return line
}

// changeRiskProto projects the assessment into the structured wire form.
//
// It exists so the prose line and the structured fields come from ONE verdict.
// A consumer that had to parse "Change risk: blast=..., approval=..." out of
// required_actions[0] was one wording change away from silently reading "no
// approval required" — the prose is for humans, and this is the contract.
//
// An unrecognized value maps to UNSPECIFIED, never to the safest member. That is
// the whole reason the zero value is UNSPECIFIED rather than LOCAL/NONE: a
// vocabulary this server does not know about must read as "unclassified", which a
// caller can refuse, and never as "safe", which a caller would proceed on.
func changeRiskProto(a changeAssessment) *awarenesspb.ChangeRisk {
	return &awarenesspb.ChangeRisk{
		BlastRadius:  blastRadiusProto(a.BlastRadius),
		ApprovalGate: approvalGateProto(a.ApprovalGate),
		Reasons:      append([]string(nil), a.Reasons...),
	}
}

// blastRadiusEnums and approvalGateEnums are keyed by the exact strings in
// blastRadiusOrder / approvalGateOrder above. Keeping them adjacent to those
// slices is deliberate: adding a severity level there without adding it here
// would otherwise silently downgrade the new level to UNSPECIFIED on the wire.
// TestEveryVocabularyValueHasAnEnum fails when they drift apart.
var blastRadiusEnums = map[string]awarenesspb.BlastRadius{
	"local":     awarenesspb.BlastRadius_BLAST_RADIUS_LOCAL,
	"service":   awarenesspb.BlastRadius_BLAST_RADIUS_SERVICE,
	"node":      awarenesspb.BlastRadius_BLAST_RADIUS_NODE,
	"cluster":   awarenesspb.BlastRadius_BLAST_RADIUS_CLUSTER,
	"security":  awarenesspb.BlastRadius_BLAST_RADIUS_SECURITY,
	"data_loss": awarenesspb.BlastRadius_BLAST_RADIUS_DATA_LOSS,
	"external":  awarenesspb.BlastRadius_BLAST_RADIUS_EXTERNAL,
}

var approvalGateEnums = map[string]awarenesspb.ApprovalGate{
	"none":                         awarenesspb.ApprovalGate_APPROVAL_GATE_NONE,
	"review_required":              awarenesspb.ApprovalGate_APPROVAL_GATE_REVIEW_REQUIRED,
	"human_approval_required":      awarenesspb.ApprovalGate_APPROVAL_GATE_HUMAN_APPROVAL_REQUIRED,
	"multi_step_approval_required": awarenesspb.ApprovalGate_APPROVAL_GATE_MULTI_STEP_APPROVAL_REQUIRED,
	"manual_only":                  awarenesspb.ApprovalGate_APPROVAL_GATE_MANUAL_ONLY,
}

func blastRadiusProto(s string) awarenesspb.BlastRadius {
	if v, ok := blastRadiusEnums[strings.TrimSpace(s)]; ok {
		return v
	}
	return awarenesspb.BlastRadius_BLAST_RADIUS_UNSPECIFIED
}

func approvalGateProto(s string) awarenesspb.ApprovalGate {
	if v, ok := approvalGateEnums[strings.TrimSpace(s)]; ok {
		return v
	}
	return awarenesspb.ApprovalGate_APPROVAL_GATE_UNSPECIFIED
}
