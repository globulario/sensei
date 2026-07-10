// SPDX-License-Identifier: Apache-2.0

package webcompscan

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func scanAll(t *testing.T, root string) []Component {
	t.Helper()
	files, err := FindSourceFiles(root)
	if err != nil {
		t.Fatalf("FindSourceFiles: %v", err)
	}
	var all []Component
	for _, f := range files {
		cs, err := ScanFile(f, root)
		if err != nil {
			t.Fatalf("ScanFile %s: %v", f, err)
		}
		all = append(all, cs...)
	}
	return Dedupe(all)
}

func byID(cs []Component) map[string]Component {
	m := map[string]Component{}
	for _, c := range cs {
		m[c.ID] = c
	}
	return m
}

// TestScan_Registrations: define + window.define + @customElement detected;
// extends-without-registration and non-literal tags are NOT emitted.
func TestScan_Registrations(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "elements.ts"), `
class AcmeButton extends HTMLElement {}
customElements.define('acme-button', AcmeButton);
window.customElements.define('acme-modal', class extends HTMLElement {});

class NotRegistered extends HTMLElement {}      // no define → must NOT appear
const TAG = 'dyn';
customElements.define(TAG, AcmeButton);          // non-literal → skip
`)
	writeFile(t, filepath.Join(root, "src", "card.ts"), `
@customElement('acme-card')
export class AcmeCard extends LitElement {}
`)

	cs := scanAll(t, root)
	m := byID(cs)
	if len(cs) != 3 {
		t.Fatalf("expected 3 web components, got %d: %v", len(cs), keys(m))
	}
	btn, ok := m["component.acme_button"]
	if !ok || btn.Kind != "ui_component" || btn.Uml == nil || btn.Uml.Stereotype != "web_component" {
		t.Fatalf("acme-button component wrong: %+v", btn)
	}
	if btn.Name != "acme-button" || btn.SourceFiles[0] != "src/elements.ts" {
		t.Errorf("acme-button name/source wrong: %+v", btn)
	}
	if _, ok := m["component.acme_modal"]; !ok {
		t.Error("window.customElements.define not detected")
	}
	if card, ok := m["component.acme_card"]; !ok {
		t.Error("@customElement decorator not detected")
	} else if card.SourceFiles[0] != "src/card.ts" {
		t.Errorf("card source = %v", card.SourceFiles)
	}
	for _, bad := range []string{"component.notregistered", "component.dyn"} {
		if _, ok := m[bad]; ok {
			t.Errorf("emitted a non-registered/non-literal element: %s", bad)
		}
	}
}

// TestScan_ExcludesDeclAndTestAndNodeModules.
func TestScan_ExcludesDeclAndTestAndNodeModules(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "real.ts"), "customElements.define('acme-real', class extends HTMLElement {});\n")
	writeFile(t, filepath.Join(root, "types.d.ts"), "customElements.define('acme-decl', X);\n")
	writeFile(t, filepath.Join(root, "real.test.ts"), "customElements.define('acme-test', X);\n")
	writeFile(t, filepath.Join(root, "node_modules", "dep", "d.ts"), "customElements.define('acme-dep', X);\n")

	m := byID(scanAll(t, root))
	if _, ok := m["component.acme_real"]; !ok {
		t.Error("real source element missing")
	}
	for _, bad := range []string{"component.acme_decl", "component.acme_test", "component.acme_dep"} {
		if _, ok := m[bad]; ok {
			t.Errorf("excluded file's element leaked: %s", bad)
		}
	}
}

// TestScan_DedupesAcrossFiles — same tag registered in two files → one component.
func TestScan_DedupesAcrossFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.ts"), "customElements.define('acme-shared', A);\n")
	writeFile(t, filepath.Join(root, "b.ts"), "customElements.define('acme-shared', B);\n")

	cs := scanAll(t, root)
	if len(cs) != 1 {
		t.Fatalf("expected 1 deduped component, got %d", len(cs))
	}
	if got := cs[0].SourceFiles; len(got) != 2 || got[0] != "a.ts" || got[1] != "b.ts" {
		t.Errorf("merged source_files = %v, want [a.ts b.ts]", got)
	}
}

// TestRender_Deterministic.
func TestRender_Deterministic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "x.ts"), "customElements.define('acme-x', X);\ncustomElements.define('acme-y', Y);\n")
	doc := Doc{Components: scanAll(t, root)}
	a, err := Render(doc)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	b, _ := Render(doc)
	if string(a) != string(b) {
		t.Error("Render is not deterministic")
	}
}

func keys(m map[string]Component) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
