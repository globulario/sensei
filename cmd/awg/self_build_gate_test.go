// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRootForGate(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// runGeneratedCheck EXECUTES the comparison against two directories.
//
// The first version of this test scanned build-awareness-graph-self.sh for
// strings. Review named the hole precisely: the comparison could be disabled
// while every searched-for string remained, because a source scan cannot
// distinguish a mechanism from a mention of one. The comparison is now its own
// entry point so the real behaviour can be driven here — no tool build, no
// ~1700-file scan, no skip.
func runGeneratedCheck(t *testing.T, committed, fresh string) (int, string) {
	t.Helper()
	cmd := exec.Command(filepath.Join(repoRootForGate(t), "scripts", "check-generated-freshness.sh"), committed, fresh)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run checker: %v", err)
	}
	return code, string(out)
}

func writeGen(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for n, c := range files {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const (
	symsFile  = "awareness_graph_code_symbols.yaml"
	edgesFile = "awareness_graph_code_edges.yaml"
	reptFile  = "awareness_graph_annotation_report.yaml"
)

func genSet(syms, edges, rept string) map[string]string {
	return map[string]string{symsFile: syms, edgesFile: edges, reptFile: rept}
}

// THE GATE COMPARES WHAT IS COMMITTED, NOT WHAT IT JUST PRODUCED.
//
// --check regenerated generated/ into a temp directory and compared only the
// seed. Both sides of that comparison derive from the fresh scan, so the
// committed artifacts never entered any comparison and could drift while the
// gate printed "fresh" and exited 0. Reproduced at 58c055fb with a deleted
// symbol and with discovered_tests: 999999. `sensei build` publishes those
// committed files into the served graph, so drift there publishes wrong
// knowledge past the gate meant to prevent it.
func TestGeneratedFreshnessCheck(t *testing.T) {
	for _, tc := range []struct {
		name             string
		committed, fresh map[string]string
		wantCode         int
		wantIn           string
	}{
		{"identical trees pass", genSet("s", "e", "r"), genSet("s", "e", "r"), 0, "ok:"},
		{"drifted code symbols fail", genSet("DRIFT", "e", "r"), genSet("s", "e", "r"), 1, "STALE:     " + symsFile},
		{"drifted code edges fail", genSet("s", "DRIFT", "r"), genSet("s", "e", "r"), 1, "STALE:     " + edgesFile},
		// The repository's own decision, in scripts/build-awareness-graph.sh:
		// the annotation report is informational, not load-bearing.
		{"drifted annotation report tolerated", genSet("s", "e", "DRIFT"), genSet("s", "e", "r"), 0, "ok:"},
		// An absent comparison is not a clean one.
		{"missing fresh copy is unchecked, not clean", genSet("s", "e", "r"),
			map[string]string{edgesFile: "e"}, 1, "UNCHECKED:"},
		{"missing committed copy fails", map[string]string{edgesFile: "e"},
			genSet("s", "e", "r"), 1, "MISSING:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			c := writeGen(t, filepath.Join(base, "committed"), tc.committed)
			f := writeGen(t, filepath.Join(base, "fresh"), tc.fresh)
			code, out := runGeneratedCheck(t, c, f)
			if code != tc.wantCode {
				t.Fatalf("exit=%d want %d\n%s", code, tc.wantCode, out)
			}
			if !strings.Contains(out, tc.wantIn) {
				t.Fatalf("output does not contain %q:\n%s", tc.wantIn, out)
			}
		})
	}
}

// And the gate CI runs must actually CONSUME the checker. A correct mechanism
// nothing calls is the defect this campaign keeps finding.
func TestSelfBuildGateConsumesTheChecker(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootForGate(t), "scripts", "build-awareness-graph-self.sh"))
	if err != nil {
		t.Fatalf("read self-build script: %v", err)
	}
	src := string(b)
	if !strings.Contains(src, "scripts/check-generated-freshness.sh") {
		t.Fatal("the gate CI runs does not invoke the generated-freshness checker, " +
			"so committed artifacts are never compared")
	}
	if !strings.Contains(src, `if ! "$AG/scripts/check-generated-freshness.sh"`) {
		t.Error("the checker is invoked but its exit status is not acted on")
	}
}
