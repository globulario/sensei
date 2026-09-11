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
	if !d.TypedProtocol {
		// Not this owner's task. The caller keeps its own protocol's behaviour.
		return Unavailable
	}
	// CONTRADICTIONS FIRST, before any action can be selected.
	//
	// Checking them inside the selection switch let an earlier branch answer
	// before the contradiction was reached: a disposition that was both
	// scope-verified AND carried a contradictory capability pair selected
	// record_result_transition and never reached its own rejection. Incompatible
	// inputs describe a state that does not exist, and no action is safe in one.
	if d.GrantInconsistent {
		return Blocked
	}
	if positions := d.lifecyclePositions(); positions > 1 {
		return Blocked
	}
	if !d.AuthorityResolved {
		// Authority absent is only coherent with NOTHING downstream of it. A
		// consumed capability, a verified scope or any other position asserts a
		// fact that authority resolution is a precondition for, so the two
		// together describe a state that cannot have happened -- and answering
		// it with "resolve authority" would quietly discard the downstream fact.
		if d.lifecyclePositions() > 0 {
			return Blocked
		}
		return ResolveAuthority
	}
	switch {
	case d.Refused:
		return None
	case d.ScopeVerified:
		return RecordResultTransition
	case d.MechanicalRepairRequired:
		return MechanicalRepair
	case d.ChangeObserved:
		return VerifyScope
	case d.CapabilityAvailable:
		return ConsumeCapability
	case d.CapabilityConsumed:
		return VerifyAdmission
	case d.ReadyForAdmission:
		return DecideAdmission
	default:
		// A state this owner does not name. NOT admission: "some status was
		// set" is not evidence that admission is the next step, and answering
		// an unrecognised state with an instruction is how a task reaches an
		// operation nobody decided it should reach.
		return Blocked
	}
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
