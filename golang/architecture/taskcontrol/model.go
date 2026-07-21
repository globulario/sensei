// SPDX-License-Identifier: AGPL-3.0-only

package taskcontrol

import "github.com/globulario/sensei/golang/architecture"

const (
	SchemaVersion = "1"
	GeneratedBy   = "sensei task-control"

	ClassMechanicallyAnswerable     = "mechanically_answerable"
	ClassStaticProbeAnswerable      = "static_probe_answerable"
	ClassArchitectJudgementRequired = "architect_judgement_required"
	ClassNonBlockingUnknown         = "non_blocking_unknown"
	ClassDominated                  = "dominated"
	ClassDuplicate                  = "duplicate"
	ClassActiveUnresolved           = "active_unresolved"
	ClassUncertifiable              = "uncertifiable"

	ProbeEligible     = "eligible"
	ProbeCompleted    = "completed"
	ProbeInconclusive = "inconclusive"
	ProbeUnavailable  = "unavailable"
	ProbeFailed       = "failed"
	ProbeRejected     = "rejected"
	ProbeSuperseded   = "superseded"

	ActionRepairBinding           = "repair_binding"
	ActionProvideMissingInput     = "provide_missing_input"
	ActionRunStaticEvidence       = "run_static_evidence"
	ActionAnswerArchitectQuestion = "answer_architect_question"
	ActionProvideExternalEvidence = "provide_external_evidence"
	ActionAdjudicateAnswer        = "adjudicate_answer"
	ActionAdvanceConvergence      = "advance_convergence"
	ActionRequestInspection       = "request_inspection_admission"
	ActionRequestMutation         = "request_mutation_admission"
	ActionPerformAdmittedEdit     = "perform_admitted_edit"
	ActionVerifyAdmission         = "verify_admission"
	ActionCompleteTests           = "complete_tests"
	ActionCompleteTask            = "complete_task"
	ActionNone                    = "none"

	ReasonDominanceCycle  = "task.control.dominance_cycle"
	ReasonNoPrimaryAction = "task.control.no_primary_action"
)

type PermissionSummary struct {
	Inspect    string   `json:"inspect" yaml:"inspect"`
	Modify     string   `json:"modify" yaml:"modify"`
	ExactScope []string `json:"exact_scope,omitempty" yaml:"exact_scope,omitempty"`
}

type ClassifiedBlocker struct {
	ID          string   `json:"id" yaml:"id"`
	Disposition string   `json:"disposition" yaml:"disposition"`
	Dimension   string   `json:"dimension" yaml:"dimension"`
	Severity    string   `json:"severity" yaml:"severity"`
	Code        string   `json:"code" yaml:"code"`
	Statement   string   `json:"statement" yaml:"statement"`
	Consequence string   `json:"consequence" yaml:"consequence"`
	GroupID     string   `json:"group_id" yaml:"group_id"`
	DominatorID string   `json:"dominator_id,omitempty" yaml:"dominator_id,omitempty"`
	DuplicateOf string   `json:"duplicate_of,omitempty" yaml:"duplicate_of,omitempty"`
	QuestionIDs []string `json:"question_ids,omitempty" yaml:"question_ids,omitempty"`
	ProbeIDs    []string `json:"probe_ids,omitempty" yaml:"probe_ids,omitempty"`
	ClaimIDs    []string `json:"claim_ids,omitempty" yaml:"claim_ids,omitempty"`
	NodeIDs     []string `json:"node_ids,omitempty" yaml:"node_ids,omitempty"`
	Files       []string `json:"files,omitempty" yaml:"files,omitempty"`
	LoadBearing bool     `json:"load_bearing" yaml:"load_bearing"`
	ReasonCodes []string `json:"reason_codes,omitempty" yaml:"reason_codes,omitempty"`
}

type ClassifiedQuestion struct {
	ID                 string   `json:"id" yaml:"id"`
	ResolutionClass    string   `json:"resolution_class" yaml:"resolution_class"`
	BlockingEffect     string   `json:"blocking_effect" yaml:"blocking_effect"`
	GroupID            string   `json:"group_id,omitempty" yaml:"group_id,omitempty"`
	DominantQuestionID string   `json:"dominant_question_id,omitempty" yaml:"dominant_question_id,omitempty"`
	AnswerabilityBasis []string `json:"answerability_basis,omitempty" yaml:"answerability_basis,omitempty"`
	RequiredActor      string   `json:"required_actor" yaml:"required_actor"`
	Priority           string   `json:"priority" yaml:"priority"`
	QuestionText       string   `json:"question_text" yaml:"question_text"`
	BlockerIDs         []string `json:"blocker_ids,omitempty" yaml:"blocker_ids,omitempty"`
	ClaimIDs           []string `json:"claim_ids,omitempty" yaml:"claim_ids,omitempty"`
	ProbeIDs           []string `json:"probe_ids,omitempty" yaml:"probe_ids,omitempty"`
}

type ClassifiedProbe struct {
	ID          string   `json:"id" yaml:"id"`
	QuestionID  string   `json:"question_id" yaml:"question_id"`
	Disposition string   `json:"disposition" yaml:"disposition"`
	Kind        string   `json:"kind" yaml:"kind"`
	SafetyClass string   `json:"safety_class" yaml:"safety_class"`
	ReasonCodes []string `json:"reason_codes,omitempty" yaml:"reason_codes,omitempty"`
	TargetFiles []string `json:"target_files,omitempty" yaml:"target_files,omitempty"`
}

type BlockerGroup struct {
	ID                    string   `json:"id" yaml:"id"`
	RootBlockerID         string   `json:"root_blocker_id" yaml:"root_blocker_id"`
	DependentBlockerIDs   []string `json:"dependent_blocker_ids,omitempty" yaml:"dependent_blocker_ids,omitempty"`
	QuestionIDs           []string `json:"question_ids,omitempty" yaml:"question_ids,omitempty"`
	ProbeIDs              []string `json:"probe_ids,omitempty" yaml:"probe_ids,omitempty"`
	AffectedFiles         []string `json:"affected_files,omitempty" yaml:"affected_files,omitempty"`
	AdmissionConsequences []string `json:"admission_consequences,omitempty" yaml:"admission_consequences,omitempty"`
}

type NextAction struct {
	Kind        string `json:"kind" yaml:"kind"`
	TargetID    string `json:"target_id,omitempty" yaml:"target_id,omitempty"`
	Summary     string `json:"summary" yaml:"summary"`
	CommandHint string `json:"command_hint,omitempty" yaml:"command_hint,omitempty"`
}

type Summary struct {
	TotalBlockers              int `json:"total_blockers" yaml:"total_blockers"`
	ActiveRootBlockers         int `json:"active_root_blockers" yaml:"active_root_blockers"`
	MechanicallyResolved       int `json:"mechanically_resolved" yaml:"mechanically_resolved"`
	StaticProbeAnswerable      int `json:"static_probe_answerable" yaml:"static_probe_answerable"`
	ArchitectJudgementRequired int `json:"architect_judgement_required" yaml:"architect_judgement_required"`
	NonBlockingUnknown         int `json:"non_blocking_unknown" yaml:"non_blocking_unknown"`
	Dominated                  int `json:"dominated" yaml:"dominated"`
	Duplicate                  int `json:"duplicate" yaml:"duplicate"`
	ActiveUnresolved           int `json:"active_unresolved" yaml:"active_unresolved"`
	Uncertifiable              int `json:"uncertifiable" yaml:"uncertifiable"`
}

type EvidenceProgress struct {
	Total        int `json:"total" yaml:"total"`
	Eligible     int `json:"eligible" yaml:"eligible"`
	Completed    int `json:"completed" yaml:"completed"`
	Inconclusive int `json:"inconclusive" yaml:"inconclusive"`
	Failed       int `json:"failed" yaml:"failed"`
	Rejected     int `json:"rejected" yaml:"rejected"`
	Unavailable  int `json:"unavailable" yaml:"unavailable"`
}

type TaskControlState struct {
	SchemaVersion       string                            `json:"schema_version" yaml:"schema_version"`
	GeneratedBy         string                            `json:"generated_by" yaml:"generated_by"`
	Binding             architecture.ClaimDocumentBinding `json:"binding" yaml:"binding"`
	TaskID              string                            `json:"task_id" yaml:"task_id"`
	Iteration           int                               `json:"iteration" yaml:"iteration"`
	GeneratedAt         string                            `json:"generated_at" yaml:"generated_at"`
	BindingHealth       string                            `json:"binding_health" yaml:"binding_health"`
	Permission          PermissionSummary                 `json:"permission" yaml:"permission"`
	Summary             Summary                           `json:"summary" yaml:"summary"`
	Evidence            EvidenceProgress                  `json:"automatic_evidence" yaml:"automatic_evidence"`
	Blockers            []ClassifiedBlocker               `json:"blockers" yaml:"blockers"`
	Questions           []ClassifiedQuestion              `json:"questions" yaml:"questions"`
	Probes              []ClassifiedProbe                 `json:"probes" yaml:"probes"`
	Groups              []BlockerGroup                    `json:"groups" yaml:"groups"`
	PrimaryBlocker      *ClassifiedBlocker                `json:"primary_blocker,omitempty" yaml:"primary_blocker,omitempty"`
	PrimaryQuestion     *ClassifiedQuestion               `json:"primary_question,omitempty" yaml:"primary_question,omitempty"`
	NextAction          NextAction                        `json:"next_action" yaml:"next_action"`
	Limitations         []string                          `json:"limitations,omitempty" yaml:"limitations,omitempty"`
	Receipts            []string                          `json:"receipts,omitempty" yaml:"receipts,omitempty"`
	ReceiptDigestSHA256 string                            `json:"receipt_digest_sha256" yaml:"receipt_digest_sha256"`
}

type Envelope struct {
	TaskControl TaskControlState `json:"task_control" yaml:"task_control"`
}

type DominanceEdge struct {
	DominatorID string
	DominatedID string
	ReasonCode  string
}
