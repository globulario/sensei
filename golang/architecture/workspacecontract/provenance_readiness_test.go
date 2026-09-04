// SPDX-License-Identifier: AGPL-3.0-only

package workspacecontract

import (
	"strings"
	"testing"
)

// This package composes a receipt from the v1 projection, and the projection is
// lossy about authority.
//
// It carries freshness, seed and build_provenance beside a combined
// authoritative bool. It does NOT carry the closure proof or the transaction
// certification the canonical verdict weighs. So a false bool with CURRENT
// freshness, CURRENT seed and INCOMPLETE provenance is produced by at least
// three different worlds, and nothing here can tell them apart.
//
// The rule: classification happens before information loss. A caller that still
// holds the whole MetadataResponse classifies and supplies the limitation (see
// cmd/awareness-mcp/workspace_tools.go and its integration controls). This
// package preserves what it is given and, when given nothing, says plainly that
// it cannot recover the cause.

func partialInputs() IdentityInputs {
	return IdentityInputs{
		RepositoryDomainSource: RepositoryDomainConfigured,
		RepositoryDomain:       "github.com/globulario/sensei-code",
		Revision:               "15b115b670d5b50ce524c24dddda217ab01b6fd9",
		RevisionStatus:         RevisionResolved,
		CoverageState:          coverageStateSufficient,
		GraphAuthority: &GraphAuthority{
			// The combined bool is false. Which half failed is not in here.
			Authoritative:        false,
			GraphFreshnessState:  "GRAPH_FRESHNESS_STATE_CURRENT",
			SeedState:            "SEED_STATE_CURRENT",
			BuildProvenanceState: "BUILD_PROVENANCE_STATE_INCOMPLETE",
		},
	}
}

// Given only the projection, the receipt says it cannot recover the cause
// rather than guessing one.
//
// An earlier version of this file inferred "binary_build_stamp" from the three
// projected fields. That is wrong whenever the canonical verdict refused the
// graph -- on the transaction, or on closure -- because it then reports that the
// graph answers authoritatively. A guess dressed as a specific cause is worse
// than the generic one: it sends a reader to repair the wrong thing.
func TestTheV1ProjectionAdmitsItCannotRecoverTheCause(t *testing.T) {
	id := ComposeIdentity(partialInputs())

	if id.CompositionState != CompositionPartial {
		t.Fatalf("composition is %q", id.CompositionState)
	}
	var found bool
	for _, l := range id.Limitations {
		switch l.Scope {
		case "graph_answer_authority", "binary_build_stamp":
			t.Fatalf("the v1 projection named a specific proposition it cannot know: %+v", l)
		case "graph_authority":
			found = true
			if !strings.Contains(l.Reason, "cannot be recovered from this v1 projection") {
				t.Fatalf("the limitation does not admit what it cannot know: %q", l.Reason)
			}
			if strings.Contains(l.Reason, "answers authoritatively") {
				t.Fatalf("the limitation asserts the graph answers, which it cannot know: %q", l.Reason)
			}
			if !l.Blocking {
				t.Fatal("a non-authoritative graph was reported as non-blocking")
			}
		}
	}
	if !found {
		t.Fatalf("a partial identity named no authority limitation at all: %+v", id.Limitations)
	}
}

// A classification made before the loss is preserved, and not supplemented.
//
// Two authority limitations would leave a reader two answers to one question,
// and the derived one would be the wrong answer in exactly the cases the exact
// one exists for.
func TestASuppliedClassificationIsPreservedAndNotSupplemented(t *testing.T) {
	for _, supplied := range []string{"graph_answer_authority", "binary_build_stamp"} {
		in := partialInputs()
		in.Limitations = []Limitation{{
			Source: "golang/client authority", Scope: supplied,
			Reason: "classified from the whole MetadataResponse", Blocking: true,
		}}
		id := ComposeIdentity(in)

		authority := 0
		for _, l := range id.Limitations {
			switch l.Scope {
			case "graph_authority", "graph_answer_authority", "binary_build_stamp":
				authority++
				if l.Scope != supplied {
					t.Fatalf("supplied %q and the receipt added %q beside it", supplied, l.Scope)
				}
			}
		}
		if authority != 1 {
			t.Fatalf("supplied %q and the receipt carries %d authority limitations", supplied, authority)
		}
	}
}

// Coverage remains its own dimension, narrated independently.
func TestCoverageIsStillNarratedSeparately(t *testing.T) {
	in := partialInputs()
	in.GraphAuthority.Authoritative = true
	in.CoverageState = "COVERAGE_STATE_THIN"
	id := ComposeIdentity(in)

	var found bool
	for _, l := range id.Limitations {
		if l.Scope == "coverage_state" {
			found = true
		}
		if l.Scope == "graph_authority" {
			t.Fatalf("an authoritative graph carried an authority limitation: %+v", l)
		}
	}
	if !found {
		t.Fatalf("thin coverage was not narrated: %+v", id.Limitations)
	}
}

// Both dimensions healthy composes complete. Without this the tests above would
// pass for a receipt that is never complete.
func TestAHealthyReceiptComposesComplete(t *testing.T) {
	in := partialInputs()
	in.GraphAuthority.Authoritative = true
	in.GraphAuthority.BuildProvenanceState = "BUILD_PROVENANCE_STATE_STAMPED"

	if id := ComposeIdentity(in); id.CompositionState != CompositionComplete {
		t.Fatalf("composition is %q: %+v", id.CompositionState, id.Limitations)
	}
}
