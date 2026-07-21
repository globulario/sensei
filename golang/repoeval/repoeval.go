// SPDX-License-Identifier: AGPL-3.0-only

package repoeval

import (
	"fmt"
	"strings"
)

type Inputs struct {
	IntegrityFails  int
	IntegrityWarns  int
	IntegrityIssues []IntegrityIssue
	UpgradePath     UpgradePath

	WeightedCoveragePercent  int
	CriticalCoveragePercent  int
	CriticalSurfaceTotal     int
	HighRiskCoveragePercent  int
	HighRiskSurfaceTotal     int
	AuthorityCoveragePercent int
	AuthoritySurfaceTotal    int
	UnknownHighRiskCount     int
	UnknownHighRiskFiles     []string

	CriticalHighInvariantCount        int
	MissingCriticalHighInvariantTests int

	ContractFoundCount         int
	ContractSynthesisSafeCount int
	ContractProposalOnlyCount  int
	ContractUnknownCount       int

	StaleFileRefCount int
	StaleFileRefs     []string

	PatternMisuseCount int
	PatternMisuseIDs   []string
}

type IntegrityIssue struct {
	Check    string   `json:"check"`
	Severity string   `json:"severity"`
	Summary  string   `json:"summary"`
	Evidence []string `json:"evidence,omitempty"`
}

type Dimension struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Score   int    `json:"score"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

type Finding struct {
	Kind           string   `json:"kind"`
	Severity       string   `json:"severity"`
	Title          string   `json:"title"`
	Summary        string   `json:"summary"`
	Evidence       []string `json:"evidence,omitempty"`
	Recommendation string   `json:"recommendation,omitempty"`
}

type AgentReadiness struct {
	Score          int      `json:"score"`
	Verdict        string   `json:"verdict"`
	Confidence     string   `json:"confidence"`
	Summary        string   `json:"summary"`
	Blockers       []string `json:"blockers,omitempty"`
	AllowedModes   []string `json:"allowed_modes,omitempty"`
	Requirements   []string `json:"requirements,omitempty"`
	OperatorAdvice []string `json:"operator_advice,omitempty"`
}

type UpgradeCandidate struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind"`
	Title         string   `json:"title"`
	Rationale     string   `json:"rationale"`
	SuggestedFile string   `json:"suggested_file,omitempty"`
	Paths         []string `json:"paths,omitempty"`
}

type UpgradePath struct {
	Summary    string             `json:"summary,omitempty"`
	Invariants []UpgradeCandidate `json:"invariants,omitempty"`
	Contracts  []UpgradeCandidate `json:"contracts,omitempty"`
}

type Report struct {
	OverallScore int    `json:"overall_score"`
	Posture      string `json:"posture"`
	Confidence   string `json:"confidence"`
	// Caveats states the basis of the verdict: what this evaluation did NOT
	// verify, and why confidence is what it is. Emitted so a high score cannot be
	// mistaken for "nothing to worry about".
	Caveats           []string         `json:"caveats,omitempty"`
	AgentReadiness    AgentReadiness   `json:"agent_readiness"`
	UpgradePath       UpgradePath      `json:"upgrade_path,omitempty"`
	IntegrityFindings []IntegrityIssue `json:"integrity_findings,omitempty"`
	Dimensions        []Dimension      `json:"dimensions"`
	Findings          []Finding        `json:"findings"`
	Recommendations   []string         `json:"recommendations"`
}

// deriveConfidence reports how much this evaluation can be trusted, and why it
// is less than "high" — separately from the score. Confidence answers "did we
// verify enough to stand behind this number?", not "is the number good?".
//
// It exists because a flattering score is easy to produce two ways this
// function names explicitly: (1) integrity defects make every higher-level
// quality signal unreliable, and (2) a perfect coverage score over an EMPTY
// surface is 100% of nothing, not evidence of governance. A caveat about corpus
// freshness is always emitted: this evaluation scores the committed corpus and
// does not re-extract from source, so it cannot see corpus↔code drift.
func deriveConfidence(in Inputs) (string, []string) {
	var caveats []string
	if in.IntegrityFails > 0 {
		caveats = append(caveats, fmt.Sprintf(
			"%d graph-integrity check(s) failing — coverage/quality scores are not trustworthy until repaired", in.IntegrityFails))
	}
	if in.CriticalSurfaceTotal == 0 {
		caveats = append(caveats, "critical-coverage scored over an EMPTY measured surface (0 nodes) — the score reflects absence of measured critical surface, not verified governance")
	}
	if in.AuthoritySurfaceTotal == 0 {
		caveats = append(caveats, "authority-coverage scored over an EMPTY measured surface (0 nodes) — not evidence of authority governance")
	}
	if in.HighRiskSurfaceTotal == 0 {
		caveats = append(caveats, "high-risk-coverage scored over an EMPTY measured surface (0 nodes) — note the coverage surface differs from awg audit's high_risk_files list")
	}
	if in.IntegrityWarns > 0 {
		caveats = append(caveats, fmt.Sprintf("%d graph-integrity warning(s) present", in.IntegrityWarns))
	}
	// Always disclosed — never verified by this evaluation.
	caveats = append(caveats, "scores the COMMITTED corpus; freshness vs current source is NOT verified — a fresh extraction (awg bootstrap / make import-graph / proto-contracts) may surface drift this score cannot see")

	switch {
	case in.IntegrityFails > 0:
		// The corpus itself is defective; do not vouch for higher-level scores.
		return "low", caveats
	case in.CriticalSurfaceTotal == 0 || in.AuthoritySurfaceTotal == 0 || in.HighRiskSurfaceTotal == 0 || in.IntegrityWarns > 0:
		// Perfect scores over empty surfaces, or a warned graph, warrant caution.
		return "medium", caveats
	default:
		return "high", caveats
	}
}

func Evaluate(in Inputs) Report {
	graphIntegrity := clamp(100-(in.IntegrityFails*35)-(in.IntegrityWarns*10), 0, 100)

	coverageScore := weightedAveragePairs(
		weightedPair{value: in.WeightedCoveragePercent, weight: 4},
		applicablePair(in.CriticalCoveragePercent, in.CriticalSurfaceTotal, 3),
		applicablePair(in.AuthorityCoveragePercent, in.AuthoritySurfaceTotal, 2),
		applicablePair(in.HighRiskCoveragePercent, in.HighRiskSurfaceTotal, 1),
	)
	if in.UnknownHighRiskCount > 0 {
		coverageScore = clamp(coverageScore-min(25, in.UnknownHighRiskCount*2), 0, 100)
	}

	testAlignment := 50
	if in.CriticalHighInvariantCount > 0 {
		covered := in.CriticalHighInvariantCount - in.MissingCriticalHighInvariantTests
		testAlignment = clamp((covered*100)/in.CriticalHighInvariantCount, 0, 100)
	}

	totalContracts := in.ContractFoundCount + in.ContractSynthesisSafeCount + in.ContractProposalOnlyCount + in.ContractUnknownCount
	contractPosture := 40
	if totalContracts > 0 {
		stable := in.ContractFoundCount + in.ContractSynthesisSafeCount
		contractPosture = clamp((stable*100)/totalContracts-
			(in.ContractProposalOnlyCount*20)/totalContracts-
			(in.ContractUnknownCount*45)/totalContracts, 0, 100)
	}

	architectureDrift := weightedAveragePairs(
		applicablePair(in.CriticalCoveragePercent, in.CriticalSurfaceTotal, 3),
		applicablePair(in.AuthorityCoveragePercent, in.AuthoritySurfaceTotal, 3),
		applicablePair(in.HighRiskCoveragePercent, in.HighRiskSurfaceTotal, 2),
		weightedPair{value: in.WeightedCoveragePercent, weight: 1},
	)
	architectureDrift = clamp(architectureDrift-
		min(20, in.StaleFileRefCount*4)-
		min(20, in.PatternMisuseCount*5)-
		min(20, in.UnknownHighRiskCount*2), 0, 100)

	agentReadinessScore := weightedAverage(
		graphIntegrity, 30,
		coverageScore, 25,
		testAlignment, 20,
		contractPosture, 15,
		architectureDrift, 10,
	)

	dimensions := []Dimension{
		dimension("graph_integrity", "Graph Integrity", graphIntegrity, summaryIntegrity(in)),
		dimension("awareness_coverage", "Awareness Coverage", coverageScore, summaryCoverage(in)),
		dimension("invariant_test_alignment", "Invariant/Test Alignment", testAlignment, summaryTestAlignment(in)),
		dimension("contract_posture", "Contract Posture", contractPosture, summaryContractPosture(in, totalContracts)),
		dimension("architecture_drift", "Architecture Drift", architectureDrift, summaryArchitecture(in)),
		dimension("agent_control_readiness", "Agent Control Readiness", agentReadinessScore, summaryAgentReadiness(in)),
	}

	overall := weightedAverage(
		graphIntegrity, 25,
		coverageScore, 20,
		testAlignment, 20,
		contractPosture, 15,
		architectureDrift, 20,
	)

	confidence, caveats := deriveConfidence(in)
	findings, recommendations := findingsFromInputs(in)
	readiness := evaluateAgentReadiness(in, agentReadinessScore, confidence)
	upgradePath := selectUpgradePath(in, readiness.Verdict)
	recommendations = append(recommendations, readiness.OperatorAdvice...)
	if upgradePath.Summary != "" {
		recommendations = append(recommendations, upgradePath.Summary)
	}
	return Report{
		OverallScore:      overall,
		Posture:           classifyPosture(overall),
		Confidence:        confidence,
		Caveats:           caveats,
		AgentReadiness:    readiness,
		UpgradePath:       upgradePath,
		IntegrityFindings: normalizeIntegrityIssues(in.IntegrityIssues),
		Dimensions:        dimensions,
		Findings:          findings,
		Recommendations:   dedupeStrings(recommendations),
	}
}

func dimension(key, label string, score int, summary string) Dimension {
	return Dimension{
		Key:     key,
		Label:   label,
		Score:   score,
		Status:  classifyDimension(score),
		Summary: summary,
	}
}

func findingsFromInputs(in Inputs) ([]Finding, []string) {
	var findings []Finding
	var recs []string
	if in.IntegrityFails > 0 {
		findings = append(findings, integrityFinding(in))
		recs = append(recs, "Fix graph integrity failures before using the overall score as a client-facing quality signal.")
	}
	if in.UnknownHighRiskCount > 0 {
		findings = append(findings, Finding{
			Kind:           "weak_point",
			Severity:       "high",
			Title:          "High-risk files lack direct awareness anchors",
			Summary:        "Critical or authority-adjacent files exist without direct graph anchors, so intent is under-specified where mistakes cost the most.",
			Evidence:       capStrings(in.UnknownHighRiskFiles, 5),
			Recommendation: "Anchor the highest-risk files first with invariants, intents, or contracts tied to owned behavior.",
		})
		recs = append(recs, "Add direct anchors to the highest-risk ungoverned files.")
	}
	if in.MissingCriticalHighInvariantTests > 0 {
		findings = append(findings, Finding{
			Kind:           "test_gap",
			Severity:       "high",
			Title:          "Critical/high invariants are missing governing tests",
			Summary:        "Some high-value invariants are declared without required_tests or a named non-applicability reason.",
			Recommendation: "Attach governing tests or record explicit non-applicability for every critical/high invariant.",
		})
		recs = append(recs, "Close invariant-to-test gaps on critical and high severity rules.")
	}
	if in.ContractProposalOnlyCount > 0 || in.ContractUnknownCount > 0 {
		findings = append(findings, Finding{
			Kind:           "contract_gap",
			Severity:       "medium",
			Title:          "Part of the contract surface is still provisional",
			Summary:        "Some intent/contract entries remain proposal-only or unknown, which weakens authority and migration guidance.",
			Recommendation: "Promote proposal-only contracts with citations and tests; resolve unknown contract areas with explicit ownership.",
		})
		recs = append(recs, "Turn provisional contract areas into explicit, tested contracts.")
	}
	if in.StaleFileRefCount > 0 {
		findings = append(findings, Finding{
			Kind:           "drift",
			Severity:       "medium",
			Title:          "The graph references files that no longer exist",
			Summary:        "Some graph anchors point at missing files, which is direct evidence of documentation / code drift.",
			Evidence:       capStrings(in.StaleFileRefs, 5),
			Recommendation: "Remove or repair stale file references so architecture evidence matches the repository.",
		})
		recs = append(recs, "Repair stale file references to reduce architecture drift.")
	}
	if in.PatternMisuseCount > 0 {
		findings = append(findings, Finding{
			Kind:           "wrong_pattern",
			Severity:       "medium",
			Title:          "Known pattern misuses are present in the architecture corpus",
			Summary:        "The repository already documents dangerous pattern misuses; they should be treated as explicit review targets in client audits.",
			Evidence:       capStrings(in.PatternMisuseIDs, 5),
			Recommendation: "Audit the documented pattern misuses and confirm whether they are historical guidance or still live risks in the codebase.",
		})
		recs = append(recs, "Review documented pattern misuses as candidate remediation priorities.")
	}
	return findings, recs
}

func summaryIntegrity(in Inputs) string {
	if in.IntegrityFails == 0 && in.IntegrityWarns == 0 {
		return "No integrity warnings or failures in the evaluation inputs."
	}
	if len(in.IntegrityIssues) == 0 {
		return itoa(in.IntegrityFails) + " fail, " + itoa(in.IntegrityWarns) + " warn."
	}
	var checks []string
	for _, issue := range normalizeIntegrityIssues(in.IntegrityIssues) {
		checks = append(checks, issue.Check)
	}
	return itoa(in.IntegrityFails) + " fail, " + itoa(in.IntegrityWarns) + " warn: " + strings.Join(capStrings(checks, 3), ", ") + "."
}

func summaryCoverage(in Inputs) string {
	return "Weighted coverage " + itoa(in.WeightedCoveragePercent) + "%; critical " + percentOrNA(in.CriticalCoveragePercent, in.CriticalSurfaceTotal) +
		"; authority " + percentOrNA(in.AuthorityCoveragePercent, in.AuthoritySurfaceTotal) +
		"; unknown high-risk files " + itoa(in.UnknownHighRiskCount) + "."
}

func summaryTestAlignment(in Inputs) string {
	if in.CriticalHighInvariantCount == 0 {
		return "No critical/high invariants found to measure."
	}
	return itoa(in.MissingCriticalHighInvariantTests) + " of " + itoa(in.CriticalHighInvariantCount) + " critical/high invariants are missing governing tests."
}

func summaryContractPosture(in Inputs, total int) string {
	if total == 0 {
		return "No intent/contract entries were found to score."
	}
	return "Found " + itoa(in.ContractFoundCount) + ", synthesis-safe " + itoa(in.ContractSynthesisSafeCount) +
		", proposal-only " + itoa(in.ContractProposalOnlyCount) + ", unknown " + itoa(in.ContractUnknownCount) + "."
}

func summaryArchitecture(in Inputs) string {
	return "Architecture drift is estimated from critical/authority coverage, stale file references, pattern misuses, and unanchored high-risk files."
}

func summaryAgentReadiness(in Inputs) string {
	switch readinessVerdict(in) {
	case "ready_for_controlled_agents":
		return "The repo has enough governing coverage, tests, and contract clarity for controlled agent changes with confidence."
	case "guarded_repair_only":
		return "The repo can support guarded repair work, but its governing authority is still too thin for broad confident agent changes across high-risk surfaces."
	default:
		return "The repo is not yet aware enough for confident agent operation; hard governance gaps would let plausible edits break behavior."
	}
}

func classifyDimension(score int) string {
	switch {
	case score >= 80:
		return "strong"
	case score >= 50:
		return "mixed"
	default:
		return "weak"
	}
}

type weightedPair struct {
	value  int
	weight int
}

func applicablePair(value, total, weight int) weightedPair {
	if total <= 0 {
		return weightedPair{}
	}
	return weightedPair{value: value, weight: weight}
}

func weightedAveragePairs(pairs ...weightedPair) int {
	var weightedSum int
	var totalWeight int
	for _, pair := range pairs {
		if pair.weight <= 0 {
			continue
		}
		weightedSum += pair.value * pair.weight
		totalWeight += pair.weight
	}
	if totalWeight == 0 {
		return 0
	}
	return clamp(weightedSum/totalWeight, 0, 100)
}

func percentOrNA(percent, total int) string {
	if total <= 0 {
		return "n/a"
	}
	return itoa(percent) + "%"
}

func classifyPosture(score int) string {
	switch {
	case score >= 85:
		return "strong"
	case score >= 70:
		return "good"
	case score >= 50:
		return "mixed"
	default:
		return "fragile"
	}
}

func weightedAverage(vals ...int) int {
	totalWeight := 0
	total := 0
	for i := 0; i+1 < len(vals); i += 2 {
		total += vals[i] * vals[i+1]
		totalWeight += vals[i+1]
	}
	if totalWeight == 0 {
		return 0
	}
	return clamp(total/totalWeight, 0, 100)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func capStrings(in []string, n int) []string {
	if len(in) <= n {
		return in
	}
	return in[:n]
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + (n % 10))
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// evaluateAgentReadiness builds the readiness block. confidence is a REQUIRED
// input (derived by deriveConfidence from integrity/surface signals) — never a
// fixed "high". A tool that judges a repo's readiness must not vouch for its own
// verdict with a confidence it did not earn; that is precisely the dishonesty
// AWG exists to prevent.
func evaluateAgentReadiness(in Inputs, score int, confidence string) AgentReadiness {
	blockers := readinessBlockers(in)
	verdict := readinessVerdict(in)
	readiness := AgentReadiness{
		Score:      score,
		Verdict:    verdict,
		Confidence: confidence,
		Blockers:   blockers,
	}
	switch verdict {
	case "ready_for_controlled_agents":
		readiness.Summary = "Controlled agent work is supportable: high-risk surfaces are governed enough that AWG can brief and judge changes with confidence."
		readiness.AllowedModes = []string{
			"controlled_feature_work",
			"guarded_refactor",
			"governed_repair",
		}
		readiness.Requirements = []string{
			"Run preflight/briefing before edits on targeted files.",
			"Require repair/gate classification on high-risk or authority-adjacent changes.",
		}
		readiness.OperatorAdvice = []string{
			"Use AWG preflight + repair-gate as the default control loop for agent-authored changes.",
		}
	case "guarded_repair_only":
		readiness.Summary = "Agent work is viable only in a narrow governed loop: repair-focused edits with explicit preflight, bounded scope, and post-edit gate checks."
		readiness.AllowedModes = []string{
			"governed_repair",
			"small_scoped_refactor_with_human_review",
		}
		readiness.Requirements = []string{
			"Limit agent work to explicit scope and issue context.",
			"Require post-edit repair report / gate on every agent change.",
			"Do not allow broad exploratory edits on authority or critical surfaces.",
		}
		if lacksReadinessBaseline(in) {
			readiness.Requirements = append(readiness.Requirements,
				"Add real critical/high invariants and explicit contract intent before allowing broader controlled-agent work.")
		}
		readiness.OperatorAdvice = []string{
			"Treat the repo as repair-only until unanchored high-risk files and missing invariant tests are reduced.",
		}
		if lacksReadinessBaseline(in) {
			readiness.OperatorAdvice = append(readiness.OperatorAdvice,
				"Do not mistake a clean bootstrap corpus for full authority: the repo still needs real governing contracts and load-bearing invariants.")
		}
	default:
		readiness.Summary = "The repo is not ready for confident controlled agents: authority is too incomplete for AWG to reliably keep plausible edits from breaking functionality."
		readiness.AllowedModes = []string{
			"human_led_changes_only",
			"evidence_gathering_without_write_authority",
		}
		readiness.Requirements = []string{
			"Anchor critical and authority-adjacent files before allowing agent edits.",
			"Close critical/high invariant test gaps.",
			"Promote provisional contracts into explicit, test-backed authority.",
		}
		readiness.OperatorAdvice = []string{
			"Do not allow autonomous or broad agent edits until the governance blockers are cleared.",
			"Use AWG first to grow governing coverage, not yet to authorize change execution.",
		}
	}
	return readiness
}

func readinessVerdict(in Inputs) string {
	if len(readinessBlockers(in)) > 0 {
		if in.UnknownHighRiskCount >= 3 || in.IntegrityFails > 0 || in.AuthorityCoveragePercent < 50 && in.AuthoritySurfaceTotal > 0 {
			return "not_ready_for_confident_agents"
		}
		return "guarded_repair_only"
	}
	if lacksReadinessBaseline(in) {
		return "guarded_repair_only"
	}
	if in.WeightedCoveragePercent >= 70 &&
		(in.CriticalSurfaceTotal == 0 || in.CriticalCoveragePercent >= 70) &&
		(in.AuthoritySurfaceTotal == 0 || in.AuthorityCoveragePercent >= 70) &&
		in.MissingCriticalHighInvariantTests == 0 &&
		in.ContractUnknownCount == 0 {
		return "ready_for_controlled_agents"
	}
	return "guarded_repair_only"
}

func readinessBlockers(in Inputs) []string {
	var blockers []string
	if in.IntegrityFails > 0 {
		blockers = append(blockers, integrityBlockerSummary(in.IntegrityIssues))
	}
	if in.UnknownHighRiskCount > 0 {
		blockers = append(blockers, "high-risk files still lack direct awareness anchors")
	}
	if in.AuthoritySurfaceTotal > 0 && in.AuthorityCoveragePercent < 60 {
		blockers = append(blockers, "authority surfaces are under-governed")
	}
	if in.CriticalSurfaceTotal > 0 && in.CriticalCoveragePercent < 60 {
		blockers = append(blockers, "critical surfaces are under-governed")
	}
	if in.MissingCriticalHighInvariantTests > 0 {
		blockers = append(blockers, "critical/high invariants are missing governing tests")
	}
	if in.ContractUnknownCount > 0 {
		blockers = append(blockers, "part of the contract surface is still unknown")
	}
	if in.ContractProposalOnlyCount > max(1, in.ContractFoundCount/2) {
		blockers = append(blockers, "too much of the contract surface remains proposal-only")
	}
	if in.StaleFileRefCount > 0 {
		blockers = append(blockers, "stale graph anchors indicate drift between code and governance evidence")
	}
	if in.PatternMisuseCount > 0 {
		blockers = append(blockers, "known pattern misuses remain live review risks")
	}
	return dedupeStrings(blockers)
}

func lacksReadinessBaseline(in Inputs) bool {
	stableContracts := in.ContractFoundCount + in.ContractSynthesisSafeCount
	return stableContracts == 0 || in.CriticalHighInvariantCount == 0
}

func selectUpgradePath(in Inputs, verdict string) UpgradePath {
	path := UpgradePath{
		Invariants: capUpgradeCandidates(in.UpgradePath.Invariants, 3),
		Contracts:  capUpgradeCandidates(in.UpgradePath.Contracts, 3),
	}
	if verdict == "ready_for_controlled_agents" {
		return path
	}
	switch {
	case len(path.Invariants) == 0 && len(path.Contracts) == 0:
		return UpgradePath{}
	case len(path.Invariants) > 0 && len(path.Contracts) > 0:
		path.Summary = "Next upgrade: author the first governing invariants and component contracts on the named paths before expanding agent scope."
	case len(path.Invariants) > 0:
		path.Summary = "Next upgrade: author the first governing invariants on the named high-leverage paths before expanding agent scope."
	case len(path.Contracts) > 0:
		path.Summary = "Next upgrade: author the first explicit component contracts before expanding agent scope."
	}
	return path
}

func capUpgradeCandidates(in []UpgradeCandidate, n int) []UpgradeCandidate {
	if len(in) <= n {
		return in
	}
	return in[:n]
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func normalizeIntegrityIssues(in []IntegrityIssue) []IntegrityIssue {
	out := make([]IntegrityIssue, 0, len(in))
	seen := map[string]bool{}
	for _, issue := range in {
		issue.Check = strings.TrimSpace(issue.Check)
		issue.Severity = strings.TrimSpace(strings.ToLower(issue.Severity))
		issue.Summary = strings.TrimSpace(issue.Summary)
		if issue.Check == "" || issue.Summary == "" {
			continue
		}
		key := issue.Check + "\x00" + issue.Summary
		if seen[key] {
			continue
		}
		seen[key] = true
		issue.Evidence = dedupeStrings(issue.Evidence)
		out = append(out, issue)
	}
	return out
}

func integrityFinding(in Inputs) Finding {
	issues := normalizeIntegrityIssues(in.IntegrityIssues)
	if len(issues) == 0 {
		return Finding{
			Kind:           "integrity",
			Severity:       "high",
			Title:          "Graph integrity checks are failing",
			Summary:        "The evaluation inputs have hard graph or source integrity failures, so higher-level quality signals are not fully trustworthy yet.",
			Recommendation: "Fix YAML / N-Triples / freshness failures before trusting the overall score.",
		}
	}
	var evidence []string
	var labels []string
	for _, issue := range issues {
		labels = append(labels, issue.Check)
		evidence = append(evidence, issue.Check+": "+issue.Summary)
		for _, detail := range capStrings(issue.Evidence, 2) {
			evidence = append(evidence, "  "+detail)
		}
	}
	return Finding{
		Kind:           "integrity",
		Severity:       "high",
		Title:          "Graph integrity checks are failing",
		Summary:        "The governance corpus has specific integrity defects: " + strings.Join(capStrings(labels, 3), ", ") + ". Higher-level quality signals are not fully trustworthy until those are repaired.",
		Evidence:       capStrings(evidence, 8),
		Recommendation: "Fix the named integrity defects before trusting the overall score or authorizing agent edits.",
	}
}

func integrityBlockerSummary(issues []IntegrityIssue) string {
	issues = normalizeIntegrityIssues(issues)
	if len(issues) == 0 {
		return "graph integrity failures make governance output unreliable"
	}
	var checks []string
	for _, issue := range issues {
		checks = append(checks, issue.Check)
	}
	return "graph integrity failures make governance output unreliable: " + strings.Join(capStrings(checks, 3), ", ")
}
