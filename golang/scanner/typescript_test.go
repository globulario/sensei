// SPDX-License-Identifier: AGPL-3.0-only

package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

// tsTestScanner builds a Scanner with a registry that knows the test
// namespace and owns the temp dir's paths.
func tsTestScanner(t *testing.T, root string) *Scanner {
	t.Helper()
	regPath := filepath.Join(root, "namespaces.yaml")
	reg := `namespaces:
  - id: test.ts_client
    label: TS Client Test
    owns:
      - web
`
	if err := os.WriteFile(regPath, []byte(reg), 0644); err != nil {
		t.Fatal(err)
	}
	r, err := LoadRegistry(regPath)
	if err != nil {
		t.Fatal(err)
	}
	return &Scanner{Registry: r, RepoRoot: root}
}

func writeTS(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestTSScanner_SymbolAttachmentAndKinds(t *testing.T) {
	root := t.TempDir()
	writeTS(t, root, "web/client.ts", `// @awareness namespace=test.ts_client
// @awareness component=client.kit
// @awareness risk=high

import { x } from './x';

// @awareness component=client.locator
// @awareness risk=medium
export function locate(id: string): string { return id; }

// @awareness component=client.config
export const defaultLocator = (id: string) => id;

// @awareness component=client.types
export interface Config { domain: string; }

class Kit {
  // @awareness component=client.kit.call
  invoke(method: string) { return method; }
}
`)
	sc := tsTestScanner(t, root)
	res, err := sc.Scan(filepath.Join(root, "web"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}

	byComponent := map[string]Annotation{}
	for _, a := range res.Annotations {
		byComponent[a.Component] = a
		if a.Language != "typescript" {
			t.Errorf("annotation %q language = %q, want typescript", a.Component, a.Language)
		}
	}

	// File-level: blank line between block and import → no symbol.
	if a := byComponent["client.kit"]; a.Symbol != "" {
		t.Errorf("file-level block attached to symbol %q, want file-level", a.Symbol)
	}
	// Exported function.
	if a := byComponent["client.locator"]; a.Symbol != "locate" || a.SymbolKind != "function" {
		t.Errorf("locate: got (%q,%q), want (locate,function)", a.Symbol, a.SymbolKind)
	}
	// Const arrow function refines kind to function.
	if a := byComponent["client.config"]; a.Symbol != "defaultLocator" || a.SymbolKind != "function" {
		t.Errorf("defaultLocator: got (%q,%q), want (defaultLocator,function)", a.Symbol, a.SymbolKind)
	}
	// Interface → shared kind "type".
	if a := byComponent["client.types"]; a.Symbol != "Config" || a.SymbolKind != "type" {
		t.Errorf("Config: got (%q,%q), want (Config,type)", a.Symbol, a.SymbolKind)
	}
	// Class method → ClassName.method, kind "method".
	if a := byComponent["client.kit.call"]; a.Symbol != "Kit.invoke" || a.SymbolKind != "method" {
		t.Errorf("Kit.invoke: got (%q,%q), want (Kit.invoke,method)", a.Symbol, a.SymbolKind)
	}
}

func TestTSScanner_SkipsGeneratedAndDeclarationFiles(t *testing.T) {
	root := t.TempDir()
	ann := "// @awareness namespace=test.ts_client\nexport function f() {}\n"
	writeTS(t, root, "web/real.ts", ann)
	writeTS(t, root, "web/types.d.ts", ann)
	writeTS(t, root, "web/service_pb.ts", ann)
	writeTS(t, root, "web/service_grpc_web_pb.ts", ann)

	sc := tsTestScanner(t, root)
	res, err := sc.Scan(filepath.Join(root, "web"))
	if err != nil {
		t.Fatal(err)
	}
	if res.ScannedFiles != 1 {
		t.Fatalf("scanned %d files, want 1 (generated/.d.ts must be skipped)", res.ScannedFiles)
	}
}

func TestTSScanner_SharedGrammarValidation(t *testing.T) {
	root := t.TempDir()
	// Unknown namespace and malformed qualified ID must fail exactly like Go.
	writeTS(t, root, "web/bad.ts", `// @awareness namespace=does.not.exist
// @awareness implements=notqualified
export function f() {}
`)
	sc := tsTestScanner(t, root)
	res, err := sc.Scan(filepath.Join(root, "web"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) != 2 {
		t.Fatalf("got %d errors, want 2 (unknown namespace + unqualified ID): %v", len(res.Errors), res.Errors)
	}
}

func TestTSScanner_BlockCommentGrammar(t *testing.T) {
	root := t.TempDir()
	writeTS(t, root, "web/block.ts", `/* @awareness namespace=test.ts_client
 * @awareness component=client.block
 * @awareness risk=low
 */
export function g() {}
`)
	sc := tsTestScanner(t, root)
	res, err := sc.Scan(filepath.Join(root, "web"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if len(res.Annotations) != 1 {
		t.Fatalf("got %d annotations, want 1", len(res.Annotations))
	}
	a := res.Annotations[0]
	if a.Symbol != "g" || a.Component != "client.block" {
		t.Errorf("got symbol=%q component=%q, want g/client.block", a.Symbol, a.Component)
	}
}

func TestTSScanner_DiscoversNamedTestsInTestFiles(t *testing.T) {
	root := t.TempDir()
	writeTS(t, root, "web/client.spec.ts", `export function TestLocateUsesConfig() {}

export const TestFallbackPath = () => {}

function helper() {}
`)
	sc := tsTestScanner(t, root)
	res, err := sc.Scan(filepath.Join(root, "web"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.DiscoveredTests) != 2 {
		t.Fatalf("got %d discovered tests, want 2", len(res.DiscoveredTests))
	}
	if res.DiscoveredTests[0].Language != "typescript" {
		t.Fatalf("first discovered test language = %q, want typescript", res.DiscoveredTests[0].Language)
	}
	got := map[string]bool{}
	for _, dt := range res.DiscoveredTests {
		got[dt.Symbol] = true
		if dt.File != "web/client.spec.ts" {
			t.Fatalf("discovered test file = %q, want web/client.spec.ts", dt.File)
		}
	}
	if !got["TestLocateUsesConfig"] || !got["TestFallbackPath"] {
		t.Fatalf("unexpected discovered tests: %+v", res.DiscoveredTests)
	}
}

func TestTSScanner_DiscoversTitleBasedTestsInTestFiles(t *testing.T) {
	root := t.TempDir()
	writeTS(t, root, "web/client.test.ts", `it("locate uses config", () => {})
test("falls back to proxy", () => {})
test(`+"`"+`template title`+"`"+`, () => {})
`)
	sc := tsTestScanner(t, root)
	res, err := sc.Scan(filepath.Join(root, "web"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, dt := range res.DiscoveredTests {
		got[dt.Symbol] = true
	}
	if !got["SpecTitle_locate_uses_config"] ||
		!got["SpecTitle_falls_back_to_proxy"] ||
		!got["SpecTitle_template_title"] {
		t.Fatalf("unexpected discovered tests: %+v", res.DiscoveredTests)
	}
}

func TestTSScanner_DedupesDuplicateTitleBasedTests(t *testing.T) {
	root := t.TempDir()
	writeTS(t, root, "web/client.test.ts", `it("same title", () => {})
it("same title", () => {})
`)
	sc := tsTestScanner(t, root)
	res, err := sc.Scan(filepath.Join(root, "web"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.DiscoveredTests) != 2 {
		t.Fatalf("got %d discovered tests, want 2", len(res.DiscoveredTests))
	}
	if res.DiscoveredTests[0].Symbol == res.DiscoveredTests[1].Symbol {
		t.Fatalf("duplicate title-based tests must not collide: %+v", res.DiscoveredTests)
	}
}

func TestSymbolID_LanguageSegment(t *testing.T) {
	goAnn := Annotation{Namespace: "ns.x", Component: "comp", Symbol: "F", Language: "go"}
	tsAnn := Annotation{Namespace: "ns.x", Component: "comp", Symbol: "F", Language: "typescript"}
	jsAnn := Annotation{Namespace: "ns.x", Component: "comp", Symbol: "F", Language: "javascript"}
	pyAnn := Annotation{Namespace: "ns.x", Component: "comp", Symbol: "F", Language: "python"}
	rsAnn := Annotation{Namespace: "ns.x", Component: "comp", Symbol: "F", Language: "rust"}
	if id := symbolID(goAnn); id != "ns.x:code.go.comp.F" {
		t.Errorf("go id = %q", id)
	}
	if id := symbolID(tsAnn); id != "ns.x:code.ts.comp.F" {
		t.Errorf("ts id = %q", id)
	}
	if id := symbolID(jsAnn); id != "ns.x:code.js.comp.F" {
		t.Errorf("js id = %q", id)
	}
	if id := symbolID(pyAnn); id != "ns.x:code.py.comp.F" {
		t.Errorf("py id = %q", id)
	}
	if id := symbolID(rsAnn); id != "ns.x:code.rs.comp.F" {
		t.Errorf("rs id = %q", id)
	}
	// Same component+symbol in two languages must not collide.
	if symbolID(goAnn) == symbolID(tsAnn) {
		t.Error("go and ts symbol IDs collide")
	}
	if symbolID(goAnn) == symbolID(jsAnn) || symbolID(tsAnn) == symbolID(jsAnn) {
		t.Error("javascript symbol ID collides with another language")
	}
	if symbolID(pyAnn) == symbolID(goAnn) || symbolID(pyAnn) == symbolID(tsAnn) || symbolID(pyAnn) == symbolID(jsAnn) {
		t.Error("python symbol ID collides with another language")
	}
	if symbolID(rsAnn) == symbolID(goAnn) || symbolID(rsAnn) == symbolID(tsAnn) || symbolID(rsAnn) == symbolID(jsAnn) || symbolID(rsAnn) == symbolID(pyAnn) {
		t.Error("rust symbol ID collides with another language")
	}
}

func TestTSScanner_JavaScriptAnnotationsAndTests(t *testing.T) {
	root := t.TempDir()
	writeTS(t, root, "web/client.spec.js", `// @awareness namespace=test.ts_client
// @awareness component=client.js
export function TestLocateUsesConfig() {}

it("falls back to proxy", () => {})
`)
	sc := tsTestScanner(t, root)
	res, err := sc.Scan(filepath.Join(root, "web"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if len(res.Annotations) != 1 {
		t.Fatalf("got %d annotations, want 1", len(res.Annotations))
	}
	if res.Annotations[0].Language != "javascript" {
		t.Fatalf("annotation language = %q, want javascript", res.Annotations[0].Language)
	}
	got := map[string]bool{}
	for _, dt := range res.DiscoveredTests {
		if dt.Language != "javascript" {
			t.Fatalf("discovered test language = %q, want javascript", dt.Language)
		}
		got[dt.Symbol] = true
	}
	if !got["TestLocateUsesConfig"] || !got["SpecTitle_falls_back_to_proxy"] {
		t.Fatalf("unexpected discovered tests: %+v", res.DiscoveredTests)
	}
}
