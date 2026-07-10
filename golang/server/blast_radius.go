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

	"github.com/globulario/awareness-graph/golang/coverage"
	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
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
) changeAssessment {
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

	// Repair-plan labels are an authored, high-confidence signal.
	for _, p := range repairPlans {
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
