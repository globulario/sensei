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
	"fmt"
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

// ---------------------------------------------------------------------------
// The implicit-construction review finding (constructionconfinement.go:168).
//
// The question the reviewer asked was not "is the limit documented" but "can
// DERIVED return while a caller mints an authority-bearing value outside the owner
// through a form this does not observe". It was measured over 23 forms against
// Record.Deadline, and the answer was yes for eight of them, all one shape: a
// composite literal whose TYPE IS ELIDED, which `lit.Type == nil` skipped outright.
// Limits() never claimed that one, so it was a silent bypass rather than a stated
// boundary. The forms below are that measurement, kept as witnesses.
//
// The forms that still DERIVE are witnesses too, and just as important: `var x T`,
// `new(T)`, an empty literal and an embedded zero initialize the field to nothing,
// and what a later write puts there is the THIRD family's question. Counting them
// would have destroyed the distinction T1 exists to make.

// ctorElided builds the fixture for one outside construction form.
func ctorElided(t *testing.T, outsideBody string) *GitSource {
	t.Helper()
	return pinned(t, map[string]string{
		"go.mod":               ctorGoMod,
		"exchange/exchange.go": ctorOwnerPkg,
		"transport/transport.go": "package transport\n\nimport (\n\t\"time\"\n\n\t\"example.com/m/exchange\"\n)\n\nvar _ = time.Now\n\n" +
			outsideBody + "\n",
	})
}

// 12. THE FINDING. Every shape of elided literal is a construction, and an elided
// literal outside the owner refutes confinement exactly as a written-out one does.
func TestConstructionConfined_AnElidedLiteralTypeIsStillAConstruction(t *testing.T) {
	forms := map[string]string{
		"slice element":         `func F() { s := []exchange.Record{{Deadline: time.Now()}}; _ = s }`,
		"pointer slice element": `func F() { s := []*exchange.Record{{Deadline: time.Now()}}; _ = s }`,
		"array element":         `func F() { a := [1]exchange.Record{{Deadline: time.Now()}}; _ = a }`,
		"map value":             `func F() { m := map[string]exchange.Record{"a": {Deadline: time.Now()}}; _ = m }`,
		"map value, pointer":    `func F() { m := map[string]*exchange.Record{"a": {Deadline: time.Now()}}; _ = m }`,
		"map key":               `func F() { m := map[exchange.Record]bool{{Deadline: time.Now()}: true}; _ = m }`,
		"slice of slice":        `func F() { s := [][]exchange.Record{{{Deadline: time.Now()}}}; _ = s }`,
	}
	for name, body := range forms {
		t.Run(name, func(t *testing.T) {
			got, _ := Derive(ctorElided(t, body), ctorProp("Deadline", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
			if got.Outcome != Refuted {
				t.Fatalf("an outsider minting a Deadline through an elided %s was not a counterexample: outcome=%s: %s",
					name, got.Outcome, got.Detail)
			}
			if !strings.Contains(got.Detail, "transport/transport.go") {
				t.Errorf("the refutation does not name the elided construction site: %s", got.Detail)
			}
		})
	}
}

// 13. An elided literal that is ALSO unkeyed must not become readable by being nested.
// Elision resolves the TYPE; it says nothing about which position is which field, so the
// unkeyed boundary still applies and the answer is UNRESOLVED, not DERIVED and not
// REFUTED.
func TestConstructionConfined_AnElidedUnkeyedLiteralIsStillUnresolved(t *testing.T) {
	got, _ := Derive(ctorElided(t, `func F() { s := []exchange.Record{{"x", time.Now()}}; _ = s }`),
		ctorProp("Deadline", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
	if got.Outcome != Unresolved {
		t.Fatalf("outcome=%s, want UNRESOLVED: %s", got.Outcome, got.Detail)
	}
	if !strings.Contains(got.Detail, "unkeyed") {
		t.Errorf("the unresolved report does not say the literal was positional: %s", got.Detail)
	}
}

// 14. A NAMED collection type declared in scope reveals its element type, so an elided
// element under it is observed.
func TestConstructionConfined_ANamedCollectionTypeInScopeRevealsItsElementType(t *testing.T) {
	got, _ := Derive(ctorElided(t, "type Records []exchange.Record\n\nfunc F() { s := Records{{Deadline: time.Now()}}; _ = s }"),
		ctorProp("Deadline", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
	if got.Outcome != Refuted {
		t.Fatalf("an elided element of a named slice type was not observed: outcome=%s: %s", got.Outcome, got.Detail)
	}
}

// 15. THE COMPLETENESS BOUNDARY, and it fails toward UNRESOLVED rather than silence.
// When the enclosing collection type is declared outside the scope searched, the element
// type is whatever that declaration says and this does not guess. Being unable to read a
// construction must never read as there being none.
func TestConstructionConfined_AnUnresolvableElidedLiteralIsUnresolvedNotIgnored(t *testing.T) {
	src := pinned(t, map[string]string{
		"go.mod":               ctorGoMod,
		"exchange/exchange.go": ctorOwnerPkg,
		"elsewhere/types.go":   "package elsewhere\n\nimport \"example.com/m/exchange\"\n\ntype Records []exchange.Record\n",
		"transport/transport.go": `package transport

import (
	"time"

	"example.com/m/elsewhere"
)

func F() { s := elsewhere.Records{{Deadline: time.Now()}}; _ = s }
`,
	})
	// elsewhere/ is deliberately NOT searched, so Records' element type is unknown here.
	got, _ := Derive(src, ctorProp("Deadline", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
	if got.Outcome != Unresolved {
		t.Fatalf("outcome=%s, want UNRESOLVED: a construction this cannot type was treated as though it were not one: %s",
			got.Outcome, got.Detail)
	}
	if !strings.Contains(got.Detail, "elided") || !strings.Contains(got.Detail, "transport/transport.go") {
		t.Errorf("the unresolved report does not name the unreadable construction: %s", got.Detail)
	}
}

// 16. A type ALIAS denotes the owner's own type, so constructing through one mints an
// owner value wherever it is written.
func TestConstructionConfined_AnAliasConstructsTheOwnersType(t *testing.T) {
	got, _ := Derive(ctorElided(t, "type Alias = exchange.Record\n\nfunc F() { p := Alias{Deadline: time.Now()}; _ = p }"),
		ctorProp("Deadline", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
	if got.Outcome != Refuted {
		t.Fatalf("a construction through an alias of the owner's type was not observed: outcome=%s: %s", got.Outcome, got.Detail)
	}
}

// 17. A DEFINED type is a DIFFERENT type, and following it would be over-refusal. The
// repair must not widen the family to every syntax that mentions the owner.
func TestConstructionConfined_ADefinedTypeIsNotTheOwnersType(t *testing.T) {
	got, _ := Derive(ctorElided(t, "type Named exchange.Record\n\nfunc F() { p := Named{Deadline: time.Now()}; _ = p }"),
		ctorProp("Deadline", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
	if got.Outcome != Derived {
		t.Fatalf("constructing a DIFFERENT named type was read as constructing the owner's: outcome=%s: %s",
			got.Outcome, got.Detail)
	}
}

// 18. THE DISTINCTION T1 EXISTS FOR, kept intact by the repair. A form that creates an
// instance but initializes the field to nothing mints no authority, and what a later
// write puts there belongs to state_mutation_confined_to_owner. Widening construction to
// cover these would collapse the two families into one.
func TestConstructionConfined_AZeroValueFormMintsNoAuthority(t *testing.T) {
	forms := map[string]string{
		"var declaration":                 `func F() { var x exchange.Record; _ = x }`,
		"var then a later write":          `func F() { var x exchange.Record; x.Deadline = time.Now(); _ = x }`,
		"new":                             `func F() { p := new(exchange.Record); _ = p }`,
		"new then a later write":          `func F() { p := new(exchange.Record); p.Deadline = time.Now() }`,
		"empty literal":                   `func F() { p := &exchange.Record{}; _ = p }`,
		"empty then a later write":        `func F() { p := &exchange.Record{}; p.Deadline = time.Now() }`,
		"a literal setting another field": `func F() { p := &exchange.Record{TaskID: "x"}; _ = p }`,
		"embedded zero, promoted write":   "type outer struct{ exchange.Record }\n\nfunc F() { var o outer; o.Deadline = time.Now(); _ = o }",
	}
	for name, body := range forms {
		t.Run(name, func(t *testing.T) {
			got, _ := Derive(ctorElided(t, body), ctorProp("Deadline", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
			if got.Outcome != Derived {
				t.Fatalf("%s initializes Deadline to nothing and must not be a site for the field claim: outcome=%s: %s",
					name, got.Outcome, got.Detail)
			}
		})
	}
}

// 19. "None found" and "none READABLE" are different facts. When nothing readable
// constructed the type but something unreadable did, reporting UNKNOWN "nothing to
// establish" would hide exactly the site this family exists to see.
func TestConstructionConfined_NoReadableSiteIsNotTheSameAsNoSite(t *testing.T) {
	src := pinned(t, map[string]string{
		"go.mod": ctorGoMod,
		// The owner declares the type and constructs nothing.
		"exchange/exchange.go": "package exchange\n\nimport \"time\"\n\ntype Record struct {\n\tTaskID   string\n\tDeadline time.Time\n}\n",
		"elsewhere/types.go":   "package elsewhere\n\nimport \"example.com/m/exchange\"\n\ntype Records []exchange.Record\n",
		"transport/transport.go": `package transport

import (
	"time"

	"example.com/m/elsewhere"
)

func F() { s := elsewhere.Records{{Deadline: time.Now()}}; _ = s }
`,
	})
	got, _ := Derive(src, ctorProp("Deadline", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
	if got.Outcome != Unresolved {
		t.Fatalf("outcome=%s, want UNRESOLVED: %s", got.Outcome, got.Detail)
	}
	if strings.Contains(got.Detail, "nothing to establish") {
		t.Errorf("an unreadable construction was reported as no construction at all: %s", got.Detail)
	}
	if !strings.Contains(got.Detail, "could not be read") {
		t.Errorf("the detail does not say the constructions were unreadable: %s", got.Detail)
	}
}

// ---------------------------------------------------------------------------
// Re-review findings on this head. Three, and the first is the one that matters most
// because it is a claim proven for one proposition SHAPE and applied to another.

// RELEVANCE DEPENDS ON THE PROPOSITION'S SHAPE (constructionconfinement.go:233, P1).
//
// With a Field named, `var x T` and `new(T)` initialize no field, mint no authority, and are
// correctly not sites. WITHOUT a Field the claim is "every construction of T originates in the
// owner", and both ARE constructions of T. My Limits() text justified the exclusion "with a
// Field named" while the analyzer applied it to both shapes.
//
// DOMAIN OF THIS CLAIM: the type-level proposition. The field-level one is unchanged, and the
// witnesses below assert both directions so neither can drift into the other.
func TestConstructionConfined_TypeLevelClaimsCountZeroValueConstructions(t *testing.T) {
	forms := map[string]string{
		"var declaration": `func F() { var x exchange.Record; _ = x }`,
		"new":             `func F() { p := new(exchange.Record); _ = p }`,
		"var inside a block": `func F() {
	if true {
		var x exchange.Record
		_ = x
	}
}`,
	}
	for name, body := range forms {
		t.Run(name, func(t *testing.T) {
			src := ctorElided(t, body)
			// TYPE-LEVEL: no field named.
			got, _ := Derive(src, ctorProp("", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
			if got.Outcome != Refuted {
				t.Errorf("type-level: %s outside the owner is a construction of the type: outcome=%s: %s",
					name, got.Outcome, got.Detail)
			}
			// FIELD-LEVEL: unchanged, because it initializes no field.
			gotField, _ := Derive(src, ctorProp("Deadline", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
			if gotField.Outcome != Derived {
				t.Errorf("field-level: %s mints no Deadline and must not be a site: outcome=%s: %s",
					name, gotField.Outcome, gotField.Detail)
			}
		})
	}
}

// Forms that construct NO T must not become sites even type-level, or the repair is
// over-refusal wearing a fix's clothes.
func TestConstructionConfined_TypeLevelClaimsIgnoreFormsThatConstructNoValue(t *testing.T) {
	forms := map[string]string{
		"nil pointer declaration": `func F() { var p *exchange.Record; _ = p }`,
		"empty slice":             `func F() { var xs []exchange.Record; _ = xs }`,
		"empty map":               `func F() { var m map[string]exchange.Record; _ = m }`,
		"new of a pointer":        `func F() { p := new(*exchange.Record); _ = p }`,
		"new of a slice":          `func F() { p := new([]exchange.Record); _ = p }`,
	}
	for name, body := range forms {
		t.Run(name, func(t *testing.T) {
			got, _ := Derive(ctorElided(t, body), ctorProp("", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
			if got.Outcome != Derived {
				t.Errorf("%s constructs no Record and must not be a site: outcome=%s: %s", name, got.Outcome, got.Detail)
			}
		})
	}
}

// A declaration WITH an initialiser must count once, not twice: the value's own construction is
// the site.
func TestConstructionConfined_AnInitialisedDeclarationIsOneConstruction(t *testing.T) {
	// An EXPLICIT type and an initialiser: the shape where a declaration and its value could
	// both be counted. `var x = T{}` has no explicit type, so it never exercised this.
	got, _ := Derive(ctorElided(t, `func F() { var x exchange.Record = exchange.Record{}; _ = x }`),
		ctorProp("", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
	if got.Outcome != Refuted {
		t.Fatalf("outcome=%s, want REFUTED: %s", got.Outcome, got.Detail)
	}
	// The owner constructs twice; the outsider once. Four would mean the declaration and its
	// literal were both counted.
	if !strings.Contains(got.Detail, "1 of 3") {
		t.Errorf("the construction count suggests one site was counted twice: %s", got.Detail)
	}
}

// DOT IMPORTS (mutationconfinement.go:539, P1). An unqualified name in a file that dot-imports
// the owner denotes the owner's type, and attributing it to the current directory made the
// construction invisible.
func TestConstructionConfined_ADotImportedTypeNameResolvesToTheOwner(t *testing.T) {
	src := pinned(t, map[string]string{
		"go.mod":               ctorGoMod,
		"exchange/exchange.go": ctorOwnerPkg,
		"transport/transport.go": `package transport

import (
	"time"

	. "example.com/m/exchange"
)

func Forge() *Record { return &Record{Deadline: time.Now()} }
`,
	})
	got, _ := Derive(src, ctorProp("Deadline", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
	if got.Outcome != Refuted {
		t.Fatalf("a construction through a dot import was not observed: outcome=%s: %s", got.Outcome, got.Detail)
	}
	if !strings.Contains(got.Detail, "transport/transport.go") {
		t.Errorf("the refutation does not name the dot-imported construction site: %s", got.Detail)
	}
}

// A LOCAL type of the same name still wins, exactly as Go resolves it. Without this the repair
// would attribute every unqualified name in a dot-importing file to the imported package.
func TestConstructionConfined_ALocalTypeOutranksADotImportedOneOfTheSameName(t *testing.T) {
	src := pinned(t, map[string]string{
		"go.mod":               ctorGoMod,
		"exchange/exchange.go": ctorOwnerPkg,
		"transport/transport.go": `package transport

import (
	"time"

	. "example.com/m/exchange"
)

// A DIFFERENT Record, declared here. Constructing it mints no exchange.Record.
type Record struct {
	Deadline time.Time
}

var _ = Open

func Local() *Record { return &Record{Deadline: time.Now()} }
`,
	})
	got, _ := Derive(src, ctorProp("Deadline", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
	if got.Outcome != Derived {
		t.Fatalf("a LOCAL type of the same name was read as the dot-imported one: outcome=%s: %s", got.Outcome, got.Detail)
	}
}

// ALIAS CHAINS RESOLVE TO A FIXPOINT (constructionconfinement.go:150, P2). The chain here is
// longer than the eight passes the old implementation allowed, so it fails mechanically against
// that version rather than by argument.
func TestConstructionConfined_AnAliasChainLongerThanTheOldBoundStillResolves(t *testing.T) {
	const n = 40
	var b strings.Builder
	b.WriteString("package transport\n\nimport (\n\t\"time\"\n\n\t\"example.com/m/exchange\"\n)\n\nvar _ = time.Now\n\n")
	// DESCENDING: A00 = A01, A01 = A02, ... A39 = exchange.Record. Zero-padded so the sorted
	// order is A00..A39 -- the REVERSE of dependency order, so only the last link can resolve
	// on the first pass and the chain needs n passes. An ascending chain sorted into dependency
	// order resolves in ONE pass, which is why the first version of this witness did not
	// exercise the bound it was written to defeat.
	for i := 0; i < n-1; i++ {
		fmt.Fprintf(&b, "type A%02d = A%02d\n", i, i+1)
	}
	fmt.Fprintf(&b, "type A%02d = exchange.Record\n", n-1)
	b.WriteString("\nfunc Forge() *A00 { return &A00{Deadline: time.Now()} }\n")

	src := pinned(t, map[string]string{
		"go.mod":                 ctorGoMod,
		"exchange/exchange.go":   ctorOwnerPkg,
		"transport/transport.go": b.String(),
	})
	got, _ := Derive(src, ctorProp("Deadline", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
	if got.Outcome != Refuted {
		t.Fatalf("a %d-link alias chain to the owner was not followed: outcome=%s: %s", n, got.Outcome, got.Detail)
	}
}

// An alias CYCLE terminates and resolves nothing. It does not compile in Go, so the only
// requirement is that the analyzer does not spin or invent an identity.
func TestConstructionConfined_AnAliasCycleTerminatesWithoutResolving(t *testing.T) {
	src := pinned(t, map[string]string{
		"go.mod":               ctorGoMod,
		"exchange/exchange.go": ctorOwnerPkg,
		"transport/transport.go": `package transport

import "example.com/m/exchange"

type Loop = Other
type Other = Loop

var _ = exchange.Open

func F() { var x Loop; _ = x }
`,
	})
	got, _ := Derive(src, ctorProp("Deadline", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
	// Whatever it concludes, it must terminate and must not claim the cycle is the owner.
	if got.Outcome == Refuted && strings.Contains(got.Detail, "transport/transport.go") {
		t.Errorf("an alias cycle was resolved to the owner's type: %s", got.Detail)
	}
}

// The result must not depend on map iteration order.
func TestConstructionConfined_AliasResolutionIsDeterministic(t *testing.T) {
	src := pinned(t, map[string]string{
		"go.mod":               ctorGoMod,
		"exchange/exchange.go": ctorOwnerPkg,
		"transport/transport.go": `package transport

import (
	"time"

	"example.com/m/exchange"
)

type Z = Y
type Y = X
type X = exchange.Record

func Forge() *Z { return &Z{Deadline: time.Now()} }
`,
	})
	first, _ := Derive(src, ctorProp("Deadline", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
	for i := 0; i < 40; i++ {
		got, _ := Derive(src, ctorProp("Deadline", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
		if got.Outcome != first.Outcome || got.Detail != first.Detail {
			t.Fatalf("run %d disagreed with the first: %s / %s vs %s / %s", i, got.Outcome, got.Detail, first.Outcome, first.Detail)
		}
	}
	if first.Outcome != Refuted {
		t.Errorf("the chained alias was not followed: %s / %s", first.Outcome, first.Detail)
	}
}

// ---------------------------------------------------------------------------
// Antigravity findings on this head. All three are in code added earlier in this same pass.

// P1 (mutationconfinement.go:557): EVERY dot import must survive. importsOf keyed them all
// under ".", so each overwrote the previous and an unqualified name from any but the last
// dot-imported package resolved nowhere -- its constructions invisible, and DERIVED returned.
func TestConstructionConfined_EveryDotImportIsResolved(t *testing.T) {
	// The owner is dot-imported FIRST, so under the old keying it was overwritten by helper.
	src := pinned(t, map[string]string{
		"go.mod":               ctorGoMod,
		"exchange/exchange.go": ctorOwnerPkg,
		"helper/helper.go":     "package helper\n\nfunc Help() int { return 1 }\n",
		"transport/transport.go": `package transport

import (
	"time"

	. "example.com/m/exchange"
	. "example.com/m/helper"
)

var _ = Help

func Forge() *Record { return &Record{Deadline: time.Now()} }
`,
	})
	got, _ := Derive(src, ctorProp("Deadline", "exchange", "transport", "helper"), at("2026-09-13T12:00:00Z"))
	if got.Outcome != Refuted {
		t.Fatalf("a construction through the FIRST of two dot imports was not observed: outcome=%s: %s",
			got.Outcome, got.Detail)
	}
}

// P1 (constructionconfinement.go:263): GO SCOPING. A locally declared name shadows a
// dot-imported one, so a package that dot-imports the owner AND declares its own Record must
// not have `var x Record` counted as constructing the owner's type.
//
// The composite-literal path never had this bug: it resolves through resolver.typeExpr, which
// checks declaration membership. My witness for local shadowing exercised that path only and I
// applied its conclusion to the zero-value path, which scans every candidate.
func TestConstructionConfined_ALocalDeclarationShadowsADotImportInZeroValueForms(t *testing.T) {
	src := pinned(t, map[string]string{
		"go.mod":               ctorGoMod,
		"exchange/exchange.go": ctorOwnerPkg,
		"transport/transport.go": `package transport

import (
	"time"

	. "example.com/m/exchange"
)

// transport's OWN Record. Go resolves the bare name to this one.
type Record struct {
	Val int
}

var _ = Open
var _ = time.Now

func F() { var x Record; _ = x }

func G() { p := new(Record); _ = p }
`,
	})
	// TYPE-LEVEL, where zero-value forms are sites: the local Record is not the owner's.
	got, _ := Derive(src, ctorProp("", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
	if got.Outcome == Refuted {
		t.Fatalf("a LOCAL type shadowing a dot-imported one was counted as the owner's: %s", got.Detail)
	}
}

// And the same file shape WITHOUT a local declaration must still be observed, or the shadowing
// repair would have disabled dot-import resolution for zero-value forms entirely.
func TestConstructionConfined_ADotImportedZeroValueFormIsStillObserved(t *testing.T) {
	src := pinned(t, map[string]string{
		"go.mod":               ctorGoMod,
		"exchange/exchange.go": ctorOwnerPkg,
		"transport/transport.go": `package transport

import (
	"time"

	. "example.com/m/exchange"
)

var _ = time.Now
var _ = Open

func F() { var x Record; _ = x }
`,
	})
	got, _ := Derive(src, ctorProp("", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
	if got.Outcome != Refuted {
		t.Fatalf("a dot-imported zero-value construction was not observed: outcome=%s: %s", got.Outcome, got.Detail)
	}
}

// P2 (constructionconfinement.go:189): A POINTER ALIAS IS NOT THE STRUCT. `type Ptr = *Record`
// denotes a pointer type; `var p Ptr` allocates a nil pointer and constructs no Record. The name
// resolvers strip a star, which is right for binding a field access and wrong for deciding what
// an alias denotes.
func TestConstructionConfined_APointerAliasConstructsNoValue(t *testing.T) {
	got, _ := Derive(ctorElided(t, "type Ptr = *exchange.Record\n\nfunc F() { var p Ptr; _ = p }"),
		ctorProp("", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
	if got.Outcome != Derived {
		t.Fatalf("a nil pointer through an alias was counted as constructing the owner: outcome=%s: %s",
			got.Outcome, got.Detail)
	}
}

// A VALUE alias still canonicalizes, so the pointer repair did not disable alias resolution.
func TestConstructionConfined_AValueAliasStillCanonicalizesForZeroValueForms(t *testing.T) {
	got, _ := Derive(ctorElided(t, "type Val = exchange.Record\n\nfunc F() { var x Val; _ = x }"),
		ctorProp("", "exchange", "transport"), at("2026-09-13T12:00:00Z"))
	if got.Outcome != Refuted {
		t.Fatalf("a value alias stopped resolving to the owner: outcome=%s: %s", got.Outcome, got.Detail)
	}
}
