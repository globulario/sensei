// SPDX-License-Identifier: AGPL-3.0-only

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

// TestComponentForDir_ManifestRoot guards the rollup for languages whose owned
// unit is the manifest directory, with sources beneath it. Applying Go's
// directory-is-package rule here would split crates/alpha into crates/alpha/src
// and invent a component per src/ folder.
func TestComponentForDir_ManifestRoot(t *testing.T) {
	cases := []struct {
		dir     string
		wantID  string
		wantOK  bool
		wantDir string
	}{
		{"crates/alpha", "component.crates.alpha", true, "crates/alpha"},
		{"crates/alpha/src", "component.crates.alpha", true, "crates/alpha"},
		{"packages/app/src/lib", "component.packages.app", true, "packages/app"},
		{"mytool", "component.mytool", true, "mytool"},
		{"mytool/sub/deep", "component.mytool", true, "mytool"},
		{"golang", "", false, ""}, // file directly in a source root → no component
		{".", "", false, ""},
		{"", "", false, ""},
	}
	for _, c := range cases {
		id, dir, ok := componentForDir(c.dir, granularityManifestRoot)
		if ok != c.wantOK || id != c.wantID || (ok && dir != c.wantDir) {
			t.Errorf("componentForDir(%q, manifestRoot) = (%q,%q,%v), want (%q,%q,%v)", c.dir, id, dir, ok, c.wantID, c.wantDir, c.wantOK)
		}
	}
}

// TestComponentForDir_PackageDir guards Go's rule: the directory holding the
// sources IS the package. The old two-segment rollup made golang/architecture a
// single component over 77 distinct packages, so every file in that tree
// resolved to the same node and the component layer discriminated nothing.
func TestComponentForDir_PackageDir(t *testing.T) {
	cases := []struct {
		dir     string
		wantID  string
		wantOK  bool
		wantDir string
	}{
		{"golang/server", "component.golang.server", true, "golang/server"},
		{"golang/architecture/workspacecontract", "component.golang.architecture.workspacecontract", true, "golang/architecture/workspacecontract"},
		{"golang/architecture/testobligation", "component.golang.architecture.testobligation", true, "golang/architecture/testobligation"},
		{"cmd/app", "component.cmd.app", true, "cmd/app"},
		{"mytool/sub/deep", "component.mytool.sub.deep", true, "mytool/sub/deep"},
		{"golang", "", false, ""}, // a source root is a layout convention, not a unit
		{".", "", false, ""},
		{"", "", false, ""},
	}
	for _, c := range cases {
		id, dir, ok := componentForDir(c.dir, granularityPackageDir)
		if ok != c.wantOK || id != c.wantID || (ok && dir != c.wantDir) {
			t.Errorf("componentForDir(%q, packageDir) = (%q,%q,%v), want (%q,%q,%v)", c.dir, id, dir, ok, c.wantID, c.wantDir, c.wantOK)
		}
	}
}

// Two packages that used to collide under the same component must now be
// distinct — this is the whole point of the migration.
func TestComponentForDir_SiblingPackagesNoLongerCollide(t *testing.T) {
	a, _, _ := componentForDir("golang/architecture/workspacecontract", granularityPackageDir)
	b, _, _ := componentForDir("golang/architecture/testobligation", granularityPackageDir)
	if a == b {
		t.Fatalf("sibling packages still share one component id: %q", a)
	}
	oldA, _, _ := componentForDir("golang/architecture/workspacecontract", granularityManifestRoot)
	oldB, _, _ := componentForDir("golang/architecture/testobligation", granularityManifestRoot)
	if oldA != oldB {
		t.Fatalf("precondition: under the old rule these collided; got %q vs %q", oldA, oldB)
	}
}

// Granularity is chosen by language, not guessed globally.
func TestGranularityFor(t *testing.T) {
	if granularityFor("go") != granularityPackageDir {
		t.Error("go must use directory-is-package")
	}
	for _, lang := range []string{"rust", "typescript", "python", "unknown-language"} {
		if granularityFor(lang) != granularityManifestRoot {
			t.Errorf("%s must use the conservative manifest-root rollup", lang)
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
