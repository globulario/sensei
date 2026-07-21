// SPDX-License-Identifier: AGPL-3.0-only

package extractor_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/rdf"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func importSymbolsYAML(t *testing.T, filename, content string) string {
	t.Helper()
	root := makeDir(t, map[string]string{filename: content})
	var buf bytes.Buffer
	_, _, err := extractor.ImportAwarenessDir(root, &buf)
	if err != nil {
		t.Fatalf("ImportAwarenessDir: %v", err)
	}
	return buf.String()
}

func assertNTValid(t *testing.T, nt string) {
	t.Helper()
	errs := extractor.ValidateNTriples(bytes.NewReader([]byte(nt)))
	for _, e := range errs {
		t.Errorf("NT validation: %s", e)
	}
}

// ── code_symbols tests ────────────────────────────────────────────────────────

// TestCodeSymbols_ValidSymbol verifies a well-formed code_symbol entry emits
// a typed CodeSymbol node with label and risk.
func TestCodeSymbols_ValidSymbol(t *testing.T) {
	out := importSymbolsYAML(t, "code_symbols.yaml", `
code_symbols:
  - id: globular.awareness_graph:code.go.server.briefing.server_Briefing
    namespace: globular.awareness_graph
    language: go
    file: golang/server/briefing.go
    symbol: server.Briefing
    kind: method
    component: server.briefing
    risk: high
`)
	if !strings.Contains(out, rdf.IRI(rdf.ClassCodeSymbol)) {
		t.Errorf("want aw:CodeSymbol rdf:type triple; got:\n%s", out)
	}
	if !strings.Contains(out, `"server.Briefing"`) {
		t.Errorf("want rdfs:label \"server.Briefing\"; got:\n%s", out)
	}
	if !strings.Contains(out, `"high"`) {
		t.Errorf("want aw:risk \"high\"; got:\n%s", out)
	}
	assertNTValid(t, out)
}

// TestCodeReferences_InternalAndExternal verifies code_references.yaml emits
// aw:references edges: internal refs reuse the target symbol IRI; external refs
// mint an "external:<name>" CodeSymbol node so sibling-convention queries work.
func TestCodeReferences_InternalAndExternal(t *testing.T) {
	out := importSymbolsYAML(t, "code_references.yaml", `
code_references:
  - from: command/issue.go:issueReopen
    to_id: command/issue.go:issueClose
    to_name: issueClose
    file: command/issue.go
  - from: command/issue.go:issueClose
    to_name: Fprintf
    file: command/issue.go
`)
	if !strings.Contains(out, rdf.IRI(rdf.PropReferences)) {
		t.Errorf("want an aw:references edge; got:\n%s", out)
	}
	// External target minted as CodeSymbol external:Fprintf with a label.
	if !strings.Contains(out, rdf.MintIRI(rdf.ClassCodeSymbol, "external:Fprintf")) {
		t.Errorf("want external:Fprintf CodeSymbol node; got:\n%s", out)
	}
	if !strings.Contains(out, `"Fprintf"`) {
		t.Errorf("want rdfs:label \"Fprintf\" for external target; got:\n%s", out)
	}
	// Internal target reuses the defined symbol IRI.
	if !strings.Contains(out, rdf.MintIRI(rdf.ClassCodeSymbol, "command/issue.go:issueClose")) {
		t.Errorf("want internal target IRI for issueClose; got:\n%s", out)
	}
	assertNTValid(t, out)
}

// TestCodeSymbols_EdgeToInvariant verifies enforces annotation emits
// aw:enforces and projects a flat SourceFile aw:implements Invariant reverse.
func TestCodeSymbols_EdgeToInvariant(t *testing.T) {
	out := importSymbolsYAML(t, "code_symbols.yaml", `
code_symbols:
  - id: globular.awareness_graph:code.go.server.briefing.server_Briefing
    namespace: globular.awareness_graph
    language: go
    file: golang/server/briefing.go
    symbol: server.Briefing
    kind: method
    component: server.briefing
    annotations:
      enforces:
        - globular.awareness_graph:invariant.briefing_status_is_always_set
`)
	if !strings.Contains(out, rdf.AwNS+"enforces") {
		t.Errorf("want aw:enforces edge; got:\n%s", out)
	}
	if !strings.Contains(out, rdf.AwNS+"implements") {
		t.Errorf("want aw:implements reverse edge from SourceFile; got:\n%s", out)
	}
	if !strings.Contains(out, rdf.IRI(rdf.ClassSourceFile)) {
		t.Errorf("want aw:SourceFile typed node; got:\n%s", out)
	}
	assertNTValid(t, out)
}

// TestCodeSymbols_EdgeToIntent verifies implements annotation emits
// aw:implements forward edge and projects a flat SourceFile aw:implements
// reverse edge. Intent typed nodes are NOT emitted here (that is intent.yaml's
// responsibility — same cross-document pattern as invariant→failure_mode).
func TestCodeSymbols_EdgeToIntent(t *testing.T) {
	out := importSymbolsYAML(t, "code_symbols.yaml", `
code_symbols:
  - id: globular.awareness_graph:code.go.server.briefing.server_Briefing
    namespace: globular.awareness_graph
    language: go
    file: golang/server/briefing.go
    symbol: server.Briefing
    kind: method
    component: server.briefing
    annotations:
      implements:
        - globular.awareness_graph:intent.briefing_returns_explicit_status
`)
	if !strings.Contains(out, rdf.AwNS+"implements") {
		t.Errorf("want aw:implements edge; got:\n%s", out)
	}
	// Reverse edge: SourceFile aw:implements Intent (for impact queries)
	if !strings.Contains(out, rdf.IRI(rdf.ClassSourceFile)) {
		t.Errorf("want aw:SourceFile typed node (for reverse implements edge); got:\n%s", out)
	}
	assertNTValid(t, out)
}

// TestCodeSymbols_ProtectsUsesProtectsAgainst verifies protects annotation
// maps to aw:protectsAgainst (distinct from the Invariant→SourceFile aw:protects).
func TestCodeSymbols_ProtectsUsesProtectsAgainst(t *testing.T) {
	out := importSymbolsYAML(t, "code_symbols.yaml", `
code_symbols:
  - id: globular.awareness_graph:code.go.server.briefing.server_Briefing
    namespace: globular.awareness_graph
    language: go
    file: golang/server/briefing.go
    symbol: server.Briefing
    kind: method
    component: server.briefing
    annotations:
      protects:
        - globular.awareness_graph:failure_mode.silent_empty_on_store_failure
`)
	if !strings.Contains(out, rdf.AwNS+"protectsAgainst") {
		t.Errorf("want aw:protectsAgainst edge; got:\n%s", out)
	}
	assertNTValid(t, out)
}

// TestCodeSymbols_TestedByEmitsTestSymbol verifies tested_by annotation emits
// aw:testedBy and creates a TestSymbol typed node.
func TestCodeSymbols_TestedByEmitsTestSymbol(t *testing.T) {
	out := importSymbolsYAML(t, "code_symbols.yaml", `
code_symbols:
  - id: globular.awareness_graph:code.go.server.briefing.server_Briefing
    namespace: globular.awareness_graph
    language: go
    file: golang/server/briefing.go
    symbol: server.Briefing
    kind: method
    component: server.briefing
    annotations:
      tested_by:
        - golang/server/main_test.go:TestBriefingStoreNil
`)
	if !strings.Contains(out, rdf.AwNS+"testedBy") {
		t.Errorf("want aw:testedBy edge; got:\n%s", out)
	}
	if !strings.Contains(out, rdf.IRI(rdf.ClassTestSymbol)) {
		t.Errorf("want aw:TestSymbol typed node; got:\n%s", out)
	}
	assertNTValid(t, out)
}

func TestCodeSymbols_DiscoveredTestSymbolEmitsMetadata(t *testing.T) {
	out := importSymbolsYAML(t, "code_symbols.yaml", `
code_symbols: []
test_symbols:
  - id: golang/server/main_test.go:TestBriefingStoreNil
    file: golang/server/main_test.go
    symbol: TestBriefingStoreNil
    package: server
    language: go
    doc: Guards nil store behavior.
`)
	if !strings.Contains(out, rdf.IRI(rdf.ClassTestSymbol)) {
		t.Errorf("want aw:TestSymbol typed node; got:\n%s", out)
	}
	if !strings.Contains(out, `"TestBriefingStoreNil"`) {
		t.Errorf("want TestSymbol label; got:\n%s", out)
	}
	if !strings.Contains(out, rdf.AwNS+"definedInFile") {
		t.Errorf("want aw:definedInFile edge; got:\n%s", out)
	}
	assertNTValid(t, out)
}

// TestCodeSymbols_MissingIDSkipped verifies an entry with an empty id is
// silently skipped (no crash, no empty-IRI triple).
func TestCodeSymbols_MissingIDSkipped(t *testing.T) {
	out := importSymbolsYAML(t, "code_symbols.yaml", `
code_symbols:
  - id: ""
    namespace: globular.awareness_graph
    symbol: broken
    kind: function
`)
	lines := strings.Count(out, "\n")
	if lines > 0 {
		t.Errorf("expected 0 triples for empty-id symbol; got %d lines:\n%s", lines, out)
	}
}

// TestCodeSymbols_Determinism verifies importing the same YAML twice produces
// identical output (no non-determinism from map iteration in annotations).
func TestCodeSymbols_Determinism(t *testing.T) {
	const yaml = `
code_symbols:
  - id: globular.awareness_graph:code.go.server.briefing.server_Briefing
    namespace: globular.awareness_graph
    language: go
    file: golang/server/briefing.go
    symbol: server.Briefing
    kind: method
    component: server.briefing
    risk: high
    annotations:
      enforces:
        - globular.awareness_graph:invariant.briefing_status_is_always_set
        - globular.awareness_graph:invariant.store_nil_returns_unavailable_not_empty
      implements:
        - globular.awareness_graph:intent.briefing_returns_explicit_status
      tested_by:
        - golang/server/main_test.go:TestBriefingStoreNil
`
	out1 := importSymbolsYAML(t, "code_symbols.yaml", yaml)
	out2 := importSymbolsYAML(t, "code_symbols.yaml", yaml)
	if out1 != out2 {
		t.Errorf("non-deterministic output:\nrun1:\n%s\nrun2:\n%s", out1, out2)
	}
}

// ── code_edges tests ──────────────────────────────────────────────────────────

// TestCodeEdges_EmitsRelationEdge verifies the code_edges importer emits
// the correct property IRI for a known relation.
func TestCodeEdges_EmitsRelationEdge(t *testing.T) {
	out := importSymbolsYAML(t, "code_edges.yaml", `
code_edges:
  - from: globular.awareness_graph:code.go.server.briefing.server_Briefing
    relation: enforces
    to: globular.awareness_graph:invariant.briefing_status_is_always_set
`)
	if !strings.Contains(out, rdf.AwNS+"enforces") {
		t.Errorf("want aw:enforces edge; got:\n%s", out)
	}
	assertNTValid(t, out)
}

// TestCodeEdges_UnknownRelationSkipped verifies unknown relation names are
// silently skipped — no crash, no invalid IRI emitted.
func TestCodeEdges_UnknownRelationSkipped(t *testing.T) {
	out := importSymbolsYAML(t, "code_edges.yaml", `
code_edges:
  - from: globular.awareness_graph:code.go.server.briefing.server_Briefing
    relation: calls
    to: globular.awareness_graph:code.go.server.impact.server_Impact
`)
	// "calls" is unknown → 0 triples
	lines := strings.Count(out, "\n")
	if lines > 0 {
		t.Errorf("expected 0 triples for unknown relation; got %d:\n%s", lines, out)
	}
}

// TestCodeEdges_TestedByEmitsTestSymbol verifies tested_by in code_edges
// correctly emits a TestSymbol typed node.
func TestCodeEdges_TestedByEmitsTestSymbol(t *testing.T) {
	out := importSymbolsYAML(t, "code_edges.yaml", `
code_edges:
  - from: globular.awareness_graph:code.go.server.briefing.server_Briefing
    relation: tested_by
    to: golang/server/main_test.go:TestBriefingStoreNil
`)
	if !strings.Contains(out, rdf.AwNS+"testedBy") {
		t.Errorf("want aw:testedBy edge; got:\n%s", out)
	}
	if !strings.Contains(out, rdf.IRI(rdf.ClassTestSymbol)) {
		t.Errorf("want aw:TestSymbol typed node; got:\n%s", out)
	}
	assertNTValid(t, out)
}
