// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=server.repair_plans
// @awareness file_role=preflight_repair_plan_matcher
// @awareness implements=globular.awareness_graph:intent.awg.graph_is_compiled_context_not_authority
// @awareness risk=low
package main

// repair_plans.go — surfaces RepairPlan nodes in Preflight and Briefing. A plan
// matches when a touched file falls in an authority domain the plan applies to,
// or when the task text names a finding class the plan repairs. The plan is the
// safe, legal route back to convergence; awareness surfaces it, the owner
// service and workflow gate execute it.

import (
	"context"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/globulario/awareness-graph/golang/rdf"
	"github.com/globulario/awareness-graph/golang/store"
)

const maxRepairPlansSurfaced = 2

type loadedRepairPlan struct {
	IRI                 string
	ID                  string
	Label               string
	Status              string
	Confidence          string
	BlastRadius         string
	ApprovalGate        string
	FindingClasses      []string
	ActivationTriggers  []string
	Preconditions       []string
	RepairSteps         []string
	VerificationSteps   []string
	RollbackSteps       []string
	OutcomeFeedback     string
	GovernedContracts   []string
	PreservedInvariants []string
	FailureModes        []string
	ForbiddenFixes      []string
	RequiredTests       []string
	ExpressedByFiles    []string
	AffectedComponents  []string
	// CoversPaths are repo-relative path prefixes whose files this plan repairs.
	CoversPaths []string
	// AuthorityDomainIDs are the bare authority-domain ids the plan applies to.
	AuthorityDomainIDs []string
}

type repairPlanCache struct {
	mu     sync.RWMutex
	loaded bool
	plans  []loadedRepairPlan
}

var globalRepairPlanCache = &repairPlanCache{}

func (s *server) loadRepairPlans(ctx context.Context) ([]loadedRepairPlan, error) {
	globalRepairPlanCache.mu.RLock()
	if globalRepairPlanCache.loaded {
		out := globalRepairPlanCache.plans
		globalRepairPlanCache.mu.RUnlock()
		return out, nil
	}
	globalRepairPlanCache.mu.RUnlock()

	globalRepairPlanCache.mu.Lock()
	defer globalRepairPlanCache.mu.Unlock()
	if globalRepairPlanCache.loaded {
		return globalRepairPlanCache.plans, nil
	}
	if s.store == nil {
		return nil, nil
	}
	facts, err := s.store.ClassFacts(ctx, rdf.ClassRepairPlan, 200)
	if err != nil {
		return nil, err
	}
	plans := classFactsToRepairPlans(facts)
	globalRepairPlanCache.plans = plans
	globalRepairPlanCache.loaded = true
	return plans, nil
}

func classFactsToRepairPlans(facts []store.ImpactFact) []loadedRepairPlan {
	byNode := map[string]*loadedRepairPlan{}
	for _, f := range facts {
		p, ok := byNode[f.NodeIRI]
		if !ok {
			p = &loadedRepairPlan{IRI: f.NodeIRI, ID: bareIDFromIRI(f.NodeIRI)}
			byNode[f.NodeIRI] = p
		}
		switch f.Predicate {
		case rdf.PropLabel:
			p.Label = f.Object
		case rdf.PropStatus:
			p.Status = f.Object
		case rdf.PropHasConfidence:
			p.Confidence = f.Object
		case rdf.PropHasBlastRadius:
			p.BlastRadius = f.Object
		case rdf.PropRequiresApprovalGate:
			p.ApprovalGate = f.Object
		case rdf.PropRepairsFindingClass:
			p.FindingClasses = append(p.FindingClasses, f.Object)
		case rdf.PropActivationTrigger:
			p.ActivationTriggers = append(p.ActivationTriggers, f.Object)
		case rdf.PropRequiresPrecondition:
			p.Preconditions = append(p.Preconditions, f.Object)
		case rdf.PropHasRepairStep:
			p.RepairSteps = append(p.RepairSteps, f.Object)
		case rdf.PropRequiresVerification:
			p.VerificationSteps = append(p.VerificationSteps, f.Object)
		case rdf.PropHasRollbackStep:
			p.RollbackSteps = append(p.RollbackSteps, f.Object)
		case rdf.PropProducesOutcomeFeedback:
			p.OutcomeFeedback = f.Object
		case rdf.PropGovernedByContract:
			p.GovernedContracts = append(p.GovernedContracts, bareIDFromIRI(f.Object))
		case rdf.PropMustNotViolateInvariant:
			p.PreservedInvariants = append(p.PreservedInvariants, bareIDFromIRI(f.Object))
		case rdf.PropRepairsFailureMode:
			p.FailureModes = append(p.FailureModes, bareIDFromIRI(f.Object))
		case rdf.PropForbids:
			p.ForbiddenFixes = append(p.ForbiddenFixes, bareIDFromIRI(f.Object))
		case rdf.PropRequiresTest:
			p.RequiredTests = append(p.RequiredTests, bareIDFromIRI(f.Object))
		case rdf.PropExpressedBy:
			if path := sourceFilePathFromIRI(f.Object); path != "" {
				p.ExpressedByFiles = append(p.ExpressedByFiles, path)
			}
		case rdf.PropAffectsComponent:
			p.AffectedComponents = append(p.AffectedComponents, bareIDFromIRI(f.Object))
		case rdf.PropCoversPath:
			p.CoversPaths = append(p.CoversPaths, f.Object)
		case rdf.PropAppliesToAuthorityDomain:
			p.AuthorityDomainIDs = append(p.AuthorityDomainIDs, bareIDFromIRI(f.Object))
		}
	}
	out := make([]loadedRepairPlan, 0, len(byNode))
	for _, p := range byNode {
		if p.Status != "" && p.Status != "active" {
			continue
		}
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// matchRepairPlans returns plans that apply to the request: by a touched file
// under the plan's covers_paths, by authority-domain membership, or by a finding
// class named in the task text. File/path signal first; capped.
func matchRepairPlans(task string, files []string, matchedDomains []loadedAuthorityDomain, plans []loadedRepairPlan) []loadedRepairPlan {
	if len(plans) == 0 {
		return nil
	}
	domainIDs := map[string]bool{}
	for _, d := range matchedDomains {
		domainIDs[d.ID] = true
	}
	taskLower := strings.ToLower(task)

	var byPath, byTask []loadedRepairPlan
	for _, p := range plans {
		if planCoversFile(p, files) || planAppliesToDomain(p, domainIDs) {
			byPath = append(byPath, p)
			continue
		}
		if task != "" && planMatchesTask(p, taskLower) {
			byTask = append(byTask, p)
		}
	}
	out := append(byPath, byTask...)
	if len(out) > maxRepairPlansSurfaced {
		out = out[:maxRepairPlansSurfaced]
	}
	return out
}

// planCoversFile reports whether any touched file is under the plan's
// covers_paths prefixes.
func planCoversFile(p loadedRepairPlan, files []string) bool {
	for _, f := range files {
		f = strings.TrimPrefix(strings.TrimSpace(f), "./")
		for _, expressed := range p.ExpressedByFiles {
			expressed = strings.TrimPrefix(strings.TrimSpace(expressed), "./")
			if expressed != "" && expressed == f {
				return true
			}
		}
		for _, prefix := range p.CoversPaths {
			prefix = strings.TrimPrefix(strings.TrimSpace(prefix), "./")
			if prefix != "" && strings.HasPrefix(f, prefix) {
				return true
			}
		}
	}
	return false
}

func planAppliesToDomain(p loadedRepairPlan, domainIDs map[string]bool) bool {
	for _, id := range p.AuthorityDomainIDs {
		if domainIDs[id] {
			return true
		}
	}
	return false
}

// planMatchesTask decides whether the task text alone selects a plan. Three
// tiers, strict to permissive: the task names a finding class the plan repairs;
// the task contains a full authored when_to_use trigger phrase; or the task
// shares >=3 distinct keywords with a single trigger (the same engine the
// pattern matcher uses). The keyword tier exists for the incident-response
// shape — an operator mid-incident has a symptom, not a file path. Dogfooding
// probe P4 ("the doctor found a bad etcd key and wants to delete it, how do I
// proceed safely") surfaced NOTHING before this tier existed.
func planMatchesTask(p loadedRepairPlan, taskLower string) bool {
	for _, fc := range p.FindingClasses {
		fc = strings.ToLower(strings.TrimSpace(fc))
		if fc != "" && strings.Contains(taskLower, fc) {
			return true
		}
	}
	taskKW := patternKeywordsMap(taskLower)
	for _, trig := range p.ActivationTriggers {
		trigLower := strings.ToLower(strings.TrimSpace(trig))
		if trigLower == "" {
			continue
		}
		if strings.Contains(taskLower, trigLower) {
			return true
		}
		overlap := 0
		for _, kw := range patternKeywords(trigLower) {
			if taskKW[kw] {
				overlap++
			}
		}
		if overlap >= 3 {
			return true
		}
	}
	return false
}

// repairPlanActions renders matched plans as bounded action lines for Preflight.
// The plan ID leads each block so agents (and golden tests) can key on it.
func repairPlanActions(plans []loadedRepairPlan) []string {
	var out []string
	for _, p := range plans {
		name := p.Label
		if name == "" {
			name = p.ID
		}
		out = append(out, "Repair plan: "+p.ID+" — approval="+orNone(p.ApprovalGate)+
			", blast="+orNone(p.BlastRadius)+", confidence="+orNone(p.Confidence))
		if len(p.Preconditions) > 0 {
			out = append(out, "Repair ["+name+"] precondition: "+p.Preconditions[0])
		}
		if len(p.RepairSteps) > 0 {
			out = append(out, "Repair ["+name+"] first step: "+p.RepairSteps[0])
		}
		if len(p.VerificationSteps) > 0 {
			out = append(out, "Repair ["+name+"] verify: "+p.VerificationSteps[0])
		}
		if len(p.RollbackSteps) > 0 {
			out = append(out, "Repair ["+name+"] rollback: "+p.RollbackSteps[0])
		}
		if p.OutcomeFeedback != "" {
			out = append(out, "Repair ["+name+"] record outcome: "+p.OutcomeFeedback)
		}
	}
	return out
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "unspecified"
	}
	return s
}

// repairPlansBriefingSection renders matched repair plans as a compact prose
// section for Briefing. Empty string when none matched.
func repairPlansBriefingSection(plans []loadedRepairPlan) string {
	if len(plans) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nApplicable repair plans:")
	for _, p := range plans {
		name := p.Label
		if name == "" {
			name = p.ID
		}
		b.WriteString("\n- " + p.ID + " — " + name)
		b.WriteString("\n  approval=" + orNone(p.ApprovalGate) + ", blast=" + orNone(p.BlastRadius) + ", confidence=" + orNone(p.Confidence))
		if len(p.Preconditions) > 0 {
			b.WriteString("\n  precondition: " + p.Preconditions[0])
		}
		if len(p.VerificationSteps) > 0 {
			b.WriteString("\n  verify: " + p.VerificationSteps[0])
		}
		if len(p.GovernedContracts) > 0 {
			b.WriteString("\n  contracts: " + strings.Join(p.GovernedContracts, ", "))
		}
		if len(p.ExpressedByFiles) > 0 {
			b.WriteString("\n  expressed by: " + strings.Join(p.ExpressedByFiles, ", "))
		}
	}
	return b.String()
}

func sourceFilePathFromIRI(iri string) string {
	s := strings.TrimPrefix(strings.TrimSuffix(iri, ">"), "<")
	const marker = "#sourceFile/"
	i := strings.Index(s, marker)
	if i < 0 {
		return ""
	}
	decoded, err := url.PathUnescape(s[i+len(marker):])
	if err != nil {
		return ""
	}
	return decoded
}

func invalidateRepairPlanCacheForTest() {
	globalRepairPlanCache.mu.Lock()
	defer globalRepairPlanCache.mu.Unlock()
	globalRepairPlanCache.loaded = false
	globalRepairPlanCache.plans = nil
}
