// SPDX-License-Identifier: AGPL-3.0-only

// Package synthesis is the canonical data-model layer for the O1 governed
// synthesis session contract (docs/design/governed-synthesis-session-o1.md):
// six closed typed documents — Session, Interpretation, Plan, Attempt,
// Evaluation, Receipt — plus their JSON Schema validation and semantic
// digests. It is the first bounded, deterministic orchestration layer for
// governed software evolution: intelligence may explore, but authority
// remains deterministic, and this package owns no authority itself.
//
// It composes or projects existing Sensei-owned facts into a new closed
// external contract; it never invents a parallel repository, workspace,
// task, graph, closure, proof, admission, or verification owner:
//
//   - repository domain / base revision / workspace identity — projected
//     from architecture.ClaimDocumentBinding / workspacecontract.Identity;
//   - graph authority — projected from workspacecontract.GraphAuthority;
//   - task identity and task-session identity — referenced by digest to
//     golang/architecture/tasksession.Session (a DIFFERENT "Session" concept:
//     tasksession.Session is the governed identity/lifecycle of the task
//     itself; synthesis.Session is the bounded planning/implementation/
//     evaluation process running for that task. Package qualification keeps
//     the two distinct; this package never embeds or recreates
//     tasksession.Session, only references its SessionDigestSHA256);
//   - architectural closure / proof obligations — referenced by digest to
//     golang/architecture/closureprotocol (BaseBinding, ProofDischarge).
//
// Admission and verification identity are deliberately NOT part of
// Session's own identity: a session is created before any candidate has
// been submitted for admission. They appear only on Receipt, as opaque
// AdmissionDecisionDigestSHA256 / AdmissionVerificationDigestSHA256
// references (naming taken verbatim from closureprotocol.CompletionReceipt),
// and only when terminal_reason is candidate-ready-for-admission. Which
// admission framework produces those digests — golang/architecture/admission
// ("v1") or closureprotocol's AdmissionDecision/AuthorityResolution ("v2")
// — is an explicitly OPEN QUESTION this package does not resolve; O1 stays
// agnostic to that choice, deferring it to O5 (the admission bridge), exactly
// as closureprotocol.CompletionReceipt already does for the same fields.
//
// This package owns no model invocation, process execution, worktree
// mutation, GitHub call, or admission decision. It is pure data: closed
// schemas, typed Go documents, canonical normalization, and semantic
// digests. The deterministic transition state machine that operates on
// these documents is a separate, later piece of work (O1 step 2), not part
// of this package's Step 1 scope.
//
// A passing Evaluation is evidence, not admission, correctness, completion,
// approval, or merge authorization. candidate-ready-for-admission on a
// Receipt means only that a candidate may be submitted to the existing
// admission owner.
package synthesis

// Limitation mirrors architecture.Limitation / workspacecontract.Limitation
// verbatim, as its own closed local type — this package's documents
// describe limitations, they do not carry authority over them.
type Limitation struct {
	Source   string `json:"source"`
	Scope    string `json:"scope"`
	Reason   string `json:"reason"`
	Blocking bool   `json:"blocking"`
}

// SourceReference is one (reference, digest) pair backing an Interpretation
// fact — the reference is a stable identifier (an awareness node ID, a
// file:line anchor, and so on); the digest lets a consumer verify the
// referenced content has not silently changed underneath the reference.
type SourceReference struct {
	Reference          string `json:"reference"`
	SourceDigestSHA256 string `json:"source_digest_sha256"`
}

// ProviderObservation carries a provider's self-reported identity as
// evidence only. It is never session authority: a provider cannot grant
// itself capability by asserting an identity here, and this package treats
// every field as untrusted input to be recorded, not trusted.
type ProviderObservation struct {
	ProviderID      string `json:"provider_id"`
	ProviderKind    string `json:"provider_kind"`
	ModelIdentifier string `json:"model_identifier,omitempty"`
	ObservedAt      string `json:"observed_at"`
}

// --- sensei.synthesis.session.v1 ---

// SessionSchemaVersion is the exact, adopted schema identifier for Session.
const SessionSchemaVersion = "sensei.synthesis.session.v1"

// GeneratedBy identifies this package as the producer of every document it
// composes.
const GeneratedBy = "sensei synthesis"

// Session is the top-level sensei.synthesis.session.v1 document: a
// provider-independent, bounded orchestration record bound to exact
// repository, workspace-identity, graph-authority, task-session, and
// architectural-closure identity, with a precommitted retry/replan budget.
// Every identity field below is either a plain, directly-comparable value
// (RepositoryDomain, BaseRevision — needed for direct drift comparison, not
// just an opaque digest) or an exact digest reference to an existing typed
// owner. See the package doc comment for why admission/verification
// identity is intentionally absent here.
type Session struct {
	SchemaVersion string `json:"schema_version"`
	SessionID     string `json:"session_id"`
	GeneratedBy   string `json:"generated_by"`

	RepositoryDomain string `json:"repository_domain"`
	BaseRevision     string `json:"base_revision"`

	// WorkspaceIdentityDigestSHA256 references a workspacecontract.Identity
	// (repository domain/revision/graph-digest binding, coverage, task
	// identity) — the existing canonical owner for "repository root or
	// workspace identity."
	WorkspaceIdentityDigestSHA256 string `json:"workspace_identity_digest_sha256"`
	// GraphAuthorityDigestSHA256 references the graph authority/marker
	// identity (workspacecontract.GraphAuthority /
	// closureprotocol.GraphSnapshot) independently of
	// WorkspaceIdentityDigestSHA256, so graph drift is separately
	// detectable from workspace-identity drift on session resumption.
	GraphAuthorityDigestSHA256 string `json:"graph_authority_digest_sha256"`
	// TaskSessionDigestSHA256 references tasksession.Session.SessionDigestSHA256
	// — the existing canonical owner for task identity and task-session
	// identity. Never embedded, only referenced.
	TaskSessionDigestSHA256 string `json:"task_session_digest_sha256"`
	// ClosureDigestSHA256 references the architectural closure/briefing
	// artifact identity for this task (a closureprotocol digest — e.g. a
	// ClosureAssessment or BaseBinding digest produced by the existing
	// closure owner).
	ClosureDigestSHA256 string `json:"closure_digest_sha256"`
	// ProofObligationDigests references zero or more
	// closureprotocol.ProofDischarge.DischargeDigestSHA256 values relevant
	// to this session's objective.
	ProofObligationDigests []string `json:"proof_obligation_digests"`

	Objective    string `json:"objective"`
	RetryBudget  int    `json:"retry_budget"`
	ReplanBudget int    `json:"replan_budget"`

	CreatedAt string `json:"created_at"`

	// SessionDigestSHA256 is the self-referential semantic digest of this
	// document with this field zeroed before hashing.
	SessionDigestSHA256 string `json:"session_digest_sha256"`
}

// --- sensei.synthesis.interpretation.v1 ---

const InterpretationSchemaVersion = "sensei.synthesis.interpretation.v1"

// Interpretation is Sensei's stronger equivalent of ARCHER's
// rule-interpretation document: evidence supplied to planning, derived from
// existing governed inputs. It is not new canonical architectural truth and
// cannot promote candidates.
type Interpretation struct {
	SchemaVersion       string `json:"schema_version"`
	InterpretationID    string `json:"interpretation_id"`
	SessionDigestSHA256 string `json:"session_digest_sha256"`
	GeneratedBy         string `json:"generated_by"`

	Objective                string            `json:"objective"`
	ApplicableIntent         []string          `json:"applicable_intent"`
	BindingInvariants        []string          `json:"binding_invariants"`
	RelevantContracts        []string          `json:"relevant_contracts"`
	AuthorityBoundaries      []string          `json:"authority_boundaries"`
	KnownFailureModes        []string          `json:"known_failure_modes"`
	ForbiddenFixes           []string          `json:"forbidden_fixes"`
	RequiredProofObligations []string          `json:"required_proof_obligations"`
	Assumptions              []string          `json:"assumptions"`
	UnresolvedQuestions      []string          `json:"unresolved_questions"`
	SourceReferences         []SourceReference `json:"source_references"`
	Limitations              []Limitation      `json:"limitations"`

	InterpretationDigestSHA256 string `json:"interpretation_digest_sha256"`
}

// --- sensei.synthesis.plan.v1 ---

const PlanSchemaVersion = "sensei.synthesis.plan.v1"

// PlanStep is one ordered, stably-identified step of a Plan.
type PlanStep struct {
	StepID           string   `json:"step_id"`
	Description      string   `json:"description"`
	IntendedFiles    []string `json:"intended_files"`
	IntendedSymbols  []string `json:"intended_symbols"`
	ExpectedEvidence []string `json:"expected_evidence"`
}

// Plan is a provider proposal bound to one Interpretation and one session
// generation. A replan produces a new immutable Plan at PlanGeneration+1;
// it never rewrites the prior generation.
type Plan struct {
	SchemaVersion              string `json:"schema_version"`
	PlanID                     string `json:"plan_id"`
	InterpretationDigestSHA256 string `json:"interpretation_digest_sha256"`
	PlanGeneration             int    `json:"plan_generation"`

	Steps          []PlanStep `json:"steps"`
	Assumptions    []string   `json:"assumptions"`
	Risks          []string   `json:"risks"`
	StopConditions []string   `json:"stop_conditions"`

	ProviderObservation ProviderObservation `json:"provider_observation"`

	PlanDigestSHA256 string `json:"plan_digest_sha256"`
}

// --- sensei.synthesis.attempt.v1 ---

const AttemptSchemaVersion = "sensei.synthesis.attempt.v1"

// TerminalProviderStatus is the closed vocabulary for how a provider's
// attempt ended. This is an initial, bounded set for O1; it describes only
// how the PROVIDER ended, never correctness, admission, or completion.
type TerminalProviderStatus string

const (
	ProviderStatusCompleted     TerminalProviderStatus = "completed"
	ProviderStatusFailed        TerminalProviderStatus = "failed"
	ProviderStatusTimedOut      TerminalProviderStatus = "timed_out"
	ProviderStatusCancelled     TerminalProviderStatus = "cancelled"
	ProviderStatusInvalidOutput TerminalProviderStatus = "invalid_output"
)

// Attempt is one immutable, monotonically-appended attempt against one Plan
// generation. It carries a proposed change-envelope digest — not an
// implicitly trusted mutation — and makes no admission or correctness
// claim.
type Attempt struct {
	SchemaVersion  string `json:"schema_version"`
	AttemptID      string `json:"attempt_id"`
	AttemptNumber  int    `json:"attempt_number"`
	PlanGeneration int    `json:"plan_generation"`

	PlanDigestSHA256           string `json:"plan_digest_sha256"`
	InputCandidateDigestSHA256 string `json:"input_candidate_digest_sha256"`

	ProviderObservation ProviderObservation `json:"provider_observation"`

	ProposedChangeDigestSHA256 string   `json:"proposed_change_digest_sha256"`
	EvidenceReferences         []string `json:"evidence_references"`

	TerminalProviderStatus TerminalProviderStatus `json:"terminal_provider_status"`

	// ProducedAt is an observation timestamp explicitly excluded from
	// AttemptDigestSHA256 (see Digest in digest.go) so attempt identity does
	// not depend on wall-clock time.
	ProducedAt string `json:"produced_at"`

	AttemptDigestSHA256 string `json:"attempt_digest_sha256"`
}

// --- sensei.synthesis.evaluation.v1 ---

const EvaluationSchemaVersion = "sensei.synthesis.evaluation.v1"

// CheckObservationStatus is the closed vocabulary for one check's outcome.
type CheckObservationStatus string

const (
	CheckPassed      CheckObservationStatus = "passed"
	CheckFailed      CheckObservationStatus = "failed"
	CheckSkipped     CheckObservationStatus = "skipped"
	CheckUnavailable CheckObservationStatus = "unavailable"
)

// CheckObservation is one evaluator check's observed outcome.
type CheckObservation struct {
	CheckID            string                 `json:"check_id"`
	Status             CheckObservationStatus `json:"status"`
	Detail             string                 `json:"detail,omitempty"`
	EvidenceReferences []string               `json:"evidence_references"`
}

// Recommendation is the closed vocabulary an Evaluation may recommend. It
// is evidence for the (Step 2) state machine, not a decision the evaluator
// itself makes: the state machine decides whether a recommendation is
// permitted by remaining budgets and session state.
type Recommendation string

const (
	RecommendAcceptCandidate Recommendation = "accept-candidate"
	RecommendRetryGeneration Recommendation = "retry-generation"
	RecommendReplan          Recommendation = "replan"
	RecommendArchitectReview Recommendation = "architect-review"
	RecommendAbort           Recommendation = "abort"
)

// Evaluation represents observations from one or more evaluators without
// collapsing them into authority. A passing evaluation is evidence, not
// admission, correctness, completion, or approval.
type Evaluation struct {
	SchemaVersion       string `json:"schema_version"`
	EvaluationID        string `json:"evaluation_id"`
	AttemptDigestSHA256 string `json:"attempt_digest_sha256"`

	EvaluatorKind    string `json:"evaluator_kind"`
	EvaluatorVersion string `json:"evaluator_version"`

	Checks                   []CheckObservation `json:"checks"`
	ClassifiedFailureReasons []string           `json:"classified_failure_reasons"`
	Recommendation           Recommendation     `json:"recommendation"`
	Limitations              []Limitation       `json:"limitations"`

	EvaluationDigestSHA256 string `json:"evaluation_digest_sha256"`
}

// --- sensei.synthesis.receipt.v1 ---

const ReceiptSchemaVersion = "sensei.synthesis.receipt.v1"

// TerminalReason is the closed vocabulary stating exactly why a session
// stopped. candidate-ready-for-admission means only that a candidate may be
// submitted to the existing admission owner — never accepted, correct,
// complete, approved, mergeable, or merged.
type TerminalReason string

const (
	ReasonCandidateReadyForAdmission TerminalReason = "candidate-ready-for-admission"
	ReasonRetryBudgetExhausted       TerminalReason = "retry-budget-exhausted"
	ReasonReplanBudgetExhausted      TerminalReason = "replan-budget-exhausted"
	ReasonArchitectReviewRequired    TerminalReason = "architect-review-required"
	ReasonIdentityDriftRefused       TerminalReason = "identity-drift-refused"
	ReasonInvalidProviderOutput      TerminalReason = "invalid-provider-output"
	ReasonEvaluatorUnavailable       TerminalReason = "evaluator-unavailable"
	ReasonExplicitlyAborted          TerminalReason = "explicitly-aborted"
)

// Receipt is the terminal disposition of a Session: exactly why it stopped,
// with pointers to the final attempt/evaluation (if any) and, only when
// TerminalReason is ReasonCandidateReadyForAdmission, opaque admission
// decision/verification digest references. See the package doc comment for
// why the admission-framework producer of those two digests is an
// explicitly deferred, unresolved question (O5), not decided here.
type Receipt struct {
	SchemaVersion       string         `json:"schema_version"`
	ReceiptID           string         `json:"receipt_id"`
	SessionDigestSHA256 string         `json:"session_digest_sha256"`
	TerminalReason      TerminalReason `json:"terminal_reason"`

	FinalAttemptDigestSHA256    *string `json:"final_attempt_digest_sha256"`
	FinalEvaluationDigestSHA256 *string `json:"final_evaluation_digest_sha256"`

	// AdmissionDecisionDigestSHA256 / AdmissionVerificationDigestSHA256 name
	// the referenced ARTIFACT, not the producing framework — deliberately,
	// per the package doc comment. Both are nil unless TerminalReason is
	// ReasonCandidateReadyForAdmission.
	AdmissionDecisionDigestSHA256     *string `json:"admission_decision_digest_sha256"`
	AdmissionVerificationDigestSHA256 *string `json:"admission_verification_digest_sha256"`

	RetryCount  int `json:"retry_count"`
	ReplanCount int `json:"replan_count"`

	Summary     string       `json:"summary"`
	Limitations []Limitation `json:"limitations"`

	CompletedAt string `json:"completed_at"`

	ReceiptDigestSHA256 string `json:"receipt_digest_sha256"`
}
