// SPDX-License-Identifier: AGPL-3.0-only

// Package interpretationclosure owns the pre-synthesis authority decision for
// architectural interpretations. It is deliberately separate from
// architecture/certification: certifying a premise to govern a repair is not
// evidence that the resulting repair is correct, admitted, or verified.
package interpretationclosure

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const ReceiptSchemaVersion = "sensei.interpretation-closure.receipt.v1"

type TruthStatus string

const (
	TruthSupported    TruthStatus = "supported"
	TruthUnknown      TruthStatus = "unknown"
	TruthContradicted TruthStatus = "contradicted"
)

type CompletenessStatus string

const (
	CompletenessComplete   CompletenessStatus = "complete"
	CompletenessIncomplete CompletenessStatus = "incomplete"
	CompletenessUnknown    CompletenessStatus = "unknown"
)

type RealizationStatus string

const (
	RealizationMinimal           RealizationStatus = "minimal"
	RealizationCandidateMinimal  RealizationStatus = "candidate_minimal"
	RealizationBroaderThanProven RealizationStatus = "broader_than_proven"
	RealizationReviewRequired    RealizationStatus = "review_required"
	RealizationUnknown           RealizationStatus = "unknown"
)

type ProofStatus string

const (
	ProofSatisfied    ProofStatus = "satisfied"
	ProofUnresolved   ProofStatus = "unresolved"
	ProofContradicted ProofStatus = "contradicted"
)

type AuthorityStatus string

const (
	AuthorityAdvisory  AuthorityStatus = "advisory"
	AuthorityGoverning AuthorityStatus = "governing"
)

// TruthFinding is a deterministic observation about one checkable portion of
// a claim. Unknown is intentionally neutral: lack of a checker is neither
// proof nor contradiction. This prevents certification pressure toward only
// shallow, mechanically decidable architectural knowledge.
type TruthFinding struct {
	ClaimID            string      `json:"claim_id"`
	Language           string      `json:"language,omitempty"`
	CheckKind          string      `json:"check_kind"`
	Subject            string      `json:"subject,omitempty"`
	Status             TruthStatus `json:"status"`
	EvidenceReferences []string    `json:"evidence_references,omitempty"`
	Detail             string      `json:"detail,omitempty"`
}

type CompletenessAssessment struct {
	Status             CompletenessStatus `json:"status"`
	EvidenceReferences []string           `json:"evidence_references,omitempty"`
	MissingSurface     []string           `json:"missing_surface,omitempty"`
	Detail             string             `json:"detail,omitempty"`
}

type RealizationAssessment struct {
	Status             RealizationStatus `json:"status"`
	EvidenceReferences []string          `json:"evidence_references,omitempty"`
	UnjustifiedSurface []string          `json:"unjustified_surface,omitempty"`
	Detail             string            `json:"detail,omitempty"`
}

type ProofObservation struct {
	ObligationID       string      `json:"obligation_id"`
	RequiredForAuthority bool      `json:"required_for_authority"`
	Status             ProofStatus `json:"status"`
	EvidenceReferences []string    `json:"evidence_references,omitempty"`
	Detail             string      `json:"detail,omitempty"`
}

// Input contains observations produced by their respective evidence owners.
// It contains no caller-supplied "certified" boolean. Policy derives
// authority from these observations every time.
type Input struct {
	InterpretationDigestSHA256 string                 `json:"interpretation_digest_sha256"`
	RepositoryRevision         string                 `json:"repository_revision"`
	GraphAuthorityDigestSHA256 string                 `json:"graph_authority_digest_sha256"`
	TruthFindings              []TruthFinding         `json:"truth_findings"`
	Completeness               CompletenessAssessment `json:"completeness"`
	Realization                RealizationAssessment  `json:"realization"`
	ProofObservations          []ProofObservation     `json:"proof_observations"`
}

// Receipt is an evidence-bound authority receipt. Authority and Blockers are
// recorded for auditability but never trusted on read: VerifyForGoverning
// recomputes them from the observations before accepting the receipt.
type Receipt struct {
	SchemaVersion string `json:"schema_version"`
	Input
	Authority AuthorityStatus `json:"authority"`
	Blockers  []string        `json:"blockers"`
	ReceiptDigestSHA256 string `json:"receipt_digest_sha256"`
}

func validDigest(s string) bool {
	if len(s) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func validateInput(in Input) error {
	if !validDigest(in.InterpretationDigestSHA256) {
		return fmt.Errorf("interpretationclosure: interpretation digest must be a SHA-256 hex digest")
	}
	if strings.TrimSpace(in.RepositoryRevision) == "" {
		return fmt.Errorf("interpretationclosure: repository revision is required")
	}
	if !validDigest(in.GraphAuthorityDigestSHA256) {
		return fmt.Errorf("interpretationclosure: graph authority digest must be a SHA-256 hex digest")
	}
	for i, f := range in.TruthFindings {
		switch f.Status {
		case TruthSupported, TruthUnknown, TruthContradicted:
		default:
			return fmt.Errorf("interpretationclosure: truth finding %d has unknown status %q", i, f.Status)
		}
		if strings.TrimSpace(f.ClaimID) == "" || strings.TrimSpace(f.CheckKind) == "" {
			return fmt.Errorf("interpretationclosure: truth finding %d requires claim_id and check_kind", i)
		}
	}
	switch in.Completeness.Status {
	case CompletenessComplete, CompletenessIncomplete, CompletenessUnknown:
	default:
		return fmt.Errorf("interpretationclosure: unknown completeness status %q", in.Completeness.Status)
	}
	switch in.Realization.Status {
	case RealizationMinimal, RealizationCandidateMinimal, RealizationBroaderThanProven, RealizationReviewRequired, RealizationUnknown:
	default:
		return fmt.Errorf("interpretationclosure: unknown realization status %q", in.Realization.Status)
	}
	for i, p := range in.ProofObservations {
		if strings.TrimSpace(p.ObligationID) == "" {
			return fmt.Errorf("interpretationclosure: proof observation %d requires obligation_id", i)
		}
		switch p.Status {
		case ProofSatisfied, ProofUnresolved, ProofContradicted:
		default:
			return fmt.Errorf("interpretationclosure: proof observation %d has unknown status %q", i, p.Status)
		}
	}
	return nil
}

func normalizeReceipt(r Receipt) Receipt {
	r.TruthFindings = append([]TruthFinding(nil), r.TruthFindings...)
	for i := range r.TruthFindings {
		r.TruthFindings[i].EvidenceReferences = sortedUnique(r.TruthFindings[i].EvidenceReferences)
	}
	sort.Slice(r.TruthFindings, func(i, j int) bool {
		a, b := r.TruthFindings[i], r.TruthFindings[j]
		if a.ClaimID != b.ClaimID { return a.ClaimID < b.ClaimID }
		if a.CheckKind != b.CheckKind { return a.CheckKind < b.CheckKind }
		if a.Subject != b.Subject { return a.Subject < b.Subject }
		return a.Status < b.Status
	})
	r.Completeness.EvidenceReferences = sortedUnique(r.Completeness.EvidenceReferences)
	r.Completeness.MissingSurface = sortedUnique(r.Completeness.MissingSurface)
	r.Realization.EvidenceReferences = sortedUnique(r.Realization.EvidenceReferences)
	r.Realization.UnjustifiedSurface = sortedUnique(r.Realization.UnjustifiedSurface)
	r.ProofObservations = append([]ProofObservation(nil), r.ProofObservations...)
	for i := range r.ProofObservations {
		r.ProofObservations[i].EvidenceReferences = sortedUnique(r.ProofObservations[i].EvidenceReferences)
	}
	sort.Slice(r.ProofObservations, func(i, j int) bool { return r.ProofObservations[i].ObligationID < r.ProofObservations[j].ObligationID })
	r.Blockers = sortedUnique(r.Blockers)
	return r
}

func sortedUnique(in []string) []string {
	if len(in) == 0 { return nil }
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" { continue }
		if _, ok := seen[v]; ok { continue }
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	if len(out) == 0 { return nil }
	return out
}

func receiptDigest(r Receipt) (string, error) {
	r = normalizeReceipt(r)
	r.ReceiptDigestSHA256 = ""
	b, err := json.Marshal(r)
	if err != nil { return "", err }
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
