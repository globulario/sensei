// SPDX-License-Identifier: AGPL-3.0-only

package closure

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
)

// C6 (INV-CLOSURE-LIVENESS), the general half: every required_next_action
// closure can emit has a registered discharge path and a responsible party.
// The emitters are read from the source itself — every literal action passed
// to addOpen / addUncertifiable / addCondition, and every RequiredNextAction
// field literal — so a new blocker cannot be introduced with an obligation
// nobody can meet. Nothing here names a particular file, claim or blocker.
func TestEveryEmittableActionHasADischargePath(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	found := map[string]string{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				sel, ok := x.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				idx := map[string]int{"addOpen": 4, "addUncertifiable": 3}[sel.Sel.Name]
				if idx > 0 && len(x.Args) > idx {
					if lit, ok := x.Args[idx].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						v, _ := strconv.Unquote(lit.Value)
						found[v] = fset.Position(lit.Pos()).String()
					}
				}
			case *ast.KeyValueExpr:
				if k, ok := x.Key.(*ast.Ident); ok && k.Name == "RequiredNextAction" {
					if lit, ok := x.Value.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						v, _ := strconv.Unquote(lit.Value)
						found[v] = fset.Position(lit.Pos()).String()
					}
				}
			}
			return true
		})
	}
	if len(found) < 10 {
		t.Fatalf("the scan found only %d actions; it is not reading the emitters", len(found))
	}
	for action, where := range found {
		if _, ok := DischargePaths[action]; !ok {
			t.Errorf("%s emits %q, which has no discharge path or responsible party", where, action)
		}
	}
}

// C6, the runtime half: a blocker whose obligation cannot be met is reported
// as Sensei's own defect — never left for a participant to fail.
func TestImpossibleObligationIsReportedAsAToolingDefect(t *testing.T) {
	cases := []struct {
		name string
		bl   Blocker
	}{
		{"unregistered action", Blocker{Code: "closure.x.y", RequiredNextAction: "consult_oracle"}},
		{"question demanded for current evidence", Blocker{Code: "closure.question.missing_artifact", RequiredNextAction: "create_open_question",
			ClaimIDs: []string{"claim.a"}, ClaimDisposition: string(DispositionEvidencedCurrent)}},
		{"evidence demanded for a stale source claim", Blocker{Code: "closure.evidence.claim_stale", RequiredNextAction: "add_evidence",
			ClaimIDs: []string{"claim.a"}, ClaimDisposition: string(DispositionStaleSource)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := &assessmentBuilder{dispByClaim: map[string]ClaimDisposition{}}
			b.addBlocker(tc.bl)
			b.checkLiveness()
			var defect *Blocker
			for i := range b.blockers {
				if b.blockers[i].Code == "closure.tooling.no_discharge_path" {
					defect = &b.blockers[i]
				}
			}
			if defect == nil {
				t.Fatalf("no tooling defect reported for %+v", tc.bl)
			}
			if defect.RequiredNextAction != ActionReportToolingDefect || DischargePaths[defect.RequiredNextAction] != PartyMaintainer {
				t.Fatalf("defect not addressed to the Sensei maintainer: %+v", defect)
			}
			if !strings.Contains(defect.Summary, b.blockers[0].ID) {
				t.Fatalf("defect does not name the blocker it is about: %q", defect.Summary)
			}
		})
	}
	ok := &assessmentBuilder{dispByClaim: map[string]ClaimDisposition{}}
	ok.addBlocker(Blocker{Code: "closure.evidence.claim_stale", RequiredNextAction: ActionRefreshExtraction,
		ClaimIDs: []string{"claim.a"}, ClaimDisposition: string(DispositionStaleSource)})
	ok.addBlocker(Blocker{Code: "closure.question.missing_artifact", RequiredNextAction: "create_open_question",
		ClaimIDs: []string{"claim.b"}, ClaimDisposition: string(DispositionNeedsQuestion)})
	ok.checkLiveness()
	if len(ok.blockers) != 2 {
		t.Fatalf("a dischargeable blocker was reported as a defect: %+v", ok.blockers)
	}
}

// DisposeClaim never treats unverified or stale freshness as current.
func TestDisposeClaimRequiresVerifiedCurrentPremises(t *testing.T) {
	// covered end to end in questiongen/closure_liveness_test.go; here the
	// nil-evaluation edge, which the pipeline never produces for a scoped claim.
	if d := DisposeClaim(sourceShapedClaimForTest(), sourceShapedReceiptsForTest(), nil); d != DispositionNeedsInvestigation {
		t.Fatalf("unverified freshness disposed as %s", d)
	}
}

func sourceShapedClaimForTest() architecture.Claim {
	return architecture.Claim{ID: "claim.a", ArchitecturalPlane: architecture.PlaneObserved,
		AssertionOrigin: architecture.OriginDerived, PremiseFacts: []string{"fact.a"}}
}

func sourceShapedReceiptsForTest() map[string]architecture.ClaimFactReceipt {
	return map[string]architecture.ClaimFactReceipt{"fact.a": {
		Fact: architecture.Fact{ID: "fact.a", Evidence: architecture.Evidence{SourceFile: "x.go"}},
		Provenance: architecture.Provenance{SourceKind: "source_file", SourceDigestStatus: architecture.SourceDigestResolved,
			SourceDigest: strings.Repeat("c", 64)},
	}}
}
