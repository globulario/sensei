// SPDX-License-Identifier: AGPL-3.0-only

package main

// One pre-mutation seam for every command that replaces a store's contents.
//
// The ownership gate was wired into runBuild alone. A census of every production path that
// PUTs to a store found four more, and only two of them were the ones the reviewer named:
//
//	cmd_build.go:287       uploadNTriples        governed, gated before this change
//	cmd_governance.go:644  uploadNTriples        --store-url, ungated
//	cmd_rebuild.go:228     reloadOxigraphStore   --oxigraph-url, ungated
//	cmd_promote.go:445     reloadOxigraphStore   pilot graph, ungated
//	cmd_audit.go:139       reloadOxigraphStore   built-in default, ungated
//
// None of the four resolves a governed domain, which is why patching the two named commands
// would have left the family half-guarded. So the check moves to the primitives themselves,
// and they now REQUIRE the caller to state what the mutation is. A caller cannot reach the
// PUT without saying whether it is publishing for a governed domain, so forgetting the check
// stops being possible -- the omission became a compile error instead of a silent hole.
//
// With no domain claimed, rule 1 of verifyStoreOwnership still applies and is the one that
// matters here: a target declared by ANOTHER domain refuses, override or not. That is the
// case where the damage lands on a domain nobody in the command mentioned. A store no domain
// claims stays writable, so disposable stores and experiments are unaffected.

import (
	"fmt"
	"os"
)

// storeMutationIntent is what a caller must state before replacing a store's contents.
//
// Domain is the governed domain this publication belongs to, or "" when the caller claims
// none. "" is not a way to skip the check: it means "this mutation is not on behalf of any
// domain", and a target owned by some other domain is refused precisely then.
type storeMutationIntent struct {
	Domain string
	// Overridden records that the operator named the endpoint at the point of use. It can
	// relax rule 2 (this domain's own declared store) and never rule 1 (somebody else's).
	Overridden bool
	// Reason names the command for the refusal message, so an operator is told which
	// publication was stopped.
	Reason string
}

// guardStoreMutation refuses a whole-store replacement that would overwrite a store the
// registry gives to another domain.
//
// An absent registry is inert -- nothing is governed, so nothing can be stranded. An
// UNREADABLE one fails closed: "no registry" and "a registry I cannot read" are different
// facts, and treating the second as the first is how an ownership check stops applying
// exactly when the registry is broken.
func guardStoreMutation(target string, intent storeMutationIntent) error {
	reg, err := LoadDomainRegistry(DefaultDomainRegistryPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("%s: refusing to replace %s: the domain registry at %s cannot be read (%v), "+
			"so whether another domain owns this store cannot be established",
			intent.reasonOr("store publication"), target, DefaultDomainRegistryPath(), err)
	}
	if err := verifyStoreOwnership(reg, intent.Domain, target, intent.Overridden); err != nil {
		return fmt.Errorf("%s: %w", intent.reasonOr("store publication"), err)
	}
	return nil
}

func (i storeMutationIntent) reasonOr(fallback string) string {
	if i.Reason == "" {
		return fallback
	}
	return i.Reason
}
