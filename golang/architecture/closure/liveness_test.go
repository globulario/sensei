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
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/architecture/plane"
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
			ClaimIDs: []string{"claim.a"}, ClaimResolution: string(ResolutionEvidencedCurrent)}},
		{"evidence demanded for a stale source claim", Blocker{Code: "closure.evidence.claim_stale", RequiredNextAction: "add_evidence",
			ClaimIDs: []string{"claim.a"}, ClaimResolution: string(ResolutionStaleSource)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := &assessmentBuilder{resolutionByClaim: map[string]ClaimResolution{}}
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
	ok := &assessmentBuilder{resolutionByClaim: map[string]ClaimResolution{}}
	ok.addBlocker(Blocker{Code: "closure.evidence.claim_stale", RequiredNextAction: ActionRefreshExtraction,
		ClaimIDs: []string{"claim.a"}, ClaimResolution: string(ResolutionStaleSource)})
	ok.addBlocker(Blocker{Code: "closure.question.missing_artifact", RequiredNextAction: "create_open_question",
		ClaimIDs: []string{"claim.b"}, ClaimResolution: string(ResolutionNeedsQuestion)})
	ok.checkLiveness()
	if len(ok.blockers) != 2 {
		t.Fatalf("a dischargeable blocker was reported as a defect: %+v", ok.blockers)
	}
}

// ResolveClaim never treats unverified or stale freshness as current.
func TestResolveClaimRequiresVerifiedCurrentPremises(t *testing.T) {
	// covered end to end in questiongen/closure_liveness_test.go; here the
	// nil-evaluation edge, which the pipeline never produces for a scoped claim.
	if d := ResolveClaim(sourceShapedClaimForTest(), sourceShapedReceiptsForTest(), nil, plane.StateJustified); d != ResolutionNeedsInvestigation {
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

// #387 review, finding B: an incomplete resolution set never yields
// evidenced_current — the old generator required EVERY referenced claim to be
// resolved, and that guarantee is kept.
func TestIncompleteResolutionSetNeverClosesOnEvidence(t *testing.T) {
	by := map[string]ClaimResolution{"claim.a": ResolutionEvidencedCurrent, "claim.q": ResolutionNeedsQuestion}
	cases := map[string]struct {
		ids  []string
		want ClaimResolution
	}{
		"all evidenced":                    {[]string{"claim.a"}, ResolutionEvidencedCurrent},
		"one claim has no resolution":      {[]string{"claim.a", "claim.missing"}, ResolutionNeedsInvestigation},
		"one claim needs a question":       {[]string{"claim.a", "claim.q"}, ResolutionNeedsQuestion},
		"only unknown claims":              {[]string{"claim.missing"}, ResolutionNeedsInvestigation},
		"an unrecognized resolution value": {[]string{"claim.a", "claim.weird"}, ResolutionNeedsInvestigation},
	}
	by["claim.weird"] = ClaimResolution("something_new")
	for name, tc := range cases {
		if got := combineResolutions(tc.ids, by); got != tc.want {
			t.Errorf("%s: got %s, want %s", name, got, tc.want)
		}
	}
	b := &assessmentBuilder{resolutionByClaim: by}
	b.addBlocker(Blocker{Code: "closure.evidence.claim_unknown", RequiredNextAction: "create_open_question", ClaimIDs: []string{"claim.a", "claim.missing"}})
	if b.blockers[0].ClaimResolution == string(ResolutionEvidencedCurrent) {
		t.Fatalf("blocker inherited evidenced_current: %+v", b.blockers[0])
	}
}

// #387 review, finding C: liveness is a property of the whole report. A defect
// reopens the agent dimension even after it was assessed closed, and the
// verdict is never closed while a defect stands, whatever the policy.
func TestLivenessIsUnconditionalAndFailsClosed(t *testing.T) {
	b := &assessmentBuilder{resolutionByClaim: map[string]ClaimResolution{},
		dimensions: []DimensionAssessment{{Dimension: DimensionAgent, Required: false, Applicable: true, State: StateClosed}}}
	b.addBlocker(Blocker{Dimension: DimensionEvidence, Code: "closure.x.y", RequiredNextAction: "consult_oracle"})
	b.checkLiveness()
	if b.dimensions[0].State != StateOpen || len(b.dimensions[0].BlockerIDs) == 0 {
		t.Fatalf("agent dimension not reopened by the defect: %+v", b.dimensions[0])
	}
	if !hasToolingDefect(b.blockers) {
		t.Fatal("defect not recorded")
	}
	// The verdict guard in Evaluate: a non-required agent dimension must not
	// let a report with a tooling defect close.
	rep := Report{Request: Request{Scope: Scope{RiskClass: RiskLowRisk}}, Dimensions: b.dimensions, Blockers: b.blockers}
	v := calculateVerdict(rep, Policy{})
	if hasToolingDefect(rep.Blockers) && (v == VerdictClosed || v == VerdictConditionallyClosed) {
		v = VerdictOpen
	}
	if v == VerdictClosed || v == VerdictConditionallyClosed {
		t.Fatalf("verdict %s with a tooling defect", v)
	}
}

// Freshness is not sufficiency (architect review of #386/#387): current
// premises prove the source did not change, not that the inference is sound.
func TestCurrentPremisesWithoutAJustifiedPlaneAreNotEvidenced(t *testing.T) {
	ev := &maintenance.ClaimEvaluation{EvaluatedStatus: architecture.StatusSupported,
		ProofLanes: maintenance.ProofLanes{PremiseFacts: maintenance.LaneState{State: maintenance.LaneCurrent}}}
	if d := ResolveClaim(sourceShapedClaimForTest(), sourceShapedReceiptsForTest(), ev, plane.StateJustified); d != ResolutionEvidencedCurrent {
		t.Fatalf("justified and current: %s", d)
	}
	for _, st := range []string{plane.StateUnderSupported, plane.StateInvalid, plane.StateUnknown, plane.StateStale, ""} {
		if d := ResolveClaim(sourceShapedClaimForTest(), sourceShapedReceiptsForTest(), ev, st); d == ResolutionEvidencedCurrent {
			t.Fatalf("plane %q with current premises closed on evidence", st)
		}
	}
}
