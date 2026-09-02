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
