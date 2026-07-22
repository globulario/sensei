// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

// RankingFactorKind defines closed ranking criteria.
type RankingFactorKind string

const (
	FactorBlastRadius               RankingFactorKind = "blast_radius"
	FactorAuthoritySensitivity      RankingFactorKind = "authority_sensitivity"
	FactorContradictionDensity      RankingFactorKind = "contradiction_density"
	FactorIncidentRecurrence        RankingFactorKind = "incident_recurrence"
	FactorTaskRelevance             RankingFactorKind = "task_relevance"
	FactorRuntimeFrequency          RankingFactorKind = "runtime_frequency"
	FactorEvidenceIndependence      RankingFactorKind = "evidence_independence"
	FactorMissingEvidenceCost       RankingFactorKind = "missing_evidence_cost"
	FactorFutureAgentErrorReduction RankingFactorKind = "future_agent_error_reduction"
)

// IsValidRankingFactorKind validates factor kinds.
func IsValidRankingFactorKind(k RankingFactorKind) bool {
	switch k {
	case FactorBlastRadius, FactorAuthoritySensitivity, FactorContradictionDensity, FactorIncidentRecurrence,
		FactorTaskRelevance, FactorRuntimeFrequency, FactorEvidenceIndependence, FactorMissingEvidenceCost,
		FactorFutureAgentErrorReduction:
		return true
	default:
		return false
	}
}

// RankingFactor represents one dimension of candidate ranking score.
type RankingFactor struct {
	Kind           RankingFactorKind `json:"kind" yaml:"kind"`
	Value          int               `json:"value" yaml:"value"`
	EvidenceRefIDs []string          `json:"evidence_ref_ids,omitempty" yaml:"evidence_ref_ids,omitempty"`
}

// RankingRecord keeps advisory ranking separate from candidate claim authority.
type RankingRecord struct {
	CandidateID string `json:"candidate_id" yaml:"candidate_id"`
	Rank        int    `json:"rank" yaml:"rank"`
	Score       int    `json:"score" yaml:"score"`

	Factors []RankingFactor `json:"factors" yaml:"factors"`
}

const (
	ChallengeStrategyVersion = "challenge.bound-evidence.v1"
	CounterexampleStrategy   = "counterexample.single-refuting-receipt.v1"

	ChallengeReasonRefutingEvidence = "refuting_evidence_present"
	ChallengeReasonEvidenceMissing  = "required_evidence_missing"
	ChallengeReasonNoCounterexample = "no_counterexample_in_bound_coverage"
)

func materializeDrafts(
	drafts []candidateDraft,
	why investigation.Document,
	allEvidence []investigation.EvidenceReceipt,
	binding Binding,
) (
	[]architecture.Claim,
	[]CandidateEnvelope,
	[]EvidenceRequest,
	[]ChallengeReceipt,
	[]CounterexampleRecord,
	[]RankingRecord,
	error,
) {
	var claims []architecture.Claim
	var envelopes []CandidateEnvelope
	var requests []EvidenceRequest
	var challenges []ChallengeReceipt
	var counterexamples []CounterexampleRecord

	evidenceByID := make(map[string]investigation.EvidenceReceipt, len(allEvidence))
	providerByEvidenceID := make(map[string]string, len(allEvidence))
	for _, receipt := range allEvidence {
		evidenceByID[receipt.ID] = receipt
		providerByEvidenceID[receipt.ID] = receipt.Provider.ID
	}

	for _, draft := range drafts {
		proposition := strings.Join([]string{
			draft.Claim.Statement.Subject,
			draft.Claim.Statement.Predicate,
			draft.Claim.Statement.Object,
		}, "\x00")
		candidateID, _, err := ComputeCandidateID(
			ComposerSchemaVersion,
			binding,
			proposition,
			draft.Claim.Scope,
			draft.Kind,
			binding.GeneratorVersion,
		)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, err
		}

		envelope := CandidateEnvelope{
			CandidateID:               candidateID,
			ClaimID:                   draft.Claim.ID,
			OutputKind:                draft.Kind,
			ObservationRefIDs:         sortedUnique(draft.ObservationRefIDs),
			SupportingEvidenceRefIDs:  sortedUnique(draft.SupportingEvidenceRefIDs),
			RefutingEvidenceRefIDs:    sortedUnique(draft.RefutingEvidenceRefIDs),
			FalsificationConditions:   sortedUnique(draft.FalsificationConditions),
			MissingEvidenceRequestIDs: nil,
			ConfidenceBasis: []ConfidenceFactor{
				{Metric: "supporting_evidence_count", Value: len(draft.SupportingEvidenceRefIDs)},
				{Metric: "refuting_evidence_count", Value: len(draft.RefutingEvidenceRefIDs)},
				{Metric: "observation_count", Value: len(draft.ObservationRefIDs)},
			},
		}

		if !coverageExecuted(why.Coverage, draft.MissingEvidenceCategory) {
			request, requestErr := buildEvidenceRequest(envelope, draft, why.Coverage)
			if requestErr != nil {
				return nil, nil, nil, nil, nil, nil, requestErr
			}
			requests = append(requests, request)
			envelope.MissingEvidenceRequestIDs = []string{request.ID}
		}

		candidateCounterexamples := buildCounterexamples(envelope, draft.Claim, evidenceByID)
		counterexamples = append(counterexamples, candidateCounterexamples...)
		challenges = append(challenges, buildChallenge(envelope, candidateCounterexamples))
		claims = append(claims, draft.Claim)
		envelopes = append(envelopes, envelope)
	}

	sort.SliceStable(claims, func(i, j int) bool { return claims[i].ID < claims[j].ID })
	sort.SliceStable(envelopes, func(i, j int) bool { return envelopes[i].CandidateID < envelopes[j].CandidateID })
	sort.SliceStable(requests, func(i, j int) bool { return requests[i].ID < requests[j].ID })
	sort.SliceStable(challenges, func(i, j int) bool { return challenges[i].ID < challenges[j].ID })
	sort.SliceStable(counterexamples, func(i, j int) bool {
		return counterexamples[i].Counterexample.ID < counterexamples[j].Counterexample.ID
	})

	rankings := buildRankings(envelopes, providerByEvidenceID)
	return claims, envelopes, requests, challenges, counterexamples, rankings, nil
}

func buildEvidenceRequest(envelope CandidateEnvelope, draft candidateDraft, coverage []investigation.CoverageEntry) (EvidenceRequest, error) {
	descriptor := strings.Join([]string{
		envelope.CandidateID,
		string(draft.MissingEvidenceCategory),
		draft.MissingEvidenceReason,
	}, "\x00")
	id := "evidence_request_" + SHA256String(descriptor)[:24]
	request := EvidenceRequest{
		ID:                    id,
		CandidateID:           envelope.CandidateID,
		Category:              draft.MissingEvidenceCategory,
		Scope:                 draft.Claim.Scope,
		ReasonCode:            draft.MissingEvidenceReason,
		Description:           fmt.Sprintf("Obtain %s evidence capable of supporting or refuting candidate %s.", draft.MissingEvidenceCategory, envelope.CandidateID),
		RequiredProofStrength: draft.RequiredProofStrength,
		ExistingCoverageRefIDs: coverageReferences(
			coverage,
			draft.MissingEvidenceCategory,
		),
	}
	if !IsValidEvidenceRequestReason(request.ReasonCode) {
		return EvidenceRequest{}, fmt.Errorf("invalid evidence request reason %q", request.ReasonCode)
	}
	return request, nil
}

func coverageExecuted(entries []investigation.CoverageEntry, category investigation.EvidenceCategory) bool {
	for _, entry := range entries {
		if entry.Category != category {
			continue
		}
		switch entry.Status {
		case investigation.CoverageSupporting,
			investigation.CoverageRefuting,
			investigation.CoverageMixed,
			investigation.CoverageNoResult:
			return true
		}
	}
	return false
}

func coverageReferences(entries []investigation.CoverageEntry, category investigation.EvidenceCategory) []string {
	var refs []string
	for _, entry := range entries {
		if entry.Category != category {
			continue
		}
		refs = append(refs, entry.ProviderID+":"+entry.TargetDigestSHA256)
	}
	return sortedUnique(refs)
}

func buildCounterexamples(
	envelope CandidateEnvelope,
	claim architecture.Claim,
	evidenceByID map[string]investigation.EvidenceReceipt,
) []CounterexampleRecord {
	var out []CounterexampleRecord
	for _, evidenceID := range envelope.RefutingEvidenceRefIDs {
		receipt, ok := evidenceByID[evidenceID]
		if !ok {
			continue
		}
		scope := intersectScope(claim.Scope, receipt.Scope)
		id := "counterexample_" + SHA256String(envelope.CandidateID+"\x00"+evidenceID)[:24]
		out = append(out, CounterexampleRecord{
			Counterexample: investigation.Counterexample{
				ID:             id,
				ClaimID:        claim.ID,
				Description:    fmt.Sprintf("Bound refuting evidence %s contests candidate %s.", evidenceID, envelope.CandidateID),
				EvidenceRefIDs: []string{evidenceID},
				Scope:          scope,
			},
			StrategyVersion: CounterexampleStrategy,
			MinimalityBasis: "one refuting evidence receipt is sufficient to contest this candidate",
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Counterexample.ID < out[j].Counterexample.ID
	})
	return out
}

func buildChallenge(envelope CandidateEnvelope, counterexamples []CounterexampleRecord) ChallengeReceipt {
	status := ChallengeSurvived
	reason := ChallengeReasonNoCounterexample
	if len(envelope.RefutingEvidenceRefIDs) > 0 {
		status = ChallengeContested
		reason = ChallengeReasonRefutingEvidence
	} else if len(envelope.MissingEvidenceRequestIDs) > 0 {
		status = ChallengeInsufficientEvidence
		reason = ChallengeReasonEvidenceMissing
	}

	counterexampleIDs := make([]string, 0, len(counterexamples))
	for _, record := range counterexamples {
		counterexampleIDs = append(counterexampleIDs, record.Counterexample.ID)
	}
	idDescriptor := strings.Join([]string{
		envelope.CandidateID,
		ChallengeStrategyVersion,
		string(status),
		reason,
	}, "\x00")
	return ChallengeReceipt{
		ID:                       "challenge_" + SHA256String(idDescriptor)[:24],
		CandidateID:              envelope.CandidateID,
		StrategyVersion:          ChallengeStrategyVersion,
		Status:                   status,
		ReasonCode:               reason,
		SupportingEvidenceRefIDs: sortedUnique(envelope.SupportingEvidenceRefIDs),
		RefutingEvidenceRefIDs:   sortedUnique(envelope.RefutingEvidenceRefIDs),
		CounterexampleIDs:        sortedUnique(counterexampleIDs),
		EvidenceRequestIDs:       sortedUnique(envelope.MissingEvidenceRequestIDs),
	}
}

func buildRankings(envelopes []CandidateEnvelope, providerByEvidenceID map[string]string) []RankingRecord {
	type scored struct {
		record RankingRecord
	}
	items := make([]scored, 0, len(envelopes))
	for _, envelope := range envelopes {
		blastRadius := len(envelope.ObservationRefIDs) + len(envelope.SupportingEvidenceRefIDs) + len(envelope.RefutingEvidenceRefIDs)
		authoritySensitivity := authoritySensitivity(envelope.OutputKind)
		contradictionDensity := len(envelope.RefutingEvidenceRefIDs) * 20
		evidenceIndependence := uniqueProviderCount(
			append(append([]string(nil), envelope.SupportingEvidenceRefIDs...), envelope.RefutingEvidenceRefIDs...),
			providerByEvidenceID,
		) * 10
		missingEvidenceCost := len(envelope.MissingEvidenceRequestIDs) * 5
		futureAgentErrorReduction := futureAgentErrorReduction(envelope.OutputKind)
		score := blastRadius + authoritySensitivity + contradictionDensity + evidenceIndependence + futureAgentErrorReduction - missingEvidenceCost
		factors := []RankingFactor{
			{Kind: FactorBlastRadius, Value: blastRadius},
			{Kind: FactorAuthoritySensitivity, Value: authoritySensitivity},
			{Kind: FactorContradictionDensity, Value: contradictionDensity, EvidenceRefIDs: sortedUnique(envelope.RefutingEvidenceRefIDs)},
			{Kind: FactorEvidenceIndependence, Value: evidenceIndependence, EvidenceRefIDs: sortedUnique(append(append([]string(nil), envelope.SupportingEvidenceRefIDs...), envelope.RefutingEvidenceRefIDs...))},
			{Kind: FactorMissingEvidenceCost, Value: -missingEvidenceCost},
			{Kind: FactorFutureAgentErrorReduction, Value: futureAgentErrorReduction},
		}
		items = append(items, scored{record: RankingRecord{
			CandidateID: envelope.CandidateID,
			Score:       score,
			Factors:     factors,
		}})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].record.Score != items[j].record.Score {
			return items[i].record.Score > items[j].record.Score
		}
		return items[i].record.CandidateID < items[j].record.CandidateID
	})
	out := make([]RankingRecord, len(items))
	for i := range items {
		items[i].record.Rank = i + 1
		out[i] = items[i].record
	}
	return out
}

func authoritySensitivity(kind CandidateKind) int {
	switch kind {
	case KindOwner:
		return 30
	case KindBoundary:
		return 20
	case KindContract:
		return 15
	case KindInvariant, KindFailureMode:
		return 10
	default:
		return 5
	}
}

func futureAgentErrorReduction(kind CandidateKind) int {
	switch kind {
	case KindOwner, KindBoundary, KindContract:
		return 20
	case KindInvariant, KindFailureMode:
		return 15
	default:
		return 5
	}
}

func uniqueProviderCount(evidenceIDs []string, providerByEvidenceID map[string]string) int {
	seen := map[string]bool{}
	for _, evidenceID := range evidenceIDs {
		if provider := providerByEvidenceID[evidenceID]; provider != "" {
			seen[provider] = true
		}
	}
	return len(seen)
}

func intersectScope(left, right architecture.ClaimScope) architecture.ClaimScope {
	repository := left.Repository
	if repository == "" {
		repository = left.Repo
	}
	return architecture.ClaimScope{
		Repository: repository,
		Files:      intersection(left.Files, right.Files),
		Symbols:    intersection(left.Symbols, right.Symbols),
		Components: intersection(left.Components, right.Components),
	}
}

func intersection(left, right []string) []string {
	var out []string
	for _, value := range left {
		if containsString(right, value) {
			out = append(out, value)
		}
	}
	return sortedUnique(out)
}
