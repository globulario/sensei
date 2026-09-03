// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func inferenceOf(n int, sev func(int) string) packageInference {
	p := packageInference{
		nodes: map[string]*awarenesspb.KnowledgeNode{},
		from:  map[string]string{},
		class: map[string]string{},
	}
	for i := 0; i < n; i++ {
		iri := fmt.Sprintf("https://ex/%03d", i)
		p.nodes[iri] = &awarenesspb.KnowledgeNode{
			Id: fmt.Sprintf("inv.%03d", i), Label: "L", Severity: sev(i),
		}
		p.from[iri] = "sibling.go"
		p.class[iri] = "invariant"
	}
	return p
}

// THE SUBORDINATE SECTION MUST NOT OUTWEIGH EVERYTHING ELSE.
//
// The package-inference section was unbounded, and it is the only section whose
// size scales with how much of the PACKAGE is governed rather than with what is
// known about the file. Measured on golang/server: 128-146 entries, ~30 KB per
// briefing, near-identical for every file -- including files with 0 direct
// anchors and 0 decision-focus items, which received 30 KB about other files.
func TestTheInferredSectionIsBoundedAndSaysSo(t *testing.T) {
	const cap = 5
	out := composePackageInferenceNote(inferenceOf(cap+7, func(int) string { return "" }), cap)

	shown := strings.Count(out, "\n- [invariant")
	if shown != cap {
		t.Fatalf("rendered %d inferred entries, want the bound %d", shown, cap)
	}
	if !strings.Contains(out, "+7 further inferred anchor(s) not shown") {
		t.Fatalf("the bound was applied without saying so:\n%s", out)
	}
	// Subordination survives the bound: the reader must still be told this is
	// not about their file.
	if !strings.Contains(out, "not necessarily this file") {
		t.Error("the subordination note was lost")
	}
}

// POSITIVE CONTROL: an unbounded-enough section must NOT claim truncation, or
// the notice appears on every briefing and stops being read.
func TestAnUntruncatedInferredSectionClaimsNoOmission(t *testing.T) {
	out := composePackageInferenceNote(inferenceOf(3, func(int) string { return "" }), 5)
	if strings.Contains(out, "not shown") {
		t.Fatalf("a complete section claimed truncation:\n%s", out)
	}
	if strings.Count(out, "\n- [invariant") != 3 {
		t.Fatal("the fixture no longer renders the case under test")
	}
}

// WHAT SURVIVES THE BOUND IS DECIDED BY AN AUTHORED PROPERTY.
//
// Alphabetical-by-id ordering decided it arbitrarily. Severity is authored on
// the entry and is independent of the subject, so it ranks identically for
// every file in the package and cannot encode a hoped-for answer.
func TestTheBoundKeepsTheMostSevereAuthoredEntries(t *testing.T) {
	sev := func(i int) string {
		switch i {
		case 9:
			return "critical"
		case 8:
			return "high"
		}
		return "" // unranked: authored no severity
	}
	out := composePackageInferenceNote(inferenceOf(10, sev), 2)
	if !strings.Contains(out, "inv.009") || !strings.Contains(out, "inv.008") {
		t.Fatalf("the bound dropped the critical/high entries and kept unranked ones "+
			"that merely sort earlier by id:\n%s", out)
	}
	if strings.Contains(out, "inv.000") {
		t.Errorf("an unranked entry outranked an authored severity:\n%s", out)
	}
}

// An unavailable walk is not an empty one, and the bound must not blur that.
func TestAnUnavailableWalkStillSaysSo(t *testing.T) {
	out := composePackageInferenceNote(packageInference{Unavailable: true, Reason: "backend down"}, 5)
	if !strings.Contains(out, "unavailable") || !strings.Contains(out, "NOT evidence") {
		t.Fatalf("an unavailable inference walk no longer distinguishes itself from an empty one:\n%s", out)
	}
}

// THE DEEPEST BRIEFING OMITS NOTHING, so the instruction to ask for one is
// followable.
//
// Found by independent review on 46b775d9. The bounded section tells a reader
// to "ask for a deeper briefing to see them" — and at depth=deep there is no
// deeper depth to ask for, so a cap there promised a recovery path that did not
// exist. That is the second-order form of the defect this bound was added to
// fix: an omission that lies about how to recover from it.
func TestTheDeepestBriefingOmitsNothing(t *testing.T) {
	const n = 120 // more than any previously-capped value
	out := composePackageInferenceNote(
		inferenceOf(n, func(int) string { return "" }), deepBriefingProfile.inferredNodes)

	if got := strings.Count(out, "\n- [invariant"); got != n {
		t.Fatalf("deep rendered %d of %d inferred entries: the deepest briefing is "+
			"still bounded, so the recovery path it advertises does not exist", got, n)
	}
	if strings.Contains(out, "not shown") {
		t.Fatalf("deep claimed an omission while omitting nothing:\n%s",
			out[max(0, len(out)-300):])
	}
}

// THE PROMISE ITSELF, not one value of it, and not one list of it.
//
// Fixing deep alone would leave the same defect available to the next depth
// anyone adds. The first version of this test asserted that — and then kept its
// OWN hardcoded list of the four depths that existed that day, so a later
// `ultra` depth could be capped, omitted from the list, and leave this green
// while the guarantee was false.
//
// That is precisely the defect being generalised, recreated inside its own
// generalisation. It is fixed the only way that holds: the test iterates
// briefingDepthRegistry, which is also what production selects from, so a depth
// cannot exist without entering this proof.
func TestEveryBoundedDepthHasAnExhaustiveDepthToPointAt(t *testing.T) {
	if len(briefingDepthRegistry) == 0 {
		t.Fatal("no depths registered: this check would pass over an empty world")
	}

	var exhaustive []string
	for _, d := range briefingDepthRegistry {
		if d.profile.inferredNodes < 0 {
			t.Errorf("depth %q has a negative inferred bound (%d), which is neither "+
				"bounded nor exhaustive", d.name, d.profile.inferredNodes)
		}
		if d.profile.inferredNodes == 0 {
			exhaustive = append(exhaustive, d.name)
		}
	}

	// Every bounded depth's truncation notice tells the reader to ask for a
	// deeper briefing. That instruction is followable only if some depth
	// returns the complete section.
	if len(exhaustive) == 0 {
		t.Fatal("no registered depth returns the complete package inference, so every " +
			"truncation notice tells the reader to ask for something that does not exist")
	}

	// The profile comment says exactly one may be exhaustive. Assert the claim
	// rather than leaving prose to be trusted.
	if len(exhaustive) != 1 {
		t.Errorf("depths %v are all exhaustive; the profile documents exactly one, so "+
			"either the comment or the registry is wrong", exhaustive)
	}

	// And it must be the deepest one a caller can name, or the escape hatch is
	// unreachable by the instruction that points at it.
	if exhaustive[0] != "deep" {
		t.Errorf("the exhaustive depth is %q, but the truncation notice tells readers to "+
			"ask for a DEEPER briefing", exhaustive[0])
	}

	// An unregistered depth must normalise into the registry, never past it.
	if got := normalizeBriefingDepth("__no_such_depth__"); got != defaultBriefingDepth {
		t.Errorf("an unregistered depth normalised to %q, which is not the default", got)
	}
	if briefingProfileForDepth("__no_such_depth__") != briefingProfileForDepth(defaultBriefingDepth) {
		t.Error("an unregistered depth resolved to a profile the registry does not hold")
	}
}
