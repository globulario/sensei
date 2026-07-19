// SPDX-License-Identifier: AGPL-3.0-only

package certification

import (
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// Evaluate is the single pure entry point: it validates the request, proves
// every resolved record against its referenced digest, recomputes the four
// lanes independently, and combines them into the frozen receipt. It never
// reads the wall clock, the filesystem, or the environment. It never returns
// a Result whose Receipt fails ValidateCertificationReceipt.
func Evaluate(req Request, rec Records, policy CertificationPolicy) (Result, error) {
	if err := ValidateRequest(req); err != nil {
		return Result{}, err
	}
	if policy.PolicyID == "" {
		return Result{}, ErrPolicyUnknown
	}
	if req.PolicyID != policy.PolicyID {
		return Result{}, fmt.Errorf("%w: request policy %q does not match evaluation policy %q", ErrRequestInvalid, req.PolicyID, policy.PolicyID)
	}
	if err := VerifyRecords(req, rec); err != nil {
		return Result{}, err
	}

	// The four lanes are computed independently; no lane sees another's
	// status.
	scope := scopeLane(req, rec)
	authority := authorityLane(req, rec)
	proof := proofLane(req, rec, policy)
	evidence := evidenceLane(req, rec, policy)
	lanes := [4]LaneResult{scope, authority, proof, evidence}

	forbidden := applicableForbiddenMoves(req, rec, lanes)
	verdict := combineVerdict(policy, lanes, rec.AdmissionRequest.ChangePlan, forbidden)

	receipt, err := buildReceipt(req, policy, lanes, forbidden, verdict)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Receipt:    receipt,
		Lanes:      lanes,
		NextAction: nextAction(verdict, lanes),
	}, nil
}

// applicableForbiddenMoves collects forbidden moves from the two typed
// sources: scope-verification violations (already lane-attributed) and typed,
// binding-valid findings. A finding bound to a different result, or scoped to
// operations outside the admitted plan, is not applicable and is ignored.
// Caller-supplied ID lists have no channel into this function.
func applicableForbiddenMoves(req Request, rec Records, lanes [4]LaneResult) []string {
	var out []string
	for _, lane := range lanes {
		out = append(out, lane.ForbiddenMoves...)
	}
	planOps := map[string]bool{}
	for _, op := range rec.AdmissionRequest.ChangePlan.Operations {
		planOps[op.OperationID] = true
	}
	for _, finding := range rec.ForbiddenMoveFindings {
		if strings.TrimSpace(finding.MoveID) == "" {
			continue
		}
		if !binding.ResultBindingEqual(finding.ResultBinding, req.ResultBinding) {
			continue // bound to some other result: not applicable here
		}
		if len(finding.OperationIDs) > 0 {
			applicable := false
			for _, id := range finding.OperationIDs {
				if planOps[strings.TrimSpace(id)] {
					applicable = true
					break
				}
			}
			if !applicable {
				continue
			}
		}
		out = append(out, strings.TrimSpace(finding.MoveID))
	}
	return closureprotocol.NormalizeSet(out)
}

// combineVerdict folds the four lanes into one frozen verdict. Priority, first
// match wins:
//
//  1. an applicable forbidden move blocks — even a perfect proof set
//  2. any conflicted lane -> uncertifiable (contradictory truth)
//  3. any blocked lane -> blocked (definite violation)
//  4. any unknown lane -> uncertifiable (missing truth; unknown authority is
//     never compensated by passing tests)
//  5. any stale lane -> stale
//  6. policy-required human review for a planned risk class -> review_required
//  7. any pass_with_exception -> certified_with_conditions (exact,
//     time-bounded, policy-permitted waivers only — enforced in the lanes)
//  8. all lanes pass or not_applicable -> certified
//
// `revoked` is never produced here: revocation is a later act against an
// already-persisted receipt.
func combineVerdict(policy CertificationPolicy, lanes [4]LaneResult, plan closureprotocol.ChangePlan, forbidden []string) closureprotocol.CertificationVerdict {
	if len(forbidden) > 0 {
		return closureprotocol.CertificationBlocked
	}
	has := func(status closureprotocol.DimensionStatus) bool {
		for _, lane := range lanes {
			if lane.Status == status {
				return true
			}
		}
		return false
	}
	switch {
	case has(closureprotocol.DimensionConflicted):
		return closureprotocol.CertificationUncertifiable
	case has(closureprotocol.DimensionBlocked):
		return closureprotocol.CertificationBlocked
	case has(closureprotocol.DimensionUnknown):
		return closureprotocol.CertificationUncertifiable
	case has(closureprotocol.DimensionStale):
		return closureprotocol.CertificationStale
	}
	for _, op := range plan.Operations {
		if op.RiskClass != "" && containsString(policy.RequireHumanReviewForRiskClasses, op.RiskClass) {
			return closureprotocol.CertificationReviewRequired
		}
	}
	if has(closureprotocol.DimensionPassWithException) {
		return closureprotocol.CertifiedWithConditions
	}
	return closureprotocol.Certified
}

// nextAction derives a deterministic next-step hint for the caller. It is
// advisory (never part of the frozen receipt or its digest).
func nextAction(verdict closureprotocol.CertificationVerdict, lanes [4]LaneResult) string {
	switch verdict {
	case closureprotocol.Certified, closureprotocol.CertifiedWithConditions:
		return "proceed to Phase 7 result rebuild/freshness verification"
	case closureprotocol.CertificationReviewRequired:
		return "obtain governed human review for the flagged risk classes"
	case closureprotocol.CertificationStale:
		return "refresh the stale admission, resolution, or evidence and re-certify"
	}
	for _, lane := range lanes {
		if lane.Status == closureprotocol.DimensionPass || lane.Status == closureprotocol.DimensionNotApplicable {
			continue
		}
		for _, reason := range lane.ReasonCodes {
			if strings.HasPrefix(reason, ReasonEvidenceMissingRuntime+":") ||
				strings.HasPrefix(reason, ReasonProofRuntimeMandateOverride+":") {
				return "obtain compatible runtime evidence"
			}
		}
	}
	if verdict == closureprotocol.CertificationUncertifiable {
		return "resolve the unknown or contradictory certification inputs"
	}
	return "resolve the blocking certification findings"
}
