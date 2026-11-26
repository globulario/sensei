// SPDX-License-Identifier: AGPL-3.0-only

package coverage

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	yaml "gopkg.in/yaml.v3"
)

// INV-AUTHORITY-SEMANTIC-SCOPE, corpus half. Authority over a truth and
// authority over the code that implements the mechanism governing that truth
// are different relations. The task-truth authorities were authored as clones
// of the repository-mutation record, so they all claimed the source tree and
// every code edit appeared to have five competing owners (#389).

type scopedAuthority struct {
	ID               string   `yaml:"id"`
	Status           string   `yaml:"status"`
	TruthLayer       string   `yaml:"truth_layer"`
	OwnerService     string   `yaml:"owner_service"`
	CoversPaths      []string `yaml:"covers_paths"`
	OwnsState        []string `yaml:"owns_state"`
	ImplementedIn    []string `yaml:"implemented_in"`
	MayWriteRoleIDs  []string `yaml:"may_write_role_ids"`
	MustMutateViaIDs []string `yaml:"must_mutate_via_ids"`
}

func loadScopedAuthorities(t *testing.T) (string, []scopedAuthority) {
	t.Helper()
	root := repoRootForHighRisk(t)
	data, err := os.ReadFile(filepath.Join(root, "docs", "awareness", "authority_domains.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		AuthorityDomains []scopedAuthority `yaml:"authority_domains"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return root, doc.AuthorityDomains
}

// A task-truth authority declares the task truth it governs and claims no
// source path: its scope is a decision recorded in a task, not the files that
// implement the recording. Its implementing code must exist, so the briefing
// relation cannot silently point at nothing.
func TestTaskTruthAuthoritiesAreScopedToTheirTruthNotToSourcePaths(t *testing.T) {
	root, auths := loadScopedAuthorities(t)
	n := 0
	for _, a := range auths {
		if a.Status != "active" || a.TruthLayer != "task" {
			continue
		}
		n++
		if len(a.CoversPaths) > 0 {
			t.Errorf("%s (task truth) covers source paths %v: that claims authority over the code, not the truth", a.ID, a.CoversPaths)
		}
		if len(a.OwnsState) == 0 {
			t.Errorf("%s (task truth) declares no owns_state: its governed truth is unnamed", a.ID)
		}
		if len(a.ImplementedIn) == 0 {
			t.Errorf("%s declares no implemented_in: agents editing its enforcement would get no warning", a.ID)
		}
		for _, p := range a.ImplementedIn {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err != nil {
				t.Errorf("%s implemented_in %q does not exist", a.ID, p)
			}
		}
	}
	if n < 4 {
		t.Fatalf("found %d active task-truth authorities, want at least the four reconciled ones", n)
	}
}

// A shared storage location does not imply shared decision authority, but two
// authorities that govern the SAME truth with different owners, writers or
// mutation paths are a genuine contradiction and must never be authored.
func TestNoTwoAuthoritiesGovernTheSameTruthDifferently(t *testing.T) {
	_, auths := loadScopedAuthorities(t)
	facts := func(a scopedAuthority) string {
		w := append([]string(nil), a.MayWriteRoleIDs...)
		m := append([]string(nil), a.MustMutateViaIDs...)
		sort.Strings(w)
		sort.Strings(m)
		return a.OwnerService + "|" + strings.Join(w, ",") + "|" + strings.Join(m, ",")
	}
	byState := map[string]scopedAuthority{}
	for _, a := range auths {
		if a.Status != "active" {
			continue
		}
		for _, s := range a.OwnsState {
			if prev, ok := byState[s]; ok && facts(prev) != facts(a) {
				t.Errorf("%s and %s both govern %q with different authority facts", prev.ID, a.ID, s)
			}
			byState[s] = a
		}
	}
}

// The five authorities of the separation ruling stay independently
// enforceable: each is authorized by its own grant, and no grant authorizes two
// of them, so holding one never grants another.
func TestEachSeparatedAuthorityHasItsOwnGrant(t *testing.T) {
	root := repoRootForHighRisk(t)
	data, err := os.ReadFile(filepath.Join(root, "docs", "awareness", "authority_grants.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		AuthorityGrants []struct {
			ID                 string   `yaml:"id"`
			Status             string   `yaml:"status"`
			AuthorityDomainIDs []string `yaml:"authority_domain_ids"`
		} `yaml:"authority_grants"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	separated := map[string]bool{
		"authority.sensei_repository_mutation":               true,
		"authority.sensei_question_disposition":              true,
		"authority.sensei_question_resolution_certification": true,
		"authority.sensei_terminal_completion":               true,
		"authority.sensei_terminal_abandonment":              true,
	}
	granted := map[string]int{}
	for _, g := range doc.AuthorityGrants {
		if g.Status != "active" {
			continue
		}
		var mine []string
		for _, d := range g.AuthorityDomainIDs {
			if separated[d] {
				mine = append(mine, d)
				granted[d]++
			}
		}
		if len(mine) > 1 {
			t.Errorf("grant %s authorizes %v together: one authority would grant another", g.ID, mine)
		}
	}
	for d := range separated {
		if granted[d] == 0 {
			t.Errorf("%s has no active grant of its own", d)
		}
	}
}
