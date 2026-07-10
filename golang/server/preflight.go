// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=server.preflight
// @awareness file_role=grpc_rpc_handler
// @awareness implements=globular.awareness_graph:intent.awareness.briefing_returns_explicit_status
// @awareness implements=globular.platform:intent.awareness.graph_is_compiled_context_not_authority
// @awareness risk=high
package main

// preflight.go — agent-facing pre-edit decision-support handler.
//
// Composes Briefing's matching engine with the pure risk classifier from
// risk_classify.go into one bounded, deterministic response. Reuses
// collectImpact + matchPatternsForBriefing — adds risk classification,
// coverage discipline, and action-oriented response shape.
//
// Discipline (from the user brief):
//   - never invent risk: every category comes from anchored facts
//   - never silently return EMPTY: store unavailable → DEGRADED response
//     with UNKNOWN_IMPACT + blind_spots + retry hint
//   - bounded output: mode controls volume, hard caps inside
//   - existing Briefing behaviour unchanged: Preflight is additive

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/globulario/sensei/golang/coverage"
	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// preflightCaps controls how many entries each list carries per mode.
type preflightCaps struct {
	invariants    int
	failureModes  int
	intents       int
	forbidden     int
	required      int
	patterns      int
	architecture  int // spine + pattern nodes governing the touched files
	actionEntries int // applied to required_actions / files_to_read / tests_to_run / forbidden_fixes
}

var preflightCapsCompact = preflightCaps{
	invariants:    3,
	failureModes:  2,
	intents:       3,
	forbidden:     5,
	required:      5,
	patterns:      1,
	architecture:  6,
	actionEntries: 8,
}

var preflightCapsStandard = preflightCaps{
	invariants:   7,
	failureModes: 5,
	intents:      7,
	forbidden:    10,
	required:     10,
	patterns:     3,
	architecture: 12,
	// Raised from 10: the composed repair-reasoning guidance (change-risk +
	// evidence + repair plan steps + authority + pattern actions) needs room or
	// the tail (e.g. the outcome hook, the authority owner) gets truncated.
	actionEntries: 24,
}

func capsFor(mode awarenesspb.PreflightMode) preflightCaps {
	if mode == awarenesspb.PreflightMode_PREFLIGHT_STANDARD {
		return preflightCapsStandard
	}
	return preflightCapsCompact
}

// Preflight is the gRPC entry point. Never returns a silent empty —
// either OK / EMPTY (with explanatory blind_spots) / DEGRADED.
// Only returns codes.Unavailable if we can't even allocate the response.
//
// @awareness namespace=globular.awareness_graph
// @awareness component=server.preflight
// @awareness implements=globular.awareness_graph:intent.awareness.briefing_returns_explicit_status
// @awareness risk=high
func (s *server) Preflight(ctx context.Context, req *awarenesspb.PreflightRequest) (*awarenesspb.PreflightResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	start := time.Now()
	task := strings.TrimSpace(req.GetTask())
	files := req.GetFiles()
	mode := req.GetMode()
	if mode == awarenesspb.PreflightMode_PREFLIGHT_MODE_UNSPECIFIED {
		mode = awarenesspb.PreflightMode_PREFLIGHT_COMPACT
	}
	caps := capsFor(mode)

	// Degraded path — store unavailable but we still build a useful response.
	if s.store == nil {
		return s.degradedPreflightResponse(task, files, start), nil
	}
	if err := s.requireCurrentGraphAuthority(ctx, "preflight"); err != nil {
		return nil, err
	}

	resp := &awarenesspb.PreflightResponse{
		Status:    awarenesspb.PreflightStatus_PREFLIGHT_STATUS_OK,
		Coverage:  &awarenesspb.CoverageSummary{},
		Authority: s.graphAuthority(ctx),
	}

	var allInvariants []*awarenesspb.KnowledgeNode
	var allFailureModes []*awarenesspb.KnowledgeNode
	var allIntents []*awarenesspb.KnowledgeNode
	var allForbiddenFixes []*awarenesspb.KnowledgeNode
	var allRequiredTests []*awarenesspb.KnowledgeNode
	var allArchitecture []*awarenesspb.KnowledgeNode

	// Per-file impact queries. Single-file failures degrade just that
	// branch; other files keep going.
	indexed := 0
	requestedDomain := strings.TrimSpace(req.GetDomain())
	for _, file := range files {
		impact, _, _, err := s.collectImpact(ctx, file, requestedDomain)
		if err != nil {
			resp.BlindSpots = append(resp.BlindSpots,
				fmt.Sprintf("impact_query_failed_for_%s: %v", file, err))
			continue
		}
		allInvariants = append(allInvariants, impact.GetDirectInvariants()...)
		allFailureModes = append(allFailureModes, impact.GetDirectFailureModes()...)
		allIntents = append(allIntents, impact.GetDirectIntents()...)
		allForbiddenFixes = append(allForbiddenFixes, impact.GetForbiddenFixes()...)
		allRequiredTests = append(allRequiredTests, impact.GetRequiredTests()...)
		allArchitecture = append(allArchitecture, impact.GetDirectArchitecture()...)
		if len(impact.GetDirectInvariants())+len(impact.GetDirectFailureModes())+len(impact.GetDirectIntents()) > 0 {
			indexed++
		}
	}

	// Implementation pattern matching — same engine Briefing uses.
	patterns := []*awarenesspb.MatchedImplementationPattern{}
	if loaded, err := s.loadImplementationPatterns(ctx); err == nil {
		narrowFile := ""
		if len(files) > 0 {
			narrowFile = files[0]
		}
		patterns = matchPatternsForBriefing(task, narrowFile, loaded)
	}

	// Intent activation-trigger matching — task phrases pull contract-level
	// intents (screen contracts, safety rules) even when no file is indexed
	// yet. Deduped against file-anchored intents; they then flow through
	// the same caps, trust scoring, and status/coverage logic as anchors.
	if loaded, err := s.loadIntentTriggers(ctx); err == nil {
		already := map[string]bool{}
		for _, n := range allIntents {
			already[n.GetId()] = true
		}
		for _, in := range matchIntentsForTask(task, loaded) {
			if already[in.GetId()] {
				continue
			}
			allIntents = append(allIntents, in)
		}
	}

	// Authority-domain matching (Phase 3) — deterministic path-prefix
	// containment against aw:coversPath. A load error skips authority
	// guidance entirely; we never invent ownership facts.
	var authorityDomains []loadedAuthorityDomain
	if loaded, err := s.loadAuthorityDomains(ctx); err == nil {
		authorityDomains = matchAuthorityDomains(files, loaded)
	}

	// Cap each list per mode (sorted by severity-then-id for determinism).
	resp.DirectInvariants = capNodes(sortBySeverityID(allInvariants), caps.invariants)
	resp.DirectFailureModes = capNodes(sortBySeverityID(allFailureModes), caps.failureModes)
	resp.DirectIntents = capNodes(sortBySeverityID(allIntents), caps.intents)
	resp.DirectForbiddenFixes = capNodes(sortBySeverityID(allForbiddenFixes), caps.forbidden)
	resp.DirectRequiredTests = capNodes(sortBySeverityID(allRequiredTests), caps.required)
	// Architecture nodes repeat across files (a component anchors many files), so
	// dedup by id before capping.
	resp.DirectArchitecture = capNodes(sortBySeverityID(dedupNodesByID(allArchitecture)), caps.architecture)
	if len(patterns) > caps.patterns {
		patterns = patterns[:caps.patterns]
	}
	resp.ImplementationPatterns = patterns

	// Trust scoring (Phase 2D): prefer accepted/active knowledge, drop
	// deprecated/superseded out of primary guidance (with a caution pointing at
	// the replacement), and flag low-confidence survivors. Runs before risk
	// classification so retired knowledge does not drive the verdict.
	trustCautions := s.applyTrustScoring(ctx, resp)
	resp.BlindSpots = append(resp.BlindSpots, trustCautions...)

	// Coverage — strict: anchors > 0 OR file indexed OR strong pattern match.
	resp.Coverage = computePreflightCoverage(files, indexed,
		resp.DirectInvariants, resp.DirectFailureModes, resp.DirectIntents,
		patterns)

	// Risk classify (pure function).
	directAll := mergeAnchors(resp.DirectInvariants, resp.DirectFailureModes, resp.DirectIntents)
	risk, reasons := classifyRisk(ClassifyInputs{
		Direct:   directAll,
		Patterns: patterns,
		Coverage: resp.Coverage,
		Files:    files,
	})
	resp.RiskClass = risk
	resp.BlindSpots = append(resp.BlindSpots, reasons...)

	// Confidence.
	resp.Confidence = computeConfidence(directAll, patterns, resp.Coverage)

	// Action assembly (bounded by caps.actionEntries).
	resp.RequiredActions = assembleRequiredActions(resp, risk, caps.actionEntries)
	resp.FilesToRead = assembleFilesToRead(resp, caps.actionEntries)
	resp.TestsToRun = assembleTestsToRun(resp, caps.actionEntries)
	resp.ForbiddenFixes = assembleForbiddenFixes(resp, caps.actionEntries)

	// Authority guidance (Phase 3) — surfaced additively through the existing
	// bounded lists. Ownership facts are prepended to required_actions (the
	// wrong-writer bug class is the one this exists to prevent) and forbidden
	// bypasses are prepended to forbidden_fixes. When no domain matched, no
	// authority line appears anywhere.
	if len(authorityDomains) > 0 {
		resp.RequiredActions = prependBounded(
			authorityRequiredActions(authorityDomains), resp.RequiredActions, caps.actionEntries)
		resp.ForbiddenFixes = prependBounded(
			authorityForbiddenBypasses(authorityDomains), resp.ForbiddenFixes, caps.actionEntries)
	}

	// Repair-plan guidance (Phase 2B). When a touched file is in an authority
	// domain a plan repairs — or the task names a finding class it addresses —
	// surface the safe repair route (preconditions, first step, verification,
	// rollback, approval gate, blast radius) ahead of generic actions. The plan
	// is advisory: the owner service and workflow gate execute it.
	var matchedRepairPlans []loadedRepairPlan
	if plans, err := s.loadRepairPlans(ctx); err == nil {
		matchedRepairPlans = matchRepairPlans(task, files, authorityDomains, plans)
		if len(matchedRepairPlans) > 0 {
			resp.RequiredActions = prependBounded(
				repairPlanActions(matchedRepairPlans), resp.RequiredActions, caps.actionEntries)
		}
	}

	// Runtime-evidence requirements (Phase 2C). When a touched file's authority
	// domain requires live proof, surface what evidence is needed and the hard
	// rule that stale/non-owner-path evidence must not become PASS.
	if profiles, err := s.loadRuntimeEvidence(ctx); err == nil {
		if matched := matchRuntimeEvidence(authorityDomains, profiles); len(matched) > 0 {
			resp.RequiredActions = prependBounded(
				evidenceRequirementActions(matched), resp.RequiredActions, caps.actionEntries)
		}
	}

	// Change-risk assessment (Phase 2F): the leading "safe to patch / needs
	// review / manual only" signal. Prepended last so it heads required_actions.
	if len(files) > 0 {
		assessment := assessChangeRisk(files, authorityDomains, matchedRepairPlans, risk,
			resp.Coverage.GetSufficient(), len(directAll) > 0)
		resp.RequiredActions = prependBounded(
			[]string{changeAssessmentAction(assessment)}, resp.RequiredActions, caps.actionEntries)
	}

	// Honest-DEGRADED gate (Phase 5):
	//
	// If the request names a file under a high-risk directory AND no
	// direct anchors apply, the graph has no actionable facts about that
	// file — regardless of whether the classifier returned UNKNOWN_IMPACT
	// (the typical case when coverage.sufficient=false) or any other
	// class via rule 9 (the new "high-risk path, indexed-but-no-anchors"
	// case). The agent must not treat silence as proof of safety.
	//
	// We do NOT change risk_class here — the classifier already returned
	// UNKNOWN_IMPACT for these paths. We escalate Status to DEGRADED so
	// the response visibly signals "best-effort; do not trust as proof
	// of safety" — same shape as the store-unavailable DEGRADED branch
	// at the top of the handler. We also clamp Confidence to LOW and
	// prepend explicit required_actions that point at the candidate
	// annotation workflow (docs/awareness/candidates/) so the agent can
	// close the loop after the edit.
	//
	// This is intentionally additive and deterministic — no graph
	// traversal, no inference. The branch fires only when (a) at least
	// one file in the request is high-risk by WEIGHT (high-risk directory
	// OR authority-domain membership — Phase 4) AND (b) the merged
	// direct-anchor set is empty. Using the weighted classifier rather than
	// a bare directory check means a file an authority domain owns degrades
	// even outside the static high-risk list, while helper/test files in a
	// high-risk directory no longer falsely degrade.
	if len(files) > 0 && len(directAll) == 0 &&
		coverage.AnyFileHighRiskWeighted(files, authorityCoversPaths(authorityDomains)) {
		resp.Status = awarenesspb.PreflightStatus_PREFLIGHT_STATUS_DEGRADED
		resp.Confidence = awarenesspb.Confidence_CONFIDENCE_LOW
		resp.BlindSpots = append(resp.BlindSpots,
			"this is NOT proof of safety — the graph has no facts about this file",
		)
		// Prepend (not append) so the operator's first read is the
		// honest-DEGRADED guidance.
		resp.RequiredActions = append([]string{
			"Read the source file directly before editing — Preflight has no anchored facts",
			"After your edit, file any newly-discovered invariants/failure_modes as candidates in docs/awareness/candidates/ so future Preflight calls become richer",
		}, resp.RequiredActions...)
		// Cap the prepended actions back to the mode-bounded limit.
		if len(resp.RequiredActions) > caps.actionEntries {
			resp.RequiredActions = resp.RequiredActions[:caps.actionEntries]
		}
	}

	// Status: EMPTY only when truly nothing returned AND coverage was deemed sufficient.
	// (Insufficient coverage already steers risk to UNKNOWN_IMPACT — that's
	// not "empty", that's "unknown".)
	//
	// Order note: this check runs AFTER the Phase-5 honest-DEGRADED gate
	// above. If that gate already set Status=DEGRADED we MUST NOT
	// overwrite it here — DEGRADED is strictly stronger than EMPTY for
	// the agent's decision-making (it explicitly says "do not trust as
	// proof of safety"), while EMPTY can be misread as "graph is happy
	// and has nothing to say".
	if resp.Status != awarenesspb.PreflightStatus_PREFLIGHT_STATUS_DEGRADED &&
		len(directAll) == 0 && len(patterns) == 0 {
		resp.Status = awarenesspb.PreflightStatus_PREFLIGHT_STATUS_EMPTY
	}

	resp.GeneratedInMs = time.Since(start).Milliseconds()
	return resp, nil
}

// degradedPreflightResponse is the bounded fallback for nil store.
// Always carries UNKNOWN_IMPACT + LOW confidence + a retry hint.
func (s *server) degradedPreflightResponse(task string, files []string, start time.Time) *awarenesspb.PreflightResponse {
	return &awarenesspb.PreflightResponse{
		Status:     awarenesspb.PreflightStatus_PREFLIGHT_STATUS_DEGRADED,
		RiskClass:  awarenesspb.RiskClass_UNKNOWN_IMPACT,
		Confidence: awarenesspb.Confidence_CONFIDENCE_LOW,
		Authority:  s.graphAuthority(context.Background()),
		Coverage: &awarenesspb.CoverageSummary{
			FileCount:  int32(len(files)),
			Sufficient: false,
			Note:       "awareness-graph store is unavailable — response is best-effort",
		},
		BlindSpots: []string{
			"awareness_store_unavailable",
			"risk_class is UNKNOWN_IMPACT until the store recovers",
		},
		RequiredActions: []string{
			"Retry preflight after awareness-graph/store is healthy",
			"In the meantime, read the file(s) directly and inspect CLAUDE.md for high-risk guidance",
		},
		GeneratedInMs: time.Since(start).Milliseconds(),
	}
}

// computePreflightCoverage applies the strict rule: anchors > 0 OR file
// indexed OR strong-tier pattern matched.
func computePreflightCoverage(files []string, indexed int,
	invariants, failureModes, intents []*awarenesspb.KnowledgeNode,
	patterns []*awarenesspb.MatchedImplementationPattern) *awarenesspb.CoverageSummary {

	directCount := len(invariants) + len(failureModes) + len(intents)
	hasStrongPattern := false
	for _, p := range patterns {
		if p.GetMatchStrength() == "strong" {
			hasStrongPattern = true
			break
		}
	}

	sufficient := false
	note := ""
	switch {
	case directCount > 0:
		sufficient = true
		note = fmt.Sprintf("%d direct anchor(s) matched", directCount)
	case len(files) > 0 && indexed > 0:
		sufficient = true
		note = fmt.Sprintf("%d/%d file(s) indexed in graph (no rules apply)", indexed, len(files))
	case hasStrongPattern:
		sufficient = true
		note = "strong-tier implementation pattern match — recipe identified"
	default:
		sufficient = false
		if len(files) > 0 {
			note = "no anchors fired, no files indexed — coverage thin for this area"
		} else {
			note = "no direct anchors and no strong pattern — task-only request without graph evidence"
		}
	}
	return &awarenesspb.CoverageSummary{
		DirectAnchorCount: int32(directCount),
		FileCount:         int32(len(files)),
		IndexedFileCount:  int32(indexed),
		Sufficient:        sufficient,
		Note:              note,
	}
}

// mergeAnchors flattens the three direct lists into one. Used by the
// classifier which doesn't care about node sub-class.
// dedupNodesByID keeps the first node seen per id, preserving input order.
func dedupNodesByID(nodes []*awarenesspb.KnowledgeNode) []*awarenesspb.KnowledgeNode {
	seen := make(map[string]bool, len(nodes))
	out := make([]*awarenesspb.KnowledgeNode, 0, len(nodes))
	for _, n := range nodes {
		id := n.GetId()
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, n)
	}
	return out
}

func mergeAnchors(lists ...[]*awarenesspb.KnowledgeNode) []*awarenesspb.KnowledgeNode {
	total := 0
	for _, l := range lists {
		total += len(l)
	}
	out := make([]*awarenesspb.KnowledgeNode, 0, total)
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

// ─── action assemblers ───────────────────────────────────────────────────

// assembleRequiredActions builds concrete next-step strings from anchors +
// patterns + risk class. Output is bounded and deduplicated.
func assembleRequiredActions(resp *awarenesspb.PreflightResponse, risk awarenesspb.RiskClass, cap int) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if s == "" || seen[s] || len(out) >= cap {
			return
		}
		seen[s] = true
		out = append(out, s)
	}

	// Pattern-driven recipe actions (highest signal for routine "create new X"
	// tasks). STRONG matches only: a medium/narrow keyword-overlap match stays
	// visible in implementation_patterns but must not drive actions — dogfooding
	// showed a repository-publish task pulling grpc-client "Call InitClient"
	// actions off a two-keyword overlap, burying the real repair guidance.
	for _, p := range resp.GetImplementationPatterns() {
		if p.GetMatchStrength() != "strong" {
			continue
		}
		for _, ref := range p.GetReferenceFiles() {
			path := stripPatternRefRole(ref)
			add("Read " + path + " before writing new code")
		}
		for _, c := range p.GetRequiredCalls() {
			add("Call " + c + " — required by the matched pattern")
		}
	}

	// Direct invariants → "verify <invariant> still holds".
	for _, inv := range resp.GetDirectInvariants() {
		if inv == nil {
			continue
		}
		label := inv.GetLabel()
		if label == "" {
			label = inv.GetId()
		}
		add("Verify invariant still holds: " + label)
	}

	// Required tests anchored to the file.
	for _, t := range resp.GetDirectRequiredTests() {
		add("Run test: " + t.GetId())
	}

	// Risk-class generic guidance.
	switch risk {
	case awarenesspb.RiskClass_SECURITY_RISK:
		add("Review for security boundary changes (auth, RBAC, PKI, mTLS, JWT)")
	case awarenesspb.RiskClass_CONVERGENCE_RISK:
		add("Walk the 4-layer chain: Repository → Desired → Installed → Runtime")
	case awarenesspb.RiskClass_DATA_LOSS_RISK:
		add("Confirm approval/backup before any destructive change")
	case awarenesspb.RiskClass_ARCHITECTURE_SENSITIVE:
		add("Re-read CLAUDE.md hard rules; call awareness.briefing(file=...) per the rule R2 contract")
	case awarenesspb.RiskClass_UNKNOWN_IMPACT:
		add("Coverage is thin — read the surrounding code, then re-run preflight with --file to narrow")
	}
	return out
}

// assembleFilesToRead pulls reference files from matched patterns + any
// expressed_by paths surfaced via anchors.
func assembleFilesToRead(resp *awarenesspb.PreflightResponse, cap int) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if s == "" || seen[s] || len(out) >= cap {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, p := range resp.GetImplementationPatterns() {
		for _, ref := range p.GetReferenceFiles() {
			add(stripPatternRefRole(ref))
		}
	}
	return out
}

// assembleTestsToRun pulls test ids from direct_required_tests + pattern
// required_tests if the matched pattern proto carries them (v1 patterns
// don't, so this is mostly graph-driven).
func assembleTestsToRun(resp *awarenesspb.PreflightResponse, cap int) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if s == "" || seen[s] || len(out) >= cap {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, t := range resp.GetDirectRequiredTests() {
		add(t.GetId())
	}
	return out
}

// assembleForbiddenFixes pulls human-readable forbid strings from
// direct_forbidden_fixes (graph-anchored) + pattern.forbidden_calls
// (pattern-anchored). Never invented.
func assembleForbiddenFixes(resp *awarenesspb.PreflightResponse, cap int) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if s == "" || seen[s] || len(out) >= cap {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, f := range resp.GetDirectForbiddenFixes() {
		label := f.GetLabel()
		if label == "" {
			label = f.GetId()
		}
		add(label)
	}
	// Same strong-only gate as assembleRequiredActions: a weak keyword match
	// must not inject another domain's forbidden calls into this change's list.
	for _, p := range resp.GetImplementationPatterns() {
		if p.GetMatchStrength() != "strong" {
			continue
		}
		for _, c := range p.GetForbiddenCalls() {
			add("Do not call " + c)
		}
	}
	return out
}

// ─── helpers ─────────────────────────────────────────────────────────────

func capNodes(nodes []*awarenesspb.KnowledgeNode, cap int) []*awarenesspb.KnowledgeNode {
	if len(nodes) <= cap {
		return nodes
	}
	return nodes[:cap]
}

// sortBySeverityID orders critical → high → warning → info → "" by
// severity then alphabetically by id. Determinism matters — callers
// expect stable top-N selection.
func sortBySeverityID(nodes []*awarenesspb.KnowledgeNode) []*awarenesspb.KnowledgeNode {
	severityRank := map[string]int{
		"critical": 4, "high": 3, "warning": 2, "info": 1,
	}
	out := make([]*awarenesspb.KnowledgeNode, len(nodes))
	copy(out, nodes)
	sort.SliceStable(out, func(i, j int) bool {
		ri := severityRank[strings.ToLower(out[i].GetSeverity())]
		rj := severityRank[strings.ToLower(out[j].GetSeverity())]
		if ri != rj {
			return ri > rj
		}
		return out[i].GetId() < out[j].GetId()
	})
	return out
}

// stripPatternRefRole turns "canonical_minimal:path/to/file.go" into
// "path/to/file.go". Same helper as the diagnose composer.
func stripPatternRefRole(s string) string {
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return s
	}
	return s[i+1:]
}

// prependBounded places head entries before tail, deduplicates, and caps the
// merged list. Used to surface authority guidance through the existing
// bounded action lists without exceeding the per-mode entry budget.
func prependBounded(head, tail []string, cap int) []string {
	out := make([]string, 0, cap)
	seen := map[string]bool{}
	for _, lst := range [][]string{head, tail} {
		for _, s := range lst {
			if s == "" || seen[s] || len(out) >= cap {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
