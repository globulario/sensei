// SPDX-License-Identifier: Apache-2.0

// Command http-scan extracts HTTP implementation contracts from Go source.
//
// It finds route registrations of the form mux.Handle("/route", handler) and
// mux.HandleFunc("/route", fn) and emits one inferred Contract node per route,
// in the same `contracts:` schema the awareness importer already ingests (so no
// importer change is needed). These are IMPLEMENTATION contracts — the executable
// HTTP surface — distinct from hand-authored ARCHITECTURAL contracts; Phase 2's
// contract_realizations schema links the two.
//
// It decides no architecture and no semantics: every node is assertion=inferred.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

type contract struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Kind        string   `yaml:"kind"`
	ReadOrWrite string   `yaml:"read_or_write,omitempty"`
	Assertion   string   `yaml:"assertion"`
	SourceFiles []string `yaml:"source_files"`
	Uml         uml      `yaml:"uml"`
}

type uml struct {
	Kind       string `yaml:"kind"`
	Stereotype string `yaml:"stereotype"`
	View       string `yaml:"view"`
	Confidence string `yaml:"confidence"`
}

type doc struct {
	Contracts []contract `yaml:"contracts"`
}

func main() {
	var (
		repoRoot = flag.String("repo-root", ".", "repository root to scan for HTTP route registrations")
		output   = flag.String("output", "", "output YAML path (default: stdout)")
		check    = flag.Bool("check", false, "regenerate in memory, diff the committed -output, exit 1 if stale")
	)
	flag.Parse()

	routes, err := scan(*repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "http-scan: %v\n", err)
		os.Exit(2)
	}
	out, err := render(routes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "http-scan: render: %v\n", err)
		os.Exit(2)
	}

	if *check {
		committed, _ := os.ReadFile(*output)
		if !bytes.Equal(bytes.TrimSpace(committed), bytes.TrimSpace(out)) {
			fmt.Fprintf(os.Stderr, "http-scan: STALE — %s differs from a fresh scan; run http-scan to regenerate\n", *output)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "http-scan: fresh (%d HTTP contracts)\n", len(routes))
		return
	}

	if *output == "" {
		os.Stdout.Write(out)
		return
	}
	if err := os.WriteFile(*output, out, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "http-scan: write: %v\n", err)
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "http-scan: wrote %d HTTP contracts to %s\n", len(routes), *output)
}

// route is one discovered HTTP route registration.
type route struct {
	path      string // the URL path literal, e.g. "/api/save-config"
	handler   string // best-effort handler expression, e.g. "d.SaveConfig"
	sourceRel string
}

func scan(root string) ([]route, error) {
	fset := token.NewFileSet()
	byPath := map[string]route{} // dedup by path; first registration wins

	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && excludedDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") ||
			strings.HasSuffix(name, ".pb.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			return nil // tolerate unparseable files
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc" {
				return true
			}
			if len(call.Args) < 1 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			path, uerr := strconv.Unquote(lit.Value)
			if uerr != nil || path == "" || path[0] != '/' {
				return true
			}
			if _, seen := byPath[path]; seen {
				return true
			}
			byPath[path] = route{path: path, handler: handlerExpr(call.Args), sourceRel: rel}
			return true
		})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	out := make([]route, 0, len(byPath))
	for _, r := range byPath {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out, nil
}

// handlerExpr renders a best-effort dotted name for the handler argument
// (call.Args[1]) — "d.SaveConfig", "serve", etc. Empty when not a simple ref.
func handlerExpr(args []ast.Expr) string {
	if len(args) < 2 {
		return ""
	}
	switch h := args[1].(type) {
	case *ast.SelectorExpr:
		if x, ok := h.X.(*ast.Ident); ok {
			return x.Name + "." + h.Sel.Name
		}
		return h.Sel.Name
	case *ast.Ident:
		return h.Name
	}
	return ""
}

func render(routes []route) ([]byte, error) {
	d := doc{Contracts: make([]contract, 0, len(routes))}
	seen := map[string]bool{}
	for _, r := range routes {
		id := "contract.http." + slug(r.path)
		if seen[id] {
			continue // distinct paths that collapse to the same id — keep the first (sorted)
		}
		seen[id] = true
		desc := fmt.Sprintf("HTTP route %s", r.path)
		if r.handler != "" {
			desc += " handled by " + r.handler
		}
		desc += "."
		d.Contracts = append(d.Contracts, contract{
			ID:          id,
			Name:        "HTTP " + r.path,
			Description: desc,
			Kind:        "http",
			ReadOrWrite: readOrWrite(r.path),
			Assertion:   "inferred",
			SourceFiles: []string{r.sourceRel},
			Uml:         uml{Kind: "Interface", Stereotype: "http_endpoint", View: "interaction", Confidence: "inferred"},
		})
	}
	var buf bytes.Buffer
	buf.WriteString("# GENERATED by cmd/http-scan — DO NOT EDIT.\n")
	buf.WriteString("# HTTP implementation contracts inferred from mux.Handle/HandleFunc routes (assertion: inferred).\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(d); err != nil {
		return nil, err
	}
	enc.Close()
	return buf.Bytes(), nil
}

// slug turns "/api/list-items" into "api_list_items". A subtree/prefix route
// ("/items/") gets a "_subtree" suffix so it stays distinct from the exact
// route ("/items") — they are different handlers.
func slug(path string) string {
	subtree := strings.HasSuffix(path, "/") && path != "/"
	var b strings.Builder
	prevUnderscore := true // trim leading separators
	for _, r := range strings.ToLower(strings.Trim(path, "/")) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevUnderscore = false
		} else if !prevUnderscore {
			b.WriteByte('_')
			prevUnderscore = true
		}
	}
	s := strings.Trim(b.String(), "_")
	if s == "" {
		s = "root"
	}
	if subtree {
		s += "_subtree"
	}
	return s
}

// readOrWrite infers a coarse read/write classification from the route verb.
func readOrWrite(path string) string {
	p := strings.ToLower(path)
	for _, w := range []string{"save", "set", "create", "update", "delete", "apply", "sign", "upload", "renew", "regenerate"} {
		if strings.Contains(p, w) {
			return "write"
		}
	}
	for _, r := range []string{"get", "list", "describe", "status", "config", "stats", "metrics", "serve"} {
		if strings.Contains(p, r) {
			return "read"
		}
	}
	return ""
}

func excludedDir(name string) bool {
	switch name {
	case "vendor", "node_modules", ".git", "dist", "build", "bin", "out",
		"third_party", "thirdparty", "generated", "testdata", "target",
		".venv", "venv", "__pycache__", ".idea", ".vscode", "example", "examples":
		return true
	}
	return false
}
