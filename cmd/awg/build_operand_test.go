// SPDX-License-Identifier: AGPL-3.0-only

package main

// THE REPEATED BLIND FINDING at cmd_build.go:135, raised on three of four consecutive heads:
// verifyStoreOwnership and activateGeneration check *domain where *repo is the correct operand.
//
// TWO VOCABULARIES COLLIDE ON THE WORD "DOMAIN", which is why this survived three rounds of
// reading. `sensei build` declares both flags, and their own help text says what they hold:
//
//	--repo    "domain/repo to update IN PLACE, e.g. github.com/globulario/services"
//	--domain  "default domain KIND for untagged nodes: repo|shared"
//
// So --repo carries the GOVERNED DOMAIN NAME -- rejectPathLikeBuildDomain exists to refuse a
// filesystem path there, and compileAwarenessInputs passes it as RepositoryDomain -- while
// --domain carries a node-tagging kind, passed as DefaultDomain. `repo` and `shared` are the
// only values it takes.
//
// Both verifyStoreOwnership and activateGeneration need the governed domain NAME:
// verifyStoreOwnership indexes reg.Domains[requested] and compares against registry KEYS, and
// activateGeneration hands its argument to recordActiveGeneration as the domain whose ACTIVE
// pointer moves. Neither can do anything with "repo".
//
// THE NAMES CANNOT SETTLE THIS, and that is the point. runScopedRepoUpdate's parameter is
// *named* `domain` and is *passed* `*repo` -- so its own activateGeneration call (line 747) is
// already correct while looking identical to the two that are wrong. Every assertion below uses
// values where the governed domain, the domain kind, and the store URL are all different, so a
// swapped operand cannot pass by coincidence.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/seedmeta"
)

const (
	// Deliberately unlike each other AND unlike any kind value.
	buildOwnedDomain   = "example.com/acme/owned"
	buildForeignDomain = "example.com/acme/foreign"
	buildOwnedStore    = "http://127.0.0.1:47111/store"
	buildForeignStore  = "http://127.0.0.1:47222/store"
)

// buildRegistryWorld writes a registry declaring a store for each of two domains, in a
// controlled HOME, and returns its path.
func buildRegistryWorld(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".sensei", "domains.yaml")
	if err := os.WriteFile(path, []byte(`domains:
    `+buildOwnedDomain+`:
        repository_identity: acme/owned
        store_url: `+buildOwnedStore+`
        allowed_corpus_roots:
            - docs/awareness
    `+buildForeignDomain+`:
        repository_identity: acme/foreign
        store_url: `+buildForeignStore+`
        allowed_corpus_roots:
            - docs/awareness
`), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadBuildRegistry(t *testing.T, path string) *DomainRegistry {
	t.Helper()
	reg, err := LoadDomainRegistry(path)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if reg == nil {
		t.Fatal("registry loaded as nil")
	}
	return reg
}

// W1. THE OPERAND CONTRACT, stated positively. verifyStoreOwnership compares against registry
// KEYS, which are governed domain names, so the operand is a domain name and nothing else.
// Asserted in all three directions -- allowed, refused, and swapped -- with values that differ
// from each other so no assertion can pass by coincidence.
func TestStoreOwnershipIsAboutGovernedDomainNames(t *testing.T) {
	reg := loadBuildRegistry(t, buildRegistryWorld(t))

	// A domain publishing into the store it declares.
	if err := verifyStoreOwnership(reg, buildOwnedDomain, buildOwnedStore, false); err != nil {
		t.Errorf("a domain was refused its OWN declared store: %v", err)
	}
	// A domain publishing into a store ANOTHER domain declares.
	err := verifyStoreOwnership(reg, buildOwnedDomain, buildForeignStore, false)
	if err == nil {
		t.Fatalf("publishing into %s, which %s declares, was allowed for %s",
			buildForeignStore, buildForeignDomain, buildOwnedDomain)
	}
	var oe *storeOwnershipError
	if !asStoreOwnershipError(err, &oe) {
		t.Fatalf("unexpected error type: %v", err)
	}
	if oe.Owner != buildForeignDomain {
		t.Errorf("the refusal names owner %q, want %q", oe.Owner, buildForeignDomain)
	}

	// SWAPPED OPERANDS must not be accepted, or nothing above proves which value goes where.
	if err := verifyStoreOwnership(reg, buildOwnedStore, buildOwnedStore, false); err == nil {
		t.Error("a store URL passed as the domain was accepted as owning that store")
	}
	if err := verifyStoreOwnership(reg, buildOwnedDomain, buildOwnedDomain, false); err == nil {
		t.Error("a domain name passed as the store target was accepted as that domain's store")
	}

	// AND THE KIND IS NOT A DOMAIN NAME. These two assertions record the measured blast radius of
	// the defect, and they are what makes the operand load-bearing rather than cosmetic:
	//
	//  - given the kind, the check finds the store's RIGHTFUL owner, sees its key is not "repo",
	//    and refuses a correctly configured build from its own store;
	//  - and where no domain declares the target at all, reg.Domains["repo"] does not exist, so
	//    it goes inert in exactly the case it exists for.
	if err := verifyStoreOwnership(reg, string(nodeDomainKindRepo), buildOwnedStore, false); err == nil {
		t.Error("the kind no longer produces the false-foreign-owner refusal; this assertion has " +
			"lost the behaviour it records")
	}
	if err := verifyStoreOwnership(reg, string(nodeDomainKindRepo),
		"http://127.0.0.1:47999/store", false); err != nil {
		t.Error("the kind no longer goes inert on an undeclared store; this assertion has lost " +
			"the behaviour it records")
	}
}

// buildOwnershipStderr drives the REAL runBuild far enough to reach the ownership check and
// returns what it wrote to stderr. The check runs before anything is compiled or dialled, so no
// store and no corpus are needed; the build failing later is expected and is not asserted on.
func buildOwnershipStderr(t *testing.T, args ...string) (int, string) {
	t.Helper()
	root := projectRoot(t, t.TempDir())
	t.Chdir(root)
	var code int
	out := captureStderr(t, func() { code = runBuild(args) })
	return code, out
}

const (
	ownershipRefusalMarker = "store another domain owns"
	// The pre-mutation admission stage, which runs immediately AFTER the ownership guard. Its
	// appearance is proof the build got PAST the guard -- so a refusal that still reaches it
	// refused in words only. A guard's position is part of its correctness.
	pastTheGuardMarker = "PUBLICATION_REFUSED"
)

// W2. DRIVEN, the case the defect broke: publishing into the store this domain itself declares.
// At head 50762d33 this printed the ownership refusal, naming its own domain as the foreign
// owner of its own store.
func TestBuildDoesNotRefuseADomainFromItsOwnDeclaredStore(t *testing.T) {
	buildRegistryWorld(t)
	_, out := buildOwnershipStderr(t, "--repo", buildOwnedDomain, "--store-url", buildOwnedStore)
	if strings.Contains(out, ownershipRefusalMarker) {
		t.Fatalf("build refused %s from its OWN declared store %s:\n%s",
			buildOwnedDomain, buildOwnedStore, out)
	}
	// And it must actually PROCEED, not merely stay quiet: without this, a repair that deleted
	// the check would pass.
	if !strings.Contains(out, pastTheGuardMarker) {
		t.Errorf("the build did not reach the stage after the ownership guard, so this witness "+
			"cannot tell 'allowed' from 'stopped for some other reason':\n%s", out)
	}
}

// W3. DRIVEN, the case the check exists for: publishing into a store another domain declares.
// The opposite witness -- without it, a repair that simply stopped checking would pass W2.
func TestBuildStillRefusesADomainFromAnotherDomainsStore(t *testing.T) {
	buildRegistryWorld(t)
	code, out := buildOwnershipStderr(t, "--repo", buildOwnedDomain, "--store-url", buildForeignStore)
	if !strings.Contains(out, ownershipRefusalMarker) {
		t.Fatalf("build did not refuse %s from %s, which %s declares:\n%s",
			buildOwnedDomain, buildForeignStore, buildForeignDomain, out)
	}
	if !strings.Contains(out, buildForeignDomain) {
		t.Errorf("the refusal does not name the owning domain:\n%s", out)
	}
	// THE REFUSAL MUST STOP THE BUILD, not only describe it. A mutant that dropped the `return 1`
	// and let the build continue past the guard survived a witness that asserted only the
	// message -- the same shape as every "a warning is not a boundary" instance on this front.
	if code != 1 {
		t.Errorf("an ownership refusal exited %d, not 1", code)
	}
	if strings.Contains(out, pastTheGuardMarker) {
		t.Fatalf("the build continued past the ownership guard after refusing:\n%s", out)
	}
}

// W4. DRIVEN, and the sharpest of the four: the tagging kind must have NO influence on the
// ownership verdict. Both outcomes must be identical with and without --domain, which is only
// true once the verdict stops reading that flag. A witness that omitted --domain would have
// passed at the defective head too, because the flag's default is also not a domain name.
func TestTheNodeTaggingKindDoesNotAffectTheOwnershipVerdict(t *testing.T) {
	for _, kind := range []string{"", string(nodeDomainKindRepo), string(nodeDomainKindShared)} {
		own := []string{"--repo", buildOwnedDomain, "--store-url", buildOwnedStore}
		other := []string{"--repo", buildOwnedDomain, "--store-url", buildForeignStore}
		if kind != "" {
			own = append(own, "--domain", kind)
			other = append(other, "--domain", kind)
		}
		buildRegistryWorld(t)
		if _, out := buildOwnershipStderr(t, own...); strings.Contains(out, ownershipRefusalMarker) {
			t.Errorf("--domain %q: refused from its own declared store:\n%s", kind, out)
		} else if !strings.Contains(out, pastTheGuardMarker) {
			t.Errorf("--domain %q: the build never reached the stage after the guard:\n%s", kind, out)
		}
		buildRegistryWorld(t)
		code, out := buildOwnershipStderr(t, other...)
		if !strings.Contains(out, ownershipRefusalMarker) {
			t.Errorf("--domain %q: did not refuse from another domain's store:\n%s", kind, out)
		}
		if code != 1 || strings.Contains(out, pastTheGuardMarker) {
			t.Errorf("--domain %q: the refusal did not stop the build (exit=%d):\n%s", kind, code, out)
		}
	}
}

// W5. ACTIVATION, both directions. Given the governed domain name the pointer moves; given the
// empty domain -- which is what --all supplies, honestly, because a whole-store load spans every
// domain and activates none -- it says so and moves nothing.
//
// At head 50762d33 this call received the tagging kind, so recordActiveGeneration left the
// unregistered domain "repo" alone and returned nil while the command printed "ACTIVE
// generation: <digest> for repo": law 6 activation silently not happening, reported as success.
func TestActivationMovesThePointerForTheGovernedDomainAndClaimsNothingOtherwise(t *testing.T) {
	registry := buildRegistryWorld(t)
	marker := seedmeta.Marker{
		Digest:      declaredGen,
		IRI:         seedmeta.NamespaceIRI + "seedBuild/sha256-" + declaredGen,
		TripleCount: 42,
	}

	var moved strings.Builder
	if err := activateGeneration(&moved, filepath.Join(t.TempDir(), "m.json"), marker,
		buildOwnedDomain, registry); err != nil {
		t.Fatalf("activateGeneration: %v", err)
	}
	if got := declaredActiveGeneration(registry, buildOwnedDomain); got != declaredGen {
		t.Fatalf("ACTIVE generation for %s = %q, want %q", buildOwnedDomain, got, declaredGen)
	}

	// The empty domain: no claim, and nothing written for the tagging kind either.
	registry = buildRegistryWorld(t)
	var none strings.Builder
	if err := activateGeneration(&none, filepath.Join(t.TempDir(), "m.json"), marker,
		"", registry); err != nil {
		t.Fatalf("activateGeneration: %v", err)
	}
	if !strings.Contains(none.String(), "NOT updated") {
		t.Errorf("an unnamed domain did not report the pointer as NOT updated:\n%s", none.String())
	}
	if got := declaredActiveGeneration(registry, buildOwnedDomain); got != "" {
		t.Errorf("an unnamed domain moved %s's pointer to %q", buildOwnedDomain, got)
	}
	if got := declaredActiveGeneration(registry, string(nodeDomainKindRepo)); got != "" {
		t.Errorf("a domain literally named %q acquired an ACTIVE generation %q",
			nodeDomainKindRepo, got)
	}
}

// W6. THE TYPE IS THE GUARD. The tagging kind is a defined type, so handing it to a governed
// domain-name consumer does not compile -- a guard a reader cannot forget. What a type cannot
// catch is a deliberate .String(), so that crossing is confined to ONE method and asserted here
// to be the only one in production code.
func TestTheTaggingKindCrossesToAStringInExactlyOnePlace(t *testing.T) {
	crossings := 0
	for _, name := range buildPackageGoFiles(t, ".") {
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		crossings += strings.Count(string(body), "nodeKind.String()")
		if strings.Contains(string(body), "string(nodeKind)") {
			t.Errorf("%s converts the tagging kind with a bare cast; use the named method so "+
				"every crossing is greppable", filepath.Base(name))
		}
	}
	if crossings != 1 {
		t.Errorf("the tagging kind crosses to a plain string %d times, want exactly 1 (the graph "+
			"builder's DefaultDomain field)", crossings)
	}
}

func buildPackageGoFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
			out = append(out, filepath.Join(dir, n))
		}
	}
	if len(out) == 0 {
		t.Fatal("no production files found; this check has lost its subject")
	}
	return out
}

// asStoreOwnershipError is errors.As, spelled out so the witness names the type it expects.
func asStoreOwnershipError(err error, target **storeOwnershipError) bool {
	for err != nil {
		if oe, ok := err.(*storeOwnershipError); ok {
			*target = oe
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// W7. THE KIND'S OWN VOCABULARY, read by membership. --domain accepted any string, so a governed
// domain name typed there was silently adopted as the default tag for every untagged node. That
// is the permissive direction this repository has recorded repeatedly, and it is also what made
// the confusion between the two flags survivable: both accepted each other's values.
func TestTheTaggingKindAcceptsOnlyItsRecognisedValues(t *testing.T) {
	for _, ok := range []string{"", "repo", "shared", "  repo  "} {
		if err := nodeDomainKind(ok).validate(); err != nil {
			t.Errorf("a recognised tagging kind %q was refused: %v", ok, err)
		}
	}
	// A governed domain name is the value that must not be adopted as a kind -- it is exactly
	// what an operator confusing the two flags would type.
	for _, bad := range []string{buildOwnedDomain, "Repo", "REPO", "repository", "repo,shared", "x"} {
		err := nodeDomainKind(bad).validate()
		if err == nil {
			t.Errorf("%q was accepted as a node tagging kind", bad)
			continue
		}
		if !strings.Contains(err.Error(), "--repo") {
			t.Errorf("the refusal for %q does not point at the flag that takes a governed domain "+
				"name: %v", bad, err)
		}
	}
}

// W7-driven. And the refusal reaches an operator running the real command.
func TestBuildRefusesAGovernedDomainNameInTheTaggingKindFlag(t *testing.T) {
	buildRegistryWorld(t)
	code, out := buildOwnershipStderr(t, "--repo", buildOwnedDomain,
		"--store-url", buildOwnedStore, "--domain", buildOwnedDomain)
	if code != 1 {
		t.Errorf("build exited %d with a governed domain name in --domain, want 1", code)
	}
	if !strings.Contains(out, "is not a node tagging kind") {
		t.Fatalf("build accepted a governed domain name as the tagging kind:\n%s", out)
	}
	if strings.Contains(out, pastTheGuardMarker) {
		t.Errorf("the build continued past the refusal:\n%s", out)
	}
}
