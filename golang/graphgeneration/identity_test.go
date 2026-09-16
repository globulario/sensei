// SPDX-License-Identifier: AGPL-3.0-only

package graphgeneration

import (
	"errors"
	"strings"
	"testing"
)

const (
	testDomainA = "github.com/globulario/sensei-code"
	testDomainB = "github.com/globulario/services"
	testDigest  = "c0b660fc42a50c4be4741592de178dfff3216c9b873d455c37cc864e27f01705"
	otherDigest = "5d0eb2f4eb8500000000000000000000000000000000000000000000000000ff"
)

func mustDomain(t *testing.T, domain, digest string) Identity {
	t.Helper()
	id, err := DomainGeneration(domain, digest)
	if err != nil {
		t.Fatalf("DomainGeneration(%q): %v", domain, err)
	}
	return id
}

// THE CENTRAL LAW, and the one a single-domain store cannot expose.
//
// Every identity in these cases carries the SAME digest. If scope were compared
// after the digest — or not at all — each of them would be accepted. That is not
// a hypothetical ordering mistake: on a store serving one domain the whole-store
// digest and the domain's digest really are the same value, so a digest-only
// implementation passes every test such a store can write.
func TestAnIdentityWithTheSameDigestStillCannotAnswerADifferentQuestion(t *testing.T) {
	for name, c := range map[string]struct {
		have Identity
		want Identity
	}{
		"a whole-store generation cannot answer for a domain": {
			have: StoreGeneration(testDigest),
			want: mustDomain(t, testDomainA, testDigest),
		},
		"a domain generation cannot answer for the whole store": {
			have: mustDomain(t, testDomainA, testDigest),
			want: StoreGeneration(testDigest),
		},
		"one domain's generation cannot answer for another's": {
			have: mustDomain(t, testDomainA, testDigest),
			want: mustDomain(t, testDomainB, testDigest),
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := c.have.Satisfies(c.want)
			if err == nil {
				t.Fatal("an identity of one thing satisfied a request for another because their digests matched")
			}
			var mismatch *ReferentMismatchError
			if !errors.As(err, &mismatch) {
				t.Fatalf("err is %T (%v), want a referent mismatch; a scope error reported as a staleness "+
					"invites the wrong remedy", err, err)
			}
			// The message must say the digests agreeing is not agreement,
			// because that is the reasoning a reader would otherwise apply.
			if !strings.Contains(err.Error(), "not agreement") {
				t.Fatalf("the refusal does not explain that equal digests are not agreement: %v", err)
			}
		})
	}
}

// The matching case still matches, and transport noise is not identity.
func TestAMatchingIdentitySatisfiesAcrossDigestCaseAndSpacing(t *testing.T) {
	want := mustDomain(t, testDomainA, testDigest)
	have := mustDomain(t, testDomainA, "  "+strings.ToUpper(testDigest)+"  ")

	if err := have.Satisfies(want); err != nil {
		t.Fatalf("an identical identity did not satisfy itself: %v", err)
	}
	if err := StoreGeneration(strings.ToUpper(testDigest)).Satisfies(StoreGeneration(testDigest)); err != nil {
		t.Fatalf("whole-store: %v", err)
	}
}

// Same referent, different generation, IS a staleness — and must be reported as
// one, because its remedy (republish or re-read) is not the scope remedy.
func TestTheSameReferentAtADifferentGenerationIsAStaleness(t *testing.T) {
	err := mustDomain(t, testDomainA, otherDigest).Satisfies(mustDomain(t, testDomainA, testDigest))
	if err == nil {
		t.Fatal("two different generations of one domain were treated as the same graph")
	}
	var stale *DigestMismatchError
	if !errors.As(err, &stale) {
		t.Fatalf("err is %T (%v), want a digest mismatch", err, err)
	}
}

// A prefix is a different string. Accepting one would let a short coincidence
// pass for a generation.
func TestADigestPrefixIsNotTheGeneration(t *testing.T) {
	short := mustDomain(t, testDomainA, testDigest[:12])
	full := mustDomain(t, testDomainA, testDigest)

	if err := short.Satisfies(full); err == nil {
		t.Fatal("a 12-character prefix satisfied the full generation")
	}
	if err := full.Satisfies(short); err == nil {
		t.Fatal("the full generation satisfied a request for a prefix of it")
	}
}

// Absence is reported as absence. "Nobody has said what this is" and "two
// sources disagree" have different remedies and must not share a message.
func TestAnUnestablishedIdentitySatisfiesNothing(t *testing.T) {
	err := Identity{}.Satisfies(mustDomain(t, testDomainA, testDigest))
	if err == nil {
		t.Fatal("a zero identity satisfied a real request")
	}
	var unestablished *UnestablishedIdentityError
	if !errors.As(err, &unestablished) {
		t.Fatalf("err is %T (%v), want an unestablished-identity error", err, err)
	}
	if !strings.Contains(err.Error(), "absence of anything to agree with") {
		t.Fatalf("absence was not reported as absence: %v", err)
	}
}

// An identity that does not state its scope is refused rather than defaulted.
// Defaulting is the mechanism the whole file exists to remove.
func TestAnIdentityMustStateItsScope(t *testing.T) {
	if err := (Identity{Digest: testDigest}).Validate(); err == nil {
		t.Fatal("an identity with no scope validated; it would then default to one")
	}
	if err := (Identity{Scope: Scope("store"), Digest: testDigest}).Validate(); err == nil {
		t.Fatal("an unknown scope validated")
	}
}

// The conflation this package exists to prevent, caught inside a single value:
// a whole-store identity that also names a domain.
func TestAWholeStoreIdentityCannotAlsoNameADomain(t *testing.T) {
	bad := Identity{Scope: ScopeWholeStore, Domain: testDomainA, Digest: testDigest}
	if err := bad.Validate(); err == nil {
		t.Fatal("a whole-store generation was allowed to name a domain")
	}
}

// A non-canonical spelling addresses a DIFFERENT graph, so minting an identity
// for it would vouch for a graph nobody publishes to. Refused at construction,
// before any comparison can be attempted.
func TestADomainGenerationRefusesANoncanonicalDomain(t *testing.T) {
	for _, domain := range []string{
		"",
		"   ",
		"GitHub.com/globulario/sensei-code",         // case
		"https://github.com/globulario/sensei-code", // scheme
		"github.com",                                // host with no path
		"github.com/globulario/sensei-code?x=1",     // query
	} {
		if id, err := DomainGeneration(domain, testDigest); err == nil {
			t.Fatalf("domain %q was accepted and produced %s", domain, id)
		}
	}
}

// A digest with no content identifies nothing, at either scope.
func TestAGenerationWithNoDigestIdentifiesNothing(t *testing.T) {
	if err := StoreGeneration("   ").Validate(); err == nil {
		t.Fatal("a whole-store identity with a blank digest validated")
	}
	if _, err := DomainGeneration(testDomainA, ""); err == nil {
		t.Fatal("a domain identity with a blank digest was constructed")
	}
}

// PublishedDomain IS NOT A SCOPE.
//
// A publication records which domain's build produced the whole-store
// generation; every other domain in the store was carried forward, not rebuilt.
// Reading that field as a scope would promote a whole-store digest into one
// domain's identity by nothing more than which build happened to run — the exact
// silent migration this PR must not perform.
func TestAPublishedGenerationIsWholeStoreScopedEvenThoughItNamesTheBuiltDomain(t *testing.T) {
	gen := Generation{MarkerDigest: testDigest, MarkerIRI: "urn:x", TripleCount: 42, PublishedDomain: testDomainA}

	id := gen.Identity()
	if id.Scope != ScopeWholeStore {
		t.Fatalf("scope=%q, want %q: the domain that ran the build is provenance, not attribution", id.Scope, ScopeWholeStore)
	}
	if id.Domain != "" {
		t.Fatalf("the whole-store identity adopted the building domain %q as its scope", id.Domain)
	}
	// And therefore it still cannot answer for that very domain.
	if err := id.Satisfies(mustDomain(t, testDomainA, testDigest)); err == nil {
		t.Fatal("the generation built BY a domain answered a request FOR that domain's graph; " +
			"the store's other domains were carried forward, not rebuilt")
	}
}

// The rendering names the scope. A bare digest in a message is how a reader
// concludes two records agree when they answer different questions.
func TestTheRenderingNamesTheScopeNotOnlyTheDigest(t *testing.T) {
	if got := StoreGeneration(testDigest).String(); !strings.Contains(got, "whole-store") {
		t.Fatalf("whole-store identity rendered as %q", got)
	}
	got := mustDomain(t, testDomainA, testDigest).String()
	if !strings.Contains(got, testDomainA) {
		t.Fatalf("domain identity rendered as %q without naming its domain", got)
	}
}
