// SPDX-License-Identifier: AGPL-3.0-only

package extractor

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/extractor/grpcwebscan"
	"github.com/globulario/sensei/golang/rdf"
)

// TestGrpcWebConsumption_RoundTripIngest proves the contract-consumption
// extractor's output is ingested by the real contracts: importer and produces
// the expected aw:consumedBy edge linking the backend Contract id (the same id
// proto-scan mints) to the consuming component. This is the round-trip the
// scope calls for: render → ingest → assert the linking triple.
func TestGrpcWebConsumption_RoundTripIngest(t *testing.T) {
	doc := grpcwebscan.Doc{Contracts: grpcwebscan.Aggregate([]grpcwebscan.Usage{
		{Service: "ResourceService", Consumer: "component.packages.sdk"},
	})}
	data, err := grpcwebscan.Render(doc)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	path := filepath.Join(t.TempDir(), "contract_consumption.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	e := rdf.NewEmitter(&buf)
	if err := importArchitectureContracts(e, path); err != nil {
		t.Fatalf("importArchitectureContracts: %v", err)
	}
	if err := e.Flush(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	subj := rdf.MintIRI(rdf.ClassContract, "contract.resource_service")
	obj := rdf.MintIRI(rdf.ClassComponent, "component.packages.sdk")
	wantEdge := subj + " " + rdf.IRI(rdf.PropConsumedBy) + " " + obj + " ."
	if !strings.Contains(out, wantEdge) {
		t.Errorf("ingested triples missing consumed_by edge.\nwant line: %s\ngot:\n%s", wantEdge, out)
	}
	// The subject must be typed as a Contract (a real, linkable node).
	wantType := subj + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassContract) + " ."
	if !strings.Contains(out, wantType) {
		t.Errorf("ingested triples missing contract type.\nwant line: %s\ngot:\n%s", wantType, out)
	}
}
