// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=server.change_impact
// @awareness file_role=change_impact_planner
// @awareness implements=globular.awareness_graph:intent.awg.graph_is_compiled_context_not_authority
// @awareness risk=low
package main

// change_impact.go — Phase 2I. Before code edits, awareness predicts what a
// proposed change will affect: services, authority domains, state objects,
// invariants, repair plans, required tests, failure modes, forbidden fixes,
// blast radius, approval gate, and the unknowns. It composes the matchers and
// impact query built in earlier phases into one structured plan. Advisory: it
// predicts, the owner services and workflow gate decide.

import (
	"context"
	"github.com/globulario/sensei/golang/subjectstate"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/coverage"
	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// ChangeImpactPlan is the structured prediction for a proposed change.
type ChangeImpactPlan struct {
	AffectedServices         []string
	AffectedAuthorityDomains []string
	AffectedStateObjects     []string
	AffectedInvariants       []string
	AffectedRepairPlans      []string
	RequiredTests            []string
	PossibleFailureModes     []string
	ForbiddenFixes           []string
	BlastRadius              string
	ApprovalGate             string
	Unknowns                 []string
}

// planChangeImpact assembles the impact plan for a proposed change.
func (s *server) planChangeImpact(ctx context.Context, task string, files []string) (*ChangeImpactPlan, error) {
	plan := &ChangeImpactPlan{}
	svc := newStringSet()
	stateObjects := newStringSet()
	invariants := newStringSet()
	failureModes := newStringSet()
	forbidden := newStringSet()
	// forbiddenAnchors holds only GOVERNED forbidden-fix anchors. `forbidden`
	// additionally carries synthetic "authority_bypass:" entries appended for
	// presentation, and mixing the two let authority-domain guidance report
	// coverage a file does not have.
	forbiddenAnchors := newStringSet()
	contracts := newStringSet()
	tests := newStringSet()

	// Per-file impact (invariants, failure modes, forbidden fixes, tests).
	indexed := 0
	hasAnchors := false
	for _, f := range files {
		if svcName := serviceFromPath(f); svcName != "" {
			svc.add(svcName)
		}
		impact, _, _, _, err := s.collectImpact(ctx, f, "")
		if err != nil {
			plan.Unknowns = append(plan.Unknowns, "impact_query_failed_for_"+f)
			continue
		}
		// Lifecycle filtering, matching what preflight applies: a deprecated,
		// superseded or retired anchor is not primary guidance and must not
		// establish that a repair plan applies to this subject.
		//
		// THE PREDICATE IS NOT NAMED HERE ANY MORE, and that is the point. A
		// standalone `primary` closure existed alongside the canonical state
		// and the compiler reported it unused once every class read the state
		// instead -- which is the proof the owner is genuinely sole rather
		// than merely first.
		//
		// ONE canonical answer to "what does the graph know about this file",
		// built once from the complete raw material and filtered once. The
		// counts below read it instead of re-deriving it, which is how the
		// three surfaces came to disagree (self-improvement program, step B).
		state := subjectstate.Build(subjectstate.Raw{
			subjectstate.ClassInvariant:    asStateNodes(impact.GetDirectInvariants()),
			subjectstate.ClassFailureMode:  asStateNodes(impact.GetDirectFailureModes()),
			subjectstate.ClassIntent:       asStateNodes(impact.GetDirectIntents()),
			subjectstate.ClassForbiddenFix: asStateNodes(impact.GetForbiddenFixes()),
			subjectstate.ClassContract:     asStateNodes(ofClass(impact.GetDirectArchitecture(), "contract")),
		}, func(n subjectstate.Node) bool {
			return isPrimaryStatus(n.GetStatus(), s.scoreNode(ctx, n.GetIri()))
		})
		// hasAnchors is a SAFETY signal: it suppresses the unknown-owner
		// escalation, so it must mean "this file has live governance", not
		// "this file has a record of some kind". Setting it from raw nodes let a
		// file whose only anchor is retired suppress that fallback while the
		// retired anchor was correctly refused everywhere else.
		// hasAnchors is DELIBERATELY NARROWER THAN GOVERNANCE, and reading it
		// from the canonical state does not make it class-complete.
		//
		// I widened this to every governed class and blast_radius.go says, in
		// its own words, why that is wrong: "hasDirectAnchors asks whether this
		// subject is anchored at all, and drives the 'we cannot name the owner'
		// escalation... Deriving the first from the second was tidier and
		// wrong: widening the applicability set to the classes a plan can name
		// would then have silently stopped the owner-unknown rule from firing."
		//
		// A live contract or forbidden fix is governance; it does not name an
		// OWNER. Counting it here suppressed the owner-unknown warning and the
		// review escalation for a high-risk file with no matched authority
		// domain -- which is the fallback's entire purpose.
		//
		// The canonical state is still the source. One owner for the anchors,
		// two questions asked of them, and the narrower one NAMES its classes
		// so the narrowing is visible here rather than implied.
		if len(state.LiveIn(subjectstate.ClassInvariant, subjectstate.ClassFailureMode)) > 0 {
			hasAnchors = true
		}
		for _, id := range state.LiveIDs(subjectstate.ClassInvariant) {
			invariants.add(id)
		}
		for _, id := range state.LiveIDs(subjectstate.ClassFailureMode) {
			failureModes.add(id)
		}
		// Forbidden fixes and contracts read the SAME canonical result as the
		// classes above, rather than being classified a second time.
		//
		// Calling the predicate again was not merely redundant: scoreNode does
		// I/O, so a graph that moves between calls, or a transient failure that
		// falls back to the protobuf status, could classify one anchor retired
		// for HasLiveAnchors and live for applicability -- two lifecycle
		// answers about one node, which is the defect the owner exists to
		// remove, reappearing inside the change that introduced the owner.
		//
		// Architecture contracts are a class a repair plan can name, so this
		// surface must see them or it would deny applicability that preflight
		// grants for the same change. The contract filter lives in the Raw map
		// above; DirectArchitecture also carries components, boundaries,
		// decisions, evidence and patterns.
		for _, id := range state.LiveIDs(subjectstate.ClassForbiddenFix) {
			forbidden.add(id)
			forbiddenAnchors.add(id)
		}
		for _, id := range state.LiveIDs(subjectstate.ClassContract) {
			contracts.add(id)
		}
		for _, n := range impact.GetRequiredTests() {
			tests.add(n.GetId())
		}
		// Examination has THREE states here, and collapsing them into one
		// boolean is what let a withdrawal read as coverage (#318 review).
		//
		//   primary anchors > 0        -> examined, and something governs it
		//   raw anchors, none primary  -> the graph looked, and every rule it
		//                                 learned has since been RETIRED
		//   no anchors at all          -> unknown; ask the index (#220)
		//
		// The index lookup answers the third state. It must not be allowed to
		// answer the second: it queries the same SourceFile subject that just
		// returned those retired anchors, so it always says yes, and a file
		// whose governance was deliberately withdrawn is re-admitted as
		// examined -- making coverageSufficient true, suppressing the
		// thin-coverage escalation, and returning local/none for a file that
		// is high-risk by authority membership and matches no path-class rule.
		//
		// Examination is now DERIVED from the canonical state rather than
		// recomputed here, and that removes a residual asymmetry: the live
		// count read three classes while the raw count read five, so a file
		// anchored only by a LIVE contract or forbidden fix scored zero on the
		// left of the comparison and non-zero on the right. Both halves are
		// class-complete by construction now, because there is only one set.
		//
		// The index may be consulted in exactly one state. A determined
		// withdrawal is an answer, and no fallback may overturn it.
		examined := state.Examination() == subjectstate.ExaminedGoverned
		if state.Examination().MayConsultIndex() {
			examined, _ = s.sourceFileExamined(ctx, f, "")
		}
		if examined {
			indexed++
		}
	}

	// Authority domains + their owned state objects.
	var authorityDomains []loadedAuthorityDomain
	if domains, err := s.loadAuthorityDomains(ctx); err == nil {
		authorityDomains = matchAuthorityDomains(files, domains)
		for _, d := range authorityDomains {
			plan.AffectedAuthorityDomains = append(plan.AffectedAuthorityDomains, d.ID)
			for _, st := range d.OwnsState {
				stateObjects.add(st)
			}
			for _, b := range d.ForbidsBypass {
				forbidden.add("authority_bypass:" + b)
			}
		}
	}

	// Repair plans.
	var matchedPlans []loadedRepairPlan
	if plans, err := s.loadRepairPlans(ctx); err == nil {
		matchedPlans = matchRepairPlans(task, files, authorityDomains, plans)
		// Reported plans are capped for the reader; assessChangeRisk below is
		// given the complete matched set, so a plan that is not shown can still
		// be the one that decides authority.
		for _, p := range surfacedRepairPlans(matchedPlans) {
			plan.AffectedRepairPlans = append(plan.AffectedRepairPlans, p.ID)
		}
	}

	// Risk → blast radius + approval gate.
	// Every governed class a plan can name counts towards coverage, for the
	// same reason: a subject anchored only by a contract or a forbidden fix is
	// still anchored.
	coverageSufficient := len(invariants.items)+len(failureModes.items)+
		len(forbiddenAnchors.items)+len(contracts.items) > 0 || indexed > 0
	risk := awarenesspb.RiskClass_UNKNOWN_IMPACT
	// The same applicability rule as preflight, from this surface's own anchors.
	// Leaving this call on the old shape would let one surface decide authority
	// with applicability and the other without it, and the two would disagree
	// about the same change while both claiming to be the verdict.
	anchors := newSubjectAnchors(invariants.sorted(), failureModes.sorted(),
		forbiddenAnchors.sorted(), contracts.sorted())
	assessment := assessChangeRisk(files, authorityDomains, matchedPlans, risk, coverageSufficient,
		hasAnchors, anchors)
	plan.BlastRadius = assessment.BlastRadius
	plan.ApprovalGate = assessment.ApprovalGate

	// Unknowns: high-risk files with no authority + no anchors; missing evidence;
	// no required tests for a high-risk change.
	authCovers := authorityCoversPaths(authorityDomains)
	if len(authorityDomains) == 0 && !hasAnchors && coverage.AnyFileHighRiskWeighted(files, authCovers) {
		plan.Unknowns = append(plan.Unknowns, "authority owner unknown for a high-risk file")
	}
	if tests.empty() && coverage.AnyFileHighRiskWeighted(files, authCovers) {
		plan.Unknowns = append(plan.Unknowns, "no required tests known for a high-risk change")
	}
	if !hasAnchors && coverage.AnyFileHighRiskWeighted(files, authCovers) {
		plan.Unknowns = append(plan.Unknowns, "no invariants anchored to these files — read the source directly")
	}

	plan.AffectedServices = svc.sorted()
	plan.AffectedStateObjects = stateObjects.sorted()
	plan.AffectedInvariants = invariants.sorted()
	plan.PossibleFailureModes = failureModes.sorted()
	plan.ForbiddenFixes = forbidden.sorted()
	plan.RequiredTests = tests.sorted()
	sort.Strings(plan.AffectedAuthorityDomains)
	sort.Strings(plan.AffectedRepairPlans)
	sort.Strings(plan.Unknowns)
	return plan, nil
}

// serviceFromPath returns the service name for a golang/<svc>/... path.
func serviceFromPath(path string) string {
	path = strings.TrimPrefix(strings.TrimSpace(path), "./")
	const prefix = "golang/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := path[len(prefix):]
	if i := strings.IndexByte(rest, '/'); i > 0 {
		return rest[:i]
	}
	return ""
}

// stringSet is a tiny ordered-by-sort set helper.
type stringSet struct{ items map[string]bool }

func newStringSet() *stringSet { return &stringSet{items: map[string]bool{}} }
func (s *stringSet) add(v string) {
	if v = strings.TrimSpace(v); v != "" {
		s.items[v] = true
	}
}
func (s *stringSet) empty() bool { return len(s.items) == 0 }
func (s *stringSet) sorted() []string {
	out := make([]string, 0, len(s.items))
	for k := range s.items {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// asStateNodes adapts the protobuf slice to the canonical owner's interface.
// KnowledgeNode already satisfies it; this only changes the slice's element
// type, and exists so a forgotten class is a missing map key rather than a
// forgotten append.
func asStateNodes(in []*awarenesspb.KnowledgeNode) []subjectstate.Node {
	out := make([]subjectstate.Node, 0, len(in))
	for _, n := range in {
		if n != nil {
			out = append(out, n)
		}
	}
	return out
}
