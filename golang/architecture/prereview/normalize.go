// SPDX-License-Identifier: Apache-2.0

package prereview

import (
	"sort"
	"strings"
)

// Normalize canonicalizes a report in place so that logically-equal reports
// produce byte-identical canonical output: string sets are trimmed, sorted, and
// de-duplicated, and item lists are stably ordered by ID. Reviewer-attention
// order is intentionally left to the deterministic ranker, which is the only
// place order carries meaning.
func Normalize(r *PreReviewReport) {
	if r == nil {
		return
	}
	r.SchemaVersion = strings.TrimSpace(r.SchemaVersion)
	r.Limitations = sortedUnique(r.Limitations)

	normalizeBinding(&r.Binding)
	normalizeCoverage(&r.Coverage)
	normalizeChange(&r.Change)
	normalizeImpact(&r.Impact)
	normalizeGovernance(&r.Governance)
	normalizeProtection(&r.Protection)
	normalizeProof(&r.Proof)
	normalizeResult(&r.Result)
	normalizeEpistemic(&r.Epistemic)

	for i := range r.ReviewerAttention {
		normalizeAttentionItem(&r.ReviewerAttention[i])
	}
}

func normalizeBinding(b *ReviewBinding) {
	b.PolicyIDs = sortedUnique(b.PolicyIDs)
}

func normalizeCoverage(c *CoverageSummary) {
	c.Available = sortedUnique(c.Available)
	c.Unavailable = sortedUnique(c.Unavailable)
}

func normalizeChange(c *ChangeSummary) {
	c.FilesCreated = sortedUnique(c.FilesCreated)
	c.FilesModified = sortedUnique(c.FilesModified)
	c.FilesDeleted = sortedUnique(c.FilesDeleted)
	c.AffectedSymbols = sortedUnique(c.AffectedSymbols)
	c.AdmittedOperations = sortedUnique(c.AdmittedOperations)
	c.ObservedOperations = sortedUnique(c.ObservedOperations)
	c.GeneratedArtifacts = sortedUnique(c.GeneratedArtifacts)
	c.OutOfEnvelopeChanges = sortedUnique(c.OutOfEnvelopeChanges)
	sort.SliceStable(c.FilesRenamed, func(i, j int) bool {
		if c.FilesRenamed[i].From != c.FilesRenamed[j].From {
			return c.FilesRenamed[i].From < c.FilesRenamed[j].From
		}
		return c.FilesRenamed[i].To < c.FilesRenamed[j].To
	})
}

func normalizeImpact(a *ArchitecturalImpact) {
	a.RiskClass = strings.TrimSpace(a.RiskClass)
	for _, s := range []*[]ImpactItem{
		&a.AffectedComponents, &a.ChangedBoundaries, &a.AffectedContracts, &a.AuthorityDomains,
		&a.StateObjects, &a.UpstreamDependents, &a.DownstreamDependencies, &a.ChangedRelationships,
	} {
		normalizeImpactItems(*s)
	}
}

func normalizeImpactItems(items []ImpactItem) {
	for i := range items {
		items[i].ID = strings.TrimSpace(items[i].ID)
		items[i].EvidenceRefs = sortedUnique(items[i].EvidenceRefs)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].ID < items[j].ID })
}

func normalizeGovernance(g *GovernanceSummary) {
	g.VerifiedRoles = sortedUnique(g.VerifiedRoles)
	g.GrantIDs = sortedUnique(g.GrantIDs)
	g.DelegationIDs = sortedUnique(g.DelegationIDs)
	g.SelectedMechanisms = sortedUnique(g.SelectedMechanisms)
	sort.SliceStable(g.Violations, func(i, j int) bool {
		if g.Violations[i].Code != g.Violations[j].Code {
			return g.Violations[i].Code < g.Violations[j].Code
		}
		return g.Violations[i].Path < g.Violations[j].Path
	})
}

func normalizeProtection(p *ProtectionSummary) {
	for _, s := range []*[]ProtectionItem{
		&p.Invariants, &p.Contracts, &p.FailureModes, &p.ForbiddenFixes,
		&p.RequiredTests, &p.GovernedExceptions, &p.IntendedDirections,
	} {
		normalizeProtectionItems(*s)
	}
}

func normalizeProtectionItems(items []ProtectionItem) {
	for i := range items {
		items[i].ID = strings.TrimSpace(items[i].ID)
		items[i].EvidenceRefs = sortedUnique(items[i].EvidenceRefs)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].ID < items[j].ID })
}

func normalizeProof(p *ProofSummary) {
	p.RequiredSlots = sortedUnique(p.RequiredSlots)
	p.DischargedSlots = sortedUnique(p.DischargedSlots)
	p.UnresolvedSlots = sortedUnique(p.UnresolvedSlots)
	for i := range p.RequiredObligations {
		p.RequiredObligations[i].ID = strings.TrimSpace(p.RequiredObligations[i].ID)
		p.RequiredObligations[i].EvidenceRefs = sortedUnique(p.RequiredObligations[i].EvidenceRefs)
	}
	sort.SliceStable(p.RequiredObligations, func(i, j int) bool {
		return p.RequiredObligations[i].ID < p.RequiredObligations[j].ID
	})
	for _, s := range []*[]EvidenceRef{
		&p.StaticEvidence, &p.TestReceipts, &p.RuntimeEvidence, &p.ArtifactReceipts,
		&p.StaleReceipts, &p.ConflictedReceipts,
	} {
		normalizeEvidenceRefs(*s)
	}
	sort.SliceStable(p.Waivers, func(i, j int) bool { return p.Waivers[i].ID < p.Waivers[j].ID })
}

func normalizeEvidenceRefs(refs []EvidenceRef) {
	sort.SliceStable(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
}

func normalizeResult(r *ResultArchitectureSummary) {
	r.ComponentsAdded = sortedUnique(r.ComponentsAdded)
	r.ComponentsRemoved = sortedUnique(r.ComponentsRemoved)
	r.BoundariesAdded = sortedUnique(r.BoundariesAdded)
	r.BoundariesRemoved = sortedUnique(r.BoundariesRemoved)
	r.AuthorityChanges = sortedUnique(r.AuthorityChanges)
	r.ContractChanges = sortedUnique(r.ContractChanges)
	r.ProofRequirementChanges = sortedUnique(r.ProofRequirementChanges)
	r.NewContradictions = sortedUnique(r.NewContradictions)
	r.InvalidatedProofs = sortedUnique(r.InvalidatedProofs)
}

func normalizeEpistemic(e *EpistemicSummary) {
	for _, s := range []*[]Statement{
		&e.Observed, &e.Governed, &e.DeterministicallyInferred, &e.ModelCandidates,
		&e.Contradicted, &e.Unknown, &e.Stale, &e.NotApplicable, &e.Uncertifiable,
	} {
		normalizeStatements(*s)
	}
}

func normalizeStatements(items []Statement) {
	for i := range items {
		items[i].ID = strings.TrimSpace(items[i].ID)
		items[i].EvidenceRefs = sortedUnique(items[i].EvidenceRefs)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].ID < items[j].ID })
}

func normalizeAttentionItem(a *ReviewerAttentionItem) {
	a.ID = strings.TrimSpace(a.ID)
	a.EvidenceRefs = sortedUnique(a.EvidenceRefs)
	a.RelatedFiles = sortedUnique(a.RelatedFiles)
	// AllowedAnswers order is meaningful (it is a choice list); trim only.
	for i := range a.AllowedAnswers {
		a.AllowedAnswers[i] = strings.TrimSpace(a.AllowedAnswers[i])
	}
}

// sortedUnique returns a trimmed, sorted, de-duplicated copy with empties
// removed. A nil or all-empty input returns nil so canonical output omits it.
func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}
