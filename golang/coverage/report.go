// SPDX-License-Identifier: AGPL-3.0-only

package coverage

// report.go — pure risk-weighted coverage report builder. Given a file
// inventory (path + whether the graph anchors at least one invariant /
// failure mode / intent to it) and the authority-domain coversPath set, it
// produces a deterministic report. The inventory is gathered offline by
// cmd/coverage-report (tree walk + seed parse); this builder does no I/O.

import "sort"

// FileCoverage is one file's coverage fact.
type FileCoverage struct {
	Path            string
	HasDirectAnchor bool
}

// SurfaceCoverage is the coverage of one named surface, by file count and by
// risk weight. Percent is the file-count percentage (0 when no files).
type SurfaceCoverage struct {
	Surface       string
	TotalFiles    int
	CoveredFiles  int
	Percent       int
	TotalWeight   int
	CoveredWeight int
}

// Report is the full risk-weighted coverage report.
type Report struct {
	TotalFiles int
	// WeightedOverallPercent is the single risk-weighted headline: covered
	// weight over total production weight. Helper/generated files stay visible
	// in the low_risk_helper surface, but do not depress the headline because
	// anchoring generated code and tests is not meaningful coverage quality.
	WeightedOverallPercent int
	// Surfaces are sorted by surface name for determinism.
	Surfaces []SurfaceCoverage
	// UnknownHighRiskFiles are high/critical files with NO direct anchor — the
	// places where missing coverage costs the most. Sorted, capped at
	// MaxUnknownHighRiskFiles. UnknownHighRiskCount is the untruncated total so
	// a capped list never reads as "that's all of them".
	UnknownHighRiskFiles []string
	UnknownHighRiskCount int
}

// MaxUnknownHighRiskFiles caps the listed unknown files so the report stays
// bounded; the count is always reported even when the list is truncated.
const MaxUnknownHighRiskFiles = 200

// BuildReport computes the risk-weighted report from a file inventory.
// Deterministic: surfaces and unknown files are sorted; map iteration order is
// never observed.
func BuildReport(files []FileCoverage, authorityCoversPaths []string) *Report {
	type acc struct {
		total, covered, totalW, coveredW int
	}
	surfaceAcc := map[string]*acc{}
	get := func(name string) *acc {
		a, ok := surfaceAcc[name]
		if !ok {
			a = &acc{}
			surfaceAcc[name] = a
		}
		return a
	}

	totalW, coveredW := 0, 0
	var unknown []string

	for _, f := range files {
		w := FileWeight(f.Path, authorityCoversPaths)
		if !isHelperOrGenerated(f.Path) {
			totalW += w
			if f.HasDirectAnchor {
				coveredW += w
			}
		}
		for _, s := range FileSurfaces(f.Path, authorityCoversPaths) {
			a := get(s)
			a.total++
			a.totalW += w
			if f.HasDirectAnchor {
				a.covered++
				a.coveredW += w
			}
		}
		if !f.HasDirectAnchor && FileRiskTier(f.Path, authorityCoversPaths) != RiskLow {
			unknown = append(unknown, normPath(f.Path))
		}
	}

	names := make([]string, 0, len(surfaceAcc))
	for n := range surfaceAcc {
		names = append(names, n)
	}
	sort.Strings(names)

	surfaces := make([]SurfaceCoverage, 0, len(names))
	for _, n := range names {
		a := surfaceAcc[n]
		surfaces = append(surfaces, SurfaceCoverage{
			Surface:       n,
			TotalFiles:    a.total,
			CoveredFiles:  a.covered,
			Percent:       pct(a.covered, a.total),
			TotalWeight:   a.totalW,
			CoveredWeight: a.coveredW,
		})
	}

	sort.Strings(unknown)
	unknownCount := len(unknown)
	if len(unknown) > MaxUnknownHighRiskFiles {
		unknown = unknown[:MaxUnknownHighRiskFiles]
	}

	return &Report{
		TotalFiles:             len(files),
		WeightedOverallPercent: pct(coveredW, totalW),
		Surfaces:               surfaces,
		UnknownHighRiskFiles:   unknown,
		UnknownHighRiskCount:   unknownCount,
	}
}

// pct returns floor(num*100/den), or 0 when den is 0.
func pct(num, den int) int {
	if den <= 0 {
		return 0
	}
	return num * 100 / den
}
