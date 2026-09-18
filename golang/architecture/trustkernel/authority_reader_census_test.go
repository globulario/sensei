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
//	body, not skipped for lacking a name. Those three sit on WRITER paths, so they witness
//	that discovery sees func literals in real source and nothing more; whether a closure
//	decides a governed READER's fate is proven on a fixture that performs a governed read,
//	because that is the claim, and production does not currently supply the case.
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
//	(2) every governed extraction reads a payload that verification ESTABLISHED, before it
//	    reads it, so a new direct call from an unchecked route is a census failure NAMING
//	    that route.
//
// THE PROVENANCE MODEL, stated so it can be argued with -- and it REPLACES a reachability
// model that was wrong. The rule used to be: a verified root ABSORBS, so every function
// reachable from one is excused. CALL REACHABILITY IS NOT DATA PROVENANCE. Under that rule a
// function could verify a chain soundly and then call a helper that ignored it, read a fresh
// payload off disk and extracted capability_consumption -- and the census reported nothing,
// because the helper sat below a root. Being CALLED BY a verifier is not reading what it
// verified.
//
// So the census follows the VALUE. A governed extraction is certified only when the payload
// it indexes is derived, in that function, from one of two origins:
//
//	A VERIFICATION THIS FUNCTION PERFORMED, through a store whose validator reaches the event
//	table -- resultrecording.LoadRecordedTransition; or
//	A PARAMETER THAT CARRIES A VERIFIED CHAIN -- which is how the other four extractors
//	receive their subject: latestArtifactFromChain takes the chain, decodeGovernedArtifact the
//	entry, chainArtifactJSON and loadTransitionReceipt the payload.
//
// A parameter is a CHANNEL, not a proof. The type says a verified chain belongs there; it does
// not say one arrived. So every parameter a governed read depends on carries a DEMAND, and
// every call site in the package must discharge it. That is what makes delegation legal and
// smuggling illegal at once, and it is also why one sound reader cannot certify an unsound
// sibling: each call site is judged on the argument IT passes, so a good call proves nothing
// about a bad one. admission.LoadRecordedConsumption still owes nothing -- it delegates to a
// loader that verifies its own ledger, and no chain crosses that call -- while a function that
// hands chainArtifactJSON a payload it read off disk is named, with the route.
//
// ORDER FALLS OUT OF THE SAME MACHINERY. Provenance BEGINS somewhere: at the function's first
// statement for a parameter, at the verifying statement for a verification. A read is judged
// against provenance that already existed WHERE THE READ HAPPENS, so a function that indexes a
// payload and verifies afterwards is reading a value nothing had established, and is reported
// with the order named. The previous census wrote that case down as a known limit instead.
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
//	SUBJECTS ARE READERS, NOT STORE OWNERS. A function is a census subject when it lies on
//	a route to a governed extraction -- it is one, or it reaches one. Owning a ledger store
//	is neither necessary nor sufficient: tasksession.appendLedgerControlState constructs a
//	store, verifies it and then WRITES control state through it, and this invariant says
//	nothing about a writer. Discovery still finds every store; only MEMBERSHIP is narrowed,
//	because a subject set padded with writers reports a claim wider than what was measured.
//
//	IDENTITY IS RECEIVER-QUALIFIED, AND EXCUSES DO NOT TRAVEL ON GUESSES. Keying functions
//	on their unqualified name merges same-named methods, and that merge is not a harmless
//	over-approximation: the merged node inherits ONE receiver's verified store, becomes a
//	verified root, and certifies a sibling that reads a governed artifact with nothing
//	verified. Three of the four subject packages already declare Error() on several types,
//	so this is ordinary code, not a hypothetical. Declarations therefore carry
//	receiver-qualified identities, and calls are resolved to them. Where a call CANNOT be
//	resolved -- `x.read()` with several candidates and no pinned receiver -- the census
//	keeps two graphs and uses them asymmetrically:
//
//	    AllEdges, the ACCUSING graph, includes every candidate. An extra edge here can only
//	    add a route the census must judge on its own merits.
//	    Edges, the EXCUSING graph, includes only pinned targets. Being below a verified root
//	    or inside a validator is what lets a read go unjudged, and neither excuse may rest
//	    on a call we guessed.
//
// KNOWN LIMITS, stated rather than hidden -- and every one of them fails CLOSED, which is the
// property that matters in a census: an unmodelled shape is REPORTED, never excused.
//
//	Call resolution is syntactic, not typed. A method reached only through an interface value
//	is an ambiguous candidate: it is accused on its own merits and never excused, so it can
//	name a route no concrete dispatch takes.
//	Provenance travels through the dataflow shapes production uses -- assignment, range, field
//	and index selection, a container that receives an entry, the ledger read/parse pair, and a
//	conduit's declared result. A chain reaching an extractor some other way (through a struct
//	field, a closure capture, an interface) is NOT excused: the read is reported as
//	unestablished, and the repair is to pass it or to widen this list deliberately.
//	Provenance is judged by source POSITION, so a loop whose provenancing assignment is written
//	BELOW the read that uses it is reported even though it executes first. That is a false
//	positive rather than a false negative, and no production reader is written that way.
//	A FUNC LITERAL's body is treated as part of the function that declares it -- its reads, its
//	stores and its provenance all belong to the enclosing name space, which is how the census
//	sees tasksession's three inline validators at all. A closure that verified a chain would
//	therefore credit its enclosing function; no production reader is arranged that way, and the
//	alternative -- attributing a closure's reads to nobody -- would lose them entirely.
//	Analysis is per-package. Every governed extractor except admission's exported loaders is
//	package-private, and those loaders verify their own ledger; an EXPORTED function whose read
//	rests on a parameter is reported as UNESTABLISHED rather than certified, because the
//	callers that would discharge the demand are not in view.

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
	// Name is this declaration's IDENTITY, receiver-qualified for a method:
	// `latestArtifactFromChain`, `(*taskStore).read`. Two methods that share a name on
	// different receivers are two nodes, never one -- see fnIdentity.
	Name  string
	Route string // file.go:<identity>, for naming a failure

	// Calls is every function/method SIMPLE name called anywhere in the body. It is what
	// matches a target declared outside this package, ledger.ValidateTaskEventPayload
	// above all, which has no local identity to resolve to.
	Calls map[string]bool

	// Edges are the local identities this body calls where the target is UNAMBIGUOUS: a
	// free function, or a method call whose receiver type is pinned (`r.m()` inside a
	// method on r) or whose name belongs to exactly one declaration.
	//
	// Edges are the EXCUSING graph. Being reachable from a verified root excuses a
	// function from the extraction rule, and being reachable from a validator excuses its
	// Artifacts reads, so an excuse must never travel along an edge we merely guessed.
	Edges map[string]bool

	// AllEdges is Edges plus every AMBIGUOUS target: when `x.read()` could be any of
	// several same-named methods, all of them are here.
	//
	// AllEdges are the ACCUSING graph. Over-approximating here can only ADD a route the
	// census must judge; over-approximating an excuse would hide one. That asymmetry is
	// the whole reason the two graphs are separate.
	AllEdges map[string]bool

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

	// Params are this declaration's parameter names, positionally, so an argument at a
	// call site can be matched to the parameter it binds. A verified chain arrives through
	// a parameter in every production extractor, and RULE TWO is about that position.
	Params []string

	// Reads is every Artifacts index in the body, with the expression it indexes and the
	// POSITION it indexes at. IndexedKeys says WHICH artifacts a function reads; this says
	// what it read them OFF and WHERE, which is what provenance and ordering are asked
	// about and what the earlier census discarded.
	Reads []artifactRead

	// decl is the declaration itself, kept so provenance can be computed over the body
	// after the whole package's call graph and validator delegation are known.
	decl *ast.FuncDecl
}

// parsedFn is one parsed function declaration, its identity, and the file it came from.
type parsedFn struct {
	base string
	id   string
	fn   *ast.FuncDecl
}

// censusAnalysis is one package's discovered facts.
type censusAnalysis struct {
	Dir string
	Fns map[string]*fnFacts

	// Governed is the artifact-key set the ledger table governs, read from the table
	// rather than hand-listed here.
	Governed map[string]bool

	// freeFuncs maps a plain function name to its identity. A call written as a bare
	// identifier can only reach one of these.
	freeFuncs map[string]string

	// methodsByName maps a method's simple name to every identity declaring it. More than
	// one entry is the AMBIGUITY this census must not resolve by guessing: three of the
	// four subject packages declare Error() on several types already.
	methodsByName map[string][]string

	// prov is every function's value provenance: identity -> name -> the events that
	// established or destroyed that name's derivation from a verified chain.
	prov map[string]map[string][]provEvent

	// resultProv is what each function hands BACK that a caller may read a chain from --
	// the conduit results, by position.
	resultProv map[string][]provFact

	// sites is every call in the package, resolved to the declarations it may reach. RULE
	// TWO is a judgement on call sites, so they are enumerated rather than walked per
	// question.
	sites []callSite
}

// fnIdentity is the census's notion of WHICH function a declaration is.
//
// THE DEFECT THIS EXISTS FOR. Keying on fn.Name.Name alone merges every same-named method
// into one node. That is not a harmless over-approximation: verifiedRoots would mark the
// merged node a root because ONE receiver's method verifies a delegating store, and
// censusFailures skips a root -- so a sibling method that reads a governed artifact with
// nothing verified is CERTIFIED by its namesake. A false negative in a census is worse
// than no census, because it is reported as a pass.
func fnIdentity(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	return "(" + types.ExprString(fn.Recv.List[0].Type) + ")." + fn.Name.Name
}

// receiverName returns the receiver's variable name, so `r.other()` inside a method on r
// resolves to the SAME receiver type instead of fanning out over every namesake.
func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 || len(fn.Recv.List[0].Names) == 0 {
		return ""
	}
	return fn.Recv.List[0].Names[0].Name
}

// receiverType renders the receiver type as fnIdentity spells it.
func receiverType(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	return types.ExprString(fn.Recv.List[0].Type)
}

// identitiesNamed resolves a SIMPLE name to every local identity that could bear it: the
// free function of that name, and every method declaring it. It is how a governed key
// handed to `decodeGovernedArtifact` or `x.read` finds the declaration(s) it may reach.
func (a *censusAnalysis) identitiesNamed(simple string) []string {
	var out []string
	if id, ok := a.freeFuncs[simple]; ok {
		out = append(out, id)
	}
	out = append(out, a.methodsByName[simple]...)
	if len(out) == 0 && a.Fns[simple] != nil {
		out = append(out, simple)
	}
	sort.Strings(out)
	return out
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

	a := &censusAnalysis{
		Dir:           dir,
		Fns:           map[string]*fnFacts{},
		Governed:      governed,
		freeFuncs:     map[string]string{},
		methodsByName: map[string][]string{},
	}

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
						decls = append(decls, parsedFn{base: base, id: fnIdentity(d), fn: d})
					}
				}
			}
		}
	}

	// PASS HALF: the DECLARATION INDEX. Every identity must be known before any call is
	// resolved, because resolving `x.read()` means asking how many declarations bear that
	// method name -- and one is a resolved edge while two is an ambiguity that may accuse
	// but must never excuse.
	for _, d := range decls {
		if d.fn.Recv == nil {
			a.freeFuncs[d.fn.Name.Name] = d.id
			continue
		}
		a.methodsByName[d.fn.Name.Name] = append(a.methodsByName[d.fn.Name.Name], d.id)
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
			factories[d.id] = s
			break
		}
	}

	for _, d := range decls {
		f := a.Fns[d.id]
		if f == nil {
			f = &fnFacts{
				Name:            d.id,
				Route:           d.base + ":" + d.id,
				Calls:           map[string]bool{},
				Edges:           map[string]bool{},
				AllEdges:        map[string]bool{},
				GovernedCallees: map[string]bool{},
				Params:          paramNamesOf(d.fn),
				decl:            d.fn,
			}
			a.Fns[d.id] = f
		}
		for c := range calledNamesIn(d.fn.Body) {
			f.Calls[c] = true
		}
		f.ReturnsStore = f.ReturnsStore || returnsStore(d.fn)
		f.Stores = append(f.Stores, a.storesIn(d.fn.Body, factories, d.fn)...)

		keys, generic := indexedArtifactKeys(d.fn.Body, consts)
		f.IndexedKeys = append(f.IndexedKeys, keys...)
		f.IndexesUnresolvedKey = f.IndexesUnresolvedKey || generic
		for callee := range calleesGivenAGovernedKey(d.fn.Body, consts, governed) {
			f.GovernedCallees[callee] = true
		}
	}

	// PASS ONE AND A QUARTER: the two call graphs. It runs after the declaration index so
	// a method call can be resolved against every receiver that declares it.
	a.resolveEdges(decls)

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

	// PASS THREE: VERIFIED-CHAIN PROVENANCE. It runs last because it needs both the call
	// graph (a conduit's provenance crosses calls) and the validator verdicts (a chain
	// verified against nothing establishes nothing).
	a.resolveProvenance(consts)
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

// resolveEdges fills in each function's two call graphs.
//
// A call is resolved to an IDENTITY, never to a bare name:
//
//	foo()     a free function: exactly one declaration can bear it, so it is resolved.
//	r.foo()   inside a method on receiver r: the receiver type is pinned, so `(*T).foo`
//	          is resolved even when three other types also declare foo.
//	x.foo()   otherwise: every method named foo is a candidate. ONE candidate is resolved.
//	          SEVERAL is AMBIGUOUS -- recorded in AllEdges so the route can still be
//	          accused, withheld from Edges so it can never excuse.
func (a *censusAnalysis) resolveEdges(decls []parsedFn) {
	for _, d := range decls {
		f := a.Fns[d.id]
		if f == nil {
			continue
		}
		recv, recvType := receiverName(d.fn), receiverType(d.fn)
		ast.Inspect(d.fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				if id, ok := a.freeFuncs[fun.Name]; ok {
					f.Edges[id] = true
					f.AllEdges[id] = true
				}
			case *ast.SelectorExpr:
				// The receiver type is pinned when the call is on this method's own
				// receiver, which is the shape most intra-type delegation takes.
				if base, ok := fun.X.(*ast.Ident); ok && recv != "" && base.Name == recv {
					pinned := "(" + recvType + ")." + fun.Sel.Name
					if _, declared := a.Fns[pinned]; declared {
						f.Edges[pinned] = true
						f.AllEdges[pinned] = true
						return true
					}
				}
				candidates := a.methodsByName[fun.Sel.Name]
				for _, id := range candidates {
					f.AllEdges[id] = true
				}
				if len(candidates) == 1 {
					f.Edges[candidates[0]] = true
				}
			}
			return true
		})
	}
}

// reachesName reports whether `from` calls the SIMPLE name `want`, or calls something
// that does, transitively within this package. `want` is a name declared OUTSIDE this
// package -- ledger.ValidateTaskEventPayload -- so it has no local identity to resolve to
// and is matched by name at every node along the way.
//
// WHY TRANSITIVE. This is the INVOKES-not-IS rule. admissionValidator delegates in one
// line, recordingPayloadValidator and dispositionPayloadValidator delegate and then add
// stricter rules of their own, and a validator that dispatched through a helper would be
// no less enforcing. A census that only read the validator's own body would push
// enforcement back out of shared code to stay readable, which is measuring its own
// convenience.
//
// It walks AllEdges, the ACCUSING graph: over-approximating here can only find delegation
// that a stricter walk would miss, and "this validator DOES enforce the table" is the one
// conclusion an extra edge cannot use to hide an unverified read.
func (a *censusAnalysis) reachesName(from, want string) bool {
	seen := map[string]bool{}
	var stack []string
	if _, ok := a.Fns[from]; ok {
		stack = append(stack, from)
	} else {
		stack = append(stack, a.identitiesNamed(from)...)
	}
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
		if f.Calls[want] {
			return true
		}
		for id := range f.AllEdges {
			if !seen[id] {
				stack = append(stack, id)
			}
		}
	}
	return false
}

// reachesIdentity reports whether the identity `from` can reach the identity `want`
// through the ACCUSING graph. It is the question "does this function get to that
// extraction site at all", asked about declarations rather than names.
func (a *censusAnalysis) reachesIdentity(from, want string) bool {
	if from == want {
		return true
	}
	seen := map[string]bool{from: true}
	stack := []string{from}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		f, ok := a.Fns[cur]
		if !ok {
			continue
		}
		for id := range f.AllEdges {
			if id == want {
				return true
			}
			if !seen[id] {
				seen[id] = true
				stack = append(stack, id)
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

// validatorClosure expands validator roots (given as SIMPLE names, which is all a
// WithPayloadValidator argument offers) into the identities that run inside verification.
//
// It walks Edges, the EXCUSING graph. Membership here says "this body's Artifacts reads
// happen during verification, so they are not extractions" -- an excuse. An ambiguous
// method call must not carry it: were `x.check()` allowed to excuse every method named
// check, a governed reader could be waved through for sharing a name with a validator's
// helper.
func (a *censusAnalysis) validatorClosure(roots map[string]bool) map[string]bool {
	out := map[string]bool{}
	var stack []string
	for r := range roots {
		stack = append(stack, a.identitiesNamed(r)...)
	}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if out[cur] {
			continue
		}
		out[cur] = true
		if f, ok := a.Fns[cur]; ok {
			for id := range f.Edges {
				if !out[id] {
					stack = append(stack, id)
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
//
// factories is keyed by factory IDENTITY, so a method factory is attributed to the
// receiver that declares it rather than to whichever namesake was parsed last.
func (a *censusAnalysis) storesIn(body *ast.BlockStmt, factories map[string]storeFact, owner *ast.FuncDecl) []storeFact {
	out := inlineStores(body)
	recv, recvType := receiverName(owner), receiverType(owner)

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
		factoryID, simple := a.resolveFactoryTarget(call.Fun, factories, recv, recvType)
		if factoryID == "" {
			return true
		}
		fact := factories[factoryID]
		lhs, ok := assign.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}
		fact.ViaFactory = simple
		fact.VarName = lhs.Name
		fact.VerifiedVia = verified[lhs.Name]
		out = append(out, fact)
		return true
	})

	sort.Slice(out, func(i, j int) bool { return out[i].VarName < out[j].VarName })
	return out
}

// resolveFactoryTarget names the store factory a call reaches, and the simple name to
// print for it. A method factory whose name is declared on several receivers is left
// UNRESOLVED: attributing the wrong factory's validator to a caller would either invent a
// failure or, worse, lend it a delegating validator it was never given.
func (a *censusAnalysis) resolveFactoryTarget(fun ast.Expr, factories map[string]storeFact, recv, recvType string) (string, string) {
	switch f := fun.(type) {
	case *ast.Ident:
		id, ok := a.freeFuncs[f.Name]
		if !ok {
			return "", ""
		}
		if _, isFactory := factories[id]; !isFactory {
			return "", ""
		}
		return id, f.Name
	case *ast.SelectorExpr:
		if base, ok := f.X.(*ast.Ident); ok && recv != "" && base.Name == recv {
			pinned := "(" + recvType + ")." + f.Sel.Name
			if _, isFactory := factories[pinned]; isFactory {
				return pinned, f.Sel.Name
			}
		}
		var found []string
		for _, id := range a.methodsByName[f.Sel.Name] {
			if _, isFactory := factories[id]; isFactory {
				found = append(found, id)
			}
		}
		if len(found) == 1 {
			return found[0], f.Sel.Name
		}
		return "", ""
	default:
		return "", ""
	}
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
		return a.reachesName(v.Sel.Name, enforcingValidatorName)
	case *ast.Ident:
		if v.Name == enforcingValidatorName {
			return true
		}
		return a.reachesName(v.Name, enforcingValidatorName)
	case *ast.FuncLit:
		// AN ANONYMOUS CLOSURE IS CLASSIFIED ON ITS OWN BODY, not skipped for lacking a
		// name. tasksession installs three of these; name-keyed discovery finds none.
		for name := range calledNamesIn(v.Body) {
			if name == enforcingValidatorName || a.reachesName(name, enforcingValidatorName) {
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
// VERIFIED-CHAIN PROVENANCE, which is what the extraction rule is actually about.
//
// THE DEFECT THIS REPLACES. The rule used to be CALL REACHABILITY: a verified root
// absorbed, and every function reachable from one was excused. Call reachability is not
// data provenance. A root could VerifyChain successfully and then call a helper that
// independently read a fresh payload off disk and extracted capability_consumption from
// it; because the helper sat below a root, the census reported nothing. The artifact
// never passed through the verified chain and the census certified it anyway. Being
// CALLED BY a function that verified something is not the same as READING what it
// verified, and only the second is the invariant.
//
// WHAT REPLACES IT. The census follows the VALUE. An extraction is certified only when
// the payload it indexes is derived, in that function, from one of exactly two origins:
//
//	A VERIFICATION THIS FUNCTION PERFORMED -- `chain, err := store.VerifyChain()` on a
//	store built with a delegating validator; or
//	A PARAMETER THAT CARRIES A VERIFIED CHAIN -- ledger.VerifiedChain, ledger.VerifiedEntry
//	or ledger.TaskEventPayload, which is how all five production extractors receive their
//	subject (latestArtifactFromChain takes the chain, decodeGovernedArtifact takes the
//	entry, chainArtifactJSON and loadTransitionReceipt take the payload).
//
// A parameter is a CHANNEL, not a proof: the type says a verified chain is what belongs
// there, it does not say one arrived. So every parameter a governed read depends on
// carries a DEMAND, and RULE TWO discharges it at every call site in the package. That is
// what makes one sound caller unable to certify an unsound sibling: the good call proves
// nothing about the bad one, because each call site is judged on the argument it passes.
//
// ORDERING FALLS OUT OF THE SAME MACHINERY, which is the second half of this repair. A
// value's provenance begins at the POSITION where it is established -- a parameter from
// the function's first statement, a verification from the statement that performs it --
// and a read is certified only against provenance that already existed where the read
// happens. A function that indexes a payload and verifies afterwards is reading a value
// no verification had yet established, and is reported with the order named. The old
// census said in its own KNOWN LIMITS that statement order was not checked and that such
// a root would pass; it is checked here.
//
// WHAT IS DELIBERATELY CONSERVATIVE. Provenance is granted only through the shapes below;
// anything else DESTROYS it. A call the census does not model, a container that also
// receives an unestablished value, a conduit that returns a fresh read in the position its
// callers read a chain from -- each is reported rather than assumed harmless. A census
// that guesses in the excusing direction is worse than no census, because it reports the
// guess as a pass.
// ---------------------------------------------------------------------------

// provenanceBearingTypes are the types that CARRY a verified chain, and therefore the only
// parameter types that can be a provenance channel.
//
// A `taskDir string` is not one of them, and that distinction is load-bearing: were every
// parameter a channel, `os.ReadFile(taskDir)` would count as chain-derived and a reader
// that parses a payload straight off disk would be certified by its own path argument.
var provenanceBearingTypes = map[string]bool{
	"VerifiedChain":    true,
	"VerifiedEntry":    true,
	"TaskEventPayload": true,
}

// provenancePreservingCalls CARRY provenance without creating it: called with an
// established value they yield one, called with anything else they yield nothing.
//
//	ReadVerifiedPayload takes a ledger.VerifiedEntry and returns that entry's bytes.
//	ParseTaskEventPayload turns bytes into the payload shape -- and is precisely how a
//	  FRESH disk read becomes a payload too, so it must not create provenance, only pass it.
//	ReadFile is here for admission.latestArtifactFromChain, which reads ve.PayloadPath: the
//	  path comes off the verified entry. Given any other path it yields nothing.
var provenancePreservingCalls = map[string]bool{
	"ReadVerifiedPayload":   true,
	"ParseTaskEventPayload": true,
	"ReadFile":              true,
}

// provFact is what the census knows about ONE value's verified-chain provenance: where it
// began, and what it rests on.
type provFact struct {
	// At is the position from which the value carries provenance. A parameter carries it
	// from token.NoPos -- before every statement -- and a verification result from the
	// statement that verified. A read is judged against At, which is how ORDER is checked.
	At token.Pos

	// Verified records provenance from a verification THIS function performed. Nothing
	// further is owed: the chain was established here.
	Verified bool

	// Params are the parameter indices the provenance rests on. Each one is a DEMAND that
	// RULE TWO discharges at every call site.
	Params map[int]bool
}

func (p provFact) established() bool { return p.Verified || len(p.Params) > 0 }

func (p provFact) merge(o provFact) provFact {
	out := provFact{At: p.At, Verified: p.Verified || o.Verified}
	if o.At > out.At {
		out.At = o.At
	}
	for _, src := range []map[int]bool{p.Params, o.Params} {
		for i := range src {
			if out.Params == nil {
				out.Params = map[int]bool{}
			}
			out.Params[i] = true
		}
	}
	return out
}

func (p provFact) signature() string {
	var idx []int
	for i := range p.Params {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	return fmt.Sprintf("%d/%v/%v", p.At, p.Verified, idx)
}

// provEvent is one point in a value's life where its provenance CHANGED. Keeping the
// history rather than a single verdict is what makes both ordering and re-assignment
// judgeable: the census asks what a name meant WHERE IT WAS READ, not what it ever meant.
//
// An event with OK false is provenance LOST -- the name was re-bound to something no
// verification established -- and a read after it is not certified by an earlier binding.
type provEvent struct {
	At   token.Pos
	Fact provFact
	OK   bool
}

// artifactRead is one `<something>.Artifacts[<key>]` index: what it reads, off WHAT, and
// WHERE. The base and the position are the two facts the old census threw away, and they
// are exactly what provenance and ordering are asked about.
type artifactRead struct {
	Key      string
	Resolved bool
	Base     ast.Expr
	BaseText string
	Pos      token.Pos
}

// describeKey names the artifact a read touches, for a failure message.
func (r artifactRead) describeKey() string {
	if r.Resolved {
		return fmt.Sprintf("%q", r.Key)
	}
	return "the required-artifact key it is handed"
}

// artifactReadsIn returns every Artifacts index in body, with its base and position.
func artifactReadsIn(body *ast.BlockStmt, consts map[string]string) []artifactRead {
	var out []artifactRead
	ast.Inspect(body, func(n ast.Node) bool {
		idx, ok := n.(*ast.IndexExpr)
		if !ok {
			return true
		}
		sel, ok := idx.X.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Artifacts" {
			return true
		}
		r := artifactRead{Base: sel.X, BaseText: types.ExprString(sel.X), Pos: idx.Pos()}
		if key, ok := resolveKeyExpr(idx.Index, consts); ok {
			r.Key, r.Resolved = key, true
		}
		out = append(out, r)
		return true
	})
	return out
}

// callSite is one call in the package, resolved to the declarations it may reach.
type callSite struct {
	Caller  string
	Call    *ast.CallExpr
	Pinned  string   // the one identity this call MUST reach, empty when unresolved
	Callees []string // every identity it MIGHT reach -- the accusing set
}

// paramNamesOf flattens a declaration's parameter list into positional names, so an
// argument at a call site can be matched to the parameter it binds. A grouped parameter
// list (`a, b string`) is two positions, and `_` holds its place rather than shifting the
// ones after it.
func paramNamesOf(fn *ast.FuncDecl) []string {
	var out []string
	if fn.Type.Params == nil {
		return out
	}
	for _, field := range fn.Type.Params.List {
		if len(field.Names) == 0 {
			out = append(out, "_")
			continue
		}
		for _, n := range field.Names {
			out = append(out, n.Name)
		}
	}
	return out
}

// paramCarriesAChain reports whether a TYPE is one a verified chain travels in, peeling
// the pointers, slices and maps it may travel behind.
//
// THE MAP CASE IS PRODUCTION, not generality for its own sake:
// tasksession.scopeVerificationBinds receives the whole verified snapshot as
// `latest map[closureprotocol.LedgerEventType]ledger.VerifiedEntry` and reads its governed
// artifacts out of that map. A census that only understood a bare parameter would call the
// entries it takes from there unestablished and condemn a correct function -- and the first
// false positive is what gets a census disabled.
func paramCarriesAChain(t ast.Expr) bool {
	switch v := t.(type) {
	case *ast.StarExpr:
		return paramCarriesAChain(v.X)
	case *ast.ArrayType:
		return paramCarriesAChain(v.Elt)
	case *ast.Ellipsis:
		return paramCarriesAChain(v.Elt)
	case *ast.MapType:
		return paramCarriesAChain(v.Value)
	case *ast.SelectorExpr:
		return provenanceBearingTypes[v.Sel.Name]
	case *ast.Ident:
		return provenanceBearingTypes[v.Name]
	}
	return false
}

// chainBearingParams returns the positional indices whose type carries a verified chain.
func chainBearingParams(fn *ast.FuncDecl) map[int]bool {
	out := map[int]bool{}
	if fn.Type.Params == nil {
		return out
	}
	i := 0
	for _, field := range fn.Type.Params.List {
		n := len(field.Names)
		if n == 0 {
			n = 1
		}
		for k := 0; k < n; k++ {
			if paramCarriesAChain(field.Type) {
				out[i] = true
			}
			i++
		}
	}
	return out
}

// chainBearingResults returns the result positions whose TYPE carries a verified chain.
//
// PROVENANCE TRAVELS ONLY IN THE TYPES THAT CARRY A CHAIN, and the restriction matters at a
// function boundary for the same reason it matters on a parameter: across a call the census
// sees a signature, not a body. Inside one body it can watch a chain become entry bytes and
// then a payload, so local values are not type-restricted; a RESULT is a channel, and a
// channel is declared.
//
// It is also what keeps RULE THREE honest. Without it the laundering rule fired on
// tasksession.initializeLedgerState returning a ledger.Head and on
// resultrecording.RecordTransition returning a RecordResult -- neither of which any caller
// can read an Artifacts map off, so neither can launder a payload.
func chainBearingResults(fn *ast.FuncDecl) map[int]bool {
	out := map[int]bool{}
	if fn.Type.Results == nil {
		return out
	}
	i := 0
	for _, field := range fn.Type.Results.List {
		n := len(field.Names)
		if n == 0 {
			n = 1
		}
		for k := 0; k < n; k++ {
			if paramCarriesAChain(field.Type) {
				out[i] = true
			}
			i++
		}
	}
	return out
}

// isZeroValueExpr reports whether an expression is a plain zero value.
//
// It exists for ERROR RETURNS. Every conduit in these packages returns
// `ledger.TaskEventPayload{}, err` on failure, and a zero payload carries no artifacts:
// reading one finds nothing and fails closed. Treating those as laundering would condemn
// five real functions, while treating a NON-zero unestablished return as harmless is
// exactly the laundering RULE THREE exists to catch.
func isZeroValueExpr(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name == "nil" || v.Name == "false"
	case *ast.BasicLit:
		return v.Value == `""` || v.Value == "0"
	case *ast.CompositeLit:
		return len(v.Elts) == 0
	case *ast.UnaryExpr:
		return isZeroValueExpr(v.X)
	case *ast.ParenExpr:
		return isZeroValueExpr(v.X)
	}
	return false
}

// pinnedCallee resolves a call to the ONE local identity it must reach, or "" when the
// target cannot be pinned. Provenance may only travel here: granting it through a guessed
// target is an excuse resting on a guess.
func (a *censusAnalysis) pinnedCallee(fun ast.Expr, recv, recvType string) string {
	switch f := fun.(type) {
	case *ast.Ident:
		if id, ok := a.freeFuncs[f.Name]; ok {
			return id
		}
	case *ast.SelectorExpr:
		if base, ok := f.X.(*ast.Ident); ok && recv != "" && base.Name == recv {
			pinned := "(" + recvType + ")." + f.Sel.Name
			if _, declared := a.Fns[pinned]; declared {
				return pinned
			}
		}
		if c := a.methodsByName[f.Sel.Name]; len(c) == 1 {
			return c[0]
		}
	}
	return ""
}

// candidateCallees resolves a call to every local identity it MIGHT reach. An unresolved
// method call accuses every namesake, which can only add a demand to discharge.
func (a *censusAnalysis) candidateCallees(fun ast.Expr, recv, recvType string) []string {
	if id := a.pinnedCallee(fun, recv, recvType); id != "" {
		return []string{id}
	}
	if sel, ok := fun.(*ast.SelectorExpr); ok {
		return a.methodsByName[sel.Sel.Name]
	}
	return nil
}

// resolveProvenance computes every function's value provenance, the chain-derived results
// it hands back to its callers, and the laundering it commits.
//
// The FIXPOINT is over CONDUITS. A function that returns a value derived from a chain it
// received is a conduit -- resultrecording.latestEventFromChain and loadEventPayload,
// questiondisposition.latestResultTransition -- and one conduit feeds another, so a single
// pass would miss the second. Provenance only ever grows here, so the iteration settles.
func (a *censusAnalysis) resolveProvenance(consts map[string]string) {
	a.prov = map[string]map[string][]provEvent{}
	a.resultProv = map[string][]provFact{}
	a.sites = nil

	for _, name := range a.names() {
		f := a.Fns[name]
		if f.decl == nil || f.decl.Body == nil {
			continue
		}
		f.Reads = artifactReadsIn(f.decl.Body, consts)
	}

	for round := 0; round <= len(a.Fns)+1; round++ {
		changed := false
		for _, name := range a.names() {
			events, results := a.provenanceIn(name)
			a.prov[name] = events
			if resultsDiffer(a.resultProv[name], results) {
				a.resultProv[name] = results
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	// The call sites, collected once the provenance they are judged against is settled.
	for _, name := range a.names() {
		f := a.Fns[name]
		if f.decl == nil || f.decl.Body == nil {
			continue
		}
		recv, recvType := receiverName(f.decl), receiverType(f.decl)
		ast.Inspect(f.decl.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			cs := callSite{
				Caller:  name,
				Call:    call,
				Pinned:  a.pinnedCallee(call.Fun, recv, recvType),
				Callees: a.candidateCallees(call.Fun, recv, recvType),
			}
			if len(cs.Callees) > 0 {
				a.sites = append(a.sites, cs)
			}
			return true
		})
	}
}

func resultsDiffer(old, new []provFact) bool {
	if len(old) != len(new) {
		return true
	}
	for i := range old {
		if old[i].signature() != new[i].signature() {
			return true
		}
	}
	return false
}

// provenanceIn computes one function's value provenance and its conduit results.
func (a *censusAnalysis) provenanceIn(name string) (map[string][]provEvent, []provFact) {
	f := a.Fns[name]
	events := map[string][]provEvent{}
	if f == nil || f.decl == nil || f.decl.Body == nil {
		return events, nil
	}
	body := f.decl.Body

	// SEED ONE: the parameters that carry a chain, from before the first statement.
	for i := range chainBearingParams(f.decl) {
		if i >= len(f.Params) || f.Params[i] == "_" || f.Params[i] == "" {
			continue
		}
		mark(events, f.Params[i], provEvent{At: token.NoPos, OK: true,
			Fact: provFact{At: token.NoPos, Params: map[int]bool{i: true}}})
	}

	// SEED TWO: a verification THIS function performed, at the position it performed it.
	// Only a store whose validator reaches the event table counts: a chain verified
	// against nothing establishes nothing, which is what makes HALF ONE and HALF TWO one
	// invariant rather than two.
	verifiedVars := map[string]bool{}
	for _, s := range f.Stores {
		if s.verified() && s.VarName != "" {
			verifiedVars[s.VarName] = true
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 || len(assign.Lhs) == 0 {
			return true
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !verificationMethodNames[sel.Sel.Name] {
			return true
		}
		base, ok := sel.X.(*ast.Ident)
		if !ok || !verifiedVars[base.Name] {
			return true
		}
		if id, ok := assign.Lhs[0].(*ast.Ident); ok && id.Name != "_" {
			mark(events, id.Name, provEvent{At: call.Pos(), OK: true,
				Fact: provFact{At: call.Pos(), Verified: true}})
		}
		return true
	})

	// FORWARD PROPAGATION. Statements are visited in source order, so one pass carries
	// provenance down a function; the loop is there for the shapes that do not, and it
	// stops as soon as nothing moves.
	recv, recvType := receiverName(f.decl), receiverType(f.decl)
	for round := 0; round < 6; round++ {
		changed := false
		ast.Inspect(body, func(n ast.Node) bool {
			switch st := n.(type) {
			case *ast.AssignStmt:
				if a.propagateAssign(name, recv, recvType, st, events) {
					changed = true
				}
			case *ast.RangeStmt:
				if a.propagateRange(name, st, events) {
					changed = true
				}
			case *ast.ValueSpec:
				if a.propagateValueSpec(name, st, events) {
					changed = true
				}
			}
			return true
		})
		if !changed {
			break
		}
	}

	// THE CONDUIT RESULTS: what this function hands back that a caller may read a chain
	// from. A result index is a conduit when ANY return establishes it -- the question is
	// existential, because every error path returns a zero value -- and RULE THREE is what
	// keeps that from becoming a laundering channel.
	results := make([]provFact, resultCount(f.decl))
	chainResults := chainBearingResults(f.decl)
	ast.Inspect(body, func(n ast.Node) bool {
		if lit, isLit := n.(*ast.FuncLit); isLit {
			_ = lit
			return false // a closure's returns are its own, not this function's
		}
		ret, ok := n.(*ast.ReturnStmt)
		if !ok || len(ret.Results) == 0 {
			return true
		}
		for i, e := range ret.Results {
			if i >= len(results) || !chainResults[i] {
				continue
			}
			if fact, ok := a.exprProvenance(name, recv, recvType, e, ret.Pos(), events); ok {
				results[i] = results[i].merge(fact)
			}
		}
		return true
	})
	return events, results
}

func resultCount(fn *ast.FuncDecl) int {
	if fn.Type.Results == nil {
		return 0
	}
	n := 0
	for _, field := range fn.Type.Results.List {
		if len(field.Names) == 0 {
			n++
			continue
		}
		n += len(field.Names)
	}
	return n
}

// mark records a provenance event, reporting whether anything changed.
func mark(events map[string][]provEvent, ident string, ev provEvent) bool {
	if ident == "" || ident == "_" {
		return false
	}
	for _, existing := range events[ident] {
		if existing.At == ev.At && existing.OK == ev.OK && existing.Fact.signature() == ev.Fact.signature() {
			return false
		}
	}
	events[ident] = append(events[ident], ev)
	sort.SliceStable(events[ident], func(i, j int) bool { return events[ident][i].At < events[ident][j].At })
	return true
}

// propagateAssign carries provenance across one assignment -- or REVOKES it.
//
// The revocation is not an extra: `payload, err := ledger.ParseTaskEventPayload(freshBytes)`
// re-binds a name that may have held a chain-derived payload a moment earlier, and a read
// after it reads the fresh bytes. Recording the loss is how the census answers what a name
// meant where it was read instead of what it ever meant.
func (a *censusAnalysis) propagateAssign(fnName, recv, recvType string, st *ast.AssignStmt, events map[string][]provEvent) bool {
	at := st.Pos()
	changed := false

	assign := func(lhs ast.Expr, fact provFact, ok bool) {
		switch target := lhs.(type) {
		case *ast.Ident:
			if ok {
				fact.At = at
				changed = mark(events, target.Name, provEvent{At: at, OK: true, Fact: fact}) || changed
			} else {
				changed = mark(events, target.Name, provEvent{At: at, OK: false}) || changed
			}
		case *ast.IndexExpr:
			// A CONTAINER RECEIVES. tasksession indexes its verified chain into
			// `latest[eventType] = ve` and reads the entries back out of the map, so a
			// container fed an established value carries it -- and one fed an
			// UNESTABLISHED value loses it, which is RULE THREE's other half.
			if base, isIdent := target.X.(*ast.Ident); isIdent {
				if ok {
					fact.At = at
					changed = mark(events, base.Name, provEvent{At: at, OK: true, Fact: fact}) || changed
				} else if _, had := usableIn(events, base.Name, at); had {
					// The loss is judged against the provenance built SO FAR in this pass, not
					// against a previous round's settled answer: on the first pass there is no
					// settled answer, and a check that consulted one would silently pass.
					changed = mark(events, base.Name, provEvent{At: at, OK: false}) || changed
				}
			}
		}
	}

	if len(st.Rhs) == 1 {
		if call, isCall := st.Rhs[0].(*ast.CallExpr); isCall {
			if g := a.pinnedCallee(call.Fun, recv, recvType); g != "" {
				results := a.resultProv[g]
				for i, lhs := range st.Lhs {
					var fact provFact
					ok := false
					if i < len(results) {
						fact, ok = a.substituteResult(fnName, recv, recvType, results[i], call, at, events)
					}
					assign(lhs, fact, ok)
				}
				return changed
			}
			fact, ok := a.exprProvenance(fnName, recv, recvType, call, at, events)
			for i, lhs := range st.Lhs {
				// A multi-valued preserving call hands the value back FIRST and an error
				// after it; only the first result carries the payload.
				assign(lhs, fact, ok && (i == 0 || len(st.Lhs) == 1))
			}
			return changed
		}
	}
	if len(st.Lhs) == len(st.Rhs) {
		for i, lhs := range st.Lhs {
			fact, ok := a.exprProvenance(fnName, recv, recvType, st.Rhs[i], at, events)
			assign(lhs, fact, ok)
		}
		return changed
	}
	if len(st.Rhs) == 1 {
		fact, ok := a.exprProvenance(fnName, recv, recvType, st.Rhs[0], at, events)
		for _, lhs := range st.Lhs {
			assign(lhs, fact, ok)
		}
	}
	return changed
}

// propagateRange carries provenance into a range variable: `for _, ve := range chain.Entries`
// is how three of the five production extractors reach their entry.
func (a *censusAnalysis) propagateRange(fnName string, st *ast.RangeStmt, events map[string][]provEvent) bool {
	f := a.Fns[fnName]
	recv, recvType := receiverName(f.decl), receiverType(f.decl)
	fact, ok := a.exprProvenance(fnName, recv, recvType, st.X, st.Pos(), events)
	changed := false
	if id, isIdent := st.Value.(*ast.Ident); isIdent {
		if ok {
			fact.At = st.Pos()
			changed = mark(events, id.Name, provEvent{At: st.Pos(), OK: true, Fact: fact}) || changed
		} else {
			changed = mark(events, id.Name, provEvent{At: st.Pos(), OK: false}) || changed
		}
	}
	return changed
}

// propagateValueSpec carries provenance across `var x = <expr>`.
func (a *censusAnalysis) propagateValueSpec(fnName string, vs *ast.ValueSpec, events map[string][]provEvent) bool {
	if len(vs.Values) == 0 {
		return false
	}
	f := a.Fns[fnName]
	recv, recvType := receiverName(f.decl), receiverType(f.decl)
	changed := false
	for i, n := range vs.Names {
		if i >= len(vs.Values) {
			break
		}
		fact, ok := a.exprProvenance(fnName, recv, recvType, vs.Values[i], vs.Pos(), events)
		if ok {
			fact.At = vs.Pos()
			changed = mark(events, n.Name, provEvent{At: vs.Pos(), OK: true, Fact: fact}) || changed
		}
	}
	return changed
}

// substituteResult translates a conduit's result provenance into the CALLER's terms: what
// the conduit rests on in its own parameters becomes what the caller passed there.
//
// This is what keeps a conduit from being a certificate. latestEventFromChain's payload is
// chain-derived BECAUSE of its chain argument, so a caller that hands it a fabricated
// chain gets back a value with no provenance -- rather than one blessed by the conduit's
// signature.
func (a *censusAnalysis) substituteResult(fnName, recv, recvType string, rf provFact, call *ast.CallExpr, at token.Pos, events map[string][]provEvent) (provFact, bool) {
	if !rf.established() {
		return provFact{}, false
	}
	out := provFact{At: at}
	ok := false
	if rf.Verified {
		// The conduit verified the chain itself; the caller owes nothing.
		out.Verified = true
		ok = true
	}
	for q := range rf.Params {
		if q >= len(call.Args) {
			continue
		}
		if argFact, argOK := a.exprProvenance(fnName, recv, recvType, call.Args[q], at, events); argOK {
			out = out.merge(argFact)
			out.At = at
			ok = true
		}
	}
	return out, ok
}

// exprProvenance reports whether an expression is derived from an established verified
// chain AT position `at`, and what that derivation rests on.
func (a *censusAnalysis) exprProvenance(fnName, recv, recvType string, e ast.Expr, at token.Pos, events map[string][]provEvent) (provFact, bool) {
	switch v := e.(type) {
	case *ast.Ident:
		return usableIn(events, v.Name, at)
	case *ast.SelectorExpr:
		return a.exprProvenance(fnName, recv, recvType, v.X, at, events)
	case *ast.IndexExpr:
		return a.exprProvenance(fnName, recv, recvType, v.X, at, events)
	case *ast.SliceExpr:
		return a.exprProvenance(fnName, recv, recvType, v.X, at, events)
	case *ast.ParenExpr:
		return a.exprProvenance(fnName, recv, recvType, v.X, at, events)
	case *ast.StarExpr:
		return a.exprProvenance(fnName, recv, recvType, v.X, at, events)
	case *ast.UnaryExpr:
		return a.exprProvenance(fnName, recv, recvType, v.X, at, events)
	case *ast.TypeAssertExpr:
		return a.exprProvenance(fnName, recv, recvType, v.X, at, events)
	case *ast.KeyValueExpr:
		return a.exprProvenance(fnName, recv, recvType, v.Value, at, events)
	case *ast.CompositeLit:
		out, ok := provFact{At: at}, false
		for _, elt := range v.Elts {
			if fact, eltOK := a.exprProvenance(fnName, recv, recvType, elt, at, events); eltOK {
				out, ok = out.merge(fact), true
			}
		}
		return out, ok
	case *ast.CallExpr:
		if g := a.pinnedCallee(v.Fun, recv, recvType); g != "" {
			results := a.resultProv[g]
			if len(results) == 0 {
				return provFact{}, false
			}
			return a.substituteResult(fnName, recv, recvType, results[0], v, at, events)
		}
		sel, isSel := v.Fun.(*ast.SelectorExpr)
		if !isSel || !provenancePreservingCalls[sel.Sel.Name] {
			// A call the census does not model DESTROYS provenance rather than passing it.
			return provFact{}, false
		}
		out, ok := provFact{At: at}, false
		for _, arg := range v.Args {
			if fact, argOK := a.exprProvenance(fnName, recv, recvType, arg, at, events); argOK {
				out, ok = out.merge(fact), true
			}
		}
		return out, ok
	}
	return provFact{}, false
}

// usableIn answers what a name meant at a position: the LAST event at or before it.
func usableIn(events map[string][]provEvent, ident string, at token.Pos) (provFact, bool) {
	var best provEvent
	found := false
	for _, ev := range events[ident] {
		if ev.At > at {
			break
		}
		best, found = ev, true
	}
	if !found || !best.OK {
		return provFact{}, false
	}
	return best.Fact, true
}

// usableFact is usableIn against a function's settled provenance.
func (a *censusAnalysis) usableFact(fnName, ident string, at token.Pos) (provFact, bool) {
	return usableIn(a.prov[fnName], ident, at)
}

// readProvenance answers whether ONE governed read's payload was established where it was
// read, and what it rests on.
func (a *censusAnalysis) readProvenance(fnName string, r artifactRead) (provFact, bool) {
	f := a.Fns[fnName]
	if f == nil || f.decl == nil {
		return provFact{}, false
	}
	return a.exprProvenance(fnName, receiverName(f.decl), receiverType(f.decl), r.Base, r.Pos, a.prov[fnName])
}

// verificationAfter reports the position of a verification this function performs AFTER the
// given read, when there is one.
//
// IT IS THE ORDERING DIAGNOSIS, and it is the limit the previous census wrote down instead
// of closing: "statement ORDER inside a verified root is not checked: a root that indexed
// before verifying would pass." A function that extracts a governed artifact and verifies
// the ledger afterwards satisfies every reachability rule -- it IS a verified root, it holds
// a delegating validator, the chain it verifies is sound -- and read nothing that
// verification had established. Verification must DOMINATE extraction, so a verification
// found below a read is named as the order it is, rather than counted as a verification the
// read enjoyed.
func (a *censusAnalysis) verificationAfter(fnName string, at token.Pos) (token.Pos, bool) {
	earliest := token.NoPos
	for _, events := range a.prov[fnName] {
		for _, ev := range events {
			if !ev.OK || !ev.Fact.Verified || ev.At <= at {
				continue
			}
			if earliest == token.NoPos || ev.At < earliest {
				earliest = ev.At
			}
		}
	}
	return earliest, earliest != token.NoPos
}

// inScopeReads are the reads of ONE function that this invariant judges: a governed key,
// or a key it receives -- a generic extractor reads whatever its callers hand it, and
// governedSites has already established that some caller hands it a governed one.
//
// A resolved UNGOVERNED key is deliberately excluded. These packages also read result
// stages, impact reports and the closure request out of the same payloads; condemning
// those would widen the invariant past what the ledger event table governs.
func (a *censusAnalysis) inScopeReads(fnName string) []artifactRead {
	f := a.Fns[fnName]
	if f == nil {
		return nil
	}
	var out []artifactRead
	for _, r := range f.Reads {
		if !r.Resolved || a.Governed[r.Key] {
			out = append(out, r)
		}
	}
	return out
}

// neededParams are the parameters whose provenance a governed read DEPENDS ON, propagated
// back through the call sites that supply them.
//
// This is the set RULE TWO discharges. It is computed rather than assumed because demanding
// provenance of every parameter that happens to receive an established value would condemn
// real code: questiondisposition.readArtifact is handed a ref taken off a verified payload,
// but nothing it does reads a required artifact, so nothing is owed at its call sites.
func (a *censusAnalysis) neededParams() map[string]map[int]bool {
	governed := a.governedSites()
	needed := map[string]map[int]bool{}
	add := func(fn string, p int) bool {
		if needed[fn] == nil {
			needed[fn] = map[int]bool{}
		}
		if needed[fn][p] {
			return false
		}
		needed[fn][p] = true
		return true
	}
	for name := range governed {
		f := a.Fns[name]
		if f == nil || f.IsValidatorBody {
			continue
		}
		for _, r := range a.inScopeReads(name) {
			fact, ok := a.readProvenance(name, r)
			if !ok {
				continue
			}
			for p := range fact.Params {
				add(name, p)
			}
		}
	}
	for round := 0; round <= len(a.Fns)+1; round++ {
		changed := false
		for _, site := range a.sites {
			caller := a.Fns[site.Caller]
			if caller == nil || caller.decl == nil {
				continue
			}
			recv, recvType := receiverName(caller.decl), receiverType(caller.decl)
			for _, callee := range site.Callees {
				for p := range needed[callee] {
					if p >= len(site.Call.Args) {
						continue
					}
					fact, ok := a.exprProvenance(site.Caller, recv, recvType, site.Call.Args[p], site.Call.Pos(), a.prov[site.Caller])
					if !ok {
						continue
					}
					for q := range fact.Params {
						if add(site.Caller, q) {
							changed = true
						}
					}
				}
			}
		}
		if !changed {
			break
		}
	}
	return needed
}

// paramLabel names a parameter for a failure message.
func (a *censusAnalysis) paramLabel(fnName string, p int) string {
	f := a.Fns[fnName]
	if f == nil || p >= len(f.Params) {
		return fmt.Sprintf("parameter %d", p)
	}
	return fmt.Sprintf("parameter %d (%s)", p, f.Params[p])
}

// simpleNameOf renders an identity without its receiver, for reading inside a sentence.
func simpleNameOf(identity string) string {
	if i := strings.LastIndex(identity, "."); i >= 0 {
		return identity[i+1:]
	}
	return identity
}

// isExportedIdentity reports whether a declaration is reachable from outside the package.
func isExportedIdentity(identity string) bool {
	name := simpleNameOf(identity)
	if name == "" {
		return false
	}
	r := name[0]
	return r >= 'A' && r <= 'Z'
}

// ---------------------------------------------------------------------------
// THE CLASSIFIER. Extracted from the census so it can be driven against synthetic
// sources: every real-tree assertion is trivially satisfied on a healthy tree, so
// disabling one changes nothing, and a census whose mutants all survive is not evidence.
// ---------------------------------------------------------------------------

// ownsStore reports whether this function holds a ledger store, inline or from a factory.
//
// OWNING A STORE IS NOT BEING A READER, and conflating the two is the defect this split
// repairs. tasksession.appendLedgerControlState constructs a store, verifies it, and then
// WRITES: StoreArtifactBytes, Append. It extracts no required artifact and this invariant
// says nothing about it. Counting it as a census subject padded the subject set with
// writers and made the reported claim -- "every reading path" -- wider than what was
// measured. Store discovery stays exactly as thorough; only MEMBERSHIP moved.
func (f *fnFacts) ownsStore() bool {
	return len(f.Stores) > 0
}

// touchesArtifacts reports whether this function reaches into an Artifacts map itself.
func (f *fnFacts) touchesArtifacts() bool {
	return len(f.IndexedKeys) > 0 || f.IndexesUnresolvedKey
}

// readerSubjects is the census's SUBJECT SET: the functions this invariant is about.
//
// A function is a subject exactly when it lies on a route to a required-artifact
// extraction -- it is such a site, or it can reach one. That is the definition the claim
// uses, so it is the definition membership must use. A function that owns a store but
// reaches no governed extraction is a WRITER or an ungoverned reader; demanding a
// table-enforcing validator of it would be asserting something this invariant does not
// say, and counting it would inflate every number the census reports.
//
// Reachability is taken over AllEdges, the accusing graph: a route that might exist is a
// route that must be judged, and a subject wrongly included is judged on its own merits
// while a subject wrongly excluded is never judged at all.
func (a *censusAnalysis) readerSubjects() map[string]bool {
	governed := a.governedSites()
	out := map[string]bool{}
	for _, name := range a.names() {
		f := a.Fns[name]
		if f.IsValidatorBody {
			continue
		}
		if governed[name] {
			out[name] = true
			continue
		}
		for site := range governed {
			if a.reachesIdentity(name, site) {
				out[name] = true
				break
			}
		}
	}
	return out
}

// verifiedRoots are the functions that build a store with a DELEGATING validator and verify
// it. Such a function ESTABLISHES a chain, which seeds provenance for the values it derives
// from that chain -- and nothing more.
//
// IT DOES NOT ABSORB. The extraction rule used to excuse everything a root called, and that
// was the defect: a root's callee may ignore the chain entirely. What a verification grants is
// provenance to the value it produced, from the position it produced it; who the verifier goes
// on to call is not part of the claim. This set survives as the seed for that, and as the way
// a witness can say "this fixture really is a verified root, and is still reported".
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
			for _, cid := range a.identitiesNamed(callee) {
				for site := range index {
					if site == cid || a.reachesIdentity(cid, site) {
						out[site] = true
					}
				}
			}
		}
	}
	return out
}

// censusFailures is the whole judgement, in the two halves the invariant needs.
func (a *censusAnalysis) censusFailures() []string {
	var failures []string

	// HALF ONE: every store on a READING path installs a DELEGATING validator.
	//
	// The subject set is the route-to-a-governed-extraction set, not the store-owning set.
	// A store held by a writer is discovered and classified like any other -- it simply
	// is not evidence about an invariant that speaks only of reads.
	subjects := a.readerSubjects()
	for _, name := range a.names() {
		f := a.Fns[name]
		if !subjects[name] {
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

	// HALF TWO: every governed extraction reads a payload some verification ESTABLISHED,
	// before it reads it, and every call site supplies the provenance it rests on.
	failures = append(failures, a.extractionFailures()...)
	return failures
}

// provenanceTally is what HALF TWO actually judged on a package, counted.
//
// A RULE THAT JUDGES NOTHING REPORTS A PASS, which is the failure this whole file was written
// against: the admission-only intermediate covered one seam of three and passed. So the
// provenance rules are measured the same way the validator rules are. If the reads resting on
// a parameter fall to zero, RULE TWO is discharging nothing and the census has quietly become
// a claim about self-verifying functions only; if the discharged call sites fall to zero, no
// caller is being asked to supply anything.
type provenanceTally struct {
	ReadsFromOwnVerification int
	ReadsRestingOnAParameter int
	DemandedParameters       int
	DischargedCallSites      int
}

func (a *censusAnalysis) provenanceTally() provenanceTally {
	var t provenanceTally
	governed := a.governedSites()
	needed := a.neededParams()
	for name := range governed {
		f := a.Fns[name]
		if f == nil || f.IsValidatorBody {
			continue
		}
		for _, r := range a.inScopeReads(name) {
			fact, ok := a.readProvenance(name, r)
			if !ok {
				continue
			}
			if fact.Verified {
				t.ReadsFromOwnVerification++
			}
			if len(fact.Params) > 0 {
				t.ReadsRestingOnAParameter++
			}
		}
	}
	for name := range needed {
		t.DemandedParameters += len(needed[name])
	}
	for _, site := range a.sites {
		caller := a.Fns[site.Caller]
		if caller == nil || caller.decl == nil {
			continue
		}
		recv, recvType := receiverName(caller.decl), receiverType(caller.decl)
		for _, callee := range site.Callees {
			for p := range needed[callee] {
				if p >= len(site.Call.Args) {
					continue
				}
				if _, ok := a.exprProvenance(site.Caller, recv, recvType, site.Call.Args[p], site.Call.Pos(), a.prov[site.Caller]); ok {
					t.DischargedCallSites++
				}
			}
		}
	}
	return t
}

// extractionFailures is HALF TWO: the extraction boundary, judged on PROVENANCE.
//
// Four rules, and each one exists because the rule it replaced was satisfiable while the
// invariant was false:
//
//	ONE   the payload a governed read indexes must be derived from a verification, at the
//	      point of the read. This is what call reachability could not say: a helper below a
//	      verified root that reads its own fresh payload is named here.
//	TWO   every call site must supply the provenance a callee's read rests on. A parameter
//	      typed ledger.VerifiedChain is a channel, not a proof, and one sound caller says
//	      nothing about an unsound sibling.
//	THREE a conduit must not launder: a function that returns a chain-derived value in some
//	      result position must not return an unestablished one there, or RULE TWO's
//	      substitution would carry a blessing the value never earned.
//	FOUR  a read whose provenance rests on a parameter of an EXPORTED function cannot be
//	      certified by a per-package census at all, because the callers are not all here.
func (a *censusAnalysis) extractionFailures() []string {
	var failures []string
	governed := a.governedSites()
	needed := a.neededParams()

	// RULE ONE: the read itself.
	for _, name := range a.names() {
		f := a.Fns[name]
		if f.IsValidatorBody || !governed[name] {
			continue
		}
		for _, r := range a.inScopeReads(name) {
			if _, ok := a.readProvenance(name, r); ok {
				continue
			}
			if at, later := a.verificationAfter(name, r.Pos); later {
				failures = append(failures, fmt.Sprintf(
					"%s: reads %s out of %s BEFORE the verification this same function performs -- the "+
						"chain is verified %d byte(s) LATER, so nothing had established %s where it was "+
						"read. Verification must DOMINATE extraction; an artifact read first and "+
						"verified afterwards was read OUTSIDE the verified boundary",
					f.Route, r.describeKey(), r.BaseText, int(at-r.Pos), r.BaseText))
				continue
			}
			failures = append(failures, fmt.Sprintf(
				"%s: reads %s out of %s, which no verified chain established -- nothing in this "+
					"function derives %s from a chain it verified through a validator that invokes %s, "+
					"nor from a parameter carrying one. Being CALLED BY a function that verified "+
					"something is not reading what it verified: this extraction is OUTSIDE the "+
					"verified boundary",
				f.Route, r.describeKey(), r.BaseText, r.BaseText, enforcingValidatorName))
		}
	}

	// RULE TWO: the call sites that must discharge what RULE ONE rested on.
	for _, site := range a.sites {
		caller := a.Fns[site.Caller]
		if caller == nil || caller.decl == nil {
			continue
		}
		recv, recvType := receiverName(caller.decl), receiverType(caller.decl)
		for _, callee := range site.Callees {
			for _, p := range sortedInts(needed[callee]) {
				if p >= len(site.Call.Args) {
					continue
				}
				if _, ok := a.exprProvenance(site.Caller, recv, recvType, site.Call.Args[p], site.Call.Pos(), a.prov[site.Caller]); ok {
					continue
				}
				failures = append(failures, fmt.Sprintf(
					"%s: hands %s a %s that no verified chain established (%s), and that is the value "+
						"%s reads a required artifact out of; along %s -> %s the extraction is OUTSIDE "+
						"the verified boundary",
					caller.Route, simpleNameOf(callee), a.paramLabel(callee, p),
					types.ExprString(site.Call.Args[p]), simpleNameOf(callee),
					caller.Route, a.routeOf(callee)))
			}
		}
	}

	// RULE THREE: conduits that launder.
	for _, name := range a.names() {
		f := a.Fns[name]
		if f.IsValidatorBody || f.decl == nil || f.decl.Body == nil {
			continue
		}
		results := a.resultProv[name]
		if len(results) == 0 {
			continue
		}
		recv, recvType := receiverName(f.decl), receiverType(f.decl)
		ast.Inspect(f.decl.Body, func(n ast.Node) bool {
			if _, isLit := n.(*ast.FuncLit); isLit {
				return false
			}
			ret, ok := n.(*ast.ReturnStmt)
			if !ok {
				return true
			}
			for i, e := range ret.Results {
				if i >= len(results) || !results[i].established() {
					continue
				}
				if isZeroValueExpr(e) {
					continue
				}
				if _, ok := a.exprProvenance(name, recv, recvType, e, ret.Pos(), a.prov[name]); ok {
					continue
				}
				failures = append(failures, fmt.Sprintf(
					"%s: returns %s at result %d, where its callers read a chain-derived value from -- "+
						"and nothing established that expression. A conduit that hands back a verified "+
						"value on one path and an unverified one on another LAUNDERS the unverified "+
						"path through its own signature, putting the read that follows OUTSIDE the "+
						"verified boundary",
					f.Route, types.ExprString(e), i))
			}
			return true
		})
	}

	// RULE FOUR: a read whose provenance rests on an EXPORTED function's parameter.
	for _, name := range a.names() {
		if !isExportedIdentity(name) || len(needed[name]) == 0 {
			continue
		}
		f := a.Fns[name]
		if f == nil || f.IsValidatorBody {
			continue
		}
		for _, p := range sortedInts(needed[name]) {
			failures = append(failures, fmt.Sprintf(
				"%s: is EXPORTED and its required-artifact read rests on %s carrying a verified chain, "+
					"which this per-package census cannot discharge -- the callers that would have to "+
					"supply it are outside the package and are not scanned. The provenance is "+
					"UNESTABLISHED here rather than absent, and an unestablished provenance may not be "+
					"read as a pass",
				f.Route, a.paramLabel(name, p)))
		}
	}
	return failures
}

func sortedInts(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for i := range m {
		out = append(out, i)
	}
	sort.Ints(out)
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

	totalSubjects, totalEnforcing, totalGoverned, totalWriterOnly := 0, 0, 0, 0
	totalOwnVerification, totalOnAParameter, totalDemanded, totalDischarged := 0, 0, 0, 0
	for _, rel := range censusPackageDirs {
		a := packages[rel]

		// ANTI-VACUITY, PER PACKAGE. A census with no subjects asserts nothing and still
		// reports PASS -- which is exactly what scanning only the admission directory did.
		// A package silently dropped from the subject set fails here rather than shrinking
		// the claim in silence.
		readers := a.readerSubjects()
		subjects := len(readers)
		enforcing, storeOwners, judgedStores := 0, 0, 0
		var writerOnly []string
		for _, name := range a.names() {
			f := a.Fns[name]
			if !f.ownsStore() {
				continue
			}
			storeOwners++
			if !readers[name] {
				writerOnly = append(writerOnly, name)
			}
			for _, s := range f.Stores {
				// The floor that matters counts stores HALF ONE actually judges: the ones
				// held by a reader subject. Counting every store in the package would keep
				// this green on a package whose readers had all lost their validator, so
				// long as one writer still had one.
				if readers[name] {
					judgedStores++
					if s.EnforcesTable {
						enforcing++
					}
				}
			}
		}
		governedSites := a.governedSites()
		if subjects == 0 {
			t.Errorf("%s: the census discovered NO subject; either every reader was removed or the "+
				"discovery rule no longer matches the code it is supposed to measure. Either way this "+
				"package is now certified by a census that asserts nothing about it", rel)
		}
		if judgedStores == 0 {
			t.Errorf("%s: no store in this package is held by a function on a route to a governed "+
				"extraction, so HALF ONE judges nothing here; either the reading paths stopped owning "+
				"their stores or subject membership stopped matching the code", rel)
		}
		if enforcing == 0 {
			t.Errorf("%s: no store on a reading path installs a validator that reaches %s; the "+
				"delegation detector has lost its subject here", rel, enforcingValidatorName)
		}
		if len(governedSites) == 0 {
			t.Errorf("%s: the census found no read of a required-artifact key; the extraction-boundary "+
				"rule has lost its subject here", rel)
		}
		// THE PROVENANCE FLOOR. HALF TWO now judges where a payload CAME FROM, and each of
		// its rules can go green by judging nothing: a package whose governed reads all rest
		// on a self-performed verification never exercises RULE TWO, and one whose demands are
		// never discharged is asserting nothing about its callers.
		tally := a.provenanceTally()
		if tally.ReadsFromOwnVerification+tally.ReadsRestingOnAParameter == 0 {
			t.Errorf("%s: no governed read here is derived from a verified chain at all -- neither "+
				"from a verification in the reading function nor from a parameter carrying one. "+
				"HALF TWO is passing this package without establishing anything about it", rel)
		}
		totalOwnVerification += tally.ReadsFromOwnVerification
		totalOnAParameter += tally.ReadsRestingOnAParameter
		totalDemanded += tally.DemandedParameters
		totalDischarged += tally.DischargedCallSites

		totalSubjects += subjects
		totalEnforcing += enforcing
		totalGoverned += len(governedSites)

		if failures := a.censusFailures(); len(failures) > 0 {
			t.Errorf("%s: %d reading path(s) break the verified boundary:\n  %s",
				rel, len(failures), strings.Join(failures, "\n  "))
		}
		totalWriterOnly += len(writerOnly)
		sort.Strings(writerOnly)
		t.Logf("%s: %d reader subject(s), %d store owner(s) of which %d writer-only %v, "+
			"%d judged store(s) %d delegating, %d governed extraction site(s) %v; provenance: "+
			"%d read(s) from a verification here, %d resting on a parameter, %d demanded "+
			"parameter(s), %d call site(s) discharged",
			rel, subjects, storeOwners, len(writerOnly), writerOnly,
			judgedStores, enforcing, len(governedSites), sortedStrings(governedSites),
			tally.ReadsFromOwnVerification, tally.ReadsRestingOnAParameter,
			tally.DemandedParameters, tally.DischargedCallSites)
	}

	// THE SEPARATION IS MEASURED, NOT ASSERTED. A store-owning function that reaches no
	// governed extraction must not be a reader subject, and the review that found this
	// named a real one: tasksession.appendLedgerControlState verifies a store and then
	// WRITES control state through it. If this reaches zero, either production stopped
	// holding writer-only stores -- in which case the separation is untested on real code
	// and only the synthetic control below is left standing -- or subject membership has
	// silently gone back to meaning "owns a store".
	if totalWriterOnly == 0 {
		t.Error("no store-owning function in the four packages is excluded from the reader subject " +
			"set; the separation between owning a store and lying on a reading path is no longer " +
			"exercised by production code, and a census that counts writers reports a claim wider " +
			"than the one it measures")
	}

	if totalGoverned == 0 {
		t.Fatal("no governed extraction site was found in ANY of the four packages; " +
			"admission.requiredArtifact's mutant would be equivalent because nothing reads at all, " +
			"which is not the claim this census makes")
	}

	// BOTH ORIGINS MUST BE EXERCISED BY PRODUCTION, or half of HALF TWO is witnessed only by
	// fixtures. Today admission, tasksession, resultrecording and questiondisposition supply
	// both: LoadRecordedTransition verifies and reads in one function, while
	// latestArtifactFromChain, decodeGovernedArtifact, chainArtifactJSON and loadTransitionReceipt
	// all read a chain they were HANDED. If either count reaches zero the census still passes on
	// the code in front of it, but one of its two provenance origins has stopped being real.
	if totalOwnVerification == 0 {
		t.Error("no governed read in the four packages is derived from a verification performed in " +
			"the same function; the local-origin half of the provenance rule is no longer exercised " +
			"by production code")
	}
	if totalOnAParameter == 0 {
		t.Error("no governed read in the four packages rests on a parameter carrying a verified " +
			"chain, so no demand exists and RULE TWO -- every call site must supply the provenance " +
			"the read depends on -- is asserting nothing about this tree")
	}
	if totalDischarged == 0 {
		t.Errorf("%d parameter demand(s) were computed and NOT ONE was discharged at a call site; "+
			"either the call sites stopped being found or the demands are unreachable, and in both "+
			"cases the census certifies the extraction boundary without checking any caller",
			totalDemanded)
	}
	t.Logf("required-artifact census: %d package(s), %d subject(s), %d delegating store(s), "+
		"%d governed extraction site(s); provenance: %d read(s) verified in place, %d resting on a "+
		"parameter, %d demand(s), %d discharged at a call site",
		len(censusPackageDirs), totalSubjects, totalEnforcing, totalGoverned,
		totalOwnVerification, totalOnAParameter, totalDemanded, totalDischarged)
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
		delegates := a.reachesName(wrapper, enforcingValidatorName)
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

// AN ANONYMOUS CLOSURE IS A VALIDATOR LIKE ANY OTHER -- PROVEN ON A GOVERNED READER.
//
// The load-bearing witness is SYNTHETIC on purpose. tasksession does install three inline
// func literals, but all three sit on WRITER paths (appendLedgerControlState,
// initializeLedgerState, verifySession), so none of them is a required-artifact reader and
// none can show that the closure rule decides a reader's fate. Using them as reader
// subjects is what padded the subject set; the discovery fact they DO witness is checked
// separately below, for what it is.
//
// So the rule is driven where it bites: a governed reader whose only validator is a func
// literal. Discovery keyed on named validator functions finds no validator at all in
// either fixture -- it reports the delegating one as unguarded and the empty one as
// nothing -- so both directions have to be required.
func TestAnAnonymousClosureDecidesAGovernedReadersFate(t *testing.T) {
	t.Run("a closure that delegates is accepted", func(t *testing.T) {
		a := analyzeFixture(t, censusFixture(t, map[string]string{"reader.go": soundClosureReader}))
		closure := theOnlyClosureStore(t, a)
		if !closure.EnforcesTable {
			t.Errorf("a func literal that calls %s was not classified as delegating; a store built "+
				"with a closure must be judged on that literal's own body", enforcingValidatorName)
		}
		if failures := a.censusFailures(); len(failures) != 0 {
			t.Errorf("a governed reader guarded by a delegating closure was condemned:\n  %s",
				strings.Join(failures, "\n  "))
		}
	})

	t.Run("a closure that enforces nothing is reported", func(t *testing.T) {
		dir := censusFixture(t, map[string]string{"reader.go": anonymousClosureReader})
		a := analyzeFixture(t, dir)
		if closure := theOnlyClosureStore(t, a); closure.EnforcesTable {
			t.Error("a func literal that reaches no table-enforcing call was classified as delegating")
		}
		assertFailsExactly(t, dir, "reader.go:LoadWithClosure")
	})
}

// theOnlyClosureStore requires the fixture's store to have been discovered AS A CLOSURE.
// Without this the subtests would pass on a census that skipped the func literal and
// happened to reach the right verdict some other way -- which is precisely the silent
// miss the closure rule exists to prevent.
func theOnlyClosureStore(t *testing.T, a *censusAnalysis) storeFact {
	t.Helper()
	var found []storeFact
	for _, name := range a.names() {
		for _, s := range a.Fns[name].Stores {
			if s.ValidatorIsClosure {
				found = append(found, s)
			}
		}
	}
	if len(found) != 1 {
		t.Fatalf("the census discovered %d inline closure validator(s) in a fixture that installs "+
			"exactly one; name-keyed discovery finds none, and a rule about closures cannot be "+
			"witnessed by a census that does not see them", len(found))
	}
	return found[0]
}

// AND THE REAL TREE STILL CONTAINS FUNC-LITERAL VALIDATORS, which is a claim about
// DISCOVERY and nothing more.
//
// These three stores are writer-path stores. They are not census subjects and this test
// does not pretend they are: it asserts only that the parser sees a validator where a
// name-keyed discovery would see none, and that the one it sees is classified on the
// literal's body. That is the fact production supplies; the fate of a governed reader
// guarded by a closure is proven above, where a governed read actually exists.
func TestTheRealTreeStillInstallsFuncLiteralValidators(t *testing.T) {
	a := analyzeRealPackages(t)[filepath.Join("golang", "architecture", "tasksession")]
	readers := a.readerSubjects()
	closures, onAReadingPath := 0, 0
	for _, name := range a.names() {
		for _, s := range a.Fns[name].Stores {
			if !s.ValidatorIsClosure {
				continue
			}
			closures++
			if readers[name] {
				onAReadingPath++
			}
			if !s.EnforcesTable {
				t.Errorf("%s: an inline closure validator was not classified as delegating to %s; "+
					"a store built with a func literal must be judged on that literal's body",
					a.routeOf(name), enforcingValidatorName)
			}
		}
	}
	if closures == 0 {
		t.Error("tasksession installs no inline closure validator any more; discovery of func-literal " +
			"validators is no longer exercised against real source, and a name-keyed discovery would " +
			"now pass here for the wrong reason")
	}
	t.Logf("tasksession: %d inline closure validator(s) discovered, all delegating; %d of them on a "+
		"route to a governed extraction", closures, onAReadingPath)
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

// THE FIXTURES ALL PERFORM THE SAME READ, and that is what makes the table below a table.
//
// Every one walks a verified chain, reads the entry's own payload and indexes a governed
// artifact out of it -- which is what admission.latestArtifactFromChain,
// resultrecording.findRecordedTransition and questiondisposition.readDispositionReceipt
// actually do. Holding the read constant means the only thing that can vary between a
// fixture the census accepts and one it condemns is the validator the chain was verified
// through, or the provenance of the payload. Nothing else is available as the reason for a
// verdict.
//
// THE EARLIER FIXTURES DID NOT DO THIS, and the reason is worth keeping. They verified a
// store and then read `ledger.ParseTaskEventPayload(nil).Artifacts[key]` -- a payload no
// chain had established. Under a census that excused everything reachable from a verified
// root, that passed, and looked sound. It was sound only against a rule that never asked
// where the payload came from, which is the defect this cycle repairs; the fixtures had to
// be rewritten because they had been written to a classifier, not to production.
const chainDerivedRead = `	for _, ve := range chain.Entries {
		data, err := ledger.ReadVerifiedPayload(ve)
		if err != nil {
			return false, err
		}
		payload, err := ledger.ParseTaskEventPayload(data)
		if err != nil {
			return false, err
		}
		if _, ok := payload.Artifacts[GOVERNEDKEY]; ok {
			return true, nil
		}
	}
	return false, nil
}
`

// enforcingValidatorDecl is a validator that delegates in one line, like
// admission.admissionValidator.
const enforcingValidatorDecl = `
func enforcingValidator(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
	return ledger.ValidateTaskEventPayload(eventType, data)
}
`

// readerFixture builds a reader that verifies storeExpr and then performs the standard
// chain-derived read of key. extraDecls carries whatever else a case needs -- a wrapper
// validator, a store factory.
func readerFixture(fn, storeExpr, key, extraDecls string) string {
	return "package fake\n" + extraDecls + "\nfunc " + fn + `(taskDir string) (bool, error) {
	store := ` + storeExpr + `
	chain, err := store.VerifyChain()
	if err != nil {
		return false, err
	}
` + strings.Replace(chainDerivedRead, "GOVERNEDKEY", strconv.Quote(key), 1)
}

// soundReader mirrors the real SPLIT: a chain verified in one function and extracted from in
// another that receives it, which is how admission.LoadLatestArtifactOptional and
// latestArtifactFromChain are arranged. The receiving half is where a parameter becomes a
// provenance channel, so this is the fixture RULE TWO is discharged against.
var soundReader = "package fake\n" + enforcingValidatorDecl + `
func LoadSound(taskDir string, out any) (bool, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(enforcingValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		return false, err
	}
	return soundFromChain(taskDir, chain, out)
}

func soundFromChain(taskDir string, chain ledger.VerifiedChain, out any) (bool, error) {
` + strings.Replace(chainDerivedRead, "GOVERNEDKEY", `"capability_consumption"`, 1)

// A NIL VALIDATOR. ledger/verify.go skips a nil validator entirely, so this chain is
// verified against nothing -- which is also why the read below it establishes nothing: the
// two halves of this invariant are one claim, and a store that enforces no event table
// cannot be the origin of a certified extraction.
var nilValidatorReader = readerFixture("LoadWithNil",
	"ledger.NewStore(taskDir, ledger.WithPayloadValidator(nil))", "capability_consumption", "")

// NO VALIDATOR AT ALL.
var omittedValidatorReader = readerFixture("LoadWithoutOption",
	"ledger.NewStore(taskDir)", "result_transition_receipt", "")

// A WRAPPER THAT STOPPED DELEGATING. This is the mutant an IS test cannot tell from a
// healthy wrapper and an INVOKES test catches: lapsedWrapper still looks exactly like
// recordingPayloadValidator -- it parses, it enforces a rule of its own -- but the one line
// that reached the event table is gone.
var lapsedWrapperReader = readerFixture("LoadWithLapsedWrapper",
	"ledger.NewStore(taskDir, ledger.WithPayloadValidator(lapsedWrapper))", "capability_consumption", `
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
`)

// AN ANONYMOUS CLOSURE THAT ENFORCES NOTHING. This is the mutant NAME-KEYED discovery misses
// outright: there is no named validator to look up, so a census keyed on one finds no
// validator here at all and reports nothing.
var anonymousClosureReader = readerFixture("LoadWithClosure",
	"ledger.NewStore(taskDir, ledger.WithPayloadValidator(func(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {\n\t\treturn nil\n\t}))",
	"question_disposition_receipt", "")

// AN ANONYMOUS CLOSURE THAT DOES DELEGATE, which the census must ACCEPT. Without this the
// closure rule above would be satisfied by a census that condemns every func literal.
var soundClosureReader = readerFixture("LoadWithSoundClosure",
	"ledger.NewStore(taskDir, ledger.WithPayloadValidator(func(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {\n\t\treturn ledger.ValidateTaskEventPayload(eventType, data)\n\t}))",
	"question_disposition_receipt", "")

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

// A NEW DIRECT CALL FROM AN UNCHECKED PATH. This is the half a census of CONSTRUCTORS alone
// cannot see: every store in the package is still built correctly, and somebody has simply
// reached past them into the extractor with a chain they fabricated. Paired with soundReader,
// whose soundFromChain it calls.
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

// A STORE FACTORY WHOSE VALIDATOR IS NIL, used by a caller that verifies it. Every rule here
// is keyed on the CALLER's store, so a factory that hands out an unguarded store is how a
// whole package goes green with nothing enforced.
var factoryWithNilValidator = readerFixture("LoadViaFactory", "newFixtureStore(taskDir)",
	"capability_consumption", `
func newFixtureStore(taskDir string) *ledger.Store {
	return ledger.NewStore(taskDir, ledger.WithPayloadValidator(nil))
}
`)

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

// A HELPER BELOW A VERIFIED ROOT THAT READS ITS OWN PAYLOAD MUST BE REPORTED.
//
// THE DEFECT THIS EXISTS FOR (review finding f3). The extraction rule used to excuse every
// function reachable from a verified root: reachedFrom walked the root's calls and
// censusFailures skipped everything it found. CALL REACHABILITY IS NOT DATA PROVENANCE. So a
// function could VerifyChain successfully -- delegating validator, sound chain, a genuine
// verified root by every test the census applied -- and then call a helper that ignored that
// chain entirely, read a fresh payload off disk and extracted capability_consumption from
// it. The helper was below the root, so the census reported nothing, and an artifact that
// never passed through any verified chain was certified.
//
// The three assertions below are the repair stated as a difference. The root really is a
// verified root, and the helper really is reachable from it through Edges -- the EXCUSING
// graph, pinned, not a guess -- so both conditions the old excuse needed are present. And
// the helper is still reported. The excuse no longer exists: what is judged is where the
// payload came from, not who called whom.
func TestAHelperBelowAVerifiedRootIsNotExcusedByBeingBelowIt(t *testing.T) {
	const helperIgnoresTheVerifiedChain = `package fake

func enforcingValidator(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
	return ledger.ValidateTaskEventPayload(eventType, data)
}

func LoadUnderVerification(taskDir string) (bool, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(enforcingValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		return false, err
	}
	if len(chain.Entries) == 0 {
		return false, nil
	}
	return consumptionStraightOffDisk(taskDir)
}

func consumptionStraightOffDisk(taskDir string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(taskDir, "ledger", "0001.payload.json"))
	if err != nil {
		return false, err
	}
	payload, err := ledger.ParseTaskEventPayload(data)
	if err != nil {
		return false, err
	}
	if _, ok := payload.Artifacts["capability_consumption"]; !ok {
		return false, errors.New("absent")
	}
	return true, nil
}
`
	dir := censusFixture(t, map[string]string{"reader.go": helperIgnoresTheVerifiedChain})
	a := analyzeFixture(t, dir)

	// THE ROOT MUST BE GENUINE, or this fixture proves nothing about the excuse: an
	// unverified caller would be reported for its own sake and the helper's fate would be
	// untested.
	if !a.verifiedRoots()["LoadUnderVerification"] {
		t.Fatal("LoadUnderVerification builds a delegating store and verifies it, but is not a " +
			"verified root; without that this fixture never reaches the excuse it exists to remove")
	}

	// AND THE EXCUSE MUST HAVE BEEN AVAILABLE. The call is a free function, so it is PINNED:
	// it sits in Edges, the graph excuses used to travel on. This is the exact shape the old
	// rule waved through.
	if !a.Fns["LoadUnderVerification"].Edges["consumptionStraightOffDisk"] {
		t.Fatalf("the verified root does not reach the helper through the EXCUSING graph %v; the "+
			"fixture no longer reproduces the reachability that used to certify it",
			sortedStrings(a.Fns["LoadUnderVerification"].Edges))
	}

	// The helper reads a payload it fetched itself. Being below a verified root is not
	// reading what that root verified, and only the helper is condemned.
	assertFailsExactly(t, dir, "reader.go:consumptionStraightOffDisk")

	joined := strings.Join(a.censusFailures(), "\n")
	if !strings.Contains(joined, "not reading what it verified") {
		t.Errorf("the failure does not say WHY the helper is not excused -- that being called by a "+
			"verifier is not reading the verified chain:\n%s", joined)
	}
}

// AN EXTRACTION THAT RUNS BEFORE THE VERIFICATION MUST BE REPORTED, AND THE ORDER NAMED.
//
// THE DEFECT THIS EXISTS FOR (review finding f4). The previous census said so itself, in its
// own KNOWN LIMITS: "Statement ORDER inside a verified root is not checked: a root that
// indexed before verifying would pass." The invariant it claims to witness says verification
// happens BEFORE extraction, so a known false negative on exactly that temporal relation left
// the proof incomplete -- the census could not go red when the ordering half stopped holding.
//
// The fixture is the minimal violation: ONE function, a delegating validator, a real
// VerifyChain on its own store, and the governed read placed above it. Every non-temporal
// property is satisfied. The PAIR is what makes this about order and nothing else: the second
// subtest moves the same verification above the same read, changes nothing else, and the
// census must then accept it. A rule that condemned both would be measuring something else.
func TestAnExtractionBeforeItsOwnVerificationIsReported(t *testing.T) {
	const readThenVerify = `package fake

func enforcingValidator(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
	return ledger.ValidateTaskEventPayload(eventType, data)
}

func LoadBeforeVerifying(taskDir string) (bool, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(enforcingValidator))
	var chain ledger.VerifiedChain
	for _, ve := range chain.Entries {
		data, err := ledger.ReadVerifiedPayload(ve)
		if err != nil {
			return false, err
		}
		payload, err := ledger.ParseTaskEventPayload(data)
		if err != nil {
			return false, err
		}
		if _, ok := payload.Artifacts["capability_consumption"]; ok {
			return true, nil
		}
	}
	verified, err := store.VerifyChain()
	if err != nil {
		return false, err
	}
	chain = verified
	return false, nil
}
`
	dir := censusFixture(t, map[string]string{"reader.go": readThenVerify})
	a := analyzeFixture(t, dir)

	// The store is sound and the function verifies it: this is a verified root by every
	// non-temporal test the census has. Only the ORDER is wrong.
	if !a.verifiedRoots()["LoadBeforeVerifying"] {
		t.Fatal("LoadBeforeVerifying verifies a delegating store and must be a verified root; if it " +
			"is not, this fixture fails for a reason that has nothing to do with ordering")
	}
	if len(a.governedSites()) == 0 {
		t.Fatal("the census found no governed read in a function that plainly performs one")
	}

	assertFailsExactly(t, dir, "reader.go:LoadBeforeVerifying")
	joined := strings.Join(a.censusFailures(), "\n")
	for _, want := range []string{"BEFORE the verification", "must DOMINATE extraction"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the failure does not name the ORDER as the defect (%q missing); a red that does "+
				"not say the extraction ran first could be reporting anything:\n%s", want, joined)
		}
	}

	// THE CONTROL. The same read, the same validator, the same verification -- moved above
	// the loop. Nothing but order differs, and the census must accept it.
	t.Run("the same function verifying FIRST is accepted", func(t *testing.T) {
		fixed := `package fake

func enforcingValidator(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
	return ledger.ValidateTaskEventPayload(eventType, data)
}

func LoadBeforeVerifying(taskDir string) (bool, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(enforcingValidator))
	verified, err := store.VerifyChain()
	if err != nil {
		return false, err
	}
	chain := verified
	for _, ve := range chain.Entries {
		data, err := ledger.ReadVerifiedPayload(ve)
		if err != nil {
			return false, err
		}
		payload, err := ledger.ParseTaskEventPayload(data)
		if err != nil {
			return false, err
		}
		if _, ok := payload.Artifacts["capability_consumption"]; ok {
			return true, nil
		}
	}
	return false, nil
}
`
		b := analyzeFixture(t, censusFixture(t, map[string]string{"reader.go": fixed}))
		if len(b.governedSites()) == 0 {
			t.Fatal("the control performs no governed read; it would pass by describing nothing")
		}
		if failures := b.censusFailures(); len(failures) != 0 {
			t.Errorf("a function that verifies its chain and THEN reads it was condemned; the rule is "+
				"then not about order:\n  %s", strings.Join(failures, "\n  "))
		}
	})
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

// A CONDUIT THAT LAUNDERS MUST BE REPORTED, AND ONE THAT MERELY FAILS MUST NOT.
//
// WHY THIS RULE EXISTS. Provenance crosses a call by SUBSTITUTION: when latestEventFromChain
// returns a payload derived from the chain it was given, a caller holding a verified chain
// gets back a value with provenance. That substitution is only as honest as the conduit. A
// function that returns a chain-derived payload on one path and a payload it read off disk
// on another hands both back through the SAME result position -- and the second inherits the
// blessing the first earned. Nothing downstream can tell them apart, because a signature is
// all the census sees across a call.
//
// AND THE FALSE POSITIVE THIS MUST NOT COMMIT. Every real conduit in these four packages
// returns a ZERO value on its error paths -- `return ledger.TaskEventPayload{}, err`. A zero
// payload carries no artifacts, so reading one finds nothing and fails closed; condemning
// those would condemn resultrecording.latestEventFromChain, loadEventPayload and
// questiondisposition.latestResultTransition, all correct. The second subtest is that
// control, and without it the rule would be satisfiable by a census that condemns every
// conduit with an error path -- which is all of them.
func TestAConduitThatLaundersAnUnverifiedPayloadIsReported(t *testing.T) {
	t.Run("a fresh read returned where callers read a chain-derived payload", func(t *testing.T) {
		const launderingConduit = `package fake

func enforcingValidator(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
	return ledger.ValidateTaskEventPayload(eventType, data)
}

func LoadViaConduit(taskDir string) (bool, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(enforcingValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		return false, err
	}
	payload, err := payloadForTask(taskDir, chain)
	if err != nil {
		return false, err
	}
	if _, ok := payload.Artifacts["capability_consumption"]; !ok {
		return false, errors.New("absent")
	}
	return true, nil
}

func payloadForTask(taskDir string, chain ledger.VerifiedChain) (ledger.TaskEventPayload, error) {
	for _, ve := range chain.Entries {
		data, err := ledger.ReadVerifiedPayload(ve)
		if err != nil {
			return ledger.TaskEventPayload{}, err
		}
		return ledger.ParseTaskEventPayload(data)
	}
	raw, err := os.ReadFile(filepath.Join(taskDir, "ledger", "0001.payload.json"))
	if err != nil {
		return ledger.TaskEventPayload{}, err
	}
	return ledger.ParseTaskEventPayload(raw)
}
`
		dir := censusFixture(t, map[string]string{"reader.go": launderingConduit})
		a := analyzeFixture(t, dir)

		// The conduit must genuinely BE a conduit, or the rule is not the thing being tested:
		// its payload result has to carry provenance on the good path, which is what the bad
		// path then borrows.
		results := a.resultProv["payloadForTask"]
		if len(results) == 0 || !results[0].established() {
			t.Fatalf("payloadForTask does not hand back a chain-derived payload at result 0 (%v); "+
				"there is then no blessing for its disk path to launder and this fixture is vacuous",
				results)
		}
		assertFailsExactly(t, dir, "reader.go:payloadForTask")
		joined := strings.Join(a.censusFailures(), "\n")
		if !strings.Contains(joined, "LAUNDERS") {
			t.Errorf("the failure does not name laundering as the defect:\n%s", joined)
		}
	})

	t.Run("a conduit whose other paths return ZERO values is accepted", func(t *testing.T) {
		const honestConduit = `package fake

func enforcingValidator(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
	return ledger.ValidateTaskEventPayload(eventType, data)
}

func LoadViaConduit(taskDir string) (bool, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(enforcingValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		return false, err
	}
	payload, err := payloadForTask(taskDir, chain)
	if err != nil {
		return false, err
	}
	if _, ok := payload.Artifacts["capability_consumption"]; !ok {
		return false, errors.New("absent")
	}
	return true, nil
}

func payloadForTask(taskDir string, chain ledger.VerifiedChain) (ledger.TaskEventPayload, error) {
	for _, ve := range chain.Entries {
		data, err := ledger.ReadVerifiedPayload(ve)
		if err != nil {
			return ledger.TaskEventPayload{}, err
		}
		return ledger.ParseTaskEventPayload(data)
	}
	return ledger.TaskEventPayload{}, errors.New("no entry")
}
`
		a := analyzeFixture(t, censusFixture(t, map[string]string{"reader.go": honestConduit}))
		if len(a.governedSites()) == 0 {
			t.Fatal("the control performs no governed read; it would pass by describing nothing")
		}
		if failures := a.censusFailures(); len(failures) != 0 {
			t.Errorf("a conduit that returns a ZERO payload on its error paths was reported as "+
				"laundering; every real conduit in the four packages does exactly this:\n  %s",
				strings.Join(failures, "\n  "))
		}
	})
}

// AN EXPORTED EXTRACTOR'S CHAIN PARAMETER CANNOT BE DISCHARGED BY THIS CENSUS, AND THAT IS
// SAID RATHER THAN ASSUMED.
//
// RULE TWO works because every caller is visible: the analysis is per-package, and every
// governed extractor in production except admission's exported loaders is package-private --
// and those loaders verify their own ledger rather than receiving one. Export a function that
// reads a required artifact out of a chain it was HANDED, and that stops being true: the
// callers that would have to supply the provenance are in packages this census never parses,
// so the demand is not discharged anywhere.
//
// The honest report is UNESTABLISHED, not absent. A census that stayed silent here would be
// certifying the one arrangement it structurally cannot see, and an absence presented as a
// pass is the failure mode this whole file exists against.
func TestAnExportedReadThatRestsOnItsCallersIsReportedAsUnestablished(t *testing.T) {
	const exportedChannel = `package fake

func ConsumptionFromChain(taskDir string, chain ledger.VerifiedChain) (bool, error) {
	for _, ve := range chain.Entries {
		data, err := ledger.ReadVerifiedPayload(ve)
		if err != nil {
			return false, err
		}
		payload, err := ledger.ParseTaskEventPayload(data)
		if err != nil {
			return false, err
		}
		if _, ok := payload.Artifacts["capability_consumption"]; ok {
			return true, nil
		}
	}
	return false, nil
}
`
	dir := censusFixture(t, map[string]string{"reader.go": exportedChannel})
	a := analyzeFixture(t, dir)

	// The read itself is structurally sound -- it walks the chain it was given. What cannot
	// be established is that any caller ever hands it a verified one.
	if needed := a.neededParams()["ConsumptionFromChain"]; !needed[1] {
		t.Fatalf("the census does not record that this read rests on parameter 1; the demand RULE "+
			"FOUR reports as undischargeable was never computed (needed: %v)", needed)
	}
	assertFailsExactly(t, dir, "reader.go:ConsumptionFromChain")
	joined := strings.Join(a.censusFailures(), "\n")
	for _, want := range []string{"EXPORTED", "UNESTABLISHED"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the failure does not say %q -- an undischargeable demand must be reported as "+
				"unestablished rather than as a defect in the read:\n%s", want, joined)
		}
	}

	// THE CONTROL: the same function, unexported. Every caller is then visible, the demand is
	// dischargeable, and the census owes the sole caller -- not this function -- an answer.
	t.Run("the same read unexported is discharged at its caller", func(t *testing.T) {
		unexported := strings.ReplaceAll(exportedChannel, "ConsumptionFromChain", "consumptionFromChain")
		caller := `
func LoadIt(taskDir string) (bool, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(enforcingValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		return false, err
	}
	return consumptionFromChain(taskDir, chain)
}
`
		b := analyzeFixture(t, censusFixture(t, map[string]string{
			"reader.go": unexported + enforcingValidatorDecl + caller,
		}))
		if len(b.governedSites()) == 0 {
			t.Fatal("the control performs no governed read; it would pass by describing nothing")
		}
		if failures := b.censusFailures(); len(failures) != 0 {
			t.Errorf("an unexported read whose sole caller supplies a verified chain was condemned; "+
				"RULE FOUR is then not about visibility:\n  %s", strings.Join(failures, "\n  "))
		}
	})
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
	if a.readerSubjects()["Unrelated"] {
		t.Error("a function that reads no ledger was made a census subject")
	}
	if failures := a.censusFailures(); len(failures) != 0 {
		t.Errorf("unexpected failures: %v", failures)
	}
}

// A WRITER-ONLY STORE IS NOT A READER SUBJECT -- and the same function becomes one the
// moment it reads.
//
// THE DEFECT THIS EXISTS FOR. Subject membership was "owns a ledger store", so
// tasksession.appendLedgerControlState -- construct, verify, StoreArtifactBytes, Append --
// was counted and judged as a required-artifact reading path. It reads no governed
// artifact; this invariant says nothing about it. The padding made every subject number
// describe a wider claim than the census measured.
//
// The control needs BOTH halves. "A writer is not a subject" alone is satisfied by a
// census that has stopped discovering anything, so the second subtest takes the SAME
// writer, adds one governed read, and requires it to be reported -- with a nil validator
// throughout, so the only thing that changed is whether it reads.
func TestAWriterOnlyStoreIsNotAReaderSubject(t *testing.T) {
	const writerOnly = `package fake

func AppendControlState(taskDir string, controlBytes []byte) error {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(nil))
	report, err := store.Verify()
	if err != nil {
		return err
	}
	if !report.Valid {
		return errors.New("invalid")
	}
	ref, err := store.StoreArtifactBytes(controlBytes, "application/yaml")
	if err != nil {
		return err
	}
	_, err = store.Append(ledger.AppendRequest{Artifacts: map[string]any{"task_control": ref}})
	return err
}
`
	t.Run("the writer is discovered, owns its store, and is not a subject", func(t *testing.T) {
		a := analyzeFixture(t, censusFixture(t, map[string]string{"writer.go": writerOnly}))

		// Discovery must be untouched: the separation moved MEMBERSHIP, not the parser. A
		// census that simply stopped seeing the store would satisfy the rest of this test
		// while seeing nothing at all.
		f, found := a.Fns["AppendControlState"]
		if !found {
			t.Fatalf("the writer was not discovered at all; discovered: %v", a.names())
		}
		if !f.ownsStore() {
			t.Error("the writer's ledger.NewStore was not discovered; store discovery must stay as " +
				"thorough as it was -- only subject membership narrowed")
		}
		if f.touchesArtifacts() {
			t.Error("the writer was recorded as touching an Artifacts map, which it does not")
		}
		if a.readerSubjects()["AppendControlState"] {
			t.Error("a function that owns a store, verifies it and only WRITES was made a " +
				"required-artifact reader subject; owning a store is not lying on a reading path")
		}
		if failures := a.censusFailures(); len(failures) != 0 {
			t.Errorf("a writer-only store was judged against an invariant about reads:\n  %s",
				strings.Join(failures, "\n  "))
		}
	})

	t.Run("one governed read turns the same writer into a judged subject", func(t *testing.T) {
		const alsoReads = `	payload, err := ledger.ParseTaskEventPayload(nil)
	if err != nil {
		return err
	}
	if _, ok := payload.Artifacts["capability_consumption"]; !ok {
		return errors.New("absent")
	}
	return nil
}
`
		reader := strings.Replace(writerOnly,
			"\t_, err = store.Append(ledger.AppendRequest{Artifacts: map[string]any{\"task_control\": ref}})\n\treturn err\n}\n",
			"\t_, _ = store.Append(ledger.AppendRequest{Artifacts: map[string]any{\"task_control\": ref}})\n"+alsoReads, 1)
		if reader == writerOnly {
			t.Fatal("the reading variant was not built; this subtest would re-run the writer case " +
				"and prove nothing about the difference a read makes")
		}
		dir := censusFixture(t, map[string]string{"writer.go": reader})
		if !analyzeFixture(t, dir).readerSubjects()["AppendControlState"] {
			t.Fatal("adding a governed read did NOT make the function a subject; membership is no " +
				"longer tracking the reading path it is supposed to describe")
		}
		assertFailsExactly(t, dir, "writer.go:AppendControlState")
	})
}

// ONE VERIFIED METHOD MUST NOT CERTIFY ITS UNVERIFIED NAMESAKE.
//
// THE DEFECT THIS EXISTS FOR. Keying the call graph on fn.Name.Name merged every same-named
// method into one node. Two methods called read -- one building and verifying a delegating
// store, one parsing a payload off disk and extracting a governed artifact -- became a
// single node that owned a verified store, so verifiedRoots called it a root and
// censusFailures skipped roots. The unverified read was reported as a PASS. That is not an
// over-approximation of reachability, which can only add routes to judge; it is a false
// negative, and the four subject packages already declare Error() on several types each,
// so same-named methods are ordinary here rather than hypothetical.
func TestOneVerifiedMethodCannotCertifyItsUnverifiedNamesake(t *testing.T) {
	const sameNameMethods = `package fake

type soundReaderT struct{}
type unsafeReaderT struct{}

func enforcingValidator(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
	return ledger.ValidateTaskEventPayload(eventType, data)
}

func (s *soundReaderT) read(taskDir string) (bool, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(enforcingValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		return false, err
	}
	for _, ve := range chain.Entries {
		data, err := ledger.ReadVerifiedPayload(ve)
		if err != nil {
			return false, err
		}
		payload, err := ledger.ParseTaskEventPayload(data)
		if err != nil {
			return false, err
		}
		if _, ok := payload.Artifacts["capability_consumption"]; ok {
			return true, nil
		}
	}
	return false, nil
}

func (u *unsafeReaderT) read(taskDir string) (bool, error) {
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
	dir := censusFixture(t, map[string]string{"reader.go": sameNameMethods})
	a := analyzeFixture(t, dir)

	// FIRST, THE IDENTITIES MUST BE TWO. If a later edit puts name keying back, this fails
	// here -- at discovery, where the defect actually lives -- rather than leaving the
	// judgement below to fail for a reason nobody can locate.
	sound, unsafe := "(*soundReaderT).read", "(*unsafeReaderT).read"
	for _, want := range []string{sound, unsafe} {
		if _, ok := a.Fns[want]; !ok {
			t.Fatalf("the census did not discover %s as its own identity; two same-named methods "+
				"have merged into one node, and whichever of them verifies a store will certify the "+
				"other. discovered: %v", want, a.names())
		}
	}
	if _, merged := a.Fns["read"]; merged {
		t.Error("a bare `read` node exists alongside the receiver-qualified ones; a call resolved to " +
			"it would carry an excuse between two unrelated declarations")
	}

	// The sound method must be the root, the unsafe one must not inherit that status.
	roots := a.verifiedRoots()
	if !roots[sound] {
		t.Errorf("%s builds and verifies a delegating store but was not recognised as a verified "+
			"root; this fixture would then fail for the wrong reason", sound)
	}
	if roots[unsafe] {
		t.Errorf("%s verifies nothing yet was recognised as a verified root; it has inherited its "+
			"namesake's store, which is exactly the merge this test exists to forbid", unsafe)
	}

	assertFailsExactly(t, dir, "reader.go:"+unsafe)
}

// AN AMBIGUOUS CALL MAY ACCUSE BUT MUST NOT EXCUSE.
//
// Receiver-qualified identities stop two methods MERGING, but they do not by themselves
// say what `x.fetch()` reaches when several types declare fetch. Without type information
// the honest answer is "any of them", and that answer points in two opposite directions:
//
//	as an ACCUSATION -- this route might reach a governed extraction -- an extra edge only
//	adds a subject to judge on its own merits;
//	as an EXCUSE -- this function works on a chain a verified root already checked -- an
//	extra edge HIDES an unverified read behind a namesake it never called.
//
// So the census keeps two graphs and lets excuses travel only where every target is pinned.
//
// WHERE THAT SPLIT LIVES NOW. The excuse this test was originally written against -- being
// reachable from a verified root -- no longer exists at all: a root absorbs nothing, and the
// unsafe namesake below is reported because of where its PAYLOAD came from, not because the
// census failed to reach it. The asymmetry it protects is still load-bearing in two places
// that remain: provenance crosses a call only through pinnedCallee, where the target is
// certain, while RULE TWO's demand is discharged against candidateCallees, where every
// namesake is accused. And the validator excuse still travels on Edges.
//
// This witness is kept because the fixture is exactly the shape that hid a governed read
// behind a namesake: a verified root dispatching ambiguously, one candidate sound and one
// reading a payload it fetched itself. It must still be reported, and only it.
func TestAnAmbiguousCallFromAVerifiedRootExcusesNothing(t *testing.T) {
	const ambiguousDispatch = `package fake

type soundFetcher struct{}
type unsafeFetcher struct{}

func enforcingValidator(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
	return ledger.ValidateTaskEventPayload(eventType, data)
}

func Entry(taskDir string) error {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(enforcingValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		return err
	}
	_ = chain
	var s soundFetcher
	return s.fetch(taskDir)
}

func (s soundFetcher) fetch(taskDir string) error {
	if taskDir == "" {
		return errors.New("no task")
	}
	return nil
}

func (u unsafeFetcher) fetch(taskDir string) error {
	data, err := os.ReadFile(taskDir)
	if err != nil {
		return err
	}
	payload, err := ledger.ParseTaskEventPayload(data)
	if err != nil {
		return err
	}
	if _, ok := payload.Artifacts["capability_consumption"]; !ok {
		return errors.New("absent")
	}
	return nil
}
`
	dir := censusFixture(t, map[string]string{"reader.go": ambiguousDispatch})
	a := analyzeFixture(t, dir)

	// The ambiguity must be REAL, or this fixture proves nothing: two declarations must
	// bear the name, and the call site must have been unable to pin either.
	if got := len(a.methodsByName["fetch"]); got != 2 {
		t.Fatalf("the fixture declares %d method(s) named fetch, want 2; without a genuine ambiguity "+
			"the two graphs coincide and this witness is vacuous", got)
	}
	entry := a.Fns["Entry"]
	if entry == nil {
		t.Fatalf("Entry was not discovered; discovered: %v", a.names())
	}
	for _, forbidden := range []string{"(soundFetcher).fetch", "(unsafeFetcher).fetch"} {
		if entry.Edges[forbidden] {
			t.Errorf("Entry's EXCUSING edges %v include %s, reached by an ambiguous `s.fetch(...)`; "+
				"reachability along that call is a guess, and an excuse may not rest on a guess",
				sortedStrings(entry.Edges), forbidden)
		}
	}
	for _, want := range []string{"(soundFetcher).fetch", "(unsafeFetcher).fetch"} {
		if !entry.AllEdges[want] {
			t.Errorf("Entry's ACCUSING edges %v omit %s; an ambiguous call must reach every candidate "+
				"there, or a route the census cannot rule out goes unjudged",
				sortedStrings(entry.AllEdges), want)
		}
	}

	if !a.verifiedRoots()["Entry"] {
		t.Fatal("Entry builds and verifies a delegating store but is not a verified root; this " +
			"fixture would then fail without reproducing the arrangement that hid the read")
	}
	assertFailsExactly(t, dir, "reader.go:(unsafeFetcher).fetch")
}

// THE OTHER EXCUSE CHANNEL, closed the same way.
//
// After the provenance repair there is exactly ONE way this census lets a governed read go
// unjudged: the reader runs INSIDE verification. Sitting below a verified root is no longer an
// excuse at all -- that was the defect -- so validatorClosure is now the only excuse channel
// left, and it is the same hazard the test above was written for, wearing different clothes: a
// validator that dispatches through an ambiguous `h.check(...)` would otherwise mark every
// method named check as running inside verification, including one that reads a governed
// artifact straight off disk and belongs to nobody's verification at all.
//
// The one remaining excuse is the one that most needs a witness.
//
// A census sound only against the mutation that happened to be found is not sound.
func TestAnAmbiguousCallFromAValidatorExcusesNothing(t *testing.T) {
	const ambiguousValidatorHelper = `package fake

type validatorHelper struct{}
type governedReader struct{}

func strictValidator(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
	if err := ledger.ValidateTaskEventPayload(eventType, data); err != nil {
		return err
	}
	var h validatorHelper
	return h.check(data)
}

func (h validatorHelper) check(data []byte) error {
	if len(data) == 0 {
		return errors.New("empty payload")
	}
	return nil
}

func (g governedReader) check(taskDir string) error {
	data, err := os.ReadFile(taskDir)
	if err != nil {
		return err
	}
	payload, err := ledger.ParseTaskEventPayload(data)
	if err != nil {
		return err
	}
	if _, ok := payload.Artifacts["capability_consumption"]; !ok {
		return errors.New("absent")
	}
	return nil
}

func Install(taskDir string) error {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(strictValidator))
	_, err := store.VerifyChain()
	return err
}
`
	dir := censusFixture(t, map[string]string{"reader.go": ambiguousValidatorHelper})
	a := analyzeFixture(t, dir)

	if got := len(a.methodsByName["check"]); got != 2 {
		t.Fatalf("the fixture declares %d method(s) named check, want 2; without a genuine ambiguity "+
			"this witness is vacuous", got)
	}
	if !a.Fns["strictValidator"].IsValidatorBody {
		t.Error("the validator itself was not recognised as running inside verification; the " +
			"closure this test constrains is not even being computed")
	}
	if a.Fns["(governedReader).check"].IsValidatorBody {
		t.Error("a governed reader was marked as running INSIDE verification because a validator " +
			"dispatches through an ambiguous call to its name; that excuse rests on a guess, and it " +
			"removes the read from the census entirely rather than judging it")
	}
	assertFailsExactly(t, dir, "reader.go:(governedReader).check")
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
	if subjects := a.readerSubjects(); len(subjects) != 0 {
		t.Errorf("a package with no ledger reader yielded %d subject(s): %v",
			len(subjects), sortedStrings(subjects))
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
