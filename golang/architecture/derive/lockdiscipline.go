// SPDX-License-Identifier: AGPL-3.0-only

package derive

// The one registered derivation: does every access to a field occur while a
// named lock is held.
//
// It answers WHETHER, never WHY. That distinction is the entire reason this
// family was chosen: the coverage gap on internal/event/bus.go had an
// architectural fact underneath it, and the two competing explanations of that
// fact — "the lock serializes map access" and "the lock makes close-versus-send
// mutually exclusive" — are BOTH purpose claims. Nothing in this file can tell
// them apart, so it does not try.
//
// # v2: flow-sensitive, bounded interprocedural, and honest about its edge
//
// v1 judged each access by walking the enclosing body top to bottom and
// flipping a boolean on Lock/Unlock. It descended into a compound statement
// that CONTAINED the access and applied every lock verb in that statement's
// body, even when the access sat in the condition before the body ran; it did
// not know select or switch existed, so an access inside a case was judged by
// the state before the select; and it had no notion of a helper that is only
// ever called with the lock held. On golang/sync's semaphore.Weighted.cur --
// under mu at every access, on inspection -- it reported six counterexamples.
//
// v2 computes the lock state AT THE ACCESS along the path that reaches it:
//
//   - a compound statement before the access contributes the state on every
//     path that falls through it; paths that terminate (return, panic) do not
//     count, and paths that disagree make the state UNRESOLVED.
//   - an access inside if/select/switch/for is judged by the state on entry to
//     its own branch, plus the statements of that branch before it.
//   - a loop whose body changes lock state across an iteration is UNRESOLVED
//     for accesses inside it; a loop that restores state is followed normally.
//   - an access in a method that never acquires the lock itself is judged by
//     its CALLERS: every call site within the package is analysed with the
//     same rules, to a bounded depth. All held → held. Any provably unheld →
//     a counterexample at that call site. Anything else → UNRESOLVED.
//
// # What resolves to UNRESOLVED rather than to either answer
//
// A stored or deferred closure; an access through an alias or a copy of the
// receiver; a helper that is exported, called through a method value, called
// from a goroutine, or reached through more calls than the bound allows; a
// lock verb inside an expression this reader does not model. Each is a way a
// real counterexample could hide, so none may be DERIVED; each is also a way a
// true discipline could be misjudged, so none may be REFUTED.

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
func (lockDiscipline) Version() string { return "v2" }

// Limits is what this derivation cannot observe. Every item is a way a real
// counterexample could exist and go unseen -- and, symmetrically, a way a true
// discipline could go unproven. Each resolves to UNRESOLVED.
func (lockDiscipline) Limits() []string {
	return []string{
		"access through an alias or a local copy of the receiver",
		"access inside a closure that is stored, deferred, or run as a goroutine",
		"a helper reachable from outside the package, through a method value, or beyond the call-depth bound",
		"lock state carried across loop iterations when the body changes it",
		"a lock acquired or released inside an expression rather than as a statement",
		"access from a file outside the package directory read",
		"access through reflection, embedding, or an interface method set",
	}
}

func (lockDiscipline) Applies(p Proposition) bool {
	return p.Kind == KindFieldAccessUnderLock &&
		strings.TrimSpace(p.Dir) != "" && strings.TrimSpace(p.Type) != "" &&
		strings.TrimSpace(p.Field) != "" && strings.TrimSpace(p.Lock) != ""
}

// callDepth bounds how far caller context is followed. Two is enough for
// "method → helper → inner helper"; beyond that the reader says UNRESOLVED
// rather than pretending the bound does not exist.
const callDepth = 2

func (l lockDiscipline) Derive(src PinnedSource, p Proposition) Attempt {
	paths, err := src.List(p.Dir)
	if err != nil {
		return Attempt{Outcome: Unknown, Detail: fmt.Sprintf("cannot list %s at the pinned commit: %v", p.Dir, err)}
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
			return Attempt{Outcome: Unknown, Inputs: read, Detail: fmt.Sprintf("cannot read %s at the pinned commit: %v", path, err)}
		}
		f, err := parser.ParseFile(fset, path, b, 0)
		if err != nil {
			return Attempt{Outcome: Unknown, Inputs: read, Detail: fmt.Sprintf("cannot parse %s: %v", path, err)}
		}
		files = append(files, f)
		read = append(read, path)
	}
	sort.Strings(read)
	if len(files) == 0 {
		return Attempt{Outcome: Unknown, Inputs: read, Detail: fmt.Sprintf("no non-test Go files under %s at the pinned commit", p.Dir)}
	}
	if !declaresField(files, p.Type, p.Field) {
		return Attempt{Outcome: Unknown, Inputs: read, Detail: fmt.Sprintf(
			"type %s has no field %s in %s; the proposition is about something that is not there", p.Type, p.Field, p.Dir)}
	}
	if !declaresField(files, p.Type, p.Lock) {
		return Attempt{Outcome: Unknown, Inputs: read, Detail: fmt.Sprintf("type %s has no field %s to be held", p.Type, p.Lock)}
	}

	// Subjects: the locations the proposition is ABOUT. Built from the proof as
	// it is constructed, never from the file list -- that conflation is the bug
	// subjects replaced.
	var subjects []Subject
	if decl := declarationSite(fset, files, p.Type, p.Field); decl != nil {
		decl.Role = "field-declaration"
		subjects = append(subjects, *decl)
	}
	if decl := declarationSite(fset, files, p.Type, p.Lock); decl != nil {
		decl.Role = "lock-declaration"
		subjects = append(subjects, *decl)
	}

	an := &analysis{fset: fset, files: files, typ: p.Type, field: p.Field, lock: p.Lock}
	an.index()

	var refuted, unresolved []string
	accesses := 0
	for _, fn := range an.funcs {
		recv := fn.recv
		ast.Inspect(fn.decl.Body, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil || sel.Sel.Name != p.Field {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			accesses++
			where := fset.Position(sel.Pos())
			subjects = append(subjects, Subject{File: where.Filename, Line: where.Line,
				Entity: p.Type + "." + p.Field, Role: "access-site"})
			if recv == "" || ident.Name != recv {
				// Not through the method's own receiver: an alias, a copy, or a
				// plain function reaching in. Whoever owns that value may or may
				// not hold the lock, and this reader cannot follow it.
				unresolved = append(unresolved, fmt.Sprintf(
					"%s accesses %s.%s not through a receiver of %s (%s)",
					fn.decl.Name.Name, ident.Name, p.Field, p.Type, where))
				return true
			}
			st, why := an.stateAt(fn, sel.Pos(), 0, map[string]bool{})
			switch st {
			case held:
			case unheld:
				refuted = append(refuted, fmt.Sprintf("%s accesses %s.%s while %s.%s is not held (%s)%s",
					fn.decl.Name.Name, recv, p.Field, recv, p.Lock, where, suffix(why)))
			default:
				unresolved = append(unresolved, fmt.Sprintf("%s accesses %s.%s and this reader cannot establish the lock state (%s)%s",
					fn.decl.Name.Name, recv, p.Field, where, suffix(why)))
			}
			return true
		})
	}
	if accesses == 0 {
		return Attempt{Outcome: Unknown, Inputs: read, Subjects: subjects, Detail: fmt.Sprintf(
			"no access to %s.%s found in %s; nothing to establish", p.Type, p.Field, p.Dir)}
	}
	if len(refuted) != 0 {
		sort.Strings(refuted)
		return Attempt{Outcome: Refuted, Inputs: read, Subjects: subjects, Detail: fmt.Sprintf(
			"%d of %d access(es) occur while %s is not held: %s",
			len(refuted), accesses, p.Lock, strings.Join(refuted, "; "))}
	}
	if len(unresolved) != 0 {
		sort.Strings(unresolved)
		return Attempt{Outcome: Unresolved, Inputs: read, Subjects: subjects, Detail: fmt.Sprintf(
			"could not establish the lock state for %d of %d access(es); no counterexample found: %s",
			len(unresolved), accesses, strings.Join(unresolved, "; "))}
	}
	return Attempt{Outcome: Derived, Inputs: read, Subjects: subjects, Detail: fmt.Sprintf(
		"all %d access(es) to %s.%s occur while %s.%s is held, across %d subject file(s) "+
			"(%d file(s) were read to compute it)",
		accesses, p.Type, p.Field, p.Type, p.Lock, len(subjectFiles(subjects)), len(read))}
}

func suffix(why string) string {
	if why == "" {
		return ""
	}
	return ": " + why
}

// lockState is the reader's answer about one program point.
type lockState int

const (
	unheld lockState = iota
	held
	unresolved
)

func (s lockState) String() string {
	switch s {
	case held:
		return "held"
	case unheld:
		return "not held"
	}
	return "unresolved"
}

// fnInfo is one function or method in the package, with the receiver name
// when it is a method of the proposition's type.
type fnInfo struct {
	decl *ast.FuncDecl
	recv string
	// acquires says the body contains an acquisition of recv.lock as a
	// statement. A body that never acquires is a helper judged by its callers.
	acquires bool
}

type analysis struct {
	fset             *token.FileSet
	files            []*ast.File
	typ, field, lock string
	funcs            []*fnInfo
	// byName maps a method name on the type to its info.
	byName map[string]*fnInfo
}

func (a *analysis) index() {
	a.byName = map[string]*fnInfo{}
	for _, f := range a.files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			info := &fnInfo{decl: fn, recv: receiverName(fn, a.typ)}
			if info.recv != "" {
				info.acquires = containsLockVerb(fn.Body, info.recv, a.lock, "acquire")
				a.byName[fn.Name.Name] = info
			}
			a.funcs = append(a.funcs, info)
		}
	}
}

// stateAt is the lock state of recv.lock at pos inside fn.
//
// If fn never acquires the lock itself, the state on entry is what its callers
// provide, resolved to the bound; otherwise entry is unheld and the body
// decides.
func (a *analysis) stateAt(fn *fnInfo, pos token.Pos, depth int, visiting map[string]bool) (lockState, string) {
	// A closure has its own life. Immediately invoked, it runs here and
	// inherits the state at the invoking statement; stored, deferred, or
	// started as a goroutine, it runs later and this reader cannot say when.
	if lit := enclosingFuncLit(fn.decl.Body, pos); lit != nil {
		if call := immediateInvocation(fn.decl.Body, lit); call != nil {
			// The outer state is read at the invocation's closing paren, which
			// lies OUTSIDE the literal. Reading it at call.Pos() -- the same
			// position as the literal's own start -- re-entered this closure
			// and recursed without bound on singleflight's doCall, whose
			// body is an immediately-invoked closure containing a deferred one.
			outer, why := a.stateAt(fn, call.Rparen, depth, visiting)
			if outer == unresolved {
				return outer, why
			}
			// Walked from the outer state rather than assumed: a closure that
			// locks for itself establishes the state whatever the caller held.
			st, w := walkTo(lit.Body.List, pos, outer, fn.recv, a.lock)
			return st, w
		}
		// Not invoked in place: it runs later, under whatever lock state the
		// world has then. The closure's OWN locking still counts -- a body that
		// takes mu before the access has established the state itself -- so
		// it is walked from an unknown entry rather than dismissed.
		st, w := walkTo(lit.Body.List, pos, unresolved, fn.recv, a.lock)
		if st == unresolved && w == "" {
			w = "inside a closure that is stored, deferred, or run as a goroutine, and the closure does not acquire the lock itself"
		}
		return st, w
	}
	entry := unheld
	why := ""
	if !fn.acquires {
		entry, why = a.callerContext(fn, depth, visiting)
		if entry != held {
			return entry, why
		}
	}
	st, w := walkTo(fn.decl.Body.List, pos, entry, fn.recv, a.lock)
	if w != "" {
		why = w
	}
	return st, why
}

// callerContext is the lock state a helper may assume on entry, from every
// call site this reader can see.
func (a *analysis) callerContext(fn *fnInfo, depth int, visiting map[string]bool) (lockState, string) {
	name := fn.decl.Name.Name
	if ast.IsExported(name) {
		// Not a boundary: a counterexample. The type's exported surface lets
		// any caller reach this access without the lock, and nothing in the
		// package can prevent that. Whether some in-package caller happens
		// to hold it does not change what the API permits.
		return unheld, "exported and never acquires the lock itself, so the type's API permits an unlocked call"
	}
	if depth >= callDepth {
		return unresolved, fmt.Sprintf("caller context deeper than %d calls", callDepth)
	}
	if visiting[name] {
		return unresolved, "recursive caller context"
	}
	visiting[name] = true
	defer delete(visiting, name)

	sites := 0
	for _, caller := range a.funcs {
		var result lockState = held
		var why string
		stop := false
		later := laterCalls(caller.decl.Body)
		ast.Inspect(caller.decl.Body, func(n ast.Node) bool {
			if stop {
				return false
			}
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil || sel.Sel.Name != name {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			if caller.recv == "" {
				// A package-level function reaching in through some variable.
				// It is a call site -- skipping it let an unlocked call from a
				// plain function count as no call at all -- and this reader
				// cannot know what lock that variable's owner holds.
				if !isCalled(caller.decl.Body, sel) {
					return true
				}
				sites++
				result, why, stop = unresolved, fmt.Sprintf("called from package-level function %s, whose lock state for %s this reader cannot follow",
					caller.decl.Name.Name, id.Name), true
				return false
			}
			if id.Name != caller.recv {
				return true
			}
			if !isCalled(caller.decl.Body, sel) {
				result, why, stop = unresolved, "used as a method value rather than called", true
				return false
			}
			sites++
			// `go s.helper()` runs on another goroutine, which holds no lock
			// of the caller's; that is a counterexample. `defer s.helper()`
			// runs at function exit, after whatever unlocks precede the
			// return, in an order this reader does not model.
			switch later[selCall(caller.decl.Body, sel)] {
			case "go":
				result, why, stop = unheld, fmt.Sprintf("started with `go` from %s: a new goroutine holds none of the caller's locks (%s)",
					caller.decl.Name.Name, a.fset.Position(sel.Pos())), true
				return false
			case "defer":
				result, why, stop = unresolved, fmt.Sprintf("deferred from %s: runs at function exit, after unlocks this reader does not order",
					caller.decl.Name.Name), true
				return false
			}
			st, w := a.stateAt(caller, sel.Pos(), depth+1, visiting)
			switch st {
			case held:
			case unheld:
				result, why, stop = unheld, fmt.Sprintf("called from %s without the lock (%s)",
					caller.decl.Name.Name, a.fset.Position(sel.Pos())), true
				return false
			default:
				result, why, stop = unresolved, fmt.Sprintf("caller %s: %s", caller.decl.Name.Name, w), true
				return false
			}
			return true
		})
		if result != held {
			return result, why
		}
	}
	if sites == 0 {
		return unresolved, "never acquires the lock itself and no call site was found in the package"
	}
	return held, ""
}

// walkTo computes the state at pos by executing stmts in order until the one
// that contains pos, then descending into it.
func walkTo(stmts []ast.Stmt, pos token.Pos, state lockState, recv, lock string) (lockState, string) {
	entryUnknown := state == unresolved
	for _, st := range stmts {
		if hasPos(st, pos) {
			return stateIn(st, pos, state, recv, lock)
		}
		var why string
		state, _, why = effect(st, state, recv, lock)
		if state == unresolved && !entryUnknown {
			return unresolved, why
		}
	}
	return state, ""
}

// stateIn descends into the statement containing pos.
func stateIn(st ast.Stmt, pos token.Pos, state lockState, recv, lock string) (lockState, string) {
	switch s := st.(type) {
	case *ast.BlockStmt:
		return walkTo(s.List, pos, state, recv, lock)
	case *ast.LabeledStmt:
		return stateIn(s.Stmt, pos, state, recv, lock)
	case *ast.IfStmt:
		if s.Init != nil && hasPos(s.Init, pos) {
			return state, ""
		}
		if s.Init != nil {
			var why string
			state, _, why = effect(s.Init, state, recv, lock)
			if state == unresolved {
				return unresolved, why
			}
		}
		if hasPos(s.Cond, pos) {
			return state, ""
		}
		if s.Body != nil && hasPos(s.Body, pos) {
			return walkTo(s.Body.List, pos, state, recv, lock)
		}
		if s.Else != nil && hasPos(s.Else, pos) {
			return stateIn(s.Else, pos, state, recv, lock)
		}
	case *ast.SwitchStmt:
		if s.Init != nil {
			var why string
			state, _, why = effect(s.Init, state, recv, lock)
			if state == unresolved {
				return unresolved, why
			}
		}
		if s.Tag != nil && hasPos(s.Tag, pos) {
			return state, ""
		}
		return clauseState(s.Body, pos, state, recv, lock)
	case *ast.TypeSwitchStmt:
		if s.Assign != nil && hasPos(s.Assign, pos) {
			return state, ""
		}
		return clauseState(s.Body, pos, state, recv, lock)
	case *ast.SelectStmt:
		return clauseState(s.Body, pos, state, recv, lock)
	case *ast.ForStmt:
		if (s.Init != nil && hasPos(s.Init, pos)) || (s.Cond != nil && hasPos(s.Cond, pos)) || (s.Post != nil && hasPos(s.Post, pos)) {
			return state, ""
		}
		return loopBody(s.Body, pos, state, recv, lock)
	case *ast.RangeStmt:
		if hasPos(s.X, pos) || (s.Key != nil && hasPos(s.Key, pos)) || (s.Value != nil && hasPos(s.Value, pos)) {
			return state, ""
		}
		return loopBody(s.Body, pos, state, recv, lock)
	case *ast.GoStmt, *ast.DeferStmt:
		// pos is in the call's arguments, evaluated now, unless it is inside a
		// func literal -- which stateAt has already separated.
		return state, ""
	}
	// A simple statement: pos is in an expression evaluated at this point.
	// A lock verb hidden inside that same expression is not modelled.
	if containsLockVerb(st, recv, lock, "any") {
		return unresolved, "a lock verb inside an expression rather than as a statement"
	}
	return state, ""
}

// clauseState judges pos inside one clause of a switch or select. Clauses are
// independent paths: each begins with the state on entry to the statement.
func clauseState(body *ast.BlockStmt, pos token.Pos, state lockState, recv, lock string) (lockState, string) {
	if body == nil {
		return state, ""
	}
	for _, cl := range body.List {
		if !hasPos(cl, pos) {
			continue
		}
		switch c := cl.(type) {
		case *ast.CaseClause:
			for _, e := range c.List {
				if hasPos(e, pos) {
					return state, ""
				}
			}
			return walkTo(c.Body, pos, state, recv, lock)
		case *ast.CommClause:
			if c.Comm != nil && hasPos(c.Comm, pos) {
				return state, ""
			}
			return walkTo(c.Body, pos, state, recv, lock)
		}
	}
	return state, ""
}

// loopBody judges pos inside a loop. If one pass through the body changes the
// lock state, the state on the next iteration's entry differs from this one's
// and this reader does not model that.
func loopBody(body *ast.BlockStmt, pos token.Pos, state lockState, recv, lock string) (lockState, string) {
	if body == nil {
		return state, ""
	}
	end, _, why := effectList(body.List, state, recv, lock)
	if end == unresolved {
		return unresolved, why
	}
	if end != state {
		return unresolved, "lock state changes across loop iterations"
	}
	return walkTo(body.List, pos, state, recv, lock)
}

// effect is the state after a statement that does NOT contain the access, on
// every path that falls through it. terminated reports that no path falls
// through (the statement always returns or panics).
func effect(st ast.Stmt, state lockState, recv, lock string) (after lockState, terminated bool, why string) {
	// An unknown entry state is not sticky: an explicit acquire or release
	// in the body establishes the state from that point on.
	switch s := st.(type) {
	case *ast.ExprStmt, *ast.DeferStmt:
		switch lockVerb(st, recv, lock) {
		case "acquire":
			return held, false, ""
		case "release":
			return unheld, false, ""
		case "defer-release":
			return state, false, ""
		}
		if isPanic(st) {
			return state, true, ""
		}
		if containsLockVerb(st, recv, lock, "any") {
			return unresolved, false, "a lock verb inside an expression rather than as a statement"
		}
		return state, false, ""
	case *ast.ReturnStmt:
		return state, true, ""
	case *ast.BlockStmt:
		return effectList(s.List, state, recv, lock)
	case *ast.LabeledStmt:
		return effect(s.Stmt, state, recv, lock)
	case *ast.IfStmt:
		if s.Init != nil {
			var w string
			state, _, w = effect(s.Init, state, recv, lock)
			if state == unresolved {
				return unresolved, false, w
			}
		}
		var branches []lockState
		var allTerminate = true
		if s.Body != nil {
			b, term, w := effectList(s.Body.List, state, recv, lock)
			if b == unresolved {
				return unresolved, false, w
			}
			if !term {
				branches = append(branches, b)
				allTerminate = false
			}
		}
		if s.Else != nil {
			b, term, w := effect(s.Else, state, recv, lock)
			if b == unresolved {
				return unresolved, false, w
			}
			if !term {
				branches = append(branches, b)
				allTerminate = false
			}
		} else {
			branches = append(branches, state)
			allTerminate = false
		}
		return merge(branches, state, allTerminate)
	case *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
		var body *ast.BlockStmt
		hasDefault := false
		switch x := s.(type) {
		case *ast.SwitchStmt:
			if x.Init != nil {
				var w string
				state, _, w = effect(x.Init, state, recv, lock)
				if state == unresolved {
					return unresolved, false, w
				}
			}
			body = x.Body
		case *ast.TypeSwitchStmt:
			body = x.Body
		case *ast.SelectStmt:
			body = x.Body
			hasDefault = true // a select always takes exactly one case
		}
		var branches []lockState
		allTerminate := true
		if body != nil {
			for _, cl := range body.List {
				var list []ast.Stmt
				switch c := cl.(type) {
				case *ast.CaseClause:
					if c.List == nil {
						hasDefault = true
					}
					list = c.Body
				case *ast.CommClause:
					list = c.Body
				}
				b, term, w := effectList(list, state, recv, lock)
				if b == unresolved {
					return unresolved, false, w
				}
				if !term {
					branches = append(branches, b)
					allTerminate = false
				}
			}
		}
		if !hasDefault {
			branches = append(branches, state) // no case matched: fall through unchanged
			allTerminate = false
		}
		return merge(branches, state, allTerminate)
	case *ast.ForStmt, *ast.RangeStmt:
		var body *ast.BlockStmt
		if f, ok := s.(*ast.ForStmt); ok {
			body = f.Body
		} else {
			body = s.(*ast.RangeStmt).Body
		}
		if body == nil {
			return state, false, ""
		}
		end, _, w := effectList(body.List, state, recv, lock)
		if end == unresolved {
			return unresolved, false, w
		}
		if end != state {
			return unresolved, false, "lock state changes across loop iterations"
		}
		return state, false, ""
	case *ast.GoStmt:
		return state, false, ""
	}
	if containsLockVerb(st, recv, lock, "any") {
		return unresolved, false, "a lock verb in a statement form this reader does not model"
	}
	return state, false, ""
}

func effectList(stmts []ast.Stmt, state lockState, recv, lock string) (lockState, bool, string) {
	for _, st := range stmts {
		var term bool
		var why string
		state, term, why = effect(st, state, recv, lock)
		if state == unresolved {
			return unresolved, false, why
		}
		if term {
			return state, true, ""
		}
	}
	return state, false, ""
}

// merge combines the fall-through states of a statement's branches.
func merge(branches []lockState, entry lockState, allTerminate bool) (lockState, bool, string) {
	if allTerminate {
		return entry, true, ""
	}
	if len(branches) == 0 {
		return entry, false, ""
	}
	first := branches[0]
	for _, b := range branches[1:] {
		if b != first {
			return unresolved, false, "branches leave the lock in different states and both fall through"
		}
	}
	return first, false, ""
}

func isPanic(st ast.Stmt) bool {
	es, ok := st.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := call.Fun.(*ast.Ident)
	return ok && id.Name == "panic"
}

// containsLockVerb reports a Lock/Unlock on recv.lock anywhere inside n.
// verb is "acquire", "release" or "any".
func containsLockVerb(n ast.Node, recv, lock, verb string) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil {
			return true
		}
		inner, ok := sel.X.(*ast.SelectorExpr)
		if !ok || inner.Sel == nil || inner.Sel.Name != lock {
			return true
		}
		id, ok := inner.X.(*ast.Ident)
		if !ok || id.Name != recv {
			return true
		}
		switch sel.Sel.Name {
		case "Lock", "RLock":
			found = verb == "acquire" || verb == "any"
		case "Unlock", "RUnlock":
			found = verb == "release" || verb == "any"
		}
		return !found
	})
	return found
}

// isCalled reports whether sel is the callee of a call expression, as opposed
// to a method value being taken.
func isCalled(root ast.Node, sel *ast.SelectorExpr) bool {
	called := false
	ast.Inspect(root, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if ok && call.Fun == sel {
			called = true
		}
		return !called
	})
	return called
}

// immediateInvocation returns the call expression invoking lit in place, if
// any: func(){...}() runs now; anything else runs later.
func immediateInvocation(root ast.Node, lit *ast.FuncLit) *ast.CallExpr {
	// A call that is the operand of `defer` or `go` runs later, whatever its
	// syntax looks like, and must not be read as running here.
	later := map[*ast.CallExpr]bool{}
	ast.Inspect(root, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.DeferStmt:
			later[s.Call] = true
		case *ast.GoStmt:
			later[s.Call] = true
		}
		return true
	})
	var found *ast.CallExpr
	ast.Inspect(root, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if ok && call.Fun == lit && !later[call] {
			found = call
		}
		return found == nil
	})
	return found
}

// subjectFiles are the distinct files the subjects live in.
func subjectFiles(subjects []Subject) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range subjects {
		if s.File != "" && !seen[s.File] {
			seen[s.File] = true
			out = append(out, s.File)
		}
	}
	sort.Strings(out)
	return out
}

// declarationSite locates where a field is declared, so the proposition's own
// entities are part of what it covers.
func declarationSite(fset *token.FileSet, files []*ast.File, typeName, field string) *Subject {
	var found *Subject
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
					if name.Name == field && found == nil {
						pos := fset.Position(name.Pos())
						found = &Subject{File: pos.Filename, Line: pos.Line,
							Entity: typeName + "." + field}
					}
				}
			}
			return true
		})
	}
	return found
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

// laterCalls maps each call that is the operand of `go` or `defer` to which.
func laterCalls(root ast.Node) map[*ast.CallExpr]string {
	out := map[*ast.CallExpr]string{}
	ast.Inspect(root, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.GoStmt:
			out[s.Call] = "go"
		case *ast.DeferStmt:
			out[s.Call] = "defer"
		}
		return true
	})
	return out
}

// selCall returns the call expression whose callee is sel, if any.
func selCall(root ast.Node, sel *ast.SelectorExpr) *ast.CallExpr {
	var found *ast.CallExpr
	ast.Inspect(root, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if ok && call.Fun == sel {
			found = call
		}
		return found == nil
	})
	return found
}
