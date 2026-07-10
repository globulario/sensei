// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleImpactInvariants() []impactInvariant {
	return []impactInvariant{
		{ID: "convergence.identity_is_build_id",
			Files:         []string{"golang/cluster_controller/cluster_controller_server/handlers_health.go"},
			RequiredTests: []string{"TestDecideVersionVerdict_BuildIdsDiffer_VersionsDiffer_Mismatch"}},
		{ID: "desired.build_id_immutable",
			Files:         []string{"golang/cluster_controller/cluster_controller_server/release_resolver.go"},
			RequiredTests: []string{"path/to/x_test.go:TestDesiredBuildIDImmutableAfterWrite"}},
		{ID: "no.tests.invariant",
			Files:         []string{"golang/foo/bar.go"},
			RequiredTests: nil},
	}
}

func TestResolveImpactTests_MapsChangedFilesToRequiredTests(t *testing.T) {
	res := resolveImpactTests(sampleImpactInvariants(), []string{
		"services/golang/cluster_controller/cluster_controller_server/handlers_health.go", // repo-prefixed
		"golang/unrelated/file.go",
	})
	if len(res.PerFile) != 1 {
		t.Fatalf("want 1 protected file matched, got %d: %v", len(res.PerFile), res.PerFile)
	}
	if len(res.UnionTests) != 1 || res.UnionTests[0] != "TestDecideVersionVerdict_BuildIdsDiffer_VersionsDiffer_Mismatch" {
		t.Errorf("union tests = %v, want the one convergence test", res.UnionTests)
	}
	if len(res.Gaps) != 0 {
		t.Errorf("no coverage gap expected, got %v", res.Gaps)
	}
}

func TestResolveImpactTests_FlagsCoverageGap(t *testing.T) {
	res := resolveImpactTests(sampleImpactInvariants(), []string{"golang/foo/bar.go"})
	if len(res.Gaps) != 1 || res.Gaps[0] != "no.tests.invariant" {
		t.Errorf("expected coverage gap for no.tests.invariant, got %v", res.Gaps)
	}
}

func TestRunRegex_AnchoredAlternationOfShortNames(t *testing.T) {
	got := runRegex([]string{"path/to/x_test.go:TestB", "TestA", "TestA"})
	if got != "^(TestA|TestB)$" {
		t.Errorf("runRegex = %q, want ^(TestA|TestB)$", got)
	}
	if runRegex(nil) != "" {
		t.Errorf("empty test set must yield empty regex")
	}
}

func TestRunRegex_SkipsNonTestGuardReferences(t *testing.T) {
	// A guard-rule reference (not a Test func) must not enter the go-test plan.
	got := runRegex([]string{"TestReal", "release_type_switch_must_have_default"})
	if got != "^(TestReal)$" {
		t.Errorf("runRegex = %q, want only the runnable TestReal", got)
	}
	if runRegex([]string{"release_type_switch_must_have_default"}) != "" {
		t.Errorf("a set of only non-test references must yield an empty plan")
	}
}

func TestParsePassedTests_FromGoTestJSON(t *testing.T) {
	dir := t.TempDir()
	results := filepath.Join(dir, "r.json")
	// A minimal go test -json stream: TestA passes, TestB fails.
	os.WriteFile(results, []byte(strings.Join([]string{
		`{"Action":"run","Test":"TestA"}`,
		`{"Action":"pass","Test":"TestA"}`,
		`{"Action":"run","Test":"TestB"}`,
		`{"Action":"fail","Test":"TestB"}`,
		`{"Action":"pass"}`, // package-level, no Test — ignored
	}, "\n")), 0o644)

	passed, err := parsePassedTests(results)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !passed["TestA"] {
		t.Errorf("TestA should be passed")
	}
	if passed["TestB"] {
		t.Errorf("TestB failed — must not be marked passed")
	}
}

func TestLoadImpactInvariants_ParsesAnchorsAndTests(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "invariants.yaml")
	os.WriteFile(p, []byte(`
invariants:
  - id: a.has_both_anchors
    implemented_by:
    - file: golang/a/impl.go
      trust: declared
    protects:
      files:
      - golang/a/protected.go
    required_tests:
    - TestA
  - id: b.no_anchor
    required_tests:
    - TestB
`), 0o644)

	invs, err := loadImpactInvariants(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// b.no_anchor has no file anchor and is dropped (cannot be impacted by a file change).
	if len(invs) != 1 || invs[0].ID != "a.has_both_anchors" {
		t.Fatalf("want only the anchored invariant, got %+v", invs)
	}
	if len(invs[0].Files) != 2 {
		t.Errorf("want both implemented_by + protects.files anchors, got %v", invs[0].Files)
	}
}
