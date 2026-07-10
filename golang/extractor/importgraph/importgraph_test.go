// SPDX-License-Identifier: Apache-2.0

package importgraph

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// ── shared test helpers ──────────────────────────────────────────────────────

// writeFile creates parent dirs and writes content (mirrors the bootstrap helper).
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func findComp(doc Doc, id string) *Component {
	for i := range doc.Components {
		if doc.Components[i].ID == id {
			return &doc.Components[i]
		}
	}
	return nil
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// All fixtures use a fictional module — no real-project paths or conventions.
const fixtureModule = "module acme.test/app\n\ngo 1.21\n"

// ── shared-core tests (language-neutral) ─────────────────────────────────────

// TestClassifier_LanguageFilter proves the classifier mechanism is
// language-neutral: a typescript rule is NOT applied during a go scan, even
// before any TypeScript parser exists.
func TestClassifier_LanguageFilter(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), fixtureModule)
	writeFile(t, filepath.Join(root, "golang", "c", "c.go"),
		"package c\nimport \"acme.test/platform/billing/billing_gateway\"\n")

	cfg := Config{Classifiers: []Rule{
		// A TypeScript rule that WOULD match the path if language were ignored.
		{ID: "ts_rule", Language: "typescript", Match: `.*billing.*`, Edge: "reads_from", Target: "component.ts_billing"},
		// The applicable Go rule.
		{ID: "go_rule", Language: "go", Match: `^acme\.test/platform/([a-z]+)/[a-z]+_gateway$`, Edge: "reads_from", Target: "component.$1"},
	}}
	doc, err := Scan(root, "go", cfg)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	c := findComp(doc, "component.golang.c")
	if c == nil {
		t.Fatal("missing component.golang.c")
	}
	if !contains(c.ReadsFrom, "component.billing") {
		t.Errorf("reads_from = %v, want component.billing (go rule applied)", c.ReadsFrom)
	}
	if contains(c.ReadsFrom, "component.ts_billing") {
		t.Errorf("typescript rule leaked into a go scan: %v", c.ReadsFrom)
	}
}

// TestScan_UnknownLanguage — scanning an unregistered language is an error, not a panic.
func TestScan_UnknownLanguage(t *testing.T) {
	if _, err := Scan(t.TempDir(), "cobol", Config{}); err == nil {
		t.Fatal("expected an error for an unregistered language")
	}
}

// TestConfig_InvalidEdge — a bad edge keyword is rejected at compile time.
func TestConfig_InvalidEdge(t *testing.T) {
	_, err := Scan(t.TempDir(), "go", Config{Classifiers: []Rule{
		{ID: "bad", Language: "go", Match: ".*", Edge: "calls", Target: "component.x"},
	}})
	if err == nil {
		t.Fatal("expected an error for an invalid edge keyword")
	}
}

// TestComponentForDir guards the language-neutral rollup scheme.
func TestComponentForDir(t *testing.T) {
	cases := []struct {
		dir     string
		wantID  string
		wantOK  bool
		wantDir string
	}{
		{"golang/server", "component.golang.server", true, "golang/server"},
		{"golang/server/internal/util", "component.golang.server", true, "golang/server"},
		{"cmd/app", "component.cmd.app", true, "cmd/app"},
		{"mytool", "component.mytool", true, "mytool"},
		{"mytool/sub/deep", "component.mytool", true, "mytool"},
		{"golang", "", false, ""}, // file directly in a source root → no component
		{".", "", false, ""},
		{"", "", false, ""},
	}
	for _, c := range cases {
		id, dir, ok := componentForDir(c.dir)
		if ok != c.wantOK || id != c.wantID || (ok && dir != c.wantDir) {
			t.Errorf("componentForDir(%q) = (%q,%q,%v), want (%q,%q,%v)", c.dir, id, dir, ok, c.wantID, c.wantDir, c.wantOK)
		}
	}
}

// TestRender_Deterministic — repeated Scan+Render is byte-identical, and the
// header names the language.
func TestRender_Deterministic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), fixtureModule)
	writeFile(t, filepath.Join(root, "golang", "a", "a.go"), "package a\nimport \"acme.test/app/golang/b\"\n")
	writeFile(t, filepath.Join(root, "golang", "b", "b.go"), "package b\nimport \"acme.test/app/golang/a\"\n")

	render := func() []byte {
		doc, err := Scan(root, "go", Config{})
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		out, err := Render(doc, "go")
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		return out
	}
	first := render()
	if !bytes.Equal(first, render()) {
		t.Error("Scan+Render is not deterministic across runs")
	}
	if !bytes.Contains(first, []byte("-lang go")) {
		t.Error("render header should name the language")
	}
}
