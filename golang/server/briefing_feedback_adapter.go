// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=server.briefing
// @awareness file_role=projection_to_wire_adapter
// @awareness implements=globular.awareness_graph:invariant.closure.briefing_feedback_wire_is_additive_typed_and_projection_derived
package main

import (
	"fmt"

	"github.com/globulario/sensei/golang/architecture/briefingfeedback"
	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// briefingFeedbackToProto is the PURE adapter from the canonical
// briefing.feedback_projection/v1 to its additive typed wire message. It validates the
// projection first, maps every closed enum EXPLICITLY (an unknown enum or impossible state is
// an internal adapter failure — never a partial projection), preserves ordering and every
// identity exactly, preserves the canonical digest, never recomputes availability, never
// inspects error text, never mutates the source, and never places a filesystem path on the
// wire that is not already a public projection field.
func briefingFeedbackToProto(p briefingfeedback.Projection) (*awarenesspb.BriefingFeedbackProjection, error) {
	if err := briefingfeedback.ValidateProjection(p); err != nil {
		return nil, fmt.Errorf("briefing feedback projection is not canonical: %w", err)
	}
	avail, err := feedbackAvailabilityToProto(p.Availability)
	if err != nil {
		return nil, err
	}
	out := &awarenesspb.BriefingFeedbackProjection{
		SchemaVersion:              p.SchemaVersion,
		ProducerName:               p.ProducerName,
		ProducerVersion:            p.ProducerVersion,
		RepositoryIdentity:         p.RepositoryIdentity,
		RequestedDomain:            p.RequestedDomain,
		RequestedFiles:             append([]string(nil), p.RequestedFiles...),
		TaskId:                     p.TaskID,
		SessionId:                  p.SessionID,
		Availability:               avail,
		NonAuthoritativeProjection: p.NonAuthoritativeProjection,
		Bound:                      p.Bound,
		DigestSha256:               p.DigestSHA256,
	}
	for _, r := range p.Records {
		cls, err := feedbackFindingClassToProto(r.VerificationClass)
		if err != nil {
			return nil, err
		}
		out.Records = append(out.Records, &awarenesspb.BriefingFeedbackVerifiedRecord{
			GovernedNodeIri:                r.GovernedNodeIRI,
			GovernedKind:                   r.GovernedKind,
			CanonicalRecordId:              r.CanonicalRecordID,
			SourceDocument:                 r.SourceDocument,
			PromotionLineageId:             r.PromotionLineageID,
			PromotionReceiptDigestSha256:   r.PromotionReceiptDigestSHA256,
			QuestionId:                     r.QuestionID,
			AnswerId:                       r.AnswerID,
			DispositionReceiptDigestSha256: r.DispositionReceiptDigestSHA256,
			OriginatingTaskId:              r.OriginatingTaskID,
			OriginatingSessionId:           r.OriginatingSessionID,
			EffectiveDomain:                r.EffectiveDomain,
			EffectiveFileScope:             append([]string(nil), r.EffectiveFileScope...),
			VerificationClass:              cls,
			ProvenanceInterpretation:       r.ProvenanceInterpretation,
		})
	}
	for _, f := range p.Findings {
		cls, err := feedbackFindingClassToProto(f.Class)
		if err != nil {
			return nil, err
		}
		disp, err := feedbackDispositionToProto(f.Disposition)
		if err != nil {
			return nil, err
		}
		out.Findings = append(out.Findings, &awarenesspb.BriefingFeedbackFinding{
			Class:          cls,
			ReasonCode:     f.ReasonCode,
			LineageId:      f.LineageID,
			ClaimedDomain:  f.ClaimedDomain,
			ClaimedFiles:   append([]string(nil), f.ClaimedFiles...),
			AffectedDomain: f.AffectedDomain,
			AffectedFiles:  append([]string(nil), f.AffectedFiles...),
			Disposition:    disp,
			Detail:         f.Detail,
		})
	}
	return out, nil
}

func feedbackAvailabilityToProto(a briefingfeedback.Availability) (awarenesspb.BriefingFeedbackAvailability, error) {
	switch a {
	case briefingfeedback.FeedbackAvailable:
		return awarenesspb.BriefingFeedbackAvailability_BRIEFING_FEEDBACK_AVAILABILITY_AVAILABLE, nil
	case briefingfeedback.FeedbackEmpty:
		return awarenesspb.BriefingFeedbackAvailability_BRIEFING_FEEDBACK_AVAILABILITY_EMPTY, nil
	case briefingfeedback.FeedbackDegraded:
		return awarenesspb.BriefingFeedbackAvailability_BRIEFING_FEEDBACK_AVAILABILITY_DEGRADED, nil
	case briefingfeedback.FeedbackUnavailable:
		return awarenesspb.BriefingFeedbackAvailability_BRIEFING_FEEDBACK_AVAILABILITY_UNAVAILABLE, nil
	case briefingfeedback.FeedbackInvalid:
		return awarenesspb.BriefingFeedbackAvailability_BRIEFING_FEEDBACK_AVAILABILITY_INVALID, nil
	}
	return 0, fmt.Errorf("unknown feedback availability %q", a)
}

func feedbackFindingClassToProto(c briefingfeedback.FindingClass) (awarenesspb.BriefingFeedbackFindingClass, error) {
	switch c {
	case briefingfeedback.PromotionVerified:
		return awarenesspb.BriefingFeedbackFindingClass_BRIEFING_FEEDBACK_FINDING_CLASS_PROMOTION_VERIFIED, nil
	case briefingfeedback.PromotionOutOfScope:
		return awarenesspb.BriefingFeedbackFindingClass_BRIEFING_FEEDBACK_FINDING_CLASS_PROMOTION_OUT_OF_SCOPE, nil
	case briefingfeedback.PromotionIncomplete:
		return awarenesspb.BriefingFeedbackFindingClass_BRIEFING_FEEDBACK_FINDING_CLASS_PROMOTION_INCOMPLETE, nil
	case briefingfeedback.PromotionIntegrityFailure:
		return awarenesspb.BriefingFeedbackFindingClass_BRIEFING_FEEDBACK_FINDING_CLASS_PROMOTION_INTEGRITY_FAILURE, nil
	case briefingfeedback.PromotionContradictory:
		return awarenesspb.BriefingFeedbackFindingClass_BRIEFING_FEEDBACK_FINDING_CLASS_PROMOTION_CONTRADICTORY, nil
	case briefingfeedback.PromotionStale:
		return awarenesspb.BriefingFeedbackFindingClass_BRIEFING_FEEDBACK_FINDING_CLASS_PROMOTION_STALE, nil
	case briefingfeedback.PromotionUnverifiable:
		return awarenesspb.BriefingFeedbackFindingClass_BRIEFING_FEEDBACK_FINDING_CLASS_PROMOTION_UNVERIFIABLE, nil
	case briefingfeedback.PromotionDiscoveryUnavailable:
		return awarenesspb.BriefingFeedbackFindingClass_BRIEFING_FEEDBACK_FINDING_CLASS_PROMOTION_DISCOVERY_UNAVAILABLE, nil
	case briefingfeedback.PromotionScopeIdentityInvalid:
		return awarenesspb.BriefingFeedbackFindingClass_BRIEFING_FEEDBACK_FINDING_CLASS_PROMOTION_SCOPE_IDENTITY_INVALID, nil
	case briefingfeedback.PromotionUnknownClassification:
		return awarenesspb.BriefingFeedbackFindingClass_BRIEFING_FEEDBACK_FINDING_CLASS_PROMOTION_UNKNOWN_CLASSIFICATION, nil
	}
	return 0, fmt.Errorf("unknown feedback finding class %q", c)
}

func feedbackDispositionToProto(d briefingfeedback.Disposition) (awarenesspb.BriefingFeedbackDisposition, error) {
	switch d {
	case briefingfeedback.DispositionAdmitted:
		return awarenesspb.BriefingFeedbackDisposition_BRIEFING_FEEDBACK_DISPOSITION_ADMITTED, nil
	case briefingfeedback.DispositionExcluded:
		return awarenesspb.BriefingFeedbackDisposition_BRIEFING_FEEDBACK_DISPOSITION_EXCLUDED, nil
	case briefingfeedback.DispositionUnavailable:
		return awarenesspb.BriefingFeedbackDisposition_BRIEFING_FEEDBACK_DISPOSITION_UNAVAILABLE, nil
	}
	return 0, fmt.Errorf("unknown feedback disposition %q", d)
}
