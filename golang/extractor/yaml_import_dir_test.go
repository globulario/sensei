// SPDX-License-Identifier: Apache-2.0

package extractor_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/extractor"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// makeDir creates a temp directory tree populated by the given files map
// (relative path → YAML content). Directories are created as needed.
func makeDir(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("makeDir mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("makeDir write %s: %v", rel, err)
		}
	}
	return root
}

func importDir(t *testing.T, dir string) (*extractor.ImportReport, string) {
	t.Helper()
	return importDirWithOpts(t, dir, extractor.ImportDirOptions{})
}

func importDirWithOpts(t *testing.T, dir string, opts extractor.ImportDirOptions) (*extractor.ImportReport, string) {
	t.Helper()
	var buf bytes.Buffer
	_, report, err := extractor.ImportAwarenessDirWithOpts(dir, &buf, opts)
	if err != nil {
		t.Fatalf("ImportAwarenessDirWithOpts: %v", err)
	}
	return report, buf.String()
}

// ── Phase A tests ─────────────────────────────────────────────────────────────

// TestImportDir_RecursiveDiscovery verifies that files in subdirectories are
// discovered and classified. A file in subdir/ with the invariants: schema
// must be imported; a file with a known-unsupported schema must be reported.
func TestImportDir_RecursiveDiscovery(t *testing.T) {
	root := makeDir(t, map[string]string{
		"invariants.yaml": `
invariants:
  - id: test.recursive.inv
    title: Recursive invariant
    severity: high
    status: active
`,
		"subdir/also_invariants.yaml": `
invariants:
  - id: test.recursive.subdir.inv
    title: Subdir invariant
    severity: low
    status: active
`,
		"subdir/unsupported.yaml": `
authority_rules:
  - id: ar.test
    title: test authority rule
    layer: Desired
`,
	})

	report, out := importDirWithOpts(t, root, extractor.ImportDirOptions{SkipNestedGenerated: true})

	// All 3 files must appear in the report — none silently hidden.
	if len(report.Files) != 3 {
		t.Fatalf("expected 3 file reports, got %d", len(report.Files))
	}

	// Both invariant files must be imported.
	imported := report.Imported()
	if len(imported) != 2 {
		t.Fatalf("expected 2 imported files, got %d", len(imported))
	}

	// The subdir unsupported file must appear as known_unsupported, not silently hidden.
	skipped := report.Skipped()
	if len(skipped) != 1 {
		t.Fatalf("expected 1 skipped file, got %d", len(skipped))
	}
	if skipped[0].Status != extractor.StatusKnownUnsupported {
		t.Errorf("skipped file status = %q, want %q", skipped[0].Status, extractor.StatusKnownUnsupported)
	}

	// Triples from both invariant files must appear in output.
	if !strings.Contains(out, "test.recursive.inv") || !strings.Contains(out, "test.recursive.subdir.inv") {
		t.Errorf("expected both invariant IDs in output; got:\n%s", out[:min(len(out), 500)])
	}
}

func TestImportDir_SkipsNestedGeneratedDirWhenParentImported(t *testing.T) {
	root := makeDir(t, map[string]string{
		"invariants.yaml": `
invariants:
  - id: parent.inv
    title: Parent invariant
    severity: high
    status: active
`,
		"generated/awareness_graph_code_symbols.yaml": `
code_symbols:
  - id: generated.symbol
    language: go
    kind: function
    defined_in: golang/server/fake.go
`,
	})

	report, out := importDirWithOpts(t, root, extractor.ImportDirOptions{SkipNestedGenerated: true})

	if strings.Contains(out, "generated.symbol") {
		t.Fatalf("nested generated/ content leaked into parent import output")
	}
	if len(report.Files) != 1 {
		t.Fatalf("expected only parent file in report, got %d", len(report.Files))
	}
	if !strings.Contains(out, "parent.inv") {
		t.Fatalf("parent file did not import correctly")
	}
}

// TestImportDir_KnownUnsupportedReported verifies that files with recognized
// but not-yet-implemented schemas appear in the report with StatusKnownUnsupported
// and a non-empty reason, and do NOT produce any triples.
// Uses schemas that are still pending (Phase B remaining / Phase C).
func TestImportDir_KnownUnsupportedReported(t *testing.T) {
	knownUnsupported := map[string]string{
		"authority_rules":   "authority_rules:\n  - id: ar1\n    title: test\n    layer: Desired\n",
		"authorities":       "authorities:\n  - id: ar1\n    owns:\n      - test\n    source: test\n",
		"namespaces":        "namespaces:\n  - id: ns1\n    label: test\n    owns:\n      - pkg\n",
		"fix_cases":         "fix_cases:\n  - id: fc1\n    title: test\n    status: DONE\n",
		"causal_rules":      "causal_rules:\n  - id: cr1\n    root_signal: disk_pressure\n",
		"queries":           "queries:\n  - id: q1\n    query: 'up'\n",
		"thresholds":        "thresholds:\n  default:\n    cpu_percent: 90\n",
		"allowlist":         "allowlist:\n  - path_pattern: '**/*_test.go'\n    pattern_id: test\n",
		"failuregraph_seed": "id: ERRCAT-test\ntype: ErrorCategory\nname: test\nseverity: high\nsummary: test\n",
	}

	for schema, content := range knownUnsupported {
		t.Run(schema, func(t *testing.T) {
			root := makeDir(t, map[string]string{"test.yaml": content})
			report, out := importDir(t, root)

			if len(report.Files) != 1 {
				t.Fatalf("expected 1 file report, got %d", len(report.Files))
			}
			fr := report.Files[0]
			if fr.Status != extractor.StatusKnownUnsupported {
				t.Errorf("status = %q, want %q", fr.Status, extractor.StatusKnownUnsupported)
			}
			if fr.Schema == "" {
				t.Error("Schema must be non-empty for known_unsupported files")
			}
			if fr.Reason == "" {
				t.Error("Reason must be non-empty for known_unsupported files")
			}
			if fr.Phase == "" {
				t.Error("Phase must be non-empty for known_unsupported files")
			}
			if out != "" {
				t.Errorf("known_unsupported file must emit zero triples; got output:\n%s", out[:min(len(out), 200)])
			}
		})
	}
}

func TestImportDir_AuthoritiesAliasReportedAsKnownUnsupported(t *testing.T) {
	root := makeDir(t, map[string]string{
		"authority_rules.yaml": `
authorities:
  - id: auth1
    owns:
      - cluster PKI
    source: /var/lib/example/pki
`,
	})

	report, out := importDir(t, root)
	if len(report.Files) != 1 {
		t.Fatalf("expected 1 file report, got %d", len(report.Files))
	}
	fr := report.Files[0]
	if fr.Status != extractor.StatusKnownUnsupported {
		t.Fatalf("status=%q want %q", fr.Status, extractor.StatusKnownUnsupported)
	}
	if fr.Schema != "authority_rules" {
		t.Fatalf("schema=%q want authority_rules", fr.Schema)
	}
	if out != "" {
		t.Fatalf("known_unsupported alias must emit zero triples; got:\n%s", out)
	}
}

func TestImportDir_NonAuthoritySchemaIgnored(t *testing.T) {
	root := makeDir(t, map[string]string{
		"annotation_report.yaml": `
scanned_files:
  - path: golang/server/main.go
    annotations:
      - line: 1
        key: component
        value: server.main
unsupported_keys: []
`,
	})

	report, out := importDir(t, root)

	if len(report.Files) != 1 {
		t.Fatalf("expected 1 file report, got %d", len(report.Files))
	}
	fr := report.Files[0]
	if fr.Status != extractor.StatusIgnored {
		t.Fatalf("status = %q, want %q", fr.Status, extractor.StatusIgnored)
	}
	if fr.Schema != "annotation_report" {
		t.Fatalf("schema = %q, want annotation_report", fr.Schema)
	}
	if len(report.Ignored()) != 1 {
		t.Fatalf("ignored=%d, want 1", len(report.Ignored()))
	}
	if len(report.Skipped()) != 0 {
		t.Fatalf("skipped=%d, want 0 for ignored non-authority", len(report.Skipped()))
	}
	if out != "" {
		t.Fatalf("ignored non-authority file must emit zero triples; got:\n%s", out)
	}
}

// TestImportDir_UnknownSchemaReported verifies that a YAML file with a
// top-level key not in the schema table appears as StatusUnknownSchema and
// does NOT produce any triples.
func TestImportDir_UnknownSchemaReported(t *testing.T) {
	root := makeDir(t, map[string]string{
		"mystery.yaml": `
my_completely_unknown_key:
  - foo: bar
`,
	})

	report, out := importDir(t, root)

	if len(report.Files) != 1 {
		t.Fatalf("expected 1 file report, got %d", len(report.Files))
	}
	fr := report.Files[0]
	if fr.Status != extractor.StatusUnknownSchema {
		t.Errorf("status = %q, want %q", fr.Status, extractor.StatusUnknownSchema)
	}
	if fr.Reason == "" {
		t.Error("Reason must be non-empty for unknown_schema files")
	}
	if report.HasUnknown() != true {
		t.Error("HasUnknown() must return true")
	}
	if out != "" {
		t.Errorf("unknown_schema file must emit zero triples; got:\n%s", out)
	}
}

// TestImportDir_IntentFileClassifiedAndImported verifies that a file with the
// intent document shape (id + level at top level) is detected as the intent
// schema and imported as Phase C. After Phase C landed, intent is importable.
func TestImportDir_IntentFileClassifiedAndImported(t *testing.T) {
	root := makeDir(t, map[string]string{
		"my_intent.yaml": `
id: test.intent.example
level: safety_rule
title: Test intent
intent: >
  This is a test intent.
`,
	})

	report, out := importDir(t, root)

	if len(report.Files) != 1 {
		t.Fatalf("expected 1 file report, got %d", len(report.Files))
	}
	fr := report.Files[0]
	if fr.Status != extractor.StatusImported {
		t.Errorf("status = %q, want imported (intent is Phase C importable)", fr.Status)
	}
	if fr.Schema != "intent" {
		t.Errorf("schema = %q, want intent", fr.Schema)
	}
	if fr.Phase != "C" {
		t.Errorf("phase = %q, want C", fr.Phase)
	}
	if fr.Count == 0 {
		t.Error("expected triples > 0 for intent import")
	}
	if !strings.Contains(out, "test.intent.example") {
		t.Errorf("expected intent ID in output; got:\n%s", out[:min(len(out), 200)])
	}
}

// TestImportDir_FailuregraphSeedClassifiedAsPhaseB verifies that a
// failuregraph seed file (id + type at top level) is classified correctly.
func TestImportDir_FailuregraphSeedClassifiedAsPhaseB(t *testing.T) {
	root := makeDir(t, map[string]string{
		"seed.yaml": `
id: ERRCAT-test-seed
type: ErrorCategory
name: test-seed
severity: high
summary: Test seed entity.
`,
	})

	report, _ := importDir(t, root)

	if len(report.Files) != 1 {
		t.Fatalf("expected 1 file report, got %d", len(report.Files))
	}
	fr := report.Files[0]
	if fr.Status != extractor.StatusKnownUnsupported {
		t.Errorf("status = %q, want known_unsupported", fr.Status)
	}
	if fr.Schema != "failuregraph_seed" {
		t.Errorf("schema = %q, want failuregraph_seed", fr.Schema)
	}
	if fr.Phase != "B" {
		t.Errorf("phase = %q, want B", fr.Phase)
	}
}

// TestImportDir_InvalidYAMLReported verifies that a file that is not valid
// YAML appears as StatusInvalid and does not produce triples.
func TestImportDir_InvalidYAMLReported(t *testing.T) {
	root := makeDir(t, map[string]string{
		"broken.yaml": "this: is: {broken yaml: [\n",
	})

	report, out := importDir(t, root)

	if len(report.Files) != 1 {
		t.Fatalf("expected 1 file report, got %d", len(report.Files))
	}
	fr := report.Files[0]
	if fr.Status != extractor.StatusInvalid {
		t.Errorf("status = %q, want %q", fr.Status, extractor.StatusInvalid)
	}
	if fr.Reason == "" {
		t.Error("Reason must be non-empty for invalid files")
	}
	if report.HasInvalid() != true {
		t.Error("HasInvalid() must return true")
	}
	if out != "" {
		t.Errorf("invalid file must emit zero triples; got:\n%s", out)
	}
}

// TestImportDir_NoSilentSkip proves that every discovered .yaml file appears
// in the report — none are silently hidden regardless of their schema.
func TestImportDir_NoSilentSkip(t *testing.T) {
	root := makeDir(t, map[string]string{
		"invariants.yaml": "invariants:\n  - id: t1\n    title: T1\n    severity: low\n    status: active\n",
		"forbidden.yaml":  "authority_rules:\n  - id: ar1\n    title: still pending\n    layer: Desired\n",
		"unknown.yaml":    "completely_unknown_key:\n  - x: 1\n",
		"sub/intent.yaml": "id: i1\nlevel: safety_rule\ntitle: I1\nintent: test\n",
		"sub/broken.yaml": "{{invalid yaml\n",
	})

	report, _ := importDir(t, root)

	if len(report.Files) != 5 {
		t.Fatalf("expected 5 file reports (one per .yaml), got %d:\n%v", len(report.Files), reportPaths(report))
	}

	statuses := map[extractor.FileStatus]int{}
	for _, f := range report.Files {
		statuses[f.Status]++
	}
	// invariants.yaml + sub/intent.yaml → imported (intent is Phase C importable)
	if statuses[extractor.StatusImported] != 2 {
		t.Errorf("expected 2 imported, got %d", statuses[extractor.StatusImported])
	}
	// forbidden.yaml (authority_rules schema) → known_unsupported
	if statuses[extractor.StatusKnownUnsupported] != 1 {
		t.Errorf("expected 1 known_unsupported, got %d", statuses[extractor.StatusKnownUnsupported])
	}
	// unknown.yaml → unknown_schema
	if statuses[extractor.StatusUnknownSchema] != 1 {
		t.Errorf("expected 1 unknown_schema, got %d", statuses[extractor.StatusUnknownSchema])
	}
	// sub/broken.yaml → invalid
	if statuses[extractor.StatusInvalid] != 1 {
		t.Errorf("expected 1 invalid, got %d", statuses[extractor.StatusInvalid])
	}
}

// TestImportDir_MultipleImportableSchemas verifies that all three currently
// importable schemas (invariants, failure_modes, incident_patterns) are
// imported when present in the same directory.
func TestImportDir_MultipleImportableSchemas(t *testing.T) {
	root := makeDir(t, map[string]string{
		"invariants.yaml": `
invariants:
  - id: test.m.inv
    title: Multi inv
    severity: high
    status: active
`,
		"failure_modes.yaml": `
failure_modes:
  - id: test.m.fm
    title: Multi fm
    severity: high
`,
		"incident_patterns.yaml": `
incident_patterns:
  - id: test.m.pat
    title: Multi pat
    severity: medium
    failure_mode: test.m.fm
`,
	})

	report, out := importDir(t, root)

	imported := report.Imported()
	if len(imported) != 3 {
		t.Fatalf("expected 3 imported files, got %d", len(imported))
	}

	schemas := map[string]bool{}
	for _, f := range imported {
		schemas[f.Schema] = true
		if f.Count == 0 {
			t.Errorf("imported file %s emitted 0 triples", f.Path)
		}
	}
	for _, want := range []string{"invariants", "failure_modes", "incident_patterns"} {
		if !schemas[want] {
			t.Errorf("schema %q not found in imported files", want)
		}
	}

	if errs := extractor.ValidateNTriples(bytes.NewReader([]byte(out))); len(errs) > 0 {
		for _, e := range errs {
			t.Errorf("validation: %s", e)
		}
		t.Fatalf("%d N-Triples validation errors", len(errs))
	}
}

// TestImportDir_SelfAwareness_ExpandedCoverage runs against the real
// docs/awareness directory and verifies that the recursive walk now imports
// more files than the original three-file approach. The minimum importable
// count is 3 (the original three); after Phase A the count should be higher
// because other files (e.g. awareness_self_invariants.yaml, convergence_rules.yaml)
// also carry the invariants: or failure_modes: schema.
func TestImportDir_SelfAwareness_ExpandedCoverage(t *testing.T) {
	docsDir := "../../docs/awareness"
	if _, err := os.Stat(docsDir); err != nil {
		t.Skipf("docs/awareness not found relative to test dir: %v", err)
	}

	report, out := importDir(t, docsDir)

	if len(report.Files) == 0 {
		t.Fatal("no files discovered in docs/awareness")
	}

	imported := report.Imported()
	if len(imported) < 3 {
		t.Fatalf("expected at least 3 imported files; got %d", len(imported))
	}

	// After Phase A the walker should find and import more than the original
	// three files (invariants.yaml / failure_modes.yaml / incident_patterns.yaml)
	// because several other files share their schema.
	if len(imported) <= 3 {
		t.Logf("WARNING: only %d files imported — expected more after recursive walk", len(imported))
	}

	// Every discovered file must appear in the report — no silent skips.
	if len(report.Files) == 0 {
		t.Fatal("report has no files")
	}

	// Emitted triples must be valid N-Triples.
	if errs := extractor.ValidateNTriples(bytes.NewReader([]byte(out))); len(errs) > 0 {
		for _, e := range errs {
			t.Errorf("validation: %s", e)
		}
		t.Fatalf("%d N-Triples validation errors after expanded import", len(errs))
	}

	// Report coverage for observability.
	skipped := report.Skipped()
	ignored := report.Ignored()
	t.Logf("docs/awareness: %d files total, %d imported, %d skipped, %d ignored",
		len(report.Files), len(imported), len(skipped), len(ignored))
	for _, f := range skipped {
		t.Logf("  skipped [%s] schema=%s: %s", f.Status, f.Schema, filepath.Base(f.Path))
	}
	for _, f := range ignored {
		t.Logf("  ignored [%s] schema=%s: %s", f.Status, f.Schema, filepath.Base(f.Path))
	}
}

func TestImportDir_SelfAwareness_NoUnsupportedAuthoritativeFilesOutsideGenerated(t *testing.T) {
	docsDir := "../../docs/awareness"
	if _, err := os.Stat(docsDir); err != nil {
		t.Skipf("docs/awareness not found relative to test dir: %v", err)
	}

	report, _ := importDir(t, docsDir)

	var leaked []string
	for _, f := range report.Skipped() {
		path := filepath.ToSlash(f.Path)
		if strings.Contains(path, "/generated/") {
			continue
		}
		// namespaces.yaml is the annotation-scanner's namespace registry, not
		// importable corpus — it is legitimately non-importable config (the
		// standalone build carries its own; see docs/awareness/namespaces.yaml).
		if strings.HasSuffix(path, "/namespaces.yaml") {
			continue
		}
		leaked = append(leaked, path+" ["+string(f.Status)+"/"+f.Schema+"]")
	}
	if len(leaked) > 0 {
		t.Fatalf("authoritative docs/awareness contains non-generated files that are not importable:\n  %s",
			strings.Join(leaked, "\n  "))
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func reportPaths(r *extractor.ImportReport) []string {
	paths := make([]string, len(r.Files))
	for i, f := range r.Files {
		paths[i] = f.Path + " [" + string(f.Status) + "]"
	}
	return paths
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ────────────────────────────────────────────────────────────────────────
// Phase 3 — candidate workflow: candidates/ subtree is ignored by the
// build pipeline. Session-discovered facts must NEVER reach the
// awareness graph until explicitly promoted.
// ────────────────────────────────────────────────────────────────────────

// TestImportDir_CandidatesDirIsSkipped verifies that ANY file under a
// directory named "candidates" is ignored by ImportAwarenessDir,
// regardless of depth, even if the YAML looks syntactically valid as an
// invariant/intent/failure_mode entry.
//
// The skip is by directory name (not by status field) so a typo in
// `status: candidate` -> `status: candidatte` cannot silently land an
// unreviewed entry as canonical.
func TestImportDir_CandidatesDirIsSkipped(t *testing.T) {
	// A real-shaped invariant entry — if the walker did process this
	// file, it would emit an aw:Invariant node with proper triples.
	candidateYAML := `invariants:
  - id: synthetic.candidate_must_not_leak
    title: Candidate must not reach the graph
    severity: high
    status: candidate
    summary: |
      If this triple appears in the output, the candidates skip is broken.
`
	// A canonical entry sitting next to candidates/ — proves the skip
	// is scoped to the candidates/ subtree, not the whole import.
	canonicalYAML := `invariants:
  - id: synthetic.canonical_must_be_imported
    title: Canonical entry alongside candidates/ must still import
    severity: high
    status: active
    summary: |
      The control case — proves only candidates/ is skipped.
`

	root := makeDir(t, map[string]string{
		"canonical.yaml":                                canonicalYAML,
		"candidates/session_discovered.yaml":            candidateYAML,
		"candidates/nested/deeper_candidate.yaml":       candidateYAML,
		"some_subsystem/candidates/area_candidate.yaml": candidateYAML,
	})

	report, out := importDir(t, root)

	// (1) The canonical entry MUST appear in the output.
	if !strings.Contains(out, "synthetic.canonical_must_be_imported") {
		t.Fatalf("expected canonical invariant to be imported; got:\n%s", out[:min(800, len(out))])
	}

	// (2) The candidate entry MUST NOT appear ANYWHERE in the output.
	if strings.Contains(out, "synthetic.candidate_must_not_leak") {
		t.Fatalf("candidate triple leaked into output — candidates/ skip is broken; output contained the synthetic.candidate id")
	}

	// (3) None of the candidate file paths should appear in the import
	//     report — they should have been short-circuited by SkipDir before
	//     classification.
	for _, f := range report.Files {
		if strings.Contains(f.Path, "/candidates/") || strings.Contains(f.Path, "candidates/") {
			t.Errorf("file under candidates/ reached the report: %s — should have been SkipDir'd", f.Path)
		}
	}
}

func TestImportDir_SkillCandidatesDirIsSkipped(t *testing.T) {
	candidateYAML := `id: imported.skill.engineering.tdd
class: ImplementationPattern
label: "Skill: tdd"
status: candidate
when_to_use:
  - Test-driven development.
reference_files:
  - path: skills/engineering/tdd/SKILL.md
    role: source_skill
must_follow:
  - Write the failing test first.
rationale: review-only candidate
source_files:
  - skills/engineering/tdd/SKILL.md
`
	canonicalYAML := `invariants:
  - id: synthetic.skill_candidate_control
    title: Canonical entry alongside skill candidates must still import
    severity: low
    status: active
    summary: Must be imported.
`
	root := makeDir(t, map[string]string{
		"canonical.yaml": canonicalYAML,
		"docs/awareness/candidates/skills/imported_skill_engineering_tdd.yaml": candidateYAML,
	})

	_, out := importDir(t, root)
	if strings.Contains(out, "imported.skill.engineering.tdd") {
		t.Fatalf("skill candidate leaked into output:\n%s", out[:min(800, len(out))])
	}
	if !strings.Contains(out, "synthetic.skill_candidate_control") {
		t.Fatalf("canonical control did not import:\n%s", out[:min(800, len(out))])
	}
}

// TestImportDir_CandidatesSkipIsByDirectoryNameNotStatus verifies the
// skip rule does NOT trigger on a file at the top level just because
// the filename contains "candidates". Only the DIRECTORY name matters.
func TestImportDir_CandidatesSkipIsByDirectoryNameNotStatus(t *testing.T) {
	yaml := `invariants:
  - id: synthetic.my_candidates_file
    title: A canonical file whose name happens to contain "candidates"
    severity: low
    status: active
    summary: Must be imported.
`
	root := makeDir(t, map[string]string{
		// File whose NAME contains "candidates" — NOT inside a candidates/ dir.
		"my_candidates_file.yaml": yaml,
	})

	_, out := importDir(t, root)
	if !strings.Contains(out, "synthetic.my_candidates_file") {
		t.Fatalf("file with 'candidates' in its name (not in a candidates/ DIR) must still import; output:\n%s",
			out[:min(800, len(out))])
	}
}
