// SPDX-License-Identifier: AGPL-3.0-only

package closure

import "testing"

// A request naming a source file must find the governance that points at it.
//
// Protection is authored as an outbound edge on the governing node: an
// invariant protects a file, a required test protects a file. Scope expansion
// followed edges forward only, so a scope seeded with the file resolved to
// exactly that file and nothing else, and every rule reading the scope for
// governance saw an ungoverned region.
//
// The visible consequence was closure.agent.required_test_unidentified asking
// for a required test and being unsatisfiable by adding one: three successive
// task generations in globulario/sensei-code declared tests in the tree, then an
// invariant protecting the file and requiring them, then the required tests as
// governed nodes anchored to the file, and the blocker stood through all three.
func TestScopeExpansionFindsTheNodesThatProtectTheSeed(t *testing.T) {
	file := Node{ID: "file:doctor.go", Classes: []string{"source_file"}}
	invariant := Node{
		ID:            "invariant:authentication_is_not_readiness",
		Classes:       []string{"invariant"},
		Protects:      []string{"file:doctor.go"},
		RequiresTests: []string{"test:TestAuthenticationAloneIsNotReadiness"},
	}
	required := Node{
		ID:       "test:TestAuthenticationAloneIsNotReadiness",
		Classes:  []string{"test"},
		Protects: []string{"file:doctor.go"},
	}
	unrelated := Node{ID: "invariant:something_else", Classes: []string{"invariant"}, Protects: []string{"file:other.go"}}

	graph := GraphIndex{Nodes: map[string]Node{
		file.ID: file, invariant.ID: invariant, required.ID: required, unrelated.ID: unrelated,
	}}

	got := expandRelevantNodes(graph, map[string]Node{file.ID: file})

	for _, want := range []string{file.ID, invariant.ID, required.ID} {
		if _, ok := got[want]; !ok {
			t.Errorf("scope is missing %s", want)
		}
	}
	if _, ok := got[unrelated.ID]; ok {
		t.Error("scope pulled in governance for another file")
	}
}

// The seed's own outbound edges still expand, and a file protected by nothing
// still resolves to itself rather than to an error.
func TestScopeExpansionLeavesAnUngovernedSeedAlone(t *testing.T) {
	file := Node{ID: "file:lonely.go", Classes: []string{"source_file"}}
	graph := GraphIndex{Nodes: map[string]Node{file.ID: file}}
	got := expandRelevantNodes(graph, map[string]Node{file.ID: file})
	if len(got) != 1 {
		t.Fatalf("an ungoverned file expanded to %d node(s)", len(got))
	}
}
