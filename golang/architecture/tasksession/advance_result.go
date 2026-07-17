// SPDX-License-Identifier: Apache-2.0

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
// and rebuilt projections. It reproduces no phase policy: the admission-v2
// disposition is folded strictly (single verified snapshot, fail-closed) by
// governanceDisposition, and at the legal scope_verified result step the transition
// is built by the accepted result pipeline and recorded by resultrecording — the
// sole side-effecting owner.
//
// It performs a ledger write only at scope_verified (recording the transition).
// Every earlier or blocked state is reported, not mutated: the agent owns applying
// admitted mutations, so the orchestrator names the ONE next legal action instead
// of forging it. Missing prerequisites become a typed refusal/waiting outcome, and
// any governance-integrity error is a hard refusal — never success, never a grant.

// advanceNow is the trusted internal clock used for capability-expiry evaluation.
// It is unexported so no caller can backdate expiry; tests override it as a
// dependency seam. The candidate's recorded_at is NOT taken from this clock — it is
// anchored to the scope_verified ledger event so retries are byte-identical.
var advanceNow = func() time.Time { return time.Now().UTC() }

// AdvanceOutcome is the closed set of orchestration outcomes.
type AdvanceOutcome string

const (
	OutcomeWaiting              AdvanceOutcome = "waiting"
	OutcomeRecorded             AdvanceOutcome = "recorded"
	OutcomeRefused              AdvanceOutcome = "refused"
	OutcomeStale                AdvanceOutcome = "stale"
	OutcomePostCommitIncomplete AdvanceOutcome = "post_commit_incomplete"
)

// Stable machine next-action identities. Each names EXACTLY ONE step; the matching
// summary in advanceNextSummaries describes the same single step (never two).
const (
	AdvanceNextResolveAuthority  = "resolve_authority"
	AdvanceNextDecideAdmission   = "decide_admission"
	AdvanceNextConsumeCapability = "consume_capability"
	AdvanceNextPerformMutation   = "perform_mutation"
	AdvanceNextVerifyScope       = "verify_scope"
	AdvanceNextMechanicalRepair  = "perform_mechanical_repair"
	AdvanceNextReconcile         = "reconcile_from_current_head"
	AdvanceNextRetrySameAdvance  = "retry_same_advance"
	AdvanceNextNone              = "none"
)

// advanceNextSummaries maps every machine action identity (this package's plus the
// single-step identities resultrecording.ClassifyNextState emits) to a one-step
// human summary. It is the single source of truth, so Action and Summary can never
// disagree and no summary can smuggle a second action.
var advanceNextSummaries = map[string]string{
	AdvanceNextResolveAuthority:                "resolve typed authority for this task (prepare-change / enroll-agent)",
	AdvanceNextDecideAdmission:                 "run admit-change to decide typed admission",
	AdvanceNextConsumeCapability:               "run consume-admission to spend the single-use capability",
	AdvanceNextPerformMutation:                 "apply the admitted mutation",
	AdvanceNextVerifyScope:                     "run verify-admission to verify the observed change against the admitted scope",
	AdvanceNextMechanicalRepair:                "perform the mechanical repair that returns the change to the admitted scope",
	AdvanceNextReconcile:                       "re-derive from the current head and advance again",
	AdvanceNextRetrySameAdvance:                "retry the exact same advance to reconcile the durable entry",
	AdvanceNextNone:                            "no legal advance now; revise scope or authority",
	resultrecording.NextActionCompleteProof:    "complete the required proof",
	resultrecording.NextActionGovernanceReview: "resolve the governance review that blocks proving",
	resultrecording.NextActionResolveQuestion:  "answer the architect question that blocks proving",
	// resultrecording.NextActionMechanicalRepair shares AdvanceNextMechanicalRepair's
	// value ("perform_mechanical_repair"), already mapped above.
}

func advanceNext(id string) NextAction {
	return NextAction{Action: id, Summary: advanceNextSummaries[id]}
}

// AdvanceResultRequest carries only operational inputs. It never accepts a
// caller-supplied phase, status, authority verdict, admission success, derivable
// digest, proof result, certification claim, or clock — those are derived from
// verified records (and the trusted internal clock) or refused.
type AdvanceResultRequest struct {
	RepositoryRoot   string
	TaskDirectory    string
	RepositoryDomain string
	// ResultRevision names the committed result. Recording requires a committed
	// revision result because the candidate is built twice deterministically.
	ResultRevision string
}

// AdvanceResult is the deterministic description of what happened and what remains
// legal. The transition entry identity is reported separately from the actual
// current ledger head, which may have advanced past it. For every outcome the
// current verified head and projected phase/status are reported when available;
// CurrentStateAvailable is false (with CurrentStateDetail set) when they cannot be
// reconstructed, so a "current" field is never silently empty.
type AdvanceResult struct {
	Outcome AdvanceOutcome

	TransitionRecorded          bool
	TransitionDisposition       resultrecording.RecordDisposition
	TransitionID                string
	TransitionEntryDigestSHA256 string
	CurrentLedgerHeadSHA256     string
	LedgerSequence              int

	TaskPhase             closureprotocol.TaskPhase
	OperationalStatus     string
	NextAction            NextAction
	WaitingReasons        []string
	CurrentStateAvailable bool
	CurrentStateDetail    string

	RefusalCode   string
	RefusalDetail string

	PostCommitEntryDigestSHA256 string
	PostCommitRecoveryAction    string

	ProjectionState string
}

// AdvanceError is a typed orchestration failure for malformed requests or genuinely
// unreadable ledger state. State-machine outcomes (waiting, refused, stale, post-
// commit) are returned as an AdvanceResult with no error.
type AdvanceError struct {
	Code   string
	Detail string
}

func (e *AdvanceError) Error() string { return e.Code + ": " + e.Detail }

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

	// Fold the admission-v2 receipts strictly from one verified snapshot. A
	// governance-integrity error is a hard refusal that never grants or suggests
	// mutation; only genuine absence moves to an earlier phase.
	disp, err := governanceDisposition(taskDir, advanceNow())
	if err != nil {
		var gerr *GovernanceError
		if errors.As(err, &gerr) {
			return withCurrentState(taskDir, AdvanceResult{
				Outcome: OutcomeRefused, NextAction: advanceNext(AdvanceNextNone),
				RefusalCode: gerr.Code, RefusalDetail: gerr.Detail,
			}), nil
		}
		return AdvanceResult{}, &AdvanceError{Code: AdvanceCodeLedgerUnreadable, Detail: err.Error()}
	}

	switch {
	case disp.Terminal:
		return advanceAtScopeVerified(ctx, req, taskDir, disp)
	case !disp.Resolved:
		return waiting(taskDir, disp, advanceNext(AdvanceNextResolveAuthority)), nil
	case disp.Status == StatusRefused:
		return withCurrentState(taskDir, AdvanceResult{
			Outcome: OutcomeRefused, TaskPhase: disp.Phase, OperationalStatus: disp.Status,
			NextAction:  advanceNext(AdvanceNextNone),
			RefusalCode: "tasksession.admission_refused", RefusalDetail: "the recorded admission decision does not admit every operation, or its capability expired",
		}), nil
	case disp.Status == StatusWaitingMechanical:
		return waiting(taskDir, disp, advanceNext(AdvanceNextMechanicalRepair)), nil
	case disp.GrantModify:
		return waiting(taskDir, disp, advanceNext(AdvanceNextConsumeCapability)), nil
	case disp.Status == StatusMutationObserved:
		return waiting(taskDir, disp, advanceNext(AdvanceNextVerifyScope)), nil
	case disp.Status == StatusAdmitted:
		// Consumed capability, mutation not yet observed: the ONE next step is to
		// apply the admitted mutation (verification is a distinct later step).
		return waiting(taskDir, disp, advanceNext(AdvanceNextPerformMutation)), nil
	case disp.Status == StatusReadyForAdmission:
		return waiting(taskDir, disp, advanceNext(AdvanceNextDecideAdmission)), nil
	default:
		return waiting(taskDir, disp, advanceNext(AdvanceNextResolveAuthority)), nil
	}
}

// waiting builds a no-write waiting result and attaches the current head/state.
func waiting(taskDir string, disp governanceState, next NextAction) AdvanceResult {
	return withCurrentState(taskDir, AdvanceResult{
		Outcome:           OutcomeWaiting,
		TaskPhase:         disp.Phase,
		OperationalStatus: disp.Status,
		NextAction:        next,
	})
}

// withCurrentState fills the actual current verified head (and, if not already
// set, phase/status) so every non-recorded outcome carries the real current state
// rather than empty fields. When the head cannot be reconstructed it records the
// unavailability explicitly instead of leaving a silent blank.
func withCurrentState(taskDir string, r AdvanceResult) AdvanceResult {
	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		r.CurrentStateAvailable = false
		r.CurrentStateDetail = "current ledger head unavailable: " + err.Error()
		return r
	}
	if r.CurrentLedgerHeadSHA256 == "" {
		r.CurrentLedgerHeadSHA256 = head
	}
	r.CurrentStateAvailable = true
	return r
}

// advanceAtScopeVerified builds the result-bound transition candidate through the
// accepted pipeline and records it. It regenerates no candidate bytes during
// recording, bypasses no expected-head protection, writes no projection directly,
// and appends no second event for an exact replay — all enforced by
// resultrecording.RecordTransition, which this delegates to. The candidate's
// recorded_at is anchored to the scope_verified ledger event, so a retry produces a
// byte-identical receipt and reconciles instead of conflicting.
func advanceAtScopeVerified(ctx context.Context, req AdvanceResultRequest, taskDir string, disp governanceState) (AdvanceResult, error) {
	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		return AdvanceResult{}, &AdvanceError{Code: AdvanceCodeLedgerUnreadable, Detail: err.Error()}
	}
	recordedAt, err := admission.LoadEventProducedAt(taskDir, closureprotocol.LedgerEventScopeVerified)
	if err != nil {
		return AdvanceResult{}, &AdvanceError{Code: AdvanceCodeLedgerUnreadable, Detail: "scope_verified produced_at: " + err.Error()}
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
		RecordedAt:                     recordedAt,
	})
	if err != nil {
		return withCurrentState(taskDir, AdvanceResult{
			Outcome: OutcomeRefused, TaskPhase: disp.Phase, OperationalStatus: disp.Status,
			NextAction:  advanceNext(AdvanceNextNone),
			RefusalCode: errorCode(err, "resultpipeline.prepare_failed"), RefusalDetail: err.Error(),
		}), nil
	}

	next, cerr := resultrecording.ClassifyNextState(candidate.BuildResult.ProofRequirements)
	if cerr != nil {
		return AdvanceResult{}, &AdvanceError{Code: AdvanceCodeRecordFailed, Detail: cerr.Error()}
	}

	res, rerr := resultrecording.RecordTransition(ctx, resultrecording.RecordRequest{TaskDirectory: taskDir, Candidate: candidate})
	if rerr != nil {
		var pce *resultrecording.PostCommitError
		if errors.As(rerr, &pce) {
			return withCurrentState(taskDir, AdvanceResult{
				Outcome:                     OutcomePostCommitIncomplete,
				TransitionRecorded:          true,
				TransitionID:                pce.TransitionID,
				TransitionEntryDigestSHA256: pce.EntryDigestSHA256,
				CurrentLedgerHeadSHA256:     pce.LedgerHeadDigestSHA256,
				TaskPhase:                   disp.Phase, OperationalStatus: disp.Status,
				NextAction:                  advanceNext(AdvanceNextRetrySameAdvance),
				PostCommitEntryDigestSHA256: pce.EntryDigestSHA256,
				PostCommitRecoveryAction:    pce.RecoveryAction,
				RefusalCode:                 pce.Code, RefusalDetail: pce.Detail,
			}), nil
		}
		var rec *resultrecording.Error
		if errors.As(rerr, &rec) && rec.Code == resultrecording.CodeStaleExpectedHead {
			return withCurrentState(taskDir, AdvanceResult{
				Outcome: OutcomeStale, TaskPhase: disp.Phase, OperationalStatus: disp.Status,
				NextAction:  advanceNext(AdvanceNextReconcile),
				RefusalCode: rec.Code, RefusalDetail: rec.Detail,
			}), nil
		}
		return withCurrentState(taskDir, AdvanceResult{
			Outcome: OutcomeRefused, TaskPhase: disp.Phase, OperationalStatus: disp.Status,
			NextAction:  advanceNext(AdvanceNextNone),
			RefusalCode: errorCode(rerr, resultrecording.CodeAppendFailed), RefusalDetail: rerr.Error(),
		}), nil
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
		NextAction:                  advanceNext(res.NextAction),
		CurrentStateAvailable:       true,
		ProjectionState:             res.ProjectionState,
	}
	if res.TaskPhase == closureprotocol.PhaseScopeVerified {
		out.WaitingReasons = next.WaitingOn
	}
	return out, nil
}

// errorCode extracts a typed code from a resultrecording/resultpipeline error, else
// a fallback.
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
