// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/contractassess"
	"github.com/globulario/sensei/golang/rdf"
)

// authoredTriple renders an N-Triples authoredIn provenance line for a node.
func authoredTriple(subjectShort, provenance string) string {
	return "<https://globular.io/awareness#" + subjectShort + "> <" +
		rdf.PropAuthoredIn + "> \"" + provenance + "\" ."
}

// writeSeed writes N-Triples lines to a temp seed file and returns its path.
func writeSeed(t *testing.T, dir string, lines ...string) string {
	t.Helper()
	seed := filepath.Join(dir, "awareness.nt")
	if err := os.WriteFile(seed, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	return seed
}

// touchYAML creates a provenance file under root so its node resolves as live.
func touchYAML(t *testing.T, root, rel string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte("invariants: []\n"), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
}

func TestDowngradeFreshnessToAdvisory(t *testing.T) {
	checks := []auditResult{
		{name: "embeddata-freshness", level: auditFAIL, summary: "STALE — 1 owned triple drift"},
		{name: "seed-orphans", level: auditFAIL, summary: "1 orphan"},
		{name: "yaml-validity", level: auditPASS, summary: "clean"},
	}
	downgradeFreshnessToAdvisory(checks)

	if checks[0].level != auditWARN {
		t.Errorf("freshness FAIL should downgrade to WARN, got %v", checks[0].level)
	}
	if !strings.HasPrefix(checks[0].summary, "ADVISORY (--warn-stale):") {
		t.Errorf("freshness summary not annotated: %q", checks[0].summary)
	}
	// Correctness checks must stay hard — only freshness is advisory.
	if checks[1].level != auditFAIL {
		t.Errorf("seed-orphans FAIL must NOT be downgraded, got %v", checks[1].level)
	}
	if checks[2].level != auditPASS {
		t.Errorf("unrelated PASS check mutated, got %v", checks[2].level)
	}
}

func TestAuditSeedGenerationInputs_SelfOnlyMatchesStandaloneSeedBuilder(t *testing.T) {
	root := t.TempDir()
	agRepo := filepath.Join(root, "awareness-graph")
	if err := os.MkdirAll(filepath.Join(agRepo, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}
	inputDirs := []string{
		filepath.Join(agRepo, "docs", "awareness"),
		filepath.Join(agRepo, "eval", "multi-swe-bench", "contracts"),
		filepath.Join(agRepo, "eval", "multi-swe-bench", "notes", "learning_events"),
		filepath.Join(agRepo, "docs", "intent"),
	}
	intentDir := filepath.Join(agRepo, "docs", "intent")

	gotDirs, gotIntent := auditSeedGenerationInputs(inputDirs, intentDir, "", agRepo)
	wantDirs := []string{filepath.Join(agRepo, "docs", "awareness")}
	if strings.Join(gotDirs, "\n") != strings.Join(wantDirs, "\n") {
		t.Fatalf("seed input dirs = %q, want %q", gotDirs, wantDirs)
	}
	if gotIntent != "" {
		t.Fatalf("seed intent dir = %q, want empty self-only seed input", gotIntent)
	}
}

func TestAuditSeedGenerationInputs_CombinedKeepsPairedInputs(t *testing.T) {
	inputDirs := []string{"/ag/docs/awareness", "/svc/docs/awareness", "/svc/docs/awareness/generated"}
	intentDir := "/svc/docs/intent"

	gotDirs, gotIntent := auditSeedGenerationInputs(inputDirs, intentDir, "/svc", "/ag")
	if strings.Join(gotDirs, "\n") != strings.Join(inputDirs, "\n") {
		t.Fatalf("seed input dirs = %q, want %q", gotDirs, inputDirs)
	}
	if gotIntent != intentDir {
		t.Fatalf("seed intent dir = %q, want %q", gotIntent, intentDir)
	}
}

func TestFilterNTriplesToDomain_KeepsRepoAndSharedSubjects(t *testing.T) {
	nt := strings.Join([]string{
		`<https://globular.io/awareness#invariant/a> <` + rdf.PropRepo + `> "github.com/o/a" .`,
		`<https://globular.io/awareness#invariant/a> <` + rdf.PropAuthoredIn + `> "docs/awareness/a.yaml" .`,
		`<https://globular.io/awareness#invariant/b> <` + rdf.PropRepo + `> "github.com/o/b" .`,
		`<https://globular.io/awareness#invariant/b> <` + rdf.PropAuthoredIn + `> "docs/awareness/b.yaml" .`,
		`<https://globular.io/awareness#invariant/shared> <` + rdf.PropDomain + `> "shared" .`,
		`<https://globular.io/awareness#invariant/shared> <` + rdf.PropAuthoredIn + `> "docs/awareness/shared.yaml" .`,
	}, "\n") + "\n"

	got, count := filterNTriplesToDomain([]byte(nt), "github.com/o/a")
	out := string(got)
	if count != 4 {
		t.Fatalf("count=%d, want 4 scoped triples:\n%s", count, out)
	}
	if !strings.Contains(out, "invariant/a") {
		t.Fatalf("repo subject missing:\n%s", out)
	}
	if !strings.Contains(out, "invariant/shared") {
		t.Fatalf("shared subject missing:\n%s", out)
	}
	if strings.Contains(out, "invariant/b") {
		t.Fatalf("foreign subject leaked:\n%s", out)
	}
}

func TestCheckAuditDomainScope_FailsUnknownDomain(t *testing.T) {
	nt := strings.Join([]string{
		`<https://globular.io/awareness#invariant/a> <` + rdf.PropRepo + `> "github.com/o/a" .`,
		`<https://globular.io/awareness#invariant/shared> <` + rdf.PropDomain + `> "shared" .`,
	}, "\n") + "\n"
	scoped := filterNTriplesToDomainResult([]byte(nt), "github.com/o/missing")

	got := checkAuditDomainScope("github.com/o/missing", "", scoped)
	if got.level != auditFAIL {
		t.Fatalf("level=%v, want FAIL for unknown domain (summary=%q)", got.level, got.summary)
	}
	if !strings.Contains(got.summary, "0 repo-owned subjects") {
		t.Fatalf("summary=%q, want zero repo-owned subject signal", got.summary)
	}
}

func TestCheckAuditDomainScope_PassesKnownDomain(t *testing.T) {
	nt := strings.Join([]string{
		`<https://globular.io/awareness#invariant/a> <` + rdf.PropRepo + `> "github.com/o/a" .`,
		`<https://globular.io/awareness#invariant/a> <` + rdf.PropAuthoredIn + `> "docs/awareness/a.yaml" .`,
		`<https://globular.io/awareness#invariant/shared> <` + rdf.PropDomain + `> "shared" .`,
	}, "\n") + "\n"
	scoped := filterNTriplesToDomainResult([]byte(nt), "github.com/o/a")

	got := checkAuditDomainScope("github.com/o/a", "/repo", scoped)
	if got.level != auditPASS {
		t.Fatalf("level=%v, want PASS for known domain (summary=%q)", got.level, got.summary)
	}
	if scoped.repoSubjects != 1 {
		t.Fatalf("repoSubjects=%d, want 1", scoped.repoSubjects)
	}
}

func TestAuditRepoForDomain_UsesMatchingRepoRoot(t *testing.T) {
	old := gitRemoteDomain
	defer func() { gitRemoteDomain = old }()
	gitRemoteDomain = func(path string) string {
		switch path {
		case "/svc":
			return "github.com/o/svc"
		case "/ag":
			return "github.com/o/ag"
		default:
			return ""
		}
	}

	if got := auditRepoForDomain("github.com/o/svc", "/svc", "/ag"); got != "/svc" {
		t.Fatalf("svc domain root = %q, want /svc", got)
	}
	if got := auditRepoForDomain("github.com/o/ag", "/svc", "/ag"); got != "/ag" {
		t.Fatalf("ag domain root = %q, want /ag", got)
	}
	if got := auditRepoForDomain("github.com/o/other", "/svc", "/ag"); got != "" {
		t.Fatalf("unknown domain root = %q, want empty", got)
	}
}

func TestAuditInputsForRepo_FiltersDirsAndIntent(t *testing.T) {
	inputs := []string{
		"/ag/docs/awareness",
		"/svc/docs/awareness",
		"/svc/docs/awareness/generated",
	}
	dirs, intent := auditInputsForRepo(inputs, "/ag/docs/intent", "/svc")
	if strings.Join(dirs, "\n") != strings.Join(inputs[1:], "\n") {
		t.Fatalf("dirs=%q, want svc-only dirs", dirs)
	}
	if intent != "" {
		t.Fatalf("intent=%q, want empty because AG intent is outside svc root", intent)
	}

	dirs, intent = auditInputsForRepo(inputs, "/svc/docs/intent", "/svc")
	if strings.Join(dirs, "\n") != strings.Join(inputs[1:], "\n") {
		t.Fatalf("dirs=%q, want svc-only dirs", dirs)
	}
	if intent != "/svc/docs/intent" {
		t.Fatalf("intent=%q, want svc intent", intent)
	}
}

func TestEvaluateMetaPrincipleCoverage_FailsUnclassifiedPrinciples(t *testing.T) {
	metas := map[string]bool{
		"meta.covered":      true,
		"meta.unclassified": true,
	}
	got := evaluateMetaPrincipleCoverage(metas, map[string]string{}, auditCoverageRegistry{
		EnforcementRatchet: struct {
			MaxReviewOnly int `yaml:"max_review_only"`
		}{MaxReviewOnly: 1},
		Coverage: []auditCoverageEntry{
			{Principle: "meta.covered", Tier: "review_only", Reason: "design-only"},
		},
	}, nil)

	if got.level != auditFAIL {
		t.Fatalf("level = %v, want FAIL", got.level)
	}
	if !strings.Contains(got.summary, "1 unclassified") {
		t.Fatalf("summary = %q, want unclassified count", got.summary)
	}
	if !strings.Contains(strings.Join(got.details, "\n"), "meta.unclassified") {
		t.Fatalf("details = %v, want unclassified principle", got.details)
	}
}

func TestEvaluateMetaPrincipleCoverage_PassesWhenAllPrinciplesAreClassified(t *testing.T) {
	metas := map[string]bool{
		"meta.scanned":  true,
		"meta.reviewed": true,
	}
	instToMeta := map[string]string{"instance.foo": "meta.scanned"}
	got := evaluateMetaPrincipleCoverage(metas, instToMeta, auditCoverageRegistry{
		EnforcementRatchet: struct {
			MaxReviewOnly int `yaml:"max_review_only"`
		}{MaxReviewOnly: 1},
		Coverage: []auditCoverageEntry{
			{Principle: "meta.reviewed", Tier: "review_only", Reason: "terminal philosophy"},
		},
	}, []string{"instance.foo"})

	if got.level != auditPASS {
		t.Fatalf("level = %v, want PASS (summary=%q details=%v)", got.level, got.summary, got.details)
	}
	if !strings.Contains(got.summary, "2 meta.* principles classified") {
		t.Fatalf("summary = %q", got.summary)
	}
}

func TestCheckSeedOrphans_FlagsGhostWhenBothRootsPresent(t *testing.T) {
	svc := t.TempDir()
	ag := t.TempDir()
	touchYAML(t, svc, "docs/awareness/invariants.yaml")
	seed := writeSeed(t, t.TempDir(),
		authoredTriple("invariant/alive", "docs/awareness/invariants.yaml"),
		authoredTriple("invariant/ghost", "docs/awareness/deleted.yaml"),
		authoredTriple("seedBuild/marker", "generated:seed_marker"), // synthetic — must be skipped
	)

	res := checkSeedOrphans(svc, ag, seed)
	if res.level != auditFAIL {
		t.Fatalf("level = %v, want FAIL", res.level)
	}
	joined := strings.Join(res.details, " | ")
	if !strings.Contains(joined, "invariant/ghost") {
		t.Errorf("expected ghost node in details, got %q", joined)
	}
	if strings.Contains(joined, "invariant/alive") {
		t.Errorf("live node wrongly flagged: %q", joined)
	}
	if strings.Contains(joined, "seedBuild/marker") || strings.Contains(joined, "seed_marker") {
		t.Errorf("synthetic marker wrongly flagged: %q", joined)
	}
	if len(res.details) != 1 {
		t.Errorf("expected exactly 1 orphan, got %d: %v", len(res.details), res.details)
	}
}

func TestCheckSeedOrphansInDomain_ExcludesForeignDomain(t *testing.T) {
	svc := t.TempDir()
	ag := t.TempDir()
	seed := writeSeed(t, t.TempDir(),
		`<https://globular.io/awareness#invariant/a> <`+rdf.PropRepo+`> "github.com/o/a" .`,
		authoredTriple("invariant/a", "docs/awareness/missing-a.yaml"),
		`<https://globular.io/awareness#invariant/b> <`+rdf.PropRepo+`> "github.com/o/b" .`,
		authoredTriple("invariant/b", "docs/awareness/missing-b.yaml"),
	)

	res := checkSeedOrphansInDomain(svc, ag, seed, "github.com/o/a")
	if res.level != auditFAIL {
		t.Fatalf("level = %v, want FAIL", res.level)
	}
	joined := strings.Join(res.details, " | ")
	if !strings.Contains(joined, "invariant/a") {
		t.Fatalf("expected domain orphan in details, got %q", joined)
	}
	if strings.Contains(joined, "invariant/b") {
		t.Fatalf("foreign domain orphan leaked into scoped audit: %q", joined)
	}
}

func TestCheckSeedOrphans_CleanSeedPasses(t *testing.T) {
	svc := t.TempDir()
	ag := t.TempDir()
	touchYAML(t, svc, "docs/awareness/invariants.yaml")
	touchYAML(t, ag, "docs/awareness/generic/meta.yaml")
	seed := writeSeed(t, t.TempDir(),
		authoredTriple("invariant/svc", "docs/awareness/invariants.yaml"),
		authoredTriple("invariant/ag", "docs/awareness/generic/meta.yaml"),
		authoredTriple("seedBuild/marker", "generated:seed_marker"),
	)

	res := checkSeedOrphans(svc, ag, seed)
	if res.level != auditPASS {
		t.Fatalf("level = %v, want PASS (details: %v)", res.level, res.details)
	}
}

func TestCheckSeedOrphans_DegradesToWarnWhenRootMissing(t *testing.T) {
	svc := t.TempDir()
	touchYAML(t, svc, "docs/awareness/invariants.yaml")
	seed := writeSeed(t, t.TempDir(),
		authoredTriple("invariant/alive", "docs/awareness/invariants.yaml"),
		authoredTriple("invariant/ghost", "docs/awareness/deleted.yaml"),
	)

	// agRepo empty: a partial checkout cannot prove the ghost is a true orphan.
	res := checkSeedOrphans(svc, "", seed)
	if res.level != auditWARN {
		t.Fatalf("level = %v, want WARN with a missing root", res.level)
	}
}

func TestAssessmentInputForIntent_ContractFound(t *testing.T) {
	doc := auditIntentDoc{
		ID:            "awareness.resolve_returns_precise_node_by_class_and_id",
		Level:         "contract",
		ExpressedBy:   []string{"golang/server/resolve.go"},
		RequiredTests: []string{"golang/server/main_test.go:TestResolve_FoundMapsCoreFields"},
	}

	result := contractassess.Assess(assessmentInputForIntent(doc))
	if result.Outcome != contractassess.ContractFound {
		t.Fatalf("outcome = %q, want %q", result.Outcome, contractassess.ContractFound)
	}
}

func TestAssessmentInputForIntent_SynthesisSafe(t *testing.T) {
	doc := auditIntentDoc{
		ID:                "awareness.multi_language_extraction",
		Level:             "mechanism",
		ExpressedBy:       []string{"golang/scanner/scanner.go"},
		RequiredTests:     []string{"golang/scanner/typescript_test.go:TestTSScanner_SharedGrammarValidation"},
		RelatedInvariants: []string{"awareness.graph_core_is_language_neutral"},
	}

	result := contractassess.Assess(assessmentInputForIntent(doc))
	if result.Outcome != contractassess.ContractSynthesisSafe {
		t.Fatalf("outcome = %q, want %q", result.Outcome, contractassess.ContractSynthesisSafe)
	}
}

func TestAssessmentInputForIntent_ProposalOnlyWithoutGoverningTest(t *testing.T) {
	doc := auditIntentDoc{
		ID:                "awareness.multi_language_extraction",
		Level:             "mechanism",
		ExpressedBy:       []string{"golang/scanner/scanner.go"},
		RelatedInvariants: []string{"awareness.graph_core_is_language_neutral"},
	}

	result := contractassess.Assess(assessmentInputForIntent(doc))
	if result.Outcome != contractassess.ContractProposalOnly {
		t.Fatalf("outcome = %q, want %q", result.Outcome, contractassess.ContractProposalOnly)
	}
}

func TestAssessmentInputForIntent_UnknownWithoutOwnership(t *testing.T) {
	doc := auditIntentDoc{
		ID:            "awareness.loose_note",
		Level:         "mechanism",
		RequiredTests: []string{"some/test.go:TestThing"},
	}

	result := contractassess.Assess(assessmentInputForIntent(doc))
	if result.Outcome != contractassess.ContractUnknown {
		t.Fatalf("outcome = %q, want %q", result.Outcome, contractassess.ContractUnknown)
	}
}

func TestCheckContractAssessment_Summary(t *testing.T) {
	dir := t.TempDir()
	writeAuditIntent(t, dir, "contract.yaml", `id: awareness.resolve
level: contract
status: active
expressed_by:
  - golang/server/resolve.go
required_tests:
  - golang/server/main_test.go:TestResolve_FoundMapsCoreFields
`)
	writeAuditIntent(t, dir, "mechanism.yaml", `id: awareness.multi_language_extraction
level: mechanism
status: active
expressed_by:
  - golang/scanner/scanner.go
required_tests:
  - golang/scanner/typescript_test.go:TestTSScanner_SharedGrammarValidation
related_invariants:
  - awareness.graph_core_is_language_neutral
`)
	writeAuditIntent(t, dir, "unknown.yaml", `id: awareness.loose_note
level: mechanism
status: active
required_tests:
  - some/test.go:TestThing
`)

	got := checkContractAssessment(dir, "")
	if got.level != auditWARN {
		t.Fatalf("level = %s, want WARN", got.level)
	}
	if !strings.Contains(got.summary, "1 contract-found, 1 contract-synthesis-safe, 0 contract-proposal-only, 1 contract-unknown (local authored intents only)") {
		t.Fatalf("summary = %q", got.summary)
	}
}

func TestCheckContractAssessment_SummaryExcludesSiblingIntentDocs(t *testing.T) {
	dir := t.TempDir()
	writeAuditIntent(t, dir, "contract.yaml", `id: awareness.resolve
level: contract
status: active
expressed_by:
  - golang/server/resolve.go
required_tests:
  - golang/server/main_test.go:TestResolve_FoundMapsCoreFields
`)

	got := checkContractAssessment(dir, "/tmp/services/docs/intent")
	if !strings.Contains(got.summary, "sibling intent docs excluded from self-audit") {
		t.Fatalf("summary = %q", got.summary)
	}
}

func TestSelectAuditIntentDirs_PrefersAwarenessGraphLocalIntent(t *testing.T) {
	root := t.TempDir()
	agRepo := filepath.Join(root, "awareness-graph")
	svcRepo := filepath.Join(root, "services")

	if err := os.MkdirAll(filepath.Join(agRepo, "docs", "intent"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(svcRepo, "docs", "intent"), 0o755); err != nil {
		t.Fatal(err)
	}

	local, paired := selectAuditIntentDirs(agRepo, svcRepo)
	if local != filepath.Join(agRepo, "docs", "intent") {
		t.Fatalf("local = %q, want %q", local, filepath.Join(agRepo, "docs", "intent"))
	}
	if paired != filepath.Join(svcRepo, "docs", "intent") {
		t.Fatalf("paired = %q, want %q", paired, filepath.Join(svcRepo, "docs", "intent"))
	}
}

func writeAuditIntent(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
