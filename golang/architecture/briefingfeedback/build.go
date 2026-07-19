// SPDX-License-Identifier: AGPL-3.0-only

package briefingfeedback

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	qp "github.com/globulario/sensei/golang/architecture/questionpromotion"
)

// deps are the promotion-owner dependencies, injectable for tests. Production always uses the
// questionpromotion boundary; the owner never reads promotion internals directly. resolveRoot
// establishes the single canonical repository root shared by discovery, descriptor loading,
// and verification.
type deps struct {
	resolveRoot func(root string) (string, error)
	discover    func(repoRoot string) ([]string, error)
	descriptor  func(repoRoot, lineageID string) qp.CandidateDescriptor
	verify      func(ctx context.Context, repoRoot, lineageID string) (qp.VerifiedPromotion, error)
}

func prodDeps() deps {
	return deps{
		resolveRoot: resolveRepoRoot,
		discover:    qp.DiscoverCommittedPromotions,
		descriptor:  qp.LoadCandidateDescriptor,
		verify:      qp.VerifyCommittedPromotion,
	}
}

// resolveRepoRoot establishes the single canonical repository root. The input is already
// validated absolute and unpadded by validateRequest; this resolves symlinks ONCE (which also
// verifies existence) and requires a directory. It never calls filepath.Abs — authority must
// not be derived from the process working directory.
func resolveRepoRoot(root string) (string, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repository root %q is not a directory", root)
	}
	return resolved, nil
}

// Build produces the canonical, deterministic, read-only feedback projection for a scope. It
// mutates nothing. A malformed/incoherent request yields a valid feedback_invalid projection; a
// discovery/verification-facility outage yields feedback_unavailable; normal outcomes yield a
// valid projection carrying availability + records + typed findings. It returns a Go error only
// for an impossible internal state (digest/validation failure), never a bare zero value.
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

	// Establish the single canonical repository root once and share it across every read.
	repoRoot, rerr := d.resolveRoot(req.RepositoryRoot)
	if rerr != nil {
		return base(FeedbackInvalid, []Finding{{Class: PromotionScopeIdentityInvalid, ReasonCode: "repository_root_unresolvable", Disposition: DispositionExcluded, AffectedDomain: req.RequestedDomain}})
	}

	lineages, err := d.discover(repoRoot)
	if err != nil {
		return base(FeedbackUnavailable, []Finding{{Class: PromotionDiscoveryUnavailable, ReasonCode: "discovery_unavailable", Disposition: DispositionUnavailable}})
	}

	var records []VerifiedRecord
	var findings []Finding
	recByLineage := map[string]bool{}
	descByLineage := map[string]qp.CandidateDescriptor{}
	facilitySeen := false

	for _, lin := range lineages {
		desc := d.descriptor(repoRoot, lin)
		if prev, dup := descByLineage[lin]; dup {
			// A duplicate lineage with a conflicting untrusted descriptor is contradictory
			// ONLY when relevant to the requested scope; unrelated duplicates are ignored.
			if !sameDescriptor(prev, desc) && (relevantFailure(prev, req, files, taskFiles) || relevantFailure(desc, req, files, taskFiles)) {
				findings = append(findings, Finding{Class: PromotionContradictory, ReasonCode: "conflicting_candidate_descriptors", LineageID: lin, ClaimedDomain: desc.ClaimedDomain, ClaimedFiles: desc.ClaimedFiles, AffectedDomain: req.RequestedDomain, AffectedFiles: files, Disposition: DispositionExcluded})
			}
			continue // dedup: never verify/render the same lineage twice
		}
		descByLineage[lin] = desc

		vp, verr := d.verify(ctx, repoRoot, lin)
		if verr != nil {
			class, reason, disp, facility := classifyFailure(verr)
			if facility {
				// A shared verification-facility outage is GLOBAL, not candidate-specific: it is
				// never relevance-filtered and is reported exactly once (discovery-order
				// independent), yielding feedback_unavailable.
				if !facilitySeen {
					facilitySeen = true
					findings = append(findings, Finding{Class: class, ReasonCode: reason, Disposition: disp, AffectedDomain: req.RequestedDomain})
				}
				continue
			}
			// Only a RELEVANT candidate-local failure (its untrusted claim plausibly binds to the
			// requested scope) may appear as a scoped finding; unrelated debris is dropped.
			if relevantFailure(desc, req, files, taskFiles) {
				findings = append(findings, Finding{Class: class, ReasonCode: reason, LineageID: lin, ClaimedDomain: desc.ClaimedDomain, ClaimedFiles: desc.ClaimedFiles, AffectedDomain: req.RequestedDomain, AffectedFiles: files, Disposition: disp})
			}
			continue
		}
		// A second verified record for the same lineage is contradictory (defensive; real
		// discovery dedups). Only relevant contradictions invalidate.
		if recByLineage[vp.PromotionLineageID] {
			findings = append(findings, Finding{Class: PromotionContradictory, ReasonCode: "duplicate_verified_lineage", LineageID: vp.PromotionLineageID, AffectedDomain: req.RequestedDomain, Disposition: DispositionExcluded})
			continue
		}
		if !admit(vp.Receipt, req, files, taskFiles) {
			continue // verified but out of the requested scope — silently excluded, no debris
		}
		recByLineage[vp.PromotionLineageID] = true
		records = append(records, recordFromVerified(vp))
	}

	return finalize(req, files, records, findings, deriveAvailability(records, findings))
}

// finalize sorts, deduplicates findings by their identity tuple, stamps, and self-validates. A
// feedback_invalid projection is a SANITIZED fail-closed carrier: it never echoes a malformed
// identity into a validated field and is never task-bound (the request was refused). Its raw
// diagnostic survives in the finding's reason code + (uncanonicalized) affected-scope fields.
func finalize(req Request, files []string, records []VerifiedRecord, findings []Finding, avail Availability) (Projection, error) {
	findings = dedupeFindings(findings)
	sortRecords(records)
	sortFindings(findings)
	p := Projection{
		SchemaVersion:              SchemaVersion,
		ProducerName:               ProducerName,
		ProducerVersion:            ProducerVersion,
		RequestedFiles:             files,
		Availability:               avail,
		Records:                    records,
		Findings:                   findings,
		NonAuthoritativeProjection: true,
		Bound:                      BoundStatement,
	}
	if avail == FeedbackInvalid {
		p.RepositoryIdentity = canonicalOrBlank(req.RepositoryIdentity)
		p.RequestedDomain = canonicalDomainOrBlank(req.RequestedDomain, p.RepositoryIdentity)
	} else {
		p.RepositoryIdentity = req.RepositoryIdentity
		p.RequestedDomain = req.RequestedDomain
		if req.Task != nil {
			p.TaskID = req.Task.TaskID
			p.SessionID = req.Task.SessionID
		}
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

// canonicalOrBlank returns s only when it is a non-empty canonical identity, else "".
func canonicalOrBlank(s string) string {
	if s != "" && !hasWhitespace(s) {
		return s
	}
	return ""
}

// canonicalDomainOrBlank returns d only when it is canonical AND exactly equal to the
// (already-sanitized) repository identity, else "" — never a repaired or incoherent domain.
func canonicalDomainOrBlank(d, identity string) string {
	if d != "" && !hasWhitespace(d) && d == identity {
		return d
	}
	return ""
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

// classifyFailure maps a TYPED questionpromotion verification cause + IMPACT to a finding
// class, reason, and disposition. A facility-unavailable impact yields a typed UNAVAILABLE
// finding (distinct from candidate invalidity); a candidate-local cause yields an EXCLUDED
// defect. An untyped/unknown error fails closed as unknown classification. It never parses
// error text and never matches reason-code strings.
func classifyFailure(err error) (class FindingClass, reason string, disp Disposition, facility bool) {
	ve, ok := qp.AsVerificationError(err)
	if !ok {
		return PromotionUnknownClassification, "unknown_verification_outcome", DispositionExcluded, false
	}
	if ve.Impact == qp.VerificationFacilityUnavailable {
		return PromotionUnverifiable, ve.ReasonCode, DispositionUnavailable, true
	}
	switch ve.Class {
	case qp.VerifyIncomplete:
		return PromotionIncomplete, ve.ReasonCode, DispositionExcluded, false
	case qp.VerifyIntegrityFailure:
		return PromotionIntegrityFailure, ve.ReasonCode, DispositionExcluded, false
	case qp.VerifyStale:
		return PromotionStale, ve.ReasonCode, DispositionExcluded, false
	case qp.VerifyUnverifiable:
		return PromotionUnverifiable, ve.ReasonCode, DispositionExcluded, false
	default:
		return PromotionUnknownClassification, "unknown_verification_class", DispositionExcluded, false
	}
}

// deriveAvailability applies the frozen precedence: invalid > unavailable > degraded >
// available > empty. It is a pure function of records + findings (contradiction is detected
// from the findings themselves), so it is discovery-order independent. Out-of-scope candidates
// never appear as findings and never degrade.
func deriveAvailability(records []VerifiedRecord, findings []Finding) Availability {
	unavailable := false
	degraded := false
	for _, f := range findings {
		switch f.Class {
		case PromotionUnknownClassification, PromotionContradictory, PromotionScopeIdentityInvalid:
			return FeedbackInvalid
		}
		if f.Disposition == DispositionUnavailable {
			unavailable = true
			continue
		}
		switch f.Class {
		case PromotionIncomplete, PromotionIntegrityFailure, PromotionStale, PromotionUnverifiable:
			degraded = true
		}
	}
	switch {
	case unavailable:
		return FeedbackUnavailable
	case degraded:
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
