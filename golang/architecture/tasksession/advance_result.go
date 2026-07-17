// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/resultpipeline"
	"github.com/globulario/sensei/golang/architecture/resultrecording"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
)

// AdvanceResultTransition is the single owner of Phase-7 advance-task sequencing.
// It advances a task by exactly one legal action derived from its verified ledger
// and rebuilt projections. It never reproduces phase policy: the admission-v2
// disposition is folded read-only by governanceDisposition, and at the legal
// scope_verified result step the transition is built by the accepted result
// pipeline and recorded by resultrecording — the sole side-effecting owner.
//
// It performs a ledger write only at scope_verified (recording the transition).
// Every earlier or blocked state is reported, not mutated: the caller (agent)
// owns applying admitted mutations, so the orchestrator names the one next legal
// action instead of forging it. Missing, contradictory, stale, or invalid
// prerequisites become a typed refusal/waiting outcome — never success.

// AdvanceOutcome is the closed set of orchestration outcomes.
type AdvanceOutcome string

const (
	// OutcomeWaiting: a legal action remains but the orchestrator cannot perform
	// it (it is the agent's to do); NextAction names it. No ledger write occurred.
	OutcomeWaiting AdvanceOutcome = "waiting"
	// OutcomeRecorded: a result transition is the authoritative outcome (including
	// an idempotent replay/reconcile that appended no second event).
	OutcomeRecorded AdvanceOutcome = "recorded"
	// OutcomeRefused: governance refuses the task; no legal advance exists now.
	OutcomeRefused AdvanceOutcome = "refused"
	// OutcomeStale: the ledger head moved concurrently; no transition was recorded
	// and nothing was falsely reported as success.
	OutcomeStale AdvanceOutcome = "stale"
	// OutcomePostCommitIncomplete: the transition entry is durable but derived-state
	// reconciliation did not complete; the committed identity is exposed and the
	// exact same advance may be retried.
	OutcomePostCommitIncomplete AdvanceOutcome = "post_commit_incomplete"
)

// AdvanceResultRequest carries only operational inputs. It never accepts a
// caller-supplied phase, status, authority verdict, admission success, derivable
// digest, proof result, or certification/completion claim — those are derived
// from verified records or refused.
type AdvanceResultRequest struct {
	RepositoryRoot   string
	TaskDirectory    string
	RepositoryDomain string
	// ResultRevision names the committed result. Recording requires a committed
	// revision result because the candidate is built twice deterministically.
	ResultRevision string
	// Now is the explicit stable clock (the RecordedAt assertion). The orchestrator
	// has no internal clock.
	Now time.Time
}

// AdvanceResult is the deterministic description of what happened and what remains
// legal. The transition entry identity is reported separately from the actual
// current ledger head, which may have advanced past it.
type AdvanceResult struct {
	Outcome AdvanceOutcome

	// Transition* are set when Outcome is recorded or post_commit_incomplete.
	TransitionRecorded          bool
	TransitionDisposition       resultrecording.RecordDisposition
	TransitionID                string
	TransitionEntryDigestSHA256 string
	CurrentLedgerHeadSHA256     string
	LedgerSequence              int

	TaskPhase         closureprotocol.TaskPhase
	OperationalStatus string
	NextAction        NextAction
	WaitingReasons    []string

	// RefusalCode/Detail carry the underlying typed reason for a refused or stale
	// outcome (e.g. resultrecording.stale_expected_head). Never a success.
	RefusalCode   string
	RefusalDetail string

	// PostCommit* are set only for OutcomePostCommitIncomplete.
	PostCommitEntryDigestSHA256 string
	PostCommitRecoveryAction    string

	ProjectionState string
}

// AdvanceError is a typed orchestration failure for malformed requests or
// genuinely unreadable ledger state. State-machine outcomes (waiting, refused,
// stale, post-commit) are returned as an AdvanceResult with no error.
type AdvanceError struct {
	Code   string
	Detail string
}

func (e *AdvanceError) Error() string { return e.Code + ": " + e.Detail }

// Stable advance-error codes.
const (
	AdvanceCodeInvalidRequest   = "tasksession.advance_invalid_request"
	AdvanceCodeLedgerUnreadable = "tasksession.advance_ledger_unreadable"
	AdvanceCodeRecordFailed     = "tasksession.advance_record_failed"
)

// AdvanceResultTransition performs one legal advance from verified task state.
func AdvanceResultTransition(ctx context.Context, req AdvanceResultRequest) (AdvanceResult, error) {
	taskDir := strings.TrimSpace(req.TaskDirectory)
	if taskDir == "" {
		return AdvanceResult{}, &AdvanceError{Code: AdvanceCodeInvalidRequest, Detail: "task directory is required"}
	}
	if req.Now.IsZero() {
		return AdvanceResult{}, &AdvanceError{Code: AdvanceCodeInvalidRequest, Detail: "now (stable clock) is required; the orchestrator has no internal clock"}
	}

	// Fold the admission-v2 receipts into the furthest legal phase, read-only.
	disp := governanceDisposition(taskDir, req.Now)

	switch {
	case disp.Terminal:
		// scope_verified: the one legal action is recording the result transition.
		return advanceAtScopeVerified(ctx, req, taskDir)
	case !disp.Resolved:
		return waiting(disp, NextAction{Action: NextRebuildResult, Summary: "resolve typed authority for this task (prepare-change / enroll-agent), then admit the change"}), nil
	case disp.Status == StatusRefused:
		return AdvanceResult{
			Outcome: OutcomeRefused, TaskPhase: disp.Phase, OperationalStatus: disp.Status,
			NextAction:  NextAction{Action: NextProposeKnowledge, Summary: "admission refused this operation; revise scope/authority — no mutation is granted"},
			RefusalCode: "tasksession.admission_refused", RefusalDetail: "the recorded admission decision does not admit every operation, or its capability expired",
		}, nil
	case disp.Status == StatusWaitingMechanical:
		return waiting(disp, NextAction{Action: NextPerformEdit, Summary: "scope verification found an out-of-envelope change; perform the mechanical repair, then re-verify"}), nil
	case disp.GrantModify:
		return waiting(disp, NextAction{Action: NextConsumeCapability, Summary: "run consume-admission to spend the single-use capability for this exact operation set before applying the mutation"}), nil
	case disp.Status == StatusAdmitted, disp.Status == StatusMutationObserved:
		return waiting(disp, NextAction{Action: NextVerifyAdmission, Summary: "apply the admitted mutation, then run verify-admission to record the observed change and verify scope"}), nil
	case disp.Status == StatusReadyForAdmission:
		return waiting(disp, NextAction{Action: NextPerformEdit, Summary: "run admit-change to decide typed admission for the resolved authority"}), nil
	default:
		return waiting(disp, NextAction{Action: NextRebuildResult, Summary: "await governance"}), nil
	}
}

// waiting builds a no-write waiting result from a disposition.
func waiting(disp governanceState, next NextAction) AdvanceResult {
	return AdvanceResult{
		Outcome:           OutcomeWaiting,
		TaskPhase:         disp.Phase,
		OperationalStatus: disp.Status,
		NextAction:        next,
	}
}

// advanceAtScopeVerified builds the result-bound transition candidate through the
// accepted pipeline and records it. It regenerates no candidate bytes during
// recording, bypasses no expected-head protection, writes no projection directly,
// and appends no second event for an exact replay — all of that is enforced by
// resultrecording.RecordTransition, which this delegates to.
func advanceAtScopeVerified(ctx context.Context, req AdvanceResultRequest, taskDir string) (AdvanceResult, error) {
	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		return AdvanceResult{}, &AdvanceError{Code: AdvanceCodeLedgerUnreadable, Detail: err.Error()}
	}

	candidate, err := resultpipeline.PrepareTransition(ctx, resultpipeline.PrepareTransitionRequest{
		Build: resultpipeline.BuildRequest{
			RepositoryRoot:   req.RepositoryRoot,
			TaskDirectory:    taskDir,
			ResultMode:       resulttransition.ResultModeRevision,
			ResultRevision:   strings.TrimSpace(req.ResultRevision),
			RepositoryDomain: req.RepositoryDomain,
		},
		ExpectedLedgerHeadDigestSHA256: head,
		RecordedAt:                     req.Now.UTC().Format(time.RFC3339),
	})
	if err != nil {
		// Missing/contradictory/invalid prerequisites (unresolved records, drifted
		// result, changed ledger) → typed refusal, never success.
		return AdvanceResult{
			Outcome: OutcomeRefused, OperationalStatus: StatusRefused,
			RefusalCode: errorCode(err, "resultpipeline.prepare_failed"), RefusalDetail: err.Error(),
		}, nil
	}

	// Waiting reasons come from the phase owner's own classifier — never re-derived
	// here. They are meaningful only when the recorded result stays scope_verified.
	next, cerr := resultrecording.ClassifyNextState(candidate.BuildResult.ProofRequirements)
	if cerr != nil {
		return AdvanceResult{}, &AdvanceError{Code: AdvanceCodeRecordFailed, Detail: cerr.Error()}
	}

	res, rerr := resultrecording.RecordTransition(ctx, resultrecording.RecordRequest{TaskDirectory: taskDir, Candidate: candidate})
	if rerr != nil {
		var pce *resultrecording.PostCommitError
		if errors.As(rerr, &pce) {
			// The entry is durable; expose committed identity and the retry path.
			return AdvanceResult{
				Outcome:                     OutcomePostCommitIncomplete,
				TransitionRecorded:          true,
				TransitionID:                pce.TransitionID,
				TransitionEntryDigestSHA256: pce.EntryDigestSHA256,
				CurrentLedgerHeadSHA256:     pce.LedgerHeadDigestSHA256,
				PostCommitEntryDigestSHA256: pce.EntryDigestSHA256,
				PostCommitRecoveryAction:    pce.RecoveryAction,
				RefusalCode:                 pce.Code, RefusalDetail: pce.Detail,
			}, nil
		}
		var rec *resultrecording.Error
		if errors.As(rerr, &rec) && rec.Code == resultrecording.CodeStaleExpectedHead {
			// A concurrent writer moved the head between prepare and record. No
			// transition was recorded; report stale, never a false success.
			return AdvanceResult{
				Outcome: OutcomeStale, OperationalStatus: StatusStale,
				NextAction:  NextAction{Action: NextRebuildResult, Summary: "the ledger head moved during recording; re-derive from the current head and advance again"},
				RefusalCode: rec.Code, RefusalDetail: rec.Detail,
			}, nil
		}
		// Any other typed recording failure (invalid candidate, id conflict, ...) is
		// a refusal, not success.
		return AdvanceResult{
			Outcome: OutcomeRefused, OperationalStatus: StatusRefused,
			RefusalCode: errorCode(rerr, resultrecording.CodeAppendFailed), RefusalDetail: rerr.Error(),
		}, nil
	}

	out := AdvanceResult{
		Outcome:                     OutcomeRecorded,
		TransitionRecorded:          true,
		TransitionDisposition:       res.Disposition,
		TransitionID:                res.TransitionID,
		TransitionEntryDigestSHA256: res.EntryDigestSHA256,
		CurrentLedgerHeadSHA256:     res.CurrentLedgerHeadSHA256,
		LedgerSequence:              res.LedgerSequence,
		TaskPhase:                   res.TaskPhase,
		OperationalStatus:           res.OperationalStatus,
		NextAction:                  NextAction{Action: res.NextAction, Summary: res.NextAction},
		ProjectionState:             res.ProjectionState,
	}
	// A complete-but-blocked result stays scope_verified and retains every blocker.
	if res.TaskPhase == closureprotocol.PhaseScopeVerified {
		out.WaitingReasons = next.WaitingOn
	}
	return out, nil
}

// errorCode extracts a typed code from a resultrecording error, else a fallback.
func errorCode(err error, fallback string) string {
	var rec *resultrecording.Error
	if errors.As(err, &rec) {
		return rec.Code
	}
	var pe *resultpipeline.ValidationError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return fallback
}
