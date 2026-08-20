// SPDX-License-Identifier: AGPL-3.0-only

package factextract

import (
	"fmt"
	"testing"
)

func corroborationFixture() []normalizedInvariantFact {
	kinds := []string{"guard", "assertion", "ci_gate", "historical_removal", "documentation_claim", "write", "schema"}
	var facts []normalizedInvariantFact
	for _, kind := range kinds {
		facts = append(facts,
			normalizedInvariantFact{
				ID: fmt.Sprintf("fact.%s.match", kind), Kind: kind,
				Subject: "Store.WriteConfig", Predicate: "GUARDS_Resource", Object: "cluster/CONFIG",
			},
			normalizedInvariantFact{
				ID: fmt.Sprintf("fact.%s.miss", kind), Kind: kind,
				Subject: "other.Thing", Predicate: "does", Object: "something else",
			},
		)
	}
	return facts
}

// The index must select exactly what the unindexed definition selects, for
// every resource asked of it. A faster scan that returns a different set is a
// change in what the extractor corroborates, not an optimisation — and this is
// the function whose result decides an authority candidate's score.
func TestCorroborationIndexAgreesWithTheDirectScan(t *testing.T) {
	facts := corroborationFixture()
	index := newCorroborationIndex(facts)

	for _, resource := range []string{"cluster/config", "cluster/CONFIG", "Store.WriteConfig", "guards_resource", "absent", ""} {
		want := authorityCorroborationFacts(resource, facts)
		got := index.matching(resource)
		if len(got) != len(want) {
			t.Fatalf("resource %q: indexed %d fact(s), direct scan %d", resource, len(got), len(want))
		}
		for i := range want {
			if got[i].ID != want[i].ID {
				t.Fatalf("resource %q: position %d is %s, direct scan has %s", resource, i, got[i].ID, want[i].ID)
			}
		}
	}
}

// Only the corroborating kinds are eligible, and the index must not widen that
// set by holding facts the direct scan would have skipped.
func TestCorroborationIndexHoldsOnlyCorroboratingKinds(t *testing.T) {
	index := newCorroborationIndex(corroborationFixture())
	if len(index.facts) != len(index.haystacks) {
		t.Fatalf("index is inconsistent: %d fact(s), %d haystack(s)", len(index.facts), len(index.haystacks))
	}
	for _, f := range index.facts {
		switch f.Kind {
		case "guard", "assertion", "ci_gate", "historical_removal", "documentation_claim":
		default:
			t.Fatalf("index holds a non-corroborating kind %q", f.Kind)
		}
	}
	if len(index.facts) != 10 {
		t.Fatalf("index holds %d fact(s), want the 10 across the five corroborating kinds", len(index.facts))
	}
}
