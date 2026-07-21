// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import (
	"fmt"
	"sort"
	"strings"
)

// ArtifactStateSchema identifies architecture.artifact_state/v1.
const ArtifactStateSchema = "architecture.artifact_state/v1"

// DimensionOutcome is the CLOSED per-dimension owner outcome vocabulary (replaces contradictory
// booleans). Zero value fails closed.
type DimensionOutcome string

const (
	OutcomeSatisfied         DimensionOutcome = "satisfied"
	OutcomeDefinitiveBlocker DimensionOutcome = "definitive_blocker"
	OutcomeDegraded          DimensionOutcome = "degraded"
	OutcomeInsufficient      DimensionOutcome = "insufficient"
	OutcomeNotApplicable     DimensionOutcome = "not_applicable"
)

func validOutcome(o DimensionOutcome) bool {
	switch o {
	case OutcomeSatisfied, OutcomeDefinitiveBlocker, OutcomeDegraded, OutcomeInsufficient, OutcomeNotApplicable:
		return true
	}
	return false
}

// DimensionObservation is a SOURCE-BOUND typed owner observation. Every judgment traces to an
// exact source; the outcome is a single closed value (no contradictory booleans).
type DimensionObservation struct {
	Dimension          string
	SourceOwner        string
	SourceSchema       string
	SourceIdentity     string
	SourceDigest       string
	SourceAvailability SourceAvailability
	SourceReasonCode   string
	Outcome            DimensionOutcome
	BlockerIDs         []string
	EvidenceIDs        []string
	QuestionIDs        []string
	NextActionOwner    string
	// SourceSeverity is an OPTIONAL owner-supplied attention severity for this dimension. When set
	// (and valid), an open dimension's attention uses it verbatim (basis source_severity) instead of
	// the governed class→severity mapping — so a source that owns its own severity (e.g. the
	// runtimeboundary assessment) is never re-severitized by controlstate.
	SourceSeverity AttentionSeverity
}

// ContradictionObservation is one typed contradiction finding.
type ContradictionObservation struct {
	Identity string
	Relevant bool
}

// ContradictionSource is the typed contradiction owner observation. An AVAILABLE source with zero
// relevant findings proves absence; graph authority alone never does.
type ContradictionSource struct {
	Owner        string
	Schema       string
	Identity     string
	Digest       string
	Availability SourceAvailability
	ReasonCode   string
	Findings     []ContradictionObservation
}

// GraphAuthorityObservation is the typed authority observation (primary source of an assessment).
type GraphAuthorityObservation struct {
	Observed  bool
	Current   bool
	Integrity bool
	Identity  string
	Digest    string
}

// ScopedFeedbackRef is an EXACT-scope Phase 9.6 feedback reference — never a repository-wide scan
// and never graph-adjacency-associated. controlstate preserves it verbatim (never reinterprets).
type ScopedFeedbackRef struct {
	ScopeIdentity     string   `json:"scope_identity" yaml:"scope_identity"`
	ProjectionDigest  string   `json:"projection_digest" yaml:"projection_digest"`
	Availability      string   `json:"availability" yaml:"availability"`
	VerifiedRecordIDs []string `json:"verified_record_ids,omitempty" yaml:"verified_record_ids,omitempty"`
	LineageIDs        []string `json:"lineage_ids,omitempty" yaml:"lineage_ids,omitempty"`
	Limitations       []string `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

// ArtifactSourceBundle is the TYPED input to BuildArtifactState — typed observations only.
type ArtifactSourceBundle struct {
	GraphAuthority GraphAuthorityObservation
	Contradiction  ContradictionSource
	Dimensions     map[string]DimensionObservation // non-contradiction dimensions
	Lifecycle      LifecycleSource
	Feedback       *ScopedFeedbackRef
}

// ArtifactState is architecture.artifact_state/v1 for one exact artifact.
type ArtifactState struct {
	ProjectionMeta `json:",inline" yaml:",inline"`
	Identity       ArtifactIdentity      `json:"identity" yaml:"identity"`
	CanonicalClass string                `json:"canonical_class" yaml:"canonical_class"`
	Coverage       AssessmentCoverage    `json:"assessment_coverage" yaml:"assessment_coverage"`
	Closure        ArtifactClosure       `json:"closure" yaml:"closure"`
	ClosureReason  string                `json:"closure_reason,omitempty" yaml:"closure_reason,omitempty"`
	Lifecycle      LifecycleAssessment   `json:"lifecycle" yaml:"lifecycle"`
	Dimensions     []DimensionAssessment `json:"dimensions,omitempty" yaml:"dimensions,omitempty"`
	Attention      []AttentionItem       `json:"attention,omitempty" yaml:"attention,omitempty"`
	Questions      []string              `json:"questions,omitempty" yaml:"questions,omitempty"`
	Evidence       []string              `json:"evidence,omitempty" yaml:"evidence,omitempty"`
	Feedback       *ScopedFeedbackRef    `json:"feedback,omitempty" yaml:"feedback,omitempty"`
	NextAction     string                `json:"next_action_owner,omitempty" yaml:"next_action_owner,omitempty"`
}

// BuildArtifactState composes the exact per-artifact projection. It validates the identity/
// resolution pair (never trusts the caller), binds every judgment to an exact typed source, and
// never copies closure.Report.Verdict.
func BuildArtifactState(reg Registry, id ArtifactIdentity, res ClassResolution, bundle ArtifactSourceBundle) (ArtifactState, error) {
	if err := reg.Validate(); err != nil {
		return ArtifactState{}, fmt.Errorf("invalid registry: %w", err)
	}
	if err := ValidateArtifactIdentity(reg, id, res); err != nil {
		return ArtifactState{}, fmt.Errorf("incoherent artifact identity: %w", err)
	}
	// Graph-authority observation coherence: Current/Integrity cannot be true while unobserved; an
	// observed authority carries a non-empty identity equal to the artifact's authority identity.
	if (bundle.GraphAuthority.Current || bundle.GraphAuthority.Integrity) && !bundle.GraphAuthority.Observed {
		return ArtifactState{}, fmt.Errorf("graph authority cannot be current/intact while unobserved")
	}
	if bundle.GraphAuthority.Observed {
		if bundle.GraphAuthority.Identity == "" {
			return ArtifactState{}, fmt.Errorf("observed graph authority has no identity")
		}
		if bundle.GraphAuthority.Identity != id.GraphAuthorityIdentity {
			return ArtifactState{}, fmt.Errorf("graph authority identity %q does not match artifact identity %q", bundle.GraphAuthority.Identity, id.GraphAuthorityIdentity)
		}
	}
	policy, known := reg.classByIRI(id.CanonicalClass)
	var limits []string
	sources := []SourceStatus{graphAuthoritySource(bundle.GraphAuthority)}

	st := ArtifactState{Identity: id, CanonicalClass: id.CanonicalClass}

	if !known || policy.Unclassified {
		st.Coverage = CoverageUnknown
		st.Closure = ClosureUnknown
		st.ClosureReason = res.ReasonCode
		if st.ClosureReason == "" {
			st.ClosureReason = "unknown_class"
		}
		st.Lifecycle = LifecycleAssessment{Applicable: true, State: LifecycleUnknown, SourceAvailability: SourceUnavailable, ReasonCode: "no_lifecycle_source"}
		limits = append(limits, "artifact class is unclassified: "+st.ClosureReason)
		return finalizeArtifactState(st, sources, limits)
	}

	st.Coverage = policy.Coverage
	st.Lifecycle = assessLifecycle(policy, bundle.Lifecycle)
	if ls, ok := lifecycleSourceStatus(policy, bundle.Lifecycle); ok {
		sources = append(sources, ls)
	}

	switch policy.Coverage {
	case CoverageExplicitlyNotApplicable:
		st.Closure = ClosureNotApplicable
		st.ClosureReason = "class_closure_not_applicable"
	case CoverageUnsupported:
		st.Closure = ClosureUnknown
		st.ClosureReason = "class_unsupported"
		limits = append(limits, "class has no reviewed artifact-closure policy")
	case CoverageAssessable:
		ap, ok := assessmentPolicies()[policy.AssessmentPolicyID]
		if !ok {
			return ArtifactState{}, fmt.Errorf("assessable class %q has no assessment policy", policy.ClassIRI)
		}
		dims, dimSources, err := assessDimensions(ap, bundle)
		if err != nil {
			return ArtifactState{}, err
		}
		st.Dimensions = dims
		sources = append(sources, dimSources...)
		for _, dm := range dims {
			st.Evidence = append(st.Evidence, dm.Evidence...)
			st.Questions = append(st.Questions, dm.Questions...)
		}
		st.Evidence = sortedUnique(st.Evidence)
		st.Questions = sortedUnique(st.Questions)
		st.Closure, st.ClosureReason = aggregateArtifactClosure(dims, bundle.GraphAuthority)
	default:
		st.Closure = ClosureUnknown
		st.ClosureReason = "class_coverage_unknown"
	}

	if bundle.Feedback != nil {
		if err := validateScopedFeedback(*bundle.Feedback); err != nil {
			return ArtifactState{}, fmt.Errorf("invalid scoped feedback: %w", err)
		}
		f := normalizeScopedFeedback(*bundle.Feedback)
		st.Feedback = &f
		sources = append(sources, srcStatus("briefingfeedback", "briefing.feedback_projection/v1", f.ScopeIdentity, f.ProjectionDigest, feedbackSourceAvailability(f.Availability), ImpactRelevant, feedbackSourceReason(f.Availability)))
	}

	attention, err := buildArtifactAttention(id, policy, bundle)
	if err != nil {
		return ArtifactState{}, fmt.Errorf("attention construction failed: %w", err)
	}
	st.Attention = attention
	st.NextAction = "architect"
	return finalizeArtifactState(st, sources, limits)
}

func finalizeArtifactState(st ArtifactState, sources []SourceStatus, limits []string) (ArtifactState, error) {
	avail := aggregateAvailability(sources)
	st.ProjectionMeta = newMeta(ArtifactStateSchema, st.Identity.RepositoryIdentity, st.Identity.DomainIdentity, avail, sources, limits)
	dig, err := computeArtifactStateDigest(st)
	if err != nil {
		return ArtifactState{}, err
	}
	st.DigestSHA256 = dig
	if err := ValidateArtifactState(st); err != nil {
		return ArtifactState{}, err
	}
	return st, nil
}

// graphAuthoritySource preserves the OBSERVATION identity (never substitutes an artifact
// identity for a missing observation identity).
func graphAuthoritySource(a GraphAuthorityObservation) SourceStatus {
	switch {
	case !a.Observed:
		return srcStatus("graph_authority", "graph_authority", a.Identity, a.Digest, SourceUnavailable, ImpactPrimary, "graph_authority_unobserved")
	case !a.Integrity:
		return srcStatus("graph_authority", "graph_authority", a.Identity, a.Digest, SourceInvalid, ImpactPrimary, "graph_authority_integrity_failure")
	case !a.Current:
		return srcStatus("graph_authority", "graph_authority", a.Identity, a.Digest, SourceDegraded, ImpactPrimary, "graph_authority_stale")
	default:
		return srcStatus("graph_authority", "graph_authority", a.Identity, a.Digest, SourceAvailable, ImpactPrimary, "")
	}
}

// assessDimensions applies each reviewed dimension policy over its typed observation, contributes
// a SourceStatus per dimension, and preserves the underlying source provenance.
func assessDimensions(ap assessmentPolicy, bundle ArtifactSourceBundle) ([]DimensionAssessment, []SourceStatus, error) {
	var out []DimensionAssessment
	var sources []SourceStatus
	for _, dp := range ap.Dimensions {
		if dp.Dimension == "contradiction" {
			if err := validateContradictionSource(dp, bundle.Contradiction); err != nil {
				return nil, nil, err
			}
			da, src := assessContradictionDimension(dp, bundle.Contradiction)
			out = append(out, da)
			sources = append(sources, src)
			continue
		}
		obs, ok := bundle.Dimensions[dp.Dimension]
		da := DimensionAssessment{Dimension: dp.Dimension, Label: dp.Label, Applicable: true, Required: dp.Required, Owner: dp.Owner, NextAction: dp.NextAction}
		if !ok {
			da.State, da.ReasonCode = DimUnknown, "source_not_observed"
			out = append(out, da)
			sources = append(sources, srcStatus(dp.Owner, "dimension:"+dp.Dimension, "", "", SourceUnavailable, ImpactRequired, "source_not_observed"))
			continue
		}
		if err := validateDimensionObservation(dp, obs); err != nil {
			return nil, nil, err
		}
		da.State, da.ReasonCode = dimensionStateFor(obs)
		if obs.Outcome == OutcomeDefinitiveBlocker {
			da.Blockers = sortedUnique(obs.BlockerIDs)
		}
		da.Evidence = sortedUnique(obs.EvidenceIDs)
		da.Questions = sortedUnique(obs.QuestionIDs)
		da.Owner = obs.SourceOwner
		out = append(out, da)
		sources = append(sources, srcStatus(obs.SourceOwner, obs.SourceSchema, obs.SourceIdentity, obs.SourceDigest, obs.SourceAvailability, ImpactRequired, obs.SourceReasonCode))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Dimension < out[j].Dimension })
	return out, sources, nil
}

// validateDimensionObservation enforces the source-bound dimension contract, including
// admissibility: an untrustworthy source may admit no blocker/evidence/question/next-action, and a
// degraded/available source may not redirect the next action away from the reviewed policy.
func validateDimensionObservation(dp dimensionPolicy, obs DimensionObservation) error {
	if obs.Dimension != dp.Dimension {
		return fmt.Errorf("dimension observation %q does not match policy dimension %q", obs.Dimension, dp.Dimension)
	}
	if obs.SourceOwner == "" || obs.SourceOwner != dp.Owner {
		return fmt.Errorf("dimension %q source owner %q does not match policy owner %q", dp.Dimension, obs.SourceOwner, dp.Owner)
	}
	if !validSourceAvailability(obs.SourceAvailability) {
		return fmt.Errorf("dimension %q source availability off-vocabulary", dp.Dimension)
	}
	if !validOutcome(obs.Outcome) {
		return fmt.Errorf("dimension %q outcome %q off-vocabulary (fail closed)", dp.Dimension, obs.Outcome)
	}
	if obs.Outcome == OutcomeNotApplicable {
		if !dp.NotApplicableEligible {
			return fmt.Errorf("dimension %q not_applicable requires an explicit applicability policy", dp.Dimension)
		}
		// A typed not_applicable is a definitive owner statement: it comes from an available source
		// and manufactures no blocker/evidence/question.
		if obs.SourceAvailability != SourceAvailable {
			return fmt.Errorf("dimension %q not_applicable requires an available source", dp.Dimension)
		}
		if len(obs.BlockerIDs) > 0 || len(obs.QuestionIDs) > 0 {
			return fmt.Errorf("dimension %q not_applicable cannot admit blockers/questions", dp.Dimension)
		}
	}
	// An owner-supplied severity, when present, must be a valid closed value; controlstate never
	// invents or overrides it.
	if obs.SourceSeverity != "" && !validSeverity(obs.SourceSeverity) {
		return fmt.Errorf("dimension %q source severity %q off-vocabulary", dp.Dimension, obs.SourceSeverity)
	}
	// Any admitted identity list must be exact, unpadded, non-absolute, sorted, and unique.
	for name, ids := range map[string][]string{"blocker": obs.BlockerIDs, "evidence": obs.EvidenceIDs, "question": obs.QuestionIDs} {
		if err := validateIdentityList(ids); err != nil {
			return fmt.Errorf("dimension %q %s identities: %w", dp.Dimension, name, err)
		}
	}

	switch obs.SourceAvailability {
	case SourceUnavailable, SourceInvalid:
		// An untrustworthy source is insufficient and manufactures nothing.
		if obs.Outcome != OutcomeInsufficient {
			return fmt.Errorf("dimension %q unavailable/invalid source must be insufficient", dp.Dimension)
		}
		if len(obs.BlockerIDs) > 0 || len(obs.EvidenceIDs) > 0 || len(obs.QuestionIDs) > 0 || obs.NextActionOwner != "" {
			return fmt.Errorf("dimension %q unavailable/invalid source cannot admit blocker/evidence/question/next-action", dp.Dimension)
		}
	case SourceDegraded:
		if obs.SourceSchema == "" || obs.SourceIdentity == "" {
			return fmt.Errorf("dimension %q observed source missing schema/identity", dp.Dimension)
		}
		if obs.Outcome == OutcomeSatisfied {
			return fmt.Errorf("dimension %q degraded source cannot be satisfied", dp.Dimension)
		}
		if obs.Outcome == OutcomeDefinitiveBlocker && len(obs.BlockerIDs) == 0 {
			return fmt.Errorf("dimension %q definitive_blocker without a blocker identity", dp.Dimension)
		}
		if obs.NextActionOwner != "" && obs.NextActionOwner != dp.NextAction {
			return fmt.Errorf("dimension %q degraded source cannot redirect the next action", dp.Dimension)
		}
	case SourceAvailable:
		if obs.SourceSchema == "" || obs.SourceIdentity == "" {
			return fmt.Errorf("dimension %q observed source missing schema/identity", dp.Dimension)
		}
		if obs.Outcome == OutcomeDefinitiveBlocker && len(obs.BlockerIDs) == 0 {
			return fmt.Errorf("dimension %q definitive_blocker without a blocker identity", dp.Dimension)
		}
		if obs.Outcome == OutcomeSatisfied && len(obs.BlockerIDs) > 0 {
			return fmt.Errorf("dimension %q satisfied must carry no blockers", dp.Dimension)
		}
		if obs.NextActionOwner != "" && obs.NextActionOwner != dp.NextAction {
			return fmt.Errorf("dimension %q next-action owner %q must equal the reviewed policy", dp.Dimension, obs.NextActionOwner)
		}
	}
	return nil
}

// validateIdentityList requires each identity to be exact (non-empty), unpadded, and non-absolute,
// and the list to be canonically sorted and unique.
func validateIdentityList(ids []string) error {
	for _, id := range ids {
		if id == "" || id != strings.TrimSpace(id) || isAbsoluteIdentity(id) {
			return fmt.Errorf("identity %q is empty, padded, or absolute", id)
		}
	}
	if !equalStrings(ids, sortedUnique(ids)) {
		return fmt.Errorf("identities are not canonically sorted and unique")
	}
	return nil
}

func dimensionStateFor(obs DimensionObservation) (DimensionState, string) {
	// An unavailable/invalid source is untrustworthy → unknown.
	if obs.SourceAvailability == SourceUnavailable || obs.SourceAvailability == SourceInvalid {
		return DimUnknown, "source_unavailable"
	}
	// A definitive blocker is DEFINITIVE even on a degraded source — it stays open, never
	// downgraded to degraded.
	if obs.Outcome == OutcomeDefinitiveBlocker {
		return DimOpen, "definitive_blocker"
	}
	if obs.SourceAvailability == SourceDegraded {
		return DimDegraded, "degraded_source"
	}
	switch obs.Outcome {
	case OutcomeSatisfied:
		return DimSatisfied, ""
	case OutcomeNotApplicable:
		return DimNotApplicable, "not_applicable"
	case OutcomeDegraded:
		return DimDegraded, "degraded"
	default:
		return DimUnknown, "insufficient_evidence"
	}
}

// validateContradictionSource enforces the contradiction owner + availability matrix: an
// observed (available/degraded/invalid) source needs owner/schema/identity, the owner exactly
// matches the contradiction dimension policy owner (no fallback), and finding identities are
// unpadded, non-empty, and unique.
func validateContradictionSource(dp dimensionPolicy, cs ContradictionSource) error {
	if cs.Availability != "" && !validSourceAvailability(cs.Availability) {
		return fmt.Errorf("contradiction source availability %q off-vocabulary", cs.Availability)
	}
	observed := cs.Availability == SourceAvailable || cs.Availability == SourceDegraded || cs.Availability == SourceInvalid
	if observed {
		if cs.Owner == "" || cs.Schema == "" || cs.Identity == "" {
			return fmt.Errorf("observed contradiction source missing owner/schema/identity")
		}
		if cs.Owner != dp.Owner {
			return fmt.Errorf("contradiction source owner %q does not match policy owner %q", cs.Owner, dp.Owner)
		}
	}
	seen := map[string]bool{}
	for _, f := range cs.Findings {
		if f.Identity == "" || f.Identity != strings.TrimSpace(f.Identity) {
			return fmt.Errorf("contradiction finding has an empty or padded identity")
		}
		if seen[f.Identity] {
			return fmt.Errorf("duplicate contradiction finding %q", f.Identity)
		}
		seen[f.Identity] = true
	}
	return nil
}

// assessContradictionDimension drives the contradiction dimension from the typed source. A
// relevant finding is a DEFINITIVE blocker (open) even on a degraded source; only the projection
// availability degrades. An available source with no relevant findings proves absence; a degraded
// source cannot prove absence; unavailable/invalid → unknown.
func assessContradictionDimension(dp dimensionPolicy, cs ContradictionSource) (DimensionAssessment, SourceStatus) {
	da := DimensionAssessment{Dimension: "contradiction", Label: dp.Label, Applicable: true, Required: dp.Required, Owner: cs.Owner, NextAction: dp.NextAction}
	if da.Owner == "" {
		da.Owner = dp.Owner
	}
	avail := cs.Availability
	if avail == "" {
		avail = SourceUnavailable
	}
	src := srcStatus(nonEmpty(cs.Owner, dp.Owner), nonEmpty(cs.Schema, "contradiction"), cs.Identity, cs.Digest, avail, ImpactRequired, cs.ReasonCode)

	var relevant []string
	for _, f := range cs.Findings {
		if f.Relevant {
			relevant = append(relevant, f.Identity)
		}
	}
	relevant = sortedUnique(relevant)

	switch avail {
	case SourceAvailable:
		if len(relevant) > 0 {
			da.State, da.ReasonCode, da.Blockers = DimOpen, "contradiction_present", relevant
		} else {
			da.State = DimSatisfied
		}
	case SourceDegraded:
		if len(relevant) > 0 {
			// A degraded source with a relevant finding is still a definitive blocker → open,
			// while the projection degrades to partial (source degraded).
			da.State, da.ReasonCode, da.Blockers = DimOpen, "contradiction_present", relevant
		} else {
			da.State, da.ReasonCode = DimUnknown, "contradiction_source_degraded"
		}
	default: // unavailable / invalid
		da.State, da.ReasonCode = DimUnknown, "contradiction_source_unavailable"
	}
	return da, src
}

// aggregateArtifactClosure applies the frozen §7 precedence. It never copies closure.Report.Verdict.
func aggregateArtifactClosure(dims []DimensionAssessment, auth GraphAuthorityObservation) (ArtifactClosure, string) {
	anyOpen, anyUnknown, anyDegraded, allReqSourcesAvail := false, false, false, true
	for _, dm := range dims {
		if !dm.Applicable || !dm.Required {
			continue
		}
		switch dm.State {
		case DimOpen:
			anyOpen = true
		case DimUnknown:
			anyUnknown = true
			allReqSourcesAvail = false
		case DimDegraded:
			anyDegraded = true
		}
	}
	authorityCurrent := auth.Observed && auth.Current && auth.Integrity
	if !auth.Observed {
		allReqSourcesAvail = false
	}
	switch {
	case anyOpen:
		return ClosureOpen, "required_dimension_open"
	case !authorityCurrent:
		return ClosureUnknown, "graph_authority_not_current"
	case anyUnknown:
		return ClosureUnknown, "required_dimension_unknown"
	case !allReqSourcesAvail:
		return ClosureUnknown, "required_source_unavailable"
	case anyDegraded:
		return ClosureDegraded, "degraded_source_or_dimension"
	default:
		return ClosureClosed, ""
	}
}

// Phase 9.6 availability vocabulary (validated, never reinterpreted).
var phase96Availability = map[string]bool{
	"feedback_available": true, "feedback_empty": true, "feedback_degraded": true,
	"feedback_unavailable": true, "feedback_invalid": true,
}

func validateScopedFeedback(f ScopedFeedbackRef) error {
	for name, v := range map[string]string{"scope identity": f.ScopeIdentity, "projection digest": f.ProjectionDigest} {
		if v == "" || v != strings.TrimSpace(v) {
			return fmt.Errorf("scoped feedback %s is empty or padded", name)
		}
	}
	if !phase96Availability[f.Availability] {
		return fmt.Errorf("scoped feedback availability %q is not a Phase 9.6 value", f.Availability)
	}
	for _, id := range append(append([]string{}, f.VerifiedRecordIDs...), f.LineageIDs...) {
		if id == "" || id != strings.TrimSpace(id) || isAbsoluteIdentity(id) {
			return fmt.Errorf("scoped feedback carries an empty, padded, or absolute-path identity")
		}
	}
	for _, l := range f.Limitations {
		if l != strings.TrimSpace(l) {
			return fmt.Errorf("scoped feedback limitation is padded")
		}
	}
	return nil
}

// isAbsoluteIdentity refuses Unix ("/"), UNC / backslash-rooted ("\\host", "\abs"), and a
// Windows DRIVE path form ("C:\" or "C:/") — but NOT legitimate colon-bearing identities like
// "aw:x", "contract:example", or "invariant:example".
func isAbsoluteIdentity(id string) bool {
	if strings.HasPrefix(id, "/") || strings.HasPrefix(id, `\`) {
		return true
	}
	if len(id) >= 3 && isASCIILetter(id[0]) && id[1] == ':' && (id[2] == '\\' || id[2] == '/') {
		return true
	}
	return false
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func normalizeScopedFeedback(f ScopedFeedbackRef) ScopedFeedbackRef {
	f.VerifiedRecordIDs = sortedUnique(f.VerifiedRecordIDs)
	f.LineageIDs = sortedUnique(f.LineageIDs)
	f.Limitations = sortedUnique(f.Limitations)
	return f
}

func feedbackSourceAvailability(v string) SourceAvailability {
	switch v {
	case "feedback_available", "feedback_empty":
		return SourceAvailable
	case "feedback_degraded":
		return SourceDegraded
	case "feedback_invalid":
		return SourceInvalid
	default:
		return SourceUnavailable
	}
}

func feedbackSourceReason(v string) string {
	if feedbackSourceAvailability(v) == SourceAvailable {
		return ""
	}
	return v
}

func computeArtifactStateDigest(st ArtifactState) (string, error) {
	st.DigestSHA256 = ""
	return digestOf(st)
}

// ValidateArtifactState strictly validates the projection and refuses impossible combinations.
func ValidateArtifactState(st ArtifactState) error {
	if err := validateMeta(st.ProjectionMeta, ArtifactStateSchema); err != nil {
		return err
	}
	if st.Identity.NodeIRI == "" || st.CanonicalClass == "" || st.Identity.GraphAuthorityIdentity == "" {
		return fmt.Errorf("artifact state missing identity")
	}
	if !validCoverage(st.Coverage) || !validClosure(st.Closure) {
		return fmt.Errorf("artifact state coverage/closure off-vocabulary")
	}
	if !validLifecycleState(st.Lifecycle.State) {
		return fmt.Errorf("artifact lifecycle state off-vocabulary")
	}
	for _, dm := range st.Dimensions {
		if !validDimState(dm.State) {
			return fmt.Errorf("dimension %q state off-vocabulary", dm.Dimension)
		}
	}
	for _, a := range st.Attention {
		if err := validateAttentionItem(a); err != nil {
			return err
		}
	}
	if st.Closure == ClosureClosed {
		for _, dm := range st.Dimensions {
			if dm.Applicable && dm.Required && dm.State != DimSatisfied {
				return fmt.Errorf("closed artifact has a non-satisfied required dimension %q", dm.Dimension)
			}
		}
		for _, s := range st.Sources {
			if (s.Impact == ImpactPrimary || s.Impact == ImpactRequired) && s.Availability != SourceAvailable {
				return fmt.Errorf("closed artifact has an unavailable required source %q", s.Owner)
			}
		}
	}
	if st.Closure == ClosureNotApplicable && st.Coverage == CoverageAssessable {
		return fmt.Errorf("assessable class cannot be not_applicable without an explicit exception")
	}
	if st.Closure == ClosureUnknown && st.Coverage == CoverageAssessable && st.ClosureReason == "" {
		return fmt.Errorf("unknown closure requires an explicit reason")
	}
	if st.Closure == ClosureDegraded {
		for _, dm := range st.Dimensions {
			if dm.Applicable && dm.Required && dm.State == DimOpen {
				return fmt.Errorf("degraded artifact conceals a definitive open blocker in %q", dm.Dimension)
			}
		}
	}
	want, err := computeArtifactStateDigest(st)
	if err != nil {
		return err
	}
	if st.DigestSHA256 != want {
		return fmt.Errorf("artifact state digest does not match its content")
	}
	return nil
}
