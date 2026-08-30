package main

import (
	"testing"

	"github.com/globulario/sensei/golang/extractor"
)

// A file that CONTRIBUTED TRIPLES with no consumed digest must enter the proof
// as unprovable, never be omitted from it.
//
// Skipping it turned "not digested" into "not consumed", so the JSONL importer
// -- which records no digest -- published facts nothing checked. Lack of
// functionality is acceptable; false attestation is not.
func TestConsumedManifestCarriesUnprovableImports(t *testing.T) {
	rep := extractor.ImportReport{Files: []extractor.FileReport{
		{Path: "a.yaml", Status: extractor.StatusImported, ConsumedDigest: "abc"},
		{Path: "runs.jsonl", Status: extractor.StatusImported}, // read, never digested
		{Path: "notes.md", Status: extractor.StatusIgnored},    // never read
	}}
	got := consumedFrom(rep)

	byPath := map[string]string{}
	for _, f := range got {
		byPath[f.Path] = f.Digest
	}
	if d, ok := byPath["a.yaml"]; !ok || d != "abc" {
		t.Fatalf("a digested import lost its digest: %v", byPath)
	}
	d, ok := byPath["runs.jsonl"]
	if !ok {
		t.Fatal("an imported file with no digest was omitted from the proof: it would publish facts nothing checked")
	}
	if d != "" {
		t.Fatalf("an undigested import gained a digest from somewhere: %q", d)
	}
	if _, ok := byPath["notes.md"]; ok {
		t.Fatal("a file that was never read entered the proof")
	}
}
