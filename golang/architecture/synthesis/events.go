// SPDX-License-Identifier: AGPL-3.0-only

package synthesis

// Event is the closed set of observations Transition emits alongside a new
// SessionState. Events are evidence of what happened during one
// transition; they carry no authority and are not, themselves, governed
// documents.
type Event interface{ synthesisEvent() }

// InterpretationRecordedEvent reports that the session's single,
// session-lifetime interpretation was recorded.
type InterpretationRecordedEvent struct {
	InterpretationDigestSHA256 string
}

func (InterpretationRecordedEvent) synthesisEvent() {}

// PlanningStartedEvent reports that a plan generation number was reserved,
// without any Plan artifact yet existing for it.
type PlanningStartedEvent struct {
	ExpectedPlanGeneration int
}

func (PlanningStartedEvent) synthesisEvent() {}

// PlanRecordedEvent reports that an immutable Plan was appended and the
// recorded plan generation advanced to match.
type PlanRecordedEvent struct {
	PlanGeneration   int
	PlanDigestSHA256 string
}

func (PlanRecordedEvent) synthesisEvent() {}

// AttemptStartedEvent reports that an attempt number was reserved, without
// any Attempt artifact yet existing for it.
type AttemptStartedEvent struct {
	ExpectedAttemptNumber int
}

func (AttemptStartedEvent) synthesisEvent() {}

// AttemptRecordedEvent reports that an immutable Attempt was appended and
// the recorded attempt number advanced to match.
type AttemptRecordedEvent struct {
	AttemptNumber       int
	AttemptDigestSHA256 string
}

func (AttemptRecordedEvent) synthesisEvent() {}

// EvaluationRecordedEvent reports that an Evaluation was accepted for the
// current attempt.
type EvaluationRecordedEvent struct {
	EvaluationDigestSHA256 string
	Recommendation         Recommendation
}

func (EvaluationRecordedEvent) synthesisEvent() {}

// RetryScheduledEvent reports that exactly one retry budget unit was
// consumed and the session entered PhaseRetry.
type RetryScheduledEvent struct {
	RemainingRetryBudget int
}

func (RetryScheduledEvent) synthesisEvent() {}

// ReplanScheduledEvent reports that exactly one replan budget unit was
// consumed and the session entered PhaseReplan.
type ReplanScheduledEvent struct {
	RemainingReplanBudget int
}

func (ReplanScheduledEvent) synthesisEvent() {}

// SessionTerminatedEvent reports that the session reached a terminal
// phase, with the exact Receipt produced.
type SessionTerminatedEvent struct {
	TerminalReason      TerminalReason
	ReceiptDigestSHA256 string
}

func (SessionTerminatedEvent) synthesisEvent() {}

// ResumeValidatedEvent reports a successful (matching-identity) resume. It
// is observational only: the SessionState it accompanies is byte-for-byte
// identical to the state before the ResumeCommand, so this event carries no
// fields that could feed back into session identity.
type ResumeValidatedEvent struct{}

func (ResumeValidatedEvent) synthesisEvent() {}
