// SPDX-License-Identifier: AGPL-3.0-only

package trustkernel

// THE REQUIRED-ARTIFACT CENSUS, across the four packages that read one.
//
// THE INVARIANT:
//
//	Every production read of an artifact whose event contract appears in
//	ledger.requiredEventArtifacts passes through a chain verified by a validator that
//	INVOKES ledger.ValidateTaskEventPayload, before extraction.
//
// WHY THIS EXISTS RATHER THAN A WITNESS ON admission.requiredArtifact ITSELF.
//
// admission.requiredArtifact is the seam that separates a DAMAGED record from a merely
// absent optional one (#354). Disabling it leaves every behavioural witness in this
// package GREEN, and that is not a hole in the witnesses -- it is EQUIVALENCE under the
// current reachable production topology:
//
//   - ledger/verify.go runs the payload validator during VERIFICATION, not only during
//     Append, and reports ledger.payload_schema_invalid;
//   - every validator installed on a required-artifact reading path reaches
//     ledger.ValidateTaskEventPayload, which enforces requiredEventArtifacts;
//   - every production read of a governed artifact happens under such a verification.
//
// So a hand-written artifact-less admission_consumed is refused -- "admission_consumed
// event requires a capability_consumption artifact" -- BEFORE any artifact extraction
// runs, and requiredArtifact never gets the chance to fire.
//
// THE EQUIVALENT MUTANT, RECORDED RATHER THAN DELETED. Disabling requiredArtifact leaves
// every witness in this package green. That is equivalence under the current reachable
// topology, NOT a gap in the witnesses: the guard's missing-artifact branch is
// unreachable exactly while the invariant above holds. AND THIS CENSUS IS WHAT KEEPS IT
// EQUIVALENT. The moment a reader verifies with nil, with a validator that no longer
// delegates, or extracts from a chain nobody verified, requiredArtifact stops being
// equivalent and becomes load-bearing again -- and the census goes red at that edit
// rather than after an incident. An equivalence that nothing watches is an assumption.
//
// FOUR PACKAGES, THREE EXTRACTION SEAMS. The earlier intermediate
// (candidate/trust-kernel-census-admission-only) scanned only the admission directory and
// therefore covered one seam of three while passing. The governed keys are read in:
//
//	admission            latestArtifactFromChain, reached with "capability_consumption"
//	tasksession          decodeGovernedArtifact, reached with "capability_consumption"
//	resultrecording      chainArtifactJSON + direct KeyReceipt reads
//	questiondisposition  direct "result_transition_receipt" and ArtifactKeyReceipt reads
//
// FOUR PROPERTIES A NAIVE WIDENING LOSES, all load-bearing and all measured below:
//
//	WRAPPERS ARE VALID -- INVOKES, NOT IS. recordingPayloadValidator and
//	dispositionPayloadValidator call ledger.ValidateTaskEventPayload and then add stricter
//	rules. Validator IDENTITY does not matter; DELEGATION to the table-carrying validator
//	does. An equality test against ValidateTaskEventPayload fails on both and would be
//	wrong. So a validator qualifies when its body REACHES that name, directly or through a
//	wrapper it calls.
//
//	ANONYMOUS CLOSURES COUNT. tasksession builds stores at control.go, session.go and
//	session.go again with inline func literals. Discovery keyed on named validator
//	functions misses all three silently, and a store is classified on the literal's own
//	body, not skipped for lacking a name.
//
//	STORES ARE BUILT BY FACTORIES. questiondisposition constructs every one of its five
//	stores through newStore(taskDir), which RETURNS ledger.NewStore(...). Discovery that
//	only sees ledger.NewStore bound to a local variable finds zero stores in that package
//	and certifies it.
//
//	VALIDATORS ARE NOT READERS. A validator's own `payload.Artifacts[key]` presence check
//	runs INSIDE verification, over the bytes being verified; it reads no content-addressed
//	artifact off disk. Counting it as a read would condemn
//	questiondisposition.validateDispositionEventPayload -- a false positive, and the first
//	false positive is what gets a census disabled.
//
// BOTH HALVES ARE REQUIRED. A census of constructors alone goes green the moment someone
// adds a direct call to decodeGovernedArtifact, chainArtifactJSON or latestArtifactFromChain
// from an unchecked path:
//
//	(1) every store on a required-artifact reading path installs a validator that invokes
//	    ValidateTaskEventPayload -- none nil, none omitted, none non-delegating;
//	(2) every governed extraction site is reachable ONLY through such a verified path, so
//	    a new direct call from an unchecked route is a census failure NAMING that route.
//
// THE REACHABILITY MODEL, stated so it can be argued with. A VERIFIED ROOT is a function
// that builds a store with a delegating validator and verifies it. In the call graph with
// every verified root's OUTGOING edges cut -- a root absorbs, because everything it calls
// works on the chain it verified -- no function may reach a governed extraction site
// unless it is itself a root or lies below one. That single rule is what makes delegation
// legal and smuggling illegal at the same time: admission.LoadRecordedConsumption reaches
// its extraction only through LoadLatestArtifactOptional, a root, so the cut stops it; a
// function that parses a payload off disk and hands it to chainArtifactJSON reaches the
// site with no root on the path, and is named.
//
// SCOPE BOUNDARY. golang/architecture/completion and golang/architecture/questionresolution
// verify with a NIL validator and then extract governed artifacts. That is a real and
// separate trust-boundary finding, already recorded. It is NOT part of this invariant: the
// keys those paths read (certification_receipt, abandonment, completion) are outside
// requiredEventArtifacts, so they are not evidence about the requiredArtifact equivalence.
//
// Subjects are discovered with go/parser and go/ast over the real package source, in the
// idiom of cmd/awg/graph_reader_census_test.go. A textual count breaks on a rename and
// passes on a comment. Nothing here changes requiredArtifact, ledger/verify.go,
// ledger/event.go or any validator; this file is test-only and edits no production source.
//
// KNOWN LIMITS, stated rather than hidden. The call graph is keyed on unqualified function
// names, so two same-named functions in one package merge; that over-approximates
// reachability, which can only ADD routes to judge, never excuse one. Analysis is
// per-package because every governed extractor except admission's exported loaders is
// package-private, and those loaders verify their own ledger. Statement ORDER inside a
// verified root is not checked: a root that indexed before verifying would pass. Both are
// recorded here rather than papered over.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

// enforcingValidatorName is the ONE function that reads requiredEventArtifacts. A
// validator enforces the table exactly when its body REACHES this name -- invokes, not
// is. Membership, not exclusion: a renamed or invented validator shows up as a gap
// instead of silently certifying one.
const enforcingValidatorName = "ValidateTaskEventPayload"

// censusPackageDirs are the four packages that read a requiredEventArtifacts-governed
// artifact today. The list is the SUBJECT SET, and the census's own floor requires each
// entry to yield subjects -- so a package silently dropped from here fails rather than
// shrinking the claim.
var censusPackageDirs = []string{
	filepath.Join("golang", "architecture", "admission"),
	filepath.Join("golang", "architecture", "tasksession"),
	filepath.Join("golang", "architecture", "resultrecording"),
	filepath.Join("golang", "architecture", "questiondisposition"),
}

// verificationMethodNames are the Store methods that produce a chain the validator has
// already run over. A chain obtained any other way is outside the verified boundary.
var verificationMethodNames = map[string]bool{
	"VerifyChain":    true,
	"VerifyChainCtx": true,
	"Verify":         true,
	"VerifyCtx":      true,
}

// storeFact is one ledger.NewStore construction, and what it was given.
type storeFact struct {
	// VarName is the variable the store was bound to, which is how verification is tied
	// back to THIS store. A store built with a validator and a DIFFERENT store verified
	// must not satisfy a laxer reading.
	VarName string

	// ViaFactory names the function that returned this store, empty when it was
	// constructed inline. questiondisposition builds all five of its stores this way.
	ViaFactory string

	HasValidatorOption bool
	ValidatorExpr      string
	ValidatorIsNil     bool
	EnforcesTable      bool

	// ValidatorIsClosure marks a validator given as an INLINE FUNC LITERAL. It is
	// recorded rather than sniffed out of the rendered expression, because that is the
	// shape name-keyed discovery drops silently and a witness has to be able to count it.
	ValidatorIsClosure bool

	// validatorNode is the validator expression itself, kept so delegation can be
	// judged after the whole package's call graph exists.
	validatorNode ast.Expr

	// VerifiedVia is the verification method called on VarName, empty if none was.
	VerifiedVia string
}

// verified reports whether this store may serve as the source of an extracted artifact.
func (s storeFact) verified() bool {
	return s.HasValidatorOption && !s.ValidatorIsNil && s.EnforcesTable && s.VerifiedVia != ""
}

// describeValidator names the validator short enough to read in a failure, together with
// where the store came from. An inline closure has no name to print, so its parameter list
// stands in for one.
func (s storeFact) describeValidator() string {
	return s.ValidatorExpr + s.origin()
}

// origin names the factory a store came from, empty when it was constructed inline. A
// failure that says only "built with a nil validator" sends the reader to a caller whose
// own source is blameless.
func (s storeFact) origin() string {
	if s.ViaFactory == "" {
		return ""
	}
	return " (via " + s.ViaFactory + ")"
}

// fnFacts is what one function says about its own ledger reading. The questions are kept
// SEPARATE, because "a validator is installed", "a chain was verified here" and "an
// artifact was extracted" are different claims and merging them would erase the
// distinction the invariant rests on.
type fnFacts struct {
	Name  string
	Route string // file.go:Name, for naming a failure

	// Calls is every function/method name called anywhere in the body.
	Calls map[string]bool

	Stores []storeFact

	// ReturnsStore marks a store FACTORY: a function whose result is the store, so the
	// verification happens in its caller.
	ReturnsStore bool

	// IndexedKeys are the artifact keys this body reads by an index whose key expression
	// resolves to a constant string.
	IndexedKeys []string
	// IndexesUnresolvedKey marks a GENERIC extractor: it indexes Artifacts by a key it
	// receives, so what it reads is decided by its callers.
	IndexesUnresolvedKey bool

	// GovernedCallees are the functions this body calls while passing a governed artifact
	// key as a literal argument. That is how a generic extractor becomes a governed one.
	GovernedCallees map[string]bool

	// IsValidatorBody marks a function that runs INSIDE verification. Its Artifacts
	// reads are presence assertions on the bytes under validation, not extractions.
	IsValidatorBody bool
}

// parsedFn is one parsed function declaration and the file it came from.
type parsedFn struct {
	base string
	fn   *ast.FuncDecl
}

// censusAnalysis is one package's discovered facts.
type censusAnalysis struct {
	Dir string
	Fns map[string]*fnFacts

	// Governed is the artifact-key set the ledger table governs, read from the table
	// rather than hand-listed here.
	Governed map[string]bool
}

// names returns every discovered function name, sorted, so every report is stable.
func (a *censusAnalysis) names() []string {
	out := make([]string, 0, len(a.Fns))
	for n := range a.Fns {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// routeOf names a function for a failure message.
func (a *censusAnalysis) routeOf(name string) string {
	if f, ok := a.Fns[name]; ok {
		return f.Route
	}
	return name
}

// analyzePackage discovers the census subjects in a package directory.
//
// Emptiness is RETURNED, not fataled: the census's own floor turns "nothing discovered"
// into a failure, and a witness needs to be able to observe an empty discovery without
// the helper aborting it.
func analyzePackage(t *testing.T, dir string, governed map[string]bool) *censusAnalysis {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return strings.HasSuffix(fi.Name(), ".go") && !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", dir, err)
	}

	a := &censusAnalysis{Dir: dir, Fns: map[string]*fnFacts{}, Governed: governed}

	// PASS ZERO: package-level string constants, so Artifacts[KeyReceipt] resolves to the
	// key it actually names. resultrecording and questiondisposition both index through a
	// constant, and a census that only understood string literals would call those reads
	// generic and stop asking which artifact they touch.
	consts := map[string]string{}
	var decls []parsedFn
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			base := filepath.Base(name)
			for _, d := range file.Decls {
				switch d := d.(type) {
				case *ast.GenDecl:
					collectStringConsts(d, consts)
				case *ast.FuncDecl:
					if d.Body != nil {
						decls = append(decls, parsedFn{base: base, fn: d})
					}
				}
			}
		}
	}

	// PASS ONE: the call graph and the store factories. Factories must be known before
	// any body is classified, because `store := newStore(taskDir)` carries its validator
	// from a function declared elsewhere in the package.
	factories := map[string]storeFact{}
	for _, d := range decls {
		if !returnsStore(d.fn) {
			continue
		}
		for _, s := range inlineStores(d.fn.Body) {
			factories[d.fn.Name.Name] = s
			break
		}
	}

	for _, d := range decls {
		name := d.fn.Name.Name
		f := a.Fns[name]
		if f == nil {
			f = &fnFacts{
				Name:            name,
				Route:           d.base + ":" + name,
				Calls:           map[string]bool{},
				GovernedCallees: map[string]bool{},
			}
			a.Fns[name] = f
		}
		for c := range calledNamesIn(d.fn.Body) {
			f.Calls[c] = true
		}
		f.ReturnsStore = f.ReturnsStore || returnsStore(d.fn)
		f.Stores = append(f.Stores, storesIn(d.fn.Body, factories)...)

		keys, generic := indexedArtifactKeys(d.fn.Body, consts)
		f.IndexedKeys = append(f.IndexedKeys, keys...)
		f.IndexesUnresolvedKey = f.IndexesUnresolvedKey || generic
		for callee := range calleesGivenAGovernedKey(d.fn.Body, consts, governed) {
			f.GovernedCallees[callee] = true
		}
	}

	// PASS ONE AND A HALF: delegation. A validator's route to ValidateTaskEventPayload may
	// cross any function in the package, so it can only be judged once every body is in.
	a.resolveValidators()

	// PASS TWO: which functions run INSIDE verification. Everything a validator
	// expression reaches is part of the check, not a consumer of its result.
	for name := range a.validatorClosure(validatorRootsIn(decls)) {
		if f, ok := a.Fns[name]; ok {
			f.IsValidatorBody = true
		}
	}
	return a
}

// collectStringConsts records package-level `const X = "literal"` declarations.
func collectStringConsts(d *ast.GenDecl, into map[string]string) {
	if d.Tok != token.CONST {
		return
	}
	for _, spec := range d.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok || len(vs.Names) != len(vs.Values) {
			continue
		}
		for i, n := range vs.Names {
			if s, ok := stringLiteral(vs.Values[i]); ok {
				into[n.Name] = s
			}
		}
	}
}

func stringLiteral(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

// returnsStore reports whether fn's result type includes *ledger.Store, i.e. it is a
// store FACTORY and its caller is where verification happens.
func returnsStore(fn *ast.FuncDecl) bool {
	if fn.Type.Results == nil {
		return false
	}
	for _, r := range fn.Type.Results.List {
		star, ok := r.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		if sel, ok := star.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "Store" {
			return true
		}
	}
	return false
}

// calledNamesIn returns every function and method name called anywhere in body.
func calledNamesIn(body ast.Node) map[string]bool {
	names := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch f := call.Fun.(type) {
		case *ast.Ident:
			names[f.Name] = true
		case *ast.SelectorExpr:
			names[f.Sel.Name] = true
		}
		return true
	})
	return names
}

// reaches reports whether `from` calls `want`, or calls something that does,
// transitively within this package.
//
// WHY TRANSITIVE. This is the INVOKES-not-IS rule. admissionValidator delegates in one
// line, recordingPayloadValidator and dispositionPayloadValidator delegate and then add
// stricter rules of their own, and a validator that dispatched through a helper would be
// no less enforcing. A census that only read the validator's own body would push
// enforcement back out of shared code to stay readable, which is measuring its own
// convenience.
func (a *censusAnalysis) reaches(from, want string) bool {
	seen := map[string]bool{}
	stack := []string{from}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[cur] {
			continue
		}
		seen[cur] = true
		f, ok := a.Fns[cur]
		if !ok {
			continue
		}
		for name := range f.Calls {
			if name == want {
				return true
			}
			if !seen[name] {
				stack = append(stack, name)
			}
		}
	}
	return false
}

// validatorRootsIn returns the names a WithPayloadValidator option was given, plus the
// names called from inside any inline func literal it was given.
func validatorRootsIn(decls []parsedFn) map[string]bool {
	roots := map[string]bool{}
	for _, d := range decls {
		ast.Inspect(d.fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "WithPayloadValidator" || len(call.Args) != 1 {
				return true
			}
			switch v := call.Args[0].(type) {
			case *ast.Ident:
				roots[v.Name] = true
			case *ast.SelectorExpr:
				roots[v.Sel.Name] = true
			case *ast.FuncLit:
				for n := range calledNamesIn(v.Body) {
					roots[n] = true
				}
			}
			return true
		})
	}
	return roots
}

// validatorClosure expands validator roots through the call graph.
func (a *censusAnalysis) validatorClosure(roots map[string]bool) map[string]bool {
	out := map[string]bool{}
	var stack []string
	for r := range roots {
		stack = append(stack, r)
	}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if out[cur] {
			continue
		}
		out[cur] = true
		if f, ok := a.Fns[cur]; ok {
			for c := range f.Calls {
				if !out[c] {
					stack = append(stack, c)
				}
			}
		}
	}
	return out
}

// inlineStores returns one storeFact per ledger.NewStore construction in body, with the
// verification (if any) performed on the same variable.
func inlineStores(body *ast.BlockStmt) []storeFact {
	verified := map[string]string{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !verificationMethodNames[sel.Sel.Name] {
			return true
		}
		if recv, ok := sel.X.(*ast.Ident); ok {
			verified[recv.Name] = sel.Sel.Name
		}
		return true
	})

	named := map[*ast.CallExpr]string{}
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) == 0 || len(assign.Rhs) != 1 {
			return true
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok || !isNewStoreCall(call) {
			return true
		}
		if id, ok := assign.Lhs[0].(*ast.Ident); ok {
			named[call] = id.Name
		}
		return true
	})

	var out []storeFact
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isNewStoreCall(call) {
			return true
		}
		fact := storeFact{VarName: named[call]}
		if fact.VarName != "" {
			fact.VerifiedVia = verified[fact.VarName]
		}
		for _, arg := range call.Args[1:] {
			opt, ok := arg.(*ast.CallExpr)
			if !ok {
				continue
			}
			sel, ok := opt.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "WithPayloadValidator" || len(opt.Args) != 1 {
				continue
			}
			fact.HasValidatorOption = true
			fact.ValidatorExpr = types.ExprString(opt.Args[0])
			fact.validatorNode = opt.Args[0]
			if lit, ok := opt.Args[0].(*ast.FuncLit); ok {
				fact.ValidatorIsClosure = true
				fact.ValidatorExpr = "an inline func literal at " + closureSignature(lit)
			}
			if id, ok := opt.Args[0].(*ast.Ident); ok && id.Name == "nil" {
				fact.ValidatorIsNil = true
			}
		}
		out = append(out, fact)
		return true
	})
	return out
}

// storesIn returns every store this body owns: the ones it constructs inline, and the
// ones it obtains from a package store FACTORY. A factory-built store carries the
// factory's validator and the CALLER's verification, which is where the two halves of
// questiondisposition's five reading paths meet.
func storesIn(body *ast.BlockStmt, factories map[string]storeFact) []storeFact {
	out := inlineStores(body)

	verified := map[string]string{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !verificationMethodNames[sel.Sel.Name] {
			return true
		}
		if recv, ok := sel.X.(*ast.Ident); ok {
			verified[recv.Name] = sel.Sel.Name
		}
		return true
	})

	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) == 0 || len(assign.Rhs) != 1 {
			return true
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		fact, isFactory := factories[id.Name]
		if !isFactory {
			return true
		}
		lhs, ok := assign.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}
		fact.ViaFactory = id.Name
		fact.VarName = lhs.Name
		fact.VerifiedVia = verified[lhs.Name]
		out = append(out, fact)
		return true
	})

	sort.Slice(out, func(i, j int) bool { return out[i].VarName < out[j].VarName })
	return out
}

// closureSignature renders an inline validator's parameter list, which is the only part
// of it short enough to name in a failure and enough to find it in the source.
func closureSignature(lit *ast.FuncLit) string {
	var params []string
	if lit.Type.Params != nil {
		for _, f := range lit.Type.Params.List {
			params = append(params, types.ExprString(f.Type))
		}
	}
	return "func(" + strings.Join(params, ", ") + ")"
}

func isNewStoreCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "NewStore" && len(call.Args) >= 1
}

// resolveValidators fills in EnforcesTable for every discovered store. It runs after the
// call graph is complete, because a validator's delegation may go through any function in
// the package -- and a func literal is classified on ITS OWN BODY, which is the only way
// tasksession's three inline closures are seen at all.
func (a *censusAnalysis) resolveValidators() {
	for _, f := range a.Fns {
		for i := range f.Stores {
			s := &f.Stores[i]
			if !s.HasValidatorOption || s.ValidatorIsNil {
				continue
			}
			s.EnforcesTable = a.enforcesTable(s.validatorNode)
		}
	}
}

// enforcesTable reports whether a validator expression INVOKES the one function that
// reads requiredEventArtifacts -- directly, through a wrapper it calls, or from inside an
// anonymous closure's body. It is never an identity test: a wrapper that delegates and
// then adds stricter rules is exactly as enforcing as the bare function.
func (a *censusAnalysis) enforcesTable(expr ast.Expr) bool {
	switch v := expr.(type) {
	case *ast.SelectorExpr:
		if v.Sel.Name == enforcingValidatorName {
			return true
		}
		return a.reaches(v.Sel.Name, enforcingValidatorName)
	case *ast.Ident:
		if v.Name == enforcingValidatorName {
			return true
		}
		return a.reaches(v.Name, enforcingValidatorName)
	case *ast.FuncLit:
		// AN ANONYMOUS CLOSURE IS CLASSIFIED ON ITS OWN BODY, not skipped for lacking a
		// name. tasksession installs three of these; name-keyed discovery finds none.
		for name := range calledNamesIn(v.Body) {
			if name == enforcingValidatorName || a.reaches(name, enforcingValidatorName) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// indexedArtifactKeys reports the artifact keys body reads out of a payload's Artifacts
// map by index, and whether any index used a key it could not resolve.
//
// An index whose key resolves is a read of THAT artifact. An index whose key is a
// parameter makes the function a GENERIC extractor: what it reads is decided by whoever
// calls it, which is why calleesGivenAGovernedKey exists.
//
// A discarded value (`if _, ok := payload.Artifacts[k]; !ok`) is still counted: reporting
// the presence or absence of a governed artifact off an unverified payload is exactly the
// damaged-versus-absent confusion this invariant exists to prevent.
func indexedArtifactKeys(body *ast.BlockStmt, consts map[string]string) ([]string, bool) {
	var keys []string
	generic := false
	ast.Inspect(body, func(n ast.Node) bool {
		idx, ok := n.(*ast.IndexExpr)
		if !ok {
			return true
		}
		sel, ok := idx.X.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Artifacts" {
			return true
		}
		if key, ok := resolveKeyExpr(idx.Index, consts); ok {
			keys = append(keys, key)
		} else {
			generic = true
		}
		return true
	})
	sort.Strings(keys)
	return keys, generic
}

// resolveKeyExpr resolves an artifact-key expression to the constant string it names.
func resolveKeyExpr(e ast.Expr, consts map[string]string) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		return stringLiteral(v)
	case *ast.Ident:
		s, ok := consts[v.Name]
		return s, ok
	case *ast.SelectorExpr:
		s, ok := consts[v.Sel.Name]
		return s, ok
	default:
		return "", false
	}
}

// calleesGivenAGovernedKey returns the functions this body calls while handing them a
// governed artifact key as an argument. That is the OTHER shape of a governed read:
// admission, tasksession and resultrecording all reach their governed artifact by passing
// "capability_consumption" into a generic extractor rather than indexing it here.
func calleesGivenAGovernedKey(body *ast.BlockStmt, consts map[string]string, governed map[string]bool) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var callee string
		switch f := call.Fun.(type) {
		case *ast.Ident:
			callee = f.Name
		case *ast.SelectorExpr:
			callee = f.Sel.Name
		default:
			return true
		}
		for _, arg := range call.Args {
			if key, ok := resolveKeyExpr(arg, consts); ok && governed[key] {
				out[callee] = true
			}
		}
		return true
	})
	return out
}

// ---------------------------------------------------------------------------
// THE CLASSIFIER. Extracted from the census so it can be driven against synthetic
// sources: every real-tree assertion is trivially satisfied on a healthy tree, so
// disabling one changes nothing, and a census whose mutants all survive is not evidence.
// ---------------------------------------------------------------------------

// isSubject reports whether this function is on a required-artifact reading path at all.
// A function that neither owns a ledger store nor touches an Artifacts map is not a
// subject and must not be demanded to verify anything -- without this the census would
// grow to demand verification of every function in four packages, and the first person to
// hit that would loosen the rule.
func (f *fnFacts) isSubject() bool {
	if f.IsValidatorBody {
		return false
	}
	return len(f.Stores) > 0 || len(f.IndexedKeys) > 0 || f.IndexesUnresolvedKey
}

// verifiedRoots are the functions that build a store with a DELEGATING validator and
// verify it. Everything such a function calls works on the chain it verified, which is
// why a root ABSORBS in the reachability rule below.
func (a *censusAnalysis) verifiedRoots() map[string]bool {
	out := map[string]bool{}
	for name, f := range a.Fns {
		if f.IsValidatorBody {
			continue
		}
		for _, s := range f.Stores {
			if s.verified() {
				out[name] = true
			}
		}
	}
	return out
}

// indexSites are the functions that reach into an Artifacts map themselves.
func (a *censusAnalysis) indexSites() map[string]bool {
	out := map[string]bool{}
	for name, f := range a.Fns {
		if f.IsValidatorBody {
			continue
		}
		if len(f.IndexedKeys) > 0 || f.IndexesUnresolvedKey {
			out[name] = true
		}
	}
	return out
}

// governedSites are the extraction sites this invariant is ABOUT: the ones that read an
// artifact the ledger event table governs.
//
// TWO SHAPES, because production uses both. A site that indexes a resolved governed key
// (resultrecording's Artifacts[KeyReceipt], questiondisposition's ArtifactKeyReceipt) is
// governed outright. A GENERIC extractor -- one indexing by a key it receives, like
// latestArtifactFromChain, decodeGovernedArtifact and chainArtifactJSON -- becomes
// governed when some function hands it a governed key, so it is pulled in through the
// call that does. Restricting the rule to governed sites is deliberate: these packages
// also read ungoverned artifacts (result stages, impact reports, the closure request), and
// condemning those would widen the invariant past what this census can honestly claim.
func (a *censusAnalysis) governedSites() map[string]bool {
	out := map[string]bool{}
	for name, f := range a.Fns {
		if f.IsValidatorBody {
			continue
		}
		for _, k := range f.IndexedKeys {
			if a.Governed[k] {
				out[name] = true
			}
		}
	}
	index := a.indexSites()
	for _, f := range a.Fns {
		if f.IsValidatorBody {
			continue
		}
		for callee := range f.GovernedCallees {
			for site := range index {
				if site == callee || a.reaches(callee, site) {
					out[site] = true
				}
			}
		}
	}
	return out
}

// routeToAGovernedSite walks the call graph from `start` with every verified root's
// OUTGOING edges CUT, and returns the path to the first governed extraction site it can
// still reach -- or nil when it can reach none.
//
// THE CUT IS THE WHOLE RULE. A verified root absorbs, because everything below it works
// on the chain that root verified. So admission.LoadRecordedConsumption, which reaches its
// extraction only through LoadLatestArtifactOptional, is stopped at that root and is
// SAFE -- delegating to a self-verifying loader hands no chain across the call and the
// delegator owes nothing. A function that parses a payload off disk and hands it to
// chainArtifactJSON crosses no root, reaches the site, and gets NAMED with its route.
func (a *censusAnalysis) routeToAGovernedSite(start string, roots, governed map[string]bool) []string {
	if governed[start] {
		return []string{start}
	}
	prev := map[string]string{start: ""}
	queue := []string{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur != start && roots[cur] {
			continue // a root absorbs: everything past it is under verification
		}
		f, ok := a.Fns[cur]
		if !ok {
			continue
		}
		callees := make([]string, 0, len(f.Calls))
		for c := range f.Calls {
			callees = append(callees, c)
		}
		sort.Strings(callees)
		for _, c := range callees {
			if _, seen := prev[c]; seen {
				continue
			}
			prev[c] = cur
			if governed[c] {
				path := []string{c}
				for at := cur; at != ""; at = prev[at] {
					path = append([]string{at}, path...)
				}
				return path
			}
			queue = append(queue, c)
		}
	}
	return nil
}

// censusFailures is the whole judgement, in the two halves the invariant needs.
func (a *censusAnalysis) censusFailures() []string {
	var failures []string

	// HALF ONE: every store on a reading path installs a DELEGATING validator.
	for _, name := range a.names() {
		f := a.Fns[name]
		if !f.isSubject() {
			continue
		}
		for _, store := range f.Stores {
			switch {
			case !store.HasValidatorOption:
				failures = append(failures, fmt.Sprintf(
					"%s: the store%s is built with NO payload validator; the chain it verifies is "+
						"checked against nothing, so an artifact-less event of a type the table requires "+
						"one for verifies clean and reaches extraction", f.Route, store.origin()))
			case store.ValidatorIsNil:
				failures = append(failures, fmt.Sprintf(
					"%s: the store%s is built with a NIL payload validator; ledger/verify.go skips a "+
						"nil validator entirely, so the event table is not enforced on this path",
					f.Route, store.origin()))
			case !store.EnforcesTable:
				failures = append(failures, fmt.Sprintf(
					"%s: payload validator %s does not reach %s, the only function that reads "+
						"requiredEventArtifacts; a validator that enforces nothing protects nobody",
					f.Route, store.describeValidator(), enforcingValidatorName))
			}
		}
	}

	// HALF TWO: every governed extraction is reachable ONLY through a verified root.
	roots := a.verifiedRoots()
	governed := a.governedSites()
	underVerification := a.reachedFrom(roots)
	for _, name := range a.names() {
		f := a.Fns[name]
		if f.IsValidatorBody || roots[name] || underVerification[name] {
			continue
		}
		route := a.routeToAGovernedSite(name, roots, governed)
		if route == nil {
			continue
		}
		if len(route) == 1 {
			failures = append(failures, fmt.Sprintf(
				"%s: reads a required-artifact key out of a payload's Artifacts map, but its chain does "+
					"not come from a store that installs a table-enforcing validator AND is verified "+
					"through it; artifact extraction here is OUTSIDE the verified boundary", f.Route))
			continue
		}
		named := make([]string, 0, len(route))
		for _, step := range route {
			named = append(named, a.routeOf(step))
		}
		failures = append(failures, fmt.Sprintf(
			"%s: reaches the required-artifact extraction at %s along %s, and no step on that route "+
				"verifies a ledger through a validator that invokes %s; the extraction is OUTSIDE the "+
				"verified boundary",
			f.Route, a.routeOf(route[len(route)-1]), strings.Join(named, " -> "), enforcingValidatorName))
	}
	return failures
}

// reachedFrom returns every function reachable from any member of roots, roots included.
func (a *censusAnalysis) reachedFrom(roots map[string]bool) map[string]bool {
	out := map[string]bool{}
	var stack []string
	for r := range roots {
		stack = append(stack, r)
	}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if out[cur] {
			continue
		}
		out[cur] = true
		if f, ok := a.Fns[cur]; ok {
			for c := range f.Calls {
				if !out[c] {
					stack = append(stack, c)
				}
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// LOCATING THE SUBJECTS.
// ---------------------------------------------------------------------------

// repoRootFromHere locates the repository root from this test file's own path.
func repoRootFromHere(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the trustkernel package directory")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}

func ledgerEventFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRootFromHere(t), "golang", "architecture", "ledger", "event.go")
}

// governedArtifactKeys reads the VALUES of the requiredEventArtifacts map literal out of
// the ledger source: the artifact keys the event table governs. It reads the table; it
// does not change it, and it is never a hand-maintained list here -- adding a fourth
// governed key widens this census on the next run.
func governedArtifactKeys(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, ledgerEventFile(t), nil, 0)
	if err != nil {
		t.Fatalf("parse ledger/event.go: %v", err)
	}
	out := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || spec.Names[0].Name != "requiredEventArtifacts" || len(spec.Values) != 1 {
			return true
		}
		lit, ok := spec.Values[0].(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			keys, ok := kv.Value.(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, k := range keys.Elts {
				if s, ok := stringLiteral(k); ok {
					out[s] = true
				}
			}
		}
		return true
	})
	return out
}

// analyzeRealPackages runs the census over all four subject packages.
func analyzeRealPackages(t *testing.T) map[string]*censusAnalysis {
	t.Helper()
	governed := governedArtifactKeys(t)
	if len(governed) == 0 {
		t.Fatal("requiredEventArtifacts declares no artifact key at all; the census has no subject " +
			"and every assertion below would pass by describing nothing")
	}
	root := repoRootFromHere(t)
	out := map[string]*censusAnalysis{}
	for _, rel := range censusPackageDirs {
		dir := filepath.Join(root, rel)
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("locate census package %s: %v", rel, err)
		}
		out[rel] = analyzePackage(t, dir, governed)
	}
	return out
}

// ---------------------------------------------------------------------------
// HALF ONE AND HALF TWO, against the four real packages.
// ---------------------------------------------------------------------------

// EVERY PRODUCTION READ OF A GOVERNED ARTIFACT CROSSES A VALIDATOR THAT INVOKES
// ledger.ValidateTaskEventPayload BEFORE EXTRACTION.
//
// This is the assumption that makes admission.requiredArtifact's mutant EQUIVALENT. It is
// asserted over whatever the parser finds in four packages, never over a maintained list:
// a reader added tomorrow is a subject the moment it is written.
func TestEveryGovernedArtifactReadCrossesADelegatingValidator(t *testing.T) {
	packages := analyzeRealPackages(t)

	totalSubjects, totalEnforcing, totalGoverned := 0, 0, 0
	for _, rel := range censusPackageDirs {
		a := packages[rel]

		// ANTI-VACUITY, PER PACKAGE. A census with no subjects asserts nothing and still
		// reports PASS -- which is exactly what scanning only the admission directory did.
		// A package silently dropped from the subject set fails here rather than shrinking
		// the claim in silence.
		subjects := 0
		enforcing := 0
		for _, name := range a.names() {
			f := a.Fns[name]
			if f.isSubject() {
				subjects++
			}
			for _, s := range f.Stores {
				if s.EnforcesTable {
					enforcing++
				}
			}
		}
		governedSites := a.governedSites()
		if subjects == 0 {
			t.Errorf("%s: the census discovered NO subject; either every reader was removed or the "+
				"discovery rule no longer matches the code it is supposed to measure. Either way this "+
				"package is now certified by a census that asserts nothing about it", rel)
		}
		if enforcing == 0 {
			t.Errorf("%s: no store installs a validator that reaches %s; the delegation detector has "+
				"lost its subject here", rel, enforcingValidatorName)
		}
		if len(governedSites) == 0 {
			t.Errorf("%s: the census found no read of a required-artifact key; the extraction-boundary "+
				"rule has lost its subject here", rel)
		}
		totalSubjects += subjects
		totalEnforcing += enforcing
		totalGoverned += len(governedSites)

		if failures := a.censusFailures(); len(failures) > 0 {
			t.Errorf("%s: %d reading path(s) break the verified boundary:\n  %s",
				rel, len(failures), strings.Join(failures, "\n  "))
		}
		t.Logf("%s: %d subject(s), %d delegating store(s), %d governed extraction site(s) %v",
			rel, subjects, enforcing, len(governedSites), sortedStrings(governedSites))
	}

	if totalGoverned == 0 {
		t.Fatal("no governed extraction site was found in ANY of the four packages; " +
			"admission.requiredArtifact's mutant would be equivalent because nothing reads at all, " +
			"which is not the claim this census makes")
	}
	t.Logf("required-artifact census: %d package(s), %d subject(s), %d delegating store(s), "+
		"%d governed extraction site(s)", len(censusPackageDirs), totalSubjects, totalEnforcing, totalGoverned)
}

// A WRAPPER THAT DELEGATES AND ADDS RULES IS A VALID VALIDATOR -- INVOKES, NOT IS.
//
// recordingPayloadValidator and dispositionPayloadValidator both call
// ledger.ValidateTaskEventPayload and then enforce stricter event contracts of their own.
// An equality test against ValidateTaskEventPayload would reject both and condemn two of
// the four packages outright, so this pins the rule that makes them legal -- against the
// REAL packages, where the wrappers actually live.
func TestDelegatingWrapperValidatorsAreAccepted(t *testing.T) {
	packages := analyzeRealPackages(t)
	wrappers := map[string]string{
		filepath.Join("golang", "architecture", "resultrecording"):     "recordingPayloadValidator",
		filepath.Join("golang", "architecture", "questiondisposition"): "dispositionPayloadValidator",
	}
	for rel, wrapper := range wrappers {
		a := packages[rel]

		// The wrapper must be a wrapper, not the bare function under another name: it has
		// to do something beyond delegating, or this witness proves nothing about wrappers.
		f, ok := a.Fns[wrapper]
		if !ok {
			t.Errorf("%s: %s no longer exists; the wrapper rule has lost its subject", rel, wrapper)
			continue
		}
		delegates := a.reaches(wrapper, enforcingValidatorName)
		if !delegates {
			t.Errorf("%s: %s no longer reaches %s -- it has stopped delegating, and every read it "+
				"guards is now outside the event table", rel, wrapper, enforcingValidatorName)
		}
		if len(f.Calls) < 2 {
			t.Errorf("%s: %s calls only %v; it is no longer a WRAPPER that adds rules, so this "+
				"witness would pass on a bare alias and prove nothing about delegation",
				rel, wrapper, sortedStrings(f.Calls))
		}

		// And a store that installs it must be accepted, not merely recognised.
		installed := 0
		for _, name := range a.names() {
			for _, s := range a.Fns[name].Stores {
				if s.ValidatorExpr != wrapper {
					continue
				}
				installed++
				// Only meaningful while the wrapper really does delegate: once it has
				// stopped, the census is RIGHT to reject it and the error above is the
				// finding. Asserting acceptance here anyway would report a second,
				// contradictory failure blaming the census for telling the truth.
				if delegates && !s.EnforcesTable {
					t.Errorf("%s: %s installs %s, a validator that DOES invoke %s, and the census "+
						"rejected it; identity is not the rule, delegation is",
						rel, a.routeOf(name), wrapper, enforcingValidatorName)
				}
			}
		}
		if installed == 0 {
			t.Errorf("%s: no store installs %s any more; the wrapper-acceptance rule has lost its "+
				"subject", rel, wrapper)
		}
	}
}

// AN ANONYMOUS CLOSURE IS A VALIDATOR LIKE ANY OTHER.
//
// tasksession installs three inline func literals -- control.go, session.go, session.go.
// Discovery keyed on named validator functions finds NONE of them and certifies the
// package by describing nothing. This requires the real package to still contain them and
// the census to have classified each on the literal's own body.
func TestAnonymousClosureValidatorsAreDiscoveredAndClassified(t *testing.T) {
	a := analyzeRealPackages(t)[filepath.Join("golang", "architecture", "tasksession")]
	closures := 0
	for _, name := range a.names() {
		for _, s := range a.Fns[name].Stores {
			if !s.ValidatorIsClosure {
				continue
			}
			closures++
			if !s.EnforcesTable {
				t.Errorf("%s: an inline closure validator was not classified as delegating to %s; "+
					"a store built with a func literal must be judged on that literal's body",
					a.routeOf(name), enforcingValidatorName)
			}
		}
	}
	if closures == 0 {
		t.Error("tasksession installs no inline closure validator any more; the anonymous-closure " +
			"rule has lost its subject, and a discovery keyed on named functions would now pass here " +
			"for the wrong reason")
	}
	t.Logf("tasksession: %d inline closure validator(s), all delegating", closures)
}

// A STORE BUILT BY A FACTORY IS STILL A STORE.
//
// questiondisposition constructs all five of its stores through newStore(taskDir), which
// RETURNS ledger.NewStore(...). Discovery that only sees a NewStore call bound to a local
// variable finds ZERO stores in that package, reports no failure, and certifies it.
func TestFactoryBuiltStoresAreDiscoveredAndAttributed(t *testing.T) {
	a := analyzeRealPackages(t)[filepath.Join("golang", "architecture", "questiondisposition")]
	viaFactory, verifiedViaFactory := 0, 0
	for _, name := range a.names() {
		for _, s := range a.Fns[name].Stores {
			if s.ViaFactory == "" {
				continue
			}
			viaFactory++
			if !s.EnforcesTable {
				t.Errorf("%s: the store it takes from %s was not classified as delegating to %s",
					a.routeOf(name), s.ViaFactory, enforcingValidatorName)
			}
			if s.VerifiedVia != "" {
				verifiedViaFactory++
			}
		}
	}
	if viaFactory == 0 {
		t.Error("questiondisposition builds no store through a factory any more; the factory rule " +
			"has lost its subject")
	}
	if verifiedViaFactory == 0 {
		t.Error("no factory-built store in questiondisposition is verified by its caller; the " +
			"factory's validator would then guard nothing, and every read in that package would be " +
			"outside the boundary")
	}
	t.Logf("questiondisposition: %d factory-built store(s), %d verified by their caller",
		viaFactory, verifiedViaFactory)
}

// ---------------------------------------------------------------------------
// THE BEHAVIOURAL HALF, against the real validator.
// ---------------------------------------------------------------------------

// THE VALIDATOR ACTUALLY REFUSES WHAT THE TABLE REQUIRES.
//
// The structural half proves a validator is installed and that it invokes
// ValidateTaskEventPayload. That is worth nothing on its own: a validator that is present
// and enforces nothing satisfies it exactly.
//
// TWO LEGS, over the SOURCE OF TRUTH rather than a hand-listed set -- every event type
// for which ledger.RequiredArtifactKeys declares a requirement:
//
//	the real read path   admission.LoadLatestArtifactOptional, which builds the store with
//	                     admission's own validator, must REFUSE an artifact-less event of
//	                     that type instead of reporting it present or absent.
//	the refusal's words  the validator must NAME the missing key. The read path cannot show
//	                     this: loadVerifiedChain collapses every verification error into
//	                     "invalid ledger chain" and keeps the detail in the report. So the
//	                     naming is asserted against ValidateTaskEventPayload directly, run
//	                     over the SAME BYTES the ledger wrote -- and the structural half
//	                     above is what pins that function as the one every reader reaches.
func TestAdmissionsValidatorRefusesEveryArtifactTheEventTableRequires(t *testing.T) {
	covered := 0
	for _, eventType := range closureprotocol.LedgerEventTypes {
		keys := ledger.RequiredArtifactKeys(eventType)
		if len(keys) == 0 {
			continue
		}
		for _, key := range keys {
			covered++
			t.Run(string(eventType)+"/"+key, func(t *testing.T) {
				taskDir := governedChain(t, 2)
				appendArtifactLessEvent(t, taskDir, eventType, "artifact-less")

				// LEG ONE: admission's own reader, wired as production wires it.
				var out map[string]any
				found, err := admission.LoadLatestArtifactOptional(taskDir, eventType, key, &out)
				if found {
					t.Fatalf("an artifact-less %s event was reported as CARRYING its required %q artifact",
						eventType, key)
				}
				if err == nil {
					t.Fatalf("admission's reader ACCEPTED an artifact-less %s event; the event table "+
						"requires a %q artifact, so this record is damaged and the reader must fail closed",
						eventType, key)
				}
				if strings.Contains(err.Error(), fmt.Sprintf("no %s event found in task ledger", eventType)) {
					t.Errorf("a DAMAGED %s record was refused AS ABSENT: %v", eventType, err)
				}

				// LEG TWO: the refusal names the key.
				data := latestPayloadBytes(t, taskDir)
				verr := ledger.ValidateTaskEventPayload(eventType, data)
				if verr == nil {
					t.Fatalf("the validator every reader installs ACCEPTED a %s payload carrying no "+
						"artifacts, though the event table requires %q; the structural census is "+
						"satisfied by a validator that enforces nothing", eventType, key)
				}
				if !strings.Contains(verr.Error(), key) {
					t.Errorf("the refusal does not NAME the missing artifact %q: %v; an unnamed refusal "+
						"cannot be told from any other malformed payload", key, verr)
				}
			})
		}
	}

	// ANTI-VACUITY. An emptied table would make this half assert nothing while passing.
	if covered == 0 {
		t.Fatal("ledger.RequiredArtifactKeys declares no required artifact for ANY event type in " +
			"closureprotocol.LedgerEventTypes: this half asserts nothing, and the readers in all four " +
			"census packages have no contract left to enforce")
	}
}

// THE TABLE IS FULLY REACHABLE FROM THE VOCABULARY THE HALF ABOVE ITERATES.
//
// The behavioural half enumerates closureprotocol.LedgerEventTypes and asks the table
// about each. A requirement declared for an event type that vocabulary omits would be
// silently untested, and the half would still report PASS. This reads the table's own
// keys out of the source and requires the vocabulary to cover them.
func TestEveryRequiredArtifactEventTypeIsInTheIteratedVocabulary(t *testing.T) {
	keys := requiredArtifactTableKeys(t, ledgerEventFile(t))
	if len(keys) == 0 {
		t.Fatal("no requiredEventArtifacts entries were found in ledger/event.go; the table reader " +
			"has lost its subject, so its coverage claim means nothing")
	}
	known := map[string]bool{}
	for _, et := range closureprotocol.LedgerEventTypes {
		known["closureprotocol.LedgerEvent"+trimEventTypeConst(et)] = true
	}
	var uncovered []string
	for _, k := range keys {
		if !known[k] {
			uncovered = append(uncovered, k)
		}
	}
	if len(uncovered) > 0 {
		sort.Strings(uncovered)
		t.Errorf("requiredEventArtifacts declares a requirement for %v, which "+
			"closureprotocol.LedgerEventTypes does not list; the behavioural half never asks about "+
			"them and passes anyway", uncovered)
	}
}

// requiredArtifactTableKeys reads the KEY EXPRESSIONS of the requiredEventArtifacts map
// literal out of the ledger source. It reads the table; it does not change it.
func requiredArtifactTableKeys(t *testing.T, eventFile string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, eventFile, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", eventFile, err)
	}
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || spec.Names[0].Name != "requiredEventArtifacts" || len(spec.Values) != 1 {
			return true
		}
		lit, ok := spec.Values[0].(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, elt := range lit.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				out = append(out, types.ExprString(kv.Key))
			}
		}
		return true
	})
	return out
}

// trimEventTypeConst maps an event type VALUE back to the tail of its constant name:
// "admission_consumed" -> "AdmissionConsumed".
func trimEventTypeConst(et closureprotocol.LedgerEventType) string {
	parts := strings.Split(string(et), "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// latestPayloadBytes returns the payload file the ledger itself wrote for the newest
// entry. Reading the real bytes rather than re-serialising a struct keeps the validator
// under test against the artifact production actually produces.
func latestPayloadBytes(t *testing.T, taskDir string) []byte {
	t.Helper()
	chain, err := ledger.NewStore(taskDir).VerifyChain()
	if err != nil {
		t.Fatalf("load the fixture chain: %v", err)
	}
	if len(chain.Entries) == 0 {
		t.Fatal("the fixture chain is empty")
	}
	data, err := os.ReadFile(chain.Entries[len(chain.Entries)-1].PayloadPath)
	if err != nil {
		t.Fatalf("read the newest payload: %v", err)
	}
	return data
}

// ---------------------------------------------------------------------------
// THE CENSUS'S OWN WITNESSES.
//
// A detector nobody has driven against a known-bad input is an assumption. Every
// real-tree assertion above is trivially satisfied on a healthy tree, so disabling one
// changes nothing and every mutant of it survives. These drive the SAME discovery and the
// SAME classifier over synthetic sources, one per way the boundary can break, where each
// must be REPORTED.
// ---------------------------------------------------------------------------

// fixtureGoverned is the governed key set the fixtures speak in. It is read from the real
// table, so a fixture cannot drift into asserting about a key the ledger stopped
// governing -- it would stop being a governed read and the witness would quietly weaken.
func fixtureGoverned(t *testing.T) map[string]bool {
	t.Helper()
	g := governedArtifactKeys(t)
	for _, want := range []string{"capability_consumption", "result_transition_receipt", "question_disposition_receipt"} {
		if !g[want] {
			t.Fatalf("the ledger event table no longer governs %q, which every fixture below reads; "+
				"these witnesses would drive the classifier over keys it does not judge", want)
		}
	}
	return g
}

func censusFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func analyzeFixture(t *testing.T, dir string) *censusAnalysis {
	t.Helper()
	return analyzePackage(t, dir, fixtureGoverned(t))
}

// soundReader mirrors the real shape: a validator that invokes ValidateTaskEventPayload,
// verified through the same store, extraction below that verified root.
const soundReader = `package fake

func enforcingValidator(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
	return ledger.ValidateTaskEventPayload(eventType, data)
}

func LoadSound(taskDir string, out any) (bool, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(enforcingValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		return false, err
	}
	return soundFromChain(taskDir, chain, out)
}

func soundFromChain(taskDir string, chain ledger.VerifiedChain, out any) (bool, error) {
	payload, err := ledger.ParseTaskEventPayload(nil)
	if err != nil {
		return false, err
	}
	ref, ok := payload.Artifacts["capability_consumption"]
	if !ok {
		return false, nil
	}
	_ = ref
	return true, nil
}
`

// A NIL VALIDATOR. ledger/verify.go skips a nil validator entirely.
const nilValidatorReader = `package fake

func LoadWithNil(taskDir string) (bool, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(nil))
	chain, err := store.VerifyChain()
	if err != nil {
		return false, err
	}
	payload, err := ledger.ParseTaskEventPayload(nil)
	if err != nil {
		return false, err
	}
	ref, ok := payload.Artifacts["capability_consumption"]
	_, _ = chain, ref
	return ok, nil
}
`

// NO VALIDATOR AT ALL.
const omittedValidatorReader = `package fake

func LoadWithoutOption(taskDir string) (bool, error) {
	store := ledger.NewStore(taskDir)
	chain, err := store.VerifyChain()
	if err != nil {
		return false, err
	}
	payload, err := ledger.ParseTaskEventPayload(nil)
	if err != nil {
		return false, err
	}
	ref, ok := payload.Artifacts["result_transition_receipt"]
	_, _ = chain, ref
	return ok, nil
}
`

// A WRAPPER THAT STOPPED DELEGATING. This is the mutant an IS test cannot tell from a
// healthy wrapper and an INVOKES test catches: lapsedWrapper still looks exactly like
// recordingPayloadValidator -- it parses, it enforces a rule of its own -- but the one
// line that reached the event table is gone.
const lapsedWrapperReader = `package fake

func lapsedWrapper(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
	payload, err := ledger.ParseTaskEventPayload(data)
	if err != nil {
		return err
	}
	return validateLapsedShape(payload)
}

func validateLapsedShape(payload ledger.TaskEventPayload) error {
	if payload.TaskID == "" {
		return errors.New("no task")
	}
	return nil
}

func LoadWithLapsedWrapper(taskDir string) (bool, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(lapsedWrapper))
	chain, err := store.VerifyChain()
	if err != nil {
		return false, err
	}
	payload, err := ledger.ParseTaskEventPayload(nil)
	if err != nil {
		return false, err
	}
	ref, ok := payload.Artifacts["capability_consumption"]
	_, _ = chain, ref
	return ok, nil
}
`

// AN ANONYMOUS CLOSURE THAT ENFORCES NOTHING. This is the mutant NAME-KEYED discovery
// misses outright: there is no named validator to look up, so a census keyed on one finds
// no validator here at all and reports nothing.
const anonymousClosureReader = `package fake

func LoadWithClosure(taskDir string) (bool, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(func(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
		return nil
	}))
	chain, err := store.VerifyChain()
	if err != nil {
		return false, err
	}
	payload, err := ledger.ParseTaskEventPayload(nil)
	if err != nil {
		return false, err
	}
	ref, ok := payload.Artifacts["question_disposition_receipt"]
	_, _ = chain, ref
	return ok, nil
}
`

// AN ANONYMOUS CLOSURE THAT DOES DELEGATE, which the census must ACCEPT. Without this the
// closure rule above would be satisfied by a census that condemns every func literal.
const soundClosureReader = `package fake

func LoadWithSoundClosure(taskDir string) (bool, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(func(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
		return ledger.ValidateTaskEventPayload(eventType, data)
	}))
	chain, err := store.VerifyChain()
	if err != nil {
		return false, err
	}
	payload, err := ledger.ParseTaskEventPayload(nil)
	if err != nil {
		return false, err
	}
	ref, ok := payload.Artifacts["question_disposition_receipt"]
	_, _ = chain, ref
	return ok, nil
}
`

// EXTRACTION REACHED FROM NOWHERE. No store, no verification, straight off disk.
const unverifiedExtraction = `package fake

func LoadStraightOffDisk(taskDir string) (bool, error) {
	data, err := os.ReadFile(taskDir)
	if err != nil {
		return false, err
	}
	payload, err := ledger.ParseTaskEventPayload(data)
	if err != nil {
		return false, err
	}
	ref, ok := payload.Artifacts["capability_consumption"]
	_ = ref
	return ok, nil
}
`

// A NEW DIRECT CALL FROM AN UNCHECKED PATH. This is the half a census of CONSTRUCTORS
// alone cannot see: every store in the package is still built correctly, and somebody has
// simply reached past them into the extractor. Paired with soundReader, whose
// soundFromChain it calls.
const smuggledExtractionCaller = `package fake

func SmuggleFromDisk(taskDir string) (bool, error) {
	data, err := os.ReadFile(taskDir)
	if err != nil {
		return false, err
	}
	_ = data
	return soundFromChain(taskDir, ledger.VerifiedChain{}, nil)
}
`

// A STORE FACTORY WHOSE VALIDATOR IS NIL, used by a caller that verifies it. Every rule
// here is keyed on the CALLER's store, so a factory that hands out an unguarded store is
// how a whole package goes green with nothing enforced.
const factoryWithNilValidator = `package fake

func newFixtureStore(taskDir string) *ledger.Store {
	return ledger.NewStore(taskDir, ledger.WithPayloadValidator(nil))
}

func LoadViaFactory(taskDir string) (bool, error) {
	store := newFixtureStore(taskDir)
	chain, err := store.VerifyChain()
	if err != nil {
		return false, err
	}
	payload, err := ledger.ParseTaskEventPayload(nil)
	if err != nil {
		return false, err
	}
	ref, ok := payload.Artifacts["capability_consumption"]
	_, _ = chain, ref
	return ok, nil
}
`

// EVERY WAY THE BOUNDARY BREAKS, each driven against the classifier and each REQUIRED to
// be reported. A census whose mutants all survive is not evidence.
func TestTheCensusReportsEveryWayTheVerifiedBoundaryBreaks(t *testing.T) {
	cases := []struct {
		name      string
		files     map[string]string
		subject   string
		wantParts []string
	}{
		{
			name:      "a store built with a NIL validator",
			files:     map[string]string{"reader.go": nilValidatorReader},
			subject:   "reader.go:LoadWithNil",
			wantParts: []string{"NIL payload validator"},
		},
		{
			name:      "a store built with NO validator option",
			files:     map[string]string{"reader.go": omittedValidatorReader},
			subject:   "reader.go:LoadWithoutOption",
			wantParts: []string{"NO payload validator"},
		},
		{
			name:      "a wrapper validator that stopped delegating",
			files:     map[string]string{"reader.go": lapsedWrapperReader},
			subject:   "reader.go:LoadWithLapsedWrapper",
			wantParts: []string{"lapsedWrapper", "does not reach " + enforcingValidatorName},
		},
		{
			name:      "an anonymous closure validator that enforces nothing",
			files:     map[string]string{"reader.go": anonymousClosureReader},
			subject:   "reader.go:LoadWithClosure",
			wantParts: []string{"func(", "does not reach " + enforcingValidatorName},
		},
		{
			name:      "a factory that hands out an unguarded store",
			files:     map[string]string{"reader.go": factoryWithNilValidator},
			subject:   "reader.go:LoadViaFactory",
			wantParts: []string{"NIL payload validator", "via newFixtureStore"},
		},
		{
			name:      "extraction reached with nothing verified at all",
			files:     map[string]string{"reader.go": unverifiedExtraction},
			subject:   "reader.go:LoadStraightOffDisk",
			wantParts: []string{"OUTSIDE the verified boundary"},
		},
		{
			name: "a new direct call into the extractor from an unchecked path",
			files: map[string]string{
				"sound.go":   soundReader,
				"smuggle.go": smuggledExtractionCaller,
			},
			subject: "smuggle.go:SmuggleFromDisk",
			wantParts: []string{
				"smuggle.go:SmuggleFromDisk -> sound.go:soundFromChain",
				"OUTSIDE the verified boundary",
			},
		},
	}
	if len(cases) == 0 {
		t.Fatal("the boundary-failure table is empty: this witness asserts nothing")
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := censusFixture(t, tc.files)
			a := analyzeFixture(t, dir)
			subjectName := tc.subject[strings.Index(tc.subject, ":")+1:]
			if _, found := a.Fns[subjectName]; !found {
				t.Fatalf("the census did not even DISCOVER %s; it cannot report what it cannot see. "+
					"discovered: %v", tc.subject, a.names())
			}
			failures := a.censusFailures()
			if len(failures) == 0 {
				t.Fatalf("the census reported NO failure for %q; it would certify this package", tc.name)
			}
			joined := strings.Join(failures, "\n")
			if !strings.Contains(joined, tc.subject) {
				t.Errorf("the failure does not NAME the route %s:\n%s", tc.subject, joined)
			}
			for _, part := range tc.wantParts {
				if !strings.Contains(joined, part) {
					t.Errorf("the failure does not say %q:\n%s", part, joined)
				}
			}
		})
	}
}

// THE NEGATIVE CONTROLS. Every refusal above is satisfiable by a census that fails
// everything. A correctly wired reader must produce NO failure, or the census is noise
// and the real-tree assertion means nothing.
func TestTheCensusPassesSoundlyVerifiedReaders(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
	}{
		{"a named delegating validator", soundReader},
		{"an anonymous closure that delegates", soundClosureReader},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := analyzeFixture(t, censusFixture(t, map[string]string{"reader.go": tc.source}))
			if len(a.governedSites()) == 0 {
				t.Fatal("the census found no governed read in a package that plainly performs one; " +
					"this control would pass by describing nothing")
			}
			if failures := a.censusFailures(); len(failures) != 0 {
				t.Errorf("a soundly verified reader was reported as breaking the boundary:\n  %s",
					strings.Join(failures, "\n  "))
			}
		})
	}
}

// ONE SOUND READER MUST NOT CERTIFY A BROKEN SIBLING, and the case that matters is both
// in ONE file -- which is how a whole file gets excused by its best function.
func TestOneSoundReaderCannotCertifyABrokenSibling(t *testing.T) {
	t.Run("separate files", func(t *testing.T) {
		dir := censusFixture(t, map[string]string{
			"sound.go":  soundReader,
			"broken.go": nilValidatorReader,
		})
		assertFailsExactly(t, dir, "broken.go:LoadWithNil")
	})
	t.Run("same file", func(t *testing.T) {
		dir := censusFixture(t, map[string]string{
			"reader.go": soundReader + "\n" + strings.Replace(nilValidatorReader, "package fake\n\n", "", 1),
		})
		assertFailsExactly(t, dir, "reader.go:LoadWithNil")
	})
}

// assertFailsExactly requires the census to condemn exactly ONE subject, named.
//
// It counts SUBJECTS rather than failure lines on purpose: one broken reader legitimately
// trips several rules at once -- a store built with a nil validator also puts its own
// extraction outside the verified boundary -- and a helper that demanded a single line
// would have to be loosened the first time the census reported the whole truth about a
// subject. What must not happen is a SECOND subject being dragged in, or the sound
// sibling being condemned alongside the broken one.
func assertFailsExactly(t *testing.T, dir string, wantSubject string) {
	t.Helper()
	failures := analyzeFixture(t, dir).censusFailures()
	if len(failures) == 0 {
		t.Fatalf("the census reported no failure at all; it would certify a package containing %s",
			wantSubject)
	}
	named := map[string]bool{}
	for _, f := range failures {
		subject, _, ok := strings.Cut(f, ": ")
		if !ok {
			t.Fatalf("a census failure does not lead with the route it names: %s", f)
		}
		named[subject] = true
	}
	if len(named) != 1 || !named[wantSubject] {
		t.Fatalf("the census condemned %v, want exactly {%s}:\n  %s",
			sortedStrings(named), wantSubject, strings.Join(failures, "\n  "))
	}
}

func sortedStrings(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// A FUNCTION THAT READS NO LEDGER IS NOT A SUBJECT, and must not be demanded to verify
// anything. Without this the census would grow to demand verification of every function
// in four packages, and the first person to hit that would loosen the rule.
func TestAFunctionThatReadsNoLedgerIsNotACensusSubject(t *testing.T) {
	dir := censusFixture(t, map[string]string{
		"sound.go": soundReader,
		"plain.go": "package fake\n\nfunc Unrelated(a int) int { return a + 1 }\n",
	})
	a := analyzeFixture(t, dir)
	if f, listed := a.Fns["Unrelated"]; listed && f.isSubject() {
		t.Error("a function that reads no ledger was made a census subject")
	}
	if failures := a.censusFailures(); len(failures) != 0 {
		t.Errorf("unexpected failures: %v", failures)
	}
}

// A VALIDATOR'S OWN PRESENCE CHECK IS NOT A READ. dispositionPayloadValidator reaches
// validateDispositionEventPayload, which tests payload.Artifacts[ArtifactKeyReceipt]
// while running INSIDE verification, over the bytes being verified. Counting that as an
// unverified extraction condemns a real function in a real package -- and the first false
// positive is what gets a census disabled.
func TestAValidatorsOwnArtifactCheckIsNotAnUnverifiedRead(t *testing.T) {
	const validatorWithACheck = `package fake

func strictValidator(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
	if err := ledger.ValidateTaskEventPayload(eventType, data); err != nil {
		return err
	}
	payload, err := ledger.ParseTaskEventPayload(data)
	if err != nil {
		return err
	}
	return requireReceipt(payload)
}

func requireReceipt(payload ledger.TaskEventPayload) error {
	if _, ok := payload.Artifacts["question_disposition_receipt"]; !ok {
		return errors.New("missing receipt")
	}
	return nil
}

func LoadStrict(taskDir string) error {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(strictValidator))
	_, err := store.VerifyChain()
	return err
}
`
	a := analyzeFixture(t, censusFixture(t, map[string]string{"reader.go": validatorWithACheck}))
	if f, ok := a.Fns["requireReceipt"]; !ok || !f.IsValidatorBody {
		t.Error("a function reached only from a validator expression was not recognised as running " +
			"inside verification")
	}
	if failures := a.censusFailures(); len(failures) != 0 {
		t.Errorf("a validator's own presence check was reported as an unverified read:\n  %s",
			strings.Join(failures, "\n  "))
	}
}

// DELEGATION IS NOT SMUGGLING. admission.LoadRecordedConsumption hands
// "capability_consumption" to LoadLatestArtifact, which verifies its own ledger before
// extracting; no chain crosses that call, so the delegator owes nothing. Demanding a store
// of it would be a false positive on a real production function.
func TestDelegatingToASelfVerifyingLoaderIsNotAnUnverifiedRead(t *testing.T) {
	const delegator = `
func LoadConsumption(taskDir string, out any) error {
	found, err := LoadSound(taskDir, out)
	if err != nil {
		return err
	}
	if !found {
		return errors.New("absent")
	}
	return nil
}
`
	a := analyzeFixture(t, censusFixture(t, map[string]string{"reader.go": soundReader + delegator}))
	if _, ok := a.Fns["LoadConsumption"]; !ok {
		t.Fatal("the delegator was not discovered at all")
	}
	if failures := a.censusFailures(); len(failures) != 0 {
		t.Errorf("a delegating loader was reported as breaking the boundary:\n  %s",
			strings.Join(failures, "\n  "))
	}
}

// ANTI-VACUITY, DRIVEN RATHER THAN ASSERTED. The discovery must find nothing in a package
// with no readers, and the census's per-package floor is what turns that into a failure.
// Without this, removing the floor changes no test.
func TestTheCensusDiscoversNothingWhenThereIsNothingToDiscover(t *testing.T) {
	dir := censusFixture(t, map[string]string{
		"plain.go": "package fake\n\nfunc Unrelated(a int) int { return a + 1 }\n",
	})
	a := analyzeFixture(t, dir)
	subjects := 0
	for _, n := range a.names() {
		if a.Fns[n].isSubject() {
			subjects++
		}
	}
	if subjects != 0 {
		t.Errorf("a package with no ledger reader yielded %d subject(s): %v", subjects, a.names())
	}
	if sites := a.governedSites(); len(sites) != 0 {
		t.Errorf("a package with no ledger reader yielded governed sites %v", sortedStrings(sites))
	}
}

// A NEW READER IS DISCOVERED THE MOMENT IT IS WRITTEN. This is the census's standing
// promise about the future: the covered set is whatever the parser derives, not a number
// anyone maintains.
func TestANewlyAddedReaderIsDiscoveredAndJudged(t *testing.T) {
	newcomer := strings.Replace(nilValidatorReader, "LoadWithNil", "LoadNewcomer", 1)
	dir := censusFixture(t, map[string]string{
		"sound.go":    soundReader,
		"newcomer.go": newcomer,
	})
	assertFailsExactly(t, dir, "newcomer.go:LoadNewcomer")

	// And once it verifies properly, the census lets it go -- otherwise the assertion
	// above would be satisfied by a census that condemns everything.
	fixed := strings.Replace(soundReader, "package fake\n\n", "", 1)
	fixed = strings.Replace(fixed, "func enforcingValidator", "func newcomerValidator", 1)
	fixed = strings.Replace(fixed, "WithPayloadValidator(enforcingValidator)", "WithPayloadValidator(newcomerValidator)", 1)
	fixed = strings.Replace(fixed, "func LoadSound", "func LoadNewcomer", 1)
	fixed = strings.Replace(fixed, "func soundFromChain", "func newcomerFromChain", 1)
	fixed = strings.Replace(fixed, "return soundFromChain(", "return newcomerFromChain(", 1)
	dir = censusFixture(t, map[string]string{
		"newcomer.go": "package fake\n\n" + fixed,
	})
	if failures := analyzeFixture(t, dir).censusFailures(); len(failures) != 0 {
		t.Errorf("a newcomer that verifies correctly stayed in the failure set:\n  %s",
			strings.Join(failures, "\n  "))
	}
}

// NO SIBLING PACKAGE READS A GOVERNED ARTIFACT OUTSIDE THE CENSUS.
//
// THE FAILURE THIS EXISTS FOR. censusPackageDirs is a written list, and a written list is
// the one part of this census nobody's edit can be caught by: dropping a package from it,
// or adding a fifth reader in a package nobody added, leaves every witness above green
// while the claim silently shrinks. That is exactly how the admission-only intermediate
// covered one seam of three and passed.
//
// So the subject set is CHECKED rather than trusted: every Go package in the repository is
// scanned for a read of a governed key, and each one found must be inside the census. The
// list stops being an assertion about four packages and becomes an assertion about all of
// them.
func TestNoPackageOutsideTheCensusReadsAGovernedArtifact(t *testing.T) {
	root := repoRootFromHere(t)
	governed := fixtureGoverned(t)

	inCensus := map[string]bool{}
	for _, rel := range censusPackageDirs {
		inCensus[filepath.Clean(rel)] = true
	}

	var outside []string
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		base := d.Name()
		if base != "." && (strings.HasPrefix(base, ".") || base == "vendor" || base == "testdata" || base == "node_modules") {
			return filepath.SkipDir
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if inCensus[filepath.Clean(rel)] {
			return nil
		}
		entries, readErr := os.ReadDir(path)
		if readErr != nil {
			return readErr
		}
		hasGo := false
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
				hasGo = true
				break
			}
		}
		if !hasGo {
			return nil
		}
		scanned++
		a := analyzePackage(t, path, governed)
		if sites := a.governedSites(); len(sites) > 0 {
			outside = append(outside, fmt.Sprintf("%s: %v", rel, sortedStrings(sites)))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the repository: %v", err)
	}
	if scanned == 0 {
		t.Fatal("no package outside the census was scanned at all; this witness asserts nothing")
	}
	if len(outside) > 0 {
		sort.Strings(outside)
		t.Errorf("%d package(s) read a required-artifact key and are NOT in censusPackageDirs, so "+
			"nothing above judges them:\n  %s\n\nEither add the package to the census or explain why "+
			"its read is not governed. A subject set that is merely written down is the one part of "+
			"this census that can be shrunk without any witness going red.",
			len(outside), strings.Join(outside, "\n  "))
	}
	t.Logf("subject-set floor: %d package(s) scanned outside the census, none reads a governed artifact", scanned)
}
