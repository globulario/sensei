// SPDX-License-Identifier: Apache-2.0

package coverage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	yaml "gopkg.in/yaml.v3"
)

// repoRootForHighRisk walks up from the test's working directory until it finds
// docs/awareness/high_risk_files.yaml, so the test reads the repository's ACTUAL
// self-governance registry rather than a fixture.
func repoRootForHighRisk(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "docs", "awareness", "high_risk_files.yaml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repo root (docs/awareness/high_risk_files.yaml)")
		}
		dir = parent
	}
}

// requiresBriefing mirrors the .claude/hooks/enforce-briefing.sh prefix match
// (`[[ "$REL_PATH" == "$prefix"* ]]`): an edit requires a briefing when its path
// is at or under a listed high-risk prefix.
func requiresBriefing(prefixes []string, relPath string) bool {
	rel := filepath.ToSlash(relPath)
	for _, p := range prefixes {
		if strings.HasPrefix(rel, filepath.ToSlash(strings.TrimSpace(p))) {
			return true
		}
	}
	return false
}

// TestHighRiskRegistryCoversAuthoritySurface proves Sensei's self-governance is
// STRUCTURAL, not a convention: an edit to any core authority-surface path is
// covered by docs/awareness/high_risk_files.yaml, so the enforce-briefing hook
// requires a briefing before the edit (and the graph records a guardrail
// `protects` edge). It also proves the registry is not blanket "everything is
// high-risk" — proximity is not authority — so a non-load-bearing path is NOT
// forced through the gate, which would train the reader to ignore it.
func TestHighRiskRegistryCoversAuthoritySurface(t *testing.T) {
	root := repoRootForHighRisk(t)
	data, err := os.ReadFile(filepath.Join(root, "docs", "awareness", "high_risk_files.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Files []string `yaml:"files"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Files) == 0 {
		t.Fatal("high_risk_files.yaml is empty — the self-governance registry is not structural")
	}

	// Every core authority surface must require a briefing before an edit.
	mustBrief := []string{
		"golang/architecture/tasksession/control.go",    // advance-task owner
		"golang/architecture/ledger/append.go",          // append-only authority
		"golang/architecture/admission/decision_v2.go",  // admission v2 decision
		"golang/architecture/resultrecording/record.go", // result recording boundary
		"cmd/awg/main.go",                         // CLI / authority command surface
		"golang/server/risk_classify.go",          // the enforcer that reads the sources
		"docs/awareness/invariants.yaml",          // governed invariants
		"docs/awareness/authority_grants.yaml",    // governed authority
		"docs/awareness/delegation_policies.yaml", // governed delegation
		"docs/awareness/high_risk_files.yaml",     // the registry governs itself
	}
	for _, p := range mustBrief {
		if !requiresBriefing(doc.Files, p) {
			t.Errorf("authority-surface path %q is NOT covered by high_risk_files.yaml — an edit would skip the briefing gate", p)
		}
	}

	// Proximity is not authority: non-load-bearing paths must not be forced
	// through briefing, or the gate becomes noise the reader learns to dismiss.
	wontBrief := []string{"README.md", "docs/skill-ingestion.md", "LICENSE"}
	for _, p := range wontBrief {
		if requiresBriefing(doc.Files, p) {
			t.Errorf("non-authority path %q is marked high-risk — the registry is over-broad", p)
		}
	}
}
