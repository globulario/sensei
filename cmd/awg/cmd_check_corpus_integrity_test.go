// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `sensei check` must make it impossible to report an incomplete corpus import
// as clean.
//
// Origin, 2026-08-05: `sensei propose` appended an entry to
// docs/awareness/invariants.yaml at 2-space list indent. That file's convention
// is 0-space, so the append made the file unparseable. The importer dropped it,
// and `sensei check` printed
//
//	docs/awareness: 210 files, 117939 triples [35 not imported]
//	All checks passed.
//
// and exited 0. Roughly 11,000 triples and three whole files were missing from
// the published graph; briefings were served from the truncated corpus for an
// entire session. Skipped files only failed under -strict, and only for two of
// the four non-imported statuses, so the default invocation — the one CI and
// humans actually run — could not see the hole.
//
// The law these tests encode: ABSENCE OF OBSERVED FAILURE IS NOT EVIDENCE THAT
// THE OBSERVER WAS ALIVE.

// captureBoth runs fn with stdout and stderr redirected, returning both.
// runCheck reports the census on stdout and the failures on stderr, so a test
// that only reads one of them proves half the behaviour.
func captureBoth(t *testing.T, fn func() int) (string, string, int) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	os.Stdout, os.Stderr = wOut, wErr

	outC := make(chan string)
	errC := make(chan string)
	go func() { var b bytes.Buffer; _, _ = io.Copy(&b, rOut); outC <- b.String() }()
	go func() { var b bytes.Buffer; _, _ = io.Copy(&b, rErr); errC <- b.String() }()

	code := fn()

	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	return <-outC, <-errC, code
}

// wellFormedInvariants is a minimal corpus file using the 0-space list
// convention that docs/awareness/invariants.yaml actually uses.
const wellFormedInvariants = `invariants:
- id: example.first_invariant
  title: First invariant
  status: active
  description: A well-formed entry at the file's own indent convention.
`

// proposeMalformedInvariants reproduces EXACTLY what `sensei propose` emitted:
// a correct entry followed by an appended entry at 2-space list indent. The
// nested keys of the appended item then sit at column 5, which YAML reads as a
// sequence item inside the previous item's block mapping.
//
// This is the fixture the directive asked for. If `sensei propose` is ever
// fixed to match each file's convention, this fixture must STAY — it is the
// shape the checker has to catch, whoever produces it.
const proposeMalformedInvariants = `invariants:
- id: example.first_invariant
  title: First invariant
  status: active
  description: A well-formed entry at the file's own indent convention.
  - id: example.appended_by_propose
    title: Appended at the wrong indent
    status: active
    description: What sensei propose actually wrote.
`

func writeCorpus(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestCheckFailsOnProposeMalformedIndentation is the primary regression: the
// exact bytes that shipped the silent hole must now fail the check.
func TestCheckFailsOnProposeMalformedIndentation(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"invariants.yaml": proposeMalformedInvariants,
	})

	stdout, stderr, code := captureBoth(t, func() int {
		return runCheck([]string{"-input", dir})
	})

	if code == 0 {
		t.Fatalf("check PASSED on a corpus whose only governed file is unparseable — "+
			"this is precisely the 2026-08-05 defect\nstdout:\n%s", stdout)
	}
	if !strings.Contains(stderr, "could not be parsed") {
		t.Errorf("failure must name the cause; stderr:\n%s", stderr)
	}
	// The operator must be told the content is GONE, not merely that a count
	// moved. "[1 not imported]" was the old output and it taught nobody.
	if !strings.Contains(stderr, "ABSENT from the graph") {
		t.Errorf("failure must state that the content is absent from the graph; stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "invariants.yaml") {
		t.Errorf("failure must name the offending file; stderr:\n%s", stderr)
	}
}

// TestCheckPassesOnTheSameCorpusCorrectlyIndented is the negative control. If
// this failed too, the test above would be proving nothing more than "the
// fixture is rejected", and a checker that rejects everything is as useless as
// one that accepts everything.
func TestCheckPassesOnTheSameCorpusCorrectlyIndented(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"invariants.yaml": wellFormedInvariants,
	})

	stdout, stderr, code := captureBoth(t, func() int {
		return runCheck([]string{"-input", dir})
	})

	if code != 0 {
		t.Fatalf("check FAILED on a well-formed corpus (exit %d)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	if !strings.Contains(stdout, "All checks passed") {
		t.Errorf("expected success line; stdout:\n%s", stdout)
	}
}

// TestCheckReportsExplicitCensus pins the counts the directive requires. A
// single opaque tally is what let an 11k-triple hole hide inside "[35 not
// imported]".
func TestCheckReportsExplicitCensus(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"invariants.yaml": wellFormedInvariants,
	})

	stdout, _, _ := captureBoth(t, func() int {
		return runCheck([]string{"-input", dir})
	})

	for _, want := range []string{
		"source files discovered",
		"source files parsed",
		"source files imported",
		"source files rejected",
		"triples produced",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("census is missing %q; stdout:\n%s", want, stdout)
		}
	}
}

// TestCheckFailsOnUndeclaredSchema covers "any expected awareness file is
// skipped". A file discovered inside the corpus and then dropped because
// nothing recognizes it is a gap in coverage, not a neutral fact — and it must
// fail without needing -strict, because -strict is not what CI runs.
//
// The legitimate way to silence this is to DECLARE the exclusion in the schema
// table, which is how docs/awareness/relation_targets.yaml was resolved: it had
// been silently dropped for exactly this reason.
func TestCheckFailsOnUndeclaredSchema(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"invariants.yaml": wellFormedInvariants,
		"mystery.yaml":    "something_nobody_registered:\n  - id: x\n",
	})

	stdout, stderr, code := captureBoth(t, func() int {
		return runCheck([]string{"-input", dir}) // NOTE: no -strict
	})

	if code == 0 {
		t.Fatalf("check PASSED with an undeclared file silently dropped\nstdout:\n%s", stdout)
	}
	if !strings.Contains(stderr, "unrecognized schema") || !strings.Contains(stderr, "mystery.yaml") {
		t.Errorf("failure must name the dropped file and why; stderr:\n%s", stderr)
	}
}

// TestCheckFailsWhenCorpusFoundButNothingImported is the "scanner never saw the
// corpus" guard stated directly: files present, zero read.
func TestCheckFailsWhenCorpusFoundButNothingImported(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"a.yaml": proposeMalformedInvariants,
		"b.yaml": "invariants:\n- id: [unclosed\n",
	})

	stdout, stderr, code := captureBoth(t, func() int {
		return runCheck([]string{"-input", dir})
	})

	if code == 0 {
		t.Fatalf("check PASSED with 0 of 2 files imported\nstdout:\n%s", stdout)
	}
	if !strings.Contains(stderr, "NONE imported") {
		t.Errorf("a corpus that imported nothing must say so explicitly; stderr:\n%s", stderr)
	}
}

// TestCensusAlwaysReconciles is a property check over the bucketing itself:
// every discovered file must land in exactly one bucket. If the accounting can
// lose a file, none of the other numbers can be trusted — which is the deeper
// version of the original bug.
func TestCensusAlwaysReconciles(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"invariants.yaml": wellFormedInvariants,
		"broken.yaml":     proposeMalformedInvariants,
		"mystery.yaml":    "something_nobody_registered:\n  - id: x\n",
	})

	stdout, stderr, _ := captureBoth(t, func() int {
		return runCheck([]string{"-input", dir})
	})

	if strings.Contains(stderr, "census does not reconcile") {
		t.Errorf("census failed to reconcile on a mixed corpus; stderr:\n%s", stderr)
	}
	// And the mixed corpus must still fail overall — it contains two defects.
	if strings.Contains(stdout, "All checks passed") {
		t.Error("a corpus with an unparseable file and an undeclared file must not pass")
	}
}
