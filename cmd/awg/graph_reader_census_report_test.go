// SPDX-License-Identifier: AGPL-3.0-only

package main

// THE MEASUREMENT. Reports what admitted main guarantees; it does not assert a target.
//
// Deliberately non-failing on the counts. The authorization is measurement, not repair, and a
// large GAP is a valid and useful result — an instrument that failed here would pressure someone
// into changing production code to make a number look right, which is the opposite of the point.
//
// It DOES fail if the census discovers nothing, because a silent zero would be indistinguishable
// from a working instrument reporting a clean tree.

import (
	"sort"
	"testing"
)

func TestReportTheAdmittedMainCensus(t *testing.T) {
	subjects := graphCommandsIn(t, ".")
	if len(subjects) == 0 {
		t.Fatal("the census discovered no production graph-reading commands at all; that is an " +
			"instrument failure, not a clean tree")
	}
	verifies, unchecked, noOwner := classifyGraphCommands(subjects)

	names := make([]string, 0, len(subjects))
	for n := range subjects {
		names = append(names, n)
	}
	sort.Strings(names)

	t.Logf("ADMITTED MAIN CENSUS: %d subjects | %d owner-resolved | %d generation-verified | GAP %d",
		len(subjects), len(subjects)-len(noOwner), len(verifies), len(unchecked))
	t.Logf("per-subject classification:")
	for _, n := range names {
		f := subjects[n]
		t.Logf("  %-52s owner=%-5v generation=%-5v", n, f.ResolvesOwner, f.VerifiesGeneration)
	}
	if len(noOwner) > 0 {
		t.Logf("NOT OWNER-RESOLVED (%d): %v", len(noOwner), noOwner)
	}
	if len(unchecked) > 0 {
		t.Logf("GAP — no served-generation verification (%d):", len(unchecked))
		for _, n := range unchecked {
			t.Logf("    %s", n)
		}
	}
}
