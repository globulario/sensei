// SPDX-License-Identifier: AGPL-3.0-only

package lifecycleaction

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// positionRows pairs each lifecycle position with the phase and status
// foldGovernance records beside it -- the LEGITIMATE tuple for that position.
//
// Tests that set a position flag on an otherwise empty disposition now reach
// Blocked through the closed mapping's miss, which would let a gate they mean to
// prove be removed without any failure. Building from the legitimate tuple makes
// the thing under test the only thing wrong.
var positionRows = []struct {
	name   string
	apply  func(*Disposition)
	phase  closureprotocol.TaskPhase
	status string
}{
	{"refused", func(d *Disposition) { d.Refused = true }, closureprotocol.PhaseRefused, StatusRefused},
	{"scope verified", func(d *Disposition) { d.ScopeVerified = true }, closureprotocol.PhaseScopeVerified, StatusScopeVerified},
	{"mechanical repair", func(d *Disposition) { d.MechanicalRepairRequired = true }, closureprotocol.PhaseWaitingMechanicalRepair, StatusWaitingMechanical},
	{"change observed", func(d *Disposition) { d.ChangeObserved = true }, closureprotocol.PhaseMutationObserved, StatusMutationObserved},
	{"capability available", func(d *Disposition) { d.CapabilityAvailable = true }, closureprotocol.PhaseAdmitted, StatusReadyForMutation},
	{"capability consumed", func(d *Disposition) { d.CapabilityConsumed = true }, closureprotocol.PhaseAdmitted, StatusAdmitted},
	{"ready for admission", func(d *Disposition) { d.ReadyForAdmission = true }, closureprotocol.PhaseReadyForAdmission, StatusReadyForAdmission},
}

// The closed lifecycle/action mapping, enumerated. A state that is not listed
// here has no ratified action, and the owner must answer Blocked for it -- NOT
// Unavailable, which tells the caller to keep its own behaviour -- rather than
// letting it fall through to admission, mutation or completion.
func TestTheClosedLifecycleActionMapping(t *testing.T) {
	seen := map[tuple]bool{}
	for _, tc := range []struct {
		name string
		in   Disposition
		want Action
	}{
		{"waiting_governance", Disposition{TypedProtocol: true, Phase: closureprotocol.PhaseWaitingGovernance, Status: StatusWaitingGovernance}, ResolveAuthority},
		{"ready_for_admission", Disposition{TypedProtocol: true, AuthorityResolved: true, Phase: closureprotocol.PhaseReadyForAdmission, Status: StatusReadyForAdmission, ReadyForAdmission: true}, DecideAdmission},
		{"refused", Disposition{TypedProtocol: true, AuthorityResolved: true, Phase: closureprotocol.PhaseRefused, Status: StatusRefused, Refused: true}, None},
		{"waiting_mechanical_repair", Disposition{TypedProtocol: true, AuthorityResolved: true, Phase: closureprotocol.PhaseWaitingMechanicalRepair, Status: StatusWaitingMechanical, MechanicalRepairRequired: true}, MechanicalRepair},
		{"admitted / capability available", Disposition{TypedProtocol: true, AuthorityResolved: true, Phase: closureprotocol.PhaseAdmitted, Status: StatusReadyForMutation, CapabilityAvailable: true}, ConsumeCapability},
		{"admitted / capability consumed", Disposition{TypedProtocol: true, AuthorityResolved: true, Phase: closureprotocol.PhaseAdmitted, Status: StatusAdmitted, CapabilityConsumed: true}, VerifyAdmission},
		{"mutation_observed", Disposition{TypedProtocol: true, AuthorityResolved: true, Phase: closureprotocol.PhaseMutationObserved, Status: StatusMutationObserved, ChangeObserved: true}, VerifyScope},
		{"scope_verified", Disposition{TypedProtocol: true, AuthorityResolved: true, Phase: closureprotocol.PhaseScopeVerified, Status: StatusScopeVerified, ScopeVerified: true}, RecordResultTransition},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Select(tc.in); got != tc.want {
				t.Fatalf("Select = %q, want %q", got, tc.want)
			}
			seen[tc.in.key()] = true
		})
	}
	// CLOSED IN BOTH DIRECTIONS. A row added to the mapping and not to this
	// enumeration is an action nothing here ever exercised; a case listed here
	// whose tuple is absent from the mapping would pass by reaching Blocked for
	// the wrong reason.
	if len(seen) != len(closedMapping) {
		t.Fatalf("enumerated %d tuples, the mapping holds %d", len(seen), len(closedMapping))
	}
	for k := range closedMapping {
		if !seen[k] {
			t.Fatalf("mapping row %+v is not enumerated by this test", k)
		}
	}
}

// Refused is a disposition, not something to execute. It must never become
// completion or any mutation action.
func TestRefusedIsNoneAndNeverCompletionOrMutation(t *testing.T) {
	got := Select(Disposition{TypedProtocol: true, AuthorityResolved: true, Phase: closureprotocol.PhaseRefused, Status: StatusRefused, Refused: true})
	if got != None {
		t.Fatalf("refused selected %q, want %q", got, None)
	}
	if got != None {
		t.Fatalf("refused selected %q; only the exact outcome proves no work was requested", got)
	}
}

// Unknown or inconsistent input must fail closed.
func TestUnknownOrInconsistentStateFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   Disposition
	}{
		{"authority resolved, nothing else known", Disposition{TypedProtocol: true, AuthorityResolved: true}},
		{"capability both available and consumed", Disposition{TypedProtocol: true, AuthorityResolved: true, Status: "admitted", CapabilityAvailable: true, CapabilityConsumed: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Select(tc.in)
			// BLOCKED, not Unavailable: this owner governs the task and its state
			// is unsafe. Unavailable would invite an adapter to keep doing
			// whatever it was doing.
			if got != Blocked {
				t.Fatalf("Select = %q, want %q", got, Blocked)
			}
			// NOT !Advances(): MechanicalRepair, ResolveAuthority and
			// DecideAdmission all return false while still requesting work, so
			// that predicate cannot prove "no action permitted".
		})
	}
}

// Typed-protocol membership is not inferred from authority resolution: a typed
// task awaiting authority must not read as a legacy file-protocol task.
func TestTypedProtocolIsSeparateFromAuthorityResolved(t *testing.T) {
	typedAwaiting := Select(Disposition{TypedProtocol: true, Phase: closureprotocol.PhaseWaitingGovernance, Status: StatusWaitingGovernance})
	notTyped := Select(Disposition{TypedProtocol: false, Phase: closureprotocol.PhaseWaitingGovernance, Status: StatusWaitingGovernance})
	if typedAwaiting == notTyped {
		t.Fatalf("a typed task awaiting authority and a non-typed task both selected %q; the two claims must be distinguishable", typedAwaiting)
	}
	if typedAwaiting != ResolveAuthority || notTyped != Unavailable {
		t.Fatalf("typed-awaiting=%q non-typed=%q, want %q and %q", typedAwaiting, notTyped, ResolveAuthority, Unavailable)
	}
}

// A grant flag disagreeing with its accompanying status describes a state
// production cannot produce. The owner refuses rather than believing whichever
// field looks more authoritative.
func TestContradictoryGrantInputsFailClosed(t *testing.T) {
	for _, name := range []string{"status says ready, grant false", "grant true, status not ready"} {
		t.Run(name, func(t *testing.T) {
			got := Select(Disposition{
				TypedProtocol: true, AuthorityResolved: true,
				Status: "ready_for_mutation", GrantInconsistent: true,
			})
			if got != Blocked {
				t.Fatalf("contradictory grant inputs selected %q, want %q", got, Blocked)
			}
			if got.Advances() {
				t.Fatalf("contradictory grant inputs selected an advancing action (%q)", got)
			}
		})
	}
}

// TypedProtocol must be an INPUT, not a function of AuthorityResolved. If it
// were derived, these two would be indistinguishable.
func TestTypedProtocolIsNotAFunctionOfAuthorityResolved(t *testing.T) {
	typedAwaiting := Disposition{TypedProtocol: true, AuthorityResolved: false, Phase: closureprotocol.PhaseWaitingGovernance, Status: StatusWaitingGovernance}
	notTyped := Disposition{TypedProtocol: false, AuthorityResolved: false, Phase: closureprotocol.PhaseWaitingGovernance, Status: StatusWaitingGovernance}
	if Select(typedAwaiting) == Select(notTyped) {
		t.Fatal("the two dispositions differ only in TypedProtocol and selected the same action; the field carries no information")
	}
	if Select(typedAwaiting) != ResolveAuthority {
		t.Fatalf("typed task awaiting authority selected %q, want %q", Select(typedAwaiting), ResolveAuthority)
	}
	if Select(notTyped) != Unavailable {
		t.Fatalf("non-typed task selected %q, want %q", Select(notTyped), Unavailable)
	}
}

// The two non-action outcomes must stay distinguishable: one permits the
// caller's own compatibility behaviour, the other forbids acting at all.
func TestUnavailableAndBlockedAreDistinct(t *testing.T) {
	notOurs := Select(Disposition{TypedProtocol: false})
	unsafe := Select(Disposition{TypedProtocol: true, AuthorityResolved: true, GrantInconsistent: true})
	if notOurs == unsafe {
		t.Fatalf("both outcomes are %q; an adapter cannot tell \"not my task\" from \"unsafe to act\"", notOurs)
	}
	if notOurs != Unavailable || unsafe != Blocked {
		t.Fatalf("outcomes were %q and %q, want %q and %q", notOurs, unsafe, Unavailable, Blocked)
	}
}

// A NONEMPTY unknown status must not select admission. Testing only the zero
// value misses this: the first version returned DecideAdmission whenever any
// status or phase was set.
func TestUnknownNonemptyStatusIsBlockedNotAdmission(t *testing.T) {
	for _, st := range []string{"unexpected", "waiting_evidence", "completed", "whatever"} {
		t.Run(st, func(t *testing.T) {
			got := Select(Disposition{TypedProtocol: true, AuthorityResolved: true, Status: st})
			if got == DecideAdmission {
				t.Fatalf("unknown status %q selected admission; a status being set is not evidence admission is next", st)
			}
			if got != Blocked {
				t.Fatalf("unknown status %q selected %q, want %q", st, got, Blocked)
			}
		})
	}
}

// A contradiction must be rejected BEFORE any action is selected, including when
// it is paired with a position an earlier branch would have answered.
func TestContradictionIsRejectedBeforeEarlierBranches(t *testing.T) {
	base := func() Disposition {
		return Disposition{
			TypedProtocol: true, AuthorityResolved: true,
			Phase: closureprotocol.PhaseAdmitted, Status: StatusReadyForMutation,
			CapabilityAvailable: true, CapabilityConsumed: true, // the contradiction
		}
	}
	for _, tc := range []struct {
		name  string
		apply func(*Disposition)
	}{
		{"with scope_verified", func(d *Disposition) { d.ScopeVerified = true }},
		{"with mechanical repair", func(d *Disposition) { d.MechanicalRepairRequired = true }},
		{"with change observed", func(d *Disposition) { d.ChangeObserved = true }},
		{"with refused", func(d *Disposition) { d.Refused = true }},
		{"with ready for admission", func(d *Disposition) { d.ReadyForAdmission = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := base()
			tc.apply(&d)
			got := Select(d)
			if got != Blocked {
				t.Fatalf("a contradictory disposition %s selected %q, want %q", tc.name, got, Blocked)
			}
			// Exact outcome asserted above; !Advances() alone would not prove it.
		})
	}
}

// Two mutually exclusive positions are a contradiction whichever pair they are.
func TestAnyTwoPositionsAreAContradiction(t *testing.T) {
	for i := range positionRows {
		for j := range positionRows {
			if i >= j {
				continue
			}
			// The tuple is row i's own, so the SECOND position is the only
			// defect: this proves the contradiction gate, not a mapping miss.
			d := Disposition{TypedProtocol: true, AuthorityResolved: true, Phase: positionRows[i].phase, Status: positionRows[i].status}
			positionRows[i].apply(&d)
			positionRows[j].apply(&d)
			if got := Select(d); got != Blocked {
				t.Fatalf("positions %s+%s selected %q, want %q", positionRows[i].name, positionRows[j].name, got, Blocked)
			}
		}
	}
}

// Authority absent is coherent with nothing downstream of it. Each of the seven
// positions is tested individually against absent authority, with the genuine
// typed-but-awaiting-authority case as the positive control.
func TestAbsentAuthorityWithAnyDownstreamPositionIsBlocked(t *testing.T) {
	for _, row := range positionRows {
		t.Run(row.name, func(t *testing.T) {
			// Every field is the one production records for this position; only
			// the authority is missing. Nothing else can be what blocks it.
			d := Disposition{TypedProtocol: true, AuthorityResolved: false, Phase: row.phase, Status: row.status}
			row.apply(&d)
			got := Select(d)
			if got == ResolveAuthority {
				t.Fatalf("%s with absent authority selected %q, silently discarding a fact authority is a precondition for", row.name, got)
			}
			if got != Blocked {
				t.Fatalf("%s with absent authority selected %q, want %q", row.name, got, Blocked)
			}
		})
	}

	// POSITIVE CONTROL: genuine typed-but-awaiting-authority, nothing downstream.
	clean := Disposition{TypedProtocol: true, AuthorityResolved: false, Phase: closureprotocol.PhaseWaitingGovernance, Status: StatusWaitingGovernance}
	if got := Select(clean); got != ResolveAuthority {
		t.Fatalf("typed-but-awaiting-authority selected %q, want %q", got, ResolveAuthority)
	}
}

// F1 (review 3985530094). A downstream position on a disposition that claims the
// typed protocol does NOT govern it is a contradiction, and a contradiction is
// nobody's to keep acting in.
//
// Unavailable was returned first, so such a disposition reached the caller as
// compatibility-safe and applyGovernedDisposition could preserve a historical
// instruction -- including a stale "perform admitted edit" -- for a state whose
// own evidence disagrees with itself. TypedProtocol is asserted by the caller and
// verified by nobody; a false assertion earns no shortcut past the gates.
func TestDownstreamPositionIsRejectedBeforeUnavailable(t *testing.T) {
	for _, row := range positionRows {
		t.Run(row.name, func(t *testing.T) {
			d := Disposition{TypedProtocol: false, AuthorityResolved: false, Phase: row.phase, Status: row.status}
			row.apply(&d)
			got := Select(d)
			if got == Unavailable {
				t.Fatalf("%s with TypedProtocol=false selected %q; an adapter reads that as \"keep your own behaviour\" and can restore a historical mutation instruction", row.name, got)
			}
			if got != Blocked {
				t.Fatalf("%s with TypedProtocol=false selected %q, want %q", row.name, got, Blocked)
			}
		})
	}

	// Typed authority established IS the assertion that the typed protocol
	// governs. Denying it while claiming the authority is the same contradiction.
	if got := Select(Disposition{TypedProtocol: false, AuthorityResolved: true}); got != Blocked {
		t.Fatalf("TypedProtocol=false with authority resolved selected %q, want %q", got, Blocked)
	}

	// POSITIVE CONTROLS. A coherent non-typed disposition is still the caller's
	// own: this repair must not turn the file protocol into Blocked.
	for _, tc := range []struct {
		name string
		in   Disposition
	}{
		{"zero value", Disposition{}},
		{"awaiting governance", Disposition{TypedProtocol: false, Phase: closureprotocol.PhaseWaitingGovernance, Status: StatusWaitingGovernance}},
	} {
		if got := Select(tc.in); got != Unavailable {
			t.Fatalf("%s selected %q, want %q; the file protocol must keep its own behaviour", tc.name, got, Unavailable)
		}
	}
}

// F2 (review 3985530099). A position is not a Boolean: it is one component of a
// tuple that must agree with the phase and status recorded beside it.
//
// Counting positions and then switching on the flag selected an operation from
// half a state while the other half described something else -- or nothing the
// owner names at all.
func TestPositionMustAgreeWithPhaseAndStatus(t *testing.T) {
	// The reviewer's two exact cases.
	t.Run("capability available with an unknown status", func(t *testing.T) {
		got := Select(Disposition{TypedProtocol: true, AuthorityResolved: true, Phase: closureprotocol.PhaseAdmitted, Status: "unexpected", CapabilityAvailable: true})
		if got == ConsumeCapability {
			t.Fatal("an unknown status beside an available capability selected consume_capability; the status is half the state and it names nothing")
		}
		if got != Blocked {
			t.Fatalf("selected %q, want %q", got, Blocked)
		}
	})
	t.Run("scope verified with a mutation status", func(t *testing.T) {
		got := Select(Disposition{TypedProtocol: true, AuthorityResolved: true, Phase: closureprotocol.PhaseScopeVerified, Status: StatusReadyForMutation, ScopeVerified: true})
		if got == RecordResultTransition {
			t.Fatal("a ready_for_mutation status beside a verified scope selected record_result_transition; the two records describe different states")
		}
		if got != Blocked {
			t.Fatalf("selected %q, want %q", got, Blocked)
		}
	})

	// SWEEP. Every legitimate row, with each of the other rows' statuses and
	// phases substituted in turn, and with an unnamed value.
	for _, row := range positionRows {
		base := Disposition{TypedProtocol: true, AuthorityResolved: true, Phase: row.phase, Status: row.status}
		row.apply(&base)
		if Select(base) == Blocked {
			t.Fatalf("%s: the legitimate tuple is Blocked; this sweep would prove nothing", row.name)
		}
		for _, other := range append([]string{"unexpected", ""}, allStatuses()...) {
			if other == row.status {
				continue
			}
			d := base
			d.Status = other
			if got := Select(d); got != Blocked {
				t.Fatalf("%s with status %q selected %q, want %q", row.name, other, got, Blocked)
			}
		}
		for _, other := range append([]closureprotocol.TaskPhase{"unexpected", "", closureprotocol.PhaseCompleted, closureprotocol.PhaseCertified}, allPhases()...) {
			if other == row.phase {
				continue
			}
			d := base
			d.Phase = other
			if got := Select(d); got != Blocked {
				t.Fatalf("%s with phase %q selected %q, want %q", row.name, other, got, Blocked)
			}
		}
	}
}

func allStatuses() []string {
	out := []string{}
	for k := range closedMapping {
		out = append(out, k.Status)
	}
	return out
}

func allPhases() []closureprotocol.TaskPhase {
	out := []closureprotocol.TaskPhase{}
	for k := range closedMapping {
		out = append(out, k.Phase)
	}
	return out
}

// Advances() is NOT a safety predicate: three actions that request real work
// return false. Pinned so a future test cannot mistake it for one.
func TestAdvancesIsNotASafetyPredicate(t *testing.T) {
	for _, a := range []Action{MechanicalRepair, ResolveAuthority, DecideAdmission} {
		if a.Advances() {
			t.Fatalf("%q reported that it advances", a)
		}
		if a == None || a == Blocked || a == Unavailable {
			t.Fatalf("%q is a non-action outcome", a)
		}
	}
	// The genuine non-action outcomes are exactly these three.
	for _, a := range []Action{None, Blocked, Unavailable} {
		if a.Advances() {
			t.Fatalf("%q reported that it advances", a)
		}
	}
}
