// SPDX-License-Identifier: AGPL-3.0-only

package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func rustTestScanner(t *testing.T, root string) *Scanner {
	t.Helper()
	regPath := filepath.Join(root, "namespaces.yaml")
	reg := `namespaces:
  - id: test.rs_client
    label: RS Client Test
    owns:
      - rs
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

func writeRS(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestRustScanner_SymbolAttachmentAndKinds(t *testing.T) {
	root := t.TempDir()
	writeRS(t, root, "rs/lib.rs", `// @awareness namespace=test.rs_client
// @awareness component=client.module

// @awareness component=client.locator
fn locate() {}

// @awareness component=client.types
struct Config;

impl Config {
    // @awareness component=client.types.call
    fn invoke(&self) {}
}
`)
	sc := rustTestScanner(t, root)
	res, err := sc.Scan(filepath.Join(root, "rs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	byComponent := map[string]Annotation{}
	for _, a := range res.Annotations {
		byComponent[a.Component] = a
		if a.Language != "rust" {
			t.Fatalf("annotation language = %q, want rust", a.Language)
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

func TestRustScanner_SharedGrammarValidation(t *testing.T) {
	root := t.TempDir()
	writeRS(t, root, "rs/lib.rs", `// @awareness namespace=does.not.exist
// @awareness implements=notqualified
fn f() {}
`)
	sc := rustTestScanner(t, root)
	res, err := sc.Scan(filepath.Join(root, "rs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) != 2 {
		t.Fatalf("got %d errors, want 2: %v", len(res.Errors), res.Errors)
	}
}

func TestRustScanner_DiscoversAttributedTests(t *testing.T) {
	root := t.TempDir()
	writeRS(t, root, "rs/lib.rs", `#[test]
fn test_locate_uses_config() {}

struct Config;
impl Config {
    #[test]
    fn test_falls_back() {}
}
`)
	sc := rustTestScanner(t, root)
	res, err := sc.Scan(filepath.Join(root, "rs"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, dt := range res.DiscoveredTests {
		if dt.Language != "rust" {
			t.Fatalf("discovered test language = %q, want rust", dt.Language)
		}
		got[dt.Symbol] = true
	}
	if !got["test_locate_uses_config"] || !got["Config.test_falls_back"] {
		t.Fatalf("unexpected discovered tests: %+v", res.DiscoveredTests)
	}
}
