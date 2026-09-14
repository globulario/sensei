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
	"os"
	"path/filepath"
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
	// The primitives that replace a store's contents.
	for _, prim := range []string{"uploadNTriples(", "reloadOxigraphStore("} {
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
