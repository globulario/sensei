// SPDX-License-Identifier: AGPL-3.0-only

// Package evaluatorcomposition is the canonical data-model layer for the O4
// governed evaluator composition contract
// (docs/design/governed-evaluator-composition-o4.md), checkpoint 2: the
// closed EvaluationPolicy, EvaluatorDescriptor, EvaluationInput,
// EvaluatorResult, and EvaluationReceipt documents, plus their JSON Schema
// validation, normalization, and semantic digests.
//
// This package owns no O3 handoff production, no CandidateArtifact loading,
// no providerport.MapToCommand call, no O1 transition, no evaluator
// execution, no candidate materialization, no Sensei/mechanical adapter,
// and no final composition -- those are later, separate checkpoints (3-5),
// not part of this checkpoint's scope. It composes with existing typed
// owners rather than inventing parallel ones: synthesis.CheckObservation
// and synthesis.Limitation are reused verbatim (not re-shaped) for every
// evaluator-facing observation, and synthesis.Recommendation's closed
// five-value vocabulary is referenced, never duplicated -- a policy's own
// failure-class rules may only ever assign one of its four non-accept
// values (see hard law 9 and the "Initial recommendation precedence"
// section of the design doc).
package evaluatorcomposition

import (
	"fmt"

	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// --- sensei.evaluatorcomposition.evaluationpolicy.v1 ---

const EvaluationPolicySchemaVersion = "sensei.evaluatorcomposition.evaluationpolicy.v1"

// EvaluatorSpec names one evaluator this policy selects and states whether
// it is required or optional (design doc "Evaluation policy": "required
// versus optional evaluator status"). O4 never adds, removes, or flips this
// itself -- only the caller-supplied policy states it, and only once.
type EvaluatorSpec struct {
	EvaluatorID string `json:"evaluator_id"`
	Required    bool   `json:"required"`
}

// FailureClassRecommendation binds one named failure class to one of the
// four non-accept Recommendation values. Recommendation is never
// synthesis.RecommendAcceptCandidate here -- accept-candidate is legal only
// through the unanimous Recommendation hard floor (design doc), never a
// per-failure-class policy assignment. When FailureClass names a
// GovernedFailureClass, ValidateEvaluationPolicy additionally rejects any
// Recommendation less severe than that class's canonical minimum -- a
// policy may narrow the contract's fixed initial recommendation precedence
// by choosing an equally or more severe outcome (escalation) but may never
// downgrade a governed class below its floor or reorder the precedence
// itself.
type FailureClassRecommendation struct {
	FailureClass   string                   `json:"failure_class"`
	Recommendation synthesis.Recommendation `json:"recommendation"`
}

// GovernedFailureClass names a failure class this contract itself
// recognizes, drawn directly from the design doc's "Initial recommendation
// precedence" section's own illustrative examples, and binds to a
// canonical minimum severity. A failure_class string outside this registry
// is not bound to any floor by this package -- evaluators and policies
// remain free to define their own ad hoc classes, subject only to the
// general closed-recommendation-vocabulary constraint every
// FailureClassRecommendation already carries.
type GovernedFailureClass string

const (
	// FailureClassAuditForbiddenFix: "a blocking sensei edit-check/sensei
	// gate --enforce forbidden-fix violation" -- design doc, abort example.
	FailureClassAuditForbiddenFix GovernedFailureClass = "audit-forbidden-fix"
	// FailureClassProofPermanentlyUndischargeable: "a proof obligation
	// classified as permanently undischargeable against this candidate" --
	// design doc, abort example.
	FailureClassProofPermanentlyUndischargeable GovernedFailureClass = "proof-obligation-permanently-undischargeable"
	// FailureClassIncidentScarConcerning: "an incident/scar match flagged
	// as concerning but not itself blocking" -- design doc, architect-review
	// example.
	FailureClassIncidentScarConcerning GovernedFailureClass = "incident-scar-concerning"
	// FailureClassProofPlanStructural: "a proof obligation that is
	// structurally undischargeable under the plan's current step sequence"
	// -- design doc, replan example.
	FailureClassProofPlanStructural GovernedFailureClass = "proof-obligation-plan-structural"
	// FailureClassAuditPlanLevel: "a mechanical/audit failure the policy
	// classifies as plan-level rather than attempt-level" -- design doc,
	// replan example.
	FailureClassAuditPlanLevel GovernedFailureClass = "audit-plan-level-failure"
	// FailureClassMechanicalCheckFailure: "a mechanical test failure with
	// no plan- or policy-level classification" -- design doc,
	// retry-generation example, and the default/lowest-severity outcome.
	FailureClassMechanicalCheckFailure GovernedFailureClass = "mechanical-check-failure"
)

// GovernedFailureClassMinimumRecommendation is the closed registry binding
// each GovernedFailureClass to its canonical minimum (most lenient
// permitted) Recommendation. It is the "canonical minimum recommendation
// for governed failure classes" the design doc's precedence section
// requires: a policy may map a governed class to its minimum or to
// anything strictly more severe, never to anything less severe.
var GovernedFailureClassMinimumRecommendation = map[GovernedFailureClass]synthesis.Recommendation{
	FailureClassAuditForbiddenFix:               synthesis.RecommendAbort,
	FailureClassProofPermanentlyUndischargeable: synthesis.RecommendAbort,
	FailureClassIncidentScarConcerning:          synthesis.RecommendArchitectReview,
	FailureClassProofPlanStructural:             synthesis.RecommendReplan,
	FailureClassAuditPlanLevel:                  synthesis.RecommendReplan,
	FailureClassMechanicalCheckFailure:          synthesis.RecommendRetryGeneration,
}

// recommendationSeverityRank orders the four non-accept Recommendation
// values from most severe (0) to least severe (3), exactly matching the
// design doc's fixed "Initial recommendation precedence": abort >
// architect-review > replan > retry-generation.
var recommendationSeverityRank = map[synthesis.Recommendation]int{
	synthesis.RecommendAbort:           0,
	synthesis.RecommendArchitectReview: 1,
	synthesis.RecommendReplan:          2,
	synthesis.RecommendRetryGeneration: 3,
}

// GovernedFailureClassMinimumRecommendationFor returns class's canonical
// minimum Recommendation and true when class is a recognized
// GovernedFailureClass, or the zero Recommendation and false otherwise.
func GovernedFailureClassMinimumRecommendationFor(class string) (synthesis.Recommendation, bool) {
	r, ok := GovernedFailureClassMinimumRecommendation[GovernedFailureClass(class)]
	return r, ok
}

// EvaluationPolicy is the one immutable, self-digested, caller-supplied
// policy document O4 validates and applies but never constructs, defaults,
// or rewrites (hard law 9). It binds the exact Session/Attempt/candidate
// identity in play, so a policy authored for one session, attempt, or
// candidate can never be silently reused for another.
type EvaluationPolicy struct {
	SchemaVersion string `json:"schema_version"`
	PolicyID      string `json:"policy_id"`

	SessionDigestSHA256           string `json:"session_digest_sha256"`
	AttemptDigestSHA256           string `json:"attempt_digest_sha256"`
	CandidateArtifactDigestSHA256 string `json:"candidate_artifact_digest_sha256"`

	Evaluators []EvaluatorSpec `json:"evaluators"`

	DeadlineAt       string `json:"deadline_at"`
	MaxEvidenceCount int    `json:"max_evidence_count"`
	MaxEvidenceBytes int64  `json:"max_evidence_bytes"`

	RequiredCheckIDs []string `json:"required_check_ids"`

	FailureClassRecommendations []FailureClassRecommendation `json:"failure_class_recommendations"`

	// PolicyDigestSHA256 is the self-referential semantic digest of this
	// document with this field zeroed before hashing.
	PolicyDigestSHA256 string `json:"policy_digest_sha256"`
}

// --- sensei.evaluatorcomposition.evaluatordescriptor.v1 ---

const EvaluatorDescriptorSchemaVersion = "sensei.evaluatorcomposition.evaluatordescriptor.v1"

// EvaluatorDescriptor is one evaluator's closed, self-describing identity.
// Evaluator kind is data, not interface shape (design doc "Evaluator
// model") -- mechanical, Sensei-owned, and external evaluators all describe
// themselves through this one document, never a bespoke Go type per kind.
type EvaluatorDescriptor struct {
	SchemaVersion string `json:"schema_version"`

	EvaluatorID      string `json:"evaluator_id"`
	EvaluatorKind    string `json:"evaluator_kind"`
	EvaluatorVersion string `json:"evaluator_version"`

	SupportedCheckIDs    []string `json:"supported_check_ids"`
	Deterministic        bool     `json:"deterministic"`
	RequiredCapabilities []string `json:"required_capabilities"`

	Limitations []synthesis.Limitation `json:"limitations"`

	// DescriptorDigestSHA256 is the self-referential semantic digest of
	// this document with this field zeroed before hashing.
	DescriptorDigestSHA256 string `json:"descriptor_digest_sha256"`
}

// --- sensei.evaluatorcomposition.evaluationinput.v1 ---

const EvaluationInputSchemaVersion = "sensei.evaluatorcomposition.evaluationinput.v1"

// EvaluationInput is the exact, closed input one evaluator invocation binds
// to (design doc "Evaluator model": "EvaluationInput binds at minimum").
// EvaluatorSurfaceRef is an opaque reference to a fresh, disposable
// per-evaluator materialization -- this checkpoint fixes only its shape as
// data; checkpoint 4 gives it real filesystem-backed meaning and lifecycle.
type EvaluationInput struct {
	SchemaVersion string `json:"schema_version"`

	SessionDigestSHA256           string `json:"session_digest_sha256"`
	AttemptDigestSHA256           string `json:"attempt_digest_sha256"`
	CandidateArtifactDigestSHA256 string `json:"candidate_artifact_digest_sha256"`

	RepositoryDomain string `json:"repository_domain"`
	BaseRevision     string `json:"base_revision"`

	PlanGeneration int `json:"plan_generation"`
	AttemptNumber  int `json:"attempt_number"`

	EvaluatorSurfaceRef string `json:"evaluator_surface_ref"`

	DeadlineAt       string `json:"deadline_at"`
	MaxEvidenceCount int    `json:"max_evidence_count"`
	MaxEvidenceBytes int64  `json:"max_evidence_bytes"`

	RequiredProofObligationDigests []string `json:"required_proof_obligation_digests"`

	// EvaluationInputDigestSHA256 is the self-referential semantic digest
	// of this document with this field zeroed before hashing.
	EvaluationInputDigestSHA256 string `json:"evaluation_input_digest_sha256"`
}

// --- sensei.evaluatorcomposition.evaluatorresult.v1 ---

const EvaluatorResultSchemaVersion = "sensei.evaluatorcomposition.evaluatorresult.v1"

// EvaluatorTerminalOutcome is the closed vocabulary for how one evaluator
// invocation ended. It describes only how the EVALUATOR ended, never
// correctness, admission, or completion -- mirroring providerport's own
// TerminalOutcome shape at the evaluator layer, but kept as its own type
// since not every evaluator (only external class-5 evaluators) is an O2
// Provider.
type EvaluatorTerminalOutcome string

const (
	EvaluatorOutcomeCompleted   EvaluatorTerminalOutcome = "completed"
	EvaluatorOutcomeUnavailable EvaluatorTerminalOutcome = "unavailable"
	EvaluatorOutcomeTimedOut    EvaluatorTerminalOutcome = "timed_out"
	EvaluatorOutcomeCancelled   EvaluatorTerminalOutcome = "cancelled"
)

// AllEvaluatorTerminalOutcomes returns the four closed EvaluatorTerminalOutcome
// values.
func AllEvaluatorTerminalOutcomes() []EvaluatorTerminalOutcome {
	return []EvaluatorTerminalOutcome{
		EvaluatorOutcomeCompleted,
		EvaluatorOutcomeUnavailable,
		EvaluatorOutcomeTimedOut,
		EvaluatorOutcomeCancelled,
	}
}

// EvidenceReference is one (reference, digest) pair backing an evaluator
// observation -- reference is a stable identifier (a captured-log path, a
// command's stdout blob ID, and so on); DigestSHA256 lets a consumer verify
// the referenced content has not silently changed underneath the reference.
// Reference is unique within one EvaluatorResult's EvidenceReferences list
// -- ValidateEvaluatorResult rejects a repeated Reference, whether or not
// its DigestSHA256 also conflicts.
type EvidenceReference struct {
	Reference    string `json:"reference"`
	DigestSHA256 string `json:"digest_sha256"`
}

// EvaluatorResult is one evaluator's own observation document -- never an
// O1 synthesis.Evaluation, never authoritative (hard law 7). It has no
// Recommendation field at all: only the O4 composer maps evidence into
// synthesis.Evaluation.Recommendation.
//
// A digest-valid EvaluatorResult is not automatically a semantically
// coherent one: ValidateEvaluatorResult additionally rejects duplicate
// Checks[i].CheckID values, a check-level EvidenceReferences entry that
// does not resolve to some top-level EvidenceReferences[j].Reference,
// duplicate or digest-conflicting top-level EvidenceReferences, and empty
// or duplicate ClassifiedFailureReasons -- an evaluator must not be able to
// report impossible or self-contradictory evidence just because its shape
// happens to validate.
type EvaluatorResult struct {
	SchemaVersion string `json:"schema_version"`

	EvaluatorID                     string `json:"evaluator_id"`
	EvaluatorDescriptorDigestSHA256 string `json:"evaluator_descriptor_digest_sha256"`
	EvaluationInputDigestSHA256     string `json:"evaluation_input_digest_sha256"`

	TerminalOutcome EvaluatorTerminalOutcome `json:"terminal_outcome"`

	Checks []synthesis.CheckObservation `json:"checks"`

	EvidenceReferences []EvidenceReference `json:"evidence_references"`

	ClassifiedFailureReasons []string               `json:"classified_failure_reasons"`
	Limitations              []synthesis.Limitation `json:"limitations"`

	// CleanupSucceeded is this evaluator's own disposable-surface cleanup
	// truth. Nullable: checkpoint 2 fixes only its shape; real conditional
	// semantics (tied to TerminalOutcome and whether a surface was ever
	// constructed) arrive at checkpoint 4 when materialization itself is
	// implemented.
	CleanupSucceeded *bool `json:"cleanup_succeeded"`

	// ResultDigestSHA256 is the self-referential semantic digest of this
	// document with this field zeroed before hashing.
	ResultDigestSHA256 string `json:"result_digest_sha256"`
}

// --- sensei.evaluatorcomposition.evaluationreceipt.v1 ---

const EvaluationReceiptSchemaVersion = "sensei.evaluatorcomposition.evaluationreceipt.v1"

// Disposition is the closed, six-value vocabulary stating exactly how far
// one governed evaluation composition progressed. It is not a correctness,
// admission, or completion verdict -- see the design doc's disposition
// tables for the authoritative field-by-field presence rule each value
// implies; FieldPresenceFor and O1TerminalReceiptRequirementFor below are
// those tables encoded as data.
type Disposition string

const (
	// DispositionInvalidOutputTerminated: the accepted Attempt's own
	// TerminalProviderStatus was invalid_output. O1's first
	// RecordAttemptCommand Transition call already terminated the session
	// (ReasonInvalidProviderOutput) before PhaseEvaluating was ever
	// entered. O4 makes no second Transition call for this disposition.
	DispositionInvalidOutputTerminated Disposition = "invalid-output-terminated"
	// DispositionCandidateLoadFailure: the exact sealed artifact could not
	// be loaded or validated after the generation handoff was accepted.
	DispositionCandidateLoadFailure Disposition = "candidate-load-failure"
	// DispositionMaterializationFailure: a required evaluator surface
	// could not be constructed from the sealed artifact.
	DispositionMaterializationFailure Disposition = "materialization-failure"
	// DispositionRequiredEvaluatorUnavailable: a required evaluator could
	// not produce a valid terminal result.
	DispositionRequiredEvaluatorUnavailable Disposition = "required-evaluator-unavailable"
	// DispositionCompositionFailure: evaluator results existed but could
	// not be composed into a valid Evaluation under the accepted policy.
	DispositionCompositionFailure Disposition = "composition-failure"
	// DispositionEvaluated: a valid O1 Evaluation was composed and
	// accepted by synthesis.Transition. The resulting O1 phase varies by
	// Recommendation and remaining budget -- see
	// O1TerminalReceiptRequirementFor.
	DispositionEvaluated Disposition = "evaluated"
)

// AllDispositions returns the six closed Disposition values.
func AllDispositions() []Disposition {
	return []Disposition{
		DispositionInvalidOutputTerminated,
		DispositionCandidateLoadFailure,
		DispositionMaterializationFailure,
		DispositionRequiredEvaluatorUnavailable,
		DispositionCompositionFailure,
		DispositionEvaluated,
	}
}

// EvaluatorResultBinding names the exact evaluator an EvaluationReceipt's
// result digest belongs to, plus that evaluator's descriptor digest -- the
// design doc's "ordered evaluator descriptor/result digests" as one typed
// pair per evaluator, rather than a bare digest list a reader would have to
// dereference every EvaluatorResult to attribute.
type EvaluatorResultBinding struct {
	EvaluatorID            string `json:"evaluator_id"`
	DescriptorDigestSHA256 string `json:"descriptor_digest_sha256"`
	ResultDigestSHA256     string `json:"result_digest_sha256"`
}

// EvaluationReceipt is O4's own closed evidence document binding the entire
// evaluation composition without replacing O1's synthesis.Evaluation or
// terminal synthesis.Receipt.
//
// Fields present on every disposition, because they come from the
// already-validated generation handoff and policy validation and exist
// before O4's first Transition call (design doc laws 1-9): SessionDigestSHA256,
// AttemptDigestSHA256, RunnerReceiptDigestSHA256, RequestDigestSHA256,
// ResultDigestSHA256, O2ReceiptDigestSHA256, PolicyDigestSHA256,
// CandidateArtifactDigestSHA256, Disposition, and ReceiptDigestSHA256. The
// remaining fields' presence is exactly FieldPresenceFor(Disposition) and
// O1TerminalReceiptRequirementFor(Disposition) -- see ValidateEvaluationReceipt,
// which enforces this.
type EvaluationReceipt struct {
	SchemaVersion string `json:"schema_version"`
	ReceiptID     string `json:"receipt_id"`

	SessionDigestSHA256       string `json:"session_digest_sha256"`
	AttemptDigestSHA256       string `json:"attempt_digest_sha256"`
	RunnerReceiptDigestSHA256 string `json:"runner_receipt_digest_sha256"`

	// RequestDigestSHA256 / ResultDigestSHA256 / O2ReceiptDigestSHA256
	// reference O2's own, unaltered Request/Result/Receipt (hard law 1).
	RequestDigestSHA256   string `json:"request_digest_sha256"`
	ResultDigestSHA256    string `json:"result_digest_sha256"`
	O2ReceiptDigestSHA256 string `json:"o2_receipt_digest_sha256"`

	// PolicyDigestSHA256 is always present -- policy validates as a
	// precondition alongside generation-handoff validation, before the
	// first Transition call (law 9).
	PolicyDigestSHA256 string `json:"policy_digest_sha256"`

	// CandidateArtifactDigestSHA256 is always present as a reference from
	// the O3 receipt; CandidateArtifactVerified states whether it was
	// additionally loaded and cross-bound (false for
	// DispositionInvalidOutputTerminated/DispositionCandidateLoadFailure,
	// true otherwise).
	CandidateArtifactDigestSHA256 string `json:"candidate_artifact_digest_sha256"`
	CandidateArtifactVerified     bool   `json:"candidate_artifact_verified"`

	// EvaluatorResultBindings binds each contributing evaluator's own
	// identity to its result -- a bare digest list cannot answer "which
	// evaluator produced this result" without loading and cross-referencing
	// every EvaluatorResult in turn, which is exactly the kind of
	// reconstruction this contract forbids elsewhere. Must be sorted in
	// strictly ascending EvaluatorID order with no duplicate EvaluatorID
	// (ValidateEvaluationReceipt enforces both -- the design doc's
	// "evaluator results ordered by evaluator ID" canonical-ordering
	// requirement, hard law 18). Must be empty for
	// DispositionInvalidOutputTerminated/DispositionCandidateLoadFailure;
	// may be any length, including empty, for every other disposition.
	EvaluatorResultBindings []EvaluatorResultBinding `json:"evaluator_result_bindings"`

	// EvaluationDigestSHA256 is non-nil only for DispositionEvaluated.
	EvaluationDigestSHA256 *string `json:"evaluation_digest_sha256"`

	// O1TerminalReceiptDigestSHA256 references synthesis.Receipt.ReceiptDigestSHA256
	// (equivalently, the ReceiptDigestSHA256 carried on the SessionTerminatedEvent
	// the triggering Transition call returned). Required non-nil for every
	// disposition except DispositionEvaluated, where it is present only
	// when the resulting O1 phase is terminal and absent when
	// RecordEvaluationCommand hands the session to PhaseRetry/PhaseReplan
	// -- see O1TerminalReceiptRequirementFor.
	O1TerminalReceiptDigestSHA256 *string `json:"o1_terminal_receipt_digest_sha256"`

	Disposition Disposition `json:"disposition"`

	// FailureDetail is required non-empty exactly when Disposition is not
	// DispositionEvaluated; empty when evaluated.
	FailureDetail string `json:"failure_detail"`

	// CleanupSucceeded is nil for DispositionInvalidOutputTerminated/
	// DispositionCandidateLoadFailure (no evaluator surface was ever
	// constructed); a non-nil boolean for every other disposition.
	CleanupSucceeded *bool `json:"cleanup_succeeded"`
	// CleanupFailureDetail is required non-empty exactly when
	// CleanupSucceeded is non-nil and false; empty otherwise.
	CleanupFailureDetail string `json:"cleanup_failure_detail"`

	// CompletedAt is an observation timestamp explicitly excluded from
	// ReceiptDigestSHA256 (see EvaluationReceiptDigest in digest.go) so
	// receipt identity does not depend on wall-clock time.
	CompletedAt string `json:"completed_at"`

	ReceiptDigestSHA256 string `json:"receipt_digest_sha256"`
}

// EvaluationReceiptFieldPresence states the required presence for
// EvaluationReceipt's disposition-conditional fields other than
// O1TerminalReceiptDigestSHA256 (see O1TerminalReceiptRequirementFor for
// that one, since it is not a strict function of Disposition alone).
type EvaluationReceiptFieldPresence struct {
	CandidateArtifactVerified          bool
	EvaluatorResultBindingsMustBeEmpty bool
	EvaluationDigest                   bool
	CleanupSucceeded                   bool
}

// FieldPresenceFor returns the required presence shape for d. Returns an
// error if d is not one of the six closed Disposition values.
func FieldPresenceFor(d Disposition) (EvaluationReceiptFieldPresence, error) {
	switch d {
	case DispositionInvalidOutputTerminated, DispositionCandidateLoadFailure:
		return EvaluationReceiptFieldPresence{
			CandidateArtifactVerified:          false,
			EvaluatorResultBindingsMustBeEmpty: true,
			EvaluationDigest:                   false,
			CleanupSucceeded:                   false,
		}, nil
	case DispositionMaterializationFailure, DispositionRequiredEvaluatorUnavailable, DispositionCompositionFailure:
		return EvaluationReceiptFieldPresence{
			CandidateArtifactVerified:          true,
			EvaluatorResultBindingsMustBeEmpty: false,
			EvaluationDigest:                   false,
			CleanupSucceeded:                   true,
		}, nil
	case DispositionEvaluated:
		return EvaluationReceiptFieldPresence{
			CandidateArtifactVerified:          true,
			EvaluatorResultBindingsMustBeEmpty: false,
			EvaluationDigest:                   true,
			CleanupSucceeded:                   true,
		}, nil
	default:
		return EvaluationReceiptFieldPresence{}, fmt.Errorf("evaluatorcomposition: %q is not one of the six closed Disposition values", d)
	}
}

// O1TerminalReceiptRequirement states whether O1TerminalReceiptDigestSHA256
// must be non-nil, or may legitimately be either, for a given Disposition.
// Unlike every other presence rule, this one is NOT a strict function of
// Disposition alone for DispositionEvaluated -- see the type's doc comment.
type O1TerminalReceiptRequirement int

const (
	// O1TerminalReceiptRequired: O1TerminalReceiptDigestSHA256 must be a
	// non-nil string. O1 has definitely already produced, or is about to
	// produce, a terminal receipt.
	O1TerminalReceiptRequired O1TerminalReceiptRequirement = iota
	// O1TerminalReceiptAmbiguous: O1TerminalReceiptDigestSHA256 may
	// legitimately be either nil or a non-nil string. Only
	// DispositionEvaluated has this requirement -- whether the resulting
	// O1 phase is terminal (present) or PhaseRetry/PhaseReplan (absent)
	// is not knowable from Disposition alone.
	O1TerminalReceiptAmbiguous
)

// O1TerminalReceiptRequirementFor returns d's O1TerminalReceiptRequirement.
// Returns an error if d is not one of the six closed Disposition values.
func O1TerminalReceiptRequirementFor(d Disposition) (O1TerminalReceiptRequirement, error) {
	switch d {
	case DispositionInvalidOutputTerminated, DispositionCandidateLoadFailure,
		DispositionMaterializationFailure, DispositionRequiredEvaluatorUnavailable, DispositionCompositionFailure:
		return O1TerminalReceiptRequired, nil
	case DispositionEvaluated:
		return O1TerminalReceiptAmbiguous, nil
	default:
		return 0, fmt.Errorf("evaluatorcomposition: %q is not one of the six closed Disposition values", d)
	}
}
