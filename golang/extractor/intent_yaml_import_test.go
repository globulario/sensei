// SPDX-License-Identifier: Apache-2.0

package extractor_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/rdf"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// intentDir builds a temp directory with the given files and runs
// ImportAwarenessDir on it. Shared by all Phase C tests.
func intentDir(t *testing.T, files map[string]string) (string, *extractor.ImportReport) {
	t.Helper()
	var buf bytes.Buffer
	root := makeDir(t, files)
	_, report, err := extractor.ImportAwarenessDir(root, &buf)
	if err != nil {
		t.Fatalf("ImportAwarenessDir: %v", err)
	}
	return buf.String(), report
}

func intentDirWithOpts(t *testing.T, files map[string]string, opts extractor.ImportDirOptions) (string, *extractor.ImportReport) {
	t.Helper()
	var buf bytes.Buffer
	root := makeDir(t, files)
	_, report, err := extractor.ImportAwarenessDirWithOpts(root, &buf, opts)
	if err != nil {
		t.Fatalf("ImportAwarenessDirWithOpts: %v", err)
	}
	return buf.String(), report
}

// ── Phase C tests ─────────────────────────────────────────────────────────────

// TestPhaseC_Intent_NormalEmitsTypedNode is the basic smoke test: a file with
// id + level is detected as the intent schema and imported as an aw:Intent
// node with the correct rdfs:label, aw:level, aw:status, and aw:authoredIn
// triples.
func TestPhaseC_Intent_NormalEmitsTypedNode(t *testing.T) {
	out, report := intentDir(t, map[string]string{
		"api.grpc_contracts.yaml": `
id: api.grpc_contracts_are_operational_surface
level: principle
title: gRPC/proto contracts are the operational surface
intent: Services expose typed gRPC/protobuf contracts.
status: extracted_candidate
`,
	})

	assertValidNT(t, out)

	if len(report.Imported()) != 1 {
		t.Fatalf("expected 1 imported file, got %d", len(report.Imported()))
	}
	if report.Imported()[0].Schema != "intent" {
		t.Errorf("schema = %q, want intent", report.Imported()[0].Schema)
	}
	if report.Imported()[0].Count == 0 {
		t.Fatal("expected triples > 0")
	}

	// Must be typed aw:Intent.
	if !strings.Contains(out, rdf.AwNS+"Intent>") {
		t.Error("expected aw:Intent class in output")
	}
	// Principle level → aw:DesignIntent subclass.
	if !strings.Contains(out, rdf.AwNS+"DesignIntent>") {
		t.Error("expected aw:DesignIntent subclass for level=principle")
	}
	// IRI contains the intent ID.
	if !strings.Contains(out, "intent/api.grpc_contracts_are_operational_surface") {
		t.Error("expected intent IRI in output")
	}
	// Title as rdfs:label.
	if !strings.Contains(out, "gRPC/proto contracts are the operational surface") {
		t.Error("expected title as rdfs:label")
	}
	// Level as aw:level literal.
	if !strings.Contains(out, `"principle"`) {
		t.Error("expected level literal in output")
	}
	// Status.
	if !strings.Contains(out, "extracted_candidate") {
		t.Error("expected status in output")
	}
}

// TestPhaseC_Intent_HierarchyLinks pins that zooms_out_to, zooms_in_to, and
// related_to produce object-property edges between intent nodes.
func TestPhaseC_Intent_HierarchyLinks(t *testing.T) {
	out, report := intentDir(t, map[string]string{
		"child.yaml": `
id: workflow.source_of_operational_truth
level: mechanism
title: Workflow is the source of operational truth
intent: All cluster mutations flow through the workflow engine.
zooms_out_to:
  - globular.vision.ai_operable_cluster
zooms_in_to:
  - workflow.idempotency_contract
related_to:
  - doctor.findings_are_operator_language
`,
	})

	assertValidNT(t, out)

	// Mechanism → aw:OperationalIntent.
	if !strings.Contains(out, rdf.AwNS+"OperationalIntent>") {
		t.Error("expected aw:OperationalIntent for level=mechanism")
	}

	// zooms_out_to edge.
	if !strings.Contains(out, rdf.AwNS+"zoomsOutTo>") {
		t.Error("expected aw:zoomsOutTo predicate")
	}
	if !strings.Contains(out, "intent/globular.vision.ai_operable_cluster") {
		t.Error("expected zooms_out_to target IRI")
	}

	// zooms_in_to edge.
	if !strings.Contains(out, rdf.AwNS+"zoomsInto>") {
		t.Error("expected aw:zoomsInto predicate")
	}
	if !strings.Contains(out, "intent/workflow.idempotency_contract") {
		t.Error("expected zooms_in_to target IRI")
	}

	// related_to edge.
	if !strings.Contains(out, rdf.AwNS+"relatedTo>") {
		t.Error("expected aw:relatedTo predicate")
	}
	if !strings.Contains(out, "intent/doctor.findings_are_operator_language") {
		t.Error("expected related_to target IRI")
	}

	if len(report.Imported()) != 1 {
		t.Fatalf("expected 1 imported file, got %d", len(report.Imported()))
	}
}

// TestPhaseC_Intent_ActivationTriggersAndBadSmells pins that activation_triggers
// become aw:activationTrigger literals and bad_smells become aw:badSmell literals.
func TestPhaseC_Intent_ActivationTriggersAndBadSmells(t *testing.T) {
	out, _ := intentDir(t, map[string]string{
		"api.grpc.yaml": `
id: api.grpc_contracts_are_operational_surface
level: principle
title: gRPC contracts
intent: Services expose typed gRPC contracts.
activation_triggers:
  - gRPC
  - proto
  - MCP tool
bad_smells:
  - agent scrapes logs to decide action
  - CLI shells into service internals
`,
	})

	assertValidNT(t, out)

	// aw:activationTrigger literals.
	if !strings.Contains(out, rdf.AwNS+"activationTrigger>") {
		t.Error("expected aw:activationTrigger predicate")
	}
	if !strings.Contains(out, `"gRPC"`) {
		t.Error("expected gRPC trigger literal")
	}
	if !strings.Contains(out, `"proto"`) {
		t.Error("expected proto trigger literal")
	}

	// aw:badSmell literals.
	if !strings.Contains(out, rdf.AwNS+"badSmell>") {
		t.Error("expected aw:badSmell predicate")
	}
	if !strings.Contains(out, "agent scrapes logs") {
		t.Error("expected bad_smells content in output")
	}
}

// TestPhaseC_Intent_ExpressedByCreatesSourceFileNode pins that expressed_by
// entries produce aw:SourceFile nodes linked with aw:expressedBy edges.
func TestPhaseC_Intent_ExpressedByCreatesSourceFileNode(t *testing.T) {
	out, _ := intentDir(t, map[string]string{
		"intent.yaml": `
id: api.grpc_contracts_are_operational_surface
level: principle
title: gRPC contracts
intent: Services expose typed gRPC contracts.
expressed_by:
  - operators/architecture-overview.md
  - developers/workflow-integration.md
required_tests:
  - golang/server/main_test.go:TestBriefing_DirectIntentOnly
`,
	})

	assertValidNT(t, out)

	// aw:expressedBy predicate.
	if !strings.Contains(out, rdf.AwNS+"expressedBy>") {
		t.Error("expected aw:expressedBy predicate")
	}
	// SourceFile IRI (/ is percent-encoded in path segments).
	if !strings.Contains(out, "operators%2Farchitecture-overview.md") {
		t.Error("expected source file IRI for operators/architecture-overview.md")
	}
	if !strings.Contains(out, "developers%2Fworkflow-integration.md") {
		t.Error("expected source file IRI for developers/workflow-integration.md")
	}
	// SourceFile type assertion.
	if !strings.Contains(out, rdf.AwNS+"SourceFile>") {
		t.Error("expected aw:SourceFile type triple for expressed_by targets")
	}
	if !strings.Contains(out, rdf.AwNS+"requiresTest>") {
		t.Error("expected aw:requiresTest predicate")
	}
	if !strings.Contains(out, "test/golang%2Fserver%2Fmain_test.go:TestBriefing_DirectIntentOnly") {
		t.Error("expected required test IRI for intent")
	}
}

func TestPhaseC_Intent_DefaultRepoScopeTagsIntent(t *testing.T) {
	out, _ := intentDirWithOpts(t, map[string]string{
		"intent.yaml": `
id: component.internal.identity
level: contract
title: Internal identity stays opaque
intent: Principal identifiers must remain opaque and prefixed.
expressed_by:
  - internal/identity/types.go
`,
	}, extractor.ImportDirOptions{
		DefaultRepo:   "github.com/globulario/Globular",
		DefaultDomain: rdf.DomainRepo,
	})

	assertValidNT(t, out)

	if !strings.Contains(out, `intent/component.internal.identity> <`+rdf.PropRepo+`> "github.com/globulario/Globular"`) {
		t.Error("expected intent node to inherit default aw:repo scope")
	}
	if !strings.Contains(out, `intent/component.internal.identity> <`+rdf.PropDomain+`> "repo"`) {
		t.Error("expected intent node to inherit default aw:domain repo scope")
	}
}

// TestPhaseC_Intent_InvalidYAMLReportedNotImported verifies that a broken YAML
// file with id + level keys (but invalid syntax) is recorded as StatusInvalid
// rather than silently dropped. The key regression: the secondary-key detection
// runs on the parsed map, so a file that fails parsing gets StatusInvalid, not
// StatusUnknownSchema.
func TestPhaseC_Intent_InvalidYAMLReportedNotImported(t *testing.T) {
	out, report := intentDir(t, map[string]string{
		"broken.yaml": "id: x\nlevel: principle\ntitle: {broken yaml: [\n",
	})

	if len(report.Files) != 1 {
		t.Fatalf("expected 1 file report, got %d", len(report.Files))
	}
	fr := report.Files[0]
	if fr.Status != extractor.StatusInvalid {
		t.Errorf("status = %q, want %q", fr.Status, extractor.StatusInvalid)
	}
	if fr.Reason == "" {
		t.Error("Reason must be non-empty for invalid files")
	}
	if out != "" {
		t.Errorf("invalid file must produce no triples; got:\n%s", out)
	}
}

// TestPhaseC_Intent_MissingIDProducesNoTriples pins that a file detected as
// intent schema but with an empty id silently produces no triples. The
// file is still reported as imported (the importer ran without error).
func TestPhaseC_Intent_MissingIDProducesNoTriples(t *testing.T) {
	out, report := intentDir(t, map[string]string{
		"empty_id.yaml": `
id: ""
level: principle
title: No ID here
intent: Should produce no triples.
`,
	})

	// The file should be imported (no error), but emit 0 triples.
	if len(report.Imported()) != 1 {
		t.Fatalf("expected 1 imported file (empty-id is not an error), got %d", len(report.Imported()))
	}
	if report.Imported()[0].Count != 0 {
		t.Errorf("expected 0 triples for empty-id intent, got %d", report.Imported()[0].Count)
	}
	if strings.Contains(out, rdf.AwNS+"Intent>") {
		t.Error("expected no Intent node for empty id")
	}
}

// TestPhaseC_Intent_LevelSubclasses verifies that each known level value
// maps to the correct RDF subclass.
func TestPhaseC_Intent_LevelSubclasses(t *testing.T) {
	cases := []struct {
		level   string
		wantCls string
	}{
		{"principle", rdf.ClassDesignIntent},
		{"pattern", rdf.ClassDesignIntent},
		{"mechanism", rdf.ClassOperationalIntent},
		{"operator_model", rdf.ClassOperationalIntent},
		{"implementation", rdf.ClassOperationalIntent},
		{"vision", rdf.ClassProductIntent},
		{"invariant", rdf.ClassConstraintIntent},
		{"contract", rdf.ClassConstraintIntent},
		{"safety_rule", rdf.ClassConstraintIntent},
		{"constraint", rdf.ClassConstraintIntent},
	}

	for _, tc := range cases {
		t.Run(tc.level, func(t *testing.T) {
			out, _ := intentDir(t, map[string]string{
				"intent.yaml": "id: test." + tc.level + "\nlevel: " + tc.level + "\ntitle: Test\nintent: test\n",
			})
			assertValidNT(t, out)
			if !strings.Contains(out, tc.wantCls+">") {
				t.Errorf("level=%q: expected class %s in output", tc.level, tc.wantCls)
			}
		})
	}
}

// TestPhaseC_Intent_RelatedInvariants pins that related_invariants produce
// aw:affects edges pointing to aw:Invariant nodes.
func TestPhaseC_Intent_RelatedInvariants(t *testing.T) {
	out, _ := intentDir(t, map[string]string{
		"intent.yaml": `
id: workflow.source_of_operational_truth
level: mechanism
title: Workflow truth
intent: All state flows through workflows.
related_invariants:
  - convergence.no_infinite_retry
  - infra.heartbeat_not_desired_authority
`,
	})

	assertValidNT(t, out)

	if !strings.Contains(out, rdf.AwNS+"affects>") {
		t.Error("expected aw:affects predicate for related_invariants")
	}
	if !strings.Contains(out, "invariant/convergence.no_infinite_retry") {
		t.Error("expected aw:Invariant IRI for convergence.no_infinite_retry")
	}
	if !strings.Contains(out, "invariant/infra.heartbeat_not_desired_authority") {
		t.Error("expected aw:Invariant IRI for infra.heartbeat_not_desired_authority")
	}
}

// TestPhaseC_Intent_IntegrationSelfAwareness runs against the real docs/intent
// directory in the services repo and verifies that intent files are now imported
// rather than classified as known_unsupported.
func TestPhaseC_Intent_IntegrationSelfAwareness(t *testing.T) {
	const docsIntent = "/home/dave/Documents/github.com/globulario/services/docs/intent"
	if _, err := os.Stat(docsIntent); err != nil {
		t.Skipf("docs/intent not found: %v", err)
	}

	var buf bytes.Buffer
	_, rep, err := extractor.ImportAwarenessDir(docsIntent, &buf)
	if err != nil {
		t.Fatalf("ImportAwarenessDir: %v", err)
	}

	imported := rep.Imported()
	if len(imported) == 0 {
		t.Fatal("expected intent files to be imported from docs/intent; got 0")
	}

	// All imported files must use the intent schema.
	for _, f := range imported {
		if f.Schema != "intent" {
			t.Errorf("unexpected schema %q for file %s", f.Schema, f.Path)
		}
	}

	// Emitted triples must be valid N-Triples.
	assertValidNT(t, buf.String())

	t.Logf("docs/intent: %d files total, %d imported, %d skipped",
		len(rep.Files), len(imported), len(rep.Skipped()))
	for _, f := range rep.Skipped() {
		t.Logf("  skipped [%s] schema=%s: %s", f.Status, f.Schema, f.Path)
	}
}
