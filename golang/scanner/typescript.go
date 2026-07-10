// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=scanner
// @awareness file_role=typescript_annotation_extractor
// @awareness implements=globular.awareness_graph:intent.awareness.multi_language_extraction
// @awareness enforces=globular.awareness_graph:invariant.awareness.symbol_extraction_may_be_language_specific
// @awareness relates_to=globular.awareness_graph:invariant.awareness.annotation_grammar_is_language_neutral
// @awareness risk=medium

// typescript.go — tree-sitter based @awareness extraction for .ts/.tsx/.js/.jsx files.
//
// Tier 1 of the multi-language model: this file owns ONLY the
// language-specific symbol attachment (which declaration a comment block
// belongs to). The annotation grammar itself is parsed by the shared
// parseAnnotationLines, and everything downstream (validation, ID minting,
// YAML emission) is the same code the Go scanner uses.
//
// Attachment rule (mirrors the Go scanner): a block of consecutive comment
// lines containing @awareness entries attaches to the declaration that
// starts on the line immediately after the block ends; a block not adjacent
// to a declaration is a file-level annotation.
package scanner

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
	tstypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// tsDeclKinds maps tree-sitter TypeScript node kinds to the shared symbol
// kind vocabulary used by the Go scanner ("function", "method", "type",
// "var", "const"). Per invariant awareness.file_role_vocabulary_is_shared,
// no TypeScript-only kind values are introduced.
var tsDeclKinds = map[string]string{
	"function_declaration":           "function",
	"generator_function_declaration": "function",
	"class_declaration":              "type",
	"abstract_class_declaration":     "type",
	"interface_declaration":          "type",
	"type_alias_declaration":         "type",
	"enum_declaration":               "type",
	"method_definition":              "method",
	"lexical_declaration":            "const", // refined to var/function below
	"variable_statement":             "var",
}

// scanTypeScriptFile parses one TS/JS source file with tree-sitter and extracts
// all @awareness annotation blocks plus conservative discovered test evidence
// from named declarations inside test files.
func (s *Scanner) scanTypeScriptFile(absPath string) (anns []Annotation, tests []DiscoveredTest, errs, warns []ValidationError) {
	relPath := s.repoRelPath(absPath)

	src, err := os.ReadFile(absPath)
	if err != nil {
		errs = append(errs, ValidationError{File: relPath, Message: "read: " + err.Error()})
		return
	}

	parser := ts.NewParser()
	defer parser.Close()
	lang := ts.NewLanguage(tstypescript.LanguageTypescript())
	if isTSXLike(absPath) {
		lang = ts.NewLanguage(tstypescript.LanguageTSX())
	}
	if err := parser.SetLanguage(lang); err != nil {
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

	for _, group := range collectTSCommentGroups(tree.RootNode(), src) {
		block, ok := parseAnnotationLines(group.lines)
		if !ok {
			continue
		}
		ann := Annotation{
			File:     relPath,
			Line:     group.startRow + 1, // tree-sitter rows are 0-based
			Keys:     block.keys,
			KeyOrder: block.order,
			Language: scriptLanguageForPath(relPath),
		}
		if decl := group.attachedDecl; decl != nil {
			sym, kind := tsSymbol(decl, src)
			ann.Symbol = sym
			ann.SymbolKind = kind
		}
		e2, w2 := s.resolveAndValidate(&ann, inferredNS, relPath)
		errs = append(errs, e2...)
		warns = append(warns, w2...)
		anns = append(anns, ann)
	}
	tests = collectTSTests(tree.RootNode(), src, relPath)
	return
}

func collectTSTests(root *ts.Node, src []byte, relPath string) []DiscoveredTest {
	lang := scriptLanguageForPath(relPath)
	if !isScriptTestFile(relPath) || lang == "" {
		return nil
	}
	var tests []DiscoveredTest
	seen := map[string]int{}
	seenExact := map[string]bool{}
	text := func(n *ts.Node) string { return string(src[n.StartByte():n.EndByte()]) }
	var walk func(*ts.Node)
	walk = func(n *ts.Node) {
		if n == nil {
			return
		}
		if dt, ok := discoveredTSNamedTest(n, text, relPath, lang); ok {
			exact := dt.Symbol + "\x00" + strconv.Itoa(dt.Line)
			if seenExact[exact] {
				goto recurse
			}
			seenExact[exact] = true
			dt.Symbol = dedupeDiscoveredTestSymbol(dt.Symbol, dt.Line, seen)
			tests = append(tests, dt)
		} else if dt, ok := discoveredTSCallTest(n, src, relPath, lang); ok {
			exact := dt.Symbol + "\x00" + strconv.Itoa(dt.Line)
			if seenExact[exact] {
				goto recurse
			}
			seenExact[exact] = true
			dt.Symbol = dedupeDiscoveredTestSymbol(dt.Symbol, dt.Line, seen)
			tests = append(tests, dt)
		}
	recurse:
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(root)
	return tests
}

func discoveredTSNamedTest(n *ts.Node, text func(*ts.Node) string, relPath, language string) (DiscoveredTest, bool) {
	switch n.Kind() {
	case "export_statement":
		if inner := tsUnwrapExport(n); inner != nil {
			return discoveredTSNamedTest(inner, text, relPath, language)
		}
	case "function_declaration", "generator_function_declaration":
		name := tsNamedDeclIdentifier(n, text)
		if strings.HasPrefix(name, "Test") {
			return DiscoveredTest{
				File:     relPath,
				Symbol:   name,
				Language: language,
				Line:     int(n.StartPosition().Row) + 1,
			}, true
		}
	case "lexical_declaration", "variable_statement":
		for i := uint(0); i < n.NamedChildCount(); i++ {
			child := n.NamedChild(i)
			if child.Kind() != "variable_declarator" {
				continue
			}
			name := ""
			isFn := false
			for j := uint(0); j < child.NamedChildCount(); j++ {
				cc := child.NamedChild(j)
				switch cc.Kind() {
				case "identifier":
					if name == "" {
						name = text(cc)
					}
				case "arrow_function", "function_expression", "generator_function":
					isFn = true
				}
			}
			if isFn && strings.HasPrefix(name, "Test") {
				return DiscoveredTest{
					File:     relPath,
					Symbol:   name,
					Language: language,
					Line:     int(child.StartPosition().Row) + 1,
				}, true
			}
		}
	}
	return DiscoveredTest{}, false
}

func discoveredTSCallTest(n *ts.Node, src []byte, relPath, language string) (DiscoveredTest, bool) {
	if n.Kind() != "call_expression" {
		return DiscoveredTest{}, false
	}
	if !isTSTestCall(n, src) {
		return DiscoveredTest{}, false
	}
	args := tsNamedChildOfKind(n, "arguments")
	if args == nil {
		return DiscoveredTest{}, false
	}
	title, ok := tsFirstStringArgument(args, src)
	if !ok {
		return DiscoveredTest{}, false
	}
	symbol := titleTestSymbol(title)
	if symbol == "" {
		return DiscoveredTest{}, false
	}
	return DiscoveredTest{
		File:     relPath,
		Symbol:   symbol,
		Language: language,
		Line:     int(n.StartPosition().Row) + 1,
		Doc:      "title: " + strings.TrimSpace(title),
	}, true
}

func isTSTestCall(n *ts.Node, src []byte) bool {
	if n.NamedChildCount() == 0 {
		return false
	}
	callee := n.NamedChild(0)
	switch callee.Kind() {
	case "identifier":
		name := nodeText(callee, src)
		return name == "it" || name == "test"
	case "member_expression":
		for i := uint(0); i < callee.NamedChildCount(); i++ {
			if c := callee.NamedChild(i); c.Kind() == "identifier" {
				name := nodeText(c, src)
				return name == "it" || name == "test"
			}
		}
	}
	return false
}

func tsFirstStringArgument(args *ts.Node, src []byte) (string, bool) {
	for i := uint(0); i < args.NamedChildCount(); i++ {
		a := args.NamedChild(i)
		switch a.Kind() {
		case "string":
			return tsStringValue(a, src), true
		case "template_string":
			if v, ok := tsTemplateStringValue(a, src); ok {
				return v, true
			}
		}
	}
	return "", false
}

func tsTemplateStringValue(n *ts.Node, src []byte) (string, bool) {
	var b strings.Builder
	for i := uint(0); i < n.NamedChildCount(); i++ {
		c := n.NamedChild(i)
		switch c.Kind() {
		case "string_fragment":
			b.WriteString(nodeText(c, src))
		case "template_substitution":
			return "", false
		}
	}
	return b.String(), true
}

func titleTestSymbol(title string) string {
	slug := slugifyTestTitle(title)
	if slug == "" {
		return ""
	}
	return "SpecTitle_" + slug
}

func slugifyTestTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range title {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	slug := strings.Trim(b.String(), "_")
	if slug == "" {
		return ""
	}
	return slug
}

func dedupeDiscoveredTestSymbol(symbol string, line int, seen map[string]int) string {
	if seen[symbol] == 0 {
		seen[symbol] = 1
		return symbol
	}
	seen[symbol]++
	return symbol + "_line" + strconv.Itoa(line)
}

func tsNamedDeclIdentifier(n *ts.Node, text func(*ts.Node) string) string {
	for i := uint(0); i < n.NamedChildCount(); i++ {
		c := n.NamedChild(i)
		if c.Kind() == "identifier" || c.Kind() == "type_identifier" {
			return text(c)
		}
	}
	return ""
}

func isScriptTestFile(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, ".test.ts") ||
		strings.HasSuffix(base, ".spec.ts") ||
		strings.HasSuffix(base, ".test.tsx") ||
		strings.HasSuffix(base, ".spec.tsx") ||
		strings.HasSuffix(base, ".test.js") ||
		strings.HasSuffix(base, ".spec.js") ||
		strings.HasSuffix(base, ".test.jsx") ||
		strings.HasSuffix(base, ".spec.jsx") ||
		strings.HasSuffix(base, ".test.mjs") ||
		strings.HasSuffix(base, ".spec.mjs") ||
		strings.HasSuffix(base, ".test.cjs") ||
		strings.HasSuffix(base, ".spec.cjs")
}

func scriptLanguageForPath(path string) string {
	switch {
	case strings.HasSuffix(path, ".ts"), strings.HasSuffix(path, ".tsx"):
		return "typescript"
	case strings.HasSuffix(path, ".js"), strings.HasSuffix(path, ".jsx"),
		strings.HasSuffix(path, ".mjs"), strings.HasSuffix(path, ".cjs"):
		return "javascript"
	default:
		return ""
	}
}

func isTSXLike(path string) bool {
	return strings.HasSuffix(path, ".tsx") || strings.HasSuffix(path, ".jsx")
}

func tsNamedChildOfKind(n *ts.Node, kind string) *ts.Node {
	for i := uint(0); i < n.NamedChildCount(); i++ {
		if c := n.NamedChild(i); c.Kind() == kind {
			return c
		}
	}
	return nil
}

func tsStringValue(n *ts.Node, src []byte) string {
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

func nodeText(n *ts.Node, src []byte) string { return string(src[n.StartByte():n.EndByte()]) }

// tsCommentGroup is a run of consecutive comment siblings, possibly attached
// to the declaration that starts on the line after the run ends.
type tsCommentGroup struct {
	lines        []string
	startRow     int
	endRow       int
	attachedDecl *ts.Node // nil → file-level annotation
}

// collectTSCommentGroups walks the tree and groups consecutive comment
// siblings, recording the adjacent following declaration when there is one.
// Recursion covers nested scopes (class bodies for method annotations).
func collectTSCommentGroups(root *ts.Node, src []byte) []tsCommentGroup {
	var groups []tsCommentGroup

	var walk func(parent *ts.Node)
	walk = func(parent *ts.Node) {
		var current *tsCommentGroup
		flush := func(next *ts.Node) {
			if current == nil {
				return
			}
			// Attach when the next named sibling is a declaration starting on
			// the line right after the comment block ends.
			if next != nil {
				if _, isDecl := tsDeclKinds[next.Kind()]; !isDecl && next.Kind() == "export_statement" {
					isDecl = tsUnwrapExport(next) != nil
					if isDecl && int(next.StartPosition().Row) == current.endRow+1 {
						current.attachedDecl = next
					}
				} else if isDecl && int(next.StartPosition().Row) == current.endRow+1 {
					current.attachedDecl = next
				}
			}
			groups = append(groups, *current)
			current = nil
		}

		count := parent.NamedChildCount()
		for i := uint(0); i < count; i++ {
			child := parent.NamedChild(i)
			if child.Kind() == "comment" {
				row := int(child.StartPosition().Row)
				text := string(src[child.StartByte():child.EndByte()])
				if current != nil && row == current.endRow+1 {
					current.lines = append(current.lines, text)
					current.endRow = int(child.EndPosition().Row)
				} else {
					flush(nil)
					current = &tsCommentGroup{
						lines:    []string{text},
						startRow: row,
						endRow:   int(child.EndPosition().Row),
					}
				}
				continue
			}
			flush(child)
			// Recurse into containers that hold annotatable members.
			switch child.Kind() {
			case "class_declaration", "abstract_class_declaration", "class_body",
				"export_statement", "program", "interface_declaration", "object":
				walk(child)
			}
		}
		flush(nil)
	}
	walk(root)
	return groups
}

// tsUnwrapExport returns the inner declaration of an export_statement, or
// nil when the export does not wrap a known declaration kind.
func tsUnwrapExport(n *ts.Node) *ts.Node {
	for i := uint(0); i < n.NamedChildCount(); i++ {
		c := n.NamedChild(i)
		if _, ok := tsDeclKinds[c.Kind()]; ok {
			return c
		}
	}
	return nil
}

// tsSymbol returns the symbol name and shared-vocabulary kind for a
// TypeScript declaration node. Methods are named ClassName.method to mirror
// the Go scanner's Type.Method convention.
func tsSymbol(n *ts.Node, src []byte) (sym, kind string) {
	if n.Kind() == "export_statement" {
		if inner := tsUnwrapExport(n); inner != nil {
			n = inner
		}
	}
	kind = tsDeclKinds[n.Kind()]

	text := func(c *ts.Node) string { return string(src[c.StartByte():c.EndByte()]) }

	switch n.Kind() {
	case "method_definition":
		name := ""
		for i := uint(0); i < n.NamedChildCount(); i++ {
			if c := n.NamedChild(i); c.Kind() == "property_identifier" {
				name = text(c)
				break
			}
		}
		// Qualify with the enclosing class name when there is one.
		for p := n.Parent(); p != nil; p = p.Parent() {
			if p.Kind() == "class_declaration" || p.Kind() == "abstract_class_declaration" {
				for i := uint(0); i < p.NamedChildCount(); i++ {
					if c := p.NamedChild(i); c.Kind() == "type_identifier" {
						return text(c) + "." + name, kind
					}
				}
			}
		}
		return name, kind

	case "lexical_declaration", "variable_statement":
		// First declarator: name + refine kind when the value is a function.
		for i := uint(0); i < n.NamedChildCount(); i++ {
			c := n.NamedChild(i)
			if c.Kind() != "variable_declarator" {
				continue
			}
			name := ""
			for j := uint(0); j < c.NamedChildCount(); j++ {
				cc := c.NamedChild(j)
				switch cc.Kind() {
				case "identifier":
					if name == "" {
						name = text(cc)
					}
				case "arrow_function", "function_expression", "generator_function":
					kind = "function"
				}
			}
			return name, kind
		}
		return "", kind

	default:
		// Named declarations: identifier or type_identifier child.
		for i := uint(0); i < n.NamedChildCount(); i++ {
			c := n.NamedChild(i)
			if c.Kind() == "identifier" || c.Kind() == "type_identifier" {
				return text(c), kind
			}
		}
		return "", kind
	}
}
