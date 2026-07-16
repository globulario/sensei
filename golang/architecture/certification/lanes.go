// SPDX-License-Identifier: AGPL-3.0-only

package certification

import (
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/authority"
	"github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/evidencereceipt"
	"github.com/globulario/sensei/golang/architecture/proofdischarge"
)

// statusRank orders lane statuses worst-first for within-lane folding:
// conflicted > blocked > unknown > stale > pass_with_exception > pass >
// not_applicable. not_applicable is a bottom rung a lane only reaches when
// explicitly stated (inspect-only proof lane); it is never improved into.
func statusRank(s closureprotocol.DimensionStatus) int {
	switch s {
	case closureprotocol.DimensionConflicted:
		return 6
	case closureprotocol.DimensionBlocked:
		return 5
	case closureprotocol.DimensionUnknown:
		return 4
	case closureprotocol.DimensionStale:
		return 3
	case closureprotocol.DimensionPassWithException:
		return 2
	case closureprotocol.DimensionPass:
		return 1
	case closureprotocol.DimensionNotApplicable:
		return 0
	default:
		return 5 // unknown vocabulary folds to blocked (fail-closed)
	}
}

// laneState accumulates findings within one lane and folds them into a single
// DimensionStatus via statusRank.
type laneState struct {
	result LaneResult
}

func newLane(lane Lane) *laneState {
	return &laneState{result: LaneResult{Lane: lane, Status: closureprotocol.DimensionPass}}
}

func (l *laneState) finding(status closureprotocol.DimensionStatus, reasons ...string) {
	if statusRank(status) > statusRank(l.result.Status) {
		l.result.Status = status
	}
	l.result.ReasonCodes = append(l.result.ReasonCodes, reasons...)
}

func (l *laneState) limit(codes ...string) {
	l.result.Limitations = append(l.result.Limitations, codes...)
}

func (l *laneState) contradiction(ids ...string) {
	l.result.Contradictions = append(l.result.Contradictions, ids...)
}

func (l *laneState) forbidden(ids ...string) {
	l.result.ForbiddenMoves = append(l.result.ForbiddenMoves, ids...)
}

func (l *laneState) done() LaneResult {
	l.result.ReasonCodes = closureprotocol.NormalizeSet(l.result.ReasonCodes)
	l.result.Limitations = closureprotocol.NormalizeSet(l.result.Limitations)
	l.result.Contradictions = closureprotocol.NormalizeSet(l.result.Contradictions)
	l.result.ForbiddenMoves = closureprotocol.NormalizeSet(l.result.ForbiddenMoves)
	return l.result
}

func isMutationKind(k closureprotocol.OperationKind) bool {
	return k != closureprotocol.OperationRead && k != closureprotocol.OperationObserve
}

// scopeLane recomputes scope from the admission decision, the single-use
// capability consumption, the scope-verification receipt (observed change
// set + result-tree binding), and the admitted change plan. It never reads a
// caller-supplied boolean: a "scope_compliant" label with violations present
// is still a violation, and a missing consumption or verification is blocked,
// never assumed.
func scopeLane(req Request, rec Records) LaneResult {
	lane := newLane(LaneScope)

	decision := rec.AdmissionDecision
	consumption := rec.CapabilityConsumption
	verification := rec.ScopeVerification
	plan := rec.AdmissionRequest.ChangePlan

	if decision.CapabilityID == "" && len(decision.OperationVerdicts) == 0 {
		lane.finding(closureprotocol.DimensionBlocked, ReasonScopeDecisionMissing)
		return lane.done()
	}
	if consumption.CapabilityID == "" {
		lane.finding(closureprotocol.DimensionBlocked, ReasonScopeConsumptionMissing)
	}
	if verification.Status == "" {
		lane.finding(closureprotocol.DimensionBlocked, ReasonScopeVerificationMissing)
	}
	if lane.result.Status == closureprotocol.DimensionBlocked {
		return lane.done()
	}

	// Chain integrity: request -> decision -> consumption -> verification.
	if strings.TrimSpace(req.AdmissionRequestDigestSHA256) == "" ||
		decision.RequestDigestSHA256 != req.AdmissionRequestDigestSHA256 {
		lane.finding(closureprotocol.DimensionBlocked, ReasonScopeRequestChainMismatch)
	}
	if consumption.DecisionDigestSHA256 != req.AdmissionDecisionDigestSHA256 ||
		consumption.CapabilityID != decision.CapabilityID {
		lane.finding(closureprotocol.DimensionBlocked, ReasonScopeCapabilityChainMismatch)
	}
	if consumption.Task.ID != req.TaskID {
		lane.finding(closureprotocol.DimensionBlocked, ReasonScopeTaskMismatch)
	}
	if verification.DecisionDigestSHA256 != req.AdmissionDecisionDigestSHA256 {
		lane.finding(closureprotocol.DimensionBlocked, ReasonScopeVerifyDecisionMismatch)
	}

	// Single use.
	if consumption.OneUseStatus != closureprotocol.ReceiptValid {
		lane.finding(closureprotocol.DimensionBlocked, ReasonScopeCapabilityReused)
	}

	// Stale admission: the capability must not have expired before it was
	// consumed.
	if exp := strings.TrimSpace(decision.CapabilityExpiry); exp != "" {
		expiry, expErr := time.Parse(time.RFC3339, exp)
		consumed, conErr := time.Parse(time.RFC3339, strings.TrimSpace(consumption.ConsumedAt))
		if expErr != nil || conErr != nil || !expiry.After(consumed) {
			lane.finding(closureprotocol.DimensionStale, ReasonScopeAdmissionExpired)
		}
	}

	// Operation coverage: admitted verdict for every planned operation and for
	// every consumed/observed operation.
	admitted := map[string]bool{}
	for _, verdict := range decision.OperationVerdicts {
		switch verdict.Verdict {
		case "admitted", "admitted_with_conditions":
			admitted[verdict.OperationID] = true
		}
	}
	for _, op := range plan.Operations {
		if !admitted[op.OperationID] {
			lane.finding(closureprotocol.DimensionBlocked, ReasonScopeOperationNotAdmitted+":"+op.OperationID)
		}
	}
	for _, id := range append(append([]string{}, consumption.ConsumedOperationIDs...), verification.ObservedOperationIDs...) {
		if !admitted[strings.TrimSpace(id)] {
			lane.finding(closureprotocol.DimensionBlocked, ReasonScopeOperationUnadmitted+":"+strings.TrimSpace(id))
		}
	}

	// Observed change set must stay inside the admitted mutation targets.
	mutationTargets := map[string]bool{}
	for _, op := range plan.Operations {
		if isMutationKind(op.Kind) && admitted[op.OperationID] {
			mutationTargets[strings.TrimSpace(op.Target)] = true
		}
	}
	for _, path := range verification.ObservedPaths {
		if !mutationTargets[strings.TrimSpace(path)] {
			lane.finding(closureprotocol.DimensionBlocked, ReasonScopeUnadmittedPath+":"+strings.TrimSpace(path))
		}
	}

	// Result-tree binding: the verification must have observed the exact
	// result under certification; anything else is a verification of some
	// other (earlier or foreign) result.
	if !binding.ResultBindingEqual(verification.ResultBinding, req.ResultBinding) {
		lane.finding(closureprotocol.DimensionStale, ReasonScopeResultBindingMismatch)
	}
	// The result must build on the admitted base.
	if baseRev := strings.TrimSpace(rec.AdmissionRequest.BaseBinding.Repository.Revision); baseRev != "" &&
		baseRev != strings.TrimSpace(req.ResultBinding.BaseRevision) {
		lane.finding(closureprotocol.DimensionStale, ReasonScopeBaseMismatch)
	}

	// Verification status, recomputed rather than trusted: violations override
	// a compliant label.
	switch {
	case len(verification.Violations) > 0 || verification.Status == ScopeViolated:
		for _, violation := range verification.Violations {
			code := strings.TrimSpace(violation.Code)
			lane.finding(closureprotocol.DimensionBlocked, ReasonScopeViolation+":"+code)
			if strings.HasPrefix(code, ReasonForbiddenMove+":") {
				lane.forbidden(strings.TrimPrefix(code, ReasonForbiddenMove+":"))
			}
		}
		if len(verification.Violations) == 0 {
			lane.finding(closureprotocol.DimensionBlocked, ReasonScopeViolation+":unspecified")
		}
	case verification.Status == ScopeCompliant:
		// contributes pass; any earlier finding already dominated
	case verification.Status == ScopeStale:
		lane.finding(closureprotocol.DimensionStale, ReasonScopeVerificationStale)
	default:
		lane.finding(closureprotocol.DimensionUnknown, ReasonScopeVerificationUnknown)
	}

	return lane.done()
}

// authorityLane recomputes authority from the typed actor binding, the
// verified per-operation AuthorityResolution records, and the admitted plan.
// Every mutation-capable operation needs a current, valid resolution with a
// grant and a legal mechanism, bound to the same actor that consumed the
// capability. Passing tests can never compensate: this lane never consults
// evidence.
func authorityLane(req Request, rec Records) LaneResult {
	lane := newLane(LaneAuthority)

	actor := rec.AdmissionRequest.ActorBinding
	if closureprotocol.ValidateActorBinding(actor) != nil {
		lane.finding(closureprotocol.DimensionBlocked, ReasonAuthorityActorInvalid)
		return lane.done()
	}

	// The capability consumer must be the authority-resolved actor.
	if rec.CapabilityConsumption.CapabilityID != "" {
		consumerDigest, errA := closureprotocol.SemanticDigest(rec.CapabilityConsumption.ConsumerActor)
		actorDigest, errB := closureprotocol.SemanticDigest(actor)
		if errA != nil || errB != nil || consumerDigest != actorDigest {
			lane.finding(closureprotocol.DimensionBlocked, ReasonAuthorityActorMismatch)
		}
	}

	claimed := map[string]bool{}
	for _, d := range closureprotocol.NormalizeSet(req.AuthorityResolutionDigests) {
		claimed[d] = true
	}

	// Typed-authority model: a single AuthorityResolution per change plan carries
	// per-operation results in OperationResults (the earlier flat, one-resolution-
	// per-operation shape is gone). Fold operation results only from resolutions
	// whose shape is valid, whose self-digest verifies, and whose digest this
	// request actually referenced. Two resolutions that disagree about the same
	// operation are a conflict.
	opResults := map[string]closureprotocol.AuthorityResolutionOperation{}
	foldedDigest := map[string]bool{}
	// The resolution's evaluation time is recorded per operation so the
	// delegation re-verification below reproduces the resolver's decision at the
	// exact instant authority was resolved.
	opEvaluatedAt := map[string]string{}
	for _, res := range rec.AuthorityResolutions {
		if closureprotocol.ValidateAuthorityResolution(res) != nil {
			lane.finding(closureprotocol.DimensionBlocked, ReasonAuthorityShapeInvalid)
			continue
		}
		digest, err := closureprotocol.AuthorityResolutionDigest(res)
		if err != nil || !claimed[digest] ||
			(strings.TrimSpace(res.AuthorityResolutionDigestSHA256) != "" && res.AuthorityResolutionDigestSHA256 != digest) {
			lane.finding(closureprotocol.DimensionBlocked, ReasonAuthorityDigestMismatch)
			continue
		}
		if foldedDigest[digest] {
			continue // identical resolution already folded
		}
		foldedDigest[digest] = true
		for _, opres := range res.OperationResults {
			id := strings.TrimSpace(opres.OperationID)
			if prior, dup := opResults[id]; dup {
				priorDigest, _ := closureprotocol.SemanticDigest(prior)
				thisDigest, _ := closureprotocol.SemanticDigest(opres)
				if priorDigest != thisDigest {
					lane.finding(closureprotocol.DimensionConflicted, ReasonAuthorityResolutionDuplicate+":"+id)
				}
				continue
			}
			opResults[id] = opres
			opEvaluatedAt[id] = res.EvaluatedAt
		}
	}

	// The delegation receipts the actor committed to, indexed so the lane can
	// bind each recorded receipt back to a digest the actor actually asserted.
	committedDelegation := map[string]bool{}
	for _, d := range closureprotocol.NormalizeSet(actor.DelegationReceiptDigests) {
		committedDelegation[d] = true
	}

	operations := append([]closureprotocol.ChangeOperation(nil), rec.AdmissionRequest.ChangePlan.Operations...)
	sort.SliceStable(operations, func(i, j int) bool { return operations[i].OperationID < operations[j].OperationID })

	for _, op := range operations {
		if !isMutationKind(op.Kind) {
			continue
		}
		res, ok := opResults[op.OperationID]
		if !ok {
			lane.finding(closureprotocol.DimensionUnknown, ReasonAuthorityOperationUnresolved+":"+op.OperationID)
			continue
		}
		switch res.Status {
		case closureprotocol.ReceiptValid:
			// proceed
		case closureprotocol.ReceiptStale:
			lane.finding(closureprotocol.DimensionStale, ReasonAuthorityResolutionStale+":"+op.OperationID)
			continue
		case closureprotocol.ReceiptConflicted:
			lane.finding(closureprotocol.DimensionConflicted, ReasonAuthorityResolutionInvalid+":"+op.OperationID+":"+string(res.Status))
			continue
		case closureprotocol.ReceiptUnknown:
			lane.finding(closureprotocol.DimensionUnknown, ReasonAuthorityResolutionInvalid+":"+op.OperationID+":"+string(res.Status))
			continue
		default:
			lane.finding(closureprotocol.DimensionBlocked, ReasonAuthorityResolutionInvalid+":"+op.OperationID+":"+string(res.Status))
			continue
		}
		if len(closureprotocol.NormalizeSet(res.GrantIDs)) == 0 {
			lane.finding(closureprotocol.DimensionBlocked, ReasonAuthorityGrantMissing+":"+op.OperationID)
		}
		if !containsString(res.LegalMechanisms, string(res.SelectedMechanism)) {
			lane.finding(closureprotocol.DimensionBlocked, ReasonAuthorityMechanismIllegal+":"+op.OperationID)
		}
		if res.SelectedMechanism != op.SelectedMechanism {
			lane.finding(closureprotocol.DimensionBlocked, ReasonAuthorityMechanismMismatch+":"+op.OperationID)
		}
		for _, domain := range op.AuthorityDomainIDs {
			if !containsString(res.AuthorityDomainIDs, domain) {
				lane.finding(closureprotocol.DimensionBlocked, ReasonAuthorityDomainMismatch+":"+op.OperationID)
			}
		}
		// A delegated actor must resolve through the operation's delegation chain,
		// and certification re-verifies that chain independently rather than
		// trusting the resolution's claim. It resolves each delegation id to a
		// concrete recorded receipt bound to a digest the actor committed to, then
		// re-runs the governed monotonicity verdict against the governed grants —
		// so a resolution can never certify a delegation the governed grants do
		// not actually permit, and cannot invent a delegation whose record was
		// never preserved.
		if len(actor.DelegationReceiptDigests) > 0 {
			for _, reason := range certifyDelegatedOperation(rec.GovernedAuthority, rec.DelegationReceipts, committedDelegation, op, res, opEvaluatedAt[op.OperationID]) {
				lane.finding(closureprotocol.DimensionBlocked, reason)
			}
		}
	}

	return lane.done()
}

// certifyDelegatedOperation independently re-verifies that a delegated operation
// resolved through a legitimate delegation chain. It returns one reason per
// violation (nil when the delegation is sound) and never trusts the resolution's
// claimed chain: it resolves each delegation id to a concrete recorded receipt
// bound to a digest the actor committed to, requires governed grants to be
// present, and re-runs the shared monotonicity verdict for every resolved
// authority domain against those governed grants — so certification reproduces
// the resolver's delegation decision from first principles.
func certifyDelegatedOperation(index authority.PolicyIndex, recorded []closureprotocol.DelegationReceipt, committed map[string]bool, op closureprotocol.ChangeOperation, res closureprotocol.AuthorityResolutionOperation, evaluatedAt string) []string {
	if len(res.DelegationChain) == 0 {
		// The actor asserted delegations but the resolution resolved through none:
		// it did not legitimately reach this operation.
		return []string{ReasonAuthorityDelegationUnresolved + ":" + op.OperationID}
	}
	// Index the recorded receipts by delegation id, admitting only receipts whose
	// digest the actor actually committed to (binding + tamper check).
	byID := map[string]closureprotocol.DelegationReceipt{}
	for _, r := range recorded {
		digest, err := closureprotocol.DelegationReceiptDigest(r)
		if err != nil || !committed[digest] {
			continue
		}
		byID[r.DelegationID] = r
	}
	chain := make([]closureprotocol.DelegationReceipt, 0, len(res.DelegationChain))
	for _, id := range res.DelegationChain {
		r, ok := byID[strings.TrimSpace(id)]
		if !ok {
			// Missing, unrecorded, or uncommitted delegation: never reconstructed.
			return []string{ReasonAuthorityDelegationUnresolved + ":" + op.OperationID + ":" + strings.TrimSpace(id)}
		}
		chain = append(chain, r)
	}
	if len(index.AuthorityGrants) == 0 {
		// No governed grants to verify against: fail closed rather than trust.
		return []string{ReasonAuthorityDelegationUnresolved + ":" + op.OperationID + ":no_governed_grants"}
	}
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(evaluatedAt))
	if err != nil {
		return []string{ReasonAuthorityDelegationUnresolved + ":" + op.OperationID + ":unresolvable_evaluation_time"}
	}
	domains := res.AuthorityDomainIDs
	if len(domains) == 0 {
		domains = op.AuthorityDomainIDs
	}
	var reasons []string
	for _, domain := range domains {
		covered := false
		lastVerdict := authority.DelegationParentUnresolved
		for _, r := range chain {
			grant, ok := index.AuthorityGrants[r.ParentGrantID]
			if !ok {
				lastVerdict = authority.DelegationParentMismatch
				continue
			}
			if v := authority.CheckDelegationForOperation(index, grant, r, op, domain, at); v == authority.DelegationOK {
				covered = true
				break
			} else {
				lastVerdict = v
			}
		}
		if !covered {
			reasons = append(reasons, ReasonAuthorityDelegationUnresolved+":"+op.OperationID+":"+domain+":"+string(lastVerdict))
		}
	}
	return reasons
}

// proofLane consumes Phase 5 ProofDischarge receipts. It never remaps evidence
// to slots (slot compatibility is proofdischarge's job); it verifies that every
// required obligation is discharged for THIS result, that every pass slot's
// mapped receipts are bound to this result binding and unrevoked, that
// exceptions are backed by exact, valid, unexpired waivers the policy permits,
// and that no governed runtime mandate was silently relaxed to not_applicable.
func proofLane(req Request, rec Records, policy CertificationPolicy) LaneResult {
	lane := newLane(LaneProof)

	now, err := time.Parse(time.RFC3339, strings.TrimSpace(req.EvaluatedAt))
	if err != nil {
		lane.finding(closureprotocol.DimensionUnknown, ReasonProofDischargeInvalid+":evaluated_at")
		return lane.done()
	}
	revoked := revokedSet(rec.Revocations)
	obligations := obligationByID(rec.Obligations)
	receiptsByID := map[string]closureprotocol.EvidenceReceipt{}
	for _, receipt := range rec.EvidenceReceipts {
		receiptsByID[strings.TrimSpace(receipt.ReceiptID)] = receipt
	}
	waiversByID := map[string]closureprotocol.WaiverReceipt{}
	for _, waiver := range rec.Waivers {
		waiversByID[strings.TrimSpace(waiver.WaiverID)] = waiver
	}
	claimedDigests := map[string]bool{}
	for _, d := range closureprotocol.NormalizeSet(req.ProofDischargeDigests) {
		claimedDigests[d] = true
	}

	dischargesByObligation := map[string]closureprotocol.ProofDischarge{}
	for _, discharge := range rec.ProofDischarges {
		dischargesByObligation[strings.TrimSpace(discharge.ObligationID)] = discharge
	}

	required := closureprotocol.NormalizeSet(rec.AdmissionDecision.RequiredProofSlots)
	if len(required) == 0 {
		inspectOnly := len(rec.AdmissionRequest.ChangePlan.Operations) > 0
		for _, op := range rec.AdmissionRequest.ChangePlan.Operations {
			if isMutationKind(op.Kind) {
				inspectOnly = false
				break
			}
		}
		if inspectOnly {
			lane.result.Status = closureprotocol.DimensionNotApplicable
			lane.limit(LimitationProofInspectOnly)
		} else {
			lane.limit(LimitationProofNoRequiredSlots)
		}
		return lane.done()
	}

	for _, requiredID := range required {
		discharge, ok := findDischarge(requiredID, dischargesByObligation, rec.ProofDischarges)
		if !ok {
			lane.finding(closureprotocol.DimensionBlocked, ReasonProofMissingObligation+":"+requiredID)
			continue
		}
		obligationID := strings.TrimSpace(discharge.ObligationID)

		if closureprotocol.ValidateProofDischarge(discharge) != nil {
			lane.finding(closureprotocol.DimensionBlocked, ReasonProofShapeInvalid+":"+obligationID)
			continue
		}
		digest, err := closureprotocol.ProofDischargeDigest(discharge)
		if err != nil || !claimedDigests[digest] {
			lane.finding(closureprotocol.DimensionBlocked, ReasonProofDigestUnreferenced+":"+obligationID)
			continue
		}
		if revoked[obligationID] || revoked[digest] {
			lane.finding(closureprotocol.DimensionBlocked, ReasonProofDischargeRevoked+":"+obligationID)
			continue
		}
		switch discharge.Status {
		case closureprotocol.ReceiptValid:
			// proceed to slots
		case closureprotocol.ReceiptStale:
			lane.finding(closureprotocol.DimensionStale, ReasonProofDischargeStale+":"+obligationID)
			continue
		case closureprotocol.ReceiptConflicted:
			lane.finding(closureprotocol.DimensionConflicted, ReasonProofIncompatibleReceipt+":"+obligationID)
			lane.contradiction(discharge.IncompatibleReceipts...)
			continue
		default:
			lane.finding(closureprotocol.DimensionBlocked, ReasonProofDischargeInvalid+":"+obligationID+":"+string(discharge.Status))
			continue
		}

		obligation, obligationKnown := obligations[obligationID]
		slotSpecs := map[string]proofdischarge.ProofSlotSpec{}
		if obligationKnown {
			for _, spec := range obligation.RequiredSlots {
				slotSpecs[strings.TrimSpace(spec.ID)] = spec
			}
		}

		for _, slot := range discharge.SlotResults {
			slotID := strings.TrimSpace(slot.SlotID)
			ref := obligationID + ":" + slotID
			switch slot.Status {
			case closureprotocol.DimensionPass:
				for _, receiptID := range slot.ReceiptIDs {
					receiptID = strings.TrimSpace(receiptID)
					receipt, found := receiptsByID[receiptID]
					if !found {
						lane.finding(closureprotocol.DimensionBlocked, ReasonProofReceiptUnresolved+":"+ref+":"+receiptID)
						continue
					}
					if revoked[receiptID] || receipt.Status == closureprotocol.ReceiptRevoked {
						lane.finding(closureprotocol.DimensionBlocked, ReasonProofReceiptRevoked+":"+ref+":"+receiptID)
						continue
					}
					if !binding.ResultBindingEqual(receipt.ResultBinding, req.ResultBinding) {
						lane.finding(closureprotocol.DimensionBlocked, ReasonProofResultBindingMismatch+":"+ref+":"+receiptID)
					}
				}
			case closureprotocol.DimensionPassWithException:
				waiverID, ok := proofSlotWaiver(slot, slotID, waiversByID, policy, now)
				if !ok {
					lane.finding(closureprotocol.DimensionBlocked, ReasonProofWaiverInvalid+":"+ref)
					continue
				}
				lane.finding(closureprotocol.DimensionPassWithException, ReasonProofWaived+":"+ref+":"+waiverID)
			case closureprotocol.DimensionNotApplicable:
				// A relaxed slot is legal only when the governed obligation is
				// known and its disposition under this policy really is
				// not_applicable. A mandated runtime slot can never be relaxed
				// (the governed invariant); an unknown obligation cannot prove
				// the relaxation was legal (fail-closed).
				if !obligationKnown {
					lane.finding(closureprotocol.DimensionUnknown, ReasonProofObligationUnresolved+":"+obligationID)
					continue
				}
				spec, hasSpec := slotSpecs[slotID]
				if !hasSpec {
					lane.finding(closureprotocol.DimensionUnknown, ReasonProofSlotUnknown+":"+ref)
					continue
				}
				disposition := proofdischarge.ResolveSlotDisposition(obligation, spec, string(policy.CoverageProfile))
				if disposition == proofdischarge.SlotRequired {
					lane.finding(closureprotocol.DimensionBlocked, ReasonProofRuntimeMandateOverride+":"+ref)
				}
			case closureprotocol.DimensionUnknown:
				if spec, hasSpec := slotSpecs[slotID]; hasSpec && !spec.Required {
					lane.limit(LimitationProofOptionalOpen + ":" + ref)
					continue
				}
				lane.finding(closureprotocol.DimensionUnknown, ReasonProofSlotUnknown+":"+ref)
			case closureprotocol.DimensionStale:
				lane.finding(closureprotocol.DimensionStale, ReasonProofMissingSlot+":"+ref)
			case closureprotocol.DimensionConflicted:
				lane.finding(closureprotocol.DimensionConflicted, ReasonProofMissingSlot+":"+ref)
				lane.contradiction(discharge.IncompatibleReceipts...)
			default: // blocked or unrecognized
				lane.finding(closureprotocol.DimensionBlocked, ReasonProofMissingSlot+":"+ref)
			}
		}
	}

	return lane.done()
}

// findDischarge matches a required ID against a discharge by obligation ID or
// by one of its slot IDs (required IDs may name either granularity).
func findDischarge(requiredID string, byObligation map[string]closureprotocol.ProofDischarge, all []closureprotocol.ProofDischarge) (closureprotocol.ProofDischarge, bool) {
	if discharge, ok := byObligation[requiredID]; ok {
		return discharge, true
	}
	for _, discharge := range all {
		for _, slot := range discharge.SlotResults {
			if strings.TrimSpace(slot.SlotID) == requiredID {
				return discharge, true
			}
		}
	}
	return closureprotocol.ProofDischarge{}, false
}

// proofSlotWaiver resolves the waiver backing a pass_with_exception slot: it
// must be referenced by the slot, valid, on the proof dimension, unexpired at
// the evaluation time, scoped to exactly this slot, and permitted by policy.
func proofSlotWaiver(slot closureprotocol.ProofSlotResult, slotID string, waivers map[string]closureprotocol.WaiverReceipt, policy CertificationPolicy, now time.Time) (string, bool) {
	if !policy.waiverAllowed(closureprotocol.DimensionProof) {
		return "", false
	}
	for _, receiptID := range slot.ReceiptIDs {
		waiver, ok := waivers[strings.TrimSpace(receiptID)]
		if !ok {
			continue
		}
		if closureprotocol.ValidateWaiverReceipt(waiver) != nil {
			continue
		}
		if waiver.Dimension != closureprotocol.DimensionProof || waiver.Status != closureprotocol.ReceiptValid {
			continue
		}
		expires, err := time.Parse(time.RFC3339, strings.TrimSpace(waiver.ExpiresAt))
		if err != nil || !expires.After(now) {
			continue
		}
		if !containsString(waiver.AppliesTo, slotID) {
			continue
		}
		return waiver.WaiverID, true
	}
	return "", false
}

// evidenceLane recomputes evidence from the admission decision's required
// evidence profiles, the Phase 4 validator (which ignores self-declared
// freshness and enforces owner paths, result binding, and runtime targets),
// and the governed runtime mandate. Runtime-kind profiles are not-applicable
// under static_test ONLY when every required governed obligation is resolved
// and none mandates runtime evidence.
func evidenceLane(req Request, rec Records, policy CertificationPolicy) LaneResult {
	lane := newLane(LaneEvidence)

	now, err := time.Parse(time.RFC3339, strings.TrimSpace(req.EvaluatedAt))
	if err != nil {
		lane.finding(closureprotocol.DimensionUnknown, ReasonEvidenceReceiptUnknown+":evaluated_at")
		return lane.done()
	}
	revoked := revokedSet(rec.Revocations)

	profiles := map[string]closureprotocol.EvidenceProfile{}
	for _, profile := range rec.EvidenceProfiles {
		profiles[strings.TrimSpace(profile.ProfileID)] = profile
	}
	receiptsByProfile := map[string][]closureprotocol.EvidenceReceipt{}
	for _, receipt := range rec.EvidenceReceipts {
		id := strings.TrimSpace(receipt.ProfileID)
		receiptsByProfile[id] = append(receiptsByProfile[id], receipt)
	}
	for _, list := range receiptsByProfile {
		sort.SliceStable(list, func(i, j int) bool {
			return strings.TrimSpace(list[i].ReceiptID) < strings.TrimSpace(list[j].ReceiptID)
		})
	}

	// Governed runtime mandate: relaxation of a runtime profile requires
	// positive knowledge that no applicable governed obligation mandates
	// runtime evidence. A required obligation that cannot be resolved leaves
	// the mandate unknown, and unknown mandate means no relaxation.
	mandate, mandateUnknown := governedRuntimeMandate(rec, lane)

	required := closureprotocol.NormalizeSet(rec.AdmissionDecision.RequiredEvidenceProfiles)
	if len(required) == 0 {
		lane.limit(LimitationEvidenceNoRequiredProfiles)
		return lane.done()
	}

	for _, profileID := range required {
		profile, known := profiles[profileID]
		if !known {
			lane.finding(closureprotocol.DimensionBlocked, ReasonEvidenceProfileUnresolved+":"+profileID)
			continue
		}
		runtimeProfile := evidencereceipt.ProfileRequiresRuntime(profile)
		if runtimeProfile && policy.CoverageProfile == CoverageStaticTest && !mandate && !mandateUnknown {
			// Not applicable now: documented, never a lane-status change.
			lane.limit(LimitationEvidenceRuntimeNotApplicable + ":" + profileID)
			continue
		}

		candidates := receiptsByProfile[profileID]
		if len(candidates) == 0 {
			reason := ReasonEvidenceMissingProfile
			if runtimeProfile {
				reason = ReasonEvidenceMissingRuntime
			}
			lane.finding(closureprotocol.DimensionBlocked, reason+":"+profileID)
			continue
		}

		// Unresolved conflicts poison the profile regardless of individual
		// receipt validity, unless an exact, policy-permitted waiver resolves
		// them (evidence-lane waivers ride the frozen epistemic dimension —
		// the frozen vocabulary has no "evidence" dimension).
		if conflicts := evidencereceipt.DetectConflicts(candidates); len(conflicts) > 0 {
			if waiverID, ok := evidenceWaiver(profileID, candidates, rec.Waivers, policy, now); ok {
				lane.finding(closureprotocol.DimensionPassWithException, ReasonEvidenceWaived+":"+profileID+":"+waiverID)
			} else {
				lane.finding(closureprotocol.DimensionConflicted, ReasonEvidenceConflicted+":"+profileID)
				for _, conflict := range conflicts {
					lane.contradiction(conflict.ReceiptA, conflict.ReceiptB)
				}
			}
			continue
		}

		worst := closureprotocol.DimensionNotApplicable
		var findings []string
		satisfied := false
		for _, receipt := range candidates {
			receiptID := strings.TrimSpace(receipt.ReceiptID)
			if revoked[receiptID] {
				worst = worseOf(worst, closureprotocol.DimensionBlocked)
				findings = append(findings, ReasonEvidenceReceiptRevoked+":"+receiptID)
				continue
			}
			assessment := evidencereceipt.Validate(evidencereceipt.ProofRequest{
				Profile:        profile,
				ExpectedResult: req.ResultBinding,
				RuntimeTarget:  rec.RuntimeTarget,
				Now:            now,
			}, receipt)
			switch assessment.Status {
			case closureprotocol.ReceiptValid:
				satisfied = true
			case closureprotocol.ReceiptStale:
				worst = worseOf(worst, closureprotocol.DimensionStale)
				findings = append(findings, ReasonEvidenceReceiptExpired+":"+receiptID)
			case closureprotocol.ReceiptUnknown:
				worst = worseOf(worst, closureprotocol.DimensionUnknown)
				findings = append(findings, ReasonEvidenceReceiptUnknown+":"+receiptID)
			case closureprotocol.ReceiptRevoked:
				worst = worseOf(worst, closureprotocol.DimensionBlocked)
				findings = append(findings, ReasonEvidenceReceiptRevoked+":"+receiptID)
			default: // invalid / conflicted / superseded
				worst = worseOf(worst, closureprotocol.DimensionBlocked)
				findings = append(findings, ReasonEvidenceReceiptInvalid+":"+receiptID)
			}
			findings = append(findings, prefixAll(assessment.ReasonCodes, receiptID)...)
		}
		if satisfied {
			continue // profile satisfied by at least one fully valid receipt
		}
		if worst == closureprotocol.DimensionNotApplicable {
			worst = closureprotocol.DimensionBlocked
		}
		lane.finding(worst, findings...)
	}

	return lane.done()
}

// governedRuntimeMandate reports whether any required governed obligation
// mandates runtime evidence. The second return is true when a required
// obligation could not be resolved (mandate unknowable — fail closed).
func governedRuntimeMandate(rec Records, lane *laneState) (bool, bool) {
	obligations := obligationByID(rec.Obligations)
	mandate := false
	unknown := false
	for _, requiredID := range closureprotocol.NormalizeSet(rec.AdmissionDecision.RequiredProofSlots) {
		obligation, ok := obligations[requiredID]
		if !ok {
			// The required ID may name a slot of a known obligation.
			found := false
			for _, candidate := range rec.Obligations {
				for _, slot := range candidate.RequiredSlots {
					if strings.TrimSpace(slot.ID) == requiredID {
						obligation, found = candidate, true
						break
					}
				}
				if found {
					break
				}
			}
			if !found {
				unknown = true
				lane.limit(LimitationEvidenceMandateUnknown + ":" + requiredID)
				continue
			}
		}
		if obligation.RequiresRuntimeEvidence {
			mandate = true
		}
	}
	return mandate, unknown
}

// evidenceWaiver resolves an epistemic-dimension waiver covering a conflicted
// evidence profile (or one of its conflicting receipts) exactly.
func evidenceWaiver(profileID string, candidates []closureprotocol.EvidenceReceipt, waivers []closureprotocol.WaiverReceipt, policy CertificationPolicy, now time.Time) (string, bool) {
	if !policy.waiverAllowed(closureprotocol.DimensionEpistemic) {
		return "", false
	}
	covered := map[string]bool{profileID: true}
	for _, receipt := range candidates {
		covered[strings.TrimSpace(receipt.ReceiptID)] = true
	}
	for _, waiver := range waivers {
		if closureprotocol.ValidateWaiverReceipt(waiver) != nil {
			continue
		}
		if waiver.Dimension != closureprotocol.DimensionEpistemic || waiver.Status != closureprotocol.ReceiptValid {
			continue
		}
		expires, err := time.Parse(time.RFC3339, strings.TrimSpace(waiver.ExpiresAt))
		if err != nil || !expires.After(now) {
			continue
		}
		for _, target := range waiver.AppliesTo {
			if covered[strings.TrimSpace(target)] {
				return waiver.WaiverID, true
			}
		}
	}
	return "", false
}

// worseOf folds two statuses to the more severe one (statusRank order).
func worseOf(current, next closureprotocol.DimensionStatus) closureprotocol.DimensionStatus {
	if statusRank(next) > statusRank(current) {
		return next
	}
	return current
}

func revokedSet(revocations []closureprotocol.RevocationReceipt) map[string]bool {
	out := make(map[string]bool, len(revocations))
	for _, revocation := range revocations {
		if id := strings.TrimSpace(revocation.RevokedTargetID); id != "" {
			out[id] = true
		}
	}
	return out
}

func containsString(list []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, item := range list {
		if strings.TrimSpace(item) == want {
			return true
		}
	}
	return false
}

func prefixAll(codes []string, suffix string) []string {
	out := make([]string, 0, len(codes))
	for _, code := range codes {
		out = append(out, code+":"+suffix)
	}
	return out
}
