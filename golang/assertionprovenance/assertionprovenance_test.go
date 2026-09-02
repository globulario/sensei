// SPDX-License-Identifier: AGPL-3.0-only

package assertionprovenance

import (
	"errors"
	"math/rand"
	"testing"
)

const (
	svc    = Domain("github.com/globulario/services")
	sen    = Domain("github.com/globulario/sensei")
	shared = "https://globular.io/awareness#forbiddenFix/delete_generated_mid_flow_without_regeneration"
)

// The measured specimen: one subject, two domains, different assertions.
func corpus() []Assertion {
	return []Assertion{
		{shared, "label", "delete generated/ mid-flow", sen},
		{shared, "requiresTest", "TestRegenerates", sen},
		{shared, "constrainedBy", "invariant.artifact_authority", svc},
		{shared, "realizedBy", "FileService", svc},
		{"https://example/only-sensei", "label", "local", sen},
	}
}

// PROOF 1. Same subject, different domains, different assertions — both
// identities stable.
//
// Under subject ownership this was impossible: each domain's digest covered
// every triple on the shared subject, so each contained the other's content and
// moved whenever the other published.
func TestSharedSubjectYieldsStableIndependentIdentities(t *testing.T) {
	all := corpus()
	dSen, err := SliceDigest(all, sen)
	if err != nil {
		t.Fatalf("sensei digest: %v", err)
	}
	dSvc, err := SliceDigest(all, svc)
	if err != nil {
		t.Fatalf("services digest: %v", err)
	}
	if dSen == dSvc {
		t.Fatal("two domains authoring different assertions produced one identity")
	}
	for _, a := range mustSlice(t, all, sen) {
		if a.Origin != sen {
			t.Fatalf("sensei's slice contains an assertion authored by %q", a.Origin)
		}
	}
	for _, a := range mustSlice(t, all, svc) {
		if a.Origin != svc {
			t.Fatalf("services' slice contains an assertion authored by %q", a.Origin)
		}
	}
}

// PROOF 2. A foreign assertion added on a shared subject, which is NOT a
// dependency, leaves the local domain PROVEN.
//
// This is the exact event that cost services its proof on the live store.
func TestForeignAssertionOnASharedSubjectDoesNotInvalidate(t *testing.T) {
	before := corpus()
	after := append(corpus(), Assertion{shared, "reviewedBy", "policy.C", sen})

	if d1, d2 := mustDigest(t, before, svc), mustDigest(t, after, svc); d1 != d2 {
		t.Fatalf("services identity moved because SENSEI added an assertion: %s -> %s", d1[:12], d2[:12])
	}
	ok, why := CarryForward(before, after, svc, nil)
	if !ok {
		t.Fatalf("services lost its proof to a foreign, non-dependency assertion: %s", why)
	}
}

// PROOF 3. THE BLADE. A foreign assertion that IS a declared dependency
// changes, and the dependent domain becomes UNPROVEN.
//
// Without this, "identity follows authorship" would silently become permission
// to ignore cross-domain semantic change — which is worse than the defect it
// replaces.
func TestChangingADeclaredDependencyInvalidatesTheDependent(t *testing.T) {
	before := corpus()
	dep := Dependency{
		On:     Assertion{shared, "constrainedBy", "invariant.artifact_authority", svc},
		Digest: DigestOf(Assertion{shared, "constrainedBy", "invariant.artifact_authority", svc}),
	}

	after := corpus()
	for i := range after {
		if after[i].Predicate == "constrainedBy" {
			after[i].Object = "invariant.artifact_authority_v2" // services changes what sensei relied on
		}
	}
	if d1, d2 := mustDigest(t, before, sen), mustDigest(t, after, sen); d1 != d2 {
		t.Fatalf("the specimen is wrong: sensei's OWN digest moved (%s -> %s), so this would "+
			"fail for the wrong reason", d1[:12], d2[:12])
	}
	ok, why := CarryForward(before, after, sen, []Dependency{dep})
	if ok {
		t.Fatal("a declared dependency changed and the dependent domain kept its proof")
	}
	t.Logf("correctly refused: %s", why)
}

// PROOF 4. One assertion claimed by two domains is refused, not resolved.
func TestAnAssertionClaimedByTwoDomainsIsRefused(t *testing.T) {
	all := append(corpus(), Assertion{shared, "label", "delete generated/ mid-flow", svc})
	if _, err := SliceDigest(all, sen); !errors.Is(err, ErrAmbiguousOwnership) {
		t.Fatalf("expected ambiguous ownership to be refused, got %v", err)
	}
}

// PROOF 5. Missing provenance is UNKNOWN, never silently assigned.
//
// The migration will produce partially annotated corpora, and the tempting
// fallback is to attribute by subject tag — the model being retired.
func TestUnattributedAssertionIsRefusedRatherThanAssigned(t *testing.T) {
	all := append(corpus(), Assertion{shared, "orphan", "value", ""})
	if _, err := SliceDigest(all, sen); !errors.Is(err, ErrUnattributed) {
		t.Fatalf("expected unattributed assertion to be refused, got %v", err)
	}
}

// PROOF 6. Reordered serialization yields an identical digest.
func TestDigestIsIndependentOfSerializationOrder(t *testing.T) {
	all := corpus()
	want := mustDigest(t, all, sen)
	for i := 0; i < 20; i++ {
		shuffled := append([]Assertion(nil), all...)
		rand.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		if got := mustDigest(t, shuffled, sen); got != want {
			t.Fatalf("digest depends on order: %s vs %s", got[:12], want[:12])
		}
	}
}

func mustSlice(t *testing.T, all []Assertion, d Domain) []Assertion {
	t.Helper()
	s, err := Slice(all, d)
	if err != nil {
		t.Fatalf("slice %s: %v", d, err)
	}
	return s
}

func mustDigest(t *testing.T, all []Assertion, d Domain) string {
	t.Helper()
	s, err := SliceDigest(all, d)
	if err != nil {
		t.Fatalf("digest %s: %v", d, err)
	}
	return s
}

// --- Proofs added for the four review findings on b4a763d6 ---------------

// Finding 1 (P1): a dependency reassigned to another domain must invalidate.
//
// Same subject, predicate and object; different author. If the digest omits
// Origin these are indistinguishable and the closure proof survives an
// authority change -- the exact collapse this package exists to prevent.
func TestCarryForwardRejectsADependencyReassignedToAnotherDomain(t *testing.T) {
	const sensei, services = Domain("sensei"), Domain("services")

	depBefore := Assertion{Subject: "inv.x", Predicate: "requiresTest", Object: "TestX", Origin: services}
	depAfter := depBefore
	depAfter.Origin = sensei // reassigned; statement byte-identical

	if DigestOf(depBefore) == DigestOf(depAfter) {
		t.Fatal("reassigning an assertion to another domain left its digest unchanged; provenance is not in the identity")
	}

	own := Assertion{Subject: "inv.x", Predicate: "notedBy", Object: "sensei", Origin: sensei}
	before := []Assertion{own, depBefore}
	after := []Assertion{own, depAfter}
	deps := []Dependency{{On: depBefore, Digest: DigestOf(depBefore)}}

	ok, why := CarryForward(before, after, sensei, deps)
	if ok {
		t.Fatalf("carry-forward survived a dependency changing author: %s", why)
	}
}

// Finding 2 (P1): the slice digest must not be forgeable by field content.
//
// The obvious example -- {a,b,c},{d,e,f} against the single merged assertion
// {a,b,"c\nd\x1fe\x1ff"} -- is NOT sufficient: those slices differ in length,
// so a count prefix alone separates them and the test passes without ever
// exercising the framing. A mutation reverting the encoding survived it.
//
// The load-bearing case holds the count fixed and moves a field boundary
// BETWEEN two assertions, so only per-field length prefixes can tell them
// apart:
//
//	A = {a,b,"c\nd"}, {e,f,g}   -> a.b.c\nd \n e.f.g \n
//	B = {a,b,c}, {"d\ne",f,g}   -> a.b.c \n d\ne.f.g \n      (identical bytes)
func TestSliceDigestDistinguishesSlicesThatSeparatorEncodingWouldCollide(t *testing.T) {
	const d = Domain("sensei")

	digest := func(name string, in []Assertion) string {
		t.Helper()
		got, err := SliceDigest(in, d)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return got
	}

	// Same assertion COUNT; the difference is only where a field ends.
	a := digest("A", []Assertion{
		{Subject: "a", Predicate: "b", Object: "c\nd", Origin: d},
		{Subject: "e", Predicate: "f", Object: "g", Origin: d},
	})
	b := digest("B", []Assertion{
		{Subject: "a", Predicate: "b", Object: "c", Origin: d},
		{Subject: "d\ne", Predicate: "f", Object: "g", Origin: d},
	})
	if a == b {
		t.Fatal("two slices differing only in field boundaries digested identically; the encoding is forgeable")
	}

	// Differing counts must also separate, for completeness.
	two := digest("two", []Assertion{
		{Subject: "a", Predicate: "b", Object: "c", Origin: d},
		{Subject: "d", Predicate: "e", Object: "f", Origin: d},
	})
	one := digest("one", []Assertion{
		{Subject: "a", Predicate: "b", Object: "c\nd\x1fe\x1ff", Origin: d},
	})
	if two == one {
		t.Fatal("a two-assertion slice digested identically to the merged single assertion")
	}
}

// Finding 3 (P2): a multi-valued predicate must not lose proofs to ordering.
//
// A subject with two requiresTest edges: depending on the FIRST must hold, and
// must hold under either serialization order.
func TestCarryForwardHonoursEveryValueOfAMultiValuedPredicate(t *testing.T) {
	const sensei, services = Domain("sensei"), Domain("services")

	first := Assertion{Subject: "inv.x", Predicate: "requiresTest", Object: "TestA", Origin: services}
	second := Assertion{Subject: "inv.x", Predicate: "requiresTest", Object: "TestB", Origin: services}
	own := Assertion{Subject: "inv.x", Predicate: "notedBy", Object: "sensei", Origin: sensei}

	deps := []Dependency{{On: first, Digest: DigestOf(first)}}

	for _, order := range []struct {
		name  string
		after []Assertion
	}{
		{"first then second", []Assertion{own, first, second}},
		{"second then first", []Assertion{own, second, first}},
	} {
		ok, why := CarryForward([]Assertion{own, first, second}, order.after, sensei, deps)
		if !ok {
			t.Fatalf("%s: an unchanged dependency was reported lost: %s", order.name, why)
		}
	}
}

// Finding 4 (P2): malformed proof metadata must fail closed, not panic.
func TestCarryForwardRefusesMalformedDependencyDigestsWithoutPanicking(t *testing.T) {
	const sensei, services = Domain("sensei"), Domain("services")

	dep := Assertion{Subject: "inv.x", Predicate: "requiresTest", Object: "TestA", Origin: services}
	own := Assertion{Subject: "inv.x", Predicate: "notedBy", Object: "sensei", Origin: sensei}
	corpus := []Assertion{own, dep}

	for _, bad := range []string{"", "abc", "0123456789ab"} {
		ok, why := CarryForward(corpus, corpus, sensei, []Dependency{{On: dep, Digest: bad}})
		if ok {
			t.Fatalf("digest %q: carry-forward accepted a pin that matches nothing live", bad)
		}
		if why == "" {
			t.Fatalf("digest %q: refused without stating why", bad)
		}
	}
}
