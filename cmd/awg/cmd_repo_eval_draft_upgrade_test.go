// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"strings"
	"testing"

	"github.com/globulario/awareness-graph/golang/repoeval"
)

func TestRenderRepoEvalUpgradeDraft_StaysInCandidatesAndCarriesTodo(t *testing.T) {
	draft, err := renderRepoEvalUpgradeDraft("/repo", "guarded_repair_only", repoeval.UpgradeCandidate{
		ID:            "component.cmd.caddy",
		Kind:          "contract",
		Title:         "Contract for component.cmd.caddy",
		Rationale:     "entrypoint contract",
		SuggestedFile: "docs/intent/component.cmd.caddy.yaml",
		Paths:         []string{"cmd/caddy/main.go"},
	})
	if err != nil {
		t.Fatalf("renderRepoEvalUpgradeDraft: %v", err)
	}
	if !strings.HasPrefix(draft.Path, "docs/awareness/candidates/repo_eval_upgrade/") {
		t.Fatalf("draft path=%q must stay under awareness candidates", draft.Path)
	}
	if !strings.Contains(draft.Content, "do_not_auto_promote: true") {
		t.Fatalf("draft must carry anti-drift promotion guard:\n%s", draft.Content)
	}
	if !strings.Contains(draft.Content, "discovered_from: repo-eval upgrade_path") {
		t.Fatalf("draft missing provenance:\n%s", draft.Content)
	}
	if !strings.Contains(draft.Content, "missing_fields:") || !strings.Contains(draft.Content, "required_tests") {
		t.Fatalf("draft must leave semantic fields explicitly missing:\n%s", draft.Content)
	}
	if strings.Contains(draft.Content, "status: active") {
		t.Fatalf("draft leaked live authority status:\n%s", draft.Content)
	}
}

func TestBuildRepoEvalUpgradeDrafts_UsesUpgradePathOnly(t *testing.T) {
	rep := repoeval.Report{
		AgentReadiness: repoeval.AgentReadiness{Verdict: "guarded_repair_only"},
		UpgradePath: repoeval.UpgradePath{
			Invariants: []repoeval.UpgradeCandidate{{
				ID:            "invariant.cmd.caddy",
				Kind:          "invariant",
				Title:         "Protect component.cmd.caddy",
				Rationale:     "entrypoint",
				SuggestedFile: "docs/awareness/invariants.yaml",
				Paths:         []string{"cmd/caddy/main.go"},
			}},
			Contracts: []repoeval.UpgradeCandidate{{
				ID:            "component.cmd.caddy",
				Kind:          "contract",
				Title:         "Contract for component.cmd.caddy",
				Rationale:     "entrypoint contract",
				SuggestedFile: "docs/intent/component.cmd.caddy.yaml",
				Paths:         []string{"cmd/caddy/main.go"},
			}},
		},
	}
	drafts, err := buildRepoEvalUpgradeDrafts("/repo", rep)
	if err != nil {
		t.Fatalf("buildRepoEvalUpgradeDrafts: %v", err)
	}
	if len(drafts) != 2 {
		t.Fatalf("draft count=%d want 2", len(drafts))
	}
	for _, draft := range drafts {
		if !strings.Contains(draft.Content, "repo_eval_verdict: guarded_repair_only") {
			t.Fatalf("draft missing verdict evidence:\n%s", draft.Content)
		}
		if draft.Kind == "contract" && draft.Target != "docs/intent/component.cmd.caddy.yaml" {
			t.Fatalf("contract target=%q", draft.Target)
		}
	}
}
