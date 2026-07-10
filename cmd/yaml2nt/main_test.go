// SPDX-License-Identifier: AGPL-3.0-only

package main

// Tests for the yaml2nt CLI wrapper. We exercise run() directly with
// io.Writer params instead of exec.Command — that's faster, avoids the
// "where is the test binary?" pathing question, and lets each test
// assert on stderr without parsing process output.
//
// Scope: the CLI's job-shape, not the importer's correctness. The
// importer has its own test suite under golang/extractor; duplicating it
// here would couple two test layers and slow CI for no gain. The tests
// below pin:
//
//   - happy path produces non-empty triples on stdout
//   - -output mode produces a valid file on disk
//   - -input missing returns a user-error exit code with a clear message
//   - -input pointing at a non-existent path returns a user-error exit
//     code (not a runtime error — the user mistyped)
//
// All tests use the existing extractor testdata fixtures rather than
// inventing new ones. The relative path "../../golang/extractor/testdata"
// is stable: `go test` always runs in the package directory.

import (
	"bytes"
	"github.com/globulario/sensei/golang/extractor"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureDir is the on-disk path to the awareness-extractor testdata,
// reachable from this package. Centralised so a future move only edits
// one constant.
const fixtureDir = "../../golang/extractor/testdata"

// TestRun_StdoutMode_ProducesValidNTriples is the happy-path smoke test.
// Invokes the CLI with -input pointing at the existing fixture set and
// asserts: exit 0, stdout non-empty, output looks like N-Triples
// (a single sample line is enough — full validation is the CLI's own
// job before writing).
func TestRun_StdoutMode_ProducesValidNTriples(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-input", fixtureDir}, &stdout, &stderr)

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d. stderr:\n%s", code, exitOK, stderr.String())
	}
	if stdout.Len() == 0 {
		t.Fatal("stdout is empty; expected N-Triples")
	}
	// One representative shape check. Full N-Triples validation runs
	// inside run() before write — if it failed, exit would be non-zero
	// and we'd have failed above.
	if !strings.Contains(stdout.String(), "<https://globular.io/awareness#") {
		t.Errorf("stdout does not look like awareness N-Triples; first 200 chars:\n%s",
			truncate(stdout.String(), 200))
	}
	// The summary line MUST go to stderr, not stdout — otherwise the
	// stdout stream is contaminated when piped to a SPARQL loader.
	if !strings.Contains(stderr.String(), "triples emitted") {
		t.Errorf("stderr missing summary line; got:\n%s", stderr.String())
	}
}

// TestRun_OutputFileMode_WritesValidFile pins the -output flag path:
// the CLI must create the file, write the same byte stream that stdout
// mode would have produced, and leave stdout empty (so wrapping scripts
// can rely on "if -output is set, nothing on stdout").
func TestRun_OutputFileMode_WritesValidFile(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "awareness.nt")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-input", fixtureDir, "-output", outPath}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d. stderr:\n%s", code, exitOK, stderr.String())
	}

	if stdout.Len() != 0 {
		t.Errorf("stdout must be empty when -output is set; got %d bytes:\n%s",
			stdout.Len(), truncate(stdout.String(), 200))
	}

	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("output file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output file is empty")
	}

	contents, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if !strings.Contains(string(contents), "<https://globular.io/awareness#") {
		t.Errorf("output file does not look like awareness N-Triples; first 200 chars:\n%s",
			truncate(string(contents), 200))
	}
	if !strings.Contains(stderr.String(), "wrote "+outPath) {
		t.Errorf("stderr should confirm the wrote path; got:\n%s", stderr.String())
	}
}

// TestRun_MissingInput_ReturnsUserError pins the user-error exit shape.
// flag.Parse with no -input must produce exit code 2 (not 1, which is
// reserved for runtime failures) and the stderr message must name the
// missing flag so operators don't have to guess.
func TestRun_MissingInput_ReturnsUserError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)

	if code != exitUserError {
		t.Fatalf("exit code = %d, want %d (user error). stderr:\n%s",
			code, exitUserError, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must be empty on user error; got:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "-input is required") {
		t.Errorf("stderr should name the missing -input flag; got:\n%s", stderr.String())
	}
}

// TestRun_NonexistentInput_ReturnsUserError pins that mistyped paths are
// classified as user errors (exit 2), not runtime errors (exit 1). The
// distinction matters for shell pipelines and CI alerting — a 1 should
// page someone; a 2 just nudges the operator to re-read the flag.
func TestRun_NonexistentInput_ReturnsUserError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	code := run([]string{"-input", missing}, &stdout, &stderr)

	if code != exitUserError {
		t.Fatalf("exit code = %d, want %d (user error). stderr:\n%s",
			code, exitUserError, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must be empty on user error; got:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), missing) {
		t.Errorf("stderr should mention the offending path; got:\n%s", stderr.String())
	}
}

// TestRun_InputIsFile_ReturnsUserError covers the not-a-directory case
// separately because os.Stat succeeds on a file — without an explicit
// check, the importer would silently produce nothing useful instead of
// rejecting the input. Pins the "must be a directory" contract.
func TestRun_InputIsFile_ReturnsUserError(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "not-a-directory.txt")
	if err := os.WriteFile(regular, []byte("hi"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"-input", regular}, &stdout, &stderr)
	if code != exitUserError {
		t.Fatalf("exit code = %d, want %d (user error). stderr:\n%s",
			code, exitUserError, stderr.String())
	}
	if !strings.Contains(stderr.String(), "not a directory") {
		t.Errorf("stderr should explain the type mismatch; got:\n%s", stderr.String())
	}
}

// TestRun_StrictMode_PassesWhenAllImported pins that -strict exits 0 when
// every file in the input directory is fully imported. The extractor testdata
// fixtures are all importable schemas, so they are the ideal strict-pass case.
func TestRun_StrictMode_PassesWhenAllImported(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-input", fixtureDir, "-strict"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d with all-importable dir. stderr:\n%s",
			code, exitOK, stderr.String())
	}
}

// TestRun_StrictMode_FailsWhenSkipped pins that -strict exits 1 when one or
// more YAML files have an unrecognized (unknown_schema) or invalid schema.
// known_unsupported files are explicitly classified and do NOT cause strict
// mode to fail — the contract is "no silently dropped data", not "all schemas
// must have importers".
func TestRun_StrictMode_FailsWhenSkipped(t *testing.T) {
	// Build a temp dir with one importable file and one unknown-schema file.
	// Using a top-level key that is not registered in the keySchemas table
	// and has no special composite detection.
	dir := t.TempDir()
	writeFile(t, dir, "invariants.yaml", `
invariants:
  - id: strict.test.inv
    title: Strict test invariant
    severity: low
    status: active
`)
	writeFile(t, dir, "completely_unknown.yaml", `
completely_unknown_top_level_key_xyzzy:
  - id: unknown.test
    title: This schema is not recognized and should fail strict mode
`)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-input", dir, "-strict"}, &stdout, &stderr)
	if code != exitRuntime {
		t.Fatalf("exit code = %d, want %d (runtime) for strict with unknown-schema file. stderr:\n%s",
			code, exitRuntime, stderr.String())
	}
	if !strings.Contains(stderr.String(), "strict mode") {
		t.Errorf("stderr should mention strict mode; got:\n%s", stderr.String())
	}
}

// TestRun_StrictMode_PassesWithKnownUnsupported pins that -strict allows
// known_unsupported files (explicitly classified, not silently dropped).
func TestRun_StrictMode_PassesWithKnownUnsupported(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "invariants.yaml", `
invariants:
  - id: strict.ku.inv
    title: Known-unsupported strict test invariant
    severity: low
    status: active
`)
	// authority_rules.yaml is registered as known_unsupported in keySchemas.
	writeFile(t, dir, "authority_rules.yaml", `
authority_rules:
  - id: ar.strict.ku.test
    title: Known-unsupported authority rule
    layer: Desired
`)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-input", dir, "-strict"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d — known_unsupported must not fail strict. stderr:\n%s",
			code, exitOK, stderr.String())
	}
}

// TestRun_StrictMode_SummaryAlwaysPrinted verifies that the import summary
// appears in stderr even in non-strict mode, so operators always see coverage.
func TestRun_StrictMode_SummaryAlwaysPrinted(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-input", fixtureDir}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d. stderr:\n%s", code, exitOK, stderr.String())
	}
	if !strings.Contains(stderr.String(), "import summary") {
		t.Errorf("stderr should always contain 'import summary'; got:\n%s", stderr.String())
	}
}

func TestRun_SummaryShowsIgnoredNonAuthority(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "annotation_report.yaml", `
scanned_files:
  - path: golang/server/main.go
unsupported_keys: []
`)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-input", dir}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d. stderr:\n%s", code, exitOK, stderr.String())
	}
	if !strings.Contains(stderr.String(), "ignored non-authority") {
		t.Fatalf("stderr should show ignored non-authority summary; got:\n%s", stderr.String())
	}
	if strings.Contains(stderr.String(), "known_unsupported") {
		t.Fatalf("ignored non-authority must not be reported as known_unsupported; got:\n%s", stderr.String())
	}
}

// TestRun_MultipleInputDirs_MergesTriples pins that repeated -input flags
// combine triples from all directories into one output stream. Two dirs
// with distinct invariant IDs must both appear in the NT output.
func TestRun_MultipleInputDirs_MergesTriples(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	writeFile(t, dir1, "invariants.yaml", `
invariants:
  - id: multi.input.inv.alpha
    title: Alpha invariant from dir1
    severity: low
    status: active
`)
	writeFile(t, dir2, "invariants.yaml", `
invariants:
  - id: multi.input.inv.beta
    title: Beta invariant from dir2
    severity: low
    status: active
`)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-input", dir1, "-input", dir2}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d. stderr:\n%s", code, exitOK, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "multi.input.inv.alpha") {
		t.Errorf("output missing invariant from dir1; got first 400 chars:\n%s", truncate(out, 400))
	}
	if !strings.Contains(out, "multi.input.inv.beta") {
		t.Errorf("output missing invariant from dir2; got first 400 chars:\n%s", truncate(out, 400))
	}
}

// writeFile is a small helper for creating test fixture files.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile %s: %v", name, err)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// TestDedupNTriples pins meta.diagnostic_output_must_be_bounded for the
// yaml2nt write step: identical triples emitted by multiple extractors must
// collapse to a single line so the seed and the "triples emitted" log report
// the actual semantic count, not the raw line count.
func TestDedupNTriples(t *testing.T) {
	in := []byte(strings.Join([]string{
		`<s> <p> "a" .`,
		`<s> <p> "b" .`,
		`<s> <p> "a" .`, // duplicate of line 1
		``,              // blank — pass-through
		`# comment`,     // comment — pass-through
		`<s> <p> "b" .`, // duplicate of line 2
		`<s> <p> "c" .`,
	}, "\n"))

	out, uniq, dup := extractor.DedupNTriples(in)
	if uniq != 3 {
		t.Fatalf("uniq=%d, want 3", uniq)
	}
	if dup != 2 {
		t.Fatalf("dup=%d, want 2", dup)
	}
	got := string(out)
	want := strings.Join([]string{
		`<s> <p> "a" .`,
		`<s> <p> "b" .`,
		``,
		`# comment`,
		`<s> <p> "c" .`,
	}, "\n")
	if got != want {
		t.Fatalf("dedup output mismatch:\ngot:\n%s\n---\nwant:\n%s", got, want)
	}
}

func TestDedupNTriples_NoDuplicates(t *testing.T) {
	in := []byte(`<s> <p> "a" .` + "\n" + `<s> <p> "b" .` + "\n")
	out, uniq, dup := extractor.DedupNTriples(in)
	if uniq != 2 || dup != 0 {
		t.Fatalf("uniq=%d dup=%d, want 2/0", uniq, dup)
	}
	if string(out) != string(in) {
		t.Fatalf("no-dup path must not modify input")
	}
}

func TestDedupNTriples_Empty(t *testing.T) {
	out, uniq, dup := extractor.DedupNTriples(nil)
	if uniq != 0 || dup != 0 || len(out) != 0 {
		t.Fatalf("empty: out=%q uniq=%d dup=%d", out, uniq, dup)
	}
}
