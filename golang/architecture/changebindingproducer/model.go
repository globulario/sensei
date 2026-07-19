// SPDX-License-Identifier: AGPL-3.0-only

// Package changebindingproducer is the AUTHORITATIVE GitHub-side producer for
// completion.change_task_binding/v1 (Phase 9.4c, Checkpoint 2). It constructs and
// self-validates a binding from EXPLICIT, authoritative GitHub change identity, verified
// checkout state, an explicit canonical task, and the exact completion-result digest,
// then hands it to a deterministic publication + typed audit surface.
//
// It infers NO identity from branch names, commit messages, PR title/body/labels, author,
// changed paths, nearest directory, the only available task, cached publications, or
// ambient Git state. Missing authoritative input fails loudly with a typed reason.
//
// Producer failures are their OWN closed vocabulary. They are NOT 9.4b completion runtime
// failures and are NEVER expressed as projection_owner_runtime_error or any degraded-pass
// reason — this package does not import or touch the 9.4b decision at all. Composition
// with enforcement is Checkpoint 3.
package changebindingproducer

import "github.com/globulario/sensei/golang/architecture/changebinding"

// EventSource is a GitHub event form. Only the frozen set is authoritative.
type EventSource string

const (
	// EventPullRequest is the only supported authoritative event form in v1.
	EventPullRequest EventSource = "github_pull_request"
)

func supportedEvent(s EventSource) bool { return s == EventPullRequest }

// ProducerInput is the typed, authoritative GitHub-derived input. Every value is explicit.
type ProducerInput struct {
	EventSource EventSource

	// Authoritative event identities.
	RepositoryProvider string
	RepositoryIdentity string
	ChangeProvider     string
	ChangeID           string
	BaseSHA            string
	HeadSHA            string

	// Verified checkout state — already READ from the checkout by the caller; the pure
	// core never touches Git and never mutates repository state.
	CheckoutRepositoryIdentity string
	CheckoutHeadSHA            string

	// Explicit task selection (from the workflow input).
	TaskDirectory string
	TaskID        string
	TaskSessionID string

	// The completion result being evaluated, and the subject (task/session) it was
	// produced for, so the producer can prove correspondence without recomputing meaning.
	CompletionResultDigestSHA256 string
	CompletionResultTaskID       string
	CompletionResultSessionID    string

	// Producer identity + provenance descriptors.
	Issuer      string
	Checkout    string // checkout provenance descriptor, e.g. actions_checkout_v4
	Tool        string
	ToolVersion string
}

// ProducerFailure is the CLOSED producer-side failure vocabulary (stable reason codes).
type ProducerFailure string

const (
	FailNone                        ProducerFailure = ""
	FailUnsupportedEvent            ProducerFailure = "unsupported_github_event"
	FailMissingEventIdentity        ProducerFailure = "missing_event_identity"
	FailMalformedRepositoryIdentity ProducerFailure = "malformed_repository_identity"
	FailMalformedChangeIdentity     ProducerFailure = "malformed_change_identity"
	FailMalformedBaseSHA            ProducerFailure = "malformed_base_sha"
	FailMalformedHeadSHA            ProducerFailure = "malformed_head_sha"
	FailCheckoutRepositoryMismatch  ProducerFailure = "checkout_repository_mismatch"
	FailCheckoutHeadMismatch        ProducerFailure = "checkout_head_mismatch"
	FailStaleHead                   ProducerFailure = "stale_head"
	FailTaskInputAbsent             ProducerFailure = "explicit_task_input_absent"
	FailTaskIdentityInvalid         ProducerFailure = "task_identity_invalid"
	FailTaskSessionMismatch         ProducerFailure = "task_session_mismatch"
	FailCompletionDigestAbsent      ProducerFailure = "completion_result_digest_absent"
	FailCompletionDigestMalformed   ProducerFailure = "completion_result_digest_malformed"
	FailCompletionSubjectMismatch   ProducerFailure = "completion_result_subject_mismatch"
	FailProvenanceUnverifiable      ProducerFailure = "provenance_unverifiable"
	FailPublicationIdentityInvalid  ProducerFailure = "publication_identity_invalid"
	FailBindingConstruction         ProducerFailure = "binding_construction_failure"
	FailSelfValidation              ProducerFailure = "binding_self_validation_failure"
	FailContradictoryPublication    ProducerFailure = "contradictory_existing_publication"
	FailPublicationWrite            ProducerFailure = "publication_write_failure"
)

// AuditSchemaVersion identifies the typed audit record.
const AuditSchemaVersion = "completion.change_task_binding_audit/v1"

// AuditRecord is the deterministic typed audit of one production attempt. It carries only
// safe identity/subject fields and stage flags — never credentials, tokens, authorization
// headers, or raw event payloads. Interpretation branches on Outcome/Failure, never prose.
type AuditRecord struct {
	SchemaVersion string          `json:"schema_version" yaml:"schema_version"`
	Outcome       string          `json:"outcome" yaml:"outcome"` // "produced" | "failed"
	Failure       ProducerFailure `json:"failure,omitempty" yaml:"failure,omitempty"`
	Reason        string          `json:"reason" yaml:"reason"` // stable code

	RepositoryIdentity           string `json:"repository_identity,omitempty" yaml:"repository_identity,omitempty"`
	ChangeID                     string `json:"change_id,omitempty" yaml:"change_id,omitempty"`
	BaseSHA                      string `json:"base_sha,omitempty" yaml:"base_sha,omitempty"`
	HeadSHA                      string `json:"head_sha,omitempty" yaml:"head_sha,omitempty"`
	TaskID                       string `json:"task_id,omitempty" yaml:"task_id,omitempty"`
	TaskSessionID                string `json:"task_session_id,omitempty" yaml:"task_session_id,omitempty"`
	CompletionResultDigestSHA256 string `json:"completion_result_digest_sha256,omitempty" yaml:"completion_result_digest_sha256,omitempty"`
	Issuer                       string `json:"issuer,omitempty" yaml:"issuer,omitempty"`
	Tool                         string `json:"tool,omitempty" yaml:"tool,omitempty"`
	ToolVersion                  string `json:"tool_version,omitempty" yaml:"tool_version,omitempty"`
	ProvenanceVerification       string `json:"provenance_verification,omitempty" yaml:"provenance_verification,omitempty"`

	BindingDigestSHA256 string `json:"binding_digest_sha256,omitempty" yaml:"binding_digest_sha256,omitempty"`
	PublicationID       string `json:"publication_id,omitempty" yaml:"publication_id,omitempty"`

	// Stage flags — where a failure occurred.
	CheckoutVerified      bool `json:"checkout_verified" yaml:"checkout_verified"`
	TaskIdentityValidated bool `json:"task_identity_validated" yaml:"task_identity_validated"`
	CompletionBound       bool `json:"completion_bound" yaml:"completion_bound"`
	BindingConstructed    bool `json:"binding_constructed" yaml:"binding_constructed"`
	SelfValidated         bool `json:"self_validated" yaml:"self_validated"`
	Published             bool `json:"published" yaml:"published"`
}

// ProduceResult is the outcome of a production attempt.
type ProduceResult struct {
	Binding *changebinding.ChangeTaskBinding
	Failure ProducerFailure
	Audit   AuditRecord
}

// OK reports a successful, self-validated production (before publication).
func (r ProduceResult) OK() bool { return r.Failure == FailNone && r.Binding != nil }
