// SPDX-License-Identifier: AGPL-3.0-only

package coverage

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestRuntimeBoundaryOwnershipBoundary proves the Phase 9.7 CP1 owner boundary statically: the
// runtimeboundary package is a PURE, transport-neutral assessment owner. It imports only stdlib plus
// the closure-protocol primitives and the RDF vocabulary; it emits no RDF, has no write-verb surface,
// and is not re-implemented by any consumer. This enforces "observed traffic cannot create
// architecture" structurally — the owner can neither mutate nor project runtime data into the graph.
func TestRuntimeBoundaryOwnershipBoundary(t *testing.T) {
	root := repoRootForHighRisk(t)
	pkg := scanPackage(t, filepath.Join(root, "golang", "architecture", "runtimeboundary"))

	forbiddenImports := []string{
		"github.com/globulario/sensei/golang/server",
		"github.com/globulario/sensei/golang/pb",
		"github.com/globulario/sensei/golang/store",
		"github.com/globulario/sensei/golang/architecture/controlstate",
		"os",
		"os/exec",
		"net",
		"net/http",
	}
	for _, imp := range forbiddenImports {
		if pkg.imports[imp] {
			t.Errorf("runtimeboundary must not import %q (pure, transport-neutral, no ambient IO)", imp)
		}
	}
	for imp := range pkg.imports {
		if strings.Contains(imp, "editor/") || strings.Contains(imp, "/cmd/") {
			t.Errorf("runtimeboundary must not import editor/CLI package %q", imp)
		}
	}

	// Only closureprotocol + rdf are permitted internal (non-stdlib) dependencies.
	allowedPrefixes := []string{
		"github.com/globulario/sensei/golang/architecture/closureprotocol",
		"github.com/globulario/sensei/golang/rdf",
	}
	for imp := range pkg.imports {
		if strings.Contains(imp, ".") && strings.Contains(imp, "/") { // non-stdlib
			ok := false
			for _, p := range allowedPrefixes {
				if strings.HasPrefix(imp, p) {
					ok = true
				}
			}
			if !ok {
				t.Errorf("runtimeboundary may only depend on closureprotocol/rdf; imports %q", imp)
			}
		}
	}

	// No RDF emission: the owner reads the RDF vocabulary (class IRIs) but never writes triples. A
	// projection into the graph is a CP2/CP3 consumer concern, never this pure owner's.
	for _, forbidden := range []string{"rdf.Builder", ".Typed(", ".Emit(", ".Quad(", ".Triple("} {
		if strings.Contains(pkg.rawText, forbidden) {
			t.Errorf("runtimeboundary must not emit RDF (found %q)", forbidden)
		}
	}

	// Structural no-mutation: the pure owner exposes no write-shaped function.
	for ident := range pkg.funcs {
		low := strings.ToLower(ident)
		for _, verb := range []string{"insert", "update", "delete", "write", "put", "mutate", "commit", "persist"} {
			if strings.HasPrefix(low, verb) {
				t.Errorf("runtimeboundary gained a write-shaped function %q (must stay read-only)", ident)
			}
		}
	}

	// No consumer re-implements the assessment: the server holds no second runtime-boundary verdict
	// function (CP1 wires no server integration; this guards the boundary as consumers arrive).
	server := scanPackage(t, filepath.Join(root, "golang", "server"))
	for _, forbidden := range []string{"AssessRuntimeBoundary", "classifyCrossing", "resultKindVerdict"} {
		if server.funcs[forbidden] {
			t.Errorf("server must not reimplement runtimeboundary assessment %q", forbidden)
		}
	}
}
