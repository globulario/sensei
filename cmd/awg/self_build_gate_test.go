// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// selfBuildScript returns the freshness gate CI runs.
func selfBuildScript(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	b, err := os.ReadFile(filepath.Join(root, "scripts", "build-awareness-graph-self.sh"))
	if err != nil {
		t.Fatalf("read self-build script: %v", err)
	}
	return string(b)
}

// THE GATE MUST CHECK WHAT IS COMMITTED, NOT ONLY WHAT IT JUST PRODUCED.
//
// --check regenerated generated/ into a TEMP directory and then compared only
// the seed. Both sides of that comparison come from the fresh scan, so the
// committed docs/awareness/generated/* never entered any comparison and could
// drift arbitrarily while the gate printed "fresh" and exited 0.
//
// Reproduced at 58c055fb twice: an annotation report reading
// `discovered_tests: 999999`, and code_symbols.yaml with a real symbol deleted.
// Both passed. `sensei build` reads docs/awareness recursively, so the committed
// files are PUBLISHED — a drifted one publishes wrong knowledge past the gate
// that exists to prevent exactly that.
//
// A SOURCE-LEVEL CHECK, deliberately. Driving the real path builds two tools and
// scans ~1700 files, which does not belong in the unit suite; and a test that
// skips when that is unavailable is not a check — the same reasoning recorded on
// TestBothStoreLoadPathsPublishTheGeneration. The behavioural proof (drift in
// each artifact fails; a clean tree passes; the report stays tolerated) is
// recorded in the campaign evidence, and this test protects the mechanism from
// silent removal.
func TestSelfBuildGateChecksCommittedGeneratedArtifacts(t *testing.T) {
	src := selfBuildScript(t)

	// REFUSE TO PASS VACUOUSLY: if the check block is renamed away, the
	// assertions below would all trivially hold on an empty search.
	const anchor = "Checking committed generated files..."
	if !strings.Contains(src, anchor) {
		t.Fatalf("anchor %q not found: this check no longer locates the comparison block", anchor)
	}

	// The load-bearing set, as scripts/build-awareness-graph.sh already decided.
	for _, name := range []string{
		"awareness_graph_code_symbols.yaml",
		"awareness_graph_code_edges.yaml",
	} {
		if !strings.Contains(src, `check_generated "`+name+`"`) {
			t.Errorf("the gate does not compare committed %s against a fresh scan, "+
				"so it can drift while the gate reports fresh", name)
		}
	}

	// The exclusion is the repository's own recorded decision, not an oversight:
	// the annotation report is "informational diagnostics, not load-bearing".
	// Widening past the standard would be a different change.
	if strings.Contains(src, `check_generated "awareness_graph_annotation_report.yaml"`) {
		t.Error("the annotation report is compared; the combined script excludes it " +
			"deliberately as informational, and this gate must not diverge from that")
	}

	// A detected difference must FAIL. A comparison whose result is discarded is
	// the defect one indirection later.
	if !strings.Contains(src, "GENERATED_STALE=true") || !strings.Contains(src, "if $GENERATED_STALE; then") {
		t.Error("drift is detected but never turned into a non-zero exit")
	}

	// An absent fresh copy must not read as agreement.
	if !strings.Contains(src, "UNCHECKED:") {
		t.Error("a missing fresh copy is treated as a clean comparison; an absent " +
			"comparison is not a passing one")
	}
}
