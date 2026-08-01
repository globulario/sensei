// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoExportedMutableAuthorityState is a permanent regression guard
// against exactly the authority leak flagged in architect review
// 4834886202: GovernedFailureClassMinimumRecommendation used to be an
// exported package-level map, a mutable reference type any importing
// package could rewrite in place (e.g. lowering audit-forbidden-fix's
// floor from abort to retry-generation before validation ever runs),
// silently defeating ValidateEvaluationPolicy's downgrade check without
// even needing reflection or unsafe -- an ordinary assignment from another
// package would do it.
//
// This test parses every non-test .go file in this package directory and
// fails if any exported package-level var declares a map or slice type
// (whether via an explicit type, a composite literal, or make(...)). A
// closed authority table in this package must be private, or -- as
// GovernedFailureClassMinimumRecommendationFor and
// recommendationSeverityRank now are -- expressed as a function/switch
// with no mutable value for anything outside this package to reach at all.
// An external consumer literally cannot rewrite what does not exist as a
// reachable value.
func TestNoExportedMutableAuthorityState(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.VAR {
				continue
			}
			for _, spec := range genDecl.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, ident := range valueSpec.Names {
					if !ident.IsExported() {
						continue
					}
					if valueSpec.Type != nil && isMutableReferenceTypeExpr(valueSpec.Type) {
						t.Errorf("%s: exported var %q has an explicit mutable reference type (map/slice) -- authority tables must be private or expressed as a function", name, ident.Name)
						continue
					}
					if i < len(valueSpec.Values) && isMutableReferenceTypeValue(valueSpec.Values[i]) {
						t.Errorf("%s: exported var %q is initialized from a mutable reference type (map/slice literal or make(...)) -- authority tables must be private or expressed as a function", name, ident.Name)
					}
				}
			}
		}
	}
}

func isMutableReferenceTypeExpr(expr ast.Expr) bool {
	switch expr.(type) {
	case *ast.MapType, *ast.ArrayType:
		return true
	default:
		return false
	}
}

func isMutableReferenceTypeValue(expr ast.Expr) bool {
	switch v := expr.(type) {
	case *ast.CompositeLit:
		return isMutableReferenceTypeExpr(v.Type)
	case *ast.CallExpr:
		fn, ok := v.Fun.(*ast.Ident)
		if !ok || fn.Name != "make" || len(v.Args) == 0 {
			return false
		}
		return isMutableReferenceTypeExpr(v.Args[0])
	default:
		return false
	}
}

// TestGovernedFailureClassMinimumRecommendationForIsReferentiallyStable
// proves the lookup is a pure function of its input across repeated calls
// -- there is no package-level mutable state left for anything (this
// package's own code, a future refactor, or an external consumer via any
// exported symbol) to have perturbed between calls.
func TestGovernedFailureClassMinimumRecommendationForIsReferentiallyStable(t *testing.T) {
	for _, class := range []GovernedFailureClass{
		FailureClassAuditForbiddenFix,
		FailureClassProofPermanentlyUndischargeable,
		FailureClassIncidentScarConcerning,
		FailureClassProofPlanStructural,
		FailureClassAuditPlanLevel,
		FailureClassMechanicalCheckFailure,
	} {
		first, firstOK := GovernedFailureClassMinimumRecommendationFor(string(class))
		for i := 0; i < 100; i++ {
			got, ok := GovernedFailureClassMinimumRecommendationFor(string(class))
			if got != first || ok != firstOK {
				t.Fatalf("%s: minimum recommendation changed across calls: first %q/%v, later %q/%v", class, first, firstOK, got, ok)
			}
		}
	}
}
