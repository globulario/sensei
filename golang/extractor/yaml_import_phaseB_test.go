// SPDX-License-Identifier: AGPL-3.0-only

package extractor_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/rdf"
)

// importDirToString is the helper shared by all Phase B tests.
// It fails the test if ImportAwarenessDir returns an error.
func importDirToString(t *testing.T, dir string) (string, *extractor.ImportReport) {
	t.Helper()
	var buf bytes.Buffer
	_, report, err := extractor.ImportAwarenessDir(dir, &buf)
	if err != nil {
		t.Fatalf("ImportAwarenessDir: %v", err)
	}
	return buf.String(), report
}

// assertValidNT fails the test if the N-Triples string fails validation.
func assertValidNT(t *testing.T, nt string) {
	t.Helper()
	if errs := extractor.ValidateNTriples(bytes.NewReader([]byte(nt))); len(errs) > 0 {
		for _, e := range errs {
			t.Errorf("NT validation: %s", e)
		}
		t.Fatalf("%d N-Triples validation errors", len(errs))
	}
}

// ── forbidden_fixes ───────────────────────────────────────────────────────────

func TestPhaseB_ForbiddenFixes_EmitsTypedNodes(t *testing.T) {
	root := makeDir(t, map[string]string{
		"forbidden_fixes.yaml": `
forbidden_fixes:
  - id: test.ff.blind_retry
    summary: Do not blindly retry on every tick.
    related_invariants:
      - convergence.no_infinite_retry
    safe_alternative: Classify before retrying.
  - id: test.ff.titled
    title: Titled forbidden fix
    reason: Because it would cause data loss.
    applies_to:
      - desired.build_id_immutable
`,
	})

	out, report := importDirToString(t, root)
	assertValidNT(t, out)

	// Both entries must be imported.
	if len(report.Imported()) != 1 {
		t.Fatalf("expected 1 imported file, got %d", len(report.Imported()))
	}
	if report.Imported()[0].Count == 0 {
		t.Fatal("expected triples > 0 from forbidden_fixes import")
	}

	// Typed as aw:ForbiddenFix.
	if !strings.Contains(out, rdf.AwNS+"ForbiddenFix") {
		t.Error("expected aw:ForbiddenFix class in output")
	}

	// IRI for first entry.
	if !strings.Contains(out, "forbiddenFix/test.ff.blind_retry") {
		t.Error("expected IRI for test.ff.blind_retry")
	}

	// Label from summary (no title).
	if !strings.Contains(out, "Do not blindly retry") {
		t.Error("expected summary as rdfs:label for first entry")
	}

	// Label from title for second entry.
	if !strings.Contains(out, "Titled forbidden fix") {
		t.Error("expected title as rdfs:label for second entry")
	}

	// Cross-reference edge to related invariant.
	if !strings.Contains(out, "convergence.no_infinite_retry") {
		t.Error("expected aw:affects edge to related invariant")
	}
}

func TestPhaseB_ForbiddenFixes_MissingIDSkipped(t *testing.T) {
	root := makeDir(t, map[string]string{
		"forbidden_fixes.yaml": `
forbidden_fixes:
  - id: ""
    summary: No ID, should be skipped.
  - id: test.ff.valid
    summary: This one is valid.
`,
	})

	out, _ := importDirToString(t, root)
	// The valid entry must appear exactly once as a typed node.
	// Count rdf:type assertions for ForbiddenFix (one per distinct node).
	typeTriple := rdf.AwNS + "ForbiddenFix> ."
	count := strings.Count(out, typeTriple)
	if count != 1 {
		t.Errorf("expected exactly 1 aw:ForbiddenFix rdf:type triple, got %d", count)
	}
	// The entry with empty ID must not produce any node.
	if strings.Contains(out, "No ID, should be skipped") {
		t.Error("entry with empty id must not emit any triples")
	}
}

// ── required_tests ────────────────────────────────────────────────────────────

func TestPhaseB_RequiredTests_EmitsTypedNodes(t *testing.T) {
	root := makeDir(t, map[string]string{
		"required_tests.yaml": `
required_tests:
  - id: test.rt.build_id_immutable
    title: Desired build_id must not change after write
    protects:
      invariants:
        - desired.build_id_immutable
      failure_modes:
        - minio.version_drift_auth_failure
  - id: test.rt.heartbeat_no_desired
    title: Heartbeat must not write desired state
    protects:
      invariants:
        - infra.heartbeat_not_desired_authority
`,
	})

	out, _ := importDirToString(t, root)
	assertValidNT(t, out)

	if !strings.Contains(out, rdf.AwNS+"Test") {
		t.Error("expected aw:Test class in output")
	}
	if !strings.Contains(out, "test/test.rt.build_id_immutable") {
		t.Error("expected IRI for test.rt.build_id_immutable")
	}
	if !strings.Contains(out, "Desired build_id must not change") {
		t.Error("expected title as rdfs:label")
	}
	// Cross-reference to invariant.
	if !strings.Contains(out, "desired.build_id_immutable") {
		t.Error("expected aw:affects edge to protected invariant")
	}
	// Cross-reference to failure mode.
	if !strings.Contains(out, "minio.version_drift_auth_failure") {
		t.Error("expected aw:affects edge to failure mode")
	}
}

func TestPhaseB_FailureModeScopedRequiredTest_RoundTripsWithoutDanglingReference(t *testing.T) {
	root := makeDir(t, map[string]string{
		"required_tests.yaml": `
required_tests:
  - id: repository:TestVA3_NonPublishedArtifact_NotInstallable
    title: Non-published artifacts must never be installable
    protects:
      failure_modes:
        - repository.skeleton_row_promoted_or_reported_installable
      files:
        - golang/repository/repository_server/version_authority_test.go
`,
		"failure_modes.yaml": `
failure_modes:
  - id: repository.skeleton_row_promoted_or_reported_installable
    title: Skeleton rows must not look installable
    severity: high
    required_tests:
      - repository:TestVA3_NonPublishedArtifact_NotInstallable
`,
	})

	out, _ := importDirToString(t, root)
	assertValidNT(t, out)

	errs, err := extractor.ValidateReferences(strings.NewReader(out))
	if err != nil {
		t.Fatalf("ValidateReferences: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("expected 0 dangling references, got %d: %v", len(errs), errs)
	}

	if !strings.Contains(out, "repository:TestVA3_NonPublishedArtifact_NotInstallable") {
		t.Fatal("expected scoped required-test id to appear in emitted triples")
	}
}

// ── contracts ─────────────────────────────────────────────────────────────────

func TestPhaseB_Contracts_EmitsTypedNodes(t *testing.T) {
	root := makeDir(t, map[string]string{
		"authority_contracts.yaml": `
version: "1"
schema: "globular.awareness.authority_contracts/v1"
contracts:
  - id: contract.test.release_authority
    domain: release_index
    kind: authority
    summary: The release-index is the release spine.
  - id: contract.test.workflow_dep
    service: workflow
    kind: dependency
    description: Workflow depends on ScyllaDB being healthy.
`,
	})

	out, _ := importDirToString(t, root)
	assertValidNT(t, out)

	if !strings.Contains(out, rdf.AwNS+"Contract") {
		t.Error("expected aw:Contract class in output")
	}
	if !strings.Contains(out, "contract/contract.test.release_authority") {
		t.Error("expected IRI for contract.test.release_authority")
	}
	if !strings.Contains(out, "The release-index is the release spine") {
		t.Error("expected summary as rdfs:label")
	}
	// Second contract uses description instead of summary.
	if !strings.Contains(out, "Workflow depends on ScyllaDB") {
		t.Error("expected description as rdfs:label for second contract")
	}
}

func TestPhaseB_Contracts_EmptyContractListProducesNoTriples(t *testing.T) {
	root := makeDir(t, map[string]string{
		"empty_contracts.yaml": `
version: "1"
schema: "test/v1"
contracts: []
`,
	})

	out, _ := importDirToString(t, root)
	if strings.Contains(out, rdf.AwNS+"Contract") {
		t.Error("empty contracts list must not emit any Contract nodes")
	}
}

// ── incidents ─────────────────────────────────────────────────────────────────

func TestPhaseB_Incident_EmitsTypedNode(t *testing.T) {
	root := makeDir(t, map[string]string{
		"INC-TEST-0001.yaml": `
incident_id: INC-TEST-0001
title: "Test incident for Phase B import"
status: RESOLVED
severity: MEDIUM
related_files:
  - golang/foo/bar.go
`,
	})

	out, _ := importDirToString(t, root)
	assertValidNT(t, out)

	if !strings.Contains(out, rdf.AwNS+"Incident") {
		t.Error("expected aw:Incident class in output")
	}
	if !strings.Contains(out, "incident/INC-TEST-0001") {
		t.Error("expected IRI for INC-TEST-0001")
	}
	if !strings.Contains(out, "Test incident for Phase B import") {
		t.Error("expected title as rdfs:label")
	}
	if !strings.Contains(out, "RESOLVED") {
		t.Error("expected status RESOLVED in output")
	}
	if !strings.Contains(out, "MEDIUM") {
		t.Error("expected severity MEDIUM in output")
	}
	// Related file should become a SourceFile node with reverse implements edge.
	if !strings.Contains(out, "golang%2Ffoo%2Fbar.go") {
		t.Error("expected source file IRI (percent-encoded path)")
	}
}

func TestPhaseB_Incident_MissingIDProducesNoTriples(t *testing.T) {
	root := makeDir(t, map[string]string{
		"no_id.yaml": `
incident_id: ""
title: No incident ID
status: RESOLVED
severity: LOW
`,
	})

	out, _ := importDirToString(t, root)
	if strings.Contains(out, rdf.AwNS+"Incident") {
		t.Error("incident with empty incident_id must not emit any node")
	}
}

// ── decisions ─────────────────────────────────────────────────────────────────

func TestPhaseB_Decisions_EmitsTypedNodes(t *testing.T) {
	root := makeDir(t, map[string]string{
		"decisions.yaml": `
decisions:
  - id: decision.test.local_success_not_global
    title: Local install success does not imply cluster convergence
    status: accepted
    rationale: The controller must confirm all nodes before marking AVAILABLE.
    related_invariants:
      - convergence.no_infinite_retry
`,
	})

	out, _ := importDirToString(t, root)
	assertValidNT(t, out)

	if !strings.Contains(out, rdf.AwNS+"Decision") {
		t.Error("expected aw:Decision class in output")
	}
	if !strings.Contains(out, "decision/decision.test.local_success_not_global") {
		t.Error("expected IRI for the decision")
	}
	if !strings.Contains(out, "accepted") {
		t.Error("expected status 'accepted' in output")
	}
	if !strings.Contains(out, "convergence.no_infinite_retry") {
		t.Error("expected aw:affects edge to related invariant")
	}
}

// ── guardrails ────────────────────────────────────────────────────────────────

func TestPhaseB_Guardrails_EmitsTypedNodes(t *testing.T) {
	root := makeDir(t, map[string]string{
		"guardrails.yaml": `
guardrails:
  - id: test.guardrail.config_seed
    title: Package install may only seed defaults if config is missing
    priority: P1
    status: DONE
    protects:
      - install.config.seed_only_if_missing
`,
	})

	out, _ := importDirToString(t, root)
	assertValidNT(t, out)

	if !strings.Contains(out, rdf.AwNS+"Guardrail") {
		t.Error("expected aw:Guardrail class in output")
	}
	if !strings.Contains(out, "guardrail/test.guardrail.config_seed") {
		t.Error("expected IRI for the guardrail")
	}
	if !strings.Contains(out, "P1") {
		t.Error("expected priority P1 as aw:severity")
	}
	if !strings.Contains(out, "DONE") {
		t.Error("expected status DONE in output")
	}
}

func TestPhaseB_Rules_EmitsTypedNodes(t *testing.T) {
	root := makeDir(t, map[string]string{
		"learning_rules.yaml": `
rules:
  - id: learning.must_be_reviewable
    summary: AI-generated awareness must remain reviewable.
    meta_principles:
      - meta.write_creates_completion_obligation
`,
	})

	out, _ := importDirToString(t, root)
	assertValidNT(t, out)

	if !strings.Contains(out, rdf.AwNS+"Guardrail") {
		t.Error("expected aw:Guardrail class in output")
	}
	if !strings.Contains(out, "guardrail/learning.must_be_reviewable") {
		t.Error("expected IRI for the rule")
	}
	if !strings.Contains(out, "AI-generated awareness must remain reviewable") {
		t.Error("expected summary as rdfs:comment")
	}
	if !strings.Contains(out, "meta.write_creates_completion_obligation") {
		t.Error("expected aw:affects edge to cited meta-principle")
	}
}

// ── patterns ──────────────────────────────────────────────────────────────────

func TestPhaseB_Patterns_EmitsTypedNodes(t *testing.T) {
	root := makeDir(t, map[string]string{
		"patterns.yaml": `
patterns:
  - id: pattern.test.persistence_gap
    title: Persistence Gap
    definition: State changes in memory but does not become durable.
`,
	})

	out, _ := importDirToString(t, root)
	assertValidNT(t, out)

	if !strings.Contains(out, rdf.AwNS+"Pattern") {
		t.Error("expected aw:Pattern class in output")
	}
	if !strings.Contains(out, "pattern/pattern.test.persistence_gap") {
		t.Error("expected IRI for the pattern")
	}
	if !strings.Contains(out, "Persistence Gap") {
		t.Error("expected title as rdfs:label")
	}
	if !strings.Contains(out, "does not become durable") {
		t.Error("expected definition as rdfs:comment")
	}
}

// ── services ──────────────────────────────────────────────────────────────────

func TestPhaseB_Services_EmitsTypedNodes(t *testing.T) {
	root := makeDir(t, map[string]string{
		"services.yaml": `
services:
  - id: etcd
    name: etcd
  - id: awareness-graph
    name: awareness-graph
`,
	})

	out, _ := importDirToString(t, root)
	assertValidNT(t, out)

	if !strings.Contains(out, rdf.AwNS+"Service") {
		t.Error("expected aw:Service class in output")
	}
	if !strings.Contains(out, "service/etcd") {
		t.Error("expected IRI for etcd service")
	}
	if !strings.Contains(out, "service/awareness-graph") {
		t.Error("expected IRI for awareness-graph service")
	}
}

func TestPhaseB_HighRiskFiles_EmitsGuardrailAndSourceFileEdges(t *testing.T) {
	root := makeDir(t, map[string]string{
		"high_risk_files.yaml": `
files:
  - golang/server/main.go
  - cmd/awareness-mcp/
`,
	})

	out, report := importDirToString(t, root)
	assertValidNT(t, out)

	if len(report.Imported()) != 1 {
		t.Fatalf("expected 1 imported file, got %d", len(report.Imported()))
	}
	if !strings.Contains(out, rdf.AwNS+"Guardrail") {
		t.Error("expected aw:Guardrail class in output")
	}
	if !strings.Contains(out, "guardrail/awareness.high_risk_files") {
		t.Error("expected guardrail node for high-risk file registry")
	}
	if !strings.Contains(out, "sourceFile/golang%2Fserver%2Fmain.go") {
		t.Error("expected source-file node for high-risk path")
	}
	if !strings.Contains(out, rdf.PropProtects) {
		t.Error("expected guardrail to protect source-file paths")
	}
}

func TestPhaseB_ActivationRules_EmitsGuardrailsAndPolicy(t *testing.T) {
	root := makeDir(t, map[string]string{
		"activation_rules.yaml": `
activation_rules:
  version: v1
  rules:
    - id: auto_briefing
      trigger: file_path
      enforcement: hook
      paths:
        - golang/server/
      tools:
        - sensei briefing --file <path>
    - id: manual_briefing
      trigger: task_concept
      enforcement: agent_judgment
      concepts:
        - authentication
  empty_policy:
    tiers:
      - tier: high_risk_target
        description: Minor edit in a high-risk directory
        action: treat_as_degraded
        announce: true
`,
	})

	out, report := importDirToString(t, root)
	assertValidNT(t, out)

	if len(report.Imported()) != 1 {
		t.Fatalf("expected 1 imported file, got %d", len(report.Imported()))
	}
	for _, want := range []string{
		"guardrail/awareness.activation_rules",
		"guardrail/activation_rule.auto_briefing",
		"guardrail/activation_rule.manual_briefing",
		"guardrail/activation_empty_policy.high_risk_target",
		"sourceFile/golang%2Fserver%2F",
		"agent_judgment",
		"treat_as_degraded",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output", want)
		}
	}
}

// ── integration: full docs/awareness with Phase B ────────────────────────────

func TestPhaseB_SelfAwareness_MoreTriplesAfterPhaseB(t *testing.T) {
	// Run against the real services docs/awareness to confirm Phase B
	// adds triples beyond Phase A's baseline.
	const docsDir = "/home/dave/Documents/github.com/globulario/services/docs/awareness"
	if _, err := os.Stat(docsDir); err != nil {
		t.Skipf("docs/awareness not found: %v", err)
	}

	var buf bytes.Buffer
	_, report, err := extractor.ImportAwarenessDir(docsDir, &buf)
	if err != nil {
		t.Skipf("docs/awareness not accessible: %v", err)
	}

	// After Phase B, imported file count must be strictly greater than Phase A (9 files).
	imported := report.Imported()
	if len(imported) <= 9 {
		t.Errorf("expected more than 9 imported files after Phase B; got %d", len(imported))
	}

	// N-Triples output must still validate.
	if errs := extractor.ValidateNTriples(bytes.NewReader(buf.Bytes())); len(errs) > 0 {
		for _, e := range errs {
			t.Errorf("validation: %s", e)
		}
		t.Fatalf("%d N-Triples validation errors after Phase B import", len(errs))
	}

	t.Logf("Phase B: %d files imported, %d skipped", len(imported), len(report.Skipped()))
}
