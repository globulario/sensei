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
// A construction site usually names its own type syntactically — `T{…}`, `&T{…}`,
// `pkg.T{…}` — so the receiver-binding problem that dominates mutation analysis mostly
// disappears. Three things remain. Resolving a QUALIFIED name to a declaring directory
// needs the pinned go.mod, and that is shared with the third family. An ELIDED element
// literal names no type at all and takes it from the enclosing collection, which is
// resolved here rather than skipped — skipping it was a silent bypass, measured. And a
// type ALIAS denotes the owner's own type, so a construction through one is a
// construction of the owner, while a DEFINED type is a different type and is not.
//
// `new(T)` and `var x T` are deliberately NOT sites for a field claim. They name the type
// but initialize no field, so they mint no authority; what a later write puts there is the
// third family's question. Counting them would collapse the distinction this family
// exists to draw.
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
		"an elided element literal whose enclosing collection type is a named type declared OUTSIDE the scope searched, which is UNRESOLVED rather than assumed: the element type is whatever that declaration says, and this does not guess",
		"a construction through a type PARAMETER instantiated with the owner's type, which needs type inference this does not perform",
		"a construction by copying an existing value (assignment, append, range), which creates no new field initialization this can observe",
		"a zero value created by declaration (var x T), by new(T), by an empty literal, or by embedding in another struct: it initializes no field explicitly, so with a Field named it mints no authority and is not a site for this claim -- what a later write puts there is the THIRD family's question, not this one",
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
	// Type declarations that are NOT structs, kept because two of them can hide a
	// construction of the owner: an alias denotes the owner's own type, and a named
	// collection type is what an elided element literal inherits from.
	named := map[typeRef]namedTypeDecl{}
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
					continue
				}
				named[typeRef{dir, ts.Name.Name}] = namedTypeDecl{
					expr: ts.Type, dir: dir, imports: imports, alias: ts.Assign.IsValid()}
			}
		}
	}
	if _, ok := structs[owner]; !ok {
		return Attempt{Outcome: Unknown, Inputs: read,
			Detail: fmt.Sprintf("no struct type %s is declared in %s under the scope searched; nothing to establish", p.Type, p.Dir)}
	}

	// An ALIAS denotes the owner's type, so `Alias{Deadline: t}` written anywhere mints an
	// owner value and must be read as a construction of it. A DEFINED type (`type N T`) is
	// a DIFFERENT type and is deliberately NOT followed: constructing an N mints no T, and
	// the conversion that would turn one into the other is a separate, stated limit.
	canonical := map[typeRef]typeRef{}
	for pass := 0; pass < 8; pass++ {
		progress := false
		for ref, nt := range named {
			if !nt.alias {
				continue
			}
			if _, done := canonical[ref]; done {
				continue
			}
			target, ok := refOfTypeName(nt.expr, nt.dir, nt.imports, modulePath)
			if !ok {
				continue
			}
			if _, ok := structs[target]; ok {
				canonical[ref], progress = target, true
				continue
			}
			// An alias whose target is itself an alias resolves once the chain below it
			// has, which is why this runs to a fixpoint rather than once.
			if through, ok := canonical[target]; ok {
				canonical[ref], progress = through, true
			}
		}
		if !progress {
			break
		}
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
		imports := importsOf(f)
		r := &resolver{structs: structs, imports: imports, modulePath: modulePath, dir: dir}
		// An ELIDED composite literal -- the `{Deadline: t}` in `[]Record{{Deadline: t}}`
		// -- names no type of its own. Go permits that elision in exactly three places:
		// the element of an array or slice literal, and the key or value of a map literal
		// (never a struct field's value). So the type always comes from the ENCLOSING
		// literal, and because ast.Inspect visits a parent before its children the answer
		// is already recorded by the time the child is reached.
		//
		// Reading lit.Type == nil as "not a construction" was a silent bypass, and not one
		// Limits() ever claimed: an outsider could write `[]exchange.Record{{Deadline: t}}`
		// and the claim still came back DERIVED. Measured 2026-09-13 across seven elided
		// shapes, all seven.
		elided := map[*ast.CompositeLit]ast.Expr{}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			typ := lit.Type
			if typ == nil {
				typ = elided[lit] // nil when the enclosing type did not reveal it
			}
			// Record what each elided CHILD inherits before deciding anything about this
			// literal: the children are reached later, and one this never records becomes
			// an unreadable construction rather than an ignored one.
			if typ != nil {
				noteElidedChildren(lit, typ, dir, imports, modulePath, named, elided)
			} else {
				// A literal whose type nothing in scope reveals cannot be shown to be the
				// owner's, and cannot be shown NOT to be. That is the same completeness
				// boundary as an unkeyed literal and is named the same way.
				unresolved = append(unresolved, fmt.Sprintf("%s:%d (elided literal type; the enclosing type is not resolvable in the scope searched)",
					filePath, fset.Position(lit.Pos()).Line))
				return true
			}
			ref, ok := r.typeExpr(typ)
			if !ok {
				// Not a struct declared in scope -- but it may be an ALIAS of one.
				if aliasRef, aok := refOfTypeName(typ, dir, imports, modulePath); aok {
					if through, cok := canonical[aliasRef]; cok {
						ref, ok = through, true
					}
				}
			} else if through, cok := canonical[ref]; cok {
				ref = through
			}
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
		// "none found" and "none READABLE" are different facts, and reporting the second as
		// the first would hide exactly the sites this family exists to see.
		if len(unresolved) != 0 {
			sort.Strings(unresolved)
			return Attempt{Outcome: Unresolved, Inputs: read, Subjects: subjects, Detail: fmt.Sprintf(
				"no readable construction of %s under %s, and %d construction(s) could not be read: %s",
				what, strings.Join(p.SearchPaths, ", "), len(unresolved), strings.Join(unresolved, "; "))}
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
			"%d construction(s) could not be read as constructions of %s and no counterexample was found among the %d that could: %s",
			len(unresolved), what, sites, strings.Join(unresolved, "; "))}
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

// namedTypeDecl is a type declaration that is not a struct: an alias, or a named
// slice/array/map. Both can hide a construction of the owner, which is why they are
// collected rather than skipped.
type namedTypeDecl struct {
	expr    ast.Expr
	dir     string
	imports map[string]string
	alias   bool // `type A = T` denotes T itself; `type A T` is a different type
}

// noteElidedChildren records the type each elided child literal of lit inherits.
//
// Go allows the elision only for an array or slice ELEMENT and a map KEY or VALUE, so
// those are the only positions read. A struct literal's field value may not elide its
// type, so a struct enclosing type yields nothing to inherit and any elided child under it
// stays unresolved -- which is correct, because such code does not compile.
func noteElidedChildren(lit *ast.CompositeLit, typ ast.Expr, dir string, imports map[string]string,
	modulePath string, named map[typeRef]namedTypeDecl, elided map[*ast.CompositeLit]ast.Expr) {

	var keyT, elemT ast.Expr
	switch x := underlyingCollection(typ, dir, imports, modulePath, named).(type) {
	case *ast.ArrayType:
		elemT = x.Elt
	case *ast.MapType:
		keyT, elemT = x.Key, x.Value
	default:
		return
	}
	note := func(e ast.Expr, as ast.Expr) {
		if as == nil {
			return
		}
		if c, ok := unparen(e).(*ast.CompositeLit); ok && c.Type == nil {
			elided[c] = as
		}
	}
	for _, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			note(kv.Key, keyT) // nil for an array or slice index, so nothing is claimed
			note(kv.Value, elemT)
			continue
		}
		note(el, elemT)
	}
}

// underlyingCollection follows a NAMED collection type to the slice, array or map it is
// declared as, so `type Records []Record` reveals what `Records{{…}}`'s elements are. It
// follows both aliases and defined types, because the question here is the shape of the
// enclosing literal, not the identity of the type being constructed -- a defined
// `type Records []Record` still has Record elements.
//
// It returns its input unchanged when the declaration is not in scope, which is what makes
// such an elided child UNRESOLVED rather than silently ignored.
func underlyingCollection(t ast.Expr, dir string, imports map[string]string,
	modulePath string, named map[typeRef]namedTypeDecl) ast.Expr {

	cur, curDir, curImports := t, dir, imports
	for i := 0; i < 8; i++ {
		switch cur.(type) {
		case *ast.ArrayType, *ast.MapType, *ast.StructType:
			return cur
		}
		ref, ok := refOfTypeName(cur, curDir, curImports, modulePath)
		if !ok {
			return cur
		}
		nt, ok := named[ref]
		if !ok {
			return cur
		}
		// A named type writes its own type expression through the imports of the file that
		// DECLARES it, not those of the file constructing it.
		cur, curDir, curImports = nt.expr, nt.dir, nt.imports
	}
	return cur
}

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
