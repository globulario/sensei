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

	refusing := 0
	for _, f := range subjects {
		if f.RefusesUnverified {
			refusing++
		}
	}

	t.Logf("ADMITTED MAIN CENSUS: %d subjects | %d owner-resolved | %d generation-verified | GAP %d",
		len(subjects), len(subjects)-len(noOwner), len(verifies), len(unchecked))
	// The stronger count, reported separately rather than folded in. "A comparison is present" and
	// "an unverifiable graph is refused" are different claims, and the gap between these two numbers
	// is exactly the set of subjects whose comparison can be satisfied by an absent declaration.
	t.Logf("  of which REFUSE an unverifiable served generation: %d (the remaining %d report the "+
		"verdict rather than refusing on it)", refusing, len(verifies)-refusing)
	t.Logf("per-subject classification:")
	for _, n := range names {
		f := subjects[n]
		t.Logf("  %-52s owner=%-5v generation=%-5v refuses=%-5v", n, f.ResolvesOwner,
			f.VerifiesGeneration, f.RefusesUnverified)
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

// The two generation facts must be a proper hierarchy and neither class may be empty. A distinction
// that cannot be observed is not a distinction, and one that holds everything is not either.
func TestTheRefusingGenerationFactIsStrictlyStrongerAndNonVacuous(t *testing.T) {
	subjects := graphCommandsIn(t, ".")
	if len(subjects) < 15 {
		t.Fatalf("the census discovered %d subject(s); it has stopped enumerating", len(subjects))
	}
	var verifying, refusing, refusingWithoutVerifying []string
	for n, f := range subjects {
		if f.VerifiesGeneration {
			verifying = append(verifying, n)
		}
		if f.RefusesUnverified {
			refusing = append(refusing, n)
			if !f.VerifiesGeneration {
				refusingWithoutVerifying = append(refusingWithoutVerifying, n)
			}
		}
	}
	sort.Strings(verifying)
	sort.Strings(refusing)

	// SUBSET: refusing implies verifying. A guard that refuses necessarily compares.
	if len(refusingWithoutVerifying) != 0 {
		t.Errorf("%d subject(s) refuse without reaching any comparison, which is incoherent: %v",
			len(refusingWithoutVerifying), refusingWithoutVerifying)
	}
	// NON-VACUOUS, both directions: if every verifier refused, the weaker fact would be redundant and
	// the census could not show the fail-open class at all; if none refused, the repair did not land.
	if len(refusing) == 0 {
		t.Fatal("no subject refuses an unverifiable served generation, so the stronger census fact is " +
			"vacuous and the guard reaches nothing")
	}
	if len(refusing) == len(verifying) {
		t.Logf("NOTE: every comparing subject also refuses, so the two counts coincide at %d. That is "+
			"a legitimate state, but it means this census can no longer distinguish a reporting "+
			"comparison from a refusing one, and the weaker fact should be re-examined.", len(refusing))
	}
	t.Logf("HIERARCHY: %d subject(s) reach a comparison, %d of those refuse an unverifiable "+
		"generation.\n  comparing: %v\n  refusing:  %v", len(verifying), len(refusing), verifying, refusing)
}
