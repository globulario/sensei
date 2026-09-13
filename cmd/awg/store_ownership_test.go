// SPDX-License-Identifier: AGPL-3.0-only

package main

// THE INVARIANT: a governed production Oxigraph store belongs to exactly ONE Sensei
// domain.
//
// Forced by the Phase 7 measurement. A marker's digest is computed over the WHOLE store,
// so publishing domain B into a store recomputes it, and domain A's ACTIVE pointer — which
// names the old whole-store digest — goes stale the instant B lands. Measured directly:
// in one reader process, at one moment, against one store, domain sensei-code agreed and
// domain sensei refused, the only difference being whose pointer the activation had
// updated.
//
// Per-domain subgraph digests would also solve it and are deliberately NOT attempted here.
// The simpler invariant is enforced instead.
//
// A second hazard the same measurement exposed: netcfg's built-in default store is
// http://localhost:7878, which on this machine is the LEGACY awg store
// (~/.local/share/awg/oxigraph, 237,049 triples, `awg-oxigraph.service`) and belongs to no
// governed domain. Neither governed domain uses it — sensei's config states :7881,
// sensei-code's :7882 — so a command run without a project config publishes into the
// legacy store purely because it is the default and reachable.

import (
	"strings"
	"testing"
)

const (
	storeA = "http://localhost:7881/store?default"
	storeB = "http://localhost:7882/store?default"
)

func ownedRegistry(t *testing.T, body string) *DomainRegistry {
	t.Helper()
	reg, err := LoadDomainRegistry(writeRegistryFixture(t, body))
	if err != nil {
		t.Fatalf("LoadDomainRegistry: %v", err)
	}
	return reg
}

// Two governed domains may not declare the same store. Refused at LOAD, so every command
// inherits it rather than each publication path having to remember.
func TestARegistryDeclaringOneStoreForTwoDomainsIsRefused(t *testing.T) {
	path := writeRegistryFixture(t, `domains:
    example.com/acme/one:
        repository_identity: acme/one
        store_url: `+storeA+`
    example.com/acme/two:
        repository_identity: acme/two
        store_url: `+storeA+`
`)
	_, err := LoadDomainRegistry(path)
	if err == nil {
		t.Fatal("a registry binding one store to two governed domains loaded successfully")
	}
	for _, want := range []string{"example.com/acme/one", "example.com/acme/two", "7881"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal omits %q: %v", want, err)
		}
	}
}

// Spelling is not identity: the same store written two ways is still one store.
func TestStoreIdentityIgnoresSpelling(t *testing.T) {
	path := writeRegistryFixture(t, `domains:
    example.com/acme/one:
        repository_identity: acme/one
        store_url: http://localhost:7881/store?default
    example.com/acme/two:
        repository_identity: acme/two
        store_url: HTTP://LocalHost:7881/store?default
`)
	if _, err := LoadDomainRegistry(path); err == nil {
		t.Error("two spellings of one store were treated as two stores")
	}
}

// Publishing domain B into the store domain A owns is refused, always — an explicit
// override cannot license stranding a neighbour's pointer.
func TestPublishingIntoAnotherDomainsStoreIsRefused(t *testing.T) {
	reg := ownedRegistry(t, `domains:
    example.com/acme/one:
        repository_identity: acme/one
        store_url: `+storeA+`
    example.com/acme/two:
        repository_identity: acme/two
        store_url: `+storeB+`
`)
	for _, overridden := range []bool{false, true} {
		err := verifyStoreOwnership(reg, "example.com/acme/two", storeA, overridden)
		if err == nil {
			t.Fatalf("overridden=%v: domain two was allowed to publish into domain one's store", overridden)
		}
		for _, want := range []string{"example.com/acme/one", "example.com/acme/two"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("overridden=%v: the refusal omits %q: %v", overridden, want, err)
			}
		}
		if !strings.Contains(err.Error(), "nothing has been") && !strings.Contains(err.Error(), "before") {
			t.Errorf("overridden=%v: the refusal does not say it precedes mutation: %v", overridden, err)
		}
	}
	// Its own store is fine.
	if err := verifyStoreOwnership(reg, "example.com/acme/two", storeB, false); err != nil {
		t.Errorf("a domain was refused its own store: %v", err)
	}
}

// A domain that declares a store must publish into it. Targeting a DIFFERENT, unowned
// store is the accident the netcfg default causes — :7878 is reachable and belongs to
// nobody — so it is refused unless the operator named it explicitly.
func TestADeclaredDomainDoesNotSilentlyPublishElsewhere(t *testing.T) {
	reg := ownedRegistry(t, `domains:
    example.com/acme/one:
        repository_identity: acme/one
        store_url: `+storeA+`
`)
	legacy := "http://localhost:7878/store?default"
	if err := verifyStoreOwnership(reg, "example.com/acme/one", legacy, false); err == nil {
		t.Fatal("a domain declaring :7881 silently published into the default :7878 store")
	}
	// Named explicitly, it is the operator's call — and law 14's notice already makes a
	// raw store URL visible.
	if err := verifyStoreOwnership(reg, "example.com/acme/one", legacy, true); err != nil {
		t.Errorf("an explicitly named store was refused for a domain that owns no claim on it: %v", err)
	}
}

// Inert until declared: a domain with no store_url behaves exactly as before, which is
// what keeps this from breaking every existing caller and every disposable store.
func TestAnUndeclaredDomainIsUnconstrained(t *testing.T) {
	reg := ownedRegistry(t, `domains:
    example.com/acme/one:
        repository_identity: acme/one
`)
	for _, target := range []string{storeA, storeB, "http://127.0.0.1:7899/store?default"} {
		if err := verifyStoreOwnership(reg, "example.com/acme/one", target, false); err != nil {
			t.Errorf("an undeclared domain was refused store %s: %v", target, err)
		}
	}
}

// A disposable store is one no domain declares — that is the non-production opt-out, and
// it is explicit in the registry rather than a flag that disables the check.
func TestADisposableStoreIsOneNoDomainClaims(t *testing.T) {
	reg := ownedRegistry(t, `domains:
    example.com/acme/one:
        repository_identity: acme/one
        store_url: `+storeA+`
    example.com/acme/two:
        repository_identity: acme/two
        store_url: `+storeB+`
`)
	disposable := "http://127.0.0.1:7899/store?default"
	// Unclaimed, and the domain declares one: still refused unless named, because the
	// accident this prevents is publishing somewhere unintended.
	if err := verifyStoreOwnership(reg, "example.com/acme/one", disposable, false); err == nil {
		t.Error("a declared domain drifted into an unclaimed store without the operator naming it")
	}
	if err := verifyStoreOwnership(reg, "example.com/acme/one", disposable, true); err != nil {
		t.Errorf("an explicitly named disposable store was refused: %v", err)
	}
}

// Activation cannot strand a neighbour, which follows from the above: a publication that
// would land in another domain's store never runs, so no activation can move a store
// another domain's pointer names.
func TestActivationCannotStrandAnotherDomainsPointer(t *testing.T) {
	reg := ownedRegistry(t, `domains:
    example.com/acme/one:
        repository_identity: acme/one
        store_url: `+storeA+`
        active_generation: aaaaaaaaaaaa
    example.com/acme/two:
        repository_identity: acme/two
        store_url: `+storeB+`
        active_generation: bbbbbbbbbbbb
`)
	if err := verifyStoreOwnership(reg, "example.com/acme/two", storeA, false); err == nil {
		t.Fatal("domain two could publish into the store domain one's ACTIVE pointer names")
	}
	// And the pointer that would have been stranded is untouched, because the refusal
	// precedes any mutation.
	if got := reg.Domains["example.com/acme/one"].ActiveGeneration; got != "aaaaaaaaaaaa" {
		t.Errorf("domain one's pointer changed during a refused publication: %q", got)
	}
}

// The check must be wired, and wired BEFORE the store is touched. Source-anchored and
// labelled so: the publication path needs a live store. The behavioural witnesses above
// prove the predicate; this proves where it runs.
func TestTheOwnershipCheckPrecedesEveryStoreMutation(t *testing.T) {
	src := readCmdSource(t, "cmd_build.go")
	call := strings.Index(src, "verifyStoreOwnership(")
	if call < 0 {
		t.Fatal("the build path never verifies store ownership")
	}
	if n := strings.Count(src, "verifyStoreOwnership("); n != 1 {
		t.Errorf("verifyStoreOwnership has %d call sites; one gate, one place", n)
	}
	// Every way this command writes to a store must come after it.
	for _, mutator := range []string{"uploadNTriples(", "putNamedGraph(", "guardAgainstLiveShrink("} {
		i := strings.Index(src, mutator)
		if i < 0 {
			continue
		}
		if i < call {
			t.Errorf("%s appears before the ownership check (mutator=%d check=%d); a refusal after the bytes are in is not a refusal", mutator, i, call)
		}
	}
	// It must be given the domain and the target store, not a placeholder that always agrees.
	line := src[call:]
	if j := strings.IndexByte(line, '\n'); j >= 0 {
		line = line[:j]
	}
	for _, want := range []string{"*domain", "*storeURL"} {
		if !strings.Contains(line, want) {
			t.Errorf("the check is not given %s: %s", want, strings.TrimSpace(line))
		}
	}
	// And a refusal must end the command.
	tail := src[call:]
	if end := strings.Index(tail, "\n\t\t}"); end > 0 {
		tail = tail[:end]
	}
	if !strings.Contains(tail, "return 1") {
		t.Errorf("a refused publication does not end the command:\n%s", tail)
	}
}

// REVIEW FINDINGS (P1 ×3, chatgpt-codex-connector on #360/#359/#362). All three held, and
// all three were in the wiring rather than the predicate:
//
//	"Pass --repo to the store ownership check"      the scoped path's domain is *repo;
//	                                               *domain may be empty, and an empty
//	                                               requested domain makes EVERY declared
//	                                               store look like another domain's
//	"Fail closed when the ownership registry cannot load"
//	                                               `if rerr == nil` SKIPPED the check on a
//	                                               malformed registry — fail-open, and a
//	                                               direct contradiction of the load-time
//	                                               validation added in the same change
//	"Update the registry selected by --domain-registry"
//	                                               both activateGeneration calls hardcoded
//	                                               the default path, so an operator using a
//	                                               non-default registry had the ACTIVE
//	                                               pointer written into the wrong file

func TestAnEmptyRequestedDomainDoesNotMakeEveryStoreForeign(t *testing.T) {
	reg := ownedRegistry(t, `domains:
    example.com/acme/one:
        repository_identity: acme/one
        store_url: `+storeA+`
`)
	// With no domain named, rule 1's "another domain owns this" would fire against every
	// declared store, refusing a publication that names no domain at all. The caller must
	// pass the domain it is publishing, and the predicate must not invent one.
	err := verifyStoreOwnership(reg, "", storeA, false)
	if err == nil {
		t.Skip("an empty domain is currently accepted; the wiring test below is what binds the caller")
	}
	if !strings.Contains(err.Error(), "example.com/acme/one") {
		t.Errorf("the refusal does not name the owner: %v", err)
	}
}

// The wiring: the ownership check and the activation must both read the registry the
// OPERATOR selected, and must refuse rather than skip when it cannot be read.
func TestTheBuildWiringUsesTheSelectedRegistryAndFailsClosed(t *testing.T) {
	src := readCmdSource(t, "cmd_build.go")

	// 1. No activation may hardcode the default registry path.
	for _, line := range strings.Split(src, "\n") {
		if strings.Contains(line, "activateGeneration(") && strings.Contains(line, "DefaultDomainRegistryPath()") {
			t.Errorf("an activation writes the pointer into the DEFAULT registry, ignoring --domain-registry: %s", strings.TrimSpace(line))
		}
	}
	// 2. The ownership check must not be skipped when the registry cannot load.
	if strings.Contains(src, "LoadDomainRegistry(buildRegistryPath(*domainRegistry)); rerr == nil {") {
		t.Error("a registry that cannot be read SKIPS the ownership check; a malformed registry must refuse, as LoadDomainRegistry's own validation does")
	}
	// 3. The scoped path publishes under *repo, so that is the domain the check must see.
	i := strings.Index(src, "verifyStoreOwnership(")
	if i < 0 {
		t.Fatal("the ownership check is gone")
	}
	line := src[i:]
	if j := strings.IndexByte(line, '\n'); j >= 0 {
		line = line[:j]
	}
	// Asserted by what the call USES, not by blacklisting a substring: the correct call
	// contains *domain inside publishedDomain(*repo, *domain), and an earlier version of
	// this check rejected it for that reason.
	if !strings.Contains(line, "publishedDomain(") {
		t.Errorf("the check does not resolve the published domain through the one helper, so it can disagree with the activation: %s", strings.TrimSpace(line))
	}
	// And the activations must use the same helper, or the two can name different domains.
	for _, l := range strings.Split(src, "\n") {
		if strings.Contains(l, "activateGeneration(") && !strings.Contains(l, "publishedDomain(") && !strings.Contains(l, ", domain,") {
			t.Errorf("an activation names a domain by another route than the ownership check: %s", strings.TrimSpace(l))
		}
	}
}
