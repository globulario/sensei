// SPDX-License-Identifier: AGPL-3.0-only

package investigation

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
)

func cleanStringList(in []string, path bool) []string {
	seen := map[string]bool{}
	for _, item := range in {
		item = strings.TrimSpace(item)
		if path {
			item = filepath.ToSlash(strings.ReplaceAll(item, `\`, `/`))
		}
		if item != "" {
			seen[item] = true
		}
	}
	out := make([]string, 0, len(seen))
	for item := range seen {
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func canonicalizeClaimScope(in architecture.ClaimScope) architecture.ClaimScope {
	c := in
	c.Repository = strings.TrimSpace(c.Repository)
	c.Repo = strings.TrimSpace(c.Repo)
	if c.Repository == "" {
		c.Repository = c.Repo
	}
	if c.Repo == "" {
		c.Repo = c.Repository
	}
	c.Domain = strings.TrimSpace(c.Domain)
	c.SourceSet = strings.TrimSpace(c.SourceSet)
	c.Files = cleanStringList(c.Files, true)
	c.Symbols = cleanStringList(c.Symbols, false)
	c.Components = cleanStringList(c.Components, false)
	return c
}

func canonicalizeLimitation(in architecture.Limitation) architecture.Limitation {
	c := in
	c.Source = strings.TrimSpace(c.Source)
	c.Scope = strings.TrimSpace(c.Scope)
	c.Reason = strings.TrimSpace(c.Reason)
	return c
}

func sortLimitations(in []architecture.Limitation) []architecture.Limitation {
	out := make([]architecture.Limitation, len(in))
	for i, lim := range in {
		out[i] = canonicalizeLimitation(lim)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

// Normalize normalizes and cleans the Document fields.
// In particular, all slice fields where order is not semantic are sorted and deduplicated.
func Normalize(doc Document) (Document, error) {
	out := doc
	out.SchemaVersion = strings.TrimSpace(out.SchemaVersion)
	out.GeneratedBy = strings.TrimSpace(out.GeneratedBy)
	out.Mode = Mode(strings.TrimSpace(string(out.Mode)))

	// Normalize Binding
	out.Binding.Repository.RepositoryDomain = strings.TrimSpace(out.Binding.Repository.RepositoryDomain)
	out.Binding.Repository.Revision = strings.TrimSpace(out.Binding.Repository.Revision)
	out.Binding.Repository.RevisionStatus = strings.TrimSpace(out.Binding.Repository.RevisionStatus)
	out.Binding.Repository.TreeDigestSHA256 = strings.TrimSpace(out.Binding.Repository.TreeDigestSHA256)
	out.Binding.Repository.GraphDigestSHA256 = strings.TrimSpace(out.Binding.Repository.GraphDigestSHA256)
	out.Binding.Repository.GraphDigestStatus = strings.TrimSpace(out.Binding.Repository.GraphDigestStatus)

	out.Binding.EvidenceSnapshotDigestSHA256 = strings.TrimSpace(out.Binding.EvidenceSnapshotDigestSHA256)
	out.Binding.InvestigationPlanDigestSHA256 = strings.TrimSpace(out.Binding.InvestigationPlanDigestSHA256)
	out.Binding.ExtractorProfileDigestSHA256 = strings.TrimSpace(out.Binding.ExtractorProfileDigestSHA256)

	out.Binding.Model.Status = strings.TrimSpace(out.Binding.Model.Status)
	out.Binding.Model.ModelName = strings.TrimSpace(out.Binding.Model.ModelName)
	out.Binding.Model.ModelDigestSHA256 = strings.TrimSpace(out.Binding.Model.ModelDigestSHA256)

	// Normalize Plan
	out.Plan.ID = strings.TrimSpace(out.Plan.ID)
	out.Plan.Description = strings.TrimSpace(out.Plan.Description)
	var cleanedQueries []string
	seenQ := map[string]bool{}
	for _, q := range out.Plan.Queries {
		q = strings.TrimSpace(q)
		if q != "" && !seenQ[q] {
			seenQ[q] = true
			cleanedQueries = append(cleanedQueries, q)
		}
	}
	out.Plan.Queries = cleanedQueries

	// Normalize Coverage
	normalizedCoverage := make([]CoverageEntry, len(out.Coverage))
	for i, entry := range out.Coverage {
		entry.ProviderID = strings.TrimSpace(entry.ProviderID)
		entry.ProviderVersion = strings.TrimSpace(entry.ProviderVersion)
		entry.Category = EvidenceCategory(strings.TrimSpace(string(entry.Category)))
		entry.TargetDigestSHA256 = strings.TrimSpace(entry.TargetDigestSHA256)
		entry.SourceSnapshotDigestSHA256 = strings.TrimSpace(entry.SourceSnapshotDigestSHA256)
		entry.Status = CoverageStatus(strings.TrimSpace(string(entry.Status)))
		entry.Reason = strings.TrimSpace(entry.Reason)
		if entry.SearchedTimeRange != nil {
			entry.SearchedTimeRange.Start = strings.TrimSpace(entry.SearchedTimeRange.Start)
			entry.SearchedTimeRange.End = strings.TrimSpace(entry.SearchedTimeRange.End)
		}
		entry.ResultEvidenceIDs = cleanStringList(entry.ResultEvidenceIDs, false)
		entry.Limitations = sortLimitations(entry.Limitations)
		normalizedCoverage[i] = entry
	}
	sort.SliceStable(normalizedCoverage, func(i, j int) bool {
		if normalizedCoverage[i].ProviderID != normalizedCoverage[j].ProviderID {
			return normalizedCoverage[i].ProviderID < normalizedCoverage[j].ProviderID
		}
		if normalizedCoverage[i].Category != normalizedCoverage[j].Category {
			return normalizedCoverage[i].Category < normalizedCoverage[j].Category
		}
		if normalizedCoverage[i].TargetDigestSHA256 != normalizedCoverage[j].TargetDigestSHA256 {
			return normalizedCoverage[i].TargetDigestSHA256 < normalizedCoverage[j].TargetDigestSHA256
		}
		return normalizedCoverage[i].SourceSnapshotDigestSHA256 < normalizedCoverage[j].SourceSnapshotDigestSHA256
	})
	out.Coverage = normalizedCoverage

	// Normalize RawEvidence
	normalizedRawEvidence := make([]EvidenceReceipt, len(out.RawEvidence))
	for i, receipt := range out.RawEvidence {
		receipt.ID = strings.TrimSpace(receipt.ID)
		receipt.Category = EvidenceCategory(strings.TrimSpace(string(receipt.Category)))
		receipt.Provider.ID = strings.TrimSpace(receipt.Provider.ID)
		receipt.Provider.Version = strings.TrimSpace(receipt.Provider.Version)
		receipt.ProofStrength = ProofStrength(strings.TrimSpace(string(receipt.ProofStrength)))
		receipt.SourceIdentity = strings.TrimSpace(receipt.SourceIdentity)
		receipt.SourceDigestSHA256 = strings.TrimSpace(receipt.SourceDigestSHA256)
		receipt.ContentDigestSHA256 = strings.TrimSpace(receipt.ContentDigestSHA256)
		receipt.ContentLocation = strings.TrimSpace(receipt.ContentLocation)
		receipt.Scope = canonicalizeClaimScope(receipt.Scope)
		receipt.CapturedAt = strings.TrimSpace(receipt.CapturedAt)
		normalizedRawEvidence[i] = receipt
	}
	sort.SliceStable(normalizedRawEvidence, func(i, j int) bool {
		return normalizedRawEvidence[i].ID < normalizedRawEvidence[j].ID
	})
	seenEvidence := map[string]EvidenceReceipt{}
	var dedupRawEvidence []EvidenceReceipt
	for _, receipt := range normalizedRawEvidence {
		if existing, ok := seenEvidence[receipt.ID]; ok {
			if !evidenceReceiptsEqual(existing, receipt) {
				return Document{}, fmt.Errorf("raw evidence ID collision for ID %q with different content", receipt.ID)
			}
			continue
		}
		seenEvidence[receipt.ID] = receipt
		dedupRawEvidence = append(dedupRawEvidence, receipt)
	}
	out.RawEvidence = dedupRawEvidence

	// Normalize Observations (Facts)
	// We sort the observations by ID. Since fact.go does not have a NormalizeFact function,
	// let's do a basic canonicalization of files/symbols within scope.
	normalizedObservations := make([]architecture.Fact, len(out.Observations))
	for i, fact := range out.Observations {
		fact.ID = strings.TrimSpace(fact.ID)
		fact.Kind = strings.TrimSpace(fact.Kind)
		fact.Subject = strings.TrimSpace(fact.Subject)
		fact.Predicate = strings.TrimSpace(fact.Predicate)
		fact.Object = strings.TrimSpace(fact.Object)
		fact.Scope.Repository = strings.TrimSpace(fact.Scope.Repository)
		fact.Scope.Files = cleanStringList(fact.Scope.Files, true)
		fact.Scope.Symbols = cleanStringList(fact.Scope.Symbols, false)
		fact.Evidence.SourceFile = strings.TrimSpace(fact.Evidence.SourceFile)
		fact.Evidence.TestName = strings.TrimSpace(fact.Evidence.TestName)
		fact.Evidence.Commit = strings.TrimSpace(fact.Evidence.Commit)
		fact.Evidence.Command = strings.TrimSpace(fact.Evidence.Command)
		fact.Extractor = strings.TrimSpace(fact.Extractor)
		normalizedObservations[i] = fact
	}
	sort.SliceStable(normalizedObservations, func(i, j int) bool {
		return normalizedObservations[i].ID < normalizedObservations[j].ID
	})
	seenObs := map[string]architecture.Fact{}
	var dedupObs []architecture.Fact
	for _, fact := range normalizedObservations {
		if existing, ok := seenObs[fact.ID]; ok {
			if !factsEqual(existing, fact) {
				return Document{}, fmt.Errorf("observation ID collision for ID %q with different content", fact.ID)
			}
			continue
		}
		seenObs[fact.ID] = fact
		dedupObs = append(dedupObs, fact)
	}
	out.Observations = dedupObs

	// Normalize CandidateClaims
	if len(out.CandidateClaims) > 0 {
		claims, err := architecture.NormalizeClaims(out.CandidateClaims)
		if err != nil {
			return Document{}, err
		}
		out.CandidateClaims = claims
	}

	// Normalize CandidateQuestions
	if len(out.CandidateQuestions) > 0 {
		questions, err := architecture.NormalizeOpenQuestions(out.CandidateQuestions)
		if err != nil {
			return Document{}, err
		}
		out.CandidateQuestions = questions
	}

	// Normalize Counterexamples
	normalizedCounterexamples := make([]Counterexample, len(out.Counterexamples))
	for i, ce := range out.Counterexamples {
		ce.ID = strings.TrimSpace(ce.ID)
		ce.ClaimID = strings.TrimSpace(ce.ClaimID)
		ce.Description = strings.TrimSpace(ce.Description)
		ce.Scope = canonicalizeClaimScope(ce.Scope)
		ce.EvidenceRefIDs = cleanStringList(ce.EvidenceRefIDs, false)
		normalizedCounterexamples[i] = ce
	}
	sort.SliceStable(normalizedCounterexamples, func(i, j int) bool {
		return normalizedCounterexamples[i].ID < normalizedCounterexamples[j].ID
	})
	seenCE := map[string]Counterexample{}
	var dedupCE []Counterexample
	for _, ce := range normalizedCounterexamples {
		if existing, ok := seenCE[ce.ID]; ok {
			if !counterexamplesEqual(existing, ce) {
				return Document{}, fmt.Errorf("counterexample ID collision for ID %q with different content", ce.ID)
			}
			continue
		}
		seenCE[ce.ID] = ce
		dedupCE = append(dedupCE, ce)
	}
	out.Counterexamples = dedupCE

	// Normalize Limitations
	out.Limitations = sortLimitations(out.Limitations)

	// Normalize Receipt
	out.Receipt.SchemaVersion = strings.TrimSpace(out.Receipt.SchemaVersion)
	out.Receipt.GeneratedBy = strings.TrimSpace(out.Receipt.GeneratedBy)
	out.Receipt.Repository.RepositoryDomain = strings.TrimSpace(out.Receipt.Repository.RepositoryDomain)
	out.Receipt.Repository.Revision = strings.TrimSpace(out.Receipt.Repository.Revision)
	out.Receipt.Repository.RevisionStatus = strings.TrimSpace(out.Receipt.Repository.RevisionStatus)
	out.Receipt.Repository.TreeDigestSHA256 = strings.TrimSpace(out.Receipt.Repository.TreeDigestSHA256)
	out.Receipt.Repository.GraphDigestSHA256 = strings.TrimSpace(out.Receipt.Repository.GraphDigestSHA256)
	out.Receipt.Repository.GraphDigestStatus = strings.TrimSpace(out.Receipt.Repository.GraphDigestStatus)
	out.Receipt.GraphDigestSHA256 = strings.TrimSpace(out.Receipt.GraphDigestSHA256)
	out.Receipt.PlanDigestSHA256 = strings.TrimSpace(out.Receipt.PlanDigestSHA256)
	out.Receipt.ExtractorProfileDigestSHA256 = strings.TrimSpace(out.Receipt.ExtractorProfileDigestSHA256)
	out.Receipt.EvidenceSnapshotDigestSHA256 = strings.TrimSpace(out.Receipt.EvidenceSnapshotDigestSHA256)
	out.Receipt.Model.Status = strings.TrimSpace(out.Receipt.Model.Status)
	out.Receipt.Model.ModelName = strings.TrimSpace(out.Receipt.Model.ModelName)
	out.Receipt.Model.ModelDigestSHA256 = strings.TrimSpace(out.Receipt.Model.ModelDigestSHA256)
	out.Receipt.ModelArtifactDigestSHA256 = strings.TrimSpace(out.Receipt.ModelArtifactDigestSHA256)
	out.Receipt.PostProcessingVersion = strings.TrimSpace(out.Receipt.PostProcessingVersion)
	out.Receipt.OutputDocumentDigestSHA256 = strings.TrimSpace(out.Receipt.OutputDocumentDigestSHA256)
	out.Receipt.TimestampSource = strings.TrimSpace(out.Receipt.TimestampSource)
	out.Receipt.NondeterminismDeclaration = strings.TrimSpace(out.Receipt.NondeterminismDeclaration)

	if out.Receipt.OutputCandidateIDsAndDigests != nil {
		cleanedMap := make(map[string]string, len(out.Receipt.OutputCandidateIDsAndDigests))
		for k, v := range out.Receipt.OutputCandidateIDsAndDigests {
			cleanedMap[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
		out.Receipt.OutputCandidateIDsAndDigests = cleanedMap
	}

	if out.Receipt.ResourceLimits != nil {
		cleanedMap := make(map[string]string, len(out.Receipt.ResourceLimits))
		for k, v := range out.Receipt.ResourceLimits {
			cleanedMap[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
		out.Receipt.ResourceLimits = cleanedMap
	}

	return out, nil
}

func evidenceReceiptsEqual(a, b EvidenceReceipt) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}

func counterexamplesEqual(a, b Counterexample) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}

func factsEqual(a, b architecture.Fact) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	if string(aj) != string(bj) {
		return false
	}
	if (a.Provenance == nil) != (b.Provenance == nil) {
		return false
	}
	if a.Provenance != nil && b.Provenance != nil {
		apj, _ := json.Marshal(a.Provenance)
		bpj, _ := json.Marshal(b.Provenance)
		if string(apj) != string(bpj) {
			return false
		}
	}
	return true
}
