// SPDX-License-Identifier: Apache-2.0

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
	tests := newStringSet()

	// Per-file impact (invariants, failure modes, forbidden fixes, tests).
	indexed := 0
	hasAnchors := false
	for _, f := range files {
		if svcName := serviceFromPath(f); svcName != "" {
			svc.add(svcName)
		}
		impact, _, _, err := s.collectImpact(ctx, f, "")
		if err != nil {
			plan.Unknowns = append(plan.Unknowns, "impact_query_failed_for_"+f)
			continue
		}
		for _, n := range impact.GetDirectInvariants() {
			invariants.add(n.GetId())
			hasAnchors = true
		}
		for _, n := range impact.GetDirectFailureModes() {
			failureModes.add(n.GetId())
			hasAnchors = true
		}
		for _, n := range impact.GetForbiddenFixes() {
			forbidden.add(n.GetId())
		}
		for _, n := range impact.GetRequiredTests() {
			tests.add(n.GetId())
		}
		if len(impact.GetDirectInvariants())+len(impact.GetDirectFailureModes())+len(impact.GetDirectIntents()) > 0 {
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
		for _, p := range matchedPlans {
			plan.AffectedRepairPlans = append(plan.AffectedRepairPlans, p.ID)
		}
	}

	// Risk → blast radius + approval gate.
	coverageSufficient := len(invariants.items)+len(failureModes.items) > 0 || indexed > 0
	risk := awarenesspb.RiskClass_UNKNOWN_IMPACT
	assessment := assessChangeRisk(files, authorityDomains, matchedPlans, risk, coverageSufficient, hasAnchors)
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
