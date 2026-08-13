// SPDX-License-Identifier: AGPL-3.0-only

package synthesis

import (
	"fmt"

	"github.com/globulario/sensei/golang/architecture/interpretationclosure"
)

// Command is the closed set of inputs Transition accepts. Command values carry
// proposed data, never self-asserted authority. Transition remains the final
// deterministic legality boundary for every state change.
type Command interface{ synthesisCommand() }

// RecordInterpretationCommand carries the full Interpretation document that
// starts the session's single, session-lifetime interpretation. Legal only
// from PhaseCreated.
//
// Interpretation remains exported for source compatibility and inspection,
// but closureReceiptDigestSHA256 is intentionally package-private. A caller
// can still construct the data shape, but it cannot manufacture the authority
// marker that Transition requires. Code outside package synthesis must use
// NewRecordInterpretationCommand to create a command that can actually move O1
// from Created to Planning.
type RecordInterpretationCommand struct {
	Interpretation Interpretation

	closureReceiptDigestSHA256 string
}

// NewRecordInterpretationCommand is the authority-promotion boundary between
// interpretation closure and O1 planning. It recomputes the interpretation
// digest and verifies the supplied closure receipt against the exact
// repository revision, graph authority, and task closure already bound into
// the session. Every binding invariant must have an explicit Gate-1 result
// (supported, unknown, or contradicted), and every declared proof obligation
// must be represented as required for authority.
//
// This constructor deliberately does not make interpretation closure repair
// verification. It only establishes that this premise earned the right to
// govern. O4 and the downstream admission/verification chain remain separate.
func NewRecordInterpretationCommand(state SessionState, interp Interpretation, receipt interpretationclosure.Receipt) (RecordInterpretationCommand, error) {
	if interp.SessionDigestSHA256 != state.Session.SessionDigestSHA256 {
		return RecordInterpretationCommand{}, fmt.Errorf("synthesis: interpretation references session digest %q, expected %q", interp.SessionDigestSHA256, state.Session.SessionDigestSHA256)
	}
	digest, err := InterpretationDigest(interp)
	if err != nil {
		return RecordInterpretationCommand{}, fmt.Errorf("synthesis: compute interpretation digest: %w", err)
	}
	if interp.InterpretationDigestSHA256 != digest {
		return RecordInterpretationCommand{}, fmt.Errorf("synthesis: interpretation declares digest %q but its actual computed digest is %q", interp.InterpretationDigestSHA256, digest)
	}
	if err := interpretationclosure.VerifyForGoverning(
		receipt,
		digest,
		state.Session.BaseRevision,
		state.Session.GraphAuthorityDigestSHA256,
		state.Session.ClosureDigestSHA256,
		interp.BindingInvariants,
		interp.RequiredProofObligations,
	); err != nil {
		return RecordInterpretationCommand{}, fmt.Errorf("synthesis: interpretation is not certified for governing authority: %w", err)
	}
	return RecordInterpretationCommand{
		Interpretation:             interp,
		closureReceiptDigestSHA256: receipt.ReceiptDigestSHA256,
	}, nil
}

// InterpretationClosureReceiptDigestSHA256 exposes audit provenance without
// exposing a constructor bypass. It is premise-authority evidence, not
// repair-verification evidence.
func (c RecordInterpretationCommand) InterpretationClosureReceiptDigestSHA256() string {
	return c.closureReceiptDigestSHA256
}

// hasVerifiedInterpretationClosure is deliberately package-private. The
// marker is minted only by NewRecordInterpretationCommand after full receipt
// verification, and Transition checks it again before accepting the command.
// The shape check catches zero-value/legacy literals while keeping Transition
// independent from interpretationclosure's evidence model.
func (c RecordInterpretationCommand) hasVerifiedInterpretationClosure() bool {
	if len(c.closureReceiptDigestSHA256) != 64 {
		return false
	}
	for _, r := range c.closureReceiptDigestSHA256 {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func (RecordInterpretationCommand) synthesisCommand() {}

// StartPlanningCommand begins a new planning generation. Legal only from
// PhaseReplan (the Created -> Planning entry instead goes through
// RecordInterpretationCommand, which both provides the interpretation and
// starts planning in one step).
type StartPlanningCommand struct{}

func (StartPlanningCommand) synthesisCommand() {}

// RecordPlanCommand carries the full Plan document produced for the
// currently expected plan generation. Legal only from PhasePlanning.
type RecordPlanCommand struct{ Plan Plan }

func (RecordPlanCommand) synthesisCommand() {}

// StartAttemptCommand reserves the next attempt number without appending
// any Attempt artifact. Legal from PhasePlanned or PhaseRetry.
type StartAttemptCommand struct{}

func (StartAttemptCommand) synthesisCommand() {}

// RecordAttemptCommand carries the full Attempt document produced for the
// currently expected attempt number. Legal only from PhaseAttempting.
type RecordAttemptCommand struct{ Attempt Attempt }

func (RecordAttemptCommand) synthesisCommand() {}

// RecordEvaluationCommand carries the full Evaluation document for the
// current attempt. Legal only from PhaseEvaluating. Its Evaluation.
// Recommendation determines the resulting phase: PhaseSucceeded,
// PhaseRetry, PhaseReplan, or PhaseFailed. Evaluation itself carries no
// timestamp field, so CompletedAt supplies the completion timestamp for
// whichever terminal Receipt (if any) this evaluation produces.
type RecordEvaluationCommand struct {
	Evaluation  Evaluation
	CompletedAt string
}

func (RecordEvaluationCommand) synthesisCommand() {}

// EvaluatorUnavailableCommand reports that no evaluation could be obtained
// at all, as distinct from an evaluation that ran and reported checks as
// unavailable. Legal only from PhaseEvaluating.
type EvaluatorUnavailableCommand struct {
	Detail string
	At     string
}

func (EvaluatorUnavailableCommand) synthesisCommand() {}

// AbortCommand explicitly stops the session. Legal from any non-terminal
// phase.
type AbortCommand struct {
	Reason string
	At     string
}

func (AbortCommand) synthesisCommand() {}

// ResumeCommand presents a caller's freshly-observed identity for comparison
// against the session's bound identity. Legal from any non-terminal phase.
type ResumeCommand struct {
	Observed ObservedIdentity
	At       string
}

func (ResumeCommand) synthesisCommand() {}
