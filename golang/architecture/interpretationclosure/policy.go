// SPDX-License-Identifier: AGPL-3.0-only

package interpretationclosure

import (
	"fmt"
	"reflect"
)

// Certify is the pure closure-policy fold over observations supplied by an
// evidence owner. It does not collect repository evidence itself and must not
// be treated as an authority source merely because this package computed the
// result. Runtime promotion therefore happens through an explicit authority
// capability which owns evidence collection, then presents the resulting
// receipt for binding/policy verification.
//
// In particular, TruthUnknown is neutral: it never grants authority and never
// blocks it. A contradiction is categorically different from absence of a
// decidable check.
func Certify(in Input) (Receipt, error) {
	if err := validateInput(in); err != nil {
		return Receipt{}, err
	}
	blockers := blockersFor(in)
	authority := AuthorityGoverning
	if len(blockers) != 0 {
		authority = AuthorityAdvisory
	}
	r := normalizeReceipt(Receipt{
		SchemaVersion: ReceiptSchemaVersion,
		Input:         in,
		Authority:     authority,
		Blockers:      blockers,
	})
	digest, err := receiptDigest(r)
	if err != nil {
		return Receipt{}, fmt.Errorf("interpretationclosure: digest receipt: %w", err)
	}
	r.ReceiptDigestSHA256 = digest
	return r, nil
}

func blockersFor(in Input) []string {
	var blockers []string
	for _, f := range in.TruthFindings {
		if f.Status == TruthContradicted {
			blockers = append(blockers, "truth:contradicted:"+f.ClaimID+":"+f.CheckKind)
		}
	}

	// Completeness is the one gate where unknown cannot safely become hard
	// authority. A bounded synthesis surface must earn the claim that the
	// required surface is reachable; #3119 is the concrete failure class.
	switch in.Completeness.Status {
	case CompletenessIncomplete:
		blockers = append(blockers, "completeness:incomplete")
	case CompletenessUnknown:
		blockers = append(blockers, "completeness:unknown")
	}

	// Minimal-realization unknown is neutral for the same anti-selection
	// reason as truth unknown. Once there is positive evidence that a
	// realization is broader than proven or explicitly needs review, it may
	// not be represented as clean governing authority.
	switch in.Realization.Status {
	case RealizationBroaderThanProven:
		blockers = append(blockers, "realization:broader_than_proven")
	case RealizationReviewRequired:
		blockers = append(blockers, "realization:review_required")
	}

	for _, p := range in.ProofObservations {
		if !p.RequiredForAuthority {
			continue
		}
		switch p.Status {
		case ProofUnresolved:
			blockers = append(blockers, "proof:unresolved:"+p.ObligationID)
		case ProofContradicted:
			blockers = append(blockers, "proof:contradicted:"+p.ObligationID)
		}
	}
	return sortedUnique(blockers)
}

// Verify verifies provenance binding, receipt integrity, and the recorded
// policy decision for either advisory or governing receipts. It never trusts
// Receipt.Authority or Receipt.Blockers as caller assertions: the policy is
// recomputed from the bound observations and compared against the receipt.
func Verify(r Receipt, interpretationDigest, repositoryRevision, graphAuthorityDigest string) error {
	if r.SchemaVersion != ReceiptSchemaVersion {
		return fmt.Errorf("interpretationclosure: unsupported receipt schema %q", r.SchemaVersion)
	}
	if r.InterpretationDigestSHA256 != interpretationDigest {
		return fmt.Errorf("interpretationclosure: receipt interpretation digest %q does not match %q", r.InterpretationDigestSHA256, interpretationDigest)
	}
	if r.RepositoryRevision != repositoryRevision {
		return fmt.Errorf("interpretationclosure: receipt repository revision %q does not match %q", r.RepositoryRevision, repositoryRevision)
	}
	if r.GraphAuthorityDigestSHA256 != graphAuthorityDigest {
		return fmt.Errorf("interpretationclosure: receipt graph authority digest %q does not match %q", r.GraphAuthorityDigestSHA256, graphAuthorityDigest)
	}

	recomputed, err := Certify(r.Input)
	if err != nil {
		return err
	}
	if recomputed.ReceiptDigestSHA256 != r.ReceiptDigestSHA256 {
		return fmt.Errorf("interpretationclosure: receipt digest mismatch: declared %q, recomputed %q", r.ReceiptDigestSHA256, recomputed.ReceiptDigestSHA256)
	}
	if recomputed.Authority != r.Authority || !reflect.DeepEqual(recomputed.Blockers, normalizeReceipt(r).Blockers) {
		return fmt.Errorf("interpretationclosure: recorded authority decision does not match recomputed policy")
	}
	return nil
}

// VerifyForGoverning adds the authority requirement to Verify. Advisory is a
// valid closure outcome, but it cannot cross the O1 promotion boundary.
func VerifyForGoverning(r Receipt, interpretationDigest, repositoryRevision, graphAuthorityDigest string) error {
	if err := Verify(r, interpretationDigest, repositoryRevision, graphAuthorityDigest); err != nil {
		return err
	}
	if r.Authority != AuthorityGoverning {
		return fmt.Errorf("interpretationclosure: interpretation is advisory, not governing: blockers=%v", r.Blockers)
	}
	return nil
}

// AssessCompleteness compares repository/graph-derived required surface with
// what the interpretation disclosed. A nil required slice means the evidence
// owner did not establish required surface and therefore yields unknown. An
// explicitly empty required slice means the owner established that no
// additional surface is required.
func AssessCompleteness(disclosed, required []string, evidenceRefs ...string) CompletenessAssessment {
	if required == nil {
		return CompletenessAssessment{
			Status:             CompletenessUnknown,
			EvidenceReferences: sortedUnique(evidenceRefs),
			Detail:             "required repair surface was not established by an evidence owner",
		}
	}
	have := make(map[string]struct{}, len(disclosed))
	for _, v := range disclosed {
		have[v] = struct{}{}
	}
	var missing []string
	for _, v := range required {
		if _, ok := have[v]; !ok {
			missing = append(missing, v)
		}
	}
	if len(missing) != 0 {
		return CompletenessAssessment{
			Status:             CompletenessIncomplete,
			EvidenceReferences: sortedUnique(evidenceRefs),
			MissingSurface:     sortedUnique(missing),
			Detail:             "required repository surface is absent from the governed interpretation",
		}
	}
	return CompletenessAssessment{
		Status:             CompletenessComplete,
		EvidenceReferences: sortedUnique(evidenceRefs),
	}
}

// AssessRealization is intentionally conservative. It can detect known
// unjustified breadth when an evidence owner supplies a justified surface;
// it does not pretend to solve general program minimization. Nil justified
// surface yields neutral unknown.
func AssessRealization(proposed, justified []string, evidenceRefs ...string) RealizationAssessment {
	if justified == nil {
		return RealizationAssessment{
			Status:             RealizationUnknown,
			EvidenceReferences: sortedUnique(evidenceRefs),
			Detail:             "no authoritative minimal-realization surface was established",
		}
	}
	allowed := make(map[string]struct{}, len(justified))
	for _, v := range justified {
		allowed[v] = struct{}{}
	}
	var extra []string
	for _, v := range proposed {
		if _, ok := allowed[v]; !ok {
			extra = append(extra, v)
		}
	}
	if len(extra) != 0 {
		return RealizationAssessment{
			Status:             RealizationBroaderThanProven,
			EvidenceReferences: sortedUnique(evidenceRefs),
			UnjustifiedSurface: sortedUnique(extra),
			Detail:             "proposed realization exceeds the surface justified by available evidence",
		}
	}
	return RealizationAssessment{
		Status:             RealizationCandidateMinimal,
		EvidenceReferences: sortedUnique(evidenceRefs),
	}
}
