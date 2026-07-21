// SPDX-License-Identifier: AGPL-3.0-only

package proofdischarge

import "errors"

// Reason codes are stable, machine-readable explanations for why a receipt
// could not discharge a slot. They are the "compatibility_reasons" content the
// frozen proof-discharge schema has no field for; they live only in the
// diagnostic DischargeReport (report.go), never on the schema-bound
// closureprotocol.ProofDischarge value.
const (
	// ReasonReceiptRevoked: the receipt id appears in the revocation index.
	ReasonReceiptRevoked = "discharge.slot.receipt_revoked"
	// ReasonEvidenceKindMismatch: the receipt's evidence_kind is not accepted
	// by the slot kind (includes any `authority`-kind receipt, which never
	// discharges a proof slot).
	ReasonEvidenceKindMismatch = "discharge.slot.evidence_kind_mismatch"
	// ReasonProfileUnknown: no governing evidence profile is known for the
	// receipt's profile_id.
	ReasonProfileUnknown = "discharge.slot.profile_unknown"
	// ReasonObservationPathUngoverned: the receipt's observation_path is not the
	// profile's legal (owner) observation path — e.g. an arbitrary shell string.
	ReasonObservationPathUngoverned = "discharge.slot.observation_path_ungoverned"
	// ReasonResultBindingMismatch: the receipt was produced for a different
	// result/shared proof context than the obligation under discharge.
	ReasonResultBindingMismatch = "discharge.slot.result_binding_mismatch"
	// ReasonAuthorityDomainMismatch: the profile is governed for an authority
	// target that does not intersect the obligation's authority surfaces.
	ReasonAuthorityDomainMismatch = "discharge.slot.authority_domain_mismatch"
	// ReasonRuntimeTargetMismatch: runtime evidence observed on a different
	// runtime target than the one under proof.
	ReasonRuntimeTargetMismatch = "discharge.slot.runtime_target_mismatch"
	// ReasonTrustInsufficient: the receipt carries no attested trust (empty or
	// "unattested") — e.g. an unapproved unsafe-probe observation.
	ReasonTrustInsufficient = "discharge.slot.trust_insufficient"
	// ReasonFreshnessExpired: the receipt's expiry is at or before ObservedAt.
	ReasonFreshnessExpired = "discharge.slot.freshness_expired"
	// ReasonConflictUnresolved: the receipt is in an unresolved conflict with
	// another live receipt.
	ReasonConflictUnresolved = "discharge.slot.conflict_unresolved"
	// ReasonReinvalidationFailed: the receipt failed the defensive Phase-5
	// re-validation at Step 0.5 and was dropped from consideration entirely
	// (never a slot candidate). Reported only on DischargeReport.DroppedReceipts.
	ReasonReinvalidationFailed = "discharge.receipt.reinvalidation_failed"
)

// TrustUnattested is the trust value that, like the empty string, does not
// establish that a receipt is attested/approved.
const TrustUnattested = "unattested"

// Sentinel errors returned by Discharge's context validation (Step 0). These
// are engine-refusal errors (fail-closed), not per-slot outcomes.
var (
	ErrObservedAtInvalid    = errors.New("proofdischarge: observed_at must be RFC3339")
	ErrRuntimeTargetMissing = errors.New("proofdischarge: runtime target required under static_test_runtime coverage profile")
)
