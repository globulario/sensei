// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

var sha256Regex = regexp.MustCompile(`^[a-f0-9]{64}$`)

// IsSHA256 checks if a string is a valid SHA256 hex hash.
func IsSHA256(s string) bool {
	return sha256Regex.MatchString(s)
}

// Validate performs constitutional grounding, semantic boundary, and input-binding validation.
func Validate(res Result, snap GroundingSnapshot) error {
	var errs []string

	// Helper to check if a slice contains a string
	contains := func(slice []string, val string) bool {
		for _, s := range slice {
			if s == val {
				return true
			}
		}
		return false
	}

	// 1. Validate Input Bindings (Section 5)
	repo := res.Binding.Repository
	if repo.RepositoryDomain == "" {
		errs = append(errs, "repository domain is empty")
	}
	if repo.RevisionStatus == "" {
		errs = append(errs, "repository revision status is empty")
	}
	if repo.GraphDigestStatus == "" {
		errs = append(errs, "repository graph digest status is empty")
	}
	if repo.Revision == "" && repo.TreeDigestSHA256 == "" {
		errs = append(errs, "repository revision or tree digest is required")
	}

	// Verify HOW and WHY document digests match input bindings
	if res.Binding.HowDocumentDigestSHA256 == "" || !IsSHA256(res.Binding.HowDocumentDigestSHA256) {
		errs = append(errs, "HOW document digest must be a valid SHA256")
	}
	if res.Binding.WhyDocumentDigestSHA256 == "" || !IsSHA256(res.Binding.WhyDocumentDigestSHA256) {
		errs = append(errs, "WHY document digest must be a valid SHA256")
	}

	// Section 5 Rule 7: evidence snapshot digest must equal WHY document binding digest
	if res.Binding.EvidenceSnapshotDigestSHA256 != res.Document.Binding.EvidenceSnapshotDigestSHA256 {
		errs = append(errs, "evidence snapshot digest must match document evidence snapshot digest")
	}

	// Section 5 Rule 4 & 6: check that result repository binding equals document binding
	docRepo := res.Document.Binding.Repository
	if docRepo.RepositoryDomain != repo.RepositoryDomain ||
		docRepo.Revision != repo.Revision ||
		docRepo.RevisionStatus != repo.RevisionStatus ||
		docRepo.GraphDigestStatus != repo.GraphDigestStatus ||
		docRepo.GraphDigestSHA256 != repo.GraphDigestSHA256 {
		errs = append(errs, "result repository binding must match document repository binding")
	}

	// Section 5 Rule 2: WHY document mode must be why
	if res.Document.Mode != investigation.ModeWhy && res.Document.Mode != investigation.ModeChallenge {
		errs = append(errs, fmt.Sprintf("document mode must be %q or %q, got %q", investigation.ModeWhy, investigation.ModeChallenge, res.Document.Mode))
	}

	// Section 5 Rule 5: WHY must bind the exact HOW output digest
	if res.Document.Binding.Why.HowDocumentDigestSHA256 != res.Binding.HowDocumentDigestSHA256 {
		errs = append(errs, "WHY document HOW digest does not match bound HOW document digest")
	}

	// Section 5 Rule 8: graph, claims, closure, question, and review-history digests must be explicit
	if res.Binding.GraphDigestSHA256 == "" || !IsSHA256(res.Binding.GraphDigestSHA256) {
		errs = append(errs, "graph digest must be explicit and valid")
	}
	if res.Binding.CurrentClaimsDigestSHA256 == "" || !IsSHA256(res.Binding.CurrentClaimsDigestSHA256) {
		errs = append(errs, "current claims digest must be explicit and valid")
	}
	if res.Binding.ClosureStateDigestSHA256 == "" || !IsSHA256(res.Binding.ClosureStateDigestSHA256) {
		errs = append(errs, "closure state digest must be explicit and valid")
	}
	if res.Binding.ExistingQuestionsDigestSHA256 == "" || !IsSHA256(res.Binding.ExistingQuestionsDigestSHA256) {
		errs = append(errs, "existing questions digest must be explicit and valid")
	}
	if res.Binding.ReviewHistoryDigestSHA256 == "" || !IsSHA256(res.Binding.ReviewHistoryDigestSHA256) {
		errs = append(errs, "review history digest must be explicit and valid")
	}
	actualGroundingDigest, digestErr := GroundingSnapshotDigest(snap)
	if digestErr != nil {
		errs = append(errs, fmt.Sprintf("grounding snapshot digest: %v", digestErr))
	} else {
		if res.Binding.GroundingSnapshotDigestSHA256 != actualGroundingDigest {
			errs = append(errs, "grounding snapshot digest does not match binding")
		}
		if res.Receipt.GroundingSnapshotDigestSHA256 != actualGroundingDigest {
			errs = append(errs, "grounding snapshot digest does not match receipt")
		}
	}

	// Validate repository domains match between Result binding and claims/counterexamples
	repoDomain := repo.RepositoryDomain

	// 2. Validate Grounding Snapshot against Candidate and Request Scopes (Section 6)
	groundingFiles := make(map[string]bool)
	for _, f := range snap.Files {
		// Reject absolute paths and path traversal
		if filepath.IsAbs(f) || strings.HasPrefix(f, "/") {
			errs = append(errs, fmt.Sprintf("grounding snapshot contains absolute file path: %s", f))
		}
		if strings.Contains(f, "..") {
			errs = append(errs, fmt.Sprintf("grounding snapshot contains path traversal: %s", f))
		}
		groundingFiles[f] = true
	}

	validateScopeGrounding := func(scope architecture.ClaimScope, contextName string) {
		for _, f := range scope.Files {
			if filepath.IsAbs(f) || strings.HasPrefix(f, "/") {
				errs = append(errs, fmt.Sprintf("%s scope contains absolute file path: %s", contextName, f))
			}
			if strings.Contains(f, "..") {
				errs = append(errs, fmt.Sprintf("%s scope contains path traversal: %s", contextName, f))
			}
			if !groundingFiles[f] {
				errs = append(errs, fmt.Sprintf("%s references unresolved file: %s", contextName, f))
			}
		}
		for _, s := range scope.Symbols {
			if !contains(snap.Symbols, s) {
				errs = append(errs, fmt.Sprintf("%s references unresolved symbol: %s", contextName, s))
			}
		}
		for _, node := range scope.Components {
			if !contains(snap.GraphNodeIDs, node) {
				errs = append(errs, fmt.Sprintf("%s references unresolved graph node: %s", contextName, node))
			}
		}
	}

	// 3. Validate Candidate Envelopes (Section 7 & 9)
	candidateIDs := make(map[string]bool)
	claimIDs := make(map[string]bool)
	candidateMap := make(map[string]CandidateEnvelope)
	candidateByClaimID := make(map[string]CandidateEnvelope)

	for _, envelope := range res.Candidates {
		if envelope.CandidateID == "" {
			errs = append(errs, "candidate ID is empty")
		} else {
			if candidateIDs[envelope.CandidateID] {
				errs = append(errs, fmt.Sprintf("duplicate candidate ID: %s", envelope.CandidateID))
			}
			candidateIDs[envelope.CandidateID] = true
			candidateMap[envelope.CandidateID] = envelope
			candidateByClaimID[envelope.ClaimID] = envelope
		}

		if envelope.ClaimID == "" {
			errs = append(errs, "claim ID is empty")
		} else {
			if claimIDs[envelope.ClaimID] {
				errs = append(errs, fmt.Sprintf("duplicate claim ID envelope: %s", envelope.ClaimID))
			}
			claimIDs[envelope.ClaimID] = true
		}

		if !IsValidCandidateKind(envelope.OutputKind) {
			errs = append(errs, fmt.Sprintf("candidate %s: unknown candidate kind %q", envelope.CandidateID, envelope.OutputKind))
		}

		// Grounding observation and evidence references
		for _, obsID := range envelope.ObservationRefIDs {
			if !contains(snap.ObservationIDs, obsID) {
				errs = append(errs, fmt.Sprintf("candidate %s references dangling observation: %s", envelope.CandidateID, obsID))
			}
		}
		for _, supID := range envelope.SupportingEvidenceRefIDs {
			if !contains(snap.EvidenceReceiptIDs, supID) {
				errs = append(errs, fmt.Sprintf("candidate %s references dangling supporting evidence: %s", envelope.CandidateID, supID))
			}
		}
		for _, refID := range envelope.RefutingEvidenceRefIDs {
			if !contains(snap.EvidenceReceiptIDs, refID) {
				errs = append(errs, fmt.Sprintf("candidate %s references dangling refuting evidence: %s", envelope.CandidateID, refID))
			}
		}

		// Supporting and refuting evidence stay distinct
		for _, supID := range envelope.SupportingEvidenceRefIDs {
			if contains(envelope.RefutingEvidenceRefIDs, supID) {
				errs = append(errs, fmt.Sprintf("candidate %s has overlapping supporting and refuting evidence ID: %s", envelope.CandidateID, supID))
			}
		}
	}

	// 4. Validate Candidate Claims against Constitutional Rules (Section 9 & 10)
	documentClaims := make(map[string]architecture.Claim)
	for _, claim := range res.Document.CandidateClaims {
		documentClaims[claim.ID] = claim

		if claim.PromotionStatus != architecture.PromotionCandidate {
			errs = append(errs, fmt.Sprintf("candidate claim %s carries authoritative promotion status %q", claim.ID, claim.PromotionStatus))
		}
		if !claim.HumanReviewRequired {
			errs = append(errs, fmt.Sprintf("candidate claim %s must require human review", claim.ID))
		}
		if claim.EpistemicStatus == architecture.StatusSupported || claim.EpistemicStatus == architecture.StatusRefuted {
			errs = append(errs, fmt.Sprintf("candidate claim %s carries active/accepted status %q", claim.ID, claim.EpistemicStatus))
		}

		// Repository binding domain match
		claimRepo := claim.Scope.Repository
		if claimRepo == "" {
			claimRepo = claim.Scope.Repo
		}
		if claimRepo != "" && claimRepo != repoDomain {
			errs = append(errs, fmt.Sprintf("candidate claim %s repository domain %q does not match result binding %q", claim.ID, claimRepo, repoDomain))
		}

		// Grounding validation on files/symbols
		validateScopeGrounding(claim.Scope, fmt.Sprintf("candidate claim %s", claim.ID))

		// Check scope expansion: claim scope must not exceed the union of its cited evidence scopes
		envelope, ok := candidateByClaimID[claim.ID]
		if !ok {
			errs = append(errs, fmt.Sprintf("candidate claim %s has no envelope", claim.ID))
		} else {
			var unionFiles []string
			var unionSymbols []string
			var unionComponents []string

			// We resolve cited evidence scopes from the raw evidence in the document
			evidenceMap := make(map[string]investigation.EvidenceReceipt)
			for _, rec := range res.Document.RawEvidence {
				evidenceMap[rec.ID] = rec
			}

			// unavailable/not_configured/skipped/searched_no_result coverage check
			coverageMap := make(map[string]investigation.CoverageEntry)
			for _, cov := range res.Document.Coverage {
				coverageMap[cov.ProviderID] = cov
			}

			for _, supID := range envelope.SupportingEvidenceRefIDs {
				rec, found := evidenceMap[supID]
				if found {
					unionFiles = append(unionFiles, rec.Scope.Files...)
					unionSymbols = append(unionSymbols, rec.Scope.Symbols...)
					unionComponents = append(unionComponents, rec.Scope.Components...)

					// Rule 7 & 8: check if provider coverage was unavailable or searched_no_result
					cov, covFound := coverageMap[rec.Provider.ID]
					if covFound {
						if cov.Status == investigation.CoverageUnavailable || cov.Status == investigation.CoverageNotConfigured || cov.Status == investigation.CoverageSkipped {
							errs = append(errs, fmt.Sprintf("candidate claim %s cites evidence %s from unavailable/not_configured provider %q as positive support", claim.ID, supID, rec.Provider.ID))
						}
						if cov.Status == investigation.CoverageNoResult {
							errs = append(errs, fmt.Sprintf("candidate claim %s cites evidence %s from searched_no_result provider %q as positive support", claim.ID, supID, rec.Provider.ID))
						}
					}
				}
			}

			// Validate subset bounding
			for _, f := range claim.Scope.Files {
				if !contains(unionFiles, f) {
					errs = append(errs, fmt.Sprintf("candidate claim %s scope file %q exceeds cited supporting evidence scopes", claim.ID, f))
				}
			}
			for _, s := range claim.Scope.Symbols {
				if !contains(unionSymbols, s) {
					errs = append(errs, fmt.Sprintf("candidate claim %s scope symbol %q exceeds cited supporting evidence scopes", claim.ID, s))
				}
			}
			for _, c := range claim.Scope.Components {
				if !contains(unionComponents, c) {
					errs = append(errs, fmt.Sprintf("candidate claim %s scope component %q exceeds cited supporting evidence scopes", claim.ID, c))
				}
			}
		}
	}

	// Verify that claim references in envelopes exist
	for _, envelope := range res.Candidates {
		if _, exists := documentClaims[envelope.ClaimID]; !exists {
			errs = append(errs, fmt.Sprintf("envelope candidate %s references dangling claim ID: %s", envelope.CandidateID, envelope.ClaimID))
		}
	}

	// 5. Validate Evidence Requests (Section 11)
	requestIDs := make(map[string]bool)
	for _, req := range res.EvidenceRequests {
		if req.ID == "" {
			errs = append(errs, "evidence request ID is empty")
		} else {
			if requestIDs[req.ID] {
				errs = append(errs, fmt.Sprintf("duplicate evidence request ID: %s", req.ID))
			}
			requestIDs[req.ID] = true
		}

		if !IsValidEvidenceRequestReason(req.ReasonCode) {
			errs = append(errs, fmt.Sprintf("evidence request %s: unknown reason code %q", req.ID, req.ReasonCode))
		}

		// Scope broadening check: evidence request scope must be no broader than the candidate scope
		candidate, found := candidateMap[req.CandidateID]
		if found {
			claim, claimFound := documentClaims[candidate.ClaimID]
			if claimFound {
				// Validate files subset
				for _, f := range req.Scope.Files {
					if !contains(claim.Scope.Files, f) {
						errs = append(errs, fmt.Sprintf("evidence request %s scope file %q exceeds candidate %s scope", req.ID, f, req.CandidateID))
					}
				}
				// Validate symbols subset
				for _, s := range req.Scope.Symbols {
					if !contains(claim.Scope.Symbols, s) {
						errs = append(errs, fmt.Sprintf("evidence request %s scope symbol %q exceeds candidate %s scope", req.ID, s, req.CandidateID))
					}
				}
			}
		} else {
			errs = append(errs, fmt.Sprintf("evidence request %s references dangling candidate ID: %s", req.ID, req.CandidateID))
		}

		// Grounding scope
		validateScopeGrounding(req.Scope, fmt.Sprintf("evidence request %s", req.ID))
	}

	challengeIDs := make(map[string]bool)
	for _, chall := range res.Challenges {
		if chall.ID == "" {
			errs = append(errs, "challenge receipt ID is empty")
		} else {
			if challengeIDs[chall.ID] {
				errs = append(errs, fmt.Sprintf("duplicate challenge ID: %s", chall.ID))
			}
			challengeIDs[chall.ID] = true
		}

		if !IsValidChallengeStatus(chall.Status) {
			errs = append(errs, fmt.Sprintf("challenge receipt %s: unknown challenge status %q", chall.ID, chall.Status))
		}
		if strings.TrimSpace(chall.StrategyVersion) == "" || strings.TrimSpace(chall.ReasonCode) == "" {
			errs = append(errs, fmt.Sprintf("challenge receipt %s requires strategy version and reason code", chall.ID))
		}
		for _, id := range chall.SupportingEvidenceRefIDs {
			if contains(chall.RefutingEvidenceRefIDs, id) {
				errs = append(errs, fmt.Sprintf("challenge receipt %s overlaps supporting and refuting evidence", chall.ID))
			}
		}

		if _, candidateExists := candidateMap[chall.CandidateID]; !candidateExists {
			errs = append(errs, fmt.Sprintf("challenge receipt %s references dangling candidate ID: %s", chall.ID, chall.CandidateID))
		}

		// Grounding evidence, requests, counterexamples references
		for _, supID := range chall.SupportingEvidenceRefIDs {
			if !contains(snap.EvidenceReceiptIDs, supID) {
				errs = append(errs, fmt.Sprintf("challenge receipt %s references dangling supporting evidence: %s", chall.ID, supID))
			}
		}
		for _, refID := range chall.RefutingEvidenceRefIDs {
			if !contains(snap.EvidenceReceiptIDs, refID) {
				errs = append(errs, fmt.Sprintf("challenge receipt %s references dangling refuting evidence: %s", chall.ID, refID))
			}
		}
		for _, reqID := range chall.EvidenceRequestIDs {
			if !requestIDs[reqID] {
				errs = append(errs, fmt.Sprintf("challenge receipt %s references dangling evidence request: %s", chall.ID, reqID))
			}
		}
	}

	// 7. Validate Counterexamples (Section 13)
	counterexampleIDs := make(map[string]bool)
	for _, record := range res.Counterexamples {
		ce := record.Counterexample
		if ce.ID == "" {
			errs = append(errs, "counterexample ID is empty")
		} else {
			if counterexampleIDs[ce.ID] {
				errs = append(errs, fmt.Sprintf("duplicate counterexample ID: %s", ce.ID))
			}
			counterexampleIDs[ce.ID] = true
		}

		// Grounding scope
		validateScopeGrounding(ce.Scope, fmt.Sprintf("counterexample %s", ce.ID))

		// Counterexample scope domain must match
		ceRepo := ce.Scope.Repository
		if ceRepo == "" {
			ceRepo = ce.Scope.Repo
		}
		if ceRepo != "" && ceRepo != repoDomain {
			errs = append(errs, fmt.Sprintf("counterexample %s repository domain %q does not match result binding %q", ce.ID, ceRepo, repoDomain))
		}

		// Dangling claim/candidate reference
		if ce.ClaimID != "" {
			_, claimFound := documentClaims[ce.ClaimID]
			_, candFound := candidateMap[ce.ClaimID]
			if !claimFound && !candFound {
				errs = append(errs, fmt.Sprintf("counterexample %s references dangling candidate/claim: %s", ce.ID, ce.ClaimID))
			}
		}

		// Counterexample scope expansion check: counterexample scope files must be subset of target candidate files
		if ce.ClaimID != "" {
			claim, claimFound := documentClaims[ce.ClaimID]
			if claimFound {
				for _, f := range ce.Scope.Files {
					if !contains(claim.Scope.Files, f) {
						errs = append(errs, fmt.Sprintf("counterexample %s scope file %q exceeds target candidate scope", ce.ID, f))
					}
				}
			}
		}
		if strings.TrimSpace(record.StrategyVersion) == "" || strings.TrimSpace(record.MinimalityBasis) == "" {
			errs = append(errs, fmt.Sprintf("counterexample %s requires strategy version and minimality basis", ce.ID))
		}

		// Grounding evidence refs
		for _, ref := range ce.EvidenceRefIDs {
			if !contains(snap.EvidenceReceiptIDs, ref) {
				errs = append(errs, fmt.Sprintf("counterexample %s references dangling evidence receipt: %s", ce.ID, ref))
			}
		}
	}

	// Verify counterexample refs in challenge receipts
	for _, chall := range res.Challenges {
		for _, ceID := range chall.CounterexampleIDs {
			if !counterexampleIDs[ceID] {
				errs = append(errs, fmt.Sprintf("challenge receipt %s references dangling counterexample: %s", chall.ID, ceID))
			}
		}
	}

	rankingCandidateIDs := make(map[string]bool)
	for _, r := range res.Rankings {
		if r.CandidateID == "" {
			errs = append(errs, "ranking record candidate ID is empty")
		} else {
			if rankingCandidateIDs[r.CandidateID] {
				errs = append(errs, fmt.Sprintf("duplicate ranking candidate ID: %s", r.CandidateID))
			}
			rankingCandidateIDs[r.CandidateID] = true
		}
		if r.Rank <= 0 {
			errs = append(errs, fmt.Sprintf("ranking record %s must have positive rank", r.CandidateID))
		}

		if _, exists := candidateMap[r.CandidateID]; !exists {
			errs = append(errs, fmt.Sprintf("ranking record references dangling candidate ID: %s", r.CandidateID))
		}

		for _, f := range r.Factors {
			if !IsValidRankingFactorKind(f.Kind) {
				errs = append(errs, fmt.Sprintf("ranking record for candidate %s: unknown factor kind %q", r.CandidateID, f.Kind))
			}
			for _, ref := range f.EvidenceRefIDs {
				if !contains(snap.EvidenceReceiptIDs, ref) {
					errs = append(errs, fmt.Sprintf("ranking factor in %s references dangling evidence: %s", r.CandidateID, ref))
				}
			}
		}
	}

	// 9. Validate Run Receipt digests (Section 16 & 17)
	receipt := res.Receipt
	if receipt.SchemaVersion == "" || receipt.GeneratedBy == "" || receipt.GeneratorVersion == "" || receipt.RulesetVersion == "" {
		errs = append(errs, "receipt schema, generator, generator version, and ruleset version are required")
	}
	for _, pair := range []struct{ got, want, name string }{
		{receipt.CurrentClaimsDigestSHA256, res.Binding.CurrentClaimsDigestSHA256, "current claims"},
		{receipt.ClosureStateDigestSHA256, res.Binding.ClosureStateDigestSHA256, "closure state"},
		{receipt.ExistingQuestionsDigestSHA256, res.Binding.ExistingQuestionsDigestSHA256, "existing questions"},
		{receipt.ReviewHistoryDigestSHA256, res.Binding.ReviewHistoryDigestSHA256, "review history"},
	} {
		if pair.got == "" || pair.got != pair.want {
			errs = append(errs, "receipt "+pair.name+" digest must exactly match binding")
		}
	}
	if receipt.InputBinding != res.Binding {
		errs = append(errs, "receipt input binding must exactly match result binding")
	}
	if receipt.HowDocumentDigestSHA256 == "" || receipt.HowDocumentDigestSHA256 != res.Binding.HowDocumentDigestSHA256 {
		errs = append(errs, fmt.Sprintf("receipt HOW digest %q does not match binding HOW digest %q", receipt.HowDocumentDigestSHA256, res.Binding.HowDocumentDigestSHA256))
	}
	if receipt.WhyDocumentDigestSHA256 == "" || receipt.WhyDocumentDigestSHA256 != res.Binding.WhyDocumentDigestSHA256 {
		errs = append(errs, fmt.Sprintf("receipt WHY digest %q does not match binding WHY digest %q", receipt.WhyDocumentDigestSHA256, res.Binding.WhyDocumentDigestSHA256))
	}
	if receipt.GraphDigestSHA256 == "" || receipt.GraphDigestSHA256 != repo.GraphDigestSHA256 {
		errs = append(errs, fmt.Sprintf("receipt graph digest %q does not match binding graph digest %q", receipt.GraphDigestSHA256, repo.GraphDigestSHA256))
	}

	if receipt.TimestampSource == "" || receipt.NondeterminismDeclaration == "" {
		errs = append(errs, "receipt timestamp source and nondeterminism declaration are required")
	}
	if receipt.ExactResultDigestSHA256 == "" {
		errs = append(errs, "receipt exact result digest is required")
	} else {
		recomputed, err := ResultDigest(res)
		if err != nil {
			errs = append(errs, fmt.Sprintf("recomputing result digest failed: %v", err))
		} else if receipt.ExactResultDigestSHA256 != recomputed {
			errs = append(errs, fmt.Sprintf("receipt output digest %q does not match recomputed result digest %q", receipt.ExactResultDigestSHA256, recomputed))
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}

	return nil
}
