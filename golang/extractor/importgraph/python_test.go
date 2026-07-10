// SPDX-License-Identifier: Apache-2.0

package importgraph

import (
	"path/filepath"
	"strings"
	"testing"
)

// Python parser fixtures — all fictional (acme.test src-layout), no real-project paths.

// TestScan_PY_AbsoluteAndRelative is the core case: an absolute import resolved
// under a source root crosses components → one depends_on; relative same-package
// ignored; an unresolved relative import is safe; stdlib ignored; third-party →
// external; multi-name import; namespace package (no __init__.py) resolves;
// main.py entrypoint → service.
func TestScan_PY_AbsoluteAndRelative(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "app", "__init__.py"), "")
	writeFile(t, filepath.Join(root, "src", "app", "helper.py"), "h = 1\n")
	writeFile(t, filepath.Join(root, "src", "app", "main.py"), `
import os, sys
from typing import List
import requests
from billing.api import charge
from .helper import h
from .missing import gone
`)
	// billing is a namespace package (no __init__.py) — must still resolve.
	writeFile(t, filepath.Join(root, "src", "billing", "api.py"), "def charge():\n    pass\n")

	doc, err := Scan(root, "python", Config{})
	if err != nil {
		t.Fatalf("Scan must not fail (unresolved imports are tolerated): %v", err)
	}
	app := findComp(doc, "component.src.app")
	if app == nil {
		t.Fatalf("missing component.src.app; got %d components", len(doc.Components))
	}
	if app.Kind != "service" {
		t.Errorf("app kind = %q, want service (has main.py)", app.Kind)
	}
	if got := app.DependsOn; len(got) != 1 || got[0] != "component.src.billing" {
		t.Errorf("app depends_on = %v, want [component.src.billing] (absolute import resolved under src/)", got)
	}
	if !contains(app.ExternalImports, "requests") {
		t.Errorf("external_imports = %v, want it to include requests", app.ExternalImports)
	}
	for _, leak := range []string{"os", "sys", "typing", "billing.api"} {
		if contains(app.ExternalImports, leak) {
			t.Errorf("%q leaked into external_imports: %v", leak, app.ExternalImports)
		}
	}
	if !contains(app.SourceFiles, "src/app/main.py") || !contains(app.SourceFiles, "src/app/helper.py") {
		t.Errorf("app source_files missing main.py/helper.py: %v", app.SourceFiles)
	}
	if findComp(doc, "component.src.billing") == nil {
		t.Error("missing component.src.billing (namespace package api.py should be scanned)")
	}
}

// TestScan_PY_ClassifierUpgrade — a python classifier rule upgrades a dotted
// module into a semantic edge; a go rule is ignored during a Python scan.
func TestScan_PY_ClassifierUpgrade(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "app", "svc.py"),
		"from app.repositories.user import UserRepo\n")

	cfg := Config{Classifiers: []Rule{
		{ID: "go_noise", Language: "go", Match: `.*repositories.*`, Edge: "reads_from", Target: "component.go_repo"},
		{ID: "py_repo", Language: "python", Match: `^app\.repositories\.(.+)$`, Edge: "reads_from", Target: "component.$1"},
	}}
	doc, err := Scan(root, "python", cfg)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	app := findComp(doc, "component.src.app")
	if app == nil {
		t.Fatal("missing component.src.app")
	}
	if !contains(app.ReadsFrom, "component.user") {
		t.Errorf("reads_from = %v, want component.user (python classifier upgrade)", app.ReadsFrom)
	}
	if contains(app.ReadsFrom, "component.go_repo") {
		t.Errorf("go classifier leaked into a python scan: %v", app.ReadsFrom)
	}
	if contains(app.ExternalImports, "app.repositories.user") {
		t.Errorf("classified import should not also be external: %v", app.ExternalImports)
	}
}

// TestScan_PY_TestAndStubExcluded — test_*.py, *_test.py and .pyi contribute nothing.
func TestScan_PY_TestAndStubExcluded(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "app", "real.py"), "x = 1\n")
	writeFile(t, filepath.Join(root, "src", "app", "test_real.py"), "import secretpkg\n")
	writeFile(t, filepath.Join(root, "src", "app", "real_test.py"), "import secretpkg2\n")
	writeFile(t, filepath.Join(root, "src", "app", "stubs.pyi"), "x: int\n")

	doc, err := Scan(root, "python", Config{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	app := findComp(doc, "component.src.app")
	if app == nil {
		t.Fatal("missing component.src.app")
	}
	for _, f := range app.SourceFiles {
		if strings.HasSuffix(f, "test_real.py") || strings.HasSuffix(f, "real_test.py") || strings.HasSuffix(f, ".pyi") {
			t.Errorf("excluded file leaked into source_files: %s", f)
		}
	}
	if contains(app.ExternalImports, "secretpkg") || contains(app.ExternalImports, "secretpkg2") {
		t.Errorf("test-file imports leaked: %v", app.ExternalImports)
	}
}

// TestScan_PY_Determinism — repeated Scan+Render is byte-identical; header names Python.
func TestScan_PY_Determinism(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "billing", "api.py"), "x = 1\n")
	writeFile(t, filepath.Join(root, "src", "app", "main.py"), "from billing.api import x\n")

	render := func() []byte {
		doc, err := Scan(root, "python", Config{})
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		out, err := Render(doc, "python")
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		return out
	}
	first := render()
	if string(first) != string(render()) {
		t.Error("Scan+Render is not deterministic")
	}
	if !strings.Contains(string(first), "-lang python") {
		t.Error("render header should name the python language")
	}
}
