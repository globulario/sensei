// SPDX-License-Identifier: AGPL-3.0-only

package trustkernel

// THE AUTHORITY-READER CENSUS.
//
// THE INVARIANT:
//
//	Every production authority-artifact reader must consume a chain verified by a
//	validator that ENFORCES requiredEventArtifacts, before artifact extraction.
//
// WHY THIS EXISTS RATHER THAN A WITNESS ON requiredArtifact ITSELF.
//
// admission.requiredArtifact is the seam that separates a DAMAGED record from a merely
// absent optional one (#354). Disabling it leaves every behavioural witness in this
// package GREEN, and that is not a hole in the witnesses -- it is EQUIVALENCE under the
// current reachable production topology:
//
//   - ledger/verify.go runs the payload validator during VERIFICATION, not only during
//     Append, and reports ledger.payload_schema_invalid;
//   - admission.admissionValidator IS ledger.ValidateTaskEventPayload, which enforces
//     requiredEventArtifacts;
//   - every production caller of latestArtifactFromChain receives a chain that was
//     verified through that validator first.
//
// So a hand-written artifact-less admission_consumed is refused -- "admission_consumed
// event requires a capability_consumption artifact" -- BEFORE any artifact extraction
// runs, and requiredArtifact never gets the chance to fire. The mutant is EQUIVALENT,
// not surviving-because-untested. It is recorded here rather than deleted, because the
// equivalence is a property of the TOPOLOGY and not of the guard.
//
// AND THAT TOPOLOGY IS WHAT THIS FILE PROTECTS. The moment a reader verifies with nil,
// with a validator that does not enforce the table, or skips verification entirely,
// requiredArtifact stops being equivalent and becomes load-bearing again. The assumption
// is therefore the thing that must be witnessed, and it must go RED at the edit that
// breaks it rather than after an incident.
//
// TWO HALVES, BOTH NECESSARY:
//
//	STRUCTURAL   every ledger.NewStore on an authority-reading path installs a
//	             table-enforcing validator -- none nil, none omitted -- and every
//	             artifact extraction takes its chain from such a verified store.
//	BEHAVIOURAL  that validator actually REFUSES what the table requires. A validator
//	             that is present and enforces nothing satisfies the structural half and
//	             protects nobody.
//
// Subjects are discovered with go/parser and go/ast over the real package source, in the
// idiom of cmd/awg/graph_reader_census_test.go. A textual count breaks on a rename and
// passes on a comment.
//
// MEASURED, when this census was built, by mutating the real packages and restoring them:
//
//	requiredArtifact disabled      every witness in this package stays GREEN. The
//	                               equivalence claim above, confirmed rather than assumed.
//	one reader's validator -> nil  the STRUCTURAL half goes red and names the route
//	                               (ledger_load.go:LoadLatestArtifactOptional), while the
//	                               BEHAVIOURAL half stays green -- because requiredArtifact
//	                               now fires. That is the equivalence dissolving in the
//	                               open: the guard went back to being load-bearing, and
//	                               only the structural half can see it happen.
//	the validator made a no-op     the BEHAVIOURAL half goes red on all three required
//	                               artifacts, while the structural half stays green --
//	                               admissionValidator still reaches
//	                               ValidateTaskEventPayload, which now enforces nothing.
//
// Neither half kills the other's mutant. That is why there are two, and why removing
// either one would leave a census that certifies a topology it cannot see.

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
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

// enforcingValidatorName is the ONE function that reads requiredEventArtifacts. A
// validator enforces the table exactly when it reaches this name; anything else is an
// unrecognised validator, which the census reports as non-enforcing rather than assuming
// it is fine. Membership, not exclusion: a renamed or invented validator shows up as a
// gap instead of silently certifying one.
const enforcingValidatorName = "ValidateTaskEventPayload"

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
	// back to THIS store. An inline construction has no name and therefore cannot be
	// shown to have been verified -- a store built with a validator and a DIFFERENT store
	// verified would otherwise satisfy a laxer reading.
	VarName string

	HasValidatorOption bool
	ValidatorExpr      string
	ValidatorIsNil     bool
	EnforcesTable      bool

	// VerifiedVia is the verification method called on VarName, empty if none was.
	VerifiedVia string
}

// verified reports whether this store may serve as the source of an extracted artifact.
func (s storeFact) verified() bool {
	return s.HasValidatorOption && !s.ValidatorIsNil && s.EnforcesTable && s.VerifiedVia != ""
}

// readerFacts is what one function says about its own authority reading. The questions
// are kept SEPARATE, because "a validator is installed" and "the chain that is read came
// from that store" are different claims and merging them would erase the distinction the
// invariant rests on.
type readerFacts struct {
	Stores []storeFact

	// ExtractsArtifact: this function reaches an artifact out of a chain, either by
	// indexing a payload's Artifacts map directly or by handing a chain to a function
	// that does.
	ExtractsArtifact bool
	// ExtractionRoute names how, for the failure message.
	ExtractionRoute string

	// ConsumesVerifiedChainParam: this function receives a ledger.VerifiedChain as a
	// parameter, so its chain was verified by whoever built it -- and that caller is a
	// census subject in its own right.
	ConsumesVerifiedChainParam bool
}

// isSubject reports whether this function is on an authority-reading path at all. A
// function that neither verifies a ledger nor extracts an artifact is not a subject and
// must not be demanded to do either.
func (f readerFacts) isSubject() bool {
	if f.ExtractsArtifact || f.ConsumesVerifiedChainParam {
		return true
	}
	for _, s := range f.Stores {
		if s.VerifiedVia != "" {
			return true
		}
	}
	return false
}

// authorityReadersIn discovers the census subjects in a package directory.
//
// Emptiness is RETURNED, not fataled: the census's own floor turns "nothing discovered"
// into a failure, and a witness needs to be able to observe an empty discovery without
// the helper aborting it.
func authorityReadersIn(t *testing.T, dir string) map[string]readerFacts {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return strings.HasSuffix(fi.Name(), ".go") && !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", dir, err)
	}

	graph := map[string]map[string]bool{}
	chainConsumers := map[string]bool{}
	type decl struct {
		base string
		fn   *ast.FuncDecl
	}
	var decls []decl

	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			base := filepath.Base(name)
			for _, d := range file.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				decls = append(decls, decl{base: base, fn: fn})
				called := calledNamesIn(fn.Body)
				existing := graph[fn.Name.Name]
				if existing == nil {
					existing = map[string]bool{}
					graph[fn.Name.Name] = existing
				}
				for n := range called {
					existing[n] = true
				}
				if takesVerifiedChain(fn) {
					chainConsumers[fn.Name.Name] = true
				}
			}
		}
	}

	out := map[string]readerFacts{}
	for _, d := range decls {
		facts := readerFacts{
			Stores:                     storesIn(d.fn.Body, graph),
			ConsumesVerifiedChainParam: chainConsumers[d.fn.Name.Name],
		}
		if indexesArtifacts(d.fn.Body) {
			facts.ExtractsArtifact = true
			facts.ExtractionRoute = "indexes a payload's Artifacts map directly"
		} else if callee := callsChainConsumer(d.fn.Body, chainConsumers); callee != "" {
			facts.ExtractsArtifact = true
			facts.ExtractionRoute = "hands a chain to " + callee
		}
		if !facts.isSubject() {
			continue
		}
		out[d.base+":"+d.fn.Name.Name] = facts
	}
	return out
}

// calledNamesIn returns every function and method name called anywhere in body.
func calledNamesIn(body *ast.BlockStmt) map[string]bool {
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

// reachesName reports whether `from` calls `want`, or calls something that does,
// transitively within this package.
//
// WHY TRANSITIVE. admissionValidator delegates in one line today, but a validator that
// dispatches through a helper is no less enforcing. A census that only reads the
// validator's own body would push the enforcement back out of shared code to stay
// readable, which is measuring its own convenience.
func reachesName(graph map[string]map[string]bool, from, want string) bool {
	seen := map[string]bool{}
	stack := []string{from}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[cur] {
			continue
		}
		seen[cur] = true
		for name := range graph[cur] {
			if name == want {
				return true
			}
			if _, declared := graph[name]; declared && !seen[name] {
				stack = append(stack, name)
			}
		}
	}
	return false
}

// takesVerifiedChain reports whether fn accepts a ledger.VerifiedChain parameter.
func takesVerifiedChain(fn *ast.FuncDecl) bool {
	if fn.Type.Params == nil {
		return false
	}
	for _, field := range fn.Type.Params.List {
		if sel, ok := field.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "VerifiedChain" {
			return true
		}
	}
	return false
}

// indexesArtifacts reports whether body reads a payload's Artifacts map by key. That is
// artifact extraction in its rawest form.
func indexesArtifacts(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		idx, ok := n.(*ast.IndexExpr)
		if !ok {
			return true
		}
		if sel, ok := idx.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "Artifacts" {
			found = true
		}
		return true
	})
	return found
}

// callsChainConsumer reports the first function in `consumers` that body calls, i.e. the
// first place this function HANDS A CHAIN to an extractor.
//
// Delegating to a loader that verifies its own ledger (LoadLatestArtifact ->
// LoadLatestArtifactOptional) is deliberately NOT this: no chain crosses that call, so
// the callee's own verification is the one that counts and the caller owes nothing.
func callsChainConsumer(body *ast.BlockStmt, consumers map[string]bool) string {
	var names []string
	for name := range calledNamesIn(body) {
		if consumers[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

// storesIn returns one storeFact per ledger.NewStore construction in body, with the
// verification (if any) that was performed on that same variable.
func storesIn(body *ast.BlockStmt, graph map[string]map[string]bool) []storeFact {
	// varName -> verification method called on it.
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

	// The NewStore calls that were bound to a variable, and which AST node each was.
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
		for _, arg := range call.Args[minInt(1, len(call.Args)):] {
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
			if id, ok := opt.Args[0].(*ast.Ident); ok && id.Name == "nil" {
				fact.ValidatorIsNil = true
			}
			fact.EnforcesTable = !fact.ValidatorIsNil && enforcesTable(opt.Args[0], graph)
		}
		out = append(out, fact)
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].VarName < out[j].VarName })
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func isNewStoreCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "NewStore" && len(call.Args) >= 1
}

// enforcesTable reports whether the validator expression reaches the one function that
// reads requiredEventArtifacts.
func enforcesTable(expr ast.Expr, graph map[string]map[string]bool) bool {
	switch v := expr.(type) {
	case *ast.SelectorExpr:
		if v.Sel.Name == enforcingValidatorName {
			return true
		}
		return reachesName(graph, v.Sel.Name, enforcingValidatorName)
	case *ast.Ident:
		return reachesName(graph, v.Name, enforcingValidatorName)
	case *ast.FuncLit:
		for name := range calledNamesIn(v.Body) {
			if name == enforcingValidatorName || reachesName(graph, name, enforcingValidatorName) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// censusFailures is the classifier, extracted from the census so it can be driven
// against synthetic sources. On a healthy tree every real-tree assertion is trivially
// satisfied, so disabling one changes nothing -- a census whose mutants all survive is
// not evidence.
func censusFailures(subjects map[string]readerFacts) []string {
	var failures []string
	for _, name := range sortedKeys(subjects) {
		facts := subjects[name]
		for _, store := range facts.Stores {
			switch {
			case !store.HasValidatorOption:
				failures = append(failures, fmt.Sprintf(
					"%s: ledger.NewStore is built with NO payload validator; the chain it verifies is "+
						"checked against nothing, so an artifact-less event of a type the table requires "+
						"one for verifies clean and reaches extraction", name))
			case store.ValidatorIsNil:
				failures = append(failures, fmt.Sprintf(
					"%s: ledger.NewStore is built with a NIL payload validator; ledger/verify.go skips a "+
						"nil validator entirely, so the event table is not enforced on this path", name))
			case !store.EnforcesTable:
				failures = append(failures, fmt.Sprintf(
					"%s: payload validator %q does not reach %s, the only function that reads "+
						"requiredEventArtifacts; a validator that enforces nothing protects nobody",
					name, store.ValidatorExpr, enforcingValidatorName))
			}
		}
		if !facts.ExtractsArtifact {
			continue
		}
		if facts.ConsumesVerifiedChainParam {
			continue
		}
		if verifiedStoreIn(facts.Stores) {
			continue
		}
		failures = append(failures, fmt.Sprintf(
			"%s: %s, but its chain does not come from a store that installs a table-enforcing "+
				"validator AND is verified through it; artifact extraction here is OUTSIDE the "+
				"verified boundary", name, facts.ExtractionRoute))
	}
	return failures
}

func verifiedStoreIn(stores []storeFact) bool {
	for _, s := range stores {
		if s.verified() {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]readerFacts) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// repoRootFromHere locates the repository root from this test file's own path.
func repoRootFromHere(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the trustkernel package directory")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}

func admissionPackageDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(repoRootFromHere(t), "golang", "architecture", "admission")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("locate the admission package: %v", err)
	}
	return dir
}

// ---------------------------------------------------------------------------
// HALF ONE: THE STRUCTURAL CENSUS, against the real admission package.
// ---------------------------------------------------------------------------

// EVERY AUTHORITY-ARTIFACT READER CONSUMES A CHAIN VERIFIED BY A TABLE-ENFORCING
// VALIDATOR.
//
// This is the assumption that makes requiredArtifact's mutant EQUIVALENT. It is asserted
// over whatever the parser finds, never over a maintained list: a reader added tomorrow
// is a subject the moment it is written.
func TestEveryAuthorityArtifactReaderVerifiesThroughATableEnforcingValidator(t *testing.T) {
	subjects := authorityReadersIn(t, admissionPackageDir(t))

	// ANTI-VACUITY. A census with no subjects asserts nothing and still reports PASS.
	if len(subjects) == 0 {
		t.Fatal("the authority-reader census discovered NO subjects in the admission package; " +
			"either every reader was removed or the discovery rule no longer matches the code it " +
			"is supposed to measure. Either way this census now asserts nothing")
	}

	// The enforcement detector must have a live subject too: if nothing in the package
	// installs a table-enforcing validator, "no failures" would mean the detector never
	// ran rather than that the code is sound.
	enforcing := 0
	extractors := 0
	for _, facts := range subjects {
		for _, s := range facts.Stores {
			if s.EnforcesTable {
				enforcing++
			}
		}
		if facts.ExtractsArtifact {
			extractors++
		}
	}
	if enforcing == 0 {
		t.Fatal("no store in the admission package installs a validator that reaches " +
			enforcingValidatorName + "; the enforcement detector has lost its subject")
	}
	if extractors == 0 {
		t.Fatal("the census found no artifact extraction in the admission package; " +
			"the extraction-boundary rule has lost its subject")
	}

	if failures := censusFailures(subjects); len(failures) > 0 {
		t.Errorf("%d authority-reading path(s) break the verified boundary:\n  %s\n\n"+
			"admission.requiredArtifact's mutant is EQUIVALENT only while every reader verifies "+
			"through a table-enforcing validator first. Each route above is a path on which that "+
			"guard becomes load-bearing again.",
			len(failures), strings.Join(failures, "\n  "))
	}

	t.Logf("authority-reader census: %d subject(s), %d enforcing store(s), %d extractor(s)",
		len(subjects), enforcing, extractors)
}

// ---------------------------------------------------------------------------
// HALF TWO: THE BEHAVIOURAL CENSUS, against the real validator.
// ---------------------------------------------------------------------------

// THE VALIDATOR ACTUALLY REFUSES WHAT THE TABLE REQUIRES.
//
// The structural half proves a validator is installed and that it reaches
// ValidateTaskEventPayload. That is worth nothing on its own: a validator that is
// present and enforces nothing satisfies it exactly.
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
//	                     above is what pins that function as the one admission installs.
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
					t.Fatalf("the validator admission installs ACCEPTED a %s payload carrying no "+
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
			"closureprotocol.LedgerEventTypes: this half asserts nothing, and admission's readers " +
			"have no contract left to enforce")
	}
}

// THE TABLE IS FULLY REACHABLE FROM THE VOCABULARY THE HALF ABOVE ITERATES.
//
// The behavioural half enumerates closureprotocol.LedgerEventTypes and asks the table
// about each. A requirement declared for an event type that vocabulary omits would be
// silently untested, and the half would still report PASS. This reads the table's own
// keys out of the source and requires the vocabulary to cover them.
func TestEveryRequiredArtifactEventTypeIsInTheIteratedVocabulary(t *testing.T) {
	keys := requiredArtifactTableKeys(t, filepath.Join(repoRootFromHere(t),
		"golang", "architecture", "ledger", "event.go"))
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
// changes nothing and every mutant of it survives. These drive the SAME discovery and
// classifier over synthetic sources, where they must go red.
// ---------------------------------------------------------------------------

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

// soundReader mirrors the real shape: a table-enforcing validator, verified through the
// same store, extraction from that verified chain.
const soundReader = `package fake

func enforcingValidator(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
	return ledger.ValidateTaskEventPayload(eventType, data)
}

func LoadSound(taskDir string, key string, out any) (bool, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(enforcingValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		return false, err
	}
	return fromChain(taskDir, chain, key, out)
}

func fromChain(taskDir string, chain ledger.VerifiedChain, key string, out any) (bool, error) {
	for i := len(chain.Entries) - 1; i >= 0; i-- {
		payload, err := ledger.ParseTaskEventPayload(nil)
		if err != nil {
			return false, err
		}
		if ref, ok := payload.Artifacts[key]; ok {
			_ = ref
			return true, nil
		}
	}
	return false, nil
}
`

const nilValidatorReader = `package fake

func LoadWithNil(taskDir string, key string) (bool, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(nil))
	chain, err := store.VerifyChain()
	if err != nil {
		return false, err
	}
	payload, err := ledger.ParseTaskEventPayload(nil)
	if err != nil {
		return false, err
	}
	_, ok := payload.Artifacts[key]
	_ = chain
	return ok, nil
}
`

const omittedValidatorReader = `package fake

func LoadWithoutOption(taskDir string, key string) (bool, error) {
	store := ledger.NewStore(taskDir)
	chain, err := store.VerifyChain()
	if err != nil {
		return false, err
	}
	payload, err := ledger.ParseTaskEventPayload(nil)
	if err != nil {
		return false, err
	}
	_, ok := payload.Artifacts[key]
	_ = chain
	return ok, nil
}
`

const nonEnforcingValidatorReader = `package fake

func looseValidator(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
	return nil
}

func LoadWithLooseValidator(taskDir string, key string) (bool, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(looseValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		return false, err
	}
	payload, err := ledger.ParseTaskEventPayload(nil)
	if err != nil {
		return false, err
	}
	_, ok := payload.Artifacts[key]
	_ = chain
	return ok, nil
}
`

const unverifiedExtraction = `package fake

func LoadStraightOffDisk(taskDir string, key string) (bool, error) {
	data, err := os.ReadFile(taskDir)
	if err != nil {
		return false, err
	}
	payload, err := ledger.ParseTaskEventPayload(data)
	if err != nil {
		return false, err
	}
	_, ok := payload.Artifacts[key]
	return ok, nil
}
`

// THE FOUR WAYS THE BOUNDARY BREAKS, each driven against the classifier.
func TestTheCensusReportsEveryWayTheVerifiedBoundaryBreaks(t *testing.T) {
	cases := []struct {
		name     string
		source   string
		subject  string
		wantPart string
	}{
		{
			name:     "a store built with a NIL validator",
			source:   nilValidatorReader,
			subject:  "reader.go:LoadWithNil",
			wantPart: "NIL payload validator",
		},
		{
			name:     "a store built with NO validator option",
			source:   omittedValidatorReader,
			subject:  "reader.go:LoadWithoutOption",
			wantPart: "NO payload validator",
		},
		{
			name:     "a validator that does not enforce the table",
			source:   nonEnforcingValidatorReader,
			subject:  "reader.go:LoadWithLooseValidator",
			wantPart: "does not reach " + enforcingValidatorName,
		},
		{
			name:     "extraction reached outside the verified boundary",
			source:   unverifiedExtraction,
			subject:  "reader.go:LoadStraightOffDisk",
			wantPart: "OUTSIDE the verified boundary",
		},
	}
	if len(cases) == 0 {
		t.Fatal("the boundary-failure table is empty: this witness asserts nothing")
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := censusFixture(t, map[string]string{"reader.go": tc.source})
			subjects := authorityReadersIn(t, dir)
			if _, found := subjects[tc.subject]; !found {
				t.Fatalf("the census did not even DISCOVER %s; it cannot report what it cannot see. "+
					"discovered: %v", tc.subject, sortedKeys(subjects))
			}
			failures := censusFailures(subjects)
			if len(failures) == 0 {
				t.Fatalf("the census reported NO failure for %q; it would certify this package", tc.name)
			}
			joined := strings.Join(failures, "\n")
			if !strings.Contains(joined, tc.subject) {
				t.Errorf("the failure does not NAME the route %s:\n%s", tc.subject, joined)
			}
			if !strings.Contains(joined, tc.wantPart) {
				t.Errorf("the failure does not say %q:\n%s", tc.wantPart, joined)
			}
		})
	}
}

// THE NEGATIVE CONTROL. Every refusal above is satisfiable by a census that fails
// everything. A correctly wired reader must produce NO failure, or the census is noise
// and the real-tree assertion means nothing.
func TestTheCensusPassesASoundlyVerifiedReader(t *testing.T) {
	dir := censusFixture(t, map[string]string{"reader.go": soundReader})
	subjects := authorityReadersIn(t, dir)
	if len(subjects) == 0 {
		t.Fatal("the census discovered no subject in a package that plainly reads authority artifacts")
	}
	if failures := censusFailures(subjects); len(failures) != 0 {
		t.Errorf("a soundly verified reader was reported as breaking the boundary:\n  %s",
			strings.Join(failures, "\n  "))
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
	failures := censusFailures(authorityReadersIn(t, dir))
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

// A FUNCTION THAT READS NO AUTHORITY IS NOT A SUBJECT, and must not be demanded to
// verify anything. Without this the census would grow to demand verification of every
// function in the package, and the first person to hit that would loosen the rule.
func TestAFunctionThatReadsNoAuthorityIsNotACensusSubject(t *testing.T) {
	dir := censusFixture(t, map[string]string{
		"sound.go": soundReader,
		"plain.go": "package fake\n\nfunc Unrelated(a int) int { return a + 1 }\n",
	})
	subjects := authorityReadersIn(t, dir)
	if _, listed := subjects["plain.go:Unrelated"]; listed {
		t.Error("a function that reads no ledger was made a census subject")
	}
	if failures := censusFailures(subjects); len(failures) != 0 {
		t.Errorf("unexpected failures: %v", failures)
	}
}

// DELEGATION IS NOT EXTRACTION. LoadLatestArtifact calls LoadLatestArtifactOptional,
// which verifies its own ledger; no chain crosses that call. Demanding a store of the
// delegator would be a false positive, and the first false positive is what gets a
// census disabled.
func TestDelegatingToASelfVerifyingLoaderIsNotAnExtraction(t *testing.T) {
	const delegator = `
func LoadOrFail(taskDir string, key string, out any) error {
	found, err := LoadSound(taskDir, key, out)
	if err != nil {
		return err
	}
	if !found {
		return errors.New("absent")
	}
	return nil
}
`
	dir := censusFixture(t, map[string]string{"reader.go": soundReader + delegator})
	subjects := authorityReadersIn(t, dir)
	if _, listed := subjects["reader.go:LoadOrFail"]; listed {
		t.Error("a delegator that hands no chain to anyone was made a census subject")
	}
	if failures := censusFailures(subjects); len(failures) != 0 {
		t.Errorf("a delegating loader was reported as breaking the boundary:\n  %s",
			strings.Join(failures, "\n  "))
	}
}

// ANTI-VACUITY, DRIVEN RATHER THAN ASSERTED. The discovery must find nothing in a
// package with no readers, and the census's floor is what turns that into a failure.
// Without this, removing the floor changes no test.
func TestTheCensusDiscoversNothingWhenThereIsNothingToDiscover(t *testing.T) {
	dir := censusFixture(t, map[string]string{
		"plain.go": "package fake\n\nfunc Unrelated(a int) int { return a + 1 }\n",
	})
	if subjects := authorityReadersIn(t, dir); len(subjects) != 0 {
		t.Errorf("a package with no authority reader yielded %d subject(s): %v",
			len(subjects), sortedKeys(subjects))
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
	fixed := strings.Replace(soundReader, "LoadSound", "LoadNewcomer", 1)
	fixed = strings.Replace(fixed, "func enforcingValidator", "func newcomerValidator", 1)
	fixed = strings.Replace(fixed, "WithPayloadValidator(enforcingValidator)", "WithPayloadValidator(newcomerValidator)", 1)
	fixed = strings.Replace(fixed, "func fromChain", "func newcomerFromChain", 1)
	fixed = strings.Replace(fixed, "return fromChain(", "return newcomerFromChain(", 1)
	dir = censusFixture(t, map[string]string{
		"sound.go":    soundReader,
		"newcomer.go": fixed,
	})
	if failures := censusFailures(authorityReadersIn(t, dir)); len(failures) != 0 {
		t.Errorf("a newcomer that verifies correctly stayed in the failure set:\n  %s",
			strings.Join(failures, "\n  "))
	}
}
