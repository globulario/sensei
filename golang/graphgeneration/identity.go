// SPDX-License-Identifier: AGPL-3.0-only

package graphgeneration

import (
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture/repodomain"
)

// identity.go separates two things this system has always computed and never
// distinguished: the identity of A STORE and the identity of ONE DOMAIN'S GRAPH.
//
// They were conflated because, for a single-domain store, they happen to be the
// same digest. Every "wrong graph / wrong store / stale snapshot" defect in this
// repository's history lives in that coincidence: a reader asks "what is domain
// D's graph?", receives the WHOLE STORE's generation, and the equality holds
// right up until a second domain exists.
//
// The split is deliberately a TYPE rather than a convention. A convention makes
// the caller responsible for remembering which kind of digest it is holding, and
// the entire class of defect is callers not remembering. A type makes a
// whole-store answer structurally unable to satisfy a domain-scoped question,
// EVEN WHEN THE TWO DIGESTS ARE BYTE-IDENTICAL — which is exactly the case a
// single-domain store presents, and exactly the case that tests pass under.
//
// WHAT THIS FILE DOES NOT DO. It does not create a DomainGeneration for anyone.
// A domain identity may only exist when publication can actually PROVE the
// attribution, and today publication cannot: it promotes staging into the
// DEFAULT graph, so no bytes are ever written under a domain's authority. The
// type therefore has no producer yet, and that absence is the honest state of
// the system rather than a gap to be filled by inference.

// Scope names what a generation digest is the identity OF.
//
// It is a closed vocabulary with no zero-value member on purpose. An identity
// that failed to state its scope would default to one of them, and defaulting is
// how a whole-store digest comes to answer a domain question in the first place.
type Scope string

const (
	// ScopeWholeStore identifies every triple a store serves, as one artifact.
	// This is what the seed marker has always certified and what the domain
	// registry's active_generation has always named.
	ScopeWholeStore Scope = "whole_store"
	// ScopeDomain identifies the graph of ONE publication domain.
	ScopeDomain Scope = "domain"
)

// Identity is a generation digest together with the scope it is the identity of.
//
// The pairing is the point: a bare digest is an answer with its question thrown
// away, and two different questions in this system legitimately have the same
// answer.
type Identity struct {
	// Scope is what Digest identifies. Never inferred from the other fields.
	Scope Scope
	// Domain is the canonical publication domain, set if and only if
	// Scope is ScopeDomain.
	Domain string
	// Digest is the generation digest, in whatever hashing rule the scope's
	// publisher uses. It is compared case-insensitively and never by prefix.
	Digest string
}

// StoreGeneration is the identity of a whole store: the generation the seed
// marker certifies.
//
// This is the LEGACY identity, and naming it legacy is not a deprecation. It is
// a true and useful statement about a store, it is what every existing freshness
// gate compares, and those readers are correct to consume it. What it is not is
// an answer to "what is domain D's graph".
func StoreGeneration(digest string) Identity {
	return Identity{Scope: ScopeWholeStore, Digest: strings.TrimSpace(digest)}
}

// DomainGeneration is the identity of one domain's published graph.
//
// It refuses to be constructed for a non-canonical domain spelling for the same
// reason ExportDomainGraph does: a non-canonical spelling addresses a DIFFERENT
// graph, so accepting one here would mint an identity for a graph nobody
// publishes to and make it indistinguishable from a domain that exists.
//
// CONSTRUCTING ONE IS A CLAIM THAT PUBLICATION PROVED THE ATTRIBUTION. Nothing
// in this repository can make that claim today. The constructor exists so that
// the reader which must eventually demand one can be written, tested and
// reviewed before the writer that will satisfy it.
func DomainGeneration(domain, digest string) (Identity, error) {
	id := Identity{Scope: ScopeDomain, Domain: strings.TrimSpace(domain), Digest: strings.TrimSpace(digest)}
	if err := id.Validate(); err != nil {
		return Identity{}, err
	}
	return id, nil
}

// Validate reports whether this identity is internally coherent.
func (i Identity) Validate() error {
	switch i.Scope {
	case ScopeWholeStore:
		// A whole-store identity that names a domain is the conflation this
		// file exists to prevent, written down in a single value.
		if strings.TrimSpace(i.Domain) != "" {
			return fmt.Errorf("a whole-store generation is the identity of every triple in a store and names no domain, "+
				"but this one names %q; a generation that is about one domain has scope %s", i.Domain, ScopeDomain)
		}
	case ScopeDomain:
		if err := repodomain.Validate(i.Domain); err != nil {
			return fmt.Errorf("a domain generation must name a canonical publication domain: %w", err)
		}
	default:
		return fmt.Errorf("a generation must state what it is the identity of (%s or %s); this one states %q",
			ScopeWholeStore, ScopeDomain, string(i.Scope))
	}
	if strings.TrimSpace(i.Digest) == "" {
		return fmt.Errorf("a generation with no digest identifies nothing")
	}
	return nil
}

// Established reports whether this identity is usable as an answer at all.
func (i Identity) Established() bool { return i.Validate() == nil }

// String renders the identity so a log line or an error names the SCOPE, not
// only the digest. A bare digest in a message is how a reader concludes two
// records agree when they answer different questions.
func (i Identity) String() string {
	switch i.Scope {
	case ScopeWholeStore:
		return fmt.Sprintf("whole-store generation %s", shortDigest(i.Digest))
	case ScopeDomain:
		return fmt.Sprintf("generation %s of domain %s", shortDigest(i.Digest), i.Domain)
	}
	return fmt.Sprintf("unscoped generation %s", shortDigest(i.Digest))
}

// Satisfies reports whether i can be used as the answer to a request for want.
//
// THE ORDER OF THE CHECKS IS THE CONTRACT. Referent before digest: two
// identities that describe different things are not made comparable by having
// equal digests, and on a single-domain store they WILL have equal digests. A
// digest-first implementation passes every test a single-domain fixture can
// write and fails the moment a second domain exists, which is precisely the
// defect history this package records.
func (i Identity) Satisfies(want Identity) error {
	if err := want.Validate(); err != nil {
		return fmt.Errorf("the identity being asked for is not well formed, so nothing could satisfy it: %w", err)
	}
	if !i.Established() {
		return &UnestablishedIdentityError{Want: want, Detail: i.Validate().Error()}
	}
	// MEASURED, so that this comment is not a stronger claim than the evidence.
	// Mutation testing kills the removal of this check entirely, and the removal
	// of its DOMAIN term. Both of the remaining refinements SURVIVE being
	// weakened, and for a structural reason worth writing down:
	//
	//	the SCOPE term cannot fire alone, because Validate forces a whole-store
	//	identity to carry no domain and a domain identity to carry one — so a
	//	scope difference always surfaces as a domain difference too;
	//
	//	comparing the domain EXACTLY rather than case-insensitively is equally
	//	unobservable, because construction rejects a non-canonical spelling
	//	before one could ever reach this comparison.
	//
	// Both are kept deliberately, as defence in depth: they state the intent at
	// the point of use, and they keep this comparison correct if Validate's
	// invariant is ever loosened. Neither is claimed as an independently proven
	// guard.
	if i.Scope != want.Scope || i.Domain != want.Domain {
		return &ReferentMismatchError{Want: want, Got: i}
	}
	if normalizeDigest(i.Digest) != normalizeDigest(want.Digest) {
		return &DigestMismatchError{Want: want, Got: i}
	}
	return nil
}

// normalizeDigest strips transport noise — surrounding space and digest case —
// and nothing else. It never shortens or expands: a prefix is a different
// string, and treating one as equal to its longer form would let a short
// coincidence pass for a generation.
func normalizeDigest(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// ReferentMismatchError: the identity on hand describes something other than
// what was asked about. The remedy is to obtain the right record, never to
// compare harder.
type ReferentMismatchError struct {
	Want Identity
	Got  Identity
}

func (e *ReferentMismatchError) Error() string {
	same := ""
	if normalizeDigest(e.Want.Digest) == normalizeDigest(e.Got.Digest) && strings.TrimSpace(e.Want.Digest) != "" {
		// Stated explicitly because this is the case that silently passed
		// before the scope was typed, and the case a single-domain store
		// presents by default.
		same = "\n  the two digests are identical, and that is not agreement: they are the identities of " +
			"different things, and a store serving one domain makes them equal by coincidence"
	}
	return fmt.Sprintf("graph identity scope mismatch: this is the %s, and what was asked for is the %s%s",
		e.Got, e.Want, same)
}

// DigestMismatchError: the same referent, at a different generation. Unlike a
// referent mismatch this IS a staleness, and it has a staleness remedy.
type DigestMismatchError struct {
	Want Identity
	Got  Identity
}

func (e *DigestMismatchError) Error() string {
	return fmt.Sprintf("graph generation mismatch: %s was asked for and %s is what is held; "+
		"they describe the same thing at different generations", e.Want, e.Got)
}

// UnestablishedIdentityError: there is no identity to compare at all.
//
// Kept distinct from a mismatch because "nobody has said what this is" and "two
// sources disagree about it" have different remedies and must not share a
// message — the same distinction undeclaredActiveGenerationError draws against
// activeGenerationMismatchError.
type UnestablishedIdentityError struct {
	Want   Identity
	Detail string
}

func (e *UnestablishedIdentityError) Error() string {
	return fmt.Sprintf("no graph identity is established for what was asked (%s): %s;\n"+
		"  this is not agreement, it is the absence of anything to agree with", e.Want, e.Detail)
}

// Identity is the whole-store identity of a published generation.
//
// PublishedDomain IS NOT THE SCOPE, and this method is where that is enforced.
// It records WHICH domain's build produced this whole-store generation — that is
// provenance, an answer to "who caused this" — while the digest remains the
// identity of every triple in the store, including every other domain's, which
// this publication carried forward rather than rebuilt. Reading PublishedDomain
// as a scope would promote a whole-store digest into a domain's identity by
// nothing more than the name of the build that happened to run.
func (g Generation) Identity() Identity {
	return StoreGeneration(g.MarkerDigest)
}
