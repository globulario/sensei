// SPDX-License-Identifier: AGPL-3.0-only

// Package providerport is the canonical data-model layer for the O2
// provider-neutral execution port contract
// (docs/design/provider-neutral-execution-port-o2.md): five closed typed
// documents -- Capabilities, Request, Result, ObservationBatch, Receipt --
// plus their JSON Schema validation and semantic digests. This is O2's
// first bounded checkpoint: closed governed contracts, semantic-digest
// helpers, generated Go types, deterministic fixtures, schema validation
// tests, and declared/computed digest-integrity tests only, mirroring the
// shape of O1 Step 1 (golang/architecture/synthesis) applied to the O2
// document family.
//
// This package does not yet define the Provider interface, execute a
// provider, collect observations, or map a provider outcome into an O1
// command or golang/architecture/synthesis.Transition. Those are later,
// explicitly deferred O2 checkpoints. This package is pure data: closed
// schemas, typed Go documents, canonical normalization, and semantic
// digests.
//
// Every Request binds exact O1 session identity
// (golang/architecture/synthesis.Session.SessionDigestSHA256) and the exact
// parent artifact digest a provider is being asked to extend; a provider
// cannot choose or alter plan generation, attempt number, or either O1
// budget -- those stay O1-owned inputs a Request only carries forward.
// Every document here declares its own digest field, and every accept path
// this package builds toward must verify that declared digest against an
// independently recomputed one -- the same integrity law
// golang/architecture/synthesis already enforces
// (invariant.synthesis.declared_digest_must_equal_computed_content_digest),
// extended across the O2 boundary. A Capabilities document is untrusted
// provider self-description: it proves only that a provider claims support
// for an operation, never eligibility or authority. A Result is a distinct,
// untrusted document -- never an accepted O1 Interpretation, Plan, Attempt,
// or Evaluation; it becomes one only through a later, explicit, pure
// mapping step accepted by synthesis.Transition.
//
// This package composes golang/architecture/synthesis's existing
// ProviderObservation and Limitation vocabulary types rather than
// redefining them.
package providerport

import "github.com/globulario/sensei/golang/architecture/synthesis"

// Operation is the closed capability vocabulary a Request may ask a
// Provider to perform, and a Capabilities document may claim support for.
type Operation string

const (
	OperationInterpretation        Operation = "interpretation"
	OperationPlanning              Operation = "planning"
	OperationGeneration            Operation = "generation"
	OperationEvaluationObservation Operation = "evaluation-observation"
)

// TerminalOutcome is the closed vocabulary for how a provider's Execute
// call ended, carried as data rather than an ambiguous Go error. A Go
// error is reserved for local contract/infrastructure failures where no
// valid Result or Receipt could be constructed at all.
type TerminalOutcome string

const (
	OutcomeCompleted             TerminalOutcome = "completed"
	OutcomeUnavailable           TerminalOutcome = "unavailable"
	OutcomeTimedOut              TerminalOutcome = "timed-out"
	OutcomeCancelled             TerminalOutcome = "cancelled"
	OutcomeInvalidOutput         TerminalOutcome = "invalid-output"
	OutcomeUnsupportedCapability TerminalOutcome = "unsupported-capability"
)

// --- sensei.providerport.capabilities.v1 ---

const CapabilitiesSchemaVersion = "sensei.providerport.capabilities.v1"

// Capabilities is a provider's self-declared capability snapshot, returned
// by Describe(). It is untrusted provider self-description: it proves only
// that a provider claims support for the listed operations. It grants
// nothing beyond eligibility to be asked -- never mutation, admission,
// completion, transition, or routing authority. Actual eligibility to
// invoke a provider belongs to the caller/configuration layer, out of this
// package's scope.
type Capabilities struct {
	SchemaVersion       string                        `json:"schema_version"`
	ProviderObservation synthesis.ProviderObservation `json:"provider_observation"`
	SupportedOperations []Operation                   `json:"supported_operations"`

	CapabilitiesDigestSHA256 string `json:"capabilities_digest_sha256"`
}

// --- sensei.providerport.request.v1 ---

const RequestSchemaVersion = "sensei.providerport.request.v1"

// Request is the O2 request envelope. It binds exact O1 session identity,
// the exact parent artifact digest, and the expected plan generation or
// attempt number the eventual mapped result must match -- a provider must
// not choose or alter plan generation, attempt number, retry budget,
// replan budget, or session identity; those stay O1-owned inputs this
// envelope only carries forward.
//
// ParentArtifactDigestSHA256 is the exact O1 artifact this request derives
// from: the session digest for interpretation, the interpretation digest
// for planning, the plan digest for generation, the attempt digest for
// evaluation-observation.
type Request struct {
	SchemaVersion string    `json:"schema_version"`
	RequestID     string    `json:"request_id"`
	Operation     Operation `json:"operation"`

	SessionDigestSHA256 string `json:"session_digest_sha256"`
	RepositoryDomain    string `json:"repository_domain"`
	BaseRevision        string `json:"base_revision"`

	ParentArtifactDigestSHA256 string `json:"parent_artifact_digest_sha256"`

	// ExpectedPlanGeneration / ExpectedAttemptNumber are nil exactly when
	// Operation does not yet have a plan generation / attempt number to
	// bind to -- see the schema's per-operation conditional requirements
	// (interpretation: both nil; planning: generation only; generation and
	// evaluation-observation: both set).
	ExpectedPlanGeneration *int `json:"expected_plan_generation"`
	ExpectedAttemptNumber  *int `json:"expected_attempt_number"`

	// DeadlineAt is a precommitted, binding execution constraint -- not a
	// bookkeeping observation timestamp -- so it stays included in
	// RequestDigestSHA256 (see Digest in digest.go).
	DeadlineAt string `json:"deadline_at"`

	// MaxObservationCount / MaxObservationBytes are precommitted before
	// execution and can never be enlarged by the provider.
	MaxObservationCount int `json:"max_observation_count"`
	MaxObservationBytes int `json:"max_observation_bytes"`

	// RequestDigestSHA256 is the self-referential semantic digest of this
	// document with this field zeroed before hashing.
	RequestDigestSHA256 string `json:"request_digest_sha256"`
}

// --- sensei.providerport.result.v1 ---

const ResultSchemaVersion = "sensei.providerport.result.v1"

// Result is the O2 result envelope a provider's Execute() returns: a
// closed terminal-outcome model as data. Result is a distinct, untrusted
// document -- never an accepted O1 Interpretation, Plan, Attempt, or
// Evaluation. It becomes one only through a later, explicit, pure mapping
// step accepted by synthesis.Transition; mapping failure is an O2
// rejection, not an O1 transition.
type Result struct {
	SchemaVersion       string          `json:"schema_version"`
	RequestDigestSHA256 string          `json:"request_digest_sha256"`
	Operation           Operation       `json:"operation"`
	TerminalOutcome     TerminalOutcome `json:"terminal_outcome"`
	Detail              string          `json:"detail"`

	// ResultDigestSHA256 is the self-referential semantic digest of this
	// document with this field zeroed before hashing.
	ResultDigestSHA256 string `json:"result_digest_sha256"`
}

// --- sensei.providerport.observationbatch.v1 ---

const ObservationBatchSchemaVersion = "sensei.providerport.observationbatch.v1"

// Observation is one bounded, sequenced streaming observation gathered
// during a request's execution. Observations are evidence attached to the
// final Result/Receipt; they never drive an O1 transition directly.
type Observation struct {
	SequenceNumber int    `json:"sequence_number"`
	Detail         string `json:"detail"`
	ObservedAt     string `json:"observed_at"`
}

// ObservationBatch is the O2 observations-aggregate document: the ordered
// set of observations gathered for one request, with its own declared/
// computed digest -- one of the five O2 documents the digest-integrity law
// applies to. Observation collection (the live streaming/sequencing
// mechanism) is a later, explicitly deferred O2 checkpoint; this package
// only defines the resulting aggregate document's closed shape.
type ObservationBatch struct {
	SchemaVersion       string        `json:"schema_version"`
	RequestDigestSHA256 string        `json:"request_digest_sha256"`
	Observations        []Observation `json:"observations"`

	// ObservationBatchDigestSHA256 is the self-referential semantic digest
	// of this document with this field zeroed before hashing.
	ObservationBatchDigestSHA256 string `json:"observation_batch_digest_sha256"`
}

// --- sensei.providerport.receipt.v1 ---

const ReceiptSchemaVersion = "sensei.providerport.receipt.v1"

// Receipt is the O2 provider execution receipt: binds request, capability
// snapshot, provider identity, response (when one exists), observations,
// and terminal outcome into one inspectable record. O2 recomputes and
// verifies this receipt's own digest and every digest it references; a
// provider-declared digest is never trusted unchecked.
type Receipt struct {
	SchemaVersion string `json:"schema_version"`
	ReceiptID     string `json:"receipt_id"`

	RequestDigestSHA256      string                        `json:"request_digest_sha256"`
	CapabilitiesDigestSHA256 string                        `json:"capabilities_digest_sha256"`
	ProviderObservation      synthesis.ProviderObservation `json:"provider_observation"`

	TerminalOutcome TerminalOutcome `json:"terminal_outcome"`

	// ResponseDigestSHA256 is non-nil exactly when TerminalOutcome is
	// OutcomeCompleted -- only a completed outcome produces a Result
	// payload worth referencing.
	ResponseDigestSHA256 *string `json:"response_digest_sha256"`
	// ObservationBatchDigestSHA256 is independently nilable -- a batch may
	// or may not exist regardless of outcome.
	ObservationBatchDigestSHA256 *string `json:"observation_batch_digest_sha256"`

	// StartedAt / CompletedAt are observation timestamps explicitly
	// excluded from ReceiptDigestSHA256 (see Digest in digest.go) -- the
	// same pattern golang/architecture/synthesis.Attempt.ProducedAt uses --
	// so receipt identity does not depend on wall-clock time.
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`

	Limitations []synthesis.Limitation `json:"limitations"`

	ReceiptDigestSHA256 string `json:"receipt_digest_sha256"`
}
