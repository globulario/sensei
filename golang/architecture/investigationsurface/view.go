// SPDX-License-Identifier: AGPL-3.0-only

package investigationsurface

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
	"github.com/globulario/sensei/golang/architecture/investigator"
)

type InvestigationSummary struct {
	SchemaVersion       string                            `json:"schema_version" yaml:"schema_version"`
	Mode                investigation.Mode                `json:"mode" yaml:"mode"`
	Repository          architecture.ClaimDocumentBinding `json:"repository" yaml:"repository"`
	DocumentDigest      string                            `json:"document_digest_sha256" yaml:"document_digest_sha256"`
	ObservationCount    int                               `json:"observation_count" yaml:"observation_count"`
	EvidenceCount       int                               `json:"evidence_count" yaml:"evidence_count"`
	CoverageCount       int                               `json:"coverage_count" yaml:"coverage_count"`
	CandidateClaimCount int                               `json:"candidate_claim_count" yaml:"candidate_claim_count"`
	QuestionCount       int                               `json:"question_count" yaml:"question_count"`
	CounterexampleCount int                               `json:"counterexample_count" yaml:"counterexample_count"`
	LimitationCount     int                               `json:"limitation_count" yaml:"limitation_count"`
}

type CoverageReport struct {
	SchemaVersion  string                        `json:"schema_version" yaml:"schema_version"`
	Mode           investigation.Mode            `json:"mode" yaml:"mode"`
	DocumentDigest string                        `json:"document_digest_sha256" yaml:"document_digest_sha256"`
	Entries        []investigation.CoverageEntry `json:"entries" yaml:"entries"`
	ByStatus       map[string]int                `json:"by_status" yaml:"by_status"`
	ByCategory     map[string]int                `json:"by_category" yaml:"by_category"`
	EvidenceCount  int                           `json:"evidence_count" yaml:"evidence_count"`
	Limitations    []architecture.Limitation     `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type CandidateView struct {
	Candidate investigator.CandidateEnvelope `json:"candidate" yaml:"candidate"`
	Claim     architecture.Claim             `json:"claim" yaml:"claim"`
	Ranking   *investigator.RankingRecord    `json:"ranking,omitempty" yaml:"ranking,omitempty"`
	Challenge *investigator.ChallengeReceipt `json:"challenge,omitempty" yaml:"challenge,omitempty"`
}

type CandidateReport struct {
	SchemaVersion string          `json:"schema_version" yaml:"schema_version"`
	ResultDigest  string          `json:"result_digest_sha256" yaml:"result_digest_sha256"`
	Candidates    []CandidateView `json:"candidates" yaml:"candidates"`
}

type BlastRadiusReport struct {
	SchemaVersion  string                    `json:"schema_version" yaml:"schema_version"`
	ResultDigest   string                    `json:"result_digest_sha256" yaml:"result_digest_sha256"`
	CandidateID    string                    `json:"candidate_id" yaml:"candidate_id"`
	ClaimID        string                    `json:"claim_id" yaml:"claim_id"`
	Scope          architecture.ClaimScope   `json:"scope" yaml:"scope"`
	Files          []string                  `json:"files" yaml:"files"`
	Symbols        []string                  `json:"symbols" yaml:"symbols"`
	Components     []string                  `json:"components" yaml:"components"`
	EvidenceIDs    []string                  `json:"evidence_ids" yaml:"evidence_ids"`
	ObservationIDs []string                  `json:"observation_ids" yaml:"observation_ids"`
	Limitations    []architecture.Limitation `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type ChallengeReport struct {
	SchemaVersion    string                              `json:"schema_version" yaml:"schema_version"`
	ResultDigest     string                              `json:"result_digest_sha256" yaml:"result_digest_sha256"`
	Candidate        CandidateView                       `json:"candidate" yaml:"candidate"`
	Counterexamples  []investigator.CounterexampleRecord `json:"counterexamples,omitempty" yaml:"counterexamples,omitempty"`
	EvidenceRequests []investigator.EvidenceRequest      `json:"evidence_requests,omitempty" yaml:"evidence_requests,omitempty"`
}

func SummarizeDocument(doc investigation.Document) (InvestigationSummary, error) {
	if err := investigation.Validate(doc); err != nil {
		return InvestigationSummary{}, err
	}
	digest, err := investigation.CalculateDocumentDigest(doc)
	if err != nil {
		return InvestigationSummary{}, err
	}
	return InvestigationSummary{
		SchemaVersion:    "investigation.surface.summary.v1",
		Mode:             doc.Mode,
		Repository:       doc.Binding.Repository,
		DocumentDigest:   digest,
		ObservationCount: len(doc.Observations), EvidenceCount: len(doc.RawEvidence), CoverageCount: len(doc.Coverage),
		CandidateClaimCount: len(doc.CandidateClaims), QuestionCount: len(doc.CandidateQuestions),
		CounterexampleCount: len(doc.Counterexamples), LimitationCount: len(doc.Limitations),
	}, nil
}

func Coverage(doc investigation.Document) (CoverageReport, error) {
	if err := investigation.Validate(doc); err != nil {
		return CoverageReport{}, err
	}
	digest, err := investigation.CalculateDocumentDigest(doc)
	if err != nil {
		return CoverageReport{}, err
	}
	entries := append([]investigation.CoverageEntry(nil), doc.Coverage...)
	sort.SliceStable(entries, func(i, j int) bool {
		a := entries[i].ProviderID + "\x00" + string(entries[i].Category) + "\x00" + entries[i].TargetDigestSHA256
		b := entries[j].ProviderID + "\x00" + string(entries[j].Category) + "\x00" + entries[j].TargetDigestSHA256
		return a < b
	})
	report := CoverageReport{SchemaVersion: "investigation.surface.coverage.v1", Mode: doc.Mode, DocumentDigest: digest, Entries: entries, ByStatus: map[string]int{}, ByCategory: map[string]int{}, EvidenceCount: len(doc.RawEvidence), Limitations: append([]architecture.Limitation(nil), doc.Limitations...)}
	for _, entry := range entries {
		report.ByStatus[string(entry.Status)]++
		report.ByCategory[string(entry.Category)]++
	}
	return report, nil
}

func GroundingFromResult(result investigator.Result) investigator.GroundingSnapshot {
	g := investigator.GroundingSnapshot{}
	for _, observation := range result.Document.Observations {
		g.ObservationIDs = append(g.ObservationIDs, observation.ID)
		g.Files = append(g.Files, observation.Scope.Files...)
		g.Symbols = append(g.Symbols, observation.Scope.Symbols...)
	}
	for _, evidence := range result.Document.RawEvidence {
		g.EvidenceReceiptIDs = append(g.EvidenceReceiptIDs, evidence.ID)
		g.Files = append(g.Files, evidence.Scope.Files...)
		g.Symbols = append(g.Symbols, evidence.Scope.Symbols...)
		g.GraphNodeIDs = append(g.GraphNodeIDs, evidence.Scope.Components...)
	}
	for _, claim := range result.Document.CandidateClaims {
		g.ClaimIDs = append(g.ClaimIDs, claim.ID)
		g.Files = append(g.Files, claim.Scope.Files...)
		g.Symbols = append(g.Symbols, claim.Scope.Symbols...)
		g.GraphNodeIDs = append(g.GraphNodeIDs, claim.Scope.Components...)
		g.GraphNodeIDs = append(g.GraphNodeIDs, claim.AboutNodes...)
	}
	g.Files = unique(g.Files)
	g.Symbols = unique(g.Symbols)
	g.GraphNodeIDs = unique(g.GraphNodeIDs)
	g.ObservationIDs = unique(g.ObservationIDs)
	g.EvidenceReceiptIDs = unique(g.EvidenceReceiptIDs)
	g.ClaimIDs = unique(g.ClaimIDs)
	return g
}

func Candidates(result investigator.Result) (CandidateReport, error) {
	grounding := GroundingFromResult(result)
	if err := investigator.Validate(result, grounding); err != nil {
		return CandidateReport{}, err
	}
	digest, err := investigator.ResultDigest(result)
	if err != nil {
		return CandidateReport{}, err
	}
	claims := map[string]architecture.Claim{}
	rankings := map[string]investigator.RankingRecord{}
	challenges := map[string]investigator.ChallengeReceipt{}
	for _, claim := range result.Document.CandidateClaims {
		claims[claim.ID] = claim
	}
	for _, ranking := range result.Rankings {
		rankings[ranking.CandidateID] = ranking
	}
	for _, challenge := range result.Challenges {
		challenges[challenge.CandidateID] = challenge
	}
	views := make([]CandidateView, 0, len(result.Candidates))
	for _, candidate := range result.Candidates {
		view := CandidateView{Candidate: candidate, Claim: claims[candidate.ClaimID]}
		if ranking, ok := rankings[candidate.CandidateID]; ok {
			copy := ranking
			view.Ranking = &copy
		}
		if challenge, ok := challenges[candidate.CandidateID]; ok {
			copy := challenge
			view.Challenge = &copy
		}
		views = append(views, view)
	}
	sort.SliceStable(views, func(i, j int) bool {
		ri, rj := 1<<30, 1<<30
		if views[i].Ranking != nil {
			ri = views[i].Ranking.Rank
		}
		if views[j].Ranking != nil {
			rj = views[j].Ranking.Rank
		}
		if ri != rj {
			return ri < rj
		}
		return views[i].Candidate.CandidateID < views[j].Candidate.CandidateID
	})
	return CandidateReport{SchemaVersion: "investigation.surface.candidates.v1", ResultDigest: digest, Candidates: views}, nil
}

func FindCandidate(result investigator.Result, candidateID string) (CandidateView, string, error) {
	report, err := Candidates(result)
	if err != nil {
		return CandidateView{}, "", err
	}
	candidateID = strings.TrimSpace(candidateID)
	for _, view := range report.Candidates {
		if view.Candidate.CandidateID == candidateID {
			return view, report.ResultDigest, nil
		}
	}
	return CandidateView{}, report.ResultDigest, fmt.Errorf("candidate %q was not found", candidateID)
}

func BlastRadius(result investigator.Result, candidateID string) (BlastRadiusReport, error) {
	view, digest, err := FindCandidate(result, candidateID)
	if err != nil {
		return BlastRadiusReport{}, err
	}
	evidence := append(append([]string(nil), view.Candidate.SupportingEvidenceRefIDs...), view.Candidate.RefutingEvidenceRefIDs...)
	return BlastRadiusReport{SchemaVersion: "investigation.surface.blast-radius.v1", ResultDigest: digest, CandidateID: view.Candidate.CandidateID, ClaimID: view.Claim.ID, Scope: view.Claim.Scope, Files: unique(view.Claim.Scope.Files), Symbols: unique(view.Claim.Scope.Symbols), Components: unique(view.Claim.Scope.Components), EvidenceIDs: unique(evidence), ObservationIDs: unique(view.Candidate.ObservationRefIDs), Limitations: append([]architecture.Limitation(nil), result.Limitations...)}, nil
}

func Challenge(result investigator.Result, candidateID string) (ChallengeReport, error) {
	view, digest, err := FindCandidate(result, candidateID)
	if err != nil {
		return ChallengeReport{}, err
	}
	if view.Challenge == nil {
		return ChallengeReport{}, errors.New("candidate has no challenge receipt")
	}
	report := ChallengeReport{SchemaVersion: "investigation.surface.challenge.v1", ResultDigest: digest, Candidate: view}
	for _, counterexample := range result.Counterexamples {
		if counterexample.Counterexample.ClaimID == view.Claim.ID {
			report.Counterexamples = append(report.Counterexamples, counterexample)
		}
	}
	for _, request := range result.EvidenceRequests {
		if request.CandidateID == candidateID {
			report.EvidenceRequests = append(report.EvidenceRequests, request)
		}
	}
	return report, nil
}

func unique(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func GroundingFromDocuments(documents ...investigation.Document) investigator.GroundingSnapshot {
	g := investigator.GroundingSnapshot{}
	for _, document := range documents {
		for _, observation := range document.Observations {
			g.ObservationIDs = append(g.ObservationIDs, observation.ID)
			g.Files = append(g.Files, observation.Scope.Files...)
			g.Symbols = append(g.Symbols, observation.Scope.Symbols...)
		}
		for _, evidence := range document.RawEvidence {
			g.EvidenceReceiptIDs = append(g.EvidenceReceiptIDs, evidence.ID)
			g.Files = append(g.Files, evidence.Scope.Files...)
			g.Symbols = append(g.Symbols, evidence.Scope.Symbols...)
			g.GraphNodeIDs = append(g.GraphNodeIDs, evidence.Scope.Components...)
		}
		for _, claim := range document.CandidateClaims {
			g.ClaimIDs = append(g.ClaimIDs, claim.ID)
			g.Files = append(g.Files, claim.Scope.Files...)
			g.Symbols = append(g.Symbols, claim.Scope.Symbols...)
			g.GraphNodeIDs = append(g.GraphNodeIDs, claim.Scope.Components...)
			g.GraphNodeIDs = append(g.GraphNodeIDs, claim.AboutNodes...)
		}
		for _, question := range document.CandidateQuestions {
			g.ExistingQuestionIDs = append(g.ExistingQuestionIDs, question.ID)
		}
	}
	g.Files = unique(g.Files)
	g.Symbols = unique(g.Symbols)
	g.GraphNodeIDs = unique(g.GraphNodeIDs)
	g.ClaimIDs = unique(g.ClaimIDs)
	g.ObservationIDs = unique(g.ObservationIDs)
	g.EvidenceReceiptIDs = unique(g.EvidenceReceiptIDs)
	g.ExistingQuestionIDs = unique(g.ExistingQuestionIDs)
	return g
}
