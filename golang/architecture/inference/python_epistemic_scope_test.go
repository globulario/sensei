// SPDX-License-Identifier: AGPL-3.0-only

package inference_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/factextract"
	"github.com/globulario/sensei/golang/architecture/inference"
)

// #386 review: a syntactic attribute assignment observed in Python must never
// become an unsupported claim of exclusive architectural ownership — through
// ANY default inference rule, not only the writer-set rule. Fresh source
// evidence (R-CLOSURE-01) proves the source did not change; it does not
// upgrade what may be inferred from it. Freshness and sufficiency are separate.
func TestPythonWriteFactsNeverBecomeOwnershipOrAuthorityClaims(t *testing.T) {
	root := t.TempDir()
	src := "class Cache:\n    def __init__(self, path):\n        self.path = path\n\n    def describe(self):\n        return self.path\n"
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "cache.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := factextract.Extract(root, factextract.Options{IncludeTests: true, MinimumConfidence: "low"})
	if err != nil {
		t.Fatal(err)
	}
	python := map[string]bool{}
	for _, f := range report.Facts {
		if f.Extractor == "python_ast_extractor" {
			python[f.ID] = true
		}
	}
	if len(python) == 0 {
		t.Fatal("fixture produced no Python facts")
	}
	reg, err := inference.DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	rules, err := reg.Select(nil)
	if err != nil {
		t.Fatal(err)
	}
	apps, err := inference.NewEngine(rules).Apply(inference.Context{Facts: report.Facts})
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{"own", "authority", "sole", "exclusive", "enforce", "govern", "responsib", "asserts_rule"}
	writerClaims := 0
	for _, app := range apps {
		fromPython := false
		for _, id := range app.PremiseFactIDs {
			fromPython = fromPython || python[id]
		}
		if !fromPython {
			continue
		}
		c := app.Claim
		if c.ArchitecturalPlane != architecture.PlaneObserved {
			t.Errorf("%s: a Python observation produced a %s-plane claim: %+v", app.RuleID, c.ArchitecturalPlane, c.Statement)
		}
		for _, w := range forbidden {
			if strings.Contains(strings.ToLower(c.Statement.Predicate), w) {
				t.Errorf("%s: predicate %q asserts more than syntax can show", app.RuleID, c.Statement.Predicate)
			}
		}
		if !c.HumanReviewRequired || c.PromotionStatus != architecture.PromotionCandidate {
			t.Errorf("%s: a syntax-only claim is not held for review: review=%v promotion=%s", app.RuleID, c.HumanReviewRequired, c.PromotionStatus)
		}
		if c.Statement.Predicate == "has_observed_writer_set" {
			writerClaims++
			if c.Confidence > 0.55 {
				t.Errorf("writer-set confidence %.2f exceeds its cap", c.Confidence)
			}
			joined := strings.Join(c.Unknowns, " | ")
			for _, must := range []string{"does not prove sole authority", "does not prove architectural ownership"} {
				if !strings.Contains(joined, must) {
					t.Errorf("single-writer claim lacks the limitation %q: %v", must, c.Unknowns)
				}
			}
			alts := strings.Join(c.AlternativeExplanations, " | ")
			if !strings.Contains(alts, "delegate") || !strings.Contains(alts, "outside the inspected source set") {
				t.Errorf("single-writer claim does not keep its alternatives open: %v", c.AlternativeExplanations)
			}
		}
	}
	if writerClaims == 0 {
		t.Fatal("no writer-set claim was derived from the Python assignment; the test is not exercising the rule")
	}
}
