// SPDX-License-Identifier: AGPL-3.0-only

package skillimport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseSkillFileParsesFrontMatter(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "skills", "engineering", "tdd", "SKILL.md")
	writeFile(t, path, `---
name: tdd
description: Test-driven development. Use when the user wants test-first work.
---

# Test-Driven Development

Tests verify behavior through public interfaces.
`)

	skill, err := ParseSkillFile(path)
	if err != nil {
		t.Fatalf("ParseSkillFile: %v", err)
	}
	if skill.Name != "tdd" {
		t.Fatalf("Name=%q want tdd", skill.Name)
	}
	if skill.Description != "Test-driven development. Use when the user wants test-first work." {
		t.Fatalf("Description=%q", skill.Description)
	}
	if strings.Contains(skill.Body, "name: tdd") || !strings.Contains(skill.Body, "# Test-Driven Development") {
		t.Fatalf("Body did not exclude front matter correctly:\n%s", skill.Body)
	}
	if !strings.HasSuffix(skill.SourcePath, "skills/engineering/tdd/SKILL.md") {
		t.Fatalf("SourcePath=%q", skill.SourcePath)
	}
}

func TestDiscoverSkillsOnlySkillMD(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "skills", "engineering", "tdd", "SKILL.md"), simpleSkill("tdd"))
	writeFile(t, filepath.Join(root, "skills", "engineering", "tdd", "tests.md"), "not a skill")
	writeFile(t, filepath.Join(root, "skills", "deprecated", "qa", "SKILL.md"), simpleSkill("qa"))
	writeFile(t, filepath.Join(root, "node_modules", "x", "SKILL.md"), simpleSkill("x"))

	skills, err := DiscoverSkills(root, false)
	if err != nil {
		t.Fatalf("DiscoverSkills: %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "tdd" {
		t.Fatalf("skills=%+v, want only tdd", skills)
	}
	if skills[0].SourcePath != "skills/engineering/tdd/SKILL.md" {
		t.Fatalf("SourcePath=%q", skills[0].SourcePath)
	}

	withDeprecated, err := DiscoverSkills(root, true)
	if err != nil {
		t.Fatalf("DiscoverSkills include deprecated: %v", err)
	}
	names := strings.Join([]string{withDeprecated[0].Name, withDeprecated[1].Name}, ",")
	if len(withDeprecated) != 2 || !strings.Contains(names, "tdd") || !strings.Contains(names, "qa") {
		t.Fatalf("withDeprecated=%+v, want tdd and qa only", withDeprecated)
	}
}

func TestExtractsTDDRules(t *testing.T) {
	body := `# Test-Driven Development

Tests verify behavior through public interfaces, not implementation details.

## Seams

Before writing any test, write down the seams under test and confirm them with the user.

## Anti-patterns

- Implementation-coupled — mocks internal collaborators, tests private methods, or verifies through a side channel.
- Do not test private methods or implementation details.

## Rules of the loop

- Write the failing test first, then only enough code to pass it.
- One seam, one test, one minimal implementation per cycle.
`
	candidates := ExtractCandidates([]Skill{{
		Name:        "tdd",
		Description: "Test-driven development. Use when the user wants test-first work.",
		SourcePath:  "skills/engineering/tdd/SKILL.md",
		Category:    "engineering",
		Body:        body,
	}}, ImportOptions{})
	c := candidates[0]
	must := strings.Join(c.MustFollow, "\n")
	for _, want := range []string{
		"Tests verify behavior through public interfaces",
		"Before writing any test, write down the seams",
		"Write the failing test first",
	} {
		if !strings.Contains(must, want) {
			t.Fatalf("must_follow missing %q:\n%s", want, must)
		}
	}
	forbidden := strings.Join(c.ForbiddenShortcuts, "\n")
	for _, want := range []string{"implementation details", "private methods"} {
		if !strings.Contains(forbidden, want) {
			t.Fatalf("forbidden_shortcuts missing %q:\n%s", want, forbidden)
		}
	}
}

func TestExtractsDiagnosingBugsRedLoopRule(t *testing.T) {
	body := `# Diagnosing Bugs

A discipline for hard bugs. Skip phases only when explicitly justified.

## Completion criterion - a tight loop that goes red

- Red-capable - it drives the actual bug code path and asserts the user's exact symptom.
- Deterministic - same verdict every run.

If you catch yourself reading code to build a theory before this command exists, stop. No red-capable command, no Phase 2.
`
	candidates := ExtractCandidates([]Skill{{
		Name:        "diagnosing-bugs",
		Description: "Diagnosis loop for hard bugs and performance regressions.",
		SourcePath:  "skills/engineering/diagnosing-bugs/SKILL.md",
		Category:    "engineering",
		Body:        body,
	}}, ImportOptions{})
	must := strings.Join(candidates[0].MustFollow, "\n")
	for _, want := range []string{
		"No red-capable command, no Phase 2",
		"red-capable",
		"actual bug code path",
	} {
		if !strings.Contains(must, want) {
			t.Fatalf("must_follow missing %q:\n%s", want, must)
		}
	}
}

func TestRendersValidImplementationPatternYAML(t *testing.T) {
	candidate := ExtractCandidates([]Skill{{
		Name:        "tdd",
		Description: "Test-driven development. Use when the user wants test-first work.",
		SourcePath:  "skills/engineering/tdd/SKILL.md",
		Category:    "engineering",
		Body:        "- Write the failing test first.\n",
	}}, ImportOptions{})[0]
	data, err := RenderCandidate(candidate)
	if err != nil {
		t.Fatalf("RenderCandidate: %v", err)
	}
	if err := ValidateCandidateYAML(data); err != nil {
		t.Fatalf("ValidateCandidateYAML: %v\n%s", err, data)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	for _, key := range []string{"id", "class", "label", "status", "when_to_use", "reference_files", "must_follow", "rationale", "source_files"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("rendered YAML missing key %q:\n%s", key, data)
		}
	}
}

func TestNeverEmitsActive(t *testing.T) {
	candidate := ExtractCandidates([]Skill{{
		Name:        "tdd",
		Description: "Test-driven development.",
		SourcePath:  "skills/engineering/tdd/SKILL.md",
		Category:    "engineering",
		Body:        "- Always test first.\n",
	}}, ImportOptions{DefaultStatus: "active"})[0]
	data, err := RenderCandidate(candidate)
	if err != nil {
		t.Fatalf("RenderCandidate: %v", err)
	}
	if strings.Contains(string(data), "status: active") {
		t.Fatalf("rendered active status:\n%s", data)
	}
	if !strings.Contains(string(data), "status: candidate") {
		t.Fatalf("missing candidate status:\n%s", data)
	}
}

func TestStableIDGeneration(t *testing.T) {
	candidates := ExtractCandidates([]Skill{
		{Name: "diagnosing-bugs", Description: "x", SourcePath: "skills/engineering/diagnosing-bugs/SKILL.md", Category: "engineering", Body: "- Always x.\n"},
		{Name: "domain-modeling", Description: "x", SourcePath: "skills/engineering/domain-modeling/SKILL.md", Category: "engineering", Body: "- Always x.\n"},
	}, ImportOptions{})
	got := map[string]bool{}
	for _, c := range candidates {
		got[c.ID] = true
	}
	for _, want := range []string{
		"imported.skill.engineering.diagnosing_bugs",
		"imported.skill.engineering.domain_modeling",
	} {
		if !got[want] {
			t.Fatalf("missing stable id %q from %+v", want, candidates)
		}
	}
}

func simpleSkill(name string) string {
	return `---
name: ` + name + `
description: Test skill. Use when needed.
---

# ` + name + `

- Always do the thing.
`
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
