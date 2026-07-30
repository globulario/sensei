// SPDX-License-Identifier: AGPL-3.0-only

package synthesis

// Command is the closed set of inputs Transition accepts. Every Command
// implementation is a plain data value; a Command never carries authority
// of its own — Transition alone decides whether a given command is legal
// in a given SessionState.
type Command interface{ synthesisCommand() }

// RecordInterpretationCommand carries the full Interpretation document that
// starts the session's single, session-lifetime interpretation. Legal only
// from PhaseCreated. A replan later in the session's life reuses this same
// interpretation — RecordInterpretationCommand is never legal a second time
// in the same session.
type RecordInterpretationCommand struct{ Interpretation Interpretation }

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
// whichever terminal Receipt (if any) this evaluation produces —
// Transition never reads a clock. Unused when the recommendation is
// retry-generation or replan with budget remaining (no receipt is produced
// in that case).
type RecordEvaluationCommand struct {
	Evaluation  Evaluation
	CompletedAt string
}

func (RecordEvaluationCommand) synthesisCommand() {}

// EvaluatorUnavailableCommand reports that no evaluation could be obtained
// at all — the evaluator infrastructure itself failed to run, as distinct
// from an evaluation that ran and reported checks as "unavailable". Legal
// only from PhaseEvaluating. At is the caller-supplied completion
// timestamp for the resulting receipt; Transition never reads a clock.
type EvaluatorUnavailableCommand struct {
	Detail string
	At     string
}

func (EvaluatorUnavailableCommand) synthesisCommand() {}

// AbortCommand explicitly stops the session. Legal from any non-terminal
// phase — an operator or governing owner may need to stop a session during
// planning, attempting, or any other non-terminal phase, not only while an
// evaluation is being decided. At is the caller-supplied completion
// timestamp for the resulting receipt.
type AbortCommand struct {
	Reason string
	At     string
}

func (AbortCommand) synthesisCommand() {}

// ResumeCommand presents a caller's freshly-observed identity for
// comparison against the session's bound identity. Legal from any
// non-terminal phase. A match leaves the state byte-for-byte unchanged (a
// successful no-op, observed only via ResumeValidatedEvent); a mismatch
// transitions to PhaseFailed with TerminalReason
// ReasonIdentityDriftRefused. At is the caller-supplied completion
// timestamp, used only in the mismatch case.
type ResumeCommand struct {
	Observed ObservedIdentity
	At       string
}

func (ResumeCommand) synthesisCommand() {}
