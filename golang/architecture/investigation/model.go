// SPDX-License-Identifier: AGPL-3.0-only

package investigation

import (
	"github.com/globulario/sensei/golang/architecture"
)

type Mode string

const (
	ModeHow          Mode = "how"
	ModeWhy          Mode = "why"
	ModeArchitecture Mode = "architecture"
	ModeBlastRadius  Mode = "blast_radius"
	ModeChallenge    Mode = "challenge"
)

func IsValidMode(mode Mode) bool {
	switch mode {
	case ModeHow, ModeWhy, ModeArchitecture, ModeBlastRadius, ModeChallenge:
		return true
	default:
		return false
	}
}

type Plan struct {
	ID          string   `json:"id" yaml:"id"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Queries     []string `json:"queries,omitempty" yaml:"queries,omitempty"`
}

type Counterexample struct {
	ID             string                  `json:"id" yaml:"id"`
	ClaimID        string                  `json:"claim_id,omitempty" yaml:"claim_id,omitempty"`
	Description    string                  `json:"description" yaml:"description"`
	Scope          architecture.ClaimScope `json:"scope" yaml:"scope"`
	EvidenceRefIDs []string                `json:"evidence_ref_ids,omitempty" yaml:"evidence_ref_ids,omitempty"`
}

type Document struct {
	SchemaVersion      string                      `json:"schema_version" yaml:"schema_version"`
	GeneratedBy        string                      `json:"generated_by" yaml:"generated_by"`
	Mode               Mode                        `json:"mode" yaml:"mode"`
	Binding            Binding                     `json:"binding" yaml:"binding"`
	Plan               Plan                        `json:"plan" yaml:"plan"`
	Coverage           []CoverageEntry             `json:"coverage" yaml:"coverage"`
	RawEvidence        []EvidenceReceipt           `json:"raw_evidence" yaml:"raw_evidence"`
	Observations       []architecture.Fact         `json:"observations" yaml:"observations"`
	CandidateClaims    []architecture.Claim        `json:"candidate_claims,omitempty" yaml:"candidate_claims,omitempty"`
	CandidateQuestions []architecture.OpenQuestion `json:"candidate_questions,omitempty" yaml:"candidate_questions,omitempty"`
	Counterexamples    []Counterexample            `json:"counterexamples,omitempty" yaml:"counterexamples,omitempty"`
	Limitations        []architecture.Limitation   `json:"limitations,omitempty" yaml:"limitations,omitempty"`
	Receipt            RunReceipt                  `json:"receipt" yaml:"receipt"`
}
