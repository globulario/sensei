// SPDX-License-Identifier: Apache-2.0

package maintenance

import "github.com/globulario/sensei/golang/architecture"

const (
	SchemaVersion = "1"
	GeneratedBy   = "sensei maintain-claims"

	LaneCurrent  = "current"
	LaneStale    = "stale"
	LaneUnknown  = "unknown"
	LaneActive   = "active"
	LaneInactive = "inactive"
	LaneAbsent   = "absent"
	LaneBlocking = "blocking"

	DispositionIntroduced  = "introduced"
	DispositionRetained    = "retained"
	DispositionChanged     = "changed"
	DispositionRevalidated = "revalidated"
	DispositionRetired     = "retired"
)

type Context struct {
	RepositoryRoot string

	Current  architecture.ClaimDocument
	Previous *architecture.ClaimDocument
	Dialogue *architecture.DialogueDocument
	Evidence *EvidenceStateDocument

	ObservedBinding architecture.ClaimDocumentBinding
	EvaluatedAt     string
}

type LaneState struct {
	State   string   `json:"state" yaml:"state"`
	Reasons []Reason `json:"reasons,omitempty" yaml:"reasons,omitempty"`
}

type ProofLanes struct {
	Binding            LaneState `json:"binding" yaml:"binding"`
	PremiseFacts       LaneState `json:"premise_facts" yaml:"premise_facts"`
	Dependencies       LaneState `json:"dependencies" yaml:"dependencies"`
	SupportingEvidence LaneState `json:"supporting_evidence" yaml:"supporting_evidence"`
	RefutingEvidence   LaneState `json:"refuting_evidence" yaml:"refuting_evidence"`
	Conflict           LaneState `json:"conflict" yaml:"conflict"`
	Supersession       LaneState `json:"supersession" yaml:"supersession"`
}

type Reason struct {
	Code   string `json:"code" yaml:"code"`
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

type Report struct {
	SchemaVersion    string                             `json:"schema_version" yaml:"schema_version"`
	GeneratedBy      string                             `json:"generated_by" yaml:"generated_by"`
	EvaluatedAt      string                             `json:"evaluated_at,omitempty" yaml:"evaluated_at,omitempty"`
	PreviousBinding  *architecture.ClaimDocumentBinding `json:"previous_binding,omitempty" yaml:"previous_binding,omitempty"`
	CurrentBinding   architecture.ClaimDocumentBinding  `json:"current_binding" yaml:"current_binding"`
	ObservedBinding  architecture.ClaimDocumentBinding  `json:"observed_binding" yaml:"observed_binding"`
	ClaimEvaluations []ClaimEvaluation                  `json:"claim_evaluations" yaml:"claim_evaluations"`
	RetiredClaims    []RetiredClaim                     `json:"retired_claims,omitempty" yaml:"retired_claims,omitempty"`
	Limitations      []architecture.Limitation          `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type ClaimEvaluation struct {
	ClaimID         string            `json:"claim_id" yaml:"claim_id"`
	PreviousStatus  string            `json:"previous_status,omitempty" yaml:"previous_status,omitempty"`
	InputStatus     string            `json:"input_status" yaml:"input_status"`
	EvaluatedStatus string            `json:"evaluated_status" yaml:"evaluated_status"`
	Disposition     string            `json:"disposition" yaml:"disposition"`
	ProofLanes      ProofLanes        `json:"proof_lanes" yaml:"proof_lanes"`
	Reasons         []Reason          `json:"reasons,omitempty" yaml:"reasons,omitempty"`
	OpenQuestions   []QuestionSummary `json:"open_questions,omitempty" yaml:"open_questions,omitempty"`
}

type RetiredClaim struct {
	ClaimID         string   `json:"claim_id" yaml:"claim_id"`
	PreviousStatus  string   `json:"previous_status,omitempty" yaml:"previous_status,omitempty"`
	EvaluatedStatus string   `json:"evaluated_status" yaml:"evaluated_status"`
	Disposition     string   `json:"disposition" yaml:"disposition"`
	Reasons         []Reason `json:"reasons" yaml:"reasons"`
}

type QuestionSummary struct {
	QuestionID          string   `json:"question_id" yaml:"question_id"`
	Status              string   `json:"status" yaml:"status"`
	AcceptedAnswerIDs   []string `json:"accepted_answer_ids,omitempty" yaml:"accepted_answer_ids,omitempty"`
	AnswerClasses       []string `json:"answer_classes,omitempty" yaml:"answer_classes,omitempty"`
	AnswerGovernance    []string `json:"answer_governance,omitempty" yaml:"answer_governance,omitempty"`
	NonProbativeReasons []Reason `json:"non_probative_reasons,omitempty" yaml:"non_probative_reasons,omitempty"`
}

type Result struct {
	Document architecture.ClaimDocument
	Report   Report
}
