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
	// Resolved to a TRUE FIXPOINT, in a deterministic order.
	//
	// This ran a fixed eight passes, which was an implementation limit wearing a semantic
	// one: a longer alias chain left its final alias absent from canonical, so an outside
	// `Alias{Deadline: …}` was ignored and a recognised owner construction could still make
	// the receipt DERIVED. Map iteration also meant a borderline chain resolved or did not
	// depending on traversal order -- a nondeterministic architectural verdict (review
	// finding constructionconfinement.go:150).
	//
	// Now it iterates until no progress, so any finite chain resolves. The FIXPOINT is what
	// removes the order dependence: once iteration continues while anything still resolves,
	// the final map is the same whichever order the aliases are visited in -- measured, a
	// mutant that REVERSES the sort changes no outcome. The sort is kept anyway, so the
	// traversal is deterministic and reviewable rather than dependent on Go's map walk, but
	// it is no longer load-bearing for correctness and this comment does not pretend it is.
	//
	// A cycle (`type A = B; type B = A`, which does not compile) makes no progress and
	// terminates without resolving, which is the correct answer for a name that denotes
	// nothing.
	aliasRefs := make([]typeRef, 0, len(named))
	for ref, nt := range named {
		if nt.alias {
			aliasRefs = append(aliasRefs, ref)
		}
	}
	sort.Slice(aliasRefs, func(i, j int) bool {
		if aliasRefs[i].dir != aliasRefs[j].dir {
			return aliasRefs[i].dir < aliasRefs[j].dir
		}
		return aliasRefs[i].name < aliasRefs[j].name
	})
	canonical := map[typeRef]typeRef{}
	// Aliases whose target is not a bare struct name -- `type Ptr = *Record`, `type L = []Record`.
	// They still canonicalize, so an elided element literal through them is observed; the
	// zero-value path consults this to refuse `var p Ptr`, which allocates no struct.
	pointerAlias := map[typeRef]bool{}
	for {
		progress := false
		for _, ref := range aliasRefs {
			if _, done := canonical[ref]; done {
				continue
			}
			nt := named[ref]
			// A POINTER ALIAS IS RECORDED, AND MARKED. `type Ptr = *Record` denotes a pointer,
			// so `var p Ptr` allocates nil and constructs no Record -- but `[]Ptr{{Deadline: t}}`
			// DOES construct one, because an elided element of a pointer slice means
			// &Record{...}. Excluding pointer aliases from canonical altogether fixed the first
			// case and silently reopened the second: an elided construction through a pointer
			// alias became invisible.
			//
			// So the alias resolves like any other and the ZERO-VALUE path rejects it, which is
			// the path where "constructs nothing" is the actual rule.
			if _, bare := directTypeRefOf(nt.expr, nt.dir, nt.imports, modulePath); !bare {
				pointerAlias[ref] = true
			}
			for _, target := range refCandidatesOfTypeName(nt.expr, nt.dir, nt.imports, modulePath) {
				if _, ok := structs[target]; ok {
					canonical[ref], progress = target, true
					break
				}
				// An alias whose target is itself an alias resolves once the chain below
				// it has, which is why this runs to a fixpoint rather than once.
				if through, ok := canonical[target]; ok {
					canonical[ref], progress = through, true
					// AND ITS POINTER-NESS. `type Ptr = *Record; type PtrAlias = Ptr` resolves
					// PtrAlias to Record through Ptr, and without carrying the mark
					// `var p PtrAlias` was counted as a struct construction: the chain lost
					// exactly the fact that makes it allocate nil.
					if pointerAlias[target] {
						pointerAlias[ref] = true
					}
					break
				}
			}
		}
		if !progress {
			break
		}
	}

	// Every declared type name in scope, struct or not, for Go's shadowing rule.
	declaredNames := map[typeRef]bool{}
	for ref := range structs {
		declaredNames[ref] = true
	}
	for ref := range named {
		declaredNames[ref] = true
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
		r := &resolver{structs: structs, imports: imports, modulePath: modulePath, dir: dir,
			declaredNames: declaredNames}
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
		elided := map[*ast.CompositeLit]elidedType{}
		// ownerRef reports whether a bare type name denotes the owner, following aliases.
		// ownerRef reports whether a bare type name denotes the owner, FOLLOWING GO'S SCOPING.
		//
		// A name declared in this package shadows a dot-imported one of the same name, so the
		// local declaration is checked first and, if it exists, the dot-imported candidates are
		// not considered at all. Scanning every candidate for a match let a package that
		// dot-imports the owner AND declares its own Record have `var x Record` counted as
		// constructing the owner's type -- a FALSE refutation.
		//
		// The composite-literal path never had this bug because it resolves through
		// resolver.typeExpr, which checks declaration membership. My witness for local shadowing
		// exercised that path only, and I applied its conclusion to this one.
		ownerRef := func(t ast.Expr) bool {
			if _, bare := directTypeRefOf(t, dir, imports, modulePath); !bare {
				return false
			}
			for _, c := range scopedCandidates(t, dir, imports, modulePath, declaredNames) {
				if c == owner {
					return true
				}
				if pointerAlias[c] {
					continue // denotes a pointer: allocates nil, constructs nothing
				}
				if through, cok := canonical[c]; cok && through == owner {
					return true
				}
			}
			return false
		}
		// noteSite records a construction at pos.
		noteSite := func(pos token.Pos, what string) {
			sites++
			p := fset.Position(pos)
			subjects = append(subjects, Subject{File: filePath, Line: p.Line, Entity: what, Role: "construction-site"})
			if dir != owner.dir {
				outside = append(outside, fmt.Sprintf("%s:%d", filePath, p.Line))
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			// ZERO-VALUE CONSTRUCTIONS, for a TYPE-LEVEL claim only.
			//
			// With a Field named, `var x T` and `new(T)` initialize no field, so they mint no
			// authority and are correctly not sites -- that is the distinction T1 exists to
			// draw, and it is preserved. Without a Field the proposition is "every
			// construction of T originates in the owner", and these ARE constructions of T:
			// each produces a usable zero value. Excluding them let an outside package create
			// one while the receipt still said DERIVED (review finding
			// constructionconfinement.go:233).
			//
			// My own Limits() text justified the exclusion "with a Field named" and the
			// analyzer applied it to both, which is the shape of this whole pass: reasoning
			// established for one configuration certifying a wider claim.
			if field == "" {
				switch x := n.(type) {
				case *ast.CallExpr:
					// new(T). Not new(*T) or new([]T), which construct no T.
					if id, isIdent := x.Fun.(*ast.Ident); isIdent && id.Name == "new" && len(x.Args) == 1 {
						if ownerRef(x.Args[0]) {
							noteSite(x.Pos(), "new("+p.Type+")")
						}
					}
				case *ast.ValueSpec:
					// var x T, with no initialiser: the declaration IS the construction. With
					// an initialiser the value's own construction is the site, counted where
					// it appears, so counting here too would double count one construction.
					if x.Type != nil && len(x.Values) == 0 && ownerRef(x.Type) {
						noteSite(x.Pos(), "var "+p.Type)
					}
				}
			}
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			typ := lit.Type
			// A written type means what it means IN THIS FILE; an inherited one means what it
			// meant where it was written, which may be another package.
			typDir, typImports := dir, imports
			if typ == nil {
				if e, ok := elided[lit]; ok {
					typ, typDir, typImports = e.expr, e.dir, e.imports
				}
			}
			// Record what each elided CHILD inherits before deciding anything about this
			// literal: the children are reached later, and one this never records becomes
			// an unreadable construction rather than an ignored one.
			if typ != nil {
				noteElidedChildren(lit, typ, typDir, typImports, modulePath, named, elided)
			} else {
				// A literal whose type nothing in scope reveals cannot be shown to be the
				// owner's, and cannot be shown NOT to be. That is the same completeness
				// boundary as an unkeyed literal and is named the same way.
				unresolved = append(unresolved, fmt.Sprintf("%s:%d (elided literal type; the enclosing type is not resolvable in the scope searched)",
					filePath, fset.Position(lit.Pos()).Line))
				return true
			}
			ref, ok := r.typeExprIn(typ, typDir, typImports)
			if !ok {
				// Not a struct declared in scope -- but it may be an ALIAS of one, under any
				// of the directories an unqualified name could come from.
				for _, aliasRef := range scopedCandidates(typ, typDir, typImports, modulePath, declaredNames) {
					if through, cok := canonical[aliasRef]; cok {
						ref, ok = through, true
						break
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

// elidedType is the type an elided child literal inherits, WITH THE SCOPE IT WAS WRITTEN IN.
//
// The expression alone is not enough. `exchange.Records{{Deadline: t}}` gives the child the
// element type `Record` as written in EXCHANGE's file, and resolving that unqualified name in
// the constructing package fails -- so the construction was silently ignored and the claim came
// back DERIVED. A type expression only means something together with the imports of the file
// that wrote it.
type elidedType struct {
	expr    ast.Expr
	dir     string
	imports map[string]string
}

// noteElidedChildren records the type each elided child literal of lit inherits.
//
// Go allows the elision only for an array or slice ELEMENT and a map KEY or VALUE, so
// those are the only positions read. A struct literal's field value may not elide its
// type, so a struct enclosing type yields nothing to inherit and any elided child under it
// stays unresolved -- which is correct, because such code does not compile.
func noteElidedChildren(lit *ast.CompositeLit, typ ast.Expr, dir string, imports map[string]string,
	modulePath string, named map[typeRef]namedTypeDecl, elided map[*ast.CompositeLit]elidedType) {

	var keyT, elemT ast.Expr
	// The collection may be declared in ANOTHER package, and then its element type is written in
	// that package's scope. underlyingCollection reports where it ended up.
	collection, collDir, collImports := underlyingCollection(typ, dir, imports, modulePath, named)
	switch x := collection.(type) {
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
			elided[c] = elidedType{expr: as, dir: collDir, imports: collImports}
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
	modulePath string, named map[typeRef]namedTypeDecl) (ast.Expr, string, map[string]string) {

	// Terminates on CONVERGENCE or a CYCLE, not on a step count. The adjacent alias resolution had
	// already been converted to a fixpoint and this loop kept a hardcoded eight, so a chain of nine
	// or more collection aliases truncated and left its elided element untyped -- reported
	// UNRESOLVED rather than recognised. A larger constant would move the boundary, not remove it.
	cur, curDir, curImports := t, dir, imports
	visited := map[typeRef]bool{}
	for {
		switch cur.(type) {
		case *ast.ArrayType, *ast.MapType, *ast.StructType:
			return cur, curDir, curImports
		}
		// Candidates, not one ref: an unqualified collection type name may come from a dot
		// import, and resolving only the current directory left such an elided child untyped --
		// reported UNRESOLVED rather than recognised.
		var nt namedTypeDecl
		found := false
		for _, ref := range refCandidatesOfTypeName(cur, curDir, curImports, modulePath) {
			if n, ok := named[ref]; ok {
				// A name already followed means the chain loops; stop where it started rather than
				// spin. Such code does not compile, so there is nothing to resolve.
				if visited[ref] {
					return cur, curDir, curImports
				}
				visited[ref] = true
				nt, found = n, true
				break
			}
		}
		if !found {
			return cur, curDir, curImports
		}
		// A named type writes its own type expression through the imports of the file that
		// DECLARES it, not those of the file constructing it.
		cur, curDir, curImports = nt.expr, nt.dir, nt.imports
	}
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
