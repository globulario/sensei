// SPDX-License-Identifier: AGPL-3.0-only

package main

// LAW 3: "All production readers resolve graph identity through one owner. They must not
// independently choose an Oxigraph port."
//
// It was false. Measured 2026-09-13: EIGHTEEN commands in this package declare
// `fs.String("addr", defaultServiceAddr(), …)` and dial it, and exactly one — `metadata`
// — resolved through the G2 owner. `briefing` is the sharpest case because it ALREADY
// resolves the governed domain (`resolveRepositoryDomain`) and then ignores it, dialing
// the netcfg default instead.
//
// Proven live during the migration: `sensei briefing --file docs/awareness/invariants.yaml`
// failed with "dial tcp 127.0.0.1:10120: connect: connection refused" in a repository
// whose own config states :10122. The graph it should read was running the whole time.
//
// The repair is not a different default port. It is that a production reader states the
// domain it is reading for and receives the endpoint AND the generation identity from the
// one owner — so the port stops being something a reader picks.

import (
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func readerFixture(t *testing.T, configuredAddr, activeGeneration string) (root, registry string) {
	t.Helper()
	root = projectRoot(t, t.TempDir())
	if configuredAddr != "" {
		writeProjectConfig(t, root, configuredAddr)
	}
	gen := ""
	if activeGeneration != "" {
		gen = "\n        active_generation: " + activeGeneration
	}
	registry = writeRegistryFixture(t, `domains:
    example.com/acme/thing:
        repository_identity: acme/thing
        allowed_corpus_roots:
            - docs/awareness`+gen+`
`)
	return root, registry
}

// A. The domain resolves to service A while the netcfg default (:10120) would be B.
// The reader must use A.
func TestAProductionReaderUsesTheDomainsEndpointNotTheDefaultPort(t *testing.T) {
	root, registry := readerFixture(t, "localhost:10122", "")
	r := resolveGraphReader(emptyFlags(), root, "example.com/acme/thing", "", registry)
	if r.Addr != "localhost:10122" {
		t.Fatalf("reader addr = %q, want the domain's configured endpoint, not %q", r.Addr, defaultServiceAddr())
	}
	if strings.Contains(r.Source, "built-in") {
		t.Errorf("the reader reports the built-in default as its source: %q", r.Source)
	}
}

// B. :10120 may be perfectly healthy and belong to another graph. Reachability is not
// identity, so nothing may select it merely because it answers.
func TestReachabilityIsNotIdentity(t *testing.T) {
	root, registry := readerFixture(t, "localhost:10122", "")
	r := resolveGraphReader(emptyFlags(), root, "example.com/acme/thing", "", registry)
	if r.Addr == defaultServiceAddr() {
		t.Fatalf("the reader selected the built-in default %q while the domain states another endpoint", r.Addr)
	}
	// And the resolution must be decided before any dial: a resolver that needed to
	// probe endpoints would be choosing by reachability.
	if r.Addr == "" {
		t.Fatal("the reader resolved no endpoint at all")
	}
}

// C. When the resolved endpoint is unavailable the reader fails closed. There is no
// second port to try, because trying one would be choosing a graph by liveness.
func TestThereIsNoFallbackEndpoint(t *testing.T) {
	root, registry := readerFixture(t, "localhost:19999", "")
	r := resolveGraphReader(emptyFlags(), root, "example.com/acme/thing", "", registry)
	if r.Addr != "localhost:19999" {
		t.Fatalf("addr = %q, want the configured (unreachable) endpoint", r.Addr)
	}
	// The structural half: no reader may name a second endpoint to fall back to.
	if n := strings.Count(briefingSource(t), "defaultServiceAddr()"); n != 0 {
		t.Errorf("briefing still names the built-in default %d time(s); a fallback port is a graph chosen by liveness", n)
	}
}

// D. The endpoint answers, but with a generation other than the declared ACTIVE one.
// The reader must refuse rather than report findings from a graph the domain does not
// declare.
func TestAReaderRefusesAResponseFromAnUndeclaredGeneration(t *testing.T) {
	root, registry := readerFixture(t, "localhost:10122", "c0b660fcaaaa")
	r := resolveGraphReader(emptyFlags(), root, "example.com/acme/thing", "", registry)
	if r.DeclaredGeneration != "c0b660fcaaaa" {
		t.Fatalf("declared generation = %q", r.DeclaredGeneration)
	}
	if err := r.verifyServed("c0b660fcaaaa"); err != nil {
		t.Errorf("the declared generation answered and was refused: %v", err)
	}
	err := r.verifyServed("999999999999")
	if err == nil {
		t.Fatal("a response from an undeclared generation was accepted")
	}
	for _, want := range []string{"c0b660fcaaaa", "999999999999"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal omits %q: %v", want, err)
		}
	}
	// Inert where nothing is declared — adding this must not refuse today's callers.
	rootB, registryB := readerFixture(t, "localhost:10122", "")
	rb := resolveGraphReader(emptyFlags(), rootB, "example.com/acme/thing", "", registryB)
	if err := rb.verifyServed("anything"); err != nil {
		t.Errorf("an undeclared generation refused a reader: %v", err)
	}
}

// E. A test/dev override stays possible and must announce itself. An override that
// looked like canonical resolution would be the port choice returning through a flag.
func TestAnExplicitOverrideIsPossibleAndVisiblyNonCanonical(t *testing.T) {
	root, registry := readerFixture(t, "localhost:10122", "")
	r := resolveGraphReader(emptyFlags(), root, "example.com/acme/thing", "localhost:19191", registry)
	if r.Addr != "localhost:19191" {
		t.Fatalf("an explicit override was ignored: %q", r.Addr)
	}
	if !strings.Contains(strings.ToLower(r.Source), "command line") {
		t.Errorf("the override does not announce itself as operator-named: %q", r.Source)
	}
	if !r.Overridden {
		t.Error("the reader does not record that its endpoint was overridden, so nothing downstream can report it as non-canonical")
	}
	// Canonical resolution must not claim to be an override, and vice versa.
	canonical := resolveGraphReader(emptyFlags(), root, "example.com/acme/thing", "", registry)
	if canonical.Overridden {
		t.Error("a canonically resolved reader claims to be overridden")
	}
}

// F. metadata and briefing must resolve the same endpoint and identity for one domain.
// Asserted by construction: both call the same owner, and the test proves there is only
// one resolver reachable from the reader paths.
func TestMetadataAndBriefingResolveThroughTheSameOwner(t *testing.T) {
	root, registry := readerFixture(t, "localhost:10122", "c0b660fcaaaa")
	a := resolveGraphReader(emptyFlags(), root, "example.com/acme/thing", "", registry)
	b := resolveGraphReader(emptyFlags(), root, "example.com/acme/thing", "", registry)
	if a.Addr != b.Addr || a.DeclaredGeneration != b.DeclaredGeneration {
		t.Fatalf("the owner is not deterministic: %+v vs %+v", a, b)
	}
	for _, f := range []string{"cmd_briefing.go", "cmd_metadata.go"} {
		src := readCmdSource(t, f)
		if !strings.Contains(src, "resolveGraphReader(") {
			t.Errorf("%s does not resolve through the shared owner", f)
		}
		if strings.Contains(src, "defaultServiceAddr()") {
			t.Errorf("%s still names the built-in default endpoint", f)
		}
	}
}

func briefingSource(t *testing.T) string { return readCmdSource(t, "cmd_briefing.go") }

func emptyFlags() *flag.FlagSet { return flag.NewFlagSet("test", flag.ContinueOnError) }

func readCmdSource(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// writeProjectConfig writes the .sensei/config.yaml a project uses to state its endpoint.
func writeProjectConfig(t *testing.T, root, addr string) {
	t.Helper()
	body := "store:\n    query_url: http://localhost:7899/query\nserver:\n    addr: " + addr + "\n"
	if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The generation check must be WIRED, and wired before the briefing is rendered: a
// verdict from an undeclared graph must never be read as findings about this domain.
//
// Source-anchored and labelled as such — driving a full briefing needs a live awareness
// service. Witness D above proves the predicate; this proves the call exists, runs before
// the render, and is given the response's OWN authority digest rather than some other
// value that would always agree.
func TestBriefingVerifiesTheServedGenerationBeforeRendering(t *testing.T) {
	src := briefingSource(t)
	call := strings.Index(src, "reader.verifyServedAuthority(")
	if call < 0 {
		t.Fatal("briefing never verifies that the graph which answered is the declared ACTIVE generation")
	}
	// The anchor must be the FIRST rendering after the RPC, and finding it takes two
	// steps because neither naive choice works:
	//
	//   - `if *asJSON {` appears twice; the `--task active` path returns early with its
	//     own copy, and indexing the first occurrence compared the check against a
	//     branch it never reaches (this test failed that way);
	//   - `printGraphAuthority` is unique but is NOT the first render: the `--json`
	//     branch returns before it, so a check placed between them leaves `--json`
	//     rendering an unverified graph — and a mutation doing exactly that survived
	//     until this was fixed.
	//
	// So: locate the RPC, then the first render after it.
	rpc := strings.Index(src, "client.Briefing(ctx,")
	if rpc < 0 {
		t.Fatal("the briefing RPC anchor is gone; this check has lost its anchor")
	}
	rel := strings.Index(src[rpc:], "if *asJSON {")
	if rel < 0 {
		t.Fatal("no rendering follows the briefing RPC; this check has lost its anchor")
	}
	render := rpc + rel
	if call < rpc {
		t.Errorf("the generation is verified before the response exists: call=%d rpc=%d", call, rpc)
	}
	if call > render {
		t.Errorf("the generation is verified AFTER the briefing is rendered: call=%d render=%d", call, render)
	}
	line := src[call:]
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	// The check must be given the RESPONSE'S OWN authority, not some other value that
	// would always agree. WHICH FIELD of that authority carries the generation now belongs
	// to the owner (graphReader.verifyServedAuthority) rather than to this call site, and
	// is pinned there by TestTheOwnerComparesTheServedGenerationNotTheRuleSnapshotRevision
	// -- so the two halves of the old single-line assertion are both still proven.
	if !strings.Contains(line, "resp.GetAuthority()") {
		t.Errorf("the check is not given the authority the response itself carried: %s", strings.TrimSpace(line))
	}
	// And a refusal must end the command rather than warn.
	tail := src[call:]
	if end := strings.Index(tail, "\n\t}"); end > 0 {
		tail = tail[:end]
	}
	if !strings.Contains(tail, "return 1") {
		t.Errorf("a refused generation does not end the briefing:\n%s", tail)
	}
}

// THE INVARIANT, enforced once for the whole package rather than by sixteen weak
// per-command checks:
//
//	For a governed repository/domain there is exactly ONE code path by which a
//	production command chooses which graph to read, and that path is the G2 owner.
//
// A command carrying `defaultServiceAddr()` as its --addr default owns a graph choice,
// whatever it does afterwards. So the mechanical form of the invariant is: no command
// declares a default endpoint. Only the owner may name the built-in tier.
//
// This is the check that makes the migration finishable and keeps it finished: a new
// command added next year with a default port fails here, which is the only way a
// sixteen-file migration does not silently regrow.
func TestNoProductionCommandDeclaresADefaultGraphEndpoint(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var offenders []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "cmd_") || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src := readCmdSource(t, name)
		if strings.Contains(src, `fs.String("addr", defaultServiceAddr()`) {
			offenders = append(offenders, name)
		}
	}
	if len(offenders) > 0 {
		t.Errorf("%d command(s) still declare a default graph endpoint, so each still owns a graph choice: %s\n"+
			"Resolve through productionReader/resolveGraphReader and declare the flag with an empty default.",
			len(offenders), strings.Join(offenders, ", "))
	}
}

// The owner is the only place the built-in tier may be named, and it must still name it:
// removing the final tier would make an unresolvable domain produce an empty address
// rather than a truthful default.
func TestOnlyTheOwnerNamesTheBuiltInDefault(t *testing.T) {
	if !strings.Contains(readCmdSource(t, "endpoint_binding.go"), "defaultServiceAddr()") {
		t.Error("the resolution owner no longer names the built-in default; an unresolvable domain would yield an empty endpoint")
	}
}

// Every command that reaches the graph must resolve through the owner. Counted rather
// than sampled, so a command that dials without resolving is named.
// THE CENSUS MUST ENUMERATE THE UNIT ITS NAME CLAIMS.
//
// This enumerated FILES and asked whether the file contained "productionReaderFor(". Its name
// says every graph-reaching COMMAND. cmd_repair_report.go holds two: runRepairReport was
// migrated and runRepairGate was not, and one call certified the whole file -- so a command
// that chose its own endpoint passed a census written to make that impossible, and the PR
// claiming "no production command chooses a graph port" shipped with one that did. Found by an
// independent reviewer, not by this test.
//
// It now enumerates FUNCTIONS. A file with runA migrated and runB dialing directly fails.
func TestEveryGraphReachingCommandResolvesThroughTheOwner(t *testing.T) {
	// A FLOOR ON THE SUBJECTS, because a census that enumerates nothing passes. Zero coverage
	// and zero defects are indistinguishable from the result alone, and this test is the
	// evidence the reader migration is complete -- so it must fail if it stops looking.
	subjects := graphCommandsIn(t, ".")
	const floor = 10
	if len(subjects) < floor {
		t.Fatalf("the census found only %d graph-reaching command(s); it covered %d+ before, so it has stopped enumerating rather than found nothing to report",
			len(subjects), floor)
	}
	// AND THE UNIT IS THE FUNCTION, asserted on the real tree rather than only on synthetic
	// fixtures. cmd_repair_report.go is the file that carried the defect: it defines two
	// graph-reaching commands, and both must appear as separate subjects. A census that
	// enumerated files would list one key here, which is exactly how a bypassing command hid
	// behind a migrated sibling.
	for _, want := range []string{"cmd_repair_report.go:runRepairReport", "cmd_repair_report.go:runRepairGate"} {
		if _, ok := subjects[want]; !ok {
			t.Errorf("%q is not a census subject; the census is not enumerating commands (subjects: %d)",
				want, len(subjects))
		}
	}

	unresolved := unmigratedGraphCommands(t, ".")
	if len(unresolved) > 0 {
		t.Errorf("%d graph-reaching command(s) choose an endpoint without the G2 owner: %s",
			len(unresolved), strings.Join(unresolved, ", "))
	}
}

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
// Parsed rather than regexed over the whole file, because the bug being prevented is precisely
// one function's text being read as another's.
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
}

// graphAuthorityNames are the two vocabularies the census reads, both by MEMBERSHIP of a
// recognised set rather than by excluding known-bad names: an unrecognised call is not a
// verification, so a renamed or invented helper shows up as a gap instead of silently
// certifying one.
var (
	ownerResolutionNames = map[string]bool{
		"productionReaderFor":           true,
		"productionReaderForRepository": true,
		"resolveGraphReader":            true,
	}
	generationCheckNames = map[string]bool{
		// The owner's comparison, and the digest-level one underneath it.
		"verifyServedAuthority": true,
		"verifyServed":          true,
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
					if generationCheckNames[n] {
						facts.VerifiesGeneration = true
					}
				}
				if !facts.ResolvesOwner {
					facts.ResolvesOwner = reaches(graph, fn.Name.Name, ownerResolutionNames)
				}
				if !facts.VerifiesGeneration {
					facts.VerifiesGeneration = reaches(graph, fn.Name.Name, generationCheckNames)
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
// Two corrections forced by an independent review of this head, and both were defects in this
// census rather than in the readers:
//
//  1. It listed cmd_edit_check.go and cmd_edit_guard.go as consuming "only EditCheck, which
//     states no generation". THIS PR ADDED A GraphAuthority TO EditCheckResponse. The exemption
//     rested on a premise the same commit falsified, so two commands were excused from
//     generation verification on the strength of a stale sentence.
//  2. It counted FILES and asserted a total of 18, while the migration census next door had
//     already moved to commands. cmd_metadata.go holds runMetadata AND runDomains;
//     cmd_repair_report.go holds runRepairReport AND runRepairGate. File scope hid four
//     subjects and let a verifying command certify a sibling that does not verify.
//
// So the subject set is DERIVED, per command, and the groups are computed rather than listed.
// A list is a claim about the world that silently stops being true.
//
// THE TWO QUESTIONS STAY SEPARATE, and both are now closed. Endpoint ownership -- who chooses
// the graph instance -- and served-generation authority -- whether the graph answering is the
// one this domain declares ACTIVE. They are still two facts, not one boolean, because
// runRepairGate once closed the first and explicitly not the second.
//
// This is the served-generation authority family's COMPLETION CONDITION, and it is an
// assertion rather than a log line. What it enforces:
//
//	production graph-reading subjects == subjects whose authoritative consumption is
//	                                    generation-verified
//
// A new production graph-reading command that consumes graph authority without comparing it
// FAILS HERE, without anyone editing a list. That is the property the whole family was for:
// the gap was not thirteen bugs, it was one invariant with no instrument behind it.
//
// The set is NOT exempt-able. There is no allowlist, because the way this gap opened was a
// sentence claiming two commands consumed no authority -- true when written, falsified by the
// commit that added GraphAuthority to EditCheckResponse, and never re-checked. A subject whose
// result genuinely may not be trusted must say so in code the census can read, not in prose.
func TestTheReaderCensusStatesWhyEachReaderDoesOrDoesNotVerify(t *testing.T) {
	subjects := graphCommandsIn(t, ".")

	// ANTI-VACUITY. A census that discovers nothing passes, and zero coverage is
	// indistinguishable from zero defects.
	const floor = 15
	if len(subjects) < floor {
		t.Fatalf("the reader census discovered %d graph-reading command(s); %d+ are known, so it has stopped enumerating",
			len(subjects), floor)
	}

	verifies, holdsAuthorityUnchecked, noOwner := classifyGraphCommands(subjects)

	// ENDPOINT OWNERSHIP IS CLOSED. Every discovered command resolves through the owner.
	if len(noOwner) != 0 {
		t.Errorf("%d command(s) still choose an endpoint without the G2 owner: %s",
			len(noOwner), strings.Join(noOwner, ", "))
	}

	// SERVED-GENERATION AUTHORITY IS TOO. Every discovered command reaches the owner's
	// comparison. Membership is named in the failure, so a regression says WHICH subject
	// rather than only that the count moved -- and no number is remembered anywhere.
	if len(holdsAuthorityUnchecked) != 0 {
		t.Errorf("%d of %d production graph-reading commands receive a graph authority and never "+
			"compare it to the domain's ACTIVE generation:\n  %s\n\nEvery one of these consumes "+
			"graph-derived information as authoritative. Either compare the authority the response "+
			"carried (reader.verifyServedAuthority) or, if the result genuinely may never be "+
			"consumed as authoritative, say so in code this census can read.",
			len(holdsAuthorityUnchecked), len(subjects), strings.Join(holdsAuthorityUnchecked, "\n  "))
	}

	// REAL-TREE ANCHORS, evidence that the derived census REACHES known readers -- not the
	// source of truth for who they are. Both files hold two commands each, which is what file
	// scope could not see.
	for _, want := range []string{
		"cmd_briefing.go:runBriefing",
		"cmd_repair_report.go:runRepairReport", "cmd_repair_report.go:runRepairGate",
		"cmd_metadata.go:runMetadata", "cmd_metadata.go:runDomains",
		// Excused for a time as "consuming only EditCheck, which states no generation" -- a
		// premise the commit that added GraphAuthority to EditCheckResponse falsified. Named
		// individually so the exemption cannot return by being forgotten.
		"cmd_edit_check.go:runEditCheck", "cmd_edit_guard.go:runEditGuard",
	} {
		if _, ok := subjects[want]; !ok {
			t.Errorf("the derived census does not reach %q (discovered %d)", want, len(subjects))
		}
	}
	// And the groups must partition the subjects: no command counted twice or lost.
	if len(verifies)+len(holdsAuthorityUnchecked) != len(subjects) {
		t.Errorf("the groups do not partition the census: %d + %d != %d",
			len(verifies), len(holdsAuthorityUnchecked), len(subjects))
	}
}

// The override notice must actually be EMITTED by the helper every command calls.
// Testing nonCanonicalReaderNotice in isolation proved the sentence can be composed; a
// mutant that deleted the emission survived exactly that gap, so this drives the real
// helper and captures what it writes.
func TestTheSharedHelperAnnouncesANonCanonicalOverride(t *testing.T) {
	root := projectRoot(t, t.TempDir())
	writeProjectConfig(t, root, "localhost:10122")
	t.Chdir(root)

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("addr", "", "")
	if err := fs.Parse([]string{"-addr", "localhost:19191"}); err != nil {
		t.Fatal(err)
	}

	out := captureStderr(t, func() {
		r := productionReaderFor(fs, "example.com/acme/thing", "localhost:19191")
		if r.Addr != "localhost:19191" {
			t.Errorf("the override was not honoured: %q", r.Addr)
		}
	})
	if !strings.Contains(out, "non-canonical override") {
		t.Errorf("the helper did not announce the override:\n%s", out)
	}
	if !strings.Contains(out, "localhost:19191") {
		t.Errorf("the announcement does not name the endpoint it is reading:\n%s", out)
	}

	// And canonical resolution stays silent — a notice printed every run is a notice
	// nobody reads.
	quiet := captureStderr(t, func() {
		fs2 := flag.NewFlagSet("test2", flag.ContinueOnError)
		fs2.String("addr", "", "")
		_ = fs2.Parse(nil)
		productionReaderFor(fs2, "example.com/acme/thing", "")
	})
	if strings.Contains(quiet, "non-canonical") {
		t.Errorf("canonical resolution announced itself as an override:\n%s", quiet)
	}
}

// The census is now evidence, so it needs its own witnesses: a detector nobody has driven
// against a known-bad input is an assumption. These run the enumerator over synthetic
// command files, which is the only way to prove it FAILS when it should.
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
