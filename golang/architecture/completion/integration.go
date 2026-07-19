// SPDX-License-Identifier: Apache-2.0

package completion

import (
	"context"
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/governedmutation"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/questionresolution"
)

const closureAssessmentSchemaVersion = "completion.closure_assessment/v1"

// ClosureVerdict is the closed set of end-to-end completion-closure verdicts.
type ClosureVerdict string

const (
	// ClosureAuthoritativeCompletion: the whole durable conjunction holds — a task is
	// authoritatively completed.
	ClosureAuthoritativeCompletion ClosureVerdict = "authoritative_completion"
	// ClosureNotCompleted: no completion exists (nor residue).
	ClosureNotCompleted ClosureVerdict = "not_completed"
	// ClosureBroken: a completed event exists but the durable conjunction does not
	// hold (missing/tampered receipt, a bound upstream component that no longer
	// verifies, or a wrong-bound completion).
	ClosureBroken ClosureVerdict = "broken_completion"
	// ClosureContradictory: contradictory terminal history.
	ClosureContradictory ClosureVerdict = "contradictory_terminal_history"
	// ClosureUnsupported: the current result world could not be established, or the
	// ledger did not verify.
	ClosureUnsupported ClosureVerdict = "unsupported"
)

// ComponentVerification is one owner's re-verification result in the composition.
type ComponentVerification struct {
	Component         string `json:"component" yaml:"component"`
	Verified          bool   `json:"verified" yaml:"verified"`
	BoundDigestSHA256 string `json:"bound_digest_sha256,omitempty" yaml:"bound_digest_sha256,omitempty"`
	Detail            string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

// CompletionClosureAssessment is the read-only, deterministic end-to-end
// composition: it reconstructs the terminal state and re-verifies each owner whose
// evidence the completion bound. Authoritative completion exists IFF the whole
// durable conjunction holds. It composes the accepted owners; it never appends
// completion, repairs upstream truth, promotes, disposes, certifies, or mutates.
type CompletionClosureAssessment struct {
	SchemaVersion                string                  `json:"schema_version" yaml:"schema_version"`
	Verdict                      ClosureVerdict          `json:"verdict" yaml:"verdict"`
	Terminal                     TerminalStateAssessment `json:"terminal" yaml:"terminal"`
	Components                   []ComponentVerification `json:"components,omitempty" yaml:"components,omitempty"`
	GovernedDriftAfterCompletion bool                    `json:"governed_drift_after_completion" yaml:"governed_drift_after_completion"`
	Bound                        []string                `json:"bound" yaml:"bound"`
	DigestSHA256                 string                  `json:"digest_sha256,omitempty" yaml:"digest_sha256,omitempty"`
}

func closureVerifierBound() []string {
	return []string{
		"a read-only end-to-end verification composing the accepted Phase-8 owners; it appends no completion and repairs no upstream truth",
		"authoritative completion exists only when the whole durable conjunction holds: correctness + closed question loop + completion authority + exactly one event + exactly one matching receipt + zero revoked + current result",
		"governed drift after completion is reported distinctly and never rewritten as corruption or current readiness",
	}
}

// VerifyCompletionClosure composes the accepted owners into a single end-to-end
// verdict for one task. It is read-only and deterministic.
func VerifyCompletionClosure(ctx context.Context, req Request) (CompletionClosureAssessment, error) {
	// One evaluation scope spans the terminal inspection AND the owner re-verification
	// below, so the same immutable ledger is digested at most once across the whole
	// closure check (InspectTerminalState reuses this scope rather than opening its own).
	ctx, _ = ledger.WithVerificationScope(ctx)
	root := strings.TrimSpace(req.RepositoryRoot)
	taskDir := strings.TrimSpace(req.TaskDirectory)
	if root == "" || taskDir == "" {
		// Absent identity is an identity failure (typed), never a runtime outage.
		return CompletionClosureAssessment{}, identityError("identity_absent", fmt.Errorf("repository root and task directory are required"))
	}
	terminal, ierr := InspectTerminalState(ctx, req)
	if ierr != nil {
		return CompletionClosureAssessment{}, ierr
	}
	a := CompletionClosureAssessment{SchemaVersion: closureAssessmentSchemaVersion, Terminal: terminal, Bound: closureVerifierBound()}

	switch terminal.State {
	case TerminalContradictoryHistory:
		a.Verdict = ClosureContradictory
	case TerminalReceiptWithoutEvent, TerminalEventWithoutValidReceipt, TerminalIntegrityFailure, TerminalWrongBinding:
		a.Verdict = ClosureBroken
	case TerminalNotCompleted:
		a.Verdict = ClosureNotCompleted
	case TerminalUnsupported:
		a.Verdict = ClosureUnsupported
	case TerminalCommitted, TerminalProjectionStaleOrMissing:
		// A valid durable conjunction exists; re-verify each bound owner end-to-end.
		comps, drift := reverifyOwners(ctx, root, taskDir, terminal)
		a.Components = comps
		a.GovernedDriftAfterCompletion = drift
		if allVerified(comps) {
			a.Verdict = ClosureAuthoritativeCompletion
		} else {
			a.Verdict = ClosureBroken
		}
	default:
		a.Verdict = ClosureUnsupported
	}
	return stampClosure(a), nil
}

// reverifyOwners re-verifies, through each owner, the evidence the completion bound:
// the completion conjunction (8.2b), the Phase-6 correctness receipt, the Phase-8.1d
// question-resolution certificate, and the frozen readiness binding. Governed drift
// is reported but is not a failure — freshness was proven at completion time.
func reverifyOwners(ctx context.Context, root, taskDir string, terminal TerminalStateAssessment) ([]ComponentVerification, bool) {
	comps := []ComponentVerification{
		{Component: "terminal_reconstruction", Verified: true, Detail: string(terminal.State)},
	}
	currentRB := terminal.CurrentResultBinding
	receipt, cerr := verifyDurableConjunction(ctx, taskDir, currentRB)
	if cerr != nil {
		comps = append(comps, ComponentVerification{Component: "completion_conjunction", Verified: false, Detail: cerr.Error()})
		return comps, false
	}
	comps = append(comps, ComponentVerification{Component: "completion_conjunction", Verified: true, BoundDigestSHA256: receipt.ReceiptDigestSHA256})

	// Phase-6 correctness: the bound correctness receipt still loads, verifies, and is
	// current for this result.
	correctnessOK, correctnessDetail := reverifyCorrectness(ctx, taskDir, currentRB, receipt.CorrectnessReceiptDigestSHA256)
	comps = append(comps, ComponentVerification{Component: "phase6_correctness", Verified: correctnessOK, BoundDigestSHA256: receipt.CorrectnessReceiptDigestSHA256, Detail: correctnessDetail})

	// Phase-8.1d question resolution: the bound certificate still loads and validates.
	qrOK, qrDetail := reverifyQuestionResolution(root, receipt.QuestionResolutionCertificateDigestSHA256)
	comps = append(comps, ComponentVerification{Component: "question_resolution", Verified: qrOK, BoundDigestSHA256: receipt.QuestionResolutionCertificateDigestSHA256, Detail: qrDetail})

	// 8.2a readiness: RECONSTRUCT the exact frozen readiness assessment from the
	// receipt's own bound fields, validate it, recompute its digest, and require
	// equality with the bound digest. Format proves syntax; recomputation proves
	// identity. This is a historical reconstruction, never a live AssessReadiness —
	// completion itself advanced the ledger head and terminal-conflict state.
	readyOK, readyDetail := reverifyReadiness(receipt)
	comps = append(comps, ComponentVerification{Component: "readiness_binding", Verified: readyOK, BoundDigestSHA256: receipt.ReadinessAssessmentDigestSHA256, Detail: readyDetail})

	governedManifest, _ := governedmutation.GovernedManifestDigest(root)
	return comps, receipt.GovernedManifestDigestSHA256 != governedManifest
}

func reverifyCorrectness(ctx context.Context, taskDir string, currentRB closureprotocol.ResultBinding, boundDigest string) (bool, string) {
	chain, err := ledger.NewStore(taskDir).VerifyChainCtx(ctx)
	if err != nil {
		return false, "chain unavailable"
	}
	ev := &evidence{resultBinding: currentRB, haveResultBinding: true}
	loadCorrectnessEvidence(taskDir, chain, currentRB, true, ev)
	if ev.correctnessCurrentValid != 1 {
		return false, "no unique valid current-result correctness certificate"
	}
	if ev.correctnessDigest != boundDigest {
		return false, "current correctness certificate does not match the completion-bound digest"
	}
	return true, ""
}

func reverifyQuestionResolution(root, boundDigest string) (bool, string) {
	cert, err := questionresolution.LoadCertificate(root, boundDigest)
	if err != nil {
		return false, "question-resolution certificate failed to load/validate: " + err.Error()
	}
	if cert.DigestSHA256 != boundDigest {
		return false, "question-resolution certificate digest does not match the bound digest"
	}
	return true, ""
}

// reconstructReadiness rebuilds the exact frozen readiness assessment that justified
// a completion, entirely from the completion receipt's own bound fields plus the
// fixed readiness constants. The Readiness verdict is recomputed from the bound
// obligations exactly as the readiness owner does (ready iff all satisfied), so a
// forged receipt with any unsatisfied/mutated obligation cannot reproduce the digest.
func reconstructReadiness(receipt TerminalCompletionReceipt) ReadinessAssessment {
	readiness := ReadinessReady
	for _, o := range receipt.Obligations {
		if o.State != EvidenceSatisfied {
			readiness = ReadinessNotReady
		}
	}
	return ReadinessAssessment{
		SchemaVersion:                ReadinessSchemaVersion,
		Task:                         receipt.Completion.Task,
		ResultBinding:                receipt.Completion.ResultBinding,
		TaskLedgerHeadDigestSHA256:   receipt.PreCompletionLedgerHeadDigestSHA256,
		GovernedManifestDigestSHA256: receipt.GovernedManifestDigestSHA256,
		Obligations:                  receipt.Obligations,
		Readiness:                    readiness,
		Limitations:                  readinessLimitations(),
		Bound:                        boundStatement(),
	}
}

// reverifyReadiness proves the bound readiness owner by identity, not syntax: it
// reconstructs the frozen assessment, validates its schema and obligations, recomputes
// its self-excluding digest, and requires exact equality with the receipt-bound digest
// AND that the reconstructed assessment is actually ready.
func reverifyReadiness(receipt TerminalCompletionReceipt) (bool, string) {
	// The bound obligation set must be exactly the canonical set — no missing, no
	// duplicate, no extra — proven explicitly so a semantic-digest set-normalization
	// can never hide a duplicated or dropped obligation.
	if len(receipt.Obligations) != len(obligationOrder) {
		return false, "readiness obligation count does not match the canonical set"
	}
	seen := map[ObligationID]bool{}
	for _, o := range receipt.Obligations {
		if seen[o.Obligation] {
			return false, "duplicate readiness obligation"
		}
		seen[o.Obligation] = true
	}
	for _, id := range obligationOrder {
		if !seen[id] {
			return false, "missing readiness obligation " + string(id)
		}
	}
	r := reconstructReadiness(receipt)
	digest, err := AssessmentDigest(r)
	if err != nil {
		return false, "readiness digest recompute failed: " + err.Error()
	}
	r.DigestSHA256 = digest
	if verr := ValidateAssessment(r); verr != nil {
		return false, "reconstructed readiness invalid: " + verr.Error()
	}
	if r.Readiness != ReadinessReady {
		return false, "reconstructed readiness is not ready"
	}
	if digest != receipt.ReadinessAssessmentDigestSHA256 {
		return false, "reconstructed readiness digest does not match the completion-bound digest"
	}
	return true, ""
}

func allVerified(comps []ComponentVerification) bool {
	for _, c := range comps {
		if !c.Verified {
			return false
		}
	}
	return true
}

func stampClosure(a CompletionClosureAssessment) CompletionClosureAssessment {
	a.DigestSHA256 = ""
	if d, err := closureprotocol.SemanticDigest(a); err == nil {
		a.DigestSHA256 = d
	}
	return a
}
