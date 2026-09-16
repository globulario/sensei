// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"strings"
)

// nodeDomainKind is `sensei build --domain`: the default TAGGING KIND for untagged nodes,
// `repo` or `shared`. It is NOT a governed domain name, and it exists as a defined type so
// that distinction is enforced by the compiler rather than by a reader noticing it.
//
// It was not enforced by anything, and the cost was measured twice in one command. Both
// verifyStoreOwnership and activateGeneration take a governed domain NAME and were handed
// this value:
//
//   - verifyStoreOwnership indexes reg.Domains by governed domain name. Given "repo" it found
//     no entry for itself and then found the domain that legitimately owned the target store,
//     whose key is not "repo" -- so it reported a domain's OWN store as belonging to a foreign
//     claimant and refused a correctly configured scoped build. Where no domain declared the
//     target store at all it went inert instead, which is the case the check exists for.
//   - activateGeneration hands its argument to recordActiveGeneration, which deliberately
//     leaves an UNREGISTERED domain alone and returns nil. Given "repo" the real domain's
//     ACTIVE pointer never moved, while the command printed "ACTIVE generation: <digest> for
//     repo". Law 6 activation silently not happening, reported as success.
//
// Two string variables, one of them named `domain`, in a command whose --repo flag carries the
// governed domain name. A type is the repair that a rename would only have postponed.
type nodeDomainKind string

const (
	nodeDomainKindRepo   nodeDomainKind = "repo"
	nodeDomainKindShared nodeDomainKind = "shared"
)

// validate reads this closed vocabulary BY MEMBERSHIP of the recognised set, never by excluding
// known-bad values.
//
// --domain accepted any string, so a governed domain name typed there was silently adopted as a
// tagging kind and every untagged node was tagged with it. That is the same permissive direction
// this repository has recorded five times: an unanticipated value arriving as if it were the
// strongest reading. Empty is admissible and means "no default", which is what the flag's own
// help text describes (inferred `repo` when --repo is set).
func (k nodeDomainKind) validate() error {
	switch k.normalized() {
	case "", nodeDomainKindRepo, nodeDomainKindShared:
		return nil
	}
	return fmt.Errorf("--domain %q is not a node tagging kind.\n"+
		"  --domain takes %q or %q: it is the default tag for nodes the corpus leaves untagged.\n"+
		"  To name the governed domain this build publishes for, use --repo %s",
		k.String(), nodeDomainKindRepo, nodeDomainKindShared, k.String())
}

func (k nodeDomainKind) normalized() nodeDomainKind {
	return nodeDomainKind(strings.TrimSpace(string(k)))
}

// String is the ONE conversion back to a plain string, for the graph builder's DefaultDomain
// field. Spelled as a method so every crossing of that boundary is visible in a grep, rather
// than an anonymous string(...) that reads like any other cast.
func (k nodeDomainKind) String() string { return strings.TrimSpace(string(k)) }
