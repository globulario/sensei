// SPDX-License-Identifier: AGPL-3.0-only

package synthesis

import (
	"fmt"

	"github.com/globulario/sensei/golang/architecture/interpretationclosure"
)

// Command is the closed set of inputs Transition accepts. Every Command
// implementation is a plain data value; a Command never carries authority
// of its own. Transition alone decides whether a given command is legal in a
// given SessionState. RecordInterpretationCommand is the one deliberate
// exception at the construction boundary: external callers cannot construct
// it without presenting a verified interpretation-closure receipt.
type Command interface{ synthesisCommand() }

// RecordInterpretationCommand carries the full Interpretation document that
// starts the session's single, session-lifetime interpretation. Legal only
// from PhaseCreated. Its carrier is unexported intentionally: code outside
// package synthesis must use NewRecordInterpretationCommand, so an authored
// interpretation cannot gain governing authority by direct struct literal.
type RecordInterpretationCommand struct{ certifiedInterpretationCommand }

type certifiedInterpretationCommand struct {
	Interpretation             Interpretation
	closureReceiptDigestSHA256 string
}

// NewRecordInterpretationCommand is the authority-promotion boundary between
// interpretation closure and O1 planning. It recomputes the interpretation
// digest and verifies a receipt against the exact repository revision and
// graph authority already bound into the session. A receipt saying
// "governing" is not trusted as a boolean: interpretationclosure recomputes
// its policy from the bound observations before this constructor succeeds.
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
	if err := interpretationclosure.VerifyForGoverning(receipt, digest, state.Session.BaseRevision, state.Session.GraphAuthorityDigestSHA256); err != nil {
		return RecordInterpretationCommand{}, fmt.Errorf("synthesis: interpretation is not certified for governing authority: %w", err)
	}
	return RecordInterpretationCommand{certifiedInterpretationCommand: certifiedInterpretationCommand{
		Interpretation:             interp,
		closureReceiptDigestSHA256: receipt.ReceiptDigestSHA256,
	}}, nil
}

// InterpretationClosureReceiptDigestSHA256 exposes audit provenance without
// exposing a constructor bypass. It is not repair-verification evidence.
func (c RecordInterpretationCommand) InterpretationClosureReceiptDigestSHA256() string {
	return c.closureReceiptDigestSHA256
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
