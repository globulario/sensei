// SPDX-License-Identifier: Apache-2.0

package prereview

import "strings"

// DeriveDisposition computes the review disposition deterministically from the
// report's structured evidence. It never trusts a caller-supplied disposition.
//
// Urgent, blocking conditions are evaluated most-urgent-first. Only when no
// blocker applies is a settled positive state reported, and the two positive
// terminal states are honored solely from their receipts: certification
// requires a certification receipt, terminal closure requires a completion
// receipt. Scope verification alone never yields certified — scope is not
// correctness.
func DeriveDisposition(r PreReviewReport) ReviewDisposition {
	switch {
	case cannotVerify(r):
		return DispositionCannotVerify
	case scopeViolation(r):
		return DispositionScopeViolation
	case governanceRequired(r):
		return DispositionGovernanceRequired
	case mechanicalRepairRequired(r):
		return DispositionMechanicalRepairRequired
	case evidenceRequired(r):
		return DispositionEvidenceRequired
	case architectDecisionRequired(r):
		return DispositionArchitectDecisionRequired
	}
	// No blocker: report the most settled receipt-backed positive state.
	if r.Coverage.Level.AtLeast(CoverageTerminal) &&
		r.Result.Completion.HasReceipt() && r.Proof.Certification.IsCertified() {
		return DispositionTerminallyClosed
	}
	if r.Coverage.Level.AtLeast(CoverageProofBound) && r.Proof.Certification.IsCertified() {
		return DispositionCertified
	}
	return DispositionReadyForHumanReview
}

// cannotVerify: conflicting evidence or a contradicted load-bearing statement
// means the report cannot establish truth, which dominates every other state.
func cannotVerify(r PreReviewReport) bool {
	return len(r.Proof.ConflictedReceipts) > 0 || len(r.Epistemic.Contradicted) > 0
}

// scopeViolation: an observed change left its admitted envelope, or a scope
// verification failed.
func scopeViolation(r PreReviewReport) bool {
	if len(r.Change.OutOfEnvelopeChanges) > 0 {
		return true
	}
	if statusIn(r.Governance.ScopeStatus, "violated", "out_of_envelope") {
		return true
	}
	for _, v := range r.Governance.Violations {
		if strings.HasPrefix(strings.TrimSpace(v.Code), "scope.") {
			return true
		}
	}
	return false
}

// governanceRequired: a governed change whose authority, admission, or
// capability is not satisfied.
func governanceRequired(r PreReviewReport) bool {
	g := r.Governance
	return statusIn(g.AuthorityStatus, "unresolved", "refused", "conflicted", "waiting_governance") ||
		statusIn(g.AdmissionStatus, "refused", "waiting_governance", "ready_for_admission") ||
		statusIn(g.CapabilityStatus, "refused", "expired")
}

// mechanicalRepairRequired: a deterministic mechanical repair is owed before
// review can proceed.
func mechanicalRepairRequired(r PreReviewReport) bool {
	return statusIn(r.Governance.ScopeStatus, "waiting_mechanical_repair") ||
		statusIn(r.Governance.AdmissionStatus, "waiting_mechanical_repair")
}

// evidenceRequired: a required proof obligation lacks compatible, fresh
// evidence.
func evidenceRequired(r PreReviewReport) bool {
	if len(r.Proof.UnresolvedSlots) > 0 || len(r.Proof.StaleReceipts) > 0 {
		return true
	}
	for _, o := range r.Proof.RequiredObligations {
		if statusIn(o.Status, "unresolved", "required") {
			return true
		}
	}
	return false
}

// architectDecisionRequired: a blocking human question that only architecture
// judgment can settle is open.
func architectDecisionRequired(r PreReviewReport) bool {
	for _, a := range r.ReviewerAttention {
		if !a.Blocking {
			continue
		}
		switch a.Category {
		case AttentionArchitectQuestion, AttentionUnknownDirection, AttentionContradiction:
			return true
		}
	}
	return false
}

func statusIn(status string, want ...string) bool {
	s := strings.TrimSpace(status)
	if s == "" {
		return false
	}
	for _, w := range want {
		if s == w {
			return true
		}
	}
	return false
}
