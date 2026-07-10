// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=scanner
// @awareness file_role=rust_annotation_extractor
// @awareness implements=globular.awareness_graph:intent.awareness.multi_language_extraction
// @awareness enforces=globular.awareness_graph:invariant.awareness.symbol_extraction_may_be_language_specific
// @awareness relates_to=globular.awareness_graph:invariant.awareness.annotation_grammar_is_language_neutral
// @awareness risk=medium

package scanner

import (
	"os"
	"strconv"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
	tsrust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
)

var rustDeclKinds = map[string]string{
	"function_item": "function",
	"struct_item":   "type",
	"enum_item":     "type",
	"trait_item":    "type",
	"type_item":     "type",
	"const_item":    "const",
	"static_item":   "var",
}

func (s *Scanner) scanRustFile(absPath string) (anns []Annotation, tests []DiscoveredTest, errs, warns []ValidationError) {
	relPath := s.repoRelPath(absPath)

	src, err := os.ReadFile(absPath)
	if err != nil {
		errs = append(errs, ValidationError{File: relPath, Message: "read: " + err.Error()})
		return
	}

	parser := ts.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(ts.NewLanguage(tsrust.Language())); err != nil {
		errs = append(errs, ValidationError{File: relPath, Message: "tree-sitter: " + err.Error()})
		return
	}
	tree := parser.Parse(src, nil)
	if tree == nil {
		errs = append(errs, ValidationError{File: relPath, Message: "tree-sitter: parse returned no tree"})
		return
	}
	defer tree.Close()

	inferredNS := s.Registry.NamespaceForPath(relPath)
	for _, group := range collectRustCommentGroups(tree.RootNode(), src) {
		block, ok := parseAnnotationLines(group.lines)
		if !ok {
			continue
		}
		ann := Annotation{
			File:     relPath,
			Line:     group.startRow + 1,
			Keys:     block.keys,
			KeyOrder: block.order,
			Language: "rust",
		}
		if decl := group.attachedDecl; decl != nil {
			sym, kind := rustSymbol(decl, src)
			ann.Symbol = sym
			ann.SymbolKind = kind
		}
		e2, w2 := s.resolveAndValidate(&ann, inferredNS, relPath)
		errs = append(errs, e2...)
		warns = append(warns, w2...)
		anns = append(anns, ann)
	}
	tests = collectRustTests(tree.RootNode(), src, relPath)
	return
}

type rustCommentGroup struct {
	lines        []string
	startRow     int
	endRow       int
	attachedDecl *ts.Node
}

func collectRustCommentGroups(root *ts.Node, src []byte) []rustCommentGroup {
	decls := map[int]*ts.Node{}
	collectRustDeclStartRows(root, decls)

	lines := strings.Split(string(src), "\n")
	var groups []rustCommentGroup
	for i := 0; i < len(lines); {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "//") {
			i++
			continue
		}
		start := i
		var block []string
		for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "//") {
			block = append(block, lines[i])
			i++
		}
		group := rustCommentGroup{
			lines:    block,
			startRow: start,
			endRow:   i - 1,
		}
		if decl := decls[i]; decl != nil {
			group.attachedDecl = decl
		}
		groups = append(groups, group)
	}
	return groups
}

func collectRustDeclStartRows(n *ts.Node, decls map[int]*ts.Node) {
	if n == nil {
		return
	}
	if _, ok := rustDeclKinds[n.Kind()]; ok {
		decls[int(n.StartPosition().Row)] = n
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		collectRustDeclStartRows(n.NamedChild(i), decls)
	}
}

func rustSymbol(n *ts.Node, src []byte) (sym, kind string) {
	kind = rustDeclKinds[n.Kind()]
	switch n.Kind() {
	case "function_item":
		name := rustNamedChildText(n, "identifier", src)
		if owner := rustEnclosingImplType(n, src); owner != "" {
			return owner + "." + name, "method"
		}
		return name, kind
	case "struct_item", "enum_item", "trait_item", "type_item":
		return rustNamedChildText(n, "type_identifier", src), kind
	case "const_item", "static_item":
		return rustNamedChildText(n, "identifier", src), kind
	default:
		return "", kind
	}
}

func collectRustTests(root *ts.Node, src []byte, relPath string) []DiscoveredTest {
	var tests []DiscoveredTest
	seen := map[string]int{}
	seenExact := map[string]bool{}
	var walk func(*ts.Node)
	walk = func(n *ts.Node) {
		if n == nil {
			return
		}
		if dt, ok := discoveredRustTest(n, src, relPath); ok {
			exact := dt.Symbol + "\x00" + strconv.Itoa(dt.Line)
			if !seenExact[exact] {
				seenExact[exact] = true
				dt.Symbol = dedupeDiscoveredTestSymbol(dt.Symbol, dt.Line, seen)
				tests = append(tests, dt)
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(root)
	return tests
}

func discoveredRustTest(n *ts.Node, src []byte, relPath string) (DiscoveredTest, bool) {
	if n.Kind() != "function_item" || !rustHasTestAttribute(n, src) {
		return DiscoveredTest{}, false
	}
	name := rustNamedChildText(n, "identifier", src)
	if name == "" {
		return DiscoveredTest{}, false
	}
	symbol := name
	if owner := rustEnclosingImplType(n, src); owner != "" {
		symbol = owner + "." + name
	}
	return DiscoveredTest{
		File:     relPath,
		Symbol:   symbol,
		Language: "rust",
		Line:     int(n.StartPosition().Row) + 1,
	}, true
}

func rustHasTestAttribute(n *ts.Node, src []byte) bool {
	parent := n.Parent()
	if parent == nil {
		return false
	}
	for i := uint(0); i < parent.NamedChildCount(); i++ {
		c := parent.NamedChild(i)
		if c.Kind() != "attribute_item" {
			continue
		}
		text := string(src[c.StartByte():c.EndByte()])
		if strings.Contains(text, "#[test]") {
			return true
		}
	}
	return false
}

func rustNamedChildText(n *ts.Node, kind string, src []byte) string {
	for i := uint(0); i < n.NamedChildCount(); i++ {
		if c := n.NamedChild(i); c.Kind() == kind {
			return string(src[c.StartByte():c.EndByte()])
		}
	}
	return ""
}

func rustEnclosingImplType(n *ts.Node, src []byte) string {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() != "impl_item" {
			continue
		}
		for i := uint(0); i < p.NamedChildCount(); i++ {
			c := p.NamedChild(i)
			if c.Kind() == "type_identifier" || c.Kind() == "scoped_type_identifier" {
				return string(src[c.StartByte():c.EndByte()])
			}
		}
	}
	return ""
}
