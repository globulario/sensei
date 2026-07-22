// SPDX-License-Identifier: AGPL-3.0-only

package deviation

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
)

const repeatedDeviationInferenceRule = "deviation.repeated_independent_occurrences.v1"

// Analyze creates an exact Phase 10.6 document from immutable receipts.
func Analyze(binding architecture.ClaimDocumentBinding, receipts []Receipt, minimumIndependentOccurrences int) (Analysis, error) {
	if minimumIndependentOccurrences == 0 {
		minimumIndependentOccurrences = DefaultMinimumOccurrences
	}
	binding = canonicalizeBinding(binding)
	if err := validateExactBinding(binding); err != nil {
		return Analysis{}, err
	}
	normalizedReceipts, err := NormalizeReceipts(receipts)
	if err != nil {
		return Analysis{}, err
	}
	for _, receipt := range normalizedReceipts {
		if receipt.Binding.RepositoryDomain != binding.RepositoryDomain {
			return Analysis{}, fmt.Errorf("deviation receipt %s repository does not match analysis binding", receipt.ID)
		}
	}
	patterns, err := Cluster(normalizedReceipts, minimumIndependentOccurrences)
	if err != nil {
		return Analysis{}, err
	}
	candidates, err := GenerateCandidates(patterns)
	if err != nil {
		return Analysis{}, err
	}
	analysis := Analysis{
		SchemaVersion:                 SchemaVersion,
		GeneratedBy:                   GeneratedBy,
		Binding:                       binding,
		MinimumIndependentOccurrences: minimumIndependentOccurrences,
		Receipts:                      normalizedReceipts,
		Patterns:                      patterns,
		Candidates:                    candidates,
	}
	analysis.Receipt = buildRunReceipt(analysis)
	digest, err := AnalysisDigest(analysis)
	if err != nil {
		return Analysis{}, err
	}
	analysis.Receipt.ExactAnalysisDigestSHA256 = digest
	if err := ValidateAnalysis(analysis); err != nil {
		return Analysis{}, err
	}
	return analysis, nil
}

// GenerateCandidates converts only repeated eligible patterns into advisory claims.
func GenerateCandidates(patterns []Pattern) ([]Candidate, error) {
	out := make([]Candidate, 0, len(patterns))
	seen := map[string]bool{}
	for _, pattern := range patterns {
		pattern = canonicalizePattern(pattern)
		if err := ValidatePattern(pattern); err != nil {
			return nil, err
		}
		if !pattern.CandidateEligible {
			continue
		}
		kind, predicate, label := candidateShape(pattern.Kind)
		status := architecture.StatusUnknown
		if len(pattern.RelatedClaimIDs) > 0 {
			status = architecture.StatusContested
		}
		claim := architecture.Claim{
			Label:       label,
			Description: fmt.Sprintf("%d independent implementation deviations share one structured architectural shape; repetition raises review priority but grants no authority.", pattern.IndependentOccurrenceCount),
			Statement: architecture.ClaimStatement{
				Subject:   pattern.Shape.Subject,
				Predicate: predicate,
				Object:    pattern.Shape.Object,
			},
			Scope:              pattern.Scope,
			ArchitecturalPlane: architecture.PlaneObserved,
			AssertionOrigin:    architecture.OriginDerived,
			EpistemicStatus:    status,
			InferenceRule:      repeatedDeviationInferenceRule,
			SupportingEvidence: pattern.EvidenceRefs,
			ConflictsWith:      pattern.RelatedClaimIDs,
			Unknowns: []string{
				"whether the architecture is incorrect, scoped incorrectly, or being repeatedly bypassed",
				"whether a governed exception or missing contract would resolve the friction",
			},
			InvalidationConditions: []string{
				"duplicate-source correction reduces independent occurrences below threshold",
				"governed review determines the occurrences do not share one architectural cause",
			},
			Confidence:          0,
			HumanReviewRequired: true,
			PromotionStatus:     architecture.PromotionCandidate,
		}
		normalizedClaims, err := architecture.NormalizeClaims([]architecture.Claim{claim})
		if err != nil {
			return nil, err
		}
		claim = normalizedClaims[0]
		candidate := Candidate{
			PatternID:  pattern.ID,
			Kind:       kind,
			Claim:      claim,
			ReceiptIDs: pattern.ReceiptIDs,
		}
		candidate.ID = expectedCandidateID(candidate)
		digest, err := CandidateDigest(candidate)
		if err != nil {
			return nil, err
		}
		candidate.SemanticDigestSHA256 = digest
		if err := ValidateCandidate(candidate); err != nil {
			return nil, err
		}
		if seen[candidate.ID] {
			return nil, fmt.Errorf("duplicate deviation candidate %s", candidate.ID)
		}
		seen[candidate.ID] = true
		out = append(out, candidate)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ValidateCandidate enforces candidate-only, zero-authority output.
func ValidateCandidate(in Candidate) error {
	candidate, err := normalizeCandidate(in)
	if err != nil {
		return err
	}
	var errs []string
	if candidate.PatternID == "" || !strings.HasPrefix(candidate.PatternID, "deviation_pattern.") {
		errs = append(errs, "candidate requires a deviation pattern id")
	}
	if candidate.ID == "" || candidate.ID != expectedCandidateID(candidate) {
		errs = append(errs, "deviation candidate id mismatch")
	}
	if candidate.Claim.Confidence != 0 {
		errs = append(errs, "repeated deviation candidate confidence must remain zero")
	}
	if !candidate.Claim.HumanReviewRequired || candidate.Claim.PromotionStatus != architecture.PromotionCandidate {
		errs = append(errs, "repeated deviation candidate must remain human-review-required and candidate-only")
	}
	if candidate.Claim.AssertionOrigin != architecture.OriginDerived || candidate.Claim.InferenceRule != repeatedDeviationInferenceRule {
		errs = append(errs, "repeated deviation candidate requires the canonical derived inference rule")
	}
	if len(candidate.ReceiptIDs) < DefaultMinimumOccurrences {
		errs = append(errs, "repeated deviation candidate requires at least two receipt ids")
	}
	actualDigest, digestErr := CandidateDigest(candidate)
	if digestErr != nil {
		errs = append(errs, digestErr.Error())
	} else if candidate.SemanticDigestSHA256 != actualDigest {
		errs = append(errs, "deviation candidate semantic digest mismatch")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func CandidateDigest(in Candidate) (string, error) {
	candidate, err := normalizeCandidate(in)
	if err != nil {
		return "", err
	}
	candidate.SemanticDigestSHA256 = ""
	data, err := json.Marshal(candidate)
	if err != nil {
		return "", err
	}
	return sha256Bytes(data), nil
}

func expectedCandidateID(in Candidate) string {
	candidate := in
	candidate.ID = ""
	candidate.SemanticDigestSHA256 = ""
	parts := []string{candidate.PatternID, string(candidate.Kind), candidate.Claim.ID}
	return "deviation_candidate." + sha256String(strings.Join(parts, "\x00"))[:24]
}

func candidateShape(kind Kind) (CandidateKind, string, string) {
	switch kind {
	case KindUndeclaredParameter:
		return CandidateContract, "requires_declared_parameter_contract", "Repeated undeclared parameter candidate"
	case KindBypassedOwnerPath:
		return CandidateOwner, "requires_review_of_owner_path", "Repeated owner-path bypass candidate"
	case KindRepeatedLocking:
		return CandidateFailureMode, "requires_explicit_concurrency_contract", "Repeated locking failure-mode candidate"
	case KindRepeatedEscapeHatch:
		return CandidateGovernanceDebt, "requires_governed_exception_contract", "Repeated escape-hatch governance candidate"
	case KindUnsatisfiedBoundary:
		return CandidateBoundary, "contests_boundary_satisfiability", "Repeated boundary-friction candidate"
	case KindMissingState:
		return CandidateContract, "requires_declared_state_contract", "Repeated missing-state candidate"
	default:
		return CandidateGovernanceDebt, "requires_governed_review", "Repeated deviation candidate"
	}
}

// ValidateAnalysis checks every semantic index and exact outer digest.
func ValidateAnalysis(in Analysis) error {
	analysis, err := normalizeAnalysis(in)
	if err != nil {
		return err
	}
	var errs []string
	if analysis.SchemaVersion != SchemaVersion || analysis.GeneratedBy != GeneratedBy {
		errs = append(errs, "analysis schema and generator must be exact")
	}
	if err := validateExactBinding(analysis.Binding); err != nil {
		errs = append(errs, err.Error())
	}
	if analysis.MinimumIndependentOccurrences < 2 {
		errs = append(errs, "analysis threshold must be at least two")
	}
	patternsByID := map[string]Pattern{}
	for _, pattern := range analysis.Patterns {
		patternsByID[pattern.ID] = pattern
	}
	for _, candidate := range analysis.Candidates {
		pattern, ok := patternsByID[candidate.PatternID]
		if !ok {
			errs = append(errs, "candidate references missing deviation pattern")
			continue
		}
		if !pattern.CandidateEligible {
			errs = append(errs, "ineligible deviation pattern produced a candidate")
		}
		if !reflect.DeepEqual(candidate.ReceiptIDs, pattern.ReceiptIDs) {
			errs = append(errs, "candidate receipt ids must exactly match its pattern")
		}
	}
	expectedReceipt := buildRunReceipt(analysis)
	expectedReceipt.ExactAnalysisDigestSHA256 = analysis.Receipt.ExactAnalysisDigestSHA256
	if !reflect.DeepEqual(analysis.Receipt, expectedReceipt) {
		errs = append(errs, "analysis run receipt semantic indexes are incomplete or mismatched")
	}
	actualDigest, digestErr := AnalysisDigest(analysis)
	if digestErr != nil {
		errs = append(errs, digestErr.Error())
	} else if analysis.Receipt.ExactAnalysisDigestSHA256 == "" || analysis.Receipt.ExactAnalysisDigestSHA256 != actualDigest {
		errs = append(errs, "analysis exact digest mismatch")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// AnalysisDigest hashes the normalized analysis with only its self-reference cleared.
func AnalysisDigest(in Analysis) (string, error) {
	analysis, err := normalizeAnalysis(in)
	if err != nil {
		return "", err
	}
	analysis.Receipt.ExactAnalysisDigestSHA256 = ""
	data, err := json.Marshal(analysis)
	if err != nil {
		return "", err
	}
	return sha256Bytes(data), nil
}

func buildRunReceipt(analysis Analysis) RunReceipt {
	receipt := RunReceipt{
		SchemaVersion:                 SchemaVersion,
		GeneratedBy:                   GeneratedBy,
		Binding:                       canonicalizeBinding(analysis.Binding),
		RulesetVersion:                RulesetVersion,
		MinimumIndependentOccurrences: analysis.MinimumIndependentOccurrences,
		ReceiptIDsAndDigests:          map[string]string{},
		PatternIDsAndDigests:          map[string]string{},
		CandidateIDsAndDigests:        map[string]string{},
		NondeterminismDeclaration:     NondeterminismNone,
	}
	for _, item := range analysis.Receipts {
		receipt.ReceiptIDsAndDigests[item.ID] = item.SemanticDigestSHA256
	}
	for _, item := range analysis.Patterns {
		receipt.PatternIDsAndDigests[item.ID] = item.SemanticDigestSHA256
	}
	for _, item := range analysis.Candidates {
		receipt.CandidateIDsAndDigests[item.ID] = item.SemanticDigestSHA256
	}
	return receipt
}

func normalizeAnalysis(in Analysis) (Analysis, error) {
	analysis := in
	analysis.SchemaVersion = strings.TrimSpace(analysis.SchemaVersion)
	analysis.GeneratedBy = strings.TrimSpace(analysis.GeneratedBy)
	analysis.Binding = canonicalizeBinding(analysis.Binding)
	receipts, err := NormalizeReceipts(analysis.Receipts)
	if err != nil {
		return Analysis{}, err
	}
	analysis.Receipts = receipts
	patterns := make([]Pattern, 0, len(analysis.Patterns))
	seenPatterns := map[string]bool{}
	for _, item := range analysis.Patterns {
		item = canonicalizePattern(item)
		if err := ValidatePattern(item); err != nil {
			return Analysis{}, err
		}
		if seenPatterns[item.ID] {
			return Analysis{}, fmt.Errorf("duplicate deviation pattern %s", item.ID)
		}
		seenPatterns[item.ID] = true
		patterns = append(patterns, item)
	}
	sort.SliceStable(patterns, func(i, j int) bool { return patterns[i].ID < patterns[j].ID })
	analysis.Patterns = patterns
	candidates := make([]Candidate, 0, len(analysis.Candidates))
	seenCandidates := map[string]bool{}
	for _, item := range analysis.Candidates {
		item, err = normalizeCandidate(item)
		if err != nil {
			return Analysis{}, err
		}
		if err := ValidateCandidate(item); err != nil {
			return Analysis{}, err
		}
		if seenCandidates[item.ID] {
			return Analysis{}, fmt.Errorf("duplicate deviation candidate %s", item.ID)
		}
		seenCandidates[item.ID] = true
		candidates = append(candidates, item)
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	analysis.Candidates = candidates
	analysis.Receipt.SchemaVersion = strings.TrimSpace(analysis.Receipt.SchemaVersion)
	analysis.Receipt.GeneratedBy = strings.TrimSpace(analysis.Receipt.GeneratedBy)
	analysis.Receipt.Binding = canonicalizeBinding(analysis.Receipt.Binding)
	analysis.Receipt.RulesetVersion = strings.TrimSpace(analysis.Receipt.RulesetVersion)
	analysis.Receipt.ExactAnalysisDigestSHA256 = strings.TrimSpace(analysis.Receipt.ExactAnalysisDigestSHA256)
	analysis.Receipt.NondeterminismDeclaration = strings.TrimSpace(analysis.Receipt.NondeterminismDeclaration)
	if analysis.Receipt.ReceiptIDsAndDigests == nil {
		analysis.Receipt.ReceiptIDsAndDigests = map[string]string{}
	}
	if analysis.Receipt.PatternIDsAndDigests == nil {
		analysis.Receipt.PatternIDsAndDigests = map[string]string{}
	}
	if analysis.Receipt.CandidateIDsAndDigests == nil {
		analysis.Receipt.CandidateIDsAndDigests = map[string]string{}
	}
	return analysis, nil
}

func normalizeCandidate(in Candidate) (Candidate, error) {
	candidate := in
	candidate.ID = strings.TrimSpace(candidate.ID)
	candidate.PatternID = strings.TrimSpace(candidate.PatternID)
	candidate.ReceiptIDs = cleanStrings(candidate.ReceiptIDs)
	candidate.SemanticDigestSHA256 = strings.TrimSpace(candidate.SemanticDigestSHA256)
	claims, err := architecture.NormalizeClaims([]architecture.Claim{candidate.Claim})
	if err != nil {
		return Candidate{}, err
	}
	candidate.Claim = claims[0]
	return candidate, nil
}
