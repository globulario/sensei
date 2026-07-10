// SPDX-License-Identifier: AGPL-3.0-only

// Package grpcwebscan extracts observable gRPC-web service-client CONSUMPTION
// from TS/JS into architecture consumed_by edges on Contract nodes.
//
// The signal is provenance + symbol: a client is consumed when a symbol whose
// name ends in `Client`/`PromiseClient` is imported from a generated grpc-web
// stub module (a specifier containing `_grpc_web_pb`) and either imported by
// name or accessed as a member of that module's namespace. The backend service
// name is recovered from the CLIENT SYMBOL (strip the Client/PromiseClient
// suffix) — never from the import path, which need not match (e.g.
// `…/repository/repository_grpc_web_pb` exports `PackageRepositoryClient`).
//
// The recovered service is minted to `contract.<snake(Service)>` using the SAME
// conversion proto-scan uses (protoscan.Snake), so the emitted edge links to the
// Contract proto-scan already defines — cross-repo, when both are in the graph.
// The consuming component is the import-graph rollup of the source file
// (importgraph.ComponentForFile), so edges land on real component nodes.
//
// Boundary: observable usage only. No intent, no UI-role, no framework, no call
// graph, no per-method consumption (v1 is service-level). Every contract carries
// assertion: inferred and reuses the contracts: schema — the reusable core
// behind both the `grpcweb-scan` CLI and `awg bootstrap`. No schema, importer,
// or vocabulary change: it only adds a Contract.consumed_by edge.
package grpcwebscan

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
	tstypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
	yaml "gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/extractor/importgraph"
	"github.com/globulario/sensei/golang/extractor/protoscan"
)

// Contract is the subset of the contracts: YAML the spine importer reads that
// this extractor emits. id/name/kind are reconstructed identically to
// proto-scan's service-level contract (so co-ingested triples merge cleanly),
// and consumed_by carries the new edge. Field order is the YAML key order — keep
// it stable for deterministic output.
type Contract struct {
	ID         string   `yaml:"id"`
	Name       string   `yaml:"name"`
	Kind       string   `yaml:"kind"`
	Assertion  string   `yaml:"assertion"`
	ConsumedBy []string `yaml:"consumed_by"`
}

// Doc is the top-level contracts: document.
type Doc struct {
	Contracts []Contract `yaml:"contracts"`
}

// Usage is one observed (service, consumer) consumption fact from a file.
type Usage struct {
	Service  string // recovered backend service name, e.g. "ResourceService"
	Consumer string // consuming component id, e.g. "component.packages.sdk"
}

var sourceExts = map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true}

func excludedDir(name string) bool {
	switch name {
	case "vendor", "node_modules", ".git", "dist", "build", "bin", "out",
		"third_party", "thirdparty", "generated", "candidates", ".awg", "testdata",
		"target", "example", "examples", ".vscode":
		return true
	}
	return false
}

// isSourceFile reports whether name is a scannable TS/JS source. The generated
// grpc-web stubs (_grpc_web_pb / _pb), declaration files, and tests are excluded:
// we scan the CONSUMER code that imports the stubs, not the stubs themselves.
func isSourceFile(name string) bool {
	l := strings.ToLower(name)
	if strings.HasSuffix(l, ".d.ts") {
		return false
	}
	if strings.Contains(l, "_pb.") || strings.HasSuffix(l, "_grpc_web_pb.ts") || strings.HasSuffix(l, "_grpc_web_pb.js") {
		return false
	}
	for _, suf := range []string{".test.ts", ".test.tsx", ".test.js", ".test.jsx",
		".spec.ts", ".spec.tsx", ".spec.js", ".spec.jsx"} {
		if strings.HasSuffix(l, suf) {
			return false
		}
	}
	return sourceExts[filepath.Ext(l)]
}

// FindSourceFiles walks root for scannable TS/JS files (sorted, deterministic).
func FindSourceFiles(root string) ([]string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	var out []string
	walkErr := filepath.WalkDir(absRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != absRoot && excludedDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if isSourceFile(d.Name()) {
			out = append(out, p)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Strings(out)
	return out, nil
}

// ScanFile parses one TS/JS file and returns its gRPC-web consumption facts.
// Usages in files that do not roll up to a component are dropped (unmappable).
func ScanFile(path, repoRoot string) ([]Usage, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	relPath := path
	if r, rerr := filepath.Rel(repoRoot, path); rerr == nil {
		relPath = filepath.ToSlash(r)
	}
	consumer, ok := importgraph.ComponentForFile(relPath)
	if !ok {
		return nil, nil // file does not map to a component → cannot attribute
	}

	jsx := strings.HasSuffix(strings.ToLower(path), ".tsx") || strings.HasSuffix(strings.ToLower(path), ".jsx")
	services := extractServices(src, jsx)
	out := make([]Usage, 0, len(services))
	for _, s := range services {
		out = append(out, Usage{Service: s, Consumer: consumer})
	}
	return out, nil
}

// Aggregate folds usages into deterministic Contract entries: one per consumed
// contract id, consumed_by = the union of consumer components (sorted). The
// id/name/kind are reconstructed to match proto-scan's service-level contract.
func Aggregate(usages []Usage) []Contract {
	consumers := map[string]map[string]bool{} // contract id -> set of components
	names := map[string]string{}              // contract id -> service name
	for _, u := range usages {
		if u.Service == "" || u.Consumer == "" {
			continue
		}
		id := "contract." + protoscan.Snake(u.Service)
		if consumers[id] == nil {
			consumers[id] = map[string]bool{}
		}
		consumers[id][u.Consumer] = true
		names[id] = u.Service
	}
	ids := make([]string, 0, len(consumers))
	for id := range consumers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Contract, 0, len(ids))
	for _, id := range ids {
		out = append(out, Contract{
			ID:         id,
			Name:       names[id],
			Kind:       "grpc",
			Assertion:  "inferred",
			ConsumedBy: sortedKeys(consumers[id]),
		})
	}
	return out
}

// extractServices walks the tree-sitter tree and returns the distinct backend
// service names this file observably consumes (sorted).
func extractServices(src []byte, jsx bool) []string {
	parser := ts.NewParser()
	defer parser.Close()
	lang := ts.NewLanguage(tstypescript.LanguageTypescript())
	if jsx {
		lang = ts.NewLanguage(tstypescript.LanguageTSX())
	}
	if err := parser.SetLanguage(lang); err != nil {
		return nil
	}
	tree := parser.Parse(src, nil)
	if tree == nil {
		return nil
	}
	defer tree.Close()
	root := tree.RootNode()

	grpcNS := map[string]bool{}   // namespace/default bindings of grpc-web modules
	services := map[string]bool{} // recovered service names (deduped per file)

	// Pass 1 — imports: collect grpc-web namespaces and any named client symbols.
	var visitImports func(n *ts.Node)
	visitImports = func(n *ts.Node) {
		if n.Kind() == "import_statement" && isGrpcWebModule(importSource(n, src)) {
			collectImportBindings(n, src, grpcNS, services)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			visitImports(n.NamedChild(i))
		}
	}
	visitImports(root)

	// Pass 2 — member access on a grpc-web namespace: `ns.XxxClient` (covers
	// `new ns.XxxClient(...)`, type positions, and aliasing assignments).
	if len(grpcNS) > 0 {
		var visitMembers func(n *ts.Node)
		visitMembers = func(n *ts.Node) {
			if n.Kind() == "member_expression" {
				obj := n.ChildByFieldName("object")
				prop := n.ChildByFieldName("property")
				if obj != nil && prop != nil && obj.Kind() == "identifier" && grpcNS[nodeText(obj, src)] {
					if svc, ok := serviceFromClientSymbol(nodeText(prop, src)); ok {
						services[svc] = true
					}
				}
			}
			for i := uint(0); i < n.NamedChildCount(); i++ {
				visitMembers(n.NamedChild(i))
			}
		}
		visitMembers(root)
	}

	return sortedKeys(services)
}

// collectImportBindings records, from one grpc-web import statement: namespace /
// default bindings (used later as `ns.XxxClient`), and direct named client
// imports (consumption is observable from the import alone).
func collectImportBindings(importStmt *ts.Node, src []byte, grpcNS, services map[string]bool) {
	clause := namedChildOfKind(importStmt, "import_clause")
	if clause == nil {
		return // side-effect import: `import "...";` — no bindings
	}
	for i := uint(0); i < clause.NamedChildCount(); i++ {
		c := clause.NamedChild(i)
		switch c.Kind() {
		case "namespace_import": // import * as ns from "..."
			if id := namedChildOfKind(c, "identifier"); id != nil {
				grpcNS[nodeText(id, src)] = true
			}
		case "identifier": // default import: import ns from "..."
			grpcNS[nodeText(c, src)] = true
		case "named_imports": // import { A, B as C } from "..."
			for j := uint(0); j < c.NamedChildCount(); j++ {
				spec := c.NamedChild(j)
				if spec.Kind() != "import_specifier" {
					continue
				}
				name := spec.ChildByFieldName("name") // the imported (original) symbol
				if name == nil {
					continue
				}
				if svc, ok := serviceFromClientSymbol(nodeText(name, src)); ok {
					services[svc] = true
				}
			}
		}
	}
}

// serviceFromClientSymbol recovers a backend service name from a grpc-web client
// symbol: strip a trailing PromiseClient / Client. A symbol that is not a client
// (no such suffix, or the suffix is the whole symbol) yields ok=false.
func serviceFromClientSymbol(sym string) (string, bool) {
	for _, suf := range []string{"PromiseClient", "Client"} {
		if strings.HasSuffix(sym, suf) && len(sym) > len(suf) {
			return strings.TrimSuffix(sym, suf), true
		}
	}
	return "", false
}

// isGrpcWebModule reports whether an import specifier is a generated grpc-web
// stub module. `_grpc_web_pb` is the standard grpc-web codegen suffix — generic,
// not project-specific.
func isGrpcWebModule(spec string) bool {
	return strings.Contains(spec, "_grpc_web_pb")
}

// importSource returns the string specifier of an import statement.
func importSource(importStmt *ts.Node, src []byte) string {
	if s := namedChildOfKind(importStmt, "string"); s != nil {
		return stringValue(s, src)
	}
	return ""
}

// Render produces the deterministic generated YAML.
func Render(doc Doc) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("# GENERATED by cmd/grpcweb-scan — DO NOT EDIT.\n")
	buf.WriteString("# consumed_by edges inferred from gRPC-web service-client usage (assertion: inferred).\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	enc.Close()
	return buf.Bytes(), nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func nodeText(n *ts.Node, src []byte) string { return string(src[n.StartByte():n.EndByte()]) }

func namedChildOfKind(n *ts.Node, kind string) *ts.Node {
	for i := uint(0); i < n.NamedChildCount(); i++ {
		if c := n.NamedChild(i); c.Kind() == kind {
			return c
		}
	}
	return nil
}

func stringValue(n *ts.Node, src []byte) string {
	for i := uint(0); i < n.NamedChildCount(); i++ {
		if c := n.NamedChild(i); c.Kind() == "string_fragment" {
			return nodeText(c, src)
		}
	}
	t := nodeText(n, src)
	if len(t) >= 2 {
		t = t[1 : len(t)-1]
	}
	return t
}

func sortedKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
