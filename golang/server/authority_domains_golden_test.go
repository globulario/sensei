// SPDX-License-Identifier: Apache-2.0

package main

// Golden usefulness tests for the Phase-3 authority domains. Like the
// implementation-pattern golden tests, these load the REAL authored corpus
// (services/docs/awareness/authority_domains.yaml) so the tests fail when the
// authored covers_paths drift away from the directories agents actually edit.
//
// Doc acceptance, verbatim:
//   - repository_server edit task returns repository authority guidance
//   - cluster_doctor_server/rules/* edit task returns diagnostic-only guidance
//   - RBAC edit task returns deny/permission owner guidance
//   - Preflight must not claim authority guidance when no domain matches

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/awareness-graph/golang/extractor"
	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
	"github.com/globulario/awareness-graph/golang/rdf"
	"github.com/globulario/awareness-graph/golang/store"
)

var goldenAuthorityCases = []struct {
	name string
	task string
	file string
	// wantActionContains entries must each appear inside some required_action.
	wantActionContains []string
	// wantBypassContains entries must each appear inside some forbidden_fix.
	wantBypassContains []string
}{
	{
		name: "repository publish edit",
		task: "change repository publish workflow installability behavior",
		file: "golang/repository/repository_server/publish_workflow.go",
		wantActionContains: []string{
			"Authority [Repository artifact metadata]: state owner is repository",
			"mutate only via repository typed RPC / publish workflow",
		},
		wantBypassContains: []string{
			"object presence in MinIO or CAS as installability authority",
		},
	},
	{
		name: "doctor rule edit",
		task: "add a new cluster-doctor rule that detects stale repository findings",
		file: "golang/cluster_doctor/cluster_doctor_server/rules/repository_findings.go",
		wantActionContains: []string{
			"Authority [Remediation execution]: state owner is workflow service",
			"evidence freshness",
		},
		wantBypassContains: []string{
			"doctor rule executing mutation inside Evaluate",
			"auto-executing hard-blocked actions (ETCD_PUT, ETCD_DELETE, NODE_REMOVE)",
		},
	},
	{
		name: "rbac access edit",
		task: "change RBAC access validation",
		file: "golang/rbac/rbac_server/rbac_access.go",
		wantActionContains: []string{
			"Authority [RBAC permissions]: state owner is rbac",
		},
		wantBypassContains: []string{
			"owner / service-account / bootstrap shortcuts that skip the explicit-deny check",
		},
	},
}

func TestPreflightGolden_AuthorityDomainsSurfaceForCoveredFiles(t *testing.T) {
	facts := loadCorpusAuthorityFacts(t)

	for _, tc := range goldenAuthorityCases {
		t.Run(tc.name, func(t *testing.T) {
			s := newAuthorityTestServer(t, facts)
			resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
				Task:  tc.task,
				Files: []string{tc.file},
				Mode:  awarenesspb.PreflightMode_PREFLIGHT_STANDARD,
			})
			if err != nil {
				t.Fatalf("Preflight: %v", err)
			}
			for _, want := range tc.wantActionContains {
				if !anyContains(resp.GetRequiredActions(), want) {
					t.Errorf("required_actions missing %q\ngot: %v", want, resp.GetRequiredActions())
				}
			}
			for _, want := range tc.wantBypassContains {
				if !anyContains(resp.GetForbiddenFixes(), want) {
					t.Errorf("forbidden_fixes missing %q\ngot: %v", want, resp.GetForbiddenFixes())
				}
			}
		})
	}
}

// The negative case: a file outside every covers_paths prefix must produce
// ZERO authority lines — claiming ownership guidance where no domain matches
// would teach agents to distrust the guidance everywhere.
func TestPreflightGolden_NoAuthorityGuidanceWithoutDomainMatch(t *testing.T) {
	facts := loadCorpusAuthorityFacts(t)
	s := newAuthorityTestServer(t, facts)

	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "tweak echo handler logging",
		Files: []string{"golang/echo/echo_server/echo.go"},
		Mode:  awarenesspb.PreflightMode_PREFLIGHT_STANDARD,
	})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	for _, line := range resp.GetRequiredActions() {
		if strings.HasPrefix(line, "Authority [") {
			t.Errorf("unmatched file surfaced authority action: %q", line)
		}
	}
	for _, line := range resp.GetForbiddenFixes() {
		if strings.HasPrefix(line, "Authority bypass forbidden [") {
			t.Errorf("unmatched file surfaced authority bypass: %q", line)
		}
	}
}

// Longest-prefix wins: a collector file sits under BOTH the remediation
// domain (cluster_doctor_server/) and the runtime-evidence domain
// (cluster_doctor_server/collector/). The more specific domain must win so
// collector edits get evidence-freshness guidance, not remediation gating.
func TestPreflightGolden_LongestPrefixSelectsMostSpecificDomain(t *testing.T) {
	facts := loadCorpusAuthorityFacts(t)
	s := newAuthorityTestServer(t, facts)

	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "extend the doctor snapshot collector",
		Files: []string{"golang/cluster_doctor/cluster_doctor_server/collector/snapshot.go"},
		Mode:  awarenesspb.PreflightMode_PREFLIGHT_STANDARD,
	})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if !anyContains(resp.GetRequiredActions(), "Authority [Runtime evidence]") {
		t.Errorf("collector file should surface the runtime-evidence domain\ngot: %v", resp.GetRequiredActions())
	}
	if anyContains(resp.GetRequiredActions(), "Authority [Remediation execution]") {
		t.Errorf("collector file surfaced the broader remediation domain despite a more specific match\ngot: %v", resp.GetRequiredActions())
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func newAuthorityTestServer(t *testing.T, authorityFacts []store.ImpactFact) *server {
	t.Helper()
	invalidateImplementationPatternCacheForTest()
	invalidateAuthorityDomainCacheForTest()
	return newServer(fakeStore{
		classFacts: func(_ context.Context, classIRI string, _ int) ([]store.ImpactFact, error) {
			if classIRI == rdf.ClassAuthorityDomain {
				return authorityFacts, nil
			}
			return nil, nil
		},
	})
}

func anyContains(list []string, substr string) bool {
	for _, s := range list {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

// loadCorpusAuthorityFacts imports the authored authority_domains.yaml from
// the sibling services repo into ImpactFact rows. Skips when not resolvable.
func loadCorpusAuthorityFacts(t *testing.T) []store.ImpactFact {
	t.Helper()
	path := resolveServicesAuthorityDomainsFile()
	if path == "" {
		t.Skip("services authority_domains.yaml not resolvable; set SERVICES_REPO to run")
	}
	// ImportAwarenessDir walks a directory; isolate the single corpus file so
	// no other services YAML is parsed.
	dir := t.TempDir()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "authority_domains.yaml"), data, 0o644); err != nil {
		t.Fatalf("stage corpus: %v", err)
	}
	var buf bytes.Buffer
	if _, _, err := extractor.ImportAwarenessDir(dir, &buf); err != nil {
		t.Fatalf("ImportAwarenessDir: %v", err)
	}
	facts := parseCorpusNTFacts(buf.String(), rdf.ClassAuthorityDomain)
	if len(facts) == 0 {
		t.Fatalf("no authority facts parsed from %s", path)
	}
	return facts
}

func resolveServicesAuthorityDomainsFile() string {
	var cands []string
	if r := os.Getenv("SERVICES_REPO"); r != "" {
		cands = append(cands, filepath.Join(r, "docs", "awareness", "authority_domains.yaml"))
	}
	cands = append(cands,
		filepath.Join("..", "..", "..", "services", "docs", "awareness", "authority_domains.yaml"),
	)
	for _, c := range cands {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	return ""
}
