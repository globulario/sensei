// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.positive_control
// @awareness file_role=positive_control_attestation_for_ruleguard_rules

// Positive-control attestation for the ruleguard rules under rules/.
//
// # WHY THIS EXISTS
//
// A ruleguard rule that matches nothing reports zero findings — and so
// does a ruleguard rule that is broken, mis-scoped, or fails to load.
// The production scanner (ruleguard_scan.go) cannot tell these apart: it
// shells out to `ruleguard -rules <file>` and treats "no report lines"
// as "the code is clean." That is exactly the failure mode named by
// meta.negative_result_requires_coverage_attestation — an empty result
// whose shape is safety but whose meaning may be ignorance.
//
// This test is the coverage attestation. For every rule file under
// rules/ it runs the rule against a known-BAD fixture that the rule MUST
// flag. If the fixture fires, the rule is proven functional, so a zero-
// findings result against real code is genuine "attested clean." If the
// fixture does NOT fire, the rule is broken/ineffective and its
// production silence is "uncharted" — the test fails and names it.
//
// It already caught one dead rule: timestamp_field_must_be_qualified
// had a `Type.Is("*timestamppb.Timestamp")` clause that crashed rule
// loading, silently killing the entire rule in production.
//
// MAINTENANCE
//
//   - Add a ruleguard rule under rules/  -> add a fixture under
//     rules/testdata/positive/<rule-file-basename>/bad.go that the rule
//     flags. This test will fail until you do (coverage is enforced,
//     not optional).
//   - A rule that genuinely cannot be positive-controlled standalone
//     (e.g. needs real generated types that can't be stubbed) goes in
//     knownUncovered below WITH a reason. That is an explicit, named
//     coverage gap — not silent.
package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// knownUncovered lists rule-file basenames (without .go) that have no
// positive-control fixture yet, each with a reason. Keep this EMPTY when
// possible — an entry here is a named hole in mechanical coverage, not a
// pass. The test logs each so the gap is never silent.
var knownUncovered = map[string]string{}

// reportLine matches a ruleguard finding: "<path>.go:<line>:<col>: <id>: ...".
var reportLine = regexp.MustCompile(`\.go:\d+:\d+: \w+:`)

func TestRuleguardRulesHavePositiveControl(t *testing.T) {
	if _, err := exec.LookPath("ruleguard"); err != nil {
		t.Skip("ruleguard not on PATH — install with " +
			"`GOTOOLCHAIN=go1.25.0 go install github.com/quasilyte/go-ruleguard/cmd/ruleguard@latest`. " +
			"CI MUST install it so this attestation actually runs.")
	}

	const rulesDir = "rules"
	const fixtureBase = "rules/testdata/positive"

	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		t.Fatalf("read rules dir: %v", err)
	}

	var ruleFiles []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		ruleFiles = append(ruleFiles, e.Name())
	}
	if len(ruleFiles) == 0 {
		t.Fatal("no ruleguard rule files found under rules/")
	}

	proven := 0
	for _, rf := range ruleFiles {
		base := strings.TrimSuffix(rf, ".go")
		t.Run(base, func(t *testing.T) {
			if reason, ok := knownUncovered[base]; ok {
				t.Skipf("KNOWN UNCOVERED (named coverage gap): %s", reason)
			}

			fixtureDir := filepath.Join(fixtureBase, base)
			if fi, err := os.Stat(fixtureDir); err != nil || !fi.IsDir() {
				t.Fatalf("no positive-control fixture: expected a known-bad fixture at %s/ that rule %s must flag. "+
					"Add one, or add %q to knownUncovered with a reason.", fixtureDir, rf, base)
			}

			rulePath := filepath.Join(rulesDir, rf)
			// Run from this package dir (inside the module); ruleguard
			// resolves the fixture package via go/packages.
			cmd := exec.Command("ruleguard", "-rules", rulePath, "./"+fixtureDir+"/...")
			out, _ := cmd.CombinedOutput() // exit 1/3 on findings; parse output, don't trust code

			// Environment guard: ruleguard is installed but cannot resolve its
			// go-ruleguard/dsl package, so it can't LOAD any rule — distinct from
			// a rule that loads but is dead. This signature is never a per-rule
			// bug (which yields a different error and still fails below).
			if bytes.Contains(out, []byte("could not import")) && bytes.Contains(out, []byte("go-ruleguard/dsl")) {
				t.Skipf("ruleguard cannot resolve its go-ruleguard/dsl package in this environment "+
					"(install ruleguard with a matching Go toolchain so it can load rules); "+
					"positive-control gate not run for %s.", rf)
			}

			if !reportLine.Match(out) {
				t.Fatalf("rule %s did NOT fire on its positive-control fixture %s/.\n"+
					"A rule that cannot flag known-bad code is dead in production but reports zero "+
					"findings as if the tree were clean (meta.negative_result_requires_coverage_attestation).\n"+
					"ruleguard output:\n%s", rf, fixtureDir, string(out))
			}
		})
		proven++
	}

	t.Logf("positive-control coverage: %d rule files, %d with fixtures attested, %d named-uncovered",
		len(ruleFiles), len(ruleFiles)-len(knownUncovered), len(knownUncovered))
}
