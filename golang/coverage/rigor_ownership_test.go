// SPDX-License-Identifier: AGPL-3.0-only

package coverage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/rigor"
)

// TestRigorOwnershipBoundary proves the Phase 9 proportional-rigor classifier is a pure,
// self-contained decision function (issue #93): it imports only stdlib + yaml, reaches nothing
// transport/store/editor/os/net, emits no RDF, and exposes no write-verb surface. It answers "what
// proof does a change owe"; it enforces nothing and mutates nothing.
func TestRigorOwnershipBoundary(t *testing.T) {
	root := repoRootForHighRisk(t)
	pkg := scanPackage(t, filepath.Join(root, "golang", "architecture", "rigor"))

	allowed := []string{"gopkg.in/yaml.v3"}
	for imp := range pkg.imports {
		if strings.Contains(imp, ".") && strings.Contains(imp, "/") { // non-stdlib
			ok := false
			for _, p := range allowed {
				if strings.HasPrefix(imp, p) {
					ok = true
				}
			}
			if !ok {
				t.Errorf("rigor may only depend on stdlib + yaml; imports %q", imp)
			}
		}
	}
	for _, imp := range []string{"os", "os/exec", "net", "net/http",
		"github.com/globulario/sensei/golang/server", "github.com/globulario/sensei/golang/pb",
		"github.com/globulario/sensei/golang/store"} {
		if pkg.imports[imp] {
			t.Errorf("rigor must not import %q (pure, filesystem-free classifier)", imp)
		}
	}
	for imp := range pkg.imports {
		if strings.Contains(imp, "editor/") || strings.Contains(imp, "/cmd/") {
			t.Errorf("rigor must not import editor/CLI package %q", imp)
		}
	}
	for _, f := range []string{"rdf.Builder", ".Typed(", ".Emit(", ".Quad(", ".Triple("} {
		if strings.Contains(pkg.rawText, f) {
			t.Errorf("rigor must not emit RDF (found %q)", f)
		}
	}
	for ident := range pkg.funcs {
		low := strings.ToLower(ident)
		for _, verb := range []string{"insert", "update", "delete", "write", "put", "mutate", "commit", "persist"} {
			if strings.HasPrefix(low, verb) {
				t.Errorf("rigor gained a write-shaped function %q (must stay read-only)", ident)
			}
		}
	}
}

// TestRigorManifestBindsToRealPackages proves the committed surface manifest agrees with reality:
// it parses, and every governed surface's owned package prefix points at a path that exists on
// disk. This is the "changed-file set / ownership map / declared class must agree" law — a surface
// can never classify code that does not exist.
func TestRigorManifestBindsToRealPackages(t *testing.T) {
	root := repoRootForHighRisk(t)
	data, err := os.ReadFile(filepath.Join(root, "docs", "rigor_classes.yaml"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	m, err := rigor.ParseManifest(data)
	if err != nil {
		t.Fatalf("the committed rigor manifest must parse: %v", err)
	}
	if len(m.Surfaces) == 0 {
		t.Fatal("the rigor manifest must declare at least one governed surface")
	}
	for _, s := range m.Surfaces {
		for _, p := range s.Packages {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err != nil {
				t.Errorf("surface %q owns package %q which does not exist on disk: %v", s.ID, p, err)
			}
		}
	}
}
