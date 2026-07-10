// SPDX-License-Identifier: Apache-2.0

package extractor_test

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

// mustContain fails the test if needle is absent from haystack.
func wantTriple(t *testing.T, haystack, needle, what string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("missing %s:\n  want triple containing: %s", what, needle)
	}
}

// TestSpine_AllNodeTypesAndEdges imports one file per spine node type plus a
// meta.* invariant, and asserts the typed nodes, the dual-typing of
// meta-principles, and a representative edge of each kind.
func TestSpine_AllNodeTypesAndEdges(t *testing.T) {
	root := makeDir(t, map[string]string{
		// A meta.* invariant — must be dual-typed aw:Invariant + aw:MetaPrinciple.
		"invariants.yaml": `
invariants:
  - id: meta.test.storage_is_not_authority
    title: Storage is not semantic authority
    severity: critical
  - id: test.plain_invariant
    title: A plain invariant
`,
		"components.yaml": `
components:
  - id: component.test.svc
    name: TestService
    kind: service
    owns_invariants:
      - test.plain_invariant
    exposes_contracts:
      - contract.test.api
    depends_on:
      - component.test.store
    satisfies_meta_principles:
      - meta.test.storage_is_not_authority
    source_files:
      - golang/server/resolve.go
`,
		"boundaries.yaml": `
boundaries:
  - id: boundary.test.read_only
    name: Read-only boundary
    kind: read_only
    separates:
      - component.test.svc
    protects:
      - test.plain_invariant
    vulnerable_to:
      - test.fm.authority_confusion
    forbids:
      - test.ff.ui_direct_write
`,
		"contracts.yaml": `
contracts:
  - id: contract.test.api
    name: TestService.Api
    kind: grpc
    stability: stable
    read_or_write: read
    exposed_by:
      - component.test.svc
    constrained_by_invariants:
      - test.plain_invariant
`,
		"decisions.yaml": `
decisions:
  - id: decision.test.read_only_v1
    title: Read-only v1
    status: accepted
    rationale: Keep it read-only.
    defines_boundaries:
      - boundary.test.read_only
    defines_contracts:
      - contract.test.api
    affects_components:
      - component.test.svc
    mitigates:
      - test.fm.authority_confusion
    rejects:
      - test.ff.ui_direct_write
    superseded_by:
      - decision.test.read_only_v2
`,
		"evidence.yaml": `
evidence:
  - id: evidence.test.ci_green
    name: CI green
    kind: ci
    status: pass
    command: go test ./...
    supports:
      - invariant:test.plain_invariant
      - decision:decision.test.read_only_v1
    validates_components:
      - component.test.svc
`,
		"meta_principle_links.yaml": `
meta_principle_links:
  - id: meta.test.storage_is_not_authority
    generates_invariants:
      - test.plain_invariant
    constrains_decisions:
      - decision.test.read_only_v1
    applies_to_components:
      - component.test.svc
`,
	})

	out, _ := importDirToString(t, root)
	assertValidNT(t, out)

	metaIRI := strings.Trim(rdf.MintIRI(rdf.ClassInvariant, "meta.test.storage_is_not_authority"), "<>")
	compIRI := strings.Trim(rdf.MintIRI(rdf.ClassComponent, "component.test.svc"), "<>")
	evIRI := strings.Trim(rdf.MintIRI(rdf.ClassEvidence, "evidence.test.ci_green"), "<>")
	decIRI := strings.Trim(rdf.MintIRI(rdf.ClassDecision, "decision.test.read_only_v1"), "<>")
	bndIRI := strings.Trim(rdf.MintIRI(rdf.ClassBoundary, "boundary.test.read_only"), "<>")

	// Dual-typing: the meta.* node carries BOTH rdf:type aw:Invariant and aw:MetaPrinciple.
	wantTriple(t, out, "<"+metaIRI+"> <"+rdf.PropType+"> <"+rdf.ClassInvariant+">", "meta node typed Invariant")
	wantTriple(t, out, "<"+metaIRI+"> <"+rdf.PropType+"> <"+rdf.ClassMetaPrinciple+">", "meta node dual-typed MetaPrinciple")
	// A plain (non-meta) invariant must NOT be typed MetaPrinciple.
	plainIRI := strings.Trim(rdf.MintIRI(rdf.ClassInvariant, "test.plain_invariant"), "<>")
	if strings.Contains(out, "<"+plainIRI+"> <"+rdf.PropType+"> <"+rdf.ClassMetaPrinciple+">") {
		t.Errorf("plain invariant must not be dual-typed MetaPrinciple")
	}

	// Each spine node type is typed.
	wantTriple(t, out, "<"+compIRI+"> <"+rdf.PropType+"> <"+rdf.ClassComponent+">", "Component typed")
	wantTriple(t, out, "<"+bndIRI+"> <"+rdf.PropType+"> <"+rdf.ClassBoundary+">", "Boundary typed")
	wantTriple(t, out, "<"+rdf.ClassContract+">", "Contract typed")
	wantTriple(t, out, "<"+decIRI+"> <"+rdf.PropType+"> <"+rdf.ClassDecision+">", "Decision typed")
	wantTriple(t, out, "<"+evIRI+"> <"+rdf.PropType+"> <"+rdf.ClassEvidence+">", "Evidence typed")

	// Representative edges.
	wantTriple(t, out, "<"+compIRI+"> <"+rdf.PropOwnsInvariant+"> <"+plainIRI+">", "Component ownsInvariant")
	wantTriple(t, out, "<"+compIRI+"> <"+rdf.PropSatisfiesMetaPrinciple+"> <"+metaIRI+">", "Component satisfiesMetaPrinciple -> meta invariant IRI")
	wantTriple(t, out, "<"+bndIRI+"> <"+rdf.PropForbids+">", "Boundary forbids")
	wantTriple(t, out, "<"+decIRI+"> <"+rdf.PropDefinesBoundary+"> <"+bndIRI+">", "Decision definesBoundary")
	wantTriple(t, out, "<"+decIRI+"> <"+rdf.PropRejects+">", "Decision rejects")
	wantTriple(t, out, "<"+evIRI+"> <"+rdf.PropSupports+"> <"+decIRI+">", "Evidence supports Decision")
	wantTriple(t, out, "<"+evIRI+"> <"+rdf.PropValidatesComponent+"> <"+compIRI+">", "Evidence validatesComponent")
	// MetaPrinciple outgoing edge attached to the invariant IRI.
	wantTriple(t, out, "<"+metaIRI+"> <"+rdf.PropGenerates+"> <"+plainIRI+">", "MetaPrinciple generates Invariant")
	wantTriple(t, out, "<"+metaIRI+"> <"+rdf.PropAppliesTo+"> <"+compIRI+">", "MetaPrinciple appliesTo Component")
	// Assertion provenance marker defaults to declared.
	wantTriple(t, out, "<"+compIRI+"> <"+rdf.PropAssertionMethod+"> \"declared\"", "Component assertionMethod declared")
}
