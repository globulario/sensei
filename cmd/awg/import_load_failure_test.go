package main

// import must not fabricate a diagnosis, and must not let a destructive remedy be
// read as implied by an exit code it cannot interpret.
//
// Observed 2026-09-13: `sensei import --refresh … --store-url …` failed with
// PUBLICATION_REFUSED (dirty awareness corpus, mutation_started: false) and import
// reported "a scoped --repo update needs a non-empty store; seed with `sensei
// build --all` first". The store was not empty, that was not the cause, and the
// recommended remedy replaces the ENTIRE store. runBuild returns only an int, so
// import has no cause to report — which is exactly why it must not invent one.

import (
	"strings"
	"testing"
)

func TestImportLoadFailureNoticeDoesNotFabricateACause(t *testing.T) {
	notice := importLoadFailureNotice()

	// It must not assert a specific cause it cannot know.
	for _, fabricated := range []string{
		"needs a non-empty store",
		"empty store",
		"seed with",
	} {
		if strings.Contains(notice, fabricated) {
			t.Errorf("the notice asserts a cause import cannot know (%q): %q", fabricated, notice)
		}
	}

	// It must point at where the real reason was printed.
	if !strings.Contains(notice, "above") {
		t.Errorf("the notice does not point at the build output that carries the actual reason: %q", notice)
	}

	// It must say why it cannot diagnose, so the silence is not mistaken for
	// there being nothing to say.
	if !strings.Contains(notice, "exit code") {
		t.Errorf("the notice does not say why import cannot diagnose the failure: %q", notice)
	}
}

// --all must never read as the remedy for an uninterpreted exit code. If it is
// named at all, it must be named as a warning.
func TestImportLoadFailureNoticeWarnsAgainstTheDestructiveRemedy(t *testing.T) {
	notice := importLoadFailureNotice()
	if !strings.Contains(notice, "--all") {
		t.Skip("the notice no longer mentions --all at all, which also satisfies the rule")
	}
	lower := strings.ToLower(notice)
	if !strings.Contains(lower, "do not run") && !strings.Contains(lower, "must not") {
		t.Errorf("--all is named without being refused: %q", notice)
	}
	if !strings.Contains(lower, "entire store") && !strings.Contains(lower, "destructive") {
		t.Errorf("--all is named without saying what it destroys: %q", notice)
	}
}

// The marker note must describe the real contract, not a consequence it got wrong.
//
// It said "no --graph-marker-file given; a live/served store may report
// freshness-stale for briefing until re-certified". But build DOES default the
// marker — cmd_build.go:242 and :705 call defaultRuntimeMarkerFile() when the flag
// is empty — so the marker is written and freshness is not necessarily stale. The
// real hazard is different and worse: defaultRuntimeMarkerFile resolves via
// resolveProjectRoot(""), i.e. from the CURRENT DIRECTORY, so the marker is bound
// to whatever project cwd happens to name, which need not be the checkout being
// refreshed.
func TestTheMarkerNoteNamesTheCwdHazardNotAFalseConsequence(t *testing.T) {
	note := importMarkerOmittedNotice()
	if strings.Contains(note, "freshness-stale") {
		t.Errorf("the note still claims freshness would go stale, which build's own default contradicts: %q", note)
	}
	for _, want := range []string{"current directory", "--graph-marker-file"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note omits %q: %q", want, note)
		}
	}
}
