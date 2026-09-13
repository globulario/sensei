// SPDX-License-Identifier: AGPL-3.0-only

package derive

// T1: construction-site confinement.
//
// Forced by a measured gap, not invented. sensei-code's W3 planning reported
// internal/ghbridge/exchange.go as unexamined, and the recipe aimed at it asks
// state_mutation_confined_to_owner about ExchangeRecord.Deadline. Run at HEAD that
// derivation correctly answers "no write to ExchangeRecord.Deadline found … nothing to
// establish": the field is set ONLY in composite literals at construction
// (architecture_runner.go:86, runner.go:138) and never mutated afterwards.
//
// The analyzer is right and must not be weakened. The missing thing is a different
// species of fact: not "who may CHANGE this field" but "who may CREATE a value carrying
// it". That is the question an authority-bearing field actually raises — a deadline a
// restarted waiter must honour is meaningless if any package can mint a record with one.
//
// The family is general and names no type: every observable construction of a named
// exported struct type, within a named repository scope, originates from the package
// that declares it. With a Field named, only constructions that INITIALIZE that field
// are considered, which is the authority-bearing-field case.

import (
	"strings"
	"testing"
)

const ctorGoMod = "module example.com/m\n\ngo 1.22\n"

// The owner declares the type and constructs it in its own constructor.
const ctorOwnerPkg = `package exchange

import "time"

type Record struct {
	TaskID   string
	Deadline time.Time
}

func Open(id string, d time.Duration) *Record {
	return &Record{TaskID: id, Deadline: time.Now().Add(d)}
}

func Reopen(id string) *Record {
	r := Record{TaskID: id, Deadline: time.Now()}
	return &r
}
`

// A bystander that uses the owner's constructor but never constructs the type itself.
const ctorBystander = `package client

import "example.com/m/exchange"

func Build() *exchange.Record { return exchange.Open("t1", 0) }
`

// A package outside the owner minting a record with a deadline of its own.
const ctorOutsider = `package transport

import (
	"time"

	"example.com/m/exchange"
)

func Forge(id string) *exchange.Record {
	return &exchange.Record{TaskID: id, Deadline: time.Now().Add(time.Hour)}
}
`

// ctorProp names only the directories the fixture actually creates: parseScope refuses a
// search path that is absent at the pinned commit, which is correct — a claim over a
// directory that does not exist has not been checked — and naming three when the fixture
// writes two is a fixture defect, not an analyzer one.
func ctorProp(field string, paths ...string) Proposition {
	return Proposition{
		Kind:        KindConstructionConfinedToOwner,
		Dir:         "exchange",
		Type:        "Record",
		Field:       field,
		SearchPaths: paths,
	}
}

// 1. POSITIVE: every construction is in the owning package.
func TestConstructionConfined_DerivedWhenOnlyTheOwnerConstructs(t *testing.T) {
	src := pinned(t, map[string]string{
		"go.mod":               ctorGoMod,
		"exchange/exchange.go": ctorOwnerPkg,
		"client/client.go":     ctorBystander,
	})
	paths := []string{"exchange", "client"}
	got, _ := Derive(src, ctorProp("Deadline", paths...), at("2026-09-13T12:00:00Z"))
	if got.Outcome != Derived {
		t.Fatalf("outcome=%s, want DERIVED: %s", got.Outcome, got.Detail)
	}
	// The subjects must name the construction sites, because that is what a later
	// coverage consumer anchors on.
	joined := strings.Join(got.SubjectFiles(), " ")
	if !strings.Contains(joined, "exchange/exchange.go") {
		t.Errorf("the owner's construction sites are not among the subjects: %s", joined)
	}
	if strings.Contains(joined, "client/client.go") {
		t.Errorf("a package that only CALLS the constructor was reported as a construction site: %s", joined)
	}
}

// 2. NEGATIVE: a construction outside the owner refutes confinement.
func TestConstructionConfined_RefutedWhenAnOutsiderConstructs(t *testing.T) {
	src := pinned(t, map[string]string{
		"go.mod":                 ctorGoMod,
		"exchange/exchange.go":   ctorOwnerPkg,
		"transport/transport.go": ctorOutsider,
	})
	paths := []string{"exchange", "transport"}
	got, _ := Derive(src, ctorProp("Deadline", paths...), at("2026-09-13T12:00:00Z"))
	if got.Outcome != Refuted {
		t.Fatalf("outcome=%s, want REFUTED: %s", got.Outcome, got.Detail)
	}
	if !strings.Contains(got.Detail, "transport") {
		t.Errorf("the refutation does not name where the outside construction is: %s", got.Detail)
	}
}

// 3. A MUTATION must not satisfy a construction rule. The two families ask different
// questions, and a write after construction is not a construction.
func TestConstructionConfined_AMutationIsNotAConstruction(t *testing.T) {
	mutator := `package transport

import (
	"time"

	"example.com/m/exchange"
)

// Mutates an existing record; constructs nothing.
func Extend(r *exchange.Record) { r.Deadline = time.Now().Add(time.Hour) }
`
	src := pinned(t, map[string]string{
		"go.mod":                 ctorGoMod,
		"exchange/exchange.go":   ctorOwnerPkg,
		"transport/transport.go": mutator,
	})
	paths := []string{"exchange", "transport"}
	got, _ := Derive(src, ctorProp("Deadline", paths...), at("2026-09-13T12:00:00Z"))
	if got.Outcome != Derived {
		t.Fatalf("outcome=%s, want DERIVED (a mutation is not a construction): %s", got.Outcome, got.Detail)
	}
	if strings.Contains(strings.Join(got.SubjectFiles(), " "), "transport") {
		t.Errorf("a mutation site was reported as a construction subject: %s", strings.Join(got.SubjectFiles(), " "))
	}
}

// 4. An unrelated type with the same field name must not match.
func TestConstructionConfined_AnUnrelatedTypeWithTheSameFieldDoesNotMatch(t *testing.T) {
	lookalike := `package transport

import "time"

// A DIFFERENT type that happens to have a Deadline field.
type Record struct {
	Deadline time.Time
}

func Make() *Record { return &Record{Deadline: time.Now()} }
`
	src := pinned(t, map[string]string{
		"go.mod":                 ctorGoMod,
		"exchange/exchange.go":   ctorOwnerPkg,
		"transport/transport.go": lookalike,
	})
	paths := []string{"exchange", "transport"}
	got, _ := Derive(src, ctorProp("Deadline", paths...), at("2026-09-13T12:00:00Z"))
	if got.Outcome != Derived {
		t.Fatalf("outcome=%s, want DERIVED — transport.Record is a different type: %s", got.Outcome, got.Detail)
	}
	if strings.Contains(strings.Join(got.SubjectFiles(), " "), "transport") {
		t.Errorf("a same-named field on an unrelated type was counted: %s", strings.Join(got.SubjectFiles(), " "))
	}
}

// 5. VACUITY: zero construction sites must not read as proof. There is no vacuous truth
// here, and that choice is the same one the mutation family already makes — "nothing to
// establish" rather than "confined".
func TestConstructionConfined_ZeroSitesIsNotEvidence(t *testing.T) {
	noCtor := `package exchange

import "time"

type Record struct {
	TaskID   string
	Deadline time.Time
}
`
	src := pinned(t, map[string]string{"go.mod": ctorGoMod, "exchange/exchange.go": noCtor})
	paths := []string{"exchange"}
	got, _ := Derive(src, ctorProp("Deadline", paths...), at("2026-09-13T12:00:00Z"))
	if got.Outcome == Derived {
		t.Fatalf("zero construction sites produced DERIVED: %s", got.Detail)
	}
	if !strings.Contains(got.Detail, "nothing to establish") {
		t.Errorf("the outcome does not say why it establishes nothing: %s", got.Detail)
	}
}

// With no Field named the family asks about the type itself, which is the general form.
func TestConstructionConfined_WithoutAFieldItAsksAboutTheType(t *testing.T) {
	src := pinned(t, map[string]string{
		"go.mod":                 ctorGoMod,
		"exchange/exchange.go":   ctorOwnerPkg,
		"transport/transport.go": ctorOutsider,
	})
	got, _ := Derive(src, ctorProp("", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
	if got.Outcome != Refuted {
		t.Fatalf("outcome=%s, want REFUTED for an outside construction of the type: %s", got.Outcome, got.Detail)
	}
}

// A field named but never initialized at any construction site: the constructions exist,
// none touches the field, so there is nothing about THAT field to establish.
func TestConstructionConfined_AFieldNoConstructionInitializesEstablishesNothing(t *testing.T) {
	src := pinned(t, map[string]string{
		"go.mod":               ctorGoMod,
		"exchange/exchange.go": ctorOwnerPkg,
	})
	got, _ := Derive(src, ctorProp("NoSuchField", "exchange"), at("2026-09-13T12:00:00Z"))
	if got.Outcome == Derived {
		t.Fatalf("a field no construction initializes produced DERIVED: %s", got.Detail)
	}
	if !strings.Contains(got.Detail, "nothing to establish") {
		t.Errorf("detail does not explain the emptiness: %s", got.Detail)
	}
}

// The DECLARATION is a subject too, and this is what makes the evidence usable for a plan
// that edits the declaring file.
//
// Found by running the family against the real case: the construction sites are
// architecture_runner.go and runner.go, so a claim whose subjects were only those would
// establish nothing about exchange.go — the file W3 actually plans to touch, and the file
// whose contents give every construction site its meaning. A receipt that omitted it
// would also be invalidated by edits it never named.
func TestConstructionConfined_TheDeclarationIsASubject(t *testing.T) {
	src := pinned(t, map[string]string{
		"go.mod":               ctorGoMod,
		"exchange/exchange.go": ctorOwnerPkg,
		"client/client.go":     ctorBystander,
	})
	got, _ := Derive(src, ctorProp("Deadline", "exchange", "client"), at("2026-09-13T12:00:00Z"))
	if got.Outcome != Derived {
		t.Fatalf("outcome=%s: %s", got.Outcome, got.Detail)
	}
	var roles []string
	declared := false
	for _, s := range got.Subjects {
		roles = append(roles, s.Role)
		if s.Role == "declaration-site" {
			declared = true
			if s.File != "exchange/exchange.go" {
				t.Errorf("the declaration subject names %s", s.File)
			}
			if s.Entity != "Record" {
				t.Errorf("the declaration subject entity is %q, want the bare type", s.Entity)
			}
		}
	}
	if !declared {
		t.Errorf("no declaration-site subject; roles were %v", roles)
	}
	// It must not inflate the confinement arithmetic.
	if !strings.Contains(got.Detail, "all 2 observable construction(s)") {
		t.Errorf("the declaration was counted as a construction: %s", got.Detail)
	}
}

// A declaration outside the owning directory is not the owner's declaration. Guards the
// loop from attributing someone else's same-named type.
func TestConstructionConfined_OnlyTheOwnersDeclarationIsASubject(t *testing.T) {
	lookalike := `package transport

import "time"

type Record struct{ Deadline time.Time }
`
	src := pinned(t, map[string]string{
		"go.mod":                 ctorGoMod,
		"exchange/exchange.go":   ctorOwnerPkg,
		"transport/transport.go": lookalike,
	})
	got, _ := Derive(src, ctorProp("Deadline", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
	for _, s := range got.Subjects {
		if s.Role == "declaration-site" && strings.Contains(s.File, "transport") {
			t.Errorf("an unrelated package's same-named declaration became a subject: %+v", s)
		}
	}
}

// An UNKEYED positional construction is the completeness boundary, not a pass.
//
// A mutation survived until this existed, and the hole it showed was real: if an unkeyed
// literal were read as "does not initialize the field", an outsider could mint a record
// positionally and escape the claim entirely. The package's own discipline for an
// unbindable site is UNRESOLVED, named in the detail — never silently not-this-type.
func TestConstructionConfined_AnUnkeyedLiteralIsUnresolvedNotIgnored(t *testing.T) {
	positional := `package transport

import (
	"time"

	"example.com/m/exchange"
)

// Positional: no field names at all.
func Forge() *exchange.Record { return &exchange.Record{"t9", time.Now()} }
`
	src := pinned(t, map[string]string{
		"go.mod":                 ctorGoMod,
		"exchange/exchange.go":   ctorOwnerPkg,
		"transport/transport.go": positional,
	})
	got, _ := Derive(src, ctorProp("Deadline", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
	if got.Outcome == Derived {
		t.Fatalf("an unkeyed outside construction was silently excluded, establishing confinement: %s", got.Detail)
	}
	if got.Outcome != Unresolved {
		t.Fatalf("outcome=%s, want UNRESOLVED: %s", got.Outcome, got.Detail)
	}
	if !strings.Contains(got.Detail, "unkeyed") || !strings.Contains(got.Detail, "transport") {
		t.Errorf("the detail does not name the unreadable site: %s", got.Detail)
	}
}

// A KEYED counterexample outranks an unreadable one: a proven violation is a stronger
// fact than an unexamined site.
func TestConstructionConfined_ACounterexampleOutranksAnUnreadableSite(t *testing.T) {
	both := `package transport

import (
	"time"

	"example.com/m/exchange"
)

func Positional() *exchange.Record { return &exchange.Record{"t9", time.Now()} }
func Keyed() *exchange.Record      { return &exchange.Record{TaskID: "t9", Deadline: time.Now()} }
`
	src := pinned(t, map[string]string{
		"go.mod":                 ctorGoMod,
		"exchange/exchange.go":   ctorOwnerPkg,
		"transport/transport.go": both,
	})
	got, _ := Derive(src, ctorProp("Deadline", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
	if got.Outcome != Refuted {
		t.Fatalf("outcome=%s, want REFUTED — a proven counterexample outranks an unreadable site: %s", got.Outcome, got.Detail)
	}
}
