// SPDX-License-Identifier: AGPL-3.0-only

package evalharness

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/evalmutant"
	"github.com/globulario/sensei/golang/architecture/modelexec"
	"github.com/globulario/sensei/golang/architecture/whyinvestigation"
)

func runComposedArm(t *testing.T) CompositionReport {
	t.Helper()
	dir := t.TempDir()
	report, err := RunCompositionArm(Options{
		RepositoryDomain: "example.com/eval",
		CapturedAt:       "2026-08-18T00:00:00Z",
		MaterializeInto: func(name string) (string, error) {
			return filepath.Join(dir, name), nil
		},
	})
	if err != nil {
		t.Fatalf("run composition arm: %v", err)
	}
	return report
}

// The arm must cover every mutant plus the clean control, and the control must
// go through the identical path — an arm whose numbers on a clean tree are
// indistinguishable from its numbers on a mutated one has said something
// important, and only running both reveals it.
func TestCompositionArmCoversEveryMutantAndTheControl(t *testing.T) {
	report := runComposedArm(t)
	if len(report.Results) != len(evalmutant.Defects()) {
		t.Fatalf("ran %d mutant(s), want %d", len(report.Results), len(evalmutant.Defects()))
	}
	if report.Baseline.Defect != "" {
		t.Errorf("the control carries a defect label: %q", report.Baseline.Defect)
	}
	for _, res := range report.Results {
		if strings.TrimSpace(res.Statement) == "" {
			t.Errorf("%s: no reference statement to grade against", res.Defect)
		}
		if !res.DefectPresent && res.WhyUnavailable == "" {
			t.Errorf("%s: the witness did not confirm the defect in its own materialized repo", res.Defect)
		}
	}
}

// THE point of a second arm. Composition must be reported alongside the
// deterministic floor, or its cost cannot be judged. This test does not require
// composition to WIN — a composition layer that recovers no more than
// extraction is a real and reportable finding, and #131 exists to measure that,
// not to confirm a hope.
func TestCompositionArmIsComparableToTheDeterministicFloor(t *testing.T) {
	dir := t.TempDir()
	base, err := RunDeterministicExtraction(Options{
		RepositoryDomain: "example.com/eval",
		CapturedAt:       "2026-08-18T00:00:00Z",
		MaterializeInto:  func(n string) (string, error) { return filepath.Join(dir, "det-"+n), nil },
	})
	if err != nil {
		t.Fatalf("deterministic arm: %v", err)
	}
	comp := runComposedArm(t)

	if len(comp.Results) != len(base.Results) {
		t.Fatalf("arms cover different suites: composition %d, deterministic %d", len(comp.Results), len(base.Results))
	}
	covered, total := base.SiteCoverageRate()
	produced, ctotal := comp.CandidateRate()
	t.Logf("deterministic: site coverage %d/%d", covered, total)
	t.Logf("composition:   candidates produced for %d/%d mutants", produced, ctotal)
	for i := range comp.Results {
		c, b := comp.Results[i], base.Results[i]
		if c.Defect != b.Defect {
			t.Fatalf("result %d: arms are not aligned (%s vs %s)", i, c.Defect, b.Defect)
		}
		t.Logf("  %-32s det_obs=%-3d comp_obs=%-3d candidates=%-3d challenges=%-3d evreq=%-3d why=%s",
			c.Defect, b.Observations, c.Observations, c.Candidates, c.Challenges, c.EvidenceRequests,
			shortReason(c.WhyUnavailable))
	}
}

func shortReason(s string) string {
	if s == "" {
		return "ok"
	}
	if len(s) > 48 {
		return s[:48] + "…"
	}
	return s
}

// The honesty requirement, same as the deterministic arm's. A candidate is
// advisory; this arm has no mechanism to decide that a candidate matched the
// intended defect, so it must never claim it did.
func TestCompositionArmNeverClaimsToHaveNamedADefect(t *testing.T) {
	report := runComposedArm(t)
	for _, res := range report.Results {
		if res.NamedTheDefect {
			t.Errorf("%s: the arm claimed to name a defect, which it cannot grade", res.Defect)
		}
	}
	var stated bool
	for _, l := range report.Limitations {
		if strings.Contains(l, "grading judgement") {
			stated = true
		}
	}
	if !stated {
		t.Error("the report does not state that candidate-to-defect matching is a grading judgement it does not make")
	}
}

// A WHY or composition that could not run must be TYPED, never left to be
// inferred from a zero candidate count. Otherwise an infrastructure failure
// reads as a negative result about the arm's capability.
func TestCompositionArmTypesAnUnavailableWhyRatherThanReportingZero(t *testing.T) {
	report := runComposedArm(t)
	for _, res := range report.Results {
		if res.Candidates == 0 && res.Challenges == 0 && res.WhyUnavailable == "" {
			// Legitimate: composition ran and produced nothing. Recorded so the
			// distinction is visible in the log rather than assumed.
			t.Logf("%s: composition ran and produced no candidate (a real negative result)", res.Defect)
		}
		if res.WhyUnavailable != "" && (res.Candidates > 0 || res.Challenges > 0) {
			t.Errorf("%s: reports WHY unavailable yet also reports composition output", res.Defect)
		}
	}
}

// Reproducibility is a completion criterion of the evaluation.
func TestCompositionReportIsReproducible(t *testing.T) {
	first, second := runComposedArm(t), runComposedArm(t)
	if len(first.Results) != len(second.Results) {
		t.Fatal("result counts differ between runs")
	}
	for i := range first.Results {
		a, b := first.Results[i], second.Results[i]
		if a.Defect != b.Defect {
			t.Fatalf("result %d: order is not stable (%s vs %s)", i, a.Defect, b.Defect)
		}
		if a.DocumentDigest != b.DocumentDigest {
			t.Errorf("%s: HOW document digest is not reproducible", a.Defect)
		}
		if a.Candidates != b.Candidates || a.Challenges != b.Challenges {
			t.Errorf("%s: composition counts are not reproducible (%d/%d vs %d/%d)",
				a.Defect, a.Candidates, a.Challenges, b.Candidates, b.Challenges)
		}
	}
}

func TestCompositionArmEnforcesRequiredInputs(t *testing.T) {
	dir := t.TempDir()
	ok := func(name string) (string, error) { return filepath.Join(dir, name), nil }
	for name, o := range map[string]Options{
		"no domain":      {CapturedAt: "2026-08-18T00:00:00Z", MaterializeInto: ok},
		"no captured_at": {RepositoryDomain: "example.com/eval", MaterializeInto: ok},
		"no materialize": {RepositoryDomain: "example.com/eval", CapturedAt: "2026-08-18T00:00:00Z"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := RunCompositionArm(o); err == nil {
				t.Fatal("a run with a missing required input was accepted")
			}
		})
	}
}

// The most dangerous number this arm can report is a zero candidate count,
// because it has two completely different meanings: composition ran and found
// nothing (a real result about capability), or composition never ran (a
// structural limit that says nothing about capability).
//
// Today it is the second: a synthetic mutant has no graph to bind, so the
// composition step is refused. The report must say which, in its own
// limitations, rather than leaving a reader to assume the flattering reading.
func TestZeroCandidatesIsDistinguishedFromCompositionNotRunning(t *testing.T) {
	report := runComposedArm(t)

	var statesTheDistinction bool
	for _, l := range report.Limitations {
		if strings.Contains(l, "COMPOSITION DID NOT RUN") {
			statesTheDistinction = true
		}
	}
	if !statesTheDistinction {
		t.Error("the report does not distinguish 'composition found nothing' from 'composition did not run'; a zero candidate count would read as a negative result about capability")
	}

	// And every zero must be accounted for: either composition ran (no typed
	// unavailability) or the refusal is recorded verbatim.
	for _, res := range report.Results {
		if res.Candidates == 0 && res.WhyUnavailable == "" {
			t.Logf("%s: composition ran and produced no candidate — a genuine negative result", res.Defect)
		}
		if res.Candidates == 0 && res.WhyUnavailable != "" && !strings.Contains(res.WhyUnavailable, "composition") && !strings.Contains(res.WhyUnavailable, "HOW") {
			t.Errorf("%s: zero candidates with an unavailability that names no stage: %q", res.Defect, res.WhyUnavailable)
		}
	}

	// HOW and WHY must still have run: the structural limit is at composition,
	// and an arm that silently stopped earlier would measure something else.
	for _, res := range report.Results {
		if res.Observations == 0 {
			t.Errorf("%s: HOW produced no observations, so this arm did not reach composition for a different reason than reported", res.Defect)
		}
	}
}

// A report must not identify itself as model-disabled while a model is bound.
// The arm, its generator version and its resource label are all derived from
// the lane rather than from a constant chosen when the function was written.
func TestArmIdentityIsDerivedFromTheLane(t *testing.T) {
	disabled := whyinvestigation.ModelLane{Config: modelexec.Config{Disabled: true}}
	bound := whyinvestigation.ModelLane{Config: modelexec.Config{Requested: true, ProviderID: "bridge", ModelName: "m"}}

	if got := armIdentityFor(disabled); got != ArmCompositionModelDisabled {
		t.Errorf("disabled lane arm = %q, want %q", got, ArmCompositionModelDisabled)
	}
	if got := armIdentityFor(bound); got != ArmCompositionModelBound {
		t.Errorf("bound lane arm = %q, want %q", got, ArmCompositionModelBound)
	}
	if got := modelResourceLabel(bound); got == "disabled" {
		t.Error("a bound lane described its own resources as disabled")
	}
	if got := modelResourceLabel(disabled); got != "disabled" {
		t.Errorf("disabled lane resource label = %q, want disabled", got)
	}
	if got := modelResourceLabel(whyinvestigation.ModelLane{}); got != "not_requested" {
		t.Errorf("unrequested lane resource label = %q, want not_requested", got)
	}
}
