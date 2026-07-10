// SPDX-License-Identifier: Apache-2.0

package coverage

import (
	"reflect"
	"testing"
)

func TestFileRiskTier(t *testing.T) {
	// A synthetic authority domain covering a directory that is NOT in the
	// static high-risk list, to prove authority membership alone raises tier.
	authCovers := []string{"golang/echo/"}

	cases := []struct {
		name string
		path string
		want RiskTier
		auth []string
	}{
		{"critical dir", "golang/repository/repository_server/publish_workflow.go", RiskCritical, nil},
		{"critical dir rbac", "golang/rbac/rbac_server/rbac_access.go", RiskCritical, nil},
		{"high dir node_agent", "golang/node_agent/node_agent_server/heartbeat.go", RiskHigh, nil},
		{"high dir mcp", "golang/mcp/mcp_server/tools.go", RiskHigh, nil},
		{"authority-only raises to high", "golang/echo/echo_server/echo.go", RiskHigh, authCovers},
		{"same path low without authority", "golang/echo/echo_server/echo.go", RiskLow, nil},
		{"test file in critical dir is low", "golang/repository/repository_server/resolver_test.go", RiskLow, nil},
		{"generated file is low", "golang/repository/repository_server/zz_version_generated.go", RiskLow, nil},
		{"pb.go is low", "golang/repository/repositorypb/repository.pb.go", RiskLow, nil},
		{"plain helper is low", "golang/util/strings.go", RiskLow, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FileRiskTier(tc.path, tc.auth); got != tc.want {
				t.Errorf("FileRiskTier(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// Authority-domain membership must increase a file's coverage weight — the
// Phase 4 acceptance property.
func TestFileWeight_AuthorityMembershipIncreasesWeight(t *testing.T) {
	path := "golang/echo/echo_server/echo.go"
	without := FileWeight(path, nil)
	with := FileWeight(path, []string{"golang/echo/"})
	if !(with > without) {
		t.Errorf("authority membership did not increase weight: with=%d without=%d", with, without)
	}
}

func TestFileSurfaces(t *testing.T) {
	cases := []struct {
		path string
		auth []string
		want []string
	}{
		{
			path: "golang/repository/repository_server/publish_workflow.go",
			auth: []string{"golang/repository/repository_server/"},
			want: []string{SurfaceCritical, SurfaceHighRisk, SurfaceAuthority, SurfaceRepository},
		},
		{
			path: "golang/rbac/rbac_server/rbac_access.go",
			auth: nil,
			want: []string{SurfaceCritical, SurfaceHighRisk, SurfaceRbac},
		},
		{
			path: "golang/cluster_doctor/cluster_doctor_server/rules/foo.go",
			auth: nil,
			want: []string{SurfaceCritical, SurfaceHighRisk, SurfaceRemediation},
		},
		{
			path: "golang/echo/echo_server/echo_test.go",
			auth: nil,
			want: []string{SurfaceLowRiskHelper},
		},
		{
			path: "golang/util/strings.go",
			auth: nil,
			want: []string{SurfaceLowRiskHelper},
		},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got := FileSurfaces(tc.path, tc.auth)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("FileSurfaces(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestAnyFileHighRiskWeighted(t *testing.T) {
	if !AnyFileHighRiskWeighted([]string{"golang/util/x.go", "golang/rbac/rbac_server/a.go"}, nil) {
		t.Error("expected true when a high-risk file is present")
	}
	if AnyFileHighRiskWeighted([]string{"golang/util/x.go", "golang/echo/echo_server/e.go"}, nil) {
		t.Error("expected false for only low-risk files")
	}
	if !AnyFileHighRiskWeighted([]string{"golang/echo/echo_server/e.go"}, []string{"golang/echo/"}) {
		t.Error("expected true when authority domain covers the file")
	}
}
