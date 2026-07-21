// SPDX-License-Identifier: AGPL-3.0-only

package extractor_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/rdf"
)

func TestMissingPlaneAnnotationPreservesExistingTriples(t *testing.T) {
	root := makeDir(t, map[string]string{
		"invariants.yaml": `
invariants:
  - id: test.no_plane
    title: No plane annotation
    status: active
`,
	})
	out, _ := importDirToString(t, root)
	if strings.Contains(out, rdf.IRI(rdf.PropArchitecturalPlane)) {
		t.Fatalf("unexpected architecturalPlane triple:\n%s", out)
	}
}

func TestInvariantAuthoredPlaneImporterAcceptsIntended(t *testing.T) {
	out, _ := importDirToString(t, makeDir(t, map[string]string{"invariants.yaml": `
invariants:
  - id: test.intended
    title: Intended invariant
    status: active
    architectural_plane: intended
`}))
	requirePlaneTriple(t, out, "intended")
}

func TestContractAuthoredPlaneImporterAcceptsDesired(t *testing.T) {
	out, _ := importDirToString(t, makeDir(t, map[string]string{"contracts.yaml": `
contracts:
  - id: contract.test.desired
    name: Desired contract
    kind: api
    status: active
    architectural_plane: desired
`}))
	requirePlaneTriple(t, out, "desired")
}

func TestDecisionAuthoredPlaneImporterAcceptsDesired(t *testing.T) {
	out, _ := importDirToString(t, makeDir(t, map[string]string{"decisions.yaml": `
decisions:
  - id: decision.test.desired
    title: Desired decision
    status: active
    architectural_plane: desired
`}))
	requirePlaneTriple(t, out, "desired")
}

func TestIntentAuthoredPlaneImporterAcceptsDesired(t *testing.T) {
	out, _ := intentDir(t, map[string]string{"intent.yaml": `
id: intent.test.desired
level: vision
title: Desired intent
intent: Build toward the explicit target.
status: active
architectural_plane: desired
`})
	requirePlaneTriple(t, out, "desired")
}

func TestGovernedPlaneImporterRejectsObserved(t *testing.T) {
	requireImportError(t, map[string]string{"intent.yaml": `
id: intent.test.bad
level: vision
title: Bad intent
intent: Bad.
status: active
architectural_plane: observed
`})
}

func TestGovernedPlaneImporterRejectsEnforced(t *testing.T) {
	requireImportError(t, map[string]string{"contracts.yaml": `
contracts:
  - id: contract.test.bad
    name: Bad contract
    status: active
    architectural_plane: enforced
`})
}

func TestHistoricalPlaneImporterRequiresHistoricalStatusOrSupersession(t *testing.T) {
	requireImportError(t, map[string]string{"decisions.yaml": `
decisions:
  - id: decision.test.bad_history
    title: Bad history
    status: active
    architectural_plane: historical
`})
	out, _ := importDirToString(t, makeDir(t, map[string]string{"decisions.yaml": `
decisions:
  - id: decision.test.good_history
    title: Good history
    status: active
    architectural_plane: historical
    superseded_by:
      - decision.test.next
`}))
	requirePlaneTriple(t, out, "historical")
}

func TestIntendedPlaneImporterRejectsRetiredNode(t *testing.T) {
	requireImportError(t, map[string]string{"contracts.yaml": `
contracts:
  - id: contract.test.retired
    name: Retired contract
    status: retired
    architectural_plane: intended
`})
}

func TestDesiredPlaneImporterRejectsSupersededNode(t *testing.T) {
	requireImportError(t, map[string]string{"decisions.yaml": `
decisions:
  - id: decision.test.superseded_desired
    title: Superseded desired
    status: active
    architectural_plane: desired
    superseded_by:
      - decision.test.next
`})
}

func requirePlaneTriple(t *testing.T, nt, value string) {
	t.Helper()
	if !strings.Contains(nt, rdf.IRI(rdf.PropArchitecturalPlane)+" "+rdf.Lit(value)) {
		t.Fatalf("missing architecturalPlane %s in:\n%s", value, nt)
	}
}

func requireImportError(t *testing.T, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	_, report, err := extractor.ImportAwarenessDir(makeDir(t, files), &buf)
	if err != nil {
		t.Fatalf("ImportAwarenessDir returned unexpected top-level error: %v", err)
	}
	if !report.HasInvalid() {
		t.Fatalf("expected import error, got triples:\n%s", buf.String())
	}
}
