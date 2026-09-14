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
	// It must be given the GOVERNED DOMAIN NAME and the target store, not a placeholder that
	// always agrees.
	//
	// THIS ASSERTION USED TO REQUIRE `*domain`, AND THAT IS WHY THE DEFECT SURVIVED. `sensei
	// build --domain` is the node tagging KIND (repo|shared); the governed domain name is
	// `--repo`. So this test demanded the wrong operand, and any correct repair failed it --
	// the reason a blind reviewer raised the finding on three consecutive heads while the
	// suite stayed green.
	//
	// The intent was right and is kept: the check must receive the two values it compares.
	// Only the name of the first one was wrong.
	line := src[call:]
	if j := strings.IndexByte(line, '\n'); j >= 0 {
		line = line[:j]
	}
	for _, want := range []string{"governedDomain", "*storeURL"} {
		if !strings.Contains(line, want) {
			t.Errorf("the check is not given %s: %s", want, strings.TrimSpace(line))
		}
	}
	// And it must NOT be given the tagging kind. Asserted negatively as well, because the
	// positive check above would pass a call that passed both.
	for _, forbidden := range []string{"*domain", "nodeKind"} {
		if strings.Contains(line, forbidden) {
			t.Errorf("the check is given the node tagging kind (%s), which is not a governed "+
				"domain name: %s", forbidden, strings.TrimSpace(line))
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
