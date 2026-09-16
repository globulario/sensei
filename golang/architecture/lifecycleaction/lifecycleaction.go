// SPDX-License-Identifier: AGPL-3.0-only

// Package lifecycleaction states which machine operation a governed task's
// lifecycle calls for next, once and for all readers.
//
// Three projections consumed the same governance fold and each decided for
// itself: applyGovernedDisposition, taskcontrol.selectNextAction and
// dispositionNextAction. Measured across the eight reachable states, two of
// three agreed on two of them. At scope_verified one said "rebuild the result",
// another said "request mutation admission" and the third said "record the
// result transition"; at mutation_observed one reopened admission entirely. The
// permission scalar cannot carry enough to choose an action, and every reader
// that tried to derive one from it produced a different answer.
//
// This is deliberately a LEAF: it imports only closureprotocol, so taskcontrol
// can consume it without the cycle that tasksession -> taskcontrol already
// creates. It names machine operations, reusing the tokens tasksession already
// defines; each adapter keeps its own presentation.
//
// It governs the mutation/completion tail ONLY. Evidence, questions and
// blockers are higher-priority selections that belong to their own owners, and
// a caller consults this after those have declined.
package lifecycleaction

import "github.com/globulario/sensei/golang/architecture/closureprotocol"

// Action is a machine operation. The values match tasksession's AdvanceNext*
// tokens so the three readers cannot drift into three vocabularies again.
type Action string

const (
	// ResolveAuthority: typed governance has not been established.
	ResolveAuthority Action = "resolve_authority"
	// DecideAdmission: authority is resolved and no typed decision binds.
	DecideAdmission Action = "decide_admission"
	// ConsumeCapability: a decision binds and its capability is unspent.
	ConsumeCapability Action = "consume_capability"
	// VerifyAdmission: the capability was validly spent. This RECONCILES and
	// records; it never asserts that a mutation still has to be performed,
	// because a process may have applied and crashed before recording.
	VerifyAdmission Action = "verify_admission"
	// VerifyScope: a change was observed and its scope is not yet verified.
	VerifyScope Action = "verify_scope"
	// RecordResultTransition: scope is verified. Rebuilding and binding the
	// result architecture is part of THIS operation, not a separate one.
	RecordResultTransition Action = "record_result_transition"
	// MechanicalRepair: scope verification is present and did not verify.
	MechanicalRepair Action = "perform_mechanical_repair"
	// None: refused. A disposition, not something to execute -- and explicitly
	// not completion.
	None Action = "none"
	// Unavailable: this owner does not govern the task. The caller keeps its own
	// protocol's established behaviour. NOT a safety verdict.
	Unavailable Action = "unavailable"
	// Blocked: this owner DOES govern the task, and its state is not safe to act
	// on -- contradictory inputs, or a state the closed mapping does not name.
	//
	// Distinct from Unavailable on purpose. An adapter that treats "no action"
	// as "keep doing what you were doing" would restore a historical
	// instruction -- including a stale mutation -- for a typed state that is
	// precisely the one nothing should be done in. Blocked must never restore a
	// prior action.
	Blocked Action = "blocked"
)

// The governance fold's status vocabulary, restated here because this package
// is a LEAF: tasksession declares these and imports this package, so this
// package cannot import them back. They are part of the closed mapping's key,
// not decoration, and a silent divergence would drop a row out of the mapping
// and turn a healthy state into Blocked. TestStatusVocabularyMatchesTaskSession
// pins every one of them to its tasksession constant.
const (
	StatusWaitingGovernance = "waiting_governance"
	StatusReadyForAdmission = "ready_for_admission"
	StatusReadyForMutation  = "ready_for_mutation"
	StatusAdmitted          = "admitted"
	StatusMutationObserved  = "mutation_observed"
	StatusScopeVerified     = "scope_verified"
	StatusWaitingMechanical = "waiting_mechanical_repair"
	StatusRefused           = "refused"
)

// position names the single lifecycle position a disposition occupies. A
// position is not a free Boolean: it is one component of a tuple that must also
// agree with the phase and status recorded beside it.
type position string

const (
	posNone                position = "none"
	posReadyForAdmission   position = "ready_for_admission"
	posCapabilityAvailable position = "capability_available"
	posCapabilityConsumed  position = "capability_consumed"
	posChangeObserved      position = "change_observed"
	posMechanicalRepair    position = "mechanical_repair_required"
	posScopeVerified       position = "scope_verified"
	posRefused             position = "refused"
)

// tuple is the complete key of the closed mapping: WHERE the task stands, and
// the phase and status recorded alongside that position, and whether typed
// authority is established.
//
// Position alone is not the state. A position flag set beside a phase or status
// describing a different -- or unnamed -- state is not a lifecycle position at
// all; it is a disagreement between two records of the same fact, and selecting
// an operation from the half that happens to be a Boolean discards the half that
// contradicts it.
type tuple struct {
	Position          position
	Phase             closureprotocol.TaskPhase
	Status            string
	AuthorityResolved bool
}

// closedMapping is THE mapping. Every row is a tuple foldGovernance actually
// produces; there are exactly eight, and this table is the enumeration.
// Anything absent from it is not a state this owner names, whatever its
// individual fields look like.
var closedMapping = map[tuple]Action{
	{posNone, closureprotocol.PhaseWaitingGovernance, StatusWaitingGovernance, false}:                  ResolveAuthority,
	{posReadyForAdmission, closureprotocol.PhaseReadyForAdmission, StatusReadyForAdmission, true}:      DecideAdmission,
	{posCapabilityAvailable, closureprotocol.PhaseAdmitted, StatusReadyForMutation, true}:              ConsumeCapability,
	{posCapabilityConsumed, closureprotocol.PhaseAdmitted, StatusAdmitted, true}:                       VerifyAdmission,
	{posChangeObserved, closureprotocol.PhaseMutationObserved, StatusMutationObserved, true}:           VerifyScope,
	{posMechanicalRepair, closureprotocol.PhaseWaitingMechanicalRepair, StatusWaitingMechanical, true}: MechanicalRepair,
	{posScopeVerified, closureprotocol.PhaseScopeVerified, StatusScopeVerified, true}:                  RecordResultTransition,
	{posRefused, closureprotocol.PhaseRefused, StatusRefused, true}:                                    None,
}

// Disposition is the lifecycle fact an adapter supplies. It is a value: this
// package reads no files and folds no chain.
type Disposition struct {
	// TypedProtocol reports that the TYPED protocol governs this task.
	//
	// Deliberately separate from AuthorityResolved. "Authority is not resolved"
	// and "this task is not on the typed protocol" are different claims, and
	// deriving the second from the first produces a field that is always true --
	// which is what the first attempt at this did. No durable source for it
	// exists yet, so callers assert it explicitly and carry the assumption
	// visibly. This package will not infer it.
	TypedProtocol bool
	// AuthorityResolved reports that typed authority is established.
	AuthorityResolved bool
	// Phase and Status are the governance fold's own vocabulary.
	Phase  closureprotocol.TaskPhase
	Status string
	// CapabilityAvailable: a decision binds and its capability is unspent.
	CapabilityAvailable bool
	// CapabilityConsumed: the capability was validly spent.
	CapabilityConsumed bool
	// ScopeVerified: scope verification passed; the operation is closed.
	ScopeVerified bool
	// ChangeObserved: a mutation was observed, scope not yet verified.
	ChangeObserved bool
	// MechanicalRepairRequired: scope verification present and not verified.
	MechanicalRepairRequired bool
	// ReadyForAdmission: authority is resolved and no typed decision binds yet.
	// An explicit flag, not "some status was set": any other nonempty status is
	// a state this owner does not name, and naming it admission would invent an
	// instruction for a state nobody described.
	ReadyForAdmission bool
	// Refused: the recorded decision does not bind.
	Refused bool
	// GrantInconsistent reports inputs that contradict each other -- a grant
	// flag disagreeing with the status accompanying it. Production cannot
	// produce this; a caller that does is describing a state that does not
	// exist, and the answer is to refuse rather than pick whichever field looks
	// more authoritative.
	GrantInconsistent bool
}

// Select returns the single machine operation this lifecycle state calls for.
//
// FAIL CLOSED. A state this owner governs but does not recognise returns
// Blocked -- never a fall-through to admission, mutation or completion, and
// never Unavailable, which permits the caller to keep its own behaviour. A state
// that reads as "nothing to do" is indistinguishable from "finished", and that
// is how a task awaiting admission came to be reported complete.
func Select(d Disposition) Action {
	// CONTRADICTIONS FIRST -- before Unavailable, not merely before selection.
	//
	// Unavailable is an answer ABOUT A COHERENT STATE: "this task is not mine,
	// keep your own protocol's behaviour". A disposition whose fields contradict
	// each other describes no state at all, and it is not any protocol's to keep
	// doing something in. Returning Unavailable first let contradictory evidence
	// reach a caller as compatibility-safe, where applyGovernedDisposition could
	// preserve a historical instruction -- including a stale mutation -- that
	// nothing in the contradictory state authorised. TypedProtocol is asserted by
	// the caller and inferred by nobody, so a FALSE assertion is exactly as
	// unverified as a true one and earns no shortcut past these gates.
	if d.GrantInconsistent {
		return Blocked
	}
	if d.lifecyclePositions() > 1 {
		return Blocked
	}
	if !d.AuthorityResolved && d.lifecyclePositions() > 0 {
		// Authority absent is only coherent with NOTHING downstream of it. A
		// consumed capability, a verified scope or any other position asserts a
		// fact that authority resolution is a precondition for, so the two
		// together describe a state that cannot have happened -- and answering
		// it with "resolve authority", or with the caller's own behaviour, would
		// quietly discard the downstream fact.
		return Blocked
	}
	if !d.TypedProtocol && d.AuthorityResolved {
		// Typed authority ESTABLISHED is itself the assertion that the typed
		// protocol governs this task. Claiming the authority while denying the
		// protocol is the same contradiction as an orphan position, and it must
		// not reach a caller as compatibility-safe either.
		return Blocked
	}
	if !d.TypedProtocol {
		// Not this owner's task, and nothing about it contradicts itself. The
		// caller keeps its own protocol's established behaviour.
		return Unavailable
	}
	// THE CLOSED MAPPING, by exact tuple. A position is checked against the
	// phase and status recorded with it; an unenumerated combination is a state
	// this owner does not name.
	//
	// FAIL CLOSED on a miss. NOT admission, and not the operation the position
	// flag alone would suggest: "some position was set" is not evidence that its
	// operation is the next step when the phase and status beside it describe
	// something else, and answering an unrecognised tuple with an instruction is
	// how a task reaches an operation nobody decided it should reach.
	if a, ok := closedMapping[d.key()]; ok {
		return a
	}
	return Blocked
}

// key is the disposition's tuple in the closed mapping.
func (d Disposition) key() tuple {
	return tuple{Position: d.position(), Phase: d.Phase, Status: d.Status, AuthorityResolved: d.AuthorityResolved}
}

// position returns the single position this disposition claims. Callers reach it
// only after lifecyclePositions has established there is at most one, so the
// order of these cases decides nothing.
func (d Disposition) position() position {
	switch {
	case d.Refused:
		return posRefused
	case d.ScopeVerified:
		return posScopeVerified
	case d.MechanicalRepairRequired:
		return posMechanicalRepair
	case d.ChangeObserved:
		return posChangeObserved
	case d.CapabilityAvailable:
		return posCapabilityAvailable
	case d.CapabilityConsumed:
		return posCapabilityConsumed
	case d.ReadyForAdmission:
		return posReadyForAdmission
	}
	return posNone
}

// lifecyclePositions counts the mutually exclusive positions a disposition
// claims. A task occupies exactly one; two or more is a contradiction, whichever
// pair it is.
func (d Disposition) lifecyclePositions() int {
	n := 0
	for _, at := range []bool{
		d.Refused, d.ScopeVerified, d.MechanicalRepairRequired,
		d.ChangeObserved, d.CapabilityAvailable, d.CapabilityConsumed,
		d.ReadyForAdmission,
	} {
		if at {
			n++
		}
	}
	return n
}

// Advances reports whether an action moves the task toward mutation.
//
// IT IS NOT A SAFETY PREDICATE. MechanicalRepair, ResolveAuthority and
// DecideAdmission all return false while still requesting real work, so
// !Advances() does not mean "no action is permitted". A test that asserts only
// !Advances() for a refused, blocked or unrecognised state proves nothing about
// what the caller was actually told to do; assert the exact outcome instead.
func (a Action) Advances() bool {
	switch a {
	case ConsumeCapability, VerifyAdmission, VerifyScope, RecordResultTransition:
		return true
	}
	return false
}
