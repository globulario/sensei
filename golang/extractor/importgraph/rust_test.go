// SPDX-License-Identifier: AGPL-3.0-only

package importgraph

import (
	"path/filepath"
	"strings"
	"testing"
)

// Rust parser fixtures — all fictional (acme.test workspace), no real-project paths.

func cargo(name string) string { return "[package]\nname = \"" + name + "\"\nversion = \"0.1.0\"\n" }

// TestScan_RS_CrossCrateTopLevel: top-level crate layout. A cross-crate use →
// one depends_on; crate::/mod/std intra/stdlib produce no edge; third-party and
// `extern crate` → external; main.rs → service.
func TestScan_RS_CrossCrateTopLevel(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foo", "Cargo.toml"), cargo("foo"))
	writeFile(t, filepath.Join(root, "foo", "src", "main.rs"), `
use bar::run;
use serde::Serialize;
use std::collections::HashMap;
use crate::util::helper;
mod util;
extern crate libc;
fn main() {}
`)
	writeFile(t, filepath.Join(root, "foo", "src", "util.rs"), "pub fn helper() {}\n")
	writeFile(t, filepath.Join(root, "bar", "Cargo.toml"), cargo("bar"))
	writeFile(t, filepath.Join(root, "bar", "src", "lib.rs"), "pub fn run() {}\n")

	doc, err := Scan(root, "rust", Config{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	foo := findComp(doc, "component.foo")
	if foo == nil {
		t.Fatalf("missing component.foo; got %d components", len(doc.Components))
	}
	if foo.Kind != "service" {
		t.Errorf("foo kind = %q, want service (has main.rs)", foo.Kind)
	}
	if got := foo.DependsOn; len(got) != 1 || got[0] != "component.bar" {
		t.Errorf("foo depends_on = %v, want [component.bar] (cross-crate use)", got)
	}
	if !contains(foo.ExternalImports, "serde") || !contains(foo.ExternalImports, "libc") {
		t.Errorf("external_imports = %v, want serde + libc (extern crate)", foo.ExternalImports)
	}
	for _, leak := range []string{"std", "bar", "crate"} {
		if contains(foo.ExternalImports, leak) {
			t.Errorf("%q leaked into external_imports: %v", leak, foo.ExternalImports)
		}
	}
	if !contains(foo.SourceFiles, "foo/src/main.rs") || !contains(foo.SourceFiles, "foo/src/util.rs") {
		t.Errorf("foo source_files missing main.rs/util.rs: %v", foo.SourceFiles)
	}
	if findComp(doc, "component.bar") == nil {
		t.Error("missing component.bar")
	}
}

// TestScan_RS_CratesWorkspace exercises the new `crates` source root and crate
// dash→underscore normalization: two cross-crate edges from a crates/ layout.
func TestScan_RS_CratesWorkspace(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "crates", "alpha", "Cargo.toml"), cargo("alpha"))
	writeFile(t, filepath.Join(root, "crates", "alpha", "src", "lib.rs"),
		"use beta::go;\nuse baz_utils::helper;\n")
	writeFile(t, filepath.Join(root, "crates", "beta", "Cargo.toml"), cargo("beta"))
	writeFile(t, filepath.Join(root, "crates", "beta", "src", "lib.rs"), "pub fn go() {}\n")
	writeFile(t, filepath.Join(root, "crates", "baz", "Cargo.toml"), cargo("baz-utils")) // dash
	writeFile(t, filepath.Join(root, "crates", "baz", "src", "lib.rs"), "pub fn helper() {}\n")

	doc, err := Scan(root, "rust", Config{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	alpha := findComp(doc, "component.crates.alpha")
	if alpha == nil {
		t.Fatalf("missing component.crates.alpha; got %v", componentIDs(doc))
	}
	want := []string{"component.crates.baz", "component.crates.beta"}
	if len(alpha.DependsOn) != 2 || alpha.DependsOn[0] != want[0] || alpha.DependsOn[1] != want[1] {
		t.Errorf("alpha depends_on = %v, want %v (crates root + baz-utils→baz_utils normalization)", alpha.DependsOn, want)
	}
}

// TestScan_RS_ClassifierUpgrade — a rust classifier rule upgrades a crate root
// into a semantic edge; a go rule is ignored during a Rust scan.
func TestScan_RS_ClassifierUpgrade(t *testing.T) {
	root := t.TempDir()
	// "gateway" is not a known source root → component.gateway (rolls to top-level dir).
	writeFile(t, filepath.Join(root, "gateway", "Cargo.toml"), cargo("gateway"))
	writeFile(t, filepath.Join(root, "gateway", "src", "lib.rs"), "use acme_payments::charge;\n")

	cfg := Config{Classifiers: []Rule{
		{ID: "go_noise", Language: "go", Match: `.*payments.*`, Edge: "reads_from", Target: "component.go_pay"},
		{ID: "rs_acme", Language: "rust", Match: `^acme_(.+)$`, Edge: "reads_from", Target: "component.$1"},
	}}
	doc, err := Scan(root, "rust", cfg)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	app := findComp(doc, "component.gateway")
	if app == nil {
		t.Fatal("missing component.gateway")
	}
	if !contains(app.ReadsFrom, "component.payments") {
		t.Errorf("reads_from = %v, want component.payments (rust classifier upgrade)", app.ReadsFrom)
	}
	if contains(app.ReadsFrom, "component.go_pay") {
		t.Errorf("go classifier leaked into a rust scan: %v", app.ReadsFrom)
	}
	if contains(app.ExternalImports, "acme_payments") {
		t.Errorf("classified import should not also be external: %v", app.ExternalImports)
	}
}

// TestScan_RS_TestsExcluded — files under tests/ (integration tests) contribute nothing.
func TestScan_RS_TestsExcluded(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foo", "Cargo.toml"), cargo("foo"))
	writeFile(t, filepath.Join(root, "foo", "src", "lib.rs"), "use realdep::x;\n")
	writeFile(t, filepath.Join(root, "foo", "tests", "it.rs"), "use secretdep::y;\n")

	doc, err := Scan(root, "rust", Config{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	foo := findComp(doc, "component.foo")
	if foo == nil {
		t.Fatal("missing component.foo")
	}
	for _, f := range foo.SourceFiles {
		if strings.Contains(f, "/tests/") {
			t.Errorf("tests/ file leaked into source_files: %s", f)
		}
	}
	if contains(foo.ExternalImports, "secretdep") {
		t.Errorf("tests/ import leaked: %v", foo.ExternalImports)
	}
	if !contains(foo.ExternalImports, "realdep") {
		t.Errorf("external_imports = %v, want realdep", foo.ExternalImports)
	}
}

// TestScan_RS_Determinism — repeated Scan+Render is byte-identical; header names Rust.
func TestScan_RS_Determinism(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foo", "Cargo.toml"), cargo("foo"))
	writeFile(t, filepath.Join(root, "foo", "src", "lib.rs"), "use bar::x;\n")
	writeFile(t, filepath.Join(root, "bar", "Cargo.toml"), cargo("bar"))
	writeFile(t, filepath.Join(root, "bar", "src", "lib.rs"), "pub fn x() {}\n")

	render := func() []byte {
		doc, err := Scan(root, "rust", Config{})
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		out, err := Render(doc, "rust")
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		return out
	}
	first := render()
	if string(first) != string(render()) {
		t.Error("Scan+Render is not deterministic")
	}
	if !strings.Contains(string(first), "-lang rust") {
		t.Error("render header should name the rust language")
	}
}

func componentIDs(doc Doc) []string {
	out := make([]string, 0, len(doc.Components))
	for _, c := range doc.Components {
		out = append(out, c.ID)
	}
	return out
}
