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
)

// TestNoHistoryRewriteOrFaultToggleAPI proves the immutability law at the API
// boundary: the NORMAL ledger build (no build tags) exports no capability to
// rewrite committed history or toggle HEAD-write failures. The fault seam lives in
// a sensei_faultinject-tagged file that ships in no ordinary build; this test reads
// the package's default-build source set and fails if any such symbol leaks into
// it. It is the regression guard for the Round-4 backdoor removal.
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

	// No production-shipped file may export a history-rewrite or fault-toggle API.
	forbidden := []string{"ForTest", "RewriteLatest", "InjectHeadWriteFaults", "FailHeadWrites", "FailNextHeadWrite", "RecoverDurableAppend"}
	fset := token.NewFileSet()
	for _, name := range pkg.GoFiles {
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() {
				continue
			}
			// Methods are checked too: the capability this guards against was a
			// METHOD on Store, so a receiver-only filter would have missed it.
			for _, bad := range forbidden {
				if strings.Contains(fn.Name.Name, bad) {
					t.Fatalf("default ledger build exports forbidden test/rewrite API %q in %s", fn.Name.Name, name)
				}
			}
			// NO EXPORTED FUNCTION MAY ACCEPT ErrEntryDurable.
			//
			// That value names a committed entry, and the round-two review finding
			// was that accepting it as a PARAMETER turns it into a capability: it is
			// an ordinary exported struct, so any caller can construct one. After
			// deleting the highest-sequence entry a caller builds the identity of the
			// entry that survived, and an API that "verifies" the argument against
			// the chain finds it matches -- then republishes HEAD over the truncated
			// prefix and launders the deletion into a valid ledger (issue #352).
			//
			// The evidence that an append really committed an entry exists only
			// inside the Append that created it, and it cannot be passed. So it
			// never is: the one bounded HEAD retry lives in appendEntry, and this
			// keeps a convenience wrapper from quietly growing the capability back.
			if fn.Type.Params == nil {
				continue
			}
			for _, param := range fn.Type.Params.List {
				if strings.Contains(typeString(param.Type), "ErrEntryDurable") {
					t.Fatalf("exported %s in %s accepts ErrEntryDurable; a caller-supplied committed-entry "+
						"identity is forgeable and must never license a HEAD republication", fn.Name.Name, name)
				}
			}
		}
	}
}

// typeString renders a parameter type well enough to spot a named type, through
// pointers, slices and package qualifiers.
func typeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return typeString(t.X)
	case *ast.SelectorExpr:
		return typeString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		return typeString(t.Elt)
	case *ast.Ellipsis:
		return typeString(t.Elt)
	default:
		return ""
	}
}
