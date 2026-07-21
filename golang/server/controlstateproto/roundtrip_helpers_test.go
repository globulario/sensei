// SPDX-License-Identifier: AGPL-3.0-only

package controlstateproto

// TEST-ONLY reverse mappers (proto → model). They exist to prove losslessness by round-trip;
// production consumes the canonical model directly and never reconstructs it from the wire
// (forbidden fix: phase9_5_reconstruct_models_from_labels_or_colors).

import (
	"fmt"

	"github.com/globulario/sensei/golang/architecture/controlstate"
	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func availabilityFromProto(a awarenesspb.ArchitectureAvailability) (controlstate.Availability, error) {
	switch a {
	case awarenesspb.ArchitectureAvailability_ARCHITECTURE_AVAILABILITY_AVAILABLE:
		return controlstate.AvailabilityAvailable, nil
	case awarenesspb.ArchitectureAvailability_ARCHITECTURE_AVAILABILITY_PARTIAL:
		return controlstate.AvailabilityPartial, nil
	case awarenesspb.ArchitectureAvailability_ARCHITECTURE_AVAILABILITY_UNAVAILABLE:
		return controlstate.AvailabilityUnavailable, nil
	case awarenesspb.ArchitectureAvailability_ARCHITECTURE_AVAILABILITY_INVALID:
		return controlstate.AvailabilityInvalid, nil
	}
	return "", fmt.Errorf("availability %v invalid on the wire (UNSPECIFIED never decodes)", a)
}

func sourceAvailabilityFromProto(a awarenesspb.ArchitectureSourceAvailability) (controlstate.SourceAvailability, error) {
	switch a {
	case awarenesspb.ArchitectureSourceAvailability_ARCHITECTURE_SOURCE_AVAILABILITY_AVAILABLE:
		return controlstate.SourceAvailable, nil
	case awarenesspb.ArchitectureSourceAvailability_ARCHITECTURE_SOURCE_AVAILABILITY_DEGRADED:
		return controlstate.SourceDegraded, nil
	case awarenesspb.ArchitectureSourceAvailability_ARCHITECTURE_SOURCE_AVAILABILITY_UNAVAILABLE:
		return controlstate.SourceUnavailable, nil
	case awarenesspb.ArchitectureSourceAvailability_ARCHITECTURE_SOURCE_AVAILABILITY_INVALID:
		return controlstate.SourceInvalid, nil
	}
	return "", fmt.Errorf("source availability %v invalid on the wire", a)
}

func sourceImpactFromProto(i awarenesspb.ArchitectureSourceImpact) (controlstate.SourceImpact, error) {
	switch i {
	case awarenesspb.ArchitectureSourceImpact_ARCHITECTURE_SOURCE_IMPACT_PRIMARY:
		return controlstate.ImpactPrimary, nil
	case awarenesspb.ArchitectureSourceImpact_ARCHITECTURE_SOURCE_IMPACT_REQUIRED:
		return controlstate.ImpactRequired, nil
	case awarenesspb.ArchitectureSourceImpact_ARCHITECTURE_SOURCE_IMPACT_RELEVANT:
		return controlstate.ImpactRelevant, nil
	case awarenesspb.ArchitectureSourceImpact_ARCHITECTURE_SOURCE_IMPACT_OPTIONAL:
		return controlstate.ImpactOptional, nil
	}
	return "", fmt.Errorf("source impact %v invalid on the wire", i)
}

func closureFromProto(c awarenesspb.ArchitectureArtifactClosure) (controlstate.ArtifactClosure, error) {
	switch c {
	case awarenesspb.ArchitectureArtifactClosure_ARCHITECTURE_ARTIFACT_CLOSURE_CLOSED:
		return controlstate.ClosureClosed, nil
	case awarenesspb.ArchitectureArtifactClosure_ARCHITECTURE_ARTIFACT_CLOSURE_OPEN:
		return controlstate.ClosureOpen, nil
	case awarenesspb.ArchitectureArtifactClosure_ARCHITECTURE_ARTIFACT_CLOSURE_DEGRADED:
		return controlstate.ClosureDegraded, nil
	case awarenesspb.ArchitectureArtifactClosure_ARCHITECTURE_ARTIFACT_CLOSURE_UNKNOWN:
		return controlstate.ClosureUnknown, nil
	case awarenesspb.ArchitectureArtifactClosure_ARCHITECTURE_ARTIFACT_CLOSURE_NOT_APPLICABLE:
		return controlstate.ClosureNotApplicable, nil
	}
	return "", fmt.Errorf("closure %v invalid on the wire (unknown is explicit, never UNSPECIFIED)", c)
}

func dimensionStateFromProto(s awarenesspb.ArchitectureDimensionState) (controlstate.DimensionState, error) {
	switch s {
	case awarenesspb.ArchitectureDimensionState_ARCHITECTURE_DIMENSION_STATE_SATISFIED:
		return controlstate.DimSatisfied, nil
	case awarenesspb.ArchitectureDimensionState_ARCHITECTURE_DIMENSION_STATE_OPEN:
		return controlstate.DimOpen, nil
	case awarenesspb.ArchitectureDimensionState_ARCHITECTURE_DIMENSION_STATE_DEGRADED:
		return controlstate.DimDegraded, nil
	case awarenesspb.ArchitectureDimensionState_ARCHITECTURE_DIMENSION_STATE_UNKNOWN:
		return controlstate.DimUnknown, nil
	case awarenesspb.ArchitectureDimensionState_ARCHITECTURE_DIMENSION_STATE_NOT_APPLICABLE:
		return controlstate.DimNotApplicable, nil
	}
	return "", fmt.Errorf("dimension state %v invalid on the wire", s)
}

func lifecycleStateFromProto(s awarenesspb.ArchitectureLifecycleState) (controlstate.LifecycleState, error) {
	switch s {
	case awarenesspb.ArchitectureLifecycleState_ARCHITECTURE_LIFECYCLE_STATE_ACTIVE:
		return controlstate.LifecycleActive, nil
	case awarenesspb.ArchitectureLifecycleState_ARCHITECTURE_LIFECYCLE_STATE_PROPOSED:
		return controlstate.LifecycleProposed, nil
	case awarenesspb.ArchitectureLifecycleState_ARCHITECTURE_LIFECYCLE_STATE_DEPRECATED:
		return controlstate.LifecycleDeprecated, nil
	case awarenesspb.ArchitectureLifecycleState_ARCHITECTURE_LIFECYCLE_STATE_SUPERSEDED:
		return controlstate.LifecycleSuperseded, nil
	case awarenesspb.ArchitectureLifecycleState_ARCHITECTURE_LIFECYCLE_STATE_REVOKED:
		return controlstate.LifecycleRevoked, nil
	case awarenesspb.ArchitectureLifecycleState_ARCHITECTURE_LIFECYCLE_STATE_UNKNOWN:
		return controlstate.LifecycleUnknown, nil
	case awarenesspb.ArchitectureLifecycleState_ARCHITECTURE_LIFECYCLE_STATE_NOT_APPLICABLE:
		return controlstate.LifecycleNotApplicable, nil
	}
	return "", fmt.Errorf("lifecycle state %v invalid on the wire", s)
}

func severityFromProto(s awarenesspb.ArchitectureAttentionSeverity) (controlstate.AttentionSeverity, error) {
	switch s {
	case awarenesspb.ArchitectureAttentionSeverity_ARCHITECTURE_ATTENTION_SEVERITY_INFORMATIONAL:
		return controlstate.SeverityInformational, nil
	case awarenesspb.ArchitectureAttentionSeverity_ARCHITECTURE_ATTENTION_SEVERITY_ATTENTION:
		return controlstate.SeverityAttention, nil
	case awarenesspb.ArchitectureAttentionSeverity_ARCHITECTURE_ATTENTION_SEVERITY_WARNING:
		return controlstate.SeverityWarning, nil
	case awarenesspb.ArchitectureAttentionSeverity_ARCHITECTURE_ATTENTION_SEVERITY_CRITICAL:
		return controlstate.SeverityCritical, nil
	}
	return "", fmt.Errorf("attention severity %v invalid on the wire", s)
}

func coverageFromProto(c awarenesspb.ArchitectureAssessmentCoverage) (controlstate.AssessmentCoverage, error) {
	switch c {
	case awarenesspb.ArchitectureAssessmentCoverage_ARCHITECTURE_ASSESSMENT_COVERAGE_ASSESSABLE:
		return controlstate.CoverageAssessable, nil
	case awarenesspb.ArchitectureAssessmentCoverage_ARCHITECTURE_ASSESSMENT_COVERAGE_EXPLICITLY_NOT_APPLICABLE:
		return controlstate.CoverageExplicitlyNotApplicable, nil
	case awarenesspb.ArchitectureAssessmentCoverage_ARCHITECTURE_ASSESSMENT_COVERAGE_UNSUPPORTED:
		return controlstate.CoverageUnsupported, nil
	case awarenesspb.ArchitectureAssessmentCoverage_ARCHITECTURE_ASSESSMENT_COVERAGE_UNKNOWN:
		return controlstate.CoverageUnknown, nil
	}
	return "", fmt.Errorf("assessment coverage %v invalid on the wire", c)
}

func sourceStatusFromProto(p *awarenesspb.ArchitectureSourceStatus) (controlstate.SourceStatus, error) {
	avail, err := sourceAvailabilityFromProto(p.GetAvailability())
	if err != nil {
		return controlstate.SourceStatus{}, err
	}
	impact, err := sourceImpactFromProto(p.GetImpact())
	if err != nil {
		return controlstate.SourceStatus{}, err
	}
	return controlstate.SourceStatus{
		Owner: p.GetOwner(), Schema: p.GetSchema(), Availability: avail, Impact: impact,
		ReasonCode: p.GetReasonCode(), Identity: p.GetIdentity(), Digest: p.GetDigest(),
	}, nil
}

func metaFromProto(p *awarenesspb.ArchitectureProjectionMeta) (controlstate.ProjectionMeta, error) {
	avail, err := availabilityFromProto(p.GetAvailability())
	if err != nil {
		return controlstate.ProjectionMeta{}, err
	}
	out := controlstate.ProjectionMeta{
		SchemaVersion: p.GetSchemaVersion(), ProducerName: p.GetProducerName(), ProducerVersion: p.GetProducerVersion(),
		RepositoryIdentity: p.GetRepositoryIdentity(), RequestedDomain: p.GetRequestedDomain(),
		Availability:               avail,
		NonAuthoritativeProjection: p.GetNonAuthoritativeProjection(),
		Limitations:                append([]string(nil), p.GetLimitations()...),
		DigestSHA256:               p.GetDigestSha256(),
	}
	for _, s := range p.GetSources() {
		ms, err := sourceStatusFromProto(s)
		if err != nil {
			return controlstate.ProjectionMeta{}, err
		}
		out.Sources = append(out.Sources, ms)
	}
	return out, nil
}

func identityFromProto(p *awarenesspb.ArchitectureArtifactIdentity) controlstate.ArtifactIdentity {
	return controlstate.ArtifactIdentity{
		NodeIRI: p.GetNodeIri(), CanonicalClass: p.GetCanonicalClass(),
		ObservedClasses:    append([]string(nil), p.GetObservedClasses()...),
		RepositoryIdentity: p.GetRepositoryIdentity(), DomainIdentity: p.GetDomainIdentity(),
		GraphAuthorityIdentity: p.GetGraphAuthorityIdentity(),
		ProvenanceIdentities:   append([]string(nil), p.GetProvenanceIdentities()...),
	}
}

func attentionFromProto(p *awarenesspb.ArchitectureAttentionItem) (controlstate.AttentionItem, error) {
	sev, err := severityFromProto(p.GetSeverity())
	if err != nil {
		return controlstate.AttentionItem{}, err
	}
	return controlstate.AttentionItem{
		ID: p.GetId(), SourceOwner: p.GetSourceOwner(), SourceSchema: p.GetSourceSchema(),
		SourceIdentity: p.GetSourceIdentity(), AttentionClass: p.GetAttentionClass(),
		ReasonCode: p.GetReasonCode(), Severity: sev, SeverityBasis: p.GetSeverityBasis(),
		SourceDigest: p.GetSourceDigest(),
		Affected:     append([]string(nil), p.GetAffectedArtifacts()...),
		Blocking:     p.GetBlocking(),
		Evidence:     append([]string(nil), p.GetEvidence()...),
		NextAction:   p.GetNextActionOwner(), ArchitectInput: p.GetArchitectInputRequired(),
	}, nil
}

func keyedCountsFromProto(in []*awarenesspb.ArchitectureKeyedCount) []controlstate.KeyedCount {
	if len(in) == 0 {
		return nil
	}
	out := make([]controlstate.KeyedCount, 0, len(in))
	for _, kc := range in {
		out = append(out, controlstate.KeyedCount{Key: kc.GetKey(), Count: int(kc.GetCount())})
	}
	return out
}

func countFromProto(p *int64) *int {
	if p == nil {
		return nil
	}
	v := int(*p)
	return &v
}

func fromProtoControlSnapshot(p *awarenesspb.ArchitectureControlSnapshot) (controlstate.ControlSnapshot, error) {
	meta, err := metaFromProto(p.GetMeta())
	if err != nil {
		return controlstate.ControlSnapshot{}, err
	}
	out := controlstate.ControlSnapshot{
		ProjectionMeta: meta,
		RegistryDigest: p.GetRegistryDigest(),
		Authority: controlstate.GraphAuthoritySummary{
			Observed: p.GetGraphAuthority().GetObserved(), Current: p.GetGraphAuthority().GetCurrent(),
			Integrity: p.GetGraphAuthority().GetIntegrity(), Identity: p.GetGraphAuthority().GetIdentity(),
		},
		CountsByClass:    keyedCountsFromProto(p.GetCountsByClass()),
		CoverageCounts:   keyedCountsFromProto(p.GetAssessmentCoverageCounts()),
		ClosureCounts:    keyedCountsFromProto(p.GetClosureCounts()),
		LifecycleUnknown: countFromProto(p.LifecycleUnknownCount),
		AttentionCounts:  keyedCountsFromProto(p.GetAttentionCountsBySeverity()),
		OpenQuestions:    countFromProto(p.OpenQuestionCount),
		Contradictions:   countFromProto(p.ContradictionCount),
		MissingEvidence:  countFromProto(p.MissingEvidenceCount),
		MissingTests:     countFromProto(p.MissingTestCount),
		MissingEnforce:   countFromProto(p.MissingEnforcementCount),
	}
	for _, a := range p.GetTopAttention() {
		ma, err := attentionFromProto(a)
		if err != nil {
			return controlstate.ControlSnapshot{}, err
		}
		out.TopAttention = append(out.TopAttention, ma)
	}
	if p.GetCoverage() != nil {
		out.Coverage = &controlstate.CoverageSummary{
			Sufficient:     p.GetCoverage().GetSufficient(),
			BlindSpotCount: int(p.GetCoverage().GetBlindSpotCount()),
			HighRiskBlind:  int(p.GetCoverage().GetHighRiskBlindSpotCount()),
		}
	}
	if p.GetActiveTask() != nil {
		out.ActiveTask = &controlstate.TaskSummary{
			TaskID: p.GetActiveTask().GetTaskId(), SessionID: p.GetActiveTask().GetSessionId(),
			Closure: p.GetActiveTask().GetClosure(), Admission: p.GetActiveTask().GetAdmission(),
		}
	}
	if p.GetCompletion() != nil {
		out.Completion = &controlstate.CompletionSummary{
			TerminalState:           p.GetCompletion().GetTerminalState(),
			AuthoritativeCompletion: p.GetCompletion().GetAuthoritativeCompletion(),
		}
	}
	if p.GetFeedbackContext() != nil {
		out.FeedbackContext = &controlstate.FeedbackContext{
			Capable: p.GetFeedbackContext().GetCapable(), Availability: p.GetFeedbackContext().GetAvailability(),
		}
	}
	return out, nil
}

func fromProtoArtifactState(p *awarenesspb.ArchitectureArtifactState) (controlstate.ArtifactState, error) {
	meta, err := metaFromProto(p.GetMeta())
	if err != nil {
		return controlstate.ArtifactState{}, err
	}
	cov, err := coverageFromProto(p.GetAssessmentCoverage())
	if err != nil {
		return controlstate.ArtifactState{}, err
	}
	cl, err := closureFromProto(p.GetClosure())
	if err != nil {
		return controlstate.ArtifactState{}, err
	}
	lcState, err := lifecycleStateFromProto(p.GetLifecycle().GetState())
	if err != nil {
		return controlstate.ArtifactState{}, err
	}
	lcAvail, err := sourceAvailabilityFromProto(p.GetLifecycle().GetSourceAvailability())
	if err != nil {
		return controlstate.ArtifactState{}, err
	}
	out := controlstate.ArtifactState{
		ProjectionMeta: meta,
		Identity:       identityFromProto(p.GetIdentity()),
		CanonicalClass: p.GetCanonicalClass(),
		Coverage:       cov, Closure: cl, ClosureReason: p.GetClosureReason(),
		Lifecycle: controlstate.LifecycleAssessment{
			Applicable: p.GetLifecycle().GetApplicable(), Vocabulary: p.GetLifecycle().GetVocabulary(),
			State: lcState, SourceOwner: p.GetLifecycle().GetSourceOwner(),
			SourceIdentity: p.GetLifecycle().GetSourceIdentity(), SourceAvailability: lcAvail,
			ReasonCode: p.GetLifecycle().GetReasonCode(),
		},
		Questions:  append([]string(nil), p.GetQuestions()...),
		Evidence:   append([]string(nil), p.GetEvidence()...),
		NextAction: p.GetNextActionOwner(),
	}
	for _, d := range p.GetDimensions() {
		ds, err := dimensionStateFromProto(d.GetState())
		if err != nil {
			return controlstate.ArtifactState{}, err
		}
		out.Dimensions = append(out.Dimensions, controlstate.DimensionAssessment{
			Dimension: d.GetDimension(), Label: d.GetLabel(), Applicable: d.GetApplicable(),
			Required: d.GetRequired(), State: ds, ReasonCode: d.GetReasonCode(),
			Blockers:  append([]string(nil), d.GetBlockers()...),
			Evidence:  append([]string(nil), d.GetEvidence()...),
			Questions: append([]string(nil), d.GetQuestions()...),
			Owner:     d.GetOwner(), NextAction: d.GetNextActionOwner(),
		})
	}
	for _, a := range p.GetAttention() {
		ma, err := attentionFromProto(a)
		if err != nil {
			return controlstate.ArtifactState{}, err
		}
		out.Attention = append(out.Attention, ma)
	}
	if p.GetFeedback() != nil {
		out.Feedback = &controlstate.ScopedFeedbackRef{
			ScopeIdentity: p.GetFeedback().GetScopeIdentity(), ProjectionDigest: p.GetFeedback().GetProjectionDigest(),
			Availability:      p.GetFeedback().GetAvailability(),
			VerifiedRecordIDs: append([]string(nil), p.GetFeedback().GetVerifiedRecordIds()...),
			LineageIDs:        append([]string(nil), p.GetFeedback().GetLineageIds()...),
			Limitations:       append([]string(nil), p.GetFeedback().GetLimitations()...),
		}
	}
	return out, nil
}
