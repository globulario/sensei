// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=server.risk_classify
// @awareness file_role=pure_risk_classifier_for_preflight
// @awareness risk=medium
package main

// risk_classify.go — pure risk classifier consumed by Preflight.
//
// Design:
//   - input: anchored facts (direct invariants/intents/failure_modes),
//     pattern matches, coverage summary, files touched
//   - output: (RiskClass, reasons) — reasons are appended to blind_spots
//     so the agent sees WHY the classifier chose its verdict
//   - no I/O, no randomness, no time-dependence — fully testable
//
// Discipline (from the user brief's adjustment 2):
//   1. coverage.sufficient gates everything — `!sufficient` → UNKNOWN_IMPACT,
//      regardless of anything else
//   2. direct anchors dominate — when anchors exist, the classifier runs
//      keyword rules against them and ignores pattern-only signals
//   3. pattern-only (no anchors) + high-risk path → ARCHITECTURE_SENSITIVE
//      minimum; never auto-LOW_RISK
//   4. pattern-only + no high-risk path + sufficient coverage → LOW_RISK
//      (the "I see a recipe, file path looks safe" case)
//   5. no anchors, no patterns, coverage sufficient → LOW_RISK
//      (the "graph knows this area, says nothing applies" case)
//   6. risk is never invented from text alone — every category requires an
//      anchored title/ID match or a structural signal (severity, path)

import (
	"strings"

	"github.com/globulario/awareness-graph/golang/coverage"
	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
)

// highRiskDirPrefixes — anchors-or-pattern hitting any of these escalates risk
// to at least ARCHITECTURE_SENSITIVE. The canonical list lives in the coverage
// package so the risk classifier and the coverage weighting stay in lockstep.
var highRiskDirPrefixes = coverage.HighRiskDirPrefixes

// Keyword groups — matched against the concatenated lowercase
// "id title summary" haystack of anchored entities. Order in this file
// has no effect; classifier evaluates the groups in the priority order
// hard-coded below (data_loss > security > convergence > arch).

var dataLossKeywords = []string{
	"data_loss", "data-loss",
	"blob_missing", "blob.missing",
	"minio.format", "format_json", "format.json",
	"etcd.wipe", "scylla.wipe", "member_wipe",
	"delete.permanent", "purge_must",
	"reformat", "destructive_changes_require_approval",
}

var securityKeywords = []string{
	"security.", "rbac.", "pki.", "authentication.", "auth_context",
	"jwt", "mtls",
	"interceptors.bootstrap_gate", "deny_overrides_allow",
	"credential", "signing_key",
	"cert", "tls_",
	"approval_token", "token.must_be_scoped",
}

var convergenceKeywords = []string{
	"convergence.", "reconcil", "drift",
	"desired_state", "installed_state",
	"runtime.identity", "runtime_proof",
	"build_id", "entrypoint_checksum",
	"apply_package", "package_release",
	"state.repository_desired_installed_runtime",
	"layer3", "layer4",
}

// ClassifyInputs is the explicit parameter bundle for classifyRisk —
// passed by value, no hidden state.
type ClassifyInputs struct {
	Direct   []*awarenesspb.KnowledgeNode
	Patterns []*awarenesspb.MatchedImplementationPattern
	Coverage *awarenesspb.CoverageSummary
	Files    []string
}

// classifyRisk runs the priority-ordered rule table and returns
// (RiskClass, reasons). Reasons are surface-level — the caller appends
// them to blind_spots so the agent sees the verdict's basis.
func classifyRisk(in ClassifyInputs) (awarenesspb.RiskClass, []string) {
	// Rule 1 — coverage gate.
	if in.Coverage == nil || !in.Coverage.GetSufficient() {
		return awarenesspb.RiskClass_UNKNOWN_IMPACT, []string{
			"coverage_insufficient: no direct anchors and no strong-tier pattern match",
		}
	}

	hasAnchors := len(in.Direct) > 0
	haystack := strings.ToLower(anchorHaystack(in.Direct))
	hasHighRiskPath := anyPathInHighRiskDir(in.Files)
	hasCriticalAnchor := anyCriticalSeverity(in.Direct)
	hasStrongPattern := anyStrongOrMediumPattern(in.Patterns)

	// Rules 2–6 — anchor-driven classification.
	if hasAnchors {
		if containsAny(haystack, dataLossKeywords) {
			return awarenesspb.RiskClass_DATA_LOSS_RISK,
				[]string{"anchored entity references data-loss keyword"}
		}
		if containsAny(haystack, securityKeywords) {
			return awarenesspb.RiskClass_SECURITY_RISK,
				[]string{"anchored entity in security/auth/rbac/pki/jwt/cert namespace"}
		}
		if containsAny(haystack, convergenceKeywords) {
			return awarenesspb.RiskClass_CONVERGENCE_RISK,
				[]string{"anchored entity references convergence/desired_state/runtime_identity keywords"}
		}
		if hasCriticalAnchor || hasHighRiskPath {
			reasons := []string{}
			if hasCriticalAnchor {
				reasons = append(reasons, "anchor with severity=critical")
			}
			if hasHighRiskPath {
				reasons = append(reasons, "file path under high-risk directory")
			}
			return awarenesspb.RiskClass_ARCHITECTURE_SENSITIVE, reasons
		}
		return awarenesspb.RiskClass_LOW_RISK,
			[]string{"anchors present, no high-risk category fired"}
	}

	// Rule 7 — pattern-only + high-risk path.
	if hasStrongPattern && hasHighRiskPath {
		return awarenesspb.RiskClass_ARCHITECTURE_SENSITIVE,
			[]string{"pattern matched but file path is under high-risk directory — pattern alone does not certify safety"}
	}

	// Rule 8 — pattern-only + clean path.
	if hasStrongPattern {
		return awarenesspb.RiskClass_LOW_RISK,
			[]string{"strong/medium pattern matched and no file is in a high-risk directory"}
	}

	// Rule 9 — high-risk path with zero anchors and no strong pattern.
	// The graph is healthy but uninformed about this file. Distinct from
	// rule 1 (coverage_insufficient = graph could not be queried at all):
	// here the graph WAS queried and returned nothing for a file where
	// the high-risk directory list says we expect it to know something.
	//
	// Returning LOW_RISK here would be a false-OK: the agent would treat
	// silence as proof of safety on a file like rules/heal_policy.go
	// (auto-heal policy authority, no @awareness annotations as of v0.0.10).
	// Instead we mark UNKNOWN_IMPACT with a distinct prefix so the Preflight
	// handler can surface this as DEGRADED + a "read source directly, add
	// candidate annotations" recommendation.
	//
	// The reason prefix "high_risk_path_no_direct_anchors:" is matched by
	// the handler — keep it stable; the test suite asserts on it.
	if hasHighRiskPath {
		return awarenesspb.RiskClass_UNKNOWN_IMPACT, []string{
			"high_risk_path_no_direct_anchors: file is under a high-risk directory but no awareness anchors apply — graph has no facts about this file; treat as unknown, read source directly",
		}
	}

	// Rule 10 — coverage sufficient (file indexed) but nothing fired AND
	// the file is NOT in a high-risk directory. Graph knows the area, no
	// rules apply.
	return awarenesspb.RiskClass_LOW_RISK,
		[]string{"graph indexes this area but no anchored rules apply to the request"}
}

// HighRiskNoAnchorReasonPrefix is the stable reason-string prefix emitted
// by rule 9 when classifyRisk detects a high-risk path with zero direct
// anchors and no strong-tier pattern. The Preflight handler matches this
// prefix to upgrade the response Status from OK to DEGRADED.
const HighRiskNoAnchorReasonPrefix = "high_risk_path_no_direct_anchors:"

// anchorHaystack concatenates id + label + description for keyword
// matching. Lowercased by the caller.
func anchorHaystack(nodes []*awarenesspb.KnowledgeNode) string {
	var b strings.Builder
	for _, n := range nodes {
		if n == nil {
			continue
		}
		b.WriteString(n.GetId())
		b.WriteByte(' ')
		b.WriteString(n.GetLabel())
		b.WriteByte(' ')
		b.WriteString(n.GetDescription())
		b.WriteByte(' ')
	}
	return b.String()
}

func containsAny(haystack string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

func anyPathInHighRiskDir(files []string) bool {
	for _, f := range files {
		for _, prefix := range highRiskDirPrefixes {
			if strings.HasPrefix(f, prefix) {
				return true
			}
		}
	}
	return false
}

func anyCriticalSeverity(nodes []*awarenesspb.KnowledgeNode) bool {
	for _, n := range nodes {
		if n != nil && strings.EqualFold(n.GetSeverity(), "critical") {
			return true
		}
	}
	return false
}

func anyStrongOrMediumPattern(patterns []*awarenesspb.MatchedImplementationPattern) bool {
	for _, p := range patterns {
		s := p.GetMatchStrength()
		if s == "strong" || s == "medium" {
			return true
		}
	}
	return false
}

// computeConfidence runs after classifyRisk so it knows the anchor/pattern
// counts. The rules are simple and bounded:
//
//	HIGH    — ≥3 direct anchors AND coverage sufficient
//	MEDIUM  — 1–2 direct anchors OR a strong-tier pattern match
//	LOW     — anything else (incl. all degraded responses)
func computeConfidence(direct []*awarenesspb.KnowledgeNode, patterns []*awarenesspb.MatchedImplementationPattern, coverage *awarenesspb.CoverageSummary) awarenesspb.Confidence {
	hasSufficient := coverage != nil && coverage.GetSufficient()
	hasStrongPattern := false
	for _, p := range patterns {
		if p.GetMatchStrength() == "strong" {
			hasStrongPattern = true
			break
		}
	}
	if len(direct) >= 3 && hasSufficient {
		return awarenesspb.Confidence_CONFIDENCE_HIGH
	}
	if len(direct) >= 1 || hasStrongPattern {
		return awarenesspb.Confidence_CONFIDENCE_MEDIUM
	}
	return awarenesspb.Confidence_CONFIDENCE_LOW
}
