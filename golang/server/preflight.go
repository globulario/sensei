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
	"github.com/globulario/sensei/golang/rdf"
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
		return s.degradedPreflightResponse(task, files, start, req.GetPublicationDomain()), nil
	}
	if err := s.requireCurrentGraphAuthority(ctx, "preflight"); err != nil {
		return nil, err
	}
	requestedDomain := strings.TrimSpace(req.GetDomain())

	resp := &awarenesspb.PreflightResponse{
		Status:    awarenesspb.PreflightStatus_PREFLIGHT_STATUS_OK,
		Coverage:  &awarenesspb.CoverageSummary{},
		Authority: s.graphAuthorityFor(ctx, req.GetPublicationDomain()),
	}
	if err := s.requireDomainWhenAmbiguous(ctx, requestedDomain); err != nil {
		return s.scopeDegradedPreflightResponse(task, files, start, err, req.GetPublicationDomain()), nil
	}
	patternScope := requestedDomain
	if patternScope == "" {
		if domains := s.availableDomains(ctx); len(domains) == 1 {
			patternScope = domains[0]
		} else {
			patternScope = s.homeDomain
		}
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
	for _, file := range files {
		impact, _, _, _, err := s.collectImpact(ctx, file, requestedDomain)
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
		// Anchors prove examination on their own; the index lookup establishes
		// it WITHOUT them, and only that second direction was missing (#220).
		// Before it, a file the graph holds and governs by nothing read
		// identically to a file nobody had ever analysed. The lookup is skipped
		// when an anchor already settled the question.
		examined := len(impact.GetDirectInvariants())+len(impact.GetDirectFailureModes())+len(impact.GetDirectIntents()) > 0
		if !examined {
			var blindSpot string
			examined, blindSpot = s.sourceFileExamined(ctx, file, requestedDomain)
			if blindSpot != "" {
				resp.BlindSpots = append(resp.BlindSpots, blindSpot)
			}
		}
		if examined {
			indexed++
		}
	}

	// A domain whose published slice has gone missing is reported here rather
	// than left to surface as a refused write (#221).
	resp.BlindSpots = append(resp.BlindSpots, s.domainSliceBlindSpots(ctx, patternScope)...)

	// Implementation pattern matching — same engine Briefing uses.
	patterns := []*awarenesspb.MatchedImplementationPattern{}
	if loaded, err := s.loadImplementationPatterns(ctx); err == nil {
		narrowFile := ""
		if len(files) > 0 {
			narrowFile = files[0]
		}
		patterns = matchPatternsForBriefing(task, narrowFile, inScopePatterns(loaded, patternScope))
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
		for _, in := range matchIntentsForTask(task, inScopeIntents(loaded, patternScope)) {
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

	// Risk classify (pure function). The canonical protection signal is
	// derived here (I/O boundary) — classifyRisk itself stays pure and never
	// touches the filesystem (contract §10).
	directAll := mergeAnchors(resp.DirectInvariants, resp.DirectFailureModes, resp.DirectIntents)
	protAssessment := s.assessCanonicalProtection(files)
	risk, reasons := classifyRisk(ClassifyInputs{
		Direct:     directAll,
		Patterns:   patterns,
		Coverage:   resp.Coverage,
		Files:      files,
		Protection: protAssessment,
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
		// Applicability is decided from the COMPLETE governed anchor set, not
		// from the response. resp.Direct* are capped for presentation -- as few
		// as three invariants and two failure modes -- and directAll carries
		// neither forbidden fixes nor architecture contracts at all. Deciding
		// applicability from that view would deny a legitimately applicable plan
		// its vote whenever the relationship ran through an anchor that had been
		// capped out or was never presented, which is the false negative that
		// makes a gate look repaired while it is quietly disabled.
		//
		// Caps are a presentation limit. They must not become an epistemic one.
		// The complete set, minus what lifecycle scoring already removed from
		// primary guidance. The capped response view had two properties: it was
		// shortened for a reader AND it had deprecated, superseded and retired
		// nodes filtered out. Taking the uncapped collections restored the
		// second problem while fixing the first -- a retired anchor could
		// re-enable a plan's blast radius while the same response was saying
		// that knowledge is not primary guidance. Caps are presentation;
		// lifecycle is not.
		isPrimary := func(n *awarenesspb.KnowledgeNode) bool {
			return isPrimaryStatus(n.GetStatus(), s.scoreNode(ctx, n.GetIri()))
		}
		assessment := assessChangeRisk(files, authorityDomains, matchedRepairPlans, risk,
			resp.Coverage.GetSufficient(), len(directAll) > 0,
			subjectAnchorIDs(
				primaryOnly(allInvariants, isPrimary),
				primaryOnly(allFailureModes, isPrimary),
				primaryOnly(allForbiddenFixes, isPrimary),
				primaryOnly(allArchitecture, isPrimary)))
		// The SAME verdict, published twice: as the prose line existing consumers
		// already read, and as structured fields. Both are derived from one
		// assessment rather than computed twice, so the sentence and the fields
		// cannot disagree about whether a change needs approval.
		resp.ChangeRisk = changeRiskProto(assessment)
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
		(coverage.AnyFileHighRiskWeighted(files, authorityCoversPaths(authorityDomains)) || protAssessment.Protected) {
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

	// Protection-assessment-itself-untrustworthy gate (contract §5 correction):
	// a repository context WAS bound but derivation failed, or coverage came
	// back DEGRADED, is a stronger signal than "no anchors" alone — it means
	// the canonical protection owner could not vouch for this file at all.
	// This must escalate regardless of anchors; an anchored-but-otherwise-OK
	// response must not silently imply protection coverage is trustworthy
	// when it manifestly is not. AvailabilityUnbound (no repository context
	// configured — the normal state for a server not bound to one exact
	// repo) does NOT fire this; it is not itself a degradation.
	//
	// The blind-spot reason itself already rode in via classifyRisk's
	// returned reasons (appended to resp.BlindSpots above) — this only
	// decides the Status escalation, so the text is never duplicated.
	if _, degraded := protectionAssessmentDegradedReason(protAssessment); degraded {
		resp.Status = awarenesspb.PreflightStatus_PREFLIGHT_STATUS_DEGRADED
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
func (s *server) degradedPreflightResponse(task string, files []string, start time.Time, publicationDomain string) *awarenesspb.PreflightResponse {
	return &awarenesspb.PreflightResponse{
		Status:     awarenesspb.PreflightStatus_PREFLIGHT_STATUS_DEGRADED,
		RiskClass:  awarenesspb.RiskClass_UNKNOWN_IMPACT,
		Confidence: awarenesspb.Confidence_CONFIDENCE_LOW,
		Authority:  degradedAuthority(s, publicationDomain, "the store is unavailable, so no publication could be resolved"),
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

func (s *server) scopeDegradedPreflightResponse(task string, files []string, start time.Time, scopeErr error, publicationDomain string) *awarenesspb.PreflightResponse {
	return &awarenesspb.PreflightResponse{
		Status:     awarenesspb.PreflightStatus_PREFLIGHT_STATUS_DEGRADED,
		RiskClass:  awarenesspb.RiskClass_UNKNOWN_IMPACT,
		Confidence: awarenesspb.Confidence_CONFIDENCE_LOW,
		Authority:  degradedAuthority(s, publicationDomain, "the domain scope could not be verified, so no publication could be resolved"),
		Coverage: &awarenesspb.CoverageSummary{
			FileCount:  int32(len(files)),
			Sufficient: false,
			Note:       "domain scope could not be verified — response is not proof of safety",
		},
		BlindSpots: []string{
			"domain_scope_unverified: " + scopeErr.Error(),
			"risk_class is UNKNOWN_IMPACT until the requested repo/domain is corrected",
		},
		RequiredActions: []string{
			"Correct --domain to one of the graph's selectable repository domains",
			"Retry preflight with the corrected domain before editing",
		},
		GeneratedInMs: time.Since(start).Milliseconds(),
	}
}

// computePreflightCoverage decides whether the graph actually looked at what
// was asked about. Coverage rests on lookups: anchors that fired, or files the
// graph has indexed.
//
// EVERY requested file must be examined, not merely one of them: an examined
// file's silence says nothing about the file beside it that the graph has never
// seen, and the summary speaks for the whole request.
//
// A matched implementation pattern is deliberately NOT one of them. A pattern
// is a recipe that recognises the shape of code from task text and file naming;
// it is good grounds for advice (must_follow, required_calls) and poor grounds
// for silence. "Code like this usually looks like that" is not "these files
// were examined and nothing governs them", and sufficient is the field whose
// whole purpose is to separate those two answers. A strong match still reports
// itself in the note, and still travels in the response as guidance.
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
	case len(files) > 0 && indexed == len(files):
		sufficient = true
		note = fmt.Sprintf("%d/%d file(s) examined in the graph — no governing rule applies", indexed, len(files))
	case hasStrongPattern:
		sufficient = false
		note = "strong-tier implementation pattern match — recipe identified, but no anchors fired and no files indexed: guidance, not coverage"
	default:
		sufficient = false
		switch {
		case indexed > 0:
			// Partial examination is not coverage of the request. The examined
			// file's silence says nothing about the one beside it that the
			// graph has never seen.
			note = fmt.Sprintf("only %d of %d requested file(s) are examined in the graph — the rest are unknown to it", indexed, len(files))
		case len(files) > 0:
			note = "no anchors fired, no files examined — coverage thin for this area"
		default:
			note = "no direct anchors and no examined files — task-only request without graph evidence"
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

// sourceFileExamined reports whether the graph holds a SourceFile node for
// this path — that the graph looked at the file — independently of whether
// anything governs it.
//
// This is the fact that makes "examined, and nothing governs it" expressible
// at all. Deriving examination from anchors (as this did before #220) made it
// the same fact as governance, so a file the graph holds and governs by
// nothing was indistinguishable from a file nobody ever analysed, and the
// coverage branch written for that state could never be reached.
//
// It fails closed in every direction it cannot answer: a failed lookup and a
// path that names files in several repositories both report NOT examined, the
// latter with a blind spot, because silently picking one repository's file is
// the identity collapse SourceFile scoping (#197) removed. When the caller
// named a domain, source files belonging to a DIFFERENT repository are
// dropped; identities that predate repository scoping carry no repository to
// compare and are neither dropped nor used to discriminate.
func (s *server) sourceFileExamined(ctx context.Context, file, requestedDomain string) (bool, string) {
	iris, err := s.store.SourceFileIRIsForPath(ctx, file)
	if err != nil {
		return false, fmt.Sprintf("index_lookup_failed_for_%s: %v", file, err)
	}
	if len(iris) == 0 {
		return false, ""
	}
	if requestedDomain != "" {
		return sourceFileExaminedInDomain(file, requestedDomain, iris)
	}
	if len(iris) == 1 {
		return true, ""
	}
	return false, ambiguousIndexBlindSpot(file, len(iris))
}

// sourceFileExaminedInDomain answers the same question for a caller that named
// a domain. Only a repository-scoped identity naming that repository is
// evidence for it. An identity that predates repository scoping is NOT: it
// carries no repository, so in a multi-repository graph it may stand for any of
// them, and accepting it would invent the attribution ParseSourceFileIRI
// refuses to invent.
func sourceFileExaminedInDomain(file, requestedDomain string, iris []string) (bool, string) {
	scoped := 0
	unscoped := 0
	for _, iri := range iris {
		id, ok := rdf.ParseSourceFileIRI(iri)
		switch {
		case !ok || id.Generation != rdf.SourceFileGenerationV2:
			unscoped++
		case id.RepositoryIdentity == requestedDomain:
			scoped++
		}
	}
	switch {
	case scoped == 1:
		return true, ""
	case scoped > 1:
		// One repository and one path mint one subject, so this cannot happen
		// from a well-formed graph — which is why it is reported rather than
		// resolved.
		return false, ambiguousIndexBlindSpot(file, scoped)
	case unscoped > 0:
		return false, fmt.Sprintf(
			"index_unscoped_for_%s: the graph's source-file identity predates repository scoping and cannot be attributed to %s",
			file, requestedDomain)
	default:
		return false, ""
	}
}

func ambiguousIndexBlindSpot(file string, count int) string {
	return fmt.Sprintf(
		"index_ambiguous_for_%s: the path names a source file in %d repositories; coverage cannot say which one was examined",
		file, count)
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

// subjectAnchorIDs names every governed anchor a subject's own files produced,
// across the classes a repair plan is able to name: invariants, failure modes,
// forbidden fixes and architecture contracts.
//
// It is fed the UNCAPPED collections deliberately. The scorer needs the
// identities rather than a count, and it needs all of them: an anchor that was
// capped out of the response is still an anchor this subject has.
// primaryOnly drops the nodes lifecycle scoring does not treat as primary
// guidance. It takes the predicate rather than calling the scorer, so the
// collection logic stays testable without a live store.
func primaryOnly(nodes []*awarenesspb.KnowledgeNode, isPrimary func(*awarenesspb.KnowledgeNode) bool) []*awarenesspb.KnowledgeNode {
	if isPrimary == nil {
		return nodes
	}
	out := make([]*awarenesspb.KnowledgeNode, 0, len(nodes))
	for _, n := range nodes {
		if isPrimary(n) {
			out = append(out, n)
		}
	}
	return out
}

func subjectAnchorIDs(lists ...[]*awarenesspb.KnowledgeNode) []string {
	var out []string
	for _, nodes := range lists {
		for _, n := range nodes {
			if id := strings.TrimSpace(n.GetId()); id != "" {
				out = append(out, id)
			}
		}
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

// degradedAuthority keeps a degraded response HONEST about the publication.
//
// A caller that supplied publication_domain and got back "no publication_domain
// was requested" would be told its own question was never asked. The two states
// are different: UNSPECIFIED means nobody asked, UNREADABLE means someone asked
// and this response cannot answer. A degraded path must say the second.
func degradedAuthority(s *server, publicationDomain, why string) *awarenesspb.GraphAuthority {
	a := s.graphAuthority(context.Background())
	if strings.TrimSpace(publicationDomain) == "" {
		return a
	}
	a.CurrentPublication = &awarenesspb.DomainPublication{
		Resolution:      awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNREADABLE,
		RequestedDomain: publicationDomain,
		Domain:          publicationDomain,
		Detail:          why,
	}
	return a
}
