// SPDX-License-Identifier: AGPL-3.0-only

package coverage

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestRuntimeProbeOwnershipBoundary proves the Phase 9.7 CP2 adapter boundary statically: the PURE
// runtimeprobe core imports only the runtimeboundary owner + closureprotocol receipt primitives — NOT
// the os-tainted probe package, and nothing transport/store/mutation. The single seam that reads
// probe.ProbeResult is isolated in the runtimeprobe/probesource subpackage. The core emits no RDF and
// exposes no write-verb surface, so observed probe evidence can neither mutate nor be projected into
// the graph.
func TestRuntimeProbeOwnershipBoundary(t *testing.T) {
	root := repoRootForHighRisk(t)
	core := scanPackage(t, filepath.Join(root, "golang", "architecture", "runtimeprobe"))

	forbidden := []string{
		"github.com/globulario/sensei/golang/architecture/probe", // os-tainted; only the seam may import it
		"github.com/globulario/sensei/golang/server",
		"github.com/globulario/sensei/golang/pb",
		"github.com/globulario/sensei/golang/store",
		"os",
		"os/exec",
		"net",
		"net/http",
	}
	for _, imp := range forbidden {
		if core.imports[imp] {
			t.Errorf("runtimeprobe core must not import %q (pure; probe seam is separate)", imp)
		}
	}
	for imp := range core.imports {
		if strings.Contains(imp, "editor/") || strings.Contains(imp, "/cmd/") {
			t.Errorf("runtimeprobe core must not import editor/CLI package %q", imp)
		}
	}

	// Only runtimeboundary + closureprotocol are permitted internal (non-stdlib) deps.
	allowed := []string{
		"github.com/globulario/sensei/golang/architecture/runtimeboundary",
		"github.com/globulario/sensei/golang/architecture/closureprotocol",
	}
	for imp := range core.imports {
		if strings.Contains(imp, ".") && strings.Contains(imp, "/") { // non-stdlib
			ok := false
			for _, p := range allowed {
				if strings.HasPrefix(imp, p) {
					ok = true
				}
			}
			if !ok {
				t.Errorf("runtimeprobe core may only depend on runtimeboundary/closureprotocol; imports %q", imp)
			}
		}
	}

	// No RDF emission and no write-verb surface.
	for _, f := range []string{"rdf.Builder", ".Typed(", ".Emit(", ".Quad(", ".Triple("} {
		if strings.Contains(core.rawText, f) {
			t.Errorf("runtimeprobe core must not emit RDF (found %q)", f)
		}
	}
	for ident := range core.funcs {
		low := strings.ToLower(ident)
		for _, verb := range []string{"insert", "update", "delete", "write", "put", "mutate", "commit", "persist"} {
			if strings.HasPrefix(low, verb) {
				t.Errorf("runtimeprobe core gained a write-shaped function %q (must stay read-only)", ident)
			}
		}
	}

	// The seam MAY import probe; confirm it exists and is the isolation point.
	seam := scanPackage(t, filepath.Join(root, "golang", "architecture", "runtimeprobe", "probesource"))
	if !seam.imports["github.com/globulario/sensei/golang/architecture/probe"] {
		t.Errorf("the probesource seam should be the (only) importer of the probe package")
	}
}
