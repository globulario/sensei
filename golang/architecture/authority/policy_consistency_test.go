// SPDX-License-Identifier: AGPL-3.0-only

package authority

import (
	"path/filepath"
	"runtime"
	"testing"
)

// TestEveryGrantCoversItsDomainsRequiredMutationPath is a WHOLE-POLICY
// invariant, deliberately not three pinned identifiers.
//
// A grant names the domains it authorizes and the mechanisms it permits. A
// domain names the mechanisms a mutation MUST travel through. If a grant
// authorizes a domain but permits none of that domain's required paths, the pair
// is unsatisfiable: the resolver in use today does not reject it, so it passes
// silently, while any consumer that enforces the requirement rejects every
// operation on that domain.
//
// That is how the abandonment domain shipped requiring
// mutation_path.terminal_completion while its own grant permitted only
// mutation_path.terminal_abandonment: the domain block was cloned from
// completion and its id renamed, and nothing compared the two halves.
//
// Pinning the three abandonment identifiers would have caught that instance and
// no other. This closes the contract for every pair in the policy, including
// pairs added later.
func TestEveryGrantCoversItsDomainsRequiredMutationPath(t *testing.T) {
	index, err := LoadPolicyIndex(repoRootForPolicy(t))
	if err != nil {
		t.Fatalf("load policy index: %v", err)
	}
	if len(index.AuthorityGrants) == 0 || len(index.AuthorityDomains) == 0 {
		t.Fatal("policy index is empty; this test would pass vacuously")
	}

	checked := 0
	for _, grant := range index.AuthorityGrants {
		if grant.Status != "active" {
			continue
		}
		for _, domainID := range grant.AuthorityDomainIDs {
			domain, ok := index.AuthorityDomains[domainID]
			if !ok {
				t.Errorf("grant %s authorizes domain %s, which the policy does not define", grant.ID, domainID)
				continue
			}
			required := domain.MustMutateViaIDs
			if len(required) == 0 {
				required = domain.LegacyMustMutateVia
			}
			if len(required) == 0 {
				continue // the domain constrains no mutation path
			}
			if len(grant.RequiredMechanismIDs) == 0 {
				continue // the grant constrains no mechanism
			}
			checked++
			if !anyShared(grant.RequiredMechanismIDs, required) {
				t.Errorf("grant %s authorizes domain %s but permits none of that domain's required "+
					"mutation paths:\n  grant permits: %v\n  domain requires: %v\n"+
					"the pair is unsatisfiable: any consumer enforcing the requirement rejects every "+
					"operation on this domain",
					grant.ID, domainID, grant.RequiredMechanismIDs, required)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no grant/domain pair constrained a mutation path; the invariant was never exercised")
	}
	t.Logf("checked %d grant/domain mutation-path pairs", checked)
}

func anyShared(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

func repoRootForPolicy(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}
