// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The published side of the reachability comparison must be the corpus
// revision, never the tool binary's own revision.
//
// A SOURCE check, because the defect it guards is a wiring mistake rather than
// a logic one. reachability.Assess is pure and answers exactly what it is
// asked; the failure was that the caller asked with graph_build_commit, which
// is the awareness-graph BINARY's vcs revision. Every unit test of Assess kept
// passing while the verdict measured the wrong thing -- so the assertion has to
// be about which value reaches the call.
//
// Measured 2026-09-08: graph_build_commit equalled `go version -m <serving
// binary>` on two live instances. Against a governed repo that is not the tool
// repo the comparison was permanently Unknown; against the tool repo it
// reported a count that was the age of the BINARY, and rebuilding the binary
// alone would have reported `current` with the store untouched.
func TestReachabilityIsNotAskedWithTheBinaryRevision(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(".", e.Name()), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "ResolveFromGit" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "reachability" {
				return true
			}
			calls++
			if len(call.Args) < 3 {
				t.Errorf("%s: ResolveFromGit takes %d arguments", e.Name(), len(call.Args))
				return true
			}
			// The published argument must not be any build-commit accessor.
			var offending string
			ast.Inspect(call.Args[2], func(n ast.Node) bool {
				if s, ok := n.(*ast.SelectorExpr); ok && strings.Contains(s.Sel.Name, "GraphBuildCommit") {
					offending = s.Sel.Name
				}
				return true
			})
			if offending != "" {
				t.Errorf("%s: the published revision passed to ResolveFromGit comes from %s, "+
					"which is the awareness-graph binary's revision and not the corpus the "+
					"generation was built from", e.Name(), offending)
			}
			return true
		})
	}
	// A pin over zero call sites proves nothing.
	if calls == 0 {
		t.Fatal("no reachability.ResolveFromGit call site found; this pin is checking nothing")
	}
}
