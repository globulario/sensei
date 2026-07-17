// SPDX-License-Identifier: Apache-2.0

package inference

import (
	"strings"

	"github.com/globulario/sensei/golang/architecture"
)

func statusForPremises(ctx Context, facts []architecture.Fact) (string, []string) {
	var unknowns []string
	if !architecture.RepositoryRevisionResolved(ctx.Binding) && !architecture.RepositoryTreeResolved(ctx.Binding) {
		unknowns = append(unknowns, "The claim cannot be certified against a resolved repository revision and graph digest.")
	}
	if !architecture.RepositoryGraphResolved(ctx.Binding) {
		unknowns = append(unknowns, "The claim cannot be certified against a resolved repository revision and graph digest.")
	}
	for _, f := range facts {
		if err := architecture.ValidateFact(f); err != nil {
			unknowns = append(unknowns, "A premise fact is structurally invalid: "+err.Error())
			continue
		}
		if f.Provenance == nil {
			unknowns = append(unknowns, "A premise fact lacks explicit provenance.")
			continue
		}
		p := f.Provenance
		// A premise is bound to an exact repository state either by a resolved
		// committed revision or, for an admitted uncommitted result tree, by a
		// resolved per-file source digest that pins the exact file content.
		revisionBound := p.RevisionStatus == architecture.RevisionResolved
		sourceBound := p.SourceKind == "source_file" && p.SourceDigestStatus == architecture.SourceDigestResolved
		if !revisionBound && !sourceBound {
			unknowns = append(unknowns, "A premise fact lacks a resolved repository revision.")
		}
		if p.SourceKind == "source_file" && p.SourceDigestStatus != architecture.SourceDigestResolved {
			unknowns = append(unknowns, "A source-backed premise fact lacks a resolved source digest.")
		}
		if hasBlockingLimitation(ctx.Limitations, f) {
			unknowns = append(unknowns, "A blocking extraction limitation applies to a premise source.")
		}
	}
	if len(unknowns) > 0 {
		return architecture.StatusUnknown, dedupeStrings(unknowns)
	}
	return architecture.StatusSupported, nil
}

func hasBlockingLimitation(limitations []architecture.Limitation, f architecture.Fact) bool {
	source := strings.TrimSpace(f.Evidence.SourceFile)
	for _, lim := range limitations {
		if !lim.Blocking {
			continue
		}
		if strings.TrimSpace(lim.Source) == "" || strings.TrimSpace(lim.Source) == source {
			return true
		}
	}
	return false
}

func baseClaim(ruleID string, plane string, statement architecture.ClaimStatement, facts []architecture.Fact, status string, unknowns []string, cap float64) architecture.Claim {
	files := map[string]bool{}
	symbols := map[string]bool{}
	repo := ""
	var premiseIDs []string
	for _, f := range facts {
		premiseIDs = append(premiseIDs, f.ID)
		if repo == "" {
			repo = f.Scope.Repository
		}
		for _, file := range f.Scope.Files {
			files[file] = true
		}
		for _, symbol := range f.Scope.Symbols {
			symbols[symbol] = true
		}
	}
	return architecture.Claim{
		Label:                   statement.Subject + " " + statement.Predicate + " " + statement.Object,
		Description:             "Deterministically derived from normalized architectural facts.",
		Statement:               statement,
		Scope:                   architecture.ClaimScope{Repository: repo, Repo: repo, Files: sortedKeys(files), Symbols: sortedKeys(symbols)},
		ArchitecturalPlane:      plane,
		AssertionOrigin:         architecture.OriginDerived,
		EpistemicStatus:         status,
		InferenceRule:           ruleID,
		PremiseFacts:            dedupeStrings(premiseIDs),
		Unknowns:                unknowns,
		Confidence:              ConservativeConfidence(facts, cap),
		HumanReviewRequired:     true,
		PromotionStatus:         architecture.PromotionCandidate,
		InvalidationConditions:  []string{"The premise fact disappears or changes.", "The source digest for the premise changes.", "The repository revision changes.", "The inference-rule version changes."},
		AlternativeExplanations: nil,
	}
}
