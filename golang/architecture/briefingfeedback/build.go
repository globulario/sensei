// SPDX-License-Identifier: AGPL-3.0-only

package briefingfeedback

import (
	"context"
	"strings"

	qp "github.com/globulario/sensei/golang/architecture/questionpromotion"
)

// deps are the promotion-owner dependencies, injectable for tests. Production always uses
// the questionpromotion boundary; the owner never reads promotion internals directly.
type deps struct {
	discover   func(repoRoot string) ([]string, error)
	descriptor func(repoRoot, lineageID string) qp.CandidateDescriptor
	verify     func(ctx context.Context, repoRoot, lineageID string) (qp.VerifiedPromotion, error)
}

func prodDeps() deps {
	return deps{
		discover:   qp.DiscoverCommittedPromotions,
		descriptor: qp.LoadCandidateDescriptor,
		verify:     qp.VerifyCommittedPromotion,
	}
}

// Build produces the canonical, deterministic, read-only feedback projection for a scope. It
// mutates nothing. A malformed request yields a valid feedback_invalid projection; a
// discovery/verification-facility outage yields feedback_unavailable; normal outcomes yield a
// valid projection carrying availability + records + typed findings. It returns a Go error
// only for an impossible internal state (digest/validation failure), never a bare zero value.
func Build(ctx context.Context, req Request) (Projection, error) {
	return build(ctx, req, prodDeps())
}

func build(ctx context.Context, req Request, d deps) (Projection, error) {
	files, taskFiles, invalidReason := validateRequest(req)
	base := func(a Availability, findings []Finding) (Projection, error) {
		return finalize(req, files, nil, findings, a)
	}
	if invalidReason != "" {
		return base(FeedbackInvalid, []Finding{{Class: PromotionScopeIdentityInvalid, ReasonCode: invalidReason, Disposition: DispositionExcluded, AffectedDomain: req.RequestedDomain}})
	}

	lineages, err := d.discover(req.RepositoryRoot)
	if err != nil {
		return base(FeedbackUnavailable, []Finding{{Class: PromotionDiscoveryUnavailable, ReasonCode: "discovery_unavailable", Disposition: DispositionUnavailable}})
	}

	var records []VerifiedRecord
	var findings []Finding
	recByLineage := map[string]bool{}
	descByLineage := map[string]qp.CandidateDescriptor{}
	contradictory := false

	for _, lin := range lineages {
		desc := d.descriptor(req.RepositoryRoot, lin)
		if prev, dup := descByLineage[lin]; dup {
			// A duplicate lineage with a conflicting untrusted descriptor is contradictory
			// ONLY when relevant to the requested scope; unrelated duplicates are ignored.
			if !sameDescriptor(prev, desc) && (relevantFailure(prev, req, files, taskFiles) || relevantFailure(desc, req, files, taskFiles)) {
				contradictory = true
				findings = append(findings, Finding{Class: PromotionContradictory, ReasonCode: "conflicting_candidate_descriptors", LineageID: lin, ClaimedDomain: desc.ClaimedDomain, ClaimedFiles: desc.ClaimedFiles, AffectedDomain: req.RequestedDomain, AffectedFiles: files, Disposition: DispositionExcluded})
			}
			continue // dedup: never verify/render the same lineage twice
		}
		descByLineage[lin] = desc

		vp, verr := d.verify(ctx, req.RepositoryRoot, lin)
		if verr != nil {
			class, reason := classifyFailure(verr)
			// Only a RELEVANT failure (its untrusted claim plausibly binds to the requested
			// scope) may appear as a scoped finding; unrelated debris is dropped.
			if relevantFailure(desc, req, files, taskFiles) {
				findings = append(findings, Finding{Class: class, ReasonCode: reason, LineageID: lin, ClaimedDomain: desc.ClaimedDomain, ClaimedFiles: desc.ClaimedFiles, AffectedDomain: req.RequestedDomain, AffectedFiles: files, Disposition: DispositionExcluded})
			}
			continue
		}
		// A second verified record for the same lineage is contradictory (defensive; real
		// discovery dedups). Only relevant contradictions invalidate.
		if recByLineage[vp.PromotionLineageID] {
			contradictory = true
			findings = append(findings, Finding{Class: PromotionContradictory, ReasonCode: "duplicate_verified_lineage", LineageID: vp.PromotionLineageID, Disposition: DispositionExcluded})
			continue
		}
		if !admit(vp.Receipt, req, files, taskFiles) {
			continue // verified but out of the requested scope — silently excluded, no debris
		}
		recByLineage[vp.PromotionLineageID] = true
		records = append(records, recordFromVerified(vp))
	}

	return finalize(req, files, records, findings, deriveAvailability(records, findings, contradictory))
}

// finalize sorts, stamps, and self-validates the projection.
func finalize(req Request, files []string, records []VerifiedRecord, findings []Finding, avail Availability) (Projection, error) {
	sortRecords(records)
	sortFindings(findings)
	p := Projection{
		SchemaVersion:              SchemaVersion,
		ProducerName:               ProducerName,
		ProducerVersion:            ProducerVersion,
		RepositoryIdentity:         req.RepositoryIdentity,
		RequestedDomain:            req.RequestedDomain,
		RequestedFiles:             files,
		Availability:               avail,
		Records:                    records,
		Findings:                   findings,
		NonAuthoritativeProjection: true,
		Bound:                      BoundStatement,
	}
	if req.Task != nil {
		p.TaskID = req.Task.TaskID
		p.SessionID = req.Task.SessionID
	}
	dig, err := ComputeDigest(p)
	if err != nil {
		return Projection{}, err
	}
	p.DigestSHA256 = dig
	if err := ValidateProjection(p); err != nil {
		return Projection{}, err
	}
	return p, nil
}

func recordFromVerified(vp qp.VerifiedPromotion) VerifiedRecord {
	rc := vp.Receipt
	return VerifiedRecord{
		GovernedNodeIRI:                vp.GovernedNodeIRI,
		GovernedKind:                   rc.GovernedTargetKind,
		CanonicalRecordID:              rc.CanonicalRecordID,
		SourceDocument:                 rc.SourceDocument,
		PromotionLineageID:             vp.PromotionLineageID,
		PromotionReceiptDigestSHA256:   rc.ReceiptDigestSHA256,
		QuestionID:                     rc.QuestionID,
		AnswerID:                       rc.AnswerID,
		DispositionReceiptDigestSHA256: rc.QuestionDispositionReceiptDigestSHA256,
		OriginatingTaskID:              rc.Task.ID,
		OriginatingSessionID:           rc.Task.SessionID,
		EffectiveDomain:                rc.EffectiveScopeDomain,
		EffectiveFileScope:             sortedUnique(rc.EffectiveScopeFiles),
		VerificationClass:              PromotionVerified,
		ProvenanceInterpretation:       provenanceInterpretation,
	}
}

// classifyFailure maps a TYPED questionpromotion verification cause to a finding class +
// reason. An untyped/unknown error fails closed as unknown classification (visible, never
// admission). It never parses error text.
func classifyFailure(err error) (FindingClass, string) {
	ve, ok := qp.AsVerificationError(err)
	if !ok {
		return PromotionUnknownClassification, "unknown_verification_outcome"
	}
	switch ve.Class {
	case qp.VerifyIncomplete:
		return PromotionIncomplete, ve.ReasonCode
	case qp.VerifyIntegrityFailure:
		return PromotionIntegrityFailure, ve.ReasonCode
	case qp.VerifyStale:
		return PromotionStale, ve.ReasonCode
	case qp.VerifyUnverifiable:
		return PromotionUnverifiable, ve.ReasonCode
	default:
		return PromotionUnknownClassification, "unknown_verification_class"
	}
}

// deriveAvailability applies the frozen precedence: invalid > unavailable > degraded >
// available > empty. Out-of-scope candidates never appear as defects and never degrade.
func deriveAvailability(records []VerifiedRecord, findings []Finding, contradictory bool) Availability {
	if contradictory {
		return FeedbackInvalid
	}
	unavailable := false
	relevantDefect := false
	for _, f := range findings {
		switch f.Class {
		case PromotionUnknownClassification, PromotionContradictory, PromotionScopeIdentityInvalid:
			return FeedbackInvalid
		case PromotionDiscoveryUnavailable:
			unavailable = true
		case PromotionIncomplete, PromotionIntegrityFailure, PromotionStale, PromotionUnverifiable:
			relevantDefect = true
		}
	}
	switch {
	case unavailable:
		return FeedbackUnavailable
	case relevantDefect:
		return FeedbackDegraded
	case len(records) > 0:
		return FeedbackAvailable
	default:
		return FeedbackEmpty
	}
}

func sameDescriptor(a, b qp.CandidateDescriptor) bool {
	return a.Readable == b.Readable && a.ClaimedDomain == b.ClaimedDomain &&
		a.ClaimedTaskID == b.ClaimedTaskID && a.ClaimedSessionID == b.ClaimedSessionID &&
		strings.Join(a.ClaimedFiles, "\x00") == strings.Join(b.ClaimedFiles, "\x00")
}
