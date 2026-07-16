// SPDX-License-Identifier: Apache-2.0

// Package certification implements Phase 6 (typed, receipt-driven
// certification) of the architectural closure protocol. It recomputes the four
// certification lanes — scope, authority, proof, evidence — exclusively from
// typed, digest-verified protocol records produced by earlier phases and emits
// the frozen closureprotocol.CertificationReceipt.
//
// Laws enforced by construction:
//
//   - No caller booleans. Request has no field a caller can set to assert an
//     outcome (no scope_valid, evidence_sufficient, required_paths_satisfied,
//     promotion_allowed, certification_status, score). Every lane status is
//     recomputed from the referenced records.
//   - Every reference is content-addressed. A record that does not reproduce
//     its claimed digest is refused, not down-scored.
//   - Result binding law: the engine only accepts a result binding produced by
//     earlier phases; it never constructs one and it makes no claim of
//     result-graph freshness (Phase 7), completion, or terminal closure
//     (Phase 8).
//   - Only this package may establish architectural correctness certification.
//     The legacy admission engine hardcodes CorrectnessCertified=false
//     everywhere; the legacy `sensei certify --event` benchmark adapter cannot
//     produce a CertificationReceipt or append a `certified` ledger event.
//   - Governed runtime mandate (invariant
//     closure.runtime_evidence_not_applicable_only_without_governed_runtime_mandate):
//     runtime evidence is not_applicable only when no applicable governed proof
//     obligation requires runtime evidence AND the policy permits relaxation.
//     ProofObligation.RequiresRuntimeEvidence overrides every coverage profile.
//
// The engine is a pure function of its inputs: it never reads the wall clock
// (Request.EvaluatedAt is the single evaluation time) and never depends on
// filesystem paths for identity.
package certification

import (
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/proofdischarge"
)

const (
	// GeneratedBy identifies this engine as the producer of certification
	// receipts and `certified` ledger events.
	GeneratedBy = "sensei certify-change"
	// EngineVersion is recorded on task-level results for auditability.
	EngineVersion = "certification.engine/1"
)

// Request is the typed certification request. Every upstream record is
// referenced by content digest; the actual records are resolved from the
// verified task ledger's content-addressed artifact store (see source.go) or
// handed in as Records for pure evaluation. There is deliberately no field on
// this type, on Records, or on the frozen CertificationReceipt that a caller
// can set to assert a lane outcome — the adversarial tests prove that by
// reflection.
type Request struct {
	TaskID        string                        `json:"task_id" yaml:"task_id"`
	PolicyID      string                        `json:"policy_id" yaml:"policy_id"`
	EvaluatedAt   string                        `json:"evaluated_at" yaml:"evaluated_at"` // RFC3339; the deterministic "now"
	ResultBinding closureprotocol.ResultBinding `json:"result_binding" yaml:"result_binding"`

	AdmissionRequestDigestSHA256      string   `json:"admission_request_digest_sha256" yaml:"admission_request_digest_sha256"`
	AdmissionDecisionDigestSHA256     string   `json:"admission_decision_digest_sha256" yaml:"admission_decision_digest_sha256"`
	CapabilityConsumptionDigestSHA256 string   `json:"capability_consumption_digest_sha256" yaml:"capability_consumption_digest_sha256"`
	ScopeVerificationDigestSHA256     string   `json:"scope_verification_digest_sha256" yaml:"scope_verification_digest_sha256"`
	AuthorityResolutionDigests        []string `json:"authority_resolution_digests" yaml:"authority_resolution_digests"`
	ProofDischargeDigests             []string `json:"proof_discharge_digests,omitempty" yaml:"proof_discharge_digests,omitempty"`
	ProofObligationDigests            []string `json:"proof_obligation_digests,omitempty" yaml:"proof_obligation_digests,omitempty"`
	EvidenceProfileDigests            []string `json:"evidence_profile_digests,omitempty" yaml:"evidence_profile_digests,omitempty"`
	EvidenceReceiptDigests            []string `json:"evidence_receipt_digests,omitempty" yaml:"evidence_receipt_digests,omitempty"`
	ArtifactReceiptDigests            []string `json:"artifact_receipt_digests,omitempty" yaml:"artifact_receipt_digests,omitempty"`
	WaiverDigests                     []string `json:"waiver_digests,omitempty" yaml:"waiver_digests,omitempty"`
	RevocationDigests                 []string `json:"revocation_digests,omitempty" yaml:"revocation_digests,omitempty"`
	ForbiddenMoveFindingDigests       []string `json:"forbidden_move_finding_digests,omitempty" yaml:"forbidden_move_finding_digests,omitempty"`
	RuntimeTargetDigestSHA256         string   `json:"runtime_target_digest_sha256,omitempty" yaml:"runtime_target_digest_sha256,omitempty"`
}

// Records holds the resolved, typed protocol records the Request references.
// Records are digest-verified against the Request (VerifyRecords) before any
// lane sees them. Missing records are represented by zero values; every lane
// fails closed (blocked/unknown) on a missing required source — it never
// substitutes a default.
type Records struct {
	AdmissionRequest      closureprotocol.AdmissionRequest      `json:"admission_request" yaml:"admission_request"`
	AdmissionDecision     closureprotocol.AdmissionDecision     `json:"admission_decision" yaml:"admission_decision"`
	CapabilityConsumption closureprotocol.CapabilityConsumption `json:"capability_consumption" yaml:"capability_consumption"`
	ScopeVerification     ScopeVerification                     `json:"scope_verification" yaml:"scope_verification"`
	AuthorityResolutions  []closureprotocol.AuthorityResolution `json:"authority_resolutions,omitempty" yaml:"authority_resolutions,omitempty"`
	ProofDischarges       []closureprotocol.ProofDischarge      `json:"proof_discharges,omitempty" yaml:"proof_discharges,omitempty"`
	Obligations           []proofdischarge.ProofObligation      `json:"proof_obligations,omitempty" yaml:"proof_obligations,omitempty"`
	EvidenceProfiles      []closureprotocol.EvidenceProfile     `json:"evidence_profiles,omitempty" yaml:"evidence_profiles,omitempty"`
	EvidenceReceipts      []closureprotocol.EvidenceReceipt     `json:"evidence_receipts,omitempty" yaml:"evidence_receipts,omitempty"`
	ArtifactReceipts      []ArtifactReceipt                     `json:"artifact_receipts,omitempty" yaml:"artifact_receipts,omitempty"`
	Waivers               []closureprotocol.WaiverReceipt       `json:"waivers,omitempty" yaml:"waivers,omitempty"`
	Revocations           []closureprotocol.RevocationReceipt   `json:"revocations,omitempty" yaml:"revocations,omitempty"`
	ForbiddenMoveFindings []ForbiddenMoveFinding                `json:"forbidden_move_findings,omitempty" yaml:"forbidden_move_findings,omitempty"`
	RuntimeTarget         *closureprotocol.RuntimeTarget        `json:"runtime_target,omitempty" yaml:"runtime_target,omitempty"`
}

// ScopeVerification is the engine-owned typed projection of a
// scope-verification receipt (the frozen protocol references it through
// CompletionReceipt.AdmissionVerificationDigestSHA256 but freezes no dedicated
// schema for it in this snapshot; reconcile with the Phase 3 schema when it
// ships). It deliberately carries NO boolean corresponding to the legacy
// admission engine's CorrectnessCertified or a caller-assertable scope_valid —
// the engine recomputes scope from the observed change set and violations, and
// the status label alone is never trusted when violations are present.
type ScopeVerification struct {
	DecisionDigestSHA256 string                        `json:"decision_digest_sha256" yaml:"decision_digest_sha256"`
	ResultBinding        closureprotocol.ResultBinding `json:"result_binding" yaml:"result_binding"`
	ObservedPaths        []string                      `json:"observed_paths,omitempty" yaml:"observed_paths,omitempty"`
	ObservedOperationIDs []string                      `json:"observed_operation_ids,omitempty" yaml:"observed_operation_ids,omitempty"`
	Status               string                        `json:"status" yaml:"status"` // scope_compliant | scope_violated | stale
	Violations           []ScopeViolation              `json:"violations,omitempty" yaml:"violations,omitempty"`
	VerifiedAt           string                        `json:"verified_at,omitempty" yaml:"verified_at,omitempty"`
}

// ScopeVerification status vocabulary.
const (
	ScopeCompliant = "scope_compliant"
	ScopeViolated  = "scope_violated"
	ScopeStale     = "stale"
)

type ScopeViolation struct {
	Code   string `json:"code" yaml:"code"`
	Path   string `json:"path,omitempty" yaml:"path,omitempty"`
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

// ForbiddenMoveFinding is a typed, binding-valid forbidden-move detection. A
// finding is applicable only when it is bound to the exact result under
// certification (and, when it names operations, to operations in the admitted
// plan). Caller-supplied forbidden-move ID lists on untyped events are never
// consumed.
type ForbiddenMoveFinding struct {
	MoveID        string                        `json:"move_id" yaml:"move_id"`
	ResultBinding closureprotocol.ResultBinding `json:"result_binding" yaml:"result_binding"`
	OperationIDs  []string                      `json:"operation_ids,omitempty" yaml:"operation_ids,omitempty"`
	Evidence      string                        `json:"evidence,omitempty" yaml:"evidence,omitempty"`
}

// ArtifactReceipt is a content-addressed generated/produced artifact
// (mirroring the frozen artifact_ref shape) plus the proof obligations it is
// claimed to relate to. Certification verifies its content address; slot
// satisfaction stays owned by Phase 5.
type ArtifactReceipt struct {
	Path                      string   `json:"path" yaml:"path"`
	MediaType                 string   `json:"media_type" yaml:"media_type"`
	DigestSHA256              string   `json:"digest_sha256" yaml:"digest_sha256"`
	RelatedProofObligationIDs []string `json:"related_proof_obligation_ids,omitempty" yaml:"related_proof_obligation_ids,omitempty"`
}

// Lane identifies one of the four certification lanes.
type Lane string

const (
	LaneScope     Lane = "scope"
	LaneAuthority Lane = "authority"
	LaneProof     Lane = "proof"
	LaneEvidence  Lane = "evidence"
)

// LaneResult is the engine's per-lane working result. Reason codes and
// limitations keep the untruncated, attributable detail; the frozen receipt
// flattens them into its three string arrays (see receipt.go).
type LaneResult struct {
	Lane           Lane                            `json:"lane" yaml:"lane"`
	Status         closureprotocol.DimensionStatus `json:"status" yaml:"status"`
	ReasonCodes    []string                        `json:"reason_codes,omitempty" yaml:"reason_codes,omitempty"`
	Limitations    []string                        `json:"limitations,omitempty" yaml:"limitations,omitempty"`
	Contradictions []string                        `json:"contradictions,omitempty" yaml:"contradictions,omitempty"`
	ForbiddenMoves []string                        `json:"forbidden_moves,omitempty" yaml:"forbidden_moves,omitempty"`
}

// Result is the full evaluation outcome: the frozen receipt plus the per-lane
// detail and a deterministic next-action hint. Only Receipt is authoritative.
type Result struct {
	Receipt    closureprotocol.CertificationReceipt `json:"receipt" yaml:"receipt"`
	Lanes      [4]LaneResult                        `json:"lanes" yaml:"lanes"` // scope, authority, proof, evidence
	NextAction string                               `json:"next_action" yaml:"next_action"`
}
