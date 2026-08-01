// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/dashboardprojection"
)

// repoRootForTest resolves this checkout's root the same way the CLI's
// resolveProjectRoot default would from within this package's directory.
func repoRootForTest(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// TestBuildDashboardProjectionOnRealRepo builds a projection from this
// repository's own real, committed awareness corpus (not a synthetic
// fixture) and requires it to pass every producer-side validation rule.
// checkEmbeddedSeedFreshness degrades to an honest "unknown" when
// sensei/awg is not resolvable on PATH during `go test` (no binary has been
// built yet in that context) rather than failing the build — that lane is
// separately proven correct by hand against `sensei rebuild --check` (see
// PR description).
// TestCheckEmbeddedSeedFreshnessRefusesNonCLIExecutable proves the
// os.Executable() guard actually blocks the dangerous case: under `go test`,
// os.Executable() resolves to this package's test binary, not the sensei
// CLI. Re-invoking it as "rebuild --check" would run this entire test suite
// recursively as a subprocess (observed directly during development: each
// invocation took ~40s and spawned further nested recursive runs before a
// hard timeout killed the process tree). This test must return an error
// quickly and must not spawn a subprocess.
func TestCheckEmbeddedSeedFreshnessRefusesNonCLIExecutable(t *testing.T) {
	repoRoot := repoRootForTest(t)
	start := time.Now()
	fresh, detail, err := checkEmbeddedSeedFreshness(repoRoot)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected an error (current executable is a go test binary, not sensei/awg), got fresh=%v detail=%q", fresh, detail)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("checkEmbeddedSeedFreshness took %s; expected an immediate refusal, not a subprocess invocation", elapsed)
	}
}

func TestBuildDashboardProjectionOnRealRepo(t *testing.T) {
	repoRoot := repoRootForTest(t)
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)

	proj, err := buildDashboardProjection(repoRoot, false, now)
	if err != nil {
		t.Fatalf("buildDashboardProjection: %v", err)
	}

	if errs := dashboardprojection.Validate(proj); len(errs) != 0 {
		t.Fatalf("Validate: %v", errs)
	}

	if len(proj.Components) == 0 {
		t.Fatal("expected at least one authored component from docs/awareness/architecture/components.yaml")
	}
	if len(proj.Regions) != 1 || proj.Regions[0].ID != ungroupedRegionID {
		t.Fatalf("expected exactly the one synthetic ungrouped region, got %+v", proj.Regions)
	}
	if len(proj.Flows) != 0 {
		t.Fatalf("expected no flows (contract_realizations.yaml is currently empty), got %d", len(proj.Flows))
	}
	if proj.Assessments.ArchitectureHealth.State != dashboardprojection.StateUnknown {
		t.Fatalf("expected architecture_health to stay honestly unknown in this producer version, got %s", proj.Assessments.ArchitectureHealth.State)
	}
	for _, c := range proj.Components {
		if c.RegionRef != ungroupedRegionID {
			t.Errorf("component %q region_ref = %q, want %q", c.ID, c.RegionRef, ungroupedRegionID)
		}
	}
}

// TestBuildDashboardProjectionNeverClaimsAvailableGivenKnownGaps proves this
// producer version cannot accidentally emit availability.state: available.
// Regions, flows, and architecture_health are material parts of the V1
// architectural view; this producer has no authored source for any of the
// three yet, so a projection it builds can only ever be honestly partial —
// not decorative metadata, but a fact enforced here.
func TestBuildDashboardProjectionNeverClaimsAvailableGivenKnownGaps(t *testing.T) {
	repoRoot := repoRootForTest(t)
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)

	proj, err := buildDashboardProjection(repoRoot, false, now)
	if err != nil {
		t.Fatal(err)
	}
	if proj.Availability.State != dashboardprojection.Partial {
		t.Fatalf("availability.state = %s, want partial (regions/flows/health are known-incomplete)", proj.Availability.State)
	}

	bySourceOwner := map[string]dashboardprojection.SourceState{}
	for _, s := range proj.Availability.Sources {
		bySourceOwner[s.Owner] = s
	}
	for _, owner := range []string{"regions", "flows", "architecture_health"} {
		s, ok := bySourceOwner[owner]
		if !ok {
			t.Fatalf("expected availability.sources to individually report %q", owner)
		}
		if s.Availability == dashboardprojection.Available {
			t.Fatalf("source %q reported as available, want partial/unavailable", owner)
		}
	}
}

// TestBuildDashboardProjectionIsDeterministic builds the same repository
// twice with a fixed clock and requires byte-identical content once the only
// intentionally time-varying field (generated_at) is excluded, matching the
// canonical Digest() convention.
func TestBuildDashboardProjectionIsDeterministic(t *testing.T) {
	repoRoot := repoRootForTest(t)
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)

	p1, err := buildDashboardProjection(repoRoot, false, now)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := buildDashboardProjection(repoRoot, false, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	d1, err := dashboardprojection.Digest(p1)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := dashboardprojection.Digest(p2)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatalf("two builds of the same revision digested differently: %s vs %s", d1, d2)
	}
}

// TestBuildDashboardProjectionPublicModeRedactsByDefault confirms the
// public-snapshot path never emits active_context (this producer never sets
// one, so ValidatePublicRedaction must always pass trivially) and reports
// the export-only handoff capability, never live.
func TestBuildDashboardProjectionPublicModeRedactsByDefault(t *testing.T) {
	repoRoot := repoRootForTest(t)
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)

	proj, err := buildDashboardProjection(repoRoot, true, now)
	if err != nil {
		t.Fatal(err)
	}
	if errs := dashboardprojection.ValidatePublicRedaction(proj); len(errs) != 0 {
		t.Fatalf("ValidatePublicRedaction: %v", errs)
	}
	if proj.Capabilities.AgentHandoff != dashboardprojection.HandoffExport {
		t.Fatalf("expected export capability in public mode, got %s", proj.Capabilities.AgentHandoff)
	}
}
