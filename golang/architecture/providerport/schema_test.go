// SPDX-License-Identifier: AGPL-3.0-only

package providerport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func schemaRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "docs", "schemas", "providerport", "v1")
}

func synthesisSchemaRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "docs", "schemas", "synthesis", "v1")
}

func readSchemaDoc(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return doc
}

var allSchemaFilenames = []string{
	CapabilitiesSchemaFilename,
	RequestSchemaFilename,
	ResultSchemaFilename,
	ObservationBatchSchemaFilename,
	ReceiptSchemaFilename,
}

// TestEmbeddedSchemasMatchCanonicalSource proves the go:embed'd copy under
// schemas/ is byte-identical to the canonical, cross-repo-pinned source
// under docs/schemas/providerport/v1/ -- go:embed cannot reach outside this
// package directory with a ".." pattern, so the canonical file is
// mechanically copied here at commit time, and this test is what stops the
// two from silently drifting apart.
func TestEmbeddedSchemasMatchCanonicalSource(t *testing.T) {
	canonicalRoot := schemaRoot(t)
	for _, filename := range allSchemaFilenames {
		t.Run(filename, func(t *testing.T) {
			canonical, err := os.ReadFile(filepath.Join(canonicalRoot, filename))
			if err != nil {
				t.Fatal(err)
			}
			embedded, err := embeddedSchemas.ReadFile("schemas/" + filename)
			if err != nil {
				t.Fatal(err)
			}
			if string(canonical) != string(embedded) {
				t.Fatalf("golang/architecture/providerport/schemas/%s has drifted from docs/schemas/providerport/v1/%s -- re-copy the canonical file", filename, filename)
			}
		})
	}
}

// TestEmbeddedSynthesisSchemasMatchCanonicalSource proves the go:embed'd
// copy under schemas/synthesis/ (the O1 schemas Request/Result payloads
// $ref) is byte-identical to the canonical, cross-repo-pinned source under
// docs/schemas/synthesis/v1/ -- this is a second, independent vendoring
// relationship from TestEmbeddedSchemasMatchCanonicalSource above, and
// needs its own drift check so a change to O1's schemas can never silently
// diverge from what this package's payloads validate against.
func TestEmbeddedSynthesisSchemasMatchCanonicalSource(t *testing.T) {
	canonicalRoot := synthesisSchemaRoot(t)
	for _, filename := range vendoredSynthesisSchemaFilenames {
		t.Run(filename, func(t *testing.T) {
			canonical, err := os.ReadFile(filepath.Join(canonicalRoot, filename))
			if err != nil {
				t.Fatal(err)
			}
			embedded, err := embeddedSynthesisSchemas.ReadFile("schemas/synthesis/" + filename)
			if err != nil {
				t.Fatal(err)
			}
			if string(canonical) != string(embedded) {
				t.Fatalf("golang/architecture/providerport/schemas/synthesis/%s has drifted from docs/schemas/synthesis/v1/%s -- re-copy the canonical file", filename, filename)
			}
		})
	}
}

// TestSchemasParseAndAreClosed proves every vendored schema parses as JSON
// and keeps additionalProperties:false at its object root.
func TestSchemasParseAndAreClosed(t *testing.T) {
	root := schemaRoot(t)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(allSchemaFilenames) {
		t.Fatalf("expected exactly the %d adopted providerport schemas, got %d entries", len(allSchemaFilenames), len(entries))
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		doc := readSchemaDoc(t, filepath.Join(root, entry.Name()))
		if v, ok := doc["additionalProperties"]; !ok || v != false {
			t.Fatalf("%s: root additionalProperties must be false", entry.Name())
		}
	}
}

func constString(t *testing.T, doc map[string]any, path ...string) string {
	t.Helper()
	var cur any = doc
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("path %v: %q is not an object", path, p)
		}
		cur, ok = m[p]
		if !ok {
			t.Fatalf("path %v: missing key %q", path, p)
		}
	}
	s, ok := cur.(string)
	if !ok {
		t.Fatalf("path %v: not a string", path)
	}
	return s
}

// TestSchemaVersionIdentifiersMatchAdopted confirms every vendored schema's
// const schema_version exactly matches this package's Go constant.
func TestSchemaVersionIdentifiersMatchAdopted(t *testing.T) {
	root := schemaRoot(t)
	cases := []struct {
		filename string
		want     string
	}{
		{CapabilitiesSchemaFilename, CapabilitiesSchemaVersion},
		{RequestSchemaFilename, RequestSchemaVersion},
		{ResultSchemaFilename, ResultSchemaVersion},
		{ObservationBatchSchemaFilename, ObservationBatchSchemaVersion},
		{ReceiptSchemaFilename, ReceiptSchemaVersion},
	}
	for _, c := range cases {
		doc := readSchemaDoc(t, filepath.Join(root, c.filename))
		if got := constString(t, doc, "properties", "schema_version", "const"); got != c.want {
			t.Errorf("%s schema_version const = %q, want %q", c.filename, got, c.want)
		}
	}
}

const zeroDigest = "0000000000000000000000000000000000000000000000000000000000000000"
