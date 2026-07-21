// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import (
	"fmt"
	"sort"
	"strings"
)

// Attention classes (frozen). No error-text/label/color derivation ever assigns these.
const (
	AttnGraphAuthorityInvalid     = "graph_authority_invalid"
	AttnGraphAuthorityUnavailable = "graph_authority_unavailable"
	AttnContradictionPresent      = "contradiction_present"
	AttnEnforcementMissing        = "enforcement_missing"
	AttnVerificationMissing       = "verification_missing"
	AttnEvidenceMissing           = "evidence_missing"
	AttnOwnershipUnresolved       = "ownership_unresolved"
	AttnScopeAmbiguous            = "scope_ambiguous"
	AttnArtifactDimensionOpen     = "artifact_dimension_open"
	AttnArchitectQuestionOpen     = "architect_question_open"
	AttnCoverageBlindSpot         = "coverage_blind_spot"
	AttnProvenanceIntegrity       = "provenance_integrity_failure"
	AttnForbiddenMove             = "forbidden_move_detected"
	AttnSeedVerification          = "seed_verification_failure"
	AttnRuntimeBoundaryViolated   = "runtime_boundary_violated"
)

// severityForClass is the frozen governed mapping condition → severity. Critical is never
// downgraded; an unknown classification fails closed to warning with a typed basis.
func severityForClass(class string, highRiskBlindSpot bool) (AttentionSeverity, string) {
	switch class {
	case AttnGraphAuthorityInvalid, AttnContradictionPresent, AttnProvenanceIntegrity, AttnSeedVerification:
		return SeverityCritical, "governed_mapping:critical"
	case AttnCoverageBlindSpot:
		if highRiskBlindSpot {
			return SeverityCritical, "governed_mapping:high_risk_blind_spot"
		}
		return SeverityWarning, "governed_mapping:blind_spot"
	case AttnGraphAuthorityUnavailable, AttnEnforcementMissing, AttnVerificationMissing,
		AttnEvidenceMissing, AttnOwnershipUnresolved, AttnScopeAmbiguous, AttnArtifactDimensionOpen:
		return SeverityWarning, "governed_mapping:warning"
	case AttnArchitectQuestionOpen:
		return SeverityAttention, "governed_mapping:attention"
	default:
		return SeverityWarning, "governed_mapping:unknown_classification"
	}
}

func dimensionAttentionClass(dim string) string {
	switch dim {
	case "enforcement":
		return AttnEnforcementMissing
	case "verification":
		return AttnVerificationMissing
	case "evidence":
		return AttnEvidenceMissing
	case "ownership":
		return AttnOwnershipUnresolved
	case "scope":
		return AttnScopeAmbiguous
	case "runtime":
		return AttnRuntimeBoundaryViolated
	default:
		return AttnArtifactDimensionOpen
	}
}

// newAttention builds an attention item with its deterministic canonical identity. controlstate
// owns the prioritization (class/severity); the source owner/schema/identity are the underlying
// finding's authorship, preserved verbatim.
func newAttention(owner, schema, identity, digest, class, reason string, sev AttentionSeverity, basis string, affected []string, blocking bool, evidence []string, nextAction string, architectInput bool) (AttentionItem, error) {
	// Fail closed: every attention item needs a full source identity, class, reason, valid
	// severity, and a severity basis.
	if owner == "" || schema == "" || identity == "" || class == "" || reason == "" || basis == "" {
		return AttentionItem{}, fmt.Errorf("attention construction missing a required field")
	}
	if !validSeverity(sev) {
		return AttentionItem{}, fmt.Errorf("attention severity %q off-vocabulary", sev)
	}
	// Every source, affected-artifact, evidence, and next-action identity must be exact, unpadded,
	// and non-absolute (no path escapes a canonical identity).
	if identity != strings.TrimSpace(identity) || isAbsoluteIdentity(identity) {
		return AttentionItem{}, fmt.Errorf("attention source identity %q is padded or absolute", identity)
	}
	if nextAction != strings.TrimSpace(nextAction) || isAbsoluteIdentity(nextAction) {
		return AttentionItem{}, fmt.Errorf("attention next-action %q is padded or absolute", nextAction)
	}
	for _, x := range append(append([]string{}, affected...), evidence...) {
		if x == "" || x != strings.TrimSpace(x) || isAbsoluteIdentity(x) {
			return AttentionItem{}, fmt.Errorf("attention affected/evidence identity %q is empty, padded, or absolute", x)
		}
	}
	a := AttentionItem{
		SourceOwner: owner, SourceSchema: schema, SourceIdentity: identity, SourceDigest: digest,
		AttentionClass: class, ReasonCode: reason, Severity: sev, SeverityBasis: basis,
		Affected: sortedUnique(affected), Blocking: blocking, Evidence: sortedUnique(evidence),
		NextAction: nextAction, ArchitectInput: architectInput,
	}
	id, err := a.attentionIdentity()
	if err != nil {
		return AttentionItem{}, err
	}
	a.ID = id
	return a, nil
}

// ── Frozen attention-family adapters (every Checkpoint-1 family has a typed construction path) ──

// AttentionForGraphAuthority maps graph-authority/seed state. Returns ok=false when authority is
// current + intact (no attention). It is error-capable: a construction failure is propagated, never
// discarded.
func AttentionForGraphAuthority(authorityID, digest string, observed, current, integrity bool, affected []string) (AttentionItem, bool, error) {
	if observed && !integrity {
		sev, basis := severityForClass(AttnGraphAuthorityInvalid, false)
		a, err := newAttention("graph_authority", "graph_authority", authorityID, digest, AttnGraphAuthorityInvalid, "graph_authority_integrity_failure", sev, basis, affected, true, nil, "sensei.operator", false)
		return a, true, err
	}
	if !observed || !current {
		sev, basis := severityForClass(AttnGraphAuthorityUnavailable, false)
		a, err := newAttention("graph_authority", "graph_authority", authorityID, digest, AttnGraphAuthorityUnavailable, "graph_authority_unavailable", sev, basis, affected, false, nil, "sensei.operator", false)
		return a, true, err
	}
	return AttentionItem{}, false, nil
}

// AttentionForContradiction maps one relevant contradiction finding.
func AttentionForContradiction(cs ContradictionSource, finding ContradictionObservation, affected []string) (AttentionItem, error) {
	sev, basis := severityForClass(AttnContradictionPresent, false)
	return newAttention(nonEmpty(cs.Owner, "extractor.contradiction"), nonEmpty(cs.Schema, "contradiction"), finding.Identity, cs.Digest, AttnContradictionPresent, "contradiction_present", sev, basis, affected, true, []string{finding.Identity}, "architect", true)
}

// AttentionForDimensionBlocker maps an open required dimension, PRESERVING the dimension source
// provenance (owner/schema/identity/digest) rather than synthetic controlstate provenance.
func AttentionForDimensionBlocker(dimension string, obs DimensionObservation, affected []string) (AttentionItem, error) {
	class := dimensionAttentionClass(dimension)
	sev, basis := severityForClass(class, false)
	// Owner-supplied severity is preferred verbatim (never re-severitized) when the source owns it —
	// controlstate composes the owner's severity, it does not author one for such a dimension.
	if validSeverity(obs.SourceSeverity) {
		sev, basis = obs.SourceSeverity, "source_severity"
	}
	return newAttention(obs.SourceOwner, nonEmpty(obs.SourceSchema, "dimension:"+dimension), nonEmpty(obs.SourceIdentity, dimension), obs.SourceDigest, class, "required_dimension_open", sev, basis, affected, true, obs.EvidenceIDs, nonEmpty(obs.NextActionOwner, "architect"), true)
}

// AttentionForOpenQuestion maps an open architect question.
func AttentionForOpenQuestion(questionID string, affected []string) (AttentionItem, error) {
	sev, basis := severityForClass(AttnArchitectQuestionOpen, false)
	return newAttention("questiondisposition", "open_question", questionID, "", AttnArchitectQuestionOpen, "architect_question_open", sev, basis, affected, false, nil, "architect", true)
}

// AttentionForBlindSpot maps a coverage blind spot (high-risk → critical, else warning).
func AttentionForBlindSpot(sourceOwner, identity string, highRisk bool, affected []string) (AttentionItem, error) {
	sev, basis := severityForClass(AttnCoverageBlindSpot, highRisk)
	return newAttention(nonEmpty(sourceOwner, "coverage"), "coverage", identity, "", AttnCoverageBlindSpot, "coverage_blind_spot", sev, basis, affected, false, nil, "architect", true)
}

// AttentionForProvenanceIntegrity maps an integrity/provenance verification failure.
func AttentionForProvenanceIntegrity(sourceOwner, identity, digest string, affected []string) (AttentionItem, error) {
	sev, basis := severityForClass(AttnProvenanceIntegrity, false)
	return newAttention(nonEmpty(sourceOwner, "questionpromotion"), "provenance", identity, digest, AttnProvenanceIntegrity, "provenance_integrity_failure", sev, basis, affected, true, nil, "sensei.operator", false)
}

// AttentionForSeedVerification maps a seed-verification failure (embedded-seed marker mismatch).
func AttentionForSeedVerification(sourceOwner, identity, digest string, affected []string) (AttentionItem, error) {
	sev, basis := severityForClass(AttnSeedVerification, false)
	return newAttention(nonEmpty(sourceOwner, "seedmeta"), "seed_verification", identity, digest, AttnSeedVerification, "seed_verification_failure", sev, basis, affected, true, nil, "sensei.operator", false)
}

// AttentionForForbiddenMove preserves a compatible typed SOURCE severity and never downgrades
// critical; an unknown source severity produces warning plus a typed limitation.
func AttentionForForbiddenMove(sourceOwner, sourceSchema, identity, digest string, sourceSeverity AttentionSeverity, affected, evidence []string, nextAction string) (AttentionItem, []string, error) {
	sev, basis := sourceSeverity, "source_severity"
	var limits []string
	if !validSeverity(sourceSeverity) {
		sev, basis = SeverityWarning, "governed_mapping:unknown_source_severity"
		limits = append(limits, "forbidden_move source severity unknown; defaulted to warning")
	}
	a, err := newAttention(nonEmpty(sourceOwner, "editcheck"), nonEmpty(sourceSchema, "forbidden_move"), identity, digest, AttnForbiddenMove, "forbidden_move_detected", sev, basis, affected, true, evidence, nonEmpty(nextAction, "architect"), true)
	return a, limits, err
}

// buildArtifactAttention composes attention for one artifact from its TYPED bundle, preserving
// each finding's source provenance. It FAILS CLOSED: a malformed attention item or a
// dimension-assessment error propagates rather than being silently omitted.
func buildArtifactAttention(id ArtifactIdentity, policy ClassPolicy, bundle ArtifactSourceBundle) ([]AttentionItem, error) {
	var items []AttentionItem
	add := func(a AttentionItem, err error) error {
		if err != nil {
			return err
		}
		items = append(items, a)
		return nil
	}
	affected := []string{id.NodeIRI}

	if a, ok, err := AttentionForGraphAuthority(id.GraphAuthorityIdentity, bundle.GraphAuthority.Digest, bundle.GraphAuthority.Observed, bundle.GraphAuthority.Current, bundle.GraphAuthority.Integrity, affected); err != nil {
		return nil, err
	} else if ok {
		items = append(items, a)
	}
	// Contradictions (available or degraded — a relevant finding is a definitive blocker).
	if bundle.Contradiction.Availability == SourceAvailable || bundle.Contradiction.Availability == SourceDegraded {
		for _, f := range bundle.Contradiction.Findings {
			if f.Relevant {
				if err := add(AttentionForContradiction(bundle.Contradiction, f, affected)); err != nil {
					return nil, err
				}
			}
		}
	}
	if policy.Coverage == CoverageAssessable {
		ap, ok := assessmentPolicies()[policy.AssessmentPolicyID]
		if !ok {
			return nil, fmt.Errorf("assessable class %q has no assessment policy", policy.ClassIRI)
		}
		dims, _, err := assessDimensions(ap, bundle)
		if err != nil {
			return nil, err
		}
		for _, da := range dims {
			if da.Dimension == "contradiction" {
				continue
			}
			if da.Applicable && da.Required && da.State == DimOpen {
				if err := add(AttentionForDimensionBlocker(da.Dimension, bundle.Dimensions[da.Dimension], affected)); err != nil {
					return nil, err
				}
			}
			for _, q := range da.Questions {
				if err := add(AttentionForOpenQuestion(q, affected)); err != nil {
					return nil, err
				}
			}
		}
	}
	return dedupSortAttention(items), nil
}

// dedupSortAttention deterministically de-duplicates by canonical identity and sorts
// critical→warning→attention→informational, then class, affected IRI, and id.
func dedupSortAttention(in []AttentionItem) []AttentionItem {
	seen := map[string]bool{}
	var out []AttentionItem
	for _, a := range in {
		if seen[a.ID] {
			continue
		}
		seen[a.ID] = true
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if severityRank(out[i].Severity) != severityRank(out[j].Severity) {
			return severityRank(out[i].Severity) < severityRank(out[j].Severity)
		}
		if out[i].AttentionClass != out[j].AttentionClass {
			return out[i].AttentionClass < out[j].AttentionClass
		}
		ai, aj := firstOrEmpty(out[i].Affected), firstOrEmpty(out[j].Affected)
		if ai != aj {
			return ai < aj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func firstOrEmpty(s []string) string {
	if len(s) > 0 {
		return s[0]
	}
	return ""
}
