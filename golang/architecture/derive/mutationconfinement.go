// SPDX-License-Identifier: AGPL-3.0-only

package derive

// The third registered derivation: is owner-controlled state written only by
// its owner.
//
// A COMPOSITION rather than a pattern. Lock discipline relates state to control
// flow inside one type; command confinement relates a boundary to the packages
// that cross it. This one must relate four things at once -- which type a
// written expression denotes, which package declares that type, where the write
// sits, and whether that place is inside the declaring package -- and cannot be
// answered by matching one syntactic shape. That is why it was chosen.
//
// # What it answers, and what it does not
//
// Answered: within the scope searched, every write to T.F whose receiver this
// derivation can bind to T originates from the package declaring T.
//
// Not answered: that F SHOULD be confined (an exported field may be meant to
// be caller-mutable, and a REFUTED here is a counterexample to confinement,
// not a defect); that no write happens by a route this cannot bind; or
// anything about reads.
//
// # Binding is syntactic, and the boundary of binding is the envelope
//
// There is no type checker here, by design: a derivation reads pinned bytes,
// not a build. A written selector `e.F` therefore counts only when `e`'s type
// can be bound from what was parsed -- a receiver, a parameter, a declared
// variable, a composite literal, a field chain through struct declarations in
// scope, a qualified name resolved through the pinned go.mod. A write to a
// field named F whose receiver cannot be bound is the completeness boundary:
// UNRESOLVED, named in the detail, never silently "not this type".

import (
	"fmt"
	"go/ast"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
)

type mutationConfinement struct{}

func (mutationConfinement) ID() string      { return "derive.state_mutation_confined_to_owner" }
func (mutationConfinement) Version() string { return "v1" }

func (mutationConfinement) Limits() []string {
	return []string{
		"a write whose receiver expression this derivation cannot bind to a type: the result of a call, an indexed or ranged element, a type-switch binding, an interface value -- each is UNRESOLVED, and a := it cannot read shadows an outer binding rather than leaking it",
		"a write reaching the field by promotion through an embedded field, which is UNRESOLVED rather than followed",
		"a write through reflection, unsafe, or an assembly routine",
		"a write through a field address that escapes the declaring package",
		"a write from a file outside the scope searched, or from a dependency outside the repository",
		"a qualified type name when the pinned tree has no go.mod to resolve the module path",
		"a write from a testdata/ directory, which the Go toolchain excludes from the program",
	}
}

func (mutationConfinement) Applies(p Proposition) bool {
	return p.Kind == KindStateMutationConfinedToOwner &&
		strings.TrimSpace(p.Dir) != "" && strings.TrimSpace(p.Type) != "" &&
		strings.TrimSpace(p.Field) != "" && len(p.SearchPaths) != 0
}

// typeRef names a struct type by declaring directory and name.
type typeRef struct{ dir, name string }

// structDecl is a struct type as declared, with the import map of the FILE
// that declared it. A field's qualified type is written in that file's
// aliases, not in the aliases of whichever file happens to write the field;
// resolving it through the mutation-site file's imports let an alias
// collision bind a chain to the wrong package (sensei#313 review, P1).
type structDecl struct {
	st      *ast.StructType
	imports map[string]string
}

func (mutationConfinement) Derive(src PinnedSource, p Proposition) Attempt {
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
		// go.mod decides how qualified names bind, so a receipt that rests on
		// it must name it as an input: a go.mod-only change that re-routes an
		// import path must invalidate the derivation (sensei#313 review, P2).
		read = append(read, "go.mod")
	}

	// Struct declarations in scope, by declaring directory and name, each
	// with the imports of the file that declared it.
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
				ts := sp.(*ast.TypeSpec)
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

	var subjects []Subject
	var outside, unresolved []string
	sites := 0
	for i, f := range files {
		filePath := read[i]
		dir := cleanDir(path.Dir(filePath))
		r := &resolver{structs: structs, imports: importsOf(f), modulePath: modulePath, dir: dir}
		w := &walker{r: r, field: p.Field, owner: owner, fset: fset, filePath: filePath, dir: dir,
			sites: &sites, subjects: &subjects, outside: &outside, unresolved: &unresolved}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			sc := newScope(nil)
			if fn.Recv != nil {
				bindFields(sc, fn.Recv.List)
			}
			if fn.Type.Params != nil {
				bindFields(sc, fn.Type.Params.List)
			}
			if fn.Type.Results != nil {
				bindFields(sc, fn.Type.Results.List)
			}
			w.block(fn.Body, sc)
		}
	}

	if sites == 0 && len(unresolved) == 0 {
		return Attempt{Outcome: Unknown, Inputs: read, Detail: fmt.Sprintf(
			"no write to %s.%s found under %s; nothing to establish", p.Type, p.Field, strings.Join(p.SearchPaths, ", "))}
	}
	if len(outside) != 0 {
		sort.Strings(outside)
		return Attempt{Outcome: Refuted, Inputs: read, Subjects: subjects, Detail: fmt.Sprintf(
			"counterexample to confinement: %d of %d observable write(s) to %s.%s originate outside %s: %s",
			len(outside), sites, p.Type, p.Field, p.Dir, strings.Join(outside, "; "))}
	}
	if len(unresolved) != 0 {
		sort.Strings(unresolved)
		return Attempt{Outcome: Unresolved, Inputs: read, Subjects: subjects, Detail: fmt.Sprintf(
			"%d write(s) to a field named %s could not be bound to a type and no counterexample was found among the %d that could: %s",
			len(unresolved), p.Field, sites, strings.Join(unresolved, "; "))}
	}
	return Attempt{Outcome: Derived, Inputs: read, Subjects: subjects, Detail: fmt.Sprintf(
		"all %d observable write(s) to %s.%s under %s originate from %s, across %d subject file(s) (%d file(s) were read to compute it)",
		sites, p.Type, p.Field, strings.Join(p.SearchPaths, ", "), p.Dir, len(subjectFiles(subjects)), len(read))}
}

// scope is one lexical binding frame. A name bound to nil is UNBOUND here: a
// := whose type this derivation cannot read shadows an outer binding rather
// than leaking it. Lookups walk outward; a hit on nil stops the walk.
type scope struct {
	parent *scope
	names  map[string]ast.Expr
}

func newScope(parent *scope) *scope { return &scope{parent: parent, names: map[string]ast.Expr{}} }

func (s *scope) lookup(name string) (ast.Expr, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if t, ok := cur.names[name]; ok {
			return t, t != nil
		}
	}
	return nil, false
}

func bindFields(sc *scope, fields []*ast.Field) {
	for _, fld := range fields {
		for _, n := range fld.Names {
			sc.names[n.Name] = fld.Type
		}
	}
}

// walker visits statements with an explicit scope chain. ast.Inspect has no
// exit hook, and a single function-wide map let a nested `o := &Other{}`
// overwrite an outer `o *pool.Options` for the rest of the function -- a real
// bypass after the block then vanished from the proof (sensei#313 review).
type walker struct {
	r          *resolver
	field      string
	owner      typeRef
	fset       *token.FileSet
	filePath   string
	dir        string
	sites      *int
	subjects   *[]Subject
	outside    *[]string
	unresolved *[]string
}

func (w *walker) block(b *ast.BlockStmt, parent *scope) {
	if b == nil {
		return
	}
	sc := newScope(parent)
	for _, st := range b.List {
		w.stmt(st, sc)
	}
}

func (w *walker) stmt(n ast.Stmt, sc *scope) {
	switch x := n.(type) {
	case nil:
		return
	case *ast.BlockStmt:
		w.block(x, sc)
	case *ast.DeclStmt:
		gd, ok := x.Decl.(*ast.GenDecl)
		if !ok {
			return
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, v := range vs.Values {
				w.expr(v, sc)
			}
			if gd.Tok != token.VAR {
				continue
			}
			for j, name := range vs.Names {
				switch {
				case vs.Type != nil:
					sc.names[name.Name] = vs.Type
				case j < len(vs.Values):
					sc.names[name.Name] = literalType(vs.Values[j]) // nil = unbound, shadowing
				default:
					sc.names[name.Name] = nil
				}
			}
		}
	case *ast.AssignStmt:
		for _, rhs := range x.Rhs {
			w.expr(rhs, sc)
		}
		if x.Tok == token.DEFINE {
			for j, lhs := range x.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || id.Name == "_" {
					continue
				}
				var t ast.Expr
				if len(x.Lhs) == len(x.Rhs) && j < len(x.Rhs) {
					t = literalType(x.Rhs[j])
				}
				sc.names[id.Name] = t // nil = unbound, and it SHADOWS
			}
			return
		}
		for _, lhs := range x.Lhs {
			w.target(lhs, sc, "write")
			w.expr(lhs, sc)
		}
	case *ast.IncDecStmt:
		w.target(x.X, sc, "write")
	case *ast.ExprStmt:
		w.expr(x.X, sc)
	case *ast.ReturnStmt:
		for _, e := range x.Results {
			w.expr(e, sc)
		}
	case *ast.IfStmt:
		inner := newScope(sc)
		w.stmt(x.Init, inner)
		w.expr(x.Cond, inner)
		w.block(x.Body, inner)
		w.stmt(x.Else, inner)
	case *ast.ForStmt:
		inner := newScope(sc)
		w.stmt(x.Init, inner)
		w.expr(x.Cond, inner)
		w.stmt(x.Post, inner)
		w.block(x.Body, inner)
	case *ast.RangeStmt:
		inner := newScope(sc)
		w.expr(x.X, inner)
		for _, e := range []ast.Expr{x.Key, x.Value} {
			if e == nil {
				continue
			}
			if x.Tok == token.DEFINE {
				if id, ok := e.(*ast.Ident); ok && id.Name != "_" {
					inner.names[id.Name] = nil // element types are not followed
				}
			} else {
				w.target(e, inner, "write")
			}
		}
		w.block(x.Body, inner)
	case *ast.SwitchStmt:
		inner := newScope(sc)
		w.stmt(x.Init, inner)
		w.expr(x.Tag, inner)
		for _, c := range x.Body.List {
			cc := c.(*ast.CaseClause)
			cs := newScope(inner)
			for _, e := range cc.List {
				w.expr(e, cs)
			}
			for _, st := range cc.Body {
				w.stmt(st, cs)
			}
		}
	case *ast.TypeSwitchStmt:
		inner := newScope(sc)
		w.stmt(x.Init, inner)
		var bound string
		if as, ok := x.Assign.(*ast.AssignStmt); ok && len(as.Lhs) == 1 {
			if id, ok := as.Lhs[0].(*ast.Ident); ok {
				bound = id.Name
			}
		}
		for _, c := range x.Body.List {
			cc := c.(*ast.CaseClause)
			cs := newScope(inner)
			if bound != "" {
				cs.names[bound] = nil // type switches are not followed
			}
			for _, st := range cc.Body {
				w.stmt(st, cs)
			}
		}
	case *ast.SelectStmt:
		for _, c := range x.Body.List {
			cc := c.(*ast.CommClause)
			cs := newScope(sc)
			w.stmt(cc.Comm, cs)
			for _, st := range cc.Body {
				w.stmt(st, cs)
			}
		}
	case *ast.LabeledStmt:
		w.stmt(x.Stmt, sc)
	case *ast.GoStmt:
		w.expr(x.Call, sc)
	case *ast.DeferStmt:
		w.expr(x.Call, sc)
	case *ast.SendStmt:
		w.expr(x.Chan, sc)
		w.expr(x.Value, sc)
	}
}

// expr descends into an expression for two things: &e.F, which is write
// authority, and function literals, which open a scope of their own.
func (w *walker) expr(e ast.Expr, sc *scope) {
	if e == nil {
		return
	}
	ast.Inspect(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			inner := newScope(sc)
			if x.Type.Params != nil {
				bindFields(inner, x.Type.Params.List)
			}
			w.block(x.Body, inner)
			return false
		case *ast.UnaryExpr:
			if x.Op == token.AND {
				w.target(x.X, sc, "address")
			}
		}
		return true
	})
}

// target classifies one written or address-taken expression: a site on the
// subject; another type's own field of the same name; a receiver the binder
// cannot resolve; or, for an embedded promotion, authority this derivation
// does not follow.
func (w *walker) target(e ast.Expr, sc *scope, kind string) {
	sel, ok := unparen(e).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != w.field {
		return
	}
	pos := w.fset.Position(sel.Pos())
	ref, bound := w.r.typeOfIn(sel.X, sc)
	switch {
	case !bound:
		*w.unresolved = append(*w.unresolved, fmt.Sprintf("%s:%d (receiver %s not bound)", w.filePath, pos.Line, exprString(sel.X)))
	case ref == w.owner:
		if kind == "address" && w.dir != w.owner.dir {
			// The sealed relation: an address of T.F taken outside the owner
			// is write authority handed across the boundary and not followed
			// -- neither a confinement nor a counterexample.
			*w.unresolved = append(*w.unresolved, fmt.Sprintf("%s:%d (address of %s.%s taken outside %s; write authority escapes)", w.filePath, pos.Line, w.owner.name, w.field, w.owner.dir))
			return
		}
		*w.sites++
		*w.subjects = append(*w.subjects, Subject{File: w.filePath, Line: pos.Line, Entity: w.owner.name + "." + w.field, Role: "mutation-site"})
		if w.dir != w.owner.dir {
			*w.outside = append(*w.outside, fmt.Sprintf("%s:%d in %s", w.filePath, pos.Line, w.dir))
		}
	default:
		// Bound to another struct. If it declares F itself, this is its own
		// field. If it does not, F reaches it by promotion through an
		// embedded field -- which may be T -- and promotion is not followed:
		// UNRESOLVED, never silently "not this type".
		decl, ok := w.r.structs[ref]
		if ok && !structDeclares(decl.st, w.field) && hasEmbedded(decl.st) {
			*w.unresolved = append(*w.unresolved, fmt.Sprintf("%s:%d (%s reached through an embedded field of %s; promotion is not followed)", w.filePath, pos.Line, w.field, ref.name))
		}
	}
}

func structDeclares(st *ast.StructType, name string) bool {
	for _, fld := range st.Fields.List {
		for _, n := range fld.Names {
			if n.Name == name {
				return true
			}
		}
	}
	return false
}

func hasEmbedded(st *ast.StructType) bool {
	for _, fld := range st.Fields.List {
		if len(fld.Names) == 0 {
			return true
		}
	}
	return false
}

// resolver binds expressions to struct types from what was parsed.
type resolver struct {
	structs    map[typeRef]structDecl
	imports    map[string]string // alias -> import path
	modulePath string
	dir        string // directory of the file being read
}

// typeOfIn binds an expression to a struct type under a scope chain, or
// reports that it cannot.
func (r *resolver) typeOfIn(e ast.Expr, sc *scope) (typeRef, bool) {
	switch x := unparen(e).(type) {
	case *ast.Ident:
		t, ok := sc.lookup(x.Name)
		if !ok {
			return typeRef{}, false
		}
		return r.typeExpr(t)
	case *ast.SelectorExpr:
		base, ok := r.typeOfIn(x.X, sc)
		if !ok {
			return typeRef{}, false
		}
		decl, ok := r.structs[base]
		if !ok {
			return typeRef{}, false
		}
		for _, fld := range decl.st.Fields.List {
			for _, n := range fld.Names {
				if n.Name == x.Sel.Name {
					// The field's type is written in the DECLARING file's
					// aliases.
					return r.typeExprIn(fld.Type, base.dir, decl.imports)
				}
			}
		}
		return typeRef{}, false
	case *ast.StarExpr:
		return r.typeOfIn(x.X, sc)
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return r.typeOfIn(x.X, sc)
		}
	case *ast.CompositeLit:
		return r.typeExpr(x.Type)
	}
	return typeRef{}, false
}

// typeExpr binds a type expression written in the current file, through the
// current file's imports.
func (r *resolver) typeExpr(t ast.Expr) (typeRef, bool) { return r.typeExprIn(t, r.dir, r.imports) }

// typeExprIn binds a type expression as written in a file of directory dir
// whose import map is imports. Every qualified name resolves through the
// aliases of the file that WROTE the type expression -- the mutation-site
// file for a binding, the declaring file for a struct field.
func (r *resolver) typeExprIn(t ast.Expr, dir string, imports map[string]string) (typeRef, bool) {
	switch x := t.(type) {
	case *ast.StarExpr:
		return r.typeExprIn(x.X, dir, imports)
	case *ast.ParenExpr:
		return r.typeExprIn(x.X, dir, imports)
	case *ast.Ident:
		ref := typeRef{dir, x.Name}
		if _, ok := r.structs[ref]; ok {
			return ref, true
		}
		return typeRef{}, false
	case *ast.SelectorExpr:
		pkg, ok := x.X.(*ast.Ident)
		if !ok {
			return typeRef{}, false
		}
		importPath, ok := imports[pkg.Name]
		if !ok || r.modulePath == "" {
			return typeRef{}, false
		}
		if importPath != r.modulePath && !strings.HasPrefix(importPath, r.modulePath+"/") {
			return typeRef{}, false
		}
		ref := typeRef{cleanDir(strings.TrimPrefix(strings.TrimPrefix(importPath, r.modulePath), "/")), x.Sel.Name}
		if _, ok := r.structs[ref]; ok {
			return ref, true
		}
		return typeRef{}, false
	case *ast.ArrayType:
		return typeRef{}, false
	}
	return typeRef{}, false
}

// literalType is the type an initialiser binds a name to when it says so
// itself: T{...}, &T{...}, new(T). Anything else binds nothing.
func literalType(v ast.Expr) ast.Expr {
	switch x := unparen(v).(type) {
	case *ast.CompositeLit:
		return x.Type
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			if cl, ok := unparen(x.X).(*ast.CompositeLit); ok {
				return cl.Type
			}
		}
	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "new" && len(x.Args) == 1 {
			return x.Args[0]
		}
	}
	return nil
}

func importsOf(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		alias := path.Base(p)
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		out[alias] = p
	}
	return out
}

// modulePathOf reads the module path from the pinned go.mod, reporting
// whether the file was read at all. Without it qualified references cannot
// be bound and say so, rather than being guessed from directory names.
func modulePathOf(src PinnedSource) (string, bool) {
	b, err := src.Read("go.mod")
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "module ") {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "module ")), true
		}
	}
	return "", true
}

func cleanDir(d string) string {
	d = strings.Trim(strings.TrimSpace(d), "/")
	if d == "" {
		return "."
	}
	return path.Clean(d)
}

func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

func exprString(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return exprString(x.X) + "." + x.Sel.Name
	case *ast.StarExpr:
		return "*" + exprString(x.X)
	case *ast.CallExpr:
		return exprString(x.Fun) + "(...)"
	case *ast.IndexExpr:
		return exprString(x.X) + "[...]"
	case *ast.ParenExpr:
		return exprString(x.X)
	}
	return fmt.Sprintf("%T", e)
}
