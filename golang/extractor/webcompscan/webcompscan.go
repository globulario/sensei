// SPDX-License-Identifier: AGPL-3.0-only

// Package webcompscan extracts native Web Components into architecture
// Component nodes.
//
// The registration is the signal: a custom element is declared by
// customElements.define('tag', …) (incl. window.customElements.define) or Lit's
// @customElement('tag') decorator. Only string-literal tags are taken (purely
// observable). A class that merely extends HTMLElement/LitElement without a
// registration is NOT a declared element and is not emitted. No framework
// inference (React/Vue/Angular) — that is a separate concern.
//
// Every Component carries assertion: inferred. It reuses the components: schema,
// and is the reusable core behind both the `webcomponent-scan` CLI and
// `awg bootstrap`.
package webcompscan

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
)

// Component mirrors the subset of the components: YAML the spine importer reads.
type Component struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Kind        string   `yaml:"kind"`
	Assertion   string   `yaml:"assertion"`
	SourceFiles []string `yaml:"source_files"`
	Uml         *UML     `yaml:"uml,omitempty"`
}

// UML is the optional UML profile block.
type UML struct {
	Kind       string `yaml:"kind"`
	Stereotype string `yaml:"stereotype"`
	View       string `yaml:"view"`
	Confidence string `yaml:"confidence"`
}

// Doc is the top-level components: document.
type Doc struct {
	Components []Component `yaml:"components"`
}

var sourceExts = map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true}

func excludedDir(name string) bool {
	switch name {
	case "vendor", "node_modules", ".git", "dist", "build", "bin", "out",
		"third_party", "thirdparty", "generated", "candidates", ".sensei", ".awg", "testdata",
		"target", "example", "examples", ".vscode":
		return true
	}
	return false
}

// isSourceFile reports whether name is a scannable TS/JS source (not a
// declaration, generated, or test file).
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

// element is a discovered custom-element registration.
type element struct {
	tag       string
	className string
}

// ScanFile parses one TS/JS file and returns its custom-element registrations.
func ScanFile(path, repoRoot string) ([]Component, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	relPath := path
	if r, rerr := filepath.Rel(repoRoot, path); rerr == nil {
		relPath = filepath.ToSlash(r)
	}

	elems := extractElements(src, strings.HasSuffix(strings.ToLower(path), ".tsx") || strings.HasSuffix(strings.ToLower(path), ".jsx"))
	out := make([]Component, 0, len(elems))
	for _, e := range elems {
		desc := "Custom element <" + e.tag + ">"
		if e.className != "" {
			desc += " (" + e.className + ")"
		}
		out = append(out, Component{
			ID:          "component." + slug(e.tag),
			Name:        e.tag,
			Description: desc,
			Kind:        "ui_component",
			Assertion:   "inferred",
			SourceFiles: []string{relPath},
			Uml:         &UML{Kind: "Component", Stereotype: "web_component", View: "structural", Confidence: "inferred"},
		})
	}
	return out, nil
}

// extractElements walks the tree-sitter tree for custom-element registrations.
func extractElements(src []byte, jsx bool) []element {
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

	var out []element
	var visit func(n *ts.Node)
	visit = func(n *ts.Node) {
		switch n.Kind() {
		case "call_expression":
			if tag, cls, ok := defineCall(n, src); ok {
				out = append(out, element{tag: tag, className: cls})
			}
		case "decorator":
			if tag, ok := customElementDecorator(n, src); ok {
				cls := decoratedClassName(n, src)
				out = append(out, element{tag: tag, className: cls})
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			visit(n.NamedChild(i))
		}
	}
	visit(tree.RootNode())
	return out
}

// defineCall matches (window.)customElements.define('tag', Class).
func defineCall(n *ts.Node, src []byte) (tag, className string, ok bool) {
	if n.NamedChildCount() == 0 {
		return "", "", false
	}
	callee := nodeText(n.NamedChild(0), src)
	if callee != "customElements.define" && callee != "window.customElements.define" &&
		!strings.HasSuffix(callee, ".customElements.define") {
		return "", "", false
	}
	args := namedChildOfKind(n, "arguments")
	if args == nil {
		return "", "", false
	}
	var strs, idents []string
	for i := uint(0); i < args.NamedChildCount(); i++ {
		a := args.NamedChild(i)
		switch a.Kind() {
		case "string":
			strs = append(strs, stringValue(a, src))
		case "identifier":
			idents = append(idents, nodeText(a, src))
		}
	}
	if len(strs) == 0 || strings.TrimSpace(strs[0]) == "" {
		return "", "", false // non-literal tag → not observable, skip
	}
	cls := ""
	if len(idents) > 0 {
		cls = idents[0]
	}
	return strs[0], cls, true
}

// customElementDecorator matches @customElement('tag').
func customElementDecorator(n *ts.Node, src []byte) (string, bool) {
	// decorator → call_expression customElement('tag')
	var call *ts.Node
	for i := uint(0); i < n.NamedChildCount(); i++ {
		if c := n.NamedChild(i); c.Kind() == "call_expression" {
			call = c
			break
		}
	}
	if call == nil || call.NamedChildCount() == 0 {
		return "", false
	}
	if nodeText(call.NamedChild(0), src) != "customElement" {
		return "", false
	}
	args := namedChildOfKind(call, "arguments")
	if args == nil {
		return "", false
	}
	for i := uint(0); i < args.NamedChildCount(); i++ {
		if a := args.NamedChild(i); a.Kind() == "string" {
			if v := stringValue(a, src); strings.TrimSpace(v) != "" {
				return v, true
			}
		}
	}
	return "", false
}

// decoratedClassName returns the name of the class the decorator is attached to.
func decoratedClassName(decorator *ts.Node, src []byte) string {
	// The decorator's sibling/parent is a class_declaration; find the type_identifier.
	for p := decorator.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "class_declaration" {
			for i := uint(0); i < p.NamedChildCount(); i++ {
				if c := p.NamedChild(i); c.Kind() == "type_identifier" {
					return nodeText(c, src)
				}
			}
			return ""
		}
	}
	return ""
}

// Render produces the deterministic generated YAML.
func Render(doc Doc) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("# GENERATED by cmd/webcomponent-scan — DO NOT EDIT.\n")
	buf.WriteString("# Component nodes inferred from native Web Component registrations (assertion: inferred).\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	enc.Close()
	return buf.Bytes(), nil
}

// Dedupe merges components sharing an id (across files), unions source_files,
// and returns them sorted by id — the deterministic final document body.
func Dedupe(in []Component) []Component {
	byID := map[string]*Component{}
	var order []string
	for i := range in {
		c := in[i]
		if ex, ok := byID[c.ID]; ok {
			ex.SourceFiles = unionSorted(ex.SourceFiles, c.SourceFiles)
			continue
		}
		cp := c
		cp.SourceFiles = append([]string(nil), c.SourceFiles...)
		byID[c.ID] = &cp
		order = append(order, c.ID)
	}
	sort.Strings(order)
	out := make([]Component, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out
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

func slug(s string) string {
	var b strings.Builder
	prev := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prev = false
		} else if !prev {
			b.WriteByte('_')
			prev = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func unionSorted(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(append([]string{}, a...), b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
