// SPDX-License-Identifier: Apache-2.0

package protoscan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSnakeAndLeadingWord(t *testing.T) {
	cases := map[string]struct{ snakeWant, leadWant string }{
		"AwarenessGraph": {"awareness_graph", "Awareness"},
		"EditCheck":      {"edit_check", "Edit"},
		"Query":          {"query", "Query"},
		"Metadata":       {"metadata", "Metadata"},
		"HTTPProbe":      {"http_probe", "H"},
	}
	for in, want := range cases {
		if got := snake(in); got != want.snakeWant {
			t.Errorf("snake(%q) = %q, want %q", in, got, want.snakeWant)
		}
		if got := leadingWord(in); got != want.leadWant {
			t.Errorf("leadingWord(%q) = %q, want %q", in, got, want.leadWant)
		}
	}
}

func TestClassifyReadWrite(t *testing.T) {
	cases := map[string]string{
		"Query":         "read",
		"Metadata":      "read",
		"Resolve":       "read",
		"GetThing":      "read",
		"ListThings":    "read",
		"CreateThing":   "write",
		"DeleteThing":   "write",
		"PromoteX":      "write",
		"UpdateConfig":  "write",
		"EditCheck":     "unknown",
		"FrobnicateFoo": "unknown",
	}
	for name, want := range cases {
		if got := classifyReadWrite(name); got != want {
			t.Errorf("classifyReadWrite(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestScanProto_ServiceAndRPCs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "svc.proto")
	src := `syntax = "proto3";
package demo.thing;

// ThingService manages things.
service ThingService {
  // Get one thing.
  rpc GetThing(GetThingRequest) returns (GetThingResponse);
  // Create a thing.
  rpc CreateThing(CreateThingRequest) returns (CreateThingResponse);
  rpc EditCheck(EditCheckRequest) returns (EditCheckResponse);
}

message GetThingRequest {}
message GetThingResponse {}
message CreateThingRequest {}
message CreateThingResponse {}
message EditCheckRequest {}
message EditCheckResponse {}
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ScanProto(path, dir, map[string]string{"ThingService": "component.thing"})
	if err != nil {
		t.Fatalf("ScanProto: %v", err)
	}

	byID := map[string]Contract{}
	for _, c := range got {
		byID[c.ID] = c
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 contracts, got %d (%v)", len(got), keys(byID))
	}

	svc, ok := byID["contract.thing_service"]
	if !ok {
		t.Fatalf("missing service contract; have %v", keys(byID))
	}
	if svc.Kind != "grpc" || svc.Assertion != "inferred" {
		t.Errorf("service contract: kind=%q assertion=%q", svc.Kind, svc.Assertion)
	}
	if svc.Uml == nil || svc.Uml.Kind != "Interface" {
		t.Errorf("service uml.kind = %v, want Interface", svc.Uml)
	}
	if svc.ReadOrWrite != "read_write" {
		t.Errorf("service read_or_write = %q, want read_write", svc.ReadOrWrite)
	}
	if len(svc.ExposedBy) != 1 || svc.ExposedBy[0] != "component.thing" {
		t.Errorf("service exposed_by = %v, want [component.thing]", svc.ExposedBy)
	}

	checks := []struct{ id, rw, umlKind string }{
		{"contract.thing_service.get_thing", "read", "Operation"},
		{"contract.thing_service.create_thing", "write", "Operation"},
		{"contract.thing_service.edit_check", "unknown", "Operation"},
	}
	for _, c := range checks {
		got, ok := byID[c.id]
		if !ok {
			t.Errorf("missing RPC contract %q", c.id)
			continue
		}
		if got.ReadOrWrite != c.rw {
			t.Errorf("%s read_or_write = %q, want %q", c.id, got.ReadOrWrite, c.rw)
		}
		if got.Uml == nil || got.Uml.Kind != c.umlKind {
			t.Errorf("%s uml.kind = %v, want %q", c.id, got.Uml, c.umlKind)
		}
		if got.Assertion != "inferred" {
			t.Errorf("%s assertion = %q, want inferred", c.id, got.Assertion)
		}
		if len(got.SourceFiles) != 1 || got.SourceFiles[0] != "svc.proto" {
			t.Errorf("%s source_files = %v, want [svc.proto]", c.id, got.SourceFiles)
		}
	}
}

func TestFindProtoFiles(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.proto"), []byte("syntax=\"proto3\";"), 0o644)
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	os.WriteFile(filepath.Join(root, "sub", "b.proto"), []byte("syntax=\"proto3\";"), 0o644)
	os.MkdirAll(filepath.Join(root, "vendor"), 0o755)
	os.WriteFile(filepath.Join(root, "vendor", "c.proto"), []byte("x"), 0o644)

	found, err := FindProtoFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("expected 2 proto files (vendor excluded), got %d: %v", len(found), found)
	}
}

func keys(m map[string]Contract) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
