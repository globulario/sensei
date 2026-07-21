// SPDX-License-Identifier: AGPL-3.0-only

package investigation

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture"
)

var sha256RE = regexp.MustCompile(`^[a-f0-9]{64}$`)

// IsValidSHA256 returns true if the string is a valid lowercase SHA256 hex digest.
func IsValidSHA256(s string) bool {
	return sha256RE.MatchString(s)
}

func isEscapingPath(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" {
		return false
	}
	return filepath.IsAbs(p) || strings.HasPrefix(p, "../") || strings.Contains(p, "/../") || p == ".."
}

// Validate performs structural, syntactic, and semantic validation on the investigation Document.
func Validate(doc Document) error {
	var errs []string

	// 1. Schema version and GeneratedBy
	if doc.SchemaVersion == "" {
		errs = append(errs, "schema_version is required")
	}
	if doc.GeneratedBy == "" {
		errs = append(errs, "generated_by is required")
	}

	// 2. Mode validation
	if !IsValidMode(doc.Mode) {
		errs = append(errs, fmt.Sprintf("invalid mode: %q", doc.Mode))
	}

	// 3. Binding validation
	if doc.Binding.Repository.RepositoryDomain == "" {
		errs = append(errs, "binding repository domain is required")
	}
	if doc.Binding.Repository.RevisionStatus == "" {
		errs = append(errs, "binding repository revision status is required")
	}
	if doc.Binding.Repository.GraphDigestStatus == "" {
		errs = append(errs, "binding repository graph digest status is required")
	}
	if !IsValidSHA256(doc.Binding.InvestigationPlanDigestSHA256) {
		errs = append(errs, "binding investigation plan digest must be a valid SHA256")
	}
	if !IsValidSHA256(doc.Binding.ExtractorProfileDigestSHA256) {
		errs = append(errs, "binding extractor profile digest must be a valid SHA256")
	}

	// 4. Model Binding Validation
	if !IsValidModelStatus(doc.Binding.Model.Status) {
		errs = append(errs, fmt.Sprintf("invalid model binding status: %q", doc.Binding.Model.Status))
	}
	if doc.Binding.Model.Status == ModelStatusResolved {
		if doc.Binding.Model.ModelName == "" {
			errs = append(errs, "resolved model status requires model_name")
		}
		if !IsValidSHA256(doc.Binding.Model.ModelDigestSHA256) {
			errs = append(errs, "resolved model status requires a valid model_digest_sha256")
		}
	}

	// 5. Model output claim validation
	hasModelOutput := len(doc.CandidateClaims) > 0 || len(doc.CandidateQuestions) > 0 || len(doc.Counterexamples) > 0
	if hasModelOutput {
		if doc.Binding.Model.Status != ModelStatusResolved {
			errs = append(errs, fmt.Sprintf("model status must be %q when model output is claimed, but got %q", ModelStatusResolved, doc.Binding.Model.Status))
		}
	}

	// 6. Evidence snapshot binding is explicit when external evidence is declared
	hasExternalEvidence := false
	for _, receipt := range doc.RawEvidence {
		if receipt.Category != EvidenceSourceCode && receipt.Category != EvidenceTests {
			hasExternalEvidence = true
			break
		}
	}
	if hasExternalEvidence {
		if doc.Binding.EvidenceSnapshotDigestSHA256 == "" {
			errs = append(errs, "evidence_snapshot_digest_sha256 must be set when external evidence is declared")
		} else if !IsValidSHA256(doc.Binding.EvidenceSnapshotDigestSHA256) {
			errs = append(errs, "evidence_snapshot_digest_sha256 must be a valid SHA256")
		}
	}

	// 7. Duplicate IDs check & Escaping Path checks
	evidenceIDs := make(map[string]bool)
	repoDomain := doc.Binding.Repository.RepositoryDomain

	for _, receipt := range doc.RawEvidence {
		if receipt.ID == "" {
			errs = append(errs, "raw evidence receipt ID is required")
		} else {
			if evidenceIDs[receipt.ID] {
				errs = append(errs, fmt.Sprintf("duplicate raw evidence receipt ID: %s", receipt.ID))
			}
			evidenceIDs[receipt.ID] = true
		}

		if !IsValidEvidenceCategory(receipt.Category) {
			errs = append(errs, fmt.Sprintf("invalid raw evidence category: %q", receipt.Category))
		}

		if !IsValidSHA256(receipt.ContentDigestSHA256) {
			errs = append(errs, fmt.Sprintf("raw evidence receipt %s must have a valid content_digest_sha256", receipt.ID))
		}

		if isEscapingPath(receipt.ContentLocation) {
			errs = append(errs, fmt.Sprintf("raw evidence receipt %s contains escaping content location path: %s", receipt.ID, receipt.ContentLocation))
		}

		for _, f := range receipt.Scope.Files {
			if isEscapingPath(f) {
				errs = append(errs, fmt.Sprintf("raw evidence receipt %s contains escaping file path: %s", receipt.ID, f))
			}
		}

		// Domain mismatch check
		receiptDomain := receipt.Scope.Domain
		if receiptDomain == "" {
			receiptDomain = receipt.Scope.Repository
		}
		if receiptDomain == "" {
			receiptDomain = receipt.Scope.Repo
		}
		if receiptDomain != "" && receiptDomain != repoDomain {
			errs = append(errs, fmt.Sprintf("raw evidence receipt %s repository/domain %q does not match document binding %q", receipt.ID, receiptDomain, repoDomain))
		}
	}

	observationIDs := make(map[string]bool)
	obsFiles := make(map[string]bool)
	obsSymbols := make(map[string]bool)

	for _, fact := range doc.Observations {
		if fact.ID == "" {
			errs = append(errs, "observation fact ID is required")
		} else {
			if observationIDs[fact.ID] {
				errs = append(errs, fmt.Sprintf("duplicate observation fact ID: %s", fact.ID))
			}
			observationIDs[fact.ID] = true
		}

		if isEscapingPath(fact.Evidence.SourceFile) {
			errs = append(errs, fmt.Sprintf("observation %s contains escaping source file path: %s", fact.ID, fact.Evidence.SourceFile))
		}

		for _, f := range fact.Scope.Files {
			if isEscapingPath(f) {
				errs = append(errs, fmt.Sprintf("observation %s contains escaping file path: %s", fact.ID, f))
			}
			obsFiles[f] = true
		}
		for _, s := range fact.Scope.Symbols {
			obsSymbols[s] = true
		}

		factDomain := fact.Scope.Repository
		if factDomain != "" && factDomain != repoDomain {
			errs = append(errs, fmt.Sprintf("observation %s repository %q does not match document binding %q", fact.ID, factDomain, repoDomain))
		}
	}

	// Gather all files and symbols from Observations and RawEvidence for candidate scope checks
	evidenceFiles := make(map[string]bool)
	evidenceSymbols := make(map[string]bool)

	for _, receipt := range doc.RawEvidence {
		for _, f := range receipt.Scope.Files {
			evidenceFiles[f] = true
		}
		for _, s := range receipt.Scope.Symbols {
			evidenceSymbols[s] = true
		}
	}

	allowedFiles := make(map[string]bool)
	allowedSymbols := make(map[string]bool)
	for f := range obsFiles {
		allowedFiles[f] = true
	}
	for f := range evidenceFiles {
		allowedFiles[f] = true
	}
	for s := range obsSymbols {
		allowedSymbols[s] = true
	}
	for s := range evidenceSymbols {
		allowedSymbols[s] = true
	}

	// 8. Coverage Entry Checks
	for i, entry := range doc.Coverage {
		if !IsValidEvidenceCategory(entry.Category) {
			errs = append(errs, fmt.Sprintf("coverage entry %d: invalid category %q", i, entry.Category))
		}
		if !IsValidCoverageStatus(entry.Status) {
			errs = append(errs, fmt.Sprintf("coverage entry %d: invalid status %q", i, entry.Status))
		}

		// searched_no_result requires execution proof
		isSearched := entry.Status == CoverageSupporting || entry.Status == CoverageRefuting ||
			entry.Status == CoverageMixed || entry.Status == CoverageNoResult

		if isSearched {
			if entry.ProviderID == "" {
				errs = append(errs, fmt.Sprintf("coverage entry %d status %q requires provider_id", i, entry.Status))
			}
			if entry.ProviderVersion == "" {
				errs = append(errs, fmt.Sprintf("coverage entry %d status %q requires provider_version", i, entry.Status))
			}
			if entry.SourceSnapshotDigestSHA256 == "" {
				errs = append(errs, fmt.Sprintf("coverage entry %d status %q requires source_snapshot_digest_sha256", i, entry.Status))
			} else if !IsValidSHA256(entry.SourceSnapshotDigestSHA256) {
				errs = append(errs, fmt.Sprintf("coverage entry %d status %q requires a valid source_snapshot_digest_sha256", i, entry.Status))
			}
		}
	}

	// 9. Candidate Claims Validation
	claimIDs := make(map[string]bool)
	for _, claim := range doc.CandidateClaims {
		if claim.ID == "" {
			errs = append(errs, "candidate claim ID is required")
		} else {
			if claimIDs[claim.ID] {
				errs = append(errs, fmt.Sprintf("duplicate candidate claim ID: %s", claim.ID))
			}
			claimIDs[claim.ID] = true
		}

		if claim.PromotionStatus != architecture.PromotionCandidate {
			errs = append(errs, fmt.Sprintf("candidate claim %s must have promotion status %q", claim.ID, architecture.PromotionCandidate))
		}

		if !claim.HumanReviewRequired {
			errs = append(errs, fmt.Sprintf("candidate claim %s must require human review", claim.ID))
		}

		if claim.Confidence < 0 || claim.Confidence > 1 {
			errs = append(errs, fmt.Sprintf("candidate claim %s confidence must be between 0 and 1, got %f", claim.ID, claim.Confidence))
		}

		// Non-escaping file paths in claim scope
		for _, f := range claim.Scope.Files {
			if isEscapingPath(f) {
				errs = append(errs, fmt.Sprintf("candidate claim %s contains escaping file path: %s", claim.ID, f))
			}
			// Scope expansion check
			if !allowedFiles[f] {
				errs = append(errs, fmt.Sprintf("candidate claim %s scope file %q is absent from observation and evidence scope (broadening refused)", claim.ID, f))
			}
		}

		// Symbols scope expansion check
		for _, s := range claim.Scope.Symbols {
			if !allowedSymbols[s] {
				errs = append(errs, fmt.Sprintf("candidate claim %s scope symbol %q is absent from observation and evidence scope (broadening refused)", claim.ID, s))
			}
		}

		claimDomain := claim.Scope.Domain
		if claimDomain == "" {
			claimDomain = claim.Scope.Repository
		}
		if claimDomain == "" {
			claimDomain = claim.Scope.Repo
		}
		if claimDomain != "" && claimDomain != repoDomain {
			errs = append(errs, fmt.Sprintf("candidate claim %s repository/domain %q does not match document binding %q", claim.ID, claimDomain, repoDomain))
		}
	}

	// 10. Candidate Questions Validation
	questionIDs := make(map[string]bool)
	for _, q := range doc.CandidateQuestions {
		if q.ID == "" {
			errs = append(errs, "candidate question ID is required")
		} else {
			if questionIDs[q.ID] {
				errs = append(errs, fmt.Sprintf("duplicate candidate question ID: %s", q.ID))
			}
			questionIDs[q.ID] = true
		}

		for _, f := range q.Scope.Files {
			if isEscapingPath(f) {
				errs = append(errs, fmt.Sprintf("candidate question %s contains escaping file path: %s", q.ID, f))
			}
			if !allowedFiles[f] {
				errs = append(errs, fmt.Sprintf("candidate question %s scope file %q is absent from observation and evidence scope (broadening refused)", q.ID, f))
			}
		}

		for _, s := range q.Scope.Symbols {
			if !allowedSymbols[s] {
				errs = append(errs, fmt.Sprintf("candidate question %s scope symbol %q is absent from observation and evidence scope (broadening refused)", q.ID, s))
			}
		}

		qDomain := q.Scope.Domain
		if qDomain == "" {
			qDomain = q.Scope.Repository
		}
		if qDomain == "" {
			qDomain = q.Scope.Repo
		}
		if qDomain != "" && qDomain != repoDomain {
			errs = append(errs, fmt.Sprintf("candidate question %s repository/domain %q does not match document binding %q", q.ID, qDomain, repoDomain))
		}
	}

	// 11. Counterexamples Validation
	ceIDs := make(map[string]bool)
	for _, ce := range doc.Counterexamples {
		if ce.ID == "" {
			errs = append(errs, "counterexample ID is required")
		} else {
			if ceIDs[ce.ID] {
				errs = append(errs, fmt.Sprintf("duplicate counterexample ID: %s", ce.ID))
			}
			ceIDs[ce.ID] = true
		}

		if ce.Description == "" {
			errs = append(errs, fmt.Sprintf("counterexample %s: description is required", ce.ID))
		}

		for _, f := range ce.Scope.Files {
			if isEscapingPath(f) {
				errs = append(errs, fmt.Sprintf("counterexample %s contains escaping file path: %s", ce.ID, f))
			}
			if !allowedFiles[f] {
				errs = append(errs, fmt.Sprintf("counterexample %s scope file %q is absent from observation and evidence scope (broadening refused)", ce.ID, f))
			}
		}

		for _, s := range ce.Scope.Symbols {
			if !allowedSymbols[s] {
				errs = append(errs, fmt.Sprintf("counterexample %s scope symbol %q is absent from observation and evidence scope (broadening refused)", ce.ID, s))
			}
		}

		ceDomain := ce.Scope.Domain
		if ceDomain == "" {
			ceDomain = ce.Scope.Repository
		}
		if ceDomain == "" {
			ceDomain = ce.Scope.Repo
		}
		if ceDomain != "" && ceDomain != repoDomain {
			errs = append(errs, fmt.Sprintf("counterexample %s repository/domain %q does not match document binding %q", ce.ID, ceDomain, repoDomain))
		}
	}

	// 12. Run Receipt Validation
	receipt := doc.Receipt
	if receipt.OutputDocumentDigestSHA256 != "" {
		if !IsValidSHA256(receipt.OutputDocumentDigestSHA256) {
			errs = append(errs, "receipt output document digest must be a valid SHA256")
		} else {
			// Compute document digest and verify matches receipt output document digest
			computedDigest, err := CalculateDocumentDigest(doc)
			if err != nil {
				errs = append(errs, fmt.Sprintf("failed to compute document digest for validation: %v", err))
			} else if computedDigest != receipt.OutputDocumentDigestSHA256 {
				errs = append(errs, fmt.Sprintf("output document digest mismatch: computed %s, receipt has %s", computedDigest, receipt.OutputDocumentDigestSHA256))
			}
		}
	}

	if receipt.PlanDigestSHA256 != "" && receipt.PlanDigestSHA256 != doc.Binding.InvestigationPlanDigestSHA256 {
		errs = append(errs, "receipt plan digest does not match binding plan digest")
	}
	if receipt.ExtractorProfileDigestSHA256 != "" && receipt.ExtractorProfileDigestSHA256 != doc.Binding.ExtractorProfileDigestSHA256 {
		errs = append(errs, "receipt extractor profile digest does not match binding extractor profile digest")
	}
	if receipt.EvidenceSnapshotDigestSHA256 != "" && receipt.EvidenceSnapshotDigestSHA256 != doc.Binding.EvidenceSnapshotDigestSHA256 {
		errs = append(errs, "receipt evidence snapshot digest does not match binding evidence snapshot digest")
	}
	if receipt.Model.Status != "" && receipt.Model.Status != doc.Binding.Model.Status {
		errs = append(errs, "receipt model status does not match binding model status")
	}
	if receipt.Model.ModelDigestSHA256 != "" && receipt.Model.ModelDigestSHA256 != doc.Binding.Model.ModelDigestSHA256 {
		errs = append(errs, "receipt model digest does not match binding model digest")
	}
	if receipt.ModelArtifactDigestSHA256 != "" && receipt.ModelArtifactDigestSHA256 != doc.Binding.Model.ModelDigestSHA256 {
		errs = append(errs, "receipt model artifact digest does not match binding model digest")
	}

	if receipt.TimestampSource != "" {
		if _, err := time.Parse(time.RFC3339, receipt.TimestampSource); err != nil {
			errs = append(errs, "receipt timestamp source must be RFC3339 formatted")
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}
