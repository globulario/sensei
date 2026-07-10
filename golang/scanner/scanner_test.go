// SPDX-License-Identifier: AGPL-3.0-only

package scanner_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/awareness-graph/golang/scanner"
)

// makeRegistry builds an in-memory namespace registry from a YAML string.
func makeRegistry(t *testing.T, yaml string) *scanner.Registry {
	t.Helper()
	tmp := t.TempDir()
	f := filepath.Join(tmp, "namespaces.yaml")
	if err := os.WriteFile(f, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	r, err := scanner.LoadRegistry(f)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

const testRegistryYAML = `
namespaces:
  - id: acme.platform
    label: ACME Platform
    owns:
      - docs/awareness
      - docs/intent
    description: Core platform knowledge.
  - id: acme.svc.echo
    label: Echo Service
    owns:
      - golang/echo
    description: Echo gRPC service.
`

func TestLoadRegistry_Valid(t *testing.T) {
	r := makeRegistry(t, testRegistryYAML)
	if !r.Has("acme.platform") {
		t.Error("expected acme.platform")
	}
	if !r.Has("acme.svc.echo") {
		t.Error("expected acme.svc.echo")
	}
	if r.Has("nonexistent") {
		t.Error("unexpected nonexistent")
	}
}

func TestLoadRegistry_DuplicateID(t *testing.T) {
	yaml := `
namespaces:
  - id: dup.ns
    label: First
    owns: []
  - id: dup.ns
    label: Second
    owns: []
`
	tmp := t.TempDir()
	f := filepath.Join(tmp, "namespaces.yaml")
	os.WriteFile(f, []byte(yaml), 0644)
	_, err := scanner.LoadRegistry(f)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected duplicate error, got %v", err)
	}
}

func TestNamespaceForPath(t *testing.T) {
	r := makeRegistry(t, testRegistryYAML)
	cases := []struct{ path, want string }{
		{"golang/echo/server.go", "acme.svc.echo"},
		{"golang/echo/sub/dir/file.go", "acme.svc.echo"},
		{"docs/awareness/invariants.yaml", "acme.platform"},
		{"docs/intent/foo.yaml", "acme.platform"},
		{"other/path.go", ""},
	}
	for _, c := range cases {
		got := r.NamespaceForPath(c.path)
		if got != c.want {
			t.Errorf("NamespaceForPath(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// writeGoFile writes a Go source file to a temp directory and returns its path.
func writeGoFile(t *testing.T, dir, relPath, content string) string {
	t.Helper()
	full := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return full
}

func TestScanFile_ValidAnnotation(t *testing.T) {
	r := makeRegistry(t, testRegistryYAML)
	dir := t.TempDir()

	writeGoFile(t, dir, "golang/echo/handlers.go", `package main

// Echo handles incoming echo requests.
//
// @awareness namespace=acme.svc.echo
// @awareness component=server.handler
// @awareness implements=acme.svc.echo:intent.echo_returns_input_unchanged
// @awareness tested_by=golang/echo/server_test.go:TestEchoHandler
func (s *server) Echo() {}
`)

	sc := &scanner.Scanner{Registry: r, RepoRoot: dir}
	result, err := sc.Scan(filepath.Join(dir, "golang/echo"))
	if err != nil {
		t.Fatal(err)
	}
	if result.HasErrors() {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	if len(result.Annotations) == 0 {
		t.Fatal("expected at least one annotation")
	}
	ann := result.Annotations[0]
	if ann.Symbol != "server.Echo" {
		t.Errorf("symbol = %q, want server.Echo", ann.Symbol)
	}
	if ann.Namespace != "acme.svc.echo" {
		t.Errorf("namespace = %q, want acme.svc.echo", ann.Namespace)
	}
	if ann.Component != "server.handler" {
		t.Errorf("component = %q, want server.handler", ann.Component)
	}
	if got := ann.Keys["implements"]; len(got) == 0 || got[0] != "acme.svc.echo:intent.echo_returns_input_unchanged" {
		t.Errorf("implements = %v", got)
	}
}

func TestScanFile_FileLevel(t *testing.T) {
	r := makeRegistry(t, testRegistryYAML)
	dir := t.TempDir()

	writeGoFile(t, dir, "golang/echo/server.go", `// @awareness namespace=acme.svc.echo
// @awareness component=server
// @awareness file_role=grpc_server
package main
`)

	sc := &scanner.Scanner{Registry: r, RepoRoot: dir}
	result, err := sc.Scan(filepath.Join(dir, "golang/echo"))
	if err != nil {
		t.Fatal(err)
	}
	if result.HasErrors() {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	if len(result.Annotations) == 0 {
		t.Fatal("expected file-level annotation")
	}
	ann := result.Annotations[0]
	if ann.Symbol != "" {
		t.Errorf("file-level annotation should have empty symbol, got %q", ann.Symbol)
	}
	if ann.Namespace != "acme.svc.echo" {
		t.Errorf("namespace = %q", ann.Namespace)
	}
}

func TestScanFile_UnknownNamespace(t *testing.T) {
	r := makeRegistry(t, testRegistryYAML)
	dir := t.TempDir()

	writeGoFile(t, dir, "golang/echo/bad.go", `package main

// @awareness namespace=does.not.exist
// @awareness implements=acme.svc.echo:intent.foo
func Foo() {}
`)

	sc := &scanner.Scanner{Registry: r, RepoRoot: dir}
	result, err := sc.Scan(filepath.Join(dir, "golang/echo"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e.Message, "unknown namespace") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected unknown namespace error, got errors: %v", result.Errors)
	}
}

func TestScanFile_UnqualifiedID(t *testing.T) {
	r := makeRegistry(t, testRegistryYAML)
	dir := t.TempDir()

	writeGoFile(t, dir, "golang/echo/bad.go", `package main

// @awareness namespace=acme.svc.echo
// @awareness implements=intent.echo_returns_input_unchanged
func Foo() {}
`)

	sc := &scanner.Scanner{Registry: r, RepoRoot: dir, Strict: true}
	result, err := sc.Scan(filepath.Join(dir, "golang/echo"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e.Message, "fully-qualified") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected unqualified ID error, got: %v", result.Errors)
	}
}

func TestScanFile_UnsupportedKey(t *testing.T) {
	r := makeRegistry(t, testRegistryYAML)
	dir := t.TempDir()

	writeGoFile(t, dir, "golang/echo/bad.go", `package main

// @awareness namespace=acme.svc.echo
// @awareness unknownkey=whatever
func Foo() {}
`)

	sc := &scanner.Scanner{Registry: r, RepoRoot: dir, Strict: true}
	result, err := sc.Scan(filepath.Join(dir, "golang/echo"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e.Message, "unsupported annotation key") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected unsupported key error, got: %v", result.Errors)
	}
}

func TestScanFile_NamespaceInferredFromPath(t *testing.T) {
	r := makeRegistry(t, testRegistryYAML)
	dir := t.TempDir()

	// No explicit namespace= in annotation — should be inferred from path.
	writeGoFile(t, dir, "golang/echo/server.go", `package main

// @awareness component=server
// @awareness implements=acme.svc.echo:intent.echo_returns_input_unchanged
func Serve() {}
`)

	sc := &scanner.Scanner{Registry: r, RepoRoot: dir}
	result, err := sc.Scan(filepath.Join(dir, "golang/echo"))
	if err != nil {
		t.Fatal(err)
	}
	if result.HasErrors() {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	if len(result.Annotations) == 0 {
		t.Fatal("expected annotation")
	}
	if result.Annotations[0].Namespace != "acme.svc.echo" {
		t.Errorf("namespace = %q, want inferred acme.svc.echo", result.Annotations[0].Namespace)
	}
}

func TestScanFile_MissingNamespace(t *testing.T) {
	r := makeRegistry(t, testRegistryYAML)
	dir := t.TempDir()

	// File outside any owned path, no namespace= annotation.
	writeGoFile(t, dir, "other/server.go", `package main

// @awareness component=server
// @awareness implements=acme.svc.echo:intent.foo
func Serve() {}
`)

	sc := &scanner.Scanner{Registry: r, RepoRoot: dir, Strict: true}
	result, err := sc.Scan(filepath.Join(dir, "other"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e.Message, "missing namespace") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected missing namespace error, got: %v", result.Errors)
	}
}

func TestBuildSymbolsAndEdges(t *testing.T) {
	result := &scanner.ScanResult{
		Annotations: []scanner.Annotation{
			{
				File:       "golang/echo/handlers.go",
				Symbol:     "server.Echo",
				SymbolKind: "method",
				Namespace:  "acme.svc.echo",
				Component:  "server.handler",
				Keys: map[string][]string{
					"implements": {"acme.svc.echo:intent.echo_returns_input_unchanged"},
					"tested_by":  {"golang/echo/server_test.go:TestEchoHandler"},
				},
				KeyOrder: []string{"implements", "tested_by"},
			},
		},
	}
	syms, edges, tests := scanner.BuildSymbolsAndEdges(result)
	if len(syms) == 0 {
		t.Fatal("expected symbols")
	}
	if syms[0].ID != "acme.svc.echo:code.go.server.handler.server_Echo" {
		t.Errorf("unexpected ID: %s", syms[0].ID)
	}
	if len(edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(edges))
	}
	if len(tests) != 0 {
		t.Errorf("expected 0 discovered tests, got %d", len(tests))
	}
}

func TestScanFile_DiscoversGoTests(t *testing.T) {
	r := makeRegistry(t, testRegistryYAML)
	dir := t.TempDir()

	writeGoFile(t, dir, "golang/echo/server_test.go", `package main

import "testing"

// TestEchoHandler verifies Echo keeps the payload unchanged.
func TestEchoHandler(t *testing.T) {}

func helper(t *testing.T) {}
`)

	sc := &scanner.Scanner{Registry: r, RepoRoot: dir}
	result, err := sc.Scan(filepath.Join(dir, "golang/echo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DiscoveredTests) != 1 {
		t.Fatalf("expected 1 discovered test, got %d", len(result.DiscoveredTests))
	}
	got := result.DiscoveredTests[0]
	if got.Symbol != "TestEchoHandler" {
		t.Fatalf("Symbol = %q, want TestEchoHandler", got.Symbol)
	}
	if got.File != "golang/echo/server_test.go" {
		t.Fatalf("File = %q", got.File)
	}
}

func TestSkipsGeneratedFiles(t *testing.T) {
	r := makeRegistry(t, testRegistryYAML)
	dir := t.TempDir()

	writeGoFile(t, dir, "golang/echo/foo.pb.go", `package main
// @awareness namespace=acme.svc.echo
// @awareness implements=acme.svc.echo:intent.foo
func Proto() {}
`)
	writeGoFile(t, dir, "golang/echo/zz_gen.go", `package main
// @awareness namespace=acme.svc.echo
// @awareness implements=acme.svc.echo:intent.foo
func Gen() {}
`)

	sc := &scanner.Scanner{Registry: r, RepoRoot: dir}
	result, err := sc.Scan(filepath.Join(dir, "golang/echo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Annotations) > 0 {
		t.Errorf("expected generated files to be skipped, got %d annotations", len(result.Annotations))
	}
}

func TestScanFile_DiscoversTestsInZZPrefixedTestFiles(t *testing.T) {
	r := makeRegistry(t, testRegistryYAML)
	dir := t.TempDir()

	writeGoFile(t, dir, "golang/echo/zz_awareness_required_test_aliases_test.go", `package main

import "testing"

func TestEchoAlias(t *testing.T) {}
`)

	sc := &scanner.Scanner{Registry: r, RepoRoot: dir}
	result, err := sc.Scan(filepath.Join(dir, "golang/echo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DiscoveredTests) != 1 {
		t.Fatalf("expected 1 discovered test, got %d", len(result.DiscoveredTests))
	}
	got := result.DiscoveredTests[0]
	if got.File != "golang/echo/zz_awareness_required_test_aliases_test.go" {
		t.Fatalf("File = %q", got.File)
	}
	if got.Symbol != "TestEchoAlias" {
		t.Fatalf("Symbol = %q", got.Symbol)
	}
}
