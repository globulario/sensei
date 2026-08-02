// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeAuditFixture(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAssessRepositoryTestCoverage_InlineRegistryAndMissing(t *testing.T) {
	root := t.TempDir()
	writeAuditFixture(t, root, "docs/awareness/invariants.yaml", `
invariants:
  - id: inv.inline
    severity: high
    required_tests: [pkg/x_test.go:TestX]
  - id: inv.registry
    severity: critical
  - id: inv.missing
    severity: high
  - id: inv.warning
    severity: warning
`)
	writeAuditFixture(t, root, "docs/awareness/required_tests_dogfood.yaml", `
required_tests:
  - id: test.registry
    protects:
      invariants: [inv.registry]
`)
	got, err := assessRepositoryTestCoverage(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.CriticalHigh != 3 || got.Covered != 2 || len(got.Missing) != 1 || got.Missing[0] != "[high] inv.missing" {
		t.Fatalf("unexpected assessment: %+v", got)
	}
}

func TestAssessRepositoryTestCoverage_RequiresExactRegistryBinding(t *testing.T) {
	root := t.TempDir()
	writeAuditFixture(t, root, "docs/awareness/invariants.yaml", `
invariants:
  - id: inv.exact
    severity: high
`)
	writeAuditFixture(t, root, "docs/awareness/required_tests.yaml", `
required_tests:
  - id: test.nearby
    protects:
      invariants: [inv.exact.but_different]
`)
	got, err := assessRepositoryTestCoverage(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Missing) != 1 {
		t.Fatalf("nearby ID must not manufacture coverage: %+v", got)
	}
}

func TestAssessRepositoryTestCoverage_MalformedRegistryIsVisible(t *testing.T) {
	root := t.TempDir()
	writeAuditFixture(t, root, "docs/awareness/invariants.yaml", "invariants: []\n")
	writeAuditFixture(t, root, "docs/awareness/required_tests.yaml", "required_tests: [\n")
	if _, err := assessRepositoryTestCoverage(root); err == nil {
		t.Fatal("malformed required-test registry must be reported")
	}
}
