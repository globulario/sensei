// SPDX-License-Identifier: AGPL-3.0-only

package proofdischarge

import (
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// Discharge maps validated evidence receipts to the proof slots of each
// obligation and emits, per obligation, a schema-conformant
// closureprotocol.ProofDischarge plus a diagnostic DischargeReport. It is
// deterministic: identical input produces byte-identical output (including
// digests). Discharge never reads the wall clock — ctx.ObservedAt is the single
// evaluation time for every freshness/expiry comparison.
func Discharge(ctx Context) ([]closureprotocol.ProofDischarge, DischargeReport, error) {
	report := DischargeReport{
		SchemaVersion: SchemaVersion,
		GeneratedBy:   GeneratedBy,
	}

	// Step 0 — context validation (fail-closed).
	if err := binding.ValidateResult(ctx.ResultBinding); err != nil {
		return nil, report, err
	}
	profile := normalizeCoverageProfile(ctx.CoverageProfile)
	ctx.CoverageProfile = profile
	report.CoverageProfile = profile

	if profile == CoverageStaticTestRuntime && ctx.RuntimeTarget == nil && anyRequiredRuntimeSlot(ctx.Obligations) {
		return nil, report, ErrRuntimeTargetMissing
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(ctx.ObservedAt)); err != nil {
		return nil, report, ErrObservedAtInvalid
	}

	// Step 0.5 — defensive re-validation. Receipts that fail are dropped
	// entirely (never a slot candidate) and logged only on the report.
	usable := make([]closureprotocol.EvidenceReceipt, 0, len(ctx.Receipts))
	for _, r := range ctx.Receipts {
		if reValidate(r, ctx.Profiles) {
			usable = append(usable, r)
		} else {
			report.DroppedReceipts = append(report.DroppedReceipts, ReceiptEvaluation{
				ReceiptID:   strings.TrimSpace(r.ReceiptID),
				Compatible:  false,
				ReasonCodes: []string{ReasonReinvalidationFailed},
			})
		}
	}
	ctx.Receipts = usable // conflict detection must only consult surviving receipts

	// Step 1 — normalize for determinism.
	sort.SliceStable(usable, func(i, j int) bool {
		return strings.TrimSpace(usable[i].ReceiptID) < strings.TrimSpace(usable[j].ReceiptID)
	})
	obligations := append([]ProofObligation(nil), ctx.Obligations...)
	sort.SliceStable(obligations, func(i, j int) bool { return obligations[i].ID < obligations[j].ID })

	// Step 2 — per obligation.
	discharges := make([]closureprotocol.ProofDischarge, 0, len(obligations))
	for _, ob := range obligations {
		d, obReport, err := dischargeOne(ctx, ob, usable)
		if err != nil {
			return nil, report, err
		}
		discharges = append(discharges, d)
		report.Obligations = append(report.Obligations, obReport)
	}
	return discharges, report, nil
}

func dischargeOne(ctx Context, ob ProofObligation, usable []closureprotocol.EvidenceReceipt) (closureprotocol.ProofDischarge, ObligationReport, error) {
	slots := append([]ProofSlotSpec(nil), ob.RequiredSlots...)
	sort.SliceStable(slots, func(i, j int) bool { return slots[i].ID < slots[j].ID })

	d := closureprotocol.ProofDischarge{ObligationID: ob.ID}
	obReport := ObligationReport{ObligationID: ob.ID}

	var missing []string
	var incompatible []string // receipt ids that failed as a candidate for some slot
	var usedReceipts []string // receipt ids that discharged some slot
	hasConflicted, hasStale := false, false

	for _, slot := range slots {
		disposition := ResolveSlotDisposition(ob, slot, ctx.CoverageProfile)
		slotReport := SlotReport{SlotID: slot.ID, CoverageDisposition: dispositionLabel(disposition)}

		if disposition == SlotNotApplicableUnderProfile {
			d.SlotResults = append(d.SlotResults, closureprotocol.ProofSlotResult{
				SlotID: slot.ID, Status: closureprotocol.DimensionNotApplicable,
			})
			obReport.Slots = append(obReport.Slots, slotReport)
			continue
		}

		var passing []string
		var failReasons []string
		for _, r := range usable {
			if !evidenceKindAllowed(slot.Kind, r.EvidenceKind) {
				continue // not a candidate; not "incompatible"
			}
			prof := ctx.Profiles[strings.TrimSpace(r.ProfileID)]
			ok, reasons := CheckCompatibility(ob, slot, r, prof, ctx)
			slotReport.Candidates = append(slotReport.Candidates, ReceiptEvaluation{
				ReceiptID:   strings.TrimSpace(r.ReceiptID),
				Compatible:  ok,
				ReasonCodes: reasons,
			})
			if ok {
				passing = append(passing, strings.TrimSpace(r.ReceiptID))
			} else {
				incompatible = append(incompatible, strings.TrimSpace(r.ReceiptID))
				failReasons = append(failReasons, reasons...)
			}
		}

		if len(passing) > 0 {
			ids := closureprotocol.NormalizeSet(passing)
			d.SlotResults = append(d.SlotResults, closureprotocol.ProofSlotResult{
				SlotID: slot.ID, Status: closureprotocol.DimensionPass, ReceiptIDs: ids,
			})
			usedReceipts = append(usedReceipts, ids...)
			obReport.Slots = append(obReport.Slots, slotReport)
			continue
		}

		// Zero passing receipts.
		if disposition == SlotOptional {
			d.SlotResults = append(d.SlotResults, closureprotocol.ProofSlotResult{
				SlotID: slot.ID, Status: closureprotocol.DimensionUnknown,
			})
			obReport.Slots = append(obReport.Slots, slotReport)
			continue
		}

		// Required slot with no passing receipt: try a governed waiver.
		if waiverID, ok := ResolveWaiver(ob, slot, ctx); ok {
			// The frozen ValidateProofDischarge requires >=1 receipt id for a
			// pass/pass_with_exception slot, so the authorizing WaiverReceipt id
			// is recorded here (the frozen ProofSlotResult has no waiver field).
			d.SlotResults = append(d.SlotResults, closureprotocol.ProofSlotResult{
				SlotID: slot.ID, Status: closureprotocol.DimensionPassWithException,
				ReceiptIDs: []string{waiverID},
			})
			usedReceipts = append(usedReceipts, waiverID)
			slotReport.WaiverApplied = waiverID
			obReport.Slots = append(obReport.Slots, slotReport)
			continue
		}

		// No waiver: the slot is unsatisfied. Classify by the dominant failure.
		status := closureprotocol.DimensionBlocked
		switch {
		case containsReason(failReasons, ReasonConflictUnresolved):
			status = closureprotocol.DimensionConflicted
			hasConflicted = true
		case containsReason(failReasons, ReasonFreshnessExpired):
			status = closureprotocol.DimensionStale
			hasStale = true
		}
		d.SlotResults = append(d.SlotResults, closureprotocol.ProofSlotResult{
			SlotID: slot.ID, Status: status,
		})
		missing = append(missing, slot.ID)
		obReport.Slots = append(obReport.Slots, slotReport)
	}

	// Obligation-level aggregation.
	d.MissingSlots = closureprotocol.NormalizeSet(missing)
	d.MappedEvidence = closureprotocol.NormalizeSet(usedReceipts)
	usedSet := map[string]bool{}
	for _, id := range usedReceipts {
		usedSet[id] = true
	}
	var incompatibleFiltered []string
	for _, id := range incompatible {
		if !usedSet[id] {
			incompatibleFiltered = append(incompatibleFiltered, id)
		}
	}
	d.IncompatibleReceipts = closureprotocol.NormalizeSet(incompatibleFiltered)

	// Obligation-level status (a receipt_status). blocked/unknown have no
	// receipt_status analog; both collapse to invalid here.
	switch {
	case len(d.MissingSlots) == 0 && !hasConflicted && !hasStale:
		d.Status = closureprotocol.ReceiptValid
	case hasConflicted:
		d.Status = closureprotocol.ReceiptConflicted
	case hasStale:
		d.Status = closureprotocol.ReceiptStale
	default:
		d.Status = closureprotocol.ReceiptInvalid
	}

	// Steps 17-18 — self-check and digest (WithDigest re-validates).
	withDigest, err := WithDigest(d)
	if err != nil {
		return closureprotocol.ProofDischarge{}, ObligationReport{}, err
	}
	return withDigest, obReport, nil
}

func normalizeCoverageProfile(s string) string {
	switch strings.TrimSpace(s) {
	case CoverageStaticTestRuntime:
		return CoverageStaticTestRuntime
	default:
		return CoverageStaticTest
	}
}

func anyRequiredRuntimeSlot(obligations []ProofObligation) bool {
	for _, ob := range obligations {
		for _, slot := range ob.RequiredSlots {
			if slot.Required && isRuntimeOwnerPathKind(slot.Kind) {
				return true
			}
		}
	}
	return false
}

// reValidate re-runs the Phase-4 structural validators and requires the receipt
// to still carry a valid status (Phase 4 marks staleness/conflict/revocation on
// the status). A receipt failing any of these is dropped from consideration.
func reValidate(r closureprotocol.EvidenceReceipt, profiles map[string]closureprotocol.EvidenceProfile) bool {
	if closureprotocol.ValidateEvidenceReceipt(r) != nil {
		return false
	}
	if r.Status != closureprotocol.ReceiptValid {
		return false
	}
	if prof, ok := profiles[strings.TrimSpace(r.ProfileID)]; ok {
		if closureprotocol.ValidateEvidenceReceiptAgainstProfile(prof, r) != nil {
			return false
		}
	}
	return true
}

func containsReason(reasons []string, want string) bool {
	for _, r := range reasons {
		if r == want {
			return true
		}
	}
	return false
}
