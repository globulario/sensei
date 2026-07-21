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
	} else {
		if doc.Binding.Model.ModelName != "" {
			errs = append(errs, fmt.Sprintf("model_name must be empty when model status is %q", doc.Binding.Model.Status))
		}
		if doc.Binding.Model.ModelDigestSHA256 != "" {
			errs = append(errs, fmt.Sprintf("model_digest_sha256 must be empty when model status is %q", doc.Binding.Model.Status))
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

		if !IsValidProofStrength(receipt.ProofStrength) {
			errs = append(errs, fmt.Sprintf("raw evidence receipt %s has invalid proof strength: %q", receipt.ID, receipt.ProofStrength))
		}

		if receipt.Provider.ID == "" {
			errs = append(errs, fmt.Sprintf("raw evidence receipt %s must have a provider ID", receipt.ID))
		}
		if receipt.Provider.Version == "" {
			errs = append(errs, fmt.Sprintf("raw evidence receipt %s must have a provider version", receipt.ID))
		}
		if receipt.SourceIdentity == "" {
			errs = append(errs, fmt.Sprintf("raw evidence receipt %s must have a source identity", receipt.ID))
		}
		if !IsValidSHA256(receipt.SourceDigestSHA256) {
			errs = append(errs, fmt.Sprintf("raw evidence receipt %s must have a valid source_digest_sha256", receipt.ID))
		}
		if !IsValidSHA256(receipt.ContentDigestSHA256) {
			errs = append(errs, fmt.Sprintf("raw evidence receipt %s must have a valid content_digest_sha256", receipt.ID))
		}
		if receipt.CapturedAt == "" {
			errs = append(errs, fmt.Sprintf("raw evidence receipt %s must have a captured_at timestamp", receipt.ID))
		} else {
			if _, err := time.Parse(time.RFC3339, receipt.CapturedAt); err != nil {
				errs = append(errs, fmt.Sprintf("raw evidence receipt %s captured_at must be RFC3339 formatted, got %q", receipt.ID, receipt.CapturedAt))
			}
		}
		if receipt.CapturedContent != "" {
			computed := SHA256String(receipt.CapturedContent)
			if computed != receipt.ContentDigestSHA256 {
				errs = append(errs, fmt.Sprintf("raw evidence receipt %s content digest mismatch: computed %s from captured_content, but content_digest_sha256 is %s", receipt.ID, computed, receipt.ContentDigestSHA256))
			}
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

		// Grounding scope of this claim locally using only cited evidence & premise facts
		claimAllowedFiles := make(map[string]bool)
		claimAllowedSymbols := make(map[string]bool)
		claimAllowedComponents := make(map[string]bool)
		claimAllowedSourceSets := make(map[string]bool)

		addScopeFromEvidenceRef := func(ref string) {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				return
			}
			id := ref
			hasPrefix := false
			if strings.HasPrefix(id, "evidence:") {
				id = strings.TrimPrefix(id, "evidence:")
				hasPrefix = true
			} else if strings.HasPrefix(id, "fact:") {
				id = strings.TrimPrefix(id, "fact:")
				hasPrefix = true
			}

			for _, rec := range doc.RawEvidence {
				if rec.ID == id || (!hasPrefix && rec.ID == ref) {
					for _, f := range rec.Scope.Files {
						claimAllowedFiles[f] = true
					}
					for _, s := range rec.Scope.Symbols {
						claimAllowedSymbols[s] = true
					}
					for _, comp := range rec.Scope.Components {
						claimAllowedComponents[comp] = true
					}
					if rec.Scope.SourceSet != "" {
						claimAllowedSourceSets[rec.Scope.SourceSet] = true
					}
				}
			}
			for _, fact := range doc.Observations {
				if fact.ID == id || (!hasPrefix && fact.ID == ref) {
					for _, f := range fact.Scope.Files {
						claimAllowedFiles[f] = true
					}
					for _, s := range fact.Scope.Symbols {
						claimAllowedSymbols[s] = true
					}
				}
			}
		}

		for _, ref := range claim.SupportingEvidence {
			addScopeFromEvidenceRef(ref)
		}
		for _, ref := range claim.RefutingEvidence {
			addScopeFromEvidenceRef(ref)
		}
		for _, ref := range claim.PremiseFacts {
			addScopeFromEvidenceRef(ref)
		}

		// Non-escaping file paths in claim scope
		for _, f := range claim.Scope.Files {
			if isEscapingPath(f) {
				errs = append(errs, fmt.Sprintf("candidate claim %s contains escaping file path: %s", claim.ID, f))
			}
			// Scope expansion check
			if !claimAllowedFiles[f] {
				errs = append(errs, fmt.Sprintf("candidate claim %s scope file %q is not grounded in its cited evidence or facts (borrowing refused)", claim.ID, f))
			}
		}

		// Symbols scope expansion check
		for _, s := range claim.Scope.Symbols {
			if !claimAllowedSymbols[s] {
				errs = append(errs, fmt.Sprintf("candidate claim %s scope symbol %q is not grounded in its cited evidence or facts (borrowing refused)", claim.ID, s))
			}
		}

		// Components scope grounding check
		for _, comp := range claim.Scope.Components {
			if !claimAllowedComponents[comp] {
				errs = append(errs, fmt.Sprintf("candidate claim %s scope component %q is not grounded in its cited evidence or facts (borrowing refused)", claim.ID, comp))
			}
		}

		// SourceSet scope grounding check
		if claim.Scope.SourceSet != "" {
			if !claimAllowedSourceSets[claim.Scope.SourceSet] {
				errs = append(errs, fmt.Sprintf("candidate claim %s scope source set %q is not grounded in its cited evidence or facts (borrowing refused)", claim.ID, claim.Scope.SourceSet))
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

		questionAllowedFiles := make(map[string]bool)
		questionAllowedSymbols := make(map[string]bool)
		questionAllowedComponents := make(map[string]bool)
		questionAllowedSourceSets := make(map[string]bool)

		for _, factID := range q.KnownFactIDs {
			id := strings.TrimPrefix(factID, "fact:")
			for _, fact := range doc.Observations {
				if fact.ID == id || fact.ID == factID {
					for _, f := range fact.Scope.Files {
						questionAllowedFiles[f] = true
					}
					for _, s := range fact.Scope.Symbols {
						questionAllowedSymbols[s] = true
					}
				}
			}
		}
		for _, evID := range q.KnownEvidence {
			id := strings.TrimPrefix(evID, "evidence:")
			for _, rec := range doc.RawEvidence {
				if rec.ID == id || rec.ID == evID {
					for _, f := range rec.Scope.Files {
						questionAllowedFiles[f] = true
					}
					for _, s := range rec.Scope.Symbols {
						questionAllowedSymbols[s] = true
					}
					for _, comp := range rec.Scope.Components {
						questionAllowedComponents[comp] = true
					}
					if rec.Scope.SourceSet != "" {
						questionAllowedSourceSets[rec.Scope.SourceSet] = true
					}
				}
			}
		}

		for _, f := range q.Scope.Files {
			if isEscapingPath(f) {
				errs = append(errs, fmt.Sprintf("candidate question %s contains escaping file path: %s", q.ID, f))
			}
			if !questionAllowedFiles[f] {
				errs = append(errs, fmt.Sprintf("candidate question %s scope file %q is not grounded in its cited known facts or evidence (borrowing refused)", q.ID, f))
			}
		}

		for _, s := range q.Scope.Symbols {
			if !questionAllowedSymbols[s] {
				errs = append(errs, fmt.Sprintf("candidate question %s scope symbol %q is not grounded in its cited known facts or evidence (borrowing refused)", q.ID, s))
			}
		}

		for _, comp := range q.Scope.Components {
			if !questionAllowedComponents[comp] {
				errs = append(errs, fmt.Sprintf("candidate question %s scope component %q is not grounded in its cited known facts or evidence (borrowing refused)", q.ID, comp))
			}
		}

		if q.Scope.SourceSet != "" {
			if !questionAllowedSourceSets[q.Scope.SourceSet] {
				errs = append(errs, fmt.Sprintf("candidate question %s scope source set %q is not grounded in its cited known facts or evidence (borrowing refused)", q.ID, q.Scope.SourceSet))
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

		ceAllowedFiles := make(map[string]bool)
		ceAllowedSymbols := make(map[string]bool)
		ceAllowedComponents := make(map[string]bool)
		ceAllowedSourceSets := make(map[string]bool)

		for _, evID := range ce.EvidenceRefIDs {
			id := strings.TrimPrefix(evID, "evidence:")
			for _, rec := range doc.RawEvidence {
				if rec.ID == id || rec.ID == evID {
					for _, f := range rec.Scope.Files {
						ceAllowedFiles[f] = true
					}
					for _, s := range rec.Scope.Symbols {
						ceAllowedSymbols[s] = true
					}
					for _, comp := range rec.Scope.Components {
						ceAllowedComponents[comp] = true
					}
					if rec.Scope.SourceSet != "" {
						ceAllowedSourceSets[rec.Scope.SourceSet] = true
					}
				}
			}
		}

		for _, f := range ce.Scope.Files {
			if isEscapingPath(f) {
				errs = append(errs, fmt.Sprintf("counterexample %s contains escaping file path: %s", ce.ID, f))
			}
			if !ceAllowedFiles[f] {
				errs = append(errs, fmt.Sprintf("counterexample %s scope file %q is not grounded in its cited evidence (borrowing refused)", ce.ID, f))
			}
		}

		for _, s := range ce.Scope.Symbols {
			if !ceAllowedSymbols[s] {
				errs = append(errs, fmt.Sprintf("counterexample %s scope symbol %q is not grounded in its cited evidence (borrowing refused)", ce.ID, s))
			}
		}

		for _, comp := range ce.Scope.Components {
			if !ceAllowedComponents[comp] {
				errs = append(errs, fmt.Sprintf("counterexample %s scope component %q is not grounded in its cited evidence (borrowing refused)", ce.ID, comp))
			}
		}

		if ce.Scope.SourceSet != "" {
			if !ceAllowedSourceSets[ce.Scope.SourceSet] {
				errs = append(errs, fmt.Sprintf("counterexample %s scope source set %q is not grounded in its cited evidence (borrowing refused)", ce.ID, ce.Scope.SourceSet))
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
	if receipt.OutputDocumentDigestSHA256 == "" {
		errs = append(errs, "receipt output document digest is required")
	} else if !IsValidSHA256(receipt.OutputDocumentDigestSHA256) {
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

	if receipt.SchemaVersion == "" {
		errs = append(errs, "receipt schema_version is required")
	} else if receipt.SchemaVersion != doc.SchemaVersion {
		errs = append(errs, fmt.Sprintf("receipt schema_version %q does not match document schema_version %q", receipt.SchemaVersion, doc.SchemaVersion))
	}
	if receipt.GeneratedBy == "" {
		errs = append(errs, "receipt generated_by is required")
	} else if receipt.GeneratedBy != doc.GeneratedBy {
		errs = append(errs, fmt.Sprintf("receipt generated_by %q does not match document generated_by %q", receipt.GeneratedBy, doc.GeneratedBy))
	}
	if receipt.PostProcessingVersion == "" {
		errs = append(errs, "receipt post_processing_version is required")
	}
	if receipt.TimestampSource == "" {
		errs = append(errs, "receipt timestamp_source is required")
	} else {
		if _, err := time.Parse(time.RFC3339, receipt.TimestampSource); err != nil {
			errs = append(errs, fmt.Sprintf("receipt timestamp_source must be RFC3339 formatted, got %q", receipt.TimestampSource))
		}
	}
	if len(receipt.ResourceLimits) == 0 {
		errs = append(errs, "receipt resource_limits are required")
	}
	if receipt.NondeterminismDeclaration == "" {
		errs = append(errs, "receipt nondeterminism_declaration is required")
	}

	// Verify OutputCandidateIDsAndDigests matches candidates exactly
	candidateIDs := make(map[string]bool)
	for _, claim := range doc.CandidateClaims {
		candidateIDs[claim.ID] = true
	}
	for _, q := range doc.CandidateQuestions {
		candidateIDs[q.ID] = true
	}
	for _, ce := range doc.Counterexamples {
		candidateIDs[ce.ID] = true
	}

	for id := range candidateIDs {
		digestVal, ok := receipt.OutputCandidateIDsAndDigests[id]
		if !ok {
			errs = append(errs, fmt.Sprintf("receipt output_candidate_ids_and_digests is missing candidate: %s", id))
		} else if !IsValidSHA256(digestVal) {
			errs = append(errs, fmt.Sprintf("receipt output_candidate_ids_and_digests for candidate %s must be a valid SHA256", id))
		}
	}

	if len(receipt.OutputCandidateIDsAndDigests) != len(candidateIDs) {
		errs = append(errs, fmt.Sprintf("receipt output_candidate_ids_and_digests length %d does not match actual candidates count %d", len(receipt.OutputCandidateIDsAndDigests), len(candidateIDs)))
	}

	if receipt.PlanDigestSHA256 != doc.Binding.InvestigationPlanDigestSHA256 {
		errs = append(errs, fmt.Sprintf("receipt plan digest %q does not match binding plan digest %q", receipt.PlanDigestSHA256, doc.Binding.InvestigationPlanDigestSHA256))
	}
	if receipt.ExtractorProfileDigestSHA256 != doc.Binding.ExtractorProfileDigestSHA256 {
		errs = append(errs, fmt.Sprintf("receipt extractor profile digest %q does not match binding extractor profile digest %q", receipt.ExtractorProfileDigestSHA256, doc.Binding.ExtractorProfileDigestSHA256))
	}
	if receipt.EvidenceSnapshotDigestSHA256 != doc.Binding.EvidenceSnapshotDigestSHA256 {
		errs = append(errs, fmt.Sprintf("receipt evidence snapshot digest %q does not match binding evidence snapshot digest %q", receipt.EvidenceSnapshotDigestSHA256, doc.Binding.EvidenceSnapshotDigestSHA256))
	}
	if receipt.Model.Status != doc.Binding.Model.Status {
		errs = append(errs, fmt.Sprintf("receipt model status %q does not match binding model status %q", receipt.Model.Status, doc.Binding.Model.Status))
	}
	if receipt.Model.ModelName != doc.Binding.Model.ModelName {
		errs = append(errs, fmt.Sprintf("receipt model name %q does not match binding model name %q", receipt.Model.ModelName, doc.Binding.Model.ModelName))
	}
	if receipt.Model.ModelDigestSHA256 != doc.Binding.Model.ModelDigestSHA256 {
		errs = append(errs, fmt.Sprintf("receipt model digest %q does not match binding model digest %q", receipt.Model.ModelDigestSHA256, doc.Binding.Model.ModelDigestSHA256))
	}
	if receipt.ModelArtifactDigestSHA256 != doc.Binding.Model.ModelDigestSHA256 {
		errs = append(errs, fmt.Sprintf("receipt model artifact digest %q does not match binding model digest %q", receipt.ModelArtifactDigestSHA256, doc.Binding.Model.ModelDigestSHA256))
	}

	// Mandate repository details match binding
	if receipt.Repository.RepositoryDomain != doc.Binding.Repository.RepositoryDomain {
		errs = append(errs, fmt.Sprintf("receipt repository domain %q does not match binding repository domain %q", receipt.Repository.RepositoryDomain, doc.Binding.Repository.RepositoryDomain))
	}
	if receipt.Repository.Revision != doc.Binding.Repository.Revision {
		errs = append(errs, fmt.Sprintf("receipt repository revision %q does not match binding repository revision %q", receipt.Repository.Revision, doc.Binding.Repository.Revision))
	}
	if receipt.Repository.RevisionStatus != doc.Binding.Repository.RevisionStatus {
		errs = append(errs, fmt.Sprintf("receipt repository revision status %q does not match binding repository revision status %q", receipt.Repository.RevisionStatus, doc.Binding.Repository.RevisionStatus))
	}
	if receipt.Repository.TreeDigestSHA256 != doc.Binding.Repository.TreeDigestSHA256 {
		errs = append(errs, fmt.Sprintf("receipt repository tree digest %q does not match binding repository tree digest %q", receipt.Repository.TreeDigestSHA256, doc.Binding.Repository.TreeDigestSHA256))
	}
	if receipt.Repository.GraphDigestSHA256 != doc.Binding.Repository.GraphDigestSHA256 {
		errs = append(errs, fmt.Sprintf("receipt repository graph digest %q does not match binding repository graph digest %q", receipt.Repository.GraphDigestSHA256, doc.Binding.Repository.GraphDigestSHA256))
	}
	if receipt.Repository.GraphDigestStatus != doc.Binding.Repository.GraphDigestStatus {
		errs = append(errs, fmt.Sprintf("receipt repository graph digest status %q does not match binding repository graph digest status %q", receipt.Repository.GraphDigestStatus, doc.Binding.Repository.GraphDigestStatus))
	}
	if receipt.GraphDigestSHA256 != doc.Binding.Repository.GraphDigestSHA256 {
		errs = append(errs, fmt.Sprintf("receipt graph digest %q does not match binding repository graph digest %q", receipt.GraphDigestSHA256, doc.Binding.Repository.GraphDigestSHA256))
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}
