// SPDX-License-Identifier: Apache-2.0

package importgraph

import (
	"path/filepath"
	"strings"
	"testing"
)

// Go-parser fixtures. All fictional (module acme.test/app) — no real-project paths.

// TestScan_Go_InternalDependsOn covers internal cross-component imports, stdlib
// ignore, external recording, multiple files in one component, intra-component
// dedup, test-file exclusion, and service-kind detection — no classifier.
func TestScan_Go_InternalDependsOn(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), fixtureModule)
	writeFile(t, filepath.Join(root, "golang", "server", "srv.go"),
		"package server\nimport (\n\t\"fmt\"\n\t\"acme.test/app/golang/store\"\n\t\"github.com/third/party\"\n)\nvar _ = fmt.Sprint\n")
	writeFile(t, filepath.Join(root, "golang", "server", "srv2.go"),
		"package server\nimport (\n\t\"acme.test/app/golang/store\"\n\t\"acme.test/app/golang/server/internal/util\"\n)\n")
	writeFile(t, filepath.Join(root, "golang", "store", "store.go"),
		"package store\nimport \"acme.test/app/golang/rdf\"\n")
	writeFile(t, filepath.Join(root, "golang", "rdf", "rdf.go"), "package rdf\n")
	writeFile(t, filepath.Join(root, "cmd", "app", "main.go"),
		"package main\nimport \"acme.test/app/golang/server\"\nfunc main(){}\n")
	// test file must NOT contribute imports or source files
	writeFile(t, filepath.Join(root, "golang", "server", "srv_test.go"),
		"package server\nimport \"acme.test/app/golang/secret\"\n")

	doc, err := Scan(root, "go", Config{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	srv := findComp(doc, "component.golang.server")
	if srv == nil {
		t.Fatalf("missing component.golang.server; got %d components", len(doc.Components))
	}
	if srv.Kind != "module" || srv.Assertion != "inferred" {
		t.Errorf("server kind=%q assertion=%q, want module/inferred", srv.Kind, srv.Assertion)
	}
	if got := srv.DependsOn; len(got) != 1 || got[0] != "component.golang.store" {
		t.Errorf("server depends_on = %v, want [component.golang.store] (deduped, intra-component skipped)", got)
	}
	if !contains(srv.SourceFiles, "golang/server/srv.go") || !contains(srv.SourceFiles, "golang/server/srv2.go") {
		t.Errorf("server source_files missing srv.go/srv2.go: %v", srv.SourceFiles)
	}
	if contains(srv.SourceFiles, "golang/server/srv_test.go") {
		t.Errorf("test file leaked into source_files: %v", srv.SourceFiles)
	}
	if !contains(srv.ExternalImports, "github.com/third/party") {
		t.Errorf("server external_imports = %v, want github.com/third/party", srv.ExternalImports)
	}
	if contains(srv.ExternalImports, "fmt") {
		t.Errorf("stdlib fmt leaked into external_imports: %v", srv.ExternalImports)
	}
	if contains(srv.DependsOn, "component.golang.secret") {
		t.Errorf("test-only import produced an edge: %v", srv.DependsOn)
	}

	if store := findComp(doc, "component.golang.store"); store == nil || !contains(store.DependsOn, "component.golang.rdf") {
		t.Errorf("store should depend on component.golang.rdf; got %+v", store)
	}
	if cmdApp := findComp(doc, "component.cmd.app"); cmdApp == nil {
		t.Fatal("missing component.cmd.app")
	} else if cmdApp.Kind != "service" {
		t.Errorf("cmd/app kind = %q, want service (has main.go)", cmdApp.Kind)
	} else if !contains(cmdApp.DependsOn, "component.golang.server") {
		t.Errorf("cmd/app depends_on = %v, want component.golang.server", cmdApp.DependsOn)
	}
}

func TestScan_Go_RootPackageIsCanonicalComponent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), fixtureModule)
	writeFile(t, filepath.Join(root, "gin.go"), "package gin\nimport \"acme.test/app/render\"\n")
	writeFile(t, filepath.Join(root, "tree.go"), "package gin\n")
	writeFile(t, filepath.Join(root, "context_test.go"), "package gin\n")
	writeFile(t, filepath.Join(root, "generated.pb.go"), "package gin\n")
	writeFile(t, filepath.Join(root, "render", "render.go"), "package render\nimport \"acme.test/app\"\n")

	doc, err := Scan(root, "go", Config{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	gin := findComp(doc, "component.gin")
	if gin == nil {
		t.Fatalf("missing component.gin: %+v", doc.Components)
	}
	if gin.Name != "gin" {
		t.Fatalf("root component name=%q want gin", gin.Name)
	}
	if got := gin.SourceFiles; len(got) != 2 || !contains(got, "gin.go") || !contains(got, "tree.go") {
		t.Fatalf("root source_files=%v want gin.go and tree.go", got)
	}
	if !contains(gin.DependsOn, "component.render") {
		t.Fatalf("root depends_on=%v want component.render", gin.DependsOn)
	}
	render := findComp(doc, "component.render")
	if render == nil || !contains(render.DependsOn, "component.gin") {
		t.Fatalf("root-target import was not represented: %+v", render)
	}
}

// TestScan_Go_ClassifierUpgrade is the fictional custom-classifier fixture: a
// project rule upgrades a raw import into a semantic reads_from edge.
func TestScan_Go_ClassifierUpgrade(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), fixtureModule)
	writeFile(t, filepath.Join(root, "golang", "consumer", "c.go"),
		"package consumer\nimport \"acme.test/platform/billing/billing_gateway\"\n")

	// NOTE: Go regexp is RE2 — no backreferences. Capture the service name.
	cfg := Config{Classifiers: []Rule{{
		ID:       "platform_gateway_read",
		Language: "go",
		Match:    `^acme\.test/platform/([a-z]+)/[a-z]+_gateway$`,
		Edge:     "reads_from",
		Target:   "component.$1",
	}}}

	doc, err := Scan(root, "go", cfg)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	c := findComp(doc, "component.golang.consumer")
	if c == nil {
		t.Fatal("missing component.golang.consumer")
	}
	if !contains(c.ReadsFrom, "component.billing") {
		t.Errorf("reads_from = %v, want component.billing (classifier upgrade)", c.ReadsFrom)
	}
	if len(c.ExternalImports) != 0 {
		t.Errorf("classified import leaked into external_imports: %v", c.ExternalImports)
	}

	// Without the rule, the same import is a plain external import.
	doc2, _ := Scan(root, "go", Config{})
	c2 := findComp(doc2, "component.golang.consumer")
	if c2 == nil || !contains(c2.ExternalImports, "acme.test/platform/billing/billing_gateway") {
		t.Errorf("without classifier, import should be external; got %+v", c2)
	}
	if c2 != nil && len(c2.ReadsFrom) != 0 {
		t.Errorf("without classifier there should be no reads_from; got %v", c2.ReadsFrom)
	}
}

// TestScan_Go_NestedRepoIgnored — a subdirectory that is its own repository
// (contains a .git entry, e.g. a submodule or a CI-placed nested checkout) is a
// repository boundary and must contribute no component, source_file, or edge.
// The rule is generic: nested .git, not a hardcoded directory name.
func TestScan_Go_NestedRepoIgnored(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), fixtureModule)
	// a normal component that SHOULD be scanned (with a normal internal edge)
	writeFile(t, filepath.Join(root, "golang", "app", "app.go"),
		"package app\nimport \"acme.test/app/golang/util\"\n")
	writeFile(t, filepath.Join(root, "golang", "util", "util.go"), "package util\n")
	// a nested repository under a known source root ("services") — its .git makes
	// it a boundary. Its .go would otherwise emit component.services.golang plus a
	// dependsOn edge; skipping the boundary means none of that happens.
	writeFile(t, filepath.Join(root, "services", ".git", "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(root, "services", "golang", "echo", "echo.go"),
		"package echo\nimport \"acme.test/app/golang/util\"\n")
	// also cover a deeper nested .git
	writeFile(t, filepath.Join(root, "vendored", "lib", ".git", "HEAD"), "ref: x\n")
	writeFile(t, filepath.Join(root, "vendored", "lib", "lib.go"), "package lib\n")

	doc, err := Scan(root, "go", Config{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// the root repo stays scannable, normal edge intact
	app := findComp(doc, "component.golang.app")
	if app == nil || !contains(app.DependsOn, "component.golang.util") {
		t.Fatalf("the normal component.golang.app (→ util) must still be scanned; got %+v", app)
	}
	// the nested repos contribute no component, no source_file, no dependsOn edge
	for _, n := range doc.Components {
		if strings.HasPrefix(n.ID, "component.services") || n.ID == "component.vendored" {
			t.Errorf("nested repo produced a component: %s", n.ID)
		}
		for _, f := range n.SourceFiles {
			if strings.HasPrefix(f, "services/") || strings.HasPrefix(f, "vendored/") {
				t.Errorf("nested-repo file leaked into source_files: %s", f)
			}
		}
	}
}

// TestGoImportGraph_ModuleInSubdirEmitsEdges is the regression for
// failure.importgraph.go_no_edges_when_gomod_in_subdir: when the module's go.mod
// lives in a subdirectory (e.g. golang/go.mod, as in globular/services) rather
// than at the repo root, internal imports must still resolve to cross-component
// dependsOn edges. The pre-fix extractor read only root/go.mod, got module="",
// classified every import as external/stdlib, and emitted ZERO edges.
func TestGoImportGraph_ModuleInSubdirEmitsEdges(t *testing.T) {
	root := t.TempDir()
	// Module declared in golang/go.mod — NOT at the repo root. The module path
	// tail ("proj") deliberately differs from the dir name ("golang") so the
	// target must be re-prefixed with the module's dir, not taken module-relative.
	writeFile(t, filepath.Join(root, "golang", "go.mod"), "module example.test/proj\n")
	writeFile(t, filepath.Join(root, "golang", "server", "srv.go"),
		"package server\nimport (\n\t\"fmt\"\n\t\"example.test/proj/store\"\n\t\"github.com/third/party\"\n)\nvar _ = fmt.Sprint\n")
	writeFile(t, filepath.Join(root, "golang", "store", "store.go"), "package store\n")

	doc, err := Scan(root, "go", Config{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	srv := findComp(doc, "component.golang.server")
	if srv == nil {
		t.Fatalf("missing component.golang.server; got %d components", len(doc.Components))
	}
	if got := srv.DependsOn; len(got) != 1 || got[0] != "component.golang.store" {
		t.Errorf("subdir-module internal import produced no/wrong edge: depends_on=%v, want [component.golang.store]", got)
	}
	if !contains(srv.ExternalImports, "github.com/third/party") {
		t.Errorf("external import lost: %v", srv.ExternalImports)
	}
	if contains(srv.ExternalImports, "example.test/proj/store") {
		t.Errorf("internal import mis-classified as external: %v", srv.ExternalImports)
	}
}

// TestGoImportGraph_DropsDanglingEdgeToGeneratedOnlyPackage is the regression
// for failure.importgraph.go_dangling_dependson_edge_to_unscanned_package: an
// import of a package whose only files are generated/excluded (e.g. a .pb.go-only
// proto package) rolls up to no scanned component, so it must NOT produce a
// dangling dependsOn edge to a component node that was never emitted. Real
// internal edges in the same component are unaffected.
func TestGoImportGraph_DropsDanglingEdgeToGeneratedOnlyPackage(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), fixtureModule) // acme.test/app
	writeFile(t, filepath.Join(root, "golang", "svc", "s.go"),
		"package svc\nimport (\n\t\"acme.test/app/golang/store\"\n\t\"acme.test/app/golang/authpb\"\n)\n")
	writeFile(t, filepath.Join(root, "golang", "store", "store.go"), "package store\n")
	// authpb has ONLY a generated .pb.go file → excluded from scanning → no node.
	writeFile(t, filepath.Join(root, "golang", "authpb", "auth.pb.go"), "package authpb\n")

	doc, err := Scan(root, "go", Config{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	svc := findComp(doc, "component.golang.svc")
	if svc == nil {
		t.Fatalf("missing component.golang.svc")
	}
	if !contains(svc.DependsOn, "component.golang.store") {
		t.Errorf("real internal edge lost: depends_on=%v", svc.DependsOn)
	}
	if contains(svc.DependsOn, "component.golang.authpb") {
		t.Errorf("dangling edge to generated-only package not dropped: %v", svc.DependsOn)
	}
	if findComp(doc, "component.golang.authpb") != nil {
		t.Error("generated-only package must not produce a component node")
	}
}

// TestScan_Go_StdlibIgnored — a component importing only stdlib has no edges.
func TestScan_Go_StdlibIgnored(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), fixtureModule)
	writeFile(t, filepath.Join(root, "pkg", "util", "u.go"),
		"package util\nimport (\n\t\"fmt\"\n\t\"net/http\"\n\t\"os\"\n)\nvar _ = fmt.Sprint\n")

	doc, err := Scan(root, "go", Config{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	u := findComp(doc, "component.pkg.util")
	if u == nil {
		t.Fatal("missing component.pkg.util")
	}
	if len(u.DependsOn) != 0 || len(u.ExternalImports) != 0 {
		t.Errorf("stdlib-only component should have no edges: depends_on=%v external=%v", u.DependsOn, u.ExternalImports)
	}
}
