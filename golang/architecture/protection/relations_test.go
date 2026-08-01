// SPDX-License-Identifier: AGPL-3.0-only

package protection

import (
	"strings"
	"testing"
)

const testInvariantsYAML = `
invariants:
  - id: test.example.protects_directly
    title: An example invariant
    severity: high
    protects:
      files:
        - src/core/engine.go
      enforces_files:
        - src/core/enforcer.go
      configures_files:
        - src/core/config.go
      observes_files:
        - src/core/observer.go
      may_affect_files:
        - src/core/unrelated_sibling.go
    required_tests:
      - src/core/engine_test.go:TestEngineInvariant
`

const testFailureModesYAML = `
failure_modes:
  - id: test.example.fm_protects
    title: An example failure mode
    protects:
      files:
        - src/core/failure_target.go
    required_tests:
      - src/core/failure_target_test.go:TestFailureRegression
`

func TestGovernedRelationReasons_DirectRolesOnly(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/awareness/invariants.yaml", testInvariantsYAML)
	writeFile(t, root, "docs/awareness/failure_modes.yaml", testFailureModesYAML)

	reasons, _, err := GovernedRelationReasons(root)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		"src/core/engine.go",
		"src/core/enforcer.go",
		"src/core/config.go",
		"src/core/observer.go",
		"src/core/engine_test.go",
		"src/core/failure_target.go",
		"src/core/failure_target_test.go",
	} {
		if len(reasons[path]) == 0 {
			t.Errorf("expected a governed-relation reason for %s, got none", path)
		}
	}

	// may_affect_files is the deliberate exception — the weakest, indirect
	// connection. It must NOT create a protection reason (contract §3.3:
	// only explicit DIRECT relationships are authorized).
	if len(reasons["src/core/unrelated_sibling.go"]) != 0 {
		t.Fatal("may_affect_files must not create protection — it is not a direct relationship")
	}
}

func TestGovernedRelationReasons_RequiredTestSplitsFileFromTestName(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/awareness/invariants.yaml", testInvariantsYAML)
	reasons, _, err := GovernedRelationReasons(root)
	if err != nil {
		t.Fatal(err)
	}
	rs, ok := reasons["src/core/engine_test.go"]
	if !ok || len(rs) == 0 {
		t.Fatal("required_tests entry must protect its file portion, not the raw 'file:Test' string")
	}
	if rs[0].Kind != "required_test" {
		t.Fatalf("expected kind=required_test, got %q", rs[0].Kind)
	}
}

// A non-YAML file living under docs/awareness/ (a design doc, a generated
// baseline) is a governed_source (unconditionally protected) but was never
// meant to be parsed as invariants.yaml/failure_modes.yaml — it must NOT be
// reported as malformed just because it isn't YAML at all.
func TestGovernedRelationReasons_NonYAMLGovernedSourceIsNotMalformed(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/awareness/invariants.yaml", testInvariantsYAML)
	writeFile(t, root, "docs/awareness/design_notes.md", "# Not YAML\n\nSome prose that is not valid YAML: [unterminated\n")
	writeFile(t, root, "docs/awareness/baseline.tsv", "col1\tcol2\nhttps://example.com/x\tvalue\n")

	_, malformed, err := GovernedRelationReasons(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(malformed) != 0 {
		t.Fatalf("non-YAML governed sources must never be reported as malformed, got %v", malformed)
	}
}

// contract §4/§6 correction (second review round): an invalid governed-
// relation target (one that escapes the repository) must be reported as
// malformed, not silently dropped — the prior behavior let the target just
// vanish with no diagnostic, which could hide a real authoring mistake
// behind an apparently-clean COMPLETE result.
func TestGovernedRelationReasons_InvalidTargetIsReportedMalformedNotSilentlyDropped(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/awareness/invariants.yaml", `
invariants:
  - id: test.example.invalid_target
    title: An invariant with one valid and one escaping target
    severity: high
    protects:
      files:
        - src/core/valid.go
        - ../escapes/repo.go
`)

	reasons, malformed, err := GovernedRelationReasons(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(reasons["src/core/valid.go"]) == 0 {
		t.Fatal("the valid target must still be protected")
	}
	if len(malformed) != 1 {
		t.Fatalf("expected exactly one malformed entry for the escaping target, got %v", malformed)
	}
	if !strings.Contains(malformed[0], "../escapes/repo.go") {
		t.Fatalf("expected the malformed entry to name the invalid target, got %q", malformed[0])
	}

	cov, err := Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	if cov.Status == CoverageComplete {
		t.Fatalf("an invalid governed-relation target must not allow COMPLETE, got %s (gaps=%v)", cov.Status, cov.Gaps)
	}
}
