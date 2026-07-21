// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import (
	"fmt"
	"sort"
)

// Checkpoint-2 additive catalog composition. The catalog is composed HERE — in the semantic
// owner — from typed graph observations, so the status→lifecycle vocabulary and the per-class
// coverage/closure rules never leak into the server, mapper, or client (forbidden fix:
// phase9_5_duplicate_closure_or_severity_tables_in_server). The composition is a bounded batch:
// one summary row per observed artifact, never a full ArtifactState per row.

// CatalogScope is the exact typed scope a catalog batch is composed under.
type CatalogScope struct {
	RepositoryIdentity string
	DomainIdentity     string
	// GraphAuthorityIdentity is the OBSERVED live authority identity (empty when unobserved —
	// the expected seed identity is never substituted; it belongs in Limitations only).
	GraphAuthorityIdentity string
	SnapshotIdentity       string
	// The typed catalog source ledger (see CatalogSnapshot): catalog_enumeration primary,
	// graph_authority required, unclassified_discovery relevant.
	Source          SourceStatus
	AuthoritySource SourceStatus
	DiscoverySource SourceStatus
	Limitations     []string
}

// CatalogArtifactObservation is ONE typed graph observation: the node, its observed class IRIs,
// its display label, and its typed governed-status observation. No closure, severity, or
// applicability is observed here — those are assessed, not observed.
type CatalogArtifactObservation struct {
	NodeIRI         string
	Label           string
	ObservedClasses []string
	Lifecycle       LifecycleSource
}

// BuildCatalogSnapshot composes a validated CatalogSnapshot from typed observations.
//
// Per row: the canonical class is registry-resolved from the OBSERVED classes (never
// caller-selected); family/coverage copy the reviewed class policy; lifecycle is assessed by the
// canonical lifecycle vocabulary; closure is the HONEST catalog-granularity value — an
// assessable class whose per-artifact dimension sources were not consulted stays UNKNOWN (never
// closed, never healthy-by-default), an explicitly-not-applicable class is NOT_APPLICABLE, and
// unsupported/unknown classes stay UNKNOWN.
//
// Row availability is typed: an ASSESSABLE row composed without its assessment sources is
// PARTIAL (its summary counts cover only the consulted sources); every other coverage is as
// complete as the ontology allows and stays AVAILABLE. Attention counts are zero-within-
// consulted-sources for a partial row — a consumer reads the row availability, never a
// manufactured healthy zero.
func BuildCatalogSnapshot(reg Registry, scope CatalogScope, observations []CatalogArtifactObservation) (CatalogSnapshot, error) {
	if err := reg.Validate(); err != nil {
		return CatalogSnapshot{}, fmt.Errorf("invalid registry: %w", err)
	}
	for name, v := range map[string]string{
		"repository identity": scope.RepositoryIdentity,
		"snapshot identity":   scope.SnapshotIdentity,
	} {
		if v == "" {
			return CatalogSnapshot{}, fmt.Errorf("catalog scope missing %s", name)
		}
	}
	// Rows require an OBSERVED authority identity (never the expected seed digest).
	if scope.GraphAuthorityIdentity == "" && len(observations) > 0 {
		return CatalogSnapshot{}, fmt.Errorf("catalog rows require an observed graph-authority identity")
	}
	regDigest, err := reg.Digest()
	if err != nil {
		return CatalogSnapshot{}, err
	}

	rows := make([]ArtifactSummary, 0, len(observations))
	for _, obs := range observations {
		row, err := buildArtifactSummary(reg, scope, obs)
		if err != nil {
			return CatalogSnapshot{}, fmt.Errorf("artifact %q: %w", obs.NodeIRI, err)
		}
		rows = append(rows, row)
	}
	// Deterministic batch order (the index re-sorts by its complete key; this keeps the catalog
	// itself canonical and digest-stable for any consumer).
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Identity.NodeIRI < rows[j].Identity.NodeIRI })

	cat := CatalogSnapshot{
		RepositoryIdentity:     scope.RepositoryIdentity,
		DomainIdentity:         scope.DomainIdentity,
		GraphAuthorityIdentity: scope.GraphAuthorityIdentity,
		SnapshotIdentity:       scope.SnapshotIdentity,
		RegistryDigest:         regDigest,
		Source:                 scope.Source,
		AuthoritySource:        scope.AuthoritySource,
		DiscoverySource:        scope.DiscoverySource,
		Limitations:            sortedUnique(scope.Limitations),
		Artifacts:              rows,
	}
	if err := ValidateCatalogScope(reg, cat); err != nil {
		return CatalogSnapshot{}, err
	}
	return cat, nil
}

// buildArtifactSummary composes one bounded row from one typed observation.
func buildArtifactSummary(reg Registry, scope CatalogScope, obs CatalogArtifactObservation) (ArtifactSummary, error) {
	id, res, err := BuildArtifactIdentity(reg, obs.NodeIRI, obs.ObservedClasses,
		scope.RepositoryIdentity, scope.DomainIdentity, scope.GraphAuthorityIdentity, nil)
	if err != nil {
		return ArtifactSummary{}, err
	}
	policy, known := reg.classByIRI(res.CanonicalClass)
	if !known {
		policy = reg.unclassifiedPolicy()
	}

	var closure ArtifactClosure
	var avail Availability
	switch policy.Coverage {
	case CoverageExplicitlyNotApplicable:
		closure, avail = ClosureNotApplicable, AvailabilityAvailable
	case CoverageAssessable:
		// Assessment sources are not consulted at catalog granularity: closure is honestly
		// unknown and the ROW is partial (never a manufactured healthy zero).
		closure, avail = ClosureUnknown, AvailabilityPartial
	default: // unsupported / unknown / unclassified
		closure, avail = ClosureUnknown, AvailabilityAvailable
	}

	return ArtifactSummary{
		Identity:  id,
		Label:     nonEmpty(obs.Label, obs.NodeIRI),
		Family:    policy.Family,
		Class:     res.CanonicalClass,
		Coverage:  policy.Coverage,
		Lifecycle: assessLifecycle(policy, obs.Lifecycle).State,
		Closure:   closure,
		// Attention/dimension counts cover only the consulted sources; a partial row signals
		// that per-artifact assessment sources were not consulted.
		OpenRequiredDimensions: 0,
		AttentionCount:         0,
		OwnerSummary:           "",
		Availability:           avail,
	}, nil
}

// ValidateAttentionItem validates one canonical attention item (identity digest, vocabulary,
// severity basis). Exported for the transport mapper, which must validate every nested attention
// item before mapping rather than silently omitting a malformed one.
func ValidateAttentionItem(a AttentionItem) error { return validateAttentionItem(a) }
