// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// THE SERVING BINARY'S STAMP IS ABOUT THE SERVING BINARY.
//
// classifyBuildProvenance required a non-empty source_repo_commit before it
// would report STAMPED. That field is the SERVICES repo commit -- the proto
// says so, and the canonical recipe fills it from `git -C ../services`. A
// Sensei server built for any other topology has no services checkout, so no
// true value exists, and the stamp could never be complete however honestly it
// was built. Observed live: :10121 and :10122 both read INCOMPLETE.
//
// Three propositions, and only the middle one is this predicate's business:
//
//	graph provenance     -> the publication receipt certifying the generation
//	binary provenance    -> which Sensei build is serving, and when it was linked
//	services commit      -> optional legacy evidence for the embedded-graph world
//
// The third spent this whole time masquerading as part of the second. #343 made
// the first authoritative on its own, so it no longer has to.

func provenanceOf(version, buildCommit, sourceCommit string, buildTime int64) awarenesspb.BuildProvenanceState {
	return classifyBuildProvenance(&awarenesspb.MetadataResponse{
		ServerVersion:      version,
		GraphBuildCommit:   buildCommit,
		SourceRepoCommit:   sourceCommit,
		GraphBuildTimeUnix: buildTime,
	})
}

// The world this repair exists for: a Sensei server serving a project domain
// from an existing store, built from the Sensei checkout alone.
func TestAServingBinaryWithNoServicesCheckoutIsStamped(t *testing.T) {
	got := provenanceOf("0.0.6", "727b8daa8a86", "", 1788533348)

	if got != awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED {
		t.Fatalf("build provenance = %s; a binary that names its own commit and link time is "+
			"stamped, and a services SHA it has no reason to hold is not part of that", got)
	}
}

// The legacy world keeps working, and keeps carrying the extra evidence. This
// is the control that stops the test above from passing for a predicate that
// simply ignores everything.
func TestTheLegacyTopologyWithAServicesCommitIsStillStamped(t *testing.T) {
	got := provenanceOf("0.0.6", "727b8daa8a86", "b98c91eb540a", 1788533348)

	if got != awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED {
		t.Fatalf("build provenance = %s for a fully stamped legacy binary", got)
	}
}

// What the predicate still refuses. Without these, dropping the services
// requirement would look identical to dropping the predicate.
func TestAnUnidentifiedOrUndatedBinaryIsNotStamped(t *testing.T) {
	for name, got := range map[string]awarenesspb.BuildProvenanceState{
		// No commit: nothing says WHICH build is serving.
		"no build commit": provenanceOf("0.0.6", "", "b98c91eb540a", 1788533348),
		// No link time: nothing says WHEN, so "stamped but old" cannot be told
		// from "stamped just now".
		"no build time": provenanceOf("0.0.6", "727b8daa8a86", "b98c91eb540a", 0),
		// Neither.
		"neither": provenanceOf("0.0.6", "", "", 0),
		// The un-stamped sentinel the linker leaves behind.
		"dev version": provenanceOf("0.0.0-dev", "727b8daa8a86", "b98c91eb540a", 1788533348),
	} {
		if got == awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED {
			t.Fatalf("%s: reported STAMPED", name)
		}
	}
}

// The two refusals stay DISTINGUISHABLE. "Built without ldflags at all" and
// "stamped but missing its link time" are different worlds, and an operator
// fixes them differently.
func TestTheTwoRefusalsRemainDistinct(t *testing.T) {
	if got := provenanceOf("0.0.0-dev", "", "", 0); got != awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_DEV {
		t.Fatalf("an unstamped dev build reported %s, want DEV", got)
	}
	if got := provenanceOf("0.0.6", "727b8daa8a86", "", 0); got != awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_INCOMPLETE {
		t.Fatalf("a released build with no link time reported %s, want INCOMPLETE", got)
	}
}
