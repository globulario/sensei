// SPDX-License-Identifier: AGPL-3.0-only

// Package controlstateproto is the PURE transport mapper between the canonical controlstate
// projections (golang/architecture/controlstate) and their protobuf wire representation.
//
// It is lossless transport, never a semantic owner (governed invariant:
// controlstate.protobuf_is_lossless_transport_not_semantic_owner):
//
//   - every mapping validates the canonical projection FIRST (including every nested attention
//     item) and fails on any mismatch — malformed fields are never silently omitted;
//   - closed vocabularies map one-to-one; a value outside the closed vocabulary is an error,
//     and *_UNSPECIFIED is never produced;
//   - unknown-versus-zero is preserved (nil counts stay absent, observed zeros stay zero);
//   - repeated fields keep the canonical controlstate order;
//   - the canonical DigestSHA256 is COPIED verbatim — nothing here recomputes a digest.
//
// The package imports controlstate, the generated protobuf types, and stdlib only. It touches
// no graph store, no governed YAML, no mutation owner, and no certification writer.
package controlstateproto

import (
	"fmt"

	"github.com/globulario/sensei/golang/architecture/controlstate"
	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// ── Closed-vocabulary enum mappings (one-to-one, error on off-vocabulary) ──

func availabilityToProto(a controlstate.Availability) (awarenesspb.ArchitectureAvailability, error) {
	switch a {
	case controlstate.AvailabilityAvailable:
		return awarenesspb.ArchitectureAvailability_ARCHITECTURE_AVAILABILITY_AVAILABLE, nil
	case controlstate.AvailabilityPartial:
		return awarenesspb.ArchitectureAvailability_ARCHITECTURE_AVAILABILITY_PARTIAL, nil
	case controlstate.AvailabilityUnavailable:
		return awarenesspb.ArchitectureAvailability_ARCHITECTURE_AVAILABILITY_UNAVAILABLE, nil
	case controlstate.AvailabilityInvalid:
		return awarenesspb.ArchitectureAvailability_ARCHITECTURE_AVAILABILITY_INVALID, nil
	}
	return 0, fmt.Errorf("availability %q is outside the closed vocabulary", a)
}

func sourceAvailabilityToProto(a controlstate.SourceAvailability) (awarenesspb.ArchitectureSourceAvailability, error) {
	switch a {
	case controlstate.SourceAvailable:
		return awarenesspb.ArchitectureSourceAvailability_ARCHITECTURE_SOURCE_AVAILABILITY_AVAILABLE, nil
	case controlstate.SourceDegraded:
		return awarenesspb.ArchitectureSourceAvailability_ARCHITECTURE_SOURCE_AVAILABILITY_DEGRADED, nil
	case controlstate.SourceUnavailable:
		return awarenesspb.ArchitectureSourceAvailability_ARCHITECTURE_SOURCE_AVAILABILITY_UNAVAILABLE, nil
	case controlstate.SourceInvalid:
		return awarenesspb.ArchitectureSourceAvailability_ARCHITECTURE_SOURCE_AVAILABILITY_INVALID, nil
	}
	return 0, fmt.Errorf("source availability %q is outside the closed vocabulary", a)
}

func sourceImpactToProto(i controlstate.SourceImpact) (awarenesspb.ArchitectureSourceImpact, error) {
	switch i {
	case controlstate.ImpactPrimary:
		return awarenesspb.ArchitectureSourceImpact_ARCHITECTURE_SOURCE_IMPACT_PRIMARY, nil
	case controlstate.ImpactRequired:
		return awarenesspb.ArchitectureSourceImpact_ARCHITECTURE_SOURCE_IMPACT_REQUIRED, nil
	case controlstate.ImpactRelevant:
		return awarenesspb.ArchitectureSourceImpact_ARCHITECTURE_SOURCE_IMPACT_RELEVANT, nil
	case controlstate.ImpactOptional:
		return awarenesspb.ArchitectureSourceImpact_ARCHITECTURE_SOURCE_IMPACT_OPTIONAL, nil
	}
	return 0, fmt.Errorf("source impact %q is outside the closed vocabulary", i)
}

func closureToProto(c controlstate.ArtifactClosure) (awarenesspb.ArchitectureArtifactClosure, error) {
	switch c {
	case controlstate.ClosureClosed:
		return awarenesspb.ArchitectureArtifactClosure_ARCHITECTURE_ARTIFACT_CLOSURE_CLOSED, nil
	case controlstate.ClosureOpen:
		return awarenesspb.ArchitectureArtifactClosure_ARCHITECTURE_ARTIFACT_CLOSURE_OPEN, nil
	case controlstate.ClosureDegraded:
		return awarenesspb.ArchitectureArtifactClosure_ARCHITECTURE_ARTIFACT_CLOSURE_DEGRADED, nil
	case controlstate.ClosureUnknown:
		return awarenesspb.ArchitectureArtifactClosure_ARCHITECTURE_ARTIFACT_CLOSURE_UNKNOWN, nil
	case controlstate.ClosureNotApplicable:
		return awarenesspb.ArchitectureArtifactClosure_ARCHITECTURE_ARTIFACT_CLOSURE_NOT_APPLICABLE, nil
	}
	return 0, fmt.Errorf("artifact closure %q is outside the closed vocabulary", c)
}

func dimensionStateToProto(s controlstate.DimensionState) (awarenesspb.ArchitectureDimensionState, error) {
	switch s {
	case controlstate.DimSatisfied:
		return awarenesspb.ArchitectureDimensionState_ARCHITECTURE_DIMENSION_STATE_SATISFIED, nil
	case controlstate.DimOpen:
		return awarenesspb.ArchitectureDimensionState_ARCHITECTURE_DIMENSION_STATE_OPEN, nil
	case controlstate.DimDegraded:
		return awarenesspb.ArchitectureDimensionState_ARCHITECTURE_DIMENSION_STATE_DEGRADED, nil
	case controlstate.DimUnknown:
		return awarenesspb.ArchitectureDimensionState_ARCHITECTURE_DIMENSION_STATE_UNKNOWN, nil
	case controlstate.DimNotApplicable:
		return awarenesspb.ArchitectureDimensionState_ARCHITECTURE_DIMENSION_STATE_NOT_APPLICABLE, nil
	}
	return 0, fmt.Errorf("dimension state %q is outside the closed vocabulary", s)
}

func lifecycleStateToProto(s controlstate.LifecycleState) (awarenesspb.ArchitectureLifecycleState, error) {
	switch s {
	case controlstate.LifecycleActive:
		return awarenesspb.ArchitectureLifecycleState_ARCHITECTURE_LIFECYCLE_STATE_ACTIVE, nil
	case controlstate.LifecycleProposed:
		return awarenesspb.ArchitectureLifecycleState_ARCHITECTURE_LIFECYCLE_STATE_PROPOSED, nil
	case controlstate.LifecycleDeprecated:
		return awarenesspb.ArchitectureLifecycleState_ARCHITECTURE_LIFECYCLE_STATE_DEPRECATED, nil
	case controlstate.LifecycleSuperseded:
		return awarenesspb.ArchitectureLifecycleState_ARCHITECTURE_LIFECYCLE_STATE_SUPERSEDED, nil
	case controlstate.LifecycleRevoked:
		return awarenesspb.ArchitectureLifecycleState_ARCHITECTURE_LIFECYCLE_STATE_REVOKED, nil
	case controlstate.LifecycleUnknown:
		return awarenesspb.ArchitectureLifecycleState_ARCHITECTURE_LIFECYCLE_STATE_UNKNOWN, nil
	case controlstate.LifecycleNotApplicable:
		return awarenesspb.ArchitectureLifecycleState_ARCHITECTURE_LIFECYCLE_STATE_NOT_APPLICABLE, nil
	}
	return 0, fmt.Errorf("lifecycle state %q is outside the closed vocabulary", s)
}

func severityToProto(s controlstate.AttentionSeverity) (awarenesspb.ArchitectureAttentionSeverity, error) {
	switch s {
	case controlstate.SeverityInformational:
		return awarenesspb.ArchitectureAttentionSeverity_ARCHITECTURE_ATTENTION_SEVERITY_INFORMATIONAL, nil
	case controlstate.SeverityAttention:
		return awarenesspb.ArchitectureAttentionSeverity_ARCHITECTURE_ATTENTION_SEVERITY_ATTENTION, nil
	case controlstate.SeverityWarning:
		return awarenesspb.ArchitectureAttentionSeverity_ARCHITECTURE_ATTENTION_SEVERITY_WARNING, nil
	case controlstate.SeverityCritical:
		return awarenesspb.ArchitectureAttentionSeverity_ARCHITECTURE_ATTENTION_SEVERITY_CRITICAL, nil
	}
	return 0, fmt.Errorf("attention severity %q is outside the closed vocabulary", s)
}

func coverageToProto(c controlstate.AssessmentCoverage) (awarenesspb.ArchitectureAssessmentCoverage, error) {
	switch c {
	case controlstate.CoverageAssessable:
		return awarenesspb.ArchitectureAssessmentCoverage_ARCHITECTURE_ASSESSMENT_COVERAGE_ASSESSABLE, nil
	case controlstate.CoverageExplicitlyNotApplicable:
		return awarenesspb.ArchitectureAssessmentCoverage_ARCHITECTURE_ASSESSMENT_COVERAGE_EXPLICITLY_NOT_APPLICABLE, nil
	case controlstate.CoverageUnsupported:
		return awarenesspb.ArchitectureAssessmentCoverage_ARCHITECTURE_ASSESSMENT_COVERAGE_UNSUPPORTED, nil
	case controlstate.CoverageUnknown:
		return awarenesspb.ArchitectureAssessmentCoverage_ARCHITECTURE_ASSESSMENT_COVERAGE_UNKNOWN, nil
	}
	return 0, fmt.Errorf("assessment coverage %q is outside the closed vocabulary", c)
}

// ── Shared message mappings ──

func sourceStatusToProto(s controlstate.SourceStatus) (*awarenesspb.ArchitectureSourceStatus, error) {
	avail, err := sourceAvailabilityToProto(s.Availability)
	if err != nil {
		return nil, err
	}
	impact, err := sourceImpactToProto(s.Impact)
	if err != nil {
		return nil, err
	}
	return &awarenesspb.ArchitectureSourceStatus{
		Owner: s.Owner, Schema: s.Schema, Availability: avail, Impact: impact,
		ReasonCode: s.ReasonCode, Identity: s.Identity, Digest: s.Digest,
	}, nil
}

func metaToProto(m controlstate.ProjectionMeta) (*awarenesspb.ArchitectureProjectionMeta, error) {
	avail, err := availabilityToProto(m.Availability)
	if err != nil {
		return nil, err
	}
	out := &awarenesspb.ArchitectureProjectionMeta{
		SchemaVersion: m.SchemaVersion, ProducerName: m.ProducerName, ProducerVersion: m.ProducerVersion,
		RepositoryIdentity: m.RepositoryIdentity, RequestedDomain: m.RequestedDomain,
		Availability:               avail,
		NonAuthoritativeProjection: m.NonAuthoritativeProjection,
		Limitations:                append([]string(nil), m.Limitations...),
		// The canonical controlstate digest, copied verbatim — never recomputed here.
		DigestSha256: m.DigestSHA256,
	}
	for _, s := range m.Sources {
		ps, err := sourceStatusToProto(s)
		if err != nil {
			return nil, err
		}
		out.Sources = append(out.Sources, ps)
	}
	return out, nil
}

func identityToProto(id controlstate.ArtifactIdentity) *awarenesspb.ArchitectureArtifactIdentity {
	return &awarenesspb.ArchitectureArtifactIdentity{
		NodeIri: id.NodeIRI, CanonicalClass: id.CanonicalClass,
		ObservedClasses:    append([]string(nil), id.ObservedClasses...),
		RepositoryIdentity: id.RepositoryIdentity, DomainIdentity: id.DomainIdentity,
		GraphAuthorityIdentity: id.GraphAuthorityIdentity,
		ProvenanceIdentities:   append([]string(nil), id.ProvenanceIdentities...),
	}
}

// ToProtoAttentionItem maps one canonical attention item. The item is validated FIRST (identity
// digest, vocabulary, severity basis) — a malformed item is an error, never silently omitted.
func ToProtoAttentionItem(a controlstate.AttentionItem) (*awarenesspb.ArchitectureAttentionItem, error) {
	if err := controlstate.ValidateAttentionItem(a); err != nil {
		return nil, fmt.Errorf("invalid attention item: %w", err)
	}
	sev, err := severityToProto(a.Severity)
	if err != nil {
		return nil, err
	}
	return &awarenesspb.ArchitectureAttentionItem{
		Id: a.ID, SourceOwner: a.SourceOwner, SourceSchema: a.SourceSchema, SourceIdentity: a.SourceIdentity,
		AttentionClass: a.AttentionClass, ReasonCode: a.ReasonCode,
		Severity: sev, SeverityBasis: a.SeverityBasis, SourceDigest: a.SourceDigest,
		AffectedArtifacts: append([]string(nil), a.Affected...),
		Blocking:          a.Blocking,
		Evidence:          append([]string(nil), a.Evidence...),
		NextActionOwner:   a.NextAction, ArchitectInputRequired: a.ArchitectInput,
	}, nil
}

func keyedCountsToProto(in []controlstate.KeyedCount) []*awarenesspb.ArchitectureKeyedCount {
	if len(in) == 0 {
		return nil
	}
	out := make([]*awarenesspb.ArchitectureKeyedCount, 0, len(in))
	for _, kc := range in {
		out = append(out, &awarenesspb.ArchitectureKeyedCount{Key: kc.Key, Count: int64(kc.Count)})
	}
	return out
}

// optCount preserves unknown-versus-zero: a nil canonical count stays ABSENT on the wire; an
// observed zero stays an explicit zero.
func optCount(n *int) *int64 {
	if n == nil {
		return nil
	}
	v := int64(*n)
	return &v
}

// ── Projection mappings ──

// ToProtoControlSnapshot maps a validated architecture.control_snapshot/v1.
func ToProtoControlSnapshot(s controlstate.ControlSnapshot) (*awarenesspb.ArchitectureControlSnapshot, error) {
	if err := controlstate.ValidateControlSnapshot(s); err != nil {
		return nil, fmt.Errorf("canonical control snapshot failed validation: %w", err)
	}
	meta, err := metaToProto(s.ProjectionMeta)
	if err != nil {
		return nil, err
	}
	out := &awarenesspb.ArchitectureControlSnapshot{
		Meta:           meta,
		RegistryDigest: s.RegistryDigest,
		GraphAuthority: &awarenesspb.ArchitectureGraphAuthoritySummary{
			Observed: s.Authority.Observed, Current: s.Authority.Current,
			Integrity: s.Authority.Integrity, Identity: s.Authority.Identity,
		},
		CountsByClass:             keyedCountsToProto(s.CountsByClass),
		AssessmentCoverageCounts:  keyedCountsToProto(s.CoverageCounts),
		ClosureCounts:             keyedCountsToProto(s.ClosureCounts),
		LifecycleUnknownCount:     optCount(s.LifecycleUnknown),
		AttentionCountsBySeverity: keyedCountsToProto(s.AttentionCounts),
		OpenQuestionCount:         optCount(s.OpenQuestions),
		ContradictionCount:        optCount(s.Contradictions),
		MissingEvidenceCount:      optCount(s.MissingEvidence),
		MissingTestCount:          optCount(s.MissingTests),
		MissingEnforcementCount:   optCount(s.MissingEnforce),
	}
	for _, a := range s.TopAttention {
		pa, err := ToProtoAttentionItem(a)
		if err != nil {
			return nil, err
		}
		out.TopAttention = append(out.TopAttention, pa)
	}
	if s.Coverage != nil {
		out.Coverage = &awarenesspb.ArchitectureCoverageSummary{
			Sufficient:             s.Coverage.Sufficient,
			BlindSpotCount:         int64(s.Coverage.BlindSpotCount),
			HighRiskBlindSpotCount: int64(s.Coverage.HighRiskBlind),
		}
	}
	if s.ActiveTask != nil {
		out.ActiveTask = &awarenesspb.ArchitectureTaskSummary{
			TaskId: s.ActiveTask.TaskID, SessionId: s.ActiveTask.SessionID,
			Closure: s.ActiveTask.Closure, Admission: s.ActiveTask.Admission,
		}
	}
	if s.Completion != nil {
		out.Completion = &awarenesspb.ArchitectureCompletionSummary{
			TerminalState:           s.Completion.TerminalState,
			AuthoritativeCompletion: s.Completion.AuthoritativeCompletion,
		}
	}
	if s.FeedbackContext != nil {
		out.FeedbackContext = &awarenesspb.ArchitectureFeedbackContext{
			Capable: s.FeedbackContext.Capable, Availability: s.FeedbackContext.Availability,
		}
	}
	return out, nil
}

// ToProtoArtifactIndex maps a validated architecture.artifact_index/v1 page. reg and pageSize
// are the same validation inputs the canonical owner used to build the page.
func ToProtoArtifactIndex(reg controlstate.Registry, idx controlstate.ArtifactIndex, pageSize int) (*awarenesspb.ArchitectureArtifactIndex, error) {
	if err := controlstate.ValidateArtifactIndex(reg, idx, pageSize); err != nil {
		return nil, fmt.Errorf("canonical artifact index failed validation: %w", err)
	}
	meta, err := metaToProto(idx.ProjectionMeta)
	if err != nil {
		return nil, err
	}
	out := &awarenesspb.ArchitectureArtifactIndex{
		Meta:           meta,
		RegistryDigest: idx.RegistryDigest,
		// The cursor is OPAQUE: copied verbatim, never parsed or rewritten here.
		NextCursor: idx.NextCursor,
		Truncated:  idx.Truncated,
	}
	for _, row := range idx.Page {
		pr, err := artifactSummaryToProto(row)
		if err != nil {
			return nil, err
		}
		out.Page = append(out.Page, pr)
	}
	return out, nil
}

func artifactSummaryToProto(a controlstate.ArtifactSummary) (*awarenesspb.ArchitectureArtifactSummary, error) {
	cov, err := coverageToProto(a.Coverage)
	if err != nil {
		return nil, err
	}
	lc, err := lifecycleStateToProto(a.Lifecycle)
	if err != nil {
		return nil, err
	}
	cl, err := closureToProto(a.Closure)
	if err != nil {
		return nil, err
	}
	avail, err := availabilityToProto(a.Availability)
	if err != nil {
		return nil, err
	}
	out := &awarenesspb.ArchitectureArtifactSummary{
		Identity: identityToProto(a.Identity), Label: a.Label, Family: a.Family, Class: a.Class,
		AssessmentCoverage: cov, Lifecycle: lc, Closure: cl,
		OpenRequiredDimensions: int64(a.OpenRequiredDimensions),
		AttentionCount:         int64(a.AttentionCount),
		OwnerSummary:           a.OwnerSummary,
		Availability:           avail,
	}
	// An empty highest severity (zero attention) stays ABSENT on the wire — never UNSPECIFIED.
	if a.HighestSeverity != "" {
		sev, err := severityToProto(a.HighestSeverity)
		if err != nil {
			return nil, err
		}
		out.HighestSeverity = &sev
	}
	return out, nil
}

// ToProtoArtifactState maps a validated architecture.artifact_state/v1.
func ToProtoArtifactState(st controlstate.ArtifactState) (*awarenesspb.ArchitectureArtifactState, error) {
	if err := controlstate.ValidateArtifactState(st); err != nil {
		return nil, fmt.Errorf("canonical artifact state failed validation: %w", err)
	}
	meta, err := metaToProto(st.ProjectionMeta)
	if err != nil {
		return nil, err
	}
	cov, err := coverageToProto(st.Coverage)
	if err != nil {
		return nil, err
	}
	cl, err := closureToProto(st.Closure)
	if err != nil {
		return nil, err
	}
	lcState, err := lifecycleStateToProto(st.Lifecycle.State)
	if err != nil {
		return nil, err
	}
	lcAvail, err := sourceAvailabilityToProto(st.Lifecycle.SourceAvailability)
	if err != nil {
		return nil, err
	}
	out := &awarenesspb.ArchitectureArtifactState{
		Meta:     meta,
		Identity: identityToProto(st.Identity), CanonicalClass: st.CanonicalClass,
		AssessmentCoverage: cov, Closure: cl, ClosureReason: st.ClosureReason,
		Lifecycle: &awarenesspb.ArchitectureLifecycleAssessment{
			Applicable: st.Lifecycle.Applicable, Vocabulary: st.Lifecycle.Vocabulary,
			State: lcState, SourceOwner: st.Lifecycle.SourceOwner,
			SourceIdentity: st.Lifecycle.SourceIdentity, SourceAvailability: lcAvail,
			ReasonCode: st.Lifecycle.ReasonCode,
		},
		Questions:       append([]string(nil), st.Questions...),
		Evidence:        append([]string(nil), st.Evidence...),
		NextActionOwner: st.NextAction,
	}
	for _, d := range st.Dimensions {
		ds, err := dimensionStateToProto(d.State)
		if err != nil {
			return nil, err
		}
		pd := &awarenesspb.ArchitectureDimensionAssessment{
			Dimension: d.Dimension, Label: d.Label, Applicable: d.Applicable, Required: d.Required,
			State: ds, ReasonCode: d.ReasonCode,
			Blockers:  append([]string(nil), d.Blockers...),
			Evidence:  append([]string(nil), d.Evidence...),
			Questions: append([]string(nil), d.Questions...),
			Owner:     d.Owner, NextActionOwner: d.NextAction,
		}
		if d.Explanation != nil {
			pd.Explanation = &awarenesspb.ArchitectureDimensionExplanation{
				Kind: d.Explanation.Kind, Known: d.Explanation.Known, Missing: d.Explanation.Missing,
				WhyNotImprovable: d.Explanation.WhyNotImprovable, NextEvidence: d.Explanation.NextEvidence,
			}
		}
		out.Dimensions = append(out.Dimensions, pd)
	}
	for _, a := range st.Attention {
		pa, err := ToProtoAttentionItem(a)
		if err != nil {
			return nil, err
		}
		out.Attention = append(out.Attention, pa)
	}
	if st.Feedback != nil {
		out.Feedback = &awarenesspb.ArchitectureScopedFeedbackRef{
			ScopeIdentity: st.Feedback.ScopeIdentity, ProjectionDigest: st.Feedback.ProjectionDigest,
			Availability:      st.Feedback.Availability,
			VerifiedRecordIds: append([]string(nil), st.Feedback.VerifiedRecordIDs...),
			LineageIds:        append([]string(nil), st.Feedback.LineageIDs...),
			Limitations:       append([]string(nil), st.Feedback.Limitations...),
		}
	}
	return out, nil
}

// ToProtoNavigationDescriptor maps a validated ontology.navigation_descriptor/v1.
func ToProtoNavigationDescriptor(d controlstate.NavigationDescriptor) (*awarenesspb.OntologyNavigationDescriptor, error) {
	if err := controlstate.ValidateNavigationDescriptor(d); err != nil {
		return nil, fmt.Errorf("canonical navigation descriptor failed validation: %w", err)
	}
	meta, err := metaToProto(d.ProjectionMeta)
	if err != nil {
		return nil, err
	}
	fallback, err := navClassToProto(d.UnknownClassFallback)
	if err != nil {
		return nil, err
	}
	out := &awarenesspb.OntologyNavigationDescriptor{
		Meta:                 meta,
		RegistryDigest:       d.RegistryDigest,
		UnknownClassFallback: fallback,
	}
	for _, f := range d.Families {
		pf := &awarenesspb.ArchitectureNavigationFamily{Id: f.ID, Label: f.Label, Order: int32(f.Order)}
		for _, c := range f.Classes {
			pc, err := navClassToProto(c)
			if err != nil {
				return nil, err
			}
			pf.Classes = append(pf.Classes, pc)
		}
		out.Families = append(out.Families, pf)
	}
	return out, nil
}

func navClassToProto(c controlstate.NavigationClass) (*awarenesspb.ArchitectureNavigationClass, error) {
	cov, err := coverageToProto(c.Coverage)
	if err != nil {
		return nil, err
	}
	return &awarenesspb.ArchitectureNavigationClass{
		ClassIri: c.ClassIRI, Label: c.Label, Order: int32(c.Order), Coverage: cov,
		AssessableArtifact: c.AssessableArtifact,
		QueryCapable:       c.QueryCapable, ResolveCapable: c.ResolveCapable,
		InspectorCapable: c.InspectorCapable, QuestionCapable: c.QuestionCapable,
		DefaultVisible: c.DefaultVisible, OverviewVisible: c.OverviewVisible,
	}, nil
}
