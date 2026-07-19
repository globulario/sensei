// SPDX-License-Identifier: AGPL-3.0-only

package coverage

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pkgFacts collects import paths, called/ident names, and func declarations across the
// non-test .go files of a package directory.
type pkgFacts struct {
	imports map[string]bool
	idents  map[string]bool
	funcs   map[string]bool
	rawText string
}

func scanPackage(t *testing.T, dir string) pkgFacts {
	t.Helper()
	f := pkgFacts{imports: map[string]bool{}, idents: map[string]bool{}, funcs: map[string]bool{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		f.rawText += string(data)
		file, err := parser.ParseFile(fset, path, data, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imp := range file.Imports {
			f.imports[strings.Trim(imp.Path.Value, `"`)] = true
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.Ident:
				f.idents[v.Name] = true
			case *ast.FuncDecl:
				f.funcs[v.Name.Name] = true
			}
			return true
		})
	}
	return f
}

// TestBriefingFeedbackOwnershipBoundary proves the single-owner boundary statically: the server
// and tasksession consume the canonical briefingfeedback owner and never reimplement promotion
// verification or read promotion artifacts directly.
func TestBriefingFeedbackOwnershipBoundary(t *testing.T) {
	root := repoRootForHighRisk(t)
	const (
		feedbackPkg = "github.com/globulario/sensei/golang/architecture/briefingfeedback"
		promoPkg    = "github.com/globulario/sensei/golang/architecture/questionpromotion"
	)

	server := scanPackage(t, filepath.Join(root, "golang", "server"))
	if !server.imports[feedbackPkg] {
		t.Errorf("golang/server must consume briefingfeedback")
	}
	if server.imports[promoPkg] {
		t.Errorf("golang/server must NOT consume questionpromotion directly (verification is owned by the feedback owner)")
	}

	tasksession := scanPackage(t, filepath.Join(root, "golang", "architecture", "tasksession"))
	if !tasksession.imports[feedbackPkg] {
		t.Errorf("tasksession must consume briefingfeedback")
	}

	// Neither consumer reimplements verification or discovery, nor re-adds a consumer-side
	// admission helper.
	for name, f := range map[string]pkgFacts{"server": server, "tasksession": tasksession} {
		for _, forbidden := range []string{"VerifyCommittedPromotion", "DiscoverCommittedPromotions", "promotionInScope"} {
			if f.idents[forbidden] || f.funcs[forbidden] {
				t.Errorf("%s must not reference %q (owned by the feedback owner / removed)", name, forbidden)
			}
		}
		// No consumer reads promotion journals or receipts directly.
		if strings.Contains(f.rawText, "project/promotions") || strings.Contains(f.rawText, "receiptFileName") {
			t.Errorf("%s must not read promotion journal/receipt artifacts directly", name)
		}
	}

	// VerifyCommittedPromotion remains owned by the promotion verifier and is invoked by the
	// canonical feedback owner.
	feedback := scanPackage(t, filepath.Join(root, "golang", "architecture", "briefingfeedback"))
	if !feedback.imports[promoPkg] {
		t.Errorf("briefingfeedback must invoke VerifyCommittedPromotion through the questionpromotion boundary")
	}
}
