// SPDX-License-Identifier: AGPL-3.0-only

// Package scanner parses @awareness annotations from Go source files and
// validates them against a namespace registry.
//
// Annotation format (in Go line comments):
//
//	// @awareness namespace=globular.examples.echo_service
//	// @awareness component=server.handler
//	// @awareness implements=globular.examples.echo_service:intent.foo
//	// @awareness enforces=globular.examples.echo_service:invariant.bar
//	func (srv *server) Echo(...) { ... }
//
// Rules:
//   - A block of consecutive @awareness lines immediately above a func/type/var
//     is a symbol-level annotation.
//   - A block immediately above the package declaration is a file-level annotation.
//   - namespace= must be a known namespace ID from the registry.
//   - All ID references (implements=, enforces=, etc.) must be fully qualified:
//     <namespace>:<class>.<slug>
//   - In strict mode, any violation is a hard error; in normal mode it is a warning.
package scanner

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// AllowedKeys is the set of valid @awareness annotation keys.
var AllowedKeys = map[string]bool{
	"namespace":          true,
	"component":          true,
	"symbol":             true,
	"file_role":          true,
	"implements":         true,
	"enforces":           true,
	"protects":           true,
	"relates_to":         true,
	"partially_violates": true,
	"calls":              true,
	"emits":              true,
	"reads":              true,
	"writes":             true,
	"tested_by":          true,
	"forbidden_fix":      true,
	"failure_mode":       true,
	"risk":               true,
	"owner":              true,
}

// AllowedRisk is the set of valid risk values.
var AllowedRisk = map[string]bool{
	"low": true, "medium": true, "high": true, "critical": true,
}

// qualifiedIDRe matches a fully-qualified awareness ID: namespace:class.slug
// namespace may contain dots; class and slug are lowercase with dots/underscores.
var qualifiedIDRe = regexp.MustCompile(`^[a-z][a-z0-9._]+:[a-z][a-z0-9_]+\.[a-z0-9_.]+$`)

// refKeys are annotation keys whose values must be fully qualified IDs
// (not free-form strings like component= or owner=).
var refKeys = map[string]bool{
	"implements":         true,
	"enforces":           true,
	"protects":           true,
	"relates_to":         true,
	"partially_violates": true,
	"forbidden_fix":      true,
}

// Annotation is one parsed @awareness block attached to a file or symbol.
type Annotation struct {
	File       string              // repo-relative file path
	Symbol     string              // empty for file-level; "TypeName.Method" or "FuncName" for symbols
	SymbolKind string              // "function", "method", "type", "var", "const", "" for file
	Line       int                 // line of the first @awareness comment
	Keys       map[string][]string // key -> ordered list of values (keys may repeat)
	KeyOrder   []string            // insertion order of first appearance of each key
	Language   string              // "go" | "typescript" | "javascript" — set by the per-language scanFile

	// resolved after namespace inference
	Namespace string
	Component string
}

// DiscoveredTest is one concrete test implementation discovered structurally
// from source code. Unlike Annotation, it does not assert contract semantics;
// it is code evidence that a concrete test implementation exists at a path.
type DiscoveredTest struct {
	File     string
	Symbol   string
	Package  string
	Language string
	Line     int
	Doc      string
}

// ValidationError records one problem found in an annotation.
type ValidationError struct {
	File    string
	Symbol  string
	Line    int
	Message string
}

func (e ValidationError) Error() string {
	if e.Symbol != "" {
		return fmt.Sprintf("%s:%d (%s): %s", e.File, e.Line, e.Symbol, e.Message)
	}
	return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Message)
}

// ScanResult is the output of scanning one source tree.
type ScanResult struct {
	Annotations      []Annotation
	DiscoveredTests  []DiscoveredTest
	Errors           []ValidationError
	Warnings         []ValidationError
	ScannedFiles     int
	AnnotatedSymbols int
}

// HasErrors reports whether any hard validation errors were found.
func (r *ScanResult) HasErrors() bool { return len(r.Errors) > 0 }

// Scanner walks a source tree and extracts @awareness annotations.
type Scanner struct {
	Registry *Registry
	RepoRoot string // absolute path to repo root (for computing relative paths)
	Strict   bool   // if true, warnings become errors
}

// Scan walks root recursively and returns all parsed annotations.
// root must be an absolute path. Files under .git, vendor, node_modules,
// and paths ending in _generated.go or .pb.go are skipped. Nested git
// repositories (directories with their own .git) are also skipped so that
// co-located checkouts (e.g. a sibling repo placed inside the workspace)
// are never scanned.
func (s *Scanner) Scan(root string) (*ScanResult, error) {
	result := &ScanResult{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" ||
				name == "dist" || name == "bin" {
				return filepath.SkipDir
			}
			// Skip nested git repos (co-located checkouts inside the workspace).
			if path != root {
				if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		base := filepath.Base(path)
		switch {
		case strings.HasSuffix(path, ".go"):
			// Skip generated Go files.
			if strings.HasSuffix(base, ".pb.go") ||
				strings.HasSuffix(base, "_generated.go") ||
				(strings.HasPrefix(base, "zz_") && !strings.HasSuffix(base, "_test.go")) {
				return nil
			}
			result.ScannedFiles++
			anns, tests, errs, warns := s.scanFile(path)
			result.Annotations = append(result.Annotations, anns...)
			result.DiscoveredTests = append(result.DiscoveredTests, tests...)
			result.Errors = append(result.Errors, errs...)
			result.Warnings = append(result.Warnings, warns...)
		case strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".tsx") ||
			strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".jsx") ||
			strings.HasSuffix(path, ".mjs") || strings.HasSuffix(path, ".cjs"):
			// Skip declaration files and generated protobuf/grpc-web stubs.
			if strings.HasSuffix(base, ".d.ts") ||
				strings.Contains(base, "_pb.") ||
				strings.HasSuffix(base, "_grpc_web_pb.ts") ||
				strings.HasSuffix(base, "_grpc_web_pb.js") {
				return nil
			}
			result.ScannedFiles++
			anns, tests, errs, warns := s.scanTypeScriptFile(path)
			result.Annotations = append(result.Annotations, anns...)
			result.DiscoveredTests = append(result.DiscoveredTests, tests...)
			result.Errors = append(result.Errors, errs...)
			result.Warnings = append(result.Warnings, warns...)
		case strings.HasSuffix(path, ".py"):
			if strings.HasSuffix(base, ".pyi") ||
				strings.HasSuffix(base, "_pb2.py") ||
				strings.HasSuffix(base, "_pb2_grpc.py") {
				return nil
			}
			result.ScannedFiles++
			anns, tests, errs, warns := s.scanPythonFile(path)
			result.Annotations = append(result.Annotations, anns...)
			result.DiscoveredTests = append(result.DiscoveredTests, tests...)
			result.Errors = append(result.Errors, errs...)
			result.Warnings = append(result.Warnings, warns...)
		case strings.HasSuffix(path, ".rs"):
			result.ScannedFiles++
			anns, tests, errs, warns := s.scanRustFile(path)
			result.Annotations = append(result.Annotations, anns...)
			result.DiscoveredTests = append(result.DiscoveredTests, tests...)
			result.Errors = append(result.Errors, errs...)
			result.Warnings = append(result.Warnings, warns...)
		}
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("walk %s: %w", root, err)
	}
	for _, a := range result.Annotations {
		if a.Symbol != "" {
			result.AnnotatedSymbols++
		}
	}
	if s.Strict {
		result.Errors = append(result.Errors, result.Warnings...)
		result.Warnings = nil
	}
	return result, nil
}

// scanFile parses one Go file and extracts all @awareness annotation blocks.
func (s *Scanner) scanFile(absPath string) (anns []Annotation, tests []DiscoveredTest, errs, warns []ValidationError) {
	defer func() {
		for i := range anns {
			anns[i].Language = "go"
		}
	}()
	relPath := s.repoRelPath(absPath)

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, absPath, nil, parser.ParseComments)
	if err != nil {
		errs = append(errs, ValidationError{File: relPath, Message: "parse: " + err.Error()})
		return
	}

	// Infer namespace from path via registry.
	inferredNS := s.Registry.NamespaceForPath(relPath)

	// file-level annotation: the package doc comment.
	if f.Doc != nil {
		if block, ok := parseAnnotationBlock(f.Doc); ok {
			ann := Annotation{File: relPath, Line: fset.Position(f.Doc.Pos()).Line, Keys: block.keys, KeyOrder: block.order}
			e2, w2 := s.resolveAndValidate(&ann, inferredNS, relPath)
			errs = append(errs, e2...)
			warns = append(warns, w2...)
			anns = append(anns, ann)
		}
	}

	// Walk all top-level declarations for symbol-level annotations.
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if dt, ok := discoveredGoTest(d, relPath, f.Name.Name, fset); ok {
				tests = append(tests, dt)
			}
			if d.Doc == nil {
				continue
			}
			block, ok := parseAnnotationBlock(d.Doc)
			if !ok {
				continue
			}
			sym, kind := funcSymbol(d)
			ann := Annotation{
				File:       relPath,
				Symbol:     sym,
				SymbolKind: kind,
				Line:       fset.Position(d.Doc.Pos()).Line,
				Keys:       block.keys,
				KeyOrder:   block.order,
			}
			e2, w2 := s.resolveAndValidate(&ann, inferredNS, relPath)
			errs = append(errs, e2...)
			warns = append(warns, w2...)
			anns = append(anns, ann)

		case *ast.GenDecl:
			// type, var, const blocks
			if d.Doc != nil {
				if block, ok := parseAnnotationBlock(d.Doc); ok {
					sym, kind := genDeclSymbol(d)
					ann := Annotation{
						File:       relPath,
						Symbol:     sym,
						SymbolKind: kind,
						Line:       fset.Position(d.Doc.Pos()).Line,
						Keys:       block.keys,
						KeyOrder:   block.order,
					}
					e2, w2 := s.resolveAndValidate(&ann, inferredNS, relPath)
					errs = append(errs, e2...)
					warns = append(warns, w2...)
					anns = append(anns, ann)
				}
			}
			// Individual specs within a block declaration.
			for _, spec := range d.Specs {
				switch sp := spec.(type) {
				case *ast.TypeSpec:
					if sp.Doc != nil {
						if block, ok := parseAnnotationBlock(sp.Doc); ok {
							ann := Annotation{
								File:       relPath,
								Symbol:     sp.Name.Name,
								SymbolKind: "type",
								Line:       fset.Position(sp.Doc.Pos()).Line,
								Keys:       block.keys,
								KeyOrder:   block.order,
							}
							e2, w2 := s.resolveAndValidate(&ann, inferredNS, relPath)
							errs = append(errs, e2...)
							warns = append(warns, w2...)
							anns = append(anns, ann)
						}
					}
				case *ast.ValueSpec:
					if sp.Doc != nil {
						if block, ok := parseAnnotationBlock(sp.Doc); ok {
							name := ""
							if len(sp.Names) > 0 {
								name = sp.Names[0].Name
							}
							ann := Annotation{
								File:       relPath,
								Symbol:     name,
								SymbolKind: declKind(d.Tok.String()),
								Line:       fset.Position(sp.Doc.Pos()).Line,
								Keys:       block.keys,
								KeyOrder:   block.order,
							}
							e2, w2 := s.resolveAndValidate(&ann, inferredNS, relPath)
							errs = append(errs, e2...)
							warns = append(warns, w2...)
							anns = append(anns, ann)
						}
					}
				}
			}
		}
	}
	return
}

func discoveredGoTest(d *ast.FuncDecl, relPath, pkg string, fset *token.FileSet) (DiscoveredTest, bool) {
	if !strings.HasSuffix(relPath, "_test.go") || d.Recv != nil || d.Name == nil {
		return DiscoveredTest{}, false
	}
	name := d.Name.Name
	if !strings.HasPrefix(name, "Test") {
		return DiscoveredTest{}, false
	}
	if d.Type == nil || d.Type.Params == nil || len(d.Type.Params.List) != 1 {
		return DiscoveredTest{}, false
	}
	if len(d.Type.Params.List[0].Names) == 0 {
		return DiscoveredTest{}, false
	}
	if !isTestingTParam(d.Type.Params.List[0].Type) {
		return DiscoveredTest{}, false
	}
	doc := ""
	if d.Doc != nil {
		doc = strings.TrimSpace(d.Doc.Text())
	}
	return DiscoveredTest{
		File:     relPath,
		Symbol:   name,
		Package:  pkg,
		Language: "go",
		Line:     fset.Position(d.Pos()).Line,
		Doc:      doc,
	}, true
}

func isTestingTParam(expr ast.Expr) bool {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return pkg.Name == "testing" && sel.Sel != nil && sel.Sel.Name == "T"
}

// resolveAndValidate fills Namespace/Component on ann and validates all keys.
func (s *Scanner) resolveAndValidate(ann *Annotation, inferredNS, file string) (errs, warns []ValidationError) {
	warn := func(msg string) {
		warns = append(warns, ValidationError{File: file, Symbol: ann.Symbol, Line: ann.Line, Message: msg})
	}
	fail := func(msg string) {
		errs = append(errs, ValidationError{File: file, Symbol: ann.Symbol, Line: ann.Line, Message: msg})
	}

	// Resolve namespace.
	if ns := ann.Keys["namespace"]; len(ns) > 0 {
		ann.Namespace = ns[0]
	} else {
		ann.Namespace = inferredNS
	}
	if ann.Namespace == "" {
		fail("missing namespace: set namespace= or add path to namespaces.yaml owns list")
	} else if !s.Registry.Has(ann.Namespace) {
		fail(fmt.Sprintf("unknown namespace %q — add it to namespaces.yaml", ann.Namespace))
	}

	// Resolve component.
	if c := ann.Keys["component"]; len(c) > 0 {
		ann.Component = c[0]
	}

	// Validate all keys.
	for key, vals := range ann.Keys {
		if !AllowedKeys[key] {
			fail(fmt.Sprintf("unsupported annotation key %q", key))
			continue
		}
		if key == "risk" {
			for _, v := range vals {
				if !AllowedRisk[v] {
					fail(fmt.Sprintf("invalid risk value %q (allowed: low|medium|high|critical)", v))
				}
			}
		}
		if refKeys[key] {
			for _, v := range vals {
				if !qualifiedIDRe.MatchString(v) {
					fail(fmt.Sprintf("key %q value %q is not a fully-qualified ID (want <namespace>:<class>.<slug>)", key, v))
				}
			}
		}
	}

	// Warn on namespace mismatch (annotation says one thing, path implies another).
	if ann.Namespace != "" && inferredNS != "" && ann.Namespace != inferredNS {
		warn(fmt.Sprintf("explicit namespace %q differs from path-inferred %q", ann.Namespace, inferredNS))
	}
	return
}

// repoRelPath converts an absolute file path to a repo-relative path.
func (s *Scanner) repoRelPath(absPath string) string {
	rel, err := filepath.Rel(s.RepoRoot, absPath)
	if err != nil {
		return absPath
	}
	return rel
}

// annotationBlock is the raw parse result of a @awareness comment group.
type annotationBlock struct {
	keys  map[string][]string
	order []string // first-appearance order
}

// parseAnnotationBlock extracts @awareness key=value lines from a Go comment
// group by delegating to the language-neutral line parser.
func parseAnnotationBlock(cg *ast.CommentGroup) (annotationBlock, bool) {
	lines := make([]string, 0, len(cg.List))
	for _, c := range cg.List {
		lines = append(lines, c.Text)
	}
	return parseAnnotationLines(lines)
}

// parseAnnotationLines extracts @awareness key=value entries from raw comment
// lines. This is the single grammar shared by every language scanner
// (invariant awareness.annotation_grammar_is_language_neutral): only the
// comment SYNTAX stripped here varies; the @awareness key=value grammar does
// not. Handles // line comments and /* */ block comments (including leading
// `*` continuation lines). Returns (block, true) if at least one @awareness
// line was found.
func parseAnnotationLines(lines []string) (annotationBlock, bool) {
	var b annotationBlock
	b.keys = make(map[string][]string)
	seen := make(map[string]bool)

	for _, raw := range lines {
		// A single input line may itself contain newlines (block comments).
		for _, line := range strings.Split(raw, "\n") {
			text := strings.TrimSpace(line)
			text = strings.TrimPrefix(text, "//")
			text = strings.TrimPrefix(text, "#")
			text = strings.TrimPrefix(text, "/*")
			text = strings.TrimSuffix(text, "*/")
			text = strings.TrimSpace(text)
			text = strings.TrimPrefix(text, "*") // block-comment continuation
			text = strings.TrimSpace(text)
			if !strings.HasPrefix(text, "@awareness ") {
				continue
			}
			kv := strings.TrimSpace(strings.TrimPrefix(text, "@awareness "))
			idx := strings.IndexByte(kv, '=')
			if idx < 0 {
				continue // malformed; caller validates later
			}
			key := strings.TrimSpace(kv[:idx])
			val := strings.TrimSpace(kv[idx+1:])
			b.keys[key] = append(b.keys[key], val)
			if !seen[key] {
				seen[key] = true
				b.order = append(b.order, key)
			}
		}
	}
	return b, len(b.keys) > 0
}

// funcSymbol returns the annotation symbol name and kind for a FuncDecl.
func funcSymbol(d *ast.FuncDecl) (sym, kind string) {
	name := d.Name.Name
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return name, "function"
	}
	// Receiver type name (strip pointer).
	recv := ""
	if len(d.Recv.List) > 0 {
		switch t := d.Recv.List[0].Type.(type) {
		case *ast.StarExpr:
			if id, ok := t.X.(*ast.Ident); ok {
				recv = id.Name
			}
		case *ast.Ident:
			recv = t.Name
		}
	}
	if recv != "" {
		return recv + "." + name, "method"
	}
	return name, "method"
}

// genDeclSymbol returns the symbol name for a GenDecl (type/var/const block).
func genDeclSymbol(d *ast.GenDecl) (sym, kind string) {
	kind = declKind(d.Tok.String())
	if len(d.Specs) == 1 {
		switch sp := d.Specs[0].(type) {
		case *ast.TypeSpec:
			return sp.Name.Name, kind
		case *ast.ValueSpec:
			if len(sp.Names) > 0 {
				return sp.Names[0].Name, kind
			}
		}
	}
	return "", kind
}

func declKind(tok string) string {
	switch tok {
	case "type":
		return "type"
	case "var":
		return "var"
	case "const":
		return "const"
	}
	return tok
}
