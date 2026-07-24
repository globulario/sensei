// SPDX-License-Identifier: AGPL-3.0-only

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

func TestCollectYAMLFiles_ExcludesNestedCandidatesButAllowsExplicitRoot(t *testing.T) {
	root := t.TempDir()
	writeValidateFile(t, root, "docs/awareness/architecture/decisions.yaml", "decisions: []\n")
	writeValidateFile(t, root, "docs/awareness/candidates/draft.yaml", "candidates: []\n")
	writeValidateFile(t, root, "docs/awareness/generated/canonical.yaml", "components: []\n")

	awareness := filepath.Join(root, "docs/awareness")
	files, err := collectYAMLFiles([]string{awareness})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if filepath.Base(filepath.Dir(file)) == "candidates" || filepath.Base(filepath.Dir(file)) == "generated" {
			t.Fatalf("nested non-canonical tree included: %s", file)
		}
	}
	files, err = collectYAMLFiles([]string{filepath.Join(awareness, "candidates")})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "draft.yaml" {
		t.Fatalf("explicit candidates root=%v", files)
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

func TestDoValidate_LocalScopeAllowsGenericMirrorIDs(t *testing.T) {
	root := t.TempDir()
	writeValidateFile(t, root, "docs/awareness/generic/state_authority_invariants.yaml", `
invariants:
  - id: meta.shared_rule
    title: shared
`)
	writeValidateFile(t, root, "docs/awareness/meta_principles.yaml", `
invariants:
  - id: meta.shared_rule
    title: active copy
`)

	report, err := doValidate(root, []string{filepath.Join(root, "docs/awareness")}, nil, []string{root}, validateScopeLocal)
	if err != nil {
		t.Fatalf("doValidate: %v", err)
	}
	if hasCheck(report.Findings, "duplicate_id") {
		t.Fatalf("local generic mirror must not be a duplicate_id: %+v", report.Findings)
	}
}

func TestDoValidate_FullScopeFlagsGenericMirrorIDs(t *testing.T) {
	root := t.TempDir()
	writeValidateFile(t, root, "docs/awareness/generic/state_authority_invariants.yaml", `
invariants:
  - id: meta.shared_rule
    title: shared
`)
	writeValidateFile(t, root, "docs/awareness/meta_principles.yaml", `
invariants:
  - id: meta.shared_rule
    title: active copy
`)

	report, err := doValidate(root, []string{filepath.Join(root, "docs/awareness")}, nil, []string{root}, validateScopeFull)
	if err != nil {
		t.Fatalf("doValidate: %v", err)
	}
	if !hasCheck(report.Findings, "duplicate_id") {
		t.Fatalf("full scope should flag generic mirror duplicates, got %+v", report.Findings)
	}
}

func TestDoValidate_RelatedInvariantsMayResolveToIntentContracts(t *testing.T) {
	root := t.TempDir()
	writeValidateFile(t, root, "docs/awareness/invariants.yaml", `
invariants:
  - id: meta.quorum_is_quality_not_constraint
    title: quorum
    related_invariants:
      - profile.intent_is_controller_owned_placement_contract
`)
	writeValidateFile(t, root, "docs/intent/profile.intent_is_controller_owned_placement_contract.yaml", `
id: profile.intent_is_controller_owned_placement_contract
level: feature
title: Profile intent is controller-owned placement contract
intent: Profile placement remains controller owned.
`)

	report, err := doValidate(
		root,
		[]string{filepath.Join(root, "docs/awareness"), filepath.Join(root, "docs/intent")},
		nil,
		[]string{root},
		validateScopeLocal,
	)
	if err != nil {
		t.Fatalf("doValidate: %v", err)
	}
	if hasCheck(report.Findings, "dangling_invariant_ref") {
		t.Fatalf("intent-backed governing ref must resolve, got %+v", report.Findings)
	}
}

func TestDoValidate_RejectsMalformedIntentIDs(t *testing.T) {
	for _, id := range []string{"intent.", "intent..router", "intent.-router", "intent.router."} {
		t.Run(id, func(t *testing.T) {
			root := t.TempDir()
			writeValidateFile(t, root, "docs/awareness/intent_bad.yaml", `
id: `+id+`
level: constraint
title: Route contract
intent: Route contract must hold.
`)
			report, err := doValidate(root, []string{filepath.Join(root, "docs/awareness")}, nil, []string{root}, validateScopeLocal)
			if err != nil {
				t.Fatalf("doValidate: %v", err)
			}
			if !hasCheck(report.Findings, "invalid_intent_id") {
				t.Fatalf("expected invalid_intent_id for %q, got %+v", id, report.Findings)
			}
		})
	}
}

func TestDoValidate_RejectsEmptyIntentTitleAndStatement(t *testing.T) {
	root := t.TempDir()
	writeValidateFile(t, root, "docs/awareness/intent_empty.yaml", `
id: intent.empty_fields.abc123def0
level: constraint
title: "   "
intent: ""
`)
	report, err := doValidate(root, []string{filepath.Join(root, "docs/awareness")}, nil, []string{root}, validateScopeLocal)
	if err != nil {
		t.Fatalf("doValidate: %v", err)
	}
	if !hasCheck(report.Findings, "intent_title_empty") || !hasCheck(report.Findings, "intent_statement_empty") {
		t.Fatalf("expected empty title and statement checks, got %+v", report.Findings)
	}
}

func TestDoValidate_RejectsMalformedIntentCandidate(t *testing.T) {
	root := t.TempDir()
	writeValidateFile(t, root, "docs/awareness/candidates/intents.yaml", `
candidates:
  - id: intent.
    class: intent
    status: candidate
    title: ""
    statement: ""
`)
	report, err := doValidate(root, []string{filepath.Join(root, "docs/awareness", "candidates")}, nil, []string{root}, validateScopeLocal)
	if err != nil {
		t.Fatalf("doValidate: %v", err)
	}
	for _, check := range []string{"invalid_intent_id", "intent_title_empty", "intent_statement_empty"} {
		if !hasCheck(report.Findings, check) {
			t.Fatalf("expected %s, got %+v", check, report.Findings)
		}
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

func TestDoValidate_ResolvesAwarenessGraphRelativeSourcePathThroughSourceRoot(t *testing.T) {
	root := t.TempDir()
	servicesRoot := filepath.Join(root, "services")
	agRoot := filepath.Join(root, "sensei")

	writeValidateFile(t, servicesRoot, "docs/intent/awareness.runtime_evidence_must_be_fresh.yaml", `
id: awareness.runtime_evidence_must_be_fresh
level: feature
reference_files:
  - path: ../awareness-graph/golang/server/graph_freshness.go
`)
	writeValidateFile(t, agRoot, "golang/server/graph_freshness.go", "package server\n")

	report, err := doValidate(
		servicesRoot,
		[]string{filepath.Join(servicesRoot, "docs/intent")},
		nil,
		[]string{servicesRoot, agRoot},
		validateScopeLocal,
	)
	if err != nil {
		t.Fatalf("doValidate: %v", err)
	}
	if hasCheck(report.Findings, "missing_source_file") {
		t.Fatalf("expected awareness-graph relative source path to resolve, got %+v", report.Findings)
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
