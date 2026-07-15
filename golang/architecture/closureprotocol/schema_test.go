// SPDX-License-Identifier: Apache-2.0

package closureprotocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSchemasParseAndAreClosed(t *testing.T) {
	root := filepath.Join("..", "..", "..", "docs", "schemas", "architectural-closure", "v1")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 16 {
		t.Fatalf("expected schema set, got %d entries", len(entries))
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		if entry.Name() != "common.schema.json" {
			if v, ok := doc["additionalProperties"]; !ok || v != false {
				t.Fatalf("%s: additionalProperties must be false", entry.Name())
			}
		}
	}
}

