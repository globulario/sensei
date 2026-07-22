// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture/investigationsurface"
)

func phase10Tools() []tool {
	return []tool{
		{Name: "awareness_investigate", Description: "Inspect or validate an exact Phase 10 investigation artifact. Read-only and candidate-only; never promotes knowledge.", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"artifact": map[string]interface{}{"type": "string"}, "operation": map[string]interface{}{"type": "string", "enum": []string{"summary", "validate", "blast_radius"}}, "candidate_id": map[string]interface{}{"type": "string"}}, "required": []string{"artifact", "operation"}, "additionalProperties": false}},
		{Name: "awareness_evidence_coverage", Description: "Report honest provider coverage, proof-bearing evidence counts, limitations, unavailable sources, and searched-no-result states from a receipt-valid HOW or WHY artifact.", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"artifact": map[string]interface{}{"type": "string"}}, "required": []string{"artifact"}, "additionalProperties": false}},
		{Name: "awareness_candidates", Description: "List or show advisory architecture candidates from a receipt-valid investigator result. Candidates are not active authority.", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"result": map[string]interface{}{"type": "string"}, "candidate_id": map[string]interface{}{"type": "string"}}, "required": []string{"result"}, "additionalProperties": false}},
		{Name: "awareness_challenge", Description: "Show the exact challenge receipt, counterexamples, and missing evidence for one advisory candidate. Read-only; cannot promote or weaken architecture.", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"result": map[string]interface{}{"type": "string"}, "candidate_id": map[string]interface{}{"type": "string"}}, "required": []string{"result", "candidate_id"}, "additionalProperties": false}},
	}
}

func callPhase10Tool(name string, args map[string]interface{}) (*toolResult, error) {
	switch name {
	case "awareness_investigate":
		artifact, err := requiredPhase10String(args, "artifact")
		if err != nil {
			return nil, err
		}
		operation, err := requiredPhase10String(args, "operation")
		if err != nil {
			return nil, err
		}
		switch operation {
		case "validate":
			report := investigationsurface.ValidateArtifact(artifact)
			text := fmt.Sprintf("kind: %s\nvalid: %t\ndigest: %s", report.ArtifactKind, report.Valid, report.DigestSHA256)
			if report.Error != "" {
				text += "\nerror: " + report.Error
			}
			return phase10ToolResult(text, report)
		case "summary":
			report := investigationsurface.ValidateArtifact(artifact)
			if !report.Valid {
				return nil, fmt.Errorf("artifact is invalid: %s", report.Error)
			}
			if report.ArtifactKind == investigationsurface.ArtifactInvestigation {
				doc, err := investigationsurface.LoadDocument(artifact)
				if err != nil {
					return nil, err
				}
				summary, err := investigationsurface.SummarizeDocument(doc)
				if err != nil {
					return nil, err
				}
				return phase10ToolResult(fmt.Sprintf("mode: %s\nobservations: %d\nevidence: %d\ncoverage: %d\ndigest: %s", summary.Mode, summary.ObservationCount, summary.EvidenceCount, summary.CoverageCount, summary.DocumentDigest), summary)
			}
			if report.ArtifactKind == investigationsurface.ArtifactResult {
				result, err := investigationsurface.LoadResult(artifact)
				if err != nil {
					return nil, err
				}
				candidates, err := investigationsurface.Candidates(result)
				if err != nil {
					return nil, err
				}
				return phase10ToolResult(fmt.Sprintf("candidates: %d\nchallenges: %d\nevidence_requests: %d\ndigest: %s", len(candidates.Candidates), len(result.Challenges), len(result.EvidenceRequests), candidates.ResultDigest), candidates)
			}
			return phase10ToolResult(fmt.Sprintf("kind: %s\ndigest: %s", report.ArtifactKind, report.DigestSHA256), report)
		case "blast_radius":
			id, err := requiredPhase10String(args, "candidate_id")
			if err != nil {
				return nil, err
			}
			result, err := investigationsurface.LoadResult(artifact)
			if err != nil {
				return nil, err
			}
			report, err := investigationsurface.BlastRadius(result, id)
			if err != nil {
				return nil, err
			}
			return phase10ToolResult(fmt.Sprintf("candidate: %s\nfiles: %d\nsymbols: %d\ncomponents: %d\nevidence: %d", report.CandidateID, len(report.Files), len(report.Symbols), len(report.Components), len(report.EvidenceIDs)), report)
		default:
			return nil, fmt.Errorf("operation must be summary, validate, or blast_radius")
		}
	case "awareness_evidence_coverage":
		artifact, err := requiredPhase10String(args, "artifact")
		if err != nil {
			return nil, err
		}
		doc, err := investigationsurface.LoadDocument(artifact)
		if err != nil {
			return nil, err
		}
		report, err := investigationsurface.Coverage(doc)
		if err != nil {
			return nil, err
		}
		return phase10ToolResult(fmt.Sprintf("mode: %s\ncoverage_entries: %d\nevidence: %d\nlimitations: %d", report.Mode, len(report.Entries), report.EvidenceCount, len(report.Limitations)), report)
	case "awareness_candidates":
		path, err := requiredPhase10String(args, "result")
		if err != nil {
			return nil, err
		}
		result, err := investigationsurface.LoadResult(path)
		if err != nil {
			return nil, err
		}
		id, _ := args["candidate_id"].(string)
		if strings.TrimSpace(id) != "" {
			view, digest, err := investigationsurface.FindCandidate(result, id)
			if err != nil {
				return nil, err
			}
			return phase10ToolResult(fmt.Sprintf("candidate: %s\nclaim: %s\nkind: %s\nresult_digest: %s", view.Candidate.CandidateID, view.Claim.ID, view.Candidate.OutputKind, digest), map[string]interface{}{"schema_version": "investigation.surface.candidate.v1", "result_digest_sha256": digest, "candidate": view})
		}
		report, err := investigationsurface.Candidates(result)
		if err != nil {
			return nil, err
		}
		return phase10ToolResult(fmt.Sprintf("candidates: %d\nresult_digest: %s", len(report.Candidates), report.ResultDigest), report)
	case "awareness_challenge":
		path, err := requiredPhase10String(args, "result")
		if err != nil {
			return nil, err
		}
		id, err := requiredPhase10String(args, "candidate_id")
		if err != nil {
			return nil, err
		}
		result, err := investigationsurface.LoadResult(path)
		if err != nil {
			return nil, err
		}
		report, err := investigationsurface.Challenge(result, id)
		if err != nil {
			return nil, err
		}
		return phase10ToolResult(fmt.Sprintf("candidate: %s\nstatus: %s\ncounterexamples: %d\nevidence_requests: %d", id, report.Candidate.Challenge.Status, len(report.Counterexamples), len(report.EvidenceRequests)), report)
	default:
		return nil, errors.New("unknown Phase 10 tool")
	}
}

func requiredPhase10String(args map[string]interface{}, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("%s must be a non-empty string", key)
	}
	return strings.TrimSpace(s), nil
}
func phase10ToolResult(text string, value any) (*toolResult, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var structured map[string]interface{}
	if err := json.Unmarshal(data, &structured); err != nil {
		return nil, err
	}
	return &toolResult{Text: text, Structured: structured}, nil
}
