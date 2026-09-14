// SPDX-License-Identifier: AGPL-3.0-only

package main

// P2-1, preserved from blind review of #361 and raised on multiple heads:
//
//	"cmd_build.go hardcodes DefaultDomainRegistryPath() in activateGeneration calls instead of
//	 using the configured domain registry path. When an operator runs
//	 'sensei build --domain-registry /custom/domains.yaml ...', publication admission and store
//	 ownership checks use the custom registry, but activateGeneration updates the active pointer
//	 in ~/.sensei/domains.yaml. The custom registry is left unmodified with a stale or missing
//	 active generation, causing readers relying on it to refuse the newly published graph."
//
// The law was ALREADY WRITTEN DOWN, in buildRegistryPath's own doc comment: "Extracted so the
// pre-mutation store-ownership check and the pre-mutation admission check cannot end up reading
// two different registries -- which would let one of them vouch for a world the other never
// saw." Activation was simply never counted as one of the operations that must agree, and it is
// the only one that WRITES.
//
// Every fixture here uses TWO registries whose contents deliberately disagree, so a repair that
// happened to read the right file by accident cannot pass.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/seedmeta"
)

const (
	regDomain   = "example.com/acme/registryscoped"
	regIdentity = "acme/registryscoped"
	selectedGen = "aaaa0000aaaa0000aaaa0000aaaa0000aaaa0000aaaa0000aaaa0000aaaa0000"
	defaultGen  = "bbbb1111bbbb1111bbbb1111bbbb1111bbbb1111bbbb1111bbbb1111bbbb1111"
	selectedStr = "http://127.0.0.1:48111/store"
	defaultStr  = "http://127.0.0.1:48222/store"
)

// twoRegistries writes a DEFAULT registry in a controlled HOME and a CUSTOM registry elsewhere.
// Their state disagrees on purpose: different store URLs and different ACTIVE generations, so any
// operation reading the wrong one produces a visibly wrong answer rather than the same answer.
//
// Returns the custom registry's path.
func twoRegistries(t *testing.T, customActive, defaultActive string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, store, active string) {
		gen := ""
		if active != "" {
			gen = "\n        active_generation: " + active
		}
		if err := os.WriteFile(path, []byte(`domains:
    `+regDomain+`:
        repository_identity: `+regIdentity+`
        store_url: `+store+`
        allowed_corpus_roots:
            - docs/awareness`+gen+`
`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(home, ".sensei", "domains.yaml"), defaultStr, defaultActive)
	custom := filepath.Join(t.TempDir(), "custom-domains.yaml")
	write(custom, selectedStr, customActive)
	return custom
}

// W1. THE WRITING OPERATION FOLLOWS ITS SELECTION. activateGeneration is the only operation in a
// publication that WRITES a declaration, and it must write the registry the transaction selected.
// Two registries, deliberately disagreeing on both store URL and ACTIVE generation, so reading
// the wrong file gives a visibly wrong answer rather than the same answer.
func TestActivationWritesTheSelectedRegistryAndNotTheDefault(t *testing.T) {
	custom := twoRegistries(t, "", defaultGen)
	markerPath := filepath.Join(t.TempDir(), "graph-authority.json")
	marker := seedmeta.Marker{
		Digest:      selectedGen,
		IRI:         seedmeta.NamespaceIRI + "seedBuild/sha256-" + selectedGen,
		TripleCount: 11,
	}

	var out strings.Builder
	if err := activateGeneration(&out, markerPath, marker, regDomain,
		selectDomainRegistry(custom)); err != nil {
		t.Fatalf("activateGeneration: %v", err)
	}
	if got := declaredActiveGeneration(custom, regDomain); got != selectedGen {
		t.Errorf("the SELECTED registry declares ACTIVE %q, want %q", got, selectedGen)
	}
	// The default registry keeps the conflicting state it began with, which is how we know
	// nothing wrote there.
	if got := declaredActiveGeneration(DefaultDomainRegistryPath(), regDomain); got != defaultGen {
		t.Errorf("the DEFAULT registry's ACTIVE generation changed from %q to %q while a custom "+
			"registry was selected", defaultGen, got)
	}
}

// W2. A CONFLICTING DEFAULT HAS NO INFLUENCE, and the selected registry's own previous pointer is
// REPLACED -- activation is the transition, and the point of one pointer is that the previous
// value stops being current.
func TestAConflictingDefaultRegistryNeitherReadsNorAbsorbsTheActivation(t *testing.T) {
	custom := twoRegistries(t, selectedGen, defaultGen)
	marker := seedmeta.Marker{
		Digest:      "cccc2222cccc2222cccc2222cccc2222cccc2222cccc2222cccc2222cccc2222",
		IRI:         seedmeta.NamespaceIRI + "seedBuild/sha256-cccc",
		TripleCount: 11,
	}

	var out strings.Builder
	if err := activateGeneration(&out, filepath.Join(t.TempDir(), "m.json"), marker, regDomain,
		selectDomainRegistry(custom)); err != nil {
		t.Fatalf("activateGeneration: %v", err)
	}
	if got := declaredActiveGeneration(custom, regDomain); got != marker.Digest {
		t.Errorf("selected registry ACTIVE = %q, want the newly published %q", got, marker.Digest)
	}
	if got := declaredActiveGeneration(DefaultDomainRegistryPath(), regDomain); got != defaultGen {
		t.Errorf("default registry ACTIVE moved to %q; it must take no writes when a registry "+
			"was selected", got)
	}
}

// W3. A SELECTED REGISTRY WITH NO DECLARATION FOR THE DOMAIN must not be silently completed from
// the default. recordActiveGeneration deliberately leaves an unregistered domain alone -- the
// registry records what an operator ADMITTED, and a publication may update a declaration but must
// not create an admission. So the selected registry stays empty, and the DEFAULT one, which does
// declare the domain, must not be written instead.
func TestASelectedRegistryMissingTheDomainIsNotCompletedFromTheDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Default: declares the domain, and would happily accept the activation.
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"), []byte(`domains:
    `+regDomain+`:
        repository_identity: `+regIdentity+`
        store_url: `+defaultStr+`
        active_generation: `+defaultGen+`
`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Selected: declares a DIFFERENT domain only.
	custom := filepath.Join(t.TempDir(), "custom-domains.yaml")
	if err := os.WriteFile(custom, []byte(`domains:
    example.com/acme/somebodyelse:
        repository_identity: acme/somebodyelse
        store_url: `+selectedStr+`
`), 0o644); err != nil {
		t.Fatal(err)
	}

	marker := seedmeta.Marker{
		Digest:      selectedGen,
		IRI:         seedmeta.NamespaceIRI + "seedBuild/sha256-" + selectedGen,
		TripleCount: 11,
	}
	var out strings.Builder
	if err := activateGeneration(&out, filepath.Join(t.TempDir(), "m.json"), marker, regDomain,
		selectDomainRegistry(custom)); err != nil {
		t.Fatalf("activateGeneration: %v", err)
	}
	if got := declaredActiveGeneration(custom, regDomain); got != "" {
		t.Errorf("the selected registry acquired a declaration for an unadmitted domain: %q", got)
	}
	if got := declaredActiveGeneration(DefaultDomainRegistryPath(), regDomain); got != defaultGen {
		t.Fatalf("the DEFAULT registry was written because the selected one had no declaration: "+
			"ACTIVE moved from %q to %q", defaultGen, got)
	}
}

// W4. NO OVERRIDE preserves the default behaviour, or the repair has broken every operator who
// never passes the flag.
func TestWithNoOverrideActivationWritesTheDefaultRegistry(t *testing.T) {
	_ = twoRegistries(t, "", "")
	marker := seedmeta.Marker{
		Digest:      defaultGen,
		IRI:         seedmeta.NamespaceIRI + "seedBuild/sha256-" + defaultGen,
		TripleCount: 11,
	}
	var out strings.Builder
	if err := activateGeneration(&out, filepath.Join(t.TempDir(), "m.json"), marker, regDomain,
		selectDomainRegistry("")); err != nil {
		t.Fatalf("activateGeneration: %v", err)
	}
	if got := declaredActiveGeneration(DefaultDomainRegistryPath(), regDomain); got != defaultGen {
		t.Errorf("with no override the default registry declares ACTIVE %q, want %q", got, defaultGen)
	}
	if !selectDomainRegistry("").Overridden() && selectDomainRegistry("x").Overridden() {
		return
	}
	t.Error("the selection does not distinguish an operator-named registry from the default")
}

// W5. ONE TRANSACTION, ONE REGISTRY -- derived from the source rather than listed.
//
// This is the property the finding was about, and W1-W4 cannot prove it: they show the writing
// operation honours whatever selection it is given, not that the build hands every operation the
// SAME one. The defect was precisely a transaction that resolved a registry for ownership and
// admission and then named the default again at activation.
//
// So: no function in the build transaction may name DefaultDomainRegistryPath(). The selection
// resolves it exactly once, in selectDomainRegistry, and every operation consumes that.
func TestTheBuildTransactionNamesOneRegistryExactlyOnce(t *testing.T) {
	src, err := os.ReadFile("cmd_build.go")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(src), "DefaultDomainRegistryPath()"); n != 0 {
		t.Errorf("the build transaction names DefaultDomainRegistryPath() %d time(s); it must "+
			"consume the ONE selection it resolved, or one stage can answer for a registry the "+
			"others never saw", n)
	}
	if n := strings.Count(string(src), "selectDomainRegistry("); n != 1 {
		t.Errorf("the build resolves its registry %d time(s), want exactly 1", n)
	}

	// And the scoped update -- the path that actually activates -- must RECEIVE the selection
	// rather than resolve one of its own. Checked on the signature, so a future parameter cannot
	// be dropped silently.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "cmd_build.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "runScopedRepoUpdate" {
			continue
		}
		found = true
		for _, p := range fn.Type.Params.List {
			if id, ok := p.Type.(*ast.Ident); ok && id.Name == "domainRegistrySelection" {
				return
			}
		}
		t.Error("runScopedRepoUpdate does not receive a domainRegistrySelection, so the registry " +
			"an operator selected cannot reach the activation at the end of it")
	}
	if !found {
		t.Fatal("runScopedRepoUpdate not found; this check has lost its subject")
	}
}

// W6. THE FLAG REACHES THE SELECTION, driven. W5 proves the transaction resolves once and names
// no default; it cannot see whether the resolution actually reads --domain-registry. Found by
// mutation: replacing selectDomainRegistry(*domainRegistry) with selectDomainRegistry("")
// survived every witness above.
//
// The ownership check is the cheapest consumer to observe -- it runs before anything is compiled
// or dialled -- so the CUSTOM registry declares the target store as belonging to a foreign domain
// while the DEFAULT registry declares nothing at all. A build that honours the flag refuses; one
// that ignores it proceeds.
func TestTheBuildActuallyReadsTheDomainRegistryFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Default registry: empty. Nothing here can refuse anything.
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"),
		[]byte("domains: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Custom registry: the target store belongs to somebody else.
	custom := filepath.Join(t.TempDir(), "custom-domains.yaml")
	if err := os.WriteFile(custom, []byte(`domains:
    example.com/acme/theowner:
        repository_identity: acme/theowner
        store_url: `+selectedStr+`
`), 0o644); err != nil {
		t.Fatal(err)
	}

	root := projectRoot(t, t.TempDir())
	t.Chdir(root)
	var code int
	out := captureStderr(t, func() {
		code = runBuild([]string{
			"--repo", regDomain,
			"--store-url", selectedStr,
			"--domain-registry", custom,
		})
	})
	if !strings.Contains(out, "store another domain owns") {
		t.Fatalf("the build did not refuse, so it never read the registry named by "+
			"--domain-registry (exit=%d):\n%s", code, out)
	}
	if !strings.Contains(out, "example.com/acme/theowner") {
		t.Errorf("the refusal does not name the owner declared in the SELECTED registry, so the "+
			"verdict may have come from elsewhere:\n%s", out)
	}
}

// W6-opposite. And the same run without the flag must NOT refuse: the default registry declares
// nothing, so a build that ignored the flag in the other direction -- always reading the custom
// path -- would be caught here.
func TestWithoutTheFlagTheBuildReadsTheDefaultRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"),
		[]byte("domains: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := projectRoot(t, t.TempDir())
	t.Chdir(root)
	out := captureStderr(t, func() {
		_ = runBuild([]string{"--repo", regDomain, "--store-url", selectedStr})
	})
	if strings.Contains(out, "store another domain owns") {
		t.Fatalf("the build refused against an EMPTY default registry:\n%s", out)
	}
}
