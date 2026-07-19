// SPDX-License-Identifier: Apache-2.0

package evidencereceipt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// schemaProperties reads the top-level property names of a closed
// (additionalProperties:false) schema in the frozen v1 schema set.
func schemaProperties(t *testing.T, name string) map[string]bool {
	t.Helper()
	path := filepath.Join("..", "..", "..", "docs", "schemas", "architectural-closure", "v1", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		AdditionalProperties any            `json:"additionalProperties"`
		Properties           map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if v, ok := doc.AdditionalProperties.(bool); !ok || v {
		t.Fatalf("%s must be a closed schema", name)
	}
	out := map[string]bool{}
	for k := range doc.Properties {
		out[k] = true
	}
	return out
}

func assertKeysWithinSchema(t *testing.T, v any, schema string) {
	t.Helper()
	allowed := schemaProperties(t, schema)
	b, err := closureprotocol.CanonicalJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for k := range m {
		if !allowed[k] {
			t.Fatalf("%s: emitted key %q not permitted by frozen schema", schema, k)
		}
	}
}

func TestReceiptMatchesFrozenSchema(t *testing.T) {
	r := testReceipt()
	assertKeysWithinSchema(t, Canonicalize(r), "evidence-receipt.schema.json")

	// A required-only receipt must also stay within the schema.
	min := Receipt{
		ReceiptID:    "r1",
		EvidenceKind: r.EvidenceKind,
		ProfileID:    "p1",
		ResultBinding: ResultBinding{
			BaseRevision:           "b",
			PatchDigestSHA256:      "pd",
			ResultTreeDigestSHA256: "tree",
			GraphDigestSHA256:      "g",
		},
		Producer:            "prod",
		ObservationPath:     "path",
		ObservedAt:          "2026-07-15T19:05:00Z",
		Status:              r.Status,
		PayloadDigestSHA256: "pl",
	}
	if err := ValidateReceipt(min); err != nil {
		t.Fatalf("minimal receipt should be structurally valid: %v", err)
	}
	assertKeysWithinSchema(t, Canonicalize(min), "evidence-receipt.schema.json")
}

func TestProfileMatchesFrozenSchema(t *testing.T) {
	assertKeysWithinSchema(t, testProfile(), "evidence-profile.schema.json")
}
