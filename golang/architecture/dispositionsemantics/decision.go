// SPDX-License-Identifier: AGPL-3.0-only

// Package dispositionsemantics states what a governed question disposition
// MEANS, separately from the machinery that records one.
//
// Three projections consume the same decision — task control, the control-state
// cache, and convergence — and each has its own vocabulary for what to do about
// it: an action kind, a wait class, a next action. Before this package they
// each read the raw ledger state and decided for themselves, and every one of
// them got it wrong in the same way (#230): they read the dialogue document's
// local status and never consulted the authority that had already decided.
//
// So the meaning lives here, once, and each projection keeps its own vocabulary
// for what that meaning does. This package deliberately knows nothing about
// actions, wait classes or admission: naming any of those here would move the
// coupling rather than remove it.
//
// It is also deliberately a leaf. The recording machinery (golang/architecture/
// questiondisposition) reaches identity, ledger and admission, and admission
// reaches convergence — so convergence could never import it. What a decision
// means has no business depending on how it was written down.
package dispositionsemantics

// State is the outcome an architect recorded for a question.
//
// The strings match the recorded receipt vocabulary exactly. An unrecognised
// value is not an error here: it decides nothing, which is the safe reading of
// a state this build does not understand.
type State string

const (
	// Dismissed: the architect decided no evidence will be sought.
	Dismissed State = "dismissed"
	// Deferred: the architect decided to wait rather than to stop asking.
	Deferred State = "deferred"
	// Answered: an answer was recorded against the question.
	Answered State = "answered"
	// TaskLocal: answered for this task only, never repository-wide truth.
	TaskLocal State = "task_local"
)

// Decision is what the verified ledger has decided about one question.
//
// The zero value means nothing was decided, which is what every question
// without a receipt carries and what any consumer gets by not supplying one.
type Decision struct {
	// State is the recorded outcome. Empty means undisposed.
	State State
	// Contested is true when the ledger holds more than one distinct receipt
	// for the question.
	Contested bool
	// ReceiptDigestSHA256 identifies the receipt the outcome came from, so a
	// suppressed demand can always be traced to the decision that suppressed
	// it.
	ReceiptDigestSHA256 string
}

// Recorded reports whether the ledger said anything at all about the question.
//
// Distinct from Decided: a contested question was recorded twice and decides
// nothing, yet anything derived before those receipts existed is still out of
// date. Cache validity asks Recorded; acting on a decision asks Decided.
func (d Decision) Recorded() bool { return d.State != "" }

// Decided reports whether a single, uncontested decision exists.
//
// Two conflicting receipts decide nothing: a contested question is exactly as
// open as an undisposed one, and must be treated that way by every consumer.
func (d Decision) Decided() bool {
	return d.State != "" && !d.Contested
}

// IsContested reports whether the ledger holds conflicting receipts.
func (d Decision) IsContested() bool { return d.Contested }

// DismissesEvidenceDemand reports that the architect decided no evidence will
// be sought for this question.
//
// A consumer that advertises an evidence demand after this is true is showing
// an operator a request the authority has already terminated.
func (d Decision) DismissesEvidenceDemand() bool {
	return d.Decided() && d.State == Dismissed
}

// RequiresArchitectJudgement reports that the question is waiting on the
// architect rather than on evidence.
//
// Deferral is a decision to wait, not a decision to stop asking, so it must not
// be read as resolution.
func (d Decision) RequiresArchitectJudgement() bool {
	return d.Decided() && d.State == Deferred
}

// LeavesDialogueAnswerAuthoritative reports that the dialogue document, not
// this ledger decision, settles whether the question has an answer.
//
// True for `answered` and `task_local`, which assert an answer exists: the
// document is the authority on whether it actually does, and a ledger that
// disagrees is a divergence to surface rather than a reason to go quiet. Also
// true when nothing was decided, and when a decision is contested, because in
// both cases this ledger has said nothing that overrides the document.
func (d Decision) LeavesDialogueAnswerAuthoritative() bool {
	if !d.Decided() {
		return true
	}
	return d.State == Answered || d.State == TaskLocal
}

// DecisionReceipt returns the receipt digest behind a decision, or an empty
// string when nothing was decided.
//
// A projection that suppresses or redirects on the strength of a decision
// should carry this, so the effect can be traced back to its cause.
func (d Decision) DecisionReceipt() string {
	if !d.Decided() {
		return ""
	}
	return d.ReceiptDigestSHA256
}
