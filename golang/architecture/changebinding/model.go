// SPDX-License-Identifier: AGPL-3.0-only

// Package changebinding implements the typed completion.change_task_binding/v1
// publication and its pure validation machinery (Phase 9.4c, Checkpoint 1).
//
// A change-to-task binding proves that the EXACT change under evaluation (repository +
// bounded base..head + head SHA + change id) belongs to the EXACT canonical task whose
// completion closure is being enforced — so a valid completion for one task can never be
// laundered onto an unrelated change.
//
// This package is PURE. It reads no working directory, Git config, branch name, commit
// message, environment, event file, network, filesystem task discovery, --task-dir, or
// any ambient state. Every input is an explicit typed value. Authoritative GitHub-derived
// inputs and enforcement wiring are later checkpoints; this one is the identity object,
// its canonical digest, a strict parser, and a pure validator.
package changebinding

// SchemaVersion is the ONLY supported publication schema identity. Anything else is an
// unsupported version (never reconstructed, never normalized).
const SchemaVersion = "completion.change_task_binding/v1"

// RepositoryIdentity is the canonical repository (its Identity is the completion-policy
// domain).
type RepositoryIdentity struct {
	Provider string `json:"provider" yaml:"provider"`
	Identity string `json:"identity" yaml:"identity"`
}

// ChangeIdentity is the canonical, bounded change: provider + change id + the exact
// base..head range.
type ChangeIdentity struct {
	Provider string `json:"provider" yaml:"provider"`
	ID       string `json:"id" yaml:"id"`
	HeadSHA  string `json:"head_sha" yaml:"head_sha"`
	BaseSHA  string `json:"base_sha" yaml:"base_sha"`
}

// TaskIdentity is the canonical task selected for the change. It must match a VERIFIED
// ledger at enforcement time (a later checkpoint); here it is compared verbatim.
type TaskIdentity struct {
	Directory string `json:"directory" yaml:"directory"`
	ID        string `json:"id" yaml:"id"`
	SessionID string `json:"session_id" yaml:"session_id"`
}

// PublicationIdentity uniquely names one issued binding.
type PublicationIdentity struct {
	ID string `json:"id" yaml:"id"`
}

// Provenance records how the binding was produced — enough to audit and, in a later
// checkpoint, to verify issuer authority. A populated provenance is NOT proof of
// authority; positive verification (the seam below) is required.
type Provenance struct {
	EventSource string `json:"event_source" yaml:"event_source"`
	Checkout    string `json:"checkout" yaml:"checkout"`
	Tool        string `json:"tool" yaml:"tool"`
	ToolVersion string `json:"tool_version" yaml:"tool_version"`
}

// ChangeTaskBinding is the typed completion.change_task_binding/v1 publication.
type ChangeTaskBinding struct {
	SchemaVersion                string              `json:"schema_version" yaml:"schema_version"`
	Repository                   RepositoryIdentity  `json:"repository" yaml:"repository"`
	Change                       ChangeIdentity      `json:"change" yaml:"change"`
	Task                         TaskIdentity        `json:"task" yaml:"task"`
	CompletionResultDigestSHA256 string              `json:"completion_result_digest_sha256" yaml:"completion_result_digest_sha256"`
	Issuer                       string              `json:"issuer" yaml:"issuer"`
	Publication                  PublicationIdentity `json:"publication" yaml:"publication"`
	Provenance                   Provenance          `json:"provenance" yaml:"provenance"`
	// DigestSHA256 is the self-excluding binding digest — the ONLY field excluded from
	// its own computation.
	DigestSHA256 string `json:"digest_sha256" yaml:"digest_sha256"`
}

// BindingValidity is the CLOSED typed validity vocabulary. Each value is also its own
// stable reason code. The zero value is the empty string, which is NONE of the classes,
// so an uninitialized result is never authoritative or acceptable.
type BindingValidity string

const (
	BindingAuthoritative          BindingValidity = "authoritative_binding"
	BindingAbsent                 BindingValidity = "binding_absent"
	BindingMalformed              BindingValidity = "binding_malformed"
	BindingStaleHead              BindingValidity = "binding_stale_head"
	BindingRepositoryMismatch     BindingValidity = "binding_repository_mismatch"
	BindingTaskMismatch           BindingValidity = "binding_task_mismatch"
	BindingChangeRangeMismatch    BindingValidity = "binding_change_range_mismatch"
	BindingContradictory          BindingValidity = "binding_contradictory"
	BindingUnsupportedVersion     BindingValidity = "binding_unsupported_version"
	BindingUnverifiableProvenance BindingValidity = "binding_unverifiable_provenance"
	BindingPublicationInvalid     BindingValidity = "binding_publication_invalid"
)

// BindingResult is the typed validation result. Detail is a human-readable supplement —
// callers and tests must branch on Validity (the stable code), never on Detail.
type BindingResult struct {
	Validity BindingValidity
	Detail   string
}

// IsAuthoritative reports whether the binding verified as authoritative. Nothing else is.
func (r BindingResult) IsAuthoritative() bool { return r.Validity == BindingAuthoritative }

func result(v BindingValidity, detail string) BindingResult {
	return BindingResult{Validity: v, Detail: detail}
}

// ProvenanceVerification is the typed outcome of the provenance/authority seam. The zero
// value is ProvenanceInvalid on purpose: an unset or unestablished verification is never
// treated as verified.
type ProvenanceVerification int

const (
	// ProvenanceInvalid: the provenance is structurally invalid, or verification was
	// never established. Zero value — never authoritative.
	ProvenanceInvalid ProvenanceVerification = iota
	// ProvenanceUnverifiable: structurally valid provenance whose issuer authority could
	// not be positively verified.
	ProvenanceUnverifiable
	// ProvenanceVerified: structurally valid AND issuer authority positively verified.
	ProvenanceVerified
)

// ProvenanceVerifier is the minimum seam a later checkpoint implements to establish
// issuer authority (GitHub event/token/signature verification). Checkpoint 1 uses only
// deterministic, explicit results. A populated Issuer field is NEVER sufficient; positive
// verification is required.
type ProvenanceVerifier interface {
	VerifyProvenance(b ChangeTaskBinding) ProvenanceVerification
}

// ExpectedSubject is the explicit subject a binding is validated against. Task is
// optional (nil when the caller is not yet validating against a selected task). Nothing
// here is read from ambient state — the caller supplies exact values.
type ExpectedSubject struct {
	Repository RepositoryIdentity
	Change     ChangeIdentity
	Task       *TaskIdentity
}
