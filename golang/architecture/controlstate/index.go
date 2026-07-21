// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ArtifactIndexSchema identifies architecture.artifact_index/v1.
const ArtifactIndexSchema = "architecture.artifact_index/v1"

const (
	defaultPageSize = 100
	maxPageSize     = 250
)

// ArtifactSummary is a bounded artifact row for navigation/lists. It never requires full
// ArtifactState construction.
type ArtifactSummary struct {
	Identity               ArtifactIdentity   `json:"identity" yaml:"identity"`
	Label                  string             `json:"label" yaml:"label"`
	Family                 string             `json:"family" yaml:"family"`
	Class                  string             `json:"class" yaml:"class"`
	Coverage               AssessmentCoverage `json:"assessment_coverage" yaml:"assessment_coverage"`
	Lifecycle              LifecycleState     `json:"lifecycle" yaml:"lifecycle"`
	Closure                ArtifactClosure    `json:"closure" yaml:"closure"`
	OpenRequiredDimensions int                `json:"open_required_dimensions" yaml:"open_required_dimensions"`
	HighestSeverity        AttentionSeverity  `json:"highest_severity,omitempty" yaml:"highest_severity,omitempty"`
	AttentionCount         int                `json:"attention_count" yaml:"attention_count"`
	OwnerSummary           string             `json:"owner_summary,omitempty" yaml:"owner_summary,omitempty"`
	Availability           Availability       `json:"availability" yaml:"availability"`
}

// CatalogSnapshot is the owner-bound, scope-coherent batch of artifact summaries.
type CatalogSnapshot struct {
	RepositoryIdentity string
	DomainIdentity     string
	// GraphAuthorityIdentity is the OBSERVED live graph-authority identity. It is empty exactly
	// when the graph authority was not observed — the expected embedded-seed identity is NEVER
	// substituted here (it may appear only in Limitations as expected-authority metadata).
	GraphAuthorityIdentity string
	RegistryDigest         string
	SnapshotIdentity       string
	// The typed catalog source ledger (modeled HERE, in the semantic owner — not in the server):
	//   Source          — catalog_enumeration, PRIMARY: the bounded known-class enumeration.
	//   AuthoritySource — graph_authority, REQUIRED: the observed authority relation
	//                     (available=current, degraded=stale, invalid=integrity failure,
	//                     unavailable=unobserved).
	//   DiscoverySource — unclassified_discovery, RELEVANT: the unknown-class node sweep.
	// A stale authority degrades the REQUIRED source (projection PARTIAL, known rows kept); it
	// never degrades the primary merely to represent a stale secondary.
	Source          SourceStatus
	AuthoritySource SourceStatus
	DiscoverySource SourceStatus
	// Limitations carries typed catalog-level limitations (e.g. the expected seed digest as
	// expected-authority metadata when the live authority is unobserved).
	Limitations []string
	Artifacts   []ArtifactSummary
}

// catalogLedger is the canonical source ledger order for catalog-derived projections.
func catalogLedger(cat CatalogSnapshot) []SourceStatus {
	return []SourceStatus{cat.Source, cat.AuthoritySource, cat.DiscoverySource}
}

// ArtifactIndexRequest selects a page.
type ArtifactIndexRequest struct {
	RepositoryIdentity string
	Domain             string
	PageSize           int
	Cursor             string
	FamilyFilter       string
	ClassFilter        string
	ClosureFilter      ArtifactClosure
	SeverityFilter     AttentionSeverity
	SearchText         string
}

// ArtifactIndex is architecture.artifact_index/v1: a deterministic paginated page.
type ArtifactIndex struct {
	ProjectionMeta `json:",inline" yaml:",inline"`
	RegistryDigest string            `json:"registry_digest" yaml:"registry_digest"`
	Page           []ArtifactSummary `json:"page" yaml:"page"`
	NextCursor     string            `json:"next_cursor,omitempty" yaml:"next_cursor,omitempty"`
	Truncated      bool              `json:"truncated" yaml:"truncated"`
}

type indexCursor struct {
	Schema           string `json:"schema"`
	Repo             string `json:"repo"`
	Domain           string `json:"domain"`
	Authority        string `json:"authority"`
	SnapshotIdentity string `json:"snapshot"`
	RegistryDigest   string `json:"registry"`
	FilterDigest     string `json:"filter"`
	LastSortKey      string `json:"last"`
	SelfDigest       string `json:"self"`
}

// catalogNotObservedReason is the canonical reason under which an unavailable catalog source may
// omit its snapshot identity.
const catalogNotObservedReason = "catalog_not_observed"

// ValidateCatalogScope validates the whole catalog before any filtering or pagination: scope
// coherence, per-summary class/family/coverage/vocabulary rules, nonnegative counts, and global
// uniqueness by exact artifact identity.
func ValidateCatalogScope(reg Registry, cat CatalogSnapshot) error {
	if cat.RepositoryIdentity == "" || cat.SnapshotIdentity == "" {
		return fmt.Errorf("catalog missing repository/snapshot identity")
	}
	regDigest, err := reg.Digest()
	if err != nil {
		return err
	}
	// The catalog MUST carry a valid primary source status whose identity is exactly the snapshot
	// identity, plus the exact registry digest it was derived under.
	if err := validateSourceStatus(cat.Source); err != nil {
		return fmt.Errorf("catalog source: %w", err)
	}
	if cat.Source.Impact != ImpactPrimary {
		return fmt.Errorf("catalog source must be the primary source")
	}
	// The catalog source identity must equal the snapshot identity whenever the source carries an
	// identity — including degraded and invalid sources. An unavailable source may omit the identity
	// only under the canonical source-not-observed reason.
	switch {
	case cat.Source.Identity != "":
		if cat.Source.Identity != cat.SnapshotIdentity {
			return fmt.Errorf("catalog source identity must equal the snapshot identity")
		}
	case cat.Source.Availability == SourceUnavailable && cat.Source.ReasonCode == catalogNotObservedReason:
		// permitted: an unavailable, never-observed catalog source
	default:
		return fmt.Errorf("catalog source must carry the snapshot identity")
	}
	if cat.RegistryDigest == "" || cat.RegistryDigest != regDigest {
		return fmt.Errorf("catalog must carry the exact registry digest")
	}
	// The graph-authority REQUIRED source: its identity IS the observed live authority identity.
	if err := validateSourceStatus(cat.AuthoritySource); err != nil {
		return fmt.Errorf("catalog authority source: %w", err)
	}
	if cat.AuthoritySource.Impact != ImpactRequired {
		return fmt.Errorf("catalog authority source must be a required source")
	}
	if cat.AuthoritySource.Identity != "" && cat.AuthoritySource.Identity != cat.GraphAuthorityIdentity {
		return fmt.Errorf("catalog authority source identity must equal the observed authority identity")
	}
	// An UNOBSERVED authority carries no identity — and then the expected seed digest must not be
	// smuggled in as the catalog authority identity, and no trusted rows may exist.
	if cat.GraphAuthorityIdentity == "" {
		if cat.AuthoritySource.Availability == SourceAvailable || cat.AuthoritySource.Availability == SourceDegraded {
			return fmt.Errorf("an observed authority source requires the catalog authority identity")
		}
		if len(cat.Artifacts) > 0 {
			return fmt.Errorf("a catalog without an observed authority identity cannot carry rows")
		}
	}
	// An INVALID authority (integrity failure) means no rows are trustworthy: the primary
	// enumeration must not present rows as authoritative.
	if cat.AuthoritySource.Availability == SourceInvalid && cat.Source.Availability == SourceAvailable {
		return fmt.Errorf("an integrity-failed authority cannot back an available catalog enumeration")
	}
	if cat.AuthoritySource.Availability == SourceUnavailable && cat.Source.Availability == SourceAvailable {
		return fmt.Errorf("an unobserved authority cannot back an available catalog enumeration")
	}
	// The unknown-class discovery RELEVANT source: explicit completeness or truncation.
	if err := validateSourceStatus(cat.DiscoverySource); err != nil {
		return fmt.Errorf("catalog discovery source: %w", err)
	}
	if cat.DiscoverySource.Impact != ImpactRelevant {
		return fmt.Errorf("catalog discovery source must be a relevant source")
	}
	for _, l := range cat.Limitations {
		if l == "" || l != strings.TrimSpace(l) {
			return fmt.Errorf("catalog limitation is empty or padded")
		}
	}
	seen := map[string]bool{}
	for _, a := range cat.Artifacts {
		if a.Identity.NodeIRI == "" {
			return fmt.Errorf("catalog summary missing identity")
		}
		if a.Identity.RepositoryIdentity != cat.RepositoryIdentity {
			return fmt.Errorf("catalog summary %q from a different repository", a.Identity.NodeIRI)
		}
		if a.Identity.DomainIdentity != cat.DomainIdentity {
			return fmt.Errorf("catalog summary %q from a different domain", a.Identity.NodeIRI)
		}
		if a.Identity.GraphAuthorityIdentity != cat.GraphAuthorityIdentity {
			return fmt.Errorf("catalog summary %q authority mismatch", a.Identity.NodeIRI)
		}
		if a.Identity.CanonicalClass != a.Class {
			return fmt.Errorf("catalog summary %q class disagrees with its identity", a.Identity.NodeIRI)
		}
		// Recompute the canonical class from the OBSERVED classes and require the recomputed class +
		// resolution to agree with both Identity.CanonicalClass and Summary.Class; a fabricated
		// canonical class, or an unknown/ambiguous set posing as a known class, is rejected here (not
		// just the duplicated class strings). ValidateArtifactIdentity also enforces canonical
		// sorted+unique observed/provenance identities.
		res := reg.ResolveCanonicalClass(a.Identity.ObservedClasses)
		if err := ValidateArtifactIdentity(reg, a.Identity, res); err != nil {
			return fmt.Errorf("catalog summary %q identity: %w", a.Identity.NodeIRI, err)
		}
		if res.CanonicalClass != a.Class {
			return fmt.Errorf("catalog summary %q class %q disagrees with the recomputed canonical class %q", a.Identity.NodeIRI, a.Class, res.CanonicalClass)
		}
		p, ok := reg.classByIRI(a.Class)
		if !ok {
			return fmt.Errorf("catalog summary %q class not in the registry", a.Identity.NodeIRI)
		}
		if a.Family != p.Family || a.Coverage != p.Coverage {
			return fmt.Errorf("catalog summary %q class/family/coverage disagrees with the registry", a.Identity.NodeIRI)
		}
		if !validClosure(a.Closure) || !validCoverage(a.Coverage) || !validLifecycleState(a.Lifecycle) {
			return fmt.Errorf("catalog summary %q off-vocabulary", a.Identity.NodeIRI)
		}
		if a.HighestSeverity != "" && !validSeverity(a.HighestSeverity) {
			return fmt.Errorf("catalog summary %q off-vocabulary severity", a.Identity.NodeIRI)
		}
		if a.Availability == "" || !validAvailability(a.Availability) {
			return fmt.Errorf("catalog summary %q availability is empty or off-vocabulary", a.Identity.NodeIRI)
		}
		if a.AttentionCount < 0 || a.OpenRequiredDimensions < 0 {
			return fmt.Errorf("catalog summary %q has a negative count", a.Identity.NodeIRI)
		}
		if a.AttentionCount == 0 && a.HighestSeverity != "" {
			return fmt.Errorf("catalog summary %q has zero attention but a highest severity", a.Identity.NodeIRI)
		}
		switch p.Coverage {
		case CoverageUnsupported, CoverageUnknown:
			if a.Closure != ClosureUnknown {
				return fmt.Errorf("catalog summary %q must use unknown closure for %s coverage", a.Identity.NodeIRI, p.Coverage)
			}
		case CoverageExplicitlyNotApplicable:
			// not_applicable permitted (explicit policy); other values are also legal outcomes.
		default:
			if a.Closure == ClosureNotApplicable {
				return fmt.Errorf("catalog summary %q uses not_applicable without explicit-not-applicable coverage", a.Identity.NodeIRI)
			}
		}
		if seen[a.Identity.NodeIRI] {
			return fmt.Errorf("catalog has a global duplicate artifact %q", a.Identity.NodeIRI)
		}
		seen[a.Identity.NodeIRI] = true
	}
	return nil
}

// validateFilters validates every non-empty filter against the closed registry/vocabulary.
func validateFilters(reg Registry, req ArtifactIndexRequest) error {
	if req.SearchText != "" {
		return fmt.Errorf("search text is not supported in Checkpoint 1")
	}
	if req.FamilyFilter != "" {
		if _, ok := reg.familyByID(req.FamilyFilter); !ok {
			return fmt.Errorf("unknown family filter %q", req.FamilyFilter)
		}
	}
	if req.ClassFilter != "" {
		p, ok := reg.classByIRI(req.ClassFilter)
		if !ok {
			return fmt.Errorf("unknown class filter %q", req.ClassFilter)
		}
		if req.FamilyFilter != "" && p.Family != req.FamilyFilter {
			return fmt.Errorf("class filter %q is not in family %q", req.ClassFilter, req.FamilyFilter)
		}
	}
	if req.ClosureFilter != "" && !validClosure(req.ClosureFilter) {
		return fmt.Errorf("invalid closure filter %q", req.ClosureFilter)
	}
	if req.SeverityFilter != "" && !validSeverity(req.SeverityFilter) {
		return fmt.Errorf("invalid severity filter %q", req.SeverityFilter)
	}
	return nil
}

// BuildArtifactIndex deterministically validates, filters, orders, and paginates the catalog.
func BuildArtifactIndex(reg Registry, req ArtifactIndexRequest, catalog CatalogSnapshot) (ArtifactIndex, error) {
	if err := reg.Validate(); err != nil {
		return ArtifactIndex{}, fmt.Errorf("invalid registry: %w", err)
	}
	if req.RepositoryIdentity == "" {
		return ArtifactIndex{}, fmt.Errorf("artifact index requires a repository identity")
	}
	if err := validateFilters(reg, req); err != nil {
		return ArtifactIndex{}, err
	}
	if err := ValidateCatalogScope(reg, catalog); err != nil {
		return ArtifactIndex{}, err
	}
	if catalog.RepositoryIdentity != req.RepositoryIdentity || catalog.DomainIdentity != req.Domain {
		return ArtifactIndex{}, fmt.Errorf("request scope disagrees with the catalog scope")
	}
	pageSize := req.PageSize
	switch {
	case pageSize <= 0:
		pageSize = defaultPageSize
	case pageSize > maxPageSize:
		return ArtifactIndex{}, fmt.Errorf("page size %d exceeds maximum %d", pageSize, maxPageSize)
	}
	regDigest, err := reg.Digest()
	if err != nil {
		return ArtifactIndex{}, err
	}
	filterDigest, err := digestOf(struct{ F, C, Cl, S string }{req.FamilyFilter, req.ClassFilter, string(req.ClosureFilter), string(req.SeverityFilter)})
	if err != nil {
		return ArtifactIndex{}, err
	}

	// Only an AVAILABLE catalog exposes trusted artifact rows; a degraded/unavailable/invalid
	// catalog yields an empty page and a partial/unavailable projection.
	if catalog.Source.Availability != SourceAvailable {
		idx := ArtifactIndex{
			ProjectionMeta: newMeta(ArtifactIndexSchema, req.RepositoryIdentity, req.Domain, aggregateAvailability(catalogLedger(catalog)), catalogLedger(catalog), catalog.Limitations),
			RegistryDigest: regDigest,
		}
		dig, derr := computeIndexDigest(idx)
		if derr != nil {
			return ArtifactIndex{}, derr
		}
		idx.DigestSHA256 = dig
		if verr := ValidateArtifactIndex(reg, idx, pageSize); verr != nil {
			return ArtifactIndex{}, verr
		}
		return idx, nil
	}

	// Filter.
	var filtered []ArtifactSummary
	for _, a := range catalog.Artifacts {
		if req.FamilyFilter != "" && a.Family != req.FamilyFilter {
			continue
		}
		if req.ClassFilter != "" && a.Class != req.ClassFilter {
			continue
		}
		if req.ClosureFilter != "" && a.Closure != req.ClosureFilter {
			continue
		}
		if req.SeverityFilter != "" && a.HighestSeverity != req.SeverityFilter {
			continue
		}
		filtered = append(filtered, a)
	}
	// Order by the complete sort key; require unique complete keys.
	keyOf := func(a ArtifactSummary) string { return indexSortKey(reg, a) }
	keySeen := map[string]bool{}
	for _, a := range filtered {
		k := keyOf(a)
		if keySeen[k] {
			return ArtifactIndex{}, fmt.Errorf("non-unique complete sort key for %q", a.Identity.NodeIRI)
		}
		keySeen[k] = true
	}
	sort.SliceStable(filtered, func(i, j int) bool { return keyOf(filtered[i]) < keyOf(filtered[j]) })

	start := 0
	if req.Cursor != "" {
		cur, err := decodeCursor(req.Cursor)
		if err != nil {
			return ArtifactIndex{}, err
		}
		if cur.Schema != ArtifactIndexSchema || cur.Repo != req.RepositoryIdentity || cur.Domain != req.Domain ||
			cur.Authority != catalog.GraphAuthorityIdentity || cur.SnapshotIdentity != catalog.SnapshotIdentity ||
			cur.RegistryDigest != regDigest || cur.FilterDigest != filterDigest {
			return ArtifactIndex{}, fmt.Errorf("cursor is bound to a different scope/snapshot/registry/filter")
		}
		for start < len(filtered) && keyOf(filtered[start]) <= cur.LastSortKey {
			start++
		}
	}

	end := start + pageSize
	truncated := end < len(filtered)
	if end > len(filtered) {
		end = len(filtered)
	}
	page := append([]ArtifactSummary(nil), filtered[start:end]...)

	idx := ArtifactIndex{
		ProjectionMeta: newMeta(ArtifactIndexSchema, req.RepositoryIdentity, req.Domain, aggregateAvailability(catalogLedger(catalog)),
			catalogLedger(catalog), catalog.Limitations),
		RegistryDigest: regDigest,
		Page:           page,
		Truncated:      truncated,
	}
	if truncated && len(page) > 0 {
		cur := indexCursor{Schema: ArtifactIndexSchema, Repo: req.RepositoryIdentity, Domain: req.Domain,
			Authority: catalog.GraphAuthorityIdentity, SnapshotIdentity: catalog.SnapshotIdentity,
			RegistryDigest: regDigest, FilterDigest: filterDigest, LastSortKey: keyOf(page[len(page)-1])}
		enc, err := encodeCursor(cur)
		if err != nil {
			return ArtifactIndex{}, err
		}
		idx.NextCursor = enc
	}
	dig, err := computeIndexDigest(idx)
	if err != nil {
		return ArtifactIndex{}, err
	}
	idx.DigestSHA256 = dig
	if err := ValidateArtifactIndex(reg, idx, pageSize); err != nil {
		return ArtifactIndex{}, err
	}
	return idx, nil
}

func indexSortKey(reg Registry, a ArtifactSummary) string {
	famOrder, classOrder := 9999, 9999
	if c, ok := reg.classByIRI(a.Class); ok {
		classOrder = c.Order
		if f, ok := reg.familyByID(c.Family); ok {
			famOrder = f.Order
		}
	}
	return fmt.Sprintf("%06d\x00%06d\x00%s\x00%s\x00%s", famOrder, classOrder, normalizeLabel(a.Label), a.Class, a.Identity.NodeIRI)
}

func normalizeLabel(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			r = r + ('a' - 'A')
		}
		out = append(out, r)
	}
	return string(out)
}

func encodeCursor(c indexCursor) (string, error) {
	c.SelfDigest = ""
	d, err := digestOf(c)
	if err != nil {
		return "", err
	}
	c.SelfDigest = d
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeCursor(s string) (indexCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return indexCursor{}, fmt.Errorf("malformed cursor")
	}
	var c indexCursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return indexCursor{}, fmt.Errorf("malformed cursor")
	}
	want := c
	want.SelfDigest = ""
	d, err := digestOf(want)
	if err != nil {
		return indexCursor{}, err
	}
	if d != c.SelfDigest {
		return indexCursor{}, fmt.Errorf("cursor self-digest mismatch")
	}
	return c, nil
}

func computeIndexDigest(idx ArtifactIndex) (string, error) {
	idx.DigestSHA256 = ""
	return digestOf(idx)
}

// ValidateArtifactIndex strictly validates the page and its invariants.
func ValidateArtifactIndex(reg Registry, idx ArtifactIndex, pageSize int) error {
	if err := validateMeta(idx.ProjectionMeta, ArtifactIndexSchema); err != nil {
		return err
	}
	if idx.RegistryDigest == "" {
		return fmt.Errorf("artifact index missing registry digest")
	}
	if pageSize > 0 && len(idx.Page) > pageSize {
		return fmt.Errorf("page length exceeds the effective page size")
	}
	if idx.Truncated && idx.NextCursor == "" {
		return fmt.Errorf("truncated index must carry a next cursor")
	}
	if !idx.Truncated && idx.NextCursor != "" {
		return fmt.Errorf("non-truncated index must not carry a next cursor")
	}
	if idx.Truncated && len(idx.Page) == 0 {
		return fmt.Errorf("truncated index cannot have an empty page")
	}
	seen := map[string]bool{}
	var prevKey string
	for i, a := range idx.Page {
		if a.Identity.NodeIRI == "" || a.Class == "" {
			return fmt.Errorf("artifact summary missing identity")
		}
		if !validClosure(a.Closure) || !validCoverage(a.Coverage) {
			return fmt.Errorf("artifact summary %q off-vocabulary closure/coverage", a.Identity.NodeIRI)
		}
		if a.AttentionCount < 0 || a.OpenRequiredDimensions < 0 {
			return fmt.Errorf("artifact summary %q has a negative count", a.Identity.NodeIRI)
		}
		key := indexSortKey(reg, a)
		if i > 0 && key <= prevKey {
			return fmt.Errorf("index page is not in canonical order")
		}
		prevKey = key
		if seen[a.Identity.NodeIRI] {
			return fmt.Errorf("duplicate artifact %q in page", a.Identity.NodeIRI)
		}
		seen[a.Identity.NodeIRI] = true
	}
	// The next cursor's last key equals the final page item.
	if idx.NextCursor != "" {
		cur, err := decodeCursor(idx.NextCursor)
		if err != nil {
			return err
		}
		if len(idx.Page) == 0 || cur.LastSortKey != indexSortKey(reg, idx.Page[len(idx.Page)-1]) {
			return fmt.Errorf("next cursor last key does not equal the final page item")
		}
	}
	want, err := computeIndexDigest(idx)
	if err != nil {
		return err
	}
	if idx.DigestSHA256 != want {
		return fmt.Errorf("artifact index digest does not match its content")
	}
	return nil
}
