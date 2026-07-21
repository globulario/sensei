// SPDX-License-Identifier: AGPL-3.0-only

// Package coverage holds the pure, importable risk-weighting and coverage-report
// logic shared by the awareness-graph server (Preflight) and the offline
// cmd/coverage-report tool. It does no I/O and no graph access — callers feed
// it file inventories and authority-domain coversPath prefixes.
//
// Raw file coverage is misleading: a missing anchor in a critical authority
// file matters far more than one in a helper test file. This package tells
// those apart so the number that matters (critical-surface coverage) is not
// diluted by easy wins.
package coverage

import "strings"

// RiskTier ranks a file's blast radius.
type RiskTier int

const (
	RiskLow RiskTier = iota
	RiskHigh
	RiskCritical
)

func (t RiskTier) String() string {
	switch t {
	case RiskCritical:
		return "critical"
	case RiskHigh:
		return "high"
	default:
		return "low"
	}
}

// Named coverage surfaces. A file may belong to several at once (a
// repository_server file is repository + authority + high_risk + critical).
const (
	SurfaceCritical      = "critical"
	SurfaceHighRisk      = "high_risk"
	SurfaceAuthority     = "authority"
	SurfaceRemediation   = "remediation"
	SurfaceRepository    = "repository"
	SurfaceRbac          = "rbac"
	SurfaceLowRiskHelper = "low_risk_helper"
)

// HighRiskDirPrefixes mirror CLAUDE.md's awareness-required directories. The
// server's risk classifier shares this exact slice so the two stay in lockstep.
var HighRiskDirPrefixes = []string{
	"golang/node_agent/",
	"golang/cluster_controller/",
	"golang/repository/",
	"golang/rbac/",
	"golang/security/",
	"golang/cluster_doctor/",
	"golang/mcp/",
	"golang/services_manager/",
	"golang/ai_executor/",
}

// CriticalDirPrefixes are the high-risk directories that own critical state or
// security boundaries — a missing anchor here is the most expensive kind.
var CriticalDirPrefixes = []string{
	"golang/security/",
	"golang/rbac/",
	"golang/repository/",
	"golang/cluster_controller/",
	"golang/cluster_doctor/",
}

// Service-level path classes for the named per-surface report. Kept broad so
// the report is stable as files move within a service.
const (
	RepositorySurfacePrefix  = "golang/repository/"
	RbacSurfacePrefix        = "golang/rbac/"
	RemediationSurfacePrefix = "golang/cluster_doctor/"
)

// isHelperOrGenerated returns true for files that are not a production review
// surface — tests, generated code, vendored protobufs. Always low risk: a
// missing anchor on a _test.go does not mean a production invariant is
// unguarded.
func isHelperOrGenerated(path string) bool {
	switch {
	case strings.HasSuffix(path, "_test.go"):
		return true
	case strings.HasSuffix(path, ".pb.go"):
		return true
	case strings.HasSuffix(path, "_generated.go"):
		return true
	case strings.Contains(path, "/zz_"), strings.HasPrefix(path, "zz_"):
		return true
	}
	return false
}

func inAnyPrefix(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func normPath(path string) string {
	return strings.TrimPrefix(strings.TrimSpace(path), "./")
}

// FileRiskTier classifies a file's blast radius. Authority-domain membership
// raises a file to at least RiskHigh even outside the static high-risk
// directory list — ownership of cluster state is what makes a file dangerous.
func FileRiskTier(path string, authorityCoversPaths []string) RiskTier {
	path = normPath(path)
	if path == "" || isHelperOrGenerated(path) {
		return RiskLow
	}
	if inAnyPrefix(path, CriticalDirPrefixes) {
		return RiskCritical
	}
	if inAnyPrefix(path, HighRiskDirPrefixes) || inAnyPrefix(path, authorityCoversPaths) {
		return RiskHigh
	}
	return RiskLow
}

// FileWeight is the coverage weight a file contributes. Base weight comes from
// the risk tier; authority-domain membership adds a bonus so a file inside an
// authority domain weighs more than an equivalent one outside it (Phase 4
// acceptance: authority membership increases coverage weight).
func FileWeight(path string, authorityCoversPaths []string) int {
	base := 1
	switch FileRiskTier(path, authorityCoversPaths) {
	case RiskCritical:
		base = 4
	case RiskHigh:
		base = 2
	}
	if !isHelperOrGenerated(path) && inAnyPrefix(normPath(path), authorityCoversPaths) {
		base++
	}
	return base
}

// FileSurfaces returns the named coverage surfaces a file belongs to.
func FileSurfaces(path string, authorityCoversPaths []string) []string {
	path = normPath(path)
	if path == "" {
		return nil
	}
	if isHelperOrGenerated(path) {
		return []string{SurfaceLowRiskHelper}
	}

	var surfaces []string
	add := func(s string) { surfaces = append(surfaces, s) }

	switch FileRiskTier(path, authorityCoversPaths) {
	case RiskCritical:
		add(SurfaceCritical)
		add(SurfaceHighRisk)
	case RiskHigh:
		add(SurfaceHighRisk)
	}
	if inAnyPrefix(path, authorityCoversPaths) {
		add(SurfaceAuthority)
	}
	if strings.HasPrefix(path, RepositorySurfacePrefix) {
		add(SurfaceRepository)
	}
	if strings.HasPrefix(path, RbacSurfacePrefix) {
		add(SurfaceRbac)
	}
	if strings.HasPrefix(path, RemediationSurfacePrefix) {
		add(SurfaceRemediation)
	}
	if len(surfaces) == 0 {
		add(SurfaceLowRiskHelper)
	}
	return surfaces
}

// AnyPathInHighRiskDir reports whether any file is under a high-risk directory.
// Kept for the server risk classifier's existing rule semantics.
func AnyPathInHighRiskDir(files []string) bool {
	for _, f := range files {
		if inAnyPrefix(normPath(f), HighRiskDirPrefixes) {
			return true
		}
	}
	return false
}

// AnyFileHighRiskWeighted reports whether any file is above RiskLow under the
// weighted classification — high-risk directory OR authority-domain membership.
// This is what Preflight's honest-DEGRADED gate uses instead of a bare
// directory check, so authority membership raises severity and helper/test
// files do not.
func AnyFileHighRiskWeighted(files []string, authorityCoversPaths []string) bool {
	for _, f := range files {
		if FileRiskTier(f, authorityCoversPaths) != RiskLow {
			return true
		}
	}
	return false
}
