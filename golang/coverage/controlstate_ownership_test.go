// SPDX-License-Identifier: AGPL-3.0-only

package coverage

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestControlStateOwnershipBoundary proves the Phase 9.5 owner boundary statically: the
// controlstate package is transport-neutral (imports no server/protobuf/editor/CLI/os) and it is
// the single semantic composition owner — no consumer reimplements its classification and it
// never uses closure.Report.Verdict as artifact closure.
func TestControlStateOwnershipBoundary(t *testing.T) {
	root := repoRootForHighRisk(t)
	cs := scanPackage(t, filepath.Join(root, "golang", "architecture", "controlstate"))

	forbiddenImports := []string{
		"github.com/globulario/sensei/golang/server",
		"github.com/globulario/sensei/golang/pb",
		"os",
		"os/exec",
		"net",
		"net/http",
	}
	for _, imp := range forbiddenImports {
		if cs.imports[imp] {
			t.Errorf("controlstate must not import %q (transport-neutral, no ambient IO)", imp)
		}
	}
	for imp := range cs.imports {
		if strings.Contains(imp, "editor/") || strings.Contains(imp, "/cmd/") {
			t.Errorf("controlstate must not import editor/CLI package %q", imp)
		}
	}

	// The owner never consumes the task/scope closure assessing package (closure.Report) — it
	// derives artifact closure from its own per-class policies.
	if cs.imports["github.com/globulario/sensei/golang/architecture/closure"] {
		t.Errorf("controlstate must not import the closure.Report assessing owner as artifact closure")
	}

	// No consumer reimplements the semantic aggregators outside controlstate.
	server := scanPackage(t, filepath.Join(root, "golang", "server"))
	for _, forbidden := range []string{"aggregateArtifactClosure", "severityForClass", "buildArtifactAttention"} {
		if server.idents[forbidden] || server.funcs[forbidden] {
			t.Errorf("server must not reimplement controlstate aggregator %q", forbidden)
		}
	}
}

// TestControlStateTransportOwnershipBoundary proves the Checkpoint-2 transport boundary
// statically:
//
//   - the pure mapper (golang/server/controlstateproto) imports ONLY controlstate, the generated
//     protobuf package, gRPC-free stdlib — no graph stores, governed-YAML readers, mutation
//     owners, or certification writers;
//   - the mapper and server define no second semantic vocabulary (no closure/severity/lifecycle
//     mapping tables outside the controlstate owner);
//   - the read-only store interface the handlers consume has NO write method (structural
//     no-mutation proof for the control-panel read surfaces).
func TestControlStateTransportOwnershipBoundary(t *testing.T) {
	root := repoRootForHighRisk(t)

	mapper := scanPackage(t, filepath.Join(root, "golang", "server", "controlstateproto"))
	allowedPrefixes := []string{
		"github.com/globulario/sensei/golang/architecture/controlstate",
		"github.com/globulario/sensei/golang/pb",
	}
	for imp := range mapper.imports {
		if strings.Contains(imp, ".") && strings.Contains(imp, "/") { // non-stdlib
			ok := false
			for _, p := range allowedPrefixes {
				if strings.HasPrefix(imp, p) {
					ok = true
				}
			}
			if !ok {
				t.Errorf("controlstateproto must stay transport-only; imports %q", imp)
			}
		}
	}
	// The mapper never recomputes digests or reimplements semantic aggregation.
	for _, forbidden := range []string{"SemanticDigest", "aggregateArtifactClosure", "severityForClass", "mapGovernedStatus", "aggregateAvailability"} {
		if mapper.funcs[forbidden] {
			t.Errorf("controlstateproto must not define semantic function %q", forbidden)
		}
	}

	// The server holds no second status→lifecycle / severity / closure table.
	server := scanPackage(t, filepath.Join(root, "golang", "server"))
	for _, forbidden := range []string{"mapGovernedStatus", "aggregateArtifactClosure", "severityForClass", "aggregateAvailability", "dimensionStateFor"} {
		if server.funcs[forbidden] {
			t.Errorf("server must not reimplement controlstate vocabulary %q", forbidden)
		}
	}

	// Structural no-mutation: the store interface behind the read handlers has no write verb.
	storePkg := scanPackage(t, filepath.Join(root, "golang", "store"))
	for ident := range storePkg.funcs {
		low := strings.ToLower(ident)
		for _, verb := range []string{"insert", "update", "delete", "write", "put", "mutate"} {
			if strings.HasPrefix(low, verb) {
				t.Errorf("read store surface gained a write-shaped function %q", ident)
			}
		}
	}
}
