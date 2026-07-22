// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
)

// ChallengeStatus represents the closed status vocabulary for a challenge.
type ChallengeStatus string

const (
	ChallengePending              ChallengeStatus = "pending"
	ChallengeSurvived             ChallengeStatus = "survived"
	ChallengeContested            ChallengeStatus = "contested"
	ChallengeRefuted              ChallengeStatus = "refuted"
	ChallengeInsufficientEvidence ChallengeStatus = "insufficient_evidence"
)

// IsValidChallengeStatus checks if status is a valid vocabulary term.
func IsValidChallengeStatus(s ChallengeStatus) bool {
	switch s {
	case ChallengePending, ChallengeSurvived, ChallengeContested, ChallengeRefuted, ChallengeInsufficientEvidence:
		return true
	default:
		return false
	}
}

// IsValidChallengeReasonCode checks the deterministic challenge reason vocabulary.
func IsValidChallengeReasonCode(reason string) bool {
	switch reason {
	case ChallengeReasonRefutingEvidence, ChallengeReasonEvidenceMissing, ChallengeReasonNoCounterexample:
		return true
	default:
		return false
	}
}

// ChallengeReceipt documents the result of an adversarial challenge.
type ChallengeReceipt struct {
	ID          string `json:"id" yaml:"id"`
	CandidateID string `json:"candidate_id" yaml:"candidate_id"`

	StrategyVersion string          `json:"strategy_version" yaml:"strategy_version"`
	Status          ChallengeStatus `json:"status" yaml:"status"`
	ReasonCode      string          `json:"reason_code" yaml:"reason_code"`

	SupportingEvidenceRefIDs []string `json:"supporting_evidence_ref_ids,omitempty" yaml:"supporting_evidence_ref_ids,omitempty"`
	RefutingEvidenceRefIDs   []string `json:"refuting_evidence_ref_ids,omitempty" yaml:"refuting_evidence_ref_ids,omitempty"`
	CounterexampleIDs        []string `json:"counterexample_ids,omitempty" yaml:"counterexample_ids,omitempty"`
	EvidenceRequestIDs       []string `json:"evidence_request_ids,omitempty" yaml:"evidence_request_ids,omitempty"`
}

func validateDeterministicSidecars(res Result) error {
	var errs []string

	claimsByID := make(map[string]architecture.Claim, len(res.Document.CandidateClaims))
	for _, claim := range res.Document.CandidateClaims {
		claimsByID[claim.ID] = claim
		if claim.Confidence != 0 {
			errs = append(errs, fmt.Sprintf("candidate claim %s confidence must remain ranking metadata only", claim.ID))
		}
	}

	candidatesByID := make(map[string]CandidateEnvelope, len(res.Candidates))
	for _, envelope := range res.Candidates {
		candidatesByID[envelope.CandidateID] = envelope
		claim, ok := claimsByID[envelope.ClaimID]
		if !ok {
			continue
		}
		proposition := strings.Join([]string{
			claim.Statement.Subject,
			claim.Statement.Predicate,
			claim.Statement.Object,
		}, "\x00")
		expectedID, _, err := ComputeCandidateID(
			res.SchemaVersion,
			res.Binding,
			proposition,
			claim.Scope,
			envelope.OutputKind,
			res.Binding.GeneratorVersion,
		)
		if err != nil {
			errs = append(errs, fmt.Sprintf("candidate %s identity recomputation failed: %v", envelope.CandidateID, err))
		} else if envelope.CandidateID != expectedID {
			errs = append(errs, fmt.Sprintf("candidate %s identity does not match its bound proposition", envelope.CandidateID))
		}
	}

	requestsByID := make(map[string]EvidenceRequest, len(res.EvidenceRequests))
	for _, request := range res.EvidenceRequests {
		requestsByID[request.ID] = request
		candidate, ok := candidatesByID[request.CandidateID]
		if !ok {
			continue
		}
		claim, ok := claimsByID[candidate.ClaimID]
		if !ok {
			continue
		}
		errs = append(errs, scopeSubsetErrors("evidence request "+request.ID, request.Scope, claim.Scope)...)
	}
	for _, envelope := range res.Candidates {
		for _, requestID := range envelope.MissingEvidenceRequestIDs {
			if _, ok := requestsByID[requestID]; !ok {
				errs = append(errs, fmt.Sprintf("candidate %s references missing evidence request %s", envelope.CandidateID, requestID))
			}
		}
	}

	for _, challenge := range res.Challenges {
		if !IsValidChallengeReasonCode(challenge.ReasonCode) {
			errs = append(errs, fmt.Sprintf("challenge %s has unknown reason code %q", challenge.ID, challenge.ReasonCode))
		}
	}

	for _, record := range res.Counterexamples {
		claim, ok := claimsByID[record.Counterexample.ClaimID]
		if !ok {
			continue
		}
		errs = append(errs, scopeSubsetErrors("counterexample "+record.Counterexample.ID, record.Counterexample.Scope, claim.Scope)...)
	}

	errs = append(errs, validateRankingRecords(res.Rankings, candidatesByID)...)
	if len(res.Receipt.ResourceLimits) == 0 {
		errs = append(errs, "receipt resource limits are required")
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func scopeSubsetErrors(name string, child, parent architecture.ClaimScope) []string {
	var errs []string
	for _, file := range child.Files {
		if !containsString(parent.Files, file) {
			errs = append(errs, fmt.Sprintf("%s file %q exceeds candidate scope", name, file))
		}
	}
	for _, symbol := range child.Symbols {
		if !containsString(parent.Symbols, symbol) {
			errs = append(errs, fmt.Sprintf("%s symbol %q exceeds candidate scope", name, symbol))
		}
	}
	for _, component := range child.Components {
		if !containsString(parent.Components, component) {
			errs = append(errs, fmt.Sprintf("%s component %q exceeds candidate scope", name, component))
		}
	}
	parentRepository := parent.Repository
	if parentRepository == "" {
		parentRepository = parent.Repo
	}
	childRepository := child.Repository
	if childRepository == "" {
		childRepository = child.Repo
	}
	if childRepository != "" && childRepository != parentRepository {
		errs = append(errs, fmt.Sprintf("%s repository %q exceeds candidate repository %q", name, childRepository, parentRepository))
	}
	if child.Domain != "" && child.Domain != parent.Domain {
		errs = append(errs, fmt.Sprintf("%s domain %q exceeds candidate domain %q", name, child.Domain, parent.Domain))
	}
	if child.SourceSet != "" && child.SourceSet != parent.SourceSet {
		errs = append(errs, fmt.Sprintf("%s source set %q exceeds candidate source set %q", name, child.SourceSet, parent.SourceSet))
	}
	return errs
}

func validateRankingRecords(rankings []RankingRecord, candidates map[string]CandidateEnvelope) []string {
	var errs []string
	seenRanks := map[int]bool{}
	for _, ranking := range rankings {
		if _, ok := candidates[ranking.CandidateID]; !ok {
			continue
		}
		if seenRanks[ranking.Rank] {
			errs = append(errs, fmt.Sprintf("ranking rank %d is duplicated", ranking.Rank))
		}
		seenRanks[ranking.Rank] = true
		sum := 0
		for _, factor := range ranking.Factors {
			sum += factor.Value
		}
		if ranking.Score != sum {
			errs = append(errs, fmt.Sprintf("ranking %s score %d does not equal factor sum %d", ranking.CandidateID, ranking.Score, sum))
		}
	}
	if len(rankings) != len(candidates) {
		errs = append(errs, fmt.Sprintf("ranking count %d must equal candidate count %d", len(rankings), len(candidates)))
	}

	expected := append([]RankingRecord(nil), rankings...)
	sort.SliceStable(expected, func(i, j int) bool {
		if expected[i].Score != expected[j].Score {
			return expected[i].Score > expected[j].Score
		}
		return expected[i].CandidateID < expected[j].CandidateID
	})
	for index, ranking := range expected {
		if ranking.Rank != index+1 {
			errs = append(errs, fmt.Sprintf("ranking %s has rank %d, expected %d from deterministic ordering", ranking.CandidateID, ranking.Rank, index+1))
		}
	}
	return errs
}
