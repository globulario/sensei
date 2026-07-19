// SPDX-License-Identifier: Apache-2.0

package briefingfeedback

import (
	"fmt"
	"sort"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// ComputeDigest is the self-excluding projection digest: clear digest_sha256, canonicalize
// the complete typed projection (key-sorted canonical JSON — platform-stable, no timestamps
// or random values), hash SHA-256, lowercase hex.
func ComputeDigest(p Projection) (string, error) {
	p.DigestSHA256 = ""
	return closureprotocol.SemanticDigest(p)
}

// recordLess / findingLess are the canonical total orders (deterministic across platforms).
func recordLess(a, b VerifiedRecord) bool {
	if a.GovernedNodeIRI != b.GovernedNodeIRI {
		return a.GovernedNodeIRI < b.GovernedNodeIRI
	}
	return a.PromotionLineageID < b.PromotionLineageID
}

func findingLess(a, b Finding) bool {
	if a.LineageID != b.LineageID {
		return a.LineageID < b.LineageID
	}
	if a.Class != b.Class {
		return a.Class < b.Class
	}
	return a.ReasonCode < b.ReasonCode
}

func sortRecords(rs []VerifiedRecord) {
	sort.SliceStable(rs, func(i, j int) bool { return recordLess(rs[i], rs[j]) })
}
func sortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool { return findingLess(fs[i], fs[j]) })
}

func recordsSorted(rs []VerifiedRecord) bool {
	for i := 1; i < len(rs); i++ {
		if recordLess(rs[i], rs[i-1]) {
			return false
		}
	}
	return true
}

func findingsSorted(fs []Finding) bool {
	for i := 1; i < len(fs); i++ {
		if findingLess(fs[i], fs[i-1]) {
			return false
		}
	}
	return true
}

// ValidateProjection strictly validates a projection: exact schema/producer/bound, valid
// availability + non-authoritative marker, valid + canonical records/findings, no duplicate
// record lineage, canonical order, and a verified self-excluding digest. The zero value
// fails (empty schema). Mutation after digesting fails digest verification.
func ValidateProjection(p Projection) error {
	if p.SchemaVersion != SchemaVersion {
		return fmt.Errorf("feedback projection schema %q is not %q", p.SchemaVersion, SchemaVersion)
	}
	if p.ProducerName != ProducerName || p.ProducerVersion != ProducerVersion {
		return fmt.Errorf("feedback projection producer identity is not canonical")
	}
	if !validAvailability(p.Availability) {
		return fmt.Errorf("feedback availability %q is off-vocabulary", p.Availability)
	}
	if !p.NonAuthoritativeProjection {
		return fmt.Errorf("feedback projection must be marked non-authoritative")
	}
	if p.Bound != BoundStatement {
		return fmt.Errorf("feedback projection bound statement is not canonical")
	}
	seen := map[string]bool{}
	for _, r := range p.Records {
		if r.VerificationClass != PromotionVerified {
			return fmt.Errorf("verified record %q class is not promotion_verified", r.PromotionLineageID)
		}
		if r.PromotionLineageID == "" || r.GovernedNodeIRI == "" {
			return fmt.Errorf("verified record missing identity")
		}
		if r.ProvenanceInterpretation != provenanceInterpretation {
			return fmt.Errorf("verified record provenance interpretation is not canonical")
		}
		if seen[r.PromotionLineageID] {
			return fmt.Errorf("duplicate verified record lineage %q", r.PromotionLineageID)
		}
		seen[r.PromotionLineageID] = true
	}
	for _, f := range p.Findings {
		if !validFindingClass(f.Class) || f.Class == PromotionVerified {
			return fmt.Errorf("finding class %q is off-vocabulary", f.Class)
		}
		if f.ReasonCode == "" {
			return fmt.Errorf("finding %q carries no reason code", f.Class)
		}
		switch f.Disposition {
		case DispositionAdmitted, DispositionExcluded, DispositionUnavailable:
		default:
			return fmt.Errorf("finding disposition %q is off-vocabulary", f.Disposition)
		}
	}
	if !recordsSorted(p.Records) || !findingsSorted(p.Findings) {
		return fmt.Errorf("feedback projection records/findings are not in canonical order")
	}
	want, err := ComputeDigest(p)
	if err != nil {
		return fmt.Errorf("recompute feedback digest: %w", err)
	}
	if p.DigestSHA256 != want {
		return fmt.Errorf("feedback projection digest does not match its content")
	}
	return nil
}
