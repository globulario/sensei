// SPDX-License-Identifier: AGPL-3.0-only

package evalharness

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/evalmutant"
)

func run(t *testing.T) Report {
	t.Helper()
	dir := t.TempDir()
	report, err := RunDeterministicExtraction(Options{
		RepositoryDomain: "example.com/eval",
		CapturedAt:       "2026-08-18T00:00:00Z",
		MaterializeInto: func(name string) (string, error) {
			return filepath.Join(dir, name), nil
		},
	})
	if err != nil {
		t.Fatalf("run arm: %v", err)
	}
	return report
}

// The arm must run over every mutant plus the clean control, and the control
// must go through the identical path. An arm whose numbers on a clean tree are
// indistinguishable from its numbers on a mutated one has said something
// important, and only running both can reveal it.
func TestArmCoversEveryMutantAndTheControl(t *testing.T) {
	report := run(t)
	if len(report.Results) != len(evalmutant.Defects()) {
		t.Fatalf("ran %d mutant(s), want %d", len(report.Results), len(evalmutant.Defects()))
	}
	if report.Baseline.Defect != "" {
		t.Errorf("the control carries a defect label: %q", report.Baseline.Defect)
	}
	for _, res := range report.Results {
		if !res.DefectPresent {
			t.Errorf("%s: the witness did not confirm the defect in its own mutant; this run measures nothing for that class", res.Defect)
		}
		if strings.TrimSpace(res.Statement) == "" {
			t.Errorf("%s: no reference statement to grade against", res.Defect)
		}
		if len(res.DefectPaths) == 0 {
			t.Errorf("%s: no defect paths recorded", res.Defect)
		}
	}
}

// The honesty requirement. Deterministic extraction produces observations, not
// verdicts, so this arm must never report having named a defect — and the
// report must say so in its own limitations rather than leaving a reader to
// infer it from a missing field.
func TestArmNeverClaimsToHaveNamedADefect(t *testing.T) {
	report := run(t)
	for _, res := range report.Results {
		if res.NamedTheDefect {
			t.Errorf("%s: this arm claimed to name a defect, which it has no mechanism to do", res.Defect)
		}
	}
	var stated bool
	for _, l := range report.Limitations {
		if strings.Contains(l, "cannot name a defect") {
			stated = true
		}
	}
	if !stated {
		t.Error("the report does not state that this arm cannot name defects")
	}
}

// Site coverage is the real measurement: did the arm recover evidence anchored
// to the files the defect lives in? That is a precondition for detection, and
// reporting it separately keeps a later composition arm from inheriting credit
// for evidence this layer supplied.
func TestArmReportsWhichDefectSitesItRecoveredEvidenceFor(t *testing.T) {
	report := run(t)
	covered, total := report.SiteCoverageRate()
	if total != len(evalmutant.Defects()) {
		t.Fatalf("coverage denominator = %d, want %d", total, len(evalmutant.Defects()))
	}
	t.Logf("baseline arm site coverage: %d/%d", covered, total)
	for _, res := range report.Results {
		t.Logf("  %-32s observations=%-3d evidence=%-3d site_covered=%v covered=%v",
			res.Defect, res.Observations, res.EvidenceReceipts, res.SiteCovered(), res.CoveredDefectPaths)
		for _, p := range res.CoveredDefectPaths {
			var inDefect bool
			for _, d := range res.DefectPaths {
				if d == p {
					inDefect = true
				}
			}
			if !inDefect {
				t.Errorf("%s: covered path %s is not one of the defect's paths", res.Defect, p)
			}
		}
	}
	// The arm must recover SOMETHING somewhere, or the harness is measuring a
	// broken extractor rather than a hard suite.
	if covered == 0 {
		t.Error("the arm recovered no evidence for any defect site; that is an extractor failure, not a suite result")
	}
}

// Reproducibility is a completion criterion of the evaluation, so the report
// must be identical across runs given identical inputs.
func TestReportIsReproducible(t *testing.T) {
	first, second := run(t), run(t)
	if first.Baseline.DocumentDigest != second.Baseline.DocumentDigest {
		t.Fatalf("the control document digest is not reproducible: %s vs %s",
			first.Baseline.DocumentDigest, second.Baseline.DocumentDigest)
	}
	if len(first.Results) != len(second.Results) {
		t.Fatal("result counts differ between runs")
	}
	for i := range first.Results {
		a, b := first.Results[i], second.Results[i]
		if a.Defect != b.Defect {
			t.Fatalf("result %d: order is not stable (%s vs %s)", i, a.Defect, b.Defect)
		}
		if a.DocumentDigest != b.DocumentDigest {
			t.Errorf("%s: document digest is not reproducible", a.Defect)
		}
		if a.Observations != b.Observations || a.EvidenceReceipts != b.EvidenceReceipts {
			t.Errorf("%s: counts are not reproducible", a.Defect)
		}
	}
}

// A mutated tree must not produce a document identical to the control's. If it
// did, this arm could not distinguish the trees at all, and every downstream
// number derived from it would be describing the same input twice.
func TestMutantsAreDistinguishableFromTheControl(t *testing.T) {
	report := run(t)
	var identical []string
	for _, res := range report.Results {
		if res.DocumentDigest == report.Baseline.DocumentDigest {
			identical = append(identical, string(res.Defect))
		}
	}
	if len(identical) == len(report.Results) {
		t.Fatalf("every mutant produced the control's document; this arm cannot distinguish any of them: %v", identical)
	}
	if len(identical) > 0 {
		// Not a failure: a defect this arm cannot see at all is a real and
		// reportable finding, and the whole point of measuring a floor.
		t.Logf("indistinguishable from the control under this arm (a genuine negative result): %v", identical)
	}
}

// Required inputs are required. A harness that defaulted them would produce a
// report bound to whatever checkout it happened to run inside.
func TestRequiredInputsAreEnforced(t *testing.T) {
	dir := t.TempDir()
	ok := func(name string) (string, error) { return filepath.Join(dir, name), nil }
	for name, o := range map[string]Options{
		"no domain":      {CapturedAt: "2026-08-18T00:00:00Z", MaterializeInto: ok},
		"no captured_at": {RepositoryDomain: "example.com/eval", MaterializeInto: ok},
		"no materialize": {RepositoryDomain: "example.com/eval", CapturedAt: "2026-08-18T00:00:00Z"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := RunDeterministicExtraction(o); err == nil {
				t.Fatal("a run with a missing required input was accepted")
			}
		})
	}
}
