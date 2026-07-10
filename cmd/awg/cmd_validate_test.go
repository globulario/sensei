// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeValidateFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func countSeverity(findings []validateFinding, severity string) int {
	n := 0
	for _, f := range findings {
		if f.Severity == severity {
			n++
		}
	}
	return n
}

func hasCheck(findings []validateFinding, check string) bool {
	for _, f := range findings {
		if f.Check == check {
			return true
		}
	}
	return false
}

func TestDoValidate_LocalRepoOwnedRefsStayErrors(t *testing.T) {
	root := t.TempDir()
	writeValidateFile(t, root, "docs/awareness/architecture/components.yaml", `
components:
  - id: component.awg.test
    name: TestComponent
    depends_on:
      - component.missing.local
    source_files:
      - golang/server/missing.go
`)
	writeValidateFile(t, root, "docs/intent/local.yaml", `
id: local.intent
level: principle
title: Local intent
related_to:
  - missing.intent
`)

	report, err := doValidate(root, []string{
		filepath.Join(root, "docs/awareness"),
		filepath.Join(root, "docs/intent"),
	}, nil, []string{root}, validateScopeLocal)
	if err != nil {
		t.Fatalf("doValidate: %v", err)
	}
	if countSeverity(report.Findings, "error") == 0 {
		t.Fatal("expected repo-local defects to remain hard errors")
	}
	if !hasCheck(report.Findings, "dangling_component_ref") {
		t.Fatalf("expected dangling_component_ref, got %+v", report.Findings)
	}
	if !hasCheck(report.Findings, "missing_source_file") {
		t.Fatalf("expected missing_source_file, got %+v", report.Findings)
	}
}

func TestDoValidate_GenericExternalRefsWarnInLocalScope(t *testing.T) {
	root := t.TempDir()
	writeValidateFile(t, root, "docs/awareness/generic/patterns.yaml", `
patterns:
  - id: pattern.external.reference
    title: External reference
    definition: Shared corpus may reference external rules.
    related_invariants:
      - external.missing.invariant
`)
	writeValidateFile(t, root, "docs/awareness/generic/invariants.yaml", `
invariants:
  - id: generic.external.file
    title: Shared external file
    severity: warning
    protects:
      files:
        - golang/node_agent/node_agent_server/runtime_proof.go
`)

	report, err := doValidate(root, []string{filepath.Join(root, "docs/awareness")}, nil, []string{root}, validateScopeLocal)
	if err != nil {
		t.Fatalf("doValidate: %v", err)
	}
	if countSeverity(report.Findings, "error") != 0 {
		t.Fatalf("expected no hard errors in local scope for shared/generic refs, got %+v", report.Findings)
	}
	if !hasCheck(report.Findings, "external_dangling_invariant_ref") {
		t.Fatalf("expected external_dangling_invariant_ref, got %+v", report.Findings)
	}
	if !hasCheck(report.Findings, "external_missing_source_file") {
		t.Fatalf("expected external_missing_source_file, got %+v", report.Findings)
	}
}

func TestDoValidate_GenericExternalRefsFailInFullScope(t *testing.T) {
	root := t.TempDir()
	writeValidateFile(t, root, "docs/awareness/generic/patterns.yaml", `
patterns:
  - id: pattern.external.reference
    title: External reference
    definition: Shared corpus may reference external rules.
    related_invariants:
      - external.missing.invariant
`)

	report, err := doValidate(root, []string{filepath.Join(root, "docs/awareness")}, nil, []string{root}, validateScopeFull)
	if err != nil {
		t.Fatalf("doValidate: %v", err)
	}
	if countSeverity(report.Findings, "error") == 0 {
		t.Fatalf("expected full scope to hard-fail unresolved external refs, got %+v", report.Findings)
	}
	if !hasCheck(report.Findings, "external_dangling_invariant_ref") {
		t.Fatalf("expected external_dangling_invariant_ref, got %+v", report.Findings)
	}
}

func TestDoValidate_OffVocabSeverityIsHardError(t *testing.T) {
	root := t.TempDir()
	// One entry per off-vocab value CG-1 fixed by hand (medium/low/ERROR/HIGH/
	// warn). Each must surface as an invalid_severity error so the drift cannot
	// silently re-enter the corpus.
	writeValidateFile(t, root, "docs/awareness/invariants.yaml", `
invariants:
  - id: bad.medium
    title: medium is off-vocab under AG-native
    severity: medium
  - id: bad.low
    title: low is off-vocab
    severity: low
  - id: bad.caps
    title: case variants are defects not aliases
    severity: HIGH
  - id: bad.error
    title: ERROR is not a severity
    severity: ERROR
  - id: bad.warn
    title: warn is a typo for warning
    severity: warn
`)

	report, err := doValidate(root, []string{filepath.Join(root, "docs/awareness")}, nil, []string{root}, validateScopeLocal)
	if err != nil {
		t.Fatalf("doValidate: %v", err)
	}
	if !hasCheck(report.Findings, "invalid_severity") {
		t.Fatalf("expected invalid_severity finding, got %+v", report.Findings)
	}
	got := 0
	for _, f := range report.Findings {
		if f.Check == "invalid_severity" {
			if f.Severity != "error" {
				t.Errorf("invalid_severity must be a hard error, got %q for %s", f.Severity, f.EntityID)
			}
			got++
		}
	}
	if got != 5 {
		t.Fatalf("expected 5 invalid_severity findings (one per off-vocab value), got %d: %+v", got, report.Findings)
	}
}

func TestDoValidate_AGNativeSeverityVocabPasses(t *testing.T) {
	root := t.TempDir()
	// Every value in the AG-native closed set must validate clean.
	writeValidateFile(t, root, "docs/awareness/invariants.yaml", `
invariants:
  - id: ok.critical
    title: c
    severity: critical
  - id: ok.high
    title: h
    severity: high
  - id: ok.warning
    title: w
    severity: warning
  - id: ok.info
    title: i
    severity: info
  - id: ok.degraded
    title: d
    severity: degraded
  - id: ok.unset
    title: severity is optional
`)

	report, err := doValidate(root, []string{filepath.Join(root, "docs/awareness")}, nil, []string{root}, validateScopeLocal)
	if err != nil {
		t.Fatalf("doValidate: %v", err)
	}
	if hasCheck(report.Findings, "invalid_severity") {
		t.Fatalf("AG-native vocab (and unset severity) must pass, got %+v", report.Findings)
	}
}

func TestCheck_Strict_SelfAwareness_Green(t *testing.T) {
	code := runCheck([]string{
		"-input", filepath.Join("..", "..", "docs", "awareness"),
		"-strict",
	})
	if code != 0 {
		t.Fatalf("runCheck strict self-awareness exit = %d, want 0", code)
	}
}

func TestDoValidate_ResolvesSiblingServicesContext(t *testing.T) {
	root := t.TempDir()
	servicesRoot := filepath.Join(root, "services")
	agRoot := filepath.Join(root, "awareness-graph")

	writeValidateFile(t, agRoot, "docs/awareness/architecture/contracts.yaml", `
contracts:
  - id: contract.workflow.foreach_guard_order
    tests:
      - TestForeach_WhenGuardSkipsBeforeUnresolvedCollection
    source_files:
      - golang/workflow/engine/engine.go
`)
	writeValidateFile(t, servicesRoot, "docs/awareness/required_tests.yaml", `
required_tests:
  - id: TestForeach_WhenGuardSkipsBeforeUnresolvedCollection
    title: sibling test
`)
	writeValidateFile(t, servicesRoot, "golang/workflow/engine/engine.go", "package engine\n")

	report, err := doValidate(
		agRoot,
		[]string{filepath.Join(agRoot, "docs/awareness")},
		[]string{filepath.Join(servicesRoot, "docs/awareness")},
		[]string{agRoot, servicesRoot},
		validateScopeLocal,
	)
	if err != nil {
		t.Fatalf("doValidate: %v", err)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("expected sibling services context to resolve refs, got %+v", report.Findings)
	}
}

func TestDoValidate_TestsFieldAcceptsGeneratedTestShortName(t *testing.T) {
	root := t.TempDir()
	servicesRoot := filepath.Join(root, "services")
	agRoot := filepath.Join(root, "awareness-graph")

	writeValidateFile(t, agRoot, "docs/awareness/architecture/contracts.yaml", `
contracts:
  - id: contract.workflow.foreach_guard_order
    tests:
      - TestForeach_WhenGuardSkipsBeforeUnresolvedCollection
`)
	writeValidateFile(t, servicesRoot, "docs/awareness/generated/tests.yaml", `
required_tests:
  - id: golang/workflow/engine/foreach_substeps_test.go:TestForeach_WhenGuardSkipsBeforeUnresolvedCollection
    title: sibling generated test
`)

	report, err := doValidate(
		agRoot,
		[]string{filepath.Join(agRoot, "docs/awareness")},
		[]string{filepath.Join(servicesRoot, "docs/awareness", "generated")},
		[]string{agRoot, servicesRoot},
		validateScopeLocal,
	)
	if err != nil {
		t.Fatalf("doValidate: %v", err)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("expected generated test alias to resolve, got %+v", report.Findings)
	}
}

func TestDoValidate_GeneratedTestsWithSameFunctionNameAreNotDuplicateIDs(t *testing.T) {
	root := t.TempDir()
	writeValidateFile(t, root, "docs/awareness/generated/tests.yaml", `
required_tests:
  - id: a/handler_test.go:TestHandler
    title: handler a
  - id: b/handler_test.go:TestHandler
    title: handler b
`)

	report, err := doValidate(
		root,
		[]string{filepath.Join(root, "docs/awareness", "generated")},
		nil,
		[]string{root},
		validateScopeLocal,
	)
	if err != nil {
		t.Fatalf("doValidate: %v", err)
	}
	if hasCheck(report.Findings, "duplicate_id") {
		t.Fatalf("same short test name across files must not be a duplicate_id: %+v", report.Findings)
	}
}
