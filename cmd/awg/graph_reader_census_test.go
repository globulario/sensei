// SPDX-License-Identifier: AGPL-3.0-only

package main

// PORTED MEASUREMENT INSTRUMENT — the property-derived production graph-reader census.
//
// Harvested from #361 (`cmd/awg/graph_reader_test.go`) under a measurement-only authorization.
// #361 is a repair corpus, not a merge vehicle: it is a parallel implementation against an
// obsolete base whose landing would delete admitted repairs. Its census, however, is the only
// mechanical way to ask what admitted main actually guarantees, so the INSTRUMENT is ported and
// none of its production repairs are.
//
// INSTRUMENT ADAPTATION vs PRODUCTION REPAIR. Nothing here changes production behaviour, and the
// recognised-name vocabularies are carried over UNCHANGED. That is sound rather than lazy:
// admitted main declares exactly one owner-resolution entry point (`resolveGraphReader`) and one
// graphReader verification method (`verifyServed`), plus the reporting path
// (`renderEndpointBlock` / `verifyActiveGeneration`). #361's sets are a strict superset, and
// because both are read BY MEMBERSHIP an absent name simply never matches. So no name needed
// adapting, and the extra entries make the instrument forward-compatible rather than permissive.
//
// A LARGE GAP IS A VALID RESULT. This file asserts the instrument's own properties, never a
// target count. The real-tree measurement is reported by TestReportTheAdmittedMainCensus and is
// deliberately non-failing: its job is to state the truth about admitted main, not to pass.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// classifyGraphCommands splits the census into its three groups. Extracted from the census so
// the rule can be driven against synthetic inputs: every mutant of the census's real-tree
// assertions survived, because on a healthy tree each of them is trivially satisfied and
// disabling it changes nothing. A census whose mutants all survive is not evidence.
func classifyGraphCommands(subjects map[string]graphCommandFacts) (verifies, unchecked, noOwner []string) {
	for cmd, facts := range subjects {
		if !facts.ResolvesOwner {
			noOwner = append(noOwner, cmd)
		}
		if facts.VerifiesGeneration {
			verifies = append(verifies, cmd)
		} else {
			unchecked = append(unchecked, cmd)
		}
	}
	sort.Strings(verifies)
	sort.Strings(unchecked)
	sort.Strings(noOwner)
	return verifies, unchecked, noOwner
}

// graphCommandsIn returns, per command entry point in dir, whether that function declares an
// `addr` flag and whether it resolves through the owner. The unit is the FUNCTION, which is
// what "command" means here: one file may define several.
//

// graphCommandFacts is what one command's body says about its own graph authority. Two
// SEPARATE questions, never merged:
//
//	ResolvesOwner       who decides which graph instance is used (endpoint ownership)
//	VerifiesGeneration  whether the graph answering is the one this domain declares ACTIVE
//
// runRepairGate closed the first and explicitly not the second, which is why one boolean
// would have erased the distinction the whole front rests on.
type graphCommandFacts struct {
	ResolvesOwner      bool
	VerifiesGeneration bool
	// RefusesUnverified is the STRONGER fact: the subject reaches a guard that refuses unless the
	// served generation IS the declared ACTIVE one, including refusing when nothing is declared.
	//
	// VerifiesGeneration is satisfied by reaching any comparison, and verifyServed returns nil when
	// no ACTIVE generation is declared. That reading is correct for runMetadata and runBriefing,
	// which REPORT the verdict, and insufficient for a subject that CONSUMES the answer -- for which
	// "a comparison is present" and "an unverifiable graph is refused" are different claims. Counting
	// only the weaker one would let a fail-open call satisfy the census.
	RefusesUnverified bool
}

// graphAuthorityNames are the two vocabularies the census reads, both by MEMBERSHIP of a
// recognised set rather than by excluding known-bad names: an unrecognised call is not a
// verification, so a renamed or invented helper shows up as a gap instead of silently
// certifying one.
var (
	ownerResolutionNames = map[string]bool{
		"productionReaderFor": true,

		"resolveGraphReader": true,
	}
	// refusingGenerationCheckNames: guards that refuse an unverifiable served generation rather than
	// reporting on it. A subject reaching one of these has a comparison that cannot be satisfied by
	// an absent declaration.
	refusingGenerationCheckNames = map[string]bool{
		"requireVerifiedServedGeneration": true,
	}

	generationCheckNames = map[string]bool{
		// The owner's comparisons, and the digest-level one underneath both.
		"verifyServedAuthority": true,
		// MetadataResponse states the served generation twice, so it has its own reading of the
		// same rule -- see graphReader.verifyServedMetadata.
		"verifyServedMetadata": true,
		"verifyServed":         true,
		// metadata REPORTS the verdict rather than refusing on it, which is its purpose;
		// renderEndpointBlock is where it asks.
		"renderEndpointBlock":    true,
		"verifyActiveGeneration": true,
	}
)

// calledNames returns every function and method name called anywhere in body, plus whether

// the body declares its own "addr" flag.
func calledNames(body *ast.BlockStmt) (names map[string]bool, declaresAddr bool) {
	names = map[string]bool{}
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
			// fs.String("addr", ...) — this function's own flag, not the file's.
			if f.Sel.Name == "String" && len(call.Args) > 0 {
				if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Value == `"addr"` {
					declaresAddr = true
				}
			}
		}
		return true
	})
	return names, declaresAddr
}

// packageCallGraph maps every function and method declared in dir to the names it calls.
// Methods are keyed by their own name, which is how they are called; two methods sharing a
// name would merge, and that is recorded here rather than discovered later.
func packageCallGraph(t *testing.T, dir string) map[string]map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return strings.HasSuffix(fi.Name(), ".go") && !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", dir, err)
	}
	graph := map[string]map[string]bool{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, d := range file.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				names, _ := calledNames(fn.Body)
				existing := graph[fn.Name.Name]
				if existing == nil {
					existing = map[string]bool{}
					graph[fn.Name.Name] = existing
				}
				for n := range names {
					existing[n] = true
				}
			}
		}
	}
	return graph
}

// reaches reports whether any name in `want` is called by `from`, or by anything it calls,
// transitively within this package.
//
// WHY TRANSITIVE. The first version of this census read only the run* function's own body.
// That was too narrow in a way that mattered: once several commands consume graph evidence
// through ONE shared function -- generateRepairReport serves repair-report and repair-gate,
// buildAuthoritativeRepairPlan serves both benchmark commands -- the comparison correctly
// lives there, and a body-only census reported the commands as unverified while they were
// verified. A census that pushes checks back out of shared code to stay readable is
// measuring its own convenience.
//
// WHAT IT DOES NOT PROVE. Reachability is not enforcement: a helper could compare on one
// branch and consume on another, and this would still count it. That distinction is proven
// by the witnesses in served_authority_test.go, which drive the real commands against a
// server serving a generation the domain does not declare. This census answers
// completeness -- is any subject unaccounted for -- and the witnesses answer correctness.
// Neither substitutes for the other.
func reaches(graph map[string]map[string]bool, from string, want map[string]bool) bool {
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
			if want[name] {
				return true
			}
			if _, declared := graph[name]; declared && !seen[name] {
				stack = append(stack, name)
			}
		}
	}
	return false
}

func graphCommandsIn(t *testing.T, dir string) map[string]graphCommandFacts {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		n := fi.Name()
		return strings.HasPrefix(n, "cmd_") && strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", dir, err)
	}
	graph := packageCallGraph(t, dir)
	out := map[string]graphCommandFacts{}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			base := filepath.Base(name)
			// `serve` is the exception with a reason: its --addr is the address it LISTENS on,
			// not a graph it reads. Named explicitly rather than pattern-excluded, so a future
			// reader cannot slip through by resembling it.
			if base == "cmd_serve.go" {
				continue
			}
			for _, d := range file.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "run") {
					continue
				}
				names, declaresAddr := calledNames(fn.Body)
				if !declaresAddr {
					continue
				}
				facts := graphCommandFacts{}
				// Direct calls first, then the rest of this command's own call graph. Per
				// COMMAND either way: one verifying command in a file cannot certify a
				// sibling that does not, because the traversal starts at this function.
				for n := range names {
					if ownerResolutionNames[n] {
						facts.ResolvesOwner = true
					}
					if refusingGenerationCheckNames[n] {
						facts.RefusesUnverified = true
					}
					if generationCheckNames[n] {
						facts.VerifiesGeneration = true
					}
				}
				// REFUSING IMPLIES COMPARING, BY CONSTRUCTION.
				//
				// A guard that refuses an unverifiable served generation necessarily performs the
				// comparison, so the stronger fact must entail the weaker one. Today it does anyway,
				// via the transitive reaches() fallback below finding verifyActiveGeneration down the
				// call chain -- but that is an accident of one chain, not a property of the
				// classifier. Blind review P2 on 77fa5e2b named the missing implication; its predicted
				// failure did not occur because of that fallback, and the weakness was real regardless.
				// Stated here so the hierarchy cannot be broken by the two name sets drifting apart.
				if facts.RefusesUnverified {
					facts.VerifiesGeneration = true
				}
				if !facts.ResolvesOwner {
					facts.ResolvesOwner = reaches(graph, fn.Name.Name, ownerResolutionNames)
				}
				if !facts.RefusesUnverified {
					facts.RefusesUnverified = reaches(graph, fn.Name.Name, refusingGenerationCheckNames)
				}
				if !facts.VerifiesGeneration {
					facts.VerifiesGeneration = facts.RefusesUnverified ||
						reaches(graph, fn.Name.Name, generationCheckNames)
				}
				out[base+":"+fn.Name.Name] = facts
			}
		}
	}
	// Emptiness is RETURNED, not fataled: the census's own floor turns it into a failure, and a
	// witness needs to be able to observe an empty discovery without the helper aborting it.
	return out
}

// unmigratedGraphCommands names the graph-reading commands that do NOT resolve their endpoint

// through the owner.
func unmigratedGraphCommands(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	for cmd, facts := range graphCommandsIn(t, dir) {
		if !facts.ResolvesOwner {
			out = append(out, cmd)
		}
	}
	sort.Strings(out)
	return out
}

// THE READER CENSUS, DERIVED AND COMMAND-SCOPED.
//

// --- THE INSTRUMENT'S OWN WITNESSES, ported unchanged ---------------------------------
//
// A detector nobody has driven against a known-bad input is an assumption. These drive the
// enumerator over synthetic command files, the only way to prove it FAILS when it should.
//
// The fixtures name #361's helper (productionReaderFor) deliberately: they prove the MEMBERSHIP
// rule works for any recognised owner name, including one admitted main does not declare — which
// is what makes the instrument forward-compatible rather than tuned to today's tree.

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

const migratedCmd = `package main

func runAlpha(args []string) int {
	fs := newFlagSet()
	addr := fs.String("addr", "", "")
	domain := fs.String("domain", "", "")
	reader := productionReaderFor(fs, *domain, *addr)
	*addr = reader.Addr
	return 0
}
`

const bypassingCmd = `package main

func runBeta(args []string) int {
	fs := newFlagSet()
	addr := fs.String("addr", "", "")
	_ = dial(*addr)
	return 0
}
`

// 1. Both commands migrated: the census passes.
func TestTheCensusPassesWhenEveryCommandIsMigrated(t *testing.T) {
	dir := censusFixture(t, map[string]string{"cmd_a.go": migratedCmd})
	if got := unmigratedGraphCommands(t, dir); len(got) != 0 {
		t.Errorf("a migrated command was reported as unmigrated: %v", got)
	}
}

// 2 and 3. THE DEFECT THIS CENSUS EXISTS FOR. One migrated command must not certify a
// bypassing one -- neither in another file nor, the case that actually happened, in the SAME
// file.
func TestOneMigratedCommandCannotCertifyABypassingOne(t *testing.T) {
	t.Run("different files", func(t *testing.T) {
		dir := censusFixture(t, map[string]string{"cmd_a.go": migratedCmd, "cmd_b.go": bypassingCmd})
		assertNamesOnly(t, unmigratedGraphCommands(t, dir), "cmd_b.go:runBeta")
	})
	t.Run("same file", func(t *testing.T) {
		dir := censusFixture(t, map[string]string{
			"cmd_both.go": migratedCmd + "\n" + strings.Replace(bypassingCmd, "package main\n\n", "", 1),
		})
		assertNamesOnly(t, unmigratedGraphCommands(t, dir), "cmd_both.go:runBeta")
	})
}

// 4. A newly added graph-reaching command with no owner wiring fails, which is the census's
// standing promise about the future.
func TestANewlyAddedGraphReachingCommandWithoutTheOwnerFails(t *testing.T) {
	dir := censusFixture(t, map[string]string{"cmd_a.go": migratedCmd, "cmd_new.go": strings.Replace(bypassingCmd, "runBeta", "runBrandNew", 1)})
	assertNamesOnly(t, unmigratedGraphCommands(t, dir), "cmd_new.go:runBrandNew")
}

// A command with no addr flag is not a graph reader and must not be demanded to resolve one.
func TestACommandWithNoEndpointFlagIsNotACensusSubject(t *testing.T) {
	dir := censusFixture(t, map[string]string{
		"cmd_a.go": migratedCmd,
		"cmd_c.go": "package main\n\nfunc runGamma(args []string) int { return 0 }\n",
	})
	for _, name := range unmigratedGraphCommands(t, dir) {
		if strings.Contains(name, "runGamma") {
			t.Errorf("a command that reads no graph was demanded to resolve an endpoint: %s", name)
		}
	}
	if _, listed := graphCommandsIn(t, dir)["cmd_c.go:runGamma"]; listed {
		t.Error("runGamma declares no addr flag and must not be a census subject at all")
	}
}

func assertNamesOnly(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("census reported %v, want exactly %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("census[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// The reader census must be FALSIFIABLE. Its real-tree assertions are all satisfied on a healthy
// tree, so disabling any of them changes nothing -- every mutant survived. These drive the
// census's own logic against inputs where it must fail.

const verifyingCmd = `package main

func runVerifier(args []string) int {
	fs := newFlagSet()
	addr := fs.String("addr", "", "")
	domain := fs.String("domain", "", "")
	reader := productionReaderFor(fs, *domain, *addr)
	*addr = reader.Addr
	if err := reader.verifyServedAuthority(resp.GetAuthority()); err != nil {
		return 1
	}
	return 0
}
`

const nonVerifyingCmd = `package main

func runTruster(args []string) int {
	fs := newFlagSet()
	addr := fs.String("addr", "", "")
	domain := fs.String("domain", "", "")
	reader := productionReaderFor(fs, *domain, *addr)
	*addr = reader.Addr
	return 0
}
`

// PER-FUNCTION VERIFICATION. A verifying command must not certify a non-verifying sibling, and
// the case that matters is both in ONE file -- which is how cmd_edit_check and cmd_edit_guard
// were excused and how runDomains stayed invisible.
func TestVerificationIsDetectedPerCommandNotPerFile(t *testing.T) {
	t.Run("separate files", func(t *testing.T) {
		dir := censusFixture(t, map[string]string{"cmd_v.go": verifyingCmd, "cmd_t.go": nonVerifyingCmd})
		assertCensusGroups(t, dir, []string{"cmd_v.go:runVerifier"}, []string{"cmd_t.go:runTruster"})
	})
	t.Run("same file", func(t *testing.T) {
		dir := censusFixture(t, map[string]string{
			"cmd_both.go": verifyingCmd + "\n" + strings.Replace(nonVerifyingCmd, "package main\n\n", "", 1),
		})
		assertCensusGroups(t, dir,
			[]string{"cmd_both.go:runVerifier"}, []string{"cmd_both.go:runTruster"})
	})
}

func assertCensusGroups(t *testing.T, dir string, wantVerifies, wantUnchecked []string) {
	t.Helper()
	verifies, unchecked, noOwner := classifyGraphCommands(graphCommandsIn(t, dir))
	if len(noOwner) != 0 {
		t.Errorf("both fixtures resolve through the owner, yet %v were reported as not doing so", noOwner)
	}
	assertNamesOnly(t, verifies, wantVerifies...)
	assertNamesOnly(t, unchecked, wantUnchecked...)
}

// The groups must PARTITION the subjects. Driven against an input where a broken classification
// would double-count or drop, which the real tree cannot exhibit.
func TestTheCensusGroupsPartitionItsSubjects(t *testing.T) {
	subjects := map[string]graphCommandFacts{
		"a.go:runA": {ResolvesOwner: true, VerifiesGeneration: true},
		"b.go:runB": {ResolvesOwner: true},
		"c.go:runC": {},
	}
	verifies, unchecked, noOwner := classifyGraphCommands(subjects)
	if len(verifies)+len(unchecked) != len(subjects) {
		t.Errorf("groups do not partition: %d + %d != %d", len(verifies), len(unchecked), len(subjects))
	}
	assertNamesOnly(t, noOwner, "c.go:runC")
	assertNamesOnly(t, verifies, "a.go:runA")
	assertNamesOnly(t, unchecked, "b.go:runB", "c.go:runC")
}

// ANTI-VACUITY, driven rather than asserted about the real tree: the enumerator must discover
// nothing in a directory with no commands, and the census's floor is what turns that into a
// failure. Without this, removing the floor changes no test.
func TestTheCensusDiscoversNothingWhenThereIsNothingToDiscover(t *testing.T) {
	dir := censusFixture(t, map[string]string{
		"cmd_none.go": "package main\n\nfunc runNoGraph(args []string) int { return 0 }\n",
	})
	subjects := map[string]graphCommandFacts{}
	func() {
		defer func() { _ = recover() }()
		subjects = graphCommandsIn(t, dir)
	}()
	if len(subjects) != 0 {
		t.Errorf("a directory with no graph-reading command yielded %d subject(s): %v", len(subjects), subjects)
	}
}

// A NEWLY INTRODUCED production graph-reading command must be DISCOVERED and must appear as
// unverified until it verifies. This is the census's standing promise about the future, and it is
// the one the family's completion condition rests on: the set of covered commands is whatever the
// census derives, not a number anyone maintains.
func TestANewProductionGraphCommandIsDiscoveredAsUnverified(t *testing.T) {
	dir := censusFixture(t, map[string]string{
		"cmd_a.go": verifyingCmd,
		// A newcomer that resolves its endpoint through the owner and never compares authority.
		"cmd_newcomer.go": strings.Replace(nonVerifyingCmd, "runTruster", "runNewcomer", 1),
	})
	subjects := graphCommandsIn(t, dir)
	facts, found := subjects["cmd_newcomer.go:runNewcomer"]
	if !found {
		t.Fatalf("a new production graph-reading command was not discovered: %v", subjects)
	}
	if !facts.ResolvesOwner {
		t.Errorf("the newcomer resolves through the owner but was not recorded as doing so")
	}
	if facts.VerifiesGeneration {
		t.Errorf("the newcomer compares no authority but was recorded as verifying")
	}
	_, unchecked, _ := classifyGraphCommands(subjects)
	assertNamesOnly(t, unchecked, "cmd_newcomer.go:runNewcomer")
}

// And once it DOES verify, the census moves it -- so the gap count tracks the code rather than a
// maintained list. Without this the previous assertion could be satisfied by a census that calls
// everything unverified.
func TestAVerifyingCommandLeavesTheGapSet(t *testing.T) {
	dir := censusFixture(t, map[string]string{
		"cmd_newcomer.go": strings.Replace(verifyingCmd, "runVerifier", "runNewcomer", 1),
	})
	verifies, unchecked, _ := classifyGraphCommands(graphCommandsIn(t, dir))
	assertNamesOnly(t, verifies, "cmd_newcomer.go:runNewcomer")
	if len(unchecked) != 0 {
		t.Errorf("a verifying command remained in the gap set: %v", unchecked)
	}
}
