// SPDX-License-Identifier: AGPL-3.0-only

package synthesis

import "fmt"

// Phase is the closed set of states the O1 deterministic orchestration
// state machine can occupy. Phase, SessionState, Command, Event, and
// Transition are all Step-2, runtime-only concepts: none of them are part
// of the six closed wire schemas from Step 1, and none of them are
// persisted as a governed document. Only the artifact documents
// (Session, Interpretation, Plan, Attempt, Evaluation, Receipt) are.
type Phase string

const (
	PhaseCreated    Phase = "created"
	PhasePlanning   Phase = "planning"
	PhasePlanned    Phase = "planned"
	PhaseAttempting Phase = "attempting"
	PhaseEvaluating Phase = "evaluating"
	PhaseRetry      Phase = "retry"
	PhaseReplan     Phase = "replan"

	// PhaseSucceeded means ONLY that a candidate was produced and is
	// eligible to be submitted to the existing admission owner. It NEVER
	// means admitted, verified, completed, or approved. Every Receipt
	// reaching this phase carries TerminalReason
	// ReasonCandidateReadyForAdmission and nothing stronger — see Receipt's
	// own doc comment and the O1 hard law that a passing evaluation is
	// evidence, not admission, correctness, completion, or approval.
	PhaseSucceeded Phase = "succeeded"
	PhaseFailed    Phase = "failed"
)

// Terminal reports whether p accepts no further lifecycle commands.
func (p Phase) Terminal() bool {
	return p == PhaseSucceeded || p == PhaseFailed
}

// SessionState is the pure runtime state Transition operates on. It is NOT
// a governed document and has no schema or semantic digest of its own —
// unlike the six Step-1 artifacts, nothing here is meant to be persisted as
// canonical truth outside the process driving the state machine. It stores
// only DIGESTS of the artifacts it has accepted, never their full bodies —
// provider prose (ProviderObservation) and other artifact content therefore
// cannot influence SessionState identity or its transitions by construction,
// satisfying the O1 hard law that timestamps and provider prose cannot
// influence authoritative transition identity unless explicitly included by
// contract.
type SessionState struct {
	Session Session
	Phase   Phase

	// InterpretationDigestSHA256 is recorded exactly once, by the command
	// that moves Created -> Planning, and never changes again for the life
	// of the session — including across every replan. A replan reuses the
	// original interpretation; it never accepts a new one.
	InterpretationDigestSHA256 string

	// PlanGeneration / AttemptNumber are the highest RECORDED (committed)
	// generation/attempt — 0 until the first RecordPlan/RecordAttempt is
	// accepted. They advance only when a matching immutable artifact is
	// actually appended, never merely because a Start* command reserved the
	// next number.
	PlanGeneration int
	AttemptNumber  int

	// ExpectedPlanGeneration / ExpectedAttemptNumber are transient
	// reservations: StartPlanning/StartAttempt set these to the next
	// number a subsequent RecordPlan/RecordAttempt must carry, WITHOUT
	// appending any artifact or advancing the recorded counters above. This
	// is what prevents an interrupted planning or attempt phase from
	// leaving a phantom recorded generation/attempt: if the session is
	// abandoned or resumed between Start* and Record*, PlanGeneration/
	// AttemptNumber still reflect only what was actually committed.
	ExpectedPlanGeneration int
	ExpectedAttemptNumber  int

	LatestPlanDigestSHA256       string
	LatestAttemptDigestSHA256    string
	LatestEvaluationDigestSHA256 string

	// RemainingRetryBudget / RemainingReplanBudget start at
	// Session.RetryBudget / Session.ReplanBudget and only ever decrease, by
	// exactly one, at the moment a classified retry/replan recommendation
	// is accepted (never at StartAttempt/StartPlanning, and never by more
	// than one per accepted evaluation). Consumed count is always derivable
	// as Session.RetryBudget-RemainingRetryBudget — not stored separately,
	// so the two can never drift apart.
	RemainingRetryBudget  int
	RemainingReplanBudget int

	// Receipt is set only once Phase.Terminal() is true.
	Receipt *Receipt
}

// ConsumedRetryBudget returns how many retry units have been consumed so
// far — always Session.RetryBudget-RemainingRetryBudget, never stored
// independently.
func (s SessionState) ConsumedRetryBudget() int {
	return s.Session.RetryBudget - s.RemainingRetryBudget
}

// ConsumedReplanBudget returns how many replan units have been consumed so
// far.
func (s SessionState) ConsumedReplanBudget() int {
	return s.Session.ReplanBudget - s.RemainingReplanBudget
}

// NewSessionState is the sole validated entry point for seeding a
// SessionState. It constructs the initial PhaseCreated state for a session,
// with full retry/replan budget remaining and nothing recorded yet — but
// only after confirming session's own declared SessionDigestSHA256 equals
// the digest SessionDigest independently recomputes from session's content.
// Schema validation alone cannot catch this: the schema only proves
// SessionDigestSHA256 looks like a SHA-256 hex value, never that it is the
// actual digest of the session it is attached to. A session that fails this
// check is rejected outright — the zero-value SessionState is returned
// alongside the error and must not be used.
func NewSessionState(session Session) (SessionState, error) {
	digest, err := SessionDigest(session)
	if err != nil {
		return SessionState{}, fmt.Errorf("synthesis: compute session digest: %w", err)
	}
	if session.SessionDigestSHA256 != digest {
		return SessionState{}, fmt.Errorf("synthesis: session declares digest %q but its actual computed digest is %q", session.SessionDigestSHA256, digest)
	}
	return SessionState{
		Session:               session,
		Phase:                 PhaseCreated,
		RemainingRetryBudget:  session.RetryBudget,
		RemainingReplanBudget: session.ReplanBudget,
	}, nil
}

// ObservedIdentity is what a caller presents to ResumeCommand: its own
// freshly-observed copies of every identity field O1 hard law 11 requires
// resumption to check. It deliberately covers the same field set as
// Session's own identity binding, no more and no less — admission/
// verification digests are never part of Session's identity (see Session's
// doc comment) and so are never part of this comparison either.
type ObservedIdentity struct {
	RepositoryDomain              string
	BaseRevision                  string
	WorkspaceIdentityDigestSHA256 string
	GraphAuthorityDigestSHA256    string
	TaskSessionDigestSHA256       string
	ClosureDigestSHA256           string
	ProofObligationDigests        []string
}

// matchesSession reports whether o matches state.Session's exact bound
// identity in every field, including proof-obligation SET equality
// (order-independent — this is a set, not an ordered list).
func (o ObservedIdentity) matchesSession(session Session) bool {
	return o.RepositoryDomain == session.RepositoryDomain &&
		o.BaseRevision == session.BaseRevision &&
		o.WorkspaceIdentityDigestSHA256 == session.WorkspaceIdentityDigestSHA256 &&
		o.GraphAuthorityDigestSHA256 == session.GraphAuthorityDigestSHA256 &&
		o.TaskSessionDigestSHA256 == session.TaskSessionDigestSHA256 &&
		o.ClosureDigestSHA256 == session.ClosureDigestSHA256 &&
		sameStringSet(o.ProofObligationDigests, session.ProofObligationDigests)
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, v := range a {
		counts[v]++
	}
	for _, v := range b {
		counts[v]--
		if counts[v] < 0 {
			return false
		}
	}
	return true
}
