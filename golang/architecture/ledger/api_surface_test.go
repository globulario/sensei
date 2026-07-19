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
	forbidden := []string{"ForTest", "RewriteLatest", "InjectHeadWriteFaults", "FailHeadWrites", "FailNextHeadWrite"}
	fset := token.NewFileSet()
	for _, name := range pkg.GoFiles {
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() {
				continue
			}
			for _, bad := range forbidden {
				if strings.Contains(fn.Name.Name, bad) {
					t.Fatalf("default ledger build exports forbidden test/rewrite API %q in %s", fn.Name.Name, name)
				}
			}
		}
	}
}
