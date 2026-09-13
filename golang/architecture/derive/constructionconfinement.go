// SPDX-License-Identifier: AGPL-3.0-only

package derive

// The fourth registered derivation: is a value of an owner-controlled type created only
// by its owner.
//
// # Why a fourth family rather than a wider third
//
// state_mutation_confined_to_owner asks who may CHANGE a field. Applied to
// internal/ghbridge/exchange.go it answered, correctly, "no write to
// ExchangeRecord.Deadline found … nothing to establish": that field is set only in
// composite literals at construction and never mutated afterwards. The analyzer was
// right, and widening it to count constructions as writes would have destroyed a
// distinction it exists to make — a constructor filling its own struct is not a caller
// reaching in.
//
// So the missing fact is a different species: not "who may change this" but "who may
// MINT one". For an authority-bearing field that is the question that matters. A
// deadline a restarted waiter must honour is meaningless if any package can construct a
// record carrying one.
//
// # What it answers, and what it does not
//
// Answered: within the scope searched, every construction of T that this derivation can
// bind to T originates from the package declaring T. With Field named, only
// constructions that INITIALIZE that field are considered.
//
// Not answered: that construction SHOULD be confined (an exported struct may be meant
// to be caller-constructible, and a REFUTED here is a counterexample, not a defect);
// that a value cannot arrive by a route this cannot see; anything about mutation after
// construction, which is the third family's question.
//
// # Binding, and why it is narrower here than for mutation
//
// A construction site names its own type syntactically — `T{…}`, `&T{…}`, `pkg.T{…}`,
// `new(T)` — so the receiver-binding problem that dominates mutation analysis mostly
// disappears. What remains is resolving a QUALIFIED name to a declaring directory, which
// needs the pinned go.mod, and that is shared with the third family.
//
// # Vacuity is refused, deliberately
//
// Zero construction sites yields "nothing to establish", never DERIVED. A confinement
// claim over an empty set is true and worthless, and worse than worthless here: a
// coverage consumer would read it as evidence about a file nothing examined. The third
// family already makes this choice for writes; this makes the same one for constructions.

import (
	"fmt"
	"go/ast"
	"go/token"
	"path"
	"sort"
	"strings"
)

type constructionConfinement struct{}

func (constructionConfinement) ID() string      { return "derive.construction_confined_to_owner" }
func (constructionConfinement) Version() string { return "v1" }

func (constructionConfinement) Limits() []string {
	return []string{
		"a value obtained from a call rather than constructed at the site: the construction is wherever that function body is, and if that body is outside the scope searched this cannot see it",
		"a construction through reflection, unsafe, or a generic instantiation whose type argument this does not resolve",
		"a construction by conversion from another struct type with an identical shape",
		"an UNKEYED positional literal, which is UNRESOLVED rather than assumed: which element initializes which field needs the declaration order, and this does not guess",
		"a construction by copying an existing value (assignment, append, range), which creates no new field initialization this can observe",
		"a zero value created by declaration (var x T) or by embedding in another struct, which initializes no field explicitly",
		"a qualified type name when the pinned tree has no go.mod to resolve the module path",
		"a construction in a file outside the scope searched, or in a dependency outside the repository",
		"a construction from a testdata/ directory, which the Go toolchain excludes from the program",
	}
}

func (constructionConfinement) Applies(p Proposition) bool {
	// Field is OPTIONAL here, unlike the mutation family: without it the claim is about
	// the type, with it about one authority-bearing field of the type.
	return p.Kind == KindConstructionConfinedToOwner &&
		strings.TrimSpace(p.Dir) != "" && strings.TrimSpace(p.Type) != "" &&
		len(p.SearchPaths) != 0
}

func (constructionConfinement) Derive(src PinnedSource, p Proposition) Attempt {
	files, read, fset, failure := parseScope(src, p.SearchPaths)
	if failure != nil {
		return *failure
	}
	if len(files) == 0 {
		return Attempt{Outcome: Unknown, Inputs: read,
			Detail: fmt.Sprintf("no non-test Go files under %s at the pinned commit", strings.Join(p.SearchPaths, ", "))}
	}
	owner := typeRef{dir: cleanDir(p.Dir), name: p.Type}
	modulePath, modRead := modulePathOf(src)
	if modRead {
		// go.mod decides how a qualified construction binds, so a receipt resting on it
		// must name it as an input.
		read = append(read, "go.mod")
	}

	structs := map[typeRef]structDecl{}
	for i, f := range files {
		dir := cleanDir(path.Dir(read[i]))
		imports := importsOf(f)
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, sp := range gd.Specs {
				ts, ok := sp.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if st, ok := ts.Type.(*ast.StructType); ok {
					structs[typeRef{dir, ts.Name.Name}] = structDecl{st: st, imports: imports}
				}
			}
		}
	}
	if _, ok := structs[owner]; !ok {
		return Attempt{Outcome: Unknown, Inputs: read,
			Detail: fmt.Sprintf("no struct type %s is declared in %s under the scope searched; nothing to establish", p.Type, p.Dir)}
	}

	field := strings.TrimSpace(p.Field)
	var subjects []Subject
	var outside, unresolved []string
	sites := 0

	// THE DECLARATION IS A SUBJECT, not only the construction sites.
	//
	// The proposition names a type, and the type's declaration is what gives every
	// construction site its meaning: adding a field, renaming one, or changing
	// Deadline's type invalidates this claim as surely as adding a construction outside
	// the owner does. A receipt whose subjects omitted the declaration would be
	// invalidated by edits it never mentions.
	//
	// It is a DIFFERENT role from a construction site and is never counted as one, so
	// the confinement arithmetic and the vacuity rule below are unaffected.
	for i, f := range files {
		if cleanDir(path.Dir(read[i])) != owner.dir {
			continue
		}
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, sp := range gd.Specs {
				ts, ok := sp.(*ast.TypeSpec)
				if !ok || ts.Name.Name != owner.name {
					continue
				}
				subjects = append(subjects, Subject{File: read[i], Line: fset.Position(ts.Pos()).Line,
					Entity: p.Type, Role: "declaration-site"})
			}
		}
	}

	for i, f := range files {
		filePath := read[i]
		dir := cleanDir(path.Dir(filePath))
		r := &resolver{structs: structs, imports: importsOf(f), modulePath: modulePath, dir: dir}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || lit.Type == nil {
				return true
			}
			ref, ok := r.typeExpr(lit.Type)
			if !ok || ref != owner {
				return true
			}
			// With a field named, only a construction that INITIALIZES it counts: a
			// literal leaving the field at its zero value mints no authority.
			//
			// An UNKEYED literal is neither. Deciding which position is which field
			// needs the declaration's field order, and guessing would attribute
			// authority to the wrong field — so it is the completeness boundary, and it
			// is named as UNRESOLVED rather than silently passed over. Treating it as
			// "not a site" would have been a bypass: an outsider could mint a record
			// positionally and escape the claim (a mutation survived on exactly that).
			if field != "" {
				switch literalInitializesField(lit, field) {
				case fieldNotInitialized:
					return true
				case fieldPositionUnreadable:
					unresolved = append(unresolved, fmt.Sprintf("%s:%d (unkeyed literal)", filePath, fset.Position(lit.Pos()).Line))
					return true
				}
			}
			sites++
			pos := fset.Position(lit.Pos())
			entity := p.Type + "{}"
			if field != "" {
				entity = p.Type + "{" + field + ":}"
			}
			subjects = append(subjects, Subject{File: filePath, Line: pos.Line, Entity: entity, Role: "construction-site"})
			if dir != owner.dir {
				outside = append(outside, fmt.Sprintf("%s:%d", filePath, pos.Line))
			}
			return true
		})
	}

	if sites == 0 {
		what := p.Type
		if field != "" {
			what = p.Type + "." + field
		}
		return Attempt{Outcome: Unknown, Inputs: read, Detail: fmt.Sprintf(
			"no construction of %s found under %s; nothing to establish", what, strings.Join(p.SearchPaths, ", "))}
	}
	what := p.Type
	if field != "" {
		what = p.Type + " initializing " + field
	}
	if len(outside) != 0 {
		sort.Strings(outside)
		return Attempt{Outcome: Refuted, Inputs: read, Subjects: subjects, Detail: fmt.Sprintf(
			"counterexample to construction confinement: %d of %d observable construction(s) of %s originate outside %s: %s",
			len(outside), sites, what, p.Dir, strings.Join(outside, "; "))}
	}
	// A counterexample outranks an unreadable site: a proven violation is a stronger fact
	// than an unexamined one. With none found, an unkeyed literal leaves the claim
	// UNRESOLVED rather than established.
	if len(unresolved) != 0 {
		sort.Strings(unresolved)
		return Attempt{Outcome: Unresolved, Inputs: read, Subjects: subjects, Detail: fmt.Sprintf(
			"%d construction(s) of %s could not be read as initializing %s and no counterexample was found among the %d that could: %s",
			len(unresolved), p.Type, field, sites, strings.Join(unresolved, "; "))}
	}
	return Attempt{Outcome: Derived, Inputs: read, Subjects: subjects, Detail: fmt.Sprintf(
		"all %d observable construction(s) of %s under %s originate from %s (%d file(s) were read to compute it)",
		sites, what, strings.Join(p.SearchPaths, ", "), p.Dir, len(read))}
}

// literalInitializes reports whether a composite literal explicitly sets field.
//
// Keyed literals only. An unkeyed positional literal is NOT read as initializing the
// field: deciding which position is which field needs the declaration's field order, and
// a mistake there would attribute authority to the wrong field. Unkeyed literals of an
// exported struct are rare and vet-discouraged; treating them as "not observed" keeps
// this from guessing, and the limit is stated in Limits().
type fieldInitKind int

const (
	fieldNotInitialized fieldInitKind = iota
	fieldInitialized
	// fieldPositionUnreadable: an unkeyed positional literal. Which element is which
	// field needs the declaration's field order, and this does not guess.
	fieldPositionUnreadable
)

func literalInitializesField(lit *ast.CompositeLit, field string) fieldInitKind {
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			// One unkeyed element makes the whole literal positional.
			return fieldPositionUnreadable
		}
		if id, ok := kv.Key.(*ast.Ident); ok && id.Name == field {
			return fieldInitialized
		}
	}
	return fieldNotInitialized
}
