// SPDX-License-Identifier: Apache-2.0

package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func pyTestScanner(t *testing.T, root string) *Scanner {
	t.Helper()
	regPath := filepath.Join(root, "namespaces.yaml")
	reg := `namespaces:
  - id: test.py_client
    label: PY Client Test
    owns:
      - py
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

func writePY(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestPYScanner_SymbolAttachmentAndKinds(t *testing.T) {
	root := t.TempDir()
	writePY(t, root, "py/client.py", `# @awareness namespace=test.py_client
# @awareness component=client.module

# @awareness component=client.locator
def locate(value):
    return value

# @awareness component=client.types
class Config:
    # @awareness component=client.types.call
    def invoke(self):
        return "ok"
`)
	sc := pyTestScanner(t, root)
	res, err := sc.Scan(filepath.Join(root, "py"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	byComponent := map[string]Annotation{}
	for _, a := range res.Annotations {
		byComponent[a.Component] = a
		if a.Language != "python" {
			t.Fatalf("annotation language = %q, want python", a.Language)
		}
	}
	if a := byComponent["client.module"]; a.Symbol != "" {
		t.Fatalf("file-level symbol = %q, want empty", a.Symbol)
	}
	if a := byComponent["client.locator"]; a.Symbol != "locate" || a.SymbolKind != "function" {
		t.Fatalf("locate = (%q,%q)", a.Symbol, a.SymbolKind)
	}
	if a := byComponent["client.types"]; a.Symbol != "Config" || a.SymbolKind != "type" {
		t.Fatalf("Config = (%q,%q)", a.Symbol, a.SymbolKind)
	}
	if a := byComponent["client.types.call"]; a.Symbol != "Config.invoke" || a.SymbolKind != "method" {
		t.Fatalf("invoke = (%q,%q)", a.Symbol, a.SymbolKind)
	}
}

func TestPYScanner_SharedGrammarValidation(t *testing.T) {
	root := t.TempDir()
	writePY(t, root, "py/bad.py", `# @awareness namespace=does.not.exist
# @awareness implements=notqualified
def f():
    return None
`)
	sc := pyTestScanner(t, root)
	res, err := sc.Scan(filepath.Join(root, "py"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) != 2 {
		t.Fatalf("got %d errors, want 2: %v", len(res.Errors), res.Errors)
	}
}

func TestPYScanner_DiscoversPytestAndUnittestTests(t *testing.T) {
	root := t.TempDir()
	writePY(t, root, "py/test_client.py", `def test_locate_uses_config():
    """locates with config"""
    assert True

class TestClient:
    def test_falls_back(self):
        assert True

def helper():
    return None
`)
	sc := pyTestScanner(t, root)
	res, err := sc.Scan(filepath.Join(root, "py"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, dt := range res.DiscoveredTests {
		if dt.Language != "python" {
			t.Fatalf("discovered test language = %q, want python", dt.Language)
		}
		got[dt.Symbol] = true
	}
	if !got["test_locate_uses_config"] || !got["TestClient.test_falls_back"] {
		t.Fatalf("unexpected discovered tests: %+v", res.DiscoveredTests)
	}
}

func TestPYScanner_SkipsNonTestFilesForDiscoveredTests(t *testing.T) {
	root := t.TempDir()
	writePY(t, root, "py/client.py", `def test_helper():
    assert True
`)
	sc := pyTestScanner(t, root)
	res, err := sc.Scan(filepath.Join(root, "py"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.DiscoveredTests) != 0 {
		t.Fatalf("got %d discovered tests, want 0", len(res.DiscoveredTests))
	}
}
