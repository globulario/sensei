// SPDX-License-Identifier: AGPL-3.0-only

package closure

import (
	"fmt"
	"testing"
)

func protectionGraph(n int) GraphIndex {
	idx := GraphIndex{Nodes: map[string]Node{}}
	for i := 0; i < n; i++ {
		file := fmt.Sprintf("file.%03d", i)
		idx.Nodes[file] = Node{ID: file, Classes: []string{"source_file"}}
		// Two governing nodes per file, declared in reverse order so the
		// index cannot pass by accidentally preserving map order.
		for _, prefix := range []string{"invariant.z", "invariant.a"} {
			id := fmt.Sprintf("%s.%03d", prefix, i)
			idx.Nodes[id] = Node{ID: id, Classes: []string{"invariant"}, Protects: []string{file}}
		}
	}
	// A node protecting several files at once, and one protecting nothing.
	idx.Nodes["invariant.wide"] = Node{ID: "invariant.wide", Classes: []string{"invariant"},
		Protects: []string{"file.000", "file.001", "", "file.002"}}
	idx.Nodes["invariant.idle"] = Node{ID: "invariant.idle", Classes: []string{"invariant"}}
	return idx
}

// The index must resolve exactly what the single-node scan resolves, for every
// node. An index that returned a different protector set would be a silent
// change in what a scope contains rather than a visible failure — and scope
// decides which governance a task is assessed against.
func TestReverseProtectionsAgreesWithTheDirectScan(t *testing.T) {
	graph := protectionGraph(25)
	index := reverseProtections(graph)

	for id := range graph.Nodes {
		want := protectorsOf(graph, id)
		got := index[id]
		if len(got) != len(want) {
			t.Fatalf("%s: indexed %d protector(s), scan found %d", id, len(got), len(want))
		}
		for i := range want {
			if got[i].ID != want[i].ID {
				t.Fatalf("%s: position %d is %s, scan has %s", id, i, got[i].ID, want[i].ID)
			}
		}
	}
	// An empty protects entry names nothing and must not create a bucket.
	if _, ok := index[""]; ok {
		t.Fatal("an empty protects target created an index entry")
	}
}

// The expansion this feeds must still find the governance that points at a
// requested file — the whole point of the change it accelerates.
func TestExpansionStillReachesProtectorsThroughTheIndex(t *testing.T) {
	graph := protectionGraph(3)
	seeds := map[string]Node{"file.001": graph.Nodes["file.001"]}

	got := expandRelevantNodes(graph, seeds)
	for _, want := range []string{"invariant.a.001", "invariant.z.001", "invariant.wide"} {
		if _, ok := got[want]; !ok {
			t.Errorf("expansion did not reach %s, which protects the requested file", want)
		}
	}
	if _, ok := got["invariant.a.002"]; ok {
		t.Error("expansion reached a protector of a different file")
	}
	if _, ok := got["invariant.idle"]; ok {
		t.Error("expansion reached a node that protects nothing")
	}
}

// A request naming a file the graph does not represent seeds nothing, and an
// expansion that will visit nothing must not pay a full pass over the graph to
// index it — the cost this change removes, in miniature.
func TestAnEmptySeedSetExpandsToNothing(t *testing.T) {
	got := expandRelevantNodes(protectionGraph(50), map[string]Node{})
	if len(got) != 0 {
		t.Fatalf("expansion from no seeds produced %d node(s)", len(got))
	}
	if got == nil {
		t.Fatal("expansion returned a nil map where callers expect an empty one")
	}
}
