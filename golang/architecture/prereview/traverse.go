// SPDX-License-Identifier: Apache-2.0

package prereview

// This file centralizes read-only traversal of the report's repeated structures
// so validation rules iterate them without duplicating the section lists.

func forEachImpactItem(a ArchitecturalImpact, fn func(section string, it ImpactItem)) {
	sections := []struct {
		name  string
		items []ImpactItem
	}{
		{"affected_components", a.AffectedComponents},
		{"changed_boundaries", a.ChangedBoundaries},
		{"affected_contracts", a.AffectedContracts},
		{"authority_domains", a.AuthorityDomains},
		{"state_objects", a.StateObjects},
		{"upstream_dependents", a.UpstreamDependents},
		{"downstream_dependencies", a.DownstreamDependencies},
		{"changed_relationships", a.ChangedRelationships},
	}
	for _, s := range sections {
		for _, it := range s.items {
			fn(s.name, it)
		}
	}
}

func forEachProtectionItem(p ProtectionSummary, fn func(section string, it ProtectionItem)) {
	sections := []struct {
		name  string
		items []ProtectionItem
	}{
		{"invariants", p.Invariants},
		{"contracts", p.Contracts},
		{"failure_modes", p.FailureModes},
		{"forbidden_fixes", p.ForbiddenFixes},
		{"required_tests", p.RequiredTests},
		{"governed_exceptions", p.GovernedExceptions},
		{"intended_directions", p.IntendedDirections},
	}
	for _, s := range sections {
		for _, it := range s.items {
			fn(s.name, it)
		}
	}
}

func forEachEpistemic(r PreReviewReport, fn func(where string, s EpistemicStatus)) {
	forEachImpactItem(r.Impact, func(section string, it ImpactItem) {
		fn("impact."+section+":"+it.ID, it.Epistemic)
	})
	forEachProtectionItem(r.Protection, func(section string, it ProtectionItem) {
		fn("protection."+section+":"+it.ID, it.Epistemic)
	})
	for _, o := range r.Proof.RequiredObligations {
		fn("proof_obligation:"+o.ID, o.Epistemic)
	}
	for _, a := range r.ReviewerAttention {
		fn("attention:"+a.ID, a.Epistemic)
	}
}

func forEachSeverity(r PreReviewReport, fn func(where string, s Severity)) {
	forEachProtectionItem(r.Protection, func(section string, it ProtectionItem) {
		fn("protection."+section+":"+it.ID, it.Severity)
	})
	for _, v := range r.Governance.Violations {
		fn("governance.violation:"+v.Code, v.Severity)
	}
	for _, a := range r.ReviewerAttention {
		fn("attention:"+a.ID, a.Severity)
	}
}
