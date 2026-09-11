// SPDX-License-Identifier: AGPL-3.0-only

package lifecycleaction

import "testing"

// The closed lifecycle/action mapping, enumerated. A state that is not listed
// here has no ratified action, and the owner must answer Blocked for it -- NOT
// Unavailable, which tells the caller to keep its own behaviour -- rather than
// letting it fall through to admission, mutation or completion.
func TestTheClosedLifecycleActionMapping(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   Disposition
		want Action
	}{
		{"waiting_governance", Disposition{TypedProtocol: true, Status: "waiting_governance"}, ResolveAuthority},
		{"ready_for_admission", Disposition{TypedProtocol: true, AuthorityResolved: true, Status: "ready_for_admission", ReadyForAdmission: true}, DecideAdmission},
		{"refused", Disposition{TypedProtocol: true, AuthorityResolved: true, Status: "refused", Refused: true}, None},
		{"waiting_mechanical_repair", Disposition{TypedProtocol: true, AuthorityResolved: true, Status: "waiting_mechanical_repair", MechanicalRepairRequired: true}, MechanicalRepair},
		{"admitted / capability available", Disposition{TypedProtocol: true, AuthorityResolved: true, Status: "ready_for_mutation", CapabilityAvailable: true}, ConsumeCapability},
		{"admitted / capability consumed", Disposition{TypedProtocol: true, AuthorityResolved: true, Status: "admitted", CapabilityConsumed: true}, VerifyAdmission},
		{"mutation_observed", Disposition{TypedProtocol: true, AuthorityResolved: true, Status: "mutation_observed", ChangeObserved: true}, VerifyScope},
		{"scope_verified", Disposition{TypedProtocol: true, AuthorityResolved: true, Status: "scope_verified", ScopeVerified: true}, RecordResultTransition},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Select(tc.in); got != tc.want {
				t.Fatalf("Select = %q, want %q", got, tc.want)
			}
		})
	}
}

// Refused is a disposition, not something to execute. It must never become
// completion or any mutation action.
func TestRefusedIsNoneAndNeverCompletionOrMutation(t *testing.T) {
	got := Select(Disposition{TypedProtocol: true, AuthorityResolved: true, Status: "refused", Refused: true})
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
	typedAwaiting := Select(Disposition{TypedProtocol: true, Status: "waiting_governance"})
	notTyped := Select(Disposition{TypedProtocol: false, Status: "waiting_governance"})
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
	typedAwaiting := Disposition{TypedProtocol: true, AuthorityResolved: false, Status: "waiting_governance"}
	notTyped := Disposition{TypedProtocol: false, AuthorityResolved: false, Status: "waiting_governance"}
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
	set := []func(*Disposition){
		func(d *Disposition) { d.Refused = true },
		func(d *Disposition) { d.ScopeVerified = true },
		func(d *Disposition) { d.MechanicalRepairRequired = true },
		func(d *Disposition) { d.ChangeObserved = true },
		func(d *Disposition) { d.CapabilityAvailable = true },
		func(d *Disposition) { d.CapabilityConsumed = true },
		func(d *Disposition) { d.ReadyForAdmission = true },
	}
	for i := range set {
		for j := range set {
			if i >= j {
				continue
			}
			d := Disposition{TypedProtocol: true, AuthorityResolved: true}
			set[i](&d)
			set[j](&d)
			if got := Select(d); got != Blocked {
				t.Fatalf("positions %d+%d selected %q, want %q", i, j, got, Blocked)
			}
		}
	}
}

// Authority absent is coherent with nothing downstream of it. Each of the seven
// positions is tested individually against absent authority, with the genuine
// typed-but-awaiting-authority case as the positive control.
func TestAbsentAuthorityWithAnyDownstreamPositionIsBlocked(t *testing.T) {
	positions := map[string]func(*Disposition){
		"refused":             func(d *Disposition) { d.Refused = true },
		"scope verified":      func(d *Disposition) { d.ScopeVerified = true },
		"mechanical repair":   func(d *Disposition) { d.MechanicalRepairRequired = true },
		"change observed":     func(d *Disposition) { d.ChangeObserved = true },
		"capability avail":    func(d *Disposition) { d.CapabilityAvailable = true },
		"capability consumed": func(d *Disposition) { d.CapabilityConsumed = true },
		"ready for admission": func(d *Disposition) { d.ReadyForAdmission = true },
	}
	for name, apply := range positions {
		t.Run(name, func(t *testing.T) {
			d := Disposition{TypedProtocol: true, AuthorityResolved: false}
			apply(&d)
			got := Select(d)
			if got == ResolveAuthority {
				t.Fatalf("%s with absent authority selected %q, silently discarding a fact authority is a precondition for", name, got)
			}
			if got != Blocked {
				t.Fatalf("%s with absent authority selected %q, want %q", name, got, Blocked)
			}
			// Exact outcome asserted above.
		})
	}

	// POSITIVE CONTROL: genuine typed-but-awaiting-authority, nothing downstream.
	clean := Disposition{TypedProtocol: true, AuthorityResolved: false}
	if got := Select(clean); got != ResolveAuthority {
		t.Fatalf("typed-but-awaiting-authority selected %q, want %q", got, ResolveAuthority)
	}
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
