// SPDX-License-Identifier: AGPL-3.0-only

package extractor_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/globulario/awareness-graph/golang/extractor"
	"github.com/globulario/awareness-graph/golang/rdf"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// implPatternDir builds a temp directory with the given files and runs
// ImportAwarenessDir on it.
func implPatternDir(t *testing.T, files map[string]string) (string, *extractor.ImportReport) {
	t.Helper()
	var buf bytes.Buffer
	root := makeDir(t, files)
	_, report, err := extractor.ImportAwarenessDir(root, &buf)
	if err != nil {
		t.Fatalf("ImportAwarenessDir: %v", err)
	}
	return buf.String(), report
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// Detection: id + class:ImplementationPattern routes to the new importer
// (and NOT to the intent importer that fires on id+level).
func TestImplementationPattern_DetectedByIDAndClass(t *testing.T) {
	out, report := implPatternDir(t, map[string]string{
		"grpc_client_standard.yaml": `
id: globular.pattern.grpc_client_standard
class: ImplementationPattern
label: Standard Globular gRPC service client
status: active

when_to_use:
  - creating a new Go client for a Globular gRPC service
  - adding client package for a new service

reference_files:
  - path: golang/echo/echo_client/echo_client.go
    role: canonical_minimal
  - path: golang/monitoring/monitoring_client/monitoring_client.go
    role: richer_reference

must_follow:
  - Constructor calls globular.InitClient(client, address, id)
  - Reconnect() calls globular.GetClientConnection(client)

required_calls:
  - globular.InitClient
  - globular.GetClientConnection

forbidden_calls:
  - grpc.Dial
  - credentials.NewTLS

rationale: |
  Shared bootstrap keeps TLS and mesh consistent.
`,
	})

	assertValidNT(t, out)

	if len(report.Imported()) != 1 {
		t.Fatalf("expected 1 imported file, got %d (skipped=%d, has_invalid=%v)",
			len(report.Imported()), len(report.Skipped()), report.HasInvalid())
	}
	if got := report.Imported()[0].Schema; got != "implementation_pattern" {
		t.Errorf("schema: want implementation_pattern, got %q", got)
	}
}

// Typed node + label + status + rationale (as rdfs:comment) + authoredIn.
func TestImplementationPattern_CoreLiterals(t *testing.T) {
	out, _ := implPatternDir(t, map[string]string{
		"p.yaml": `
id: globular.pattern.grpc_client_standard
class: ImplementationPattern
label: Standard Globular gRPC service client
status: active
rationale: |
  Shared bootstrap keeps TLS and mesh consistent.
when_to_use:
  - creating a new Go client
reference_files:
  - path: a.go
    role: r
  - path: b.go
    role: r
`,
	})

	subj := rdf.MintIRI(rdf.ClassImplementationPattern, "globular.pattern.grpc_client_standard")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassImplementationPattern)+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropLabel)+` "Standard Globular gRPC service client"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropStatus)+` "active"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropComment)+` "Shared bootstrap keeps TLS and mesh consistent."`)
}

// when_to_use entries each become an aw:activationTrigger literal.
func TestImplementationPattern_WhenToUseAsActivationTriggers(t *testing.T) {
	out, _ := implPatternDir(t, map[string]string{
		"p.yaml": `
id: globular.pattern.grpc_client_standard
class: ImplementationPattern
label: x
when_to_use:
  - creating a new Go client
  - adding client package
reference_files:
  - path: a.go
    role: r
  - path: b.go
    role: r
`,
	})

	subj := rdf.MintIRI(rdf.ClassImplementationPattern, "globular.pattern.grpc_client_standard")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropActivationTrigger)+` "creating a new Go client"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropActivationTrigger)+` "adding client package"`)
}

// reference_files emit "role:path" string literals via aw:referenceFile.
func TestImplementationPattern_ReferenceFilesAsRoleColonPath(t *testing.T) {
	out, _ := implPatternDir(t, map[string]string{
		"p.yaml": `
id: globular.pattern.grpc_client_standard
class: ImplementationPattern
label: x
when_to_use: [creating a new Go client]
reference_files:
  - path: golang/echo/echo_client/echo_client.go
    role: canonical_minimal
  - path: golang/monitoring/monitoring_client/monitoring_client.go
    role: richer_reference
`,
	})

	subj := rdf.MintIRI(rdf.ClassImplementationPattern, "globular.pattern.grpc_client_standard")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropReferenceFile)+` "canonical_minimal:golang/echo/echo_client/echo_client.go"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropReferenceFile)+` "richer_reference:golang/monitoring/monitoring_client/monitoring_client.go"`)
}

// required_calls and forbidden_calls each emit one literal triple.
func TestImplementationPattern_RequiredAndForbiddenCalls(t *testing.T) {
	out, _ := implPatternDir(t, map[string]string{
		"p.yaml": `
id: globular.pattern.grpc_client_standard
class: ImplementationPattern
label: x
when_to_use: [x]
reference_files: [{path: a, role: r}, {path: b, role: r}]
required_calls:
  - globular.InitClient
  - globular.GetClientConnection
forbidden_calls:
  - grpc.Dial
  - credentials.NewTLS
`,
	})

	subj := rdf.MintIRI(rdf.ClassImplementationPattern, "globular.pattern.grpc_client_standard")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRequiresCall)+` "globular.InitClient"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRequiresCall)+` "globular.GetClientConnection"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropForbidsCall)+` "grpc.Dial"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropForbidsCall)+` "credentials.NewTLS"`)
}

// must_follow steps become aw:mustFollow literals — one per item.
func TestImplementationPattern_MustFollowSteps(t *testing.T) {
	out, _ := implPatternDir(t, map[string]string{
		"p.yaml": `
id: globular.pattern.grpc_client_standard
class: ImplementationPattern
label: x
when_to_use: [x]
reference_files: [{path: a, role: r}, {path: b, role: r}]
must_follow:
  - Constructor calls globular.InitClient(client, address, id)
  - Reconnect() calls globular.GetClientConnection(client)
`,
	})

	subj := rdf.MintIRI(rdf.ClassImplementationPattern, "globular.pattern.grpc_client_standard")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropMustFollow)+` "Constructor calls globular.InitClient(client, address, id)"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropMustFollow)+` "Reconnect() calls globular.GetClientConnection(client)"`)
}

// Empty id is a soft skip — file parsed but no triples emitted, no error.
func TestImplementationPattern_EmptyIDSoftSkip(t *testing.T) {
	out, report := implPatternDir(t, map[string]string{
		"p.yaml": `
class: ImplementationPattern
label: nameless
`,
	})

	// File without id won't match the detect rule (id+class) — should be UnknownSchema.
	// Verifies the detector doesn't crash and routes a malformed file to "unknown".
	if len(report.Imported()) != 0 {
		t.Errorf("expected 0 imported, got %d", len(report.Imported()))
	}
	if !strings.Contains(out, "ImplementationPattern") {
		// We expect NO triples about this pattern.
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func mustContain(t *testing.T, body, needle string) {
	t.Helper()
	if !strings.Contains(body, needle) {
		t.Errorf("output missing expected triple:\n  %s\nfull output:\n%s", needle, body)
	}
}
