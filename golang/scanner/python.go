// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=scanner
// @awareness file_role=python_annotation_extractor
// @awareness implements=globular.awareness_graph:intent.awareness.multi_language_extraction
// @awareness enforces=globular.awareness_graph:invariant.awareness.symbol_extraction_may_be_language_specific
// @awareness relates_to=globular.awareness_graph:invariant.awareness.annotation_grammar_is_language_neutral
// @awareness risk=medium

package scanner

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
	tspython "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

var pyDeclKinds = map[string]string{
	"function_definition": "function",
	"class_definition":    "type",
}

func (s *Scanner) scanPythonFile(absPath string) (anns []Annotation, tests []DiscoveredTest, errs, warns []ValidationError) {
	relPath := s.repoRelPath(absPath)

	src, err := os.ReadFile(absPath)
	if err != nil {
		errs = append(errs, ValidationError{File: relPath, Message: "read: " + err.Error()})
		return
	}

	parser := ts.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(ts.NewLanguage(tspython.Language())); err != nil {
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

	for _, group := range collectPYCommentGroups(tree.RootNode(), src) {
		block, ok := parseAnnotationLines(group.lines)
		if !ok {
			continue
		}
		ann := Annotation{
			File:     relPath,
			Line:     group.startRow + 1,
			Keys:     block.keys,
			KeyOrder: block.order,
			Language: "python",
		}
		if decl := group.attachedDecl; decl != nil {
			sym, kind := pySymbol(decl, src)
			ann.Symbol = sym
			ann.SymbolKind = kind
		}
		e2, w2 := s.resolveAndValidate(&ann, inferredNS, relPath)
		errs = append(errs, e2...)
		warns = append(warns, w2...)
		anns = append(anns, ann)
	}
	tests = collectPYTests(tree.RootNode(), src, relPath)
	return
}

func collectPYTests(root *ts.Node, src []byte, relPath string) []DiscoveredTest {
	if !isPythonTestFile(relPath) {
		return nil
	}
	var tests []DiscoveredTest
	seen := map[string]int{}
	seenExact := map[string]bool{}

	var walk func(*ts.Node)
	walk = func(n *ts.Node) {
		if n == nil {
			return
		}
		if dt, ok := discoveredPYTest(n, src, relPath); ok {
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

func discoveredPYTest(n *ts.Node, src []byte, relPath string) (DiscoveredTest, bool) {
	if n.Kind() == "decorated_definition" {
		if inner := pyUnwrapDecorated(n); inner != nil {
			return discoveredPYTest(inner, src, relPath)
		}
	}
	if n.Kind() != "function_definition" {
		return DiscoveredTest{}, false
	}
	name := pyNamedChildText(n, "identifier", src)
	if !strings.HasPrefix(name, "test_") {
		return DiscoveredTest{}, false
	}
	symbol := name
	if parent := pyEnclosingClass(n, src); parent != "" {
		symbol = parent + "." + name
	}
	doc := ""
	if ds := pyLeadingStringDoc(n, src); ds != "" {
		doc = ds
	}
	return DiscoveredTest{
		File:     relPath,
		Symbol:   symbol,
		Language: "python",
		Line:     int(n.StartPosition().Row) + 1,
		Doc:      doc,
	}, true
}

func pySymbol(n *ts.Node, src []byte) (sym, kind string) {
	if n.Kind() == "decorated_definition" {
		if inner := pyUnwrapDecorated(n); inner != nil {
			n = inner
		}
	}
	kind = pyDeclKinds[n.Kind()]
	switch n.Kind() {
	case "class_definition":
		return pyNamedChildText(n, "identifier", src), kind
	case "function_definition":
		name := pyNamedChildText(n, "identifier", src)
		if cls := pyEnclosingClass(n, src); cls != "" {
			return cls + "." + name, "method"
		}
		return name, kind
	default:
		return "", kind
	}
}

type pyCommentGroup struct {
	lines        []string
	startRow     int
	endRow       int
	attachedDecl *ts.Node
}

func collectPYCommentGroups(root *ts.Node, src []byte) []pyCommentGroup {
	decls := map[int]*ts.Node{}
	collectPYDeclStartRows(root, decls)

	lines := strings.Split(string(src), "\n")
	var groups []pyCommentGroup
	for i := 0; i < len(lines); {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "#") {
			i++
			continue
		}
		start := i
		var block []string
		for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "#") {
			block = append(block, lines[i])
			i++
		}
		group := pyCommentGroup{
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

func pyUnwrapDecorated(n *ts.Node) *ts.Node {
	for i := uint(0); i < n.NamedChildCount(); i++ {
		c := n.NamedChild(i)
		if _, ok := pyDeclKinds[c.Kind()]; ok {
			return c
		}
	}
	return nil
}

func collectPYDeclStartRows(n *ts.Node, decls map[int]*ts.Node) {
	if n == nil {
		return
	}
	switch n.Kind() {
	case "decorated_definition":
		if inner := pyUnwrapDecorated(n); inner != nil {
			decls[int(n.StartPosition().Row)] = n
		}
	case "function_definition", "class_definition":
		decls[int(n.StartPosition().Row)] = n
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		collectPYDeclStartRows(n.NamedChild(i), decls)
	}
}

func pyNamedChildText(n *ts.Node, kind string, src []byte) string {
	for i := uint(0); i < n.NamedChildCount(); i++ {
		if c := n.NamedChild(i); c.Kind() == kind {
			return string(src[c.StartByte():c.EndByte()])
		}
	}
	return ""
}

func pyEnclosingClass(n *ts.Node, src []byte) string {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "class_definition" {
			return pyNamedChildText(p, "identifier", src)
		}
		if p.Kind() == "decorated_definition" {
			if inner := pyUnwrapDecorated(p); inner != nil && inner.Kind() == "class_definition" {
				return pyNamedChildText(inner, "identifier", src)
			}
		}
	}
	return ""
}

func pyLeadingStringDoc(n *ts.Node, src []byte) string {
	body := pyNamedChildOfKind(n, "block")
	if body == nil || body.NamedChildCount() == 0 {
		return ""
	}
	first := body.NamedChild(0)
	if first.Kind() != "expression_statement" {
		return ""
	}
	for i := uint(0); i < first.NamedChildCount(); i++ {
		c := first.NamedChild(i)
		if c.Kind() == "string" || c.Kind() == "concatenated_string" {
			return strings.TrimSpace(pyStringValue(c, src))
		}
	}
	return ""
}

func pyNamedChildOfKind(n *ts.Node, kind string) *ts.Node {
	for i := uint(0); i < n.NamedChildCount(); i++ {
		if c := n.NamedChild(i); c.Kind() == kind {
			return c
		}
	}
	return nil
}

func pyStringValue(n *ts.Node, src []byte) string {
	text := string(src[n.StartByte():n.EndByte()])
	text = strings.TrimSpace(text)
	for _, prefix := range []string{"r", "u", "b", "f", "R", "U", "B", "F"} {
		if strings.HasPrefix(text, prefix) && len(text) > 1 {
			text = text[1:]
		}
	}
	switch {
	case strings.HasPrefix(text, `"""`) && strings.HasSuffix(text, `"""`) && len(text) >= 6:
		return text[3 : len(text)-3]
	case strings.HasPrefix(text, `'''`) && strings.HasSuffix(text, `'''`) && len(text) >= 6:
		return text[3 : len(text)-3]
	case strings.HasPrefix(text, `"`) && strings.HasSuffix(text, `"`) && len(text) >= 2:
		return text[1 : len(text)-1]
	case strings.HasPrefix(text, `'`) && strings.HasSuffix(text, `'`) && len(text) >= 2:
		return text[1 : len(text)-1]
	default:
		return text
	}
}

func isPythonTestFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py")
}
