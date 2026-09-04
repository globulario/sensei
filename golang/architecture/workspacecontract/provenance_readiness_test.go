// SPDX-License-Identifier: AGPL-3.0-only

package workspacecontract

import (
	"strings"
	"testing"
)

// The receipt must say WHICH proposition failed.
//
// "freshness, seed, or build-provenance is not current" was true and told a
// reader nothing: the same sentence whether the graph is stale, the seed is
// behind, or the serving binary carries no source stamp. Three conditions,
// three different repairs, and one of them is not about the graph at all.

func divergentIdentity() Identity {
	return ComposeIdentity(IdentityInputs{
		RepositoryDomainSource: RepositoryDomainConfigured,
		RepositoryDomain:       "github.com/globulario/sensei-code",
		Revision:               "15b115b670d5b50ce524c24dddda217ab01b6fd9",
		RevisionStatus:         RevisionResolved,
		CoverageState:          coverageStateSufficient,
		GraphAuthority: &GraphAuthority{
			// The reproduced world, in the fields the receipt already carries:
			// answers fine, provenance unstatable.
			Authoritative:        false,
			GraphFreshnessState:  "GRAPH_FRESHNESS_STATE_CURRENT",
			SeedState:            "SEED_STATE_CURRENT",
			BuildProvenanceState: "BUILD_PROVENANCE_STATE_INCOMPLETE",
		},
	})
}

// The pair is coherent and the receipt says so: the graph answers, the
// workspace is not governance-ready, and the limitation names which.
func TestThePartialReceiptNamesThePropositionThatFailed(t *testing.T) {
	id := divergentIdentity()

	if id.CompositionState != CompositionPartial {
		t.Fatalf("composition is %q; an unstatable provenance chain is not a complete identity", id.CompositionState)
	}
	var named bool
	for _, l := range id.Limitations {
		if l.Scope == "binary_build_stamp" {
			named = true
			if !strings.Contains(l.Reason, "answers authoritatively") {
				t.Fatalf("the limitation does not say the graph is usable: %q", l.Reason)
			}
			// It says where the repair is: a property of the serving binary,
			// so rebuilding the corpus would not have moved it.
			if !strings.Contains(l.Reason, "SERVING BINARY") {
				t.Fatalf("the limitation does not say where the repair is: %q", l.Reason)
			}
			// And it must NOT claim to be about the graph's provenance. A
			// restamped binary establishes nothing about a store published
			// long ago by inputs nobody recorded.
			if !strings.Contains(l.Reason, "says nothing about which commits produced the graph") {
				t.Fatalf("the limitation overclaims what a build stamp proves: %q", l.Reason)
			}
			if !l.Blocking {
				t.Fatal("an unstatable provenance chain was reported as non-blocking")
			}
		}
		if strings.Contains(l.Reason, "freshness, seed, or build-provenance") {
			t.Fatalf("the receipt still reports the disjunction that names nothing: %q", l.Reason)
		}
	}
	if !named {
		t.Fatalf("no limitation named the failing proposition: %+v", id.Limitations)
	}
}

// A graph that cannot answer is a different limitation with a different repair.
func TestAnUnusableGraphIsReportedAsSuchRatherThanAsProvenance(t *testing.T) {
	in := divergentIdentity()
	in.GraphAuthority.GraphFreshnessState = "GRAPH_FRESHNESS_STATE_STALE"
	in.GraphAuthority.SeedState = "SEED_STATE_STALE"
	id := ComposeIdentity(IdentityInputs{
		RepositoryDomainSource: RepositoryDomainConfigured,
		RepositoryDomain:       in.Binding.RepositoryDomain,
		Revision:               "15b115b670d5b50ce524c24dddda217ab01b6fd9",
		RevisionStatus:         RevisionResolved,
		CoverageState:          coverageStateSufficient,
		GraphAuthority:         in.GraphAuthority,
	})
	for _, l := range id.Limitations {
		if l.Scope == "binary_build_stamp" {
			t.Fatalf("a graph that cannot answer was reported as a build-stamp problem: %q", l.Reason)
		}
	}
}

// Both propositions healthy composes complete. Without this the test above
// would pass for a receipt that is never complete.
func TestBothPropositionsHealthyComposesComplete(t *testing.T) {
	in := divergentIdentity()
	in.GraphAuthority.Authoritative = true
	in.GraphAuthority.BuildProvenanceState = "BUILD_PROVENANCE_STATE_STAMPED"

	id := ComposeIdentity(IdentityInputs{
		RepositoryDomainSource: RepositoryDomainConfigured,
		RepositoryDomain:       in.Binding.RepositoryDomain,
		Revision:               "15b115b670d5b50ce524c24dddda217ab01b6fd9",
		RevisionStatus:         RevisionResolved,
		CoverageState:          coverageStateSufficient,
		GraphAuthority:         in.GraphAuthority,
	})
	if id.CompositionState != CompositionComplete {
		t.Fatalf("composition is %q with both propositions healthy: %+v", id.CompositionState, id.Limitations)
	}
}
