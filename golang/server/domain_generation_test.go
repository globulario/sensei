// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/graphgeneration"
)

// fixedDomainGeneration establishes one identity, for its own domain only.
//
// It refuses every other domain the way a real record would: a publication that
// proved attribution for one domain has proved nothing about another.
func fixedDomainGeneration(id graphgeneration.Identity) domainGenerationResolver {
	return func(_ context.Context, domain string) (graphgeneration.Identity, bool, string) {
		if domain != id.Domain {
			return graphgeneration.Identity{}, false, "this fixture establishes an identity only for " + id.Domain
		}
		return id, true, ""
	}
}

// mislabeledDomainGeneration returns one identity for ANY domain asked.
//
// It is a NEGATIVE CONTROL, not a realistic record. The realistic fixture above
// refuses a foreign domain before the handler ever compares anything, which would
// leave the handler's own scope check unreached and therefore unproven — a
// handler that blindly trusted whatever its resolver returned would pass every
// other test in this file.
func mislabeledDomainGeneration(id graphgeneration.Identity) domainGenerationResolver {
	return func(_ context.Context, _ string) (graphgeneration.Identity, bool, string) {
		return id, true, ""
	}
}

// THE PROPERTY THAT MATTERS MOST IN THIS CHANGE.
//
// A tag-derived slice digest is present, per-domain, and specific-looking. It is
// the obvious value to hand back as "this domain's generation", and handing it
// back would let the server declare an identity no publication ever proved.
//
// The refusal must survive a proof set that looks entirely healthy: live
// publication, domain covered, closure proven, slice digest recorded.
func TestATagDerivedSliceDigestIsNotADomainGeneration(t *testing.T) {
	const domain = "github.com/globulario/sensei-code"
	writeProofSet(t, liveDigest, map[string]graphgeneration.DomainProof{
		domain: proven(domain, liveDigest),
	})

	s := newServer(nil)
	s.oxigraphQueryURL = proofSetStoreURL

	id, established, why := publishedDomainGeneration(s, domain)
	if established {
		t.Fatalf("a tag-derived slice digest was promoted to a domain generation: %s", id)
	}
	if id.Established() {
		t.Fatalf("a refusal still produced a usable identity: %s", id)
	}
	// The operator must be told WHICH value was refused and why it is not an
	// identity, or the refusal reads as a missing feature rather than a finding.
	for _, want := range []string{"tag-derived", "view over the", "default graph", domain} {
		if !strings.Contains(why, want) {
			t.Fatalf("the refusal does not say %q: %s", want, why)
		}
	}
}

// The three ways of having no domain identity are distinct, because an operator
// tells a misconfiguration from the migration's state by which one they get.
// Collapsing them into one message would make those indistinguishable.
func TestTheWaysOfHavingNoDomainIdentityAreDistinguishable(t *testing.T) {
	const domain = "github.com/globulario/sensei-code"

	t.Run("this server is bound to no store", func(t *testing.T) {
		s := newServer(nil)
		_, established, why := publishedDomainGeneration(s, domain)
		if established {
			t.Fatal("a server bound to no store established a domain identity")
		}
		if !strings.Contains(why, "not bound to a store") {
			t.Fatalf("why=%q", why)
		}
	})

	t.Run("the store has published nothing", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		s := newServer(nil)
		s.oxigraphQueryURL = proofSetStoreURL
		_, established, why := publishedDomainGeneration(s, domain)
		if established {
			t.Fatal("a store with no proof set established a domain identity")
		}
		if !strings.Contains(why, "published no proof set") {
			t.Fatalf("why=%q", why)
		}
	})

	t.Run("the publication does not mention this domain", func(t *testing.T) {
		writeProofSet(t, liveDigest, map[string]graphgeneration.DomainProof{
			"github.com/globulario/services": proven("github.com/globulario/services", liveDigest),
		})
		s := newServer(nil)
		s.oxigraphQueryURL = proofSetStoreURL
		_, established, why := publishedDomainGeneration(s, domain)
		if established {
			t.Fatal("a domain absent from the publication established an identity")
		}
		if !strings.Contains(why, "no record at all") || !strings.Contains(why, domain) {
			t.Fatalf("why=%q", why)
		}
	})

	t.Run("an unnamed domain", func(t *testing.T) {
		s := newServer(nil)
		s.oxigraphQueryURL = proofSetStoreURL
		_, established, why := publishedDomainGeneration(s, "   ")
		if established {
			t.Fatal("an unnamed domain established an identity")
		}
		if !strings.Contains(why, "unnamed domain") {
			t.Fatalf("why=%q", why)
		}
	})
}

// The production shape — no injected resolver — establishes nothing. This is the
// test that says out loud that commissioning still refuses after this change,
// and it is the one that must start failing when publication learns to write a
// domain's graph under that domain's authority.
func TestTheProductionResolverEstablishesNoDomainIdentityToday(t *testing.T) {
	const domain = "github.com/globulario/sensei-code"
	writeProofSet(t, liveDigest, map[string]graphgeneration.DomainProof{
		domain: proven(domain, liveDigest),
	})

	s := newServer(nil) // no domainGeneration hook: the production shape
	s.oxigraphQueryURL = proofSetStoreURL

	if _, established, _ := s.resolveDomainGeneration(context.Background(), domain); established {
		t.Fatal("the production path established a domain-scoped identity; if publication now proves " +
			"domain attribution, this test should be replaced by one that asserts WHICH identity it establishes")
	}
}

// The hook is consulted when set, so the reader's full contract stays reachable
// and under test while the production path refuses.
func TestAnInjectedDomainGenerationIsUsed(t *testing.T) {
	const domain = "github.com/globulario/sensei-code"
	want, err := graphgeneration.DomainGeneration(domain, liveDigest)
	if err != nil {
		t.Fatal(err)
	}

	s := newServer(nil)
	s.domainGeneration = fixedDomainGeneration(want)

	got, established, why := s.resolveDomainGeneration(context.Background(), domain)
	if !established {
		t.Fatalf("the injected identity was not used: %s", why)
	}
	if err := got.Satisfies(want); err != nil {
		t.Fatalf("the resolver returned a different identity: %v", err)
	}
}
