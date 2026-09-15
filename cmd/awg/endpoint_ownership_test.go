// SPDX-License-Identifier: AGPL-3.0-only

package main

// ENDPOINT OWNERSHIP — one bounded family, measured by the property-derived census.
//
// Baseline before this family (admitted main aedd007f):
//
//	20 subjects | 3 owner-resolved | 2 generation-verified | GAP 18
//
// Seventeen subjects declared `fs.String("addr", defaultServiceAddr(), …)` and dialled it. That is
// not a missing check; it is each command CHOOSING A GRAPH. The owner's own contract says why the
// default had to go too: "a non-empty value is always an operator naming it rather than a port the
// command chose for them", so a command defaulting the flag to a real address made every ordinary
// run indistinguishable from an explicit override and bypassed canonical resolution entirely.
//
// This family repairs endpoint ownership ONLY. Served-generation verification is deliberately
// untouched and the census still reports GAP 18 after it — that is the scoped, honest result.

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

// W1. NO PRODUCTION GRAPH-READING COMMAND DECLARES ITS OWN GRAPH ENDPOINT.
//
// Derived from the source, not listed: every `run*` function's own `addr` flag must default to the
// empty string, because that is the single input the owner uses to tell "the operator named it"
// from "resolve canonically". A non-empty default silently converts every run into an override.
func TestNoProductionGraphCommandDeclaresADefaultEndpoint(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		n := fi.Name()
		return strings.HasPrefix(n, "cmd_") && strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	var offenders []string
	seen := 0
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			base := filepath.Base(name)
			// `serve` is the named exception: its --addr is what it LISTENS on, not a graph it reads.
			if base == "cmd_serve.go" {
				continue
			}
			for _, d := range file.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "run") {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "String" || len(call.Args) < 2 {
						return true
					}
					lit, ok := call.Args[0].(*ast.BasicLit)
					if !ok || lit.Value != `"addr"` {
						return true
					}
					seen++
					// The default must be the empty literal.
					if def, ok := call.Args[1].(*ast.BasicLit); !ok || def.Value != `""` {
						offenders = append(offenders, base+":"+fn.Name.Name)
					}
					return true
				})
			}
		}
	}
	// ANTI-VACUITY: finding no addr flags at all would pass this trivially.
	if seen == 0 {
		t.Fatal("no production command declares an addr flag at all; this check has lost its subject")
	}
	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Errorf("%d production graph-reading command(s) declare their own graph endpoint default:\n  %s\n\n"+
			"The owner reads a non-empty --addr as an operator naming the endpoint, so a real default "+
			"makes every ordinary run an override and canonical resolution never happens.",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

// W2. THE RESOLVED ADDRESS IS NOT DISCARDED. Routing through the owner and then dialling the raw
// flag would satisfy the census while changing nothing, so every subject must assign the owner's
// answer back over the value its later lines use.
func TestEveryMigratedSubjectAdoptsTheOwnersAddress(t *testing.T) {
	files, err := filepath.Glob("cmd_*.go")
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	migrated := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		n := strings.Count(src, "productionReaderFor(")
		if n == 0 {
			continue
		}
		migrated += n
		if got := strings.Count(src, "*addr = reader.Addr"); got != n {
			missing = append(missing, f)
		}
	}
	if migrated == 0 {
		t.Fatal("no subject resolves through productionReaderFor; this check has lost its subject")
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("file(s) resolve through the owner and do not adopt its address: %s",
			strings.Join(missing, ", "))
	}
}

// W3. CANONICAL SELECTION, EXPLICIT OVERRIDE, AND NO FALLBACK — the owner's three contracted
// behaviours, driven rather than read. These are the semantics the migration must not change.
func TestTheOwnerCanonicalOverrideAndNoFallbackStillHold(t *testing.T) {
	root, registry := readerFixture(t, "localhost:19001", "")
	t.Chdir(root)

	// (1) canonical: the project's configuration decides, and it is not reported as an override.
	canonical := productionReaderFor(emptyFlags(), "", "", "")
	if canonical.Addr != "localhost:19001" {
		t.Errorf("canonical resolution gave %q, want the project's configured endpoint", canonical.Addr)
	}
	if canonical.Overridden {
		t.Error("canonical resolution reported itself as an operator override")
	}

	// (2) explicit override wins and is recorded as non-canonical.
	fs := emptyFlags()
	fs.String("addr", "", "")
	if err := fs.Parse([]string{"-addr", "localhost:19999"}); err != nil {
		t.Fatal(err)
	}
	out := captureStderr(t, func() {
		over := productionReaderFor(fs, "", "", "localhost:19999")
		if over.Addr != "localhost:19999" {
			t.Errorf("override gave %q, want the named endpoint", over.Addr)
		}
		if !over.Overridden {
			t.Error("an operator-named endpoint was not recorded as an override")
		}
	})
	if !strings.Contains(out, "non-canonical") {
		t.Errorf("the override was not announced as non-canonical:\n%s", out)
	}

	// (3) NO FALLBACK. With no configuration the owner names the built-in default and says so; it
	// never probes a second endpoint, because selecting a graph by liveness is the failure the
	// whole front exists to prevent.
	bare := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bare, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bare, ".sensei", "config.yaml"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(bare)
	fallback := productionReaderFor(emptyFlags(), "", "", "")
	if fallback.Source != "the built-in default" {
		t.Errorf("with no configured endpoint the source is %q, want it named as the built-in default",
			fallback.Source)
	}
	if fallback.Overridden {
		t.Error("the built-in default was reported as an operator override")
	}
	_ = registry
}

// W4. THE PROJECT ROOT IS RESOLVED BY WALKING UP, never taken from a target-repo flag. briefing
// paid for this: passing --repo as the projectRoot made the same command resolve a different
// endpoint from a subdirectory, because ./.sensei/config.yaml was not found there.
func TestEndpointResolutionIsUnaffectedByTheWorkingSubdirectory(t *testing.T) {
	root, _ := readerFixture(t, "localhost:19002", "")
	deep := filepath.Join(root, "golang", "server", "embeddata")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Chdir(root)
	fromRoot := productionReaderFor(emptyFlags(), "", "", "")
	t.Chdir(deep)
	fromDeep := productionReaderFor(emptyFlags(), "", "", "")

	if fromRoot.Addr != fromDeep.Addr {
		t.Errorf("the same command resolved %q from the root and %q from a subdirectory",
			fromRoot.Addr, fromDeep.Addr)
	}
	if fromDeep.Addr != "localhost:19002" {
		t.Errorf("from a subdirectory the endpoint is %q, want the project's configured one", fromDeep.Addr)
	}
}

// W5. THE FAMILY'S COMPLETION ORACLE, and it ASSERTS.
//
// Found by mutation: bypassing the owner in one subject survived every witness above, because the
// census REPORT is deliberately non-failing on counts. A measurement that never fails cannot also
// be a completion condition, so this is the assertion — and it is scoped to endpoint ownership
// only. Generation verification is a separate family and its gaps must not fail here.
func TestEveryDiscoveredSubjectResolvesThroughTheOwner(t *testing.T) {
	subjects := graphCommandsIn(t, ".")
	// ANTI-VACUITY: zero discovered subjects is failure, not a clean tree.
	const floor = 15
	if len(subjects) < floor {
		t.Fatalf("the census discovered %d graph-reading command(s); %d+ are known, so it has stopped "+
			"enumerating and this oracle would pass by finding nothing", len(subjects), floor)
	}
	_, _, noOwner := classifyGraphCommands(subjects)
	if len(noOwner) != 0 {
		t.Errorf("%d of %d production graph-reading command(s) still select a graph endpoint without "+
			"the owner:\n  %s\n\nEach must obtain its reader through productionReaderFor / "+
			"resolveGraphReader; a command that chooses its own address chooses a graph.",
			len(noOwner), len(subjects), strings.Join(noOwner, "\n  "))
	}
}

// W6. ORDERING: reader resolution must follow the context it depends on.
//
// Four subjects settle their governed domain AFTER flag parsing — contract-bootstrap from its task
// file, edit-brief from the repository configuration, preflight and verify-obligations through
// resolveRepositoryDomain. A reader resolved before that carries the endpoint and declared
// generation of a different domain (usually none) and looks resolved while answering for the wrong
// one.
//
// Found by mutation: removing contract-bootstrap's task-file domain fallback survived every other
// witness. Asserted POSITIONALLY on the source, because the property is an ordering and the
// defect is textual placement.
func TestReaderResolutionFollowsTheContextItDependsOn(t *testing.T) {
	cases := map[string]struct{ file, context string }{
		"contract-bootstrap takes its domain from the task file": {
			"cmd_contract_bootstrap.go", "*domain = strings.TrimSpace(task.Domain)"},
		"edit-brief falls back to the repository's configured domain": {
			"cmd_edit_brief.go", "resolveRepositoryDomain(projectRoot, \"\").Domain"},
		"preflight resolves the repository domain": {
			"cmd_preflight.go", "resolvedDomain := resolveRepositoryDomain(*repo, *domain)"},
		"verify-obligations resolves the repository domain": {
			"cmd_verify_obligations.go", "resolvedDomain := resolveRepositoryDomain(*repo, *domain)"},
	}
	for name, c := range cases {
		b, err := os.ReadFile(c.file)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		src := string(b)
		ctx := strings.Index(src, c.context)
		if ctx < 0 {
			t.Errorf("%s: the context this ordering depends on is gone (%q); the witness has lost "+
				"its anchor and the ordering it protects is now unchecked", name, c.context)
			continue
		}
		res := strings.Index(src, "productionReaderFor(")
		if res < 0 {
			t.Errorf("%s: %s no longer resolves through the owner", name, c.file)
			continue
		}
		if res < ctx {
			t.Errorf("%s: the reader is resolved at offset %d, BEFORE the domain is settled at %d; "+
				"it would carry another domain's endpoint and declared generation", name, res, ctx)
		}
	}
}

// W7. OUT-OF-TREE EXECUTION. A command that names a target checkout must resolve THAT project's
// endpoint, from anywhere.
//
// Raised as a P1 by blind review of the first version of this family: the helper hardcoded the
// working directory, so `sensei preflight --repo /path/to/target` run from /tmp found no
// configuration and fell through to the built-in default — dialling the wrong graph while looking
// correctly resolved. The rule is not "use CWD" but "walk up from the hint when there is one".
func TestATargetCheckoutResolvesItsOwnEndpointFromOutsideTheTree(t *testing.T) {
	target, _ := readerFixture(t, "localhost:19010", "")
	elsewhere := t.TempDir() // no .sensei here at all
	t.Chdir(elsewhere)

	r := productionReaderFor(emptyFlags(), target, "", "")
	if r.Addr != "localhost:19010" {
		t.Fatalf("from outside the tree the endpoint is %q, want the target's configured %q; a "+
			"command naming --repo must reach that project's configuration", r.Addr, "localhost:19010")
	}
	if strings.Contains(r.Source, "built-in") {
		t.Errorf("resolution fell through to the built-in default: %q", r.Source)
	}

	// And a SUBDIRECTORY of the target still resolves the same endpoint -- the half briefing paid
	// for. `--repo .`-shaped hints must be walked up, not used verbatim.
	deep := filepath.Join(target, "golang", "server")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(deep)
	fromDeepHint := productionReaderFor(emptyFlags(), ".", "", "")
	if fromDeepHint.Addr != "localhost:19010" {
		t.Errorf("with hint \".\" from a subdirectory the endpoint is %q, want %q; the hint was used "+
			"verbatim instead of being walked up", fromDeepHint.Addr, "localhost:19010")
	}
}

// W8. EVERY SUBJECT WITH A TARGET-REPO FLAG PASSES IT. Derived from the source: a command that
// names a checkout and then resolves its endpoint from the working directory is the defect above,
// reintroduced one command at a time.
func TestSubjectsWithATargetCheckoutFlagPassItToTheOwner(t *testing.T) {
	files, err := filepath.Glob("cmd_*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked, offenders := 0, []string{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		if !strings.Contains(src, "productionReaderFor(") {
			continue
		}
		// Which target-checkout flag does this file declare, if any?
		var hint string
		for _, cand := range []string{`"repo-root"`, `"repo"`, `"root"`} {
			if i := strings.Index(src, "fs.String("+cand); i >= 0 {
				// the variable it was assigned to
				line := src[strings.LastIndex(src[:i], "\n")+1 : i]
				if v := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ":=")); v != "" {
					hint = "*" + v
				}
				break
			}
		}
		if hint == "" {
			continue // no target-checkout flag: the working directory is the only answer
		}
		checked++
		if !strings.Contains(src, "productionReaderFor(fs, "+hint+",") {
			offenders = append(offenders, f+" (declares "+hint+")")
		}
	}
	if checked == 0 {
		t.Fatal("no migrated subject declares a target-checkout flag; this check has lost its subject")
	}
	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Errorf("%d subject(s) name a target checkout and do not pass it to the owner:\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}
