// SPDX-License-Identifier: AGPL-3.0-only

package grpcwebscan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/extractor/protoscan"
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

// scanRepo runs the full extractor over a repo root and returns the aggregated
// contracts (the shape the CLI / bootstrap emit).
func scanRepo(t *testing.T, root string) []Contract {
	t.Helper()
	files, err := FindSourceFiles(root)
	if err != nil {
		t.Fatalf("FindSourceFiles: %v", err)
	}
	var all []Usage
	for _, f := range files {
		us, err := ScanFile(f, root)
		if err != nil {
			t.Fatalf("ScanFile %s: %v", f, err)
		}
		all = append(all, us...)
	}
	return Aggregate(all)
}

func byID(cs []Contract) map[string]Contract {
	m := map[string]Contract{}
	for _, c := range cs {
		m[c.ID] = c
	}
	return m
}

// TestScan_NamedImport: a named import of ResourceServiceClient from a grpc-web
// stub is observable consumption on its own.
func TestScan_NamedImport(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "packages", "sdk", "src", "resource.ts"), `
import { ResourceServiceClient } from "globular-web-client/resource/resource_grpc_web_pb";
const c = new ResourceServiceClient(addr, null, {});
`)
	cs := scanRepo(t, root)
	m := byID(cs)
	got, ok := m["contract.resource_service"]
	if !ok {
		t.Fatalf("named import not detected; got %v", keysOf(m))
	}
	if got.Name != "ResourceService" || got.Kind != "grpc" || got.Assertion != "inferred" {
		t.Errorf("contract fields wrong: %+v", got)
	}
	if len(got.ConsumedBy) != 1 || got.ConsumedBy[0] != "component.packages.sdk" {
		t.Errorf("consumed_by = %v, want [component.packages.sdk]", got.ConsumedBy)
	}
}

// TestScan_NamespaceImport: `import * as ns` then `new ns.ResourceServiceClient`.
func TestScan_NamespaceImport(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "packages", "sdk", "src", "groups.ts"), `
import * as resourceGrpc from "globular-web-client/resource/resource_grpc_web_pb";
function clientFactory(): resourceGrpc.ResourceServiceClient {
  return new resourceGrpc.ResourceServiceClient(addr, null, { withCredentials: true });
}
`)
	m := byID(scanRepo(t, root))
	if _, ok := m["contract.resource_service"]; !ok {
		t.Fatalf("namespace-member construction not detected; got %v", keysOf(m))
	}
}

// TestScan_PromiseClient: *ServicePromiseClient recovers the same service.
func TestScan_PromiseClient(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "packages", "sdk", "src", "event.ts"), `
import * as eventGrpc from "globular-web-client/event/event_grpc_web_pb";
const c = new eventGrpc.EventServicePromiseClient(base, null, options);
`)
	m := byID(scanRepo(t, root))
	if _, ok := m["contract.event_service"]; !ok {
		t.Fatalf("EventServicePromiseClient not detected; got %v", keysOf(m))
	}
}

// TestScan_MultipleServices: several distinct services in one SDK.
func TestScan_MultipleServices(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "packages", "sdk", "src", "rbac.ts"), `
import * as rbacGrpc from "globular-web-client/rbac/rbac_grpc_web_pb";
import { FileServiceClient } from "globular-web-client/file/file_grpc_web_pb";
import * as authGrpc from "globular-web-client/authentication/authentication_grpc_web_pb";
const a = new rbacGrpc.RbacServiceClient(addr);
const b = new FileServiceClient(addr);
const c = new authGrpc.AuthenticationServiceClient(addr);
`)
	m := byID(scanRepo(t, root))
	for _, id := range []string{"contract.rbac_service", "contract.file_service", "contract.authentication_service"} {
		if _, ok := m[id]; !ok {
			t.Errorf("missing %s; got %v", id, keysOf(m))
		}
	}
}

// TestScan_DedupAcrossFiles: same service in two components → one contract,
// unioned (sorted) consumed_by.
func TestScan_DedupAcrossFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "packages", "sdk", "src", "a.ts"), `
import * as g from "globular-web-client/resource/resource_grpc_web_pb";
new g.ResourceServiceClient(addr);
`)
	// apps/web rolls up to component.apps.web — a second, distinct consumer.
	writeFile(t, filepath.Join(root, "apps", "web", "src", "b.ts"), `
import { ResourceServiceClient } from "globular-web-client/resource/resource_grpc_web_pb";
new ResourceServiceClient(addr);
`)
	cs := scanRepo(t, root)
	if len(cs) != 1 {
		t.Fatalf("expected 1 contract, got %d: %v", len(cs), cs)
	}
	want := []string{"component.apps.web", "component.packages.sdk"}
	if got := cs[0].ConsumedBy; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("consumed_by = %v, want %v (sorted union)", got, want)
	}
}

// TestScan_IgnoresLocalServiceClass: a local *Service / *ServiceClient with no
// grpc-web provenance is NOT consumption.
func TestScan_IgnoresLocalServiceClass(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "packages", "sdk", "src", "local.ts"), `
class FooService {}
class BarServiceClient {}            // *Client name but no grpc-web import
const a = new FooService();
const b = new BarServiceClient();
export function makeFooService(): FooService { return new FooService(); }
`)
	if cs := scanRepo(t, root); len(cs) != 0 {
		t.Fatalf("expected no contracts for local classes, got %v", cs)
	}
}

// TestScan_IgnoresComputedConstruction: a grpc-web namespace accessed by a
// computed subscript (service name not recoverable) is skipped.
func TestScan_IgnoresComputedConstruction(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "packages", "sdk", "src", "dyn.ts"), `
import * as g from "globular-web-client/resource/resource_grpc_web_pb";
const key = "ResourceServiceClient";
const c = new g[key](addr);          // computed member — not a literal symbol
`)
	if cs := scanRepo(t, root); len(cs) != 0 {
		t.Fatalf("expected no contracts for computed construction, got %v", cs)
	}
}

// TestScan_DropsUnmappableFile: a usage in a file that does not roll up to a
// component is dropped (no consumed_by target).
func TestScan_DropsUnmappableFile(t *testing.T) {
	root := t.TempDir()
	// A file directly under a source root (packages/) maps to no component.
	writeFile(t, filepath.Join(root, "packages", "loose.ts"), `
import { ResourceServiceClient } from "globular-web-client/resource/resource_grpc_web_pb";
new ResourceServiceClient(addr);
`)
	if cs := scanRepo(t, root); len(cs) != 0 {
		t.Fatalf("expected no contracts from unmappable file, got %v", cs)
	}
}

// TestIDParity_WithProtoScan: the contract id this extractor mints for a
// consumed service is byte-identical to the service-level contract id proto-scan
// emits for the same proto service — proving the cross-repo link.
func TestIDParity_WithProtoScan(t *testing.T) {
	root := t.TempDir()
	// Consumer side: frontend uses ResourceServiceClient.
	writeFile(t, filepath.Join(root, "packages", "sdk", "src", "resource.ts"), `
import { ResourceServiceClient } from "globular-web-client/resource/resource_grpc_web_pb";
new ResourceServiceClient(addr);
`)
	cs := scanRepo(t, root)
	if len(cs) != 1 {
		t.Fatalf("expected 1 consumed contract, got %v", cs)
	}
	consumedID := cs[0].ID

	// Definer side: a backend proto defining `service ResourceService`.
	protoPath := filepath.Join(root, "proto", "resource.proto")
	writeFile(t, protoPath, `syntax = "proto3";
package test;
service ResourceService {
  rpc GetResource(GetRq) returns (GetRsp);
}
message GetRq {}
message GetRsp {}
`)
	defs, err := protoscan.ScanProto(protoPath, root, nil)
	if err != nil {
		t.Fatalf("ScanProto: %v", err)
	}
	var svcID string
	for _, c := range defs {
		if c.Uml != nil && c.Uml.Kind == "Interface" {
			svcID = c.ID
		}
	}
	if svcID == "" {
		t.Fatal("proto-scan produced no service-level contract")
	}
	if consumedID != svcID {
		t.Fatalf("id mismatch: consumer emits %q, proto-scan defines %q — link would break", consumedID, svcID)
	}
}

// TestRender_Deterministic.
func TestRender_Deterministic(t *testing.T) {
	doc := Doc{Contracts: Aggregate([]Usage{
		{Service: "ResourceService", Consumer: "component.packages.sdk"},
		{Service: "RbacService", Consumer: "component.apps.web"},
		{Service: "ResourceService", Consumer: "component.apps.web"},
	})}
	a, err := Render(doc)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	b, _ := Render(doc)
	if string(a) != string(b) {
		t.Error("Render is not deterministic")
	}
}

func keysOf(m map[string]Contract) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
