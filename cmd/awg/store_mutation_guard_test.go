// SPDX-License-Identifier: AGPL-3.0-only

package main

// RE-REVIEW FINDING (P1, cmd_build.go:157): "Enforce ownership in every store publication
// command." The reviewer named rebuild and governance activate. The census found two more --
// promote and audit -- so patching the named pair would have left the family half-guarded.
//
// DOMAIN OF THESE CLAIMS: every path in cmd/awg that replaces a store's contents via the two
// PUT primitives. It does not cover a store mutated by something outside this binary, which
// no check here can see.

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func ownedStoreRegistry(t *testing.T, owner, store string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"),
		[]byte("domains:\n  "+owner+":\n    repository_identity: acme/owned\n    store_url: "+store+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 1. A publication claiming NO domain must not replace a store another domain owns. This is
// the case the four ungated commands presented, and the damage lands on a domain nobody in
// the command mentioned.
func TestAnUnclaimedPublicationCannotReplaceAnotherDomainsStore(t *testing.T) {
	ownedStoreRegistry(t, "example.com/acme/owned", "http://127.0.0.1:7881/store?default")
	err := guardStoreMutation("http://127.0.0.1:7881/store?default", storeMutationIntent{Reason: "sensei rebuild"})
	if err == nil {
		t.Fatal("a publication claiming no domain was allowed to replace a governed store")
	}
	if !strings.Contains(err.Error(), "example.com/acme/owned") {
		t.Errorf("the refusal does not name the domain whose store would be lost: %v", err)
	}
	if !strings.Contains(err.Error(), "sensei rebuild") {
		t.Errorf("the refusal does not name the publication it stopped: %v", err)
	}
}

// 2. An operator naming the endpoint does NOT make it acceptable. Rule 1 outranks the
// override, because the override expresses intent about this caller's own store.
func TestNamingTheEndpointDoesNotPermitOverwritingAnotherDomainsStore(t *testing.T) {
	ownedStoreRegistry(t, "example.com/acme/owned", "http://127.0.0.1:7881/store?default")
	if err := guardStoreMutation("http://127.0.0.1:7881/store?default",
		storeMutationIntent{Overridden: true, Reason: "sensei governance activate"}); err == nil {
		t.Fatal("an explicit --store-url overrode another domain's ownership")
	}
}

// 3. AN EQUIVALENT SPELLING of the owned store is the same store. This ties finding
// store_ownership.go:51 to this seam: without canonicalization the guard is bypassed by
// writing the URL differently.
func TestAnEquivalentSpellingOfAnOwnedStoreIsStillRefused(t *testing.T) {
	ownedStoreRegistry(t, "example.com/acme/owned", "http://127.0.0.1:7881/store?default")
	for _, spelling := range []string{
		"http://127.0.0.1:7881",
		"http://127.0.0.1:7881/",
		"http://127.0.0.1:7881/store",
		"http://127.0.0.1:7881/query",
	} {
		if err := guardStoreMutation(spelling, storeMutationIntent{Reason: "sensei rebuild"}); err == nil {
			t.Errorf("spelling %q evaded the ownership guard for the store it names", spelling)
		}
	}
}

// 4. A store NO domain claims stays writable, so disposable stores and experiments work.
// Without this the guard could pass every test above by refusing everything.
func TestAStoreNoDomainClaimsRemainsWritable(t *testing.T) {
	ownedStoreRegistry(t, "example.com/acme/owned", "http://127.0.0.1:7881/store?default")
	if err := guardStoreMutation("http://127.0.0.1:7899/store?default", storeMutationIntent{Reason: "sensei rebuild"}); err != nil {
		t.Errorf("a disposable store nobody owns was refused: %v", err)
	}
}

// 5. The owning domain may publish into its own store.
func TestTheOwningDomainMayPublishIntoItsOwnStore(t *testing.T) {
	ownedStoreRegistry(t, "example.com/acme/owned", "http://127.0.0.1:7881/store?default")
	if err := guardStoreMutation("http://127.0.0.1:7881/store",
		storeMutationIntent{Domain: "example.com/acme/owned", Reason: "sensei build"}); err != nil {
		t.Errorf("the owner was refused its own store: %v", err)
	}
}

// 6. An UNREADABLE registry fails closed; an ABSENT one is inert. Two different facts.
func TestAnUnreadableRegistryStopsTheMutationAndAnAbsentOneDoesNot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Absent: nothing is governed, so nothing can be stranded.
	if err := guardStoreMutation("http://127.0.0.1:7881/store", storeMutationIntent{Reason: "x"}); err != nil {
		t.Errorf("an absent registry blocked a mutation: %v", err)
	}
	// Unreadable: whether another domain owns this store cannot be established.
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"),
		[]byte("domains:\n  - not: a mapping\n   bad indent: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := guardStoreMutation("http://127.0.0.1:7881/store", storeMutationIntent{Reason: "x"}); err == nil {
		t.Error("an unreadable registry was treated as an absent one, so ownership stopped being checked exactly when the registry was broken")
	}
}

// 7. THE CENSUS. Every call to a store-replacing primitive must pass an intent, which is
// what binds it to the guard. A future publisher that forgets cannot compile, and this test
// states the rule so the reason survives.
func TestEveryStoreReplacingCallStatesItsMutationIntent(t *testing.T) {
	// THE PRIMITIVE LIST IS DERIVED, NOT WRITTEN DOWN.
	//
	// It used to be a hardcoded pair, and putNamedGraph was missing from it -- so the census
	// enumerated two primitives while the commit claimed every publisher, and dropping a
	// primitive from the list silently shrank the census. Pattern C again: the unit a proof
	// quantifies over must be the unit it checks. A primitive is now DEFINED as a function that
	// calls guardStoreMutation, so the census cannot be narrowed by editing a list.
	prims := guardedStoreMutationPrimitives(t)
	if len(prims) < 3 {
		t.Errorf("only %d store-mutating primitive(s) reach the guard (%v); three are known, so the census has stopped enumerating",
			len(prims), prims)
	}
	for _, prim := range prims {
		for _, name := range productionSourcesNaming(t, prim) {
			src := readCmdSource(t, name)
			for _, call := range callsOf(src, prim) {
				if !strings.Contains(call, "storeMutationIntent{") {
					t.Errorf("%s calls %s without stating a storeMutationIntent, so it is not bound to the ownership guard:\n  %s",
						name, prim, strings.TrimSpace(call))
				}
			}
		}
	}
	// And the guard must be inside the primitives, not merely available to them.
	for _, f := range []string{"cmd_build.go", "cmd_rebuild.go"} {
		if !strings.Contains(readCmdSource(t, f), "guardStoreMutation(") {
			t.Errorf("%s no longer calls guardStoreMutation; the seam has moved out of the primitive", f)
		}
	}
}

// guardedStoreMutationPrimitives names every function that ACCEPTS a storeMutationIntent, as
// "<name>(" ready for call-site matching.
//
// Accepting the intent is the right definition, not calling the guard: a command may call the
// guard directly on its own behalf (import does), and its callers owe nothing. The functions
// whose CALLERS must state an intent are exactly those that take one. Derived so the census's
// subject list cannot be narrowed by editing a list.
func guardedStoreMutationPrimitives(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return strings.HasSuffix(fi.Name(), ".go") && !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var out []string
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, d := range file.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				takesIntent := false
				if fn.Type.Params != nil {
					for _, field := range fn.Type.Params.List {
						if id, ok := field.Type.(*ast.Ident); ok && id.Name == "storeMutationIntent" {
							takesIntent = true
						}
					}
				}
				// guardStoreMutation itself accepts one because it IS the seam. Its callers
				// forward an intent already stated by theirs, so it is not a subject.
				if takesIntent && fn.Name.Name != "guardStoreMutation" {
					out = append(out, fn.Name.Name+"(")
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// callsOf returns each single-line call site of prim in src.
func callsOf(src, prim string) []string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		if strings.Contains(line, prim) && !strings.Contains(line, "func "+strings.TrimSuffix(prim, "(")) {
			out = append(out, line)
		}
	}
	return out
}

// productionSourcesNaming lists the non-test cmd/awg files mentioning prim.
func productionSourcesNaming(t *testing.T, prim string) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		b, err := os.ReadFile(n)
		if err != nil {
			continue
		}
		if strings.Contains(string(b), prim) {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		t.Fatalf("no production file calls %s; this census has lost its anchor", prim)
	}
	return out
}

// ANTIGRAVITY FINDING (P1, store_mutation_guard.go:55): guardStoreMutation hardcoded
// DefaultDomainRegistryPath and ignored --domain-registry.
//
// This reintroduced, inside the new guard, the exact defect a review finding on this branch had
// already made me repair for activation: --domain-registry selects the registry every other
// check reads. A guard consulting a different file answers a question nobody asked, and can
// refuse a publication the selected registry permits -- or permit one it forbids.
func TestTheGuardReadsTheRegistryTheOperatorSelected(t *testing.T) {
	// The DEFAULT registry gives the store to prod. The SELECTED one gives it to alpha.
	ownedStoreRegistry(t, "example.com/acme/prod", "http://127.0.0.1:7881/store?default")
	selected := filepath.Join(t.TempDir(), "staging-domains.yaml")
	if err := os.WriteFile(selected, []byte("domains:\n  example.com/acme/alpha:\n"+
		"    repository_identity: acme/alpha\n    store_url: http://127.0.0.1:7881/store?default\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// alpha publishing into its own store, per the SELECTED registry, must be allowed.
	if err := guardStoreMutation("http://127.0.0.1:7881/store?default", storeMutationIntent{
		Domain: "example.com/acme/alpha", Reason: "sensei build", RegistryPath: selected}); err != nil {
		t.Errorf("the selected registry grants this store to alpha, but the guard refused: %v", err)
	}
	// And with no registry selected, the default still governs: prod owns it, so alpha refuses.
	if err := guardStoreMutation("http://127.0.0.1:7881/store?default", storeMutationIntent{
		Domain: "example.com/acme/alpha", Reason: "sensei build"}); err == nil {
		t.Error("with no registry selected the default must govern, and it gives this store to prod")
	}
	// An unreadable SELECTED registry fails closed and names the file it could not read.
	bad := filepath.Join(t.TempDir(), "broken.yaml")
	if err := os.WriteFile(bad, []byte("domains:\n  - not: a mapping\n   bad: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := guardStoreMutation("http://127.0.0.1:7881/store?default", storeMutationIntent{
		Domain: "example.com/acme/alpha", Reason: "sensei build", RegistryPath: bad})
	if err == nil {
		t.Fatal("an unreadable selected registry was treated as absent")
	}
	if !strings.Contains(err.Error(), bad) {
		t.Errorf("the refusal names the wrong registry: %v", err)
	}
}

// A NAMED-GRAPH PUT is a mutation of somebody's store, so it passes the same seam.
func TestANamedGraphPutCannotTargetAnotherDomainsStore(t *testing.T) {
	ownedStoreRegistry(t, "example.com/acme/owned", "http://127.0.0.1:7881/store?default")
	err := putNamedGraph(context.Background(), "http://127.0.0.1:7881/store?default",
		"http://example.com/graph/staging", []byte("<a> <b> <c> .\n"),
		storeMutationIntent{Reason: "test named-graph put"})
	if err == nil {
		t.Fatal("a named-graph PUT into another domain's store was allowed")
	}
	if !strings.Contains(err.Error(), "example.com/acme/owned") {
		t.Errorf("the refusal does not name the owning domain: %v", err)
	}
}

// WHICH REGISTRY BUILD CONSULTS, asserted through the real runBuild.
//
// SCOPE, stated because getting this wrong is the pattern this pass exists to correct: build
// has TWO ownership checks. This exercises the PRE-MUTATION one (cmd_build.go, beside the
// endpoint-agreement block), which reads buildRegistryPath(--domain-registry). It does NOT
// reach the primitive-level guard inside uploadNTriples: measured, the build refuses at
// admission -- "not inside a git repository" -- long before any upload, so reaching that guard
// needs a real corpus and a live store. The primitive's own honouring of intent.RegistryPath is
// proven directly against guardStoreMutation instead.
func TestBuildConsultsTheRegistryItWasGiven(t *testing.T) {
	// The DEFAULT registry is silent about this store; the SELECTED one gives it to a neighbour.
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"),
		[]byte("domains:\n  example.com/acme/alpha:\n    repository_identity: acme/alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(t.TempDir(), "selected-domains.yaml")
	if err := os.WriteFile(selected, []byte("domains:\n"+
		"  example.com/acme/alpha:\n    repository_identity: acme/alpha\n"+
		"  example.com/acme/neighbour:\n    repository_identity: acme/neighbour\n"+
		"    store_url: http://127.0.0.1:7881/store?default\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, so, se := captureStdoutStderr(t, func() int {
		return runBuild([]string{"--repo", "example.com/acme/alpha",
			"--domain-registry", selected,
			"--store-url", "http://127.0.0.1:7881/store?default"})
	})
	out := so + se
	// The SELECTED registry says a neighbour owns this store, so the build must refuse and say
	// whose it is. Reading the default instead would find no owner and proceed.
	if !strings.Contains(out, "example.com/acme/neighbour") {
		t.Errorf("build did not consult the registry it was given: the selected file gives this store to a neighbour\n%s", out)
	}
}

// THE DIRECTION THAT ACTUALLY BREAKS. Reading the wrong registry produces a FALSE REFUSAL, not
// a false permit: a legitimate publication is stopped because a stale default names another
// owner. Here the SELECTED registry grants the store to alpha while the DEFAULT gives it to
// prod, and the build must not refuse on ownership grounds.
//
// Same scope caveat as above: this covers build's PRE-MUTATION check. It does not reach the
// primitive guard, so a mutant dropping RegistryPath from build's intent survives both of these
// -- recorded as equivalent-behind-the-earlier-check rather than as a passing test.
func TestBuildIsNotRefusedByAStaleDefaultRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The stale default: prod owns the store.
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"),
		[]byte("domains:\n  example.com/acme/prod:\n    repository_identity: acme/prod\n"+
			"    store_url: http://127.0.0.1:7881/store?default\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The selected registry: alpha owns it.
	selected := filepath.Join(t.TempDir(), "selected-domains.yaml")
	if err := os.WriteFile(selected, []byte("domains:\n  example.com/acme/alpha:\n"+
		"    repository_identity: acme/alpha\n    store_url: http://127.0.0.1:7881/store?default\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, so, se := captureStdoutStderr(t, func() int {
		return runBuild([]string{"--repo", "example.com/acme/alpha",
			"--domain-registry", selected,
			"--store-url", "http://127.0.0.1:7881/store?default"})
	})
	out := so + se
	if strings.Contains(out, "example.com/acme/prod") {
		t.Errorf("a stale DEFAULT registry refused a publication the SELECTED registry grants:\n%s", out)
	}
	if strings.Contains(out, "belongs to") && strings.Contains(out, "prod") {
		t.Errorf("ownership was evaluated against the wrong registry:\n%s", out)
	}
}

// BLIND-PASS FINDING (P1, cmd_build.go:722): the scoped path's intent omitted Overridden.
//
// Without it the guard treats an endpoint the operator named explicitly as one the command chose,
// so rule 2 -- this domain publishing somewhere other than its declared store -- refuses a
// publication the operator asked for by name. FALSE REFUSAL. Rule 1, another domain's store, is
// unaffected: an override never relaxes it, which the second case below pins.
func TestAnOverriddenEndpointIsNotRefusedAsThisDomainsOwnMismatch(t *testing.T) {
	// alpha declares store A; the operator names store B explicitly. B belongs to nobody.
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"),
		[]byte("domains:\n  example.com/acme/alpha:\n    repository_identity: acme/alpha\n"+
			"    store_url: http://127.0.0.1:7881/store?default\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	named := "http://127.0.0.1:7899/store?default"

	if err := guardStoreMutation(named, storeMutationIntent{
		Domain: "example.com/acme/alpha", Reason: "sensei build", Overridden: true}); err != nil {
		t.Errorf("an endpoint the operator named explicitly was refused as a mismatch: %v", err)
	}
	// And WITHOUT the override the same target IS refused, so the flag is load-bearing.
	if err := guardStoreMutation(named, storeMutationIntent{
		Domain: "example.com/acme/alpha", Reason: "sensei build"}); err == nil {
		t.Error("an unnamed endpoint differing from the declared store must still be refused")
	}
	// Rule 1 is never relaxed: another domain's store refuses override or not.
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"),
		[]byte("domains:\n  example.com/acme/neighbour:\n    repository_identity: acme/neighbour\n"+
			"    store_url: "+named+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := guardStoreMutation(named, storeMutationIntent{
		Domain: "example.com/acme/alpha", Reason: "sensei build", Overridden: true}); err == nil {
		t.Error("an override reached past rule 1 into another domain's store")
	}
}

// The scoped path must actually PASS it -- proving the guard honours Overridden says nothing
// about whether runScopedRepoUpdate fills it in, which is the shape that let this ship.
func TestTheScopedPathStatesWhetherTheEndpointWasNamed(t *testing.T) {
	src := readCmdSource(t, "cmd_build.go")
	i := strings.Index(src, "putNamedGraph(ctx, storeEndpoint, stagingIRI, stagedNT")
	if i < 0 {
		t.Fatal("the scoped named-graph publication is gone; this check has lost its anchor")
	}
	call := src[i:]
	if j := strings.Index(call, "err != nil"); j > 0 {
		call = call[:j]
	}
	if !strings.Contains(call, "Overridden:") {
		t.Errorf("the scoped publication does not state whether the endpoint was named:\n%s", strings.TrimSpace(call))
	}
}
