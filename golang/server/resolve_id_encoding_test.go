// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store"
)

// A Test node's id IS a file path ("golang/server/foo_test.go:TestBar"), so its
// IRI path segment is EncodeIRIPath-encoded on the wire. Agents copy the id
// straight out of Impact/Briefing/Preflight to decide what to run, so leaking
// the wire form ("golang%2Fserver%2Ffoo_test.go:TestBar") hands them a command
// that does not exist. These tests pin the decoded form at the one boundary
// that produces every surfaced id.
func TestAwarenessIDFromIRI_DecodesEncodedPathSegment(t *testing.T) {
	const want = "golang/server/foo_test.go:TestBar"
	iri := mintedIRI(rdf.ClassTest, want)
	if !strings.Contains(iri, "%2F") {
		t.Fatalf("precondition: minted IRI %q should carry an encoded path", iri)
	}
	got, ok := awarenessIDFromIRI(iri)
	if !ok {
		t.Fatalf("awarenessIDFromIRI(%q) not ok", iri)
	}
	if got != want {
		t.Fatalf("id = %q, want %q", got, want)
	}
}

// The "class:id" pairs in the Referenced IDs block are the agent's copy source
// for a follow-up Resolve call, so they carry the same contract.
func TestAwarenessRelatedID_DecodesEncodedPathSegment(t *testing.T) {
	const id = "golang/server/foo_test.go:TestBar"
	got, ok := awarenessRelatedID(mintedIRI(rdf.ClassTest, id))
	if !ok {
		t.Fatal("awarenessRelatedID not ok")
	}
	if want := "test:" + id; got != want {
		t.Fatalf("related id = %q, want %q", got, want)
	}
}

// Decoding on the way out only stays safe because resolveIRIForClassAndID
// re-encodes on the way back in. If either side drifts, an agent that copies a
// surfaced id into Resolve gets a double-encoded IRI that matches no node —
// the exact bug the decode at resolveIRIForClassAndID:155 was added to fix.
func TestSurfacedTestIDRoundTripsBackToTheSameIRI(t *testing.T) {
	const id = "golang/server/foo_test.go:TestBar"
	original := mintedIRI(rdf.ClassTest, id)

	surfaced, ok := awarenessIDFromIRI(original)
	if !ok {
		t.Fatal("awarenessIDFromIRI not ok")
	}
	back, _, err := resolveIRIForClassAndID("test", surfaced)
	if err != nil {
		t.Fatalf("resolveIRIForClassAndID: %v", err)
	}
	if back != original {
		t.Fatalf("round-trip IRI = %q, want %q", back, original)
	}
}

// End-to-end through the RPC an agent actually calls: a required test reached
// by Impact must be reported in runnable form.
func TestImpact_RequiredTestIDIsDecodedForAgents(t *testing.T) {
	const id = "golang/server/foo_test.go:TestBar"
	s := newServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{
				{NodeIRI: mintedIRI(rdf.ClassTest, id), TypeIRI: rdf.ClassTest},
			}, nil
		},
	})
	resp, err := s.Impact(context.Background(), &awarenesspb.ImpactRequest{File: "golang/server/foo.go"})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	tests := resp.GetRequiredTests()
	if len(tests) != 1 {
		t.Fatalf("required_tests=%d, want 1", len(tests))
	}
	if got := tests[0].GetId(); got != id {
		t.Fatalf("required test id = %q, want %q", got, id)
	}
}
