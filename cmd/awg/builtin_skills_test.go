// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"gopkg.in/yaml.v3"
)

func TestBundledSenseiArchitectSkillPackage(t *testing.T) {
	skill := builtinSkills[0]
	files, err := bundledSkillFiles(skill)
	if err != nil {
		t.Fatalf("bundledSkillFiles: %v", err)
	}
	for _, rel := range []string{
		"SKILL.md",
		"README.md",
		"references/OPERATING-MODEL.md",
		"references/TOOL-PLAYBOOK.md",
		"references/ARCHITECTURE-VIEW.md",
		"references/FINDING-RUBRIC.md",
		"references/WORKFLOW-BRANCHES.md",
		"references/DURABLE-FEEDBACK.md",
	} {
		if _, ok := files[rel]; !ok {
			t.Fatalf("bundled skill missing %s", rel)
		}
	}

	var front map[string]any
	if err := yaml.Unmarshal(extractFrontMatter(t, string(files["SKILL.md"])), &front); err != nil {
		t.Fatalf("invalid SKILL.md front matter: %v", err)
	}
	if front["name"] != "sensei-architect" {
		t.Fatalf("front matter name = %#v", front["name"])
	}
	desc, _ := front["description"].(string)
	if !strings.Contains(desc, "architectural conscience") || !strings.Contains(desc, "Use") {
		t.Fatalf("description is not an effective model-invocation trigger: %q", desc)
	}
}

func TestInstalledSkillReferencesResolve(t *testing.T) {
	dir := t.TempDir()
	if _, err := scaffoldProject(dir, initOptions{skills: true}); err != nil {
		t.Fatalf("scaffoldProject: %v", err)
	}
	base := filepath.Join(dir, ".sensei", "skills", "sensei-architect")
	skillMD, err := os.ReadFile(filepath.Join(base, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range markdownRefs(string(skillMD)) {
		if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "#") {
			continue
		}
		if _, err := os.Stat(filepath.Join(base, filepath.FromSlash(ref))); err != nil {
			t.Fatalf("SKILL.md reference %q does not resolve: %v", ref, err)
		}
	}
}

func TestSenseiArchitectSkillSurfaceConsistency(t *testing.T) {
	skillText := allBundledSkillText(t)

	mcpSource, err := os.ReadFile(filepath.Join("..", "awareness-mcp", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	mcpTools := extractMCPTools(string(mcpSource))
	for _, tool := range referencedAwarenessTools(skillText) {
		if !mcpTools[tool] {
			t.Fatalf("skill references MCP tool %s, but cmd/awareness-mcp does not register it", tool)
		}
	}

	for _, risk := range enumNames(awarenesspb.RiskClass_name, "RISK_CLASS_UNSPECIFIED") {
		if !strings.Contains(skillText, risk) {
			t.Fatalf("skill does not describe current risk class %s", risk)
		}
	}
	for _, status := range []string{"OK", "EMPTY", "DEGRADED"} {
		if !strings.Contains(skillText, status) {
			t.Fatalf("skill does not describe status %s", status)
		}
	}
	for _, class := range extractMCPQueryClasses(string(mcpSource)) {
		if !protoQueryClassExists(class) {
			t.Fatalf("MCP query class %q is not backed by the proto QueryClass enum", class)
		}
	}
	for _, class := range []string{"contract", "component", "boundary", "decision", "evidence", "design_pattern", "implementation_pattern", "pattern_misuse", "meta_principle"} {
		if !strings.Contains(skillText, "`"+class+"`") && !strings.Contains(skillText, class) {
			t.Fatalf("skill omits architecture query class %s", class)
		}
	}

	for _, forbidden := range []string{"SELECT ?", "CONSTRUCT ", "INSERT DATA", "DELETE WHERE", "ASK {", "sparql endpoint"} {
		if strings.Contains(skillText, forbidden) {
			t.Fatalf("skill includes raw graph query example %q", forbidden)
		}
	}
	for _, required := range []string{
		"Never treat `EMPTY` as safe",
		"Candidates are not active authority",
		"Never claim Sensei replaces source inspection, tests, builds, runtime observation, review, or user decisions",
	} {
		if !strings.Contains(skillText, required) {
			t.Fatalf("skill missing required safety statement: %s", required)
		}
	}

	commands := extractCLICommands(t)
	for _, cmd := range referencedSenseiCommands(skillText) {
		if !commands[cmd] {
			t.Fatalf("skill references unknown sensei command %q", cmd)
		}
	}
	flags := extractCLIFlags(t)
	for _, flag := range referencedSenseiFlags(skillText) {
		if !flags[flag] {
			t.Fatalf("skill references unknown sensei flag --%s", flag)
		}
	}
}

func findBuiltinSkill(t *testing.T, name string) builtinSkill {
	t.Helper()
	for _, s := range builtinSkills {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("builtin skill %q is not registered", name)
	return builtinSkill{}
}

func TestBundledSenseiImportSkillPackage(t *testing.T) {
	skill := findBuiltinSkill(t, "sensei-import")
	files, err := bundledSkillFiles(skill)
	if err != nil {
		t.Fatalf("bundledSkillFiles: %v", err)
	}
	for _, rel := range []string{
		"SKILL.md",
		"README.md",
		"references/IMPORT-PLAYBOOK.md",
	} {
		if _, ok := files[rel]; !ok {
			t.Fatalf("bundled skill missing %s", rel)
		}
	}

	var front map[string]any
	if err := yaml.Unmarshal(extractFrontMatter(t, string(files["SKILL.md"])), &front); err != nil {
		t.Fatalf("invalid SKILL.md front matter: %v", err)
	}
	if front["name"] != "sensei-import" {
		t.Fatalf("front matter name = %#v", front["name"])
	}
	desc, _ := front["description"].(string)
	if !strings.Contains(desc, "import") || !strings.Contains(desc, "Use") {
		t.Fatalf("description is not an effective model-invocation trigger: %q", desc)
	}

	// Installed references must resolve.
	dir := t.TempDir()
	if _, err := scaffoldProject(dir, initOptions{skills: true}); err != nil {
		t.Fatalf("scaffoldProject: %v", err)
	}
	base := filepath.Join(dir, ".sensei", "skills", "sensei-import")
	skillMD, err := os.ReadFile(filepath.Join(base, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range markdownRefs(string(skillMD)) {
		if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "#") {
			continue
		}
		if _, err := os.Stat(filepath.Join(base, filepath.FromSlash(ref))); err != nil {
			t.Fatalf("SKILL.md reference %q does not resolve: %v", ref, err)
		}
	}

	// Every sensei command and flag the skill names must exist, so the guidance
	// cannot drift away from the real CLI surface.
	var skillText strings.Builder
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		skillText.Write(files[name])
		skillText.WriteByte('\n')
	}
	text := skillText.String()

	commands := extractCLICommands(t)
	for _, cmd := range referencedSenseiCommands(text) {
		if !commands[cmd] {
			t.Fatalf("skill references unknown sensei command %q", cmd)
		}
	}
	// git long-flags legitimately appear in the clone/history guidance.
	gitFlags := map[string]bool{"depth": true, "unshallow": true, "is-shallow-repository": true}
	flags := extractCLIFlags(t)
	for _, flag := range referencedSenseiFlags(text) {
		if gitFlags[flag] {
			continue
		}
		if !flags[flag] {
			t.Fatalf("skill references unknown sensei flag --%s", flag)
		}
	}
}

func extractFrontMatter(t *testing.T, text string) []byte {
	t.Helper()
	if !strings.HasPrefix(text, "---\n") {
		t.Fatal("SKILL.md missing YAML front matter")
	}
	rest := strings.TrimPrefix(text, "---\n")
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		t.Fatal("SKILL.md front matter is not closed")
	}
	return []byte(rest[:idx])
}

func markdownRefs(text string) []string {
	re := regexp.MustCompile(`\[[^\]]+\]\(([^)#]+)(?:#[^)]+)?\)`)
	var refs []string
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		refs = append(refs, m[1])
	}
	return refs
}

func allBundledSkillText(t *testing.T) string {
	t.Helper()
	files, err := bundledSkillFiles(builtinSkills[0])
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		b.Write(files[name])
		b.WriteByte('\n')
	}
	return b.String()
}

func extractMCPTools(source string) map[string]bool {
	out := map[string]bool{}
	re := regexp.MustCompile(`Name:\s+"(awareness_[a-z_]+)"`)
	for _, m := range re.FindAllStringSubmatch(source, -1) {
		out[m[1]] = true
	}
	return out
}

func referencedAwarenessTools(text string) []string {
	seen := map[string]bool{}
	re := regexp.MustCompile(`\bawareness_[a-z_]+\b`)
	for _, m := range re.FindAllString(text, -1) {
		seen[m] = true
	}
	return sortedBoolKeys(seen)
}

func enumNames(values map[int32]string, skip string) []string {
	var out []string
	for _, name := range values {
		if name != skip {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func extractMCPQueryClasses(source string) []string {
	start := strings.Index(source, "var mcpQueryClasses = []string{")
	if start < 0 {
		return nil
	}
	end := strings.Index(source[start:], "}")
	if end < 0 {
		return nil
	}
	block := source[start : start+end]
	re := regexp.MustCompile(`"([a-z_]+)"`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(block, -1) {
		out = append(out, m[1])
	}
	return out
}

func protoQueryClassExists(class string) bool {
	want := "QUERY_CLASS_" + strings.ToUpper(class)
	for _, name := range awarenesspb.QueryClass_name {
		if name == want {
			return true
		}
	}
	return false
}

func extractCLICommands(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	re := regexp.MustCompile(`case "([a-z0-9-]+)":`)
	for _, m := range re.FindAllStringSubmatch(string(data), -1) {
		out[m[1]] = true
	}
	return out
}

func referencedSenseiCommands(text string) []string {
	seen := map[string]bool{}
	re := regexp.MustCompile(`\bsensei\s+([a-z][a-z0-9-]+)`)
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		seen[m[1]] = true
	}
	return sortedBoolKeys(seen)
}

func extractCLIFlags(t *testing.T) map[string]bool {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{"help": true}
	re := regexp.MustCompile(`(?:fs|flag)\.(?:Bool|String|Int|Duration|Float64)\("([a-z0-9-]+)"|(?:fs|flag)\.(?:Bool|String|Int|Duration|Float64)Var\([^,\n]+,\s*"([a-z0-9-]+)"|(?:fs|flag)\.Var\([^,\n]+,\s*"([a-z0-9-]+)"`)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(data), -1) {
			for _, name := range m[1:] {
				if name != "" {
					out[name] = true
				}
			}
		}
	}
	return out
}

func referencedSenseiFlags(text string) []string {
	seen := map[string]bool{}
	re := regexp.MustCompile(`--([a-z][a-z0-9-]+)`)
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		seen[m[1]] = true
	}
	return sortedBoolKeys(seen)
}

func sortedBoolKeys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
