// SPDX-License-Identifier: AGPL-3.0-only

// Package assertionprovenance defines what a published domain IS.
//
// A domain used to be identified by the subjects it tagged: every triple whose
// subject carried `aw:repo D` belonged to D's slice. That is structurally
// incapable of giving stable identities to domains that share subjects, and
// they demonstrably do. Measured on the live corpus:
//
//	173  identifiers authored in two repositories
//	 89  subjects published by both services and sensei
//	     48 forbiddenFix, 31 codeSymbol, 5 test, 3 component, 1 testSymbol, 1 invariant
//
// Those are not mistakes and not import noise — custody already excludes the
// shared meta-principle pack. They are shared architectural referents that two
// repositories legitimately say DIFFERENT things about. Under subject
// ownership, whichever domain publishes last rewrites its neighbours' slice
// digests, so no publication order yields three simultaneously proven domains.
// That was measured, not predicted: publishing sensei moved the services slice
// from 059f47624207 to 5d0eb2f4eb85 and cost services its closure proof.
//
// So identity attaches to ASSERTIONS, not to subjects:
//
//	subject identity  ≠  assertion ownership
//
// Three concepts stay separate, because collapsing any pair is what produced
// the defect:
//
//	custody      may this repository publish or change this knowledge?
//	provenance   which repository AUTHORED this assertion?      (this package)
//	closure      what must remain true for that assertion to be trusted?
//
// This package answers only the middle question.
package assertionprovenance

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Domain is a publication domain, e.g. "github.com/globulario/sensei".
type Domain string

// Assertion is one statement together with the domain that authored it.
//
// Origin is NOT derived from Subject. That derivation is the defect this
// package exists to remove: a shared subject would otherwise hand every
// assertion about it to whichever domain claimed the subject.
type Assertion struct {
	Subject   string
	Predicate string
	Object    string

	// Origin is the domain that authored this assertion. Empty means UNKNOWN,
	// and unknown provenance is never silently attributed — see Slice.
	Origin Domain
}

// canonical renders an assertion for digesting. Object is included: two domains
// saying different things about one subject are different assertions, which is
// the entire point.
func (a Assertion) canonical() string {
	return a.Subject + "\x1f" + a.Predicate + "\x1f" + a.Object
}

// ErrUnattributed reports assertions with no Origin.
//
// FAIL CLOSED. During the migration from subject tagging most assertions will
// be unattributed, and the tempting fallback -- "attribute it by subject tag" --
// is exactly the model being retired. A domain whose identity depends on
// unattributed assertions is UNPROVEN, never approximately proven.
var ErrUnattributed = errors.New("assertion has no authoring domain")

// ErrAmbiguousOwnership reports one assertion claimed by two domains.
//
// Deliberately an error rather than a resolution rule. Picking a winner would
// make domain identity depend on iteration order or on a tie-break nobody
// declared, and a silent tie-break is how the previous model became
// order-dependent in the first place.
var ErrAmbiguousOwnership = errors.New("assertion claimed by more than one domain")

// Slice returns the assertions authored by one domain, canonically ordered.
//
// It refuses rather than guesses: any unattributed assertion, and any assertion
// claimed by two domains, is an error. Callers turn that into UNPROVEN.
func Slice(all []Assertion, d Domain) ([]Assertion, error) {
	owner := map[string]Domain{}
	for _, a := range all {
		if strings.TrimSpace(string(a.Origin)) == "" {
			return nil, fmt.Errorf("%w: %s %s", ErrUnattributed, a.Subject, a.Predicate)
		}
		key := a.canonical()
		if prev, seen := owner[key]; seen && prev != a.Origin {
			return nil, fmt.Errorf("%w: %s %s claimed by %q and %q",
				ErrAmbiguousOwnership, a.Subject, a.Predicate, prev, a.Origin)
		}
		owner[key] = a.Origin
	}

	var out []Assertion
	seen := map[string]bool{}
	for _, a := range all {
		if a.Origin != d || seen[a.canonical()] {
			continue
		}
		seen[a.canonical()] = true
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].canonical() < out[j].canonical() })
	return out, nil
}

// SliceDigest identifies what a domain authored.
//
// ORDER INDEPENDENT by construction: the canonical forms are sorted before
// hashing, so serialization order, insertion order, and the order in which
// OTHER domains publish cannot move this value. That stability is the property
// the subject-based digest lacked.
func SliceDigest(all []Assertion, d Domain) (string, error) {
	slice, err := Slice(all, d)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, a := range slice {
		h.Write([]byte(a.canonical()))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Dependency is a foreign assertion a domain's own assertions rely upon.
//
// Dependencies are tracked SEPARATELY from identity. A foreign assertion never
// enters the dependent domain's digest — otherwise a neighbour editing an
// unrelated statement would change this domain's identity, which is the defect.
// It does gate carry-forward: see CarryForward.
type Dependency struct {
	On     Assertion // the foreign assertion relied upon
	Digest string    // its canonical digest when the proof was taken
}

// DigestOf is the identity of a single assertion, for dependency pinning.
func DigestOf(a Assertion) string {
	sum := sha256.Sum256([]byte(a.canonical()))
	return hex.EncodeToString(sum[:])
}

// CarryForward reports whether a domain's existing closure proof survives a new
// publication of some other domain.
//
// The rule is DEPENDENCY-SENSITIVE, NOT SUBJECT-TOLERANT. A rule of the form
// "carry forward when the differences are confined to co-authored subjects"
// was considered and rejected: it would wave through a foreign change to
// `X requiresTest TestOld -> TestNew` precisely because X is co-authored,
// hiding the semantic change that closure exists to detect.
//
// So: the domain's own authored digest must be unchanged, and every declared
// dependency must still digest to the value recorded when the proof was taken.
func CarryForward(before, after []Assertion, d Domain, deps []Dependency) (bool, string) {
	beforeDigest, err := SliceDigest(before, d)
	if err != nil {
		return false, "cannot identify the prior slice: " + err.Error()
	}
	afterDigest, err := SliceDigest(after, d)
	if err != nil {
		return false, "cannot identify the current slice: " + err.Error()
	}
	if beforeDigest != afterDigest {
		return false, fmt.Sprintf("this domain's own authored assertions changed (%s -> %s)",
			beforeDigest[:12], afterDigest[:12])
	}

	live := map[string]string{}
	for _, a := range after {
		live[a.Subject+"\x1f"+a.Predicate] = DigestOf(a)
	}
	for _, dep := range deps {
		key := dep.On.Subject + "\x1f" + dep.On.Predicate
		got, present := live[key]
		if !present {
			return false, fmt.Sprintf("a declared dependency disappeared: %s %s",
				dep.On.Subject, dep.On.Predicate)
		}
		if got != dep.Digest {
			return false, fmt.Sprintf("a declared dependency changed: %s %s (%s -> %s)",
				dep.On.Subject, dep.On.Predicate, dep.Digest[:12], got[:12])
		}
	}
	return true, "authored assertions unchanged and every declared dependency still holds"
}
