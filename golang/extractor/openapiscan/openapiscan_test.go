// SPDX-License-Identifier: AGPL-3.0-only

package openapiscan

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func byID(cs []Contract) map[string]Contract {
	m := map[string]Contract{}
	for _, c := range cs {
		m[c.ID] = c
	}
	return m
}

const spec30 = `openapi: 3.0.3
info:
  title: Order API
paths:
  /orders:
    get:
      operationId: listOrders
      summary: List orders
    post:
      operationId: createOrder
      summary: Create an order
    parameters:
      - name: limit
        in: query
  /orders/{id}:
    delete:
      deprecated: true
      summary: Remove an order
    $ref: '#/components/x'
`

// TestScanSpec_OpenAPI30 covers Interface aggregation, read/write per method,
// operationId ids, deprecated→stability, and path-level non-method keys ignored.
func TestScanSpec_OpenAPI30(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "api", "openapi.yaml")
	writeFile(t, p, spec30)

	cs, err := ScanSpec(p, root)
	if err != nil {
		t.Fatalf("ScanSpec: %v", err)
	}
	m := byID(cs)
	// 1 Interface + 3 operations (parameters + $ref ignored)
	if len(cs) != 4 {
		t.Fatalf("expected 4 contracts (1 interface + 3 ops), got %d: %v", len(cs), keys(m))
	}
	svc, ok := m["contract.order_api"]
	if !ok || svc.Uml.Kind != "Interface" || svc.Kind != "rest" {
		t.Fatalf("service contract wrong: %+v", svc)
	}
	if svc.ReadOrWrite != "read_write" {
		t.Errorf("service read_or_write = %q, want read_write (GET read + POST/DELETE write)", svc.ReadOrWrite)
	}
	if svc.SourceFiles[0] != "api/openapi.yaml" {
		t.Errorf("source_files = %v, want api/openapi.yaml", svc.SourceFiles)
	}
	if get := m["contract.order_api.listorders"]; get.ReadOrWrite != "read" || get.Uml.Kind != "Operation" {
		t.Errorf("GET op wrong: %+v", get)
	}
	if post := m["contract.order_api.createorder"]; post.ReadOrWrite != "write" {
		t.Errorf("POST op read_or_write = %q, want write", post.ReadOrWrite)
	}
	del, ok := m["contract.order_api.delete_orders_id"] // no operationId → method_path slug
	if !ok {
		t.Fatalf("missing delete op (slug fallback); have %v", keys(m))
	}
	if del.ReadOrWrite != "write" || del.Stability != "deprecated" {
		t.Errorf("DELETE op wrong: read_or_write=%q stability=%q (want write/deprecated)", del.ReadOrWrite, del.Stability)
	}
}

// TestScanSpec_Swagger2AndJSON — Swagger 2.0 and a .json spec parse the same way.
func TestScanSpec_Swagger2AndJSON(t *testing.T) {
	root := t.TempDir()
	sw := filepath.Join(root, "swagger.yaml")
	writeFile(t, sw, "swagger: \"2.0\"\ninfo:\n  title: Legacy API\npaths:\n  /ping:\n    get:\n      operationId: ping\n")
	cs, err := ScanSpec(sw, root)
	if err != nil || len(cs) != 2 {
		t.Fatalf("swagger 2.0: got %d contracts, err %v", len(cs), err)
	}
	if cs[0].ID != "contract.legacy_api" {
		t.Errorf("swagger service id = %q", cs[0].ID)
	}

	// JSON spec (JSON is valid YAML).
	js := filepath.Join(root, "openapi.json")
	writeFile(t, js, `{"openapi":"3.1.0","info":{"title":"JSON API"},"paths":{"/things":{"get":{"operationId":"getThings"}}}}`)
	cj, err := ScanSpec(js, root)
	if err != nil || len(cj) != 2 {
		t.Fatalf("json spec: got %d contracts, err %v", len(cj), err)
	}
	if byID(cj)["contract.json_api.getthings"].ReadOrWrite != "read" {
		t.Errorf("json GET op should be read")
	}
}

// TestScanSpec_NotASpec — a plain YAML (no openapi/swagger key) yields nothing.
func TestScanSpec_NotASpec(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "config.yaml")
	writeFile(t, p, "name: not-a-spec\npaths:\n  - a\n  - b\n")
	cs, err := ScanSpec(p, root)
	if err != nil || cs != nil {
		t.Errorf("non-spec should yield (nil,nil); got %d contracts, err %v", len(cs), err)
	}
}

// TestFindSpecFiles_DiscoversOnlySpecs — discovery picks specs, skips plain YAML/excluded dirs.
func TestFindSpecFiles_DiscoversOnlySpecs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "api", "openapi.yaml"), spec30)
	writeFile(t, filepath.Join(root, "config.yaml"), "name: x\n")                                  // not a spec
	writeFile(t, filepath.Join(root, "node_modules", "dep", "swagger.yaml"), "swagger: \"2.0\"\n") // excluded dir

	found, err := FindSpecFiles(root)
	if err != nil {
		t.Fatalf("FindSpecFiles: %v", err)
	}
	if len(found) != 1 || filepath.Base(found[0]) != "openapi.yaml" {
		t.Errorf("FindSpecFiles = %v, want only api/openapi.yaml", found)
	}
}

// TestRender_Deterministic — repeated Render is byte-identical.
func TestRender_Deterministic(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "openapi.yaml")
	writeFile(t, p, spec30)
	cs, _ := ScanSpec(p, root)
	doc := Doc{Contracts: cs}
	a, err := Render(doc)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	b, _ := Render(doc)
	if string(a) != string(b) {
		t.Error("Render is not deterministic")
	}
}

func keys(m map[string]Contract) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
