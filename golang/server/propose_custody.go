// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture/packcustody"
	"github.com/globulario/sensei/golang/architecture/repodomain"
	"github.com/globulario/sensei/golang/propose"
)

// foreignDomainClaim reports why a proposal's repository-domain claim cannot be
// honoured by this server's write path, or "" when it can.
//
// The oracle is the WRITE PATH's own governed identity — `repository.domain` in
// the owning checkout's config — compared against what the request claims. It is
// deliberately not the reverse: the request cannot be allowed to establish which
// repository a directory belongs to, or the claim would be verifying itself.
// This is the same rule packcustody applies to publication custody: a document's
// own assertion about its owner is not evidence of its owner.
//
// Only a claim that PARSES as a repository domain is checked. `domain: shared`
// and other scope vocabulary are not repository-identity claims and are none of
// this function's business.
func foreignDomainClaim(awarenessDir string, pr propose.Request) string {
	claimed := strings.TrimSpace(pr.Domain)
	if claimed == "" {
		claimed = strings.TrimSpace(pr.Repo)
	}
	if claimed == "" {
		return "" // no claim to honour; unchanged behaviour
	}
	if repodomain.Validate(claimed) != nil {
		return "" // not a repository-domain claim
	}

	root, ok := packcustody.ProjectRootFor(awarenessDir)
	if !ok {
		return fmt.Sprintf(
			"request names repository domain %q, but the configured write path (%s) is not inside a Sensei checkout, "+
				"so the repository that owns this review queue cannot be established; "+
				"filing the proposal anyway would put it in an unknown repository's queue",
			claimed, awarenessDir)
	}

	configured, err := repodomain.Configured(root)
	if err != nil {
		// A malformed identity is not an absent one, and neither grants custody.
		return fmt.Sprintf(
			"request names repository domain %q, but this server's write path (%s) has an unusable repository identity: %v",
			claimed, root, err)
	}
	if configured == "" {
		return fmt.Sprintf(
			"request names repository domain %q, but the checkout that owns this review queue (%s) declares no "+
				"repository.domain, so the claim cannot be verified; configure repository.domain in its "+
				"%s, or omit the domain to file against this queue unqualified",
			claimed, root, repodomain.ConfigPath(root))
	}
	if configured != claimed {
		return fmt.Sprintf(
			"request names repository domain %q but this server writes into %q (%s); refusing to file a proposal "+
				"into another repository's review queue — point the server at %s's awareness directory, or "+
				"submit the proposal to the server that owns it",
			claimed, configured, root, claimed)
	}
	return ""
}
