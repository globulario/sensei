// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

// Normalize performs idempotent normalization of the Result document and its subcomponents.
func Normalize(res Result) (Result, error) {
	// 1. Normalize inner Document using investigation package's canonical Normalizer
	normDoc, err := investigation.Normalize(res.Document)
	if err != nil {
		return Result{}, err
	}
	res.Document = normDoc

	// 2. Normalize Candidates
	normalizedCandidates := make([]CandidateEnvelope, len(res.Candidates))
	for i, c := range res.Candidates {
		obs := append([]string(nil), c.ObservationRefIDs...)
		sort.Strings(obs)
		sup := append([]string(nil), c.SupportingEvidenceRefIDs...)
		sort.Strings(sup)
		ref := append([]string(nil), c.RefutingEvidenceRefIDs...)
		sort.Strings(ref)
		fals := append([]string(nil), c.FalsificationConditions...)
		sort.Strings(fals)
		miss := append([]string(nil), c.MissingEvidenceRequestIDs...)
		sort.Strings(miss)

		normalizedCandidates[i] = CandidateEnvelope{
			CandidateID:               strings.TrimSpace(c.CandidateID),
			ClaimID:                   strings.TrimSpace(c.ClaimID),
			OutputKind:                c.OutputKind,
			ObservationRefIDs:         obs,
			SupportingEvidenceRefIDs:  sup,
			RefutingEvidenceRefIDs:    ref,
			FalsificationConditions:   fals,
			MissingEvidenceRequestIDs: miss,
			ConfidenceBasis:           append([]ConfidenceFactor(nil), c.ConfidenceBasis...),
		}
	}
	sort.SliceStable(normalizedCandidates, func(i, j int) bool {
		return normalizedCandidates[i].CandidateID < normalizedCandidates[j].CandidateID
	})
	res.Candidates = normalizedCandidates

	// 3. Normalize Challenges
	normalizedChallenges := make([]ChallengeReceipt, len(res.Challenges))
	for i, c := range res.Challenges {
		sup := append([]string(nil), c.SupportingEvidenceRefIDs...)
		sort.Strings(sup)
		ref := append([]string(nil), c.RefutingEvidenceRefIDs...)
		sort.Strings(ref)
		ce := append([]string(nil), c.CounterexampleIDs...)
		sort.Strings(ce)
		er := append([]string(nil), c.EvidenceRequestIDs...)
		sort.Strings(er)

		normalizedChallenges[i] = ChallengeReceipt{
			ID:                       strings.TrimSpace(c.ID),
			CandidateID:              strings.TrimSpace(c.CandidateID),
			StrategyVersion:          strings.TrimSpace(c.StrategyVersion),
			Status:                   c.Status,
			ReasonCode:               strings.TrimSpace(c.ReasonCode),
			SupportingEvidenceRefIDs: sup,
			RefutingEvidenceRefIDs:   ref,
			CounterexampleIDs:        ce,
			EvidenceRequestIDs:       er,
		}
	}
	sort.SliceStable(normalizedChallenges, func(i, j int) bool {
		return normalizedChallenges[i].ID < normalizedChallenges[j].ID
	})
	res.Challenges = normalizedChallenges

	// 4. Normalize EvidenceRequests
	normalizedReqs := make([]EvidenceRequest, len(res.EvidenceRequests))
	for i, r := range res.EvidenceRequests {
		cov := append([]string(nil), r.ExistingCoverageRefIDs...)
		sort.Strings(cov)

		// Normalize scope files in place
		sortedFiles := append([]string(nil), r.Scope.Files...)
		for j, f := range sortedFiles {
			sortedFiles[j] = filepath.Clean(strings.TrimSpace(f))
		}
		sort.Strings(sortedFiles)

		sortedSymbols := append([]string(nil), r.Scope.Symbols...)
		sort.Strings(sortedSymbols)

		sortedComponents := append([]string(nil), r.Scope.Components...)
		sort.Strings(sortedComponents)

		normalizedReqs[i] = EvidenceRequest{
			ID:          strings.TrimSpace(r.ID),
			CandidateID: strings.TrimSpace(r.CandidateID),
			Category:    r.Category,
			Scope: investorClaimScope(
				strings.TrimSpace(r.Scope.Repository),
				strings.TrimSpace(r.Scope.Repo),
				strings.TrimSpace(r.Scope.Domain),
				strings.TrimSpace(r.Scope.SourceSet),
				sortedFiles,
				sortedSymbols,
				sortedComponents,
			),
			ReasonCode:             strings.TrimSpace(r.ReasonCode),
			Description:            strings.TrimSpace(r.Description),
			RequiredProofStrength:  r.RequiredProofStrength,
			ExistingCoverageRefIDs: cov,
		}
	}
	sort.SliceStable(normalizedReqs, func(i, j int) bool {
		return normalizedReqs[i].ID < normalizedReqs[j].ID
	})
	res.EvidenceRequests = normalizedReqs

	// 5. Normalize Rankings
	normalizedRankings := make([]RankingRecord, len(res.Rankings))
	for i, r := range res.Rankings {
		factors := make([]RankingFactor, len(r.Factors))
		for j, f := range r.Factors {
			evs := append([]string(nil), f.EvidenceRefIDs...)
			sort.Strings(evs)
			factors[j] = RankingFactor{
				Kind:           f.Kind,
				Value:          f.Value,
				EvidenceRefIDs: evs,
			}
		}
		sort.SliceStable(factors, func(x, y int) bool {
			return factors[x].Kind < factors[y].Kind
		})

		normalizedRankings[i] = RankingRecord{
			CandidateID: strings.TrimSpace(r.CandidateID),
			Rank:        r.Rank,
			Score:       r.Score,
			Factors:     factors,
		}
	}
	sort.SliceStable(normalizedRankings, func(i, j int) bool {
		return normalizedRankings[i].CandidateID < normalizedRankings[j].CandidateID
	})
	res.Rankings = normalizedRankings

	// 6. Normalize Counterexamples
	normalizedCounterexamples := make([]CounterexampleRecord, len(res.Counterexamples))
	for i, r := range res.Counterexamples {
		evs := append([]string(nil), r.Counterexample.EvidenceRefIDs...)
		sort.Strings(evs)

		sortedFiles := append([]string(nil), r.Counterexample.Scope.Files...)
		for j, f := range sortedFiles {
			sortedFiles[j] = filepath.Clean(strings.TrimSpace(f))
		}
		sort.Strings(sortedFiles)

		sortedSymbols := append([]string(nil), r.Counterexample.Scope.Symbols...)
		sort.Strings(sortedSymbols)

		sortedComponents := append([]string(nil), r.Counterexample.Scope.Components...)
		sort.Strings(sortedComponents)

		normalizedCounterexamples[i] = CounterexampleRecord{
			Counterexample: investigation.Counterexample{
				ID:             strings.TrimSpace(r.Counterexample.ID),
				ClaimID:        strings.TrimSpace(r.Counterexample.ClaimID),
				Description:    strings.TrimSpace(r.Counterexample.Description),
				EvidenceRefIDs: evs,
				Scope: investorClaimScope(
					strings.TrimSpace(r.Counterexample.Scope.Repository),
					strings.TrimSpace(r.Counterexample.Scope.Repo),
					strings.TrimSpace(r.Counterexample.Scope.Domain),
					strings.TrimSpace(r.Counterexample.Scope.SourceSet),
					sortedFiles,
					sortedSymbols,
					sortedComponents,
				),
			},
			StrategyVersion: strings.TrimSpace(r.StrategyVersion),
			MinimalityBasis: strings.TrimSpace(r.MinimalityBasis),
		}
	}
	sort.SliceStable(normalizedCounterexamples, func(i, j int) bool {
		return normalizedCounterexamples[i].Counterexample.ID < normalizedCounterexamples[j].Counterexample.ID
	})
	res.Counterexamples = normalizedCounterexamples

	// 7. Normalize Binding strings
	res.Binding.HowDocumentDigestSHA256 = strings.TrimSpace(res.Binding.HowDocumentDigestSHA256)
	res.Binding.WhyDocumentDigestSHA256 = strings.TrimSpace(res.Binding.WhyDocumentDigestSHA256)
	res.Binding.GraphDigestSHA256 = strings.TrimSpace(res.Binding.GraphDigestSHA256)
	res.Binding.CurrentClaimsDigestSHA256 = strings.TrimSpace(res.Binding.CurrentClaimsDigestSHA256)
	res.Binding.ClosureStateDigestSHA256 = strings.TrimSpace(res.Binding.ClosureStateDigestSHA256)
	res.Binding.ExistingQuestionsDigestSHA256 = strings.TrimSpace(res.Binding.ExistingQuestionsDigestSHA256)
	res.Binding.ReviewHistoryDigestSHA256 = strings.TrimSpace(res.Binding.ReviewHistoryDigestSHA256)
	res.Binding.EvidenceSnapshotDigestSHA256 = strings.TrimSpace(res.Binding.EvidenceSnapshotDigestSHA256)
	res.Binding.GroundingSnapshotDigestSHA256 = strings.TrimSpace(res.Binding.GroundingSnapshotDigestSHA256)
	res.Binding.GeneratorVersion = strings.TrimSpace(res.Binding.GeneratorVersion)
	res.Binding.RulesetVersion = strings.TrimSpace(res.Binding.RulesetVersion)

	// 8. Normalize Receipt strings
	res.Receipt.SchemaVersion = strings.TrimSpace(res.Receipt.SchemaVersion)
	res.Receipt.GeneratedBy = strings.TrimSpace(res.Receipt.GeneratedBy)
	res.Receipt.GroundingSnapshotDigestSHA256 = strings.TrimSpace(res.Receipt.GroundingSnapshotDigestSHA256)
	res.Receipt.HowDocumentDigestSHA256 = strings.TrimSpace(res.Receipt.HowDocumentDigestSHA256)
	res.Receipt.WhyDocumentDigestSHA256 = strings.TrimSpace(res.Receipt.WhyDocumentDigestSHA256)
	res.Receipt.GraphDigestSHA256 = strings.TrimSpace(res.Receipt.GraphDigestSHA256)
	res.Receipt.CurrentClaimsDigestSHA256 = strings.TrimSpace(res.Receipt.CurrentClaimsDigestSHA256)
	res.Receipt.ClosureStateDigestSHA256 = strings.TrimSpace(res.Receipt.ClosureStateDigestSHA256)
	res.Receipt.ExistingQuestionsDigestSHA256 = strings.TrimSpace(res.Receipt.ExistingQuestionsDigestSHA256)
	res.Receipt.ReviewHistoryDigestSHA256 = strings.TrimSpace(res.Receipt.ReviewHistoryDigestSHA256)
	res.Receipt.GeneratorVersion = strings.TrimSpace(res.Receipt.GeneratorVersion)
	res.Receipt.RulesetVersion = strings.TrimSpace(res.Receipt.RulesetVersion)
	res.Receipt.TimestampSource = strings.TrimSpace(res.Receipt.TimestampSource)
	res.Receipt.NondeterminismDeclaration = strings.TrimSpace(res.Receipt.NondeterminismDeclaration)

	return res, nil
}

func investorClaimScope(repo, rep, dom, ss string, f, s, c []string) architecture.ClaimScope {
	return architecture.ClaimScope{
		Repository: repo,
		Repo:       rep,
		Domain:     dom,
		SourceSet:  ss,
		Files:      f,
		Symbols:    s,
		Components: c,
	}
}
