// SPDX-License-Identifier: AGPL-3.0-only

package main

import "testing"

// Both repositories may emit literals under one shared subject and predicate.
// Removing a services-authored value from a combined artifact must remain
// external when the committed seed already carries Sensei's current exact value.
func TestClassifySeedDiff_RemovedExternalLiteralOnSharedEdgeStaysExternal(t *testing.T) {
	subject := `<https://globular.io/awareness#sourceFile/golang%2Fworkflow%2Fengine%2Fengine.go>`
	predicate := `<http://www.w3.org/2000/01/rdf-schema#label>`
	agLabel := subject + " " + predicate + ` "engine.go" .`
	svcLabel := subject + " " + predicate + ` "workflow engine source" .`

	agOnly := nt(agLabel)
	committed := nt(agLabel, svcLabel)
	generated := nt(agLabel)

	owned, external := classifySeedDiff(committed, generated, agOnly)
	if len(owned) != 0 {
		t.Fatalf("services literal on a shared edge must stay external, got owned=%v", owned)
	}
	if len(external) != 1 || external[0] != svcLabel {
		t.Fatalf("services literal must be external, got %v", external)
	}
}

func TestNtObjectTermPreservesCompleteIdentity(t *testing.T) {
	cases := map[string]string{
		`<s> <p> "literal with spaces"@en .`:                         `"literal with spaces"@en`,
		`<s> <p> "42"^^<http://www.w3.org/2001/XMLSchema#integer> .`: `"42"^^<http://www.w3.org/2001/XMLSchema#integer>`,
		`<s> <p> <https://example.test/object> .`:                    `<https://example.test/object>`,
	}
	for line, want := range cases {
		if got := ntObjectTerm(line); got != want {
			t.Fatalf("ntObjectTerm(%q)=%q, want %q", line, got, want)
		}
	}
}

func TestNtOwnershipKeyPreservesStableExternalObjectIdentity(t *testing.T) {
	left := `<https://globular.io/awareness#sourceFile/x> <https://globular.io/awareness#relatedTo> <https://example.test/left> .`
	right := `<https://globular.io/awareness#sourceFile/x> <https://globular.io/awareness#relatedTo> <https://example.test/right> .`
	if ntOwnershipKey(left) == ntOwnershipKey(right) {
		t.Fatal("different stable external IRIs must not share an ownership key")
	}

	leftLiteral := `<https://globular.io/awareness#sourceFile/x> <http://www.w3.org/2000/01/rdf-schema#comment> "left value" .`
	rightLiteral := `<https://globular.io/awareness#sourceFile/x> <http://www.w3.org/2000/01/rdf-schema#comment> "right value" .`
	if ntOwnershipKey(leftLiteral) == ntOwnershipKey(rightLiteral) {
		t.Fatal("different literals must not share an ownership key")
	}
}

func TestNtOwnershipKeyKeepsBlankNodesSerializationLocal(t *testing.T) {
	left := `<https://globular.io/awareness#sourceFile/x> <https://globular.io/awareness#relatedTo> _:left .`
	right := `<https://globular.io/awareness#sourceFile/x> <https://globular.io/awareness#relatedTo> _:right .`
	if ntOwnershipKey(left) != ntOwnershipKey(right) {
		t.Fatal("blank-node labels are serialization-local and must collapse to one ownership kind")
	}
}
