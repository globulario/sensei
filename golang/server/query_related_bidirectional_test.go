// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"testing"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
)

// A Test node is almost always the OBJECT of an edge (invariant requiresTest
// test), never the subject of much. Before related-queries traversed inbound
// edges, asking for a test's neighbours returned nothing — the node looked
// orphaned. It must now surface the invariant that requires it, labelled from
// the test's perspective ("verifies").
func TestQueryRelated_TestSurfacesItsInvariant(t *testing.T) {
	s := newServer(newEmbeddedSeedStore())
	resp, err := s.Query(context.Background(), &awarenesspb.QueryRequest{
		Mode: awarenesspb.QueryMode_QUERY_MODE_RELATED,
		Id:   "test:TestRuntimeStatus_NotDerivedFromDesiredState",
	})
	if err != nil {
		t.Fatalf("Query related: %v", err)
	}
	var found bool
	for _, r := range resp.GetRows() {
		if r.GetId() == "invariant:state.runtime_not_desired" {
			found = true
			if r.GetRelation() != "verifies" {
				t.Errorf("inbound relation = %q, want %q", r.GetRelation(), "verifies")
			}
		}
	}
	if !found {
		t.Fatalf("test node not linked to invariant state.runtime_not_desired (rows=%d) — "+
			"inbound traversal regressed", len(resp.GetRows()))
	}
}

// A SourceFile is linked almost entirely by inbound edges (code symbols are
// definedInFile it). Two bugs hid this: related-queries were outgoing-only, and
// the file's already-encoded id was double-encoded on resolve so it never
// matched the stored node. With both fixed, a file surfaces the symbols it
// defines, via the inverse "defines" relation.
func TestQueryRelated_SourceFileSurfacesInboundSymbols(t *testing.T) {
	requireCombinedSeed(t)
	s := newServer(newEmbeddedSeedStore())
	// This file is the object of CodeSymbol→definedInFile edges in the seed.
	const fileID = "source_file:cmd%2Floadnt%2Fmain.go"
	resp, err := s.Query(context.Background(), &awarenesspb.QueryRequest{
		Mode: awarenesspb.QueryMode_QUERY_MODE_RELATED,
		Id:   fileID,
	})
	if err != nil {
		t.Fatalf("Query related: %v", err)
	}
	var defines int
	for _, r := range resp.GetRows() {
		if r.GetRelation() == "defines" && r.GetClass() == "code_symbol" {
			defines++
		}
	}
	if defines == 0 {
		t.Fatalf("file %s surfaced no defined symbols (rows=%d) — inbound traversal or "+
			"id round-trip regressed", fileID, len(resp.GetRows()))
	}
}

// The id round-trip must be idempotent: a row carries an encoded path segment,
// and resolving it back must hit the same node, not a double-encoded miss.
func TestResolveIRI_EncodedSourceFileIDRoundTrips(t *testing.T) {
	const encoded = "cmd%2Floadnt%2Fmain.go"
	iri, _, err := resolveIRIForClassAndID("source_file", encoded)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := "https://globular.io/awareness#sourceFile/cmd%2Floadnt%2Fmain.go"
	if iri != want {
		t.Fatalf("round-trip IRI = %q, want %q (double-encoding regressed)", iri, want)
	}
}

func TestResolveIRI_ProofSlotIDRoundTrips(t *testing.T) {
	const id = "slot.contract.contract.repo_fork_and_view_nontty_scriptability.repo_fork_non_tty"
	iri, class, err := resolveIRIForClassAndID("proof_slot", id)
	if err != nil {
		t.Fatalf("resolve proof_slot: %v", err)
	}
	want := "https://globular.io/awareness#proofSlot/" + id
	if iri != want {
		t.Fatalf("proof_slot IRI = %q, want %q", iri, want)
	}
	if class != "proof_slot" {
		t.Fatalf("class = %q, want proof_slot", class)
	}
}
