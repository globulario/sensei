// SPDX-License-Identifier: AGPL-3.0-only

package derive

// The one registered derivation: does every access to a field occur while a
// named lock is held.
//
// It answers WHETHER, never WHY. That distinction is the entire reason this
// family was chosen: the coverage gap on internal/event/bus.go had an
// architectural fact underneath it, and the two competing explanations of that
// fact — "the lock serializes map access" and "the lock makes close-versus-send
// mutually exclusive" — are BOTH purpose claims. One is right and one is wrong,
// and nothing in this file can tell them apart, so it does not try. It
// establishes the discipline and leaves the purpose unestablished.
//
// # What it is conservative about
//
// A lock discipline is a dataflow property and this is a syntactic
// approximation, so every uncertainty resolves toward NOT_DERIVED or UNKNOWN
// rather than toward DERIVED. Specifically:
//
//   - only accesses through a method receiver of the named type are considered
//     protected-able; an access from anywhere else is a counterexample, because
//     a caller outside the type cannot be assumed to hold the lock.
//   - the lock must be acquired earlier in the same function body, at statement
//     level, and not released before the access.
//   - `defer` of the unlock is treated as releasing at function end, which is
//     what it does.
//   - a composite literal that initialises the field at construction is not an
//     access: nothing else can observe the value yet.
//
// A property this cannot see is a counterexample it will miss. That is why the
// outcome is scoped to the files it actually read, and why Established.Scope
// refuses to generalise beyond them.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

type lockDiscipline struct{}

func (lockDiscipline) ID() string      { return "derive.field_access_under_lock" }
func (lockDiscipline) Version() string { return "v1" }

// Limits is what this derivation cannot observe. Every item is a way a real
// counterexample could exist and go unseen, which is why a DERIVED result is
// scoped to the files read rather than stated as a property of the program.
func (lockDiscipline) Limits() []string {
	return []string{
		"access through an alias or a local copy of the receiver",
		"access from a helper the receiver is passed to",
		"access from a goroutine whose lock state this cannot follow",
		"access from a file outside the package directory read",
		"access through reflection, embedding, or an interface method set",
		"lock acquisition or release performed by a called function rather than in the same body",
	}
}

func (lockDiscipline) Applies(p Proposition) bool {
	return p.Kind == KindFieldAccessUnderLock &&
		strings.TrimSpace(p.Dir) != "" && strings.TrimSpace(p.Type) != "" &&
		strings.TrimSpace(p.Field) != "" && strings.TrimSpace(p.Lock) != ""
}

func (l lockDiscipline) Derive(src PinnedSource, p Proposition) (Outcome, []string, string) {
	paths, err := src.List(p.Dir)
	if err != nil {
		return Unknown, nil, fmt.Sprintf("cannot list %s at the pinned commit: %v", p.Dir, err)
	}
	var read []string
	fset := token.NewFileSet()
	var files []*ast.File
	for _, path := range paths {
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		b, err := src.Read(path)
		if err != nil {
			return Unknown, read, fmt.Sprintf("cannot read %s at the pinned commit: %v", path, err)
		}
		f, err := parser.ParseFile(fset, path, b, 0)
		if err != nil {
			return Unknown, read, fmt.Sprintf("cannot parse %s: %v", path, err)
		}
		files = append(files, f)
		read = append(read, path)
	}
	sort.Strings(read)
	if len(files) == 0 {
		return Unknown, read, fmt.Sprintf("no non-test Go files under %s at the pinned commit", p.Dir)
	}
	if !declaresField(files, p.Type, p.Field) {
		return Unknown, read, fmt.Sprintf("type %s has no field %s in %s; the proposition is about something that is not there",
			p.Type, p.Field, p.Dir)
	}
	if !declaresField(files, p.Type, p.Lock) {
		return Unknown, read, fmt.Sprintf("type %s has no field %s to be held", p.Type, p.Lock)
	}

	var counterexamples []string
	accesses := 0
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			recv := receiverName(fn, p.Type)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || sel.Sel == nil || sel.Sel.Name != p.Field {
					return true
				}
				ident, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				// An access from outside a method of the type cannot be assumed
				// protected: the caller holds no lock this proposition names.
				if recv == "" || ident.Name != recv {
					accesses++
					counterexamples = append(counterexamples, fmt.Sprintf(
						"%s accesses %s.%s outside a method of %s (%s)",
						fn.Name.Name, ident.Name, p.Field, p.Type, fset.Position(sel.Pos())))
					return true
				}
				accesses++
				if !heldAt(fn.Body, recv, p.Lock, sel.Pos()) {
					counterexamples = append(counterexamples, fmt.Sprintf(
						"%s accesses %s.%s without holding %s.%s (%s)",
						fn.Name.Name, recv, p.Field, recv, p.Lock, fset.Position(sel.Pos())))
				}
				return true
			})
		}
	}
	if accesses == 0 {
		return Unknown, read, fmt.Sprintf("no access to %s.%s found in %s; nothing to establish",
			p.Type, p.Field, p.Dir)
	}
	if len(counterexamples) != 0 {
		sort.Strings(counterexamples)
		return NotDerived, read, fmt.Sprintf("%d of %d access(es) are not under %s: %s",
			len(counterexamples), accesses, p.Lock, strings.Join(counterexamples, "; "))
	}
	return Derived, read, fmt.Sprintf("all %d access(es) to %s.%s occur while %s.%s is held, across %d file(s)",
		accesses, p.Type, p.Field, p.Type, p.Lock, len(read))
}

func declaresField(files []*ast.File, typeName, field string) bool {
	found := false
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok || ts.Name == nil || ts.Name.Name != typeName {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				return true
			}
			for _, fl := range st.Fields.List {
				for _, name := range fl.Names {
					if name.Name == field {
						found = true
					}
				}
			}
			return true
		})
	}
	return found
}

// receiverName returns the receiver identifier when fn is a method on typeName.
func receiverName(fn *ast.FuncDecl, typeName string) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	f := fn.Recv.List[0]
	t := f.Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	id, ok := t.(*ast.Ident)
	if !ok || id.Name != typeName || len(f.Names) == 0 {
		return ""
	}
	return f.Names[0].Name
}

// heldAt reports whether recv.lock is held at pos, by walking statements in
// order and tracking acquisition and release.
//
// Conservative by construction: anything it cannot follow leaves the lock
// un-held, so an access it does not understand becomes a counterexample rather
// than a silent pass.
func heldAt(body *ast.BlockStmt, recv, lock string, pos token.Pos) bool {
	held := false
	var walk func(stmts []ast.Stmt) (found bool)
	walk = func(stmts []ast.Stmt) bool {
		for _, st := range stmts {
			// A nested function literal gets its own reasoning: the lock state
			// of the enclosing body does not carry into a closure that may run
			// later. It is analysed as its own body.
			if hasPos(st, pos) {
				switch s := st.(type) {
				case *ast.BlockStmt:
					if walk(s.List) {
						return true
					}
				case *ast.IfStmt:
					if s.Body != nil && walk(s.Body.List) {
						return true
					}
				case *ast.ForStmt:
					if s.Body != nil && walk(s.Body.List) {
						return true
					}
				case *ast.RangeStmt:
					if s.Body != nil && walk(s.Body.List) {
						return true
					}
				}
				return true
			}
			switch verb := lockVerb(st, recv, lock); verb {
			case "acquire":
				held = true
			case "release":
				held = false
			case "defer-release":
				// released at function end, so still held here
			}
		}
		return false
	}
	// Closures: a func literal containing pos is analysed on its own, because a
	// closure stored for later cannot inherit the caller's lock state.
	if lit := enclosingFuncLit(body, pos); lit != nil && lit.Body != nil {
		held = false
		walk(lit.Body.List)
		return held
	}
	walk(body.List)
	return held
}

func hasPos(n ast.Node, pos token.Pos) bool {
	return n != nil && n.Pos() <= pos && pos <= n.End()
}

func enclosingFuncLit(root ast.Node, pos token.Pos) *ast.FuncLit {
	var found *ast.FuncLit
	ast.Inspect(root, func(n ast.Node) bool {
		lit, ok := n.(*ast.FuncLit)
		if ok && hasPos(lit, pos) {
			found = lit
		}
		return true
	})
	return found
}

// lockVerb classifies a statement's effect on recv.lock.
func lockVerb(st ast.Stmt, recv, lock string) string {
	call, deferred := callOf(st)
	if call == nil {
		return ""
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil {
		return ""
	}
	inner, ok := sel.X.(*ast.SelectorExpr)
	if !ok || inner.Sel == nil || inner.Sel.Name != lock {
		return ""
	}
	id, ok := inner.X.(*ast.Ident)
	if !ok || id.Name != recv {
		return ""
	}
	switch sel.Sel.Name {
	case "Lock", "RLock":
		if deferred {
			return ""
		}
		return "acquire"
	case "Unlock", "RUnlock":
		if deferred {
			return "defer-release"
		}
		return "release"
	}
	return ""
}

func callOf(st ast.Stmt) (*ast.CallExpr, bool) {
	switch s := st.(type) {
	case *ast.ExprStmt:
		if c, ok := s.X.(*ast.CallExpr); ok {
			return c, false
		}
	case *ast.DeferStmt:
		return s.Call, true
	}
	return nil, false
}
