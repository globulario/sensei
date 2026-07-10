// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"gopkg.in/yaml.v3"
)

type skillSignalPatternYAML struct {
	ID                 string   `yaml:"id"`
	Label              string   `yaml:"label"`
	Status             string   `yaml:"status"`
	Rationale          string   `yaml:"rationale"`
	WhenToUse          []string `yaml:"when_to_use"`
	MustFollow         []string `yaml:"must_follow"`
	RequiredCalls      []string `yaml:"required_calls"`
	ForbiddenCalls     []string `yaml:"forbidden_calls"`
	ForbiddenShortcuts []string `yaml:"forbidden_shortcuts"`
	ReferenceFiles     []struct {
		Path string `yaml:"path"`
		Role string `yaml:"role"`
	} `yaml:"reference_files"`
}

type skillSignalCase struct {
	id          string
	task        string
	expectedTop []string
	allowed     []string
	forbidden   []string
	maxPatterns int
	minScore    int
}

func TestBriefing_ImportedSkillSignalQuality(t *testing.T) {
	patterns := loadPromotedImportedSkillPatterns(t)
	cases := []skillSignalCase{
		{
			id:          "skill_signal_debug_flaky_test",
			task:        "debug a flaky test in skill ingestion and build a red-capable reproduction loop",
			expectedTop: []string{"imported.skill.engineering.diagnosing_bugs"},
			allowed:     []string{"imported.skill.engineering.tdd", "imported.skill.engineering.code_review"},
			forbidden:   []string{"imported.skill.productivity.", "imported.skill.personal.", "imported.skill.engineering.ask_matt"},
			maxPatterns: 3,
			minScore:    2,
		},
		{
			id:          "skill_signal_tdd_yaml_rendering",
			task:        "write a TDD regression for YAML rendering through public interfaces",
			expectedTop: []string{"imported.skill.engineering.tdd"},
			allowed:     []string{"imported.skill.engineering.diagnosing_bugs", "imported.skill.engineering.code_review"},
			forbidden:   []string{"imported.skill.productivity.", "imported.skill.personal."},
			maxPatterns: 3,
			minScore:    2,
		},
		{
			id:          "skill_signal_review_patch",
			task:        "review the skill ingestion patch against the spec and repository coding standards",
			expectedTop: []string{"imported.skill.engineering.code_review"},
			allowed:     []string{"imported.skill.engineering.tdd", "imported.skill.engineering.implement"},
			forbidden:   []string{"imported.skill.productivity.", "imported.skill.personal."},
			maxPatterns: 3,
			minScore:    2,
		},
		{
			id:          "skill_signal_design_promotion_workflow",
			task:        "design a promotion workflow for implementation pattern candidates",
			expectedTop: []string{"imported.skill.engineering.codebase_design"},
			allowed:     []string{"imported.skill.engineering.domain_modeling", "imported.skill.engineering.to_spec"},
			forbidden:   []string{"imported.skill.productivity.", "imported.skill.personal."},
			maxPatterns: 3,
			minScore:    2,
		},
		{
			id:          "skill_signal_merge_conflict",
			task:        "resolve a merge conflict in generated awareness YAML",
			expectedTop: []string{"imported.skill.engineering.resolving_merge_conflicts"},
			allowed:     []string{"imported.skill.engineering.code_review"},
			forbidden:   []string{"imported.skill.productivity.", "imported.skill.personal."},
			maxPatterns: 3,
			minScore:    2,
		},
		{
			id:          "skill_signal_research_candidate_skip",
			task:        "research how candidate directories are skipped by the awareness importer",
			expectedTop: []string{"imported.skill.engineering.research"},
			allowed:     []string{"imported.skill.engineering.wayfinder"},
			forbidden:   []string{"imported.skill.productivity.", "imported.skill.personal."},
			maxPatterns: 3,
			minScore:    2,
		},
		{
			id:          "skill_signal_prototype_validator",
			task:        "prototype a new generated candidate validator for skill ingestion",
			expectedTop: []string{"imported.skill.engineering.prototype"},
			allowed:     []string{"imported.skill.engineering.implement", "imported.skill.engineering.tdd"},
			forbidden:   []string{"imported.skill.productivity.", "imported.skill.personal."},
			maxPatterns: 3,
			minScore:    2,
		},
		{
			id:          "skill_signal_negative_readme_rename",
			task:        "rename a variable in a README example",
			expectedTop: nil,
			allowed:     nil,
			forbidden:   []string{"imported.skill."},
			maxPatterns: 0,
			minScore:    0,
		},
	}

	positiveTotal := 0
	positiveCases := 0
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			got := matchPatternsForBriefing(tc.task, "", patterns)
			ids := matchedPatternBareIDs(got)
			score := scoreSkillSignal(ids, tc)
			if len(tc.expectedTop) > 0 {
				positiveTotal += score
				positiveCases++
			}
			if len(ids) > tc.maxPatterns {
				t.Fatalf("matched %d patterns, max %d: %v", len(ids), tc.maxPatterns, ids)
			}
			if score < tc.minScore {
				t.Fatalf("score=%d want >=%d; matched=%v", score, tc.minScore, ids)
			}
			for _, f := range tc.forbidden {
				for _, id := range ids {
					if strings.HasPrefix(id, f) {
						t.Fatalf("forbidden pattern %q matched via prefix %q; all matches=%v", id, f, ids)
					}
				}
			}
			if len(tc.expectedTop) > 0 {
				if len(ids) == 0 || !containsString(tc.expectedTop, ids[0]) {
					t.Fatalf("top match missing or wrong; want one of %v; all matches=%v", tc.expectedTop, ids)
				}
			}
		})
	}
	if positiveCases == 0 {
		t.Fatalf("no positive signal-quality cases ran")
	}
	if avg := float64(positiveTotal) / float64(positiveCases); avg < 2.0 {
		t.Fatalf("average positive signal score = %.2f, want >= 2.00", avg)
	}
}

func loadPromotedImportedSkillPatterns(t *testing.T) []loadedPattern {
	t.Helper()
	root := repoRootForServerTest(t)
	dir := filepath.Join(root, "docs", "awareness", "architecture", "patterns")
	entries, err := filepath.Glob(filepath.Join(dir, "ip_imported_skill_*.yaml"))
	if err != nil {
		t.Fatalf("glob imported skill patterns: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("no promoted imported skill patterns found under %s", dir)
	}
	sort.Strings(entries)
	var out []loadedPattern
	for _, path := range entries {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var p skillSignalPatternYAML
		if err := yaml.Unmarshal(raw, &p); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		if p.Status != "active" {
			continue
		}
		lp := loadedPattern{
			ID:                 p.ID,
			Domain:             "globular",
			Label:              p.Label,
			Status:             p.Status,
			Rationale:          p.Rationale,
			ActivationTriggers: p.WhenToUse,
			MustFollow:         p.MustFollow,
			RequiredCalls:      p.RequiredCalls,
			ForbiddenCalls:     p.ForbiddenCalls,
		}
		for _, ref := range p.ReferenceFiles {
			role := ref.Role
			if role == "" {
				role = "reference"
			}
			lp.ReferenceFiles = append(lp.ReferenceFiles, role+":"+ref.Path)
		}
		out = append(out, lp)
	}
	return out
}

func scoreSkillSignal(ids []string, tc skillSignalCase) int {
	score := 0
	if len(tc.expectedTop) > 0 && len(ids) > 0 && containsString(tc.expectedTop, ids[0]) {
		score += 2
	}
	for _, id := range ids {
		if containsString(tc.allowed, id) {
			score++
		}
		for _, f := range tc.forbidden {
			if strings.HasPrefix(id, f) {
				score--
			}
		}
	}
	if len(ids) > tc.maxPatterns {
		score--
	}
	if len(tc.expectedTop) > 0 && (len(ids) == 0 || !containsAnyString(tc.expectedTop, ids)) {
		score -= 2
	}
	return score
}

func matchedPatternBareIDs(patterns []*awarenesspb.MatchedImplementationPattern) []string {
	out := make([]string, 0, len(patterns))
	for _, p := range patterns {
		out = append(out, strings.TrimPrefix(p.GetId(), "implementation_pattern:"))
	}
	return out
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func containsAnyString(want, got []string) bool {
	for _, w := range want {
		if containsString(got, w) {
			return true
		}
	}
	return false
}

func repoRootForServerTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatalf("could not find repo root from %s", dir)
		}
		dir = next
	}
}
