// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness-graph/golang/extractor/importgraph"
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

func TestExtractComponents(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "cmd", "server", "main.go"), "package main\nfunc main(){}\n")
	writeFile(t, filepath.Join(root, "pkg", "lib", "lib.go"), "package lib\n")
	writeFile(t, filepath.Join(root, "pkg", "lib", "lib_test.go"), "package lib\n") // test-only must not make a component by itself
	writeFile(t, filepath.Join(root, "vendor", "x", "x.go"), "package x\n")         // excluded
	writeFile(t, filepath.Join(root, "docs", "readme.md"), "# docs")                // no source → no component

	comps := extractComponents(root)
	byID := map[string]bootstrapComponent{}
	for _, c := range comps {
		byID[c.ID] = c
	}

	svc, ok := byID["component.cmd.server"]
	if !ok {
		t.Fatalf("missing component.cmd.server; got %v", keysOf(byID))
	}
	if svc.Kind != "service" {
		t.Errorf("cmd/server kind = %q, want service (has main.go)", svc.Kind)
	}
	if svc.Assertion != "inferred" {
		t.Errorf("assertion = %q, want inferred", svc.Assertion)
	}
	if svc.Uml == nil || svc.Uml.Kind != "Component" {
		t.Errorf("uml = %v, want Component", svc.Uml)
	}

	lib, ok := byID["component.pkg.lib"]
	if !ok {
		t.Fatalf("missing component.pkg.lib")
	}
	if lib.Kind != "module" {
		t.Errorf("pkg/lib kind = %q, want module", lib.Kind)
	}

	if _, bad := byID["component.vendor.x"]; bad {
		t.Error("vendor/ must be excluded from components")
	}
}

func TestExtractTests(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a", "a_test.go"),
		"package a\nimport \"testing\"\nfunc TestFoo(t *testing.T){}\nfunc TestBar(t *testing.T){}\nfunc helper(){}\n")
	writeFile(t, filepath.Join(root, "b", "thing.spec.ts"), "describe('x', () => {})")
	writeFile(t, filepath.Join(root, "c", "thing.go"), "package c\n") // not a test

	tests := extractTests(root)
	ids := map[string]bool{}
	for _, x := range tests {
		ids[x.ID] = true
	}
	if !ids["a/a_test.go:TestFoo"] || !ids["a/a_test.go:TestBar"] {
		t.Errorf("expected Go test funcs TestFoo/TestBar, got %v", ids)
	}
	if !ids["b/thing.spec.ts"] {
		t.Errorf("expected file-level TS test entry, got %v", ids)
	}
	if len(tests) != 3 {
		t.Errorf("expected 3 tests, got %d (%v)", len(tests), ids)
	}
}

func TestDotSlug(t *testing.T) {
	cases := map[string]string{
		"golang/server":    "golang.server",
		"cmd/awg":          "cmd.awg",
		"a-b/c.d":          "a_b.c_d",
		"pkg/lib_internal": "pkg.lib_internal",
	}
	for in, want := range cases {
		if got := dotSlug(in); got != want {
			t.Errorf("dotSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMergeImportGraphComponents_MergesEdgesIntoSingleComponentSet(t *testing.T) {
	base := []bootstrapComponent{
		{
			ID:          "component.cmd.server",
			Name:        "server",
			Kind:        "service",
			Assertion:   "inferred",
			SourceFiles: []string{"cmd/server/main.go"},
		},
	}
	imported := []importgraph.Component{
		{
			ID:              "component.cmd.server",
			Name:            "server",
			Kind:            "service",
			SourceFiles:     []string{"cmd/server/main.go", "cmd/server/http.go"},
			DependsOn:       []string{"component.pkg.lib"},
			ExternalImports: []string{"github.com/caddyserver/certmagic"},
		},
		{
			ID:          "component.pkg.lib",
			Name:        "lib",
			Kind:        "module",
			SourceFiles: []string{"pkg/lib/lib.go"},
		},
	}

	got := mergeImportGraphComponents(base, imported)
	if len(got) != 2 {
		t.Fatalf("component count=%d want 2: %+v", len(got), got)
	}
	byID := map[string]bootstrapComponent{}
	for _, comp := range got {
		byID[comp.ID] = comp
	}
	svc := byID["component.cmd.server"]
	if len(svc.SourceFiles) != 2 {
		t.Fatalf("merged source files=%v want 2 entries", svc.SourceFiles)
	}
	if len(svc.DependsOn) != 1 || svc.DependsOn[0] != "component.pkg.lib" {
		t.Fatalf("depends_on=%v want [component.pkg.lib]", svc.DependsOn)
	}
	if len(svc.External) != 1 || svc.External[0] != "github.com/caddyserver/certmagic" {
		t.Fatalf("external_imports=%v want certmagic", svc.External)
	}
	if _, ok := byID["component.pkg.lib"]; !ok {
		t.Fatalf("missing imported-only component: %+v", byID)
	}
}

func keysOf(m map[string]bootstrapComponent) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
