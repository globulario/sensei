// SPDX-License-Identifier: AGPL-3.0-only

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
		"references/SPECIALIZED-SKILLS.md",
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

func TestManagedSkillTemplatesIncludeArchitect(t *testing.T) {
	assertManagedSkillRegistered(t, "sensei-architect")
}

func TestManagedSkillTemplatesIncludeImport(t *testing.T) {
	assertManagedSkillRegistered(t, "sensei-import")
}

func TestManagedSkillTemplatesIncludeAdmission(t *testing.T) {
	assertManagedSkillRegistered(t, "sensei-admission")
}

func TestManagedSkillTemplatesIncludeClosure(t *testing.T) {
	assertManagedSkillRegistered(t, "sensei-closure")
}

func TestManagedSkillTemplatesIncludeBenchmark(t *testing.T) {
	assertManagedSkillRegistered(t, "sensei-benchmark")
}

func TestEveryManagedSkillHasValidFrontmatter(t *testing.T) {
	for _, skill := range builtinSkills {
		files, err := bundledSkillFiles(skill)
		if err != nil {
			t.Fatalf("bundledSkillFiles(%s): %v", skill.Name, err)
		}
		var front map[string]any
		if err := yaml.Unmarshal(extractFrontMatter(t, string(files["SKILL.md"])), &front); err != nil {
			t.Fatalf("%s invalid SKILL.md front matter: %v", skill.Name, err)
		}
		if front["name"] != skill.Name {
			t.Fatalf("%s front matter name = %#v", skill.Name, front["name"])
		}
		desc, _ := front["description"].(string)
		if strings.TrimSpace(desc) == "" || !strings.Contains(desc, "Use") {
			t.Fatalf("%s description is not an effective trigger: %q", skill.Name, desc)
		}
	}
}

func TestManagedSkillNamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, skill := range builtinSkills {
		if seen[skill.Name] {
			t.Fatalf("duplicate managed skill name %q", skill.Name)
		}
		seen[skill.Name] = true
	}
}

func TestAdmissionSkillDescriptionContainsAdmissionTriggers(t *testing.T) {
	desc := managedSkillDescription(t, "sensei-admission")
	for _, want := range []string{"may I change this", "safe to modify", "admit this task", "permission to edit", "change envelope", "verify admission", "scope compliance", "architecture-sensitive implementation"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("admission description missing %q: %q", want, desc)
		}
	}
}

func TestClosureSkillDescriptionContainsClosureTriggers(t *testing.T) {
	desc := managedSkillDescription(t, "sensei-closure")
	for _, want := range []string{"close the architecture", "why is this blocked", "closure open", "answer the question", "record architect answer", "plan Evidence", "record probe result", "advance convergence", "stalled", "oscillating", "waiting on architect", "waiting on Evidence"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("closure description missing %q: %q", want, desc)
		}
	}
}

func TestBenchmarkSkillDescriptionRequiresExplicitBenchmarkIntent(t *testing.T) {
	desc := managedSkillDescription(t, "sensei-benchmark")
	for _, want := range []string{"Use only", "explicit blind historical external proof", "sealed oracle", "Do not use for ordinary import"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("benchmark description missing %q: %q", want, desc)
		}
	}
}

func TestInstalledSkillReferencesResolve(t *testing.T) {
	dir := t.TempDir()
	if _, err := scaffoldProject(dir, initOptions{skills: true}); err != nil {
		t.Fatalf("scaffoldProject: %v", err)
	}
	for _, skill := range builtinSkills {
		base := filepath.Join(dir, ".sensei", "skills", skill.Name)
		skillMD, err := os.ReadFile(filepath.Join(base, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		for _, ref := range markdownRefs(string(skillMD)) {
			if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "#") {
				continue
			}
			if _, err := os.Stat(filepath.Join(base, filepath.FromSlash(ref))); err != nil {
				t.Fatalf("%s SKILL.md reference %q does not resolve: %v", skill.Name, ref, err)
			}
		}
	}
}

func TestEverySkillReferenceLinkResolves(t *testing.T) {
	for _, skill := range builtinSkills {
		files, err := bundledSkillFiles(skill)
		if err != nil {
			t.Fatalf("bundledSkillFiles(%s): %v", skill.Name, err)
		}
		for name, data := range files {
			for _, ref := range markdownRefs(string(data)) {
				if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "#") {
					continue
				}
				base := filepath.Dir(name)
				target := filepath.ToSlash(filepath.Clean(filepath.Join(base, ref)))
				if target == "." {
					target = ref
				}
				if _, ok := files[target]; !ok {
					t.Fatalf("%s %s reference %q resolves to missing %s", skill.Name, name, ref, target)
				}
			}
		}
	}
}

func TestAdmissionReferencesAreComplete(t *testing.T) {
	assertSkillFiles(t, "sensei-admission", []string{
		"SKILL.md",
		"README.md",
		"references/ADMISSION-MODEL.md",
		"references/AGENT-WORKFLOW.md",
		"references/DECISION-SEMANTICS.md",
		"references/DIFF-VERIFICATION.md",
	})
}

func TestClosureReferencesAreComplete(t *testing.T) {
	assertSkillFiles(t, "sensei-closure", []string{
		"SKILL.md",
		"README.md",
		"references/CLOSURE-MODEL.md",
		"references/DIALOGUE-WORKFLOW.md",
		"references/EVIDENCE-PROBE-WORKFLOW.md",
		"references/CONVERGENCE-WORKFLOW.md",
		"references/HONESTY-BOUNDARIES.md",
	})
}

func TestBenchmarkReferencesAreComplete(t *testing.T) {
	assertSkillFiles(t, "sensei-benchmark", []string{
		"SKILL.md",
		"README.md",
		"references/BLIND-EVALUATION.md",
		"references/TASK-CURATION.md",
		"references/HUMAN-DIRECTION.md",
		"references/ORACLE-EVALUATION.md",
		"references/FALSE-GREEN-RUBRIC.md",
	})
}

func TestArchitectSpecializedSkillReferenceResolves(t *testing.T) {
	assertSkillFiles(t, "sensei-architect", []string{"references/SPECIALIZED-SKILLS.md"})
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
	for _, class := range []string{"contract", "component", "boundary", "decision", "evidence", "design_pattern", "implementation_pattern", "pattern_misuse", "architecture_claim", "meta_principle"} {
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

func TestSkillReferencedCommandsExist(t *testing.T) {
	text := allManagedSkillText(t)
	commands := extractCLICommands(t)
	for _, cmd := range referencedSenseiCommands(text) {
		if !commands[cmd] {
			t.Fatalf("managed skills reference unknown sensei command %q", cmd)
		}
	}
}

func TestSkillCommandExamplesUseKnownFlags(t *testing.T) {
	text := allManagedSkillText(t)
	gitFlags := map[string]bool{"depth": true, "unshallow": true, "is-shallow-repository": true}
	flags := extractCLIFlags(t)
	for _, flag := range referencedSenseiFlags(text) {
		if gitFlags[flag] {
			continue
		}
		if !flags[flag] {
			t.Fatalf("managed skills reference unknown sensei flag --%s", flag)
		}
	}
}

func TestAdmissionSkillReferencesActualMCPTools(t *testing.T) {
	text := bundledSkillText(t, "sensei-admission")
	mcpSource, err := os.ReadFile(filepath.Join("..", "awareness-mcp", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	mcpTools := extractMCPTools(string(mcpSource))
	for _, tool := range []string{"admit_change", "verify_admission"} {
		if !strings.Contains(text, tool) {
			t.Fatalf("admission skill does not reference MCP tool %s", tool)
		}
		if !mcpTools[tool] {
			t.Fatalf("admission skill references MCP tool %s, but it is not registered", tool)
		}
	}
}

func TestClosureSkillDoesNotClaimUnavailableMCPTools(t *testing.T) {
	text := bundledSkillText(t, "sensei-closure")
	for _, forbidden := range []string{"MCP `assess", "MCP `generate", "MCP `record", "MCP `plan", "mcp__"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("closure skill claims unavailable MCP surface %q", forbidden)
		}
	}
}

func TestBenchmarkSkillDoesNotClaimNetworkSupport(t *testing.T) {
	text := bundledSkillText(t, "sensei-benchmark")
	if strings.Contains(text, "network support") || strings.Contains(text, "perform network operations") && !strings.Contains(text, "do not") {
		t.Fatalf("benchmark skill claims network support")
	}
}

func TestAdmissionSkillSaysAdmissionIsNotCorrectness(t *testing.T) {
	assertSkillTextContains(t, "sensei-admission", "Admission is permission to attempt")
	assertSkillTextContains(t, "sensei-admission", "not proof of correctness")
}

func TestAdmissionSkillSaysScopeComplianceIsNotCorrectness(t *testing.T) {
	assertSkillTextContains(t, "sensei-admission", "Scope compliance is not correctness certification")
}

func TestAdmissionSkillRestrictsAutomaticEvidenceToControlledStaticReads(t *testing.T) {
	assertSkillTextContains(t, "sensei-admission", "only `advance-task` may run its closed static-read registry")
	assertSkillTextContains(t, "sensei-admission", "Never record or invent architect answers")
}

func TestClosureSkillSaysAnswerIsNonProbative(t *testing.T) {
	assertSkillTextContains(t, "sensei-closure", "Answer is non-probative")
}

func TestClosureSkillKeepsUnsafeProbesAsPlans(t *testing.T) {
	assertSkillTextContains(t, "sensei-closure", "Runtime,")
	assertSkillTextContains(t, "sensei-closure", "destructive probes remain plans only")
}

func TestClosureSkillSaysOneConvergenceIteration(t *testing.T) {
	assertSkillTextContains(t, "sensei-closure", "Advance exactly one convergence iteration")
}

func TestBenchmarkSkillRequiresRoleSeparation(t *testing.T) {
	assertSkillTextContains(t, "sensei-benchmark", "role separation")
}

func TestBenchmarkSkillSurfacesCriticalFalseGreen(t *testing.T) {
	assertSkillTextContains(t, "sensei-benchmark", "Surface critical false green first")
}

func TestImportSkillDoesNotClaimClosure(t *testing.T) {
	assertSkillTextContains(t, "sensei-import", "does not establish bounded task closure")
}

func TestImportSkillLoadsAndVerifiesProjectReconstruction(t *testing.T) {
	for _, want := range []string{
		"<checkout>/.sensei/project",
		"artifact_ready",
		"live_loaded",
		"architecture_claim",
		"result is a failed import, not thin coverage",
	} {
		assertSkillTextContains(t, "sensei-import", want)
	}
}

func TestArchitectSkillCreatesBoundedAwarenessCheckpoint(t *testing.T) {
	for _, want := range []string{
		"sensei prepare-change",
		".sensei/project/claims.yaml",
		"task-bound questions/probes",
		"mutation must not begin",
		"empty claim document",
	} {
		assertSkillTextContains(t, "sensei-architect", want)
	}
}

func TestAdmissionSkillPreparesTaskWhenBundleIsMissing(t *testing.T) {
	for _, want := range []string{
		"If no convergence bundle exists",
		"sensei prepare-change",
		"sensei task-briefing",
		"sensei task-status",
		"Ask only the primary architect question",
		"not invent an empty bundle",
	} {
		assertSkillTextContains(t, "sensei-admission", want)
	}
}

func TestManagedSkillUsesTaskBriefingBeforeEdit(t *testing.T) {
	text := bundledSkillText(t, "sensei-architect")
	brief := strings.Index(text, "sensei task-briefing")
	guard := strings.Index(text, "Guard implementation")
	if brief < 0 || guard < 0 || brief > guard {
		t.Fatalf("task briefing is not established before the implementation guard")
	}
}

func TestManagedSkillRunsStaticEvidenceBeforeHumanEscalation(t *testing.T) {
	text := bundledSkillText(t, "sensei-architect")
	advance := strings.Index(text, "run_static_evidence")
	ask := strings.Index(text, "Ask the human only")
	if advance < 0 || ask < 0 || advance > ask {
		t.Fatalf("managed workflow does not run static evidence before human escalation")
	}
}

func TestManagedSkillNeverBypassesWaitingMutation(t *testing.T) {
	for _, skill := range []string{"sensei-architect", "sensei-admission"} {
		text := bundledSkillText(t, skill)
		if !strings.Contains(text, "mutation must not begin") && !strings.Contains(text, "Never mutate on `waiting`") {
			t.Fatalf("%s does not explicitly refuse mutation while waiting", skill)
		}
	}
}

func TestArchitectSkillRoutesExactMutationToAdmission(t *testing.T) {
	assertSkillTextContains(t, "sensei-architect", "Exact proposed edit or permission question:")
	assertSkillTextContains(t, "sensei-architect", "use sensei-admission")
}

func TestSkillRoutingDistinguishesImportFromBenchmark(t *testing.T) {
	assertSkillTextContains(t, "sensei-import", "sensei-import:")
	assertSkillTextContains(t, "sensei-import", "sensei-benchmark:")
	assertSkillTextContains(t, "sensei-architect", "Blind historical external proof:")
}

func TestSkillRoutingDistinguishesPreflightFromAdmission(t *testing.T) {
	assertSkillTextContains(t, "sensei-architect", "Preflight is advisory preparation")
	assertSkillTextContains(t, "sensei-architect", "admission decision is the execution-control boundary")
}

func TestSkillRoutingDistinguishesAdmissionFromClosure(t *testing.T) {
	assertSkillTextContains(t, "sensei-admission", "Waiting or refused because architecture is incomplete: use `sensei-closure`")
}

func TestSkillRoutingDoesNotLoadAllSkillsForOrdinaryEdit(t *testing.T) {
	assertSkillTextContains(t, "sensei-admission", "The normal path should load only this skill")
}

func TestArchitectSkillLabelsClaimsNonAuthoritative(t *testing.T) {
	text := allBundledSkillText(t)
	for _, required := range []string{
		"`architecture_claim`",
		"explicit-query-only",
		"non-authoritative",
		"never present it as governed knowledge",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("architect skill does not label architecture claims correctly; missing %q", required)
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

func assertManagedSkillRegistered(t *testing.T, name string) {
	t.Helper()
	skill := findBuiltinSkill(t, name)
	if skill.SourceDir != filepath.ToSlash(filepath.Join("templates", "skills", name)) {
		t.Fatalf("%s source dir = %q", name, skill.SourceDir)
	}
	if len(skill.Targets) != 3 {
		t.Fatalf("%s target count = %d", name, len(skill.Targets))
	}
	if _, err := bundledSkillFiles(skill); err != nil {
		t.Fatalf("bundledSkillFiles(%s): %v", name, err)
	}
}

func managedSkillDescription(t *testing.T, name string) string {
	t.Helper()
	files, err := bundledSkillFiles(findBuiltinSkill(t, name))
	if err != nil {
		t.Fatal(err)
	}
	var front map[string]any
	if err := yaml.Unmarshal(extractFrontMatter(t, string(files["SKILL.md"])), &front); err != nil {
		t.Fatal(err)
	}
	desc, _ := front["description"].(string)
	return desc
}

func assertSkillFiles(t *testing.T, name string, required []string) {
	t.Helper()
	files, err := bundledSkillFiles(findBuiltinSkill(t, name))
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range required {
		if _, ok := files[rel]; !ok {
			t.Fatalf("%s missing %s", name, rel)
		}
	}
}

func bundledSkillText(t *testing.T, name string) string {
	t.Helper()
	files, err := bundledSkillFiles(findBuiltinSkill(t, name))
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

func allManagedSkillText(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, skill := range builtinSkills {
		b.WriteString(bundledSkillText(t, skill.Name))
		b.WriteByte('\n')
	}
	return b.String()
}

func assertSkillTextContains(t *testing.T, name, want string) {
	t.Helper()
	if !strings.Contains(bundledSkillText(t, name), want) {
		t.Fatalf("%s missing required text %q", name, want)
	}
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
	re := regexp.MustCompile(`Name:\s+"([a-z_]+)"`)
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
