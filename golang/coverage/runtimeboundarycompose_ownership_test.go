// SPDX-License-Identifier: AGPL-3.0-only

package coverage

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestRuntimeBoundaryComposeOwnershipBoundary proves the Phase 9.7 CP3 anti-doppelgänger DAG
// statically. Runtime-boundary truth reaches the control panel through ONE bridge package that
// projects an already-decided verdict verbatim; no downstream surface re-derives runtime-boundary
// semantics.
//
//	runtimeboundarycompose ──► runtimeboundary   (reads a.Verdict; never re-assesses)
//	         └────────────────► controlstate      (fills its typed DimensionObservation)
//
// controlstate must NOT import runtimeboundary (else it could reach the verdict logic and recompute);
// runtimeboundary must NOT import controlstate; the server carries the runtime dimension through the
// generic transport and never imports the runtimeboundary owner directly.
func TestRuntimeBoundaryComposeOwnershipBoundary(t *testing.T) {
	root := repoRootForHighRisk(t)
	bridge := scanPackage(t, filepath.Join(root, "golang", "architecture", "runtimeboundarycompose"))

	// 1. The bridge depends only on the two owners it projects between (+ stdlib) — nothing
	//    transport/store/editor/os/net, so it cannot grow a side channel.
	allowed := []string{
		"github.com/globulario/sensei/golang/architecture/runtimeboundary",
		"github.com/globulario/sensei/golang/architecture/controlstate",
		"github.com/globulario/sensei/golang/architecture/closureprotocol",
		"github.com/globulario/sensei/golang/rdf",
	}
	for imp := range bridge.imports {
		if strings.Contains(imp, ".") && strings.Contains(imp, "/") { // non-stdlib
			ok := false
			for _, p := range allowed {
				if strings.HasPrefix(imp, p) {
					ok = true
				}
			}
			if !ok {
				t.Errorf("runtimeboundarycompose may only depend on runtimeboundary/controlstate; imports %q", imp)
			}
		}
	}
	for _, imp := range []string{"os", "os/exec", "net", "net/http",
		"github.com/globulario/sensei/golang/server", "github.com/globulario/sensei/golang/pb",
		"github.com/globulario/sensei/golang/store"} {
		if bridge.imports[imp] {
			t.Errorf("runtimeboundarycompose must not import %q (pure projection)", imp)
		}
	}

	// 2. The bridge re-runs NO assessment — it reads the decided verdict, never re-derives it.
	for _, forbidden := range []string{"AssessRuntimeBoundary(", "classifyCrossing(", "resultKindVerdict("} {
		if strings.Contains(bridge.rawText, forbidden) {
			t.Errorf("runtimeboundarycompose must not re-run assessment (found %q)", forbidden)
		}
	}

	// 3. controlstate is the composer; it must never reach the runtimeboundary owner nor the bridge
	//    (no doppelgänger: it only ever sees its own typed DimensionObservation).
	control := scanPackage(t, filepath.Join(root, "golang", "architecture", "controlstate"))
	for _, forbidden := range []string{
		"github.com/globulario/sensei/golang/architecture/runtimeboundary",
		"github.com/globulario/sensei/golang/architecture/runtimeboundarycompose",
	} {
		if control.imports[forbidden] {
			t.Errorf("controlstate must not import %q (it composes typed observations, not verdicts)", forbidden)
		}
	}
	for _, forbidden := range []string{"AssessRuntimeBoundary", "classifyCrossing", "resultKindVerdict"} {
		if strings.Contains(control.rawText, forbidden) {
			t.Errorf("controlstate must not redefine runtime-boundary assessment logic (found %q)", forbidden)
		}
	}

	// 4. The server carries the runtime dimension through the generic transport; it must not import
	//    the runtimeboundary owner directly (only the bridge, if anything, may).
	for _, pkg := range []string{
		filepath.Join(root, "golang", "server"),
		filepath.Join(root, "golang", "server", "controlstateproto"),
	} {
		sp := scanPackage(t, pkg)
		if sp.imports["github.com/globulario/sensei/golang/architecture/runtimeboundary"] {
			t.Errorf("%s must not import the runtimeboundary owner directly (compose through the bridge)", pkg)
		}
	}
}
