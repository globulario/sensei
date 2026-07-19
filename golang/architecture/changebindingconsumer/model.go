// SPDX-License-Identifier: Apache-2.0

// Package changebindingconsumer places authoritative completion.change_task_binding/v1
// validation IN FRONT of the closed Phase 9.4b completion-enforcement decision (Phase 9.4c,
// Checkpoint 3). It proves the completion result being evaluated belongs to the current
// repository, GitHub change, exact base/head SHAs, exact canonical task/session, and exact
// completion-result publication BEFORE any completion interpretation runs.
//
// The binding does not self-authorize: at consumption time authority is established by EXACT
// correspondence to the current trusted execution subject (reconstructed independently of
// the publication) and a positive provenance verification through the current boundary. No
// cryptographic attestation is claimed; authority derives from exact current-context
// correspondence over a controlled publication channel — stated honestly here and in audit.
//
// Composition is a two-typed-layer result (binding decision + completion decision). The
// completion evaluation is supplied as a THUNK that Compose invokes ONLY when the binding
// is accepted or not required — so the 9.4b runtime-degradation lane is structurally
// unreachable for any binding failure.
package changebindingconsumer

// BindingGateValidity is the typed binding-gate vocabulary — DISTINCT from the 9.4b
// completion-verdict vocabulary. Each value is its own stable reason code. The zero value
// is the empty string, which is none of the accepting classes → fail closed.
type BindingGateValidity string

const (
	BindingNotRequired                  BindingGateValidity = "binding_not_required"
	BindingAccepted                     BindingGateValidity = "authoritative_binding_accepted"
	BindingGateAbsent                   BindingGateValidity = "binding_absent"
	BindingGateMalformed                BindingGateValidity = "binding_malformed"
	BindingGateStaleHead                BindingGateValidity = "binding_stale_head"
	BindingGateRepositoryMismatch       BindingGateValidity = "binding_repository_mismatch"
	BindingGateTaskMismatch             BindingGateValidity = "binding_task_mismatch"
	BindingGateChangeRangeMismatch      BindingGateValidity = "binding_change_range_mismatch"
	BindingGateContradictory            BindingGateValidity = "binding_contradictory"
	BindingGateUnsupportedVersion       BindingGateValidity = "binding_unsupported_version"
	BindingGateUnverifiableProvenance   BindingGateValidity = "binding_unverifiable_provenance"
	BindingGatePublicationInvalid       BindingGateValidity = "binding_publication_invalid"
	BindingGateCurrentEventMismatch     BindingGateValidity = "binding_current_event_mismatch"
	BindingGateCheckoutMismatch         BindingGateValidity = "binding_checkout_mismatch"
	BindingGateCompletionResultMismatch BindingGateValidity = "binding_completion_result_mismatch"
	BindingGateTaskSessionMismatch      BindingGateValidity = "binding_task_session_mismatch"
	BindingGateProducerMismatch         BindingGateValidity = "binding_producer_identity_mismatch"
	BindingGateUnsupportedExecution     BindingGateValidity = "binding_unsupported_execution_context"
)

// accepted reports whether a binding-gate outcome permits the completion evaluation to run.
func (v BindingGateValidity) accepted() bool {
	return v == BindingNotRequired || v == BindingAccepted
}

// BindingGate is the typed binding-gate result.
type BindingGate struct {
	Validity BindingGateValidity
	Detail   string
}

func gate(v BindingGateValidity, detail string) BindingGate {
	return BindingGate{Validity: v, Detail: detail}
}

// CurrentSubject is the expected subject reconstructed from the CURRENT trusted execution
// context — event identities, verified checkout, explicit task, and the completion result
// actually passed to enforcement — INDEPENDENT of anything the binding publication claims.
// The consumer verifies the binding corresponds exactly to these values; it never accepts
// publication-contained values as their own proof.
type CurrentSubject struct {
	// Authoritative event identities.
	RepositoryProvider string
	RepositoryIdentity string
	ChangeProvider     string
	ChangeID           string
	BaseSHA            string
	HeadSHA            string

	// Verified checkout state (read-only elsewhere).
	CheckoutRepositoryIdentity string
	CheckoutHeadSHA            string

	// Explicit task + the completion result passed to enforcement.
	TaskDirectory                string
	TaskID                       string
	TaskSessionID                string
	CompletionResultDigestSHA256 string

	// Expected producer identity for this execution (from the frozen support contract).
	ExpectedIssuer string
	ExpectedTool   string
}

// CompletionOutcome is the 9.4b decision surfaced abstractly (result + stable reason), so
// this package never depends on the 9.4b runner. Result is "pass" | "degraded_pass" | "block".
type CompletionOutcome struct {
	Result string
	Reason string
}

// FinalResult is the composed enforcement result across the two typed layers.
type FinalResult struct {
	Result     string // "pass" | "degraded_pass" | "block"
	Reason     string // the binding reason (when blocked at binding) or the completion reason
	Stage      string // "binding" | "completion"
	Binding    BindingGate
	Completion *CompletionOutcome // nil when the binding blocked before completion ran
}
