// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=server.briefing
// @awareness file_role=grpc_rpc_handler
// @awareness implements=globular.awareness_graph:intent.awareness.briefing_returns_explicit_status
// @awareness implements=globular.platform:intent.awareness.graph_is_compiled_context_not_authority
package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
	"github.com/globulario/awareness-graph/golang/rdf"
	"github.com/globulario/awareness-graph/golang/store"
)

// Briefing composes a prose context briefing for a file or task.
// Status is always one of OK | EMPTY | DEGRADED — never absent.
// A nil store returns codes.Unavailable, never a silent empty response.
//
// @awareness namespace=globular.awareness_graph
// @awareness component=server.briefing
// @awareness implements=globular.awareness_graph:intent.awareness.briefing_returns_explicit_status
// @awareness enforces=globular.awareness_graph:invariant.awareness.store_unavailable_explicit
// @awareness protects=globular.awareness_graph:failure_mode.awareness.empty_graph_silently_treated_as_no_awareness
// @awareness tested_by=golang/server/main_test.go:TestBriefingStoreNil
// @awareness risk=high
func (s *server) Briefing(ctx context.Context, req *awarenesspb.BriefingRequest) (*awarenesspb.BriefingResponse, error) {
	file := strings.TrimSpace(req.GetFile())
	task := strings.TrimSpace(req.GetTask())
	profile := briefingProfileForDepth(strings.TrimSpace(req.GetDepth()))
	if file == "" && task == "" {
		return nil, status.Error(codes.InvalidArgument, "either file or task must be set")
	}
	if s.store == nil {
		return nil, status.Error(codes.Unavailable, "store is unavailable")
	}
	if err := s.requireCurrentGraphAuthority(ctx, "briefing"); err != nil {
		return nil, err
	}

	start := time.Now()

	// Task-only mode: invariants/failure_modes/intents are file-anchored so
	// we cannot surface those without a file. ImplementationPatterns, however,
	// match against the task text via aw:activationTrigger — those CAN be
	// surfaced from task alone (the agent doesn't have a file yet, only
	// knows they're about to write "a new gRPC client"). So we run the
	// pattern matcher even in task-only mode and return whatever matches.
	if file == "" {
		// Task-only mode has no file to resolve a domain from, so the scope is
		// the explicit request or the home domain — never another repo's rules.
		scope := briefingScope(strings.TrimSpace(req.GetDomain()), "", s.homeDomain)
		var implPatterns []*awarenesspb.MatchedImplementationPattern
		if loaded, err := s.loadImplementationPatterns(ctx); err == nil {
			implPatterns = matchPatternsForBriefing(task, "", inScopePatterns(loaded, scope))
		}
		implPatterns = implPatterns[:min(len(implPatterns), profile.patterns)]
		// Intent activation triggers match from task alone too — a
		// contract-level intent (e.g. a screen's law-of-the-page) must
		// surface before the agent knows any file path or node ID.
		var matchedIntents []*awarenesspb.KnowledgeNode
		if loaded, err := s.loadIntentTriggers(ctx); err == nil {
			matchedIntents = matchIntentsForTask(task, inScopeIntents(loaded, scope))
		}
		var referenced []string
		for _, p := range implPatterns {
			referenced = append(referenced, p.GetId())
		}
		for _, in := range matchedIntents {
			referenced = append(referenced, "intent:"+in.GetId())
		}
		referenced = compactReferencedIDsWithCap(referenced, profile.referencedIDs)
		statusVal := awarenesspb.BriefingStatus_BRIEFING_STATUS_EMPTY
		if len(implPatterns) > 0 || len(matchedIntents) > 0 {
			statusVal = awarenesspb.BriefingStatus_BRIEFING_STATUS_OK
		}
		var pb strings.Builder
		pb.WriteString(composeTaskOnlyBriefingProse(task, implPatterns))
		appendMatchedIntentsSection(&pb, matchedIntents)
		out := &awarenesspb.BriefingResponse{
			Prose:                  pb.String(),
			GeneratedInMs:          time.Since(start).Milliseconds(),
			ReferencedIds:          referenced,
			Status:                 statusVal,
			ImplementationPatterns: implPatterns,
			Authority:              s.graphAuthority(ctx),
		}
		s.logBriefingUsage(req, out, profile)
		return out, nil
	}

	impact, provByID, resolvedScope, err := s.collectImpact(ctx, file, strings.TrimSpace(req.GetDomain()))
	if err != nil {
		// Preserve an already-coded status (e.g. FailedPrecondition for an
		// ambiguous domain scope); only an uncoded error is a backend failure.
		if _, ok := status.FromError(err); ok && status.Code(err) != codes.Unknown {
			return nil, err
		}
		return nil, status.Errorf(codes.Unavailable, "backend query failed: %v", err)
	}
	fileIRI := mintedIRI(rdf.ClassSourceFile, file)
	codeSyms, err := collectCodeSymbols(ctx, s.store, fileIRI)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "code symbol query failed: %v", err)
	}
	impact = limitImpactResponseWithProfile(impact, profile)
	codeSyms = limitCodeSymbolsWithCap(codeSyms, profile.codeSymbols)
	if task == "" {
		task = "(not provided)"
	}

	referenced := make([]string, 0)
	referenced = appendReferencedIDs(referenced, impact.GetDirectInvariants())
	referenced = appendReferencedIDs(referenced, impact.GetDirectFailureModes())
	referenced = appendReferencedIDs(referenced, impact.GetDirectIncidentPatterns())
	referenced = appendReferencedIDs(referenced, impact.GetDirectIntents())
	referenced = appendReferencedIDs(referenced, impact.GetForbiddenFixes())
	referenced = appendReferencedIDs(referenced, impact.GetRequiredTests())
	referenced = appendReferencedIDs(referenced, impact.GetDirectArchitecture())
	existingIRIs := buildExistingIRISet(impact)
	referenced = append(referenced, codeRefIDsFromSymbols(codeSyms, existingIRIs)...)

	// Every briefing SECTION is scoped to the same resolved domain as the
	// impact above — a domain-scoped briefing must not leak another repo's
	// implementation patterns or intent triggers (shared nodes always pass).
	// An unanchored file resolves to "", so fall back to the home domain.
	sectionScope := briefingScope(strings.TrimSpace(req.GetDomain()), resolvedScope, s.homeDomain)

	// Implementation patterns — task/file-shape matched, bounded to 3.
	// A failure here degrades the patterns section only, never the whole
	// briefing. The cache is process-local so the cost is amortised.
	var implPatterns []*awarenesspb.MatchedImplementationPattern
	if loaded, err := s.loadImplementationPatterns(ctx); err == nil {
		implPatterns = matchPatternsForBriefing(task, file, inScopePatterns(loaded, sectionScope))
	}
	implPatterns = implPatterns[:min(len(implPatterns), profile.patterns)]
	for _, p := range implPatterns {
		referenced = append(referenced, p.GetId())
	}

	// Intent activation triggers — task-matched, deduped against intents
	// already anchored to the file. Lets a new/unanchored file still pull
	// the screen contract when the task names the screen.
	var matchedIntents []*awarenesspb.KnowledgeNode
	if loaded, err := s.loadIntentTriggers(ctx); err == nil {
		already := map[string]bool{}
		for _, n := range impact.GetDirectIntents() {
			already[n.GetId()] = true
		}
		for _, in := range matchIntentsForTask(task, inScopeIntents(loaded, sectionScope)) {
			if already[in.GetId()] {
				continue
			}
			matchedIntents = append(matchedIntents, in)
			referenced = append(referenced, "intent:"+in.GetId())
		}
	}

	// Repair plans — matched by the file's authority domain (Phase 2B). When a
	// file belongs to a repository/remediation/workflow/rbac/runtime-evidence
	// authority domain, the applicable repair plans are the safe route back to
	// convergence.
	var repairPlans []loadedRepairPlan
	if domains, derr := s.loadAuthorityDomains(ctx); derr == nil {
		matchedDomains := matchAuthorityDomains([]string{file}, domains)
		if plans, perr := s.loadRepairPlans(ctx); perr == nil {
			repairPlans = matchRepairPlans(task, []string{file}, matchedDomains, plans)
		}
	}
	for _, rp := range repairPlans {
		referenced = append(referenced, "repair_plan:"+rp.ID)
	}

	statusVal := awarenesspb.BriefingStatus_BRIEFING_STATUS_OK
	if len(referenced) == 0 {
		statusVal = awarenesspb.BriefingStatus_BRIEFING_STATUS_EMPTY
	}
	if statusVal == awarenesspb.BriefingStatus_BRIEFING_STATUS_EMPTY && len(implPatterns) > 0 {
		statusVal = awarenesspb.BriefingStatus_BRIEFING_STATUS_OK
	}

	// Principle-based reasoning: when no direct anchors exist for a
	// high-risk file, infer which generative meta-principles apply based
	// on the file's path and role. This turns "no awareness found" into
	// "no specific invariants, but these architectural principles apply."
	var principleGuidance []principleMatch
	if statusVal == awarenesspb.BriefingStatus_BRIEFING_STATUS_EMPTY {
		if principles, err := s.fetchMetaPrinciples(ctx); err == nil {
			principleGuidance = inferPrincipleGuidance(file, principles)
			for _, pg := range principleGuidance {
				referenced = append(referenced, "invariant:"+pg.ID)
			}
			if len(principleGuidance) > 0 {
				statusVal = awarenesspb.BriefingStatus_BRIEFING_STATUS_OK
			}
		}
	}
	referenced = compactReferencedIDsWithCap(referenced, profile.referencedIDs)

	// Rendering groups — cross-file visual consistency contracts.
	var renderingGroups []store.RenderingGroupInfo
	if groups, gerr := s.store.RenderingGroupsForFile(ctx, fileIRI); gerr == nil {
		renderingGroups = groups
	}

	prose := composeBriefingProseWithPrinciples(file, task, impact, codeSyms, implPatterns, principleGuidance, statusVal, profile)
	var ib strings.Builder
	appendMatchedIntentsSection(&ib, matchedIntents)
	prose += ib.String()
	prose += repairPlansBriefingSection(repairPlans)
	prose += renderingGroupsBriefingSection(renderingGroups)
	prose += provenanceBriefingSection(provByID)

	// Realized-contract spine: if this file anchors a contract, follow it up to
	// the architectural guarantee it realizes (+ that contract's invariant and
	// required test) and render it as a repair instruction. Candidates are shown
	// separately and never as authority.
	spineProse, spineRefs := s.realizedContractSpineSection(ctx, impact)
	prose += spineProse
	referenced = append(referenced, spineRefs...)

	out := &awarenesspb.BriefingResponse{
		Prose:                  prose,
		GeneratedInMs:          time.Since(start).Milliseconds(),
		ReferencedIds:          referenced,
		Status:                 statusVal,
		ImplementationPatterns: implPatterns,
		Authority:              s.graphAuthority(ctx),
	}
	s.logBriefingUsage(req, out, profile)
	return out, nil
}

func appendReferencedIDs(dst []string, nodes []*awarenesspb.KnowledgeNode) []string {
	for _, n := range nodes {
		if n == nil || n.GetClass() == "" || n.GetId() == "" {
			continue
		}
		dst = append(dst, n.GetClass()+":"+n.GetId())
	}
	return dst
}

func composeBriefingProse(file, task string, impact *awarenesspb.ImpactResponse, codeSyms []codeSymbol, implPatterns []*awarenesspb.MatchedImplementationPattern, status awarenesspb.BriefingStatus) string {
	return composeBriefingProseWithPrinciples(file, task, impact, codeSyms, implPatterns, nil, status, standardBriefingProfile)
}

func composeBriefingProseWithPrinciples(file, task string, impact *awarenesspb.ImpactResponse, codeSyms []codeSymbol, implPatterns []*awarenesspb.MatchedImplementationPattern, principles []principleMatch, status awarenesspb.BriefingStatus, profile briefingSurfaceProfile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Awareness briefing for %s\n", file)
	fmt.Fprintf(&b, "Task: %s", task)
	if status == awarenesspb.BriefingStatus_BRIEFING_STATUS_EMPTY {
		b.WriteString("\n\nNo direct awareness anchors found.")
		return b.String()
	}
	if impact != nil &&
		len(impact.GetDirectInvariants()) == 0 &&
		len(impact.GetDirectFailureModes()) == 0 &&
		len(impact.GetDirectIncidentPatterns()) == 0 &&
		len(impact.GetDirectIntents()) == 0 &&
		len(impact.GetForbiddenFixes()) == 0 &&
		len(impact.GetRequiredTests()) == 0 &&
		len(impact.GetDirectArchitecture()) == 0 &&
		len(codeSyms) == 0 &&
		len(principles) == 0 &&
		len(implPatterns) > 0 {
		b.WriteString("\n\nNo direct awareness anchors found for this file. Task-level implementation patterns still apply.")
		appendImplementationPatternsSection(&b, implPatterns)
		return b.String()
	}
	// When we have principle-based guidance but no direct anchors, render
	// the principle section instead of specific invariants/failure_modes.
	if len(principles) > 0 && impact != nil &&
		len(impact.GetDirectInvariants()) == 0 &&
		len(impact.GetDirectFailureModes()) == 0 &&
		len(impact.GetDirectArchitecture()) == 0 {
		appendPrincipleGuidanceSection(&b, principles)
		appendImplementationPatternsSection(&b, implPatterns)
		return b.String()
	}
	appendDecisionFocusSection(&b, impact)
	appendCodeContextSection(&b, codeSyms, profile.codeSectionItems)
	appendBriefingSection(&b, "Direct invariants", impact.GetDirectInvariants(), true)
	appendBriefingSection(&b, "Direct failure modes", impact.GetDirectFailureModes(), true)
	appendBriefingSection(&b, "Direct incident patterns", impact.GetDirectIncidentPatterns(), false)
	appendBriefingSection(&b, "Direct intents", impact.GetDirectIntents(), false)
	appendArchitectureSection(&b, impact.GetDirectArchitecture())
	appendBriefingSection(&b, "Forbidden fixes", impact.GetForbiddenFixes(), false)
	appendBriefingSection(&b, "Required tests", impact.GetRequiredTests(), false)
	appendImplementationPatternsSection(&b, implPatterns)
	return b.String()
}

func appendDecisionFocusSection(b *strings.Builder, impact *awarenesspb.ImpactResponse) {
	if impact == nil {
		return
	}
	var lines []string
	if n := firstKnowledgeNode(impact.GetDirectInvariants()); n != nil {
		lines = append(lines, "Respect: "+briefingNodeSummary(n, true))
	}
	if n := firstKnowledgeNode(impact.GetDirectFailureModes()); n != nil {
		lines = append(lines, "Watch for: "+briefingNodeSummary(n, true))
	}
	if n := firstKnowledgeNode(impact.GetRequiredTests()); n != nil {
		lines = append(lines, "Verify with: "+briefingNodeSummary(n, false))
	}
	if n := firstKnowledgeNode(impact.GetDirectIntents()); n != nil {
		lines = append(lines, "Keep intent: "+briefingNodeSummary(n, false))
	}
	if len(lines) == 0 {
		return
	}
	b.WriteString("\n\nDecision focus:")
	for _, line := range lines {
		fmt.Fprintf(b, "\n- %s", line)
	}
}

func firstKnowledgeNode(nodes []*awarenesspb.KnowledgeNode) *awarenesspb.KnowledgeNode {
	for _, n := range nodes {
		if n != nil && n.GetId() != "" {
			return n
		}
	}
	return nil
}

func briefingNodeSummary(n *awarenesspb.KnowledgeNode, withSeverity bool) string {
	if n == nil {
		return ""
	}
	id := n.GetId()
	if withSeverity && n.GetSeverity() != "" {
		id = "[" + n.GetSeverity() + "] " + id
	}
	if n.GetLabel() == "" {
		return id
	}
	return id + " — " + n.GetLabel()
}

// appendArchitectureSection renders the architectural context governing the
// file — Component / Boundary / Contract / Decision / DesignPattern /
// ImplementationPattern / PatternMisuse — each prefixed by its class so an
// agent sees what shape and boundaries apply before editing.
func appendArchitectureSection(b *strings.Builder, nodes []*awarenesspb.KnowledgeNode) {
	if len(nodes) == 0 {
		return
	}
	fmt.Fprintf(b, "\n\nArchitecture (components, boundaries, contracts, decisions, patterns):")
	for _, n := range nodes {
		if n == nil {
			continue
		}
		fmt.Fprintf(b, "\n- [%s] %s", n.GetClass(), n.GetId())
		if n.GetLabel() != "" {
			fmt.Fprintf(b, " — %s", n.GetLabel())
		}
	}
}

// composeTaskOnlyBriefingProse handles the task-only branch where no file
// is provided. Invariants/failure_modes/intents need a file anchor so they
// can't surface, but implementation patterns match via aw:activationTrigger
// against the task text and are surfaced when found.
func composeTaskOnlyBriefingProse(task string, patterns []*awarenesspb.MatchedImplementationPattern) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Awareness briefing (task-only)\nTask: %s", task)
	if len(patterns) == 0 {
		b.WriteString("\n\nNo direct awareness anchors found. " +
			"Invariants, failure modes, and intents are file-anchored — " +
			"re-run with --file <path> to retrieve them. " +
			"Implementation patterns matched no activation triggers in this task text.")
		return b.String()
	}
	appendImplementationPatternsSection(&b, patterns)
	b.WriteString("\n\n(Task-only mode: invariants/failure_modes/intents not " +
		"included because they're file-anchored. Re-run with --file to get them.)")
	return b.String()
}

// appendImplementationPatternsSection renders the matched patterns as a
// compact list. Empty when no patterns matched — never invented. Bounded
// by maxPatternsPerBriefing upstream so this section can't bloat.
//
// Prose vs structured contract:
//   - References, required_calls, forbidden_calls are rendered in prose so
//     they're visible in the narrated briefing.
//   - must_follow steps are intentionally OMITTED from prose because the
//     full list can run 10+ verbose lines per pattern. They remain in
//     the structured BriefingResponse.implementation_patterns[].must_follow
//     field so MCP tools and validators read the full set unaltered.
//   - rationale_summary is structured-only (first line of rationale) —
//     the full rationale stays in the YAML source.
func appendImplementationPatternsSection(b *strings.Builder, patterns []*awarenesspb.MatchedImplementationPattern) {
	if len(patterns) == 0 {
		return
	}
	b.WriteString("\n\nImplementation patterns:")
	for _, p := range patterns {
		fmt.Fprintf(b, "\n- %s [%s]", trimPatternIDPrefix(p.GetId()), p.GetMatchStrength())
		if p.GetLabel() != "" {
			fmt.Fprintf(b, " — %s", p.GetLabel())
		}
		if refs := p.GetReferenceFiles(); len(refs) > 0 {
			b.WriteString("\n  References:")
			for _, ref := range refs {
				fmt.Fprintf(b, "\n  - %s", ref)
			}
		}
		if calls := p.GetRequiredCalls(); len(calls) > 0 {
			b.WriteString("\n  Required calls:")
			for _, c := range calls {
				fmt.Fprintf(b, "\n  - %s", c)
			}
		}
		if calls := p.GetForbiddenCalls(); len(calls) > 0 {
			b.WriteString("\n  Forbidden calls:")
			for _, c := range calls {
				fmt.Fprintf(b, "\n  - %s", c)
			}
		}
	}
}

// trimPatternIDPrefix strips the "implementation_pattern:" prefix when
// present so the prose lists the bare id (e.g. "globular.pattern.foo").
func trimPatternIDPrefix(id string) string {
	const p = "implementation_pattern:"
	if strings.HasPrefix(id, p) {
		return id[len(p):]
	}
	return id
}

// ── Principle-based reasoning ─────────────────────────────────────────────────
//
// When a file has no direct awareness anchors, the briefing infers which
// generative meta-principles (invariant:meta.*) apply based on the file's
// path and likely role. This turns "no awareness found" into actionable
// architectural guidance.

// principleMatch pairs a meta-principle ID with why it applies to this file.
type principleMatch struct {
	ID     string // e.g. "meta.storage_is_not_semantic_authority"
	Label  string // from the graph
	Reason string // why it applies to this specific file
}

// fetchMetaPrinciples queries the graph for all invariant:meta.* nodes.
// Results are cached for the process lifetime (they change only on graph reload).
func (s *server) fetchMetaPrinciples(ctx context.Context) ([]principleMatch, error) {
	s.metaPrincipleOnce.Do(func() {
		facts, err := s.store.ClassFacts(ctx, rdf.ClassInvariant, 200)
		if err != nil {
			return
		}
		type raw struct {
			id, label, severity string
		}
		byIRI := map[string]*raw{}
		for _, f := range facts {
			id, ok := awarenessIDFromIRI(f.NodeIRI)
			if !ok || !strings.HasPrefix(id, "meta.") {
				continue
			}
			r, exists := byIRI[f.NodeIRI]
			if !exists {
				r = &raw{id: id}
				byIRI[f.NodeIRI] = r
			}
			switch f.Predicate {
			case rdf.PropLabel:
				r.label = f.Object
			case rdf.PropSeverity:
				r.severity = f.Object
			}
		}
		for _, r := range byIRI {
			s.metaPrinciples = append(s.metaPrinciples, principleMatch{
				ID:    r.id,
				Label: r.label,
			})
		}
	})
	if len(s.metaPrinciples) == 0 {
		return nil, fmt.Errorf("no meta-principles found in graph")
	}
	return s.metaPrinciples, nil
}

// fileRoleSignals maps file path patterns to the architectural signals
// they carry. Each signal triggers specific meta-principles.
type fileRoleSignal struct {
	pathContains string
	actor        string   // who owns state in this subsystem
	layer        string   // which truth layer (Repository/Desired/Installed/Runtime)
	operations   []string // what kind of state operations (read/write/dispatch/verify/observe)
}

var fileRoleSignals = []fileRoleSignal{
	// Controller — owns desired state, dispatches workflows
	{pathContains: "cluster_controller/", actor: "cluster-controller", layer: "Desired",
		operations: []string{"write_state", "dispatch", "reconcile"}},
	// Node agent — owns installed state, executes installs
	{pathContains: "node_agent/", actor: "node-agent", layer: "Installed",
		operations: []string{"write_state", "verify", "execute"}},
	// Repository — owns artifact identity
	{pathContains: "repository/", actor: "repository", layer: "Repository",
		operations: []string{"write_state", "identity", "publish"}},
	// Workflow — coordinates but does not own state
	{pathContains: "workflow/", actor: "workflow-engine", layer: "",
		operations: []string{"dispatch", "coordinate"}},
	// Doctor — observes but does not own state
	{pathContains: "cluster_doctor/", actor: "cluster-doctor", layer: "",
		operations: []string{"observe", "diagnose"}},
	// RBAC/Security — owns auth decisions
	{pathContains: "rbac/", actor: "rbac", layer: "",
		operations: []string{"auth", "verify"}},
	{pathContains: "security/", actor: "security", layer: "",
		operations: []string{"auth", "identity", "verify"}},
	// MCP — admin surface, must respect ownership boundaries
	{pathContains: "mcp/", actor: "mcp-tools", layer: "",
		operations: []string{"admin", "read_state"}},
	// AI executor — diagnoses and remediates
	{pathContains: "ai_executor/", actor: "ai-executor", layer: "",
		operations: []string{"observe", "remediate"}},
}

// principleApplicability maps (operation type) → (which meta-principles apply + why).
var principleApplicability = map[string][]struct {
	id     string
	reason string
}{
	"write_state": {
		{id: "meta.storage_is_not_semantic_authority", reason: "this file writes state — verify the code is the owning actor for this layer"},
		{id: "meta.write_creates_completion_obligation", reason: "state writes must have a cleanup/completion path"},
		{id: "meta.half_done_must_not_look_done", reason: "multi-step writes must not satisfy completeness checks when interrupted"},
		{id: "meta.competing_writers_must_converge_or_be_fenced", reason: "if multiple instances can write this state, they must agree or be leader-fenced"},
	},
	"read_state": {
		{id: "meta.storage_is_not_semantic_authority", reason: "this file reads state — verify it's reading through the owner's typed API, not the backing store directly"},
		{id: "meta.fallback_must_degrade_semantics", reason: "if this reads from a fallback, the response must carry degraded status"},
		{id: "meta.connection_errors_must_not_be_absorbed", reason: "connection failures to the state source must surface the real error, not a generic timeout"},
	},
	"identity": {
		{id: "meta.identity_computation_must_be_invariant", reason: "identity fields must use the same canonical computation at every site"},
	},
	"dispatch": {
		{id: "meta.silence_is_not_valid_for_unexpected", reason: "dispatch/switch logic must have explicit default handling — silent no-ops block progress"},
		{id: "meta.write_creates_completion_obligation", reason: "dispatched operations create obligations that must be tracked to completion"},
	},
	"observe": {
		{id: "meta.storage_is_not_semantic_authority", reason: "observation produces evidence, not truth — do not write to owned state from an observer"},
		{id: "meta.absence_scope_must_be_explicit", reason: "missing data in observations is unknown, not known-bad"},
		{id: "meta.authority_must_express_uncertainty", reason: "diagnostic results must distinguish absence from uncertainty"},
		{id: "meta.critical_path_no_non_critical_dependency", reason: "observation paths must not block on non-critical services — a hung RPC stalls the observer"},
	},
	"verify": {
		{id: "meta.identity_computation_must_be_invariant", reason: "verification must use the same computation as the producer — not an independent recomputation"},
		{id: "meta.fallback_must_degrade_semantics", reason: "if verification cannot complete, the result must not look like success"},
		{id: "meta.connection_errors_must_not_be_absorbed", reason: "TLS or auth errors during verification must surface, not collapse into timeout"},
	},
	"reconcile": {
		{id: "meta.half_done_must_not_look_done", reason: "reconciliation must distinguish intermediate from terminal state"},
		{id: "meta.silence_is_not_valid_for_unexpected", reason: "unhandled cases in reconciliation routing must fail loudly"},
		{id: "meta.absence_scope_must_be_explicit", reason: "missing nodes or packages may be unknown, not absent"},
		{id: "meta.competing_writers_must_converge_or_be_fenced", reason: "reconcilers must be leader-gated — non-leaders must not touch shared state"},
	},
	"admin": {
		{id: "meta.storage_is_not_semantic_authority", reason: "admin tools must call the owning actor's API — 'admin' means authorized to call, not authorized to bypass ownership"},
	},
	"remediate": {
		{id: "meta.storage_is_not_semantic_authority", reason: "repair is a request to the owner, not direct storage access"},
		{id: "meta.write_creates_completion_obligation", reason: "remediation actions must be tracked to completion"},
	},
	"publish": {
		{id: "meta.identity_computation_must_be_invariant", reason: "publish creates canonical identity — the computation must be the same everywhere it's verified"},
		{id: "meta.half_done_must_not_look_done", reason: "interrupted publish must not leave state that looks published"},
	},
	"execute": {
		{id: "meta.write_creates_completion_obligation", reason: "installation creates records that must be completed or cleaned up"},
		{id: "meta.identity_computation_must_be_invariant", reason: "installed binary identity must match the manifest's canonical checksum"},
		{id: "meta.critical_path_no_non_critical_dependency", reason: "install path must not depend on non-critical services — a hung dependency stalls convergence"},
	},
	"coordinate": {
		{id: "meta.silence_is_not_valid_for_unexpected", reason: "workflow coordination must not silently drop unrecognized step results"},
	},
	"diagnose": {
		{id: "meta.authority_must_express_uncertainty", reason: "diagnostic output must express confidence — unknown is a valid answer"},
		{id: "meta.absence_scope_must_be_explicit", reason: "not-found-where is not does-not-exist"},
	},
	"auth": {
		{id: "meta.authority_must_express_uncertainty", reason: "auth failures must be explicit — never collapse 'denied' and 'unknown' into the same response"},
		{id: "meta.connection_errors_must_not_be_absorbed", reason: "auth connection errors must surface the real error — wrong key or wrong MAC must not look like timeout"},
	},
}

// fileSkipPrinciples returns true for files that should NOT receive
// principle-based guidance because they are unlikely to contain
// architectural state-access logic. This keeps the signal-to-noise
// ratio high by filtering out utilities, tests, generated code, and
// boilerplate.
func fileSkipPrinciples(file string) bool {
	base := file
	if idx := strings.LastIndexByte(file, '/'); idx >= 0 {
		base = file[idx+1:]
	}
	lower := strings.ToLower(base)

	// Generated code — gRPC stubs, proto, auto-generated files.
	for _, suffix := range []string{".pb.go", ".pb.gw.go", "_grpc.pb.go", ".gen.go"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	for _, prefix := range []string{"zz_generated", "zz_version"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}

	// Test files — they verify principles, they don't violate them.
	if strings.HasSuffix(lower, "_test.go") {
		return true
	}

	// Utility / helper / constants — pure logic, no state access.
	for _, pattern := range []string{
		"_util.go", "_utils.go", "_helper.go", "_helpers.go",
		"constants.go", "types.go", "errors.go", "doc.go",
		"string_", "format_", "parse_", "convert_",
	} {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	// Proto directories — all generated.
	if strings.Contains(file, "/pb/") || strings.Contains(file, "/proto/") {
		return true
	}

	return false
}

// inferPrincipleGuidance matches file path signals against meta-principles.
func inferPrincipleGuidance(file string, allPrinciples []principleMatch) []principleMatch {
	// Skip files unlikely to contain architectural state-access logic.
	if fileSkipPrinciples(file) {
		return nil
	}

	var signal *fileRoleSignal
	for i := range fileRoleSignals {
		if strings.Contains(file, fileRoleSignals[i].pathContains) {
			signal = &fileRoleSignals[i]
			break
		}
	}
	if signal == nil {
		return nil // not a recognized subsystem — no inference
	}

	// Build principle label lookup.
	labelByID := map[string]string{}
	for _, p := range allPrinciples {
		labelByID[p.ID] = p.Label
	}

	// Collect applicable principles, deduplicating by ID.
	seen := map[string]bool{}
	var result []principleMatch
	for _, op := range signal.operations {
		applicable, ok := principleApplicability[op]
		if !ok {
			continue
		}
		for _, a := range applicable {
			if seen[a.id] {
				continue
			}
			seen[a.id] = true
			label := labelByID[a.id]
			if label == "" {
				label = a.id // fallback to ID if not in graph
			}
			result = append(result, principleMatch{
				ID:     a.id,
				Label:  label,
				Reason: fmt.Sprintf("[%s/%s] %s", signal.actor, op, a.reason),
			})
		}
	}
	return result
}

func appendPrincipleGuidanceSection(b *strings.Builder, principles []principleMatch) {
	if len(principles) == 0 {
		return
	}
	b.WriteString("\n\nNo direct invariants anchored to this file, but the following generative")
	b.WriteString("\narchitectural principles apply based on this file's role and subsystem:")
	b.WriteString("\n\nApplicable meta-principles:")
	for _, p := range principles {
		fmt.Fprintf(b, "\n- [critical] %s — %s", p.ID, p.Label)
		fmt.Fprintf(b, "\n  Why: %s", p.Reason)
	}
	b.WriteString("\n\nThese principles are generative — they derive correctness for cases that")
	b.WriteString("\ndon't have specific invariants yet. Run the 6-question checklist from")
	b.WriteString("\nmeta.storage_is_not_semantic_authority before any state access:")
	b.WriteString("\n  1. What layer does this state belong to?")
	b.WriteString("\n  2. Which actor owns this truth?")
	b.WriteString("\n  3. Which typed RPC/API is the allowed read path?")
	b.WriteString("\n  4. Which typed RPC/API is the allowed write path?")
	b.WriteString("\n  5. What is the canonical source of identity?")
	b.WriteString("\n  6. Is the value mutable, immutable, observed, derived, or canonical?")
}

func appendBriefingSection(b *strings.Builder, title string, nodes []*awarenesspb.KnowledgeNode, withSeverity bool) {
	if len(nodes) == 0 {
		return
	}
	fmt.Fprintf(b, "\n\n%s:", title)
	for _, n := range nodes {
		if n == nil {
			continue
		}
		if withSeverity && n.GetSeverity() != "" {
			fmt.Fprintf(b, "\n- [%s] %s", n.GetSeverity(), n.GetId())
		} else {
			fmt.Fprintf(b, "\n- %s", n.GetId())
		}
		if n.GetLabel() != "" {
			fmt.Fprintf(b, " — %s", n.GetLabel())
		}
	}
}

func renderingGroupsBriefingSection(groups []store.RenderingGroupInfo) string {
	if len(groups) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nRendering groups (cross-file visual consistency):\n")
	for _, g := range groups {
		label := g.Label
		if label == "" {
			label = g.ID
		}
		b.WriteString(fmt.Sprintf("- %s: %s\n", g.ID, label))
		if g.Contract != "" {
			// Show first line of contract only
			lines := strings.SplitN(g.Contract, "\n", 2)
			b.WriteString(fmt.Sprintf("  Contract: %s\n", strings.TrimSpace(lines[0])))
		}
	}
	return b.String()
}
