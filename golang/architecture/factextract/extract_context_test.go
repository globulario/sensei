// SPDX-License-Identifier: AGPL-3.0-only

package factextract

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeExtractFixture is a repository with enough Go files that a cancelled run
// has somewhere to stop.
func writeExtractFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/extract\n")
	for _, name := range []string{"a", "b", "c", "d"} {
		write("pkg"+name+"/"+name+".go", "package pkg"+name+"\n\nfunc Do"+strings.ToUpper(name)+"() error { return nil }\n")
	}
	return root
}

// #131 world 1: the AST pass dominates a large repository and could not observe
// a deadline, so a caller enforcing a ceiling bounded the stages around it and
// let this one run on — and the run could still report itself completed.
func TestExtractContextStopsWhenTheCallerCancels(t *testing.T) {
	root := writeExtractFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	report, err := ExtractContext(ctx, root, Options{IncludeTests: true})
	if err != nil {
		t.Fatalf("a cancelled extraction must report a partial document, not an error: %v", err)
	}
	if len(report.Limitations) == 0 {
		t.Fatal("a truncated extraction that does not say it is truncated is worse than one that overran")
	}
	blocking := false
	said := false
	for _, lim := range report.Limitations {
		if strings.Contains(lim.Reason, "stopped") {
			said = true
			if lim.Blocking {
				blocking = true
			}
		}
	}
	if !said {
		t.Fatalf("no limitation says where the run stopped: %+v", report.Limitations)
	}
	if !blocking {
		t.Fatalf("an involuntary truncation must be blocking: %+v", report.Limitations)
	}
	if len(report.Candidates) != 0 {
		t.Fatalf("candidates were synthesized from a truncated fact set: %d", len(report.Candidates))
	}
}

// The ceiling must bind the AST pass itself, not merely the stages around it.
// A cancellation that only surfaced between stages would let a whole-repository
// pass run to completion first, which is the state #131 measured.
func TestExtractContextNamesTheASTStageItStoppedIn(t *testing.T) {
	root := writeExtractFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	report, err := ExtractContext(ctx, root, Options{IncludeTests: true})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, lim := range report.Limitations {
		if lim.Source == "go_ast_extractor" && strings.Contains(lim.Reason, "of") && strings.Contains(lim.Reason, "file(s)") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the AST stage did not report how far it got: %+v", report.Limitations)
	}
}

// An uncancelled run is unchanged: same facts, same candidates, no limitation
// invented by the new plumbing. Extract keeps its old signature and behaviour.
func TestExtractContextMatchesUnboundedExtract(t *testing.T) {
	root := writeExtractFixture(t)

	bounded, err := ExtractContext(context.Background(), root, Options{IncludeTests: true})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := Extract(root, Options{IncludeTests: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(bounded.Facts) != len(plain.Facts) {
		t.Fatalf("facts differ: bounded=%d plain=%d", len(bounded.Facts), len(plain.Facts))
	}
	if len(bounded.Candidates) != len(plain.Candidates) {
		t.Fatalf("candidates differ: bounded=%d plain=%d", len(bounded.Candidates), len(plain.Candidates))
	}
	for _, lim := range bounded.Limitations {
		if strings.Contains(lim.Reason, "stopped") {
			t.Fatalf("an uncancelled run reported a stop: %q", lim.Reason)
		}
	}
}

// The load holds the context and still overruns it: measured on Sensei's own
// repository, a 20-second ceiling produced a 122-second run because
// packages.Load shells out to the Go toolchain and type-checks the module past
// the deadline it was given. So the ceiling binds the document — the stage is
// abandoned and its absence is recorded — rather than being left to a
// cancellation the loader may honour whenever it gets round to it.
func TestExtractContextAbandonsTheSemanticStageAtTheCeiling(t *testing.T) {
	root := writeExtractFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	report, err := ExtractContext(ctx, root, Options{IncludeTests: true})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, lim := range report.Limitations {
		if lim.Source == "go_semantic_extractor" && strings.Contains(lim.Reason, "abandoned") {
			if !lim.Blocking {
				t.Fatal("abandoning a stage must be blocking: the document is missing observations")
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("the semantic stage did not report being abandoned: %+v", report.Limitations)
	}
	for _, f := range report.Facts {
		if f.Extractor == "go_semantic_extractor" {
			t.Fatal("an abandoned stage contributed facts")
		}
	}
}

// A caller with no ceiling gets the old inline path, so nothing about the
// unbounded run changes — no goroutine, no abandonment, same observations.
func TestExtractWithoutACeilingKeepsItsSemanticObservations(t *testing.T) {
	root := writeExtractFixture(t)
	report, err := Extract(root, Options{IncludeTests: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, lim := range report.Limitations {
		if strings.Contains(lim.Reason, "abandoned") {
			t.Fatalf("an unbounded run abandoned a stage: %q", lim.Reason)
		}
	}
}
