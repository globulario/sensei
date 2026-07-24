// SPDX-License-Identifier: AGPL-3.0-only

package protection

import "testing"

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

	reasons, err := GovernedRelationReasons(root)
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
	reasons, err := GovernedRelationReasons(root)
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
