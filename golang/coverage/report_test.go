// SPDX-License-Identifier: Apache-2.0

package coverage

import (
	"reflect"
	"testing"
)

func sampleInventory() ([]FileCoverage, []string) {
	auth := []string{"golang/repository/repository_server/", "golang/rbac/rbac_server/"}
	files := []FileCoverage{
		{Path: "golang/repository/repository_server/publish_workflow.go", HasDirectAnchor: true},
		{Path: "golang/repository/repository_server/resolver.go", HasDirectAnchor: false},
		{Path: "golang/rbac/rbac_server/rbac_access.go", HasDirectAnchor: true},
		{Path: "golang/rbac/rbac_server/rbac_deny.go", HasDirectAnchor: false},
		{Path: "golang/echo/echo_server/echo.go", HasDirectAnchor: false}, // low-risk helper surface
		{Path: "golang/util/strings_test.go", HasDirectAnchor: false},     // helper, low risk
	}
	return files, auth
}

func surfaceByName(rep *Report, name string) (SurfaceCoverage, bool) {
	for _, s := range rep.Surfaces {
		if s.Surface == name {
			return s, true
		}
	}
	return SurfaceCoverage{}, false
}

func TestBuildReport_PerSurfacePercentages(t *testing.T) {
	files, auth := sampleInventory()
	rep := BuildReport(files, auth)

	// repository surface: 2 files, 1 covered -> 50%
	if s, ok := surfaceByName(rep, SurfaceRepository); !ok || s.TotalFiles != 2 || s.CoveredFiles != 1 || s.Percent != 50 {
		t.Errorf("repository surface = %+v, want total=2 covered=1 percent=50", s)
	}
	// rbac surface: 2 files, 1 covered -> 50%
	if s, ok := surfaceByName(rep, SurfaceRbac); !ok || s.TotalFiles != 2 || s.CoveredFiles != 1 || s.Percent != 50 {
		t.Errorf("rbac surface = %+v, want total=2 covered=1 percent=50", s)
	}
	// critical surface: all four server files (repo+rbac), 2 covered -> 50%
	if s, ok := surfaceByName(rep, SurfaceCritical); !ok || s.TotalFiles != 4 || s.CoveredFiles != 2 || s.Percent != 50 {
		t.Errorf("critical surface = %+v, want total=4 covered=2 percent=50", s)
	}
	// low-risk helper: echo + the _test.go -> 2 files, 0 covered
	if s, ok := surfaceByName(rep, SurfaceLowRiskHelper); !ok || s.TotalFiles != 2 || s.CoveredFiles != 0 {
		t.Errorf("low_risk_helper surface = %+v, want total=2 covered=0", s)
	}
}

func TestBuildReport_UnknownHighRiskFiles(t *testing.T) {
	files, auth := sampleInventory()
	rep := BuildReport(files, auth)

	want := []string{
		"golang/rbac/rbac_server/rbac_deny.go",
		"golang/repository/repository_server/resolver.go",
	}
	if !reflect.DeepEqual(rep.UnknownHighRiskFiles, want) {
		t.Errorf("UnknownHighRiskFiles = %v, want %v (sorted, high/critical with no anchor)", rep.UnknownHighRiskFiles, want)
	}
	if rep.UnknownHighRiskCount != 2 {
		t.Errorf("UnknownHighRiskCount = %d, want 2", rep.UnknownHighRiskCount)
	}
	// The unanchored echo + test files are NOT high-risk, so excluded.
	for _, f := range rep.UnknownHighRiskFiles {
		if f == "golang/echo/echo_server/echo.go" || f == "golang/util/strings_test.go" {
			t.Errorf("low-risk file %q must not appear in unknown high-risk list", f)
		}
	}
}

// The report must be deterministic: identical inputs -> identical output.
func TestBuildReport_Deterministic(t *testing.T) {
	files, auth := sampleInventory()
	a := BuildReport(files, auth)
	b := BuildReport(files, auth)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("BuildReport not deterministic:\n a=%+v\n b=%+v", a, b)
	}
}

// Weighted overall reflects risk: covering the critical files moves the
// headline far more than covering helpers.
func TestBuildReport_WeightedOverallReflectsRisk(t *testing.T) {
	auth := []string{"golang/repository/repository_server/"}
	critical := []FileCoverage{{Path: "golang/repository/repository_server/a.go", HasDirectAnchor: true}}
	helper := []FileCoverage{{Path: "golang/util/a.go", HasDirectAnchor: true}}

	repCritical := BuildReport(append(critical, FileCoverage{Path: "golang/util/b.go", HasDirectAnchor: false}), auth)
	repHelper := BuildReport(append(helper, FileCoverage{Path: "golang/repository/repository_server/b.go", HasDirectAnchor: false}), auth)

	// Same 1-of-2 file coverage, but covering the critical file yields a higher
	// risk-weighted percentage than covering the helper.
	if !(repCritical.WeightedOverallPercent > repHelper.WeightedOverallPercent) {
		t.Errorf("covering critical (%d%%) should outweigh covering helper (%d%%)",
			repCritical.WeightedOverallPercent, repHelper.WeightedOverallPercent)
	}
}

func TestBuildReport_WeightedOverallIgnoresHelperGeneratedFiles(t *testing.T) {
	rep := BuildReport([]FileCoverage{
		{Path: "golang/echo/echo_server/handlers.go", HasDirectAnchor: true},
		{Path: "golang/echo/echo_server/handlers_test.go", HasDirectAnchor: false},
		{Path: "golang/echo/echopb/echo.pb.go", HasDirectAnchor: false},
		{Path: "golang/echo/echo_server/zz_version_generated.go", HasDirectAnchor: false},
	}, nil)

	if rep.WeightedOverallPercent != 100 {
		t.Fatalf("WeightedOverallPercent=%d want 100; helper/generated files should not depress production coverage", rep.WeightedOverallPercent)
	}
	s, ok := surfaceByName(rep, SurfaceLowRiskHelper)
	if !ok {
		t.Fatalf("missing %s surface", SurfaceLowRiskHelper)
	}
	if s.TotalFiles != 4 || s.CoveredFiles != 1 {
		t.Fatalf("low_risk_helper surface = %+v, want total=4 covered=1", s)
	}
}
