// SPDX-License-Identifier: Apache-2.0

package proofdischarge

// DischargeReport is the non-authoritative, non-schema diagnostic companion to a
// []closureprotocol.ProofDischarge result. It exists solely because the frozen
// schema has no field for per-slot / per-receipt reasoning. Nothing in Phase 6
// (certification) may read this report as authoritative — only
// ProofDischarge.Status / SlotResults[].Status / DischargeDigestSHA256 are
// authoritative. This mirrors probe.GenerationReport / probe.RecordingReport.
type DischargeReport struct {
	SchemaVersion   string             `json:"schema_version" yaml:"schema_version"`
	GeneratedBy     string             `json:"generated_by" yaml:"generated_by"`
	CoverageProfile string             `json:"coverage_profile" yaml:"coverage_profile"`
	Obligations     []ObligationReport `json:"obligations" yaml:"obligations"`
	// DroppedReceipts records receipts dropped at Step 0.5 (re-validation
	// failure) before they could become a candidate for any slot. They are not
	// eligible for any ProofDischarge.incompatible_receipts field.
	DroppedReceipts []ReceiptEvaluation `json:"dropped_receipts,omitempty" yaml:"dropped_receipts,omitempty"`
}

type ObligationReport struct {
	ObligationID string       `json:"obligation_id" yaml:"obligation_id"`
	Slots        []SlotReport `json:"slots" yaml:"slots"`
}

type SlotReport struct {
	SlotID              string              `json:"slot_id" yaml:"slot_id"`
	CoverageDisposition string              `json:"coverage_disposition" yaml:"coverage_disposition"` // "required" | "not_applicable_under_profile" | "optional"
	Candidates          []ReceiptEvaluation `json:"candidates,omitempty" yaml:"candidates,omitempty"`
	WaiverApplied       string              `json:"waiver_applied,omitempty" yaml:"waiver_applied,omitempty"` // waiver_id, diagnostic only
}

// ReceiptEvaluation records, for one receipt considered against one slot, whether
// it passed the compatibility predicate and why not if it failed. ReasonCodes is
// the "compatibility_reasons" content the frozen schema has no room for.
type ReceiptEvaluation struct {
	ReceiptID   string   `json:"receipt_id" yaml:"receipt_id"`
	Compatible  bool     `json:"compatible" yaml:"compatible"`
	ReasonCodes []string `json:"reason_codes,omitempty" yaml:"reason_codes,omitempty"`
}

// Coverage-disposition strings recorded in SlotReport.CoverageDisposition.
const (
	DispositionRequired      = "required"
	DispositionNotApplicable = "not_applicable_under_profile"
	DispositionOptional      = "optional"
)

func dispositionLabel(d SlotDisposition) string {
	switch d {
	case SlotNotApplicableUnderProfile:
		return DispositionNotApplicable
	case SlotOptional:
		return DispositionOptional
	default:
		return DispositionRequired
	}
}
