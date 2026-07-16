// SPDX-License-Identifier: Apache-2.0

package certification

import "errors"

// Engine-refusal errors. These mean the engine could not even establish its
// inputs (forged or missing references, invalid request shape) — semantically
// distinct from a blocked/uncertifiable verdict, which is a computed answer.
var (
	ErrRequestInvalid       = errors.New("certification: request is invalid")
	ErrPolicyUnknown        = errors.New("certification: unknown certification policy id")
	ErrRecordDigestMismatch = errors.New("certification: record does not reproduce its referenced digest")
	ErrRecordMissing        = errors.New("certification: referenced record is missing")
	ErrRecordUnreferenced   = errors.New("certification: record carries no matching digest reference")
	ErrLedgerInvalid        = errors.New("certification: task ledger failed verification")
	ErrStaleExpectedHead    = errors.New("certification: expected ledger head does not match current head")
	ErrTaskMismatch         = errors.New("certification: request task does not match ledger task")
)

// Stable, machine-readable reason codes. Reason codes explain a lane finding;
// limitation codes document an intentional non-requirement. All of them are
// folded (deduplicated, sorted) into the frozen receipt's flat Limitations
// array; the per-lane attribution stays on Result.Lanes.
const (
	// Scope lane.
	ReasonScopeDecisionMissing         = "scope.decision_missing"
	ReasonScopeConsumptionMissing      = "scope.consumption_missing"
	ReasonScopeVerificationMissing     = "scope.verification_missing"
	ReasonScopeRequestChainMismatch    = "scope.request_chain_mismatch"
	ReasonScopeCapabilityChainMismatch = "scope.capability_chain_mismatch"
	ReasonScopeCapabilityReused        = "scope.capability_reused_or_invalid"
	ReasonScopeTaskMismatch            = "scope.task_mismatch"
	ReasonScopeAdmissionExpired        = "scope.admission_expired"
	ReasonScopeOperationNotAdmitted    = "scope.operation_not_admitted" // :<operation_id>
	ReasonScopeOperationUnadmitted     = "scope.operation_unadmitted_observed"
	ReasonScopeUnadmittedPath          = "scope.unadmitted_path" // :<path>
	ReasonScopeVerifyDecisionMismatch  = "scope.verification_decision_mismatch"
	ReasonScopeResultBindingMismatch   = "scope.result_binding_mismatch"
	ReasonScopeBaseMismatch            = "scope.base_mismatch"
	ReasonScopeViolation               = "scope.violation" // :<violation code>
	ReasonScopeVerificationStale       = "scope.verification_stale"
	ReasonScopeVerificationUnknown     = "scope.verification_unknown"

	// Authority lane.
	ReasonAuthorityActorInvalid         = "authority.actor_invalid"
	ReasonAuthorityActorMismatch        = "authority.actor_mismatch"
	ReasonAuthorityShapeInvalid         = "authority.shape_invalid"         // :<operation_id>
	ReasonAuthorityDigestMismatch       = "authority.digest_mismatch"       // :<operation_id>
	ReasonAuthorityOperationUnresolved  = "authority.operation_unresolved"  // :<operation_id>
	ReasonAuthorityResolutionInvalid    = "authority.resolution_invalid"    // :<operation_id>:<status>
	ReasonAuthorityResolutionStale      = "authority.resolution_stale"      // :<operation_id>
	ReasonAuthorityGrantMissing         = "authority.grant_missing"         // :<operation_id>
	ReasonAuthorityMechanismIllegal     = "authority.mechanism_illegal"     // :<operation_id>
	ReasonAuthorityMechanismMismatch    = "authority.mechanism_mismatch"    // :<operation_id>
	ReasonAuthorityDomainMismatch       = "authority.domain_mismatch"       // :<operation_id>
	ReasonAuthorityDelegationUnresolved = "authority.delegation_unresolved" // :<operation_id>:<delegation_id>
	ReasonAuthorityResolutionDuplicate  = "authority.resolution_duplicate"  // :<operation_id>

	// Proof lane.
	ReasonProofMissingObligation      = "proof.missing_obligation"         // :<id>
	ReasonProofDigestUnreferenced     = "proof.digest_unreferenced"        // :<obligation_id>
	ReasonProofDischargeInvalid       = "proof.discharge_invalid"          // :<obligation_id>:<status>
	ReasonProofDischargeStale         = "proof.discharge_stale"            // :<obligation_id>
	ReasonProofDischargeRevoked       = "proof.discharge_revoked"          // :<obligation_id>
	ReasonProofShapeInvalid           = "proof.shape_invalid"              // :<obligation_id>
	ReasonProofMissingSlot            = "proof.missing_slot"               // :<obligation_id>:<slot_id>
	ReasonProofSlotUnknown            = "proof.slot_unknown"               // :<obligation_id>:<slot_id>
	ReasonProofReceiptUnresolved      = "proof.receipt_unresolved"         // :<obligation_id>:<slot_id>:<receipt_id>
	ReasonProofReceiptRevoked         = "proof.receipt_revoked"            // :<obligation_id>:<slot_id>:<receipt_id>
	ReasonProofResultBindingMismatch  = "proof.result_binding_mismatch"    // :<obligation_id>:<slot_id>:<receipt_id>
	ReasonProofWaived                 = "proof.waived"                     // :<obligation_id>:<slot_id>:<waiver_id>
	ReasonProofWaiverInvalid          = "proof.waiver_invalid"             // :<obligation_id>:<slot_id>
	ReasonProofIncompatibleReceipt    = "proof.incompatible_receipt"       // :<obligation_id>
	ReasonProofRuntimeMandateOverride = "proof.runtime_mandate_overridden" // :<obligation_id>:<slot_id>
	ReasonProofObligationUnresolved   = "proof.obligation_unresolved"      // :<obligation_id>

	LimitationProofInspectOnly     = "proof.not_applicable_inspect_only"
	LimitationProofNoRequiredSlots = "proof.no_required_slots"
	LimitationProofOptionalOpen    = "proof.optional_slot_open" // :<obligation_id>:<slot_id>

	// Evidence lane.
	ReasonEvidenceProfileUnresolved = "evidence.profile_unresolved"      // :<profile_id>
	ReasonEvidenceMissingProfile    = "evidence.missing_profile"         // :<profile_id>
	ReasonEvidenceMissingRuntime    = "evidence.missing_runtime_profile" // :<profile_id>
	ReasonEvidenceReceiptExpired    = "evidence.receipt_expired"         // :<receipt_id>
	ReasonEvidenceReceiptUnknown    = "evidence.freshness_unobserved"    // :<receipt_id>
	ReasonEvidenceReceiptInvalid    = "evidence.receipt_invalid"         // :<receipt_id>
	ReasonEvidenceReceiptRevoked    = "evidence.receipt_revoked"         // :<receipt_id>
	ReasonEvidenceConflicted        = "evidence.conflicted"              // :<profile_id>
	ReasonEvidenceWaived            = "evidence.waived"                  // :<profile_id>:<waiver_id>

	LimitationEvidenceRuntimeNotApplicable = "evidence.runtime_lane:not_applicable" // :<profile_id>
	LimitationEvidenceNoRequiredProfiles   = "evidence.no_required_profiles"
	LimitationEvidenceMandateUnknown       = "evidence.runtime_mandate_unresolved" // :<obligation_id>

	// Cross-lane.
	ReasonForbiddenMove = "forbidden_move" // :<move_id>
)
