// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

// GroundingSnapshot defines the deterministic grounding context to check candidates offline.
type GroundingSnapshot struct {
	Files               []string `json:"files" yaml:"files"`
	Symbols             []string `json:"symbols" yaml:"symbols"`
	GraphNodeIDs        []string `json:"graph_node_ids" yaml:"graph_node_ids"`
	ClaimIDs            []string `json:"claim_ids" yaml:"claim_ids"`
	ObservationIDs      []string `json:"observation_ids" yaml:"observation_ids"`
	EvidenceReceiptIDs  []string `json:"evidence_receipt_ids" yaml:"evidence_receipt_ids"`
	ExistingQuestionIDs []string `json:"existing_question_ids" yaml:"existing_question_ids"`
}

// Result binds the canonical investigation document plus Phase 10 sidecar structures.
type Result struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`
	GeneratedBy   string `json:"generated_by" yaml:"generated_by"`

	Binding Binding `json:"binding" yaml:"binding"`

	Document investigation.Document `json:"document" yaml:"document"`

	Candidates       []CandidateEnvelope    `json:"candidates" yaml:"candidates"`
	Challenges       []ChallengeReceipt     `json:"challenges" yaml:"challenges"`
	EvidenceRequests []EvidenceRequest      `json:"evidence_requests" yaml:"evidence_requests"`
	Rankings         []RankingRecord        `json:"rankings" yaml:"rankings"`
	Counterexamples  []CounterexampleRecord `json:"counterexamples,omitempty" yaml:"counterexamples,omitempty"`

	Limitations []architecture.Limitation `json:"limitations,omitempty" yaml:"limitations,omitempty"`
	Receipt     RunReceipt                `json:"receipt" yaml:"receipt"`
}
