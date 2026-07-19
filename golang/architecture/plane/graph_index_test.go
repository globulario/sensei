// SPDX-License-Identifier: Apache-2.0

package plane

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/rdf"
)

func TestGraphIndexUsesExplicitRDFType(t *testing.T) {
	idx, err := ReadGraphIndex(strings.NewReader(graphNT(t, "invariant", "i.one", "active", "")))
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Nodes) != 1 {
		t.Fatalf("nodes=%d", len(idx.Nodes))
	}
}

func TestGraphIndexDoesNotInferClassFromIRIPrefix(t *testing.T) {
	nt := rdf.MintIRI(rdf.ClassInvariant, "i.untyped") + " " + rdf.IRI(rdf.PropStatus) + " " + rdf.Lit("active") + " .\n"
	idx, err := ReadGraphIndex(strings.NewReader(nt))
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Nodes) != 0 {
		t.Fatalf("nodes=%d", len(idx.Nodes))
	}
}

func TestGraphIndexReadsStatusAssertionPlaneAndProvenance(t *testing.T) {
	nt := graphNT(t, "decision", "d.one", "active", architecture.PlaneDesired) +
		ntLit("decision", "d.one", rdf.PropAssertionMethod, "declared") +
		ntLit("decision", "d.one", rdf.PropAuthoredIn, "docs/awareness/decisions.yaml")
	idx, err := ReadGraphIndex(strings.NewReader(nt))
	if err != nil {
		t.Fatal(err)
	}
	n := onlyNode(t, idx)
	if n.Status != "active" || n.AssertionMethod != "declared" || n.ArchitecturalPlane != architecture.PlaneDesired || len(n.AuthoredIn) != 1 {
		t.Fatalf("node=%+v", n)
	}
}

func TestGraphIndexRejectsMalformedNTriples(t *testing.T) {
	if _, err := ReadGraphIndex(strings.NewReader(`<x> <y> "unterminated .`)); err == nil {
		t.Fatal("expected malformed N-Triples error")
	}
}

func TestGraphIndexPreservesLiteralValues(t *testing.T) {
	nt := graphNT(t, "intent", "i.literal", "active", "") +
		ntLit("intent", "i.literal", rdf.PropComment, `literal with spaces and "quotes"`)
	idx, err := ReadGraphIndex(strings.NewReader(nt))
	if err != nil {
		t.Fatal(err)
	}
	if got := onlyNode(t, idx).Comment; got != `literal with spaces and "quotes"` {
		t.Fatalf("literal=%q", got)
	}
}

func TestGraphIndexIgnoresUnrelatedClasses(t *testing.T) {
	nt := rdf.MintIRI(rdf.ClassComponent, "component.x") + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassComponent) + " .\n"
	idx, err := ReadGraphIndex(strings.NewReader(nt))
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Nodes) != 0 {
		t.Fatalf("nodes=%d", len(idx.Nodes))
	}
}

func TestGraphSnapshotDigestCurrentWhenMatching(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.nt")
	data := []byte(graphNT(t, "intent", "i.one", "active", ""))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := sha256Hex(data)
	got, ok, reasons, err := VerifyGraphSnapshot(path, digest, architecture.GraphDigestResolved)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != digest || reasons[0].Code != "plane.graph.digest_current" {
		t.Fatalf("got=%s ok=%v reasons=%+v", got, ok, reasons)
	}
}

func TestGraphSnapshotDigestMismatchFailsResolvedInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.nt")
	if err := os.WriteFile(path, []byte(graphNT(t, "intent", "i.one", "active", "")), 0o644); err != nil {
		t.Fatal(err)
	}
	_, ok, reasons, err := VerifyGraphSnapshot(path, strings.Repeat("0", 64), architecture.GraphDigestResolved)
	if err != nil {
		t.Fatal(err)
	}
	if ok || reasons[0].Code != "plane.graph.digest_mismatch" {
		t.Fatalf("ok=%v reasons=%+v", ok, reasons)
	}
}

func onlyNode(t *testing.T, idx GraphIndex) GovernedNode {
	t.Helper()
	if len(idx.Nodes) != 1 {
		t.Fatalf("nodes=%d", len(idx.Nodes))
	}
	for _, n := range idx.Nodes {
		return n
	}
	return GovernedNode{}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
