// SPDX-License-Identifier: AGPL-3.0-only

package ledger_test

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode"
)

// TestNoHistoryRewriteOrFaultToggleAPI proves the immutability law at the API
// boundary: the NORMAL ledger build (no build tags) exports no capability to
// rewrite committed history or toggle HEAD-write failures. The fault seam lives in
// a sensei_faultinject-tagged file that ships in no ordinary build; this test reads
// the package's default-build source set and fails if any such symbol leaks into
// it.
//
// It is a rule, not a list. A fixed list of names passed while
// WithHeadPublicationFault shipped as a production StoreOption, because that name
// was not on it. So the guard inspects the WHOLE exported surface -- functions,
// methods, types, struct fields, interface methods, vars, consts -- and matches
// CamelCase words against control vocabulary (see forbiddenControlWord). Word
// matching, not substring matching, is what keeps it precise: "Defaults" is not a
// fault and "Testament" is not a test.
func TestNoHistoryRewriteOrFaultToggleAPI(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the ledger package directory")
	}
	dir := filepath.Dir(thisFile)

	// build.Default carries no custom build tags, so files gated by
	// //go:build sensei_faultinject are excluded from GoFiles (they land in
	// IgnoredGoFiles). GoFiles is exactly the shipped, non-test source set.
	pkg, err := build.Default.ImportDir(dir, 0)
	if err != nil {
		t.Fatalf("import ledger dir: %v", err)
	}

	// The tagged fault seam must be excluded from the default build.
	for _, f := range pkg.GoFiles {
		if f == "faultinject.go" {
			t.Fatal("faultinject.go leaked into the default (untagged) build")
		}
	}

	fset := token.NewFileSet()
	checked := 0
	for _, name := range pkg.GoFiles {
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, ident := range exportedIdentifiers(file) {
			checked++
			if word, bad := forbiddenControlWord(ident); bad {
				t.Errorf("default ledger build exports %q in %s: the word %q names a test/fault/rewrite control, "+
					"which must live behind the sensei_faultinject build tag or in test files", ident, name, word)
			}
		}
	}
	// A guard that inspects nothing passes vacuously.
	if checked == 0 {
		t.Fatal("no exported identifiers inspected; the guard is not reading the package")
	}
}

// TestOnlyAppendPublishesHead proves HEAD recovery authority is confined to
// Store.Append in the default build: every call that can write HEAD is reachable
// only from appendEntry. A name list of exported symbols cannot show this -- an
// exported method that calls writeHead has an innocent name -- so the guard follows
// the calls themselves.
func TestOnlyAppendPublishesHead(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the ledger package directory")
	}
	dir := filepath.Dir(thisFile)
	pkg, err := build.Default.ImportDir(dir, 0)
	if err != nil {
		t.Fatalf("import ledger dir: %v", err)
	}

	allowed := map[string]map[string]bool{
		"writeHead":              {"publishHead": true},
		"publishHead":            {"appendEntry": true, "recoverUnpublishedHead": true},
		"recoverUnpublishedHead": {"appendEntry": true},
	}
	seen := map[string]int{}
	fset := token.NewFileSet()
	for _, name := range pkg.GoFiles {
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				var callee string
				switch x := n.(type) {
				case *ast.CallExpr:
					if id, ok := x.Fun.(*ast.Ident); ok {
						callee = id.Name
					}
				case *ast.Ident:
					// A function value passed around escapes the call graph above.
					if _, guarded := allowed[x.Name]; guarded && !isCallee(fn.Body, x) {
						t.Errorf("%s in %s references %s as a value; HEAD writers must only be called", fn.Name.Name, name, x.Name)
					}
				}
				if callers, guarded := allowed[callee]; guarded {
					seen[callee]++
					if !callers[fn.Name.Name] {
						t.Errorf("%s in %s calls %s; only %v may, so HEAD recovery stays inside Store.Append",
							fn.Name.Name, name, callee, keys(callers))
					}
				}
				return true
			})
		}
	}
	for callee := range allowed {
		if seen[callee] == 0 {
			t.Errorf("no call to %s found; the guard is not reading the package", callee)
		}
	}
}

func isCallee(body *ast.BlockStmt, id *ast.Ident) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && call.Fun == ast.Expr(id) {
			found = true
		}
		return !found
	})
	return found
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestForbiddenControlWordMatcher pins the matcher in both directions, so the
// guard above can neither go blind nor cry wolf.
func TestForbiddenControlWordMatcher(t *testing.T) {
	for _, name := range []string{
		"WithHeadPublicationFault",
		"WithHeadPublicationFaults",
		"InjectHeadWriteFaults",
		"FailNextHeadWrite",
		"FailHeadWritesForTest",
		"RewriteLatestPayloadForTest",
		"Store.ArmFaultInjector",
		"HeadFaultInjection",
		"TamperEntry",
		"ForgeHead",
	} {
		if _, bad := forbiddenControlWord(name); !bad {
			t.Errorf("%q was not flagged; the guard would let it ship", name)
		}
	}
	for _, name := range []string{
		"Default",
		"Defaults",
		"WithPayloadValidator",
		"Store.ReconcileDerivedState",
		"ReconcileResult.ProjectionsRebuilt",
		"ErrEntryDurable",
		"Testament",
		"EntryDigestSHA256",
		"VerifyChainCtx",
	} {
		if word, bad := forbiddenControlWord(name); bad {
			t.Errorf("%q was flagged on %q; a guard that cries wolf gets deleted", name, word)
		}
	}
}

// controlStems match any CamelCase word that BEGINS with them (fault, faults,
// faulty; inject, injected, injector, injection).
var controlStems = []string{"fault", "inject", "tamper", "forge"}

// controlWords match whole words only: "fail" is a control verb where "failed" or
// "failure" describe an outcome, and "test" must not match "testament".
var controlWords = map[string]bool{"fail": true, "fails": true, "rewrite": true, "rewrites": true, "test": true, "tests": true}

func forbiddenControlWord(ident string) (string, bool) {
	for _, part := range strings.Split(ident, ".") {
		for _, word := range camelWords(part) {
			if controlWords[word] {
				return word, true
			}
			for _, stem := range controlStems {
				if strings.HasPrefix(word, stem) {
					return word, true
				}
			}
		}
	}
	return "", false
}

// camelWords splits a Go identifier into lower-cased words: "HTTPHeadFault" ->
// http, head, fault. Digits stay with the word they follow.
func camelWords(s string) []string {
	r := []rune(s)
	var words []string
	start := 0
	for i := 1; i < len(r); i++ {
		lowerToUpper := unicode.IsUpper(r[i]) && !unicode.IsUpper(r[i-1])
		acronymEnd := unicode.IsUpper(r[i]) && unicode.IsUpper(r[i-1]) && i+1 < len(r) && unicode.IsLower(r[i+1])
		if lowerToUpper || acronymEnd || r[i] == '_' {
			words = append(words, strings.ToLower(strings.Trim(string(r[start:i]), "_")))
			start = i
		}
	}
	words = append(words, strings.ToLower(strings.Trim(string(r[start:]), "_")))
	return words
}

// exportedIdentifiers lists every exported name a file declares, qualified by its
// owner for methods, fields and interface methods.
func exportedIdentifiers(file *ast.File) []string {
	var out []string
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if !d.Name.IsExported() {
				continue
			}
			if d.Recv != nil && len(d.Recv.List) > 0 {
				out = append(out, receiverName(d.Recv.List[0].Type)+"."+d.Name.Name)
				continue
			}
			out = append(out, d.Name.Name)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.IsExported() {
						out = append(out, s.Name.Name)
					}
					var fields *ast.FieldList
					switch tt := s.Type.(type) {
					case *ast.StructType:
						fields = tt.Fields
					case *ast.InterfaceType:
						fields = tt.Methods
					}
					if fields == nil {
						continue
					}
					for _, f := range fields.List {
						for _, n := range f.Names {
							if n.IsExported() {
								out = append(out, s.Name.Name+"."+n.Name)
							}
						}
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if n.IsExported() {
							out = append(out, n.Name)
						}
					}
				}
			}
		}
	}
	return out
}

func receiverName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return receiverName(e.X)
	case *ast.IndexExpr:
		return receiverName(e.X)
	case *ast.IndexListExpr:
		return receiverName(e.X)
	case *ast.Ident:
		return e.Name
	}
	return "?"
}
