// SPDX-License-Identifier: Apache-2.0

package proofrequirements

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/closure"
)

func idx(nodes ...closure.Node) (closure.GraphIndex, closure.Report) {
	g := closure.GraphIndex{Nodes: map[string]closure.Node{}, NodesByID: map[string]string{}}
	var rep closure.Report
	for _, n := range nodes {
		g.Nodes[n.IRI] = n
		g.NodesByID[n.ID] = n.IRI
		rep.RelevantNodes = append(rep.RelevantNodes, closure.NodeReceipt{ID: n.ID, IRI: n.IRI, Classes: n.Classes})
	}
	return g, rep
}

func TestProjectScopedGraphClassifies(t *testing.T) {
	g, rep := idx(
		closure.Node{ID: "ob.1", IRI: "iri:ob.1", Classes: []string{ClassProofObligation}, Kind: "static_test", DependsOn: []string{"slot.a"}},
		closure.Node{ID: "slot.a", IRI: "iri:slot.a", Classes: []string{ClassProofSlot}, Kind: "static_test"},
		closure.Node{ID: "component.x", IRI: "iri:component.x", Classes: []string{"Component"}, RequiresTests: []string{"test.z"}},
		closure.Node{ID: "test.y", IRI: "iri:test.y", Classes: []string{ClassTest}},
		closure.Node{ID: "ev.1", IRI: "iri:ev.1", Classes: []string{ClassRuntimeEvidence}, Kind: "runtime"},
		closure.Node{ID: "ff.1", IRI: "iri:ff.1", Classes: []string{ClassForbiddenFix}, Forbids: []string{"cache_the_reload"}},
		closure.Node{ID: "plain.1", IRI: "iri:plain.1", Classes: []string{"Decision"}},
	)
	proj, err := ProjectScopedGraph(rep, g)
	if err != nil {
		t.Fatal(err)
	}
	// Both ProofObligation and ProofSlot nodes are proof obligations (mirroring
	// admission's projectProof); the ProofSlot is additionally a required slot.
	if len(proj.Obligations) != 2 || proj.Obligations[0].ID != "ob.1" || proj.Obligations[1].ID != "slot.a" {
		t.Fatalf("obligations = %+v", proj.Obligations)
	}
	if len(proj.RequiredSlots) != 1 || proj.RequiredSlots[0].ID != "slot.a" {
		t.Fatalf("slots = %+v", proj.RequiredSlots)
	}
	// Both the requiresTest relation (test.z) and the Test-class node (test.y).
	ids := map[string]bool{}
	for _, r := range proj.RequiredTests {
		ids[r.ID] = true
	}
	if !ids["test.z"] || !ids["test.y"] || len(proj.RequiredTests) != 2 {
		t.Fatalf("required tests = %+v", proj.RequiredTests)
	}
	if len(proj.RuntimeEvidenceProfiles) != 1 || proj.RuntimeEvidenceProfiles[0].ID != "ev.1" {
		t.Fatalf("runtime evidence = %+v", proj.RuntimeEvidenceProfiles)
	}
	if len(proj.ForbiddenMoves) != 1 || proj.ForbiddenMoves[0].ID != "ff.1" {
		t.Fatalf("forbidden = %+v", proj.ForbiddenMoves)
	}
}

// A closure blocker is never emitted as a forbidden move; the projector only sees
// governed ForbiddenFix / forbid relations.
func TestProjectScopedGraphForbiddenSeparateFromBlockers(t *testing.T) {
	g, rep := idx(closure.Node{ID: "n.1", IRI: "iri:n.1", Classes: []string{"Component"}})
	rep.Blockers = []closure.Blocker{{ID: "blocker.1", Code: "closure.some.blocker"}}
	proj, err := ProjectScopedGraph(rep, g)
	if err != nil {
		t.Fatal(err)
	}
	if len(proj.ForbiddenMoves) != 0 {
		t.Fatalf("closure blocker must not become a forbidden move: %+v", proj.ForbiddenMoves)
	}
}

func TestProjectScopedGraphDeterministic(t *testing.T) {
	nodes := []closure.Node{
		{ID: "ob.2", IRI: "iri:ob.2", Classes: []string{ClassProofObligation}},
		{ID: "ob.1", IRI: "iri:ob.1", Classes: []string{ClassProofObligation}},
		{ID: "ff.1", IRI: "iri:ff.1", Classes: []string{ClassForbiddenFix}},
	}
	g1, r1 := idx(nodes...)
	g2, r2 := idx(nodes[2], nodes[0], nodes[1]) // reordered
	p1, _ := ProjectScopedGraph(r1, g1)
	p2, _ := ProjectScopedGraph(r2, g2)
	if len(p1.Obligations) != 2 || p1.Obligations[0].ID != "ob.1" || p1.Obligations[1].ID != "ob.2" {
		t.Fatalf("obligations not sorted: %+v", p1.Obligations)
	}
	if len(p2.Obligations) != len(p1.Obligations) || p2.Obligations[0].ID != p1.Obligations[0].ID {
		t.Fatal("projection is order-dependent")
	}
}

// An unresolvable relevant-node receipt is skipped, not fabricated.
func TestResolveScopedNodesSkipsUnresolvable(t *testing.T) {
	g, rep := idx(closure.Node{ID: "n.1", IRI: "iri:n.1", Classes: []string{"Component"}})
	rep.RelevantNodes = append(rep.RelevantNodes, closure.NodeReceipt{ID: "ghost", IRI: "iri:ghost"})
	if got := ResolveScopedNodes(rep, g); len(got) != 1 || got[0].ID != "n.1" {
		t.Fatalf("expected only the resolvable node, got %+v", got)
	}
}
