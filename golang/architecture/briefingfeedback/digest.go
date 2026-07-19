// SPDX-License-Identifier: Apache-2.0

package briefingfeedback

import (
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// ComputeDigest is the self-excluding projection digest: clear digest_sha256, canonicalize the
// complete typed projection (key-sorted canonical JSON — platform-stable, no timestamps or
// random values), hash SHA-256, lowercase hex.
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

// findingKey is the frozen finding identity tuple: class + reason code + lineage id + affected
// domain + affected files + disposition. Two findings with the same tuple are the same finding.
func findingKey(f Finding) string {
	return strings.Join([]string{
		string(f.Class), f.ReasonCode, f.LineageID, f.AffectedDomain,
		strings.Join(f.AffectedFiles, "\x1f"), string(f.Disposition),
	}, "\x00")
}

func findingLess(a, b Finding) bool {
	return findingKey(a) < findingKey(b)
}

// dedupeFindings removes exact-duplicate findings by their identity tuple, keeping the first
// occurrence. This is the single frozen rule; ValidateProjection additionally REFUSES any
// residual duplicate so a duplicate can never be silently preserved.
func dedupeFindings(fs []Finding) []Finding {
	seen := map[string]bool{}
	out := fs[:0:0]
	for _, f := range fs {
		k := findingKey(f)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, f)
	}
	return out
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

// sortedCanonical reports whether files is a strictly sorted, unique, fully repository-relative
// canonical slice (no padding, no duplicates, no unsafe segments).
func sortedCanonical(files []string) bool {
	for i, f := range files {
		if c, ok := canonicalRelFile(f); !ok || c != f {
			return false
		}
		if i > 0 && files[i-1] >= f {
			return false // unsorted or duplicate
		}
	}
	return true
}

// dispositionAllowed encodes the frozen (class, disposition) matrix. Findings never carry the
// admitted disposition (admitted candidates become records, not findings). Outage classes must
// be unavailable; definitive candidate defects must be excluded; unverifiable may be either
// (candidate-local → excluded, facility → unavailable).
func dispositionAllowed(c FindingClass, d Disposition) bool {
	switch d {
	case DispositionUnavailable:
		return c == PromotionDiscoveryUnavailable || c == PromotionUnverifiable
	case DispositionExcluded:
		switch c {
		case PromotionDiscoveryUnavailable:
			return false // an outage is never a per-candidate exclusion
		default:
			return true
		}
	default: // admitted or off-vocabulary
		return false
	}
}

// ValidateProjection strictly validates a projection: canonical schema/producer/bound identity,
// coherent repository/domain/task identity, canonical requested files, valid + canonical +
// deduplicated records and findings with a coherent (class, disposition) matrix, an availability
// that is exactly the deterministic function of the content, and a verified self-excluding
// digest. The zero value fails. Any mutation after digesting fails digest verification.
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

	// Repository + domain identity coherence. A feedback_invalid projection is a sanitized
	// fail-closed carrier that may blank an identity that was the reason for refusal; every
	// other projection must carry a present, canonical repository identity.
	if hasWhitespace(p.RepositoryIdentity) {
		return fmt.Errorf("feedback projection repository identity is malformed")
	}
	// An ESTABLISHED projection (the owner ran against a real repository) must carry a present,
	// canonical repository identity. A fail-closed carrier (invalid request, or unavailable when
	// no repository context is established) may blank the identity that could not be established.
	established := p.Availability == FeedbackAvailable || p.Availability == FeedbackEmpty || p.Availability == FeedbackDegraded
	if established && p.RepositoryIdentity == "" {
		return fmt.Errorf("feedback projection repository identity is empty")
	}
	// An unavailable projection may blank the repository identity ONLY for the exact
	// repository_context_absent carrier (no repository was ever established). Every other
	// unavailable case (domain mismatch, task scope not established, discovery/facility outage,
	// internal unavailability) requires a canonical non-empty identity, so a manually assembled
	// unavailable projection can never erase an established repository identity.
	if p.Availability == FeedbackUnavailable && p.RepositoryIdentity == "" {
		absent := len(p.Records) == 0 && p.TaskID == "" && p.SessionID == "" &&
			len(p.Findings) == 1 &&
			p.Findings[0].Class == PromotionDiscoveryUnavailable &&
			p.Findings[0].ReasonCode == string(RepositoryContextAbsent) &&
			p.Findings[0].Disposition == DispositionUnavailable
		if !absent {
			return fmt.Errorf("only a repository_context_absent carrier may blank an unavailable projection's repository identity")
		}
	}
	// An invalid projection likewise requires a canonical identity unless it is the
	// scope-identity carrier whose own identity could not be established.
	if p.Availability == FeedbackInvalid && p.RepositoryIdentity == "" {
		invalidCarrier := len(p.Records) == 0 && len(p.Findings) == 1 &&
			(p.Findings[0].Class == PromotionScopeIdentityInvalid)
		if !invalidCarrier {
			return fmt.Errorf("only a scope-identity-invalid carrier may blank an invalid projection's repository identity")
		}
	}
	if p.RequestedDomain != "" {
		if hasWhitespace(p.RequestedDomain) {
			return fmt.Errorf("feedback projection requested domain is padded/noncanonical")
		}
		if p.RepositoryIdentity != "" && p.RequestedDomain != p.RepositoryIdentity {
			return fmt.Errorf("feedback projection requested domain is incoherent with repository identity")
		}
	}
	if !sortedCanonical(p.RequestedFiles) {
		return fmt.Errorf("feedback projection requested files are unsafe, unsorted, or duplicated")
	}

	// Task identity: either both present or both absent (no half-bound projection).
	if (p.TaskID == "") != (p.SessionID == "") {
		return fmt.Errorf("feedback projection carries a task id without a session id (or vice versa)")
	}

	// Verified records.
	seen := map[string]bool{}
	for _, r := range p.Records {
		if r.VerificationClass != PromotionVerified {
			return fmt.Errorf("verified record %q class is not promotion_verified", r.PromotionLineageID)
		}
		for name, v := range map[string]string{
			"governed_node_iri": r.GovernedNodeIRI, "canonical_record_id": r.CanonicalRecordID,
			"promotion_lineage_id": r.PromotionLineageID, "promotion_receipt_digest": r.PromotionReceiptDigestSHA256,
			"question_id": r.QuestionID, "answer_id": r.AnswerID,
			"disposition_receipt_digest": r.DispositionReceiptDigestSHA256,
			"originating_task_id":        r.OriginatingTaskID, "originating_session_id": r.OriginatingSessionID,
		} {
			if v == "" {
				return fmt.Errorf("verified record %q missing provenance identity %s", r.PromotionLineageID, name)
			}
		}
		if r.EffectiveDomain != "" && hasWhitespace(r.EffectiveDomain) {
			return fmt.Errorf("verified record %q effective domain is noncanonical", r.PromotionLineageID)
		}
		if len(r.EffectiveFileScope) == 0 || !sortedCanonical(r.EffectiveFileScope) {
			return fmt.Errorf("verified record %q effective file scope is empty, unsafe, or noncanonical", r.PromotionLineageID)
		}
		if r.ProvenanceInterpretation != provenanceInterpretation {
			return fmt.Errorf("verified record provenance interpretation is not canonical")
		}
		if seen[r.PromotionLineageID] {
			return fmt.Errorf("duplicate verified record lineage %q", r.PromotionLineageID)
		}
		seen[r.PromotionLineageID] = true
	}

	// Findings.
	fseen := map[string]bool{}
	for _, f := range p.Findings {
		if !validFindingClass(f.Class) || f.Class == PromotionVerified {
			return fmt.Errorf("finding class %q is off-vocabulary", f.Class)
		}
		if f.ReasonCode == "" {
			return fmt.Errorf("finding %q carries no reason code", f.Class)
		}
		if !dispositionAllowed(f.Class, f.Disposition) {
			return fmt.Errorf("finding class %q with disposition %q is a contradictory combination", f.Class, f.Disposition)
		}
		k := findingKey(f)
		if fseen[k] {
			return fmt.Errorf("duplicate finding identity %q", f.Class)
		}
		fseen[k] = true
	}

	if !recordsSorted(p.Records) || !findingsSorted(p.Findings) {
		return fmt.Errorf("feedback projection records/findings are not in canonical order")
	}

	// Availability must be exactly the deterministic function of the content.
	if want := deriveAvailability(p.Records, p.Findings); want != p.Availability {
		return fmt.Errorf("feedback availability %q is inconsistent with content (want %q)", p.Availability, want)
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
