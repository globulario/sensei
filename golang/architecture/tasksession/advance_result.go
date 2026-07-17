// SPDX-License-Identifier: Apache-2.0

package tasksession

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/resultpipeline"
	"github.com/globulario/sensei/golang/architecture/resultrecording"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
)

// AdvanceResultTransition is the single owner of Phase-7 advance-task sequencing.
// It advances a task by exactly one legal action derived from its verified ledger:
// the admission-v2 disposition is folded strictly (single verified snapshot, fail-
// closed), and at the legal scope_verified result step the transition is built by
// the accepted result pipeline and recorded by resultrecording — the sole side-
// effecting owner. A ledger write happens only at scope_verified; every earlier or
// blocked state is reported, not mutated.
//
// It carries no process-global mutable state: the trusted clock, snapshot hook, and
// pipeline recorder are immutable production dependencies injected through the
// unexported advanceResultTransition; tests call that helper with local
// dependencies instead of mutating any global.

// advanceDeps is the immutable dependency set for one advance. Production builds it
// once via productionAdvanceDeps; tests construct their own with a controlled clock,
// snapshot hook, or recorder — no shared global, no ForTest API, no fault toggle.
type advanceDeps struct {
	now           func() time.Time
	afterSnapshot func(taskDir string)
	prepare       func(context.Context, resultpipeline.PrepareTransitionRequest) (resultpipeline.TransitionCandidate, error)
	record        func(context.Context, resultrecording.RecordRequest) (resultrecording.RecordResult, error)
}

func productionAdvanceDeps() advanceDeps {
	return advanceDeps{
		now:           func() time.Time { return time.Now().UTC() },
		afterSnapshot: nil,
		prepare:       resultpipeline.PrepareTransition,
		record:        resultrecording.RecordTransition,
	}
}

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
	AdvanceNextRecordTransition  = "record_result_transition"
	AdvanceNextRetrySameAdvance  = "retry_same_advance"
	AdvanceNextNone              = "none"
)

// advanceNextSummaries maps every machine action identity (this package's plus the
// single-step identities resultrecording emits on the status projection) to a one-
// step human summary — the single source of truth, so Action and Summary can never
// disagree and no summary can smuggle a second action.
var advanceNextSummaries = map[string]string{
	AdvanceNextResolveAuthority:                "resolve typed authority for this task (prepare-change / enroll-agent)",
	AdvanceNextDecideAdmission:                 "run admit-change to decide typed admission",
	AdvanceNextConsumeCapability:               "run consume-admission to spend the single-use capability",
	AdvanceNextPerformMutation:                 "apply the admitted mutation",
	AdvanceNextVerifyScope:                     "run verify-admission to verify the observed change against the admitted scope",
	AdvanceNextMechanicalRepair:                "perform the mechanical repair that returns the change to the admitted scope",
	AdvanceNextRecordTransition:                "advance the task to record the result transition at scope_verified",
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
// digest, proof result, certification claim, or clock.
type AdvanceResultRequest struct {
	RepositoryRoot   string
	TaskDirectory    string
	RepositoryDomain string
	ResultRevision   string
}

// AdvanceResult is the deterministic description of what happened and what remains
// legal. For every non-recorded outcome the CURRENT state (head, sequence, phase,
// status, next action, waiting reasons, projection state) is reconstructed from the
// current verified ledger after the attempt — never the pre-attempt disposition.
// CurrentStateAvailable is false (with CurrentStateDetail set) when the current
// state cannot be reconstructed, and phase/status are then left empty.
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

// AdvanceError is a typed orchestration failure for malformed requests. State-
// machine outcomes are returned as an AdvanceResult with no error.
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

// AdvanceResultTransition performs one legal advance from verified task state using
// immutable production dependencies.
func AdvanceResultTransition(ctx context.Context, req AdvanceResultRequest) (AdvanceResult, error) {
	return advanceResultTransition(ctx, req, productionAdvanceDeps())
}

func advanceResultTransition(ctx context.Context, req AdvanceResultRequest, deps advanceDeps) (AdvanceResult, error) {
	taskDir := strings.TrimSpace(req.TaskDirectory)
	if taskDir == "" {
		return AdvanceResult{}, &AdvanceError{Code: AdvanceCodeInvalidRequest, Detail: "task directory is required"}
	}

	disp, err := governanceDisposition(taskDir, deps.now(), deps.afterSnapshot)
	if err != nil {
		var gerr *GovernanceError
		if errors.As(err, &gerr) {
			return withCurrentState(taskDir, deps, AdvanceResult{
				Outcome: OutcomeRefused, RefusalCode: gerr.Code, RefusalDetail: gerr.Detail,
			}), nil
		}
		return AdvanceResult{}, &AdvanceError{Code: AdvanceCodeLedgerUnreadable, Detail: err.Error()}
	}

	switch {
	case disp.Terminal:
		return advanceAtScopeVerified(ctx, req, taskDir, deps)
	case disp.Status == StatusRefused:
		return withCurrentState(taskDir, deps, AdvanceResult{
			Outcome:     OutcomeRefused,
			RefusalCode: "tasksession.admission_refused", RefusalDetail: "the recorded admission decision does not admit every operation, or its capability expired",
		}), nil
	default:
		// Every non-terminal, non-refused state is a waiting state whose single
		// current next action is derived from the disposition.
		return withCurrentState(taskDir, deps, AdvanceResult{Outcome: OutcomeWaiting}), nil
	}
}

// advanceAtScopeVerified builds the result-bound transition candidate through the
// accepted pipeline and records it. It regenerates no candidate bytes during
// recording, bypasses no expected-head protection, writes no projection directly,
// and appends no second event for an exact replay — all enforced by the recorder.
// The candidate's recorded_at is anchored to the scope_verified ledger event, so a
// retry produces a byte-identical receipt and reconciles instead of conflicting.
func advanceAtScopeVerified(ctx context.Context, req AdvanceResultRequest, taskDir string, deps advanceDeps) (AdvanceResult, error) {
	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		return AdvanceResult{}, &AdvanceError{Code: AdvanceCodeLedgerUnreadable, Detail: err.Error()}
	}
	recordedAt, err := admission.LoadEventProducedAt(taskDir, closureprotocol.LedgerEventScopeVerified)
	if err != nil {
		return AdvanceResult{}, &AdvanceError{Code: AdvanceCodeLedgerUnreadable, Detail: "scope_verified produced_at: " + err.Error()}
	}

	candidate, err := deps.prepare(ctx, resultpipeline.PrepareTransitionRequest{
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
		return withCurrentState(taskDir, deps, AdvanceResult{
			Outcome: OutcomeRefused, RefusalCode: errorCode(err, "resultpipeline.prepare_failed"), RefusalDetail: err.Error(),
		}), nil
	}

	res, rerr := deps.record(ctx, resultrecording.RecordRequest{TaskDirectory: taskDir, Candidate: candidate})
	if rerr != nil {
		var pce *resultrecording.PostCommitError
		if errors.As(rerr, &pce) {
			return withCurrentState(taskDir, deps, AdvanceResult{
				Outcome:                     OutcomePostCommitIncomplete,
				TransitionRecorded:          true,
				TransitionID:                pce.TransitionID,
				TransitionEntryDigestSHA256: pce.EntryDigestSHA256,
				PostCommitEntryDigestSHA256: pce.EntryDigestSHA256,
				PostCommitRecoveryAction:    pce.RecoveryAction,
				RefusalCode:                 pce.Code, RefusalDetail: pce.Detail,
			}), nil
		}
		var rec *resultrecording.Error
		if errors.As(rerr, &rec) && rec.Code == resultrecording.CodeStaleExpectedHead {
			return withCurrentState(taskDir, deps, AdvanceResult{
				Outcome: OutcomeStale, RefusalCode: rec.Code, RefusalDetail: rec.Detail,
			}), nil
		}
		return withCurrentState(taskDir, deps, AdvanceResult{
			Outcome: OutcomeRefused, RefusalCode: errorCode(rerr, resultrecording.CodeAppendFailed), RefusalDetail: rerr.Error(),
		}), nil
	}

	recorded := AdvanceResult{
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
	// A complete-but-blocked result stays scope_verified and retains its waiting
	// reasons; read them from the authoritative status projection.
	if res.TaskPhase == closureprotocol.PhaseScopeVerified {
		if cs, err := loadCurrentState(taskDir, deps); err == nil {
			recorded.WaitingReasons = cs.waitingReasons
		}
	}
	return recorded, nil
}

// withCurrentState reconstructs and attaches the genuinely current state after the
// attempt. It never labels the pre-attempt disposition as current: head, sequence,
// phase, status, next action, waiting reasons, and projection state all come from
// the current verified ledger / result-transition projection. On reconstruction
// failure it sets CurrentStateAvailable=false with the reason and leaves phase and
// status empty.
func withCurrentState(taskDir string, deps advanceDeps, r AdvanceResult) AdvanceResult {
	cs, err := loadCurrentState(taskDir, deps)
	if err != nil {
		r.CurrentStateAvailable = false
		r.CurrentStateDetail = "current state unavailable: " + err.Error()
		r.TaskPhase = ""
		r.OperationalStatus = ""
		return r
	}
	r.CurrentLedgerHeadSHA256 = cs.head
	r.LedgerSequence = cs.sequence
	r.TaskPhase = cs.phase
	r.OperationalStatus = cs.status
	r.NextAction = cs.next
	r.WaitingReasons = cs.waitingReasons
	r.ProjectionState = cs.projectionState
	r.CurrentStateAvailable = true
	return r
}

// currentSnapshot is the reconstructed current task state.
type currentSnapshot struct {
	head            string
	sequence        int
	phase           closureprotocol.TaskPhase
	status          string
	next            NextAction
	waitingReasons  []string
	projectionState string
}

// loadCurrentState derives the current state from the current verified ledger: the
// head/sequence from the verified chain, and phase/status/next/waiting/projection
// from the recorded result-transition projection when one exists (it reflects the
// furthest recorded state, including a transition another writer just recorded),
// else from the governance disposition. It reads the projection in-memory from the
// chain via ledger.Project, so a durable transition whose on-disk projection was not
// reconciled is still reported as its true current state (with projection_drift).
func loadCurrentState(taskDir string, deps advanceDeps) (currentSnapshot, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(governanceValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		return currentSnapshot{}, err
	}
	set, err := ledger.Project(chain)
	if err != nil {
		return currentSnapshot{}, err
	}
	cs := currentSnapshot{
		head:            chain.Head.EntryDigestSHA256,
		sequence:        chain.Head.Sequence,
		projectionState: ledger.ProjectionState(taskDir, set),
	}
	// A recorded result-transition writes the resultrecording status projection
	// (JSON, schema resultrecording.projection/v1). When present it is the furthest
	// recorded state (proving / complete-but-blocked). Any other projection (e.g. the
	// legacy tasksession lifecycle status) is not a result-transition state, so we
	// fall through to the governance disposition rather than misread it.
	if statusBytes, ok := set.Files["projections/status.yaml"]; ok {
		var doc struct {
			SchemaVersion     string                    `json:"schema_version"`
			TaskPhase         closureprotocol.TaskPhase `json:"task_phase"`
			OperationalStatus string                    `json:"operational_status"`
			WaitingOn         []string                  `json:"waiting_on"`
			NextAction        string                    `json:"next_action"`
		}
		if json.Unmarshal(statusBytes, &doc) == nil && doc.SchemaVersion == "resultrecording.projection/v1" {
			cs.phase = doc.TaskPhase
			cs.status = doc.OperationalStatus
			cs.next = advanceNext(doc.NextAction)
			cs.waitingReasons = doc.WaitingOn
			return cs, nil
		}
	}
	disp, err := governanceDisposition(taskDir, deps.now(), deps.afterSnapshot)
	if err != nil {
		return currentSnapshot{}, err
	}
	cs.phase = disp.Phase
	cs.status = disp.Status
	cs.next = dispositionNextAction(disp)
	return cs, nil
}

// dispositionNextAction maps a governance disposition to the single current next
// legal action.
func dispositionNextAction(disp governanceState) NextAction {
	switch {
	case !disp.Resolved:
		return advanceNext(AdvanceNextResolveAuthority)
	case disp.Status == StatusRefused:
		return advanceNext(AdvanceNextNone)
	case disp.Terminal:
		return advanceNext(AdvanceNextRecordTransition)
	case disp.Status == StatusWaitingMechanical:
		return advanceNext(AdvanceNextMechanicalRepair)
	case disp.GrantModify:
		return advanceNext(AdvanceNextConsumeCapability)
	case disp.Status == StatusMutationObserved:
		return advanceNext(AdvanceNextVerifyScope)
	case disp.Status == StatusAdmitted:
		return advanceNext(AdvanceNextPerformMutation)
	case disp.Status == StatusReadyForAdmission:
		return advanceNext(AdvanceNextDecideAdmission)
	default:
		return advanceNext(AdvanceNextResolveAuthority)
	}
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
