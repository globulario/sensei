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
		"a write whose receiver expression this derivation cannot bind to a type: the result of a call, an indexed element beyond one level, a closure capture, a shadowed name, an interface value, an embedded-field promotion",
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
	modulePath := modulePathOf(src)

	// Struct declarations in scope, by declaring directory and name.
	structs := map[typeRef]*ast.StructType{}
	for i, f := range files {
		dir := cleanDir(path.Dir(read[i]))
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, sp := range gd.Specs {
				ts := sp.(*ast.TypeSpec)
				if st, ok := ts.Type.(*ast.StructType); ok {
					structs[typeRef{dir, ts.Name.Name}] = st
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
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			r.bindings = map[string]ast.Expr{}
			if fn.Recv != nil {
				for _, fld := range fn.Recv.List {
					for _, n := range fld.Names {
						r.bindings[n.Name] = fld.Type
					}
				}
			}
			if fn.Type.Params != nil {
				for _, fld := range fn.Type.Params.List {
					for _, n := range fld.Names {
						r.bindings[n.Name] = fld.Type
					}
				}
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.DeclStmt:
					if gd, ok := x.Decl.(*ast.GenDecl); ok && gd.Tok == token.VAR {
						for _, sp := range gd.Specs {
							vs := sp.(*ast.ValueSpec)
							for j, name := range vs.Names {
								switch {
								case vs.Type != nil:
									r.bindings[name.Name] = vs.Type
								case j < len(vs.Values):
									if t := literalType(vs.Values[j]); t != nil {
										r.bindings[name.Name] = t
									} else {
										delete(r.bindings, name.Name)
									}
								}
							}
						}
					}
				case *ast.AssignStmt:
					if x.Tok == token.DEFINE {
						for j, lhs := range x.Lhs {
							id, ok := lhs.(*ast.Ident)
							if !ok {
								continue
							}
							if j < len(x.Rhs) && len(x.Lhs) == len(x.Rhs) {
								if t := literalType(x.Rhs[j]); t != nil {
									r.bindings[id.Name] = t
									continue
								}
							}
							delete(r.bindings, id.Name)
						}
					}
					if x.Tok != token.DEFINE {
						for _, lhs := range x.Lhs {
							classify(r, lhs, p.Field, owner, fset, filePath, dir, &sites, &subjects, &outside, &unresolved)
						}
					}
				case *ast.IncDecStmt:
					classify(r, x.X, p.Field, owner, fset, filePath, dir, &sites, &subjects, &outside, &unresolved)
				case *ast.UnaryExpr:
					if x.Op == token.AND {
						classify(r, x.X, p.Field, owner, fset, filePath, dir, &sites, &subjects, &outside, &unresolved)
					}
				}
				return true
			})
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

// classify decides what one written expression is: a write to the subject, a
// write to some other type's field of the same name, or a write this
// derivation cannot bind.
func classify(r *resolver, e ast.Expr, field string, owner typeRef, fset *token.FileSet, filePath, dir string,
	sites *int, subjects *[]Subject, outside, unresolved *[]string) {
	sel, ok := unparen(e).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != field {
		return
	}
	pos := fset.Position(sel.Pos())
	ref, bound := r.typeOf(sel.X)
	switch {
	case !bound:
		*unresolved = append(*unresolved, fmt.Sprintf("%s:%d (receiver %s not bound)", filePath, pos.Line, exprString(sel.X)))
	case ref == owner:
		*sites++
		*subjects = append(*subjects, Subject{File: filePath, Line: pos.Line, Entity: owner.name + "." + field, Role: "mutation-site"})
		if dir != owner.dir {
			*outside = append(*outside, fmt.Sprintf("%s:%d in %s", filePath, pos.Line, dir))
		}
	}
}

// resolver binds expressions to struct types from what was parsed.
type resolver struct {
	structs    map[typeRef]*ast.StructType
	imports    map[string]string // alias -> import path
	modulePath string
	dir        string // directory of the file being read
	bindings   map[string]ast.Expr
}

// typeOf binds an expression to a struct type, or reports that it cannot.
func (r *resolver) typeOf(e ast.Expr) (typeRef, bool) {
	switch x := unparen(e).(type) {
	case *ast.Ident:
		t, ok := r.bindings[x.Name]
		if !ok {
			return typeRef{}, false
		}
		return r.typeExpr(t)
	case *ast.SelectorExpr:
		base, ok := r.typeOf(x.X)
		if !ok {
			return typeRef{}, false
		}
		st, ok := r.structs[base]
		if !ok {
			return typeRef{}, false
		}
		for _, fld := range st.Fields.List {
			for _, n := range fld.Names {
				if n.Name == x.Sel.Name {
					return r.typeExprIn(fld.Type, base.dir)
				}
			}
		}
		return typeRef{}, false
	case *ast.StarExpr:
		return r.typeOf(x.X)
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return r.typeOf(x.X)
		}
	case *ast.IndexExpr:
		base, ok := r.typeOf(x.X)
		_ = base
		if !ok {
			return typeRef{}, false
		}
		return typeRef{}, false
	case *ast.CompositeLit:
		return r.typeExpr(x.Type)
	}
	return typeRef{}, false
}

// typeExpr binds a type expression written in the current file.
func (r *resolver) typeExpr(t ast.Expr) (typeRef, bool) { return r.typeExprIn(t, r.dir) }

// typeExprIn binds a type expression as written in a file of directory dir.
// A qualified name resolves through the imports of the CURRENT file, which is
// an approximation stated in Limits when the chain crosses files.
func (r *resolver) typeExprIn(t ast.Expr, dir string) (typeRef, bool) {
	switch x := t.(type) {
	case *ast.StarExpr:
		return r.typeExprIn(x.X, dir)
	case *ast.ParenExpr:
		return r.typeExprIn(x.X, dir)
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
		importPath, ok := r.imports[pkg.Name]
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

// modulePathOf reads the module path from the pinned go.mod, or "" when the
// tree has none -- in which case qualified references cannot be bound and say
// so, rather than being guessed from directory names.
func modulePathOf(src PinnedSource) string {
	b, err := src.Read("go.mod")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "module ") {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "module "))
		}
	}
	return ""
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
